#!/usr/bin/env python3
"""Compare gx-torrent, qBittorrent, Transmission and Deluge in Docker.

Every client runs in the same kind of container on an isolated network, with the
same payload, the same reference peers and the same rules, so the difference
between two rows is the client and not the day. docs/measurements.md explains the
method and keeps the results; `make measure-compare` is the usual entry point.

  scripts/compare/run.py build                 build the images
  scripts/compare/run.py run [--rounds N] ...  run the scenarios, append results
  scripts/compare/run.py report                print the tables of the results
  scripts/compare/run.py clean                 remove the payloads and containers

Standard library only. The data lives in the test work area
($GEXTTO_TEST_ROOT, default ~/.cache/gextto-tests/compare — a real disk: /tmp is
often tmpfs), the results in gextto-data/bench/compare.jsonl.
"""
import argparse
import base64
import hashlib
import json
import os
import platform
import shutil
import signal
import statistics
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
TEST_ROOT = Path(
    os.environ.get(
        "GEXTTO_TEST_ROOT",
        Path(os.environ.get("XDG_CACHE_HOME") or Path.home() / ".cache") / "gextto-tests",
    )
)
WORK = Path(os.environ.get("GX_COMPARE_DIR", TEST_ROOT / "compare"))
RESULTS = Path(os.environ.get("GX_COMPARE_OUT", ROOT / "gextto-data" / "bench" / "compare.jsonl"))

CLIENTS = ["gx-torrent", "qbittorrent", "transmission", "deluge"]
SCENARIOS = ["idle", "full", "line", "line-utp", "check-upload", "many"]

NET = "gxc-net"
SUBNET = "172.30.0"
TRACKER_IP = f"{SUBNET}.2"
SUT_IP = f"{SUBNET}.10"
ANNOUNCE = f"http://{TRACKER_IP}:6969/announce"
SUT_CPUS = "2,3"
REF_CPUS = "0,1"
SUT_MEMORY = "4g"
REF_MEMORY = "1500m"
UID = f"{os.getuid()}:{os.getgid()}"

PIECE = 2 * 1024 * 1024
MANY_COUNT = 100
MANY_SIZE = 2 * 1024 * 1024
LINE_PEERS = 4
LINE_RATE = "25mbit"  # 4 peers x 25 Mbit/s = a 100 Mbit/s line
LINE_DELAY = "40ms"
MIN_AVAILABLE_KIB = 1500 * 1024
QUIET_CORES = 1.0
MAX_ATTEMPTS = 3


# The test torrents of every official round (docs/measurements.md). A payload that
# hashes differently is refused: the round would not compare with the earlier ones.
OFFICIAL_HASHES = {
    "big.bin": "f9221811202b8222c1ea7d6b18c1956ac6fcbd5a",
    "line.bin": "d4b2903110733690a8cc2a731bb6859b0db6461c",
    "many-0000.bin": "cd646c86c9b3a72653199896634b7d0a2f1dea32",
    "many-0099.bin": "dbf983049538743b152e57de03b92066d8dc7444",
}


def log(*args):
    print(time.strftime("%H:%M:%S"), *args, flush=True)


def sh(*cmd, check=True, capture=True):
    result = subprocess.run(cmd, check=False, text=True, capture_output=capture)
    if check and result.returncode != 0:
        raise RuntimeError(f"{' '.join(cmd)}: {result.stderr.strip()}")
    return result.stdout.strip() if capture else ""


# ---------------------------------------------------------------------------
# Torrents and payload
# ---------------------------------------------------------------------------


def bencode(value):
    if isinstance(value, int):
        return b"i%de" % value
    if isinstance(value, bytes):
        return b"%d:%s" % (len(value), value)
    if isinstance(value, str):
        return bencode(value.encode())
    if isinstance(value, list):
        return b"l" + b"".join(bencode(v) for v in value) + b"e"
    if isinstance(value, dict):
        out = b"d"
        for key in sorted(value, key=lambda k: k.encode()):
            out += bencode(key) + bencode(value[key])
        return out + b"e"
    raise TypeError(type(value))


class Payload:
    """One file with its .torrent. The bytes come from SHAKE-256 of the name and the
    chunk number: they look random, so nothing compresses them, and they are the same
    on every machine and in every future round, which keeps the info hash the same."""

    def __init__(self, name, size, piece=PIECE):
        self.name, self.size, self.piece = name, size, piece
        self.path = WORK / "payload" / name
        self.torrent_path = WORK / "payload" / f"{name}.torrent"
        self.prepare()
        self.data = self.torrent_path.read_bytes()
        self.info_hash = self._hash_from(self.data)
        expected = OFFICIAL_HASHES.get(name)
        if expected and expected != self.info_hash:
            raise SystemExit(f"{name}: info hash {self.info_hash}, expected {expected}: not the official payload")

    def prepare(self):
        self.path.parent.mkdir(parents=True, exist_ok=True)
        if not self.path.exists() or self.path.stat().st_size != self.size:
            log(f"creating {self.name} ({self.size} bytes)")
            with open(self.path, "wb") as out:
                left, number = self.size, 0
                while left:
                    size = min(left, 16 * 1024 * 1024)
                    out.write(hashlib.shake_256(f"gextto-compare/{self.name}/{number}".encode()).digest(size))
                    left -= size
                    number += 1
            self.torrent_path.unlink(missing_ok=True)
        if self.torrent_path.exists():
            return
        pieces = bytearray()
        with open(self.path, "rb") as src:
            while chunk := src.read(self.piece):
                pieces += hashlib.sha1(chunk).digest()
        meta = {
            "announce": ANNOUNCE,
            "info": {
                "name": self.name,
                "length": self.size,
                "piece length": self.piece,
                "pieces": bytes(pieces),
                "private": 1,
            },
        }
        self.torrent_path.write_bytes(bencode(meta))

    @staticmethod
    def _hash_from(data):
        # The info dictionary is the last key of the files written above.
        start = data.index(b"4:infod") + len(b"4:info")
        return hashlib.sha1(data[start:-1]).hexdigest()

    def link_into(self, folder):
        folder.mkdir(parents=True, exist_ok=True)
        target = folder / self.name
        if not target.exists():
            os.link(self.path, target)

    def drop_cache(self):
        """Forget this file's pages, so the next read really comes from the disk."""
        os.sync()
        fd = os.open(self.path, os.O_RDONLY)
        try:
            os.posix_fadvise(fd, 0, 0, os.POSIX_FADV_DONTNEED)
        finally:
            os.close(fd)


# ---------------------------------------------------------------------------
# HTTP helpers
# ---------------------------------------------------------------------------


def http(url, data=None, headers=None, timeout=20, opener=None):
    req = urllib.request.Request(url, data=data, headers=headers or {})
    opener = opener or urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(req, timeout=timeout) as resp:
        return resp.read()


def multipart(fields, files):
    boundary = uuid.uuid4().hex
    out = b""
    for key, value in fields.items():
        out += (
            f'--{boundary}\r\nContent-Disposition: form-data; name="{key}"\r\n\r\n{value}\r\n'
        ).encode()
    for key, (filename, content) in files.items():
        out += (
            f'--{boundary}\r\nContent-Disposition: form-data; name="{key}"; filename="{filename}"\r\n'
            "Content-Type: application/x-bittorrent\r\n\r\n"
        ).encode() + content + b"\r\n"
    out += f"--{boundary}--\r\n".encode()
    return out, {"Content-Type": f"multipart/form-data; boundary={boundary}"}


# ---------------------------------------------------------------------------
# Containers
# ---------------------------------------------------------------------------


class Container:
    def __init__(self, name, image, args, env_dir, ip, cpus, memory, cap_add=()):
        self.name, self.ip, self.dir = name, ip, env_dir
        cmd = [
            "docker", "run", "-d", "--name", name, "--network", NET, "--ip", ip,
            "--user", UID, "-e", "HOME=/data", "-v", f"{env_dir}:/data",
            "--cpuset-cpus", cpus, "--cpu-shares", "512",
            "--memory", memory, "--memory-swap", memory,
            "--ulimit", "nofile=65536:65536",
        ]
        for cap in cap_add:
            cmd += ["--cap-add", cap]
        self.id = sh(*cmd, image, *args)

    @property
    def pid(self):
        return int(sh("docker", "inspect", "-f", "{{.State.Pid}}", self.name))

    @property
    def cgroup(self):
        return Path(f"/sys/fs/cgroup/system.slice/docker-{self.id}.scope")

    def cpu_seconds(self):
        for line in (self.cgroup / "cpu.stat").read_text().splitlines():
            if line.startswith("usage_usec"):
                return int(line.split()[1]) / 1e6
        return 0.0

    def memory_stat(self):
        out = {}
        for line in (self.cgroup / "memory.stat").read_text().splitlines():
            key, value = line.split()
            out[key] = int(value)
        return out

    def rss_kib(self, pid):
        for line in Path(f"/proc/{pid}/status").read_text().splitlines():
            if line.startswith("VmRSS"):
                return int(line.split()[1])
        return 0

    def shape(self, rate, delay):
        sh("docker", "exec", "--user", "root", self.name, "tc", "qdisc", "add", "dev", "eth0",
           "root", "netem", "delay", delay, "rate", rate, "limit", "1000")

    def block_tcp(self):
        """Drop incoming TCP on the peer port: a peer reachable only over uTP."""
        sh("docker", "exec", "--user", "root", self.name, "tc", "qdisc", "add", "dev", "eth0", "ingress")
        sh("docker", "exec", "--user", "root", self.name, "tc", "filter", "add", "dev", "eth0", "parent", "ffff:",
           "protocol", "ip", "u32", "match", "ip", "protocol", "6", "0xff", "match", "ip", "dport", "6881", "0xffff",
           "action", "drop")

    def restart(self):
        sh("docker", "stop", "-t", "120", self.name)
        started = time.time()
        sh("docker", "start", self.name)
        return started

    def logs(self):
        return subprocess.run(["docker", "logs", "--tail", "30", self.name], text=True, capture_output=True).stderr


class Client:
    """One running client: a container plus the calls the scenarios need."""

    name = ""
    image = ""

    def __init__(self, slot, ip, cpus, memory, reference=False, protocol="TCP", cap_add=(), utp=True):
        self.reference, self.protocol, self.utp = reference, protocol, utp
        self.dir = WORK / "run" / slot
        shutil.rmtree(self.dir, ignore_errors=True)
        (self.dir / "cfg").mkdir(parents=True)
        (self.dir / "dl").mkdir()
        self.configure()
        self.container = Container(f"gxc-{slot}", self.image, self.command(), self.dir, ip, cpus, memory, cap_add)
        self.ip = ip
        self.helpers = []

    def configure(self):
        pass

    def command(self):
        raise NotImplementedError

    def wait_ready(self, timeout=240):
        deadline = time.time() + timeout
        last = None
        while time.time() < deadline:
            try:
                self.status()
                return
            except Exception as exc:  # the API is not up yet
                last = exc
                time.sleep(0.25)
        raise RuntimeError(f"{self.name} did not answer: {last}\n{self.container.logs()}")

    def add(self, payload, skip_check=False):
        raise NotImplementedError

    def recheck(self, payload):
        raise NotImplementedError

    def status(self):
        """{hash: (state, progress)} with state in checking/downloading/seeding/other."""
        raise NotImplementedError

    def stop(self):
        for name in [c for c in self.helpers] + [self.container.name]:
            sh("docker", "rm", "-f", name, check=False)
        shutil.rmtree(self.dir, ignore_errors=True)


class QBitAPI(Client):
    """qBittorrent's Web API; gx-torrent answers the same calls."""

    def url(self, path):
        return f"http://{self.ip}:8080/api/v2/{path}"

    def add(self, payload, skip_check=False):
        body, headers = multipart(
            {"savepath": "/data/dl", "skip_checking": "true" if skip_check else "false"},
            {"torrents": (f"{payload.name}.torrent", payload.data)},
        )
        answer = http(self.url("torrents/add"), body, headers, timeout=60)
        if b"Fails" in answer:
            raise RuntimeError(f"{self.name} refused the torrent: {answer!r}")

    def recheck(self, payload):
        http(self.url("torrents/recheck"), urllib.parse.urlencode({"hashes": payload.info_hash}).encode())

    def status(self):
        rows = json.loads(http(self.url("torrents/info")))
        out = {}
        for row in rows:
            raw = row.get("state", "")
            if raw.startswith("checking") or raw == "allocating":
                state = "checking"
            elif raw in ("uploading", "stalledUP", "queuedUP", "forcedUP", "stoppedUP", "pausedUP"):
                state = "seeding"
            elif raw in ("downloading", "stalledDL", "queuedDL", "forcedDL", "metaDL", "forcedMetaDL"):
                state = "downloading"
            else:
                state = raw or "other"
            out[row["hash"]] = (state, float(row.get("progress", 0)))
        return out


class QBittorrent(QBitAPI):
    name = "qbittorrent"
    image = "gxc-qbittorrent"

    def configure(self):
        folder = self.dir / "cfg" / "qBittorrent" / "config"
        folder.mkdir(parents=True)
        # A reference peer speaks plaintext, keeps every torrent active and uses
        # the transport of the scenario; the client under test keeps its defaults.
        encryption = 2 if self.reference else 0
        protocol = self.protocol if self.reference else "Both"
        queueing = "false" if self.reference else "true"
        (folder / "qBittorrent.conf").write_text(
            "[LegalNotice]\nAccepted=true\n"
            "[BitTorrent]\n"
            "Session\\DHTEnabled=false\nSession\\PeXEnabled=false\nSession\\LSDEnabled=false\n"
            f"Session\\Encryption={encryption}\nSession\\BTProtocol={protocol}\n"
            f"Session\\QueueingSystemEnabled={queueing}\n"
            "Session\\DefaultSavePath=/data/dl\nSession\\Port=6881\n"
            "Session\\IgnoreLimitsOnLAN=true\n"
            "[Network]\nPortForwardingEnabled=false\n"
            "[Preferences]\n"
            "WebUI\\Port=8080\nWebUI\\LocalHostAuth=false\n"
            "WebUI\\AuthSubnetWhitelistEnabled=true\nWebUI\\AuthSubnetWhitelist=0.0.0.0/0\n"
            "WebUI\\HostHeaderValidation=false\nWebUI\\UseUPnP=false\n"
        )

    def command(self):
        return ["qbittorrent-nox", "--profile=/data/cfg", "--confirm-legal-notice"]


class GxTorrent(QBitAPI):
    name = "gx-torrent"
    image = "gxc-gx-torrent"

    def command(self):
        return [
            "gx-torrent", "-mode", "standalone", "-listen", "0.0.0.0:8080", "-insecure",
            "-data", "/data/cfg", "-download-dir", "/data/dl", "-peer-ports", "6881-6881",
            "-no-dht", "-no-pex", "-no-lsd", "-no-upnp", "-no-natpmp",
        ]


class Transmission(Client):
    name = "transmission"
    image = "gxc-transmission"
    session = ""

    def configure(self):
        (self.dir / "cfg" / "settings.json").write_text(json.dumps({
            "rpc-whitelist-enabled": False, "rpc-host-whitelist-enabled": False,
            "rpc-authentication-required": False, "rpc-port": 9091, "rpc-bind-address": "0.0.0.0",
            "dht-enabled": False, "lpd-enabled": False, "pex-enabled": False,
            "port-forwarding-enabled": False, "encryption": 0, "peer-port": 6881,
            "download-dir": "/data/dl", "incomplete-dir-enabled": False,
            "ratio-limit-enabled": False, "idle-seeding-limit-enabled": False,
            # Transmission dials over uTP first and waits for minutes on a TCP-only
            # peer, so the TCP scenarios turn its uTP off.
            "utp-enabled": self.utp,
        }))

    def command(self):
        return ["transmission-daemon", "-f", "-g", "/data/cfg", "--log-level=error"]

    def rpc(self, method, **arguments):
        url = f"http://{self.ip}:9091/transmission/rpc"
        body = json.dumps({"method": method, "arguments": arguments}).encode()
        try:
            return json.loads(http(url, body, {"X-Transmission-Session-Id": self.session}))
        except urllib.error.HTTPError as exc:
            if exc.code != 409:
                raise
            self.session = exc.headers.get("X-Transmission-Session-Id", "")
            return json.loads(http(url, body, {"X-Transmission-Session-Id": self.session}))

    def add(self, payload, skip_check=False):
        answer = self.rpc("torrent-add", metainfo=base64.b64encode(payload.data).decode(), **{"download-dir": "/data/dl"})
        if answer.get("result") != "success":
            raise RuntimeError(f"transmission refused the torrent: {answer}")

    def recheck(self, payload):
        self.rpc("torrent-verify", ids=[payload.info_hash])

    def status(self):
        rows = self.rpc("torrent-get", fields=["hashString", "percentDone", "status"])["arguments"]["torrents"]
        states = {1: "checking", 2: "checking", 3: "downloading", 4: "downloading", 5: "seeding", 6: "seeding", 0: "other"}
        return {r["hashString"]: (states.get(r["status"], "other"), float(r["percentDone"])) for r in rows}


class Deluge(Client):
    name = "deluge"
    image = "gxc-deluge"

    def configure(self):
        core = {
            "dht": False, "upnp": False, "natpmp": False, "lsd": False, "utpex": False,
            "download_location": "/data/dl", "listen_ports": [6881, 6881], "random_port": False,
            "allow_remote": True, "new_release_check": False, "stop_seed_at_ratio": False,
        }
        (self.dir / "cfg" / "core.conf").write_text('{"file": 1, "format": 1}' + json.dumps(core))
        self.jar = urllib.request.build_opener(
            urllib.request.ProxyHandler({}),
            urllib.request.HTTPCookieProcessor(),
        )
        self.seq = 0
        self.connected = False

    def command(self):
        return ["deluged", "-d", "-c", "/data/cfg", "-L", "warning", "-u", "0.0.0.0"]

    def start_web(self):
        """The web front end runs in its own container, so its CPU does not count."""
        name = f"{self.container.name}-web"
        sh("docker", "rm", "-f", name, check=False)
        sh("docker", "run", "-d", "--name", name, "--network", f"container:{self.container.name}",
           "--user", UID, "-e", "HOME=/data", "-v", f"{self.dir}:/data", "--cpuset-cpus", REF_CPUS,
           self.image, "deluge-web", "-d", "-c", "/data/cfg", "-p", "8112", "-L", "warning")
        if name not in self.helpers:
            self.helpers.append(name)

    def rpc(self, method, *params):
        self.seq += 1
        body = json.dumps({"method": method, "params": list(params), "id": self.seq}).encode()
        answer = json.loads(http(f"http://{self.ip}:8112/json", body, {"Content-Type": "application/json"},
                                 timeout=60, opener=self.jar))
        if answer.get("error"):
            # web.connect returned before the daemon's methods were listed: connect again.
            self.connected = False
            raise RuntimeError(f"deluge {method}: {answer['error']}")
        return answer["result"]

    def connect(self):
        if self.connected:
            return
        self.rpc("auth.login", "deluge")
        hosts = self.rpc("web.get_hosts")
        self.rpc("web.connect", hosts[0][0])
        self.connected = True

    def wait_ready(self, timeout=240):
        self.connected = False
        self.start_web()
        super().wait_ready(timeout)

    def add(self, payload, skip_check=False):
        self.connect()
        self.rpc("core.add_torrent_file", f"{payload.name}.torrent", base64.b64encode(payload.data).decode(),
                 {"download_location": "/data/dl"})

    def recheck(self, payload):
        self.rpc("core.force_recheck", [payload.info_hash])

    def status(self):
        self.connect()
        rows = self.rpc("core.get_torrents_status", {}, ["progress", "state"])
        states = {"Checking": "checking", "Downloading": "downloading", "Seeding": "seeding", "Queued": "seeding"}
        return {h: (states.get(r["state"], r["state"]), float(r["progress"]) / 100.0) for h, r in rows.items()}


CLIENT_CLASSES = {c.name: c for c in (GxTorrent, QBittorrent, Transmission, Deluge)}


# ---------------------------------------------------------------------------
# Host checks and sampling
# ---------------------------------------------------------------------------


def host_busy_seconds():
    fields = Path("/proc/stat").read_text().splitlines()[0].split()[1:]
    values = [int(f) for f in fields]
    return (sum(values[:8]) - values[3] - values[4]) / os.sysconf("SC_CLK_TCK")


def mem_available_kib():
    for line in Path("/proc/meminfo").read_text().splitlines():
        if line.startswith("MemAvailable"):
            return int(line.split()[1])
    return 0


def ffmpeg_running():
    return subprocess.run(["pgrep", "-x", "ffmpeg"], capture_output=True).returncode == 0


FFMPEG_SEEN = threading.Event()


def watch_ffmpeg():
    """A decode during a run disturbs it: the run is marked and done again."""
    while True:
        if ffmpeg_running():
            FFMPEG_SEEN.set()
        time.sleep(2)


def guard_host():
    if mem_available_kib() < MIN_AVAILABLE_KIB:
        raise SystemExit("less than 1.5 GiB of memory available: stopping before the host suffers")


def wait_quiet(limit=1200):
    """Wait for the rest of the host to use less than QUIET_CORES, for 10 s in a row."""
    started = time.time()
    while time.time() - started < limit:
        guard_host()
        before = host_busy_seconds()
        time.sleep(10)
        cores = (host_busy_seconds() - before) / 10
        if cores < QUIET_CORES and not ffmpeg_running():
            return cores
        log(f"host busy ({cores:.2f} cores), waiting")
    log("host never went quiet; the run is recorded with its background figure")
    return -1.0


class Sampler:
    """Peak memory of the client under test and CPU of everything, while it runs."""

    def __init__(self, sut, others=()):
        self.sut, self.others = sut, list(others)
        self.pid = sut.container.pid
        self.stop_flag = threading.Event()
        self.peak_anon = self.peak_rss = self.peak_file_mapped = 0
        self.last_anon = self.last_rss = 0
        self.thread = threading.Thread(target=self.loop, daemon=True)

    def containers(self):
        return [self.sut.container] + [o.container for o in self.others]

    def __enter__(self):
        self.t0 = time.time()
        self.cpu0 = self.sut.container.cpu_seconds()
        self.all0 = sum(c.cpu_seconds() for c in self.containers())
        self.host0 = host_busy_seconds()
        self.thread.start()
        return self

    def loop(self):
        ticks = 0
        while not self.stop_flag.wait(0.5):
            self.sample()
            ticks += 1
            if ticks % 10 == 0 and mem_available_kib() < MIN_AVAILABLE_KIB:
                os._exit(3)

    def sample(self):
        try:
            mem = self.sut.container.memory_stat()
            self.last_anon = mem.get("anon", 0)
            self.last_rss = self.sut.container.rss_kib(self.pid) * 1024
        except OSError:
            return
        self.peak_anon = max(self.peak_anon, self.last_anon)
        self.peak_rss = max(self.peak_rss, self.last_rss)
        self.peak_file_mapped = max(self.peak_file_mapped, mem.get("file_mapped", 0))

    def __exit__(self, *exc):
        self.stop_flag.set()
        self.thread.join()
        self.sample()
        self.wall = time.time() - self.t0
        self.cpu = self.sut.container.cpu_seconds() - self.cpu0
        all_cpu = sum(c.cpu_seconds() for c in self.containers()) - self.all0
        self.background = max(0.0, (host_busy_seconds() - self.host0 - all_cpu)) / max(self.wall, 0.001)

    def metrics(self):
        return {
            "wall_s": round(self.wall, 2),
            "cpu_s": round(self.cpu, 2),
            "cpu_pct": round(self.cpu / max(self.wall, 0.001) * 100, 2),
            "peak_anon_mib": round(self.peak_anon / 1048576, 1),
            "peak_rss_mib": round(self.peak_rss / 1048576, 1),
            "peak_mapped_mib": round(self.peak_file_mapped / 1048576, 1),
            "anon_mib": round(self.last_anon / 1048576, 1),
            "rss_mib": round(self.last_rss / 1048576, 1),
            "background_cores": round(self.background, 2),
        }


# ---------------------------------------------------------------------------
# Scenario helpers
# ---------------------------------------------------------------------------


def start_tracker():
    sh("docker", "rm", "-f", "gxc-tracker", check=False)
    sh("docker", "run", "-d", "--name", "gxc-tracker", "--network", NET, "--ip", TRACKER_IP,
       "--cpuset-cpus", REF_CPUS, "gxc-tracker", "python3", "-u", "/tracker.py")
    deadline = time.time() + 20
    while time.time() < deadline:
        try:
            http(f"http://{TRACKER_IP}:6969/peers", timeout=2)
            return
        except Exception:
            time.sleep(0.2)
    raise RuntimeError("tracker did not start")


def peers_known(payload):
    return int(http(f"http://{TRACKER_IP}:6969/peers?hex={payload.info_hash}", timeout=5))


def start_sut(client, utp=True):
    sut = CLIENT_CLASSES[client]("sut", SUT_IP, SUT_CPUS, SUT_MEMORY, utp=utp)
    sut.wait_ready()
    return sut


def start_reference(index, payload, protocol="TCP", seed=True, shape=False):
    ref = QBittorrent(f"ref{index}", f"{SUBNET}.{20 + index}", REF_CPUS, REF_MEMORY,
                      reference=True, protocol=protocol, cap_add=("NET_ADMIN",) if shape else ())
    ref.wait_ready()
    if shape:
        ref.container.shape(LINE_RATE, LINE_DELAY)
        if protocol == "UTP":
            # In this mode qBittorrent still accepts TCP and then resets it; a real
            # uTP-only peer does not answer on TCP at all.
            ref.container.block_tcp()
    if seed:
        payload.link_into(ref.dir / "dl")
        ref.add(payload, skip_check=True)
    return ref


def wait_announced(payload, count, timeout=60):
    deadline = time.time() + timeout
    while time.time() < deadline:
        if peers_known(payload) >= count:
            return
        time.sleep(0.2)
    raise RuntimeError("the reference peers did not reach the tracker")


def wait_done(client, info_hash, timeout=900, want="seeding"):
    deadline = time.time() + timeout
    while time.time() < deadline:
        guard_host()
        row = client.status().get(info_hash)
        if row and row[1] >= 0.9999 and (want is None or row[0] == want):
            return
        time.sleep(0.25)
    raise RuntimeError(f"{client.name}: timed out, status {client.status().get(info_hash)}")


def wait_check_started(client, info_hash, timeout=10):
    deadline = time.time() + timeout
    while time.time() < deadline:
        row = client.status().get(info_hash)
        if row and (row[0] != "seeding" or row[1] < 0.9999):
            return
        time.sleep(0.02)


def same_file(a, b):
    return subprocess.run(["cmp", "-s", str(a), str(b)]).returncode == 0


def cleanup():
    names = sh("docker", "ps", "-aq", "--filter", "name=gxc-sut", "--filter", "name=gxc-ref", check=False)
    for cid in names.split():
        sh("docker", "rm", "-f", cid, check=False)
    shutil.rmtree(WORK / "run", ignore_errors=True)
    os.sync()


# ---------------------------------------------------------------------------
# Scenarios
# ---------------------------------------------------------------------------


def scenario_idle(client, env):
    sut = start_sut(client)
    time.sleep(15)
    with Sampler(sut) as sample:
        time.sleep(60)
    return sample.metrics()


def transfer(client, payload, peers, protocol="TCP", shape=False):
    refs = []
    payload.drop_cache()
    start_tracker()
    for index in range(peers):
        refs.append(start_reference(index, payload, protocol=protocol, shape=shape))
    wait_announced(payload, peers)
    sut = start_sut(client, utp=protocol != "TCP")
    with Sampler(sut, refs) as sample:
        sut.add(payload)
        wait_done(sut, payload.info_hash, want=None)
    metrics = sample.metrics()
    metrics["size_mib"] = payload.size / 1048576
    metrics["mb_s"] = round(payload.size / 1e6 / sample.wall, 2)
    metrics["cpu_s_per_gib"] = round(sample.cpu / (payload.size / 2**30), 2)
    os.sync()
    metrics["intact"] = same_file(payload.path, sut.dir / "dl" / payload.name)
    return metrics


def scenario_full(client, env):
    return transfer(client, env["big"], 1)


def scenario_line(client, env):
    return transfer(client, env["line"], LINE_PEERS, shape=True)


def scenario_line_utp(client, env):
    return transfer(client, env["line"], LINE_PEERS, protocol="UTP", shape=True)


def scenario_check_upload(client, env):
    payload = env["big"]
    start_tracker()
    sut = start_sut(client, utp=False)
    payload.link_into(sut.dir / "dl")
    # Not every client hashes existing files when a torrent is added (Transmission
    # trusts them), so the check measured here is the explicit recheck of each one.
    sut.add(payload)
    wait_done(sut, payload.info_hash)
    payload.drop_cache()
    with Sampler(sut) as check:
        sut.recheck(payload)
        wait_check_started(sut, payload.info_hash)
        wait_done(sut, payload.info_hash)
    if check.cpu < 0.25 * payload.size / 2**30:
        raise RuntimeError(f"{client}: {check.cpu:.2f} s of CPU cannot have hashed the file")
    metrics = {f"check_{k}": v for k, v in check.metrics().items()}
    metrics["check_mb_s"] = round(payload.size / 1e6 / check.wall, 1)

    payload.drop_cache()
    wait_announced(payload, 1)
    leecher = start_reference(0, payload, seed=False)
    with Sampler(sut, [leecher]) as upload:
        leecher.add(payload)
        wait_done(leecher, payload.info_hash, want=None)
    metrics.update({f"up_{k}": v for k, v in upload.metrics().items()})
    metrics["up_mb_s"] = round(payload.size / 1e6 / upload.wall, 2)
    metrics["up_cpu_s_per_gib"] = round(upload.cpu / (payload.size / 2**30), 2)
    os.sync()
    metrics["up_intact"] = same_file(payload.path, leecher.dir / "dl" / payload.name)
    return metrics


def scenario_many(client, env):
    payloads = env["many"]
    start_tracker()
    sut = start_sut(client)
    for payload in payloads:
        payload.link_into(sut.dir / "dl")
    started = time.time()
    for payload in payloads:
        sut.add(payload)
    added = time.time() - started

    def all_seeding(timeout=900):
        deadline = time.time() + timeout
        while time.time() < deadline:
            guard_host()
            try:
                rows = sut.status()
            except Exception:
                time.sleep(0.25)
                continue
            if len(rows) == len(payloads) and all(r[1] >= 0.9999 and r[0] == "seeding" for r in rows.values()):
                return
            time.sleep(0.5)
        raise RuntimeError(f"{client}: not all torrents reached seeding")

    all_seeding()
    ready = time.time() - started
    time.sleep(15)
    with Sampler(sut) as sample:
        time.sleep(60)
    metrics = {"add_s": round(added, 2), "ready_s": round(ready, 2), **sample.metrics()}

    restart_from = sut.container.restart()
    sut.wait_ready()
    all_seeding()
    metrics["restart_s"] = round(time.time() - restart_from, 2)
    return metrics


SCENARIO_FUNCS = {
    "idle": scenario_idle,
    "full": scenario_full,
    "line": scenario_line,
    "line-utp": scenario_line_utp,
    "check-upload": scenario_check_upload,
    "many": scenario_many,
}


def setup_env(args, scenarios):
    env = {}
    if {"full", "check-upload"} & set(scenarios):
        env["big"] = Payload("big.bin" if args.big_gib == 4 else f"big-{args.big_gib}.bin", int(args.big_gib * 2**30))
    if {"line", "line-utp"} & set(scenarios):
        env["line"] = Payload("line.bin" if args.line_gib == 0.5 else f"line-{args.line_gib}.bin", int(args.line_gib * 2**30))
    if "many" in scenarios:
        env["many"] = [Payload(f"many-{i:04d}.bin", MANY_SIZE, piece=256 * 1024) for i in range(args.many)]
    return env


def ensure_network():
    if not sh("docker", "network", "ls", "-q", "--filter", f"name=^{NET}$"):
        sh("docker", "network", "create", "--internal", "--subnet", f"{SUBNET}.0/24", NET)


def command_build(args):
    for target in CLIENTS + ["tracker"]:
        log("building", target)
        subprocess.run(
            ["nice", "-n", "10", "docker", "build", "-q", "-f", "scripts/compare/Dockerfile",
             "--target", target, "-t", f"gxc-{target}", "."],
            check=True, cwd=ROOT, stdout=subprocess.DEVNULL,
        )


def command_clean(args):
    """Drop the payloads and the containers of a finished round: a round leaves
    several GiB behind and the harness regenerates them from the fixed formula."""
    cleanup()
    shutil.rmtree(WORK, ignore_errors=True)
    print(f"removed {WORK}")


def describe_host():
    """What a future round has to match, written next to the results."""
    cpu = next((line.split(":", 1)[1].strip() for line in Path("/proc/cpuinfo").read_text().splitlines()
                if line.startswith("model name")), "?")
    work_mount = sh("findmnt", "-no", "SOURCE,FSTYPE", "--target", str(WORK), check=False)
    source = work_mount.split()[0] if work_mount else ""
    disk = sh("lsblk", "-dno", "MODEL", "/dev/" + sh("lsblk", "-no", "PKNAME", source, check=False), check=False) if source else ""
    images = {t: sh("docker", "image", "inspect", "-f", "{{.Id}}", f"gxc-{t}", check=False)[7:19] for t in CLIENTS}
    return {
        "cpu": cpu, "cores": os.cpu_count(), "kernel": platform.release(),
        "ram_gib": round(os.sysconf("SC_PAGE_SIZE") * os.sysconf("SC_PHYS_PAGES") / 2**30, 1),
        "disk": disk, "filesystem": work_mount, "docker": sh("docker", "version", "-f", "{{.Server.Version}}"),
        "images": images, "sut_cpus": SUT_CPUS, "sut_memory": SUT_MEMORY,
        "ref_cpus": REF_CPUS, "ref_memory": REF_MEMORY,
    }


def command_run(args):
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(143))
    if not args.no_build:
        command_build(args)
    os.sched_setaffinity(0, {int(c) for c in REF_CPUS.split(",")})
    clients = args.clients.split(",")
    scenarios = args.scenarios.split(",")
    ensure_network()
    cleanup()
    env = setup_env(args, scenarios)
    RESULTS.parent.mkdir(parents=True, exist_ok=True)
    commit = sh("git", "-C", str(ROOT), "describe", "--always", "--dirty", check=False)
    with RESULTS.open("a") as out:
        out.write(json.dumps({"scenario": "host", "tag": args.tag, "commit": commit, "stamp": time.strftime("%Y-%m-%dT%H:%M:%S"),
                              "client": "", "host": describe_host(),
                              "hashes": {k: v.info_hash for k, v in env.items() if isinstance(v, Payload)}}) + "\n")
    failures = 0
    try:
        failures = run_rounds(args, clients, scenarios, env, commit)
    finally:
        cleanup()
        sh("docker", "rm", "-f", "gxc-tracker", check=False)
    log("finished, failures:", failures)
    return 1 if failures else 0


def disturbed(record):
    if record.get("ffmpeg"):
        return True
    return any(k.endswith("background_cores") and v > QUIET_CORES for k, v in record.items())


def run_rounds(args, clients, scenarios, env, commit):
    threading.Thread(target=watch_ffmpeg, daemon=True).start()
    failures = 0
    for round_no in range(args.rounds):
        # Interleaved rounds: a slow hour touches every client, not just one.
        order = clients[round_no % len(clients):] + clients[:round_no % len(clients)]
        for scenario in scenarios:
            for client in order:
                for attempt in range(MAX_ATTEMPTS):
                    quiet = wait_quiet()
                    FFMPEG_SEEN.clear()
                    log(f"round {round_no + 1}/{args.rounds} {scenario} {client} (host {quiet:.2f} cores)")
                    record = {
                        "stamp": time.strftime("%Y-%m-%dT%H:%M:%S"), "tag": args.tag, "commit": commit,
                        "round": round_no + 1, "client": client, "scenario": scenario, "quiet_cores": round(quiet, 2),
                    }
                    try:
                        record.update(SCENARIO_FUNCS[scenario](client, env))
                    except Exception as exc:  # recorded and visible, the round goes on
                        record["error"] = str(exc)[:1500]
                        failures += 1
                        log("FAILED:", record["error"])
                    finally:
                        cleanup()
                    record["ffmpeg"] = FFMPEG_SEEN.is_set()
                    record["disturbed"] = disturbed(record)
                    with RESULTS.open("a") as out:
                        out.write(json.dumps(record) + "\n")
                    log(json.dumps({k: v for k, v in record.items()
                                    if k not in ("stamp", "tag", "commit", "round", "client", "scenario")}))
                    if not record["disturbed"]:
                        break
                    log("disturbed by the rest of the host, running it again")
    return failures


# ---------------------------------------------------------------------------
# Report
# ---------------------------------------------------------------------------


REPORT = {
    "idle": ["cpu_pct", "rss_mib", "anon_mib", "background_cores"],
    "full": ["wall_s", "mb_s", "cpu_s_per_gib", "peak_anon_mib", "peak_rss_mib", "background_cores"],
    "line": ["wall_s", "mb_s", "cpu_pct", "cpu_s_per_gib", "peak_anon_mib", "peak_rss_mib", "background_cores"],
    "line-utp": ["wall_s", "mb_s", "cpu_pct", "cpu_s_per_gib", "peak_anon_mib", "peak_rss_mib", "background_cores"],
    "check-upload": ["check_wall_s", "check_mb_s", "check_cpu_s", "check_peak_anon_mib", "up_wall_s", "up_mb_s",
                     "up_cpu_s_per_gib", "up_peak_anon_mib", "up_peak_rss_mib", "up_background_cores"],
    "many": ["add_s", "ready_s", "restart_s", "cpu_pct", "rss_mib", "anon_mib", "background_cores"],
}


def command_report(args):
    rows = [json.loads(line) for line in RESULTS.read_text().splitlines()]
    if args.tag:
        rows = [r for r in rows if r.get("tag") == args.tag]
    for row in rows:
        if row["scenario"] == "host":
            print(json.dumps({k: row.get(k) for k in ("stamp", "commit", "host", "hashes")}))
    for scenario, columns in REPORT.items():
        selected = [r for r in rows if r["scenario"] == scenario]
        if not selected:
            continue
        print(f"\n### {scenario}\n")
        print("| client | runs | " + " | ".join(columns) + " |")
        print("|---|---|" + "---|" * len(columns))
        for client in CLIENTS:
            mine = [r for r in selected if r["client"] == client and not r.get("disturbed")]
            good = [r for r in mine if "error" not in r and r.get("intact", True) and r.get("up_intact", True)]
            bad = len(mine) - len(good)
            cells = []
            for column in columns:
                values = [r[column] for r in good if column in r]
                if not values:
                    cells.append("—")
                    continue
                cells.append(f"{statistics.median(values):g} ({min(values):g}–{max(values):g})")
            print(f"| {client} | {len(good)}{f' +{bad} failed' if bad else ''} | " + " | ".join(cells) + " |")
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("build")
    run = sub.add_parser("run")
    run.add_argument("--rounds", type=int, default=3)
    run.add_argument("--clients", default=",".join(CLIENTS))
    run.add_argument("--scenarios", default=",".join(SCENARIOS))
    run.add_argument("--big-gib", type=float, default=4)
    run.add_argument("--line-gib", type=float, default=0.5)
    run.add_argument("--many", type=int, default=MANY_COUNT)
    run.add_argument("--no-build", action="store_true")
    run.add_argument("--tag", default="")
    report = sub.add_parser("report")
    report.add_argument("--tag", default="")
    sub.add_parser("clean")
    args = parser.parse_args()
    commands = {
        "build": command_build,
        "run": command_run,
        "report": command_report,
        "clean": command_clean,
    }
    return commands[args.command](args) or 0


if __name__ == "__main__":
    sys.exit(main())
