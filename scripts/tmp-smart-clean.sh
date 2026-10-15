#!/usr/bin/env bash
# Clean /tmp of what is safe to remove — an "intelligent" cleaner, not a
# find -delete:
#
#   * only the entries of the current user (never root's or another user's);
#   * only older than a threshold (default 24 h), so nothing in use is touched;
#   * never a file a running process still holds open (an open file would keep
#     the space busy anyway, and removing it makes a live program fail later in
#     obscure ways); a directory holding an open file is left alone too;
#   * a directory goes only when its *newest* content is old enough, so a tree
#     that is still being written to stays;
#   * stale build and test residue (go-build*, Test*, playwright-*, *.tmp) ages
#     faster (default 6 h) — it is junk by nature — but never younger than the
#     hard floor (1 h);
#   * system and desktop plumbing is never touched: systemd-private-*,
#     snap-private-tmp, .X11-unix, .ICE-unix, .XIM-unix, .font-unix, .Test-unix,
#     and any entry that is a socket.
#
# Usage:
#   scripts/tmp-smart-clean.sh [--dry-run] [--older-than HOURS]
#                              [--junk-older-than HOURS] [--floor HOURS]
#                              [--paths DIR] [--verbose]
#
# With a systemd user timer (scripts/install-tmp-cleaner.sh) it runs daily; the
# summary goes to the journal (`journalctl --user -u tmp-clean`).
set -euo pipefail

DRY_RUN=0
VERBOSE=0
OLDER_HOURS=24
JUNK_HOURS=6
FLOOR_HOURS=1
PATHS=()

usage() {
  sed -n '2,27p' "$0" | sed 's/^# \{0,1\}//'
  exit 2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    --verbose) VERBOSE=1; shift ;;
    --older-than) OLDER_HOURS="${2:?}"; shift 2 ;;
    --junk-older-than) JUNK_HOURS="${2:?}"; shift 2 ;;
    --floor) FLOOR_HOURS="${2:?}"; shift 2 ;;
    --paths) PATHS+=("${2:?}"); shift 2 ;;
    -h | --help) usage ;;
    *) echo "unknown option: $1" >&2; usage ;;
  esac
done
[[ ${#PATHS[@]} -gt 0 ]] || PATHS=(/tmp)

UID_NOW="$(id -u)"
NOW="$(date +%s)"

# Never descend into the system/desktop plumbing, whatever its age. The runtime
# dirs of tmux/screen/ssh hold sockets: a socket is not visible as a path in
# /proc (its fd reads "socket:[…]"), so the in-use check cannot see it and the
# name is the only guard.
PROTECTED_NAME='^(systemd-private-|snap-private-tmp|systemd-|\.X11-unix|\.ICE-unix|\.XIM-unix|\.font-unix|\.Test-unix|\.X11-|\.esd-|\.Xauthority|\.X0-lock|tmux-|screen-|ssh-|keyring-|pulse-|dbus-|gnupg-|claude-)'

# Residue that ages faster.
JUNK_NAME='^(go-build[0-9]*|Test[A-Za-z0-9_]*|gxtest[_.]|gextto-|playwright-|tmp\.|.*\.tmp$|.*\.partial$)'

# collect_in_use lists what the running processes hold open right now. Only the
# readable ones (our own processes, plus whatever another user happens to expose)
# matter: another user's file is skipped by the owner check anyway.
in_use_file="$(mktemp "${TMPDIR:-/tmp}/tmp-clean-inuse.XXXXXX")"
trap 'rm -f "$in_use_file"' EXIT
for pid in /proc/[0-9]*; do
  [[ -d "$pid/fd" ]] || continue
  for fd in "$pid"/fd/*; do
    link="$(readlink -f "$fd" 2>/dev/null)" || continue
    [[ -n "$link" && "$link" != "/" ]] || continue
    printf '%s\n' "${link% (deleted)}"
  done
done > "$in_use_file" 2>/dev/null || true

# held_open reports whether the path (or something inside it) is open.
held_open() {
  local path="$1"
  grep -qxF -- "$path" "$in_use_file" 2>/dev/null && return 0
  grep -qF -- "$path/" "$in_use_file" 2>/dev/null && return 0
  return 1
}

# newest_mtime prints the newest mtime found under dir (dir itself included).
newest_mtime() {
  local dir="$1" newest=0 m
  while IFS= read -r m; do
    [[ -n "$m" ]] || continue
    ((m > newest)) && newest="$m"
  done < <(find "$dir" -xdev -mindepth 0 -printf '%T@\n' 2>/dev/null | cut -d. -f1)
  printf '%s' "$newest"
}

removed=0
freed_kib=0
skipped_in_use=0
skipped_young=0
would=0

count_entries() { find "$1" -xdev -mindepth 1 | wc -l; }
size_kib() { du -sk "$1" 2>/dev/null | awk '{print $1+0}'; }

remove_entry() {
  local path="$1" reason="$2" entries size
  entries="$(count_entries "$path")"
  size="$(size_kib "$path")"
  if [[ "$DRY_RUN" == 1 ]]; then
    would=$((would + 1))
    printf 'would remove (%s, %s KiB, %s entries): %s\n' "$reason" "$size" "$entries" "$path"
    return 0
  fi
  if rm -rf -- "$path" 2>/dev/null; then
    removed=$((removed + 1))
    freed_kib=$((freed_kib + size))
    [[ "$VERBOSE" == 1 ]] && printf 'removed (%s, %s KiB): %s\n' "$reason" "$size" "$path"
  fi
}

for root in "${PATHS[@]}"; do
  [[ -d "$root" ]] || continue
  # Direct children only: recursion is decided per entry.
  while IFS= read -r -d '' entry; do
    name="$(basename "$entry")"
    # Ownership: ours only.
    [[ "$(stat -c %u -- "$entry" 2>/dev/null || echo -1)" == "$UID_NOW" ]] || continue
    # Plumbing and sockets: never.
    if [[ "$name" =~ $PROTECTED_NAME ]]; then continue; fi
    if [[ -S "$entry" ]]; then continue; fi
    # A running process holds it (or something inside it): hands off.
    if held_open "$entry"; then
      skipped_in_use=$((skipped_in_use + 1))
      [[ "$VERBOSE" == 1 ]] && printf 'kept (in use): %s\n' "$entry"
      continue
    fi
    age_h=$(( (NOW - $(stat -c %Y -- "$entry" 2>/dev/null || echo "$NOW")) / 3600 ))
    # Residue ages faster than the rest; the hard floor protects what was just
    # created either way.
    threshold=$OLDER_HOURS
    if [[ "$name" =~ $JUNK_NAME ]]; then
      threshold=$JUNK_HOURS
    fi
    ((threshold < FLOOR_HOURS)) && threshold=$FLOOR_HOURS
    if [[ -d "$entry" && ! -L "$entry" ]]; then
      # A directory holding a socket is an IPC endpoint in use (a tmux or ssh
      # socket cannot be seen as a path in /proc): never removed.
      if [[ -n "$(find "$entry" -xdev -type s -print -quit 2>/dev/null)" ]]; then
        skipped_in_use=$((skipped_in_use + 1))
        [[ "$VERBOSE" == 1 ]] && printf 'kept (holds a socket): %s\n' "$entry"
        continue
      fi
      newest="$(newest_mtime "$entry")"
      content_h=$(( (NOW - newest) / 3600 ))
      if ((content_h < threshold)); then
        skipped_young=$((skipped_young + 1))
        [[ "$VERBOSE" == 1 ]] && printf 'kept (young content, %sh): %s\n' "$content_h" "$entry"
        continue
      fi
      remove_entry "$entry" "old dir, youngest content ${content_h}h"
      continue
    fi
    if ((age_h < threshold)); then
      skipped_young=$((skipped_young + 1))
      continue
    fi
    remove_entry "$entry" "old file, ${age_h}h"
  done < <(find "$root" -xdev -mindepth 1 -maxdepth 1 -print0 2>/dev/null)
done

if [[ "$DRY_RUN" == 1 ]]; then
  printf 'tmp-smart-clean (dry run): %d entries would go, %d kept (in use), %d kept (too young)\n' \
    "$would" "$skipped_in_use" "$skipped_young"
else
  printf 'tmp-smart-clean: %d entries removed, %d MiB freed, %d kept (in use), %d kept (too young)\n' \
    "$removed" "$((freed_kib / 1024))" "$skipped_in_use" "$skipped_young"
fi
