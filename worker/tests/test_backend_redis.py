"""Optional end-to-end test against an isolated Redis instance."""

import json
import os
import threading
import time
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
        client.xgroup_create(RESULT_STREAM, "api", id="0", mkstream=True)
        api_events = []
        api_stop = threading.Event()
        first_ack_allowed = threading.Event()

        def consume_results():
            while not api_stop.is_set():
                batches = client.xreadgroup("api", "test-api", {RESULT_STREAM: ">"},
                                            count=1, block=1000)
                for _, messages in batches:
                    for message_id, event in messages:
                        api_events.append(event)
                        if event["to"] == "DATA_CHECK":
                            first_ack_allowed.wait(timeout=3)
                        client.xack(RESULT_STREAM, "api", message_id)

        def run_worker():
            try:
                worker.run()
            except Exception as exc:  # surfaced in the test thread
                errors.append(exc)

        thread = threading.Thread(target=run_worker, daemon=True)
        api_thread = threading.Thread(target=consume_results, daemon=True)
        api_thread.start()
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
            first = client.xread({RESULT_STREAM: "0-0"}, count=1, block=5000)
            self.assertTrue(first, f"worker stopped: {errors}")
            first_id, first_event = first[0][1][0]
            self.assertEqual(first_event["to"], "DATA_CHECK")
            self.assertFalse(client.xread({RESULT_STREAM: first_id}, count=1, block=200),
                             "worker published the next status before API acknowledged the first")
            first_ack_allowed.set()
            second = client.xread({RESULT_STREAM: first_id}, count=1, block=5000)
            self.assertTrue(second, f"worker stopped: {errors}")
            events = [first_event, second[0][1][0][1]]
            self.assertEqual([(event["from"], event["to"]) for event in events], [
                ("SUBMITTED", "DATA_CHECK"), ("DATA_CHECK", "INFO_REQUESTED"),
            ])
            self.assertEqual({event["application_id"] for event in events}, {fields["application_id"]})
            self.assertTrue(all(event["event_id"] for event in events))
            deadline = time.monotonic() + 2
            while len(api_events) < 2 and time.monotonic() < deadline:
                time.sleep(0.01)
            self.assertEqual([(event["from"], event["to"]) for event in api_events], [
                ("SUBMITTED", "DATA_CHECK"), ("DATA_CHECK", "INFO_REQUESTED"),
            ])
            self.assertEqual(errors, [])
        finally:
            first_ack_allowed.set()
            worker.stop.set()
            api_stop.set()
            thread.join(timeout=4)
            api_thread.join(timeout=4)
            client.close()


if __name__ == "__main__":
    unittest.main()
