#!/usr/bin/env bash
# Lightweight, side-effect-free checks for the two installers.
#
# Every invocation is either --help or --dry-run, so nothing is written and no
# service is touched: this is safe to run as a normal user and inside CI. It
# catches the regressions that are easy to introduce in shell (a broken flag, a
# dry-run that suddenly mutates, a missing key step).
#
# It deliberately does NOT use `set -e`: it counts failures and reports them all.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
INSTALL="$ROOT/install.sh"
USER_INSTALL="$ROOT/scripts/install-user-service.sh"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
# Keep the user-service checks far from the real unit file.
export XDG_CONFIG_HOME="$TMP/xdg"

failures=0
LAST_OUT=""
LAST_CODE=0

capture() { LAST_OUT="$("$@" 2>&1)"; LAST_CODE=$?; }
pass() { printf '  ok   %s\n' "$1"; }
fail() { printf '  FAIL %s\n' "$1"; failures=$((failures + 1)); }
contains() { printf '%s' "$LAST_OUT" | grep -qF -- "$1"; }

expect() {
  # expect <label> <exit-code> [must-contain]
  local label="$1" want="$2" needle="${3:-}"
  if [[ "$LAST_CODE" != "$want" ]]; then
    printf '       (exit %s, wanted %s)\n' "$LAST_CODE" "$want"
    printf '%s\n' "$LAST_OUT" | tail -3 | sed 's/^/       /'
    fail "$label"
    return
  fi
  if [[ -n "$needle" ]] && ! contains "$needle"; then
    printf '       (missing %q)\n' "$needle"
    fail "$label"
    return
  fi
  pass "$label"
}

check_common() {
  # check_common <label> <script>
  local label="$1" script="$2"

  capture bash -n "$script"
  expect "$label: syntax" 0

  capture bash "$script" --help
  expect "$label: --help exits 0" 0 "Usage:"

  capture bash "$script" --help
  if contains "--dry-run"; then pass "$label: --help documents --dry-run"; else fail "$label: --help documents --dry-run"; fi

  capture bash "$script" --definitely-not-a-flag
  expect "$label: unknown flag is rejected" 1 "unknown option"
}

check_install() {
  local label="install.sh"

  capture bash "$INSTALL" --dry-run --data-dir "$TMP/data" --install-dir "$TMP/opt"
  expect "$label: --dry-run works" 0 "systemctl restart gextto.service"
  if contains "write /etc/systemd/system/gextto.service"; then
    pass "$label: --dry-run writes the unit"
  else
    fail "$label: --dry-run writes the unit"
  fi

  if contains "write /etc/systemd/system/gextto-update.path"; then
    pass "$label: --dry-run sets up in-app updates"
  else
    fail "$label: --dry-run sets up in-app updates"
  fi
  if contains "systemctl enable --now gextto-update.path"; then
    pass "$label: --dry-run enables the update path unit"
  else
    fail "$label: --dry-run enables the update path unit"
  fi

  capture bash "$INSTALL" --help
  if contains "--media-group"; then pass "$label: --help documents --media-group"; else fail "$label: --help documents --media-group"; fi

  capture bash "$INSTALL" --dry-run --media-group gextto-selftest-missing-group --data-dir "$TMP/data" --install-dir "$TMP/opt"
  expect "$label: a missing media group is skipped" 0 "does not exist; skipped"

  capture bash "$INSTALL" --dry-run --uninstall --data-dir "$TMP/data" --install-dir "$TMP/opt"
  expect "$label: --dry-run --uninstall works" 0 "uninstalled"

  capture bash "$INSTALL" --dry-run --uninstall --purge --data-dir / --install-dir "$TMP/opt"
  expect "$label: purge refuses filesystem root" 1 "unsafe data directory target"

  capture bash "$INSTALL" --dry-run --purge --data-dir "$TMP/data" --install-dir "$TMP/opt"
  expect "$label: purge requires uninstall" 1 "--purge requires --uninstall"

  : > "$TMP/local.tar.gz"
  capture bash "$INSTALL" --dry-run --local-archive "$TMP/local.tar.gz" \
    --data-dir "$TMP/data" --install-dir "$TMP/opt"
  expect "$label: --local-archive is honoured" 0 "use local payload"

  capture bash "$INSTALL" --dry-run --version latest --data-dir "$TMP/data" --install-dir "$TMP/opt"
  expect "$label: --version latest selects the latest release" 0 "releases/latest/download"

  # The dry-run must not have created anything under the overridden paths.
  if [[ -e "$TMP/opt" ]]; then
    fail "$label: --dry-run left files behind"
  else
    pass "$label: --dry-run leaves no files"
  fi
}

check_user_install() {
  local label="install-user-service.sh"

  capture bash "$USER_INSTALL" --dry-run --data-dir "$TMP/udata"
  expect "$label: --dry-run works" 0 "systemctl --user restart gextto.service"
  if contains "write $XDG_CONFIG_HOME/systemd/user/gextto.service"; then
    pass "$label: --dry-run targets the user unit"
  else
    fail "$label: --dry-run targets the user unit"
  fi

  capture bash "$USER_INSTALL" --dry-run --uninstall --data-dir "$TMP/udata"
  expect "$label: --dry-run --uninstall works" 0 "uninstalled"

  capture bash "$USER_INSTALL" --dry-run --uninstall --purge --data-dir /
  expect "$label: purge refuses filesystem root" 1 "unsafe data purge target"

  capture bash "$USER_INSTALL" --dry-run --purge --data-dir "$TMP/udata"
  expect "$label: purge requires uninstall" 1 "--purge requires --uninstall"

  capture bash "$USER_INSTALL" --dry-run --port 8081 --data-dir "$TMP/udata"
  expect "$label: --port is applied" 0 "0.0.0.0:8081"

  capture bash "$USER_INSTALL" --dry-run --port 8081 --listen 127.0.0.1:8181 --data-dir "$TMP/udata"
  expect "$label: --listen overrides the port-derived address" 0 "127.0.0.1:8181"

  # Nothing may be written to the (overridden) unit directory by a dry-run.
  if [[ -e "$XDG_CONFIG_HOME/systemd/user/gextto.service" ]]; then
    fail "$label: --dry-run left a unit behind"
  else
    pass "$label: --dry-run leaves no unit"
  fi
}

check_package_script() {
  capture bash -n "$ROOT/scripts/package-linux.sh"
  expect "package-linux.sh: syntax" 0

  capture bash "$ROOT/scripts/package-linux.sh" --binary
  expect "package-linux.sh: missing option value is rejected" 2 "requires a value"

  capture bash -n "$ROOT/scripts/build-release.sh"
  expect "build-release.sh: syntax" 0
  capture bash "$ROOT/scripts/build-release.sh" --label
  expect "build-release.sh: missing option value is rejected" 2 "requires a value"

  capture bash "$ROOT/scripts/release-manifest.sh" --build 12 --commits 2 --output "$TMP/release.json"
  expect "release-manifest.sh: writes the manifest" 0 "manifest:"
  if grep -q '"app_version": "1.1.12"' "$TMP/release.json" 2>/dev/null && grep -q '"commits": \[' "$TMP/release.json"; then
    pass "release-manifest.sh: manifest has version and commits"
  else
    fail "release-manifest.sh: manifest has version and commits"
  fi
}

printf 'installer self-test\n'
check_common "install.sh" "$INSTALL"
check_common "install-user-service.sh" "$USER_INSTALL"
check_install
check_user_install
check_package_script

if (( failures > 0 )); then
  printf '\n%d check(s) failed\n' "$failures" >&2
  exit 1
fi
printf '\nall installer checks passed\n'
