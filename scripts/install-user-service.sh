#!/usr/bin/env bash
# Install (or refresh) gextto as a systemd *user* service, without root.
#
# This is the deployment used when the daemon runs as a normal login user
# instead of a dedicated system account created by install.sh. It builds the
# binary from this checkout, writes ~/.config/systemd/user/gextto.service and
# enables/restarts it.
#
# Overrides: GEXTTO_DATA_DIR, GEXTTO_PORT, GEXTTO_LISTEN, GEXTTO_ENGINE_PORT,
# GEXTTO_ACTIVE, GEXTTO_DRY_RUN, GEXTTO_LOG, GEXTTO_BINARY.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BINARY="${GEXTTO_BINARY:-$ROOT/bin/gexttod}"
DATA_DIR="${GEXTTO_DATA_DIR:-$HOME/gextto-data}"
PORT="${GEXTTO_PORT:-5000}"
# Gextto is meant to be used over the LAN from the other PCs of the same owner,
# so the service listens on every interface by default. Restrict access with a
# firewall or reverse proxy when the network is not fully trusted.
LISTEN="${GEXTTO_LISTEN:-0.0.0.0:$PORT}"
ENGINE_PORT="${GEXTTO_ENGINE_PORT:-8889}"
ACTIVE="${GEXTTO_ACTIVE:-1}"
DRY_RUN="${GEXTTO_DRY_RUN:-0}"
LOG="${GEXTTO_LOG:-info}"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
UNIT="$UNIT_DIR/gextto.service"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }

[[ -x "$BINARY" ]] || { log "building $BINARY"; ( cd "$ROOT" && make build ); }

mkdir -p "$UNIT_DIR" "$DATA_DIR"

cat > "$UNIT" <<UNIT
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
Environment=GEXTTO_DRY_RUN=$DRY_RUN
Environment=GEXTTO_LIBTORRENT=1
Environment=GEXTTO_LOG=$LOG
Restart=on-failure
RestartSec=5
LimitNOFILE=65536
TimeoutStopSec=90

[Install]
WantedBy=default.target
UNIT

systemctl --user daemon-reload
systemctl --user enable gextto.service
systemctl --user restart gextto.service
log "gextto user service installed: $UNIT"
log "UI bind: $LISTEN   status: systemctl --user status gextto.service"
