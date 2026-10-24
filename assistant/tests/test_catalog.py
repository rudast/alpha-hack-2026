import unittest
from assistant.domain.catalog import get_all_products, get_product_by_id


class TestCatalog(unittest.TestCase):
    def test_total_fourteen_products(self):
        products = get_all_products()
        self.assertEqual(len(products), 14, "Must contain exactly 14 credit products")

    def test_unique_ids_and_valid_ranges(self):
        products = get_all_products()
        seen_ids = set()
        seen_backend_ids = set()

        for p in products:
            self.assertNotIn(p.id, seen_ids, f"Duplicate frontend ID: {p.id}")
            self.assertNotIn(p.backend_id, seen_backend_ids, f"Duplicate backend ID: {p.backend_id}")
            seen_ids.add(p.id)
            seen_backend_ids.add(p.backend_id)

            self.assertGreater(p.rate, 0, f"{p.id}: rate must be positive")
            self.assertLessEqual(p.term[0], p.term[1], f"{p.id}: invalid term range")
            self.assertLessEqual(p.amount[0], p.amount[1], f"{p.id}: invalid amount range")

            # Check that every product has at least passport and bki
            doc_str = " ".join(p.required_docs).lower()
            self.assertIn("паспорт", doc_str, f"{p.id}: missing passport doc")
            self.assertIn("бки", doc_str, f"{p.id}: missing BKI consent doc")

    def test_lookup_aliases(self):
        self.assertIsNotNone(get_product_by_id("working-capital"))
        self.assertIsNotNone(get_product_by_id("working_capital"))
        self.assertIsNotNone(get_product_by_id("overdraft"))
        self.assertIsNotNone(get_product_by_id("seller"))
        self.assertIsNotNone(get_product_by_id("marketplace_seller"))
        self.assertIsNone(get_product_by_id("non_existent_product"))

    def test_young_business_limits(self):
        seller = get_product_by_id("seller")
        self.assertFalse(seller.needs9)
        self.assertEqual(seller.young_max, 3_000_000)

        express = get_product_by_id("express")
        self.assertFalse(express.needs9)
        self.assertEqual(express.young_max, 2_000_000)

        auto = get_product_by_id("auto")
        self.assertFalse(auto.needs9)
        self.assertEqual(auto.young_max, 5_000_000)

        secured = get_product_by_id("secured-property")
        self.assertFalse(secured.needs9)
        self.assertEqual(secured.young_max, 10_000_000)

        overdraft = get_product_by_id("overdraft")
        self.assertTrue(overdraft.needs9)
        self.assertIsNone(overdraft.young_max)


if __name__ == "__main__":
    unittest.main()
