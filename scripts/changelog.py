"""Print the CHANGELOG entry for one version.

The release workflow feeds this straight into the Release page, and checks it
before building so that a missing entry fails in ten seconds rather than after
four platforms have been built.

    python3 scripts/changelog.py 0.3.0
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

CHANGELOG = Path(__file__).resolve().parent.parent / "CHANGELOG.md"


def entry(version: str) -> str:
    text = CHANGELOG.read_text(encoding="utf-8")
    # From this version's heading to the next one, whatever it happens to be.
    found = re.search(
        rf"^## {re.escape(version)}\s*$(.*?)(?=^## |\Z)",
        text,
        re.MULTILINE | re.DOTALL,
    )
    if found is None:
        raise SystemExit(f"CHANGELOG.md has no {version} section")
    body = found.group(1).strip()
    if not body:
        raise SystemExit(f"The {version} section in CHANGELOG.md is empty")
    return body


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit(f"Usage: {sys.argv[0]} <version>")
    print(entry(sys.argv[1].removeprefix("v")))


if __name__ == "__main__":
    main()
