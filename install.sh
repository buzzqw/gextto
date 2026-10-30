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
# GEXTTO_PORT, GEXTTO_ENGINE_PORT, GEXTTO_LISTEN, GEXTTO_USER, GEXTTO_GROUP,
# GEXTTO_INSTALL_DIR, GEXTTO_REPO, GEXTTO_RELEASE, GEXTTO_ARCH,
# GEXTTO_SKIP_PACKAGES, GEXTTO_SKIP_MEDIA_TOOLS, GEXTTO_LOCAL_ARCHIVE,
# GEXTTO_NO_START, GEXTTO_HEALTH_TIMEOUT, GEXTTO_HTTP_TIMEOUT,
# GEXTTO_MEDIA_GROUPS.
set -euo pipefail

INSTALL_DIR="${GEXTTO_INSTALL_DIR:-/opt/gextto}"
DATA_DIR="${GEXTTO_DATA_DIR:-/var/lib/gextto}"
PORT="${GEXTTO_PORT:-5000}"
ENGINE_PORT="${GEXTTO_ENGINE_PORT:-8889}"
# LISTEN empty means the default 0.0.0.0:$PORT, resolved once PORT is known.
LISTEN="${GEXTTO_LISTEN:-}"
# Service user: by default the local user who runs the installer, so the
# daemon can create folders in that user's media tree without extra
# permissions. Use `--user gextto` (or GEXTTO_USER) for the isolated,
# login-less system account; GEXTTO_GROUP overrides its group.
SERVICE_USER="${GEXTTO_USER:-}"
SERVICE_GROUP="${GEXTTO_GROUP:-}"
REPO="${GEXTTO_REPO:-buzzqw/gextto}"
RELEASE="${GEXTTO_RELEASE:-continuous}"
LOCAL_ARCHIVE="${GEXTTO_LOCAL_ARCHIVE:-}"
NO_START="${GEXTTO_NO_START:-0}"
# How long the restarted service gets to answer over HTTP; the first start of
# a new version may migrate the databases. GEXTTO_HEALTH_TIMEOUT is the older
# name of the same limit.
HTTP_TIMEOUT="${GEXTTO_HTTP_TIMEOUT:-${GEXTTO_HEALTH_TIMEOUT:-180}}"
MEDIA_GROUPS="${GEXTTO_MEDIA_GROUPS:-}"
# Media tools (ffprobe, MediaInfo, mkvmerge) unlock the quality checks and the
# audio-track cleanup. They are installed when the distribution ships them;
# this switch keeps minimal servers free of the extra packages.
SKIP_MEDIA_TOOLS="${GEXTTO_SKIP_MEDIA_TOOLS:-0}"

# Download hardening: HTTPS only (redirects included), a real TLS floor and
# timeouts, so a hung or downgrading mirror cannot block or weaken the install.
CURL_OPTS=(--fail --location --retry 3 --retry-delay 2 --proto '=https'
  --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 300)
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
      --listen ADDR:PORT  web UI listen address (default: 0.0.0.0:PORT;
                          use 127.0.0.1:PORT to keep it on this host only)
      --engine-port PORT  internal engine port (default: 8889)
      --data-dir DIR      service data directory (default: /var/lib/gextto)
      --install-dir DIR   program directory (default: /opt/gextto)
      --user NAME         service user; default: the local user who runs the
                          installer (use "gextto" for the isolated, login-less
                          system account)
      --media-group G     add the service user to group G (repeatable or
                          comma-separated), e.g. the group that owns the NAS
                          media folders
      --local-archive F   install from a local .tar.gz instead of downloading us
      --no-media-tools    do not install ffmpeg, mediainfo and mkvtoolnix

Environment overrides: GEXTTO_DATA_DIR, GEXTTO_PORT, GEXTTO_ENGINE_PORT,
GEXTTO_LISTEN, GEXTTO_USER, GEXTTO_GROUP, GEXTTO_INSTALL_DIR, GEXTTO_REPO,
GEXTTO_RELEASE, GEXTTO_ARCH, GEXTTO_SKIP_PACKAGES, GEXTTO_SKIP_MEDIA_TOOLS,
GEXTTO_LOCAL_ARCHIVE, GEXTTO_NO_START, GEXTTO_HEALTH_TIMEOUT,
GEXTTO_HTTP_TIMEOUT, GEXTTO_MEDIA_GROUPS.
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
      --listen) LISTEN="${2:-}"; [[ -n "$LISTEN" ]] || die "--listen requires a value"; shift 2 ;;
      --data-dir) DATA_DIR="${2:-}"; [[ -n "$DATA_DIR" ]] || die "--data-dir requires a value"; shift 2 ;;
      --install-dir) INSTALL_DIR="${2:-}"; [[ -n "$INSTALL_DIR" ]] || die "--install-dir requires a value"; shift 2 ;;
      --user) SERVICE_USER="${2:-}"; [[ -n "$SERVICE_USER" ]] || die "--user requires a value"; shift 2 ;;
      --media-group) [[ -n "${2:-}" ]] || die "--media-group requires a value"; MEDIA_GROUPS="${MEDIA_GROUPS:+$MEDIA_GROUPS,}$2"; shift 2 ;;
      --gx-torrent) shift ;;
      --no-media-tools) SKIP_MEDIA_TOOLS=1; shift ;;
      --local-archive) LOCAL_ARCHIVE="${2:-}"; [[ -n "$LOCAL_ARCHIVE" ]] || die "--local-archive requires a value"; shift 2 ;;
      *) die "unknown option: $1 (use --help)" ;;
    esac
  done
}

require_root() {
  [[ "$(id -u)" == "0" ]] || die "run as root (or with sudo)"
}

# detect_distro reads /etc/os-release and maps the distribution to the package
# family whose package names we know. The package manager is the fallback for
# derivatives that do not carry an ID_LIKE we recognise.
DISTRO_NAME=""
DISTRO_ID=""
DISTRO_FAMILY=""
BASE_PACKAGES=()
MEDIA_PACKAGES=()
detect_distro() {
  local id="" like="" name=""
  if [[ -r /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    id="${ID:-}"
    like="${ID_LIKE:-}"
    name="${PRETTY_NAME:-${NAME:-}}"
  fi
  DISTRO_NAME="${name:-unknown distribution}"
  DISTRO_ID="${id:-unknown}"
  local key
  key=" ${id,,} ${like,,} "
  case "$key" in
    *" debian "*|*" ubuntu "*|*" linuxmint "*|*" pop "*|*" raspbian "*) DISTRO_FAMILY="debian" ;;
    *" fedora "*|*" rhel "*|*" centos "*|*" rocky "*|*" alma "*|*" amazon "*) DISTRO_FAMILY="fedora" ;;
    *" opensuse "*|*" suse "*|*" sles "*) DISTRO_FAMILY="suse" ;;
    *" arch "*|*" manjaro "*|*" endeavouros "*) DISTRO_FAMILY="arch" ;;
  esac
  if [[ -z "$DISTRO_FAMILY" ]]; then
    if command -v apt-get >/dev/null 2>&1; then
      DISTRO_FAMILY="debian"
    elif command -v dnf >/dev/null 2>&1; then
      DISTRO_FAMILY="fedora"
    elif command -v zypper >/dev/null 2>&1; then
      DISTRO_FAMILY="suse"
    elif command -v pacman >/dev/null 2>&1; then
      DISTRO_FAMILY="arch"
    fi
  fi
  log "distribution: $DISTRO_NAME ($DISTRO_ID, packages: ${DISTRO_FAMILY:-unknown})"
}

# base_packages holds what the installer itself needs. Names differ per family
# for useradd: Debian calls the package passwd, openSUSE and Arch call it shadow,
# Fedora and its derivatives call it shadow-utils.
base_packages() {
  case "$DISTRO_FAMILY" in
    debian) BASE_PACKAGES=(ca-certificates curl tar gzip coreutils passwd) ;;
    fedora) BASE_PACKAGES=(ca-certificates curl tar gzip coreutils shadow-utils) ;;
    suse) BASE_PACKAGES=(ca-certificates curl tar gzip coreutils shadow) ;;
    arch) BASE_PACKAGES=(ca-certificates curl tar gzip coreutils shadow) ;;
    *) BASE_PACKAGES=() ;;
  esac
}

# media_packages are the optional tools Gextto uses to read the real quality of
# a file (ffprobe, MediaInfo) and to clean audio tracks (mkvmerge). They are
# best-effort: their absence only disables those features.
media_packages() {
  if [[ "$SKIP_MEDIA_TOOLS" == "1" ]]; then
    MEDIA_PACKAGES=()
    return
  fi
  case "$DISTRO_FAMILY" in
    debian|fedora|suse) MEDIA_PACKAGES=(ffmpeg mediainfo mkvtoolnix) ;;
    arch) MEDIA_PACKAGES=(ffmpeg mediainfo mkvtoolnix-cli) ;;
    *) MEDIA_PACKAGES=() ;;
  esac
}

install_package_list() {
  case "$DISTRO_FAMILY" in
    debian) apt-get install -y --no-install-recommends "$@" ;;
    fedora) dnf install -y "$@" ;;
    suse) zypper --non-interactive install "$@" ;;
    arch) pacman -S --needed --noconfirm "$@" ;;
    *) return 1 ;;
  esac
}

preflight() {
  # In --dry-run nothing is executed, so missing tools are a warning, not an
  # error: the plan can be reviewed anywhere.
  local fatal=1
  [[ "$DRY_RUN" == "1" ]] && fatal=0
  if ! command -v systemctl >/dev/null 2>&1 || [[ ! -d /run/systemd/system ]]; then
    if [[ "$fatal" == "1" ]]; then
      die "systemd is required (no running systemd detected)"
    else
      warn "systemd is required (no running systemd detected)"
    fi
  fi
}

# has_command finds tools that live in /usr/sbin or /sbin even when the caller's
# PATH does not include those directories (a non-root dry-run, for example).
has_command() {
  command -v "$1" >/dev/null 2>&1 && return 0
  local dir
  for dir in /usr/sbin /sbin /usr/bin /bin; do
    [[ -x "$dir/$1" ]] && return 0
  done
  return 1
}

# require_commands runs after install_packages: a minimal system may start
# without curl, tar or useradd, which the distribution packages provide.
require_commands() {
  local fatal=1
  [[ "$DRY_RUN" == "1" ]] && fatal=0
  local cmd
  for cmd in sha256sum install id useradd realpath curl tar gzip; do
    if ! has_command "$cmd"; then
      if [[ "$fatal" == "1" ]]; then
        die "missing required command: $cmd (install it and run the installer again)"
      else
        warn "missing required command: $cmd"
      fi
    fi
  done
}

# install_packages is best-effort: a single missing optional package must never
# abort the installation (for example a media tool absent from the configured
# repositories). The base list and the media list are separate so a missing
# optional package cannot keep curl or tar from being installed.
install_packages() {
  detect_distro
  if [[ "${GEXTTO_SKIP_PACKAGES:-0}" == "1" ]]; then
    log "skipping system packages"
    return
  fi
  base_packages
  media_packages
  if [[ -z "$DISTRO_FAMILY" ]]; then
    warn "unknown distribution: install curl, tar, sha256sum, useradd and a media prober manually"
    return
  fi
  if [[ "$DRY_RUN" == "1" ]]; then
    log "[dry-run] install system packages: ${BASE_PACKAGES[*]}"
    if [[ ${#MEDIA_PACKAGES[@]} -gt 0 ]]; then
      log "[dry-run] install media tools: ${MEDIA_PACKAGES[*]}"
    fi
    return
  fi
  if [[ "$DISTRO_FAMILY" == "debian" ]]; then
    apt-get update || warn "apt-get update failed; continuing"
  fi
  log "installing system packages: ${BASE_PACKAGES[*]}"
  install_package_list "${BASE_PACKAGES[@]}" \
    || warn "some system packages could not be installed; continuing"
  if [[ ${#MEDIA_PACKAGES[@]} -gt 0 ]]; then
    log "installing media tools: ${MEDIA_PACKAGES[*]}"
    local media_failed=0 pkg
    for pkg in "${MEDIA_PACKAGES[@]}"; do
      install_package_list "$pkg" || media_failed=1
    done
    [[ "$media_failed" == "0" ]] \
      || warn "some media tools could not be installed; quality checks and audio cleanup stay limited"
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
    curl "${CURL_OPTS[@]}" "${base}/${asset}?cachebust=${cachebust}" -o "$work/$asset" \
      || die "unable to download ${asset} from ${base}"
    curl "${CURL_OPTS[@]}" "${base}/${asset}.sha256?cachebust=${cachebust}" -o "$work/${asset}.sha256" \
      || die "unable to download checksum for ${asset}"
    (cd "$work" && sha256sum -c "${asset}.sha256") \
      || die "checksum verification failed for ${asset}"
  fi

  # --no-same-owner: the archive is checksum-verified, but extracting as root
  # must never hand files the numeric owner stored at build time.
  tar --no-same-owner -xzf "$work/$asset" -C "$work"
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
    SERVICE_GROUP="$SERVICE_USER"
    # Remember that we created it, so --purge removes only our own account and
    # never a pre-existing system user that happened to share the name.
    if [[ "$DRY_RUN" != "1" ]]; then
      install -d /etc/gextto
      printf '%s\n' "$SERVICE_USER" > /etc/gextto/created-user
    fi
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

# resolve_service_identity picks the service user and its primary group. With
# no --user/GEXTTO_USER the local user who invoked the installer wins (SUDO_USER),
# falling back to a dedicated "gextto" account for unattended/root runs.
resolve_service_identity() {
  # Running the installer again (an update, a repair) keeps the user of the
  # installed service: switching account would re-own the whole data folder.
  # Only an explicit --user/GEXTTO_USER changes it.
  if [[ -z "$SERVICE_USER" && -f "$UNIT_PATH" ]]; then
    SERVICE_USER="$(sed -n 's/^User=//p' "$UNIT_PATH" | head -n 1)"
    [[ -z "$SERVICE_GROUP" ]] && SERVICE_GROUP="$(sed -n 's/^Group=//p' "$UNIT_PATH" | head -n 1)"
  fi
  if [[ -z "$SERVICE_USER" ]]; then
    if [[ -n "${SUDO_USER:-}" && "${SUDO_USER}" != "root" ]]; then
      SERVICE_USER="$SUDO_USER"
    else
      SERVICE_USER="gextto"
    fi
  fi
  if [[ -n "$SERVICE_GROUP" ]]; then
    return 0
  fi
  if id -u "$SERVICE_USER" >/dev/null 2>&1; then
    SERVICE_GROUP="$(id -gn "$SERVICE_USER" 2>/dev/null || echo "$SERVICE_USER")"
  else
    SERVICE_GROUP="$SERVICE_USER"
  fi
}

install_files() {
  local work="$1"
  run install -d -o "$SERVICE_USER" -g "$SERVICE_GROUP" "$DATA_DIR"
  # Switching service user (e.g. from the isolated "gextto" account to the
  # local user) must leave the existing data writable: re-own it when the
  # owner differs, so databases and state keep working after the switch.
  if [[ "$DRY_RUN" != "1" && -d "$DATA_DIR" ]]; then
    local data_owner
    data_owner="$(stat -c '%U' "$DATA_DIR" 2>/dev/null || true)"
    if [[ -n "$data_owner" && "$data_owner" != "$SERVICE_USER" ]]; then
      log "data directory owner $data_owner -> $SERVICE_USER (chown -R)"
      run chown -R "$SERVICE_USER:$SERVICE_GROUP" "$DATA_DIR"
    fi
  fi
  run install -d "$INSTALL_DIR"

  # Keep the previous binaries so a failed upgrade can be rolled back: the
  # engine supervisor restores gx-torrent.prev before switching engines, as
  # after `gexttod --update`.
  if [[ "$DRY_RUN" != "1" && -f "$INSTALL_DIR/gexttod" ]]; then
    cp -a "$INSTALL_DIR/gexttod" "$INSTALL_DIR/gexttod.prev"
  fi
  if [[ "$DRY_RUN" != "1" && -f "$INSTALL_DIR/gx-torrent" ]]; then
    cp -a "$INSTALL_DIR/gx-torrent" "$INSTALL_DIR/gx-torrent.prev"
  fi

  run install -m 0755 "$work/gexttod" "$INSTALL_DIR/gexttod"
  # gx-torrent is the torrent engine: Gextto starts it from here in managed
  # mode. Its absence makes Gextto report the engine unavailable and run its
  # fallback chain instead.
  if [[ -f "$work/gx-torrent" ]]; then
    run install -m 0755 "$work/gx-torrent" "$INSTALL_DIR/gx-torrent"
  else
    warn "gx-torrent is missing from the payload: Gextto will use its fallback engine"
  fi
  # gx-ed2k was retired in favour of aMule: drop the binary an older
  # release installed.
  run rm -f "$INSTALL_DIR/gx-ed2k" "$INSTALL_DIR/gx-ed2k.prev"
  [[ -f "$work/run.sh" ]] && run install -m 0755 "$work/run.sh" "$INSTALL_DIR/run.sh"
  [[ -f "$work/VERSION" ]] && run install -m 0644 "$work/VERSION" "$INSTALL_DIR/VERSION"
  # Licence and third-party notices of the shipped binaries.
  [[ -f "$work/LICENSE" ]] && run install -m 0644 "$work/LICENSE" "$INSTALL_DIR/LICENSE"
  [[ -f "$work/THIRD-PARTY-NOTICES" ]] && run install -m 0644 "$work/THIRD-PARTY-NOTICES" "$INSTALL_DIR/THIRD-PARTY-NOTICES"

  # Replace the bundled libraries cleanly: leftovers from a previous version
  # must not shadow the new ones.
  if [[ "$DRY_RUN" != "1" ]]; then
    rm -rf "${INSTALL_DIR:?}/lib"
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
Group=$SERVICE_GROUP
EnvironmentFile=-/etc/gextto/gextto.env
Environment=GEXTTO_DATA_DIR=$DATA_DIR
Environment=GEXTTO_LISTEN=$LISTEN
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

# service_health watches the service after a restart. Only an HTTP answer
# proves the new version works: a simple unit is "active" the moment its
# process starts, so a version crashing a few seconds later looked fine. Any
# moment outside "active" (Restart=on-failure turns a crash into
# "activating") is a crash. Prints healthy, crashed or slow.
service_health() {
  local waited=0 state code
  while (( waited < HTTP_TIMEOUT )); do
    state="$(systemctl is-active "$SERVICE_NAME" 2>/dev/null || true)"
    if [[ "$state" != "active" ]]; then
      echo crashed
      return 0
    fi
    # Any answer below 500 counts: with the login on, /api/status may be 401.
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "http://127.0.0.1:$PORT/api/status" || true)"
    if [[ "$code" =~ ^[1-4][0-9][0-9]$ ]]; then
      echo healthy
      return 0
    fi
    sleep 2
    waited=$((waited + 2))
  done
  echo slow
}

# restart_and_verify restarts the service and checks that it answers; when the
# new binary crashes it rolls back to the previous one.
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

  case "$(service_health)" in
    healthy)
      log "gextto is running and the web UI answers on port $PORT"
      return 0
      ;;
    slow)
      # Alive but not answering yet (a long database migration): a rollback
      # would interrupt it.
      warn "gextto is running but the web UI did not answer within ${HTTP_TIMEOUT}s; inspect: journalctl -u $SERVICE_NAME -n 100"
      return 0
      ;;
  esac

  if [[ -f "$INSTALL_DIR/gexttod.prev" ]]; then
    warn "gextto did not start; rolling back to the previous binary"
    install -m 0755 "$INSTALL_DIR/gexttod.prev" "$INSTALL_DIR/gexttod"
    systemctl daemon-reload
    systemctl restart "$SERVICE_NAME" || true
    if [[ "$(service_health)" == "crashed" ]]; then
      warn "rollback did not restore a running service"
    else
      warn "rollback successful: the previous version is running again"
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
    # Remove the service account only when this installer created it (the
    # marker is read before /etc/gextto is removed).
    local created_user=""
    [[ -f /etc/gextto/created-user ]] && created_user="$(cat /etc/gextto/created-user 2>/dev/null || true)"
    if [[ -n "$created_user" ]] && id -u "$created_user" >/dev/null 2>&1; then
      run userdel "$created_user" 2>/dev/null || warn "could not remove user $created_user"
      run groupdel "$created_user" 2>/dev/null || true
    fi
    # Environment overrides, if the operator added any.
    run rm -rf /etc/gextto
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
  log "UI:      http://${lan_ip:-127.0.0.1}:$PORT  (listening on $LISTEN)"
  log "first run: open the UI, the setup wizard guides you through folders, sources and the first title"
  log "status:  systemctl status $SERVICE_NAME"
  log "logs:    journalctl -u $SERVICE_NAME -f"
  # Only warn about the open UI when it is actually reachable off-host.
  case "$LISTEN" in
    127.*|localhost:*|"[::1]:"*) ;;
    *) warn "the web UI is open until a password is set: the setup wizard asks for one at the first visit (for access from the internet use an HTTPS reverse proxy, see docs/SECURITY.md)" ;;
  esac
}

main() {
  parse_args "$@"
  resolve_service_identity
  # Resolve the listen address. --listen (or GEXTTO_LISTEN) may carry its own
  # port, and the health check below must poll the port actually bound.
  if [[ -n "$LISTEN" ]]; then
    PORT="${LISTEN##*:}"
    [[ "$PORT" =~ ^[0-9]+$ ]] || die "--listen must end with a port: $LISTEN"
  else
    LISTEN="0.0.0.0:$PORT"
  fi
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
  log "service user: $SERVICE_USER (group $SERVICE_GROUP)"
  preflight
  install_packages
  require_commands
  log "build: gx-torrent (pure Go)"

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
