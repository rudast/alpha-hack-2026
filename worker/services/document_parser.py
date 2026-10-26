"""Extract unverified business metrics from structured document exports."""

from __future__ import annotations

import base64
import json
import mimetypes
import os
import stat
from decimal import Decimal, InvalidOperation
from pathlib import Path
from typing import Any

from worker.services.document_readers import READABLE_TYPES, extract_text
from worker.services.bank_statement_1c import Unsupported1CFormat, parse_1c_statement
from worker.services.account_statement_csv import UnsupportedStatementFormat, parse_csv_statement
from worker.services.marketplace_csv import parse_marketplace_csv
from worker.services.financial_report_text import parse_financial_text
from worker.services.registry_text import (parse_egrul_text, parse_egrn_text,
                                           parse_vehicle_text, parse_technical_passport)
from worker.services.valuation_text import parse_valuation_text
from worker.services.contract_text import parse_purchase_contract
from worker.services.demo_attestation import read_attestation

MAX_DOCUMENT_BYTES = 10 * 1024 * 1024
MAX_BASE64_CHARS = ((MAX_DOCUMENT_BYTES + 2) // 3) * 4


def _read_document_path(path: str) -> bytes:
    """Read a regular file below the configured document mount without following links."""
    root = os.environ.get("WORKER_DOCUMENT_ROOT")
    if not root:
        raise ValueError("WORKER_DOCUMENT_ROOT is not configured")
    root_path = Path(root).absolute()
    requested = Path(path)
    if requested.is_absolute():
        try:
            requested = requested.relative_to(root_path)
        except ValueError as exc:
            raise ValueError("document path is outside the document root") from exc
    parts = requested.parts
    if not parts or any(part in ("", ".", "..") for part in parts):
        raise ValueError("invalid document path")
    directory_flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
    file_flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK
    fd = None
    try:
        fd = os.open(root_path, directory_flags)
        for part in parts[:-1]:
            next_fd = os.open(part, directory_flags, dir_fd=fd)
            os.close(fd)
            fd = next_fd
        file_fd = os.open(parts[-1], file_flags, dir_fd=fd)
        try:
            metadata = os.fstat(file_fd)
            if not stat.S_ISREG(metadata.st_mode):
                raise ValueError("document path is not a regular file")
            if metadata.st_size > MAX_DOCUMENT_BYTES:
                raise ValueError("document exceeds 10 MiB limit")
            with os.fdopen(file_fd, "rb", closefd=False) as stream:
                raw = stream.read(MAX_DOCUMENT_BYTES + 1)
            if len(raw) > MAX_DOCUMENT_BYTES:
                raise ValueError("document exceeds 10 MiB limit")
            return raw
        finally:
            os.close(file_fd)
    except OSError as exc:
        raise ValueError("document path cannot be read") from exc
    finally:
        if fd is not None:
            os.close(fd)


def _money(value: Any) -> int:
    try:
        amount = Decimal(str(value))
    except (InvalidOperation, ValueError) as exc:
        raise ValueError(f"invalid monetary value: {value}") from exc
    if not amount.is_finite() or amount != amount.to_integral_value():
        raise ValueError("money must be an integer number of rubles")
    if abs(amount) > 1_000_000_000_000_000:
        raise ValueError("money exceeds supported range")
    return int(amount)


def _field(value: Any, document_id: str | None, period: str | None) -> dict[str, Any]:
    return {"value": value, "source_document_id": document_id,
            "period": period, "verification_status": "UNVERIFIED"}


def _json_fields(kind: str, raw: bytes, document_id: str | None) -> dict[str, Any]:
    data = json.loads(raw.decode("utf-8-sig"))
    if not isinstance(data, dict):
        raise ValueError("JSON document must be an object")
    fields: dict[str, Any] = {}
    schemas = {
        "financial_statements": ("revenue", "net_profit", "equity", "liabilities"),
        "target_financial_statements": ("revenue", "net_profit", "equity", "liabilities"),
        "marketplace_sales_report": ("sales", "returns", "payouts"),
        "valuation_report": ("appraised_value",),
    }
    if not any(name in data for name in schemas[kind]):
        raise UnsupportedStatementFormat("JSON document schema is not supported for this kind")
    period = data.get("period")
    if not isinstance(period, str) or not period:
        raise ValueError("period is required")
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
        if isinstance(media_type, str):
            media_type = media_type.split(";", 1)[0].strip().lower()
        elif isinstance(document.get("path"), str):
            media_type = mimetypes.guess_type(document["path"])[0]
        item: dict[str, Any] = {"id": document.get("id"), "kind": kind,
                                "filename": document.get("filename"),
                                "status": "UNSUPPORTED", "extracted_fields": {}}
        raw = document.get("content")
        try:
            if raw is None and isinstance(document.get("content_base64"), str):
                encoded = document["content_base64"]
                if len(encoded) > MAX_BASE64_CHARS:
                    raise ValueError("document exceeds 10 MiB limit")
                try:
                    raw = base64.b64decode(encoded, validate=True)
                except (ValueError, base64.binascii.Error) as exc:
                    raise ValueError("invalid base64 document content") from exc
            if raw is None and isinstance(document.get("content_text"), str):
                raw = document["content_text"].encode("utf-8")
            if raw is None and isinstance(document.get("path"), str):
                raw = _read_document_path(document["path"])
            if not isinstance(raw, bytes):
                raise ValueError("document content is required")
            if len(raw) > MAX_DOCUMENT_BYTES:
                raise ValueError("document exceeds 10 MiB limit")
            if media_type == "image/png" and kind in ("passport", "bki_consent"):
                item["_demo_attestation"] = read_attestation(raw)
            if kind == "account_statement" and media_type == "text/csv":
                item["extracted_fields"] = parse_csv_statement(raw, document.get("id"))
                item["status"] = "PARSED"
            elif kind == "marketplace_sales_report" and media_type == "text/csv":
                item["extracted_fields"] = parse_marketplace_csv(raw, document.get("id"))
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
                fields = {
                    "document_text": {
                        **_field(extracted.text, document.get("id"), None),
                        "extraction_method": extracted.method,
                        "truncated": extracted.truncated,
                    }
                }
                if not extracted.truncated:
                    if kind == "account_statement":
                        try:
                            fields.update(parse_csv_statement(extracted.text.encode("utf-8"), document.get("id")))
                        except UnsupportedStatementFormat:
                            pass  # Keep the text for manual review when the table layout is unknown.
                    elif kind == "marketplace_sales_report":
                        try:
                            fields.update(parse_marketplace_csv(extracted.text.encode("utf-8"), document.get("id")))
                        except UnsupportedStatementFormat:
                            pass
                    elif kind in ("financial_statements", "target_financial_statements"):
                        fields.update(parse_financial_text(extracted.text, document.get("id")))
                    elif kind == "egrul_extract":
                        fields.update(parse_egrul_text(extracted.text, document.get("id")))
                    elif kind == "egrn_extract":
                        fields.update(parse_egrn_text(extracted.text, document.get("id")))
                    elif kind == "technical_passport":
                        fields.update(parse_technical_passport(extracted.text, document.get("id")))
                    elif kind == "vehicle_registration":
                        fields.update(parse_vehicle_text(extracted.text, document.get("id")))
                    elif kind == "valuation_report":
                        fields.update(parse_valuation_text(extracted.text, document.get("id")))
                    elif kind in ("preliminary_purchase_contract", "business_purchase_contract",
                                  "equipment_purchase_contract"):
                        fields.update(parse_purchase_contract(kind, extracted.text, document.get("id")))
                item["extracted_fields"] = fields
                item["status"] = "TEXT_EXTRACTED"
            else:
                item["error"] = "this document kind or media type is not parsed yet"
        except (Unsupported1CFormat, UnsupportedStatementFormat) as exc:
            item["status"] = "UNSUPPORTED"
            item["error"] = str(exc)
        except (ValueError, TypeError, UnicodeError) as exc:
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
            }.get(item["kind"])
            if reason is None:
                reason = ("FIELDS_UNVERIFIED" if len(item["extracted_fields"]) > 1
                          else "FIELDS_NOT_EXTRACTED")
        review_reasons.append({"document_id": item["id"], "reason": reason})
    return {"application_id": application_id,
            "status": "REVIEW_REQUIRED" if review_reasons else "DONE",
            "review_reasons": review_reasons, "documents": items}
