#!/usr/bin/env bash
# test-workdir.sh — the one work area for tests, measurements and screenshots.
#
# Nothing a test, a benchmark or a screenshot run produces is written straight
# into $HOME or into the checkout: everything lives under one root, and every
# cycle gets its own folder inside it, removed when the cycle ends.
#
#   scripts/test-workdir.sh root          # print the root (created if missing)
#   scripts/test-workdir.sh new <name>    # create and print <root>/<name>
#   scripts/test-workdir.sh clean         # remove what no process is using
#   scripts/test-workdir.sh clean --all   # remove everything (stop the runs first)
#
# Root: $GEXTTO_TEST_ROOT, default ${XDG_CACHE_HOME:-$HOME/.cache}/gextto-tests.
# It sits on a real disk on purpose: /tmp is often tmpfs, too small for torrent
# payloads and gone after a reboot.
set -euo pipefail

ROOT="${GEXTTO_TEST_ROOT:-${XDG_CACHE_HOME:-$HOME/.cache}/gextto-tests}"

# A wrong or empty GEXTTO_TEST_ROOT must never turn `clean` into something that
# removes the home or the checkout.
case "$ROOT" in
"" | / | "$HOME" | "$HOME/" | /home | /home/"$(id -un)" | /tmp | /var | /usr | /etc)
	echo "test-workdir: refusing to use '$ROOT' as the work area" >&2
	exit 1
	;;
esac

# alive reports whether the pid in a pid file still runs: a folder with a live
# pid file is an instance left running on purpose and is kept by `clean`.
alive() {
	local pid
	pid=$(cat "$1" 2>/dev/null) || return 1
	[[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null
}

cmd_root() {
	mkdir -p "$ROOT"
	echo "$ROOT"
}

cmd_new() {
	local name=${1:-}
	[[ -n "$name" ]] || {
		echo "usage: test-workdir.sh new <name>" >&2
		exit 2
	}
	mkdir -p "$ROOT/$name"
	echo "$ROOT/$name"
}

cmd_clean() {
	local all=0 entry pidfile pid
	[[ ${1:-} == --all ]] && all=1
	[[ -d "$ROOT" ]] || return 0
	for entry in "$ROOT"/*; do
		[[ -e "$entry" ]] || continue
		pid=""
		if ((!all)); then
			for pidfile in "$entry"/*.pid; do
				[[ -f "$pidfile" ]] || continue
				if alive "$pidfile"; then
					pid=$(cat "$pidfile")
					break
				fi
			done
		fi
		if [[ -n "$pid" ]]; then
			echo "keep   $entry (running, pid $pid)"
		else
			rm -rf "$entry"
			echo "remove $entry"
		fi
	done
	# An empty root stays: it is the place the next run writes to.
}

case "${1:-root}" in
root) cmd_root ;;
new) cmd_new "${2:-}" ;;
clean) cmd_clean "${2:-}" ;;
-h | --help | help) sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//' ;;
*)
	echo "test-workdir: unknown command: $1" >&2
	exit 2
	;;
esac
