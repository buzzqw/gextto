#!/usr/bin/env python3
"""libtorrent-leech.py downloads a torrent from a given peer with libtorrent,
as an external (non gx-core) peer for the choking/seeding campaign:

    scripts/libtorrent-leech.py t.torrent /save/dir <listen-port> <peer-ip:peer-port>

It prints one line on completion: DONE ms=<elapsed> downloaded=<bytes>.
Used by TestMeasureChokingLibtorrent.
"""
import sys, time
import libtorrent as lt

torrent, save, listen_port, peer = sys.argv[1], sys.argv[2], int(sys.argv[3]), sys.argv[4]
peer_ip, peer_port = peer.rsplit(":", 1)
peer_port = int(peer_port)

ses = lt.session({
    "listen_interfaces": f"127.0.0.1:{listen_port}",
    "enable_dht": False, "enable_lsd": False, "enable_upnp": False,
    "enable_natpmp": False, "allow_multiple_connections_per_ip": True,
})
h = ses.add_torrent({"ti": lt.torrent_info(torrent), "save_path": save})
h.connect_peer((peer_ip, peer_port))
start = time.time()
while True:
    st = h.status()
    if st.is_seeding:
        break
    if time.time() - start > 300:
        print(f"TIMEOUT downloaded={st.total_done}", flush=True)
        sys.exit(1)
    time.sleep(0.05)
print(f"DONE ms={int((time.time()-start)*1000)} downloaded={h.status().total_done}", flush=True)
