#!/usr/bin/env bash
# Nightly complete benchmark pass: every Go benchmark and the full client
# comparison, unattended. Meant for a quiet night (AGENTS.md, The benchmark
# rule): it waits for the load to settle and turns the automatic media
# integrity off for the round, putting the previous value back afterwards.
#
#   scripts/nightly-benchmarks.sh                 # both passes
#   scripts/nightly-benchmarks.sh --go-only       # Go benchmarks only
#   scripts/nightly-benchmarks.sh --compare-only  # client comparison only
#   scripts/nightly-benchmarks.sh --check         # wait + integrity check only
#
# --clients LIST and --rounds N go to the comparison untouched, for the round
# of one client alone that replaces only its rows (AGENTS.md, The benchmark
# rule): --compare-only --clients gx-torrent.
#
# Environment: GEXTTO_DATA_DIR (default ~/gextto-data), GEXTTO_API (default
# http://127.0.0.1:5000), GEXTTO_TEST_ROOT (work area), GEXTTO_BENCH_WAIT_MIN
# (how long to wait for a quiet host, default 60).
#
# Output: ${GEXTTO_TEST_ROOT:-~/.cache/gextto-tests}/nightly/<stamp>.log, the
# bench-all files in gextto-data/bench/ and compare.jsonl next to them.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

DATA_DIR="${GEXTTO_DATA_DIR:-$HOME/gextto-data}"
API="${GEXTTO_API:-http://127.0.0.1:${GEXTTO_PORT:-5000}}"
WORK_ROOT="${GEXTTO_TEST_ROOT:-${XDG_CACHE_HOME:-$HOME/.cache}/gextto-tests}"
LOG_DIR="$WORK_ROOT/nightly"
MAX_WAIT_MIN="${GEXTTO_BENCH_WAIT_MIN:-60}"
MODE=all
COMPARE_OPTIONS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --go-only) MODE=go; shift ;;
    --compare-only) MODE=compare; shift ;;
    --check) MODE=check; shift ;;
    --clients) COMPARE_OPTIONS+=(--clients "${2:?--clients requires a value}"); shift 2 ;;
    --rounds) COMPARE_OPTIONS+=(--rounds "${2:?--rounds requires a value}"); shift 2 ;;
    --max-wait-min) MAX_WAIT_MIN="${2:?--max-wait-min requires a value}"; shift 2 ;;
    -h|--help) awk 'NR>1 && /^#/ {sub(/^# ?/, ""); print; next} NR>1 {exit}' "$0"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

mkdir -p "$LOG_DIR"
exec 9>"$LOG_DIR/.lock"
if ! flock -n 9; then
  echo "another nightly benchmark run is already in progress" >&2
  exit 1
fi

STAMP="$(date +%Y%m%d-%H%M%S)"
TAG="gx-$(date +%Y-%m-%dT%H%M)"
LOG="$LOG_DIR/$STAMP.log"
exec > >(tee -a "$LOG") 2>&1
echo "nightly benchmarks $STAMP on $(hostname), mode $MODE"

# wait_quiet blocks until the machine is at rest — one-minute load at most 1 and
# no ffmpeg running on the host, three checks in a row — the condition the
# benchmark rule asks for. A host that stays busy is not measured: an ffmpeg of
# another program (a media server scanning its library, for one) marks every
# comparison run disturbed, and the harness then throws the run away.
QUIET_CHECKS=3
QUIET_INTERVAL_S=20
wait_quiet() {
  local deadline quiet=0 load owners
  deadline=$(( $(date +%s) + MAX_WAIT_MIN * 60 ))
  while :; do
    load="$(cut -d' ' -f1 /proc/loadavg)"
    owners="$(pgrep -a -x ffmpeg || true)"
    if [[ -n "$owners" ]]; then
      quiet=0
      echo "ffmpeg is running on the host: ${owners%%$'\n'*}"
    elif awk -v l="$load" 'BEGIN { exit !(l <= 1.0) }'; then
      quiet=$((quiet + 1))
      echo "host quiet (load $load), $quiet/$QUIET_CHECKS"
      if (( quiet >= QUIET_CHECKS )); then
        return 0
      fi
    else
      quiet=0
      echo "load $load, waiting for a quiet host"
    fi
    if (( $(date +%s) >= deadline )); then
      echo "the host was not quiet for ${MAX_WAIT_MIN} minutes: not measuring on it" >&2
      return 1
    fi
    sleep "$QUIET_INTERVAL_S"
  done
}

# media integrity runs ffmpeg on the library and the comparison harness marks
# such a run as disturbed. The previous value is put back on exit.
integrity_original=""
restore_integrity() {
  [[ -n "$integrity_original" ]] || return 0
  curl -fsS -X POST "$API/api/config/settings" -H 'Content-Type: application/json' \
    -d "{\"key\":\"media_integrity_enabled\",\"value\":\"$integrity_original\"}" >/dev/null ||
    echo "warning: could not restore media_integrity_enabled=$integrity_original" >&2
}
trap restore_integrity EXIT

prepare_integrity() {
  command -v sqlite3 >/dev/null 2>&1 || { echo "warning: sqlite3 missing, media integrity not checked" >&2; return 0; }
  local db="$DATA_DIR/gextto_config.db" value
  [[ -f "$db" ]] || return 0
  value="$(sqlite3 "$db" "select value from settings where key='media_integrity_enabled';" 2>/dev/null || true)"
  value="${value:-false}"
  if [[ "$value" == "false" ]]; then
    echo "media integrity is off"
    return 0
  fi
  echo "media integrity is $value: turning it off for the round"
  if ! curl -fsS -X POST "$API/api/config/settings" -H 'Content-Type: application/json' \
      -d '{"key":"media_integrity_enabled","value":"false"}' >/dev/null; then
    echo "cannot turn media integrity off through $API; refusing to run a disturbed benchmark" >&2
    return 1
  fi
  integrity_original="$value"
}

wait_quiet
prepare_integrity

if [[ "$MODE" == check ]]; then
  echo "check ok"
  exit 0
fi

status=0
if [[ "$MODE" != compare ]]; then
  echo "== every Go benchmark =="
  scripts/bench-all.sh || status=$?
fi
if [[ "$MODE" != go ]]; then
  echo "== client comparison: ${COMPARE_OPTIONS[*]:-every client, three rounds} =="
  # The host is checked again here: the Go pass takes half an hour, and what
  # starts after it (a media server scanning, a backup) would throw away every
  # run of the round.
  if ! wait_quiet; then
    status=1
  elif python3 scripts/compare/run.py build; then
    # The tag is the round's name: the tables printed here are only its rows, so
    # a round of one client alone is read on its own and replaces only its rows.
    python3 scripts/compare/run.py run --max-minutes 150 --tag "$TAG" "${COMPARE_OPTIONS[@]}" || status=$?
    python3 scripts/compare/run.py report --tag "$TAG" || status=$?
  else
    echo "the comparison images could not be built" >&2
    status=1
  fi
fi
restore_integrity
trap - EXIT
echo "nightly benchmarks finished with status $status; log $LOG"
exit "$status"
