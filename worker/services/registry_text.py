"""Conservative identifiers from text layers of Russian register extracts.

These values are OCR/text hints. This module does not validate signatures or
query FNS/Rosreestr, so callers must keep them UNVERIFIED.
"""

from __future__ import annotations

import re
from typing import Any


def _field(value: str, document_id: str | None) -> dict[str, Any]:
    return {"value": value, "source_document_id": document_id,
            "period": None, "verification_status": "UNVERIFIED"}


def _inn_valid(value: str) -> bool:
    digits = [int(char) for char in value]
    if len(digits) == 10:
        return sum(a * b for a, b in zip(digits, (2, 4, 10, 3, 5, 9, 4, 6, 8))) % 11 % 10 == digits[9]
    if len(digits) == 12:
        first = sum(a * b for a, b in zip(digits, (7, 2, 4, 10, 3, 5, 9, 4, 6, 8))) % 11 % 10
        second = sum(a * b for a, b in zip(digits, (3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8))) % 11 % 10
        return first == digits[10] and second == digits[11]
    return False


def _ogrn_valid(value: str) -> bool:
    if len(value) == 13:
        return (int(value[:12]) % 11) % 10 == int(value[12])
    if len(value) == 15:
        return (int(value[:14]) % 13) % 10 == int(value[14])
    return False


def _labelled_number(text: str, label: str, lengths: tuple[int, ...]) -> str | None:
    pattern = rf"\b{label}\b\)?\s*(?:[:№]|номер)?\s*([0-9](?:[  ]*[0-9]){{{min(lengths)-1},{max(lengths)-1}}})(?![0-9])"
    candidates = []
    for match in re.finditer(pattern, text[:12000], re.I):
        value = re.sub(r"\D", "", match.group(1))
        if len(value) in lengths:
            candidates.append(value)
    unique = set(candidates)
    return candidates[0] if len(unique) == 1 else None


def parse_egrul_text(text: str, document_id: str | None) -> dict[str, Any]:
    normalized = text.casefold().replace("ё", "е")
    if not any(title in normalized for title in (
        "единого государственного реестра юридических лиц",
        "единого государственного реестра индивидуальных предпринимателей",
        "единый государственный реестр юридических лиц",
        "единый государственный реестр индивидуальных предпринимателей",
        "выписка из егрюл", "выписка из егрип",
    )):
        return {}
    fields = {}
    inn = _labelled_number(text, "ИНН", (10, 12))
    ogrn = _labelled_number(text, "ОГРН(?:ИП)?", (13, 15))
    if inn and _inn_valid(inn):
        fields["inn"] = _field(inn, document_id)
    if ogrn and _ogrn_valid(ogrn):
        fields["ogrn"] = _field(ogrn, document_id)
    return fields


CADASTRE = re.compile(r"\b\d{2}:\d{2}:\d{5,7}:\d{1,9}\b")


def parse_egrn_text(text: str, document_id: str | None) -> dict[str, Any]:
    normalized = text.casefold().replace("ё", "е")
    if not ("единого государственного реестра недвижимости" in normalized
            or "единый государственный реестр недвижимости" in normalized
            or "выписка из егрн" in normalized):
        return {}
    candidates = []
    for match in re.finditer(r"кадастровый\s+номер\s*[:№]?\s*([^\n]{1,80})", text, re.I):
        cadastre = CADASTRE.search(match.group(1))
        if cadastre:
            candidates.append(cadastre.group())
    unique = set(candidates)
    return {"cadastral_number": _field(candidates[0], document_id)} if len(unique) == 1 else {}


VIN = re.compile(r"\b[A-HJ-NPR-Z0-9]{17}\b", re.I)


def parse_vehicle_text(text: str, document_id: str | None) -> dict[str, Any]:
    normalized = text.casefold().replace("ё", "е")
    if not any(title in normalized for title in (
        "паспорт транспортного средства",
        "свидетельство о регистрации транспортного средства",
        "свидетельство о регистрации тс",
        "электронный паспорт транспортного средства",
    )):
        return {}
    values = []
    for match in re.finditer(r"\bVIN\b\s*(?:[:№-]|номер)?\s*([^\n]{1,40})", text, re.I):
        vin = VIN.search(match.group(1))
        if vin:
            values.append(vin.group().upper())
    return {"vin": _field(values[0], document_id)} if len(set(values)) == 1 and values else {}
