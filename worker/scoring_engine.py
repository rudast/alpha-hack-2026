"""Deterministic, database-free scoring of a complete application snapshot.

The bundled rates and thresholds are demonstration policy, not calibrated
credit-risk estimates. Missing inputs never receive a favourable default.
"""

from __future__ import annotations

import math
from decimal import Decimal, ROUND_HALF_UP
from functools import lru_cache
from pathlib import Path
from statistics import pstdev
from typing import Any

import yaml


class MissingData(ValueError):
    def __init__(self, fields: list[str]):
        self.fields = list(dict.fromkeys(fields))
        super().__init__(", ".join(self.fields))


def _number(data: dict[str, Any], key: str, *, zero: bool = True) -> float:
    value = data.get(key)
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value):
        raise MissingData([key])
    if value < 0 or (not zero and value == 0):
        raise MissingData([key])
    return float(value)


@lru_cache(maxsize=1)
def load_config() -> dict[str, Any]:
    with (Path(__file__).parent / "scoring.yaml").open(encoding="utf-8") as stream:
        config = yaml.safe_load(stream)
    if not isinstance(config, dict):
        raise ValueError("scoring configuration must be an object")
    weights = config["weights"]
    for code, weight in weights.items():
        if not isinstance(weight, (int, float)) or weight <= 0:
            raise ValueError(f"invalid weight {code}")
    for code, bins in config["bins"].items():
        edges, values = bins["edges"], bins["values"]
        if len(values) != len(edges) + 1 or any(a >= b for a, b in zip(edges, edges[1:])):
            raise ValueError(f"invalid bins {code}")
        if any(not 0 <= value <= 1 for value in values):
            raise ValueError(f"invalid bin share {code}")
        if values != sorted(values) and values != sorted(values, reverse=True):
            raise ValueError(f"nonmonotonic bins {code}")
    for name, policy in config["classes"].items():
        if policy["counter"] >= policy["approve"] or policy["auto_limit"] <= 0:
            raise ValueError(f"invalid class {name}")
    rate = config["rate"]
    if rate["slope_pp_per_point"] <= 0 or rate["min_pp"] >= rate["max_pp"]:
        raise ValueError("invalid rate formula")
    limiters = config["limiters"]
    if not 0 < limiters["pti_max"] < 1 or not 0 < limiters["ltv_max"] < 1:
        raise ValueError("invalid limiters")
    for product, policy in config["products"].items():
        if policy["class"] not in config["classes"]:
            raise ValueError(f"unknown class for {product}")
        codes = config["core_factors"] + policy["factors"]
        if not codes or any(code not in weights for code in codes):
            raise ValueError(f"invalid factors for {product}")
        if not isinstance(policy.get("documents"), list) or any(
                not isinstance(kind, str) or not kind for kind in policy["documents"]):
            raise ValueError(f"invalid documents for {product}")
    return config


def _bin(config: dict[str, Any], code: str, value: float) -> float:
    bins = config["bins"][code]
    for edge, share in zip(bins["edges"], bins["values"]):
        if value < edge:
            return float(share)
    return float(bins["values"][-1])


def _payment(amount: float, months: int, annual_rate_percent: float) -> float:
    monthly = annual_rate_percent / 1200
    return amount / months if monthly == 0 else amount * monthly / (1 - (1 + monthly) ** -months)


# Отраслевой риск — справочник банка, а не данные клиента: если ОКВЭД неизвестен или его нет
# в справочнике, фактор получает нейтральное значение и попадает в data_gaps (scoring.md, раздел 7.4),
# а клиенту ничего не запрашивается.
INDUSTRY_UNKNOWN = "unknown"
INDUSTRY_SHARES = {"low": 1.0, "medium": 0.6, "high": 0.2, INDUSTRY_UNKNOWN: 0.5}


def _industry(config: dict[str, Any], data: dict[str, Any]) -> str:
    explicit = data.get("industry_risk")
    if explicit in ("low", "medium", "high"):
        return explicit
    okved = data.get("okved")
    if not isinstance(okved, str) or not okved:
        return INDUSTRY_UNKNOWN
    for group, prefixes in config["industry_risk"].items():
        if any(okved.startswith(prefix) for prefix in prefixes):
            return group
    return INDUSTRY_UNKNOWN


def _factor_shares(data: dict[str, Any], config: dict[str, Any],
                   amount: float, months: int) -> tuple[dict[str, tuple[float, Any]], list[str], list[str]]:
    product = data["product_id"]
    policy = config["products"][product]
    required = config["core_factors"] + policy["factors"]
    shares: dict[str, tuple[float, Any]] = {}
    knockouts: list[str] = []
    limiters: list[str] = []
    history = data.get("credit_history")
    if not isinstance(history, dict):
        raise MissingData(["credit_history"])
    overdue = _number(history, "current_overdue_days")
    closed = _number(history, "closed_overdue_30d_24m")
    short = _number(history, "overdues_30d_12m")
    history_months = _number(history, "history_months")
    inquiries = _number(history, "inquiries_90d")
    if overdue > 30:
        c1a = 0.0
        knockouts.append("KO1")
    elif overdue > 0:
        c1a = 0.2
    elif closed > 0:
        c1a = 0.4
    elif short >= 3:
        c1a = 0.3
    elif short >= 1:
        c1a = 0.6
    else:
        c1a = 1.0 if history_months >= 12 else 0.5
    shares["C1a"] = (c1a, {"current_overdue_days": overdue,
                           "closed_overdue_30d_24m": closed,
                           "overdues_30d_12m": short, "history_months": history_months})
    shares["C1b"] = (_bin(config, "C1b", inquiries), inquiries)

    debt = _number(data, "debt")
    equity = data.get("equity")
    if isinstance(equity, bool) or not isinstance(equity, (int, float)) or not math.isfinite(equity):
        raise MissingData(["equity"])
    if equity <= 0:
        ratio = None
        shares["C2"] = (0.0, ratio)
        knockouts.append("KO3")
    else:
        ratio = debt / equity
        shares["C2"] = (_bin(config, "C2", ratio), round(ratio, 4))

    age = _number(data, "age_months")
    shares["C3"] = (_bin(config, "C3", age), age)
    registry = data.get("registry")
    if not isinstance(registry, dict):
        raise MissingData(["registry"])
    red, yellow = registry.get("red_flags", []), registry.get("yellow_flags", [])
    if not isinstance(red, list) or not isinstance(yellow, list):
        raise MissingData(["registry.flags"])
    if red:
        knockouts.append("KO2")
    shares["C4a"] = (0.0 if red else 0.5 if yellow else 1.0,
                     {"red_flags": red, "yellow_flags": yellow})
    industry = _industry(config, data)
    shares["C4b"] = (INDUSTRY_SHARES[industry], industry)

    if "P1" in required:
        turnover = _number(data, "turnover_monthly", zero=False)
        pti = _payment(amount, months, config["classes"][policy["class"]]["base_rate"]) / turnover
        shares["P1"] = (_bin(config, "P1", pti), round(pti, 4))
        if pti > config["limiters"]["pti_max"]:
            limiters.append("L-PTI")
    if "P2" in required:
        values = data.get("monthly_turnovers")
        if not isinstance(values, list) or len(values) < 6:
            raise MissingData(["monthly_turnovers"])
        if any(isinstance(x, bool) or not isinstance(x, (int, float)) or not math.isfinite(x) or x < 0 for x in values):
            raise MissingData(["monthly_turnovers"])
        mean = sum(values) / len(values)
        if mean <= 0:
            raise MissingData(["monthly_turnovers"])
        cv = pstdev(values) / mean
        shares["P2"] = (_bin(config, "P2", cv), round(cv, 4))
    if "P3" in required:
        value = _number(data, "collateral_confirmed_value", zero=False)
        collateral_type = data.get("collateral_type")
        if collateral_type not in config["haircut"]:
            raise MissingData(["collateral_type"])
        ltv = amount / (value * config["haircut"][collateral_type])
        shares["P3"] = (_bin(config, "P3", ltv), round(ltv, 4))
        if ltv > config["limiters"]["ltv_max"]:
            limiters.append("L-LTV")
    if "P4" in required:
        down = _number(data, "down_payment")
        price = _number(data, "object_price", zero=False)
        share = down / price
        shares["P4"] = (_bin(config, "P4", share), round(share, 4))
    if "P5" in required:
        has_account = data.get("has_settlement_account")
        if not isinstance(has_account, bool):
            raise MissingData(["has_settlement_account"])
        shares["P5"] = (1.0 if has_account else 0.0, has_account)
        if not has_account:
            knockouts.append("KO5")
    if "P6a" in required or "P6b" in required:
        target_turnover = _number(data, "target_turnover", zero=False)
        target_profit = data.get("target_profit")
        if isinstance(target_profit, bool) or not isinstance(target_profit, (int, float)) or not math.isfinite(target_profit):
            raise MissingData(["target_profit"])
        margin = target_profit / target_turnover
        shares["P6a"] = (_bin(config, "P6a", margin), round(margin, 4))
        if margin < 0:
            knockouts.append("KO4")
        payback = amount / target_profit if target_profit > 0 else math.inf
        shares["P6b"] = (_bin(config, "P6b", payback), round(payback, 4) if math.isfinite(payback) else None)
    if "P7" in required:
        concentration = _number(data, "marketplace_revenue_share")
        if concentration > 100:
            raise MissingData(["marketplace_revenue_share"])
        shares["P7"] = (_bin(config, "P7", concentration), concentration)
    return shares, knockouts, limiters


def _score(data: dict[str, Any], config: dict[str, Any], amount: int, months: int) -> dict[str, Any]:
    required = config["core_factors"] + config["products"][data["product_id"]]["factors"]
    shares, knockouts, limiters = _factor_shares(data, config, amount, months)
    total = sum(config["weights"][code] for code in required)
    score = 100 * sum(config["weights"][code] * shares[code][0] for code in required) / total
    factors = [{"code": code, "weight": config["weights"][code], "r": shares[code][0],
                "points": round(config["weights"][code] * shares[code][0], 2),
                "delta": round(100 * config["weights"][code] * (shares[code][0] - 0.5) / total, 2),
                "value": shares[code][1]} for code in required]
    return {"score": round(score, 1), "factors": factors,
            "ko": knockouts, "limiters": limiters}


def _rate(score: float, base_rate: float, config: dict[str, Any]) -> float:
    base = Decimal(str(base_rate))
    rule = config["rate"]
    premium = Decimal(str(rule["slope_pp_per_point"])) * (
        Decimal(str(rule["anchor_score"])) - Decimal(str(score)))
    premium = max(Decimal(str(rule["min_pp"])), min(Decimal(str(rule["max_pp"])), premium))
    return float((base + premium).quantize(Decimal("0.01"), rounding=ROUND_HALF_UP))


def score_application(data: dict[str, Any], config: dict[str, Any] | None = None) -> dict[str, Any]:
    """Return an explainable decision or raise MissingData for incomplete inputs."""
    config = config or load_config()
    product = data.get("product_id")
    if product not in config["products"]:
        raise MissingData(["product_id"])
    if type(data.get("amount")) is not int or data["amount"] <= 0:
        raise MissingData(["amount"])
    if type(data.get("term_months")) is not int or data["term_months"] <= 0:
        raise MissingData(["term_months"])
    amount, months = data["amount"], data["term_months"]
    product_policy = config["products"][product]
    class_name = product_policy["class"]
    policy = config["classes"][class_name]
    calculated = _score(data, config, amount, months)
    score, ko, limiters = calculated["score"], calculated["ko"], calculated["limiters"]
    review_reasons = list(data.get("review_reasons", []))
    outcome = "MANUAL_REVIEW" if (ko or score < policy["counter"] or review_reasons) else (
        "MANUAL_REVIEW" if amount > policy["auto_limit"] and score >= policy["approve"] else (
            "APPROVED" if score >= policy["approve"] and not limiters else "COUNTER_OFFER"))
    result: dict[str, Any] = {
        "model_version": config["version"], "product_id": product, "class": class_name,
        "score": score, "outcome": outcome,
        "thresholds": {"approve": policy["approve"], "counter": policy["counter"]},
        "ko": ko, "limiters": limiters, "factors": calculated["factors"],
        "data_gaps": ["industry_risk"] if _industry(config, data) == INDUSTRY_UNKNOWN else [],
        "review_reasons": review_reasons,
        "rate": _rate(score, policy["base_rate"], config),
    }
    if outcome == "COUNTER_OFFER":
        offered_amount, offered_months = amount, months
        constraints = False
        if "P1" in product_policy["factors"]:
            turnover = data["turnover_monthly"]
            target_payment = turnover * config["counter"]["pti_target"]
            if _payment(offered_amount, offered_months, policy["base_rate"]) > target_payment:
                constraints = True
                for term in range(offered_months + 1, product_policy.get("max_term_months", offered_months) + 1):
                    offered_months = term
                    if _payment(offered_amount, term, policy["base_rate"]) <= target_payment:
                        break
                monthly = policy["base_rate"] / 1200
                cap = target_payment * (1 - (1 + monthly) ** -offered_months) / monthly
                offered_amount = min(offered_amount, int(cap))
        if "P3" in product_policy["factors"]:
            cap = int(config["counter"]["ltv_target"] * data["collateral_confirmed_value"]
                      * config["haircut"][data["collateral_type"]])
            if offered_amount > cap:
                constraints = True
                offered_amount = cap
        if not constraints:
            offered_amount = int(amount * score / policy["approve"])
        offered_amount = (offered_amount // config["counter"]["round_to"]) * config["counter"]["round_to"]
        if offered_amount < config["counter"]["min_offer"]:
            result["outcome"] = "MANUAL_REVIEW"
            result["review_reasons"].append("OFFER_BELOW_MINIMUM")
        else:
            rescored = _score(data, config, offered_amount, offered_months)
            result["offer"] = {"amount": offered_amount, "term_months": offered_months,
                               "rate": _rate(rescored["score"], policy["base_rate"], config),
                               "score": rescored["score"],
                               "reason": ",".join(limiters) if limiters else "SCORE"}
    negatives = sorted((f for f in calculated["factors"] if f["delta"] <= -1), key=lambda f: f["delta"])
    positives = sorted((f for f in calculated["factors"] if f["delta"] >= 1), key=lambda f: -f["delta"])
    result["reasons_negative"] = [{"factor": f["code"], "delta": f["delta"]} for f in negatives[:3]]
    result["reasons_positive"] = [{"factor": f["code"], "delta": f["delta"]} for f in positives[:3]]
    return result
