"""Single entry point that classifies tasks before service execution."""

from __future__ import annotations

from collections.abc import Callable
from typing import Any

from worker.services.document_parser import parse_documents
from worker.services.financial_verifier import credit_history, verify_application
from worker.services.risk_reviewer import critical_risk_review


Service = Callable[[dict[str, Any]], dict[str, Any]]

# Only this registry decides which lane a task uses.
ROUTES: dict[str, tuple[str, Service]] = {
    "credit_history": ("fast", credit_history),
    "verify_application": ("fast", verify_application),
    "parse_documents": ("slow", parse_documents),
    "critical_risk_review": ("slow", critical_risk_review),
}


def lane_for(kind: str) -> str:
    try:
        return ROUTES[kind][0]
    except KeyError as exc:
        raise ValueError(f"unknown task kind: {kind}") from exc


def dispatch(kind: str, payload: Any) -> dict[str, Any]:
    if not isinstance(payload, dict):
        raise ValueError("task payload must be an object")
    try:
        service = ROUTES[kind][1]
    except KeyError as exc:
        raise ValueError(f"unknown task kind: {kind}") from exc
    return service(payload)
