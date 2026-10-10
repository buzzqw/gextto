#!/usr/bin/env bash
# Build the standalone gx-torrent archive for Windows: the daemon alone, to be
# used like qbittorrent-nox (web UI, qBittorrent-compatible API, RSS), without
# Gextto. It can run interactively or as a Windows service.
#
#   scripts/package-gx-torrent-windows.sh [--arch amd64|arm64] [--output FILE] [--label TEXT]
#
# Layout of the archive:
#   gx-torrent.exe  install-service.ps1  uninstall-service.ps1  README.txt  VERSION
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

# Data, logs and the web UI live under ProgramData; the first run opens the
# wizard at http://<host>:8890/ui/setup to choose folders, ports and bandwidth.
$data = Join-Path $env:ProgramData "gx-torrent"
New-Item -ItemType Directory -Force -Path $data | Out-Null
$log = Join-Path $data "gx-torrent.log"

$bin = '"' + $exe + '" -mode standalone -listen 0.0.0.0:8890 -data "' + $data + '" -log-file "' + $log + '"'

if (Get-Service -Name gx-torrent -ErrorAction SilentlyContinue) {
    Stop-Service -Name gx-torrent -Force -ErrorAction SilentlyContinue
    sc.exe delete gx-torrent | Out-Null
    Start-Sleep -Seconds 1
}
New-Service -Name gx-torrent -BinaryPathName $bin -DisplayName "gx-torrent" `
    -Description "gx-torrent (BitTorrent client, web UI on the listen port)" -StartupType Automatic
Start-Service -Name gx-torrent
Write-Host "gx-torrent installed and started."
Write-Host "  Web UI:  http://<host>:8890/  (first run: http://<host>:8890/ui/setup)"
Write-Host "  Data:    $data"
Write-Host "  Log:     $log"
Write-Host "  Stop:    Stop-Service gx-torrent   Uninstall: .\uninstall-service.ps1"
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

Manual equivalent:

    sc.exe create gx-torrent binPath= "\"C:\path\gx-torrent.exe\" -mode standalone -listen 0.0.0.0:8890 -data \"C:\ProgramData\gx-torrent\" -log-file \"C:\ProgramData\gx-torrent\gx-torrent.log\"" start= auto

INTERFACES
----------
- Web UI:        http://<host>:8890/  (login, torrents, categories, RSS).
- qBittorrent:   /api/v2 (Sonarr, Radarr, mobile apps).
- Indexer search (Torznab) and RSS notifications, configured in /ui/rss.

NOTES
-----
- The service runs in session 0: it never opens a browser; reach the wizard at
  the printed address.
- Only the daemon is Windows-ready. Gextto (the manager) is Linux-only.
README

echo "$LABEL" > "$STAGE/VERSION"

( cd "$STAGE" && zip -q -r "$OUTPUT" . )
( cd "$(dirname "$OUTPUT")" && sha256sum "$(basename "$OUTPUT")" > "$(basename "$OUTPUT").sha256" )

echo "archive: $OUTPUT"
echo "checksum: $OUTPUT.sha256"
