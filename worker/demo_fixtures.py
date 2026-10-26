"""Generate signed PNG test fixtures for the opt-in demo approval flow."""

from __future__ import annotations

import argparse
import os
from pathlib import Path

from worker.services.demo_attestation import create_demo_png


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--application-id", required=True)
    parser.add_argument("--inn", required=True)
    parser.add_argument("--subject-name", required=True,
                        help="Director name from the demo company registry")
    parser.add_argument("--output-dir", required=True, type=Path)
    args = parser.parse_args()
    key = os.environ.get("WORKER_DEMO_ATTESTATION_KEY", "")
    if len(key.encode("utf-8")) < 32:
        parser.error("WORKER_DEMO_ATTESTATION_KEY must contain at least 32 bytes")
    args.output_dir.mkdir(parents=True, exist_ok=True)
    for kind in ("passport", "bki_consent"):
        path = args.output_dir / f"{kind}.png"
        with path.open("xb") as output:
            output.write(create_demo_png(
                kind, args.application_id, args.inn, args.subject_name, key))
        print(path)


if __name__ == "__main__":
    main()
