#!/usr/bin/env bash
# Prune GitHub Actions caches, keeping only the newest N of each kind.
#
# GitHub has no built-in "keep the last N" policy: a cache lives until the
# repository exceeds its budget (10 GB) and then the least-recently-used ones are
# evicted. The CodeQL overlay base databases are one per commit (~75 MB each), so
# on an active repository they alone fill the budget and the eviction can throw
# away the useful Go/npm caches first. This script keeps the newest KEEP caches
# of every key prefix and deletes older ones, so the space is freed where it is
# plentiful and the build caches survive.
#
# Usage:
#   scripts/prune-actions-caches.sh [--keep N] [--repo OWNER/REPO]
#                                   [--overlay-only] [--dry-run]
#
# Environment:
#   GITHUB_REPOSITORY  used as the default repository in Actions
#   GH_TOKEN           token with `actions: write` (in Actions: GITHUB_TOKEN)
#
# Needs the GitHub CLI (gh); in CI it runs with the workflow's GITHUB_TOKEN.
set -euo pipefail

KEEP=20
REPO=""
DRY_RUN=0
OVERLAY_ONLY=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --keep) KEEP="$2"; shift 2 ;;
    --repo) REPO="$2"; shift 2 ;;
    --overlay-only) OVERLAY_ONLY=1; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

[[ "$KEEP" =~ ^[0-9]+$ ]] || { echo "error: --keep needs a number" >&2; exit 2; }
command -v gh >/dev/null 2>&1 || { echo "error: gh (GitHub CLI) not found" >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { echo "error: python3 not found" >&2; exit 1; }
if [[ -z "$REPO" ]]; then
  REPO="${GITHUB_REPOSITORY:-$(gh repo view --json nameWithOwner -q .nameWithOwner)}"
fi
[[ -n "$REPO" ]] || { echo "error: cannot determine the repository" >&2; exit 1; }

list="$(mktemp)"
plan="$(mktemp)"
trap 'rm -f "$list" "$plan"' EXIT

# One line per cache: created_at, id, key. --jq runs per page, so pagination
# concatenates the lines.
gh api --paginate "repos/$REPO/actions/caches?per_page=100" \
  --jq '.actions_caches[] | [.created_at, (.id|tostring), .key] | @tsv' >"$list"

# Group by key prefix (the trailing commit hash / run+attempt counters stripped)
# and keep the newest KEEP per group; print the ids to delete.
python3 - "$KEEP" "$OVERLAY_ONLY" "$list" >"$plan" <<'PY'
import re
import sys

keep, overlay_only, path = int(sys.argv[1]), sys.argv[2] == "1", sys.argv[3]
groups = {}
for line in open(path, encoding="utf-8"):
    line = line.rstrip("\n")
    if not line:
        continue
    created, cid, key = line.split("\t", 2)
    if overlay_only and not key.startswith("codeql-"):
        continue
    prefix = re.sub(r"-[0-9a-f]{40}.*$", "", key)
    prefix = re.sub(r"-[0-9]+-[0-9]+$", "", prefix)
    groups.setdefault(prefix, []).append((created, cid, key))
for _prefix, items in groups.items():
    items.sort(reverse=True)  # newest first: ISO-8601 timestamps sort lexically
    for _created, cid, _key in items[keep:]:
        print(cid)
PY

total=$(wc -l <"$list")
count=0
while read -r id; do
  [[ -n "$id" ]] || continue
  if [[ "$DRY_RUN" == "1" ]]; then
    echo "would delete cache $id"
  else
    gh api -X DELETE "repos/$REPO/actions/caches/$id" >/dev/null
    echo "deleted cache $id"
  fi
  count=$((count + 1))
done <"$plan"

if [[ "$DRY_RUN" == "1" ]]; then
  echo "dry run: $count of $total cache(s) would be deleted (keeping the newest $KEEP per kind)"
else
  echo "pruned $count of $total cache(s), kept the newest $KEEP per kind"
fi
