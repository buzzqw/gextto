#!/usr/bin/env bash
# Build the standalone gx-ed2k archive: the eD2k/Kad search daemon alone, with
# its own web page and API.
#
#   scripts/package-gx-ed2k.sh [--binary PATH] [--output FILE] [--arch ARCH]
#                              [--label TEXT]
#
# Layout: gx-ed2k  gx-ed2k.service  run.sh  install-service.sh
#         uninstall-service.sh  README.md  VERSION  LICENSE  THIRD-PARTY-NOTICES
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BINARY="${ROOT}/bin/gx-ed2k"
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
    -h|--help) echo "usage: package-gx-ed2k.sh [--binary PATH] [--output FILE] [--arch ARCH] [--label TEXT]"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

[[ -n "$LABEL" ]] || LABEL="$CHANNEL"
[[ -z "$OUTPUT" ]] && OUTPUT="${ROOT}/gx-ed2k-linux-${ARCH}.tar.gz"

if [[ ! -x "$BINARY" ]]; then
  echo "error: gx-ed2k binary not found at $BINARY" >&2
  echo "       build it first with \`make gx-ed2k\`" >&2
  exit 1
fi

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
chmod 0755 "$STAGE"
cp "$BINARY" "$STAGE/gx-ed2k"
chmod +x "$STAGE/gx-ed2k"

cat > "$STAGE/run.sh" <<'RUN'
#!/usr/bin/env bash
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
export GX_ED2K_DATA="${GX_ED2K_DATA:-$DIR/data}"
mkdir -p "$GX_ED2K_DATA"
exec "$DIR/gx-ed2k" -mode standalone -listen 0.0.0.0:8899 -data "$GX_ED2K_DATA" "$@"
RUN
chmod +x "$STAGE/run.sh"

cat > "$STAGE/gx-ed2k.service" <<'UNIT'
[Unit]
Description=gx-ed2k eD2k/Kad search daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=gx-ed2k
Group=gx-ed2k
StateDirectory=gx-ed2k
WorkingDirectory=/opt/gx-ed2k
ExecStart=/opt/gx-ed2k/gx-ed2k -mode standalone -listen 0.0.0.0:8899 -data /var/lib/gx-ed2k
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=read-only

[Install]
WantedBy=multi-user.target
UNIT

cat > "$STAGE/install-service.sh" <<'INSTALL'
#!/usr/bin/env bash
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo "run as root" >&2; exit 1; }
DIR="$(cd "$(dirname "$0")" && pwd)"
id gx-ed2k >/dev/null 2>&1 || useradd --system --home /var/lib/gx-ed2k --shell /usr/sbin/nologin gx-ed2k
install -d -o gx-ed2k -g gx-ed2k -m 0755 /opt/gx-ed2k /var/lib/gx-ed2k
install -m 0755 "$DIR/gx-ed2k" /opt/gx-ed2k/gx-ed2k
install -m 0644 "$DIR/gx-ed2k.service" /etc/systemd/system/gx-ed2k.service
systemctl daemon-reload
systemctl enable --now gx-ed2k.service
echo "gx-ed2k is running; open http://<host>:8899/"
INSTALL

cat > "$STAGE/uninstall-service.sh" <<'UNINSTALL'
#!/usr/bin/env bash
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo "run as root" >&2; exit 1; }
systemctl disable --now gx-ed2k.service 2>/dev/null || true
rm -f /etc/systemd/system/gx-ed2k.service
systemctl daemon-reload
echo "service removed; /var/lib/gx-ed2k was kept"
UNINSTALL
chmod +x "$STAGE/install-service.sh" "$STAGE/uninstall-service.sh"

cat > "$STAGE/README.md" <<'README'
# gx-ed2k

`gx-ed2k` is the eD2k/Kad **search** daemon of Gextto, usable on its own.

It bootstraps Kad, searches keywords on Kad and on the configured eD2k servers
and exposes the results on its own web page and on `/api/v1`:

```sh
./run.sh
# http://127.0.0.1:8899/
curl 'http://127.0.0.1:8899/api/v1/search?q=rare%20movie'
```

It does **not** implement the eMule client-to-client transfer protocol:
`/api/v1/health` reports `"download": false` and the page says so. It searches,
it does not download, upload or share files.

Options: `-listen`, `-data`, `-token`, `-kad-listen`, `-tcp-port`,
`-nodes-url`, `-serverlist-url`, `-servers`, `-search-timeout`, `-lang`.

A daemon behind NAT still searches; Kad reachability (HighID/LowID) affects the
quality of results and is shown in `-log-file` output, not promised.

License: EUPL 1.2. Third-party notices are in `THIRD-PARTY-NOTICES`.
README

grep -q "EUROPEAN UNION PUBLIC LICENCE v. 1.2" "${ROOT}/LICENSE" \
  || { echo "LICENSE does not contain the EUPL 1.2 text; refusing to package" >&2; exit 1; }
cp "${ROOT}/LICENSE" "$STAGE/LICENSE"
cp "${ROOT}/THIRD-PARTY-NOTICES" "$STAGE/THIRD-PARTY-NOTICES"
echo "$LABEL" > "$STAGE/VERSION"

tar --numeric-owner -C "$STAGE" -czf "$OUTPUT" .
( cd "$(dirname "$OUTPUT")" && sha256sum "$(basename "$OUTPUT")" > "$(basename "$OUTPUT").sha256" )

echo "archive: $OUTPUT"
echo "checksum: $OUTPUT.sha256"
