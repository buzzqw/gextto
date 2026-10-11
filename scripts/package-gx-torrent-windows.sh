#!/usr/bin/env bash
# Build the standalone gx-torrent archive for Windows: the daemon alone, to be
# used like qbittorrent-nox (web UI, qBittorrent-compatible API, RSS), without
# Gextto. It can run interactively or as a Windows service.
#
#   scripts/package-gx-torrent-windows.sh [--arch amd64|arm64] [--output FILE] [--label TEXT]
#
# Layout of the archive:
#   gx-torrent.exe  install-service.ps1  uninstall-service.ps1  README.txt  VERSION
#   LICENSE.txt  THIRD-PARTY-NOTICES.txt
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ARCH="amd64"
OUTPUT=""
CHANNEL="${GEXTTO_CHANNEL:-continuous}"
LABEL=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --arch|--output|--label)
      option="$1"
      [[ $# -ge 2 && -n "$2" ]] || { echo "$option requires a value" >&2; exit 2; }
      case "$option" in
        --arch) ARCH="$2" ;;
        --output) OUTPUT="$2" ;;
        --label) LABEL="$2" ;;
      esac
      shift 2
      ;;
    -h|--help) echo "usage: package-gx-torrent-windows.sh [--arch amd64|arm64] [--output FILE] [--label TEXT]"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

case "$ARCH" in
  amd64|arm64) ;;
  *) echo "unsupported arch: $ARCH (use amd64 or arm64)" >&2; exit 2 ;;
esac

[[ -n "$LABEL" ]] || LABEL="$CHANNEL"
[[ -z "$OUTPUT" ]] && OUTPUT="${ROOT}/gx-torrent-windows-${ARCH}.zip"
# zip runs from inside the staging directory, so the output path must be
# absolute: create its directory first and resolve it.
OUT_DIR="$(mkdir -p "$(dirname "$OUTPUT")" && cd "$(dirname "$OUTPUT")" && pwd)"
OUTPUT="${OUT_DIR}/$(basename "$OUTPUT")"

# The daemon's own build number, when it has already been built, so the package
# reports the same 1.1.<n> as the checkout.
GX_BUILD="$(cat "${ROOT}/gx-torrent.build_number" 2>/dev/null || echo 0)"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

echo "building gx-torrent.exe (windows/$ARCH, build $GX_BUILD)…"
( cd "$ROOT" && GOOS=windows GOARCH="$ARCH" CGO_ENABLED=0 go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X github.com/buzzqw/gextto/internal/constants.GxTorrentBuild=${GX_BUILD}" \
    -o "$STAGE/gx-torrent.exe" ./cmd/gx-torrent )

cat > "$STAGE/install-service.ps1" <<'PS1'
# Install gx-torrent as a Windows service. Run this in an ELEVATED PowerShell.
$ErrorActionPreference = "Stop"
$dir = Split-Path -Parent $MyInvocation.MyCommand.Path
$exe = Join-Path $dir "gx-torrent.exe"
if (-not (Test-Path $exe)) { throw "gx-torrent.exe not found next to this script" }

$serviceName = "gx-torrent"
# A virtual service account: the Service Control Manager creates it and grants
# it "Log on as a service". It has no rights beyond what this script grants —
# unlike LocalSystem, which can write anywhere and is a SYSTEM-privileged
# arbitrary-file-write target for anything that reaches the API.
$account = "NT SERVICE\$serviceName"

# Data, logs and the web UI live under ProgramData; the first run opens the
# wizard at http://<host>:8890/ui/setup to choose folders, ports and bandwidth.
$data = Join-Path $env:ProgramData "gx-torrent"
New-Item -ItemType Directory -Force -Path $data | Out-Null
$log = Join-Path $data "gx-torrent.log"

$bin = '"' + $exe + '" -mode standalone -listen 0.0.0.0:8890 -data "' + $data + '" -log-file "' + $log + '"'

if (Get-Service -Name $serviceName -ErrorAction SilentlyContinue) {
    Stop-Service -Name $serviceName -Force -ErrorAction SilentlyContinue
    sc.exe delete $serviceName | Out-Null
    Start-Sleep -Seconds 1
}
New-Service -Name $serviceName -BinaryPathName $bin -DisplayName "gx-torrent" `
    -Description "gx-torrent (BitTorrent client, web UI on the listen port)" -StartupType Automatic
# Run as the service's own virtual account, not LocalSystem.
sc.exe config $serviceName obj= "$account" | Out-Null
# Let it read its own program folder and write its data folder (state, logs,
# default downloads). A download folder chosen elsewhere must be granted too.
icacls $dir /grant "${account}:(OI)(CI)RX" | Out-Null
icacls $data /grant "${account}:(OI)(CI)F" | Out-Null
Start-Service -Name $serviceName
Write-Host "gx-torrent installed and started (running as $account)."
Write-Host "  Web UI:  http://<host>:8890/  (first run: http://<host>:8890/ui/setup)"
Write-Host "  Data:    $data"
Write-Host "  Log:     $log"
Write-Host "  Stop:    Stop-Service $serviceName   Uninstall: .\uninstall-service.ps1"
Write-Host "  Note: a download folder outside $data needs access for the account, e.g."
Write-Host "        icacls D:\media /grant `"${account}:(OI)(CI)M`""
PS1

cat > "$STAGE/uninstall-service.ps1" <<'PS1'
# Remove the gx-torrent Windows service. Run this in an ELEVATED PowerShell.
$ErrorActionPreference = "Continue"
if (Get-Service -Name gx-torrent -ErrorAction SilentlyContinue) {
    Stop-Service -Name gx-torrent -Force
    sc.exe delete gx-torrent | Out-Null
    Write-Host "gx-torrent service removed (data under %ProgramData%\gx-torrent was kept)."
} else {
    Write-Host "gx-torrent service is not installed."
}
PS1

cat > "$STAGE/README.txt" <<'README'
gx-torrent (standalone, Windows)
=================================

gx-torrent is a BitTorrent client with a web UI, a qBittorrent-compatible API
and RSS, usable on its own like qbittorrent-nox (it is the same binary Gextto
drives as its torrent engine). The installation is the web wizard: no setup
program to run.

RUN WITHOUT INSTALLING
----------------------
    gx-torrent.exe -mode standalone -listen 0.0.0.0:8890 -data C:\gx-torrent

Then open http://<host>:8890/: the first run shows the setup wizard (language,
download folder, temporary folder, peer port with a "test port" button,
bandwidth limits, password, LAN access, indexer).

INSTALL AS A WINDOWS SERVICE
----------------------------
In an elevated PowerShell, from the extracted folder:

    .\install-service.ps1

It registers the service "gx-torrent" with data in %ProgramData%\gx-torrent and
the log at %ProgramData%\gx-torrent\gx-torrent.log, and starts it. Manage it with
the usual tools (services.msc, Start-Service/Stop-Service gx-torrent). Remove it
with:

    .\uninstall-service.ps1

If PowerShell blocks the script (the default execution policy is Restricted, and
a file downloaded from the browser carries the mark-of-the-web), run it with:

    powershell -ExecutionPolicy Bypass -File .\install-service.ps1

or clear the block first with:  Unblock-File .\install-service.ps1

Manual equivalent:

    sc.exe create gx-torrent binPath= "\"C:\path\gx-torrent.exe\" -mode standalone -listen 0.0.0.0:8890 -data \"C:\ProgramData\gx-torrent\" -log-file \"C:\ProgramData\gx-torrent\gx-torrent.log\"" start= auto

INTERFACES & AUTOMATION
-----------------------
- Web UI:        http://<host>:8890/  (login, torrents, categories, tags, streaming, RSS).
- qBittorrent:   /api/v2 (Sonarr, Radarr, Lidarr, mobile apps).
                 Configure in Sonarr/Radarr as download client "qBittorrent"
                 Host: localhost (or server IP), Port: 8890,
                 Username: admin, Password: (set in setup wizard).
- Indexer search (Torznab) and RSS notifications, configured in /ui/rss.
- In-browser streaming: play videos in browser or VLC via HTTP Range streaming.

FLAGS & SETTINGS
----------------
-listen <addr>      Web UI / API bind address (default: 127.0.0.1:8890)
-data <path>        State folder (default: %ProgramData%\gx-torrent)
-download-dir <dir> Default downloads folder (default: <data>\downloads)
-peer-ports <range> Incoming peer port range (default: 6881-6891)
-proxy <url>        SOCKS5 or HTTP CONNECT proxy
-encryption <0|1|2> MSE/PE stream encryption (0 disabled, 1 enabled, 2 forced)
-no-dht / -no-utp   Disable DHT or uTP protocols

NOTES
-----
- The service runs as the virtual account NT SERVICE\gx-torrent (not
  LocalSystem). The installer grants it its program folder (read/execute) and
  %ProgramData%\gx-torrent (full). If you point the download folder somewhere
  else, grant that folder too, e.g.
      icacls D:\media /grant "NT SERVICE\gx-torrent:(OI)(CI)M"
  without it the daemon cannot write the downloads.
- The service runs in session 0: it never opens a browser; reach the wizard at
  the printed address.
- A download folder on a network share must be a UNC path (\\server\share\...),
  not a mapped drive letter: a mapped drive is invisible to the service. A
  network folder is linked with a symlink, which may need Developer Mode (or the
  symlink privilege); a local folder uses a junction and needs no privilege.
- Only the daemon is Windows-ready. Gextto (the manager) is Linux-only.
README

cp "${ROOT}/LICENSE" "$STAGE/LICENSE.txt"
cp "${ROOT}/THIRD-PARTY-NOTICES" "$STAGE/THIRD-PARTY-NOTICES.txt"
echo "$LABEL" > "$STAGE/VERSION"

( cd "$STAGE" && zip -q -r "$OUTPUT" . )
( cd "$(dirname "$OUTPUT")" && sha256sum "$(basename "$OUTPUT")" > "$(basename "$OUTPUT").sha256" )

echo "archive: $OUTPUT"
echo "checksum: $OUTPUT.sha256"
