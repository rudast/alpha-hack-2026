"""Extract unverified business metrics from structured document exports."""

from __future__ import annotations

import csv
import base64
import io
import json
from collections import defaultdict
from datetime import date
from decimal import Decimal, InvalidOperation
from typing import Any

from worker.services.document_readers import READABLE_TYPES, extract_text
from worker.services.bank_statement_1c import Unsupported1CFormat, parse_1c_statement


def _money(value: Any) -> int:
    try:
        amount = Decimal(str(value))
    except (InvalidOperation, ValueError) as exc:
        raise ValueError(f"invalid monetary value: {value}") from exc
    if not amount.is_finite() or amount != amount.to_integral_value():
        raise ValueError("money must be an integer number of rubles")
    return int(amount)


def _field(value: Any, document_id: str | None, period: str | None) -> dict[str, Any]:
    return {"value": value, "source_document_id": document_id,
            "period": period, "verification_status": "UNVERIFIED"}


def _json_fields(kind: str, raw: bytes, document_id: str | None) -> dict[str, Any]:
    data = json.loads(raw.decode("utf-8-sig"))
    if not isinstance(data, dict):
        raise ValueError("JSON document must be an object")
    period = data.get("period")
    if not isinstance(period, str) or not period:
        raise ValueError("period is required")
    fields: dict[str, Any] = {}
    schemas = {
        "financial_statements": ("revenue", "net_profit", "equity", "liabilities"),
        "target_financial_statements": ("revenue", "net_profit", "equity", "liabilities"),
        "marketplace_sales_report": ("sales", "returns", "payouts"),
        "valuation_report": ("appraised_value",),
    }
    for name in schemas[kind]:
        if name not in data:
            raise ValueError(f"{name} is required")
        fields[name] = _field(_money(data[name]), document_id, period)
    if kind == "valuation_report":
        for name in ("asset_id", "owner", "rights", "encumbrances"):
            if name not in data or data[name] in (None, ""):
                raise ValueError(f"{name} is required")
            fields[name] = _field(data[name], document_id, period)
    return fields


def _statement_fields(raw: bytes, document_id: str | None) -> dict[str, Any]:
    reader = csv.DictReader(io.StringIO(raw.decode("utf-8-sig")))
    if not reader.fieldnames or not {"date", "credit", "debit", "balance"}.issubset(reader.fieldnames):
        raise ValueError("CSV columns date, credit, debit, balance are required")
    monthly: dict[str, dict[str, int]] = defaultdict(lambda: {"credit": 0, "debit": 0})
    final_date: date | None = None
    final_balance = 0
    for row in reader:
        day = date.fromisoformat(row["date"])
        month = day.strftime("%Y-%m")
        monthly[month]["credit"] += _money(row["credit"])
        monthly[month]["debit"] += _money(row["debit"])
        if final_date is None or day >= final_date:
            final_date, final_balance = day, _money(row["balance"])
    if final_date is None:
        raise ValueError("statement has no transactions")
    periods = sorted(monthly)
    turnover = [{"period": period, **monthly[period]} for period in periods]
    return {
        "turnover_monthly": _field(turnover, document_id, f"{periods[0]}..{periods[-1]}"),
        "closing_balance": _field(final_balance, document_id, final_date.isoformat()),
    }


def parse_documents(payload: dict[str, Any]) -> dict[str, Any]:
    application_id = payload.get("application_id")
    documents = payload.get("documents")
    if not isinstance(application_id, str) or not application_id:
        raise ValueError("application_id is required")
    if not isinstance(documents, list):
        raise ValueError("documents must be an array")
    items = []
    for document in documents:
        if not isinstance(document, dict):
            raise ValueError("each document must be an object")
        kind, media_type = document.get("kind"), document.get("media_type")
        item: dict[str, Any] = {"id": document.get("id"), "kind": kind,
                                "filename": document.get("filename"),
                                "status": "UNSUPPORTED", "extracted_fields": {}}
        raw = document.get("content")
        if raw is None and isinstance(document.get("content_base64"), str):
            try:
                raw = base64.b64decode(document["content_base64"], validate=True)
            except (ValueError, base64.binascii.Error) as exc:
                raw = None
                item["error"] = f"invalid base64 content: {exc}"
        if raw is None and isinstance(document.get("content_text"), str):
            raw = document["content_text"].encode("utf-8")
        try:
            if not isinstance(raw, bytes):
                raise ValueError("document content is required")
            if kind == "account_statement" and media_type == "text/csv":
                item["extracted_fields"] = _statement_fields(raw, document.get("id"))
                item["status"] = "PARSED"
            elif kind == "account_statement" and media_type == "text/plain" and raw.removeprefix(b"\xef\xbb\xbf").startswith(b"1CClientBankExchange"):
                item["extracted_fields"] = parse_1c_statement(raw, document.get("id"))
                item["status"] = "PARSED"
            elif kind in ("financial_statements", "target_financial_statements",
                          "marketplace_sales_report", "valuation_report") and media_type == "application/json":
                item["extracted_fields"] = _json_fields(kind, raw, document.get("id"))
                item["status"] = "PARSED"
            elif media_type in READABLE_TYPES:
                extracted = extract_text(raw, media_type)
                item["extracted_fields"] = {
                    "document_text": {
                        **_field(extracted.text, document.get("id"), None),
                        "extraction_method": extracted.method,
                        "truncated": extracted.truncated,
                    }
                }
                item["status"] = "TEXT_EXTRACTED"
            else:
                item["error"] = "this document kind or media type is not parsed yet"
        except Unsupported1CFormat as exc:
            item["status"] = "UNSUPPORTED"
            item["error"] = str(exc)
        except (ValueError, TypeError, UnicodeError, csv.Error) as exc:
            item["status"] = "FAILED"
            item["error"] = str(exc)
        items.append(item)
    review_reasons = []
    if not items:
        review_reasons.append({"document_id": None, "reason": "NO_DOCUMENTS"})
    for item in items:
        if item["status"] == "PARSED":
            continue
        reason = {
            "FAILED": "DOCUMENT_INVALID",
            "UNSUPPORTED": "FORMAT_UNSUPPORTED",
        }.get(item["status"])
        if reason is None:
            reason = {
                "passport": "PASSPORT_NOT_VERIFIED",
                "bki_consent": "BKI_CONSENT_NOT_VERIFIED",
            }.get(item["kind"], "FIELDS_NOT_EXTRACTED")
        review_reasons.append({"document_id": item["id"], "reason": reason})
    return {"application_id": application_id,
            "status": "REVIEW_REQUIRED" if review_reasons else "DONE",
            "review_reasons": review_reasons, "documents": items}
