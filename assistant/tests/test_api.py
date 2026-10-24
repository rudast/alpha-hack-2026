import unittest

try:
    from fastapi.testclient import TestClient
    from assistant.main import app
    HAS_FASTAPI = True
except ImportError:
    HAS_FASTAPI = False


class TestAPIEndpoints(unittest.TestCase):
    def setUp(self):
        if not HAS_FASTAPI:
            self.skipTest("fastapi or httpx not installed in current environment")
        self.client = TestClient(app)

    def test_healthz(self):
        res = self.client.get("/healthz")
        self.assertEqual(res.status_code, 200)
        self.assertEqual(res.json(), {"status": "ok", "service": "assistant"})

        # Also test proxied prefix path
        res_proxy = self.client.get("/api/v1/assistant/healthz")
        self.assertEqual(res_proxy.status_code, 200)
        self.assertEqual(res_proxy.json(), {"status": "ok", "service": "assistant"})

    def test_products_endpoint(self):
        res = self.client.get("/api/v1/assistant/products")
        self.assertEqual(res.status_code, 200)
        data = res.json()
        self.assertEqual(len(data), 14)

    def test_chat_endpoint(self):
        payload = {
            "session_id": "api-test-1",
            "message": "Здравствуйте! Нужен кредит 1 миллион рублей на 12 месяцев",
        }
        res = self.client.post("/api/v1/assistant/chat", json=payload)
        self.assertEqual(res.status_code, 200)
        data = res.json()
        self.assertEqual(data["session_id"], "api-test-1")
        self.assertTrue(len(data["recommended_products"]) > 0)
        self.assertTrue(len(data["actions"]) > 0)

    def test_recommend_endpoint(self):
        payload = {
            "org": "ooo",
            "collateral": "without",
            "age_months": 12,
            "amount": 2000000,
            "term_months": 12,
            "purpose": "marketplace",
        }
        res = self.client.post("/api/v1/assistant/recommend", json=payload)
        self.assertEqual(res.status_code, 200)
        data = res.json()
        self.assertTrue(len(data["offers"]) > 0)
        self.assertEqual(data["offers"][0]["product"]["id"], "seller")


if __name__ == "__main__":
    unittest.main()
