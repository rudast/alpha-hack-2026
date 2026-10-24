"""Optional end-to-end check of routing and document results."""

import json
import os
import threading
import unittest
from uuid import uuid4

import redis

from worker.main import Worker


@unittest.skipUnless(os.getenv("REDIS_TEST_ADDR"), "set REDIS_TEST_ADDR for Redis integration")
class LegacyRedisTests(unittest.TestCase):
    def test_fast_and_slow_jobs(self):
        client = redis.Redis.from_url("redis://" + os.environ["REDIS_TEST_ADDR"], decode_responses=True)
        client.ping()
        worker = Worker(client)
        errors = []

        def run_worker():
            try:
                worker.run()
            except Exception as exc:
                errors.append(exc)

        thread = threading.Thread(target=run_worker, daemon=True)
        thread.start()

        def request(kind, payload):
            task_id = uuid4().hex
            client.xadd("worker:requests", {"task": json.dumps({
                "id": task_id, "kind": kind, "payload": payload,
            })})
            reply = client.blpop("worker:reply:" + task_id, timeout=8)
            self.assertIsNotNone(reply, f"worker stopped: {errors}")
            result = json.loads(reply[1])
            self.assertNotIn("error", result)
            return result["data"]

        try:
            history = request("credit_history", {"inn": "7700000002"})
            self.assertEqual(history["debt"], 2400000)

            parsed = request("parse_documents", {"application_id": str(uuid4()), "documents": [{
                "id": "statement-1", "kind": "account_statement", "filename": "statement.csv",
                "media_type": "text/csv", "content_text":
                "date,credit,debit,balance\n2025-01-02,100.50,0,1100.50\n",
            }]})
            self.assertEqual(parsed["documents"][0]["status"], "PARSED")
            self.assertEqual(parsed["documents"][0]["extracted_fields"]["closing_balance"]["value"],
                             "1100.50")
            self.assertEqual(errors, [])
        finally:
            worker.stop.set()
            thread.join(timeout=4)
            client.close()


if __name__ == "__main__":
    unittest.main()
