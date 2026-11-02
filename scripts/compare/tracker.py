#!/usr/bin/env python3
"""Minimal BitTorrent HTTP tracker for the isolated comparison network."""
import http.server
import socket
import struct
import threading
import time
import urllib.parse

swarms = {}
lock = threading.Lock()


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        if parsed.path == "/peers":
            # Used by the harness: how many peers announced this info hash.
            wanted = urllib.parse.parse_qs(parsed.query).get("hex", [""])[0]
            with lock:
                count = len({ip for ip, _ in swarms.get(bytes.fromhex(wanted) if wanted else b"", {})})
            self.reply(str(count).encode())
            return
        query = urllib.parse.parse_qs(parsed.query, encoding="latin-1")
        query = {k.encode("latin-1"): [v.encode("latin-1") for v in vs] for k, vs in query.items()}
        info_hash = query.get(b"info_hash", [b""])[0]
        port = int(query.get(b"port", [b"0"])[0])
        event = query.get(b"event", [b""])[0]
        addr = (self.client_address[0], port)
        now = time.time()
        with lock:
            swarm = swarms.setdefault(info_hash, {})
            if event == b"stopped":
                swarm.pop(addr, None)
            else:
                swarm[addr] = now
            peers = b"".join(
                socket.inet_aton(ip) + struct.pack("!H", p)
                for (ip, p), seen in swarm.items()
                if (ip, p) != addr and now - seen < 900
            )
        self.reply(b"d8:intervali1800e12:min intervali60e5:peers%d:%se" % (len(peers), peers))

    def reply(self, body):
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


http.server.ThreadingHTTPServer(("0.0.0.0", 6969), Handler).serve_forever()
