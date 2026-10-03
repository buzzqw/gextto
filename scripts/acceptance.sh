#!/usr/bin/env bash
# Isolated smoke test: a temporary data directory and dedicated ports in
# dry-run, so it never touches a real installation.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BINARY="${ROOT}/bin/gexttod"
[[ -x "$BINARY" ]] || { CGO_ENABLED=1 go build -o "$BINARY" "${ROOT}/cmd/gexttod"; }

DATA="$(mktemp -d)"
PORT="${GEXTTO_PORT:-5055}"
ENGINE_PORT="${GEXTTO_ENGINE_PORT:-8899}"
LOG="$DATA/daemon.out"
PID=""

# shellcheck disable=SC2317  # cleanup is invoked indirectly by the EXIT trap
cleanup() {
  if [[ -n "$PID" ]]; then
    kill "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
  rm -rf "$DATA"
}
trap cleanup EXIT

GEXTTO_DATA_DIR="$DATA" GEXTTO_LISTEN="127.0.0.1:$PORT" \
  GEXTTO_ENGINE_LISTEN="127.0.0.1:$ENGINE_PORT" \
  GEXTTO_DRY_RUN=1 GEXTTO_ACTIVE=0 "$BINARY" > "$LOG" 2>&1 &
PID=$!

started=0
for _ in $(seq 1 40); do
  if curl -sf -m 2 "http://127.0.0.1:$PORT/api/status" >/dev/null; then started=1; break; fi
  sleep 0.5
done
if [[ "$started" != "1" ]]; then
  echo "FAIL: daemon did not become ready on port $PORT" >&2
  echo "--- $LOG ---" >&2
  cat "$LOG" >&2 || true
  exit 1
fi

fail=0
for path in /api/status /api/health /api/config /api/series /api/movies /api/torrents /api/comics; do
  code="$(curl -s -o /dev/null -w '%{http_code}' -m 5 "http://127.0.0.1:$PORT$path")"
  if [[ "$code" != "200" ]]; then echo "FAIL $path -> $code"; fail=1; else echo "ok   $path"; fi
done

code="$(curl -s -o /dev/null -w '%{http_code}' -m 5 "http://127.0.0.1:$PORT/")"
if [[ "$code" == "200" ]]; then echo "ok   /"; else echo "FAIL / -> $code"; fail=1; fi

exit "$fail"
