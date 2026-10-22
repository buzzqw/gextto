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

export GEXTTO_SERIES GEXTTO_BUILD GX_SERIES GX_BUILD
exec docker compose -f docker-full/docker-compose.yml build "$@"
