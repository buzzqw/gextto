#!/usr/bin/env python3
"""phase3-seed.py seeds a phase3-env.sh destination torrent with libtorrent, as
an external (non gx-core) peer for TestMeasureMergePhase3:

    scripts/phase3-seed.py ~/gx-phase3/debian 46881 &
    GX_MEASURE_MERGE3=~/gx-phase3/debian GX_MEASURE_MERGE3_PEER=127.0.0.1:46881 \\
        go test ./cmd/gx-torrent/ -run MeasureMergePhase3 -v
"""
import os, sys, time
import libtorrent as lt

env, port = sys.argv[1], int(sys.argv[2])
ses = lt.session({"listen_interfaces": f"127.0.0.1:{port}", "enable_dht": False,
                  "enable_lsd": False, "enable_upnp": False, "enable_natpmp": False,
                  "allow_multiple_connections_per_ip": True})
h = ses.add_torrent({"ti": lt.torrent_info(os.path.join(env, "dst.torrent")),
                     "save_path": os.path.join(env, "seed")})
while not h.status().is_seeding:
    time.sleep(0.2)
print(f"libtorrent {lt.__version__} seeding on 127.0.0.1:{port}", flush=True)
while True:
    time.sleep(1)
