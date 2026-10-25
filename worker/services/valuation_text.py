"""Conservative value hint from a dated Russian valuation report."""

from __future__ import annotations

import re
from datetime import datetime
from decimal import Decimal
from typing import Any


MONEY = re.compile(r"\b(\d{1,3}(?:[ \u00a0]\d{3})+|\d+)(?:[,.](\d{2}))?\s*(?:руб(?:лей|ля|\.)?|₽)(?=\s|[;,)]|$)", re.I)
DATE = re.compile(r"\b(\d{2}\.\d{2}\.20\d{2})\b")


def _dated_value(text: str, label: str) -> str | None:
    values = []
    for match in re.finditer(label, text, re.I):
        fragment = text[match.end():match.end() + 100].split("\n", 2)
        found = MONEY.search(fragment[0])
        if not found and len(fragment) > 1:
            found = MONEY.fullmatch(fragment[1].strip())
        if found:
            value = Decimal(found.group(1).replace(" ", "").replace("\u00a0", ""))
            if found.group(2):
                value += Decimal(found.group(2)) / 100
            if value <= 1_000_000_000_000_000:
                values.append(str(value.quantize(Decimal("0.01"))))
    return values[0] if values and len(set(values)) == 1 else None


def parse_valuation_text(text: str, document_id: str | None) -> dict[str, Any]:
    normalized = text.casefold().replace("ё", "е")
    if "отчет об оценке" not in normalized and "отчет о рыночной стоимости" not in normalized:
        return {}
    dates = []
    for match in re.finditer(r"дата\s+оценки\s*[:—-]?\s*([^\n]{1,40})", text, re.I):
        day = DATE.search(match.group(1))
        if day:
            try:
                dates.append(datetime.strptime(day.group(1), "%d.%m.%Y").date().isoformat())
            except ValueError:
                continue
    value = _dated_value(text, r"рыночная\s+стоимость(?:\s+объекта\s+оценки)?\s*[:—-]?")
    if value is None or len(set(dates)) != 1:
        return {}
    return {"appraised_value": {"value": value, "source_document_id": document_id,
                                "period": dates[0], "verification_status": "UNVERIFIED",
                                "currency": "RUB"}}
