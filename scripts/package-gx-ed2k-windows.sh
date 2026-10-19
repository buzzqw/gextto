#!/usr/bin/env bash
# Build the standalone gx-ed2k archive for Windows: the eD2k/Kad search
# daemon alone, usable interactively or as a Windows service.
#
#   scripts/package-gx-ed2k-windows.sh [--arch amd64|arm64] [--output FILE] [--label TEXT]
#
# Layout: gx-ed2k.exe  install-service.ps1  uninstall-service.ps1  README.txt
#         VERSION  LICENSE.txt  THIRD-PARTY-NOTICES.txt
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
    -h|--help) echo "usage: package-gx-ed2k-windows.sh [--arch amd64|arm64] [--output FILE] [--label TEXT]"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

case "$ARCH" in
  amd64|arm64) ;;
  *) echo "unsupported arch: $ARCH (use amd64 or arm64)" >&2; exit 2 ;;
esac

[[ -n "$LABEL" ]] || LABEL="$CHANNEL"
[[ -z "$OUTPUT" ]] && OUTPUT="${ROOT}/gx-ed2k-windows-${ARCH}.zip"
OUT_DIR="$(mkdir -p "$(dirname "$OUTPUT")" && cd "$(dirname "$OUTPUT")" && pwd)"
OUTPUT="${OUT_DIR}/$(basename "$OUTPUT")"

ED_BUILD="${GEXTTO_GXED2K_BUILD:-$(tr -dc '0-9' < "$ROOT/gx-ed2k.build_number" 2>/dev/null || true)}"
[[ "$ED_BUILD" =~ ^[0-9]+$ ]] || ED_BUILD=0
COMMIT="${GEXTTO_COMMIT:-${GITHUB_SHA:-$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || true)}}"
BUILT_AT="${GEXTTO_BUILT_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

echo "building gx-ed2k.exe (windows/$ARCH, build $ED_BUILD)…"
( cd "$ROOT" && GOOS=windows GOARCH="$ARCH" CGO_ENABLED=0 go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X github.com/buzzqw/gextto/internal/constants.GxEd2kBuild=${ED_BUILD} -X github.com/buzzqw/gextto/internal/constants.Commit=${COMMIT} -X github.com/buzzqw/gextto/internal/constants.BuiltAt=${BUILT_AT} -X github.com/buzzqw/gextto/internal/constants.GxEd2kChannel=${GEXTTO_GX_CHANNEL:-}" \
    -o "$STAGE/gx-ed2k.exe" ./cmd/gx-ed2k )

cat > "$STAGE/install-service.ps1" <<'PS1'
# Install gx-ed2k as a Windows service. Run this in an ELEVATED PowerShell.
$ErrorActionPreference = "Stop"
$dir = Split-Path -Parent $MyInvocation.MyCommand.Path
$exe = Join-Path $dir "gx-ed2k.exe"
if (-not (Test-Path $exe)) { throw "gx-ed2k.exe not found next to this script" }

$serviceName = "gx-ed2k"
$account = "NT SERVICE\$serviceName"
$data = Join-Path $env:ProgramData "gx-ed2k"
New-Item -ItemType Directory -Force -Path $data | Out-Null
$log = Join-Path $data "gx-ed2k.log"

$bin = '"' + $exe + '" -mode standalone -listen 0.0.0.0:8899 -data "' + $data + '" -kad-listen 0.0.0.0:4672 -tcp-port 4662 -upnp=true -log-file "' + $log + '"'

if (Get-Service -Name $serviceName -ErrorAction SilentlyContinue) {
    Stop-Service -Name $serviceName -Force -ErrorAction SilentlyContinue
    sc.exe delete $serviceName | Out-Null
    Start-Sleep -Seconds 1
}
New-Service -Name $serviceName -BinaryPathName $bin -DisplayName "gx-ed2k" `
    -Description "gx-ed2k (eD2k/Kad search daemon, web UI on port 8899)" -StartupType Automatic
sc.exe config $serviceName obj= "$account" | Out-Null
icacls $dir /grant "${account}:(OI)(CI)RX" | Out-Null
icacls $data /grant "${account}:(OI)(CI)F" | Out-Null

# Kad/UDP and eD2k/TCP must be reachable from outside for HighID and a healthy
# Kad routing table. UPnP may also open the router; these rules cover the host.
foreach ($rule in @(
    @{Name="gx-ed2k Kad (UDP 4672)"; Proto="UDP"; Port=4672},
    @{Name="gx-ed2k eD2k (TCP 4662)"; Proto="TCP"; Port=4662})) {
    if (-not (Get-NetFirewallRule -DisplayName $rule.Name -ErrorAction SilentlyContinue)) {
        New-NetFirewallRule -DisplayName $rule.Name -Direction Inbound -Protocol $rule.Proto `
            -LocalPort $rule.Port -Action Allow -Profile Any | Out-Null
    }
}

Start-Service -Name $serviceName
Write-Host "gx-ed2k installed and started (running as $account)."
Write-Host "  Web UI:  http://<host>:8899/"
Write-Host "  Data:    $data"
Write-Host "  Log:     $log"
Write-Host "  Stop:    Stop-Service $serviceName   Uninstall: .\uninstall-service.ps1"
PS1

cat > "$STAGE/uninstall-service.ps1" <<'PS1'
# Remove the gx-ed2k Windows service. Run this in an ELEVATED PowerShell.
$ErrorActionPreference = "Continue"
if (Get-Service -Name "gx-ed2k" -ErrorAction SilentlyContinue) {
    Stop-Service -Name "gx-ed2k" -Force
    for ($i = 0; $i -lt 20; $i++) {
        if ((Get-Service -Name "gx-ed2k").Status -eq "Stopped") { break }
        Start-Sleep -Milliseconds 500
    }
    sc.exe delete "gx-ed2k" | Out-Null
    Write-Host "gx-ed2k service removed (data under %ProgramData%\gx-ed2k was kept)."
} else {
    Write-Host "gx-ed2k service is not installed."
}
foreach ($name in @("gx-ed2k Kad (UDP 4672)", "gx-ed2k eD2k (TCP 4662)")) {
    Get-NetFirewallRule -DisplayName $name -ErrorAction SilentlyContinue | Remove-NetFirewallRule -ErrorAction SilentlyContinue
}
PS1

cat > "$STAGE/README.txt" <<'README'
gx-ed2k (standalone, Windows)
=============================

gx-ed2k is the eD2k/Kad search daemon used by Gextto, usable on its own: a web
page on port 8899 and a native /api/v1. It is a SEARCH client: it does not
download, upload or share files.

RUN WITHOUT INSTALLING
----------------------
    gx-ed2k.exe -mode standalone -listen 0.0.0.0:8899 -data C:\gx-ed2k

Then open http://<host>:8899/ and search. The page also validates an
ed2k:// link and shows whether Kad is open or limited by NAT/firewall.

INSTALL AS A WINDOWS SERVICE
----------------------------
In an elevated PowerShell, from the extracted folder:

    .\install-service.ps1

It registers the service "gx-ed2k" with data in %ProgramData%\gx-ed2k, the log
in %ProgramData%\gx-ed2k\gx-ed2k.log, opens UDP 4672 and TCP 4662 in Windows
Firewall, and starts it. Remove it with:

    .\uninstall-service.ps1

If PowerShell blocks the script, run:
    powershell -ExecutionPolicy Bypass -File .\install-service.ps1

FLAGS
-----
-listen <addr>        Web UI / API (default: 127.0.0.1:8899 managed, 0.0.0.0:8899 standalone)
-data <path>          State folder (node id, nodes.dat, server.met, logs)
-kad-listen <addr>    Kad UDP bind (default: 0.0.0.0:4672)
-tcp-port <port>      TCP port announced to the eD2k servers (default: 4662)
-nodes-url <url>      nodes.dat source (default: https://upd.emule-security.org/nodes.dat)
-serverlist-url <url> server.met source
-servers <list>       comma-separated host:port eD2k servers
-upnp=true|false      ask the router to forward the Kad UDP/eD2k TCP ports
-search-timeout <d>   whole search budget (default: 20s)
-token <token>        API token required outside loopback

UPDATES
-------
The page has "Check for updates": it compares this build with the published
release manifest and links the gx-ed2k-windows-<arch>.zip archive. A build compiled
from a local checkout does not offer updates.

NOTES
-----
- The service runs as NT SERVICE\gx-ed2k, not LocalSystem.
- For HighID and a healthy Kad table, allow inbound UDP 4672 and TCP 4662
  (the installer adds the firewall rules; UPnP can also map them on the router).
- Gextto (the manager) is Linux-only; this package is the standalone daemon.
- Search results are shown for inspection: gx-ed2k has no transfer engine and
  does not queue downloads.

LICENSE
-------
gx-ed2k is open source, distributed under the European Union Public Licence
v. 1.2. The full text is in LICENSE.txt; the notices and licences of the
third-party components are in THIRD-PARTY-NOTICES.txt.
README

grep -q "EUROPEAN UNION PUBLIC LICENCE v. 1.2" "${ROOT}/LICENSE" \
  || { echo "LICENSE does not contain the EUPL 1.2 text; refusing to package" >&2; exit 1; }
cp "${ROOT}/LICENSE" "$STAGE/LICENSE.txt"
cp "${ROOT}/THIRD-PARTY-NOTICES" "$STAGE/THIRD-PARTY-NOTICES.txt"
echo "$LABEL" > "$STAGE/VERSION"

( cd "$STAGE" && zip -q -r "$OUTPUT" . )
( cd "$(dirname "$OUTPUT")" && sha256sum "$(basename "$OUTPUT")" > "$(basename "$OUTPUT").sha256" )

echo "archive: $OUTPUT"
echo "checksum: $OUTPUT.sha256"
