#!/usr/bin/env bash
# Fast development build using the current version/build number (no increment).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GEXTTO_BINARY="${GEXTTO_BINARY:-$ROOT/bin/gexttod}" \
  "$ROOT/scripts/build-daemon.sh"
