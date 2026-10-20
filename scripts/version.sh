#!/usr/bin/env bash
# Version of a build number, shared by the scripts that publish it.
#
# The third number runs from 0 to 999: when it reaches 1000 the middle number
# grows by one and the third restarts from 0 (1.2.999 -> 1.3.0). The switch
# happens at the first round thousand after the last published build, so what
# is already installed is not renumbered: Gextto's 1.1 series ends at build
# 1999 (rollover at 2000), gx-torrent's at build 999 (rollover at 1000).
#
# This mirrors versionFromBuild in internal/constants/constants.go; the two
# must agree (release.json is compared with the version the binary reports).

# version_for_build BUILD FIRST_ROLLOVER prints the version of BUILD, keeping
# the historical "1.1.<build>" form until FIRST_ROLLOVER is reached.
version_for_build() {
  local build="$1" first_rollover="$2"
  if ! [[ "$build" =~ ^[0-9]+$ ]]; then
    printf '1.1.%s\n' "$build"
    return 0
  fi
  build=$((10#$build))
  if (( build < first_rollover )); then
    printf '1.1.%d\n' "$build"
    return 0
  fi
  printf '1.%d.%d\n' $(( 2 + (build - first_rollover) / 1000 )) $(( build % 1000 ))
}

# gextto_version_for_build BUILD prints Gextto's version for BUILD.
gextto_version_for_build() {
  version_for_build "$1" 2000
}

# gx_version_for_build BUILD prints gx-torrent's version for its own BUILD.
gx_version_for_build() {
  version_for_build "$1" 1000
}

# version_label_for_build BUILD FIRST_ROLLOVER prints the version with the
# build number in parentheses once the rollover makes the two differ, so the
# build is never lost from the label (mirrors VersionLabelWithBuild).
version_label_for_build() {
  local build="$1" first_rollover="$2" version
  version="$(version_for_build "$build" "$first_rollover")"
  if [[ "$version" == "1.1.$build" ]]; then
    printf '%s\n' "$version"
    return 0
  fi
  printf '%s (build %s)\n' "$version" "$build"
}

# gx_version_label_for_build BUILD is gx-torrent's label for its own BUILD.
gx_version_label_for_build() {
  version_label_for_build "$1" 1000
}
