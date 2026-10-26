"""An actual signed demo upload can reach APPROVED; altered evidence cannot."""

import json
import io
import os
import tempfile
import unittest
from datetime import date, timedelta
from pathlib import Path
from unittest.mock import patch

from PIL import Image

from worker.backend_protocol import completion_events
from worker.services.demo_attestation import create_demo_png
from worker.services.document_readers import ExtractedText
from worker.tests.test_backend_protocol import submission


KEY = "test-operator-secret-for-demo-fixtures-2026"
DIRECTOR = "Смирнова Ольга Ивановна"


def fields_for(directory: Path, *, signer_key: str = KEY, consent: bool = True,
               signed_inn: str = "7700000002", signed_name: str = DIRECTOR,
               signed_application_id: str | None = None,
               issued_on: date | None = None) -> dict[str, str]:
    fields = submission(
        form=json.dumps({"applicant": {"position": "Директор", "has_collateral": False},
                         "consents": {"bki_request": consent}}),
        company=json.dumps({"org_type": "OOO", "okved": "41.20", "age_months": 60,
                            "registry": {"status": "ACTIVE", "director": DIRECTOR},
                            "credit_history": {"other_banks": [{"debt": 2400000}]}}),
    )
    documents = []
    for kind in ("passport", "bki_consent"):
        filename = kind + ".png"
        (directory / filename).write_bytes(create_demo_png(
            kind, signed_application_id or fields["application_id"], signed_inn, signed_name,
            signer_key, issued_on))
        documents.append({"kind": kind, "name": filename, "path": filename})
    fields["documents"] = json.dumps(documents)
    return fields


class DemoApprovalTests(unittest.TestCase):
    def test_signed_files_produce_real_approval_events(self):
        with tempfile.TemporaryDirectory() as directory:
            fields = fields_for(Path(directory))
            with patch.dict(os.environ, {"WORKER_DOCUMENT_ROOT": directory,
                                      "WORKER_DEMO_ATTESTATION_KEY": KEY}):
                events = completion_events(fields)
        self.assertEqual([(event["from"], event["to"]) for event in events], [
            ("DATA_CHECK", "SCORING"), ("SCORING", "APPROVED")])
        decision = json.loads(events[-1]["decision"])
        self.assertEqual(decision["evidence_mode"], "synthetic_demo")
        self.assertGreaterEqual(decision["score"], 75)
        self.assertTrue(all(item["verification_status"] == "VERIFIED"
                            for item in decision["documents"]))
        self.assertTrue(all(item["verification_method"] == "signed_demo_fixture"
                            for item in decision["documents"]))
        self.assertTrue(all(item["id"] for item in decision["documents"]))
        self.assertTrue(all("document_text" not in item["extracted_fields"]
                            for item in decision["documents"]))

    def test_wrong_key_subject_date_or_consent_forces_manual_review(self):
        cases = [
            {"signer_key": "wrong-test-operator-secret-for-demo-2026"},
            {"signed_inn": "7700000001"},
            {"signed_application_id": "11111111-1111-4111-8111-111111111111"},
            {"signed_name": "Чужой руководитель"},
            {"issued_on": date.today() - timedelta(days=181)},
            {"consent": False},
        ]
        for changes in cases:
            with self.subTest(changes=changes), tempfile.TemporaryDirectory() as directory:
                fields = fields_for(Path(directory), **changes)
                with patch.dict(os.environ, {"WORKER_DOCUMENT_ROOT": directory,
                                          "WORKER_DEMO_ATTESTATION_KEY": KEY}), \
                        patch("worker.services.document_parser.extract_text",
                              return_value=ExtractedText("DEMO TEST DOCUMENT", "image_ocr", False)):
                    events = completion_events(fields)
                self.assertEqual(events[-1]["to"], "MANUAL_REVIEW")
                self.assertNotIn("_demo_attestation", events[-1].get("decision", ""))

    def test_no_operator_key_forces_manual_review(self):
        with tempfile.TemporaryDirectory() as directory:
            fields = fields_for(Path(directory))
            with patch.dict(os.environ, {"WORKER_DOCUMENT_ROOT": directory,
                                      "WORKER_DEMO_ATTESTATION_KEY": ""}), \
                    patch("worker.services.document_parser.extract_text",
                          return_value=ExtractedText("DEMO TEST DOCUMENT", "image_ocr", False)):
                events = completion_events(fields)
        self.assertEqual(events[-1]["to"], "MANUAL_REVIEW")

    def test_plain_uploaded_images_are_not_verified(self):
        with tempfile.TemporaryDirectory() as directory:
            fields = fields_for(Path(directory))
            image = Image.new("RGB", (64, 32), "white")
            output = io.BytesIO()
            image.save(output, format="PNG")
            for kind in ("passport", "bki_consent"):
                Path(directory, kind + ".png").write_bytes(output.getvalue())
            with patch.dict(os.environ, {"WORKER_DOCUMENT_ROOT": directory,
                                      "WORKER_DEMO_ATTESTATION_KEY": KEY}), \
                    patch("worker.services.document_parser.extract_text",
                          return_value=ExtractedText("DEMO TEST DOCUMENT", "image_ocr", False)):
                events = completion_events(fields)
        self.assertEqual(events[-1]["to"], "MANUAL_REVIEW")


if __name__ == "__main__":
    unittest.main()
