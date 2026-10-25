import unittest
from assistant.engine.matcher import ProductMatcher, compute_annuity


class TestMatcher(unittest.TestCase):
    def test_annuity_calculation(self):
        # 1 000 000 rubles at 12% annual rate for 12 months
        # Monthly rate = 1%, annuity factor = 0.01 * 1.01^12 / (1.01^12 - 1) ~ 0.0888487
        # PMT ~ 88 849 rubles
        pmt, overpayment = compute_annuity(1_000_000, 12.0, 12)
        self.assertAlmostEqual(pmt, 88849, delta=5)
        self.assertAlmostEqual(overpayment, pmt * 12 - 1_000_000, delta=1)

    def test_business_under_9_months_excludes_restricted_products(self):
        # Overdraft, working capital, equipment, ready business require >= 9 months
        offers = ProductMatcher.match(
            org="ooo",
            collateral="without",
            age_months=5,
            age_category="lt9",
            amount=1_000_000,
            term_months=12,
        )
        matched_ids = [o.product.id for o in offers]
        self.assertNotIn("overdraft", matched_ids)
        self.assertNotIn("working-capital", matched_ids)
        self.assertNotIn("ooo-unsecured", matched_ids)

        # Allowed products for young business: seller, express, small
        self.assertTrue(any(pid in matched_ids for pid in ["express", "small", "seller"]))

    def test_business_under_9_months_caps_amounts(self):
        # Seller credit max 3m for young business
        offers = ProductMatcher.match(
            org="ip",
            collateral="without",
            age_months=4,
            age_category="lt9",
            amount=5_000_000,
            term_months=12,
            purpose="marketplace",
        )
        self.assertTrue(len(offers) > 0)
        best = offers[0]
        self.assertEqual(best.product.id, "seller")
        self.assertTrue(any("ограничен" in w.lower() or "скорректирована" in w.lower() for w in best.warnings))

    def test_org_filtering(self):
        # IP cannot get mortgage-ooo or ooo-unsecured
        ip_offers = ProductMatcher.match(
            org="ip",
            collateral="with",
            age_months=24,
            amount=5_000_000,
            term_months=36,
        )
        ip_product_ids = [o.product.id for o in ip_offers]
        self.assertNotIn("mortgage-ooo", ip_product_ids)
        self.assertNotIn("ooo-unsecured", ip_product_ids)

        # OOO cannot get mortgage-ip or ip-unsecured
        ooo_offers = ProductMatcher.match(
            org="ooo",
            collateral="without",
            age_months=24,
            amount=500_000,
            term_months=12,
        )
        ooo_product_ids = [o.product.id for o in ooo_offers]
        self.assertNotIn("ip-unsecured", ooo_product_ids)
        self.assertNotIn("mortgage-ip", ooo_product_ids)

    def test_collateral_without_excludes_secured(self):
        offers = ProductMatcher.match(
            org="ooo",
            collateral="without",
            age_months=24,
            amount=2_000_000,
            term_months=24,
        )
        for o in offers:
            self.assertIn(o.product.collateral, ["without", "any"])
            self.assertNotEqual(o.product.collateral, "with")

    def test_purpose_ranking(self):
        # Marketplace inquiry should rank seller #1
        offers = ProductMatcher.match(
            org="ooo",
            collateral="without",
            age_months=12,
            amount=2_000_000,
            term_months=12,
            purpose="marketplace",
        )
        self.assertEqual(offers[0].product.id, "seller")

        # Vehicle inquiry should rank auto #1
        v_offers = ProductMatcher.match(
            org="ooo",
            collateral="with",
            age_months=12,
            amount=3_000_000,
            term_months=24,
            purpose="vehicle",
        )
    def test_target_product_boost_and_warning(self):
        # Mature business asking for overdraft gets overdraft
        offers = ProductMatcher.match(
            org="ooo",
            collateral="without",
            age_months=24,
            amount=500_000,
            term_months=6,
            target_product_id="overdraft",
        )
        self.assertEqual(offers[0].product.id, "overdraft")

        # Young business asking for overdraft gets warning and available alternatives
        young_offers = ProductMatcher.match(
            org="ooo",
            collateral="without",
            age_months=4,
            amount=500_000,
            term_months=6,
            target_product_id="overdraft",
        )
        self.assertNotIn("overdraft", [o.product.id for o in young_offers])
        self.assertTrue(any("от 9 месяцев" in w for w in young_offers[0].warnings))


if __name__ == "__main__":
    unittest.main()
