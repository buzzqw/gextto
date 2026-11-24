#!/usr/bin/env bash
# Exercise install.sh's payload transaction without root, systemd or network.
# Variables assigned for the sourced installer functions look unused here.
# shellcheck disable=SC2034
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Source the installer functions but not its final main invocation.
sed '$d' "$ROOT/install.sh" > "$TMP/install-functions.sh"
# shellcheck source=/dev/null
source "$TMP/install-functions.sh"

INSTALL_DIR="$TMP/live"
WORK_DIR="$TMP/work"
PAYLOAD_DIR="$WORK_DIR/payload"
mkdir -p "$PAYLOAD_DIR/lib" "$INSTALL_DIR/lib"
printf 'new daemon\n' > "$PAYLOAD_DIR/gexttod"
printf 'new engine\n' > "$PAYLOAD_DIR/gx-torrent"
printf 'new launcher\n' > "$PAYLOAD_DIR/run.sh"
printf 'new\n' > "$PAYLOAD_DIR/VERSION"
printf 'new library\n' > "$PAYLOAD_DIR/lib/engine.so"
chmod 0755 "$PAYLOAD_DIR/gexttod" "$PAYLOAD_DIR/gx-torrent" "$PAYLOAD_DIR/run.sh"

printf 'old daemon\n' > "$INSTALL_DIR/gexttod"
printf 'old engine\n' > "$INSTALL_DIR/gx-torrent"
printf 'old launcher\n' > "$INSTALL_DIR/run.sh"
printf 'old\n' > "$INSTALL_DIR/VERSION"
printf 'old library\n' > "$INSTALL_DIR/lib/engine.so"
printf 'operator data\n' > "$INSTALL_DIR/custom.txt"
chmod 0755 "$INSTALL_DIR/gexttod" "$INSTALL_DIR/gx-torrent" "$INSTALL_DIR/run.sh"

assert_file() {
  local path="$1" want="$2" got
  got="$(cat "$path")"
  [[ "$got" == "$want" ]] || { echo "$path = $got, wanted $want" >&2; exit 1; }
}

stage_install_payload "$WORK_DIR"
commit_install_payload
assert_file "$INSTALL_DIR/gexttod" "new daemon"
assert_file "$INSTALL_DIR/gx-torrent" "new engine"
assert_file "$INSTALL_DIR/run.sh" "new launcher"
assert_file "$INSTALL_DIR/VERSION" "new"
assert_file "$INSTALL_DIR/lib/engine.so" "new library"
assert_file "$INSTALL_DIR/gexttod.prev" "old daemon"
assert_file "$INSTALL_DIR/gx-torrent.prev" "old engine"
rollback_install_payload
assert_file "$INSTALL_DIR/gexttod" "old daemon"
assert_file "$INSTALL_DIR/gx-torrent" "old engine"
assert_file "$INSTALL_DIR/run.sh" "old launcher"
assert_file "$INSTALL_DIR/VERSION" "old"
assert_file "$INSTALL_DIR/lib/engine.so" "old library"
assert_file "$INSTALL_DIR/custom.txt" "operator data"

# Fail while installing the second entry, after the first live rename and after
# the old engine has been moved aside. The partial transaction must disappear.
INSTALL_COMMITTED_NAMES=()
INSTALL_PAYLOAD_NAMES=()
INSTALL_MANAGED_SEEN=()
INSTALL_STAGE=""
INSTALL_ROLLBACK_DIR=""
INSTALL_ROLLBACK_ACTIVE=0
stage_install_payload "$WORK_DIR"
# shellcheck disable=SC2329  # this mock is invoked indirectly by commit_install_payload
mv() {
  local args=("$@")
  local from="${args[-2]}" to="${args[-1]}"
  if [[ "$to" == "$INSTALL_DIR/gx-torrent" && "$from" == "$INSTALL_STAGE/"* ]]; then
    return 1
  fi
  command mv "${args[@]}"
}
if commit_install_payload; then
  echo "injected install failure was ignored" >&2
  exit 1
fi
unset -f mv
assert_file "$INSTALL_DIR/gexttod" "old daemon"
assert_file "$INSTALL_DIR/gx-torrent" "old engine"
assert_file "$INSTALL_DIR/run.sh" "old launcher"
assert_file "$INSTALL_DIR/VERSION" "old"
assert_file "$INSTALL_DIR/lib/engine.so" "old library"
[[ "$INSTALL_ROLLBACK_ACTIVE" == "0" ]] || { echo "rollback did not finish" >&2; exit 1; }

# The installer health gate accepts the expected status JSON or an auth challenge.
HTTP_TIMEOUT=1
LISTEN="0.0.0.0:5000"
[[ "$(service_health_url)" == "http://127.0.0.1:5000/api/status" ]]
systemctl() { echo active; }
sleep() { SECONDS=$((SECONDS + 2)); }
curl() {
  local output="" arg
  while [[ $# -gt 0 ]]; do
    arg="$1"
    case "$arg" in
      -o) output="$2"; shift 2 ;;
      -w|--max-time) shift 2 ;;
      *) shift ;;
    esac
  done
  printf '%s' "$MOCK_BODY" > "$output"
  printf '%s' "$MOCK_CODE"
}
MOCK_CODE=200
MOCK_BODY='{"name":"gextto","version":"1.2.3"}'
[[ "$(service_health)" == "healthy" ]]
MOCK_CODE=401
MOCK_BODY='{"ok":false,"error":"login required"}'
[[ "$(service_health)" == "healthy" ]]
MOCK_CODE=403
MOCK_BODY='{"ok":false,"error":"forbidden"}'
[[ "$(service_health)" == "healthy" ]]
MOCK_CODE=200
MOCK_BODY='{"name":"something-else","version":"1.2.3"}'
[[ "$(service_health)" == "unhealthy" ]]

echo "installer payload transaction checks passed"
