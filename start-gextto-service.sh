#!/usr/bin/env bash
# Build the daemon from this checkout and install it as a systemd *system*
# service, running as the current user. For a production install use install.sh
# (it also defaults to the local user; add `--user gextto` for the isolated
# dedicated account); this script is aimed at a local server where the checkout
# is the source of truth.
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

# systemd_path escapes a single-path setting (WorkingDirectory=): systemd
# expands %-specifiers there but does not unquote the value.
systemd_path() { printf '%s' "${1//%/%%}"; }

# systemd_value quotes an Environment= value, which systemd splits on
# whitespace and expands %-specifiers in.
systemd_value() {
    local value="$1"
    value="${value//\\/\\\\}"
    value="${value//\"/\\\"}"
    value="${value//%/%%}"
    printf '"%s"' "$value"
}

# systemd_exec quotes one ExecStart argument, where systemd also expands
# $variables: a literal dollar is written as $$.
systemd_exec() {
    local value
    value="$(systemd_value "$1")"
    value="${value//\$/\$\$}"
    printf '%s' "$value"
}

LEGACY_SERVICE="${GEXTTO_LEGACY_SERVICE:-}"
if [[ -n "$LEGACY_SERVICE" ]] && systemctl is-active --quiet "$LEGACY_SERVICE"; then
    echo "The legacy service '$LEGACY_SERVICE' is active and uses the same ports. Stop it first: sudo systemctl stop $LEGACY_SERVICE" >&2
    exit 1
fi

if [[ ! -x "$BIN" ]] || find "$ROOT"/*.go "$ROOT"/internal "$ROOT"/cmd "$ROOT/go.mod" -newer "$BIN" -print -quit 2>/dev/null | grep -q .; then
    ( cd "$ROOT" && make build )
fi

GENERATED_UNIT="$(mktemp)"
trap 'rm -f "$GENERATED_UNIT"' EXIT
sed \
    -e "s#^User=.*#User=$(sed_escape "$(id -un)")#" \
    -e "s#^Group=.*#Group=$(sed_escape "$(id -gn)")#" \
    -e "s#^WorkingDirectory=.*#WorkingDirectory=$(sed_escape "$(systemd_path "$DATA_DIR")")#" \
    -e "s#^ExecStart=.*#ExecStart=$(sed_escape "$(systemd_exec "$BIN")")#" \
    -e "s#^Environment=GEXTTO_DATA_DIR=.*#Environment=GEXTTO_DATA_DIR=$(sed_escape "$(systemd_value "$DATA_DIR")")#" \
    "$UNIT_SOURCE" > "$GENERATED_UNIT"

if command -v systemd-analyze >/dev/null 2>&1; then
    systemd-analyze verify "$GENERATED_UNIT" >/dev/null 2>&1 ||
        { echo "the generated unit is not valid: run systemd-analyze verify $GENERATED_UNIT" >&2; exit 1; }
fi

sudo install -d -o "$(id -un)" -g "$(id -gn)" "$DATA_DIR"
sudo install -m 0644 "$GENERATED_UNIT" "/etc/systemd/system/$UNIT_NAME"
rm -f "$GENERATED_UNIT"
sudo systemctl daemon-reload
sudo systemctl enable "$UNIT_NAME"
sudo systemctl restart "$UNIT_NAME"
sudo systemctl --no-pager --full status "$UNIT_NAME"
