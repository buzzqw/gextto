#!/usr/bin/env bash
# Shared guard for scripts that recursively remove operator-selected paths.
# Sourcing this file defines gextto_safe_clean_path and SAFE_CLEAN_PATH.

gextto_safe_clean_path() {
  local raw="${1:-}" checkout="${2:-}" resolved
  [[ -n "$raw" ]] || return 1
  resolved="$(realpath -m -- "$raw")" || return 1
  case "$resolved" in
    /|"$HOME"|"$checkout"|/home|/root|/etc|/usr|/var|/var/lib|/var/log|/var/cache|/var/spool|/usr/local|\
    "$HOME/.ssh"|"$HOME/.config"|"$HOME/.local"|"$HOME/.gnupg"|"$HOME/.cache/opencode")
      return 1
      ;;
  esac
  # A parent of home or of the checkout is just as unsafe as the directory
  # itself: deleting it would remove the protected tree with everything else.
  case "$HOME/" in "$resolved/"*) return 1 ;; esac
  case "$checkout/" in "$resolved/"*) return 1 ;; esac
  # shellcheck disable=SC2034 # read by the sourcing script
  SAFE_CLEAN_PATH="$resolved"
}
