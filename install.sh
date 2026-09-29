#!/usr/bin/env bash
# gextto installer: downloads the latest published continuous payload and
# installs it as a systemd service. Supports Debian, Ubuntu, Fedora, openSUSE
# and Arch Linux.
#
#   curl -fsSL .../install.sh | sudo bash
#   sudo bash install.sh --help
#   sudo bash install.sh --uninstall
#   sudo bash install.sh --dry-run
#
# Environment overrides (kept for backward compatibility): GEXTTO_DATA_DIR,
# GEXTTO_PORT, GEXTTO_ENGINE_PORT, GEXTTO_USER, GEXTTO_INSTALL_DIR,
# GEXTTO_REPO, GEXTTO_RELEASE, GEXTTO_ARCH, GEXTTO_SKIP_PACKAGES,
# GEXTTO_LOCAL_ARCHIVE, GEXTTO_NO_START, GEXTTO_HEALTH_TIMEOUT.
set -euo pipefail

INSTALL_DIR="${GEXTTO_INSTALL_DIR:-/opt/gextto}"
DATA_DIR="${GEXTTO_DATA_DIR:-/var/lib/gextto}"
PORT="${GEXTTO_PORT:-5000}"
ENGINE_PORT="${GEXTTO_ENGINE_PORT:-8889}"
SERVICE_USER="${GEXTTO_USER:-gextto}"
REPO="${GEXTTO_REPO:-buzzqw/gextto}"
RELEASE="${GEXTTO_RELEASE:-continuous}"
LOCAL_ARCHIVE="${GEXTTO_LOCAL_ARCHIVE:-}"
NO_START="${GEXTTO_NO_START:-0}"
HEALTH_TIMEOUT="${GEXTTO_HEALTH_TIMEOUT:-20}"
DRY_RUN=0
PURGE=0
ACTION="install"
WORK_DIR=""

SERVICE_NAME="gextto.service"
UNIT_PATH="/etc/systemd/system/gextto.service"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*" >&2; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

# run executes a mutating command, or prints it when --dry-run is set.
run() {
  if [[ "$DRY_RUN" == "1" ]]; then
    printf '   [dry-run] %s\n' "$*" >&2
    return 0
  fi
  "$@"
}

usage() {
  cat <<'EOF'
gextto installer

Usage:
  install.sh [options]

Options:
  -h, --help              show this help and exit
      --uninstall         stop the service and remove the program (keeps data)
      --purge             with --uninstall, also remove the data directory
      --dry-run           print what would happen, change nothing
      --no-start          install the files but do not start the service
      --version TAG       release tag or "latest"/"stable" (default: continuous)
      --release TAG       alias of --version
      --port PORT         web UI port (default: 5000)
      --engine-port PORT  internal engine port (default: 8889)
      --data-dir DIR      service data directory (default: /var/lib/gextto)
      --install-dir DIR   program directory (default: /opt/gextto)
      --user NAME         service user (default: gextto)
      --local-archive F   install from a local .tar.gz instead of downloading us

Environment overrides: GEXTTO_DATA_DIR, GEXTTO_PORT, GEXTTO_ENGINE_PORT,
GEXTTO_USER, GEXTTO_INSTALL_DIR, GEXTTO_REPO, GEXTTO_RELEASE, GEXTTO_ARCH,
GEXTTO_SKIP_PACKAGES, GEXTTO_LOCAL_ARCHIVE, GEXTTO_NO_START.
EOF
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -h|--help) usage; exit 0 ;;
      --uninstall) ACTION="uninstall"; shift ;;
      --purge) PURGE=1; shift ;;
      --dry-run) DRY_RUN=1; shift ;;
      --no-start) NO_START=1; shift ;;
      --version|--release) RELEASE="${2:-}"; [[ -n "$RELEASE" ]] || die "$1 requires a value"; shift 2 ;;
      --port) PORT="${2:-}"; [[ -n "$PORT" ]] || die "--port requires a value"; shift 2 ;;
      --engine-port) ENGINE_PORT="${2:-}"; [[ -n "$ENGINE_PORT" ]] || die "--engine-port requires a value"; shift 2 ;;
      --data-dir) DATA_DIR="${2:-}"; [[ -n "$DATA_DIR" ]] || die "--data-dir requires a value"; shift 2 ;;
      --install-dir) INSTALL_DIR="${2:-}"; [[ -n "$INSTALL_DIR" ]] || die "--install-dir requires a value"; shift 2 ;;
      --user) SERVICE_USER="${2:-}"; [[ -n "$SERVICE_USER" ]] || die "--user requires a value"; shift 2 ;;
      --local-archive) LOCAL_ARCHIVE="${2:-}"; [[ -n "$LOCAL_ARCHIVE" ]] || die "--local-archive requires a value"; shift 2 ;;
      *) die "unknown option: $1 (use --help)" ;;
    esac
  done
}

require_root() {
  [[ "$(id -u)" == "0" ]] || die "run as root (or with sudo)"
}

preflight() {
  # In --dry-run nothing is executed, so missing tools are a warning, not an
  # error: the plan can be reviewed anywhere.
  local fatal=1
  [[ "$DRY_RUN" == "1" ]] && fatal=0
  fail_or_warn() {
    if [[ "$fatal" == "1" ]]; then die "$1"; else warn "$1"; fi
  }

  if ! command -v systemctl >/dev/null 2>&1 || [[ ! -d /run/systemd/system ]]; then
    fail_or_warn "systemd is required (no running systemd detected)"
  fi
  local cmd
  for cmd in curl tar sha256sum install id useradd; do
    command -v "$cmd" >/dev/null 2>&1 || fail_or_warn "missing required command: $cmd"
  done
}

# install_packages is best-effort: a single missing optional package must never
# abort the installation (for example libssl3 does not exist on Debian 11).
install_packages() {
  [[ "${GEXTTO_SKIP_PACKAGES:-0}" == "1" ]] && { log "skipping system packages"; return; }
  if [[ "$DRY_RUN" == "1" ]]; then
    log "[dry-run] install system packages"
    return
  fi
  if command -v apt-get >/dev/null; then
    apt-get update -y || warn "apt-get update failed; continuing"
    apt-get install -y ca-certificates curl tar coreutils openssl libstdc++6 zlib1g zstd \
      || apt-get install -y ca-certificates curl tar coreutils openssl \
      || warn "some optional packages could not be installed; continuing"
  elif command -v dnf >/dev/null; then
    dnf install -y ca-certificates curl tar coreutils openssl openssl-libs libstdc++ zlib zstd \
      || warn "some optional packages could not be installed; continuing"
  elif command -v zypper >/dev/null; then
    zypper --non-interactive install ca-certificates curl tar coreutils openssl libstdc++6 zlib zstd \
      || warn "some optional packages could not be installed; continuing"
  elif command -v pacman >/dev/null; then
    pacman -Sy --noconfirm ca-certificates curl tar coreutils openssl zlib zstd \
      || warn "some optional packages could not be installed; continuing"
  else
    warn "unknown distribution: install curl, tar, sha256sum, OpenSSL and zstd manually"
  fi
}

# payload_asset resolves the published asset name for the current or requested
# architecture and dies on unsupported values.
payload_asset() {
  local arch="${GEXTTO_ARCH:-$(uname -m)}"
  case "$arch" in
    x86_64|amd64) arch=x86_64 ;;
    aarch64|arm64) arch=aarch64 ;;
    *) die "unsupported architecture: $arch (accepted: x86_64, aarch64)" ;;
  esac
  printf 'gextto-linux-%s.tar.gz' "$arch"
}

payload_base() {
  case "$RELEASE" in
    latest|stable) printf 'https://github.com/%s/releases/latest/download' "$REPO" ;;
    *) printf 'https://github.com/%s/releases/download/%s' "$REPO" "$RELEASE" ;;
  esac
}

obtain_payload() {
  local work="$1"
  local asset base
  asset="$(payload_asset)"
  base="$(payload_base)"

  if [[ -n "$LOCAL_ARCHIVE" ]]; then
    [[ -f "$LOCAL_ARCHIVE" ]] || die "local archive not found: $LOCAL_ARCHIVE"
    asset="$(basename "$LOCAL_ARCHIVE")"
    if [[ "$DRY_RUN" == "1" ]]; then
      log "[dry-run] use local payload $LOCAL_ARCHIVE"
      return 0
    fi
    cp -f "$LOCAL_ARCHIVE" "$work/$asset"
    if [[ -f "$LOCAL_ARCHIVE.sha256" ]]; then
      (cd "$work" && sha256sum -c "$asset.sha256") || die "checksum verification failed for $asset"
    else
      warn "no $asset.sha256 next to the local archive; skipping verification"
    fi
  else
    if [[ "$DRY_RUN" == "1" ]]; then
      log "[dry-run] download ${base}/${asset}"
      return 0
    fi
    log "downloading ${REPO} ${RELEASE} (${asset#gextto-linux-})"
    local cachebust; cachebust="$(date +%s)"
    curl -fL --retry 3 --retry-delay 2 "${base}/${asset}?cachebust=${cachebust}" -o "$work/$asset" \
      || die "unable to download ${asset} from ${base}"
    curl -fL --retry 3 --retry-delay 2 "${base}/${asset}.sha256?cachebust=${cachebust}" -o "$work/${asset}.sha256" \
      || die "unable to download checksum for ${asset}"
    (cd "$work" && sha256sum -c "${asset}.sha256") \
      || die "checksum verification failed for ${asset}"
  fi

  tar -xzf "$work/$asset" -C "$work"
  [[ -x "$work/gexttod" ]] || die "published payload does not contain an executable gexttod"
}

create_service_user() {
  if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
    local nologin; nologin="$(command -v nologin || echo /usr/sbin/nologin)"
    # --user-group guarantees the matching group exists even on distributions
    # whose useradd does not create one by default.
    run useradd --system --user-group --home-dir "$DATA_DIR" --shell "$nologin" "$SERVICE_USER" \
      || die "unable to create service user $SERVICE_USER"
    log "created service user $SERVICE_USER"
  fi
  if ! getent group "$SERVICE_USER" >/dev/null 2>&1; then
    run groupadd --system "$SERVICE_USER" || true
  fi
}

install_files() {
  local work="$1"
  run install -d -o "$SERVICE_USER" -g "$SERVICE_USER" "$DATA_DIR"
  run install -d "$INSTALL_DIR"

  # Keep the previous binary so a failed upgrade can be rolled back.
  if [[ "$DRY_RUN" != "1" && -f "$INSTALL_DIR/gexttod" ]]; then
    cp -a "$INSTALL_DIR/gexttod" "$INSTALL_DIR/gexttod.prev"
  fi

  run install -m 0755 "$work/gexttod" "$INSTALL_DIR/gexttod"
  [[ -f "$work/run.sh" ]] && run install -m 0755 "$work/run.sh" "$INSTALL_DIR/run.sh"
  [[ -f "$work/VERSION" ]] && run install -m 0644 "$work/VERSION" "$INSTALL_DIR/VERSION"

  # Replace the bundled libraries cleanly: leftovers from a previous version
  # must not shadow the new ones.
  if [[ "$DRY_RUN" != "1" ]]; then
    rm -rf "$INSTALL_DIR/lib"
  fi
  run install -d "$INSTALL_DIR/lib"
  if [[ -d "$work/lib" ]]; then
    run cp -a "$work/lib/." "$INSTALL_DIR/lib/"
  fi

  # Remove the legacy daemon API token from previous installations. Other
  # variables in the file, if an operator added any, are left untouched.
  if [[ "$DRY_RUN" != "1" && -f /etc/gextto/gextto.env ]]; then
    sed -i '/^GEXTTO_API_TOKEN=/d' /etc/gextto/gextto.env
  fi
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
User=$SERVICE_USER
Group=$SERVICE_USER
EnvironmentFile=-/etc/gextto/gextto.env
Environment=GEXTTO_DATA_DIR=$DATA_DIR
Environment=GEXTTO_LISTEN=0.0.0.0:$PORT
Environment=GEXTTO_ENGINE_LISTEN=127.0.0.1:$ENGINE_PORT
Environment=GEXTTO_LOG=info
WorkingDirectory=$DATA_DIR
ExecStart=$INSTALL_DIR/gexttod
Restart=on-failure
RestartSec=5
LimitNOFILE=65536
# The embedded libtorrent and the archive paths may live on NFS/NAS mounts.
TimeoutStopSec=90

[Install]
WantedBy=multi-user.target
UNIT
)"
  if [[ "$DRY_RUN" == "1" ]]; then
    printf '   [dry-run] write %s\n' "$UNIT_PATH" >&2
    return 0
  fi
  install -d /etc/systemd/system
  printf '%s\n' "$unit" > "$UNIT_PATH"
}

# restart_and_verify restarts the service, waits for it to become active and
# rolls back the previous binary when the new one fails to start.
restart_and_verify() {
  run systemctl daemon-reload
  run systemctl enable "$SERVICE_NAME"
  if [[ "$NO_START" == "1" ]]; then
    log "service installed but not started (--no-start)"
    return 0
  fi
  run systemctl restart "$SERVICE_NAME"
  [[ "$DRY_RUN" == "1" ]] && return 0

  local waited=0
  while (( waited < HEALTH_TIMEOUT )); do
    if systemctl is-active --quiet "$SERVICE_NAME"; then
      log "gextto is running"
      rm -f "$INSTALL_DIR/gexttod.prev"
      return 0
    fi
    sleep 1
    waited=$((waited + 1))
  done

  if [[ -f "$INSTALL_DIR/gexttod.prev" ]]; then
    warn "gextto did not start; rolling back to the previous binary"
    install -m 0755 "$INSTALL_DIR/gexttod.prev" "$INSTALL_DIR/gexttod"
    systemctl daemon-reload
    systemctl restart "$SERVICE_NAME" || true
    sleep 2
    if systemctl is-active --quiet "$SERVICE_NAME"; then
      warn "rollback successful: the previous version is running again"
    else
      warn "rollback did not restore a running service"
    fi
  fi
  die "gextto failed to start; inspect: journalctl -u $SERVICE_NAME -n 100"
}

uninstall() {
  if systemctl list-unit-files "$SERVICE_NAME" >/dev/null 2>&1; then
    run systemctl disable --now "$SERVICE_NAME" || warn "could not stop/disable $SERVICE_NAME"
  fi
  [[ -f "$UNIT_PATH" ]] && run rm -f "$UNIT_PATH"
  run systemctl daemon-reload || true

  if [[ "$PURGE" == "1" ]]; then
    warn "removing data directory $DATA_DIR (databases, downloads, backups)"
    run rm -rf "$DATA_DIR"
  else
    log "data kept in $DATA_DIR (use --purge to remove it)"
  fi
  log "removing program directory $INSTALL_DIR"
  run rm -rf "$INSTALL_DIR"
  log "gextto uninstalled"
}

print_summary() {
  local version=""
  if [[ -f "$INSTALL_DIR/VERSION" ]]; then
    version="$(tr -d '[:space:]' < "$INSTALL_DIR/VERSION" 2>/dev/null || true)"
  fi
  log "gextto installed${version:+ (version $version)}"
  log "UI:      http://127.0.0.1:$PORT  (listening on 0.0.0.0:$PORT)"
  log "status:  systemctl status $SERVICE_NAME"
  log "logs:    journalctl -u $SERVICE_NAME -f"
  warn "the web UI has no built-in authentication: keep it on a trusted network or put an authenticated HTTPS reverse proxy in front (see docs/SECURITY.md)"
}

main() {
  parse_args "$@"

  if [[ "$ACTION" == "uninstall" ]]; then
    [[ "$DRY_RUN" == "1" ]] || require_root
    uninstall
    return 0
  fi

  # --dry-run changes nothing and may be reviewed without privileges.
  [[ "$DRY_RUN" == "1" ]] || require_root
  preflight
  install_packages

  WORK_DIR="$(mktemp -d)"
  trap 'rm -rf "$WORK_DIR"' EXIT
  obtain_payload "$WORK_DIR"

  create_service_user
  install_files "$WORK_DIR"
  write_unit
  restart_and_verify
  print_summary
}

main "$@"
