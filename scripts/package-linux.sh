#!/usr/bin/env bash
# Build the standalone Linux archive consumed by the installer and by
# `gexttod --update`. The Go binary embeds the compiled web UI, so the archive
# ships the daemon, the bundled libtorrent shared libraries, a launcher and a
# short README. Layout:
#   gexttod  run.sh  lib/  README.md  VERSION
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BINARY="${ROOT}/bin/gexttod"
OUTPUT=""
ARCH="$(uname -m)"
CHANNEL="${GEXTTO_CHANNEL:-continuous}"
LABEL=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary) BINARY="$2"; shift 2 ;;
    --output) OUTPUT="$2"; shift 2 ;;
    --arch) ARCH="$2"; shift 2 ;;
    --label) LABEL="$2"; shift 2 ;;
    -h|--help) echo "usage: package-linux.sh [--binary PATH] [--output FILE] [--arch ARCH] [--label TEXT]"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

[[ -n "$LABEL" ]] || LABEL="$CHANNEL"

if [[ ! -x "$BINARY" ]]; then
  echo "binary not found, building: $BINARY"
  GEXTTO_BINARY="$BINARY" "${ROOT}/scripts/build-daemon.sh"
fi

[[ -z "$OUTPUT" ]] && OUTPUT="${ROOT}/gextto-linux-${ARCH}.tar.gz"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

cp "$BINARY" "$STAGE/gexttod"
chmod +x "$STAGE/gexttod"
mkdir -p "$STAGE/lib"

# Bundle the libtorrent shared libraries next to the executable: the daemon is
# linked with an $ORIGIN/lib rpath, so it runs straight from the archive.
LIBDIR="$(pkg-config --variable=libdir libtorrent-rasterbar 2>/dev/null || echo /usr/lib)"
for lib in libtorrent-rasterbar.so; do
  for candidate in "$LIBDIR/$lib" /usr/lib/$lib /usr/lib/x86_64-linux-gnu/$lib /usr/local/lib/$lib; do
    if [[ -e "$candidate" ]]; then
      cp -a "$candidate"* "$STAGE/lib/" 2>/dev/null || true
      break
    fi
  done
done

cat > "$STAGE/run.sh" <<'RUN'
#!/usr/bin/env bash
# Launch gextto from an extracted archive.
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
export LD_LIBRARY_PATH="$DIR/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
if [[ -d "$DIR/ui" ]]; then
  export GEXTTO_UI_DIR="$DIR/ui"
fi
exec "$DIR/gexttod" "$@"
RUN
chmod +x "$STAGE/run.sh"

cp "${ROOT}/README.md" "$STAGE/README.md" 2>/dev/null || true
echo "$LABEL" > "$STAGE/VERSION"

# Go binaries embed the UI; no ui/ directory is required. Keep one if the caller
# provided an override (GEXTTO_UI_DIR points at a source tree).
if [[ -n "${GEXTTO_UI_DIR:-}" && -d "${GEXTTO_UI_DIR}" ]]; then
  cp -a "${GEXTTO_UI_DIR}" "$STAGE/ui"
fi

tar -C "$STAGE" -czf "$OUTPUT" .
( cd "$(dirname "$OUTPUT")" && sha256sum "$(basename "$OUTPUT")" > "$(basename "$OUTPUT").sha256" )

echo "archive: $OUTPUT"
echo "checksum: $OUTPUT.sha256"
