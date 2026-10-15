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
# It listens on the whole LAN, like the packaged standalone daemon, and start
# and status print the address another device on the network opens (its own
# page stays open until a password is set in its settings.json, standalone's
# LAN bypass): it is a test daemon to try from the phone or another PC, not a
# second server to expose beyond the local network.
#
# start warns when the test binary is older than the checkout (run it with
# --build to pick up the current code) and still detects an instance set up
# with a page password, whose API answers 401 until a session exists.
#
# Settings (environment):
#   GX_TEST_DIR         test directory           (default ~/gx-core-test)
#   GX_TEST_LISTEN      web page / API address   (default 0.0.0.0:8899)
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
LISTEN="${GX_TEST_LISTEN:-0.0.0.0:8899}"
PEER_PORTS="${GX_TEST_PEER_PORTS:-51500-51510}"
TOKEN="${GX_TEST_TOKEN:-}"
BIN="${GX_TEST_BIN:-$DIR/bin/gx-torrent}"
PID_FILE="$DIR/gx-torrent.pid"
LOG_FILE="$DIR/gx-torrent.log"
PORT="${LISTEN##*:}"
HOST="${LISTEN%:*}"
HOST="${HOST#[}"
HOST="${HOST%]}"
# A wildcard bind is not an address anyone can type: the health checks use
# loopback and the LAN address is looked up separately (lan_url).
WILDCARD=0
case "$HOST" in
  "" | 0.0.0.0 | ::) WILDCARD=1 ;;
esac
BASE_URL="http://127.0.0.1:$PORT"

usage() {
  sed -n '2,11p' "$SCRIPT" | sed 's/^# \{0,1\}//'
  exit 2
}

loopback_host() {
  [[ "$1" == "localhost" || "$1" == "::1" || "$1" == 127.* ]]
}

# lan_ip is the machine's address on the local network: the source IP of the
# route to the gateway (the kernel only picks the route, no packet is sent),
# falling back to the first address of `hostname -I`; empty when the host has no
# usable LAN address.
lan_ip() {
  local ip=""
  if command -v ip >/dev/null 2>&1; then
    ip="$(ip -4 route get 1.1.1.1 2>/dev/null \
      | awk '{for (i = 1; i < NF; i++) if ($i == "src") {print $(i + 1); exit}}')"
  fi
  if [[ -z "$ip" ]] && command -v hostname >/dev/null 2>&1; then
    ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
  fi
  printf '%s' "$ip"
}

# lan_url is where another device on the local network opens the test daemon:
# the bound address when it is a concrete one, the machine's LAN address when a
# wildcard is bound, empty when it listens on loopback only (or this host has no
# LAN address).
lan_url() {
  if [[ "$WILDCARD" == 1 ]]; then
    local ip
    ip="$(lan_ip)"
    [[ -n "$ip" ]] || return 0
    printf 'http://%s:%s' "$ip" "$PORT"
    return 0
  fi
  if loopback_host "$HOST"; then
    return 0
  fi
  printf 'http://%s:%s' "$HOST" "$PORT"
}

# report_lan prints the address on the LAN, or why the daemon cannot be reached
# there.
report_lan() {
  local url
  url="$(lan_url)"
  if [[ -n "$url" ]]; then
    echo "reachable from the LAN at $url"
  elif [[ "$WILDCARD" == 1 ]]; then
    echo "no LAN address on this host: reachable from this machine only ($BASE_URL)" >&2
  else
    echo "bound to $HOST: reachable from this machine only, not from the LAN" >&2
  fi
}

# health_code is the HTTP status of /api/v1/health: "200" when the script can
# read it, "401" when the page password guards the API (still proof that the
# daemon answers), "" or "000" when nothing listens.
health_code() {
  local args=(-sS -o /dev/null -m 2 -w '%{http_code}')
  [[ -n "$TOKEN" ]] && args+=(-H "X-Gx-Token: $TOKEN")
  curl "${args[@]}" "$BASE_URL/api/v1/health" 2>/dev/null || true
}

# daemon_answers reports whether the daemon is up: any HTTP status counts,
# because a standalone instance set up with a page password answers 401 to the
# API until a session exists.
daemon_answers() {
  local code
  code="$(health_code)"
  [[ "$code" =~ ^[0-9]{3}$ && "$code" != "000" ]]
}

# warn_if_stale points out a test binary that lags behind the checkout: the page
# then shows an old build number and old behaviour, which reads as "the last
# merge changed nothing".
warn_if_stale() {
  local built want
  built="$("$BIN" -version 2>/dev/null | awk -F. '{print $NF}')"
  want="$(tr -d '[:space:]' < "$ROOT/gx-torrent.build_number" 2>/dev/null)"
  if [[ -n "$built" && -n "$want" && "$built" != "$want" ]]; then
    echo "note: $BIN is gx-torrent build $built while this checkout is $want: run '$0 restart --build' to test the current code" >&2
  fi
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
  warn_if_stale
  if pid="$(running_pid)"; then
    echo "already running (pid $pid) on $BASE_URL"
    report_lan
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
  elif [[ "$WILDCARD" == 1 ]] || ! loopback_host "$HOST"; then
    args+=(-insecure)
  fi
  args+=("${EXTRA_ARGS[@]}")
  nohup "$BIN" "${args[@]}" >> "$DIR/gx-torrent.stdout" 2>&1 &
  echo $! > "$PID_FILE"
  for _ in $(seq 1 60); do
    if daemon_answers; then
      echo "gx-core test started (pid $(cat "$PID_FILE")): $BASE_URL"
      report_lan
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
  local code
  code="$(health_code)"
  case "$code" in
    200)
      curl -fsS -m 3 ${TOKEN:+-H "X-Gx-Token: $TOKEN"} "$BASE_URL/api/v1/health" 2>/dev/null \
        | grep -oE '"(version|pid|mode|data_dir)": *"?[^,"}]*' | tr -d '"' | tr '\n' ' '
      echo
      ;;
    401) echo "API behind the page password (401): open $BASE_URL/ and sign in" ;;
    "" | 000) echo "no answer on $BASE_URL" ;;
    *) echo "HTTP $code on $BASE_URL/api/v1/health" ;;
  esac
}

status() {
  local pid
  warn_if_stale
  if pid="$(running_pid)"; then
    echo "running (pid $pid) on $BASE_URL, directory $DIR"
    report_lan
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
