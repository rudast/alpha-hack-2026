"""Extraction is deliberately incomplete and always requires review."""

import io
import unittest

from docx import Document

from worker.services.document_parser import parse_documents


FINANCIAL = """Бухгалтерский баланс
Единица измерения: тыс. руб.
На 31 декабря 2025 г.
Капитал и резервы\t1300\t2 500\t1 200
Долгосрочные обязательства\t1400\t300\t100
Краткосрочные обязательства\t1500\t700\t600
Отчет о финансовых результатах
Выручка\t2110\t12 000\t9 000
Чистая прибыль (убыток)\t2400\t(500)\t100
"""


def parse(kind: str, text: str, media_type: str = "text/plain") -> dict:
    if media_type.endswith("document"):
        document = Document()
        for line in text.splitlines():
            document.add_paragraph(line)
        file = io.BytesIO()
        document.save(file)
        raw = file.getvalue()
    else:
        raw = text.encode("utf-8")
    return parse_documents({"application_id": "app-1", "documents": [
        {"id": "doc-1", "kind": kind, "media_type": media_type, "content": raw}
    ]})


class StructuredTextParsersTest(unittest.TestCase):
    def test_financial_rows_use_reporting_column_and_thousand_rubles(self):
        result = parse("financial_statements", FINANCIAL)
        item = result["documents"][0]
        self.assertEqual(item["status"], "TEXT_EXTRACTED")
        fields = item["extracted_fields"]
        self.assertEqual({name: fields[name]["value"] for name in
                          ("revenue", "net_profit", "equity", "liabilities")},
                         {"revenue": 12_000_000, "net_profit": -500_000,
                          "equity": 2_500_000, "liabilities": 1_000_000})
        self.assertEqual(fields["revenue"]["period"], "2025")
        self.assertEqual(fields["revenue"]["verification_status"], "UNVERIFIED")
        self.assertEqual(result["status"], "REVIEW_REQUIRED")

    def test_financial_docx_and_split_cells(self):
        text = """Отчет о финансовых результатах
Единица измерения: тыс. руб.
За 2025
Выручка
2110
9 402
6 445
Чистая прибыль (убыток)
2400
(1 901)
211
"""
        result = parse("target_financial_statements", text,
                       "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
        fields = result["documents"][0]["extracted_fields"]
        self.assertEqual(fields["revenue"]["value"], 9_402_000)
        self.assertEqual(fields["net_profit"]["value"], -1_901_000)

    def test_financial_ambiguous_columns_and_missing_units_are_not_guessed(self):
        text = """Бухгалтерский баланс
Единица измерения: тыс. руб.
На 31 декабря 2025
Капитал и резервы 1300 300 100
Долгосрочные обязательства 1400 300
"""
        fields = parse("financial_statements", text)["documents"][0]["extracted_fields"]
        self.assertNotIn("equity", fields)
        self.assertNotIn("liabilities", fields)
        fields = parse("financial_statements", FINANCIAL.replace("тыс. руб.", "единицы"))["documents"][0]["extracted_fields"]
        self.assertNotIn("revenue", fields)

    def test_egrul_extracts_only_checksummed_identifiers(self):
        text = """Выписка из Единого государственного реестра юридических лиц
Основной государственный регистрационный номер (ОГРН)
1027700132195
Идентификационный номер налогоплательщика (ИНН)
7707083893
"""
        result = parse("egrul_extract", text)
        fields = result["documents"][0]["extracted_fields"]
        self.assertEqual(fields["inn"]["value"], "7707083893")
        self.assertEqual(fields["ogrn"]["value"], "1027700132195")
        self.assertEqual(result["status"], "REVIEW_REQUIRED")
        bad = parse("egrul_extract", text.replace("7707083893", "7707083894"))
        self.assertNotIn("inn", bad["documents"][0]["extracted_fields"])

    def test_egrn_extracts_labelled_cadastre_only(self):
        text = """Выписка из Единого государственного реестра недвижимости
Кадастровый номер: 77:01:0004012:1234
"""
        result = parse("egrn_extract", text)
        fields = result["documents"][0]["extracted_fields"]
        self.assertEqual(fields["cadastral_number"]["value"], "77:01:0004012:1234")
        self.assertEqual(fields["cadastral_number"]["verification_status"], "UNVERIFIED")
        unrelated = parse("egrn_extract", "Кадастровый номер: 77:01:0004012:1234")
        self.assertNotIn("cadastral_number", unrelated["documents"][0]["extracted_fields"])

    def test_vehicle_registration_extracts_labelled_vin_only(self):
        text = """Свидетельство о регистрации транспортного средства
VIN: XTA210990Y1234567
"""
        result = parse("vehicle_registration", text)
        fields = result["documents"][0]["extracted_fields"]
        self.assertEqual(fields["vin"]["value"], "XTA210990Y1234567")
        self.assertEqual(result["review_reasons"][0]["reason"], "FIELDS_UNVERIFIED")
        bad = parse("vehicle_registration", text.replace("XTA210990Y1234567", "XTA21099OY1234567"))
        self.assertNotIn("vin", bad["documents"][0]["extracted_fields"])

    def test_valuation_uses_market_value_and_explicit_valuation_date(self):
        text = """Отчет об оценке недвижимости
Дата оценки: 20.10.2026
Рыночная стоимость объекта оценки: 12 345 678,90 руб.
Заявленная заказчиком цена: 15 000 000 руб.
"""
        result = parse("valuation_report", text)
        value = result["documents"][0]["extracted_fields"]["appraised_value"]
        self.assertEqual(value["value"], "12345678.90")
        self.assertEqual(value["period"], "2026-10-20")
        self.assertEqual(result["status"], "REVIEW_REQUIRED")

    def test_valuation_without_date_or_unique_value_requires_review(self):
        text = """Отчет об оценке
Рыночная стоимость: 100 000 руб.
"""
        fields = parse("valuation_report", text)["documents"][0]["extracted_fields"]
        self.assertNotIn("appraised_value", fields)

    def test_contract_price_does_not_replace_appraised_value(self):
        text = """Предварительный договор купли-продажи нежилого помещения
Цена договора: 15 000 000 руб.
Первоначальный взнос: 3 000 000 руб.
"""
        result = parse("preliminary_purchase_contract", text)
        fields = result["documents"][0]["extracted_fields"]
        self.assertEqual(fields["contract_price"]["value"], "15000000.00")
        self.assertEqual(fields["down_payment"]["value"], "3000000.00")
        self.assertNotIn("appraised_value", fields)
        self.assertEqual(result["status"], "REVIEW_REQUIRED")

    def test_business_contract_price_needs_specific_title_and_label(self):
        text = """Договор купли-продажи доли
Цена договора: 7 500 000,50 руб.
"""
        fields = parse("business_purchase_contract", text)["documents"][0]["extracted_fields"]
        self.assertEqual(fields["contract_price"]["value"], "7500000.50")
        unrelated = parse("business_purchase_contract", "Цена договора: 7 500 000 руб.")
        self.assertNotIn("contract_price", unrelated["documents"][0]["extracted_fields"])

    def test_equipment_contract_price(self):
        text = """Договор купли-продажи оборудования
Стоимость договора: 2 400 000 руб.
"""
        fields = parse("equipment_purchase_contract", text)["documents"][0]["extracted_fields"]
        self.assertEqual(fields["contract_price"]["value"], "2400000.00")

    def test_technical_passport_cadastral_number(self):
        text = """Технический паспорт
Кадастровый номер: 77:01:0004012:1234
"""
        fields = parse("technical_passport", text)["documents"][0]["extracted_fields"]
        self.assertEqual(fields["cadastral_number"]["value"], "77:01:0004012:1234")


if __name__ == "__main__":
    unittest.main()
