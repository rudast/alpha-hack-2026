"""Slow critical-risk review interface; no automatic refusal is issued."""

from __future__ import annotations

from typing import Any


def critical_risk_review(payload: dict[str, Any]) -> dict[str, Any]:
    application_id = payload.get("application_id")
    if not isinstance(application_id, str) or not application_id:
        raise ValueError("application_id is required")
    ratio = payload.get("debt_to_equity")
    flags = []
    if ratio is None:
        flags.append("DEBT_TO_EQUITY_UNDEFINED")
    elif isinstance(ratio, bool) or not isinstance(ratio, (int, float)):
        raise ValueError("debt_to_equity must be a number or null")
    elif ratio > 2:
        flags.append("HIGH_DEBT_TO_EQUITY")

    return {
        "application_id": application_id,
        "status": "STUB",
        "risk_flags": flags,
        "recommended_status": "MANUAL_REVIEW" if flags else "SCORING",
        "decision": None,
        "reason": "full scoring and external checks are not connected yet",
    }
