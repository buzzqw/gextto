#!/usr/bin/env bash
# Run a test gx-core (gx-torrent standalone) next to the Gextto in production,
# to try a local change without waiting for a CI release.
#
#   scripts/gx-core-test.sh build      # compile this checkout into the test dir
#   scripts/gx-core-test.sh start      # start it (add --build to compile first)
#   scripts/gx-core-test.sh restart    # stop + start (add --build to recompile)
#   scripts/gx-core-test.sh stop | status | log
#
# Extra daemon options go after "--", e.g. `start -- -debug -no-dht`.
#
# It never touches what Gextto uses: its own directory (state, session DB,
# downloads, log), its own API port and its own peer ports. The build goes to
# the test directory, not to bin/, and does not bump the committed build
# numbers, so Gextto's managed gx-torrent is never replaced. It is a local
# build without a release channel: it never updates itself.
#
# Settings (environment):
#   GX_TEST_DIR         test directory           (default ~/gx-core-test)
#   GX_TEST_LISTEN      web page / API address   (default 127.0.0.1:8899)
#   GX_TEST_PEER_PORTS  peer port range          (default 51500-51510)
#   GX_TEST_TOKEN       X-Gx-Token required by the API; without it a
#                       non-loopback GX_TEST_LISTEN gets -insecure
#   GX_TEST_BIN         binary to run instead of the test build
#
# Production uses 0.0.0.0:8890 and peer ports 51300-51400: the defaults stay
# clear of both.
set -euo pipefail

# Resolve a symlink (e.g. ~/gx-core-test.sh) to find the checkout.
SCRIPT="$(readlink -f "$0")"
ROOT="$(cd "$(dirname "$SCRIPT")/.." && pwd)"
DIR="${GX_TEST_DIR:-$HOME/gx-core-test}"
LISTEN="${GX_TEST_LISTEN:-127.0.0.1:8899}"
PEER_PORTS="${GX_TEST_PEER_PORTS:-51500-51510}"
TOKEN="${GX_TEST_TOKEN:-}"
BIN="${GX_TEST_BIN:-$DIR/bin/gx-torrent}"
PID_FILE="$DIR/gx-torrent.pid"
LOG_FILE="$DIR/gx-torrent.log"
PORT="${LISTEN##*:}"
HOST="${LISTEN%:*}"
[[ "$HOST" == "0.0.0.0" || -z "$HOST" ]] && HOST="127.0.0.1"
BASE_URL="http://$HOST:$PORT"

usage() {
  sed -n '2,11p' "$SCRIPT" | sed 's/^# \{0,1\}//'
  exit 2
}

running_pid() {
  [[ -f "$PID_FILE" ]] || return 1
  local pid
  pid="$(cat "$PID_FILE")"
  [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null || return 1
  # The pid file may outlive the process and the pid be reused. After a
  # rebuild the running binary shows as "<path> (deleted)".
  local exe
  exe="$(readlink "/proc/$pid/exe" 2>/dev/null)"
  [[ "${exe% (deleted)}" == "$(readlink -f "$BIN")" ]] || return 1
  echo "$pid"
}

build() {
  mkdir -p "$DIR/bin"
  local gx_build commit built_at
  gx_build="$(tr -d '[:space:]' < "$ROOT/gx-torrent.build_number")"
  commit="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
  if [[ -n "$(git -C "$ROOT" status --porcelain -- cmd/gx-torrent internal 2>/dev/null)" ]]; then
    commit="$commit-dirty"
  fi
  built_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "building gx-torrent from $ROOT ($commit) into $DIR/bin"
  (cd "$ROOT" && CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/buzzqw/gextto/internal/constants.GxTorrentBuild=$gx_build -X github.com/buzzqw/gextto/internal/constants.Commit=$commit -X github.com/buzzqw/gextto/internal/constants.BuiltAt=$built_at" \
    -o "$DIR/bin/gx-torrent.new" ./cmd/gx-torrent)
  # Rename over the old file: a running test daemon keeps its own copy.
  mv -f "$DIR/bin/gx-torrent.new" "$DIR/bin/gx-torrent"
  "$DIR/bin/gx-torrent" -version
}

start() {
  local pid
  if pid="$(running_pid)"; then
    echo "already running (pid $pid) on $BASE_URL"
    return 0
  fi
  if [[ ! -x "$BIN" ]]; then
    echo "no binary at $BIN: run '$0 build' (or start --build) first" >&2
    exit 1
  fi
  if ss -Hltn "sport = :$PORT" 2>/dev/null | grep -q .; then
    echo "port $PORT is already in use: set GX_TEST_LISTEN to a free address" >&2
    exit 1
  fi
  mkdir -p "$DIR/data" "$DIR/downloads"
  local args=(
    -mode standalone
    -listen "$LISTEN"
    -data "$DIR/data"
    -download-dir "$DIR/downloads"
    -peer-ports "$PEER_PORTS"
    -log-file "$LOG_FILE"
    # Leave the router alone: no UPnP/NAT-PMP mapping for a test daemon.
    -no-upnp
    -no-natpmp
  )
  if [[ -n "$TOKEN" ]]; then
    args+=(-token "$TOKEN")
  elif [[ "${LISTEN%:*}" != "127.0.0.1" && "${LISTEN%:*}" != "localhost" ]]; then
    args+=(-insecure)
  fi
  args+=("${EXTRA_ARGS[@]}")
  nohup "$BIN" "${args[@]}" >> "$DIR/gx-torrent.stdout" 2>&1 &
  echo $! > "$PID_FILE"
  for _ in $(seq 1 60); do
    if curl -fsS -m 2 ${TOKEN:+-H "X-Gx-Token: $TOKEN"} "$BASE_URL/api/v1/health" > /dev/null 2>&1; then
      echo "gx-core test started (pid $(cat "$PID_FILE")): $BASE_URL"
      status_line
      return 0
    fi
    if ! kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
      echo "gx-core test exited at startup; last lines:" >&2
      tail -n 20 "$DIR/gx-torrent.stdout" "$LOG_FILE" 2>/dev/null >&2 || true
      rm -f "$PID_FILE"
      exit 1
    fi
    sleep 0.5
  done
  echo "gx-core test did not answer on $BASE_URL within 30 s; see $LOG_FILE" >&2
  exit 1
}

stop() {
  local pid
  if ! pid="$(running_pid)"; then
    echo "not running"
    rm -f "$PID_FILE"
    return 0
  fi
  kill "$pid"
  for _ in $(seq 1 60); do
    kill -0 "$pid" 2>/dev/null || { echo "stopped (pid $pid)"; rm -f "$PID_FILE"; return 0; }
    sleep 0.5
  done
  echo "pid $pid did not stop within 30 s; sending SIGKILL" >&2
  kill -9 "$pid" 2>/dev/null || true
  rm -f "$PID_FILE"
}

status_line() {
  curl -fsS -m 3 ${TOKEN:+-H "X-Gx-Token: $TOKEN"} "$BASE_URL/api/v1/health" 2>/dev/null \
    | grep -oE '"(version|pid|mode|data_dir)": *"?[^,"}]*' | tr -d '"' | tr '\n' ' '
  echo
}

status() {
  local pid
  if pid="$(running_pid)"; then
    echo "running (pid $pid) on $BASE_URL, directory $DIR"
    status_line
  else
    echo "not running (directory $DIR)"
    return 3
  fi
}

[[ $# -ge 1 ]] || usage
COMMAND="$1"
shift
DO_BUILD=0
EXTRA_ARGS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --build) DO_BUILD=1; shift ;;
    --) shift; EXTRA_ARGS=("$@"); break ;;
    *) echo "unknown option: $1" >&2; usage ;;
  esac
done

case "$COMMAND" in
  build) build ;;
  start) [[ $DO_BUILD == 1 ]] && build; start ;;
  restart) [[ $DO_BUILD == 1 ]] && build; stop; start ;;
  stop) stop ;;
  status) status ;;
  log) exec tail -n 50 -F "$LOG_FILE" ;;
  *) usage ;;
esac
