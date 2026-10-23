"""Conservative parser for 1CClientBankExchange account summary sections.

The format is documented by 1C at
https://v8.1c.ru/tekhnologii/obmen-dannymi-i-integratsiya/standarty-i-formaty/standart-obmena-s-sistemami-klient-banka/formaty-obmena/
"""

from __future__ import annotations

from collections import defaultdict
from datetime import date, datetime, timedelta
from decimal import Decimal, InvalidOperation
from typing import Any


class Unsupported1CFormat(ValueError):
    """Valid-looking export variant that needs manual review or another adapter."""


def _rubles(raw: str) -> Decimal:
    try:
        value = Decimal(raw.strip().replace(",", "."))
        if not value.is_finite() or value.as_tuple().exponent < -2 or abs(value) > 1_000_000_000_000_000:
            raise ValueError("1C amount is invalid or exceeds supported range")
        return value.quantize(Decimal("0.01"))
    except InvalidOperation as exc:
        raise ValueError(f"invalid 1C amount: {raw}") from exc


def _date(raw: str) -> date:
    try:
        return datetime.strptime(raw, "%d.%m.%Y").date()
    except ValueError as exc:
        raise ValueError(f"invalid 1C date: {raw}") from exc


def _decode(raw: bytes) -> str:
    raw = raw.removeprefix(b"\xef\xbb\xbf")
    if not raw.startswith(b"1CClientBankExchange"):
        raise ValueError("1CClientBankExchange header is required")
    # The header itself is ASCII; the declared encoding controls Cyrillic fields.
    for encoding in ("utf-8-sig", "cp1251", "cp866"):
        try:
            text = raw.decode(encoding)
        except UnicodeDecodeError:
            continue
        if "Кодировка=Windows" in text and encoding == "cp1251":
            return text
        if "Кодировка=DOS" in text and encoding == "cp866":
            return text
        if encoding == "utf-8-sig" and "Кодировка=" in text:
            return text
    raise ValueError("unsupported or undeclared 1C encoding")


def _sections(text: str) -> list[dict[str, str]]:
    sections: list[dict[str, str]] = []
    current: dict[str, str] | None = None
    for line in text.splitlines():
        line = line.strip()
        if line == "СекцияРасчСчет":
            if current is not None:
                raise ValueError("nested 1C account section")
            current = {}
        elif line == "КонецРасчСчет":
            if current is None:
                raise ValueError("orphan 1C account section end")
            sections.append(current)
            current = None
        elif line.startswith("СекцияДокумент="):
            if current is not None:
                raise ValueError("unclosed 1C account section")
        elif "=" in line and current is not None:
            key, value = line.split("=", 1)
            if key in current:
                raise ValueError(f"duplicate 1C field: {key}")
            current[key] = value.strip()
    if current is not None:
        raise ValueError("unclosed 1C account section")
    if not sections:
        raise Unsupported1CFormat("1C file has no account summary sections")
    return sections


def _header(text: str) -> dict[str, str]:
    header: dict[str, str] = {}
    for line in text.splitlines():
        line = line.strip()
        if line == "СекцияРасчСчет" or line.startswith("СекцияДокумент="):
            break
        if "=" in line:
            key, value = line.split("=", 1)
            if key != "РасчСчет":
                header[key] = value.strip()
    return header


def parse_1c_statement(raw: bytes, document_id: str | None) -> dict[str, Any]:
    text = _decode(raw)
    sections = _sections(text)
    header = _header(text)
    try:
        header_start = _date(header["ДатаНачала"])
        header_end = _date(header["ДатаКонца"])
    except KeyError as exc:
        raise Unsupported1CFormat(f"1C header lacks {exc.args[0]}") from exc
    if header_start > header_end:
        raise ValueError("1C header date interval is invalid")
    account: str | None = None
    monthly: dict[str, dict[str, Decimal]] = defaultdict(lambda: {"credit": Decimal(0), "debit": Decimal(0)})
    ranges: list[tuple[date, date]] = []
    balances: list[tuple[date, date, Decimal, Decimal]] = []
    opening: tuple[date, Decimal] | None = None
    closing: tuple[date, Decimal] | None = None
    for section in sections:
        number = section.get("РасчСчет", "")
        if len(number) != 20 or not number.isdigit():
            raise ValueError("1C account number must contain 20 digits")
        if account is None:
            account = number
        elif number != account:
            raise Unsupported1CFormat("1C file contains multiple accounts; split the file")
        try:
            start = _date(section["ДатаНачала"])
            end = _date(section["ДатаКонца"])
            start_balance = _rubles(section["НачальныйОстаток"])
            credit = _rubles(section["ВсегоПоступило"])
            debit = _rubles(section["ВсегоСписано"])
        except KeyError as exc:
            raise Unsupported1CFormat(f"1C section lacks {exc.args[0]}; cannot verify turnover") from exc
        if start > end or start.strftime("%Y-%m") != end.strftime("%Y-%m"):
            raise Unsupported1CFormat("1C section must fit within one calendar month")
        if credit < 0 or debit < 0:
            raise ValueError("1C turnover cannot be negative")
        if any(start <= previous_end and previous_start <= end for previous_start, previous_end in ranges):
            raise ValueError("1C account sections overlap")
        ranges.append((start, end))
        end_balance = start_balance + credit - debit
        if "КонечныйОстаток" in section and _rubles(section["КонечныйОстаток"]) != end_balance:
            raise ValueError("1C closing balance does not match opening balance and turnover")
        month = start.strftime("%Y-%m")
        monthly[month]["credit"] += credit
        monthly[month]["debit"] += debit
        if opening is None or start < opening[0]:
            opening = (start, start_balance)
        if closing is None or end > closing[0]:
            closing = (end, end_balance)
        balances.append((start, end, start_balance, end_balance))
    assert opening is not None and closing is not None
    if opening[0] != header_start or closing[0] != header_end:
        raise Unsupported1CFormat("1C account sections do not cover the declared period")
    previous_end: date | None = None
    previous_balance: Decimal | None = None
    for start, end, start_balance, end_balance in sorted(balances):
        if previous_end is not None:
            if start > previous_end + timedelta(days=1):
                raise Unsupported1CFormat("1C account sections have a gap")
            if start_balance != previous_balance:
                raise ValueError("1C balance does not carry over between sections")
        previous_end, previous_balance = end, end_balance
    period = f"{opening[0].isoformat()}..{closing[0].isoformat()}"
    # Strings preserve kopecks exactly in JSON. Each amount is in RUB.
    def field(value: Any, field_period: str) -> dict[str, Any]:
        return {"value": value, "source_document_id": document_id,
                "period": field_period, "verification_status": "UNVERIFIED"}
    turnover = [{"period": month, "credit": str(totals["credit"].quantize(Decimal("0.01"))),
                 "debit": str(totals["debit"].quantize(Decimal("0.01")))}
                for month, totals in sorted(monthly.items())]
    return {
        "account_number": field(account, period),
        "turnover_monthly": {**field(turnover, period), "currency": "RUB"},
        "opening_balance": {**field(str(opening[1]), opening[0].isoformat()), "currency": "RUB"},
        "closing_balance": {**field(str(closing[1]), closing[0].isoformat()), "currency": "RUB"},
    }
