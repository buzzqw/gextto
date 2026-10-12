#!/usr/bin/env bash
# Keep the Go build cache from growing without bound.
#
# When the build cache (GOCACHE, normally ~/.cache/go-build) grows past a
# threshold, wipe it and immediately rebuild the packages and run the test
# suite, so a clean and warm cache is left behind instead of a cold one. The
# cache mainly balloons with CGO builds (the embedded libtorrent engine), which
# leave many large entries that are never reused.
#
#   scripts/refresh-build-cache.sh              # check and, if over the limit, clean + rebuild + test
#   scripts/refresh-build-cache.sh --dry-run    # only report the size and the decision
#   scripts/refresh-build-cache.sh --force      # clean + rebuild + test regardless of the size
#   scripts/refresh-build-cache.sh --status     # size, limit and the state of the user timer
#   scripts/refresh-build-cache.sh --install    # install and enable a weekly systemd user timer
#   scripts/refresh-build-cache.sh --uninstall  # remove that timer
#
# Configuration:
#   GEXTTO_BUILD_CACHE_LIMIT_GB   threshold in GiB (default 8); --limit-gb N overrides it
#   GEXTTO_GO                     path to the go binary (default: from PATH, then common locations)
#
# The rebuild deliberately does NOT run `make build`: that bumps the committed
# build_number and would leave the checkout dirty every run. It runs
# `go build ./...` and `make test`, which populate the same cache without
# touching build_number.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LIMIT_GB="${GEXTTO_BUILD_CACHE_LIMIT_GB:-8}"
MODE="check"
LOCK_FILE="${XDG_RUNTIME_DIR:-${HOME}/.cache}/gextto-build-cache.lock"
UNIT_DIR="${HOME}/.config/systemd/user"
UNIT_NAME="gextto-build-cache"
TIMER_UNIT="${UNIT_NAME}.timer"
SERVICE_UNIT="${UNIT_NAME}.service"

log() { printf '%s refresh-build-cache: %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*"; }

# usage prints the header comment block (everything after the shebang, up to
# the first non-comment line), with the leading "# " stripped.
usage() { awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0"; }

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run) MODE="dry-run" ;;
        --force) MODE="force" ;;
        --status) MODE="status" ;;
        --install) MODE="install" ;;
        --uninstall) MODE="uninstall" ;;
        --limit-gb)
            [[ $# -ge 2 && -n "$2" ]] || { echo "--limit-gb requires a value" >&2; exit 2; }
            LIMIT_GB="$2"
            shift
            ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown option: $1" >&2; exit 2 ;;
    esac
    shift
done

if ! [[ "$LIMIT_GB" =~ ^[0-9]+$ ]] || [[ "$LIMIT_GB" -lt 1 ]]; then
    echo "invalid threshold ${LIMIT_GB} GiB: use a positive integer" >&2
    exit 2
fi
LIMIT_MB=$(( LIMIT_GB * 1024 ))

# resolveGo finds the go binary: GEXTTO_GO, then PATH, then the usual install
# locations (a systemd user service starts with a minimal PATH).
resolveGo() {
    if [[ -n "${GEXTTO_GO:-}" && -x "${GEXTTO_GO}" ]]; then
        printf '%s' "${GEXTTO_GO}"
        return 0
    fi
    local found
    if found="$(command -v go 2>/dev/null)"; then
        printf '%s' "$found"
        return 0
    fi
    for candidate in /usr/local/go/bin/go /usr/lib/go/bin/go /usr/bin/go "${HOME}/go/bin/go"; do
        if [[ -x "$candidate" ]]; then
            printf '%s' "$candidate"
            return 0
        fi
    done
    return 1
}

# cacheDir returns GOCACHE as reported by go, or the default location.
cacheDir() {
    local go_bin="$1" reported=""
    reported="$("$go_bin" env GOCACHE 2>/dev/null || true)"
    if [[ -n "$reported" ]]; then
        printf '%s' "$reported"
    else
        printf '%s' "${HOME}/.cache/go-build"
    fi
}

# cacheSizeMB prints the rounded size of a directory in MiB (0 when missing).
cacheSizeMB() {
    local dir="$1"
    [[ -d "$dir" ]] || { printf '0'; return; }
    du -sm "$dir" 2>/dev/null | awk '{print $1}' || printf '0'
}

# humanGB renders MiB as GiB with one decimal.
humanGB() { LC_ALL=C awk -v mb="$1" 'BEGIN { printf "%.1f", mb / 1024 }'; }

installUnits() {
    local script_abs="$ROOT/scripts/refresh-build-cache.sh"
    local service_src="$ROOT/systemd/${SERVICE_UNIT}"
    local timer_src="$ROOT/systemd/${TIMER_UNIT}"
    [[ -f "$service_src" && -f "$timer_src" ]] || { echo "unit templates not found in $ROOT/systemd" >&2; exit 1; }
    command -v systemctl >/dev/null 2>&1 || { echo "systemctl not found" >&2; exit 1; }
    mkdir -p "$UNIT_DIR"
    sed "s|__SCRIPT__|${script_abs}|" "$service_src" > "$UNIT_DIR/${SERVICE_UNIT}"
    cp "$timer_src" "$UNIT_DIR/${TIMER_UNIT}"
    systemctl --user daemon-reload
    systemctl --user enable --now "${TIMER_UNIT}"
    log "timer installed: systemctl --user list-timers ${TIMER_UNIT}"
}

uninstallUnits() {
    command -v systemctl >/dev/null 2>&1 || { echo "systemctl not found" >&2; exit 1; }
    systemctl --user disable --now "${TIMER_UNIT}" 2>/dev/null || true
    rm -f "$UNIT_DIR/${TIMER_UNIT}" "$UNIT_DIR/${SERVICE_UNIT}"
    systemctl --user daemon-reload 2>/dev/null || true
    log "timer removed"
}

case "$MODE" in
    install) installUnits; exit 0 ;;
    uninstall) uninstallUnits; exit 0 ;;
esac

GO_BIN="$(resolveGo || true)"
if [[ -z "$GO_BIN" ]]; then
    log "go binary not found (set GEXTTO_GO or PATH); nothing to do"
    exit 1
fi
GOCACHE_DIR="$(cacheDir "$GO_BIN")"
SIZE_MB="$(cacheSizeMB "$GOCACHE_DIR")"

case "$MODE" in
    status)
        log "go: ${GO_BIN}"
        log "GOCACHE: ${GOCACHE_DIR} ($(humanGB "$SIZE_MB") GiB)"
        log "threshold: ${LIMIT_GB} GiB"
        if command -v systemctl >/dev/null 2>&1; then
            systemctl --user list-timers "${TIMER_UNIT}" --no-pager 2>/dev/null || log "timer not installed"
        fi
        exit 0
        ;;
    dry-run)
        if [[ "$SIZE_MB" -gt "$LIMIT_MB" ]]; then
            log "GOCACHE $(humanGB "$SIZE_MB") GiB > ${LIMIT_GB} GiB: would clean and rebuild"
        else
            log "GOCACHE $(humanGB "$SIZE_MB") GiB <= ${LIMIT_GB} GiB: nothing to do"
        fi
        exit 0
        ;;
esac

if [[ "$MODE" != "force" ]] && [[ "$SIZE_MB" -le "$LIMIT_MB" ]]; then
    log "GOCACHE $(humanGB "$SIZE_MB") GiB <= ${LIMIT_GB} GiB: nothing to do"
    exit 0
fi

# One run at a time (the timer and a manual run must not collide).
mkdir -p "$(dirname "$LOCK_FILE")"
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
    log "another refresh is already running; skipping"
    exit 0
fi

log "GOCACHE $(humanGB "$SIZE_MB") GiB > ${LIMIT_GB} GiB: cleaning and rebuilding"
"$GO_BIN" clean -cache
log "cache cleaned (now $(humanGB "$(cacheSizeMB "$GOCACHE_DIR")") GiB)"

build_status=0
test_status=0
if [[ -f "$ROOT/Makefile" ]]; then
    log "warming the cache: go build ./..."
    ( cd "$ROOT" && "$GO_BIN" build ./... ) || build_status=$?
    log "warming the cache: make test"
    ( cd "$ROOT" && make test ) || test_status=$?
else
    log "no Makefile in $ROOT; warming only with go build ./..."
    ( cd "$ROOT" && "$GO_BIN" build ./... ) || build_status=$?
fi

log "done (build=$build_status test=$test_status); GOCACHE now $(humanGB "$(cacheSizeMB "$GOCACHE_DIR")") GiB"
if [[ "$build_status" -ne 0 || "$test_status" -ne 0 ]]; then
    log "the cache was rebuilt, but a build or test step failed; see the output above"
    exit 1
fi
exit 0
