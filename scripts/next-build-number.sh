#!/usr/bin/env bash
# Monotonic build number: read `build_number`, increment it, persist and print
# the new value. Release and CI builds call it once so every published payload
# gets a distinct, increasing build number.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FILE="$ROOT/build_number"

current="$(tr -dc '0-9' < "$FILE" 2>/dev/null || true)"
[[ -n "$current" ]] || current=1000
next=$((current + 1))
printf '%s\n' "$next" > "$FILE"
printf '%s\n' "$next"
