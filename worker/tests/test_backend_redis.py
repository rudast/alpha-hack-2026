"""Optional end-to-end test against an isolated Redis instance."""

import json
import os
import threading
import time
import unittest
from unittest.mock import patch
from uuid import uuid4

import redis

import worker.backend_protocol as protocol
from worker.backend_protocol import (APPLICATION_STREAM, FAST_GROUP, RESULT_STREAM,
                                     ROUTED_HASH, BackendWorker)
from worker.tests.test_backend_protocol import submission


@unittest.skipUnless(os.getenv("REDIS_TEST_ADDR"), "set REDIS_TEST_ADDR for Redis integration")
class BackendRedisTests(unittest.TestCase):
    def test_pending_application_recovers_after_worker_restart(self):
        client = redis.Redis.from_url("redis://" + os.environ["REDIS_TEST_ADDR"], decode_responses=True)
        suffix = uuid4().hex
        streams = {
            "APPLICATION_STREAM": "test:applications:" + suffix,
            "RESULT_STREAM": "test:results:" + suffix,
            "SLOW_STREAM": "test:slow:" + suffix,
            "HISTORY_STREAM": "test:history:" + suffix,
            "ROUTED_HASH": "test:routed:" + suffix,
            "FAST_GROUP": "test-fast-" + suffix,
            "SLOW_GROUP": "test-slow-" + suffix,
            "RESULT_GROUP": "test-api-" + suffix,
        }
        try:
            with patch.multiple(protocol, **streams):
                first_worker = BackendWorker(client)
                first_worker._create_groups()
                client.xgroup_create(streams["RESULT_STREAM"], streams["RESULT_GROUP"],
                                     id="0", mkstream=True)
                fields = submission()
                original_id = client.xadd(streams["APPLICATION_STREAM"], fields)
                incoming = client.xreadgroup(streams["FAST_GROUP"], "first-fast",
                                             {streams["APPLICATION_STREAM"]: ">"}, count=1)
                first_worker._application(*incoming[0][1][0])
                self.assertEqual(client.xlen(streams["SLOW_STREAM"]), 1)
                self.assertTrue(client.xpending_range(streams["APPLICATION_STREAM"],
                                                      streams["FAST_GROUP"], original_id, original_id, 1))

                # Another process claims the original pending request. Its
                # durable slow route already exists, so it must not duplicate it.
                restarted = BackendWorker(client)
                _, reclaimed, _ = client.xautoclaim(streams["APPLICATION_STREAM"],
                    streams["FAST_GROUP"], "restarted-fast", min_idle_time=0,
                    start_id="0-0", count=10)
                self.assertEqual(len(reclaimed), 1)
                restarted._application(*reclaimed[0])
                self.assertEqual(client.xlen(streams["SLOW_STREAM"]), 1)

                first_result_id, _ = client.xreadgroup(streams["RESULT_GROUP"], "api",
                    {streams["RESULT_STREAM"]: ">"}, count=1)[0][1][0]
                client.xack(streams["RESULT_STREAM"], streams["RESULT_GROUP"], first_result_id)

                # The old process had taken the slow task and stopped. Reclaim
                # it on restart, publish the final event and ACK both queues.
                client.xreadgroup(streams["SLOW_GROUP"], "first-slow",
                                  {streams["SLOW_STREAM"]: ">"}, count=1)
                _, slow_reclaimed, _ = client.xautoclaim(streams["SLOW_STREAM"],
                    streams["SLOW_GROUP"], "restarted-slow", min_idle_time=0,
                    start_id="0-0", count=10)
                self.assertEqual(len(slow_reclaimed), 1)
                restarted._slow(*slow_reclaimed[0])
                self.assertFalse(client.xpending_range(streams["APPLICATION_STREAM"],
                                                       streams["FAST_GROUP"], original_id, original_id, 1))
                self.assertEqual(client.xpending(streams["SLOW_STREAM"], streams["SLOW_GROUP"])["pending"], 0)
                self.assertFalse(client.hexists(streams["ROUTED_HASH"], original_id))
                results = client.xrange(streams["RESULT_STREAM"])
                self.assertEqual([(event["from"], event["to"]) for _, event in results], [
                    ("SUBMITTED", "DATA_CHECK"), ("DATA_CHECK", "INFO_REQUESTED"),
                ])
        finally:
            client.delete(streams["APPLICATION_STREAM"], streams["RESULT_STREAM"],
                          streams["SLOW_STREAM"], streams["HISTORY_STREAM"],
                          streams["ROUTED_HASH"])
            client.close()

    def test_history_and_application_exchange(self):
        client = redis.Redis.from_url("redis://" + os.environ["REDIS_TEST_ADDR"], decode_responses=True)
        client.ping()
        suffix = uuid4().hex
        streams = {
            "APPLICATION_STREAM": "test:applications:" + suffix,
            "RESULT_STREAM": "test:results:" + suffix,
            "SLOW_STREAM": "test:slow:" + suffix,
            "HISTORY_STREAM": "test:history:" + suffix,
            "ROUTED_HASH": "test:routed:" + suffix,
            "FAST_GROUP": "test-fast-" + suffix,
            "SLOW_GROUP": "test-slow-" + suffix,
            "RESULT_GROUP": "test-api-" + suffix,
        }
        patches = patch.multiple(protocol, **streams)
        patches.start()
        application_stream = streams["APPLICATION_STREAM"]
        result_stream = streams["RESULT_STREAM"]
        fast_group = streams["FAST_GROUP"]
        result_group = streams["RESULT_GROUP"]
        worker = BackendWorker(client)
        errors = []
        client.xgroup_create(result_stream, result_group, id="0", mkstream=True)
        api_events = []
        api_stop = threading.Event()
        first_ack_allowed = threading.Event()

        def consume_results():
            while not api_stop.is_set():
                batches = client.xreadgroup(result_group, "test-api", {result_stream: ">"},
                                            count=1, block=1000)
                for _, messages in batches:
                    for message_id, event in messages:
                        api_events.append(event)
                        if event["to"] == "DATA_CHECK":
                            first_ack_allowed.wait(timeout=3)
                        client.xack(result_stream, result_group, message_id)

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
            client.xadd(streams["HISTORY_STREAM"], {"request_id": history_id,
                                                     "inn": "7700000002"})
            reply = client.blpop(protocol.HISTORY_REPLY_PREFIX + history_id, timeout=5)
            self.assertIsNotNone(reply)
            self.assertEqual(json.loads(reply[1]), {
                "equity": 4_000_000,
                "other_banks": [{"bank": "ВТБ", "kind": "Инвестиционный кредит", "debt": 2_400_000}],
            })

            fields = submission()
            request_id = client.xadd(application_stream, fields)
            first = client.xread({result_stream: "0-0"}, count=1, block=5000)
            self.assertTrue(first, f"worker stopped: {errors}")
            first_id, first_event = first[0][1][0]
            self.assertEqual(first_event["to"], "DATA_CHECK")
            self.assertTrue(client.xpending_range(application_stream, fast_group,
                                                  request_id, request_id, 1),
                            "original request must remain pending during scoring")
            self.assertFalse(client.xread({result_stream: first_id}, count=1, block=200),
                             "worker published the next status before API acknowledged the first")
            first_ack_allowed.set()
            second = client.xread({result_stream: first_id}, count=1, block=5000)
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
            deadline = time.monotonic() + 2
            while client.xpending_range(application_stream, fast_group,
                                        request_id, request_id, 1) and time.monotonic() < deadline:
                time.sleep(0.01)
            self.assertFalse(client.xpending_range(application_stream, fast_group,
                                                   request_id, request_id, 1))
            self.assertFalse(client.hexists(streams["ROUTED_HASH"], request_id))
            self.assertEqual(errors, [])
        finally:
            first_ack_allowed.set()
            worker.stop.set()
            api_stop.set()
            thread.join(timeout=4)
            api_thread.join(timeout=4)
            client.delete(streams["APPLICATION_STREAM"], streams["RESULT_STREAM"],
                          streams["SLOW_STREAM"], streams["HISTORY_STREAM"],
                          streams["ROUTED_HASH"])
            patches.stop()
            client.close()


if __name__ == "__main__":
    unittest.main()
