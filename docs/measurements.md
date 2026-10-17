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
| gexttod at rest | **46 CPU ticks/min (~0.7 %)**, 0 directory opens per 20 s, no write probe on the NAS | 1785 | `GEXTTO_PPROF`, `strace`, `/proc` over 60 s |
| gexttod in the first minutes | **0.39 % of a core**, stale-temp sweep 0.21 s, 8 new TLS connections in 300 s | 1790 | profile from start, `strace -e connect` |
| Feed archive | **320 MB**, 446,994 rows, one row per infohash; Archive page 37 ms | 1791 | live `gextto_archive.db`, `dbstat`, Archive page |
| Archive insert | **40.9–41.8 ms per 1,000 releases** (3.00 MB, 50,528 allocs) | 1791 | `BenchmarkArchiveSaveBatch` |
| Archive search (standalone) | **7.4 ms** on 100,000 releases, 49× faster than before | — | `BenchmarkFeedArchiveSearch` |
| Piece picker | `RarestManyPeers` **13.8 µs**, bitfield update+disconnect **65–68 µs** | 228 | `go test -bench . ./internal/gxcore/internal/piecepicker` |
| uTP loopback | **131–139 MB/s**, 5.6 KB and 99 allocs per op | 224 | `BenchmarkTransferLoopback` |
| Storage | **~5.8 GB/s** buffered, **~7.4 GB/s** mmap; 16 KiB writes on NFS **81 MiB/s** | — | `BenchmarkStorageWrite`, `BenchmarkStorageWriteMmap`, NFS harness |
| Indexer lanes | 30 titles, 6 at a time: **110 s**, 205/0 requests, 5,935 releases (aggregate: 450 s, 35/25, 2,737) | 1796 | `TestMeasureManagerLanes` |
| Latency guard | alone **8,503 KiB/s kept**; with a competing download the other device gets **+55 % (router counters)** or **+66 % (trials only)** | 1794 | `TestMeasureLatencyGuard` |
| Merge on real data | **25.0 % from the network** (Debian and Arch ISOs), also after a restart and in any add order | — | `TestMeasureMergePhase3` |
| DHT | **1,290 nodes in ~80 s**, 314/1,432 queries answered (21.9 %), peers found for a real infohash | — | `make measure-dht` |
| Real swarm throughput | **9.3–9.4 MB/s** on the Ubuntu ISO, against 6.3–7.1 MB/s of libtorrent on the same swarm (one campaign) | — | `make measure-realswarm` |
| Web UI rows | **9,831 B per tick**, ~1.7 KB per row | — | `/ui/rows` on the live page |
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
| `BenchmarkUTPConnRemoteAddr` | 113 ns/op, 40 B, 3 allocs | addresses converted once at wrap |
| `BenchmarkStorageWrite` / `StorageWriteMmap` | ~5.8 / ~7.4 GB/s | the disk is far above any network speed: `O_DIRECT` and io_uring are not justified |
| `BenchmarkArchiveSaveBatch` (1,000 releases, ~400 B magnets) | 40.9–41.8 ms, 3.00 MB, 50,528 allocs | prepared statements per chunk |
| `BenchmarkFeedArchiveSearch` (100,000 releases) | 7.4 ms | compact index on bbolt |
| `BenchmarkMetricsScrape` (one torrent) | ~0.24 ms, 121 KB, 813 allocs | computed only when scraped |
| `BenchmarkCriteriaMatch` (RSS rule, 4 names) | 142 ns/op, 0 B, 0 allocs | scans the title in place |
| `BenchmarkRateHistoryAdd` / `LifetimeAccumulate` | 42 / 6.4 ns/op, 0 allocs | the statistics page costs nothing on the sampling path |
| `BenchmarkRateHistorySnapshot1h` / `StatsPageTranslate` | 11.3 µs, 20 KiB, 1 alloc / 255 µs, 89 KiB, 1,919 allocs | one snapshot and one page translation per graph request |
| `BenchmarkSeedPolicyPass` (10 seed rules, 100 torrents) | 41.4 µs, 5.2 KB, 128 allocs per pass | what the seed rules add to one `EnforceSeedPolicy` pass (every few seconds), plus one `torrent_meta` query only when rules exist; measured 2026-10-17 on the N97 |
| `BenchmarkPayloadContentPath` (2,000-file torrent) | Original 207 ns, 64 B, 1 alloc; NoSubfolder 1.65 µs, 0 allocs | the `content_path` every list refresh now computes per torrent (content layout); measured 2026-10-17 on the N97 |

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

**Current figures** (live `gextto_archive.db`, `dbstat`, `BenchmarkArchiveSaveBatch`):

| | Value |
|---|---|
| Size | 320 MB, 446,994 rows |
| Identity | one row per torrent (infohash); the exact magnet for rows without a hash |
| Insert | 40.9–41.8 ms per 1,000 releases (3.00 MB, 50,528 allocs) |
| Archive page | 37 ms |
| Standalone feed search | 7.4 ms on 100,000 releases |

**Choices behind these figures.** Uniqueness is on `magnet_key`, the first 16 bytes of
the SHA-256 of the exact magnet: the dedup is unchanged without a 400-byte index. Rows
with the same infohash are the same content, so the newest is kept, base32 hashes are
stored as hex and a partial UNIQUE index enforces it; a known torrent under another
magnet refreshes its row, while URL rows without a hash are never merged. Inserts are
prepared per chunk (`INSERT OR IGNORE` plus a conditional refresh), faster than a
two-conflict upsert. The plain title index is gone: titles match through FTS or
`lower(title) LIKE`.

### 4.4 Indexer managers

**Current figures** (`TestMeasureManagerLanes`, the live Prowlarr and Jackett, 30
titles, 6 at a time):

| | Value |
|---|---|
| Cycle | 110 s (per title p50 2.1 s, max 100 s) |
| Requests | 205 answered, 0 failed |
| Releases | 5,935 |

**Choices behind these figures.** Each manager indexer is a lane, read from the manager
at run time (10 min cache, never copied into the configuration): Prowlarr
`/api/v1/search?indexerIds=`, Jackett the indexer's Torznab endpoint. Prowlarr serves
one search per indexer, so a lane has one slot (Jackett two), its own timeout (90 s
behind Prowlarr, 45 s direct) and its own health; a title skips a lane whose queue would
outlast its 100 s budget. A tracker present in both managers is searched once, through
the faster lane; Prowlarr's failing indexers are skipped; the aggregate is the fallback
when the list cannot be read.

### 4.5 The home line and the latency guard

**Current figures** (`TestMeasureLatencyGuard`, a second instance downloading three
public ISOs, 90 s phases; the other device is an HTTP download read and discarded):

| | Value |
|---|---|
| Alone on the line | 8,503 KiB/s kept, the trial at 8 % |
| With a competing download, router counters | the other device gets 6,769 KiB/s (+55 %) |
| With a competing download, trials only | 7,253 KiB/s (+66 %) |

**Choices behind these figures.** The guard reads the engine's cumulative `peer_wire_*`
counters, because the reported rate is averaged over tens of seconds, and probes a
public host every 2 s while Auto is in force. It learns the idle reference by trial: a
latency that falls after Gextto backs off means the queue was its own; one that stays
high means someone else is on the line. The router's UPnP WAN counters decide first when
they answer. The lowest reference is kept for 6 h, so one disturbed trial cannot blind
the guard.

### 4.6 Swarm: seeding, choking, pipelining

**Current figures.** The unchoker holds its properties on the synthetic harness (fast
downloaders in download, fast uploaders in seed, immediate fast unchoke, optimistic
unchoke that does not steal a slot and rotates, budget, fairness); the tick with 50 peers
costs ~1.1 µs, 896 B, 1 alloc.

Local swarm (2 MiB, 3 leechers): **3.00× corpus** in ~41–42 ms; with mesh the leechers
exchange 2.52 MB and the seed drops to **1.84×**. Super-seeding is opt-in: it does not
reduce the upload on a measurable swarm and costs ~24 % locally.

Pipelining at RTT 50 ms, 256 KiB pieces: 305 KiB/s with one block in flight, 3.8 MiB/s
with the default queue, **6,756 KiB/s** with two pieces per peer (×1.75 over
single-piece).

Real swarm (Ubuntu 24.04.3, the same machine, 40 s window): gx-core **9.3–9.4 MiB/s**
against libtorrent 2.0.11 at 6.3–7.1 MiB/s, metadata in 3 s and comparable peer counts.
`make measure-realswarm`: 276 MB in 90 s (75 peers), 793 MB in 180 s (80 peers), 1.08 GB
in 180 s (85 peers), `utp_syn_drops` 0 everywhere.

**Choices behind these figures.** Choking stays the standard rate-based one: a real
libtorrent leecher is served at full speed on loopback, and the real-swarm half needs an
inbound port this host does not have. Super-seeding stays opt-in. Multi-piece per peer
shares one in-flight budget, and the write ring buffer keeps the disk from becoming the
bottleneck.

### 4.7 DHT

**Current figures** (`GX_MEASURE_DHT=2m GX_MEASURE_DHT_INFOHASH=<Ubuntu>`): a fresh node
fills the table to **1,290 nodes in ~80 s**, runs 1,432 `get_peers` with 314 replies
(**21.9 %**) and returns 2 peers for the Ubuntu infohash (1 confirmed by more than one
subnet). A 5-minute Debian run settles at ~296 nodes (peak 525) with 4,703 queries and
1,670 replies (**35.5 %**), `subnet_share_max` 4–7 and `nodes_insecure_id` ~42 %.

**Choices behind these figures.** The routing table is not concentrated, so the
anti-Sybil cap stays a hint, off by default. Multi-path `get_peers` is closed: the
instrumentation exists, but `peers_confirmed_multi` was 0 in both real runs. A torrent
resolves its metadata in ~49 s with 12 peers; the rest is the routing-table fill (~30 s
cold), not the lookup.

### 4.8 Merge on real data

**Current behaviour.** Phase 3 reuses at **16 KiB block** granularity: the verified spans
of a partly covered destination piece are written and read back, and only the missing
blocks are requested; the piece is verified whole. Safe because BEP 52 leaves are blocks
of the file, not of the piece.

**Current figures** (real Debian and Arch ISOs, source 75 % complete, one source piece in
four corrupted):

| Run | From the network | Time |
|---|---|---|
| Debian 756 MiB | 198,180,864 B (25.0 %) | 1.31 s |
| Arch 1.5 GiB | 410,255,360 B (25.0 %) | 2.71 s |
| Debian with a libtorrent 2.0.11 seeder | 25.0 %, complete | — |
| resume after a restart | 25.0 % | 1.00 s |
| sibling added later | 25.0 % | 0.91 s |

All destination pieces were reused at block level and passed their hash. With nothing
reusable, the candidate filter costs 115 ns and 0 allocations over 32 unrelated session
torrents.

### 4.9 Other measurements

**Statistics page** (standalone daemon, 0 torrents): the new sample and lifetime folding
are free (`42 ns/op` and `6.4 ns/op`, 0 allocs); a graph request pays 11.3 µs / 20 KiB / 1
alloc and the page translation 255 µs / 89 KiB / 1,919 allocs. Over 30 s idle the daemon spends about **91 CPU ticks** (+20 ms against the version
without the page), and with the page open polling every 3 s the same ticks: within
noise. The binary grew **+52 KiB**; the three history rings are ~45 KiB.

**RSS term matching**: the rule preview (up to 5,000 archived items) and every feed poll
went from 250 to **142 ns/op, 0 B, 0 allocs**; the first normalising versions cost
395–1,590 ns.

**Crash-safe adds and engine-aware settings**: the add intent written before every engine
add costs **76–79 µs, 3.2 kB, 44 allocs** per started download (negligible next to the
engine add); the per-torrent default peer cap is one engine command per torrent, only at
add, session reload or when the value changes. Live service 12 minutes after a restart:
`gexttod` RSS 70.7 MB, `gx-torrent` 104.6 MB — no regression.

**Web UI refresh (gx-torrent page)**: the 2 s table refresh ships the rows as JSON
(`GET /ui/rows`, same `uiPageData`) and renders them with a client template verified
cell-by-cell against the server one; filters, speeds and status still travel as
translated HTML (`/ui/live?notable=1`). On the live service: `/ui/rows` **3,432 B** +
notable **6,399 B** = **9,831 B per tick**, about 1.7 KB per row. Empty sessions and
limited windows use the server HTML, pinned by `TestUIRowsJSON` and the i18n guards.

**Prometheus metrics**: one scrape is **~0.24 ms, 121 KB, 813 allocs** (≈0.001 % of a core
at 15 s), computed only when scraped.

**Robustness at no cost**: Torznab answers are decoded leniently (a raw `&` or an HTML
entity in a title), **2.22–2.32 ms** against **2.24–2.32 ms** strict on a 100-item answer
(identical allocations); the uTP header refuses inputs shorter than 20 bytes instead of
panicking, the bencode screen bounds lengths and depth before the decoder, and the mmap
fault guard turns a fault into an I/O error with the same write cost (35.9–37.5 µs,
0 alloc).

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

The automated baseline passes: **14 axe-core/Playwright tests**, and the gx-torrent pages
had **0 violations** in the audit (light and dark theme, sample torrents, feeds, rules
and wanted titles). The method, the manual checklist, the acceptance criteria and the
known limits are in [`ACCESSIBILITY.md`](ACCESSIBILITY.md).

### Backup and restore

Round trip on a copy of the live data: **all 40 tables of the four databases identical**
and integrity ok, with the default recycle bin, the probe leftovers and the run marker
excluded. A restore on another machine starts with only the data and state folders; an
unavailable download, temp or trash folder is a **warning** and is fixable from the
interface. Verified in a systemd container
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
