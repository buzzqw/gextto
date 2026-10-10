# Install gx-torrent as a Windows service, start it, check it serves
# /api/v1/health, stop it and remove it. It is the real install path
# (New-Service with the daemon's binPath) exercised end to end; on a machine
# without administrator rights New-Service fails and the script exits nonzero.
#
#   pwsh -File scripts/gx-torrent-service-selftest.ps1 -ExePath .\gx-torrent.exe
param(
    [Parameter(Mandatory = $true)][string]$ExePath,
    [string]$ServiceName = "gx-torrent-selftest",
    [int]$Port = 18890
)

$ErrorActionPreference = "Stop"

$exe = (Resolve-Path $ExePath).Path
$data = Join-Path $env:RUNNER_TEMP "gx-torrent-service-selftest"
if (Test-Path $data) { Remove-Item -Recurse -Force $data }
New-Item -ItemType Directory -Force -Path $data | Out-Null
$log = Join-Path $data "gx-torrent.log"

function Remove-TestService {
    if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
        Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
        sc.exe delete $ServiceName | Out-Null
        Start-Sleep -Seconds 1
    }
}

Remove-TestService
try {
    # The same binPath the shipped install-service.ps1 builds.
    $bin = '"' + $exe + '" -mode standalone -listen 127.0.0.1:' + $Port + ' -data "' + $data + '" -log-file "' + $log + '"'
    Write-Host "installing service $ServiceName"
    New-Service -Name $ServiceName -BinaryPathName $bin -StartupType Manual `
        -DisplayName "gx-torrent service selftest" | Out-Null
    Start-Service -Name $ServiceName
    Write-Host "service started; waiting for /api/v1/health on port $Port"

    $healthy = $false
    for ($i = 0; $i -lt 40; $i++) {
        try {
            $r = Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 "http://127.0.0.1:$Port/api/v1/health"
            if ($r.StatusCode -eq 200) { $healthy = $true; break }
        } catch { }
        Start-Sleep -Seconds 1
    }
    if (-not $healthy) { throw "the service did not answer /api/v1/health on port $Port" }
    Write-Host "service answered /api/v1/health"

    Stop-Service -Name $ServiceName -Force
    for ($i = 0; $i -lt 30; $i++) {
        if ((Get-Service -Name $ServiceName).Status -eq "Stopped") { break }
        Start-Sleep -Milliseconds 500
    }
    $status = (Get-Service -Name $ServiceName).Status
    if ($status -ne "Stopped") { throw "service status after stop = $status" }
    Write-Host "service stopped cleanly"
} finally {
    Remove-TestService
}
