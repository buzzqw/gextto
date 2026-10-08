#!/usr/bin/env bash
# Build the gextto daemon with the version and build number compiled in.
#
#   scripts/build-daemon.sh              # use the current build number
#   GEXTTO_BUMP_BUILD=1 scripts/...      # increment the build number first
#   GEXTTO_BUILD=1234 scripts/...        # force a specific number (CI)
#   GEXTTO_BINARY=/path/gexttod scripts/...
#
# The build number is what `gexttod --version` prints and identifies the exact
# binary. The gx-torrent daemon (pure Go, optional torrent backend) is built
# next to gexttod, where Gextto's managed mode looks for it; set
# GEXTTO_SKIP_GXTORRENT=1 to skip it.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${GEXTTO_BINARY:-$ROOT/bin/gexttod}"

VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION" 2>/dev/null || true)"
[[ -n "$VERSION" ]] || VERSION="0.1.0"

if [[ -n "${GEXTTO_BUILD:-}" ]]; then
    BUILD="$GEXTTO_BUILD"
elif [[ "${GEXTTO_BUMP_BUILD:-0}" == "1" ]]; then
    BUILD="$("$ROOT/scripts/next-build-number.sh")"
else
    BUILD="$(tr -dc '0-9' < "$ROOT/build_number" 2>/dev/null || true)"
    [[ -n "$BUILD" ]] || BUILD=1000
fi

mkdir -p "$(dirname "$OUT")"
(
    cd "$ROOT"
    TAGS=()
    [[ -n "${GEXTTO_TAGS:-}" ]] && TAGS=(-tags "$GEXTTO_TAGS")
    CGO_ENABLED=1 go build -trimpath "${TAGS[@]}" \
        -ldflags "-s -w -X github.com/buzzqw/gextto/internal/constants.Version=$VERSION -X github.com/buzzqw/gextto/internal/constants.Build=$BUILD" \
        -o "$OUT" ./cmd/gexttod
)
printf 'built %s (version %s, build %s)\n' "$OUT" "$VERSION" "$BUILD"

if [[ "${GEXTTO_SKIP_GXTORRENT:-0}" != "1" ]]; then
    # gx-torrent is replaced only when its code changed. Gextto keeps a running
    # daemon across its own restarts as long as the binary is the same, so an
    # update that touches only gexttod does not drop the transfers. The code is
    # compared on a build without the build number (stored in
    # gx-torrent.code-sha256); the installed binary keeps the build number of
    # its last real change.
    GX_OUT="$(dirname "$OUT")/gx-torrent"
    GX_HASH_FILE="$GX_OUT.code-sha256"
    GX_PROBE="$(mktemp "${TMPDIR:-/tmp}/gx-torrent-probe.XXXXXX")"
    trap 'rm -f "$GX_PROBE"' EXIT
    (
        cd "$ROOT"
        CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$GX_PROBE" ./cmd/gx-torrent
    )
    GX_HASH="$(sha256sum "$GX_PROBE" | cut -d' ' -f1)"
    if [[ -x "$GX_OUT" && -f "$GX_HASH_FILE" && "$(cat "$GX_HASH_FILE")" == "$GX_HASH" && "${GEXTTO_FORCE_GXTORRENT:-0}" != "1" ]]; then
        printf 'gx-torrent unchanged, kept %s\n' "$GX_OUT"
    else
        (
            cd "$ROOT"
            CGO_ENABLED=0 go build -trimpath \
                -ldflags "-s -w -X github.com/buzzqw/gextto/internal/constants.Build=$BUILD" \
                -o "$GX_OUT.new" ./cmd/gx-torrent
        )
        mv -f "$GX_OUT.new" "$GX_OUT"
        printf '%s\n' "$GX_HASH" > "$GX_HASH_FILE"
        printf 'built %s\n' "$GX_OUT"
    fi
fi
