"""Opt-in, signed identity evidence for synthetic demo applications only.

The key is supplied by the demo operator, never bundled with the repository.
These fixtures are not a substitute for an identity or consent provider.
"""

from __future__ import annotations

import hashlib
import hmac
import io
import json
from datetime import date
from typing import Any

from PIL import Image, ImageDraw, ImageFont
from PIL.PngImagePlugin import PngInfo


METADATA_KEY = "credit_demo_attestation"
DEMO_KINDS = {"passport", "bki_consent"}
CONSENT_USER = "Демо Банк"
MAX_CONSENT_DAYS = 180


def _canonical(claims: dict[str, Any]) -> bytes:
    return json.dumps(claims, ensure_ascii=False, sort_keys=True,
                      separators=(",", ":")).encode("utf-8")


def read_attestation(raw: bytes) -> dict[str, Any] | None:
    """Read an untrusted claim; verification happens in verify_attestation."""
    try:
        with Image.open(io.BytesIO(raw)) as image:
            if image.format != "PNG":
                return None
            encoded = image.info.get(METADATA_KEY)
        if not isinstance(encoded, str) or len(encoded) > 4096:
            return None
        value = json.loads(encoded)
        return value if isinstance(value, dict) else None
    except (OSError, ValueError, Image.DecompressionBombError):
        return None


def verify_attestation(attestation: Any, *, key: str, kind: str,
                       application_id: str, inn: str, subject_name: str,
                       consent_given: bool, today: date | None = None) -> bool:
    """Accept only a correctly signed fixture for this demo application."""
    if not isinstance(key, str) or len(key.encode("utf-8")) < 32 or kind not in DEMO_KINDS:
        return False
    if not isinstance(attestation, dict) or set(attestation) != {"claims", "signature"}:
        return False
    claims, signature = attestation["claims"], attestation["signature"]
    if not isinstance(claims, dict) or not isinstance(signature, str):
        return False
    expected_keys = {"version", "issuer", "kind", "application_id", "subject_inn",
                     "subject_name", "purpose", "issued_on", "credit_history_user"}
    if set(claims) != expected_keys or claims.get("version") != 1 or claims.get("issuer") != "demo-fixture":
        return False
    if (claims.get("kind"), claims.get("application_id"), claims.get("subject_inn"),
            claims.get("subject_name")) != (kind, application_id, inn, subject_name):
        return False
    if not subject_name or not isinstance(subject_name, str):
        return False
    expected_purpose = "credit_history_request" if kind == "bki_consent" else "identity_check"
    if claims.get("purpose") != expected_purpose or claims.get("credit_history_user") != CONSENT_USER:
        return False
    if kind == "bki_consent" and not consent_given:
        return False
    try:
        issued = date.fromisoformat(claims["issued_on"])
    except (TypeError, ValueError):
        return False
    age = ((today or date.today()) - issued).days
    if age < 0 or age > MAX_CONSENT_DAYS:
        return False
    expected = hmac.new(key.encode("utf-8"), _canonical(claims), hashlib.sha256).hexdigest()
    return hmac.compare_digest(signature, expected)


def create_demo_png(kind: str, application_id: str, inn: str,
                    subject_name: str, key: str, issued_on: date | None = None) -> bytes:
    """Create an explicitly labelled demo fixture for an operator to upload."""
    if kind not in DEMO_KINDS or len(key.encode("utf-8")) < 32:
        raise ValueError("demo kind and a key of at least 32 bytes are required")
    if not application_id or not inn or not subject_name:
        raise ValueError("application_id, inn and subject_name are required")
    claims = {
        "version": 1, "issuer": "demo-fixture", "kind": kind,
        "application_id": application_id, "subject_inn": inn,
        "subject_name": subject_name,
        "purpose": "credit_history_request" if kind == "bki_consent" else "identity_check",
        "issued_on": (issued_on or date.today()).isoformat(),
        "credit_history_user": CONSENT_USER,
    }
    signature = hmac.new(key.encode("utf-8"), _canonical(claims), hashlib.sha256).hexdigest()
    metadata = PngInfo()
    metadata.add_text(METADATA_KEY, json.dumps(
        {"claims": claims, "signature": signature}, ensure_ascii=False))
    image = Image.new("RGB", (1200, 320), "white")
    draw = ImageDraw.Draw(image)
    try:
        font = ImageFont.truetype("DejaVuSans.ttf", 32)
    except OSError:
        font = ImageFont.load_default()
    for row, line in enumerate(("DEMO TEST DOCUMENT - NOT A REAL ID OR CONSENT",
                                f"Type: {kind}", f"Company INN: {inn}",
                                f"Application: {application_id}")):
        draw.text((30, 25 + row * 65), line, fill="black", font=font)
    output = io.BytesIO()
    image.save(output, format="PNG", pnginfo=metadata)
    return output.getvalue()
