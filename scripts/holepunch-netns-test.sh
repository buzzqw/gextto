#!/usr/bin/env bash
# holepunch-netns-test.sh — verifica end-to-end del BEP 55 con network namespace.
#
# Richiede root (CAP_NET_ADMIN) e questi comandi: ip, iptables, python3, curl,
# e il binario del demone (default: ./bin/gx-torrent, costruito da `make
# gx-torrent` o `make build`).
#
# Topologia (tutto dentro namespace di rete dedicati: la rete dell'host NON
# viene toccata, ne' interfacce ne' rotte ne' firewall):
#
#   gx-lab (relay)        gx-r (router)                 client netns
#   --------------        --------------                ------------
#   relay 10.0.0.1  <---  vr 10.0.0.2
#                         var 10.10.0.1  <---  gx-a 10.10.0.2  (leecher, dietro NAT)
#                         vbr 10.20.0.1  <---  gx-b 10.20.0.2  (seeder,  dietro NAT)
#
# Il router fa MASQUERADE verso gx-lab (i client escono) e DROPpa le nuove
# connessioni in entrata e tra i due client: leecher e seeder non si vedono
# direttamente, entrambi raggiungono solo il relay. Un tracker HTTP minimo
# (python3, in gx-lab) fa conoscere gli endpoint. Se il buco riesce, il leecher
# compare con origine "holepunch" tra i peer del torrent.
#
# Uso:
#   sudo scripts/holepunch-netns-test.sh [--bin PATH] [--keep] [--size MiB]
#   sudo scripts/holepunch-netns-test.sh --help
#
# All'uscita vengono rimossi i namespace, i veth e i daemon: sul sistema
# restano solo le voci in /run/netns durante l'esecuzione.
#
# NOTA: Linux conntrack e endpoint-dependent, quindi con il solo MASQUERADE il
# buco puo non aprirsi (serve un NAT "cone"). Lo script riporta sempre cosa ha
# osservato (connessione diretta / holepunch / niente), così le regole del
# router si possono tarare. Vedi docs/gx-torrent.md, sezione Holepunching.
set -euo pipefail

# Anchor relative paths to the repository root, not the caller's directory.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="${ROOT}/bin/gx-torrent"
SIZE_MIB=4
KEEP=0
TRACKER_PORT=13800
RELAY_PEER_PORT=51400
A_PEER_PORT=51401
B_PEER_PORT=51402
RELAY_API=18881
A_API=18882
B_API=18883
DEADLINE=90

log() { printf '\033[1;34m==>\033[0m %s\n' "$*" >&2; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
  printf 'Usage: %s [--bin PATH] [--keep] [--size MiB]\n\n' "$(basename "$0")"
  sed -n '2,44p' "$0" | sed 's/^# \{0,1\}//'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bin) BIN="${2:-}"; [[ -n "$BIN" ]] || die "--bin requires a value"; shift 2 ;;
    --size) SIZE_MIB="${2:-}"; [[ "$SIZE_MIB" =~ ^[0-9]+$ ]] || die "--size needs a number"; shift 2 ;;
    --keep) KEEP=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1 (use --help)" ;;
  esac
done

[[ "$(id -u)" == "0" ]] || die "run me as root (sudo)"
for cmd in ip iptables python3 curl; do
  command -v "$cmd" >/dev/null || die "missing command: $cmd"
done
[[ -x "$BIN" ]] || die "daemon binary not found: $BIN (build with: make gx-torrent)"
BIN="$(realpath "$BIN")"

WORK="$(mktemp -d /tmp/holepunch-netns.XXXXXX)"
L=gx-lab; R=gx-r; A=gx-a; B=gx-b
PIDS=()

# remove_links deletes veth ends that a previous aborted run may have left in
# the root namespace (the peer inside a deleted netns disappears with it).
remove_links() {
  local link
  for link in vrel var vbr; do
    ip link del "$link" 2>/dev/null || true
  done
}

cleanup() {
  local pid
  for pid in "${PIDS[@]:-}"; do
    kill "$pid" 2>/dev/null || true
  done
  if [[ "$KEEP" == "1" ]]; then
    warn "kept for debugging: netns $L $R $A $B, work dir $WORK"
    return
  fi
  ip netns del "$A" 2>/dev/null || true
  ip netns del "$B" 2>/dev/null || true
  ip netns del "$R" 2>/dev/null || true
  ip netns del "$L" 2>/dev/null || true
  remove_links
  rm -rf "$WORK"
}
trap cleanup EXIT

# --------------------------------------------------------------- topologia ---
setup_netns() {
  ip netns del "$R" 2>/dev/null || true
  ip netns del "$A" 2>/dev/null || true
  ip netns del "$B" 2>/dev/null || true
  ip netns del "$L" 2>/dev/null || true
  remove_links
  ip netns add "$L"
  ip netns add "$R"
  ip netns add "$A"
  ip netns add "$B"
  # loopback is down by default in a fresh namespace: the daemon API binds it.
  ip netns exec "$L" ip link set lo up
  ip netns exec "$R" ip link set lo up
  ip netns exec "$A" ip link set lo up
  ip netns exec "$B" ip link set lo up

  # lab (relay) <-> router: both ends live in a namespace, so the host network
  # namespace is never touched (not even transiently, after the moves).
  ip link add vrel type veth peer name vr
  ip link set vrel netns "$L"
  ip link set vr netns "$R"
  ip netns exec "$L" ip addr add 10.0.0.1/24 dev vrel
  ip netns exec "$L" ip link set vrel up
  ip netns exec "$R" ip addr add 10.0.0.2/24 dev vr
  ip netns exec "$R" ip link set vr up

  # router <-> client A (distinct names: the two veth ends must not collide)
  ip link add var type veth peer name vac
  ip link set var netns "$R"
  ip link set vac netns "$A"
  ip netns exec "$R" ip addr add 10.10.0.1/24 dev var
  ip netns exec "$R" ip link set var up
  ip netns exec "$A" ip addr add 10.10.0.2/24 dev vac
  ip netns exec "$A" ip link set vac up
  ip netns exec "$A" ip route add default via 10.10.0.1

  # router <-> client B
  ip link add vbr type veth peer name vbc
  ip link set vbr netns "$R"
  ip link set vbc netns "$B"
  ip netns exec "$R" ip addr add 10.20.0.1/24 dev vbr
  ip netns exec "$R" ip link set vbr up
  ip netns exec "$B" ip addr add 10.20.0.2/24 dev vbc
  ip netns exec "$B" ip link set vbc up
  ip netns exec "$B" ip route add default via 10.20.0.1

  # Router: forwarding e NAT verso la root; niente inbound nuovo verso i client
  # ne tra i due client (li rende "dietro NAT" e non raggiungibili tra loro).
  ip netns exec "$R" sysctl -qw net.ipv4.ip_forward=1
  ip netns exec "$R" iptables -t nat -A POSTROUTING -o vr -j MASQUERADE
  ip netns exec "$R" iptables -A FORWARD -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
  ip netns exec "$R" iptables -A FORWARD -i vr -o var -j DROP
  ip netns exec "$R" iptables -A FORWARD -i vr -o vbr -j DROP
  ip netns exec "$R" iptables -A FORWARD -i var -o vbr -j DROP
  ip netns exec "$R" iptables -A FORWARD -i vbr -o var -j DROP
  ip netns exec "$R" iptables -A FORWARD -i var -o vr -j ACCEPT
  ip netns exec "$R" iptables -A FORWARD -i vbr -o vr -j ACCEPT
}

# ------------------------------------------------- tracciante + torrent ---------
write_tracker() {
  cat > "$WORK/tracker.py" <<'PY'
import http.server, socket, struct, sys, time, urllib.parse
peers = {}
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        q = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)
        ih = q.get('info_hash', [''])[0]
        try:
            port = int(q.get('port', ['0'])[0])
        except ValueError:
            port = 0
        ip = self.client_address[0]
        now = time.time()
        table = peers.setdefault(ih, {})
        table[(ip, port)] = now
        out = b''
        for (pip, pport), seen in list(table.items()):
            if now - seen > 900:
                del table[(pip, pport)]
                continue
            if (pip, pport) == (ip, port):
                continue
            try:
                out += socket.inet_aton(pip) + struct.pack('>H', pport)
            except OSError:
                pass
        body = b'd8:intervali60e5:peers' + str(len(out)).encode() + b':' + out + b'e'
        self.send_response(200)
        self.send_header('Content-Type', 'text/plain')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)
http.server.HTTPServer(('0.0.0.0', int(sys.argv[1])), H).serve_forever()
PY
}

write_torrent() {
  local content="$1" out="$2" announce="$3"
  cat > "$WORK/mktorrent.py" <<'PY'
import hashlib, os, sys

def benc(x):
    if isinstance(x, int):
        return b'i%de' % x
    if isinstance(x, bytes):
        return b'%d:' % len(x) + x
    if isinstance(x, str):
        return benc(x.encode())
    if isinstance(x, list):
        return b'l' + b''.join(benc(i) for i in x) + b'e'
    if isinstance(x, dict):
        return b'd' + b''.join(benc(k) + benc(v) for k, v in sorted(x.items())) + b'e'
    raise TypeError(type(x))

path, out, announce = sys.argv[1], sys.argv[2], sys.argv[3]
data = open(path, 'rb').read()
plen = 16384
pieces = b''.join(hashlib.sha1(data[i:i + plen]).digest() for i in range(0, len(data), plen))
info = {'name': os.path.basename(path), 'piece length': plen, 'pieces': pieces, 'length': len(data)}
torrent = {'info': info}
if announce:
    torrent['announce'] = announce
open(out, 'wb').write(benc(torrent))
print(hashlib.sha1(benc(info)).hexdigest())
PY
  python3 "$WORK/mktorrent.py" "$content" "$out" "$announce"
}

# ------------------------------------------------------------------ daemon ----
start_daemon() {
  # start_daemon <netns|-> <data-dir> <api> <peer-port> <listen-ip> <logfile>
  local ns="$1" data="$2" api="$3" port="$4" ip="$5" logfile="$6"
  mkdir -p "$data"
  local args=(
    -data "$data"
    -listen "127.0.0.1:$api"
    -peer-ports "$port-$port"
    -listen-interface "$ip"
    -no-dht -no-lsd -no-upnp -no-natpmp
    -debug
  )
  if [[ "$ns" == "-" ]]; then
    "$BIN" "${args[@]}" >"$logfile" 2>&1 &
  else
    ip netns exec "$ns" "$BIN" "${args[@]}" >"$logfile" 2>&1 &
  fi
  PIDS+=("$!")
}

daemon_api() {
  # daemon_api <netns|-> <api-port> <path> [curl args...]
  local ns="$1" api="$2" path="$3"; shift 3
  if [[ "$ns" == "-" ]]; then
    curl -s "http://127.0.0.1:$api$path" "$@"
  else
    ip netns exec "$ns" curl -s "http://127.0.0.1:$api$path" "$@"
  fi
}

wait_api() {
  # wait_api <netns|-> <api-port>
  local ns="$1" api="$2" i
  for i in $(seq 1 40); do
    if [[ -n "$(daemon_api "$ns" "$api" /api/v1/health 2>/dev/null)" ]]; then return 0; fi
    sleep 0.5
  done
  return 1
}

# --------------------------------------------------------------------- test ---
log "topologia: relay in gx-lab + leecher/seeder dietro NAT (solo namespace, host intatto)"
setup_netns

log "tracker HTTP su 10.0.0.1:$TRACKER_PORT"
write_tracker
ip netns exec "$L" python3 "$WORK/tracker.py" "$TRACKER_PORT" >"$WORK/tracker.log" 2>&1 &
PIDS+=("$!")
TRACKER="http://10.0.0.1:$TRACKER_PORT/announce"

log "creo un torrent di prova da ${SIZE_MIB} MiB"
mkdir -p "$WORK/content" "$WORK/seed" "$WORK/down"
head -c $((SIZE_MIB * 1024 * 1024)) /dev/urandom > "$WORK/content/holepunch.bin"
cp "$WORK/content/holepunch.bin" "$WORK/seed/holepunch.bin"
HASH="$(write_torrent "$WORK/content/holepunch.bin" "$WORK/test.torrent" "$TRACKER")"
log "info hash: $HASH"

log "avvio i tre daemon (uTP + holepunch attivi)"
start_daemon "$L"  "$WORK/relay" "$RELAY_API" "$RELAY_PEER_PORT" "10.0.0.1" "$WORK/relay.log"
start_daemon "$A"  "$WORK/a"     "$A_API"     "$A_PEER_PORT"     "10.10.0.2" "$WORK/a.log"
start_daemon "$B"  "$WORK/b"     "$B_API"     "$B_PEER_PORT"     "10.20.0.2" "$WORK/b.log"
wait_api "$L" "$RELAY_API" || { echo "--- relay.log ---" >&2; tail -n 20 "$WORK/relay.log" >&2; die "relay API non risponde"; }
wait_api "$A" "$A_API"     || { echo "--- a.log ---" >&2; tail -n 20 "$WORK/a.log" >&2; die "leecher API non risponde"; }
wait_api "$B" "$B_API"     || { echo "--- b.log ---" >&2; tail -n 20 "$WORK/b.log" >&2; die "seeder API non risponde"; }

# Reachability across the NAT: the leecher must reach the lab (tracker + relay
# peer port) through the router, or nothing else can work.
TRACKER_CODE="$(ip netns exec "$A" curl -s -m 3 -o /dev/null -w '%{http_code}' "http://10.0.0.1:$TRACKER_PORT/" 2>/dev/null || true)"
RELAY_TCP="fail"
if ip netns exec "$A" timeout 3 bash -c "exec 3<>/dev/tcp/10.0.0.1/$RELAY_PEER_PORT" 2>/dev/null; then RELAY_TCP="open"; fi
log "probe da gx-a -> gx-lab: tracker_http=${TRACKER_CODE:-fail} relay_tcp=$RELAY_TCP"

log "il seeder (B) carica il .torrent e i dati"
log "  B add-file: $(daemon_api "$B" "$B_API" /api/v1/add-file -F "torrent=@$WORK/test.torrent" -F "destination=$WORK/seed")"
MAGNET="magnet:?xt=urn:btih:$HASH&tr=$TRACKER"
log "  relay add: $(daemon_api "$L" "$RELAY_API" /api/v1/add --data-urlencode "magnet=$MAGNET")"
log "  A add: $(daemon_api "$A" "$A_API" /api/v1/add --data-urlencode "magnet=$MAGNET")"

log "attendo che il leecher raggiunga il seeder (max ${DEADLINE}s)"
result="niente"
for _ in $(seq 1 "$DEADLINE"); do
  peers="$(daemon_api "$A" "$A_API" "/api/v1/torrents/$HASH/peers" 2>/dev/null || true)"
  case "$peers" in
    *'"source":"holepunch"'*) result="holepunch"; break ;;
    *'"address":"10.20.0.2'*) result="diretta"; break ;;
  esac
  sleep 1
done

echo
if [[ "$result" == "holepunch" ]]; then
  log "PASS: il leecher ha raggiunto il seeder via holepunch (BEP 55)"
elif [[ "$result" == "diretta" ]]; then
  warn "il leecher ha raggiunto il seeder DIRETTAMENTE: il NAT emulato e troppo permissivo (non ha esercitato il buco)"
else
  warn "nessuna connessione osservata: puo servire un NAT 'cone' o regole conntrack diverse"
fi
echo "--- peer del leecher ---"
daemon_api "$A" "$A_API" "/api/v1/torrents/$HASH/peers" 2>/dev/null || true
echo
if [[ "$result" != "holepunch" ]]; then
  echo "--- tracker.log (ultime righe) ---"
  tail -n 10 "$WORK/tracker.log" 2>/dev/null || true
  for n in relay a b; do
    echo "--- $n.log (tracker/peer/holepunch/error) ---"
    grep -iE "tracker|holepunch|peer|error" "$WORK/$n.log" 2>/dev/null | tail -n 12 || true
  done
fi
echo
if [[ "$KEEP" == "1" ]]; then
  warn "topologia lasciata attiva (--keep); log in $WORK"
fi
[[ "$result" == "holepunch" ]]
