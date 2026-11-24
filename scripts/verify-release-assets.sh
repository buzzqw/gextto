#!/usr/bin/env bash
# Verify the complete release asset set before a GitHub release is published.
# Every archive must be present, non-empty, accompanied by its own SHA-256 file,
# and contain the executables/launcher/marker promised by the package scripts.
set -euo pipefail

expected_payloads() {
  printf '%s\n' \
    gextto-linux-x86_64.tar.gz \
    gextto-linux-aarch64.tar.gz \
    gx-torrent-linux-x86_64.tar.gz \
    gx-torrent-linux-aarch64.tar.gz \
    gx-torrent-windows-amd64.zip \
    gx-torrent-windows-arm64.zip
}

if [[ "${1:-}" == "--list" ]]; then
  expected_payloads
  exit 0
fi
if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  echo "usage: verify-release-assets.sh [DIRECTORY]"
  exit 0
fi

DIR="${1:-dist}"
[[ -d "$DIR" ]] || { echo "release asset directory not found: $DIR" >&2; exit 1; }
DIR="$(cd "$DIR" && pwd)"

mapfile -t payloads < <(expected_payloads)
for payload in "${payloads[@]}"; do
  archive="$DIR/$payload"
  checksum="$DIR/$payload.sha256"
  [[ -f "$archive" && ! -L "$archive" && -s "$archive" ]] || {
    echo "missing or empty release asset: $payload" >&2
    exit 1
  }
  [[ -f "$checksum" && ! -L "$checksum" ]] || {
    echo "missing checksum for release asset: $payload" >&2
    exit 1
  }
  mapfile -t checksum_lines < "$checksum"
  [[ "${#checksum_lines[@]}" -eq 1 ]] || {
    echo "malformed checksum file for $payload" >&2
    exit 1
  }
  read -r expected filename extra <<< "${checksum_lines[0]}"
  [[ "$expected" =~ ^[[:xdigit:]]{64}$ && "$filename" == "$payload" && -z "$extra" ]] || {
    echo "malformed checksum file for $payload" >&2
    exit 1
  }
  actual="$(sha256sum "$archive" | awk '{print $1}')"
  [[ "${actual,,}" == "${expected,,}" ]] || {
    echo "checksum mismatch for $payload" >&2
    exit 1
  }
done

for arch in x86_64 aarch64; do
  contents="$(tar -tzf "$DIR/gextto-linux-$arch.tar.gz")"
  for entry in ./gexttod ./gx-torrent ./run.sh ./VERSION; do
    grep -Fxq "$entry" <<< "$contents" || {
      echo "gextto-linux-$arch.tar.gz is missing $entry" >&2
      exit 1
    }
  done
  contents="$(tar -tzf "$DIR/gx-torrent-linux-$arch.tar.gz")"
  grep -Fxq './gx-torrent' <<< "$contents" || {
    echo "gx-torrent-linux-$arch.tar.gz is missing ./gx-torrent" >&2
    exit 1
  }
done
for arch in amd64 arm64; do
  contents="$(unzip -Z1 "$DIR/gx-torrent-windows-$arch.zip")"
  grep -Fxq 'gx-torrent.exe' <<< "$contents" || {
    echo "gx-torrent-windows-$arch.zip is missing gx-torrent.exe" >&2
    exit 1
  }
done

echo "verified ${#payloads[@]} release assets and their SHA-256 checksums in $DIR"
