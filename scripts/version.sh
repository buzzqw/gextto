#!/usr/bin/env bash
# Version of a build number, shared by the scripts that publish it.
#
# The third number is always the build. The second number is raised by hand,
# when the project starts a new series: it lives in `version_series` for Gextto
# and in `gx_torrent.version_series` for the daemon, so it never moves by a
# counter and the installed build is never renumbered.
#
# This mirrors AppVersion/GxTorrentVersion in internal/constants/constants.go,
# stamped by scripts/build-daemon.sh from the same two files (release.json is
# compared with the version the binary reports).

# series_from_file FILE prints the trimmed series, or 1.1 when unavailable.
series_from_file() {
  local value
  value="$(tr -d '[:space:]' < "$1" 2>/dev/null || true)"
  [[ -n "$value" ]] || value="1.1"
  printf '%s\n' "$value"
}

# version_for_build BUILD SERIES prints SERIES.BUILD.
version_for_build() {
  local build="$1" series="$2"
  if ! [[ "$build" =~ ^[0-9]+$ ]]; then
    printf '%s.%s\n' "$series" "$build"
    return 0
  fi
  printf '%s.%d\n' "$series" $((10#$build))
}

# gextto_version_for_build BUILD prints Gextto's version for BUILD.
gextto_version_for_build() {
  version_for_build "$1" "$(series_from_file "$ROOT/version_series")"
}

# gx_version_for_build BUILD prints gx-torrent's version for its own BUILD.
gx_version_for_build() {
  version_for_build "$1" "$(series_from_file "$ROOT/gx_torrent.version_series")"
}

# version_label_for_build BUILD SERIES prints the version to show. The build is
# already the third number; only an old manifest whose version dropped it gets
# it in parentheses, exactly like VersionLabelWithBuild in Go.
version_label_for_build() {
  local build="$1" series="$2" version
  version="$(version_for_build "$build" "$series")"
  if [[ "$build" == "" || "$build" == "0" || "$version" == *".$build" ]]; then
    printf '%s\n' "$version"
    return 0
  fi
  printf '%s (build %s)\n' "$version" "$build"
}

# gx_version_label_for_build BUILD is gx-torrent's label for its own BUILD.
gx_version_label_for_build() {
  version_label_for_build "$1" "$(series_from_file "$ROOT/gx_torrent.version_series")"
}
