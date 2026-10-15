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
for arg in "$@"; do
  case "$arg" in
    --uninstall) UNINSTALL=1 ;;
    --run-now) RUN_NOW=1 ;;
    -h | --help) sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

if [[ "$UNINSTALL" == 1 ]]; then
  systemctl --user disable --now tmp-clean.timer 2>/dev/null || true
  rm -f "$SERVICE" "$TIMER"
  systemctl --user daemon-reload
  echo "tmp-clean timer removed"
  exit 0
fi

[[ -x "$SCRIPT" ]] || { echo "missing $SCRIPT" >&2; exit 1; }
mkdir -p "$UNIT_DIR"

cat > "$SERVICE" <<EOF
[Unit]
Description=Clean /tmp of the current user's stale files (age, in-use, junk)
Documentation=file:$SCRIPT

[Service]
Type=oneshot
ExecStart=$SCRIPT --verbose
# The log is short: keep it in the journal, and do not fail the unit for a file
# that vanished while the cleaner was running.
Nice=10
IOSchedulingClass=idle
EOF

cat > "$TIMER" <<'EOF'
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

systemctl --user daemon-reload
systemctl --user enable --now tmp-clean.timer
if [[ "$RUN_NOW" == 1 ]]; then
  systemctl --user start tmp-clean.service
  systemctl --user --no-pager --lines=0 status tmp-clean.service || true
fi
echo "installed: $SERVICE"
echo "installed: $TIMER"
systemctl --user list-timers tmp-clean.timer --no-pager
