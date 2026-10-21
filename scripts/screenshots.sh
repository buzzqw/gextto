#!/usr/bin/env bash
# Regenerate the screenshots of the README and of the guided tour
# (docs/TOUR.en.md, docs/TOUR.it.md) from a live test instance.
#
#   make screenshots                  # or scripts/screenshots.sh
#
# It compiles gexttod and gx-torrent into a work directory (bin/ and the
# committed build numbers are left alone, so the running service is never
# touched), starts a real instance there — not dry-run, which cannot add
# torrents — with automation off, its own ports and a download cap, adds a few
# legal torrents (Debian, Ubuntu Server and the Blender open movies) and a
# small public-domain library, lets them download for a while, then drives the
# interface with Playwright (uiweb/end2end/screenshots.config.ts) and writes
# docs/screenshots/<lang>/<name>.png. The work directory is removed at the end.
#
# The downloads are real: a few hundred MB on the line and on the disk of the
# work directory, which lives under ~/.cache (not /tmp, often a tmpfs in RAM).
#
# Settings (environment):
#   GEXTTO_SHOTS_WORK     work directory     (default ~/.cache/gextto-screenshots)
#   GEXTTO_SHOTS_SETTLE   seconds of download before the shots (default 150)
#   GEXTTO_SHOTS_DL       download cap in KiB/s (default 2000)
#   GEXTTO_SHOTS_LANGS    languages with every shot (default en,it)
#   GEXTTO_SHOTS_DASHBOARD_LANGS  languages with the dashboard only (default de,fr,es,pl)
#   GEXTTO_SHOTS_KEEP=1   keep the instance running and the work directory
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="${GEXTTO_SHOTS_WORK:-${XDG_CACHE_HOME:-$HOME/.cache}/gextto-screenshots}"
SETTLE="${GEXTTO_SHOTS_SETTLE:-150}"
DL_LIMIT="${GEXTTO_SHOTS_DL:-2000}"
UI_PORT=15023
ENGINE_PORT=18912
GX_PORT=18913
# Peer ports clear of production (51300-51400), gx-core-test (51500-51510)
# and the default 6881-6891.
PEER_MIN=51600
PEER_MAX=51610
URL="http://127.0.0.1:$UI_PORT"

command -v npx >/dev/null || { echo "npx (Node.js) is required" >&2; exit 1; }
command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }

rm -rf "$WORK"
mkdir -p "$WORK/bin" "$WORK/data"

cleanup() {
    if [[ "${GEXTTO_SHOTS_KEEP:-0}" == "1" ]]; then
        printf 'kept: %s on %s (stop: pkill -f %s)\n' "$WORK" "$URL" "$WORK/bin"
        return
    fi
    # gx-torrent outlives its gexttod by design: stop both, then drop the data.
    pkill -f "$WORK/bin/gexttod" 2>/dev/null || true
    sleep 2
    pkill -f "$WORK/bin/gx-torrent" 2>/dev/null || true
    sleep 1
    rm -rf "$WORK"
}
trap cleanup EXIT

# Build into the work directory with the committed build numbers, without
# bumping them (scripts/build-daemon.sh would bump gx-torrent's on a new
# output directory).
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION" 2>/dev/null || echo 0.1.0)"
BUILD="$(tr -dc '0-9' < "$ROOT/build_number")"
GX_BUILD="$(tr -dc '0-9' < "$ROOT/gx-torrent.build_number")"
COMMIT="$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || true)"
PKG=github.com/buzzqw/gextto/internal/constants
(
    cd "$ROOT"
    CGO_ENABLED=0 nice -n 10 go build -trimpath \
        -ldflags "-s -w -X $PKG.Version=$VERSION -X $PKG.Build=$BUILD -X $PKG.Commit=$COMMIT" \
        -o "$WORK/bin/gexttod" ./cmd/gexttod
    CGO_ENABLED=0 nice -n 10 go build -trimpath -buildvcs=false \
        -ldflags "-s -w -X $PKG.GxTorrentBuild=$GX_BUILD -X $PKG.Commit=$COMMIT" \
        -o "$WORK/bin/gx-torrent" ./cmd/gx-torrent
)
printf 'built gexttod %s (build %s) and gx-torrent (build %s) in %s\n' "$VERSION" "$BUILD" "$GX_BUILD" "$WORK/bin"

start() {
    (
        cd "$ROOT"
        GEXTTO_ACTIVE=0 GEXTTO_DRY_RUN=0 GEXTTO_DATA_DIR="$WORK/data" \
        GEXTTO_LISTEN="127.0.0.1:$UI_PORT" GEXTTO_ENGINE_LISTEN="127.0.0.1:$ENGINE_PORT" \
        GEXTTO_GXTORRENT_LISTEN="127.0.0.1:$GX_PORT" \
            nohup nice -n 10 "$WORK/bin/gexttod" >"$WORK/gexttod.out" 2>&1 &
    )
    for _ in $(seq 1 120); do
        curl -sf "$URL/api/status" >/dev/null && return 0
        sleep 1
    done
    echo "the test instance did not start, see $WORK/gexttod.out" >&2
    exit 1
}

stop() {
    pkill -f "$WORK/bin/gexttod" 2>/dev/null || true
    sleep 3
    pkill -f "$WORK/bin/gx-torrent" 2>/dev/null || true
    sleep 2
}

post() { # path json
    curl -sf -X POST -H "Origin: $URL" -H 'Content-Type: application/json' -d "$2" "$URL$1" >/dev/null \
        || { echo "POST $1 failed" >&2; exit 1; }
}

setting() { post /api/config/settings "{\"key\":\"$1\",\"value\":\"$2\"}"; }

# Phase 1: skip the wizard and write the settings that need a restart (peer
# ports, speed rule) plus the library.
start
curl -sf -X POST -H "Origin: $URL" -d action=skip "$URL/setup/finish" -o /dev/null
# gx-torrent takes Gextto's language when it starts: English for its page.
post /api/i18n/language '{"lang":"en"}'
setting torrent_port_min "$PEER_MIN"
setting torrent_port_max "$PEER_MAX"
setting torrent_speed_mode fixed
setting torrent_dl_limit "$DL_LIMIT"
setting torrent_ul_limit 600
# A Creative Commons series (VODO) and public-domain movies: names only, the
# automation is off so nothing is searched.
post /api/config/series '[{"name":"Pioneer One","seasons":"1","quality":"1080p","language":"en","enabled":true,"season_subfolders":true}]'
post /api/config/movies '[{"id":1,"name":"Night of the Living Dead","year":"1968","quality":"1080p","language":"en","enabled":true},{"id":2,"name":"Nosferatu","year":"1922","quality":"1080p","enabled":true},{"id":3,"name":"Plan 9 from Outer Space","year":"1957","quality":"720p","enabled":true},{"id":4,"name":"Charade","year":"1963","quality":"1080p","enabled":true}]'
stop

# Phase 2: the real instance, with legal torrents downloading.
start
sleep 5
debian="$(curl -sf https://cdimage.debian.org/debian-cd/current/amd64/bt-cd/ | grep -oE 'debian-[0-9.]+-amd64-netinst\.iso\.torrent' | head -n1 || true)"
ubuntu="$(curl -sf https://releases.ubuntu.com/24.04/ | grep -oE 'ubuntu-[0-9.]+-live-server-amd64\.iso\.torrent' | head -n1 || true)"
torrents=(
    https://webtorrent.io/torrents/big-buck-bunny.torrent
    https://webtorrent.io/torrents/sintel.torrent
    https://webtorrent.io/torrents/tears-of-steel.torrent
    https://webtorrent.io/torrents/cosmos-laundromat.torrent
)
[[ -n "$debian" ]] && torrents+=("https://cdimage.debian.org/debian-cd/current/amd64/bt-cd/$debian")
[[ -n "$ubuntu" ]] && torrents+=("https://releases.ubuntu.com/24.04/$ubuntu")
for t in "${torrents[@]}"; do
    post /api/torrents/add "{\"magnet\":\"$t\"}"
done
printf 'added %d torrents, downloading for %ss\n' "${#torrents[@]}" "$SETTLE"
sleep "$SETTLE"

(
    cd "$ROOT/uiweb/end2end"
    [[ -d node_modules ]] || npm ci --no-audit --no-fund
    GEXTTO_SHOTS_PORT="$UI_PORT" GEXTTO_SHOTS_GX_PORT="$GX_PORT" \
    GEXTTO_SHOTS_RESULTS="$WORK/results" \
        nice -n 10 npx playwright test -c screenshots.config.ts
)
printf 'screenshots written to %s\n' "${GEXTTO_SHOTS_DIR:-$ROOT/docs/screenshots}"
