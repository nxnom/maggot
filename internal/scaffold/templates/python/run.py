#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""{{NAME}}: {{DESCRIPTION}}

Standalone CLI. Run directly (./run.py --help) or with `maggot run {{ID}}`.
Add PyPI packages to the `dependencies` list above; uv installs them on first run.
"""

import argparse
import json
import os
import sys
from pathlib import Path


def event(**fields):
    """Append a progress/output event for the maggot UI. Optional: no-op without MAGGOT_EVENTS."""
    path = os.environ.get("MAGGOT_EVENTS")
    if path:
        with open(path, "a", encoding="utf-8") as f:
            f.write(json.dumps(fields) + "\n")


def main() -> int:
    parser = argparse.ArgumentParser(description="{{DESCRIPTION}}")
    parser.add_argument("files", nargs="+", help="input text files")
    parser.add_argument("--out", default=".", help="output folder (default: current folder)")
    parser.add_argument("--upper", action="store_true", help="convert text to upper case")
    args = parser.parse_args()

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    for i, name in enumerate(args.files):
        src = Path(name)
        if not src.is_file():
            print(f"error: {src} is not a file", file=sys.stderr)
            return 2
        text = src.read_text(encoding="utf-8")
        if args.upper:
            text = text.upper()
        dest = out / f"{src.stem}.out{src.suffix}"
        dest.write_text(text, encoding="utf-8")
        event(type="output", path=str(dest.resolve()))
        event(type="progress", value=(i + 1) / len(args.files))
        print(dest)
    return 0


if __name__ == "__main__":
    sys.exit(main())
