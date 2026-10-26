"""Generic monthly marketplace CSV export, not a vendor-specific API adapter."""

from __future__ import annotations

import csv
import io
import re
from collections import defaultdict
from decimal import Decimal
from typing import Any

from worker.services.account_statement_csv import UnsupportedStatementFormat, decoded_texts, _money


ALIASES = {
    "period": {"period", "month", "период", "месяц"},
    "sales": {"sales", "продажи", "сумма продаж"},
    "returns": {"returns", "возвраты", "сумма возвратов"},
    "payouts": {"payouts", "выплаты", "сумма выплат"},
    "currency": {"currency", "валюта"},
}
MONTH = re.compile(r"^\d{4}-(0[1-9]|1[0-2])$")


def _columns(row: list[str]) -> dict[str, int] | None:
    names = [value.strip().casefold().replace("ё", "е") for value in row]
    mapping: dict[str, int] = {}
    for name, aliases in ALIASES.items():
        normalized = {alias.replace("ё", "е") for alias in aliases}
        matches = [index for index, value in enumerate(names) if value in normalized]
        if len(matches) > 1:
            raise UnsupportedStatementFormat(f"ambiguous marketplace {name} columns")
        if matches:
            mapping[name] = matches[0]
    return mapping if {"period", "sales", "returns", "payouts"}.issubset(mapping) else None


def _rows(raw: bytes) -> tuple[dict[str, int], list[list[str]]]:
    for text in decoded_texts(raw):
        for delimiter in (";", ",", "\t"):
            try:
                rows = list(csv.reader(io.StringIO(text), delimiter=delimiter, strict=True))
            except csv.Error:
                continue
            for index, header in enumerate(rows[:11]):
                columns = _columns(header)
                if columns is not None:
                    return columns, rows[index + 1:]
    raise UnsupportedStatementFormat("marketplace columns period, sales, returns, payouts are required")


def _field(value: Any, document_id: str | None, period: str) -> dict[str, Any]:
    return {"value": value, "source_document_id": document_id, "period": period,
            "verification_status": "UNVERIFIED", "currency": "RUB"}


def parse_marketplace_csv(raw: bytes, document_id: str | None) -> dict[str, Any]:
    columns, rows = _rows(raw)
    monthly: dict[str, dict[str, Decimal]] = defaultdict(
        lambda: {"sales": Decimal("0.00"), "returns": Decimal("0.00"),
                 "payouts": Decimal("0.00")})
    for row in rows:
        if not any(cell.strip() for cell in row):
            continue
        if len(row) <= max(columns.values()):
            raise ValueError("marketplace row has missing columns")
        period = row[columns["period"]].strip()
        if not MONTH.fullmatch(period):
            raise ValueError("marketplace period must be YYYY-MM")
        if "currency" in columns and row[columns["currency"]].strip().upper() not in ("", "RUB", "RUR", "РУБ"):
            raise UnsupportedStatementFormat("only RUB marketplace reports are supported")
        for name in ("sales", "returns", "payouts"):
            if not row[columns[name]].strip():
                raise ValueError(f"marketplace {name} is required")
            amount = _money(row[columns[name]])
            if amount < 0:
                raise ValueError(f"marketplace {name} must be non-negative")
            monthly[period][name] += amount

    if not monthly:
        raise ValueError("marketplace report has no periods")
    periods = sorted(monthly)
    overall_period = f"{periods[0]}..{periods[-1]}"
    result = {
        name: _field(str(sum((month[name] for month in monthly.values()), Decimal("0.00"))),
                     document_id, overall_period)
        for name in ("sales", "returns", "payouts")
    }
    result["monthly"] = _field([
        {"period": period, **{name: str(monthly[period][name])
                              for name in ("sales", "returns", "payouts")}}
        for period in periods
    ], document_id, overall_period)
    return result
