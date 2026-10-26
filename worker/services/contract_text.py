"""Explicit monetary terms from purchase contracts; no legal validation."""

from __future__ import annotations

import re
from decimal import Decimal
from typing import Any

from worker.services.valuation_text import MONEY


def _labelled_amount(text: str, label: str) -> str | None:
    values = []
    for line in text.splitlines():
        match = re.search(label, line, re.I)
        if not match:
            continue
        amount = MONEY.search(line[match.end():])
        if amount:
            value = Decimal(amount.group(1).replace(" ", "").replace("\u00a0", ""))
            if amount.group(2):
                value += Decimal(amount.group(2)) / 100
            if value <= 1_000_000_000_000_000:
                values.append(str(value.quantize(Decimal("0.01"))))
    return values[0] if values and len(set(values)) == 1 else None


def parse_purchase_contract(kind: str, text: str, document_id: str | None) -> dict[str, Any]:
    normalized = text.casefold().replace("ё", "е")
    if kind == "preliminary_purchase_contract":
        if not re.search(r"предварительн\w*\s+договор\w*\s+купли[ -]продажи", normalized):
            return {}
    elif kind == "business_purchase_contract":
        if "договор" not in normalized or not any(term in normalized for term in (
            "купли-продажи предприятия", "купли-продажи долей", "купли-продажи доли",
        )):
            return {}
    elif kind == "equipment_purchase_contract":
        if "договор" not in normalized or "купли-продажи оборудования" not in normalized:
            return {}
    else:
        return {}

    labels = {"contract_price": r"(?:цена|стоимость)\s+договора\s*[:—-]?"}
    if kind == "preliminary_purchase_contract":
        labels["down_payment"] = r"первоначальный\s+взнос\s*[:—-]?"
    fields = {}
    for name, label in labels.items():
        value = _labelled_amount(text, label)
        if value is not None:
            fields[name] = {"value": value, "source_document_id": document_id,
                            "period": None, "verification_status": "UNVERIFIED",
                            "currency": "RUB"}
    return fields
