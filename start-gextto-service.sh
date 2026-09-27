#!/usr/bin/env bash
# Build the daemon from this checkout and install it as a systemd *system*
# service. For a production install use install.sh, which also creates the
# dedicated service account; this script is aimed at a local server where the
# checkout is the source of truth.
#
# For a per-user service without root, use scripts/install-user-service.sh.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
UNIT_NAME="gextto.service"
BIN="$ROOT/bin/gexttod"
DATA_DIR="${GEXTTO_DATA_DIR:-$ROOT/data}"
UNIT_SOURCE="$ROOT/systemd/$UNIT_NAME"

# Escape \, # and & so paths interpolate safely into the sed replacements below.
sed_escape() { printf '%s' "$1" | sed -e 's/[\\&#]/\\&/g'; }

LEGACY_SERVICE="${GEXTTO_LEGACY_SERVICE:-}"
if [[ -n "$LEGACY_SERVICE" ]] && systemctl is-active --quiet "$LEGACY_SERVICE"; then
    echo "The legacy service '$LEGACY_SERVICE' is active and uses the same ports. Stop it first: sudo systemctl stop $LEGACY_SERVICE" >&2
    exit 1
fi

if [[ ! -x "$BIN" ]] || find "$ROOT"/*.go "$ROOT"/internal "$ROOT"/cmd "$ROOT/go.mod" -newer "$BIN" -print -quit 2>/dev/null | grep -q .; then
    ( cd "$ROOT" && make build )
fi

# The embedded UI needs no bundle; an on-disk override is optional.
if [[ ! -f "$ROOT/webui/pkg/ui.js" ]]; then
    echo "warning: webui/pkg/ui.js is missing from the checkout" >&2
fi

GENERATED_UNIT="$(mktemp)"
sed \
    -e "s#^User=.*#User=$(sed_escape "$(id -un)")#" \
    -e "s#^Group=.*#Group=$(sed_escape "$(id -gn)")#" \
    -e "s#^WorkingDirectory=.*#WorkingDirectory=$(sed_escape "$DATA_DIR")#" \
    -e "s#^ExecStart=.*#ExecStart=$(sed_escape "$BIN")#" \
    -e "s#^Environment=GEXTTO_DATA_DIR=.*#Environment=GEXTTO_DATA_DIR=$(sed_escape "$DATA_DIR")#" \
    "$UNIT_SOURCE" > "$GENERATED_UNIT"

sudo install -d -o "$(id -un)" -g "$(id -gn)" "$DATA_DIR"
sudo install -m 0644 "$GENERATED_UNIT" "/etc/systemd/system/$UNIT_NAME"
rm -f "$GENERATED_UNIT"
sudo systemctl daemon-reload
sudo systemctl enable "$UNIT_NAME"
sudo systemctl restart "$UNIT_NAME"
sudo systemctl --no-pager --full status "$UNIT_NAME"
