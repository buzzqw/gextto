#!/usr/bin/env bash
# gextto installer: installs build dependencies, builds the Go daemon and the
# embedded web UI, then installs it as a systemd service. Supports Debian,
# Ubuntu, Fedora, openSUSE and Arch Linux.
#
#   curl -fsSL .../install.sh | bash
#
# Overrides: GEXTTO_DATA_DIR, GEXTTO_PORT, GEXTTO_ENGINE_PORT, GEXTTO_USER,
# GEXTTO_INSTALL_DIR, GEXTTO_PORT, GEXTTO_SKIP_PACKAGES,
# GEXTTO_SKIP_LIBTORRENT_BUILD.
set -euo pipefail

INSTALL_DIR="${GEXTTO_INSTALL_DIR:-/opt/gextto}"
DATA_DIR="${GEXTTO_DATA_DIR:-/var/lib/gextto}"
PORT="${GEXTTO_PORT:-5000}"
ENGINE_PORT="${GEXTTO_ENGINE_PORT:-8889}"
SERVICE_USER="${GEXTTO_USER:-gextto}"
GO_MIN="1.26"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

install_packages() {
  [[ "${GEXTTO_SKIP_PACKAGES:-0}" == "1" ]] && { log "skipping system packages"; return; }
  if command -v apt-get >/dev/null; then
    apt-get update -y
    apt-get install -y build-essential pkg-config libssl-dev zlib1g-dev libbz2-dev \
      libboost-dev libboost-system-dev libboost-python-dev libtorrent-rasterbar-dev curl tar
  elif command -v dnf >/dev/null; then
    dnf install -y gcc-c++ make pkgconfig openssl-devel zlib-devel bzip2-devel \
      boost-devel libtorrent-rasterbar-devel curl tar
  elif command -v zypper >/dev/null; then
    zypper --non-interactive install gcc-c++ make pkg-config libopenssl-devel \
      zlib-devel libbz2-devel boost-devel libtorrent-rasterbar-devel curl tar
  elif command -v pacman >/dev/null; then
    pacman -Sy --noconfirm base-devel pkgconf openssl zlib bzip2 boost libtorrent-rasterbar curl tar
  else
    log "unknown distribution: install a C++ toolchain, libtorrent-rasterbar and Go manually"
  fi
}

ensure_go() {
  if command -v go >/dev/null; then
    local have
    have="$(go env GOVERSION 2>/dev/null | sed 's/^go//')"
    if [[ -n "$have" ]] && printf '%s\n%s\n' "$GO_MIN" "$have" | sort -V -C; then
      log "Go $have found (>= $GO_MIN)"
      return
    fi
    log "Go ${have:-unknown} is older than $GO_MIN; installing a newer toolchain"
  else
    log "installing Go"
  fi
  local arch; arch="$(uname -m)"
  case "$arch" in
    x86_64) arch=amd64 ;;
    aarch64) arch=arm64 ;;
  esac
  local version="1.26.0"
  curl -fsSL "https://go.dev/dl/go${version}.linux-${arch}.tar.gz" -o /tmp/go.tar.gz
  rm -rf /usr/local/go
  tar -C /usr/local -xzf /tmp/go.tar.gz
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
}

main() {
  [[ "$(id -u)" == "0" ]] || die "run as root"
  install_packages
  ensure_go

  log "building gextto"
  local work; work="$(mktemp -d)"
  trap 'rm -rf "$work"' EXIT
  # Build from the current checkout when run in-tree, otherwise fetch the source.
  if [[ -f "$(dirname "$0")/go.mod" ]]; then
    cp -a "$(dirname "$0")" "$work/src"
  else
    curl -fsSL "${GEXTTO_SOURCE_URL:-https://github.com/buzzqw/gextto/archive/refs/heads/main.tar.gz}" \
      | tar -xz -C "$work" --strip-components=1
  fi
  chmod +x "$work/src/scripts/build-daemon.sh" "$work/src/scripts/next-build-number.sh" 2>/dev/null || true
  GEXTTO_BINARY="$work/gexttod" GEXTTO_BUMP_BUILD=1 "$work/src/scripts/build-daemon.sh"

  id -u "$SERVICE_USER" >/dev/null 2>&1 || useradd --system --home "$DATA_DIR" --shell /usr/sbin/nologin "$SERVICE_USER"
  install -d -o "$SERVICE_USER" -g "$SERVICE_USER" "$DATA_DIR"
  install -d "$INSTALL_DIR"
  install -m 0755 "$work/gexttod" "$INSTALL_DIR/gexttod"
  echo "source-main" > "$INSTALL_DIR/VERSION"

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

  # Bundle libtorrent next to the binary ($ORIGIN/lib rpath).
  install -d "$INSTALL_DIR/lib"
  local libdir; libdir="$(pkg-config --variable=libdir libtorrent-rasterbar 2>/dev/null || echo /usr/lib)"
  for candidate in "$libdir"/libtorrent-rasterbar.so* /usr/lib/libtorrent-rasterbar.so* /usr/lib/x86_64-linux-gnu/libtorrent-rasterbar.so*; do
    [[ -e "$candidate" ]] && cp -a "$candidate" "$INSTALL_DIR/lib/" && break
  done

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
