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
# The payload is built on Ubuntu 22.04 (glibc 2.35), so it runs on Debian 12+,
# Ubuntu 22.04+, Fedora 39+, openSUSE Leap 15.6+/Tumbleweed and Arch. Before
# touching the system the installer runs the downloaded binary once: an
# incompatible system is reported with the missing libraries instead of a
# service that never starts.
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
HTTP_TIMEOUT="${GEXTTO_HTTP_TIMEOUT:-60}"
MEDIA_GROUPS="${GEXTTO_MEDIA_GROUPS:-}"
CHECKOUT_ROOT=""
# Piped into bash (`curl ... | sudo bash`) there is no script file and
# BASH_SOURCE is unset: with `set -u` it must be read with a default.
if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" ]]; then
  CHECKOUT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fi
DRY_RUN=0
PURGE=0
ACTION="install"
WORK_DIR=""

SERVICE_NAME="gextto.service"
UNIT_PATH="/etc/systemd/system/gextto.service"
# In-app updates: the web UI (running as the unprivileged service user) drops a
# request file in the data directory; this root path unit notices it and runs
# `gexttod --update`, which replaces the program and restarts the service.
UPDATE_PATH_UNIT="/etc/systemd/system/gextto-update.path"
UPDATE_SERVICE_UNIT="/etc/systemd/system/gextto-update.service"
UPDATE_LOG="/var/log/gextto-update.log"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*" >&2; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

# Refuse directory targets that would make an install/chown/rm operation
# affect a system root, the checkout, or the user's whole home directory.
validate_directory_target() {
  local target="$1" label="$2" resolved
  [[ -n "$target" ]] || die "$label must not be empty"
  resolved="$(realpath -m -- "$target")" || die "cannot resolve $label: $target"
  case "$resolved" in
    /|/bin|/boot|/dev|/etc|/home|/lib|/lib64|/media|/mnt|/opt|/proc|/root|/run|/sbin|/srv|/sys|/tmp|/usr|/usr/local|/var|"$CHECKOUT_ROOT"|"$HOME")
      die "refusing unsafe $label target: $target (resolves to $resolved)"
      ;;
  esac
}

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
      --media-group G     add the service user to group G (repeatable or
                          comma-separated), e.g. the group that owns the NAS
                          media folders
      --local-archive F   install from a local .tar.gz instead of downloading us

Environment overrides: GEXTTO_DATA_DIR, GEXTTO_PORT, GEXTTO_ENGINE_PORT,
GEXTTO_USER, GEXTTO_INSTALL_DIR, GEXTTO_REPO, GEXTTO_RELEASE, GEXTTO_ARCH,
GEXTTO_SKIP_PACKAGES, GEXTTO_LOCAL_ARCHIVE, GEXTTO_NO_START,
GEXTTO_MEDIA_GROUPS.
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
      --media-group) [[ -n "${2:-}" ]] || die "--media-group requires a value"; MEDIA_GROUPS="${MEDIA_GROUPS:+$MEDIA_GROUPS,}$2"; shift 2 ;;
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
  # curl, tar and gzip are checked after install_packages, which installs
  # them: minimal systems (containers, netinstall) often lack them.
  local cmd
  for cmd in sha256sum install id useradd realpath; do
    command -v "$cmd" >/dev/null 2>&1 || fail_or_warn "missing required command: $cmd"
  done
}

require_download_tools() {
  [[ "$DRY_RUN" == "1" ]] && return 0
  local cmd
  for cmd in curl tar gzip; do
    command -v "$cmd" >/dev/null 2>&1 || die "missing required command: $cmd (install it and run the installer again)"
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
    dnf install -y ca-certificates curl tar gzip coreutils openssl openssl-libs libstdc++ zlib zstd \
      || warn "some optional packages could not be installed; continuing"
  elif command -v zypper >/dev/null; then
    # The bundled libtorrent needs OpenSSL 3: on Leap 15.x it is libopenssl3,
    # not installed by default (the default openssl there is 1.1).
    zypper --non-interactive install ca-certificates curl tar gzip coreutils openssl libopenssl3 libstdc++6 zlib zstd \
      || zypper --non-interactive install ca-certificates curl tar gzip libopenssl3 libstdc++6 \
      || warn "some optional packages could not be installed; continuing"
  elif command -v pacman >/dev/null; then
    # No -y: refreshing the databases without a full upgrade (-Syu) is a
    # partial upgrade, which Arch does not support.
    pacman -S --needed --noconfirm ca-certificates curl tar gzip coreutils openssl gcc-libs zlib zstd \
      || warn "some optional packages could not be installed (run pacman -Syu first?); continuing"
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
      cp -f "$LOCAL_ARCHIVE.sha256" "$work/$asset.sha256"
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
  check_payload_runs "$work"
}

# check_payload_runs starts the downloaded binary once (--version only) before
# anything on the system changes. A missing library or a too-old glibc is
# reported with the exact cause instead of a service that never starts.
check_payload_runs() {
  local work="$1" output
  if output="$(LD_LIBRARY_PATH="$work/lib" "$work/gexttod" --version 2>&1)"; then
    log "payload ok: $output"
    return 0
  fi
  warn "the downloaded gexttod does not run on this system:"
  printf '   %s\n' "$output" >&2
  if command -v ldd >/dev/null 2>&1; then
    local missing
    missing="$(LD_LIBRARY_PATH="$work/lib" ldd "$work/gexttod" "$work"/lib/*.so* 2>/dev/null | grep 'not found' | sort -u || true)"
    [[ -n "$missing" ]] && { warn "missing libraries:"; printf '   %s\n' "$missing" >&2; }
  fi
  if printf '%s' "$output" | grep -q 'GLIBC_'; then
    warn "this system's C library is older than the one Gextto needs (glibc 2.35: Debian 12, Ubuntu 22.04 or newer)"
  fi
  die "installation aborted; nothing was changed"
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
  # Supplementary groups give the service access to media folders owned by
  # another user (NAS mounts, a shared "media" group) without touching their
  # permissions.
  local group
  local -a groups
  IFS=',' read -r -a groups <<< "$MEDIA_GROUPS"
  for group in "${groups[@]}"; do
    group="${group// /}"
    [[ -n "$group" ]] || continue
    if ! getent group "$group" >/dev/null 2>&1; then
      warn "group $group does not exist; skipped"
      continue
    fi
    run usermod -a -G "$group" "$SERVICE_USER" && log "service user $SERVICE_USER added to group $group"
  done
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
  # gx-torrent is the default torrent engine: Gextto starts it from here in
  # managed mode. Its absence would silently fall back to embedded libtorrent.
  if [[ -f "$work/gx-torrent" ]]; then
    run install -m 0755 "$work/gx-torrent" "$INSTALL_DIR/gx-torrent"
  else
    warn "gx-torrent is missing from the payload: Gextto will fall back to embedded libtorrent"
  fi
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
# Never kill Gextto while it stops: it waits for copies and moves of media
# files to finish (they can take minutes on NFS/NAS mounts) and logs what it is
# waiting for every 15 seconds. A SIGKILL halfway leaves partial files behind.
TimeoutStopSec=infinity

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

# write_update_units installs the root path unit that serves the "Update"
# button of the web UI. The request file only triggers the update: its content
# is ignored, so the unprivileged service cannot choose what root runs or
# downloads. The update follows the installed channel (continuous or stable).
write_update_units() {
  local request="$DATA_DIR/update-request"
  local path_unit service_unit
  path_unit="$(cat <<UNIT
[Unit]
Description=Watch for Gextto update requests from the web UI

[Path]
PathExists=$request
Unit=gextto-update.service

[Install]
WantedBy=multi-user.target
UNIT
)"
  service_unit="$(cat <<UNIT
[Unit]
Description=Update Gextto (requested from the web UI)
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
# Remove the request first: a failed update must not loop.
ExecStartPre=/bin/rm -f $request
ExecStart=$INSTALL_DIR/gexttod --update
StandardOutput=append:$UPDATE_LOG
StandardError=append:$UPDATE_LOG
# The restart waits for media copies to the NAS to finish.
TimeoutStartSec=infinity
UNIT
)"
  if [[ "$DRY_RUN" == "1" ]]; then
    printf '   [dry-run] write %s and %s\n' "$UPDATE_PATH_UNIT" "$UPDATE_SERVICE_UNIT" >&2
    return 0
  fi
  printf '%s\n' "$path_unit" > "$UPDATE_PATH_UNIT"
  printf '%s\n' "$service_unit" > "$UPDATE_SERVICE_UNIT"
  # Readable by the service, so the UI can show how the last update went.
  touch "$UPDATE_LOG"
  chmod 0644 "$UPDATE_LOG"
}

# wait_for_http waits until the web UI answers. A service that is "active" may
# still be migrating its databases: only an HTTP answer proves it works.
wait_for_http() {
  local waited=0
  while (( waited < HTTP_TIMEOUT )); do
    if curl -fs -o /dev/null --max-time 3 "http://127.0.0.1:$PORT/api/status"; then
      log "web UI answering on port $PORT"
      return 0
    fi
    sleep 2
    waited=$((waited + 2))
  done
  warn "the service is running but the web UI did not answer within ${HTTP_TIMEOUT}s; inspect: journalctl -u $SERVICE_NAME -n 100"
}

# restart_and_verify restarts the service, waits for it to become active and
# rolls back the previous binary when the new one fails to start.
restart_and_verify() {
  run systemctl daemon-reload
  run systemctl enable "$SERVICE_NAME"
  run systemctl enable --now gextto-update.path || warn "could not enable in-app updates (gextto-update.path)"
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
      # Keep the previous binary as gexttod.prev: the in-app updater restores
      # it when a new version fails to start.
      wait_for_http
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
  if [[ -f "$UPDATE_PATH_UNIT" ]]; then
    run systemctl disable --now gextto-update.path || true
  fi
  run rm -f "$UPDATE_PATH_UNIT" "$UPDATE_SERVICE_UNIT"
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
  local lan_ip
  lan_ip="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
  log "UI:      http://${lan_ip:-127.0.0.1}:$PORT  (listening on 0.0.0.0:$PORT)"
  log "first run: open the UI, the setup wizard guides you through folders, sources and the first title"
  log "status:  systemctl status $SERVICE_NAME"
  log "logs:    journalctl -u $SERVICE_NAME -f"
  warn "the web UI is open until a password is set: the setup wizard asks for one at the first visit (for access from the internet use an HTTPS reverse proxy, see docs/SECURITY.md)"
}

main() {
  parse_args "$@"
  [[ "$PURGE" != "1" || "$ACTION" == "uninstall" ]] || die "--purge requires --uninstall"
  validate_directory_target "$DATA_DIR" "data directory"
  validate_directory_target "$INSTALL_DIR" "install directory"

  if [[ "$ACTION" == "uninstall" ]]; then
    [[ "$DRY_RUN" == "1" ]] || require_root
    uninstall
    return 0
  fi

  # --dry-run changes nothing and may be reviewed without privileges.
  [[ "$DRY_RUN" == "1" ]] || require_root
  preflight
  install_packages
  require_download_tools

  WORK_DIR="$(mktemp -d)"
  trap 'rm -rf "$WORK_DIR"' EXIT
  obtain_payload "$WORK_DIR"

  create_service_user
  install_files "$WORK_DIR"
  write_unit
  write_update_units
  restart_and_verify
  print_summary
}

main "$@"
