import unittest

from worker.router import dispatch


ACCOUNT = "40702810900000000001"


def section(start: str, end: str, opening: str, credit: str, debit: str, closing: str) -> str:
    return "\n".join((
        "СекцияРасчСчет", f"ДатаНачала={start}", f"ДатаКонца={end}", f"РасчСчет={ACCOUNT}",
        f"НачальныйОстаток={opening}", f"ВсегоПоступило={credit}",
        f"ВсегоСписано={debit}", f"КонечныйОстаток={closing}", "КонецРасчСчет",
    ))


def parse(text: str, encoding: str = "cp1251") -> dict:
    document = {"id": "statement-1", "kind": "account_statement", "filename": "kl_to_1c.txt",
                "media_type": "text/plain", "content": text.encode(encoding)}
    return dispatch("parse_documents", {"application_id": "application-1", "documents": [document]})["documents"][0]


class BankStatement1CTest(unittest.TestCase):
    def test_windows_export_preserves_kopecks_and_months(self):
        text = "\n".join(("1CClientBankExchange", "Кодировка=Windows", "ДатаНачала=01.01.2026", "ДатаКонца=28.02.2026",
                          section("01.01.2026", "31.01.2026", "100.10", "20.20", "5.05", "115.25"),
                          section("01.02.2026", "28.02.2026", "115.25", "50,75", "10.00", "156.00"),
                          "КонецФайла"))
        item = parse(text)
        self.assertEqual(item["status"], "PARSED")
        fields = item["extracted_fields"]
        self.assertEqual(fields["turnover_monthly"]["value"], [
            {"period": "2026-01", "credit": "20.20", "debit": "5.05"},
            {"period": "2026-02", "credit": "50.75", "debit": "10.00"},
        ])
        self.assertEqual(fields["closing_balance"]["value"], "156.00")
        self.assertEqual(fields["account_number"]["value"], ACCOUNT)

    def test_dos_export(self):
        text = "\n".join(("1CClientBankExchange", "Кодировка=DOS", "ДатаНачала=01.01.2026", "ДатаКонца=31.01.2026",
                          section("01.01.2026", "31.01.2026", "100", "20", "5", "115")))
        self.assertEqual(parse(text, "cp866")["status"], "PARSED")

    def test_utf8_export_with_bom(self):
        text = "\n".join(("1CClientBankExchange", "Кодировка=UTF-8", "ДатаНачала=01.01.2026", "ДатаКонца=31.01.2026",
                          section("01.01.2026", "31.01.2026", "100", "20", "5", "115")))
        document = {"id": "statement-1", "kind": "account_statement", "filename": "kl_to_1c.txt",
                    "media_type": "text/plain", "content": text.encode("utf-8-sig")}
        result = dispatch("parse_documents", {"application_id": "application-1", "documents": [document]})
        self.assertEqual(result["documents"][0]["status"], "PARSED")

    def test_bad_balance_is_failed(self):
        text = "\n".join(("1CClientBankExchange", "Кодировка=Windows", "ДатаНачала=01.01.2026", "ДатаКонца=31.01.2026",
                          section("01.01.2026", "31.01.2026", "100", "20", "5", "999")))
        item = parse(text)
        self.assertEqual(item["status"], "FAILED")
        self.assertIn("closing balance", item["error"])

    def test_multi_month_section_needs_another_adapter(self):
        text = "\n".join(("1CClientBankExchange", "Кодировка=Windows", "ДатаНачала=01.01.2026", "ДатаКонца=28.02.2026",
                          section("01.01.2026", "28.02.2026", "100", "20", "5", "115")))
        self.assertEqual(parse(text)["status"], "UNSUPPORTED")

    def test_partial_declared_period_is_not_marked_parsed(self):
        text = "\n".join(("1CClientBankExchange", "Кодировка=Windows", "ДатаНачала=01.01.2026", "ДатаКонца=28.02.2026",
                          section("01.01.2026", "31.01.2026", "100", "20", "5", "115")))
        self.assertEqual(parse(text)["status"], "UNSUPPORTED")

    def test_mismatched_balance_between_sections_is_failed(self):
        text = "\n".join(("1CClientBankExchange", "Кодировка=Windows", "ДатаНачала=01.01.2026", "ДатаКонца=02.01.2026",
                          section("01.01.2026", "01.01.2026", "100", "20", "5", "115"),
                          section("02.01.2026", "02.01.2026", "120", "0", "0", "120")))
        item = parse(text)
        self.assertEqual(item["status"], "FAILED")
        self.assertIn("carry over", item["error"])


if __name__ == "__main__":
    unittest.main()
