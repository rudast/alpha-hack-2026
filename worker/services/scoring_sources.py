"""Explicit synthetic bureau profiles for demo companies only.

Unknown companies are not silently treated as having a clean credit history.
"""

from __future__ import annotations

from typing import Any


BUREAU_DEMO: dict[str, dict[str, int]] = {
    "7700000001": {"current_overdue_days": 0, "closed_overdue_30d_24m": 0,
                   "overdues_30d_12m": 0, "history_months": 60, "inquiries_90d": 1},
    "7700000002": {"current_overdue_days": 0, "closed_overdue_30d_24m": 0,
                   "overdues_30d_12m": 0, "history_months": 24, "inquiries_90d": 1},
    "7700000003": {"current_overdue_days": 0, "closed_overdue_30d_24m": 0,
                   "overdues_30d_12m": 0, "history_months": 48, "inquiries_90d": 1},
    "7700000004": {"current_overdue_days": 0, "closed_overdue_30d_24m": 0,
                   "overdues_30d_12m": 3, "history_months": 60, "inquiries_90d": 6},
    "770000000005": {"current_overdue_days": 0, "closed_overdue_30d_24m": 0,
                      "overdues_30d_12m": 0, "history_months": 36, "inquiries_90d": 1},
}

REGISTRY_DEMO = {inn: {"status": "ACTIVE", "red_flags": [], "yellow_flags": [],
                       "source": "synthetic_registry"} for inn in BUREAU_DEMO}


def bureau_profile(inn: str, supplied: Any) -> dict[str, Any] | None:
    profile = BUREAU_DEMO.get(inn)
    if profile is not None:
        # The backend demo seed stores only other_banks in credit_history.
        # Complete its missing scoring fields with the explicit demo fixture;
        # values supplied by the backend always take precedence.
        return {**profile, **supplied} if isinstance(supplied, dict) else profile.copy()
    return supplied.copy() if isinstance(supplied, dict) and supplied else None


def registry_profile(inn: str, supplied: Any) -> dict[str, Any] | None:
    if isinstance(supplied, dict) and supplied:
        profile = supplied.copy()
    else:
        demo = REGISTRY_DEMO.get(inn)
        if demo is None:
            return None
        profile = demo.copy()
    if "red_flags" not in profile:
        status = str(profile.get("status", "")).upper()
        if not status:
            return None
        profile["red_flags"] = [] if status == "ACTIVE" else [status]
    profile.setdefault("yellow_flags", [])
    return profile
