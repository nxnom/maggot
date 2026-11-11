#!/bin/sh
# {{NAME}}: {{DESCRIPTION}}
# Standalone CLI. Run directly (./run.sh --help) or with `maggot run {{ID}}`.
set -eu

usage() {
  echo "usage: run.sh [--out DIR] [--upper] FILE..." >&2
}

# event appends a progress/output event for the maggot UI (no-op without MAGGOT_EVENTS).
event() {
  if [ -n "${MAGGOT_EVENTS:-}" ]; then printf '%s\n' "$1" >> "$MAGGOT_EVENTS"; fi
}

out="."
upper=0
while [ $# -gt 0 ]; do
  case "$1" in
    --out) out="$2"; shift 2 ;;
    --upper) upper=1; shift ;;
    -h|--help) usage; exit 0 ;;
    --) shift; break ;;
    -*) echo "error: unknown flag $1" >&2; usage; exit 2 ;;
    *) break ;;
  esac
done
[ $# -gt 0 ] || { usage; exit 2; }

mkdir -p "$out"
for src in "$@"; do
  if [ ! -f "$src" ]; then echo "error: $src is not a file" >&2; exit 2; fi
  base=$(basename "$src")
  stem=${base%.*}
  ext=${base##*.}
  dest="$out/$stem.out.$ext"
  if [ "$upper" = 1 ]; then tr '[:lower:]' '[:upper:]' < "$src" > "$dest"; else cp "$src" "$dest"; fi
  event "{\"type\":\"output\",\"path\":\"$dest\"}"
  echo "$dest"
done
