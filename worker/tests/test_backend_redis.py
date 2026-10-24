"""Optional end-to-end test against an isolated Redis instance."""

import json
import os
import threading
import unittest
from uuid import uuid4

import redis

from worker.backend_protocol import BackendWorker, RESULT_STREAM
from worker.tests.test_backend_protocol import submission


@unittest.skipUnless(os.getenv("REDIS_TEST_ADDR"), "set REDIS_TEST_ADDR for Redis integration")
class BackendRedisTests(unittest.TestCase):
    def test_history_and_application_exchange(self):
        client = redis.Redis.from_url("redis://" + os.environ["REDIS_TEST_ADDR"], decode_responses=True)
        client.ping()
        worker = BackendWorker(client)
        errors = []

        def run_worker():
            try:
                worker.run()
            except Exception as exc:  # surfaced in the test thread
                errors.append(exc)

        thread = threading.Thread(target=run_worker, daemon=True)
        thread.start()
        try:
            history_id = "test-" + uuid4().hex
            client.xadd("credit-history:requests", {"request_id": history_id,
                                                     "inn": "7700000002"})
            reply = client.blpop("credit-history:reply:" + history_id, timeout=5)
            self.assertIsNotNone(reply)
            self.assertEqual(json.loads(reply[1])["debt"], 2400000)

            fields = submission()
            client.xadd("applications:submitted", fields)
            events = []
            last_id = "0-0"
            while len(events) < 2:
                batches = client.xread({RESULT_STREAM: last_id}, count=2, block=5000)
                self.assertTrue(batches, f"worker stopped: {errors}")
                for _, messages in batches:
                    for message_id, event in messages:
                        last_id = message_id
                        events.append(event)
            self.assertEqual([(event["from"], event["to"]) for event in events], [
                ("SUBMITTED", "DATA_CHECK"), ("DATA_CHECK", "INFO_REQUESTED"),
            ])
            self.assertEqual({event["application_id"] for event in events}, {fields["application_id"]})
            self.assertTrue(all(event["event_id"] for event in events))
            self.assertEqual(errors, [])
        finally:
            worker.stop.set()
            thread.join(timeout=4)
            client.close()


if __name__ == "__main__":
    unittest.main()
