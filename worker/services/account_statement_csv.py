"""Conservative CSV account statement parser with balance reconciliation."""

from __future__ import annotations

import csv
import io
import re
from collections import defaultdict
from datetime import date, datetime
from decimal import Decimal
from typing import Any


class UnsupportedStatementFormat(ValueError):
    """The file may be a statement, but its layout needs a separate adapter."""


HEADERS = {
    "date": {"date", "дата", "дата операции", "дата проводки"},
    "credit": {"credit", "поступление", "приход", "кредит"},
    "debit": {"debit", "списание", "расход", "дебет"},
    "balance": {"balance", "остаток", "остаток после операции"},
    "account": {"account", "account_number", "счет", "счёт", "номер счета", "номер счёта"},
    "currency": {"currency", "валюта"},
}
MONEY = re.compile(r"^[+-]?\d+(?:[.,]\d{1,2})?$")


def decoded_texts(raw: bytes):
    for encoding in ("utf-8-sig", "cp1251", "cp866"):
        try:
            yield raw.decode(encoding)
        except UnicodeDecodeError:
            continue


def _headers(row: list[str]) -> dict[str, int] | None:
    normalized = [value.strip().casefold().replace("ё", "е") for value in row]
    mapping: dict[str, int] = {}
    for name, aliases in HEADERS.items():
        matching = [index for index, value in enumerate(normalized)
                    if value in {alias.replace("ё", "е") for alias in aliases}]
        if len(matching) > 1:
            raise UnsupportedStatementFormat(f"ambiguous {name} columns")
        if matching:
            mapping[name] = matching[0]
    return mapping if {"date", "credit", "debit", "balance"}.issubset(mapping) else None


def _rows(raw: bytes) -> tuple[dict[str, int], list[list[str]]]:
    for text in decoded_texts(raw):
        for delimiter in (";", ",", "\t"):
            try:
                rows = list(csv.reader(io.StringIO(text), delimiter=delimiter, strict=True))
            except csv.Error:
                continue
            for index, header in enumerate(rows[:11]):
                mapping = _headers(header)
                if mapping is not None:
                    return mapping, rows[index + 1:]
    raise UnsupportedStatementFormat("statement columns date, credit, debit, balance are required")


def _money(raw: str) -> Decimal:
    value = raw.strip().replace("\u00a0", "").replace(" ", "")
    if not value:
        return Decimal("0.00")
    if not MONEY.fullmatch(value):
        raise ValueError(f"invalid statement amount: {raw}")
    if len(value) > 30:
        raise ValueError("statement amount exceeds supported range")
    amount = Decimal(value.replace(",", "."))
    if abs(amount) > 1_000_000_000_000_000:
        raise ValueError("statement amount exceeds supported range")
    return amount.quantize(Decimal("0.01"))


def _date(raw: str) -> date:
    for pattern in ("%Y-%m-%d", "%d.%m.%Y", "%d/%m/%Y"):
        try:
            return datetime.strptime(raw.strip(), pattern).date()
        except ValueError:
            continue
    raise ValueError(f"invalid statement date: {raw}")


def _field(value: Any, document_id: str | None, period: str) -> dict[str, Any]:
    return {"value": value, "source_document_id": document_id, "period": period,
            "verification_status": "UNVERIFIED", "currency": "RUB"}


def parse_csv_statement(raw: bytes, document_id: str | None) -> dict[str, Any]:
    columns, raw_rows = _rows(raw)
    transactions: list[tuple[date, Decimal, Decimal, Decimal]] = []
    accounts: set[str] = set()
    for row in raw_rows:
        if not any(cell.strip() for cell in row):
            continue
        if len(row) <= max(columns.values()):
            raise ValueError("statement row has missing columns")
        day = _date(row[columns["date"]])
        credit, debit = _money(row[columns["credit"]]), _money(row[columns["debit"]])
        if not row[columns["balance"]].strip():
            raise ValueError("statement balance is required")
        balance = _money(row[columns["balance"]])
        if credit < 0 or debit < 0 or (credit > 0 and debit > 0):
            raise ValueError("statement credit and debit must be non-negative and separate")
        if "currency" in columns and row[columns["currency"]].strip().upper() not in ("", "RUB", "RUR", "РУБ"):
            raise UnsupportedStatementFormat("only RUB statements are supported")
        if "account" in columns and row[columns["account"]].strip():
            account = row[columns["account"]].strip().replace(" ", "")
            if len(account) != 20 or not account.isdigit():
                raise ValueError("account number must contain 20 digits")
            accounts.add(account)
        transactions.append((day, credit, debit, balance))

    if not transactions:
        raise ValueError("statement has no transactions")
    if len(accounts) > 1:
        raise UnsupportedStatementFormat("statement contains multiple accounts")

    # Bank exports may run oldest-first or newest-first. Validate the running
    # balance in either direction; do not sort transactions and invent an order.
    ascending = all(a[0] <= b[0] for a, b in zip(transactions, transactions[1:]))
    descending = all(a[0] >= b[0] for a, b in zip(transactions, transactions[1:]))
    if not (ascending or descending):
        raise UnsupportedStatementFormat("transactions are not ordered by date")

    def reconciles(items: list[tuple[date, Decimal, Decimal, Decimal]]) -> bool:
        return all(current[3] == previous[3] + current[1] - current[2]
                   for previous, current in zip(items, items[1:]))

    chronological = transactions if ascending else list(reversed(transactions))
    if not reconciles(chronological):
        reverse = list(reversed(transactions))
        if not descending or not reconciles(reverse):
            raise ValueError("statement balance does not reconcile with transactions")
        chronological = reverse

    first, last = chronological[0], chronological[-1]
    opening = first[3] - first[1] + first[2]
    period = f"{first[0].isoformat()}..{last[0].isoformat()}"
    monthly: dict[str, dict[str, Decimal]] = defaultdict(
        lambda: {"credit": Decimal("0.00"), "debit": Decimal("0.00")})
    for day, credit, debit, _ in chronological:
        month = day.strftime("%Y-%m")
        monthly[month]["credit"] += credit
        monthly[month]["debit"] += debit
    turnover = [{"period": month, "credit": str(totals["credit"]),
                 "debit": str(totals["debit"])} for month, totals in sorted(monthly.items())]
    result = {
        "turnover_monthly": _field(turnover, document_id, period),
        "opening_balance": _field(str(opening), document_id, first[0].isoformat()),
        "closing_balance": _field(str(last[3]), document_id, last[0].isoformat()),
    }
    if accounts:
        result["account_number"] = _field(next(iter(accounts)), document_id, period)
    return result
