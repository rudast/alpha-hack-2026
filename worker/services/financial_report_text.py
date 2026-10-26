"""Extract labelled Russian accounting form rows from PDF/OCR text.

Only the current-period column is read. Missing units, ambiguous rows and
incomplete liabilities are deliberately left for manual review.
"""

from __future__ import annotations

import re
from typing import Any


ROWS = {
    "revenue": ("2110", "выручка"),
    "net_profit": ("2400", "чистая прибыль"),
    "equity": ("1300", "капитал и резервы"),
    "long_term_liabilities": ("1400", "долгосрочные обязательства"),
    "short_term_liabilities": ("1500", "краткосрочные обязательства"),
}
NUMBER = re.compile(r"^\(?-?\d[\d \u00a0]*\)?$")


def _field(value: int, document_id: str | None, period: str) -> dict[str, Any]:
    return {"value": value, "source_document_id": document_id, "period": period,
            "verification_status": "UNVERIFIED", "currency": "RUB"}


def _units(text: str) -> int | None:
    matches = re.findall(r"(?:ед(?:иница)?\.?\s*измерения|в\s+)(?:\s*[:—-]\s*|\s+)(тыс(?:ячах)?|млн|руб(?:лях)?)[.\s]*(?:руб(?:лей|лях)?|₽)?", text, re.I)
    factors = {"тыс": 1000, "тысячах": 1000, "млн": 1_000_000,
               "руб": 1, "рублях": 1}
    found = {factors[match.lower()] for match in matches}
    return found.pop() if len(found) == 1 else None


def _period(text: str) -> str | None:
    # Form headers put the reporting year ahead of the comparable year.
    header = re.search(r"(?:за\s+|на\s+31\s+декабря\s+)(20\d{2})", text[:4000], re.I)
    return header.group(1) if header else None


def _amount(value: str, factor: int) -> int | None:
    value = value.strip()
    if not NUMBER.fullmatch(value):
        return None
    negative = value.startswith("(") or value.startswith("-")
    digits = int(re.sub(r"\D", "", value))
    amount = digits * factor * (-1 if negative else 1)
    return amount if abs(amount) <= 1_000_000_000_000_000 else None


def _row(lines: list[str], code: str, label: str, factor: int) -> int | None:
    values: list[int] = []
    for index, line in enumerate(lines):
        if label not in line.casefold().replace("ё", "е"):
            continue
        # PDFium and DOCX tables put code and first amount on the same line;
        # some PDF text layers put each table cell on its own line.
        for offset in range(3):
            if index + offset >= len(lines):
                break
            code_match = re.search(rf"\b{code}\b", lines[index + offset])
            if not code_match:
                continue
            tail = lines[index + offset][code_match.end():].lstrip(" \t;|")
            separate_cell = "\t" in tail
            if not tail and index + offset + 1 < len(lines):
                tail = lines[index + offset + 1]
                separate_cell = True
            if separate_cell:
                tail = tail.split("\t", 1)[0].strip()
            # Plain whitespace cannot distinguish "300 100" (one value) from
            # two adjacent columns. Accept it only when a cell boundary exists.
            if NUMBER.fullmatch(tail) and (separate_cell or " " not in tail):
                amount = _amount(tail, factor)
                if amount is not None:
                    values.append(amount)
            break
    return values[0] if values and len(set(values)) == 1 else None


def parse_financial_text(text: str, document_id: str | None) -> dict[str, Any]:
    normalized = text.casefold().replace("ё", "е")
    if not ("бухгалтерский баланс" in normalized or "отчет о финансовых результатах" in normalized):
        return {}
    factor, period = _units(text), _period(text)
    if factor is None or period is None:
        return {}
    lines = [line.strip() for line in text.splitlines() if line.strip()]
    found = {name: _row(lines, code, label, factor)
             for name, (code, label) in ROWS.items()}
    fields = {name: _field(found[name], document_id, period)
              for name in ("revenue", "net_profit", "equity") if found[name] is not None}
    if found["long_term_liabilities"] is not None and found["short_term_liabilities"] is not None:
        fields["liabilities"] = _field(
            found["long_term_liabilities"] + found["short_term_liabilities"], document_id, period)
    return fields
