#!/usr/bin/env bash
# Publish a verified Linux/Windows release as a draft, verify every uploaded
# asset remotely, then move the continuous tag (stable tags already exist),
# upload release.json and make the release public. Any failure leaves the
# release draft and propagates a non-zero status.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CHANNEL=""
TAG=""
LABEL=""
DIST="$ROOT/dist"
REPO="${GITHUB_REPOSITORY:-buzzqw/gextto}"
COMMIT="${GITHUB_SHA:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --channel|--tag|--label|--dist|--repo|--commit)
      [[ $# -ge 2 && -n "$2" ]] || { echo "$1 requires a value" >&2; exit 2; }
      case "$1" in
        --channel) CHANNEL="$2" ;;
        --tag) TAG="$2" ;;
        --label) LABEL="$2" ;;
        --dist) DIST="$2" ;;
        --repo) REPO="$2" ;;
        --commit) COMMIT="$2" ;;
      esac
      shift 2
      ;;
    -h|--help)
      echo "usage: publish-github-release.sh --channel continuous|stable --tag TAG [--label LABEL] [--dist DIR] [--repo OWNER/REPO] [--commit SHA]"
      exit 0
      ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

[[ "$CHANNEL" == continuous || "$CHANNEL" == stable ]] || { echo "--channel must be continuous or stable" >&2; exit 2; }
[[ -n "$TAG" ]] || { echo "--tag is required" >&2; exit 2; }
[[ -n "$LABEL" ]] || LABEL="$TAG"
[[ -n "$COMMIT" ]] || COMMIT="$(git -C "$ROOT" rev-parse HEAD)"
[[ "$COMMIT" =~ ^[0-9a-fA-F]{40}$ ]] || { echo "--commit must be a full SHA" >&2; exit 2; }
command -v gh >/dev/null 2>&1 || { echo "gh is required" >&2; exit 1; }

retry() {
  local description="$1" attempt
  shift
  for attempt in 1 2 3 4 5; do
    if "$@"; then
      return 0
    fi
    echo "$description failed (attempt $attempt of 5), retrying" >&2
    sleep $((attempt * 5))
  done
  return 1
}

# This runs before any remote mutation: a missing archive or bad checksum must
# never displace the currently published continuous tag or manifest.
"$ROOT/scripts/verify-release-assets.sh" "$DIST"
mapfile -t payloads < <("$ROOT/scripts/verify-release-assets.sh" --list)
files=()
for payload in "${payloads[@]}"; do
  files+=("$DIST/$payload" "$DIST/$payload.sha256")
done

if [[ "$CHANNEL" == continuous ]]; then
  TITLE="Gextto continuous build"
  NOTES="Automatic build from commit $COMMIT on main (build $(tr -dc '0-9' < "$ROOT/build_number"))."
else
  TITLE="Gextto $TAG"
  NOTES="Release $TAG ($COMMIT)."
fi

PUBLISHED=0
RELEASE_READY=0
VERIFY_DIR="$(mktemp -d)"
cleanup() {
  local status=$?
  trap - EXIT
  if [[ "$status" != "0" && "$PUBLISHED" == "0" && "$RELEASE_READY" == "1" ]]; then
    # Best effort, but do not hide the original failure. The release was made
    # draft before uploading, so a transient edit failure cannot publish it.
    retry "revert of $TAG to draft" gh release edit "$TAG" --repo "$REPO" --draft=true >/dev/null 2>&1 || true
  fi
  rm -rf -- "$VERIFY_DIR"
  exit "$status"
}
trap cleanup EXIT

# An existing rolling release is unpublished while its assets are replaced.
if retry "inspection of $TAG" gh release view "$TAG" --repo "$REPO" >/dev/null 2>&1; then
  retry "drafting of $TAG" gh release edit "$TAG" --repo "$REPO" --draft=true >/dev/null
else
  create=(gh release create "$TAG" --repo "$REPO" --draft --title "$TITLE" --notes "$NOTES")
  [[ "$CHANNEL" == continuous ]] && create+=(--prerelease --target "$COMMIT")
  [[ "$CHANNEL" == stable ]] && create+=(--verify-tag)
  retry "creation of release $TAG" "${create[@]}" >/dev/null
fi
RELEASE_READY=1

for file in "${files[@]}"; do
  retry "upload of $(basename "$file")" gh release upload "$TAG" --repo "$REPO" --clobber "$file" >/dev/null
done

# Verify what clients can actually download, not just the local build output.
# release.json from an earlier run may still be present in the draft; it is not
# generated or uploaded until every payload checksum below has passed.
retry "download of release assets" gh release download "$TAG" --repo "$REPO" --dir "$VERIFY_DIR" >/dev/null
"$ROOT/scripts/verify-release-assets.sh" "$VERIFY_DIR"

GEXTTO_COMMIT="$COMMIT" GITHUB_SHA="$COMMIT" "$ROOT/scripts/release-manifest.sh" \
  --channel "$CHANNEL" --label "$LABEL" --output "$DIST/release.json"
retry "upload of release.json" gh release upload "$TAG" --repo "$REPO" --clobber "$DIST/release.json" >/dev/null
rm -f -- "$VERIFY_DIR/release.json"
retry "download of release.json" gh release download "$TAG" --repo "$REPO" --pattern release.json --dir "$VERIFY_DIR" >/dev/null
python3 - "$VERIFY_DIR/release.json" "$CHANNEL" "$LABEL" "$COMMIT" <<'PY'
import json
import sys

path, channel, label, commit = sys.argv[1:]
with open(path, encoding="utf-8") as source:
    manifest = json.load(source)
expected = {"channel": channel, "label": label, "commit": commit}
for key, value in expected.items():
    if manifest.get(key) != value:
        raise SystemExit(f"release manifest {key} is {manifest.get(key)!r}, expected {value!r}")
if not manifest.get("commits") or manifest["commits"][0].get("sha") != commit:
    raise SystemExit("release manifest does not describe the published commit")
PY

# Only after all payloads and the manifest have been verified may the rolling
# tag move. Stable tags already point at this commit; the same check catches a
# mis-triggered or stale release.
ref="repos/$REPO/git/refs/tags/$TAG"
if gh api "$ref" --jq '.object.sha' >/dev/null 2>&1; then
  retry "move of tag $TAG" gh api -X PATCH "$ref" -f sha="$COMMIT" -F force=true >/dev/null
else
  retry "creation of tag $TAG" gh api -X POST "repos/$REPO/git/refs" -f ref="refs/tags/$TAG" -f sha="$COMMIT" >/dev/null
fi
actual_commit=""
# The Git ref API can acknowledge a PATCH and still serve the previous object
# for a moment. Retry the comparison: a single stale read used to leave a fully
# verified continuous release in draft and fail the workflow.
for attempt in 1 2 3 4 5; do
  actual_commit="$(gh api "$ref" --jq '.object.sha' 2>/dev/null || true)"
  [[ "$actual_commit" == "$COMMIT" ]] && break
  sleep "$attempt"
done
[[ "$actual_commit" == "$COMMIT" ]] || { echo "tag $TAG points at $actual_commit, expected $COMMIT" >&2; exit 1; }

edit=(gh release edit "$TAG" --repo "$REPO" --draft=false --title "$TITLE" --notes "$NOTES")
[[ "$CHANNEL" == continuous ]] && edit+=(--prerelease)
[[ "$CHANNEL" == stable ]] && edit+=(--prerelease=false)
retry "publication of $TAG" "${edit[@]}" >/dev/null
PUBLISHED=1
echo "published $TAG ($CHANNEL) at $COMMIT with ${#payloads[@]} verified payloads"
