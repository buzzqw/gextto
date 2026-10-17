#!/usr/bin/env bash
# Write release.json, the manifest published next to the release archives.
# The in-app update check downloads it to tell whether a newer build exists
# and to show what changed (the recent commits), without calling the GitHub
# API (no rate limits, no token).
#
#   scripts/release-manifest.sh --channel continuous --build 812 --output release.json
#
# Options:
#   --channel NAME   continuous or stable (default: continuous)
#   --label TEXT     installed release marker (default: the channel)
#   --build N        Gextto build number (default: GEXTTO_BUILD or build_number)
#   --built-at TIME  RFC 3339 build time (default: now, UTC)
#   --commits N      how many recent commits to list (default: 100)
#   --output FILE    destination (default: release.json)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CHANNEL="continuous"
LABEL=""
BUILD="${GEXTTO_BUILD:-}"
BUILT_AT="${GEXTTO_BUILT_AT:-}"
COMMITS=100
OUTPUT="release.json"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --channel|--label|--build|--built-at|--commits|--output)
      [[ $# -ge 2 && -n "$2" ]] || { echo "$1 requires a value" >&2; exit 2; }
      case "$1" in
        --channel) CHANNEL="$2" ;;
        --label) LABEL="$2" ;;
        --build) BUILD="$2" ;;
        --built-at) BUILT_AT="$2" ;;
        --commits) COMMITS="$2" ;;
        --output) OUTPUT="$2" ;;
      esac
      shift 2
      ;;
    -h|--help) sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

[[ -n "$LABEL" ]] || LABEL="$CHANNEL"
[[ -n "$BUILD" ]] || BUILD="$(tr -dc '0-9' < "$ROOT/build_number" 2>/dev/null || true)"
[[ -n "$BUILD" ]] || BUILD=0
[[ "$BUILD" =~ ^[0-9]+$ ]] || { echo "--build must be a number" >&2; exit 2; }
[[ "$COMMITS" =~ ^[0-9]+$ ]] || { echo "--commits must be a number" >&2; exit 2; }
[[ -n "$BUILT_AT" ]] || BUILT_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION" 2>/dev/null || true)"
COMMIT="${GEXTTO_COMMIT:-${GITHUB_SHA:-$(git -C "$ROOT" rev-parse HEAD)}}"

# GX_PATHS is the code of the gx-torrent daemon: its package and every package
# of this repository it imports (`go list -deps ./cmd/gx-torrent`, kept in
# sync by TestReleaseManifestCoversDaemonPackages). A commit that touches one
# of them is flagged "gx": true, so the standalone daemon offers an update
# only when its own code changed.
GX_PATHS=(cmd/gx-torrent internal/auth internal/bwschedule internal/autospeed internal/constants internal/debugpprof internal/dht internal/flaresolverr
  internal/gxcore internal/logging internal/queue internal/rss internal/settings
  internal/notify internal/torznab internal/updatecheck internal/utp internal/webui go.mod go.sum)
declare -A GX_COMMITS=()
while read -r sha; do
  [[ -n "$sha" ]] && GX_COMMITS[$sha]=1
done < <(git -C "$ROOT" log -n "$COMMITS" --format='%H' "$COMMIT" -- "${GX_PATHS[@]}" 2>/dev/null || true)
GX_COMMIT="$(git -C "$ROOT" log -n 1 --format='%H' "$COMMIT" -- "${GX_PATHS[@]}" 2>/dev/null || true)"
# gx-core's own build number (committed, see scripts/next-gx-build-number.sh).
GX_BUILD="${GEXTTO_GX_BUILD:-$(tr -dc '0-9' < "$ROOT/gx-torrent.build_number" 2>/dev/null || true)}"
[[ "$GX_BUILD" =~ ^[0-9]+$ ]] || GX_BUILD=0

# json_escape quotes a string for JSON (backslash, quote, control characters).
json_escape() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  value="${value//$'\t'/ }"
  value="${value//$'\r'/}"
  value="${value//$'\n'/ }"
  printf '"%s"' "$value"
}

{
  printf '{\n'
  printf '  "name": "gextto",\n'
  printf '  "channel": %s,\n' "$(json_escape "$CHANNEL")"
  printf '  "label": %s,\n' "$(json_escape "$LABEL")"
  printf '  "version": %s,\n' "$(json_escape "$VERSION")"
  printf '  "app_version": %s,\n' "$(json_escape "1.1.$BUILD")"
  printf '  "build": %s,\n' "$BUILD"
  printf '  "commit": %s,\n' "$(json_escape "$COMMIT")"
  printf '  "built_at": %s,\n' "$(json_escape "$BUILT_AT")"
  printf '  "gx_torrent_commit": %s,\n' "$(json_escape "$GX_COMMIT")"
  printf '  "gx_version": %s,\n' "$(json_escape "1.1.$GX_BUILD")"
  printf '  "gx_build": %s,\n' "$GX_BUILD"
  printf '  "commits": ['
  first=1
  # A shallow checkout lists only what it has: the update check treats a
  # missing installed commit as "more changes than shown".
  while IFS=$'\t' read -r sha date subject; do
    [[ -n "$sha" ]] || continue
    if [[ "$first" == "1" ]]; then first=0; printf '\n'; else printf ',\n'; fi
    gx=false
    [[ -n "${GX_COMMITS[$sha]:-}" ]] && gx=true
    printf '    {"sha": %s, "date": %s, "subject": %s, "gx": %s}' \
      "$(json_escape "$sha")" "$(json_escape "$date")" "$(json_escape "$subject")" "$gx"
  done < <(git -C "$ROOT" log -n "$COMMITS" --format='%H%x09%cI%x09%s' "$COMMIT" 2>/dev/null || true)
  [[ "$first" == "1" ]] || printf '\n  '
  printf ']\n'
  printf '}\n'
} > "$OUTPUT"

echo "manifest: $OUTPUT ($CHANNEL, build $BUILD, ${COMMIT:0:12})"
