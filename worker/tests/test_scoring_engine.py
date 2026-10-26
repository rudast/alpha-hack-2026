import unittest
from pathlib import Path

import yaml

from worker.scoring_engine import MissingData, load_config, score_application


def base(**changes):
    data = {
        "product_id": "working_capital", "amount": 3_000_000, "term_months": 12,
        "credit_history": {"current_overdue_days": 0, "closed_overdue_30d_24m": 0,
                           "overdues_30d_12m": 0, "history_months": 48, "inquiries_90d": 1},
        "debt": 3_000_000, "equity": 5_000_000, "age_months": 48,
        "registry": {"red_flags": [], "yellow_flags": []}, "industry_risk": "low",
        "turnover_monthly": 2_500_000, "review_reasons": [],
    }
    data.update(changes)
    return data


class ScoringEngineTests(unittest.TestCase):
    def test_unknown_industry_is_neutral_and_not_asked_from_the_client(self):
        data = base()
        del data["industry_risk"]
        data["okved"] = "46.90"  # нет в справочнике
        result = score_application(data)
        factor = next(f for f in result["factors"] if f["code"] == "C4b")
        self.assertEqual(factor["r"], 0.5)
        self.assertEqual(result["data_gaps"], ["industry_risk"])

        known = base()
        del known["industry_risk"]
        known["okved"] = "47.19"
        self.assertEqual(score_application(known)["data_gaps"], [])

    def test_configuration_covers_all_products(self):
        self.assertEqual(len(load_config()["products"]), 14)

    def test_worker_document_requirements_match_backend_catalog(self):
        catalog_path = Path(__file__).resolve().parents[2] / "internal/catalog/products.yaml"
        if not catalog_path.exists():
            self.skipTest("backend catalog is not included in the worker image")
        catalog = yaml.safe_load(catalog_path.read_text(encoding="utf-8"))
        config = load_config()
        self.assertEqual(config["base_documents"], catalog["base_documents"])
        self.assertEqual(set(config["products"]), {item["id"] for item in catalog["products"]})
        for item in catalog["products"]:
            with self.subTest(product=item["id"]):
                policy = config["products"][item["id"]]
                self.assertEqual(policy["documents"], item.get("documents", []))
                if "P1" in policy["factors"]:
                    self.assertEqual(policy["max_term_months"], item["max_term_months"])

    def test_s1_approval_and_rate(self):
        result = score_application(base())
        self.assertEqual(result["score"], 85.0)
        self.assertEqual(result["outcome"], "APPROVED")
        self.assertEqual(result["rate"], 18.25)  # база working_capital 19 % − 0,75
        self.assertTrue(all("value" in factor for factor in result["factors"]))

    def test_s2_low_score_to_manual_review(self):
        history = dict(base()["credit_history"], overdues_30d_12m=3, inquiries_90d=6)
        result = score_application(base(amount=4_000_000, credit_history=history,
                                        debt=7_000_000, equity=2_000_000,
                                        age_months=24, industry_risk="medium",
                                        turnover_monthly=1_800_000))
        self.assertEqual(result["score"], 30.7)
        self.assertEqual(result["outcome"], "MANUAL_REVIEW")

    def test_s3_collateral_counter_offer(self):
        result = score_application(base(product_id="auto_loan", amount=2_000_000,
                                        term_months=48, debt=800_000, equity=2_000_000,
                                        age_months=36, collateral_type="vehicle",
                                        collateral_confirmed_value=2_500_000))
        self.assertEqual(result["score"], 74.1)
        self.assertEqual(result["outcome"], "COUNTER_OFFER")
        self.assertEqual(result["offer"]["amount"], 1_500_000)
        self.assertEqual(result["offer"]["rate"], 14.62)  # база auto_loan 15,5 % − 0,88

    def test_stop_factor_and_unverified_evidence_prevent_approval(self):
        history = dict(base()["credit_history"], current_overdue_days=45)
        self.assertEqual(score_application(base(credit_history=history))["ko"], ["KO1"])
        self.assertEqual(score_application(base(credit_history=history))["outcome"], "MANUAL_REVIEW")
        self.assertEqual(score_application(base(review_reasons=["PASSPORT_NOT_VERIFIED"]))["outcome"], "MANUAL_REVIEW")

    def test_missing_bureau_never_becomes_good_history(self):
        with self.assertRaises(MissingData) as caught:
            score_application(base(credit_history=None))
        self.assertEqual(caught.exception.fields, ["credit_history"])

    def test_non_integer_amount_is_rejected_before_scoring(self):
        with self.assertRaises(MissingData):
            score_application(base(amount=3_000_000.5))

    def test_overdraft_without_settlement_account_is_manual(self):
        result = score_application(base(product_id="overdraft", amount=500_000,
                                        term_months=2, has_settlement_account=False))
        self.assertEqual(result["outcome"], "MANUAL_REVIEW")
        self.assertIn("KO5", result["ko"])

    def test_s12_counter_offer_uses_formula_and_catalog_max_term(self):
        result = score_application(base(turnover_monthly=700_000))
        self.assertEqual(result["outcome"], "COUNTER_OFFER")
        self.assertIn("L-PTI", result["limiters"])
        # At the product's 19% the requested 3 million first meets the 20% payment target
        # at month 27, within the catalogue's 36-month maximum.
        self.assertEqual(result["offer"]["amount"], 3_000_000)
        self.assertEqual(result["offer"]["term_months"], 27)

    def test_documented_scenarios_s4_to_s17(self):
        clean_history = base()["credit_history"]
        cases = {
            "S4": (dict(credit_history=dict(clean_history, current_overdue_days=45)), 66.1, "MANUAL_REVIEW"),
            "S5": (dict(registry={"red_flags": ["LIQUIDATION"], "yellow_flags": []}), 78.3, "MANUAL_REVIEW"),
            "S6": (dict(age_months=20, debt=7_500_000, credit_history=dict(clean_history,
                        overdues_30d_12m=1, inquiries_90d=4), industry_risk="medium",
                        turnover_monthly=1_700_000), 62.1, "COUNTER_OFFER"),
            "S7": (dict(amount=6_000_000, turnover_monthly=6_000_000), 93.3, "MANUAL_REVIEW"),
            "S8": (dict(equity=-100_000), 67.2, "MANUAL_REVIEW"),
            "S9": (dict(turnover_monthly=None), None, "INFO_REQUESTED"),
            "S10": (dict(product_id="express", amount=500_000, term_months=6,
                         age_months=5, credit_history=dict(clean_history, history_months=0)),
                    65.4, "COUNTER_OFFER"),
            "S11": (dict(product_id="express", amount=800_000), 90.8, "APPROVED"),
            "S12": (dict(turnover_monthly=700_000), 65.6, "COUNTER_OFFER"),
            "S13": (dict(product_id="mortgage_business", amount=10_000_000, term_months=84,
                         collateral_type="real_estate", collateral_confirmed_value=16_000_000,
                         down_payment=6_000_000, object_price=16_000_000), 81.1, "APPROVED"),
            "S14": (dict(product_id="credit_line", amount=2_000_000,
                         monthly_turnovers=[1_250_000, 3_750_000] * 3), 86.5, "APPROVED"),
            "S15": (dict(product_id="marketplace_seller", amount=1_500_000,
                         turnover_monthly=1_800_000, marketplace_revenue_share=85), 90.0, "APPROVED"),
            "S16": (dict(product_id="business_purchase", amount=8_000_000, term_months=60,
                         collateral_type="other", collateral_confirmed_value=12_000_000,
                         target_turnover=40_000_000, target_profit=3_000_000), 71.0, "COUNTER_OFFER"),
            "S17": (dict(product_id="overdraft", amount=500_000, term_months=2,
                         has_settlement_account=False), 76.5, "MANUAL_REVIEW"),
        }
        for name, (changes, expected_score, expected_outcome) in cases.items():
            with self.subTest(name=name):
                if expected_outcome == "INFO_REQUESTED":
                    with self.assertRaises(MissingData):
                        score_application(base(**changes))
                    continue
                result = score_application(base(**changes))
                self.assertEqual(result["score"], expected_score)
                self.assertEqual(result["outcome"], expected_outcome)


if __name__ == "__main__":
    unittest.main()
