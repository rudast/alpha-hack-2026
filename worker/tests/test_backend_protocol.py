"""Compatibility checks for the backend branch Redis contract."""

import json
import unittest
from uuid import uuid4

from worker.backend_protocol import (
    APPLICATION_STREAM, FAST_GROUP, HISTORY_REPLY_PREFIX, HISTORY_STREAM,
    RESULT_STREAM, SLOW_GROUP, SLOW_STREAM, BackendWorker, completion_events,
)


class FakePipeline:
    def __init__(self):
        self.calls = []

    def xadd(self, *args):
        self.calls.append(("xadd", *args))
        return self

    def xack(self, *args):
        self.calls.append(("xack", *args))
        return self

    def lpush(self, *args):
        self.calls.append(("lpush", *args))
        return self

    def expire(self, *args):
        self.calls.append(("expire", *args))
        return self

    def execute(self):
        self.calls.append(("execute",))


class FakeRedis:
    def __init__(self):
        self.pipelines = []
        self.acks = []

    def pipeline(self, transaction=True):
        assert transaction
        pipe = FakePipeline()
        self.pipelines.append(pipe)
        return pipe

    def xack(self, *args):
        self.acks.append(args)


def submission(**extra):
    fields = {
        "application_id": str(uuid4()), "inn": "7700000002",
        "product_id": "express", "amount": "500000", "term_months": "6",
        "form": '{"applicant":{"position":"Директор"}}',
        "company": '{"org_type":"OOO","registry":{},"financials":{}}',
        "documents": "[]", "debt": "2400000", "equity": "4000000",
    }
    fields.update(extra)
    return fields


class BackendProtocolTests(unittest.TestCase):
    def test_credit_history_reply_uses_backend_list_and_ack(self):
        client = FakeRedis()
        BackendWorker(client)._history("1-0", {"request_id": "req", "inn": "7700000002"})
        calls = client.pipelines[0].calls
        self.assertEqual(calls[0][0:2], ("lpush", HISTORY_REPLY_PREFIX + "req"))
        self.assertEqual(json.loads(calls[0][2]), {"debt": 2400000, "equity": 4000000})
        self.assertEqual(calls[-2], ("xack", HISTORY_STREAM, FAST_GROUP, "1-0"))

    def test_submission_is_routed_to_slow_pool_and_emits_flat_events(self):
        client = FakeRedis()
        worker = BackendWorker(client)
        fields = submission(documents=json.dumps([
            {"id": "p", "kind": "passport", "filename": "p.pdf"},
            {"id": "c", "kind": "bki_consent", "filename": "c.pdf"},
        ]))
        worker._application("1-0", fields)
        first = client.pipelines[0].calls
        self.assertEqual(first[0][1], RESULT_STREAM)
        self.assertEqual(first[0][2]["from"], "SUBMITTED")
        self.assertEqual(first[0][2]["to"], "DATA_CHECK")
        self.assertIn(":1-0:DATA_CHECK", first[0][2]["event_id"])
        self.assertEqual(first[1][1], SLOW_STREAM)
        self.assertEqual(first[2], ("xack", APPLICATION_STREAM, FAST_GROUP, "1-0"))

        worker._slow("2-0", first[1][2])
        calls = client.pipelines[1].calls
        events = [call[2] for call in calls if call[0] == "xadd"]
        self.assertEqual([(e["from"], e["to"]) for e in events], [
            ("DATA_CHECK", "SCORING"), ("SCORING", "MANUAL_REVIEW"),
        ])
        self.assertTrue(all(":1-0:" in event["event_id"] for event in events))
        decision = json.loads(events[-1]["decision"])
        self.assertEqual(decision["documents"][0]["status"], "CONTENT_UNAVAILABLE")
        self.assertEqual(calls[-2], ("xack", SLOW_STREAM, SLOW_GROUP, "2-0"))

    def test_missing_consent_is_requested_in_comment_and_decision(self):
        events = completion_events(submission())
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["to"], "INFO_REQUESTED")
        self.assertIn("documents.bki_consent", events[0]["comment"])
        self.assertEqual(json.loads(events[0]["info_request"])["missing_fields"], [
            "documents.passport", "documents.bki_consent",
        ])

    def test_available_document_content_is_parsed_without_database(self):
        fields = submission(documents=json.dumps([
            {"id": "p", "kind": "passport", "filename": "p.pdf"},
            {"id": "c", "kind": "bki_consent", "filename": "c.pdf"},
            {"id": "f", "kind": "financial_statements", "filename": "f.json",
             "media_type": "application/json", "content_text": json.dumps({
                 "period": "2025", "revenue": 100, "net_profit": 10,
                 "equity": 50, "liabilities": 20,
             })},
        ]))
        decision = json.loads(completion_events(fields)[-1]["decision"])
        financial = next(item for item in decision["documents"] if item["id"] == "f")
        self.assertEqual(financial["status"], "PARSED")
        self.assertEqual(financial["extracted_fields"]["revenue"]["value"], 100)
        self.assertEqual(financial["extracted_fields"]["revenue"]["period"], "2025")

    def test_invalid_company_json_requests_data_instead_of_crashing(self):
        events = completion_events(submission(company="{"))
        self.assertEqual(events[0]["to"], "INFO_REQUESTED")
        self.assertEqual(json.loads(events[0]["decision"])["missing_fields"], ["application_data"])


if __name__ == "__main__":
    unittest.main()
