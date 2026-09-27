#!/usr/bin/env bash
# gextto installer: downloads the latest published continuous payload and
# installs it as a systemd service. Supports Debian, Ubuntu, Fedora, openSUSE
# and Arch Linux.
#
#   curl -fsSL .../install.sh | bash
#
# Overrides: GEXTTO_DATA_DIR, GEXTTO_PORT, GEXTTO_ENGINE_PORT, GEXTTO_USER,
# GEXTTO_INSTALL_DIR, GEXTTO_REPO, GEXTTO_RELEASE, GEXTTO_ARCH,
# GEXTTO_SKIP_PACKAGES.
set -euo pipefail

INSTALL_DIR="${GEXTTO_INSTALL_DIR:-/opt/gextto}"
DATA_DIR="${GEXTTO_DATA_DIR:-/var/lib/gextto}"
PORT="${GEXTTO_PORT:-5000}"
ENGINE_PORT="${GEXTTO_ENGINE_PORT:-8889}"
SERVICE_USER="${GEXTTO_USER:-gextto}"
REPO="${GEXTTO_REPO:-buzzqw/gextto}"
RELEASE="${GEXTTO_RELEASE:-continuous}"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

install_packages() {
  [[ "${GEXTTO_SKIP_PACKAGES:-0}" == "1" ]] && { log "skipping system packages"; return; }
  if command -v apt-get >/dev/null; then
    apt-get update -y
    apt-get install -y ca-certificates curl tar coreutils openssl libstdc++6 libssl3 zlib1g zstd
  elif command -v dnf >/dev/null; then
    dnf install -y ca-certificates curl tar coreutils openssl openssl-libs libstdc++ zlib zstd
  elif command -v zypper >/dev/null; then
    zypper --non-interactive install ca-certificates curl tar coreutils openssl libstdc++6 zlib zstd
  elif command -v pacman >/dev/null; then
    pacman -Sy --noconfirm ca-certificates curl tar coreutils openssl zlib zstd
  else
    log "unknown distribution: install curl, tar, sha256sum, OpenSSL and the runtime C++/zlib libraries manually"
  fi
}

download_payload() {
  local work="$1"
  local arch="${GEXTTO_ARCH:-$(uname -m)}"
  case "$arch" in
    x86_64|amd64) arch=x86_64 ;;
    aarch64|arm64) arch=aarch64 ;;
    *) die "unsupported architecture: $arch (available assets: x86_64, aarch64)" ;;
  esac

  local asset="gextto-linux-${arch}.tar.gz"
  local base
  case "$RELEASE" in
    latest|stable) base="https://github.com/${REPO}/releases/latest/download" ;;
    *) base="https://github.com/${REPO}/releases/download/${RELEASE}" ;;
  esac

  log "downloading ${REPO} ${RELEASE} (${arch})"
  local cachebust; cachebust="$(date +%s)"
  curl -fL --retry 3 --retry-delay 2 "${base}/${asset}?cachebust=${cachebust}" -o "$work/$asset" \
    || die "unable to download ${asset} from ${base}"
  curl -fL --retry 3 --retry-delay 2 "${base}/${asset}.sha256?cachebust=${cachebust}" -o "$work/${asset}.sha256" \
    || die "unable to download checksum for ${asset}"
  (cd "$work" && sha256sum -c "${asset}.sha256") \
    || die "checksum verification failed for ${asset}"
  tar -xzf "$work/$asset" -C "$work"
  [[ -x "$work/gexttod" ]] || die "published payload does not contain an executable gexttod"
}

main() {
  [[ "$(id -u)" == "0" ]] || die "run as root"
  install_packages

  log "installing published gextto payload"
  local work; work="$(mktemp -d)"
  trap 'rm -rf "$work"' EXIT
  download_payload "$work"

  id -u "$SERVICE_USER" >/dev/null 2>&1 || useradd --system --home "$DATA_DIR" --shell /usr/sbin/nologin "$SERVICE_USER"
  install -d -o "$SERVICE_USER" -g "$SERVICE_USER" "$DATA_DIR"
  install -d "$INSTALL_DIR"
  install -m 0755 "$work/gexttod" "$INSTALL_DIR/gexttod"
  install -m 0755 "$work/run.sh" "$INSTALL_DIR/run.sh"
  install -m 0644 "$work/VERSION" "$INSTALL_DIR/VERSION"

  # Generate an API token on a fresh install: the daemon binds 0.0.0.0 by
  # default, so an unauthenticated API would be exposed to the whole network.
  # The token is stored in a root-readable EnvironmentFile, not in the world
  # readable unit.
  API_TOKEN="${GEXTTO_API_TOKEN:-}"
  if [[ -z "$API_TOKEN" ]]; then
    if command -v openssl >/dev/null; then
      API_TOKEN="$(openssl rand -hex 24)"
    else
      API_TOKEN="$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
    fi
  fi
  install -d -m 0750 /etc/gextto
  printf 'GEXTTO_API_TOKEN=%s\n' "$API_TOKEN" > /etc/gextto/gextto.env
  chmod 0640 /etc/gextto/gextto.env
  chown "root:$SERVICE_USER" /etc/gextto/gextto.env 2>/dev/null || true

  # Install the bundled libtorrent next to the binary ($ORIGIN/lib rpath).
  install -d "$INSTALL_DIR/lib"
  cp -a "$work/lib/." "$INSTALL_DIR/lib/"

  install -d /etc/systemd/system
  sed -e "s#GEXTTO_DATA_DIR=.*#GEXTTO_DATA_DIR=$DATA_DIR#" \
      -e "s#GEXTTO_LISTEN=.*#GEXTTO_LISTEN=0.0.0.0:$PORT#" \
      -e "s#GEXTTO_ENGINE_LISTEN=.*#GEXTTO_ENGINE_LISTEN=127.0.0.1:$ENGINE_PORT#" \
      -e "s#User=.*#User=$SERVICE_USER#" \
      -e "s#Group=.*#Group=$SERVICE_USER#" \
      -e "s#WorkingDirectory=.*#WorkingDirectory=$DATA_DIR#" \
      -e "s#ExecStart=.*#ExecStart=$INSTALL_DIR/gexttod#" \
      "$(dirname "$0")/systemd/gextto.service" > /etc/systemd/system/gextto.service 2>/dev/null || \
  cat > /etc/systemd/system/gextto.service <<UNIT
[Unit]
Description=Gextto (gextto) media acquisition and archiving daemon
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
TimeoutStopSec=90

[Install]
WantedBy=multi-user.target
UNIT

  systemctl daemon-reload
  systemctl enable --now gextto.service
  log "gextto installed and started"
  log "UI: http://127.0.0.1:$PORT   status: systemctl status gextto.service"
  log "API token saved in /etc/gextto/gextto.env (enter it in the UI when prompted)"
}

main "$@"
