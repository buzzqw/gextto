#!/usr/bin/env bash
# Build the docker-full Gextto image with the versions of this checkout. It
# reads the same single sources as make build: build_number, version_series,
# gx-torrent.build_number and gx_torrent.version_series. It does not bump them.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

GEXTTO_SERIES="$(tr -d '[:space:]' < version_series)"
GEXTTO_BUILD="$(tr -dc '0-9' < build_number)"
GX_SERIES="$(tr -d '[:space:]' < gx_torrent.version_series 2>/dev/null || true)"
[[ -n "$GX_SERIES" ]] || GX_SERIES="1.1"
GX_BUILD="$(tr -dc '0-9' < gx-torrent.build_number)"

# indexarr is built from its own checkout (INDEXARR_SRC in docker-full/.env,
# relative to docker-full/; default: next to this repository). Its version is
# the checkout's git description, like its own make build.
INDEXARR_SRC="${INDEXARR_SRC:-}"
if [[ -z "$INDEXARR_SRC" && -f docker-full/.env ]]; then
	INDEXARR_SRC="$(sed -n 's/^INDEXARR_SRC=//p' docker-full/.env | tail -n 1)"
fi
INDEXARR_SRC="${INDEXARR_SRC:-../../indexarr}"
case "$INDEXARR_SRC" in
/*) INDEXARR_DIR="$INDEXARR_SRC" ;;
*) INDEXARR_DIR="$ROOT/docker-full/$INDEXARR_SRC" ;;
esac
if [[ ! -f "$INDEXARR_DIR/Dockerfile" ]]; then
	echo "indexarr checkout not found in $INDEXARR_DIR: clone it there or set INDEXARR_SRC in docker-full/.env" >&2
	exit 1
fi
INDEXARR_VERSION="$(git -C "$INDEXARR_DIR" describe --tags --always 2>/dev/null || echo dev)"

export GEXTTO_SERIES GEXTTO_BUILD GX_SERIES GX_BUILD INDEXARR_SRC INDEXARR_VERSION
exec docker compose -f docker-full/docker-compose.yml build "$@"
