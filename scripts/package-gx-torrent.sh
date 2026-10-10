#!/usr/bin/env bash
# Build the standalone gx-torrent archive: the daemon alone, to be used like
# qbittorrent-nox (web UI, qBittorrent-compatible API, RSS), without Gextto.
#
#   scripts/package-gx-torrent.sh [--binary PATH] [--output FILE] [--arch ARCH]
#                                 [--label TEXT]
#
# Layout of the archive:
#   gx-torrent  gx-torrent.service  run.sh  README.md  VERSION  .sha256
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BINARY="${ROOT}/bin/gx-torrent"
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
    -h|--help) echo "usage: package-gx-torrent.sh [--binary PATH] [--output FILE] [--arch ARCH] [--label TEXT]"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

[[ -n "$LABEL" ]] || LABEL="$CHANNEL"
[[ -z "$OUTPUT" ]] && OUTPUT="${ROOT}/gx-torrent-linux-${ARCH}.tar.gz"

if [[ ! -x "$BINARY" ]]; then
  echo "error: gx-torrent binary not found at $BINARY" >&2
  echo "       build it first with \`make gx-torrent\`" >&2
  exit 1
fi

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

cp "$BINARY" "$STAGE/gx-torrent"
chmod +x "$STAGE/gx-torrent"

cat > "$STAGE/run.sh" <<'RUN'
#!/usr/bin/env bash
# Launch gx-torrent from an extracted archive.
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
exec "$DIR/gx-torrent" "$@"
RUN
chmod +x "$STAGE/run.sh"

cat > "$STAGE/gx-torrent.service" <<'UNIT'
# gx-torrent standalone, usable like qbittorrent-nox.
# Install: copy the extracted directory to /opt/gx-torrent, adjust User/Group,
# then (as root) `cp gx-torrent.service /etc/systemd/system/` and
# `systemctl enable --now gx-torrent`.
[Unit]
Description=gx-torrent (BitTorrent client, web UI on the listen port)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=gx-torrent
Group=gx-torrent
WorkingDirectory=/opt/gx-torrent
# First run opens the wizard at http://<host>:8890/ui/setup
ExecStart=/opt/gx-torrent/gx-torrent -mode standalone -listen 0.0.0.0:8890 -data /var/lib/gx-torrent
Restart=on-failure
RestartSec=3
# NoNewPrivileges/ProtectSystem are conservative defaults; relax if your media
# folders live elsewhere.
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
UNIT

cat > "$STAGE/README.md" <<'README'
# gx-torrent (standalone)

`gx-torrent` is a BitTorrent client with a web UI, a qBittorrent-compatible API
and RSS, usable on its own like `qbittorrent-nox` (it is the same binary Gextto
drives as its torrent engine).

## Run

```bash
./run.sh -mode standalone -listen 0.0.0.0:8890 -data /var/lib/gx-torrent
```

Then open `http://<host>:8890/`: the first run shows the **setup wizard**
(language, download folder, password, LAN access, peer port). The settings are
saved in `settings.json` under the data directory.

## Interfaces

- Web UI: `http://<host>:8890/` (login, torrents, categories, RSS at `/ui/rss`).
- qBittorrent Web API: `/api/v2` (Sonarr, Radarr, mobile apps: add/remove, tags,
  trackers, file priorities, peers, per-torrent limits, super-seeding).
- Indexer search: configure Jackett/Prowlarr/MIRCrew (Torznab) in `/ui/rss` or
  via the `indexers` key in `settings.json`; a feed can also be a **search query**
  (the `search` field) instead of an RSS URL.
- Notifications: set `-notify-url` (or `GX_TORRENT_NOTIFY_URL`) to POST a JSON
  webhook on RSS match and error.
- Container: the `Dockerfile` in the project root builds a standalone image
  (data in `/data`, web UI on port 8080).

## Manage it

Copy the directory to `/opt/gx-torrent`, update `gx-torrent.service`
(`User`, `Group`, paths), then enable the unit. Or run it by hand as above.
README

echo "$LABEL" > "$STAGE/VERSION"

tar --numeric-owner -C "$STAGE" -czf "$OUTPUT" .
( cd "$(dirname "$OUTPUT")" && sha256sum "$(basename "$OUTPUT")" > "$(basename "$OUTPUT").sha256" )

echo "archive: $OUTPUT"
echo "checksum: $OUTPUT.sha256"
