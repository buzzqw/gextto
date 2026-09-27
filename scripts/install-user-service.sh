#!/usr/bin/env bash
# Install (or refresh) gextto as a systemd *user* service, without root.
#
# This is the deployment used when the daemon runs as a normal login user
# instead of a dedicated system account created by install.sh. It builds the
# binary from this checkout, writes ~/.config/systemd/user/gextto.service and
# enables/restarts it.
#
# Overrides: GEXTTO_DATA_DIR, GEXTTO_PORT, GEXTTO_ENGINE_PORT, GEXTTO_ACTIVE,
# GEXTTO_DRY_RUN, GEXTTO_LOG, GEXTTO_BINARY.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BINARY="${GEXTTO_BINARY:-$ROOT/bin/gexttod}"
DATA_DIR="${GEXTTO_DATA_DIR:-$HOME/gextto-data}"
PORT="${GEXTTO_PORT:-5000}"
ENGINE_PORT="${GEXTTO_ENGINE_PORT:-8889}"
ACTIVE="${GEXTTO_ACTIVE:-1}"
DRY_RUN="${GEXTTO_DRY_RUN:-0}"
LOG="${GEXTTO_LOG:-info}"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
UNIT="$UNIT_DIR/gextto.service"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }

[[ -x "$BINARY" ]] || { log "building $BINARY"; ( cd "$ROOT" && make build ); }

# Optional API token: use the provided one, or generate one when asked. Without
# a token the API is unauthenticated, which is only safe on loopback.
API_TOKEN="${GEXTTO_API_TOKEN:-}"
if [[ -z "$API_TOKEN" && "${GEXTTO_GENERATE_TOKEN:-0}" == "1" ]]; then
    API_TOKEN="$(openssl rand -hex 24 2>/dev/null || head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
fi
TOKEN_ENV=""
[[ -n "$API_TOKEN" ]] && TOKEN_ENV="Environment=GEXTTO_API_TOKEN=$API_TOKEN"
if [[ -z "$API_TOKEN" && "$PORT" != "127.0.0.1:"* ]]; then
    log "warning: listening on $PORT without GEXTTO_API_TOKEN; the API is reachable without authentication"
fi

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
Environment=GEXTTO_LISTEN=0.0.0.0:$PORT
Environment=GEXTTO_ENGINE_LISTEN=127.0.0.1:$ENGINE_PORT
Environment=GEXTTO_ACTIVE=$ACTIVE
Environment=GEXTTO_DRY_RUN=$DRY_RUN
Environment=GEXTTO_LIBTORRENT=1
Environment=GEXTTO_LOG=$LOG
$TOKEN_ENV
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
log "UI: http://127.0.0.1:$PORT   status: systemctl --user status gextto.service"
