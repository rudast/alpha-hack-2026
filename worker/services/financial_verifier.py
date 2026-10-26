"""Fast synthetic credit history and application checks."""

from __future__ import annotations

from typing import Any


KNOWN_HISTORY = {
    "7700000001": {"debt": 3_000_000, "equity": 5_000_000},
    "7700000002": {"debt": 2_400_000, "equity": 4_000_000},
    "7700000003": {"debt": 1_800_000, "equity": 3_000_000},
    "7700000004": {"debt": 7_000_000, "equity": 2_000_000},
    "770000000005": {"debt": 800_000, "equity": 2_000_000},
}


def _fnv1a32(value: str) -> int:
    result = 2166136261
    for byte in value.encode("utf-8"):
        result ^= byte
        result = (result * 16777619) & 0xFFFFFFFF
    return result


def credit_history(payload: dict[str, Any]) -> dict[str, int]:
    inn = payload.get("inn")
    if not isinstance(inn, str) or not inn.isdigit() or len(inn) not in (10, 12):
        raise ValueError("inn must contain 10 or 12 digits")
    if inn in KNOWN_HISTORY:
        return KNOWN_HISTORY[inn].copy()
    value = _fnv1a32(inn)
    return {
        "debt": 1_000_000 + value % 5_000_000,
        "equity": 2_000_000 + (value // 7) % 6_000_000,
    }


def verify_application(payload: dict[str, Any]) -> dict[str, Any]:
    """Return preliminary checks; the bank decision is out of scope."""
    application = payload.get("application")
    company = payload.get("company", {})
    if not isinstance(application, dict) or not isinstance(company, dict):
        raise ValueError("application and company must be objects")

    required = ("inn", "org_type", "product_id", "amount", "term_months", "purpose")
    missing = [field for field in required if application.get(field) in (None, "")]
    if application.get("bki_consent") is not True:
        missing.append("bki_consent")
    if application.get("org_type") not in (None, "", "IP", "OOO"):
        missing.append("org_type")
    for field in ("amount", "term_months"):
        value = application.get(field)
        if value is not None and (isinstance(value, bool) or not isinstance(value, (int, float)) or value <= 0):
            missing.append(field)

    debt = company.get("total_debt")
    equity = company.get("equity")
    ratio = None
    flags: list[str] = []
    if isinstance(debt, (int, float)) and not isinstance(debt, bool) and isinstance(equity, (int, float)) and not isinstance(equity, bool):
        if equity <= 0:
            flags.append("EQUITY_NON_POSITIVE")
        else:
            ratio = round(debt / equity, 4)
            if ratio > 2:
                flags.append("HIGH_DEBT_TO_EQUITY")
    else:
        flags.append("FINANCIALS_UNAVAILABLE")

    missing = list(dict.fromkeys(missing))
    return {
        "complete": not missing,
        "missing_fields": missing,
        "debt_to_equity": ratio,
        "risk_flags": flags,
        "manual_review_required": bool(flags),
        "next_status": "INFO_REQUESTED" if missing else "SCORING",
    }
