#!/usr/bin/env bash
# Publish a verified Linux/Windows release without touching the live one until
# every asset has been checked.
#
# The release is built on a *candidate* draft with its own temporary tag:
# upload, remote download and checksum verification all happen there. Only when
# the candidate is complete and verified is the public release replaced: the
# rolling tag is moved (continuous), the old release is deleted and the
# candidate takes its tag and is published. A failure before that point leaves
# the live release and its assets exactly as they were.
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
# never displace the currently published release or manifest.
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
  # A stable release must describe an existing tag that already points at the
  # commit being published; the candidate draft cannot check this for us.
  tag_commit="$(gh api "repos/$REPO/commits/$TAG" --jq '.sha' 2>/dev/null || true)"
  [[ "$tag_commit" == "$COMMIT" ]] || { echo "tag $TAG points at ${tag_commit:-nothing}, expected $COMMIT" >&2; exit 1; }
fi

# The candidate carries its own tag: nothing the workflow uploads can collide
# with the live release, and a failed upload only leaves a draft behind.
CANDIDATE_TAG="$TAG-candidate-${GITHUB_RUN_ID:-$$}"

PUBLISHED=0
OLD_DELETED=0
VERIFY_DIR="$(mktemp -d)"
cleanup() {
  local status=$?
  trap - EXIT
  if [[ "$status" != "0" && "$PUBLISHED" == "0" ]]; then
    if [[ "$OLD_DELETED" == "0" ]]; then
      # The live release was never touched: drop the candidate and its tag.
      retry "cleanup of $CANDIDATE_TAG" gh release delete "$CANDIDATE_TAG" --repo "$REPO" --yes --cleanup-tag >/dev/null 2>&1 || true
    else
      # The old release is gone: the verified draft is the only complete copy,
      # so it is kept for a retry instead of being deleted.
      echo "the verified draft $CANDIDATE_TAG was kept: the previous $TAG release is gone" >&2
    fi
  fi
  rm -rf -- "$VERIFY_DIR"
  exit "$status"
}
trap cleanup EXIT

create=(gh release create "$CANDIDATE_TAG" --repo "$REPO" --draft --title "$TITLE" --notes "$NOTES" --target "$COMMIT")
[[ "$CHANNEL" == continuous ]] && create+=(--prerelease)
retry "creation of candidate $CANDIDATE_TAG" "${create[@]}" >/dev/null

for file in "${files[@]}"; do
  retry "upload of $(basename "$file")" gh release upload "$CANDIDATE_TAG" --repo "$REPO" --clobber "$file" >/dev/null
done

# Verify what clients can actually download, not just the local build output.
# release.json from an earlier run may still be present in the draft; it is not
# generated or uploaded until every payload checksum below has passed.
retry "download of candidate assets" gh release download "$CANDIDATE_TAG" --repo "$REPO" --dir "$VERIFY_DIR" >/dev/null
"$ROOT/scripts/verify-release-assets.sh" "$VERIFY_DIR"

GEXTTO_COMMIT="$COMMIT" GITHUB_SHA="$COMMIT" "$ROOT/scripts/release-manifest.sh" \
  --channel "$CHANNEL" --label "$LABEL" --output "$DIST/release.json"
retry "upload of release.json" gh release upload "$CANDIDATE_TAG" --repo "$REPO" --clobber "$DIST/release.json" >/dev/null
rm -f -- "$VERIFY_DIR/release.json"
retry "download of release.json" gh release download "$CANDIDATE_TAG" --repo "$REPO" --pattern release.json --dir "$VERIFY_DIR" >/dev/null
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

# The candidate is complete and verified. From here the live release is
# replaced; every step is retried and the tag is confirmed before the old
# release is removed.
if [[ "$CHANNEL" == continuous ]]; then
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
fi

candidate_id="$(retry "lookup of candidate $CANDIDATE_TAG" gh api "repos/$REPO/releases/tags/$CANDIDATE_TAG" --jq '.id')"
[[ -n "$candidate_id" ]] || { echo "cannot find the candidate release $CANDIDATE_TAG" >&2; exit 1; }

# The old release goes only now, with a complete verified candidate waiting.
if gh release view "$TAG" --repo "$REPO" >/dev/null 2>&1; then
  retry "removal of the previous $TAG release" gh release delete "$TAG" --repo "$REPO" --yes >/dev/null
fi
OLD_DELETED=1

retry "rename of the candidate to $TAG" gh api -X PATCH "repos/$REPO/releases/$candidate_id" -f tag_name="$TAG" >/dev/null

edit=(gh release edit "$TAG" --repo "$REPO" --draft=false --title "$TITLE" --notes "$NOTES")
[[ "$CHANNEL" == continuous ]] && edit+=(--prerelease)
[[ "$CHANNEL" == stable ]] && edit+=(--prerelease=false)
retry "publication of $TAG" "${edit[@]}" >/dev/null
PUBLISHED=1

# The candidate tag is not part of the release any more; leaving it behind would
# add a tag per build to the repository.
gh api -X DELETE "repos/$REPO/git/refs/tags/$CANDIDATE_TAG" >/dev/null 2>&1 || true

echo "published $TAG ($CHANNEL) at $COMMIT with ${#payloads[@]} verified payloads"
