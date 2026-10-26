"""Files supplied by the API must stay inside the read-only document mount."""

import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from worker.services.document_parser import parse_documents


def parse(path: str) -> dict:
    return parse_documents({"application_id": "application-1", "documents": [{
        "id": "statement-1", "kind": "account_statement", "path": path,
    }]})["documents"][0]


class DocumentPathTests(unittest.TestCase):
    def test_relative_and_absolute_paths_inside_mount_are_parsed(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "statement.csv").write_text(
                "date,credit,debit,balance\n2025-01-02,100,0,1100\n", encoding="utf-8")
            with patch.dict(os.environ, {"WORKER_DOCUMENT_ROOT": directory}):
                for path in ("statement.csv", str(root / "statement.csv")):
                    with self.subTest(path=path):
                        item = parse(path)
                        self.assertEqual(item["status"], "PARSED")
                        self.assertEqual(item["extracted_fields"]["turnover_monthly"]["value"][0]["credit"], "100.00")

    def test_parent_traversal_and_symlink_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "statement.csv").write_text(
                "date,credit,debit,balance\n2025-01-02,100,0,1100\n", encoding="utf-8")
            (root / "link.csv").symlink_to(root / "statement.csv")
            with patch.dict(os.environ, {"WORKER_DOCUMENT_ROOT": directory}):
                for path in ("../statement.csv", "/tmp/outside.csv", "link.csv"):
                    with self.subTest(path=path):
                        self.assertEqual(parse(path)["status"], "FAILED")

    def test_missing_mount_and_large_file_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "large.csv").write_bytes(b"x" * (10 * 1024 * 1024 + 1))
            with patch.dict(os.environ, {}, clear=True):
                self.assertEqual(parse("large.csv")["status"], "FAILED")
            with patch.dict(os.environ, {"WORKER_DOCUMENT_ROOT": directory}):
                item = parse("large.csv")
                self.assertEqual(item["status"], "FAILED")
                self.assertIn("10 MiB", item["error"])


if __name__ == "__main__":
    unittest.main()
