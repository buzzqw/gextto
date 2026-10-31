#!/usr/bin/env bash
# Build the standalone gx-torrent archive: the daemon alone, to be used like
# qbittorrent-nox (web UI, qBittorrent-compatible API, RSS), without Gextto.
#
#   scripts/package-gx-torrent.sh [--binary PATH] [--output FILE] [--arch ARCH]
#                                 [--label TEXT]
#
# Layout of the archive:
#   gx-torrent  gx-torrent.service  run.sh  install-service.sh  uninstall-service.sh  README.md  VERSION  LICENSE  THIRD-PARTY-NOTICES
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
# mktemp -d creates the folder 0700 and it becomes the archive's "./" entry: a
# `tar xzf` into an existing folder (as root, /opt) would turn that folder 0700.
chmod 0755 "$STAGE"

cp "$BINARY" "$STAGE/gx-torrent"
chmod +x "$STAGE/gx-torrent"

cat > "$STAGE/run.sh" <<'RUN'
#!/usr/bin/env bash
# Launch gx-torrent from an extracted archive or install it as a system service.
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
export GX_TORRENT_DATA="${GX_TORRENT_DATA:-$DIR/data}"

if [[ -t 0 && $# -eq 0 ]]; then
  echo "================================================="
  echo " gx-torrent — Standalone BitTorrent Client"
  echo "================================================="
  echo "1) Avvia gx-torrent direttamente / Start gx-torrent directly"
  echo "2) Installa il servizio systemd / Install systemd service (gx-torrent.service)"
  read -r -p "Scelta / Choice [1/2] (default 1): " choice
  if [[ "$choice" == "2" ]]; then
    if [[ -x "$DIR/install-service.sh" ]]; then
      if [[ "$(id -u)" != "0" ]]; then
        exec sudo "$DIR/install-service.sh"
      else
        exec "$DIR/install-service.sh"
      fi
    fi
  fi
fi

exec "$DIR/gx-torrent" "$@"
RUN
chmod +x "$STAGE/run.sh"

cat > "$STAGE/install-service.sh" <<'INSTALL'
#!/usr/bin/env bash
# Install gx-torrent as a systemd system service. Run as root:
#   sudo ./install-service.sh
set -euo pipefail
if [[ "$(id -u)" != "0" ]]; then
  echo "error: run as root (sudo ./install-service.sh)" >&2
  exit 1
fi
command -v systemctl >/dev/null 2>&1 || { echo "error: systemd not found" >&2; exit 1; }
DIR="$(cd "$(dirname "$0")" && pwd)"
for file in gx-torrent run.sh gx-torrent.service; do
  [[ -e "$DIR/$file" ]] || { echo "error: $file not found next to this script" >&2; exit 1; }
done

# Dedicated system user, no login. Its home is the state directory, which systemd
# creates on first start (StateDirectory=gx-torrent in the unit).
if ! id -u gx-torrent >/dev/null 2>&1; then
  useradd -r -s /usr/sbin/nologin -d /var/lib/gx-torrent gx-torrent
fi

install -d -o gx-torrent -g gx-torrent /opt/gx-torrent /var/lib/gx-torrent
install -m 0755 -o gx-torrent -g gx-torrent "$DIR/gx-torrent" /opt/gx-torrent/gx-torrent
install -m 0755 -o gx-torrent -g gx-torrent "$DIR/run.sh" /opt/gx-torrent/run.sh
install -m 0644 "$DIR/gx-torrent.service" /etc/systemd/system/gx-torrent.service
systemctl daemon-reload
systemctl enable --now gx-torrent
echo "gx-torrent service installed and started (web UI on the listen port, default 8890)."
INSTALL
chmod +x "$STAGE/install-service.sh"

cat > "$STAGE/uninstall-service.sh" <<'UNINSTALL'
#!/usr/bin/env bash
# Remove the gx-torrent systemd service. Data under /var/lib/gx-torrent is kept.
# Run as root: sudo ./uninstall-service.sh
set -euo pipefail
if [[ "$(id -u)" != "0" ]]; then
  echo "error: run as root (sudo ./uninstall-service.sh)" >&2
  exit 1
fi
systemctl disable --now gx-torrent 2>/dev/null || true
rm -f /etc/systemd/system/gx-torrent.service
systemctl daemon-reload
echo "gx-torrent service removed (data in /var/lib/gx-torrent kept)."
UNINSTALL
chmod +x "$STAGE/uninstall-service.sh"

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
# systemd creates /var/lib/gx-torrent (owned by the service user) before start.
StateDirectory=gx-torrent
WorkingDirectory=/opt/gx-torrent
# First run opens the wizard at http://<host>:8890/ui/setup
ExecStart=/opt/gx-torrent/gx-torrent -mode standalone -listen 0.0.0.0:8890 -data /var/lib/gx-torrent
Restart=on-failure
RestartSec=3
# Conservative hardening: the daemon only writes under its data directory
# (/var/lib/gx-torrent) and the download folders. If those live under /home,
# add them explicitly, e.g. ReadWritePaths=/home/user/media.
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=read-only

[Install]
WantedBy=multi-user.target
UNIT

cat > "$STAGE/README.md" <<'README'
# gx-torrent — Standalone BitTorrent Client & Daemon

`gx-torrent` is a lightweight, pure-Go BitTorrent client and daemon designed as
a drop-in replacement for `qbittorrent-nox` and `transmission-daemon`. It features
a modern web UI, full qBittorrent Web API v2 compatibility for automation suites,
RSS rules with smart episode tracking, and direct multi-indexer search.

It is powered by **gx-core**, an advanced engine supporting BitTorrent v1,
hybrids, and v2-only (BEP 52 Merkle trees), uTP (BEP 29), NAT holepunching
(BEP 55), Super-seeding (BEP 16), IPv6 dual-stack DHT (BEP 32/42/44), and
zero-allocation MSE/PE stream encryption, with a small footprint (29–39 MB at
rest). It is one pure-Go process with no external engine, no C library and no
database server; the figures below are the whole daemon, not a part of it.

---

## 1. Quick Start

### Running Directly

```bash
./run.sh -mode standalone -listen 0.0.0.0:8890 -data /var/lib/gx-torrent
```

Or using the binary directly:
```bash
./gx-torrent -mode standalone -listen 0.0.0.0:8890 -data /var/lib/gx-torrent
```

At startup the daemon prints the addresses where its web UI is reachable, e.g.
`web UI: http://192.168.1.10:8890/` (LAN) and `web UI: http://127.0.0.1:8890/`
(this host), so you can open the page without looking up the IP.

### Initial Setup Wizard

Open your browser at `http://<server-ip>:8890/`.
On the first launch, the interactive **Setup Wizard** guides you through:
1. **Interface Language**: Select English, Italian, German, French, Spanish, or Polish.
2. **Storage Paths**: The default download folder and the temporary (incomplete) folder are pre-filled with two subfolders of the directory that holds the daemon binary (`download`, or its word in the chosen language, and `download-temp`); accept them or point the fields at your own paths.
3. **Connectivity & Port Test**: Select the incoming peer port (default `6881`) and run the built-in reachability test to verify router port forwarding (UPnP / NAT-PMP).
4. **Global Bandwidth Limits**: Set optional download and upload rate limits.
5. **Security & Authentication**: Set the admin username and password. You can optionally allow password-free access from the local area network (LAN).
6. **Indexer Integration**: Optionally pre-configure Torznab indexers (Jackett, Prowlarr, MIRCrew).

Settings are automatically saved to `<data-dir>/settings.json`.

---

## 2. CPU and Memory Footprint

`gx-torrent` is a single pure-Go process (`CGO_ENABLED=0`): the engine, the
queue, the web UI, the RSS reader and the qBittorrent-compatible API all live in
it. There is no libtorrent, no Python and no database server to run alongside.

These are measurements of the whole daemon, on an **Intel N97 mini-PC (4 cores,
16 GB RAM)**, taken on 26–30 October 2026; the at-rest and public-swarm figures
are with DHT enabled and a full IP filter loaded (172,740 rules). The machine,
the date and the command for every figure are in
[docs/measurements.md](https://github.com/buzzqw/gextto/blob/main/docs/measurements.md).

| Situation | CPU | Memory (RSS) |
|---|---|---|
| At rest | **0.6–1.0 %** of one core | **29–39 MB** |
| Downloading from public swarms at 8.2–8.8 MB/s with 116–132 peers | **18–37 %** of one core, **2.0–4.3 s of CPU per 100 MB** | — |
| Loopback transfer (seed and leecher on the same host) | **7.5–8.3 s of CPU per 1.09 GiB** for both sides | **≈100 MB peak** |
| Five minutes after a burst, with no transfer | — | **550 MB → 35–40 MB** |

At rest the CPU is mostly real network work — the DHT and incoming peer
handshakes — not internal polling. The connection cap is sized from the machine
(55 peers per torrent and 300 in total on this 4-core host), so a large number
of active torrents does not turn into an unbounded CPU bill. Memory held by
piece buffers is returned after a burst instead of staying reserved.

---

## 3. Production Service Setup (systemd)

To run `gx-torrent` as a permanent background service on Linux:

```bash
sudo ./install-service.sh
```

The script creates the `gx-torrent` system user, installs the binary under
`/opt/gx-torrent`, copies the unit to `/etc/systemd/system/`, then enables and
starts the service. `StateDirectory=gx-torrent` makes systemd create and own
`/var/lib/gx-torrent` (the state directory) before start. Remove the service
with `sudo ./uninstall-service.sh`; the data under `/var/lib/gx-torrent` is kept.

**Download folder permissions**: ensure your target download directory (e.g.
`/srv/torrents` or `/mnt/storage`) is readable and writable by the `gx-torrent`
user or group. *Note*: The bundled unit enables systemd security hardening
(`ProtectHome=read-only`). If your media directory is under `/home`, add an
override:
```bash
sudo systemctl edit gx-torrent
# Add:
# [Service]
# ReadWritePaths=/home/username/downloads
```

**Manual installation** (equivalent to the script): copy `gx-torrent` and
`run.sh` to `/opt/gx-torrent`, create the `gx-torrent` user, copy
`gx-torrent.service` to `/etc/systemd/system/`, then
`sudo systemctl daemon-reload && sudo systemctl enable --now gx-torrent`.

### Updates

*Settings → Maintenance* checks for a new release on a timer (every 6 hours by
default) or when you press **Check for updates**. When gx-torrent's own code
changed the button becomes **Update**: it downloads this archive again from the
same release channel, verifies the SHA-256, tries the new binary, keeps the old
one as `gx-torrent.prev`, and restarts in place; a popup then lists what
changed. If the new version fails to start three times, the previous one is
restored by itself. The service user must be able to write the program folder
(`/opt/gx-torrent`, as installed by `install-service.sh`). On Windows the page
checks and links the new zip; install it by hand.

---

## 4. Web UI Features & Usage

Access the dashboard at `http://<server-ip>:8890/`:

- **Torrent Management**:
  - **Add Torrents**: Add via `.torrent` file upload, magnet link, or direct HTTP/HTTPS URL. Set initial category, tags, start paused, or select specific files.
  - **Control**: Pause, resume, recheck integrity, force announce to trackers, and relocate download files to a different path on the fly.
  - **Removal**: Delete the torrent metadata only, or remove both torrent and downloaded data.

- **Detailed Inspector Tabs**:
  - **General**: Progress, ratio, download/upload speed, transfer totals, ETA, info-hash, and piece availability map.
  - **Files**: Inspect all files, toggle individual file selection (skip unwanted files), and monitor completion percentages.
  - **Trackers**: Real-time status of all tracker tiers, seeders/leechers counts, announce intervals, and scrape responses.
  - **Peers**: Connected peers list with client identification, IP, country, flags, transfer rates, transport protocol (TCP vs uTP), and encryption status.
  - **HTTP Seeds**: Manage WebSeed URLs (BEP 19).
  - **Limits**: Configure per-torrent maximum download/upload rates, connection limits, and upload slot allocations.

- **Categories & Tags**:
  - Create custom categories with dedicated save paths (e.g. `movies` -> `/data/movies`, `tv` -> `/data/tv`). New torrents assigned to a category automatically inherit its destination path.
  - Filter and organize swarms using custom color-coded tags.

- **In-Browser Media Streaming**:
  - Stream MKV and MP4 video files directly inside your browser or open them in an external media player (VLC, mpv) while downloading via HTTP Range streaming.
  - The engine's dynamic piece picker automatically shifts piece priority to the video playhead window and readahead buffer.

- **RSS Automation & Multi-Indexer Search (`/ui/rss`)**:
  - Add standard RSS feeds or configure Torznab indexers (Jackett, Prowlarr, MIRCrew).
  - Create automated download rules using regular expressions, required/excluded keywords, target categories, and quality tags.
  - **Smart Episode Filter**: Remembers downloaded series and seasons to prevent re-fetching duplicate or older episodes.

---

## 5. Automation & *arr Integration (Sonarr, Radarr, Lidarr, Mobile Apps)

`gx-torrent` provides native, drop-in compatibility with the **qBittorrent Web API v2** (`/api/v2`). You can connect Sonarr, Radarr, Lidarr, Readarr, and mobile management apps without any proxy or bridge.

In Sonarr / Radarr (*Settings → Download Clients → Add → qBittorrent*):
- **Name**: `gx-torrent`
- **Host**: `<server-ip>` (or `localhost` if on the same host)
- **Port**: `8890` (or your configured port)
- **Username**: `admin` (or the username set in the wizard)
- **Password**: The password configured in the setup wizard
- **Category**: Set as needed (e.g. `tv-sonarr` or `radarr`)

---

## 6. Command-Line Reference & Environment Variables

| Flag | Environment Variable | Default | Description |
|---|---|---|---|
| `-mode` | `GX_TORRENT_MODE` | `standalone` | Operating mode: `standalone` or `managed` (by Gextto) |
| `-listen` | `GX_TORRENT_LISTEN` | `0.0.0.0:8890` | Web UI and REST API bind address (standalone listens on the whole LAN; use `127.0.0.1:8890` to keep it on this host only) |
| `-data` | `GX_TORRENT_DATA` | `~/.local/share/gx-torrent` | State directory (`settings.json`, session database, resume states) |
| `-download-dir` | `GX_TORRENT_DOWNLOAD_DIR` | `<data>/downloads` | Default save path for downloaded files |
| `-peer-ports` | `GX_TORRENT_PEER_PORTS` | `6881-6891` | Peer listening port range (uses first free port; `0` = dynamic per torrent) |
| `-listen-interface` | `GX_TORRENT_LISTEN_INTERFACE` | all | Network interface or IP for incoming peer connections |
| `-outgoing-interface`| `GX_TORRENT_OUTGOING_INTERFACE`| default | Bind outgoing traffic to interface/IP (VPN killswitch) |
| `-proxy` | `GX_TORRENT_PROXY` | empty | SOCKS5 or HTTP CONNECT proxy (`socks5://user:pass@host:port`) |
| `-ipfilter` | `GX_TORRENT_IPFILTER` | empty | IP blocklist file path (supports CIDR, P2P, and eMule `.dat`) |
| `-encryption` | `GX_TORRENT_ENCRYPTION` | `1` | MSE/PE encryption: `0` = disabled, `1` = enabled, `2` = forced |
| `-no-dht` | `GX_TORRENT_NO_DHT` | `false` | Disable Mainline DHT |
| `-no-pex` | `GX_TORRENT_NO_PEX` | `false` | Disable Peer Exchange (PEX) |
| `-no-utp` | `GX_TORRENT_NO_UTP` | `false` | Disable uTP protocol (TCP peers only) |
| `-no-holepunch` | `GX_TORRENT_NO_HOLEPUNCH` | `false` | Disable BEP 55 NAT holepunching |
| `-no-lsd` | `GX_TORRENT_NO_LSD` | `false` | Disable Local Service Discovery |
| `-no-upnp` | `GX_TORRENT_NO_UPNP` | `false` | Disable UPnP automatic port forwarding |
| `-no-natpmp` | `GX_TORRENT_NO_NATPMP` | `false` | Disable NAT-PMP port forwarding |
| `-token` | `GX_TORRENT_TOKEN` | empty | Shared secret required in `X-Gx-Token` header (API authentication) |
| `-log-file` | `GX_TORRENT_LOG_FILE` | stderr | Log output file (automatically rotated at 5 MB x 4 files) |
| `-notify-url` | `GX_TORRENT_NOTIFY_URL` | empty | Webhook URL to POST JSON payloads on RSS match or error |
| `-debug` | `GX_TORRENT_DEBUG` | `false` | Enable verbose engine debug logging |

---

## 7. Docker Container

A standalone Docker image can be built from the repository root:

```bash
docker build -t gx-torrent .
docker run -d \
  --name gx-torrent \
  -p 8890:8080 \
  -p 6881:6881 \
  -p 6881:6881/udp \
  -v /path/to/data:/data \
  -v /path/to/downloads:/downloads \
  gx-torrent
```

Web UI is exposed on port `8890` (mapped from container port `8080`).

---

## 8. Configuration File (`settings.json`)

The daemon maintains its runtime configuration inside `<data-dir>/settings.json`:
- `standalone_password`: Bcrypt-hashed login password.
- `bypass_local`: `"true"` to allow unauthenticated access from private IP subnets (LAN).
- `categories`: Map of category names to target directory paths.
- `rules`: Configured RSS automation rules.
- `indexers`: Configured Torznab indexer URLs and API keys.

To reset a forgotten password, simply stop the daemon, remove the `standalone_password` entry from `settings.json`, and restart: the web interface will prompt for setup.

---

## 9. License

`gx-torrent` is open source distributed under the [EUPL 1.2](LICENSE) license.
Third-party component notices and licenses are listed in [THIRD-PARTY-NOTICES](THIRD-PARTY-NOTICES).
README

# Fail rather than ship a release with the wrong or a missing licence: every
# package must carry the project's own EUPL 1.2 text.
grep -q "EUROPEAN UNION PUBLIC LICENCE v. 1.2" "${ROOT}/LICENSE" \
  || { echo "LICENSE does not contain the EUPL 1.2 text; refusing to package" >&2; exit 1; }
cp "${ROOT}/LICENSE" "$STAGE/LICENSE"
cp "${ROOT}/THIRD-PARTY-NOTICES" "$STAGE/THIRD-PARTY-NOTICES"
echo "$LABEL" > "$STAGE/VERSION"

tar --numeric-owner -C "$STAGE" -czf "$OUTPUT" .
( cd "$(dirname "$OUTPUT")" && sha256sum "$(basename "$OUTPUT")" > "$(basename "$OUTPUT").sha256" )

echo "archive: $OUTPUT"
echo "checksum: $OUTPUT.sha256"
