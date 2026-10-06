#!/usr/bin/env bash
# Install (or refresh) gextto as a systemd *user* service, without root.
#
# This is the deployment used when the daemon runs as a normal login user
# instead of a dedicated system account created by install.sh. It builds the
# binary from this checkout, writes ~/.config/systemd/user/gextto.service,
# restarts the service and rolls back the previous unit if the new one fails to
# start.
#
#   scripts/install-user-service.sh --help
#   scripts/install-user-service.sh --dry-run
#   scripts/install-user-service.sh --uninstall
#
# Environment overrides: GEXTTO_DATA_DIR, GEXTTO_PORT, GEXTTO_LISTEN,
# GEXTTO_ENGINE_PORT, GEXTTO_ACTIVE, GEXTTO_DRY_RUN, GEXTTO_LOG, GEXTTO_BINARY.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BINARY="${GEXTTO_BINARY:-$ROOT/bin/gexttod}"
DATA_DIR="${GEXTTO_DATA_DIR:-$HOME/gextto-data}"
PORT="${GEXTTO_PORT:-5000}"
# Gextto is meant to be used over the LAN from the other PCs of the same owner,
# so the service listens on every interface by default. Restrict access with a
# firewall or reverse proxy when the network is not fully trusted.
LISTEN="${GEXTTO_LISTEN:-}"
ENGINE_PORT="${GEXTTO_ENGINE_PORT:-8889}"
ACTIVE="${GEXTTO_ACTIVE:-1}"
SERVICE_DRY_RUN="${GEXTTO_DRY_RUN:-0}"
LOG="${GEXTTO_LOG:-info}"

UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
UNIT="$UNIT_DIR/gextto.service"
SERVICE_NAME="gextto.service"
HEALTH_TIMEOUT="${GEXTTO_HEALTH_TIMEOUT:-20}"

PLAN=0          # installer --dry-run (changes nothing)
PURGE=0
NO_START=0
REBUILD=0
ACTION="install"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*" >&2; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

validate_data_purge_target() {
  local resolved
  [[ -n "$DATA_DIR" ]] || die "data directory must not be empty"
  resolved="$(realpath -m -- "$DATA_DIR")" || die "cannot resolve data directory: $DATA_DIR"
  case "$resolved" in
    /|/bin|/boot|/dev|/etc|/home|/lib|/lib64|/media|/mnt|/opt|/proc|/root|/run|/sbin|/srv|/sys|/tmp|/usr|/usr/local|/var|"$ROOT"|"$HOME")
      die "refusing unsafe data purge target: $DATA_DIR (resolves to $resolved)"
      ;;
  esac
}

# run executes a mutating command, or prints it when --dry-run is set.
run() {
  if [[ "$PLAN" == "1" ]]; then
    printf '   [dry-run] %s\n' "$*" >&2
    return 0
  fi
  "$@"
}

usage() {
  cat <<'EOF'
gextto user-service installer (no root required)

Usage:
  scripts/install-user-service.sh [options]

Options:
  -h, --help              show this help and exit
      --uninstall         stop the service and remove the unit (keeps data)
      --purge             with --uninstall, also remove the data directory
      --dry-run           print what would happen, change nothing
      --no-start          write the unit but do not start the service
      --rebuild           rebuild bin/gexttod even if it already exists
      --binary PATH       daemon binary (default: bin/gexttod in this checkout)
      --data-dir DIR      service data directory (default: ~/gextto-data)
      --port PORT         web UI port (default: 5000)
      --listen ADDR       listen address (default: 0.0.0.0:PORT)
      --engine-port PORT  internal engine port (default: 8889)

Environment overrides: GEXTTO_DATA_DIR, GEXTTO_PORT, GEXTTO_LISTEN,
GEXTTO_ENGINE_PORT, GEXTTO_ACTIVE, GEXTTO_DRY_RUN, GEXTTO_LOG, GEXTTO_BINARY.
EOF
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -h|--help) usage; exit 0 ;;
      --uninstall) ACTION="uninstall"; shift ;;
      --purge) PURGE=1; shift ;;
      --dry-run) PLAN=1; shift ;;
      --no-start) NO_START=1; shift ;;
      --rebuild) REBUILD=1; shift ;;
      --binary) BINARY="${2:-}"; [[ -n "$BINARY" ]] || die "--binary requires a value"; shift 2 ;;
      --data-dir) DATA_DIR="${2:-}"; [[ -n "$DATA_DIR" ]] || die "--data-dir requires a value"; shift 2 ;;
      --port) PORT="${2:-}"; [[ -n "$PORT" ]] || die "--port requires a value"; shift 2 ;;
      --listen) LISTEN="${2:-}"; [[ -n "$LISTEN" ]] || die "--listen requires a value"; shift 2 ;;
      --engine-port) ENGINE_PORT="${2:-}"; [[ -n "$ENGINE_PORT" ]] || die "--engine-port requires a value"; shift 2 ;;
      *) die "unknown option: $1 (use --help)" ;;
    esac
  done
}

preflight() {
  if ! command -v systemctl >/dev/null 2>&1; then
    die "systemctl is required"
  fi
  command -v realpath >/dev/null 2>&1 || die "realpath is required"
  if ! systemctl --user show-environment >/dev/null 2>&1; then
    if [[ "$PLAN" == "1" ]]; then
      warn "no systemd user manager detected (dry-run continues)"
    else
      die "no systemd user manager for $USER (is systemd running and is a login session active?)"
    fi
  fi
}

build_binary() {
  if [[ -x "$BINARY" && "$REBUILD" != "1" ]]; then
    return 0
  fi
  if [[ "$PLAN" == "1" ]]; then
    log "[dry-run] build binary at $BINARY"
    return 0
  fi
  log "building $BINARY"
  ( cd "$ROOT" && GEXTTO_BINARY="$BINARY" make build )
}

write_unit() {
  local unit
  unit="$(cat <<UNIT
[Unit]
Description=Gextto media acquisition and archiving daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$ROOT
ExecStart=$BINARY
Environment=GEXTTO_DATA_DIR=$DATA_DIR
Environment=GEXTTO_LISTEN=$LISTEN
Environment=GEXTTO_ENGINE_LISTEN=127.0.0.1:$ENGINE_PORT
Environment=GEXTTO_ACTIVE=$ACTIVE
Environment=GEXTTO_DRY_RUN=$SERVICE_DRY_RUN
Environment=GEXTTO_LOG=$LOG
Restart=on-failure
RestartSec=5
LimitNOFILE=65536
# Never kill Gextto while it stops: it waits for copies and moves of media
# files to finish (they can take minutes on NFS/NAS mounts) and logs what it is
# waiting for every 15 seconds. A SIGKILL halfway leaves partial files behind.
TimeoutStopSec=infinity

[Install]
WantedBy=default.target
UNIT
)"
  run mkdir -p "$UNIT_DIR" "$DATA_DIR"
  # Back up the current unit so a failed activation can be rolled back.
  if [[ "$PLAN" != "1" && -f "$UNIT" ]]; then
    cp -a "$UNIT" "$UNIT.prev"
  fi
  if [[ "$PLAN" == "1" ]]; then
    printf '   [dry-run] write %s\n' "$UNIT" >&2
    return 0
  fi
  printf '%s\n' "$unit" > "$UNIT"
}

# activate restarts the service, waits for it to become active and restores the
# previous unit when the new one fails to start.
activate() {
  run systemctl --user daemon-reload
  run systemctl --user enable "$SERVICE_NAME"
  if [[ "$NO_START" == "1" ]]; then
    log "service installed but not started (--no-start)"
    return 0
  fi
  run systemctl --user restart "$SERVICE_NAME"
  [[ "$PLAN" == "1" ]] && return 0

  local waited=0
  while (( waited < HEALTH_TIMEOUT )); do
    if systemctl --user is-active --quiet "$SERVICE_NAME"; then
      log "gextto is running"
      rm -f "$UNIT.prev"
      return 0
    fi
    sleep 1
    waited=$((waited + 1))
  done

  if [[ -f "$UNIT.prev" ]]; then
    warn "gextto did not start; restoring the previous unit"
    cp -a "$UNIT.prev" "$UNIT"
    systemctl --user daemon-reload
    systemctl --user restart "$SERVICE_NAME" || true
    sleep 2
    if systemctl --user is-active --quiet "$SERVICE_NAME"; then
      warn "rollback successful: the previous unit is running again"
    else
      warn "rollback did not restore a running service"
    fi
  fi
  die "gextto failed to start; inspect: journalctl --user -u $SERVICE_NAME -n 100"
}

uninstall() {
  if systemctl --user list-unit-files "$SERVICE_NAME" >/dev/null 2>&1; then
    run systemctl --user disable --now "$SERVICE_NAME" || warn "could not stop/disable $SERVICE_NAME"
  fi
  [[ -f "$UNIT" ]] && run rm -f "$UNIT"
  run systemctl --user daemon-reload || true

  if [[ "$PURGE" == "1" ]]; then
    warn "removing data directory $DATA_DIR (databases, downloads, backups)"
    run rm -rf "$DATA_DIR"
  else
    log "data kept in $DATA_DIR (use --purge to remove it)"
  fi
  log "gextto user service uninstalled"
}

print_summary() {
  local user="${USER:-$(id -un)}"
  log "gextto user service installed: $UNIT"
  log "UI:      $LISTEN"
  log "status:  systemctl --user status $SERVICE_NAME"
  log "logs:    journalctl --user -u $SERVICE_NAME -f"
  if command -v loginctl >/dev/null 2>&1 && ! loginctl show-user "$user" 2>/dev/null | grep -q 'Linger=yes'; then
    warn "the service stops when you log out: run 'loginctl enable-linger $user' to keep it running"
  fi
  warn "the web UI has no built-in authentication: keep it on a trusted network or put an authenticated HTTPS reverse proxy in front (see docs/SECURITY.md)"
}

main() {
  parse_args "$@"
  [[ "$PURGE" != "1" || "$ACTION" == "uninstall" ]] || die "--purge requires --uninstall"
  # Listen address follows the port unless it was given explicitly.
  LISTEN="${LISTEN:-0.0.0.0:$PORT}"

  if [[ "$ACTION" == "uninstall" ]]; then
    [[ "$PLAN" == "1" ]] || preflight
    [[ "$PURGE" != "1" ]] || validate_data_purge_target
    uninstall
    return 0
  fi

  preflight
  build_binary
  write_unit
  activate
  print_summary
}

main "$@"
