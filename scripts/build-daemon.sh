#!/usr/bin/env bash
# Build the gextto daemon with the version and build number compiled in.
#
#   scripts/build-daemon.sh              # pure Go (default): no libtorrent
#   GEXTTO_LIBTORRENT=1 scripts/...      # also link the embedded libtorrent
#   GEXTTO_BUMP_BUILD=1 scripts/...      # increment the build number first
#   GEXTTO_BUILD=1234 scripts/...        # force a specific number (CI)
#   GEXTTO_BINARY=/path/gexttod scripts/...
#
# By default the binary is pure Go: gx-torrent (the default engine) is a pure-Go
# daemon and does not need libtorrent-rasterbar. The embedded libtorrent engine
# is opt-in with GEXTTO_LIBTORRENT=1, which enables cgo and needs the dev headers
# and a C/C++ toolchain.
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

# The commit and build time let the in-app update check tell whether a
# published release is newer than the running binary (release.json).
COMMIT="${GEXTTO_COMMIT:-${GITHUB_SHA:-}}"
[[ -n "$COMMIT" ]] || COMMIT="$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || true)"
BUILT_AT="${GEXTTO_BUILT_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

mkdir -p "$(dirname "$OUT")"
# Pure Go by default; GEXTTO_LIBTORRENT=1 links the embedded libtorrent engine.
CGO_ENABLED_BUILD=0
if [[ "${GEXTTO_LIBTORRENT:-0}" == "1" ]]; then
    CGO_ENABLED_BUILD=1
fi
(
    cd "$ROOT"
    TAGS=()
    [[ -n "${GEXTTO_TAGS:-}" ]] && TAGS=(-tags "$GEXTTO_TAGS")
    CGO_ENABLED="$CGO_ENABLED_BUILD" go build -trimpath "${TAGS[@]}" \
        -ldflags "-s -w -X github.com/buzzqw/gextto/internal/constants.Version=$VERSION -X github.com/buzzqw/gextto/internal/constants.Build=$BUILD -X github.com/buzzqw/gextto/internal/constants.Commit=$COMMIT -X github.com/buzzqw/gextto/internal/constants.BuiltAt=$BUILT_AT" \
        -o "$OUT" ./cmd/gexttod
)
if [[ "$CGO_ENABLED_BUILD" == "1" ]]; then
    LIBS="libtorrent"
else
    LIBS="pure Go"
fi
printf 'built %s (version %s, build %s, %s)\n' "$OUT" "$VERSION" "$BUILD" "$LIBS"

if [[ "${GEXTTO_SKIP_GXTORRENT:-0}" != "1" ]]; then
    # gx-torrent is replaced only when its code changed. Gextto keeps a running
    # daemon across its own restarts as long as the binary is the same, so an
    # update that touches only gexttod does not drop the transfers. The code is
    # compared on a build without the build number (stored in
    # gx-torrent.code-sha256); the installed binary keeps the build number of
    # its last real change. -buildvcs=false keeps the comparison meaningful:
    # otherwise Go stamps vcs.revision into the binary and every commit looks
    # like a gx-torrent change.
    GX_OUT="$(dirname "$OUT")/gx-torrent"
    GX_HASH_FILE="$GX_OUT.code-sha256"
    GX_PROBE="$(mktemp "${TMPDIR:-/tmp}/gx-torrent-probe.XXXXXX")"
    trap 'rm -f "$GX_PROBE"' EXIT
    (
        cd "$ROOT"
        CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-s -w" -o "$GX_PROBE" ./cmd/gx-torrent
    )
    GX_HASH="$(sha256sum "$GX_PROBE" | cut -d' ' -f1)"
    if [[ -x "$GX_OUT" && -f "$GX_HASH_FILE" && "$(cat "$GX_HASH_FILE")" == "$GX_HASH" && "${GEXTTO_FORCE_GXTORRENT:-0}" != "1" ]]; then
        printf 'gx-torrent unchanged, kept %s\n' "$GX_OUT"
    else
        # A real gx-torrent build: bump its own build number (independent from
        # Gextto's) and stamp it in.
        GX_BUILD="$("$ROOT/scripts/next-gx-build-number.sh")"
        (
            cd "$ROOT"
            CGO_ENABLED=0 go build -trimpath -buildvcs=false \
                -ldflags "-s -w -X github.com/buzzqw/gextto/internal/constants.GxTorrentBuild=$GX_BUILD" \
                -o "$GX_OUT.new" ./cmd/gx-torrent
        )
        mv -f "$GX_OUT.new" "$GX_OUT"
        printf '%s\n' "$GX_HASH" > "$GX_HASH_FILE"
        printf 'built %s (build %s)\n' "$GX_OUT" "$GX_BUILD"
    fi
fi
