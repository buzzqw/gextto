#!/usr/bin/env bash
# Build the standalone Linux archive consumed by the installer and by
# `gexttod --update`. The Go binary embeds the server-rendered web UI, so the archive
# ships the daemon, the bundled libtorrent shared libraries, a launcher, a short
# README and the licence notices. Layout:
#   gexttod  gx-torrent  run.sh  lib/  README.md  VERSION  LICENSE  THIRD-PARTY-NOTICES
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BINARY="${ROOT}/bin/gexttod"
OUTPUT=""
ARCH="$(uname -m)"
CHANNEL="${GEXTTO_CHANNEL:-continuous}"
LABEL=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary|--output|--arch|--label)
      option="$1"
      [[ $# -ge 2 && -n "$2" ]] || { echo "$option requires a value" >&2; exit 2; }
      case "$option" in
        --binary) BINARY="$2" ;;
        --output) OUTPUT="$2" ;;
        --arch) ARCH="$2" ;;
        --label) LABEL="$2" ;;
      esac
      shift 2
      ;;
    -h|--help) echo "usage: package-linux.sh [--binary PATH] [--output FILE] [--arch ARCH] [--label TEXT]"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

[[ -n "$LABEL" ]] || LABEL="$CHANNEL"

if [[ ! -x "$BINARY" ]]; then
  echo "binary not found, building: $BINARY"
  GEXTTO_BINARY="$BINARY" "${ROOT}/scripts/build-daemon.sh"
fi

[[ -z "$OUTPUT" ]] && OUTPUT="${ROOT}/gextto-linux-${ARCH}.tar.gz"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

cp "$BINARY" "$STAGE/gexttod"
chmod +x "$STAGE/gexttod"
# gx-torrent is the default torrent engine: the archive must ship it next to
# gexttod or a fresh install would fall back to libtorrent. build-daemon.sh
# builds it by default; refuse to package without it unless explicitly allowed.
GX_BINARY="$(dirname "$BINARY")/gx-torrent"
if [[ -x "$GX_BINARY" ]]; then
  cp "$GX_BINARY" "$STAGE/gx-torrent"
  chmod +x "$STAGE/gx-torrent"
elif [[ "${GEXTTO_ALLOW_MISSING_GXTORRENT:-0}" == "1" ]]; then
  echo "warning: gx-torrent not found next to $BINARY; the archive will only run with embedded libtorrent" >&2
else
  echo "error: gx-torrent binary not found at $GX_BINARY" >&2
  echo "       build it with scripts/build-daemon.sh, or set GEXTTO_ALLOW_MISSING_GXTORRENT=1 to package without it" >&2
  exit 1
fi
mkdir -p "$STAGE/lib"

# Bundle the libtorrent shared libraries next to the executable: the daemon is
# linked with an $ORIGIN/lib rpath, so it runs straight from the archive.
LIBDIR="$(pkg-config --variable=libdir libtorrent-rasterbar 2>/dev/null || echo /usr/lib)"
LIBTORRENT="libtorrent-rasterbar.so"
BUNDLED_LIB=0
for candidate in "$LIBDIR/$LIBTORRENT" "/usr/lib/$LIBTORRENT" "/usr/lib/x86_64-linux-gnu/$LIBTORRENT" "/usr/local/lib/$LIBTORRENT"; do
  if [[ -e "$candidate" ]]; then
    cp -a "$candidate"* "$STAGE/lib/" 2>/dev/null || true
    BUNDLED_LIB=1
    break
  fi
done
if [[ "$BUNDLED_LIB" == "0" ]]; then
  echo "warning: $LIBTORRENT not found; the archive only runs where libtorrent is installed system-wide" >&2
fi

cat > "$STAGE/run.sh" <<'RUN'
#!/usr/bin/env bash
# Launch gextto from an extracted archive or install it as a system service.
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
export LD_LIBRARY_PATH="$DIR/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
export GEXTTO_DATA_DIR="${GEXTTO_DATA_DIR:-$DIR/data}"

if [[ -t 0 && $# -eq 0 ]]; then
  echo "================================================="
  echo " Gextto — Media Acquisition & Archiving"
  echo "================================================="
  echo "1) Avvia Gextto direttamente / Start Gextto directly"
  echo "2) Installa il servizio systemd / Install systemd service (gextto.service)"
  read -r -p "Scelta / Choice [1/2] (default 1): " choice
  if [[ "$choice" == "2" ]]; then
    if [[ -x "$DIR/install-service.sh" ]]; then
      if [[ "$(id -u)" != "0" ]]; then
        exec sudo "$DIR/install-service.sh"
      else
        exec "$DIR/install-service.sh"
      fi
    fi
  fi
fi

exec "$DIR/gexttod" "$@"
RUN
chmod +x "$STAGE/run.sh"

cat > "$STAGE/install-service.sh" <<'INSTALL'
#!/usr/bin/env bash
# Install Gextto as a systemd system service. Run as root:
#   sudo ./install-service.sh
set -euo pipefail
if [[ "$(id -u)" != "0" ]]; then
  echo "error: run as root (sudo ./install-service.sh)" >&2
  exit 1
fi
command -v systemctl >/dev/null 2>&1 || { echo "error: systemd not found" >&2; exit 1; }
DIR="$(cd "$(dirname "$0")" && pwd)"
for file in gexttod gx-torrent run.sh gextto.service; do
  [[ -e "$DIR/$file" ]] || { echo "error: $file not found next to this script" >&2; exit 1; }
done

SERVICE_USER="${SUDO_USER:-gextto}"
if [[ "$SERVICE_USER" == "root" ]]; then
  SERVICE_USER="gextto"
fi
if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
  useradd -r -s /usr/sbin/nologin -d /var/lib/gextto gextto
  SERVICE_USER="gextto"
fi
SERVICE_GROUP="$(id -gn "$SERVICE_USER" 2>/dev/null || echo "$SERVICE_USER")"

install -d -o "$SERVICE_USER" -g "$SERVICE_GROUP" /opt/gextto /var/lib/gextto
cp -a "$DIR/." /opt/gextto/
chown -R "$SERVICE_USER:$SERVICE_GROUP" /opt/gextto
chmod +x /opt/gextto/gexttod /opt/gextto/gx-torrent /opt/gextto/run.sh 2>/dev/null || true

sed -e "s/^User=.*/User=$SERVICE_USER/" -e "s/^Group=.*/Group=$SERVICE_GROUP/" \
  "$DIR/gextto.service" > /etc/systemd/system/gextto.service

systemctl daemon-reload
systemctl enable --now gextto
echo "Gextto service installed and started (web UI on port 5000)."
INSTALL
chmod +x "$STAGE/install-service.sh"

cat > "$STAGE/uninstall-service.sh" <<'UNINSTALL'
#!/usr/bin/env bash
# Remove the Gextto systemd service. Data under /var/lib/gextto is kept.
# Run as root: sudo ./uninstall-service.sh
set -euo pipefail
if [[ "$(id -u)" != "0" ]]; then
  echo "error: run as root (sudo ./uninstall-service.sh)" >&2
  exit 1
fi
systemctl disable --now gextto 2>/dev/null || true
rm -f /etc/systemd/system/gextto.service
systemctl daemon-reload
echo "Gextto service removed (data in /var/lib/gextto kept)."
UNINSTALL
chmod +x "$STAGE/uninstall-service.sh"

cat > "$STAGE/gextto.service" <<'UNIT'
[Unit]
Description=Gextto media acquisition and archiving daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=gextto
Group=gextto
StateDirectory=gextto
EnvironmentFile=-/etc/gextto/gextto.env
Environment=GEXTTO_DATA_DIR=/var/lib/gextto
Environment=GEXTTO_LISTEN=0.0.0.0:5000
Environment=GEXTTO_ENGINE_LISTEN=127.0.0.1:8889
Environment=GEXTTO_LOG=info
WorkingDirectory=/var/lib/gextto
ExecStart=/opt/gextto/run.sh
Restart=on-failure
RestartSec=5
LimitNOFILE=65536
TimeoutStopSec=infinity

[Install]
WantedBy=multi-user.target
UNIT

cp "${ROOT}/README.md" "$STAGE/README.md" 2>/dev/null || true
cp "${ROOT}/README.it.md" "$STAGE/README.it.md" 2>/dev/null || true
# Fail rather than ship a release with the wrong or a missing licence: every
# package must carry the project's own EUPL 1.2 text.
grep -q "EUROPEAN UNION PUBLIC LICENCE v. 1.2" "${ROOT}/LICENSE" \
  || { echo "LICENSE does not contain the EUPL 1.2 text; refusing to package" >&2; exit 1; }
cp "${ROOT}/LICENSE" "$STAGE/LICENSE"
cp "${ROOT}/THIRD-PARTY-NOTICES" "$STAGE/THIRD-PARTY-NOTICES"
echo "$LABEL" > "$STAGE/VERSION"

tar --numeric-owner -C "$STAGE" -czf "$OUTPUT" .
( cd "$(dirname "$OUTPUT")" && sha256sum "$(basename "$OUTPUT")" > "$(basename "$OUTPUT").sha256" )

echo "archive: $OUTPUT"
echo "checksum: $OUTPUT.sha256"
