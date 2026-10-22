"""Slow document parsing interface with an honest placeholder response."""

from __future__ import annotations

from typing import Any


def parse_documents(payload: dict[str, Any]) -> dict[str, Any]:
    application_id = payload.get("application_id")
    documents = payload.get("documents")
    if not isinstance(application_id, str) or not application_id:
        raise ValueError("application_id is required")
    if not isinstance(documents, list):
        raise ValueError("documents must be an array")

    items = []
    for document in documents:
        if not isinstance(document, dict):
            raise ValueError("each document must be an object")
        items.append({
            "kind": document.get("kind"),
            "filename": document.get("filename"),
            "status": "NOT_PARSED",
            "extracted_fields": {},
        })
    return {
        "application_id": application_id,
        "status": "STUB",
        "documents": items,
        "reason": "document parser is not connected yet",
    }
