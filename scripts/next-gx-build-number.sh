#!/usr/bin/env bash
# Monotonic build number of the gx-torrent daemon, independent from Gextto's
# `build_number`: read `gx-torrent.build_number`, increment it, persist and
# print the new value. The daemon is rebuilt only when its code changes, so its
# number grows by one per real gx-torrent build and never follows Gextto's.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FILE="$ROOT/gx-torrent.build_number"

# Serialize concurrent builds so two builds never get the same number.
if command -v flock >/dev/null; then
  exec 9<>"$FILE"
  flock 9
fi

current="$(tr -dc '0-9' < "$FILE" 2>/dev/null || true)"
[[ -n "$current" ]] || current=0
next=$((current + 1))
printf '%s\n' "$next" > "$FILE"
printf '%s\n' "$next"
