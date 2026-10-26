"""Compatibility checks for the backend branch Redis contract."""

import json
import os
import tempfile
import time
import unittest
from pathlib import Path
from unittest.mock import patch
from uuid import UUID, uuid4

from worker.backend_protocol import (
    APPLICATION_STREAM, FAST_GROUP, HISTORY_REPLY_PREFIX, HISTORY_STREAM,
    RESULT_STREAM, ROUTED_HASH, SLOW_GROUP, SLOW_STREAM, BackendWorker,
    completion_events, inspect_application, result_event,
)
from worker.services.scoring_sources import bureau_profile


class FakePipeline:
    def __init__(self, client):
        self.client = client
        self.calls = []

    def xadd(self, *args):
        self.calls.append(("xadd", *args))
        return self

    def xack(self, *args):
        self.calls.append(("xack", *args))
        return self

    def hset(self, *args):
        self.calls.append(("hset", *args))
        return self

    def hdel(self, *args):
        self.calls.append(("hdel", *args))
        return self

    def lpush(self, *args):
        self.calls.append(("lpush", *args))
        return self

    def expire(self, *args):
        self.calls.append(("expire", *args))
        return self

    def execute(self):
        for call in self.calls:
            if call[0] == "xack":
                self.client.xack(*call[1:])
            elif call[0] == "hset":
                self.client.routed[call[2]] = call[3]
            elif call[0] == "hdel":
                self.client.routed.pop(call[2], None)
        self.calls.append(("execute",))


class FakeRedis:
    def __init__(self):
        self.pipelines = []
        self.acks = []
        self.added = []
        self.routed = {}
        self.claims = []

    def pipeline(self, transaction=True):
        assert transaction
        pipe = FakePipeline(self)
        self.pipelines.append(pipe)
        return pipe

    def xack(self, *args):
        self.acks.append(args)

    def xadd(self, stream, fields):
        self.added.append((stream, fields))
        return f"{len(self.added)}-0"

    def hexists(self, key, field):
        assert key == ROUTED_HASH
        return field in self.routed

    def xclaim(self, *args, **kwargs):
        self.claims.append((args, kwargs))
        return []

    def xinfo_groups(self, stream):
        assert stream == RESULT_STREAM
        return [{"name": "api", "last-delivered-id": "999-0"}]

    def xpending_range(self, *args):
        return []


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
    def test_backend_name_and_path_provide_stable_document_provenance(self):
        with tempfile.TemporaryDirectory() as directory:
            Path(directory, "statement.csv").write_text(
                "date,credit,debit,balance\n2025-01-02,100,0,1100\n", encoding="utf-8")
            fields = submission(documents=json.dumps([
                {"kind": "account_statement", "name": "statement.csv", "path": "statement.csv"},
            ]))
            with patch.dict(os.environ, {"WORKER_DOCUMENT_ROOT": directory}):
                _, first = inspect_application(fields)
                _, second = inspect_application(fields)
            document = first["documents"][0]
            self.assertEqual(document["filename"], "statement.csv")
            self.assertEqual(document["status"], "PARSED")
            self.assertEqual(document["id"], second["documents"][0]["id"])
            self.assertEqual(document["extracted_fields"]["turnover_monthly"]["source_document_id"], document["id"])

    def test_demo_bureau_fills_partial_backend_seed_without_overwriting_it(self):
        profile = bureau_profile("7700000002", {"other_banks": [{"debt": 2400000}],
                                                  "current_overdue_days": 12})
        self.assertEqual(profile["current_overdue_days"], 12)
        self.assertEqual(profile["history_months"], 24)
        self.assertEqual(profile["other_banks"], [{"debt": 2400000}])
        self.assertIsNone(bureau_profile("1234567890", {"other_banks": []}).get("history_months"))

    def test_slow_task_renews_ownership_during_long_processing(self):
        client = FakeRedis()
        worker = BackendWorker(client)
        with patch("worker.backend_protocol.SLOW_HEARTBEAT_SECONDS", 0.01), \
                patch.object(worker, "_slow_task", side_effect=lambda *_: time.sleep(0.04)):
            worker._slow("slow-1", {})
        self.assertGreaterEqual(len(client.claims), 1)
        self.assertEqual(client.claims[0][1]["message_ids"], ["slow-1"])

    def test_event_id_is_stable_uuid_and_result_has_no_nul(self):
        application_id = str(uuid4())
        event = result_event(application_id, "SCORING", "MANUAL_REVIEW", "ошибка\x00данных",
                             {"outcome": "MANUAL_REVIEW", "reasons": ["неверный\x00файл"]},
                             "загрузите\x00файл", source_id="1-0")
        UUID(event["event_id"])
        self.assertEqual(event, result_event(application_id, "SCORING", "MANUAL_REVIEW",
                                             "ошибка\x00данных",
                                             {"outcome": "MANUAL_REVIEW", "reasons": ["неверный\x00файл"]},
                                             "загрузите\x00файл", source_id="1-0"))
        self.assertNotIn("\\u0000", event["decision"])
        self.assertNotIn("\x00", event["info_request"])
        self.assertNotIn("\x00", event["comment"])

    def test_credit_history_reply_uses_backend_list_and_ack(self):
        client = FakeRedis()
        BackendWorker(client)._history("1-0", {"request_id": "req", "inn": "7700000002"})
        calls = client.pipelines[0].calls
        self.assertEqual(calls[0][0:2], ("lpush", HISTORY_REPLY_PREFIX + "req"))
        self.assertEqual(json.loads(calls[0][2]), {
            "equity": 4_000_000,
            "other_banks": [{"bank": "ВТБ", "kind": "Инвестиционный кредит", "debt": 2_400_000}],
        })
        self.assertEqual(calls[-2], ("xack", HISTORY_STREAM, FAST_GROUP, "1-0"))

    def test_submission_is_routed_to_slow_pool_and_emits_flat_events(self):
        client = FakeRedis()
        worker = BackendWorker(client)
        fields = submission(company=json.dumps({
            "org_type": "OOO", "okved": "47.19", "age_months": 60,
            "registry": {"status": "ACTIVE"},
        }), documents=json.dumps([
            {"id": "p", "kind": "passport", "filename": "p.pdf"},
            {"id": "c", "kind": "bki_consent", "filename": "c.pdf"},
        ]))
        worker._application("1-0", fields)
        first = client.pipelines[0].calls
        self.assertEqual(client.added[0][0], RESULT_STREAM)
        self.assertEqual(client.added[0][1]["from"], "SUBMITTED")
        self.assertEqual(client.added[0][1]["to"], "DATA_CHECK")
        UUID(client.added[0][1]["event_id"])
        self.assertEqual(first[0][1], SLOW_STREAM)
        self.assertEqual(first[0][2]["after_result_id"], "1-0")
        self.assertEqual(first[0][2]["original_message_id"], "1-0")
        self.assertEqual(first[1], ("hset", ROUTED_HASH, "1-0", "1"))
        self.assertEqual(client.acks, [])
        self.assertTrue(client.hexists(ROUTED_HASH, "1-0"))

        worker._application("1-0", fields)
        self.assertEqual(len(client.added), 1, "reclaimed request must not route twice")

        worker._slow("2-0", first[0][2])
        events = [event for stream, event in client.added[1:] if stream == RESULT_STREAM]
        self.assertEqual([(e["from"], e["to"]) for e in events], [
            ("DATA_CHECK", "SCORING"), ("SCORING", "MANUAL_REVIEW"),
        ])
        self.assertTrue(all(UUID(event["event_id"]) for event in events))
        decision = json.loads(events[-1]["decision"])
        self.assertEqual(decision["documents"][0]["status"], "CONTENT_UNAVAILABLE")
        self.assertEqual(decision["outcome"], "MANUAL_REVIEW")
        self.assertIn((APPLICATION_STREAM, FAST_GROUP, "1-0"), client.acks)
        self.assertEqual(client.acks[-1], (SLOW_STREAM, SLOW_GROUP, "2-0"))
        self.assertFalse(client.hexists(ROUTED_HASH, "1-0"))

    def test_complete_verified_express_application_can_be_approved(self):
        fields = submission(company=json.dumps({
            "org_type": "OOO", "okved": "47.19", "age_months": 60,
            "registry": {"status": "ACTIVE"},
        }), documents=json.dumps([
            {"id": "p", "kind": "passport", "filename": "p.pdf", "verification_status": "VERIFIED"},
            {"id": "c", "kind": "bki_consent", "filename": "c.pdf", "verification_status": "VERIFIED",
             "subject_inn": "7700000002"},
        ]))
        events = completion_events(fields)
        self.assertEqual([(event["from"], event["to"]) for event in events], [
            ("DATA_CHECK", "SCORING"), ("SCORING", "APPROVED"),
        ])
        decision = json.loads(events[-1]["decision"])
        self.assertEqual(decision["outcome"], "APPROVED")
        self.assertTrue(all("value" in factor for factor in decision["factors"]))

    def test_missing_scoring_input_requests_data(self):
        fields = submission(documents=json.dumps([
            {"id": "p", "kind": "passport", "verification_status": "VERIFIED"},
            {"id": "c", "kind": "bki_consent", "verification_status": "VERIFIED",
             "subject_inn": "7700000002"},
        ]))
        events = completion_events(fields)
        self.assertEqual(events[0]["to"], "INFO_REQUESTED")
        self.assertIn("age_months", json.loads(events[0]["decision"])["missing_fields"])
        self.assertIn("дату регистрации бизнеса", events[0]["info_request"])

    def test_product_documents_are_requested_before_scoring(self):
        fields = submission(product_id="working_capital", documents=json.dumps([
            {"id": "p", "kind": "passport", "verification_status": "VERIFIED"},
            {"id": "c", "kind": "bki_consent", "verification_status": "VERIFIED",
             "subject_inn": "7700000002"},
        ]))
        events = completion_events(fields)
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["to"], "INFO_REQUESTED")
        self.assertEqual(json.loads(events[0]["decision"])["missing_fields"], [
            "documents.financial_statements",
        ])
        self.assertIn("бухгалтерскую отчётность", events[0]["info_request"])

    def test_secured_product_requests_its_catalog_documents(self):
        events = completion_events(submission(product_id="mortgage_business"))
        self.assertEqual(json.loads(events[0]["decision"])["missing_fields"], [
            "documents.passport", "documents.bki_consent", "documents.title_documents",
            "documents.preliminary_purchase_contract", "documents.egrn_extract",
            "documents.valuation_report",
        ])

    def test_consent_for_another_subject_never_auto_approves(self):
        fields = submission(company=json.dumps({
            "org_type": "OOO", "okved": "47.19", "age_months": 60,
            "registry": {"status": "ACTIVE"},
        }), documents=json.dumps([
            {"id": "p", "kind": "passport", "verification_status": "VERIFIED"},
            {"id": "c", "kind": "bki_consent", "verification_status": "VERIFIED",
             "subject_inn": "7700000001"},
        ]))
        decision = json.loads(completion_events(fields)[-1]["decision"])
        self.assertEqual(decision["outcome"], "MANUAL_REVIEW")

    def test_documents_without_unique_ids_are_not_treated_as_verified(self):
        company = json.dumps({"org_type": "OOO", "okved": "47.19", "age_months": 60,
                              "registry": {"status": "ACTIVE"}})
        for id_value in (None, "same"):
            with self.subTest(id_value=id_value):
                docs = [{"kind": "passport", "verification_status": "VERIFIED"},
                        {"kind": "bki_consent", "verification_status": "VERIFIED",
                         "subject_inn": "7700000002"}]
                if id_value is not None:
                    for doc in docs:
                        doc["id"] = id_value
                decision = json.loads(completion_events(submission(
                    company=company, documents=json.dumps(docs)))[-1]["decision"])
                self.assertEqual(decision["outcome"], "MANUAL_REVIEW")

    def test_one_month_statement_is_enough_for_loan_up_to_1m(self):
        statement = "date,credit,debit,balance\n2025-01-02,2500000,0,3500000\n"
        fields = submission(product_id="loan_up_to_1m", amount="500000", term_months="12",
            company=json.dumps({"org_type": "OOO", "okved": "47.19", "age_months": 60,
                                "registry": {"status": "ACTIVE"}}),
            documents=json.dumps([
                {"id": "p", "kind": "passport", "verification_status": "VERIFIED"},
                {"id": "c", "kind": "bki_consent", "verification_status": "VERIFIED",
                 "subject_inn": "7700000002"},
                {"id": "s", "kind": "account_statement", "media_type": "text/csv",
                 "content_text": statement, "verification_status": "VERIFIED"},
            ]))
        events = completion_events(fields)
        self.assertEqual(events[-1]["to"], "APPROVED")
        decision = json.loads(events[-1]["decision"])
        self.assertGreater(next(f for f in decision["factors"] if f["code"] == "P1")["r"], 0)

    def test_working_capital_uses_parsed_revenue_but_requires_verification(self):
        fields = submission(product_id="working_capital", amount="3000000", term_months="12",
            company=json.dumps({"org_type": "OOO", "okved": "47.19", "age_months": 48,
                                "registry": {"status": "ACTIVE"}}),
            documents=json.dumps([
                {"id": "p", "kind": "passport", "verification_status": "VERIFIED"},
                {"id": "c", "kind": "bki_consent", "verification_status": "VERIFIED",
                 "subject_inn": "7700000002"},
                {"id": "f", "kind": "financial_statements", "media_type": "application/json",
                 "content_text": json.dumps({"period": "2025", "revenue": 30_000_000,
                                             "net_profit": 2_000_000, "equity": 4_000_000,
                                             "liabilities": 2_400_000})},
            ]))
        events = completion_events(fields)
        self.assertEqual([(item["from"], item["to"]) for item in events], [
            ("DATA_CHECK", "SCORING"), ("SCORING", "MANUAL_REVIEW"),
        ])
        decision = json.loads(events[-1]["decision"])
        self.assertEqual(decision["model_version"], "scoring-1")
        self.assertIn("FINANCIAL_STATEMENTS_UNVERIFIED", decision["review_reasons"])
        self.assertEqual(next(f for f in decision["factors"] if f["code"] == "P1")["r"], 0.7)

    def test_missing_consent_is_requested_in_comment_and_decision(self):
        events = completion_events(submission())
        self.assertEqual(len(events), 1)
        self.assertEqual(events[0]["to"], "INFO_REQUESTED")
        self.assertIn("согласие на запрос в БКИ", events[0]["comment"])
        self.assertEqual(json.loads(events[0]["decision"])["missing_fields"], [
            "documents.passport", "documents.bki_consent",
        ])
        self.assertEqual(events[0]["info_request"], events[0]["comment"])
        self.assertIn("паспорт", events[0]["comment"])

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

    def test_older_pending_slow_task_can_complete(self):
        client = FakeRedis()
        fields = submission()
        BackendWorker(client)._slow("old-1", {"task": json.dumps(fields)})
        self.assertEqual(client.added[0][1]["to"], "INFO_REQUESTED")
        self.assertEqual(client.acks[-1], (SLOW_STREAM, SLOW_GROUP, "old-1"))

    def test_corrupt_slow_task_requests_review(self):
        client = FakeRedis()
        BackendWorker(client)._slow("bad-1", {
            "task": "{", "application_id": str(uuid4()), "after_result_id": "1-0",
        })
        self.assertEqual(client.added[0][1]["to"], "INFO_REQUESTED")
        self.assertEqual(client.acks[-1], (SLOW_STREAM, SLOW_GROUP, "bad-1"))


if __name__ == "__main__":
    unittest.main()
