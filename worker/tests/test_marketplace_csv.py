import unittest

from worker.services.document_parser import parse_documents


def parse(text: str):
    result = parse_documents({"application_id": "application-1", "documents": [{
        "id": "market-1", "kind": "marketplace_sales_report", "filename": "report.csv",
        "media_type": "text/csv", "content_text": text,
    }]})
    return result["documents"][0]


class MarketplaceCSVTests(unittest.TestCase):
    def test_generic_russian_monthly_report(self):
        item = parse("Период;Продажи;Возвраты;Выплаты;Валюта\n"
                     "2025-01;1000,50;100,25;800,10;RUB\n"
                     "2025-02;2000;200;1700;RUB\n")
        self.assertEqual(item["status"], "PARSED")
        fields = item["extracted_fields"]
        self.assertEqual(fields["sales"]["value"], "3000.50")
        self.assertEqual(fields["returns"]["value"], "300.25")
        self.assertEqual(fields["payouts"]["value"], "2500.10")
        self.assertEqual(fields["sales"]["period"], "2025-01..2025-02")
        self.assertEqual(fields["sales"]["verification_status"], "UNVERIFIED")

    def test_unknown_vendor_layout_requires_adapter(self):
        item = parse("Дата заказа;Выручка;Комиссия\n01.01.2025;100;10\n")
        self.assertEqual(item["status"], "UNSUPPORTED")

    def test_invalid_negative_sales_fail(self):
        item = parse("period,sales,returns,payouts\n2025-01,-1,0,0\n")
        self.assertEqual(item["status"], "FAILED")

    def test_missing_payout_is_not_zero(self):
        item = parse("period,sales,returns,payouts\n2025-01,100,0,\n")
        self.assertEqual(item["status"], "FAILED")


if __name__ == "__main__":
    unittest.main()
