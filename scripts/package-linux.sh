#!/usr/bin/env bash
# Build the standalone Linux archive consumed by the installer and by
# `gexttod --update`. The Go binary embeds the server-rendered web UI, so the archive
# ships the daemon, the bundled libtorrent shared libraries, a launcher and a
# short README. Layout:
#   gexttod  gx-torrent  run.sh  lib/  README.md  VERSION
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BINARY="${ROOT}/bin/gexttod"
OUTPUT=""
ARCH="$(uname -m)"
CHANNEL="${GEXTTO_CHANNEL:-continuous}"
LABEL=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary|--output|--arch|--label)
      option="$1"
      [[ $# -ge 2 && -n "$2" ]] || { echo "$option requires a value" >&2; exit 2; }
      case "$option" in
        --binary) BINARY="$2" ;;
        --output) OUTPUT="$2" ;;
        --arch) ARCH="$2" ;;
        --label) LABEL="$2" ;;
      esac
      shift 2
      ;;
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
# gx-torrent is the default torrent engine: the archive must ship it next to
# gexttod or a fresh install would fall back to libtorrent. build-daemon.sh
# builds it by default; refuse to package without it unless explicitly allowed.
GX_BINARY="$(dirname "$BINARY")/gx-torrent"
if [[ -x "$GX_BINARY" ]]; then
  cp "$GX_BINARY" "$STAGE/gx-torrent"
  chmod +x "$STAGE/gx-torrent"
elif [[ "${GEXTTO_ALLOW_MISSING_GXTORRENT:-0}" == "1" ]]; then
  echo "warning: gx-torrent not found next to $BINARY; the archive will only run with embedded libtorrent" >&2
else
  echo "error: gx-torrent binary not found at $GX_BINARY" >&2
  echo "       build it with scripts/build-daemon.sh, or set GEXTTO_ALLOW_MISSING_GXTORRENT=1 to package without it" >&2
  exit 1
fi
mkdir -p "$STAGE/lib"

# Bundle the libtorrent shared libraries next to the executable: the daemon is
# linked with an $ORIGIN/lib rpath, so it runs straight from the archive.
LIBDIR="$(pkg-config --variable=libdir libtorrent-rasterbar 2>/dev/null || echo /usr/lib)"
LIBTORRENT="libtorrent-rasterbar.so"
BUNDLED_LIB=0
for candidate in "$LIBDIR/$LIBTORRENT" "/usr/lib/$LIBTORRENT" "/usr/lib/x86_64-linux-gnu/$LIBTORRENT" "/usr/local/lib/$LIBTORRENT"; do
  if [[ -e "$candidate" ]]; then
    cp -a "$candidate"* "$STAGE/lib/" 2>/dev/null || true
    BUNDLED_LIB=1
    break
  fi
done
if [[ "$BUNDLED_LIB" == "0" ]]; then
  echo "warning: $LIBTORRENT not found; the archive only runs where libtorrent is installed system-wide" >&2
fi

cat > "$STAGE/run.sh" <<'RUN'
#!/usr/bin/env bash
# Launch gextto from an extracted archive.
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
export LD_LIBRARY_PATH="$DIR/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
exec "$DIR/gexttod" "$@"
RUN
chmod +x "$STAGE/run.sh"

cp "${ROOT}/README.md" "$STAGE/README.md" 2>/dev/null || true
echo "$LABEL" > "$STAGE/VERSION"

tar --numeric-owner -C "$STAGE" -czf "$OUTPUT" .
( cd "$(dirname "$OUTPUT")" && sha256sum "$(basename "$OUTPUT")" > "$(basename "$OUTPUT").sha256" )

echo "archive: $OUTPUT"
echo "checksum: $OUTPUT.sha256"
