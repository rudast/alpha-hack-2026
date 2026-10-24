"""Redis protocol used by the backend branch, without database access."""

from __future__ import annotations

import json
import logging
import os
import socket
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from typing import Any
from uuid import UUID

import redis

from worker.services.document_parser import parse_documents
from worker.services.financial_verifier import credit_history


HISTORY_STREAM = "credit-history:requests"
APPLICATION_STREAM = "applications:submitted"
RESULT_STREAM = "applications:results"
SLOW_STREAM = "worker:backend:slow"
FAST_GROUP = "worker-backend-fast"
SLOW_GROUP = "worker-backend-slow"
HISTORY_REPLY_PREFIX = "credit-history:reply:"
FAST_WORKERS = 8
SLOW_WORKERS = 2
HISTORY_REPLY_TTL = 60
FAST_IDLE_MS = 30_000
SLOW_IDLE_MS = 15 * 60_000
RESULT_GROUP = "api"
RESULT_ACK_TIMEOUT = 60

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


def inspect_application(fields: dict[str, str]) -> tuple[list[str], dict[str, Any]]:
    """Check only facts present in the backend snapshot; never invent a score."""
    missing: list[str] = []
    inn = fields.get("inn", "")
    if not inn.isdigit() or len(inn) not in (10, 12):
        missing.append("inn")
    if not fields.get("product_id"):
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
    for document in documents:
        if not isinstance(document, dict) or not isinstance(document.get("kind"), str):
            raise ValueError("documents must contain objects with kind")
        present.add(document["kind"])
        if any(key in document for key in ("content", "content_base64", "content_text")):
            usable_documents.append(document)
        else:
            unavailable_documents.append({
                "id": document.get("id"), "kind": document["kind"],
                "filename": document.get("filename"), "status": "CONTENT_UNAVAILABLE",
                "extracted_fields": {},
            })

    # The backend currently sends metadata only. Its catalogue requires these
    # documents for every product; a name alone does not verify consent.
    for kind in ("passport", "bki_consent"):
        if kind not in present:
            missing.append("documents." + kind)

    parsed = parse_documents({"application_id": fields["application_id"], "documents": usable_documents}) if usable_documents else {
        "documents": [], "review_reasons": [],
    }
    parsed_documents = parsed["documents"] + unavailable_documents
    review_reasons = parsed["review_reasons"] + [
        {"document_id": item["id"], "reason": "CONTENT_UNAVAILABLE"}
        for item in unavailable_documents
    ]
    for item in parsed_documents:
        if item["kind"] in ("passport", "bki_consent"):
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


def result_event(application_id: str, from_status: str, to_status: str,
                 comment: str, decision: dict[str, Any] | None = None,
                 info_request: dict[str, Any] | None = None,
                 source_id: str = "") -> dict[str, str]:
    """Flatten fields; event_id is stable on retry of the same Redis request."""
    event = {
        "event_id": f"{application_id}:{source_id}:{to_status}",
        "application_id": application_id,
        "from": from_status,
        "to": to_status,
        "comment": comment,
    }
    if decision is not None:
        event["decision"] = json.dumps(decision, ensure_ascii=False)
    if info_request is not None:
        event["info_request"] = json.dumps(info_request, ensure_ascii=False)
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
        return [result_event(
            application_id, "DATA_CHECK", "INFO_REQUESTED",
            "Нужны данные: " + ", ".join(missing), decision,
            {"missing_fields": missing}, source_id,
        )]
    return [
        result_event(application_id, "DATA_CHECK", "SCORING", "Предварительная проверка завершена",
                     source_id=source_id),
        result_event(application_id, "SCORING", "MANUAL_REVIEW",
                     "Требуется проверка документов и решение сотрудника", decision,
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
        first_result_id = self.redis.xadd(RESULT_STREAM, result_event(
            application_id, "SUBMITTED", "DATA_CHECK", "Заявка принята воркером",
            source_id=message_id))
        pipe = self.redis.pipeline(transaction=True)
        pipe.xadd(SLOW_STREAM, {
            "task": json.dumps({**fields, "__source_message_id": message_id}, ensure_ascii=False),
            "after_result_id": first_result_id,
            "application_id": application_id,
        })
        pipe.xack(APPLICATION_STREAM, FAST_GROUP, message_id)
        pipe.execute()

    def _slow(self, message_id: str, fields: dict[str, str]) -> None:
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
                    {"missing_fields": ["application_data"]},
                    source_id=message_id,
                ))
            self.redis.xack(SLOW_STREAM, SLOW_GROUP, message_id)
            return
        previous_id = fields.get("after_result_id")
        for event in events:
            if previous_id:
                self._wait_for_result_ack(previous_id)
            previous_id = self.redis.xadd(RESULT_STREAM, event)
        self.redis.xack(SLOW_STREAM, SLOW_GROUP, message_id)

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
