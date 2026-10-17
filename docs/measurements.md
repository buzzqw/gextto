# Measurements and verification of Gextto, gx-torrent and gx-core

The numbers we use to decide, with the machine they were taken on and the command to
repeat them. The figures to beat are in §2, the benchmarks in §3, the investigations and
their decisions in §4, the verification that is not a benchmark in §5.

Everything is measured on an Intel N97 mini-PC with 16 GB of RAM unless stated
otherwise.

The state of the product and the open items are in [`evoluzione.md`](evoluzione.md);
the engine is in [`gx-torrent.md`](gx-torrent.md) (inventory
[`../internal/gxcore/GEXTTO.md`](../internal/gxcore/GEXTTO.md)) and its BEP compliance
in [`bep.md`](bep.md).

## Index

1. [How to measure](#1-how-to-measure)
2. [Current baselines](#2-current-baselines)
3. [Benchmark reference](#3-benchmark-reference)
4. [Investigations and decisions](#4-investigations-and-decisions)
5. [Verification](#5-verification)
6. [Adding a measurement](#6-adding-a-measurement)

---

## 1. How to measure

Two rules apply everywhere: measure the system **as it really runs** (queue and slots,
seed policy, scheduler and auto-management on — never "with the queues off", which would
be a laboratory and not production), and compare two clients only with the same swarm,
the same role, the same policies and the same metrics.

A number is only usable with four things written next to it: the **machine**, the
**date**, the **build**, and the **command** (or harness) that produced it. A single pass
on a public swarm cannot rank a parameter: peers are luck. Parameters are compared with
the controlled local swarm or with repeated, interleaved runs (`-count`, `benchstat`);
a public swarm says whether the feature works, not which value is better.

**Profilers.** The engine and the daemon have an opt-in loopback pprof endpoint:
`GX_TORRENT_PPROF=127.0.0.1:6061` on gx-torrent, `GEXTTO_PPROF=127.0.0.1:6062` on
Gextto. Both are off unless the variable is set, so a profile never costs anything in
normal use.

### Harnesses

Cheap probes and opt-in tests, all outside CI:

| Harness | What it measures |
|---|---|
| `scripts/bench-engines.sh [--size MiB] [--dir DIR] [--leechers N]` | RAM and throughput of gx-core: loopback swarm throughput with peak RSS, and disk write throughput of a directory (`--dir` on the NFS mount to compare) |
| `make measure-seeding` | local swarm, seeder plus 3 leechers by default, distinct `127.0.0.x` addresses. `GX_MEASURE_SUPERSEED=1` compares super-seeding, `GX_MEASURE_MESH=1` connects the leechers to each other, `GX_MEASURE_LEECHERS=N` sets the size |
| `make measure-choking` | the choking campaign: a real **libtorrent** leecher on loopback (`scripts/libtorrent-leech.py`, needs `python3-libtorrent`), plus a real-swarm half that needs an inbound port |
| `make measure-pipeline` | the adaptive request queue at shallow (4/4), default (50/250) and deep (200/500) depth; the knobs travel in `NetworkOptions` (`MaxRequestsOut`/`DefaultRequestsOut`) |
| `make measure-realswarm GX_MEASURE_REAL=<public.torrent>` | a public torrent with DHT, trackers and uTP (`GX_MEASURE_REAL_SECONDS`, default 120 s): peers, uTP split, socket counters. A run that sees no peer fails, so it cannot pass hollow |
| `make measure-merge3` | Swarm Merging Phase 3 on real ISOs (§4.8) |
| `make measure-dht` | the public DHT: routing-table fill and query success (`GX_MEASURE_DHT=5m`, `GX_MEASURE_DHT_INFOHASH=<40 hex>`) |
| `TestMeasureLatencyGuard` | the latency guard against a second instance and a competing download on the real line (`GX_MEASURE_LATENCY_GUARD`, `GX_MEASURE_COMPETITOR_URL`) |
| `TestMeasureManagerLanes` | Jackett/Prowlarr lanes against the aggregate on the live managers (`GX_MEASURE_LANES=<data dir>`) |
| `TestMeasureMergePhase3` | block-granularity merge on real ISOs (`GX_MEASURE_MERGE3=<dir>`, `GX_MEASURE_MERGE3_PEER` for a libtorrent seeder) |
| unchoker simulation (`internal/gxcore/internal/unchoker/sim_test.go`) | the real `Unchoker` driven by synthetic peers: fast downloaders in download, fast uploaders in seed, immediate fast unchoke, optimistic unchoke that does not steal a slot and rotates, budget respected, fairness |
| `TestMeasurePipeliningLatency` | an in-process latency proxy before the seed (no `tc`/`netem`, no root): every chunk released at arrival plus latency, in order and without limiting bandwidth |
| `scripts/holepunch-netns-test.sh` | BEP 55 end to end between two clients behind a stateful firewall, with a public relay (`sudo`, network namespaces, `--keep` to debug) |

## 2. Current baselines

The numbers a change is compared against. Unless noted, measured 2026-10-17 on the N97
with the live service or a standalone instance as stated.

| Area | Figure today | Build | Reproduce with |
|---|---|---|---|
| gx-torrent idle | **60–64 MB RSS** (44–48 MB anonymous), 1–2 % CPU | 226 | clean restart with 3 seeds and 1 download, full IP filter; `ps`/`/proc` |
| gx-torrent under load | **44–48 % of a core at 8–9 MB/s**, ~0.35–0.43 CPU per peer; picker 9 % of the profile | 226 | the peer-reader load test (9–10 releases, Auto, collector every 5 s, pprof 45 s) |
| Memory under load | RssAnon peak **1105 MB** with 10 torrents and the NFS write cache; **507 MB outside the two caches** | 226 | same load, `pprof` heap at the end |
| CPU per peer after the picker rewrite | **−49 %** against the holder-slice picker (3273 → 1962 ticks/60 s) | 228 | same swarm, 3 ISOs, ~170–200 peers |
| gexttod at rest | **46 CPU ticks/min (~0.7 %)**, 0 directory opens per 20 s, no write probe on the NAS | 1785 | `GEXTTO_PPROF`, `strace`, `/proc` over 60 s |
| gexttod in the first minutes | **0.39 % of a core**, stale-temp sweep 0.21 s, 8 new TLS connections in 300 s | 1790 | profile from start, `strace -e connect` |
| Feed archive | **320 MB**, 446,994 rows, one row per infohash; Archive page 37 ms | 1791 | live `gextto_archive.db`, `dbstat`, Archive page |
| Archive insert | **40.9–41.8 ms per 1,000 releases** (3.00 MB, 50,528 allocs) | 1791 | `BenchmarkArchiveSaveBatch` |
| Archive search (standalone) | **7.4 ms** on 100,000 releases, 49× faster than before | — | `BenchmarkFeedArchiveSearch` |
| Piece picker | `RarestManyPeers` **13.8 µs** (was 24.2–24.9), bitfield update+disconnect **65–68 µs** (was 1.04 ms) | 228 | `go test -bench . ./internal/gxcore/internal/piecepicker` |
| uTP loopback | **131–139 MB/s**, 5.6 KB and 99 allocs per op (was 116–127, 23.6 KB, 118) | 224 | `BenchmarkTransferLoopback` |
| Storage | **~5.8 GB/s** buffered, **~7.4 GB/s** mmap; 16 KiB writes on NFS **81 MiB/s** | — | `BenchmarkStorageWrite`, `BenchmarkStorageWriteMmap`, NFS harness |
| Indexer lanes | 30 titles, 6 at a time: **110 s**, 205/0 requests, 5,935 releases (aggregate: 450 s, 35/25, 2,737) | 1796 | `TestMeasureManagerLanes` |
| Latency guard | alone **8,503 KiB/s kept**; with a competing download the other device gets **+55 % (router counters)** or **+66 % (trials only)** | 1794 | `TestMeasureLatencyGuard` |
| Merge on real data | **25.0 % from the network** (Debian and Arch ISOs), also after a restart and in any add order | — | `TestMeasureMergePhase3` |
| DHT | **1,290 nodes in ~80 s**, 314/1,432 queries answered (21.9 %), peers found for a real infohash | — | `make measure-dht` |
| Real swarm throughput | **9.3–9.4 MB/s** on the Ubuntu ISO, against 6.3–7.1 MB/s of libtorrent on the same swarm (one campaign) | — | `make measure-realswarm` |
| Web UI rows | **9,831 B per tick** (was 26,701 B of HTML), −63 % | — | `/ui/live` versus `/ui/rows` on the live page |
| Prometheus scrape | **~0.24 ms, 121 KB, 813 allocs** per scrape | 232 | `BenchmarkMetricsScrape` |

## 3. Benchmark reference

Micro-benchmarks with `-benchmem`; run them with `-count=6` and compare with `benchstat`
when a change is meant to be an optimisation. A single pass is noise.

| Benchmark | Current figure | Note |
|---|---|---|
| `BenchmarkEngineLoggerFiltered` vs `Forwarded` | 151–175 vs 730–785 ns/op, 3 vs 6 allocs/op | records below the handler threshold are discarded before formatting, so `Debugf`/`Infof` on the hot paths allocate nothing at WARNING |
| `BenchmarkBlocked` (IP filter) | 18.6 / 37.7 ns/op, 0 allocs | sorted merged intervals with binary search |
| `BenchmarkPickerRarestSteady` | ~105 / 143.5 µs/op, 0 allocs | piece picker, steady state |
| `BenchmarkTickUnchokeSeeding` | 1.3 / 1.64 µs/op, 1 alloc | unchoker tick; 50 synthetic peers ≈1.1 µs, 896 B, 1 alloc |
| `BenchmarkPrefillFromUnrelatedV2` (16 candidates) | 1.49 / 1.58 µs/op, 128 B, 16 allocs | one-shot cost of the Phase 2 merge scan; no steady-state path |
| `BenchmarkMergeCandidatesUnrelated` (32 torrents) | ~265 ns/op, 0 allocs | filter paid when nothing is reusable |
| `BenchmarkNewConnID` (uTP) | 31 ns/op, 0 allocs | the old version built a 64K-entry table per dial (512 KiB, 152,353 ns/op) |
| `BenchmarkTransferLoopback` (uTP, 16 KiB block) | 131–139 MB/s, 5.6 KB, 99 allocs | connection-id probe, lazy `Event` channels, reusable read window, `netip.AddrPort` key |
| `BenchmarkUTPConnRemoteAddr` | 113 ns/op, 40 B, 3 allocs (was 259, 152, 6) | addresses converted once at wrap |
| `BenchmarkStorageWrite` / `StorageWriteMmap` | ~5.8 / ~7.4 GB/s | the disk is far above any network speed: `O_DIRECT` and io_uring are not justified |
| `BenchmarkArchiveSaveBatch` (1,000 releases, ~400 B magnets) | 40.9–41.8 ms, 3.00 MB, 50,528 allocs | prepared statements per chunk |
| `BenchmarkFeedArchiveSearch` (100,000 releases) | 7.4 ms | was 362 ms |
| `BenchmarkMetricsScrape` (one torrent) | ~0.24 ms, 121 KB, 813 allocs | computed only when scraped |
| `BenchmarkCriteriaMatch` (RSS rule, 4 names) | 142 ns/op, 0 B, 0 allocs | was 250 ns; the first normalising versions cost 395–1,590 ns |
| `BenchmarkRateHistoryAdd` / `LifetimeAccumulate` | 42 / 6.4 ns/op, 0 allocs | the statistics page costs nothing on the sampling path |
| `BenchmarkRateHistorySnapshot1h` / `StatsPageTranslate` | 11.3 µs, 20 KiB, 1 alloc / 255 µs, 89 KiB, 1,919 allocs | one snapshot and one page translation per graph request |

## 4. Investigations and decisions

Each entry states the question, the numbers that answered it and the decision. The
numbers a change **replaced** stay here only while they justify the design; the
build-by-build chronicles are compressed to one row per step.

### 4.1 Engine CPU and memory

**Current figures**, from a load test with 9–10 releases, Auto speed, a collector every
5 s and a CPU profile at ≥ 8 MB/s:

| | Value |
|---|---|
| Idle | 60–64 MB RSS (44–48 MB anonymous), 1–2 % of a core |
| Under load | 44–48 % of a core at 8–9 MB/s with ~110–130 peers |
| CPU per MB/s / per peer | 5.3 / 0.35–0.43 |
| Memory under load | RssAnon peak ~1.1 GB with 10 torrents and the NFS write cache; ~507 MB outside the two caches |
| Piece picker | 9 % of the profile |

**Choices behind these figures.** The read path uses a progress-based deadline, so a
dead uTP connection fails at once instead of looping; the socket handles each datagram in
the receive buffer and copies only what stays queued; `waitEvents` is a plain select.
The IP filter keeps the parsed rules instead of the file, `GOGC=50` trims the heap
headroom, and the buffers of a finished burst are released. The collector costs 0.5 %
(`gcBgMarkWorker`) and 2.2 % (`mallocgc`) of the profile.

**Piece picker.** With ~200 peers it reads the peer bitfield (O(1)), keeps a count per
piece and a counter of high-priority pieces, and touches only the pieces a disconnecting
peer had. On three public ISOs at 9.4 MB/s it is 9 % of the CPU;
`BenchmarkRarestManyPeers` is 13.8 µs and the bitfield update+disconnect 65–68 µs.
Pinned by `TestHoldersMatchAReferenceModel` (20,000 random have/don't-have/disconnect
steps).

**uTP retransmission.** A 5 ms minimum-RTT floor keeps a fast path from collapsing the
RTO; a per-connection backoff doubles on timeout and halves on delivery; a healthy
connection never timeout-resends. `BenchmarkTransferLoopback` is 5.6 KB / 99 allocs per
op at 131–139 MB/s. The listen socket keeps a 50-SYN backlog bound that never binds on
the real swarms tested (`utp_syn_drops` 0).

### 4.2 Daemon at rest

**Current figures** (live service, 4 torrents, 1 peer; `GEXTTO_PPROF`, `strace` and
`/proc`):

| | Value |
|---|---|
| Idle CPU | 46 ticks/min (~0.7 % of a core) |
| Directory opens | 0 per 20 s; no write probe |
| `torrentEventWorker` | 90 ms per 60 s |
| First minutes after a start | 0.39 % of a core, stale-temp sweep 0.21 s |
| Metadata calls | ~5,000 a day for 55 series |

**Choices behind these figures.** The `.torrent` copies are trusted for 5 minutes (or
until the name or the copy folder changes) instead of being stat'ed at every 1.5 s poll.
The Salute tile and the health check share a 30 s cache and use `access(W_OK)` on network
shares instead of writing, so the NAS disks can spin down. A consistent pack-identity
verdict is reused for 60 s. The stale-temp sweep uses `WalkDir` and stats only temporary
names. One TMDB transport is shared and the calendar is bounded to 8 lookups with a
15 min cache. `RemoveSeededCompleted`, `DetachCompletedArchivedSingles` and
`DetachErrorTorrents` query only what the session view does not already rule out.

**gx-torrent at rest** (same session): **1.45 % of a core**, of which 41 % syscalls and
~50 % `runtime.findRunnable` — uTP state packets and sends, incoming MSE handshakes
(`mse.keyPair` 4.6 %) and the DHT loop, which is real traffic. The ~330 `nanosleep`/s are
the Go scheduler's own wake-ups. No spinning loop, nothing to change.

### 4.3 Feed archive

**Compact magnet key** (live `gextto_archive.db`, 446,994 rows, `dbstat`): the 572 MB
were table 264 MB, the automatic index on `magnet TEXT UNIQUE` **207 MB** (average magnet
399 bytes), `idx_archive_title` 45 MB (never used), FTS 23 MB, other indexes 29 MB. Only
126,601 distinct infohashes existed (plus 49,390 `.torrent` URLs). Uniqueness is now on
`magnet_key`, the first 16 bytes of the SHA-256 of the exact magnet (same dedup, the
magnet is hashed through a stack buffer), and the title index is gone. The one-off
rebuild keeps the ids, so the FTS index stays valid, then VACUUMs: **599 → 352 MB
(−41 %)** in 8.2 s, `quick_check` ok, search intact.
`BenchmarkArchiveSaveBatch` went from 58.7–59.1 ms to **52.9–54.0 ms** per 1,000 releases
and the file from 57.7 to **38.2 MiB** (−34 %); the +4 allocations per row are the
16-byte key.

**One row per torrent** (build 1791): of the 191,102 rows with an infohash, **64,502
repeated a torrent already listed** (43,679 torrents). Same infohash is the same content:
the newest row is kept (the one Search returns first and the orchestrator chose), base32
hashes are stored as hex, `idx_archive_hash` (partial UNIQUE) enforces it, and a known
torrent under another magnet refreshes its row; URL rows without a hash are never
merged. The one-off pass removed **64,619 rows in 5.6 s, 352 → 320 MB**, `quick_check`
ok, no infohash twice, Archive page at 37 ms.

| `BenchmarkArchiveSaveBatch` | ns/op | B/op | allocs/op |
|---|---|---|---|
| magnet `UNIQUE`, `Exec` per row | 58.7–59.1 ms | 2.64 MB | 40,016 |
| one upsert with two conflict targets | 70.2–71.7 ms | 2.80 MB | 44,017 |
| **`INSERT OR IGNORE` + conditional refresh, prepared per chunk** | **40.9–41.8 ms** | 3.00 MB | 50,528 |

**Decision:** the two-conflict upsert was rejected (+17 ms per 1,000 releases) and
compiling the SQL per row was the larger cost; the kept variant is **−30 %** against the
morning's code with +13 % allocated bytes (prepared-statement arguments). The expression
index `lower(COALESCE(magnet_hash,''))` is gone: hashes are normalized and use
`idx_archive_hash`.

### 4.4 Indexer managers

**The aggregate is serialised by the slowest indexer** (live Prowlarr and Jackett, 30
enabled series titles). One search alone: Prowlarr aggregate **1.0 s** (LimeTorrents
1.2 s, The Pirate Bay 1.2 s, TorrentLeech 3.6 s), Jackett aggregate 2.1 s.
Prowlarr, 30 titles: 1 at a time 143 s (p50 4.1 s), 2 at a time 123 s (p50 8.2 s), **6 at
a time 119 s (p50 24.6 s)**. Prowlarr serves each indexer **one search at a time**: the
throughput is that of TorrentLeech (~0.25 searches/s) whatever the concurrency, and a
request that times out throws away the results of the indexers that had answered.

**Per-indexer lanes against the aggregate** (30 titles, 6 at a time; the lanes are
measured first, on a rested Prowlarr, because a timed-out aggregate keeps running inside
it):

| mode | cycle wall | per title p50 / max | requests ok / failed | releases |
|---|---|---|---|---|
| **lanes** | **110 s** | **2.1 s** / 100 s | **205 / 0** | **5,935** |
| aggregate, 90 s | 450 s | 90.0 s / 90 s | 35 / 25 | 2,737 |
| aggregate, 45 s (earlier) | 225 s | 45.0 s / 45 s | 33 / 27 | 2,672 |

**−76 % cycle time, no failed request, +117 % releases.** A Jackett lane answers in
0.1–1.3 s (and Jackett caches a repeated search); the 100 s maximum is a title whose
TorrentLeech search queued behind the others. Managers get **90 s** per request
(`managerRequestTimeout`; other indexers keep 45 s, the title budget is 100 s); a cycle
counts as failed for a source only when most of its requests failed, and a timeout is
reported as "slow answers". LimeTorrents and TorrentLeech are configured in **both**
managers, so each title used to be searched twice on the same trackers; the lanes keep
one, the faster.

### 4.5 The home line and the latency guard

**Latency under a saturated download** (`ping` 60 × 0.5 s): gateway 0.75 → 0.67 ms (the
LAN is not the bottleneck); Internet (9.9.9.9) **19.8 ms at rest → 62.5 ms average,
82.6 ms peak** at 9.4 MB/s down, near-zero upload. A full download queues at the ISP and
adds ~40 ms for every device in the house. The ping auto-speed acts only on the upload
cap, so it would not have helped.

**Designs tried** (`TestMeasureLatencyGuard`, a separate standalone instance downloading
three public ISOs, 90 s phases; the other device is a plain HTTP download of the Ubuntu
ISO read and discarded):

| design | phase | Gextto | other device | capped | verdict |
|---|---|---|---|---|---|
| reference = window minimum, router counters vs reported rates | alone | 9,196 KiB/s | — | 43 % | false "others": the reported rates lag tens of seconds behind the counters |
| same | others | 8,479 | 754 | 93 % | ineffective: the reference was the loaded latency (56 ms) |
| peak inference without router | others | 8,764 | 298 | 0 % | blind: one competing flow takes 3–10 % of the line and Gextto stays "near its peak" |
| **trials + instant wire rates** | **alone** | **8,503** | — | 8 % (the trial) | the line stays Gextto's |
| | others, guard off | 4,532 | 4,372 | — | today's sharing |
| | **others, router** | 2,084 | **6,769 (+55 %)** | 100 % | yields |
| | **others, no router** | 1,606 | **7,253 (+66 %)** | 100 % | yields on the trial alone |

**Decision:** read the cumulative `peer_wire_*` counters (the reported rate is averaged),
probe the latency every 2 s only while Auto is in force, and learn the idle reference by
trial. The first two designs were rejected on these numbers. Release: the competitor
stopped after a minute and the cap was lifted **2 min 4 s** later (the calm spell is 60 s
now). A run where the CDN gave the competitor 253 KiB/s ended in an ambiguous trial
(100 → 53 ms), taken as "Gextto's own" with 53 ms as the reference; since then the
**lowest** reference is kept for 6 h and only a lower measure replaces it, so one
disturbed trial cannot blind the guard.

### 4.6 Swarm: seeding, choking, pipelining

**Unchoker.** The real `Unchoker` driven by synthetic peers holds every property
(fast downloaders in download, fast uploaders in seed, immediate fast unchoke,
optimistic unchoke that does not steal a regular slot and rotates, budget, fairness), and
the tick with 50 peers costs **~1.1 µs, 896 B, 1 alloc**: negligible. Hardening: an
interested but stalled peer receives the next piece at the tick instead of waiting for
`not-interested` alone.

**Local seeding** (2 MiB corpus, 3 leechers, 5 repetitions): normal **~41–42 ms at
3.00× corpus**; super-seeding ~51–52 ms (**~24 % slower**) with the same 3.00×. With
mesh the leechers upload **2.52 MB among themselves** and the seed drops from 3.00× to
**1.84× (−39 %)**. Super-seeding does not reduce the upload on a small fast swarm and
costs throughput; the BEP 16 advantage is for the initial seeding of large torrents with
many slow peers, which one host cannot reproduce. **Decision: super-seeding stays
opt-in, not default.**

**Pipelining** (RTT 50 ms, 256 KiB pieces, 4 MiB corpus, one leecher):

| profile | single piece | multi-piece |
|---|---|---|
| `shallow` def=1/max=1 | 301 KiB/s | 301 KiB/s (window of 1 respected) |
| `default` def=50/max=250 | 3,853 KiB/s | **6,756 KiB/s (×1.75)** |

The adaptive queue alone took `default` from 305 KiB/s to **3.8 MiB/s (~12×)**; the
ceiling was the piece's block count, so a peer now downloads up to two pieces at once,
sharing one in-flight budget (the extra piece opens only when the window has room) and
never receiving the same piece twice. The bounded write ring buffer (4 pieces per
torrent) keeps the disk from becoming the bottleneck while more pieces are in flight: on
loopback seeding is equivalent, the gain is on slow storage.

**End to end on loopback:** `make measure-seeding` 3.00× corpus in 11 ms;
`make measure-pipeline` 16 MiB in 101 ms (**~162 MiB/s**), with RTT 50 ms 0.608 s /
**6,736 KiB/s** against 13.55 s / 302 KiB/s shallow.

**Real swarm, controlled campaign** (Ubuntu 24.04.3 desktop, ~6.0 GB, the same machine,
40 s after a 5 s ramp-up, three public UDP trackers, two runs each of libtorrent 2.0.11
and gx-core):

| Client | run | metadata | peers (window) | downloaded | average |
|---|---|---|---|---|---|
| libtorrent 2.0.11 | 1 | 3 s | 83 (81 seed) | 292.9 MB | **7,149 KiB/s** |
| libtorrent 2.0.11 | 2 | 3 s | 66 (61 seed) | 258.8 MB | **6,317 KiB/s** |
| gx-core | 1 | 3 s | 80 (77 seed) | 385.1 MB | **9,401 KiB/s** |
| gx-core | 2 | 3 s | 81 (79 seed) | 381.2 MB | **9,305 KiB/s** |

**Reading:** gx-core is **on par with or above** libtorrent on this swarm (median ~9.3
against ~6.7 MiB/s), with immediate metadata and a comparable number of peers. It is not
an isolated demonstration of choking or pipelining (peer choice differs, one machine,
two runs): the isolation is the deterministic unchoker bench and the in-process latency
proxy above.

**Real swarms and uTP** (`make measure-realswarm`): Big Buck Bunny 276 MB in 90 s (75
peers, 23 uTP), Debian netinst 793 MB in 180 s (80 peers, 18 uTP), Ubuntu 24.04 1.08 GB
in 180 s (85 peers, 31 uTP), `utp_syn_drops` **0** everywhere. A 5-minute Debian run
completes at 100 % with peers peaking at 80 (uTP 24), dials 270 (107 errors — the cost of
racing uTP against TCP-only peers), RTO resends 831 and fast 69: the swarm is reached and
the counters stay far from a storm. The reported "starts fast then slows down" was a
**scarce swarm** (trackers reported 0–1 seeders, a single 100 % peer), not an engine-wide
defect.

**Choking campaign against libtorrent:** our seeder served a real libtorrent leecher on
loopback, **48 MiB (100 %) in 4.84 s (~10.4 MB/s)** with the leecher verifying complete.
The real-swarm half found 0 peers because seeding needs an **inbound** port and this host
is behind a NAT (the download run finds 80 peers outbound); the harness is kept for a
host with a reachable port. **Decision: no choking change** — reciprocation works
locally and against libtorrent; the missing datum is blocked by the host, not by the
algorithm, and the campaign stays the gate for any future change.

### 4.7 DHT

**Cold start.** The peer-search retry while a torrent looks for peers uses a **15 s**
interval (`DHTMinAnnounceInterval`); the Ubuntu torrent over DHT only resolves its
metadata in ~49 s with 12 peers, and the remaining time is the routing-table fill
(~30 s cold), not the lookup.

**Public DHT** (`GX_MEASURE_DHT=2m GX_MEASURE_DHT_INFOHASH=<Ubuntu>
go test -run TestMeasureDHTRealSwarm`): a fresh node bootstrapped from the built-in
routers filled the table to **1,290 nodes in ~80 s**, ran **1,432 `get_peers` with 314
replies (21.9 %)** and returned **2 peers for the Ubuntu infohash (1 confirmed by more
than one subnet)**: the lookup path returns real peers, not just nodes. `dropped=411`,
`reflected_observations=189`, `nat_mapping=1`, `nodes_insecure_id=76`, `subnet_share_max=4`.
A 5-minute Debian run: nodes ~296 (peak 525), `subnet_share_max` 4–7, `nodes_insecure_id`
≈42 %, `queries_out` 4,703 / `replies_in` 1,670 (**35.5 %**), `peers_found` 0 for that
run's infohash. Another public run reached **4,262 nodes** at 21.9 % success.

**Read:** the routing table is **not concentrated**; no node exceeds the candidate
thresholds (8/16/24), so an anti-Sybil cap would be a no-op while CGNAT and mobile
legitimately share subnets. **Decision: the cap stays a hint, off by default.** The
65–78 % of unanswered queries are nodes that do not reply, not proof of poisoning.

**Multi-path `get_peers` — closed with data.** The instrumentation exists and is
read-only (`notePeerReport`/`expirePeerConfirm`, reporters counted per subnet, so one
host on many ports cannot confirm its own peers). In both real runs
`peers_confirmed_multi` was **0**: no peer was confirmed by two distinct subnets, so
there is no evidence justifying the rewrite. A controlled adversarial swarm would be the
only next step, and it is out of scope for a home client.

### 4.8 Merge on real data

Phase 2 reuses a destination piece only when **every** source piece overlapping it is
verified; Phase 3 reuses the rest at **16 KiB block** granularity (verified spans written
to disk, blocks recorded per piece, read back at pick time, whole piece verified as
usual). It is safe because BEP 52 leaves are blocks of the **file**, not of the piece: two
torrents with the same `pieces root` share the Merkle tree below the piece layer whatever
their piece length. A failed whole-piece check drops the local blocks, does not ban the
peer and downloads the piece whole.

Real data: `scripts/phase3-env.sh` builds v2-only Debian netinst and Arch Linux torrents
(source 256 KiB pieces, destination 1 MiB, same file, one source piece in four corrupted)
and `TestMeasureMergePhase3` runs a gx-torrent seeder (or a **libtorrent** seeder with
`scripts/phase3-seed.py`) against a leecher holding the partial source.

| Run (loopback) | before (Phase 2) | after (Phase 3) |
|---|---|---|
| Debian 756 MiB from the network | 792,723,456 B (100 %) | **198,180,864 B (25.0 %)** |
| Debian total time | 2.81 s | **1.31 s** |
| Arch 1.5 GiB from the network | 1,640,497,152 B (100 %) | **410,255,360 B (25.0 %)** |
| Arch total time / CPU / peak RSS | 5.82 s / 17.7 s / 109 MiB | **2.71 s / 10.0 s / 111 MiB** |
| Debian with a libtorrent 2.0.11 seeder | — | 25.0 % from the network, complete |
| resume after a restart (Debian) | 100 % | **25.0 %**, `merged_bytes` 594,542,592 re-derived |
| sibling added later (Debian) | 100 % | **25.0 %**, same 594,542,592 re-derived |

All 756 (Debian) and 1,565 (Arch) destination pieces were reused at block level and every
one passed its hash. The resume keeps the reloaded bitfield (no piece re-read or
re-hashed, via `Verifier.Skip`/`Only`) and re-derives the block reuse, so a restart no
longer re-downloads the file; the later-sibling trigger gives the same result whatever
the add order. The cost when nothing is reusable is the candidate filter: **115 ns and
0 allocations** over 32 unrelated session torrents.

### 4.9 Other measurements

**Statistics page** (standalone daemon, 0 torrents): the new sample and lifetime folding
are free (`42 ns/op` and `6.4 ns/op`, 0 allocs); a graph request pays 11.3 µs / 20 KiB / 1
alloc and the page translation 255 µs / 89 KiB / 1,919 allocs. Over 30 s idle the daemon
spent **89 → 91 CPU ticks (+20 ms, +2 %)**, and with the page open polling every 3 s the
same ticks: within noise. The binary grew **+52 KiB**; the three history rings are ~45 KiB.

**RSS term matching**: the rule preview (up to 5,000 archived items) and every feed poll
went from 250 to **142 ns/op, 0 B, 0 allocs**; the first normalising versions cost
395–1,590 ns.

**Crash-safe adds and engine-aware settings**: the add intent written before every engine
add costs **76–79 µs, 3.2 kB, 44 allocs** per started download (negligible next to the
engine add); the per-torrent default peer cap is one engine command per torrent, only at
add, session reload or when the value changes. Live service 12 minutes after a restart:
`gexttod` RSS 70.7 MB, `gx-torrent` 104.6 MB — no regression.

**Web UI refresh (gx-torrent page)**: the 2 s table refresh used to ship server-rendered
HTML at ~10 KB per torrent (a 300-row window ≈3 MB per tick). Rows now travel as JSON
(`GET /ui/rows`, same `uiPageData`) and are rendered with a client template verified
cell-by-cell against the server one. On the live service: `/ui/live` **26,701 B** per
tick → `/ui/rows` **3,432 B** + notable **6,399 B** = **9,831 B (−63 %)**; per row ~10 KB
HTML vs ~1.7 KB JSON (~6×). Empty sessions and limited windows still use the server HTML,
pinned by `TestUIRowsJSON` and the i18n guards.

**Prometheus metrics**: one scrape is **~0.24 ms, 121 KB, 813 allocs** (≈0.001 % of a core
at 15 s), computed only when scraped.

**Robustness at no cost**: Torznab answers are decoded leniently (a raw `&` or an HTML
entity in a title), **2.22–2.32 ms** against **2.24–2.32 ms** strict on a 100-item answer
(identical allocations); the uTP header refuses inputs shorter than 20 bytes instead of
panicking, the bencode screen bounds lengths and depth before the decoder, and the mmap
fault guard turns a fault into an I/O error with an equivalent write bench (36.3–38.0 →
35.9–37.5 µs, 0 alloc).

**Auto-tune connection cap**: on a public swarm the bound (50–400) is **safe** — a lower
cap used less than half the memory with comparable throughput, and the spread across
caps was swarm luck (one 60 s pass cannot rank a parameter); the controlled local sweep
(8 seeders, 64 MiB, loopback) is **flat** across the cap (99–104 MiB/s), because one seed
source already saturates the path. **The cap bounds connections (memory), never the
rate.**

**Storage and disk**: buffered **~5.8 GB/s**, mmap **~7.4 GB/s** (ranged mmap sync ~29
GB/s through the page cache), far above any network speed. Removing `O_SYNC` and batching
durability took 16 KiB writes on NFS from **35 to 81 MiB/s** (2.3×), up to ~118× at small
blocks on NVMe, with the invariant that a persisted bit refers to data already fsynced
(`syncPieces()` on the run loop, `writeBitfield()` ordered snapshot → fsync → write).
The SQLite `cache_size` stays at SQLite's default 2 MB: on the real archive DB (510 MB,
FTS5) the latency is flat within noise even on a broad `MATCH` (~1.14 s), because the OS
page cache serves the file — `gexttod` RSS 67.6 MB against 109.7 MB at the same uptime.

### 4.10 Rejected designs and ideas not added

Closed with a measurement, so they are not retried without new data:

| Idea | Why it was closed |
|---|---|
| `io_uring` / AIO | the disk is at 5.8–7.4 GB/s, far above any network speed; Go exposes it only with a dependency or cgo, and the mmap path is already asynchronous |
| Batched uTP datagram I/O (`recvmmsg`) | on the measured load (TCP carried most of the bytes) batching would save under 1 % of the syscall time; worth re-measuring only on a uTP-heavy session |
| Contiguous-write coalescing | the page cache already merges adjacent writebacks |
| Rarity buckets, watermark cursor in `pickSequential`/`pickFileEdge`, `PieceDownloader` bitmask, `clear(b.Data)` in `bufferpool`, `FADV_RANDOM`, `saveLocked` outside `d.mu`, `/proc/meminfo` under `d.mu` | no measurement justified the cost |
| Upsert with two conflict targets (archive) | +17 ms per 1,000 releases against `INSERT OR IGNORE` + refresh |
| `idx_archive_title`, expression index on `lower(magnet_hash)` | never used by the queries; only write time and space |
| Multi-path `get_peers` | `peers_confirmed_multi` was 0 in both real runs |
| Anti-Sybil cap on by default | the routing table is not concentrated and legitimate CGNAT/mobile subnets would be capped |
| Choking changes | reciprocation works locally and against libtorrent |
| Super-seeding by default | no upload reduction on the measurable swarms, and ~24 % slower locally |
| Latency guard: window-minimum reference, router counters against reported rates, peak inference | false "others", ineffective, blind — the trials design replaced them |

**Postponed, with a reason:** qBittorrent's `renameFile`/`renameFolder` (needs rename
support in the engine: a feature of its own); graduated file priority with
mixed-priority files (open item in [`evoluzione.md`](evoluzione.md) §8); qBittorrent's
`setAutoManagement` tuning.

## 5. Verification

### Accessibility

**Method**: static review (HTML/CSS/JS), semantics/names/ARIA/focus, responsive
layout/visible focus/reduced-motion, light/dark theme contrast, axe-core via Playwright,
regression tests (dialogs, keyboard sort, polling, narrow viewports). Suite: **14 green
tests** on dashboard, downloads, settings, maintenance, health, logs
(`cd uiweb/end2end && npm ci && npm run test:a11y`; a safety net, not a substitute for
the manual assessment).

**gx-torrent pages** (2026-10-15, Chromium via Playwright + `@axe-core/playwright`, tags
`wcag2a`/`wcag2aa`, light and dark theme, a standalone daemon with sample torrents,
feeds, rules and wanted titles): the torrent list, its context and column menus, the
detail panel (general, files, peers), *Connection details*, *Global speed limits*, the
RSS rules and feeds with their editors, the wanted page with its editor and the
*Releases* dialog — **0 violations**. The first run found and led to fixing: column and
panel resizers without a value (`aria-valuenow`), zero-count filters dimmed below the
contrast minimum, `aria-selected` rows outside a grid (the table is now `role="grid"`
with `aria-multiselectable`), `aria-expanded` set on a row by the context menu, preview
lists that scroll but could not take the focus, and the progress percentage drawn over
the bar (now beside it, readable on any bar colour).

**Residual validation** (before any formal declaration): primary workflows with
Tab/Shift+Tab/Enter/Space/arrows/Escape; at least one desktop screen reader
(NVDA+Firefox or VoiceOver+Safari) and TalkBack where applicable; zoom 200 %/400 %;
every form with a screen reader; async states not colour-only; both themes, mobile
viewports, reduced motion; browser-specific behaviours and injected external content.

**Acceptance criteria**: reliable name+role for every control; every mouse action from
the keyboard; focus contained/restored in the dialogs; async announcements without
excessive interruptions; contrasts in both themes; semantics usable for
tables/tabs/progress/errors; no regression on desktop and mobile.

### Backup and restore

Round trip on a copy of the live data: **all 40 tables of the four databases identical**
and integrity ok, with the default recycle bin, the probe leftovers and the run marker
excluded. A restore on another machine starts with only the data and state folders; an
unavailable download, temp or trash folder is a **warning** (the daemon used to
crash-loop on mkdir) and is fixable from the interface. Verified in a systemd container
(install, upgrade, restore offline in dry-run).

### Packaging

The release archive root is **0755** (`mktemp -d` made it 0700, which tar applied to the
extracted folder) and `build-release.sh` checks it; `install.sh` keeps `gexttod.prev`
**and** `gx-torrent.prev`, as `gexttod --update` does.

## 6. Adding a measurement

- Next to each figure: the machine, the date, the build and the command that produced
  it, so the measurement can be repeated and compared.
- If it changes a baseline, update §2 (and §3 for a benchmark); if it decides something,
  add it to §4 and, when rejected, to §4.10.
- Compare before and after with `-count`/`benchstat` or an interleaved A/B. One pass on a
  public swarm cannot rank a parameter.
