#!/usr/bin/env bash
# Install (or refresh) the systemd **user** units that run the smart /tmp
# cleaner daily. User scope on purpose: no sudo, the journal keeps the log, and
# the cleaner only ever looks at the current user's own files.
#
#   scripts/install-tmp-cleaner.sh              # install and enable the timer
#   scripts/install-tmp-cleaner.sh --run-now     # …and clean once, right away
#   scripts/install-tmp-cleaner.sh --uninstall
#
# Logs: `journalctl --user -u tmp-clean` (or `systemctl --user status tmp-clean`).
set -euo pipefail

ROOT="$(cd "$(dirname "$(readlink -f "$0")")/.." && pwd)"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
SERVICE="$UNIT_DIR/tmp-clean.service"
TIMER="$UNIT_DIR/tmp-clean.timer"
SCRIPT="$ROOT/scripts/tmp-smart-clean.sh"

UNINSTALL=0
RUN_NOW=0
DRY_RUN=0
for arg in "$@"; do
  case "$arg" in
    --uninstall) UNINSTALL=1 ;;
    --run-now) RUN_NOW=1 ;;
    --dry-run) DRY_RUN=1 ;;
    -h | --help) sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

# systemd_value quotes a setting whose value may contain a space (Documentation=,
# ExecStart=): systemd splits those on whitespace and expands %-specifiers, so
# quotes and backslashes are escaped for its parser.
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

if [[ "$UNINSTALL" == 1 ]]; then
  systemctl --user disable --now tmp-clean.timer 2>/dev/null || true
  rm -f "$SERVICE" "$TIMER"
  systemctl --user daemon-reload
  echo "tmp-clean timer removed"
  exit 0
fi

[[ -x "$SCRIPT" ]] || { echo "missing $SCRIPT" >&2; exit 1; }
mkdir -p "$UNIT_DIR"

service_unit="$(cat <<EOF
[Unit]
Description=Clean /tmp of the current user's stale files (age, in-use, junk)
Documentation=$(systemd_value "file:$SCRIPT")

[Service]
Type=oneshot
ExecStart=$(systemd_exec "$SCRIPT") --verbose
# The log is short: keep it in the journal, and do not fail the unit for a file
# that vanished while the cleaner was running.
Nice=10
IOSchedulingClass=idle
EOF
)"
timer_unit="$(cat <<'EOF'
[Unit]
Description=Run the smart /tmp cleaner daily

[Timer]
OnCalendar=daily
# A random delay keeps it from firing at the same second as everything else,
# and a machine that was off catches up at the next boot.
RandomizedDelaySec=30m
Persistent=true

[Install]
WantedBy=timers.target
EOF
)"
if [[ "$DRY_RUN" == 1 ]]; then
  printf '%s\n' "$service_unit" "$timer_unit"
  exit 0
fi
printf '%s\n' "$service_unit" > "$SERVICE"
printf '%s\n' "$timer_unit" > "$TIMER"

systemctl --user daemon-reload
systemctl --user enable --now tmp-clean.timer
if [[ "$RUN_NOW" == 1 ]]; then
  systemctl --user start tmp-clean.service
  systemctl --user --no-pager --lines=0 status tmp-clean.service || true
fi
echo "installed: $SERVICE"
echo "installed: $TIMER"
systemctl --user list-timers tmp-clean.timer --no-pager
