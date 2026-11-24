#!/usr/bin/env bash
# Build small stand-in release archives and exercise the publication verifier.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERIFY="$ROOT/scripts/verify-release-assets.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
DIST="$TMP/dist"
mkdir -p "$DIST"

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
done < <("$VERIFY" --list)
"$VERIFY" "$DIST"

rm "$DIST/gextto-linux-aarch64.tar.gz.sha256"
if "$VERIFY" "$DIST" >/dev/null 2>&1; then
  echo "missing checksum was accepted" >&2
  exit 1
fi
(cd "$DIST" && sha256sum gextto-linux-aarch64.tar.gz > gextto-linux-aarch64.tar.gz.sha256)
printf 'corrupted\n' >> "$DIST/gextto-linux-x86_64.tar.gz"
if "$VERIFY" "$DIST" >/dev/null 2>&1; then
  echo "corrupted payload was accepted" >&2
  exit 1
fi

echo "release asset verification checks passed"
