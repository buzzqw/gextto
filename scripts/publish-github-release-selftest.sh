#!/usr/bin/env bash
# Exercise the publication flow with a fake `gh`, so a retry-call signature
# mismatch or a skipped remote verification cannot silently break a release.
# Everything runs in a temporary directory: no token, network or tag is used.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
DIST="$TMP/dist"
MOCK_BIN="$TMP/bin"
mkdir -p "$DIST" "$MOCK_BIN"

# Build the same complete set the package scripts publish.
for arch in x86_64 aarch64; do
  stage="$TMP/gextto-$arch"
  mkdir -p "$stage"
  printf 'daemon\n' > "$stage/gexttod"
  printf 'engine\n' > "$stage/gx-torrent"
  printf 'launcher\n' > "$stage/run.sh"
  printf '%s\n' "$arch" > "$stage/VERSION"
  chmod 0755 "$stage/gexttod" "$stage/gx-torrent" "$stage/run.sh"
  tar -C "$stage" -czf "$DIST/gextto-linux-$arch.tar.gz" .

  stage="$TMP/gx-$arch"
  mkdir -p "$stage"
  printf 'engine\n' > "$stage/gx-torrent"
  chmod 0755 "$stage/gx-torrent"
  tar -C "$stage" -czf "$DIST/gx-torrent-linux-$arch.tar.gz" .
done
python3 - "$DIST" <<'PY'
import pathlib
import sys
import zipfile

root = pathlib.Path(sys.argv[1])
for arch in ("amd64", "arm64"):
    with zipfile.ZipFile(root / f"gx-torrent-windows-{arch}.zip", "w") as archive:
        archive.writestr("gx-torrent.exe", b"engine")
PY
while IFS= read -r payload; do
  (cd "$DIST" && sha256sum "$payload" > "$payload.sha256")
done < <("$ROOT/scripts/verify-release-assets.sh" --list)

cat > "$MOCK_BIN/gh" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$MOCK_LOG"
if [[ "${1:-}" == "release" ]]; then
  case "${2:-}" in
    view)
      [[ "${MOCK_VIEW:-ok}" == "ok" ]] || exit 1
      ;;
    download)
      dir=""
      while [[ $# -gt 0 ]]; do
        if [[ "$1" == "--dir" ]]; then dir="$2"; shift 2; else shift; fi
      done
      if [[ -n "$dir" ]]; then cp "$FIXTURE_DIST"/* "$dir"/ 2>/dev/null || true; fi
      ;;
  esac
fi
if [[ "${1:-}" == "api" && " $* " == *" --jq "* ]]; then
  printf '%s\n' "$PUBLISH_COMMIT"
fi
exit 0
MOCK
chmod +x "$MOCK_BIN/gh"
# A retry must never fire when every gh call succeeds: a no-op sleep keeps a
# regression loud and fast instead of hiding it behind 75 seconds of backoff.
# shellcheck disable=SC2016  # the variable expands when the generated mock runs
printf '#!/usr/bin/env bash\nprintf "sleep\\n" >> "$MOCK_SLEEP_LOG"\nexit 0\n' > "$MOCK_BIN/sleep"
chmod +x "$MOCK_BIN/sleep"

COMMIT="$(git -C "$ROOT" rev-parse HEAD)"
export MOCK_LOG="$TMP/gh.log" MOCK_SLEEP_LOG="$TMP/sleep.log" FIXTURE_DIST="$DIST"
export PUBLISH_COMMIT="$COMMIT"
: > "$MOCK_LOG"
: > "$MOCK_SLEEP_LOG"

publish() {
  PATH="$MOCK_BIN:$PATH" bash "$ROOT/scripts/publish-github-release.sh" \
    --channel continuous --tag continuous --label continuous-test \
    --dist "$DIST" --repo owner/repo --commit "$COMMIT"
}

# Existing rolling release: it is drafted, never recreated, and published.
publish > "$TMP/out" 2>&1
grep -qF 'release view' "$MOCK_LOG" || { echo "release view was never called" >&2; exit 1; }
if grep -qF 'release create' "$MOCK_LOG"; then echo "existing release was recreated" >&2; exit 1; fi
grep -qF 'published continuous' "$TMP/out" || { echo "publication did not complete" >&2; exit 1; }
check_no_retry() {
  if [[ -s "$MOCK_SLEEP_LOG" ]]; then
    echo "a gh call was retried although it succeeded" >&2
    echo "--- gh calls ---" >&2
    cat "$MOCK_LOG" >&2
    echo "--- output ---" >&2
    cat "$TMP/out" >&2
    exit 1
  fi
}
check_no_retry

# Missing release: the probe legitimately fails, then a draft is created.
: > "$MOCK_LOG"
: > "$MOCK_SLEEP_LOG"
MOCK_VIEW=missing publish > "$TMP/out" 2>&1
grep -qF 'release create' "$MOCK_LOG" || { echo "missing release was not created" >&2; exit 1; }
grep -qF 'published continuous' "$TMP/out" || { echo "publication did not complete" >&2; exit 1; }

echo "publish flow checks passed"
