# Measurements and verification of Gextto, gx-torrent and gx-core

The technical document of the numbers: how each measurement is taken, what it says
today, and how the engine and the interface are verified. Everything is measured on an
Intel N97 unless stated otherwise, and the purpose is always to **decide**: a change on
a data path is kept only if it is at least equivalent to what it replaces, and what a
measurement does not justify is closed with the data instead of implemented.

The state of the product and the open items are in
[`evoluzione.md`](evoluzione.md); the engine is in [`gx-torrent.md`](gx-torrent.md)
(inventory [`../internal/gxcore/GEXTTO.md`](../internal/gxcore/GEXTTO.md)) and its BEP
compliance in [`bep.md`](bep.md).

## Index

1. [Measurement harnesses](#1-measurement-harnesses)
2. [Benchmarks and footprint](#2-benchmarks-and-footprint)
3. [Swarm: seeding, choking, pipelining](#3-swarm-seeding-choking-pipelining)
4. [DHT](#4-dht)
5. [Merge on real data](#5-merge-on-real-data)
6. [Accessibility verification](#6-accessibility-verification)

---

## 1. Measurement harnesses

The measurements below are taken by hand, outside CI: they serve to decide with
numbers, and a change on a data path is kept only if it is **at least equivalent** to
what it replaces. Two rules apply everywhere: measure the system **as it really runs**
(queue and slots, seed policy, scheduler and auto-management on — never "with the
queues off", which would be a laboratory and not production), and compare two clients
only with the same swarm, the same role, the same policies and the same metrics.

**Cheap probes and harnesses.**

- `scripts/bench-engines.sh` — RAM and throughput probe of gx-core: swarm throughput on
  loopback (with peak RSS) and disk write throughput of a directory (`--dir`, point it
  at a mount NFS to compare):
  `scripts/bench-engines.sh [--size MiB] [--dir DIR] [--leechers N]`;
- `make measure-seeding` — local swarm, seeder plus 3 leechers by default, with
  distinct `127.0.0.x` addresses (the engine refuses two connections from the same IP).
  `GX_MEASURE_SUPERSEED=1` compares super-seeding, `GX_MEASURE_MESH=1` connects the
  leechers to each other, `GX_MEASURE_LEECHERS=N` sets the swarm size; the output is
  the seed upload as `Nx corpus` with averages and times. Absolute numbers depend on
  the machine and are not comparable between runs, and a peer may take two retry ticks
  (10 s) in super-seeding: the harness reports instead of failing;
- `make measure-choking` — the choking campaign: the controlled half serves a real
  **libtorrent** leecher on loopback (`scripts/libtorrent-leech.py`, needs
  `python3-libtorrent`), the real-swarm half seeds a public torrent and needs a
  reachable inbound port;
- `make measure-pipeline` — the **adaptive request queue**, local swarm at shallow
  (def=4/max=4), default (50/250) and deep (200/500) depths, reporting bytes and time;
  the knobs travel in `NetworkOptions` (`MaxRequestsOut`/`DefaultRequestsOut`). On
  loopback the RTT is ~0, so the depth is not the bottleneck and the profiles give
  nearly identical numbers (~200 ms, ~40 MB/s with one leecher): it is a
  regression/sanity check, not a WAN demonstration;
- `make measure-realswarm GX_MEASURE_REAL=<public.torrent>` — a public torrent with the
  DHT, the trackers and uTP enabled (`GX_MEASURE_REAL_SECONDS`, default 120 s),
  reporting peak peers, the uTP peer split and the uTP socket counters. It is the
  control-path check the local swarm cannot give: a run that sees no peer at all fails,
  so it cannot pass hollow;
- `make measure-merge3` — Swarm Merging Phase 3 on real ISOs (see §5);
- `make measure-dht` — the **public DHT**, routing-table fill and query success
  (`GX_MEASURE_DHT=5m`, `GX_MEASURE_DHT_INFOHASH=<40 hex>`), the basis of the anti-Sybil
  and multi-path numbers (§4);
- the **deterministic unchoker harness** (`internal/gxcore/internal/unchoker/sim_test.go`,
  run by `make test-engine`): it drives the real `Unchoker` with synthetic peers and
  fixes its properties (fast downloaders in download, fast uploaders in seed, immediate
  `FastUnchoke`, optimistic unchoke that does not steal a regular slot and rotates, the
  budget respected, fairness). A safety net for future variants of the algorithm.

**Isolation with latency.** `TestMeasurePipeliningLatency` puts an **in-process latency
proxy** in front of the seed (no `tc`/`netem`, no root): every chunk is released at
arrival plus latency, in order and **without limiting the bandwidth**, so the RTT is
added without throttling the link.

**Holepunching.** `scripts/holepunch-netns-test.sh` verifies end-to-end that gx-torrent
opens a hole (BEP 55, `ut_holepunch`) between two clients behind NAT/firewall with a
public peer as **relay** (engine behaviour in [`gx-torrent.md`](gx-torrent.md)). It is
root-only and runs entirely inside dedicated network namespaces, so the host network is
not touched: `gx-lab` (public: tracker, seed, relay `10.1.0.1:51410`) plus two stateful
firewalls `gx-r1`/`gx-r2` (outbound allowed, new inbound DROPped, `ESTABLISHED,RELATED`
accepted, TCP towards the peer ports refused so the hole opens over uTP) and the clients
`gx-a` (leecher) and `gx-b` (seed). A minimal Python tracker introduces them; the seed
completes and stays interested, then the leecher asks the relay for an introduction and
the two cross-dial over uTP. Success is a peer with origin `holepunch` (the dialling
side) or `incoming` (the other) in `/api/v1/torrents/<hash>/peers`. Choices worth
knowing: a **stateful firewall** instead of a cone NAT (conntrack would create a local
entry conflicting with the SNAT; the firewall reproduces the same observable and makes
the test deterministic), seed and relay on different IPs (the engine deduplicates peers
by IP), `-outgoing-interface` set to the listening IP so the announce carries the right
address, a long tracker interval so a re-announce is not mistaken for the holepunch, and
success detected on both sides because the dial is a race. Usage: `make gx-torrent`, then
`sudo scripts/holepunch-netns-test.sh` (`--keep` to debug, `--size 16` for a larger
torrent); debug with `HOLEPUNCH_DEADLINE` (default 90 s), `HOLEPUNCH_B_DEADLINE` (60 s)
and `HOLEPUNCH_DIAG=1` (conntrack dump). Limits: it emulates with a stateful firewall
rather than a real cone NAT, and which side reports `holepunch` or `incoming` depends on
who wins the race.

## 2. Benchmarks and footprint

**What is measured here:** the cost of the code — CPU, allocations, RSS — with
`go test -bench … -benchmem`, `pprof` CPU/heap profiles and the footprint of the daemon
and of `gx-torrent` (RSS, goroutines, allocations). The swarm behaviour (throughput,
seeding, choking, pipelining) is in §3. Intel N97; the figures a change replaced are
kept where they justify the design.

**Engine micro-benchmarks** (quiet machine / machine under load):

| Benchmark | Result | Note |
|---|---|---|
| `BenchmarkEngineLoggerFiltered` vs `Forwarded` | **151–175 vs 730–785 ns/op, 3 vs 6 allocs/op** | records below the handler threshold are discarded **before** being formatted, so at WARNING (normal daemon use) the `Debugf`/`Infof` calls on the hot paths allocate nothing; the handler still receives the same records (ERROR/WARNING, no swarm-noise count lost) |
| `BenchmarkBlocked` (IP filter lookup) | **18.6 / 37.7 ns/op, 0 alloc/op** | sorted merged intervals with binary search; the old lookup allocated a `map` and a slice on the path of every peer/tracker connection |
| `BenchmarkPickerRarestSteady` | **~105 / 143.5 µs/op, 0 allocs/op** | piece picker, steady state |
| `BenchmarkTickUnchokeSeeding` | **1.3 / 1.64 µs/op, 1 alloc/op** | unchoker tick |
| `BenchmarkPrefillFromUnrelatedV2` (16 candidates) | **1.49 / 1.58 µs/op, 128 B/op, 16 allocs/op** | one-shot cost of the Phase 2 merge scan (allocation, resume, later trigger), **no steady-state path** |
| `BenchmarkMergeCandidatesUnrelated` (32 torrents) | **~265 ns/op, 0 allocs/op** | the filter the resume and later-trigger paths pay when nothing is reusable (§5) |
| `BenchmarkNewConnID` (uTP) | **31 ns/op, 0 B/op, 0 allocs/op** | the connection id probes from a random value and stops at the first free one, O(1) with the connections a socket really has; it used to build a 64K-entry table per dial (512 KiB and 152,353 ns/op) |
| `BenchmarkTransferLoopback` (uTP, 16 KiB block) | **45.6 → 62.0 MB/s, 82,020 → 23,692 B/op (−71%), 173 → 116 allocs/op (−33%)** | four optimisations from the profile: the connection-id probe, lazy `Event` channels (the per-send `acked` flag no longer allocates one per packet), a reusable read window (`readPos` offset instead of reslicing) and a `netip.AddrPort` connection key instead of a `UDPAddr.String()` per inbound packet |
| `BenchmarkUTPConnRemoteAddr` | **259 → 113 ns/op, 152 → 40 B/op, 6 → 3 allocs/op** | the TCP-view addresses are converted once at wrap (`NewUTPConn`) instead of at every `RemoteAddr()` call; the rest is `String()` |

Matching 16 unrelated v2 candidates costs ~1.6 µs once per torrent (one small map per
candidate) and the picker/peer/piece-write paths are not touched, so the merge cannot
affect seeding or download throughput.

**Swarm health in the acquisition score is allocation-neutral** (2026-10-16, Intel N97,
`go test -bench 'BenchmarkReleaseScore|BenchmarkAcquisitionScore' -benchmem -count 6 .`).
The adjustment (`AcquisitionScore = ReleaseScore + healthAdjustment`) is a pure switch
plus a `math.Log10` per candidate: **959–1009 vs 973–1024 ns/op, 224 B/op and 1 alloc/op
for both** — the adjustment costs nothing measurable and adds no allocation (the
224 B/1 alloc come from the pre-existing `ReleaseScore`). It runs once per candidate in
the cycle, never on a transfer path, so it cannot affect seeding or download throughput.

**Auto-tune connection cap on a public swarm** (2026-10-16, Intel N97, `GX_MEASURE_AUTOTUNE=<Debian
netinst .torrent> GX_MEASURE_AUTOTUNE_SECONDS=40 go test ./cmd/gx-torrent/ -run
TestMeasureAutotune -v`). The guided tuning picks `torrent_max_connections_per_torrent`
between 50 and 400; this battery downloads the same public torrent with the cap at the
floor, at the ceiling and unlimited (one 40 s run each, DHT/uTP/PEX on, while the live
Gextto service was also running). Indicative numbers on a noisy public swarm:

| Cap | average | peak | peak peers | RSS delta | CPU |
|---|---|---|---|---|---|
| 50 (floor) | 8.4 MiB/s | 8.4 MiB/s | 25 | +43 MB | 8.4 s |
| 400 (ceiling) | 2.3 MiB/s | 4.0 MiB/s | 78 | +93 MB | 11.2 s |
| unlimited | 5.7 MiB/s | 6.4 MiB/s | 78 | +97 MB | 10.5 s |

All three moved about the same total (~330 MB in 40 s): **the cap is not the bottleneck,
the swarm is**, so the auto-tune bound does not cripple the transfer. The lower cap used
**less than half the memory** (fewer connections) with comparable throughput. The point is
the method: repeat the battery (`scripts`-style, the run is opt-in and outside CI) before
reading any single number as a trend; a public swarm varies run to run.

**Connection-cap sweep on a bigger public ISO** (2026-10-16, Ubuntu 24.04.3 desktop, ~6 GB,
`GX_MEASURE_AUTOTUNE_CAPS=20,50,100,200,400,800,0 GX_MEASURE_AUTOTUNE_SECONDS=60`), one 60 s
pass per cap:

| Cap | 20 | 50 | 100 | 200 | 400 | 800 | unlimited |
|---|---|---|---|---|---|---|---|
| avg MiB/s | 1.5 | 0.1 | 0.0 | 1.6 | 1.1 | 0.7 | 0.2 |
| peak peers | 7 | 20 | 1 | 34 | 30 | 19 | 33 |

The spread is **swarm luck, not the cap**: cap 100 got a single peer and zero bytes, cap 20
got seven peers and the best rate. A single short pass on a public swarm **cannot rank the
parameters** — which peers you happen to reach dominates everything. Conclusion for the
auto-tune: the 50–400 bound is **safe** (on Debian it never lost to unlimited), and a low cap
saves memory; but "the right value" cannot be read from a public swarm. To actually tune a
parameter, run **repeated, interleaved** passes and/or the **controlled local swarm**
(`make measure-seeding` / `measure-pipeline`), where the parameter is the only variable.

**Controlled cap sweep** (2026-10-16, Intel N97, `GX_MEASURE_AUTOTUNE_LOCAL=1
GX_MEASURE_AUTOTUNE_LOCAL_SEEDERS=8 GX_MEASURE_AUTOTUNE_LOCAL_SIZE_MB=64 go test
./cmd/gx-torrent/ -run TestMeasureAutotuneLocal`). A local swarm of eight seeders on
loopback, 64 MiB corpus, the leecher's per-torrent cap swept:

| Cap | 1 | 2 | 4 | 8 | unlimited |
|---|---|---|---|---|---|
| time | 622 ms | 613 ms | 643 ms | 714 ms | 663 ms |
| rate | 103 MiB/s | 104 MiB/s | 99 MiB/s | 90 MiB/s | 97 MiB/s |

Throughput is **flat** across the cap: on loopback one seed source already saturates the
path, so the cap only bounds how many connections are *allowed*, never the rate. This is the
parameter-isolated confirmation of the public result: the auto-tune bound (50–400) trades
connections (memory) for nothing in throughput. A per-connection memory curve needs a
per-peer RSS breakdown the shared-process harness cannot give; the public runs already show
a low cap using less than half the memory.

**RSS term matching** (2026-10-15, Intel N97, `go test -bench BenchmarkCriteriaMatch
-benchmem -count 5 ./internal/rss/`: one rule with two *one of* and two *none of* terms
over four typical release names). The terms are found with `rss.HasTerm` — separators of
release names normalized, from a word start — instead of a lowercase substring:
**250 → 142 ns/op, 46 → 0 B/op, 1 → 0 allocs/op**. A first version normalizing both
strings per term cost 1,590 ns/op and 8 allocs/op, and one normalizing the title once
into a buffer 395 ns/op with 1 allocation (the buffer escaped): the kept version scans
the title in place, jumping to the occurrences of the term's first letter with
`strings.IndexByte`. The rule preview (up to 5,000 archived items) and every feed poll
pay this per item.

**Statistics page** (2026-10-14, Intel N97, `go test -bench … -benchmem` + RSS/CPU
probe of the standalone daemon, 0 torrents). What the page adds to the tick is a
history sample and the folding of the session byte counters into the all-time totals,
both free at the sampling path: `BenchmarkRateHistoryAdd` **42 ns/op, 0 allocs/op** and
`BenchmarkLifetimeAccumulate` **6.4 ns/op, 0 allocs/op**. A graph request pays
`BenchmarkRateHistorySnapshot1h` **11.3 µs/op, 20 KiB, 1 alloc/op** (the output slice,
one per request) and the page translation `BenchmarkStatsPageTranslate`
**255 µs/op, 89 KiB, 1,919 allocs/op**. Over 30 s idle the daemon spent **89 → 91 CPU
ticks (+20 ms, +2 %)** and with the page open, polling every 3 s, the same ticks
(88–91 vs 89 baseline): within run-to-run noise. RSS after the first 35 s is
indistinguishable (~108–114 MB both, dominated by the engine's early growth), and the
Go-pure binary grew **24,075,951 → 24,128,331 bytes (+52 KiB)**: the page and the new
code. The three history rings (1 200 + 1 440 + 168 points × 16 bytes) are ~45 KiB of
fixed resident memory.

**uTP retransmission.** The RTO consumes the loss signal. A **minimum-RTT floor of
5 ms**: without it a fast path (loopback or LAN, RTT of tens of µs) collapses the RTO
to sub-ms and a briefly stalled peer becomes a resend storm — on the silent-peer
harness (1 s) 18,184 resends at a 1 µs floor against 2,919 at 5 ms (**6.2×**). A
**per-connection backoff** doubles on each RTO timeout (cap 4×) and halves on each
delivered packet, so a silent or congested peer is retried with growing patience and a
link that delivers again relaxes to the measured RTT — silent-peer A/B (3×4 s) 17,663
resends fixed against 8,853 adaptive (**50% fewer**). A healthy connection never
timeout-resends, so the backoff stays 1 and the swarm numbers are unchanged. Fresh
connections seed the RTO from the per-host winning-dial latency EMA instead of the fixed
400 ms, clamped to [5 ms, 400 ms], and the samples always win: a 5 ms seed gives an RTO
of [10, 20] ms against [800, 1600] ms by default.

**The engine rewrite and `own-deps` are allocation-neutral.** The 54 engine benchmarks
compared with `benchstat` (`-count=6`) show identical `B/op` and `allocs/op`
everywhere, `BenchmarkHasAnyNotIn` 3.77 → **3.15 ns/op** (−16%) and `BenchmarkBlocked`
20.9 → **18.8 ns/op** (−10%); the rest is equal (`BenchmarkPickerRarestSteady`
115.6 → 105.5 µs, `BenchmarkPickerRarestShuffled` 1.071 → 1.057 ms,
`BenchmarkStorageWrite`/`StorageWriteMmap` 45.7/35.7 → 46.2/35.7 µs,
`BenchmarkTickUnchokeSeeding` 1.266 µs, `BenchmarkRequestRelease` 1.762 → 1.761 µs,
`BenchmarkUpdateInterestNothingWanted` 2.787 → 2.794 µs,
`BenchmarkPrefillFromUnrelatedV2` 1.487 → 1.488 µs). The three benchmarks that a single
pass had shown +13…17% slower (picker file-edge scan, interest update, prefill) are
**run-to-run drift**: re-measured interleaved (`-count=20` each) they are statistically
equal (p ≥ 0.15). The `uTP` connection-id probe and the word-wise interest update
(`HasAnyOutside` + `skipBits`, 231 → **2.8 µs** per peer on 100k pieces, ×80, 0 alloc)
are the two places where the rewrite is measurably faster.

**The v1 cross-layout filter has a cost of its own.** Replacing a per-candidate map
with a nested scan had pushed `BenchmarkMergeCandidatesUnrelated` from ~98 to
**1360 ns/op**; the scan is now nested only while `len(a)*len(b) <= 1024` (lengths
compared before paths) and larger packs index the smaller torrent in a map:
**375 → 265 ns/op, still 0 allocs**. (Re-run 2026-10-14, Intel N97: ~286–351 ns/op,
0 allocs — same shape, the absolute varies with machine and load.) `BenchmarkV1FilesMatchNoMatch` (unrelated torrents
with the same file length): 8 files 290 → 278 ns, 64 files 18.2 → 3.4 µs, 2000 files
17.3 ms → **0.13 ms** (109 KB, 9 allocs).

**The disk is far from being the bottleneck.** `BenchmarkStorageWrite` gives
**~5.8 GB/s** buffered and **~7.4 GB/s** mmap (ranged mmap sync ~29 GB/s through the
page cache), far above any network speed, which is why `O_DIRECT` and `io_uring` are
not justified.

**Footprint.** The IP filter stores the IPv4 rules, like the IPv6 ones, as **sorted and
merged intervals** with binary search; the pointer-based segment tree
(`internal/blocklist/stree`) is gone. With the Aetherinox blocklist (34 MB,
**666,763 rules** → 343,024 intervals) the live heap is **5.4 MB** (was 190 MB) and
`gx-torrent` RSS **~105 MB** (RssAnon 92 MB) against 295 MB, with the same number of
rules loaded; the 34 MB raw file stays in the daemon's cache (rare re-reading on manual
reload) and is removable later. The SQLite `cache_size` is SQLite's default **2 MB**
(`sqlite.go`): on the real archive DB (510 MB, FTS5; `Archive.Browse` and
`CountMatches`, tried up to 16 MB of cache) the latency is **flat**, within the noise
even on the broad `MATCH` (~1.14 s), because it is the OS page cache that serves the
file — `gexttod` RSS **67.6 MB** (RssAnon 46.2 MB) against 109.7 MB at the same uptime.
At ~29 min uptime, real 34 MB blocklist and 4 active torrents: `gexttod` RSS 113 MB
(RssAnon 89 MB), `gx-torrent` RSS 165 MB (RssAnon 150 MB) — workload-dependent
(peers and active pieces), within the range of the other rounds, with no new
steady-state allocation.

**Crash-safe adds and engine-aware settings (2026-10-16, Intel N97, 4 cores).** The
add intent written before every engine add and cleared after it (`torrent_add_intents`)
costs **76–79 µs, 3.2 kB, 44 allocs** per started download (WAL SQLite, benchmark of
`BeginTorrentAdd`+`EndTorrentAdd`, 3 runs): negligible next to the engine add itself;
the recovery is one indexed query per cycle start. The per-torrent default peer cap
(`max_connections_per_torrent`) is one engine command per torrent, only at add, session
reload or when the value changes. Live service 12 minutes after the restart (2 torrents):
`gexttod` RSS **70.7 MB**, ~1 % CPU including start-up (before: 83.4 MB, 0.8 %, at 81 min
uptime); `gx-torrent` RSS **104.6 MB**, 0.3 % CPU (before: 115.7 MB, 0.7 %, at 106 min) —
no regression.

**Live CPU and memory, profiled during a download burst (2026-10-17, Intel N97, 4 cores).**
The TUI's *Sistema* row reads the whole-machine CPU (`cpu_percent`, `/proc/stat`) and the
*Daemon* row `gexttod`'s own (`process_cpu_percent`), so a 100 % reading is the machine:
while Jellyfin analysed two freshly archived episodes (`jellyfin-ffmpeg`, two `blackframe`
jobs at 100–216 % each, nice 10) the host sat at 100 % with `gexttod` at ~2 %.
`gx-torrent` (build 220, opt-in `GX_TORRENT_PPROF`) is load-proportional. A 40 s CPU
profile taken by a burst watcher at **3.9 MB/s, 44 peers** shows **8.98 s of samples =
22.5 % of one core** (~57 ns/byte, ~17 MB/s per core): **29 % syscalls** (peer reads,
disk), **31.5 % cumulative in `peerreader.readPiece`** — of which **MSE decrypt
(`mse.Conn.Read` → `cipher.StreamReader`) 23.4 %, RC4 flat 10.1 %** — **SHA-1 4.7 %**
(piece hashing), the rest Go runtime (futex 8.5 %, `selectgo`, `memclr`/`memmove`); no
hot loop. Heap in use during the burst **159 MB**: **112 MB (70 %) in the piece buffer
pool** (`bufferpool`, one buffer per piece length, reused through `sync.Pool`) and 33 MB
the IP-filter file; RSS 368 MB, 463 goroutines (~10 per peer connection). After the burst
the heap in use settles at **180 MB** (71 % the **read cache**, `cachedpiece.ReadAt`, plus
the 33 MB IP filter) while RSS stays **~920 MB**: Go keeps the freed arenas mapped
(high-water, not a leak). Idle CPU ~0.4 %; right after a restart RSS is **90–110 MB** and
flat. Allocations since start **9.4 GB**, dominated by `utp.(*Socket).reader` (2.9 GB) and
the piece pool (2.6 GB) — candidates for a future allocation reduction, not a current
defect. `gexttod` stayed at ~2 % CPU / ~68 MB throughout.

**Peer reader under a saturated line (2026-10-17, Intel N97, 4 cores).** Load test: 9–10
recent releases re-added, *Auto speed* (no caps), a collector sampling `/proc` every 5 s
and a 45 s pprof CPU profile at ≥ 8 MB/s. Means over the samples above 4 MB/s:

| gx-torrent | CPU mean (max) | rate | peers | CPU per MB/s | CPU per peer | RssAnon max |
|---|---|---|---|---|---|---|
| build 220 | 168 % (209 %) | 8.25 MB/s | 98 | 20.4 | 1.71 | 922 MB |
| build 221–222 (deadline throttled, limiter atomics) | 198–221 % (256 %) | 7.9–8.1 MB/s | 135–143 | 24.5–27.9 | 1.39–1.64 | 1266–1555 MB |
| **build 223** (progress-based deadline) | **46 % (136 %)** | **8.12 MB/s** | 116 (max 159) | **5.7** | **0.40** | 1858 MB |
| **build 224** (uTP: in-place receive, no reflect.Select) | **45 % (80 %)** | **8.24 MB/s** | 131 (max 169) | **5.5** | **0.35** | 1718 MB |
| **build 225** (IP filter rules cached parsed, uTP buffers released) | 44 % (99 %) | 6.80 MB/s | 125 (max 198) | 6.5 | **0.35** | 1844 MB |
| **build 226** (GOGC=50) | 48 % (76 %) | **8.98 MB/s** | 111 (max 180) | **5.3** | 0.43 | **1105 MB** |

The profiles of builds 220–222 showed 40–47 % of the CPU in the uTP read path:
`SetReadDeadline` (220), then `sync.RWMutex.Lock` in `utp Conn.Read` plus `time.Now`
(222). The cause was a busy loop, not lock pressure. `Conn.Read` on a destroyed uTP
connection returns the ack-timeout error, which reports `Timeout()`. `readPiece` took that
error as a slow peer and looped, taking the transport-wide lock on every pass. The build
223 profile (14.8 s of samples in 45 s = **33 % of one core at 8 MB/s**) has no lock, no
`time.Now` and no deadline frame left: syscalls 30 %, futex/scheduler, RC4 4.5 % flat,
SHA-1 1.6 %. The earlier "RC4 29–51 %" readings were cumulative times that included the
uTP read underneath: RC4 flat was 0.7 % in build 222. The build 223 run (15 min, 173
samples above 4 MB/s) had a peer count close to builds 221–222: −72 % CPU per MB/s and
−77 % per peer against build 220. Build 224 (15 min, 179 samples) removes the per-datagram copy in the
uTP socket reader (43 % of the allocated bytes) and the `reflect.Select` in every blocking
uTP Read/Write (3.3 % of the CPU in build 223): −12 % CPU per peer, peak 80 % instead of
136 %. `BenchmarkTransferLoopback`: 23.6 KB / 118 allocs → 5.6 KB / 99 allocs per op,
116–127 → 131–139 MB/s. What remains is syscalls (~32 %, one `recvfrom`/`sendto` per uTP
datagram and one ACK per data packet through the 500 µs state timer), scheduler wake-ups
(futex ~14 %), RC4 ~5 % and SHA-1 ~2 %. The RssAnon peak (1.7–1.9 GB with 10 torrents)
is the piece buffer pool and the read cache, not the reader.

**Memory under the same load (build 225).** The RssAnon peak follows the write cache: the
auto policy gives 1.26 GB to piece buffers when downloads go to NFS (`cache_storage:
network`) on this 16 GB host, and the read cache is 128 MB. So the comparable figure is the
memory outside the two caches (RssAnon − write − read, mean of the second half of each run):
**1032 MB → 859 MB**. In the heap profile at the end of the run, uTP receive buffers
(`appendReadBuf` + queued payloads) drop from **376 MB to 66 MB** and the IP filter from 33 MB
(raw file kept for session reopens) plus 5 MB to **5 MB** (the parsed rules are cached
instead); total live heap **1342 MB → 925 MB**, of which 694 MB is the piece pool. The uTP
figure was retention, not data in flight: the readers were waiting for data, while consumed
inbound slots and acked sends stayed reachable through their slices' backing arrays and
drained read buffers kept their burst-sized capacity. CPU per peer is unchanged (0.35); the
lower mean rate of this run (6.8 MB/s) was the swarm. The garbage collector costs
`mallocgc` 1.9 % of the profile, with `gcBgMarkWorker` not among the top 300 frames.

**GOGC=50 (build 226).** Since the collector was that cheap, the daemon now runs at
`GOGC=50` unless the environment sets `GOGC`. Same load (all 10 releases restarted from
zero, 15 min): RssAnon peak **1844 → 1105 MB**, second-half mean **1566 → 953 MB**, memory
outside the caches **859 → 507 MB**; live heap at the end 1.68 GB → 0.88 GB (`NextGC`
1.93 → 1.22 GB, 114 collections in ~15 min). The collector's cost stays negligible:
`gcBgMarkWorker` 0.5 % and `mallocgc` 2.2 % of the profile; CPU per MB/s 5.3, the best of
the series. Against build 224 the memory outside the caches is halved (1032 → 507 MB). After a clean restart of
build 226 (3 seeds and 1 download loaded, full IP filter) `gx-torrent` holds **60–64 MB** RSS
(44–48 MB anonymous) at 1–2 % CPU, against 90–110 MB right after a restart of build 220.

**Robustness at no performance cost.** Torznab answers are decoded with `Strict=false`
plus the HTML entity table (a raw `&` or `&egrave;` in a scraped title no longer makes
the whole answer unreadable), and the cost is nil: on a 100-item answer, strict
**2.24–2.32 ms/op, 642.8 kB/op, 17179 allocs/op**, lenient **2.22–2.29 ms/op,
642.8 kB/op, 17179 allocs/op**. Prowlarr's aggregate search is JSON (one
`json.Unmarshal` into a slice of small structs), outside the XML path. The same holds
for the other robustness work: the uTP header refuses inputs shorter than 20 bytes
instead of panicking, the bencode screen bounds lengths and depth before the decoder,
and the mmap fault guard turns a fault into an I/O error with an **equivalent** write
bench (36.3–38.0 → 35.9–37.5 µs, 0 alloc).

**Other measurements.** The feed archive (`feed_archive.go`, bbolt with a compact
index): `BenchmarkFeedArchiveSearch` on 100k elements **362 ms → 7.4 ms** (49×) with
2,800× less allocation. HTML listings used as feeds (`internal/rss/listing.go`, ext.to
and TorrentGalaxy resolved to direct magnets and feed links instead of failing on
invalid XML): **50 elements in 0.3 s / 1.9 s** on the two real URLs. The auto-speed
probe is ~1 connection/s and does not touch the data path; with auto-speed off nothing
changes, so there is no regression. Still to measure: **graduated file priority** with
mixed-priority files (the open item is in [`evoluzione.md`](evoluzione.md) §8).

**Web UI refresh (gx-torrent page).** The 2 s table refresh shipped the rows as
server-rendered HTML (`/ui/live`); with 24 columns the row markup costs about
**10 KB per torrent**, so a 300-row window moves ~3 MB per tick plus a full
table re-parse. The rows now travel as JSON (`GET /ui/rows`, same
`uiPageData`, `?rows=` window unchanged) and the browser renders them with a
client template verified cell-by-cell against the server one (node harness:
every `c-*` cell identical on the live session); filters/speeds/status still
travel as translated HTML (`/ui/live?notable=1`). Measured on the live service
(Intel N97, 2026-10-15, 2 torrents): `/ui/live` **26,701 B** per tick →
`/ui/rows` **3,432 B** + notable **6,399 B** = **9,831 B** (−63%); per row
~10 KB HTML vs ~1.7 KB JSON (~6×). Empty session and limited windows still use
the server HTML (translated empty states, "show more"). Correctness is pinned
by `TestUIRowsJSON` and the i18n guards (state badges and labels go through
`t()` with client keys).

**Optimisations in place.** Items ordered by **expected value = (impact × certainty) /
(effort × risk)**, one item = one commit:

| Intervention | Effect |
|---|---|
| Remove `O_SYNC`, batch durability | 16 KiB **35 → 81 MiB/s** on NFS (2.3×), 64 KiB 1.5×, 256 KiB 1.6×, 1 MiB 1.2×, up to ~118× at small blocks on NVMe; the invariant is that a persisted bit refers to data already fsynced (`syncPieces()` on the run loop, `writeBitfield()` ordered snapshot → fsync → write, `updateStats` in two phases with no Bolt transaction open during the fsyncs) |
| mmap opt-in (`-mmap`, Linux) | **~7.0 GB/s** on tmpfs against **~5.9 GB/s** for `pwrite`; a **ranged flush** takes a clean mapping from 477 µs to **13 ns**, with one dirtied piece unchanged (~1%) |
| LSD without `d.mu`/`t.Stats()` (lock-free snapshot) | no lock on the local-service-discovery path |
| Streaming: targeted `Torrent.PiecesDone(begin,end)` | the streaming reader does not walk the whole bitfield |
| MSE `StreamWriter` in place | 0 alloc per block |
| `servedRequests` bounded window (~1024) | bounded memory in the tracker path |
| Piece cache with lazy TTL | no sharding needed (profiling showed no contention) |
| `Bitfield.Count` with `math/bits` | one word per iteration |
| Optionals: `findLocked` O(1) | constant-time lookup |
| Write ring buffer (4 pieces/torrent) | the network keeps flowing while pieces are written; the prerequisite for multi-piece per peer (§3) |
| Disk guard, mmap fault guard, word-wise interest, bounded bencode screen | see above and [`evoluzione.md`](evoluzione.md) §6 |

Follow-ups **not added**, each without a measurement to justify it: io_uring/AIO (Go
does not expose it without a dependency or cgo, and the mmap path is already
asynchronous), explicit contiguous-write coalescing (the page cache already merges
adjacent writebacks), rarity buckets and a picker refactor, a watermark cursor in
`pickSequential`/`pickFileEdge`, a `PieceDownloader` bitmask, `clear(b.Data)` in
`bufferpool`, `FADV_RANDOM`, `saveLocked` outside `d.mu`, `/proc/meminfo` under `d.mu`
and `HandleDisconnect` O(N). **Postponed**: qBittorrent's
`renameFile`/`renameFolder` (it needs rename support in the engine: a feature of its
own).

**Real line, uncapped** (2026-10-16, the live Gextto install, 15 torrents, ~300 peers).
Removing the effective cap (a global limit *and* a "temporary keep" limit — the temporary
one wins, so both had to go) with the peak observer running: download reaches **6–7.7 MB/s**
(peak **7729 KiB/s**; the recorded `autotune_peak_down_kib` = 7644), while the earlier 3000
KiB/s cap had held it at ~3 MB/s. Upload is 97–245 KiB/s, but that is **demand-limited**, not
the line: with one or two seeds and the rest downloading, few peers request our data, so the
achieved upload is a lower bound; the line's upload capacity shows while seeding popular
content. Effect on the tuning: a download proposal from the peak is solid (~80% of 7644),
while an upload proposal is conservative because the sample is demand-bound.

**A capped rate is not the line.** The peak observer must only learn from an uncapped
direction: with a 3000 KiB/s global the "peak" was 2991 (the cap itself) and proposing 80%
of it would ratchet the limit down every cycle. The code now skips (and forgets the peak of)
any direction that has a limit in force. Note the effective limit can also come from the
*temporary* limit (`torrent_temp_*`, kept until removed), which overrides the global.

## 3. Swarm: seeding, choking, pipelining

**Metrics.** Choking (upload slots): who is unchoked and for how long, fairness at
equal speed, optimistic unchoke rotation, behaviour while downloading (rewarding fast
downloaders) and while seeding (rewarding fast uploaders). Seeding end-to-end: bytes
uploaded by the seed against the corpus (`Nx corpus`), throughput up and down,
completion time, ratio and time to 1:1 in a real campaign.

**Deterministic unchoker bench.** All the properties hold on the real `Unchoker` driven
by synthetic peers, and the tick with 50 peers costs ~1.1 µs, 896 B/op, 1 alloc:
**negligible**. Hardening applied along the way: an interested but stalled peer (zero
speed) receives the next piece at the tick instead of waiting for `not-interested`
alone.

**Local swarm, first round (Intel N97, corpus 2 MiB).** Seeding without mesh (seed → 3
leechers, 5 repetitions): normal ~41–42 ms with the seed at **3.00× corpus**;
super-seeding ~51–52 ms (**~24% slower**) with the same 3.00×. Mesh with 2 peers
(indicative): both **1.00×**, the leechers exchange the pieces among themselves; mesh
with 3+ peers on one host does not converge. A later run measured the reciprocation
more precisely: with mesh the leechers upload **2.52 MB among themselves** and the
seed's upload drops from 3.00× to **1.84× (−39%)**.

**Honest reading.** The engine's choking is **correct and negligible as a cost**. On
small or fast swarms super-seeding **does not reduce** the upload (the exchange is
already enough) and costs throughput and variability: the BEP 16 advantage is for the
*initial seeding* of large torrents with many slow peers, which one host cannot
reproduce. Consequence: **super-seeding stays opt-in, not default**, and the scaling
question goes to a real campaign.

**Pipelining, isolated with latency.** With RTT 50 ms, 256 KiB pieces (16 blocks) and a
4 MiB corpus, one leecher:

| Profile | blocks in flight | time | average |
| --- | --- | --- | --- |
| `shallow` def=1/max=1 | 1 | 13.4 s | 305 KiB/s |
| `default` def=50/max=250 | ≤50 | 1.1 s | **3.8 MiB/s** |

~12× with the adaptive queue (a second run reproduced it exactly: 300 KiB/s in 13.7 s
against 3850 KiB/s in 1.06 s, ~12.8×). The ceiling is readable from the numbers: with
256 KiB pieces a queue of ≤250 is bounded by the piece's block count, so the
`default` run was already at it — the next lever for high-bandwidth-delay links was
**multi-piece per peer**. A peer now downloads up to two pieces at once, sharing a
single in-flight budget (the extra piece opens only when the window has room) and never
receiving the same piece twice; re-running the same harness:

| Profile | single piece | multi-piece |
| --- | --- | --- |
| `shallow` def=1/max=1 | 301 KiB/s | 301 KiB/s (window of 1 respected) |
| `default` def=50/max=250 | 3853 KiB/s | **6756 KiB/s** (×1.75) |

So multi-piece raises the throughput at RTT 50 ms where the single-piece block count was
the limit, while the `shallow` profile keeps its window of 1, and RAM and CPU stay
equivalent. The **bounded write ring buffer** (4 pieces per torrent) is what keeps the
disk from becoming the bottleneck while more pieces are in flight: on loopback the
seeding harness is **equivalent** (the disk is not the bottleneck there) and the gain is
on slow storage.

**End-to-end on loopback.** `make measure-seeding`, 2 MiB and 3 leechers: **3.00×
corpus**, all done in 11 ms. `make measure-pipeline` in a local swarm, 8 MiB × 2:
16 MiB in 101 ms (**~162 MiB/s**); with RTT 50 ms, default depth 0.608 s /
**6736 KiB/s** and shallow 13.55 s / 302 KiB/s. The harness runs the seeder and every
leecher in one process and the engine's outgoing interface is process-wide: each
leecher creates its connection before the next daemon replaces the setting, so the
distinct source addresses are kept and the swarm reaches the intended 3.00×.

**Campaign on a real swarm, first run (2026-10-12).** The same well-seeded public
torrent (Ubuntu 24.04.3 desktop amd64, ~6.0 GB), the same machine, the same 40 s window
after a 5 s ramp-up post-metadata, three public UDP trackers in the magnet, sequential
runs (never in parallel), two runs each of **libtorrent 2.0.11** (`python3-libtorrent`)
and **gx-core** (`gx-torrent -mode standalone`):

| Client | run | metadata | peers (window) | downloaded | average |
| --- | --- | --- | --- | --- | --- |
| libtorrent 2.0.11 | 1 | 3 s | 83 (81 seed) | 292.9 MB | **7,149 KiB/s** |
| libtorrent 2.0.11 | 2 | 3 s | 66 (61 seed) | 258.8 MB | **6,317 KiB/s** |
| gx-core | 1 | 3 s | 80 (77 seed) | 385.1 MB | **9,401 KiB/s** |
| gx-core | 2 | 3 s | 81 (79 seed) | 381.2 MB | **9,305 KiB/s** |

**Honest reading:** on this swarm gx-core is **on par with or above** libtorrent
(median ~9.3 against ~6.7 MiB/s), with immediate metadata and a comparable number of
peers. It is **not** an isolated demonstration of choking or pipelining (the choice of
peers differs, one machine, two runs): it is the proof that gx-core really downloads
from a real swarm and that neither is a bottleneck against libtorrent at this load. The
isolation of the two algorithms is above (the in-process latency proxy for pipelining,
the deterministic bench for choking); `tc netem` is not needed.

**Downloads and uTP on real swarms** (`make measure-realswarm`, one run each): Big Buck
Bunny 276 MB in 90 s (75 peers, 23 uTP, 0 SYN drops), Debian netinst 793 MB in 180 s (80
peers, 18 uTP, 0 SYN drops), Ubuntu 24.04 1.08 GB in 180 s (85 peers, 31 uTP, 0 SYN
drops). Real uTP peers connect, the downloads complete and `utp_syn_drops` stays 0 (the
50-SYN backlog bound never binds); the resends show loss on a noisy swarm without a
storm — Big Buck Bunny dials 342, accepts 57, RTO resends 2563, fast 557; Debian dials
277, accepts 14, RTO resends 932, fast 41.

**Validation on a live torrent (2026-10-14).** `make measure-realswarm` on the Debian
13.7 netinst torrent, 2 minutes: the download **completes to 100%**, peers peak **80**
(uTP peak 26), connections out 53 uTP / 118 TCP and 6 in, uTP socket **dials 265 (91
errors)**, accepts 6 (0 errors), **RTO resends 1211, fast 0, SYN drops 0**. The engine
finishes a real swarm, and a healthy path never timeout-resends; the dial error rate
(~34%) is the expected cost of racing uTP against TCP-only peers and is worth a closer
look in a later round, not a regression.

**Longer real-swarm run while investigating a reported slowdown (2026-10-14).** To rule
out a degradation of the engine over time (the user reports downloads that "start fast
then slow down" until the gx-torrent daemon is restarted), `make measure-realswarm` was
run on the same Debian 13.7 netinst torrent for **5 minutes**: the download **completes
to 100%**, `downloaded=792,723,456 B`; peers peak **80** (uTP peak **24**); connections
out **49 uTP / 108 TCP** and 9 uTP in; uTP socket **dials 270 (107 errors)**, accepts 9
(0 errors), **RTO resends 831, fast 69, SYN drops 0**. A longer window changes nothing:
the swarm is reached, the ISO is fetched and the retransmission counters stay far from a
storm. The slowdown on the live instance is therefore not an engine-wide defect; the
torrent that triggered it (`Silo.S02E09`) had a **scarce swarm** (trackers reported 0–1
seeders) and the daemon was reaching a single 100% peer, so the rate was bounded by that
peer, not by gx-core.

**Choking campaign, controlled against libtorrent.** Our seeder serves a real
libtorrent leecher on loopback: **48 MiB uploaded (100% of the file) in 4.84 s**
(~10.4 MB/s) with the leecher verifying complete. The real-swarm half (seeding the
Debian ISO for 3 min) found **0 peers**, because seeding needs an **inbound** port and
this host is behind a NAT with no reachable port (the download run finds 80 peers
outbound); the harness is kept for a host with a reachable port.

**Decision: no choking change.** Reciprocation works locally and an external libtorrent
client is served well; the only missing datum — a real swarm where peers download
*from* us — is blocked by the test host's NAT, not by the algorithm. The campaign stays
as the gate for any future change.

**DHT cold start.** The peer-search retry while a torrent looks for peers uses a **15 s**
interval (`DHTMinAnnounceInterval`); the Ubuntu torrent over DHT only resolves its
metadata in ~49 s with 12 peers, and the remaining time is the routing-table fill
(~30 s cold), not the lookup.

## 4. DHT

**Peer discovery over the public DHT works** (2026-10-16, Intel N97, `cd internal/dht &&
GX_MEASURE_DHT=2m GX_MEASURE_DHT_INFOHASH=8ce412454ecff20763b805fe68e608cb2fe931bb
go test -run TestMeasureDHTRealSwarm -v`, the Ubuntu 24.04 torrent). A fresh DHT node
bootstrapped from the built-in routers filled the routing table to **1290 nodes in ~80 s**,
ran **1432 `get_peers` queries with 314 replies (21.9%)** and, for the Ubuntu infohash,
returned **`peers_found=2`, `peers_confirmed_multi=1`**: the lookup path returns real peers,
not just nodes. `dropped=411` (unresponsive/parameter-blocked nodes), `reflected_observations=189`,
`nat_mapping=1`, `nodes_insecure_id=76` (the anti-Sybil counter, no subnet concentration:
`subnet_share_max=4`). Conclusion: the discovery lever the auto-tune cannot move is already
in place — DHT finds peers, trackers have their own health and PEX/LSD are wired — so there
is no low-risk peer-discovery gap to close; more speed means more swarm, not more knobs.

The gx-core DHT (`internal/dht`, a module of its own) covers BEP 5/32/33/42/43/44/46
with a routing table as a bucket-less binary tree; what it still needs is **hardening
and diagnostics**, not more BEP coverage (see [`bep.md`](bep.md)).

**What it has.** `Stats()` exposes per-RPC and screening counters, the reflected
address, the NAT type, `subnet_share_max`, `nodes_insecure_id`, `subnet_cap_hint` and
`peers_confirmed_multi`, and it is the measurement basis of everything below. The
`announce_peer` token is bound to source address **and** scope (the info-hash), so a
token from one swarm is not reused on another. The **anti-Sybil cap per subnet**
(`Config.MaxNodesPerSubnet`, default 0 = off) is implemented with an O(1) counter:
nodes beyond the threshold stay in the address book with `noTree`, and
`subnet_cap_hint` advises a value. Out of scope: DHT over I2P and the BEP 44 `put` path (the iterative `get` client already
serves the BEP 46 feed resolver behind `GET /api/v1/dht-feed`).

**Measurements.** `make measure-dht` on the public DHT (opt-in, outside CI):

- ~740 known nodes, `subnet_share_max` 14, `nodes_insecure_id` ≈42%, and the discovery
  of **108 peers in ~30 s** on the Ubuntu 24.04 torrent;
- a 3 minute run (2026-10-13): nodes settled at ~254 from a peak of ~536 (stale nodes
  cleaned), `subnet_share_max` **8** — below the hint threshold of 16, so no cap is
  suggested — `nodes_insecure_id` ≈40%, `capped` 0, `queries_out` 2899 / `replies_in`
  834 (**≈29% reply success**), `dropped` 1290 (rate limit), `reflected_observations`
  619 (the sending side of the BEP 42 `ip` field works) and `peers_found` 0 (a random
  info-hash);
- a 5 minute run on a real info-hash (Debian, 2026-10-14): nodes ~296 (peak 525),
  `subnet_share_max` 4–7, `nodes_insecure_id` ≈42%, `nodes_capped` 0, `queries_out`
  4703 / `replies_in` 1670 (**35.5%**), `reflected_observations` 1063, `peers_found` 0
  and **`peers_confirmed_multi` 0**;
- a further run on the public network: **4262 nodes**, 21.9% query success, 875
  reflected pings, 2 peers found from a cold start.

**Read.** The routing table is **not concentrated** and no node exceeds any candidate
threshold (8/16/24), so enforcing the anti-Sybil cap would be a no-op while CGNAT and
mobile legitimately share subnets: **the cap stays a hint, off by default**. The ~65–71%
of unanswered queries are nodes that do not reply, not proof of poisoning.

**Multi-path `get_peers` — closed with data.** The idea is to confirm peers across
independent paths and prefer those seen from ≥2 (an explicit per-lookup state machine
like the BEP 44 one, with a bounded fan-out and a deadline). The instrumentation exists
and is read-only: `notePeerReport`/`expirePeerConfirm` (`internal/dht/confirm.go`) track
which responding nodes reported each (info-hash, peer), bounded to `peerConfirmMax`
entries and expiring after `peerConfirmWindow`, and **reporters are counted per subnet**
(the same /24 or /48 unit as the cap), so one host on many ports or a Sybil block cannot
confirm its own peers and inflate the datum. In both real measurements
`peers_confirmed_multi` was **0**, so **no peer was confirmed by two distinct subnets**
and there is no evidence justifying the cost of the rewrite: the item is closed with
data. A controlled adversarial swarm would be the only next step, and it is out of
scope for a home client.

## 5. Merge on real data

**Phase 3, block-granularity reuse.** Phase 2 reuses a destination piece only when
**every** source piece overlapping it is verified; Phase 3 reuses the rest at **16 KiB
block** granularity: the verified spans of a partially covered destination piece are
written to disk (`storePartialBlocks`) and the blocks they cover are recorded per piece
(`torrent.partialBlocks`); when the piece is picked, the downloader reads those blocks
back (`prefillBlocks` → `PieceDownloader.Prefill`) and requests only the others, with
the assembled piece verified whole as usual. It is safe because BEP 52 leaves are the
16 KiB blocks of the **file**, not of the piece: two torrents with the same
`pieces root` share the whole Merkle tree below the piece layer, whatever their piece
length, so a span copied from a verified source piece is the file's true content. The
final whole-piece check still runs; if it fails on a piece with local blocks, the blocks
are dropped, the peer is not banned (the mismatch cannot be blamed on it) and the piece
is downloaded whole.

**Test environment (real data, public torrents).** `scripts/phase3-env.sh` downloads the
Debian netinst and Arch Linux ISOs over BitTorrent (aria2c) and builds, with
python3-libtorrent, two **v2-only** torrents of each — source with 256 KiB pieces,
destination with 1 MiB pieces, same file so same root — corrupting one source piece in
four, so every destination piece straddles verified and unverified source pieces.
`TestMeasureMergePhase3` (`GX_MEASURE_MERGE3=<dir>`, opt-in) runs a gx-torrent seeder of
the destination and a leecher holding the partial source, adds the destination to the
leecher and measures the bytes taken from the network; `GX_MEASURE_MERGE3_PEER` with
`scripts/phase3-seed.py` uses a **libtorrent** seeder instead, for interoperability.

| Run (loopback) | before (Phase 2) | after (Phase 3) |
| --- | --- | --- |
| Debian 756 MiB: from the network | 792,723,456 B (100%) | **198,180,864 B (25.0%)** |
| Debian: total time | 2.81 s | **1.31 s** |
| Arch 1.5 GiB: from the network | 1,640,497,152 B (100%) | **410,255,360 B (25.0%)** |
| Arch: total time | 5.82 s | **2.71 s** |
| Arch: CPU (whole process, seeder included) | 17.7 s | **10.0 s** |
| Arch: peak RSS | 109 MiB | 111 MiB |
| Debian with a libtorrent 2.0.11 seeder | — | 25.0% from the network, complete |

All 756 (Debian) and 1565 (Arch) destination pieces were reused at block level and every
one passed its hash.

**On a resume (2026-10-13).** A destination merged with no peer, then paused and reloaded
by a restarted leecher session, downloads after the restart:

| Run (loopback) | before (no resume merge) | after (resume merge) |
| --- | --- | --- |
| Debian 756 MiB: from the network after the restart | 792,723,456 B (100%) | **198,180,864 B (25.0%)** |
| `merged_bytes` re-derived | 0 | 594,542,592 B |
| partial (block-level) pieces re-derived | 0 | 756 (every destination piece) |
| total time | 2.61 s | 1.00 s |

Exactly the no-restart figure: the reloaded bitfield is kept (no piece re-read or
re-hashed, via `Verifier.Skip`/`Only`) and the block-level reuse is re-derived, so a
restart no longer re-downloads the file. The added cost when nothing is reusable is only
the candidate filter: **115 ns and 0 allocations** over 32 unrelated session torrents.

**When the sibling arrives later (2026-10-13).** The destination is added first, with no
sibling and no peer, and the source is added afterwards:

| Run (loopback) | before (no trigger) | after (later-sibling trigger) |
| --- | --- | --- |
| later merge observed | no | yes |
| `merged_bytes` | 0 | 594,542,592 B |
| partial (block-level) pieces | 0 | 756 (every destination piece) |
| Debian 756 MiB: from the network | 792,723,456 B (100%) | **198,180,864 B (25.0%)** |
| total time | 2.61 s | 0.91 s |

The same 25.0% as the first run, the resume path and the no-restart merge: the order in
which the torrents are added no longer decides whether the reuse happens.

## 6. Accessibility verification

**Method**: static review (HTML/CSS/JS), semantics/names/ARIA/focus,
responsive layout/visible focus/reduced-motion, light/dark theme contrast, axe-core
via Playwright, regression tests (dialogs, keyboard sort, polling, narrow
viewports). Suite: **14 green tests** on dashboard, downloads, settings,
maintenance, health, logs (`cd uiweb/end2end && npm ci && npm run test:a11y`;
a safety net, not a substitute for the manual assessment).

**gx-torrent pages** (2026-10-15, Chromium via Playwright + `@axe-core/playwright`, tags
`wcag2a`/`wcag2aa`, light and dark theme, a standalone daemon with sample torrents,
feeds, rules and wanted titles): the torrent list, its context and column menus, the
detail panel (general, files, peers), *Connection details*, *Global speed limits*, the
RSS rules and feeds with their editors, the wanted page with its editor and the
*Releases* dialog — **0 violations**. The first run found and led to fixing: column and
panel resizers without a value (`aria-valuenow`), zero-count filters dimmed below the
contrast minimum, `aria-selected` rows outside a grid (the table is now
`role="grid"` with `aria-multiselectable`), `aria-expanded` set on a row by the context
menu, preview lists that scroll but could not take the focus, and the progress
percentage drawn over the bar (now beside it, readable on any bar colour).

**Residual validation** (before any formal declaration): primary
workflows with Tab/Shift+Tab/Enter/Space/arrows/Escape; at least one desktop
screen reader (NVDA+Firefox or VoiceOver+Safari) and TalkBack where applicable; zoom
200%/400% (overflow, dialogs, tables, forms); every form with a screen reader
(error summary, field association, invalid states, recovery); async
states not colour-only (loading/success/warning/failure/empty/retry); both
themes, mobile viewports, reduced motion; browser-specific behaviours and
injected external content.

**Acceptance criteria**: reliable name+role for every control; every
mouse action from the keyboard; focus contained/restored in the dialogs;
async announcements without excessive interruptions; contrasts in both themes; semantics
usable for tables/tabs/progress/errors; no regression on desktop and mobile.

---
