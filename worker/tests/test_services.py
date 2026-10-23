import unittest
import base64

from worker.router import dispatch, lane_for


class WorkerServicesTest(unittest.TestCase):
    def test_credit_history_preserves_existing_demo_data(self):
        self.assertEqual(
            dispatch("credit_history", {"inn": "7700000002"}),
            {"debt": 2_400_000, "equity": 4_000_000},
        )
        self.assertEqual(lane_for("credit_history"), "fast")

    def test_application_verifier_flags_high_debt_and_missing_consent(self):
        result = dispatch("verify_application", {
            "application": {
                "inn": "7700000004", "org_type": "OOO", "product_id": "working_capital",
                "amount": 1_000_000, "term_months": 12, "purpose": "Оборотные средства",
                "bki_consent": False,
            },
            "company": {"total_debt": 7_000_000, "equity": 2_000_000},
        })
        self.assertFalse(result["complete"])
        self.assertEqual(result["missing_fields"], ["bki_consent"])
        self.assertEqual(result["debt_to_equity"], 3.5)
        self.assertTrue(result["manual_review_required"])
        self.assertEqual(result["next_status"], "INFO_REQUESTED")

    def test_parser_extracts_unverified_statement_fields(self):
        result = dispatch("parse_documents", {
            "application_id": "application-1",
            "documents": [{"id": "document-1", "kind": "financial_statements", "filename": "report.json",
                           "media_type": "application/json", "content_text":
                           '{"period":"2025","revenue":1000000,"net_profit":200000,"equity":500000,"liabilities":300000}'}],
        })
        self.assertEqual(lane_for("parse_documents"), "slow")
        self.assertEqual(result["documents"][0]["status"], "PARSED")
        self.assertEqual(result["documents"][0]["extracted_fields"]["equity"]["value"], 500000)

    def test_parser_accepts_backend_document_bytes_in_base64(self):
        raw = b'{"period":"2025","revenue":1000000,"net_profit":200000,"equity":500000,"liabilities":300000}'
        result = dispatch("parse_documents", {
            "application_id": "application-1",
            "documents": [{"id": "document-1", "kind": "financial_statements", "filename": "report.json",
                           "media_type": "application/json", "content_base64": base64.b64encode(raw).decode("ascii")}],
        })
        self.assertEqual(result["documents"][0]["status"], "PARSED")
        self.assertEqual(result["documents"][0]["extracted_fields"]["equity"]["source_document_id"], "document-1")

    def test_critical_review_never_rejects_automatically(self):
        result = dispatch("critical_risk_review", {
            "application_id": "application-1", "debt_to_equity": 3.5,
        })
        self.assertEqual(result["recommended_status"], "MANUAL_REVIEW")
        self.assertIsNone(result["decision"])

    def test_text_from_passport_does_not_count_as_verified_identity(self):
        result = dispatch("parse_documents", {
            "application_id": "application-1",
            "documents": [{"id": "passport-1", "kind": "passport", "filename": "passport.txt",
                           "media_type": "text/plain", "content_text": "ФИО Иванов Иван Иванович"}],
        })
        self.assertEqual(result["status"], "REVIEW_REQUIRED")
        self.assertEqual(result["review_reasons"], [
            {"document_id": "passport-1", "reason": "PASSPORT_NOT_VERIFIED"}])


if __name__ == "__main__":
    unittest.main()
