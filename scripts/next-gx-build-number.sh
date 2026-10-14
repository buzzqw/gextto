#!/usr/bin/env bash
# Monotonic build number of gx-core (the gx-torrent daemon), independent from
# Gextto's `build_number`. `gx-torrent.build_number` is committed: it is the
# single source shared by checkouts and CI, like `build_number`. With
# GEXTTO_BUMP_BUILD=1 (make build, make gx-torrent) the number is incremented,
# persisted and printed; otherwise (CI, release packaging) the committed value
# is printed unchanged, so a release carries the number of its checkout. The
# daemon is rebuilt only when its code changes, so its number grows by one per
# real gx-core build and never follows Gextto's.
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
if [[ "${GEXTTO_BUMP_BUILD:-0}" != "1" ]]; then
  printf '%s\n' "$current"
  exit 0
fi
next=$((current + 1))
printf '%s\n' "$next" > "$FILE"
printf '%s\n' "$next"
