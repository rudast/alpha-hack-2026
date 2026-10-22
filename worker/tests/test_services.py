import unittest

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
                "inn": "7700000004", "org_type": "OOO", "product_id": "working-capital",
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

    def test_parser_does_not_claim_to_extract_data(self):
        result = dispatch("parse_documents", {
            "application_id": "application-1",
            "documents": [{"kind": "financial_report", "filename": "report.pdf"}],
        })
        self.assertEqual(lane_for("parse_documents"), "slow")
        self.assertEqual(result["documents"][0]["status"], "NOT_PARSED")
        self.assertEqual(result["documents"][0]["extracted_fields"], {})

    def test_critical_review_never_rejects_automatically(self):
        result = dispatch("critical_risk_review", {
            "application_id": "application-1", "debt_to_equity": 3.5,
        })
        self.assertEqual(result["recommended_status"], "MANUAL_REVIEW")
        self.assertIsNone(result["decision"])


if __name__ == "__main__":
    unittest.main()
