#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""{{NAME}}: {{DESCRIPTION}}

Standalone CLI that can stay loaded between runs (`maggot warm {{ID}}`).
Put slow setup (loading a model) in load() and one run in handle().
Run directly (./run.py --help) or with `maggot run {{ID}}`.
"""

import argparse
import json
import os
import sys
import time
from pathlib import Path

import maggot_warm


def event(**fields):
    """Append a progress/output event for the maggot UI. Optional: no-op without MAGGOT_EVENTS."""
    path = os.environ.get("MAGGOT_EVENTS")
    if path:
        with open(path, "a", encoding="utf-8") as f:
            f.write(json.dumps(fields) + "\n")


def load():
    """Slow, one-time setup. A warm process does this once; a cold run every time."""
    started = time.monotonic()
    model = {"upper": str.upper}  # replace with the real model
    return {"model": model, "loaded_at": time.time(), "load_seconds": time.monotonic() - started}


def handle(argv, state):
    """One run. Parse argv, use the loaded state, print results, return the exit code."""
    parser = argparse.ArgumentParser(prog="run.py", description="{{DESCRIPTION}}")
    parser.add_argument("files", nargs="+", help="input text files")
    parser.add_argument("--out", default=".", help="output folder (default: current folder)")
    parser.add_argument("--upper", action="store_true", help="convert text to upper case")
    args = parser.parse_args(argv)

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    for i, name in enumerate(args.files):
        src = Path(name)
        if not src.is_file():
            print(f"error: {src} is not a file", file=sys.stderr)
            return 2
        text = src.read_text(encoding="utf-8")
        if args.upper:
            text = state["model"]["upper"](text)
        dest = out / f"{src.stem}.out{src.suffix}"
        dest.write_text(text, encoding="utf-8")
        event(type="output", path=str(dest.resolve()))
        event(type="progress", value=(i + 1) / len(args.files))
        print(dest)
    return 0


if __name__ == "__main__":
    sys.exit(maggot_warm.main(load, handle))
