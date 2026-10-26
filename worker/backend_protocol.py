"""Redis protocol used by the backend branch, without database access."""

from __future__ import annotations

import json
import logging
import os
import socket
import threading
import time
from collections import Counter
from concurrent.futures import ThreadPoolExecutor
from datetime import date
from typing import Any
from uuid import NAMESPACE_URL, UUID, uuid5

import redis

from worker.services.document_parser import parse_documents
from worker.services.financial_verifier import credit_history
from worker.services.demo_attestation import verify_attestation
from worker.services.scoring_sources import BUREAU_DEMO, bureau_profile, registry_profile
from worker.scoring_engine import MissingData, load_config, score_application


HISTORY_STREAM = "credit-history:requests"
APPLICATION_STREAM = "applications:submitted"
RESULT_STREAM = "applications:results"
SLOW_STREAM = "worker:backend:slow"
ROUTED_HASH = "worker:backend:routed"
FAST_GROUP = "worker-backend-fast"
SLOW_GROUP = "worker-backend-slow"
HISTORY_REPLY_PREFIX = "credit-history:reply:"
FAST_WORKERS = 8
SLOW_WORKERS = 2
HISTORY_REPLY_TTL = 60
FAST_IDLE_MS = 30_000
SLOW_IDLE_MS = 60_000
SLOW_HEARTBEAT_SECONDS = 20
RESULT_GROUP = "api"
RESULT_ACK_TIMEOUT = 60

DOCUMENT_LABELS = {
    "passport": "паспорт заявителя", "bki_consent": "согласие на запрос в БКИ",
    "account_statement": "выписку по расчётному счёту",
    "financial_statements": "бухгалтерскую отчётность или декларацию УСН",
    "marketplace_sales_report": "отчёт о продажах маркетплейса",
    "title_documents": "правоустанавливающие документы на залог",
    "equipment_purchase_contract": "договор или счёт на оборудование",
    "equipment_specification": "спецификацию оборудования",
    "vehicle_registration": "ПТС/СТС или договор покупки автомобиля",
    "egrn_extract": "выписку из ЕГРН", "technical_passport": "технический паспорт",
    "valuation_report": "отчёт об оценке залога", "business_plan": "бизнес-план",
    "target_financial_statements": "отчётность приобретаемого бизнеса",
    "business_purchase_contract": "договор покупки бизнеса",
    "preliminary_purchase_contract": "предварительный договор покупки",
}
DATA_LABELS = {
    "age_months": "дату регистрации бизнеса",
    "turnover_monthly": "подтверждённый месячный оборот",
    "monthly_turnovers": "помесячные обороты минимум за 6 месяцев",
    "collateral_confirmed_value": "подтверждённую оценку залога",
    "collateral_type": "тип залога",
    "credit_history": "данные кредитной истории",
    "registry": "сведения из реестра",
    "debt": "сумму долга", "equity": "собственный капитал",
    "okved": "код ОКВЭД",
}

log = logging.getLogger(__name__)


def _json_field(fields: dict[str, str], name: str, expected: type, default: Any) -> Any:
    raw = fields.get(name)
    if raw is None:
        return default
    value = json.loads(raw)
    if not isinstance(value, expected):
        raise ValueError(f"{name} must be {expected.__name__}")
    return value


def _positive_int(fields: dict[str, str], name: str) -> int | None:
    try:
        value = int(fields[name])
    except (KeyError, TypeError, ValueError):
        return None
    return value if value > 0 else None


def _missing_message(missing: list[str]) -> str:
    documents = [DOCUMENT_LABELS.get(item.removeprefix("documents."), item)
                 for item in missing if item.startswith("documents.")]
    data = [DATA_LABELS.get(item, item) for item in missing
            if not item.startswith("documents.")]
    parts = []
    if documents:
        parts.append("Загрузите документы: " + ", ".join(documents) + ".")
    if data:
        parts.append("Уточните данные заявки: " + ", ".join(data) + ".")
    return " ".join(parts) if parts else "Требуется дополнить заявку."


def inspect_application(fields: dict[str, str]) -> tuple[list[str], dict[str, Any]]:
    """Check only facts present in the backend snapshot; never invent a score."""
    missing: list[str] = []
    inn = fields.get("inn", "")
    if not inn.isdigit() or len(inn) not in (10, 12):
        missing.append("inn")
    product_id = fields.get("product_id")
    products = load_config()["products"]
    if product_id not in products:
        missing.append("product_id")
    if _positive_int(fields, "amount") is None:
        missing.append("amount")
    if _positive_int(fields, "term_months") is None:
        missing.append("term_months")

    company = _json_field(fields, "company", dict, {})
    form = _json_field(fields, "form", dict, {})
    documents = _json_field(fields, "documents", list, [])
    if company.get("org_type") not in ("IP", "OOO"):
        missing.append("company.org_type")

    present: set[str] = set()
    usable_documents: list[dict[str, Any]] = []
    unavailable_documents: list[dict[str, Any]] = []
    for index, document in enumerate(documents):
        if not isinstance(document, dict) or not isinstance(document.get("kind"), str):
            raise ValueError("documents must contain objects with kind")
        document = document.copy()
        document.setdefault("filename", document.get("name"))
        if not document.get("id"):
            identity = document.get("path") or f"{document['kind']}:{document.get('filename')}:{index}"
            document["id"] = str(uuid5(NAMESPACE_URL, f"application-document:{fields['application_id']}:{identity}"))
        present.add(document["kind"])
        if any(key in document for key in ("content", "content_base64", "content_text", "path")):
            usable_documents.append(document)
        else:
            unavailable_documents.append({
                "id": document.get("id"), "kind": document["kind"],
                "filename": document.get("filename"), "status": "CONTENT_UNAVAILABLE",
                "extracted_fields": {},
            })

    # These requirements mirror the backend product catalogue. A document
    # name alone does not verify its contents or the subject of BKI consent.
    required = load_config()["base_documents"] + (
        products[product_id]["documents"] if product_id in products else [])
    for kind in required:
        if kind not in present:
            missing.append("documents." + kind)

    parsed = parse_documents({"application_id": fields["application_id"], "documents": usable_documents}) if usable_documents else {
        "documents": [], "review_reasons": [],
    }
    parsed_documents = parsed["documents"] + unavailable_documents
    id_counts = Counter(doc.get("id") for doc in documents if isinstance(doc.get("id"), str))
    verified = {(doc["id"], doc["kind"]) for doc in documents
                if isinstance(doc.get("id"), str) and doc["id"] and id_counts[doc["id"]] == 1
                and doc.get("verification_status") == "VERIFIED"
                and (doc.get("kind") != "bki_consent" or doc.get("subject_inn") == inn)}
    registry = company.get("registry")
    director = registry.get("director") if isinstance(registry, dict) else None
    consents = form.get("consents")
    consent_given = isinstance(consents, dict) and consents.get("bki_request") is True
    for item in parsed_documents:
        attestation = item.pop("_demo_attestation", None)
        if (inn in BUREAU_DEMO and item["status"] in ("PARSED", "TEXT_EXTRACTED")
                and verify_attestation(
                    attestation, key=os.environ.get("WORKER_DEMO_ATTESTATION_KEY", ""),
                    kind=item["kind"], application_id=fields["application_id"],
                    inn=inn, subject_name=director, consent_given=consent_given)):
            verified.add((item["id"], item["kind"]))
            item["verification_method"] = "signed_demo_fixture"
        if (item["id"], item["kind"]) in verified:
            item["verification_status"] = "VERIFIED"
        if item["kind"] in ("passport", "bki_consent"):
            # The API stores decision JSON; avoid copying passport/consent OCR
            # text into the application record when only verification matters.
            item["extracted_fields"].pop("document_text", None)
    review_reasons = [reason for reason in parsed["review_reasons"]
                      if not any(doc_id == reason.get("document_id") for doc_id, _ in verified)] + [
        {"document_id": item["id"], "reason": "CONTENT_UNAVAILABLE"}
        for item in unavailable_documents if (item["id"], item["kind"]) not in verified
    ]
    for item in parsed_documents:
        if item["kind"] in ("passport", "bki_consent") and (item["id"], item["kind"]) not in verified:
            review_reasons.append({
                "document_id": item["id"],
                "reason": "PASSPORT_NOT_VERIFIED" if item["kind"] == "passport" else "BKI_CONSENT_NOT_VERIFIED",
            })

    ratio: float | None = None
    flags: list[str] = []
    try:
        debt = int(fields["debt"])
        equity = int(fields["equity"])
    except (KeyError, TypeError, ValueError):
        flags.append("FINANCIALS_UNAVAILABLE")
    else:
        if equity <= 0:
            flags.append("EQUITY_NON_POSITIVE")
        elif debt < 0:
            flags.append("DEBT_INVALID")
        else:
            ratio = round(debt / equity, 4)
            if ratio > 2:
                flags.append("DEMO_HIGH_DEBT_TO_EQUITY")

    return missing, {
        "outcome": "PRELIMINARY_REVIEW",
        "debt_to_equity": ratio,
        "risk_flags": flags,
        "documents": parsed_documents,
        "review_reasons": review_reasons,
        "form_received": bool(form),
        "reason": "Full scoring, identity and consent verification require review",
    }


def _document_fields(documents: list[dict[str, Any]], kind: str) -> tuple[dict[str, Any], bool]:
    for document in documents:
        if document.get("kind") == kind and document.get("status") in ("PARSED", "TEXT_EXTRACTED"):
            return document.get("extracted_fields", {}), document.get("verification_status") == "VERIFIED"
    return {}, False


def _snapshot(fields: dict[str, str], preliminary: dict[str, Any]) -> dict[str, Any]:
    company = _json_field(fields, "company", dict, {})
    form = _json_field(fields, "form", dict, {})
    documents = preliminary["documents"]
    product = fields["product_id"]
    registry = registry_profile(fields["inn"], company.get("registry"))
    history = bureau_profile(fields["inn"], company.get("credit_history"))
    snapshot: dict[str, Any] = {
        "product_id": product,
        "amount": int(fields["amount"]), "term_months": int(fields["term_months"]),
        "registry": registry, "credit_history": history, "okved": company.get("okved"),
        "review_reasons": [reason["reason"] for reason in preliminary["review_reasons"]],
    }
    if "debt" in fields:
        snapshot["debt"] = int(fields["debt"])
    if "equity" in fields:
        snapshot["equity"] = int(fields["equity"])
    if company.get("age_months") is not None:
        snapshot["age_months"] = company["age_months"]
    elif company.get("registration_date"):
        registered = date.fromisoformat(company["registration_date"])
        today = date.today()
        snapshot["age_months"] = (today.year - registered.year) * 12 + today.month - registered.month - (today.day < registered.day)
    collateral = form.get("collateral", {})
    if isinstance(collateral, dict):
        snapshot["collateral_type"] = collateral.get("type")
        # Customer-declared value is useful for manual review, but is not a
        # confirmed valuation for an automatic decision.
        snapshot["declared_collateral_value"] = collateral.get("value")
    mortgage = form.get("mortgage", {})
    if isinstance(mortgage, dict):
        snapshot["down_payment"] = mortgage.get("down_payment")
        snapshot["object_price"] = mortgage.get("object_price")
    rko = form.get("rko", {})
    if isinstance(rko, dict):
        snapshot["has_settlement_account"] = rko.get("has_settlement_account")
    marketplace = form.get("marketplace", {})
    if isinstance(marketplace, dict):
        snapshot["marketplace_revenue_share"] = marketplace.get("revenue_share")

    account, account_verified = _document_fields(documents, "account_statement")
    financial, financial_verified = _document_fields(documents, "financial_statements")
    sales, sales_verified = _document_fields(documents, "marketplace_sales_report")
    valuation, valuation_verified = _document_fields(documents, "valuation_report")
    target, target_verified = _document_fields(documents, "target_financial_statements")
    if product in ("overdraft", "credit_line", "loan_up_to_1m") and account.get("turnover_monthly"):
        monthly = account["turnover_monthly"]["value"]
        values = [float(item["credit"]) for item in monthly]
        min_months = 1 if product == "loan_up_to_1m" else 6
        if len(values) >= min_months:
            snapshot["monthly_turnovers"] = values
            snapshot["turnover_monthly"] = sum(values) / len(values)
        if not account_verified:
            snapshot["review_reasons"].append("ACCOUNT_STATEMENT_UNVERIFIED")
    if product == "working_capital" and financial.get("revenue"):
        snapshot["turnover_monthly"] = float(financial["revenue"]["value"]) / 12
        if not financial_verified:
            snapshot["review_reasons"].append("FINANCIAL_STATEMENTS_UNVERIFIED")
    if product == "marketplace_seller" and sales.get("monthly"):
        monthly = sales["monthly"]["value"]
        values = [float(item["sales"]) - float(item["returns"]) for item in monthly]
        if values:
            snapshot["turnover_monthly"] = sum(values) / len(values)
        if not sales_verified:
            snapshot["review_reasons"].append("MARKETPLACE_REPORT_UNVERIFIED")
    if valuation.get("appraised_value"):
        snapshot["collateral_confirmed_value"] = float(valuation["appraised_value"]["value"])
        if not valuation_verified:
            snapshot["review_reasons"].append("VALUATION_UNVERIFIED")
    if product == "business_purchase" and target.get("revenue") and target.get("net_profit"):
        snapshot["target_turnover"] = float(target["revenue"]["value"])
        snapshot["target_profit"] = float(target["net_profit"]["value"])
        if not target_verified:
            snapshot["review_reasons"].append("TARGET_FINANCIALS_UNVERIFIED")
    financials = company.get("financials")
    if isinstance(financials, dict) and financials.get("verification_status") == "VERIFIED":
        if "turnover_monthly" not in snapshot:
            turnover = financials.get("turnover_monthly")
            if isinstance(turnover, (int, float)) and not isinstance(turnover, bool):
                snapshot["turnover_monthly"] = turnover
        if "monthly_turnovers" not in snapshot and isinstance(financials.get("monthly_turnovers"), list):
            snapshot["monthly_turnovers"] = financials["monthly_turnovers"]
    return snapshot


def result_event(application_id: str, from_status: str, to_status: str,
                 comment: str, decision: dict[str, Any] | None = None,
                 info_request: str | None = None,
                 source_id: str = "") -> dict[str, str]:
    """Flatten fields; a retried Redis request produces the same safe event."""
    def without_nul(value: Any) -> Any:
        if isinstance(value, str):
            return value.replace("\x00", "")
        if isinstance(value, dict):
            return {without_nul(key): without_nul(item) for key, item in value.items()}
        if isinstance(value, list):
            return [without_nul(item) for item in value]
        return value

    event = {
        "event_id": str(uuid5(NAMESPACE_URL, f"application-result:{application_id}:{source_id}:{from_status}:{to_status}")),
        "application_id": application_id,
        "from": from_status,
        "to": to_status,
        "comment": without_nul(comment),
    }
    if decision is not None:
        event["decision"] = json.dumps(without_nul(decision), ensure_ascii=False)
    if info_request is not None:
        event["info_request"] = without_nul(info_request)
    return event


def completion_events(fields: dict[str, str]) -> list[dict[str, str]]:
    application_id = fields["application_id"]
    source_id = fields.get("__source_message_id", "")
    try:
        missing, decision = inspect_application(fields)
    except (KeyError, TypeError, ValueError, json.JSONDecodeError) as exc:
        missing = ["application_data"]
        decision = {"outcome": "INFO_REQUESTED", "error": str(exc)}

    if missing:
        decision["outcome"] = "INFO_REQUESTED"
        decision["missing_fields"] = missing
        message = _missing_message(missing)
        return [result_event(
            application_id, "DATA_CHECK", "INFO_REQUESTED",
            message, decision, message, source_id,
        )]
    try:
        score_input = _snapshot(fields, decision)
        scored = score_application(score_input)
    except MissingData as exc:
        missing = exc.fields
        decision["outcome"] = "INFO_REQUESTED"
        decision["missing_fields"] = missing
        message = _missing_message(missing)
        return [result_event(
            application_id, "DATA_CHECK", "INFO_REQUESTED",
            message, decision, message, source_id,
        )]
    except (ValueError, TypeError, KeyError) as exc:
        decision["outcome"] = "MANUAL_REVIEW"
        decision["review_reasons"].append({"document_id": None, "reason": "SCORING_INPUT_INVALID"})
        decision["error"] = str(exc)
        return [
            result_event(application_id, "DATA_CHECK", "SCORING", "Данные получены; требуется проверка",
                         source_id=source_id),
            result_event(application_id, "SCORING", "MANUAL_REVIEW", "Проверка входных данных скоринга",
                         decision, source_id=source_id),
        ]
    scored["documents"] = decision["documents"]
    if fields.get("inn") in BUREAU_DEMO:
        scored["evidence_mode"] = "synthetic_demo"
    comment = ("Демонстрационное решение скоринга: " if scored.get("evidence_mode") == "synthetic_demo"
               else "Решение скоринга: ") + scored["outcome"]
    return [
        result_event(application_id, "DATA_CHECK", "SCORING", "Предварительная проверка завершена",
                     source_id=source_id),
        result_event(application_id, "SCORING", scored["outcome"],
                     comment, scored,
                     source_id=source_id),
    ]


class BackendWorker:
    """Eight fast consumers and two slow consumers for the backend Redis contract."""

    def __init__(self, client: redis.Redis[Any]) -> None:
        self.redis = client
        self.stop = threading.Event()
        self.consumer = f"{socket.gethostname()}-{os.getpid()}"

    def _create_groups(self) -> None:
        for stream, group in ((HISTORY_STREAM, FAST_GROUP), (APPLICATION_STREAM, FAST_GROUP),
                              (SLOW_STREAM, SLOW_GROUP)):
            try:
                self.redis.xgroup_create(stream, group, id="0", mkstream=True)
            except redis.exceptions.ResponseError as exc:
                if not str(exc).startswith("BUSYGROUP"):
                    raise

    def _history(self, message_id: str, fields: dict[str, str]) -> None:
        request_id = fields.get("request_id", "")
        try:
            answer = credit_history({"inn": fields.get("inn")})
        except (TypeError, ValueError) as exc:
            answer = {"error": str(exc)}
        pipe = self.redis.pipeline(transaction=True)
        if request_id:
            key = HISTORY_REPLY_PREFIX + request_id
            pipe.lpush(key, json.dumps(answer))
            pipe.expire(key, HISTORY_REPLY_TTL)
        else:
            log.warning("credit history request %s has no request_id", message_id)
        pipe.xack(HISTORY_STREAM, FAST_GROUP, message_id)
        pipe.execute()

    def _application(self, message_id: str, fields: dict[str, str]) -> None:
        application_id = fields.get("application_id", "")
        try:
            UUID(application_id)
        except (TypeError, ValueError, AttributeError):
            log.warning("application request %s has invalid application_id", message_id)
            self.redis.xack(APPLICATION_STREAM, FAST_GROUP, message_id)
            return
        # The original request stays pending until the slow consumer has
        # published its final result. Reclaiming it must not enqueue a second
        # slow task while the first one is still durable in SLOW_STREAM.
        if self.redis.hexists(ROUTED_HASH, message_id):
            return
        first_result_id = self.redis.xadd(RESULT_STREAM, result_event(
            application_id, "SUBMITTED", "DATA_CHECK", "Заявка принята воркером",
            source_id=message_id))
        pipe = self.redis.pipeline(transaction=True)
        pipe.xadd(SLOW_STREAM, {
            "task": json.dumps({**fields, "__source_message_id": message_id}, ensure_ascii=False),
            "after_result_id": first_result_id,
            "application_id": application_id,
            "original_message_id": message_id,
        })
        pipe.hset(ROUTED_HASH, message_id, "1")
        pipe.execute()

    def _ack_application_task(self, slow_id: str, original_id: str) -> None:
        pipe = self.redis.pipeline(transaction=True)
        if original_id:
            pipe.xack(APPLICATION_STREAM, FAST_GROUP, original_id)
            pipe.hdel(ROUTED_HASH, original_id)
        pipe.xack(SLOW_STREAM, SLOW_GROUP, slow_id)
        pipe.execute()

    def _slow(self, message_id: str, fields: dict[str, str]) -> None:
        heartbeat_stop = threading.Event()

        def keep_pending_alive() -> None:
            while not heartbeat_stop.wait(SLOW_HEARTBEAT_SECONDS):
                try:
                    self.redis.xclaim(SLOW_STREAM, SLOW_GROUP, self.consumer,
                                      min_idle_time=0, message_ids=[message_id], justid=True)
                except Exception:
                    log.exception("could not renew slow task %s; it may be reclaimed", message_id)

        heartbeat = threading.Thread(target=keep_pending_alive, daemon=True)
        heartbeat.start()
        try:
            self._slow_task(message_id, fields)
        finally:
            heartbeat_stop.set()
            heartbeat.join(timeout=1)

    def _slow_task(self, message_id: str, fields: dict[str, str]) -> None:
        original_id = fields.get("original_message_id", "")
        try:
            task = json.loads(fields["task"])
            if not isinstance(task, dict):
                raise ValueError("task must be an object")
            events = completion_events(task)
        except (KeyError, TypeError, ValueError, json.JSONDecodeError) as exc:
            log.error("invalid slow task %s: %s", message_id, exc)
            application_id = fields.get("application_id", "")
            if application_id:
                previous_id = fields.get("after_result_id")
                if previous_id:
                    self._wait_for_result_ack(previous_id)
                self.redis.xadd(RESULT_STREAM, result_event(
                    application_id, "DATA_CHECK", "INFO_REQUESTED",
                    "Ошибка данных задания; требуется повторная проверка",
                    {"outcome": "INFO_REQUESTED", "error": "invalid worker task"},
                    "Ошибка данных задания; требуется повторная проверка",
                    source_id=message_id,
                ))
            self._ack_application_task(message_id, original_id)
            return
        previous_id = fields.get("after_result_id")
        for event in events:
            if previous_id:
                self._wait_for_result_ack(previous_id)
            previous_id = self.redis.xadd(RESULT_STREAM, event)
        self._ack_application_task(message_id, original_id)

    @staticmethod
    def _stream_id(value: str) -> tuple[int, int]:
        milliseconds, sequence = value.split("-", 1)
        return int(milliseconds), int(sequence)

    def _result_acknowledged(self, message_id: str) -> bool:
        groups = self.redis.xinfo_groups(RESULT_STREAM)
        group = next((item for item in groups if item["name"] == RESULT_GROUP), None)
        if group is None or self._stream_id(group["last-delivered-id"]) < self._stream_id(message_id):
            return False
        return not self.redis.xpending_range(RESULT_STREAM, RESULT_GROUP, message_id, message_id, 1)

    def _wait_for_result_ack(self, message_id: str) -> None:
        deadline = time.monotonic() + RESULT_ACK_TIMEOUT
        while not self.stop.is_set() and time.monotonic() < deadline:
            if self._result_acknowledged(message_id):
                return
            self.stop.wait(0.2)
        raise TimeoutError(f"API did not acknowledge result {message_id}")

    def _reclaim(self, stream: str, group: str, consumer: str, idle_ms: int,
                 handle: Any) -> None:
        cursor = "0-0"
        while True:
            cursor, messages, _ = self.redis.xautoclaim(
                stream, group, consumer, min_idle_time=idle_ms, start_id=cursor, count=10)
            for message_id, fields in messages:
                try:
                    handle(message_id, fields)
                except Exception:
                    log.exception("reclaimed task %s failed and remains pending", message_id)
            if cursor in ("", "0-0"):
                return

    def _fast_loop(self, consumer: str) -> None:
        handlers = {HISTORY_STREAM: self._history, APPLICATION_STREAM: self._application}
        while not self.stop.is_set():
            batches = self.redis.xreadgroup(
                FAST_GROUP, consumer, {HISTORY_STREAM: ">", APPLICATION_STREAM: ">"},
                count=10, block=1000)
            for stream, messages in batches:
                for message_id, fields in messages:
                    try:
                        handlers[stream](message_id, fields)
                    except Exception:
                        log.exception("fast task %s failed unexpectedly and remains pending", message_id)
            if not batches:
                for stream, handle in handlers.items():
                    self._reclaim(stream, FAST_GROUP, consumer, FAST_IDLE_MS, handle)

    def _slow_loop(self, consumer: str) -> None:
        while not self.stop.is_set():
            batches = self.redis.xreadgroup(
                SLOW_GROUP, consumer, {SLOW_STREAM: ">"}, count=1, block=1000)
            for _, messages in batches:
                for message_id, fields in messages:
                    try:
                        self._slow(message_id, fields)
                    except Exception:
                        log.exception("slow task %s failed unexpectedly and remains pending", message_id)
            if not batches:
                self._reclaim(SLOW_STREAM, SLOW_GROUP, consumer, SLOW_IDLE_MS, self._slow)

    def run(self) -> None:
        self.redis.ping()
        self._create_groups()
        log.info("Backend protocol ready: %d fast, %d slow consumers", FAST_WORKERS, SLOW_WORKERS)
        with ThreadPoolExecutor(max_workers=FAST_WORKERS + SLOW_WORKERS) as pool:
            futures = [pool.submit(self._fast_loop, f"{self.consumer}-fast-{i}")
                       for i in range(FAST_WORKERS)]
            futures += [pool.submit(self._slow_loop, f"{self.consumer}-slow-{i}")
                        for i in range(SLOW_WORKERS)]
            while not self.stop.wait(0.2):
                for future in futures:
                    if future.done():
                        future.result()
                        raise RuntimeError("backend worker loop stopped unexpectedly")
