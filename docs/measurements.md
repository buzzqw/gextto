# Measurements and verification of Gextto, gx-torrent and gx-core

The numbers we use to decide, with the machine they were taken on and the command to
repeat them. The figures to beat are in §2, the benchmarks in §3, the investigations and
their decisions in §4, the verification that is not a benchmark in §5.

Everything is measured on an Intel N97 mini-PC with 16 GB of RAM unless stated
otherwise.

The state of the product and the open items are in [`evoluzione.md`](evoluzione.md);
the engine is in [`gx-torrent.md`](gx-torrent.md) (inventory
[`../internal/gxcore/GEXTTO.md`](https://github.com/buzzqw/gextto/blob/main/internal/gxcore/GEXTTO.md)) and its BEP compliance
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
| `make measure-realswarm GX_MEASURE_REAL=<public.torrent>` | a public torrent with DHT, trackers and uTP (`GX_MEASURE_REAL_SECONDS`, default 120 s): peers, uTP split, socket counters. A comma-separated list runs all the torrents together, all active, and adds the CPU of the process and the CPU per 100 MB. A run that sees no peer fails, so it cannot pass hollow. The test daemons tick every 50 ms; `GX_TEST_TICK=3s` gives them the production tick, without which the per-tick statistics and peer snapshots weigh about 60 times more in a profile than they do in the service |
| `make measure-merge3` | Swarm Merging Phase 3 on real ISOs (§4.8) |
| `make measure-dht` | the public DHT: routing-table fill and query success (`GX_MEASURE_DHT=5m`, `GX_MEASURE_DHT_INFOHASH=<40 hex>`) |
| `TestMeasureLatencyGuard` | the latency guard against a second instance and a competing download on the real line (`GX_MEASURE_LATENCY_GUARD`, `GX_MEASURE_COMPETITOR_URL`) |
| `TestMeasureManagerLanes` | Jackett/Prowlarr lanes against the aggregate on the live managers (`GX_MEASURE_LANES=<data dir>`) |
| `TestMeasureManyFiles` | descriptors, peak RSS and check time of one torrent with many files already on disk (`GX_MEASURE_FILES=500,2000,5000`; run it niced and memory-capped, see §4.9) |
| `TestMeasureManyFilesDownload` / `TestMeasureRealManyFiles` | a torrent of many files downloaded at full speed from a second daemon over loopback (`GX_MEASURE_DL=files:avgsize`) or from the real swarm (`GX_MEASURE_REAL_MANY=<torrent>`), data under `GX_MEASURE_DIR` on a real disk, stopping by itself if the host is under strain (§4.9) |
| `TestMeasureMergePhase3` | block-granularity merge on real ISOs (`GX_MEASURE_MERGE3=<dir>`, `GX_MEASURE_MERGE3_PEER` for a libtorrent seeder) |
| unchoker simulation (`internal/gxcore/internal/unchoker/sim_test.go`) | the real `Unchoker` driven by synthetic peers: fast downloaders in download, fast uploaders in seed, immediate fast unchoke, optimistic unchoke that does not steal a slot and rotates, budget respected, fairness |
| `TestMeasurePipeliningLatency` | an in-process latency proxy before the seed (no `tc`/`netem`, no root): every chunk released at arrival plus latency, in order and without limiting bandwidth |
| `scripts/holepunch-netns-test.sh` | BEP 55 end to end between two clients behind a stateful firewall, with a public relay (`sudo`, network namespaces, `--keep` to debug) |

## 2. Current baselines

The numbers a change is compared against. Unless noted, measured 2026-10-17 on the N97
with the live service or a standalone instance as stated.

| Area | Figure today | Build | Reproduce with |
|---|---|---|---|
| gx-torrent idle | **38.8 MB RSS** (23.0 MB anonymous, peak 39.1 MB), **0.58 %** of a core over 240 s; 2026-10-26 | 260 | live service, 2 seeds and 1 download, full IP filter (172,740 rules); `GX_TORRENT_PPROF` CPU profile, `/proc/<pid>/status` |
| gx-torrent idle after a burst | **550 MB → 35–40 MB RSS** after 5 min below the slow-rate threshold; log line `memory released` | 248 | 7 torrents (4 stalled, 3 seeds), full IP filter; `/proc/<pid>/status`, `gx-torrent.log` |
| gx-torrent under load | **44–48 % of a core at 8–9 MB/s**, ~0.35–0.43 CPU per peer; picker 9 % of the profile | 226 | the peer-reader load test (9–10 releases, Auto, collector every 5 s, pprof 45 s) |
| gx-torrent under load, public ISOs (2026-10-26) | **2.0–4.3 s of CPU per 100 MB**, 18–37 % of a core at 8.2–8.8 MB/s, 116–132 peers; the spread between runs of the same binary is wider than the change measured (§4.1) | 261 | 5 public ISOs (Arch, Debian DVD and netinst, Ubuntu desktop and server) for 180 s, automatic peer cap, `GX_TEST_TICK=3s`, data under `~/.cache` |
| gx-torrent loopback transfer (2026-10-26) | **234–274 MB/s, 7.5–8.3 s of CPU per 1.09 GiB** for seeder and leecher together, 100 MiB peak RSS | 261 | `GX_MEASURE_DL=8:134217728`, `GOMAXPROCS=2`, `CPUQuota=200%`, three interleaved rounds against the previous build |
| Automatic peer cap | **55 per torrent, 300 in all** on this 4-core/16 GiB host; with 9 active torrents **−36 % peers and −12 % CPU** against a fixed 55 per torrent, throughput within the swarm noise (−4 %) | 1934 (engine 245) | `GX_MEASURE_AUTO=1` / `GX_MEASURE_MAXCONN`, 9 public ISOs together, 150 s × 2 interleaved rounds (§4.1) |
| Memory under load | RssAnon peak **1105 MB** with 10 torrents and the NFS write cache; **507 MB outside the two caches** | 226 | same load, `pprof` heap at the end |
| gexttod at rest | **46 CPU ticks/min (~0.7 %)**, 0 directory opens per 20 s, no write probe on the NAS | 1785 | `GEXTTO_PPROF`, `strace`, `/proc` over 60 s |
| gexttod at rest (2026-10-26) | **0.35 %** of a core over 240 s, 6.8 MB live heap, 73 MB RSS | 2035 | live service, `GEXTTO_PPROF` CPU profile and heap |
| gexttod and gx-torrent at rest, first pass | gexttod **0.42 %** of a core over 120 s, **70.7 MB RSS** (1.26 % and 69.3 MB in the first minute, while start-up work runs); gx-torrent **0.76 %**, **28.4 MB RSS** | 2073 / engine 264 | live service, CPU ticks from `/proc/<pid>/stat`, `VmRSS`; no regression against the 2035 / 260 figures above |
| gexttod and gx-torrent at rest, second pass | gexttod **0.43 %** of a core over 120 s, **72.2 MB RSS** five minutes after start with no cycle yet (83.4 MB after a cycle ran: the Go heap keeps the peak, not a leak); gx-torrent **0.78 %**, **30.5 MB RSS** | 2075 / engine 264 | same measurement; the archive's second read handle costs about 1.5 MB at rest on this archive |
| gexttod and gx-torrent at rest, end of the second pass | gexttod **0.46 %** of a core over 120 s, **71.1 MB RSS**; gx-torrent **0.78 %**, **30.3 MB RSS** | 2078 / engine 265 | same measurement after the uTP timer change: no regression in either process |
| gexttod restart: translation merge | **0.85 s CPU, 45 MB, 1.03 M allocs → 3 ms, 2.5 MB, 35 allocs** when the bundled catalogs did not change | 2037 | `BenchmarkSeedDefaultTranslationsReseed` |
| Web UI CPU per request (live) | Series **112 → 37 ms**, log tail (refreshed every 5 s) **28 → 5 ms**, dashboard **25–36 → 18–19 ms**, archive 39–40 ms, downloads 10 ms | 2038 | `/proc/<pid>/stat` ticks over 30 requests per page, N97, 55 series, 1,863 episodes |
| gexttod in the first minutes | **0.39 % of a core**, stale-temp sweep 0.21 s, 8 new TLS connections in 300 s | 1790 | profile from start, `strace -e connect` |
| Feed archive | **258 MB**, 307,477 rows after the 2026-10-21 dedup (was 480 MB, 610,254), one row per listing; Archive page 7–23 ms | 1935 | live `gextto_archive.db`, `dbstat`, `/api/archive` |
| Archive insert | **40.9–41.8 ms per 1,000 releases** (3.00 MB, 50,528 allocs) | 1791 | `BenchmarkArchiveSaveBatch` |
| Archive search (standalone) | **7.4 ms** on 100,000 releases, 49× faster than before | — | `BenchmarkFeedArchiveSearch` |
| Archive content-filter page | the candidate scan used to copy the `magnet` text of **all 364,348 rows — 138,090,257 B (~131.7 MiB) — per request**; now it reads `id,title` (28.8 MB of titles) and loads the ≤500 visible rows with one `id IN (…)` query | 2072 | live `gextto_archive.db`: `SELECT count(*), sum(length(magnet)), sum(length(title)) FROM archive` |
| Piece picker | `RarestManyPeers` **10.5–11.1 µs**, bitfield update+disconnect **35–37 µs**, one departure on 27,250 pieces **95–126 µs** (was 413–440), sequential pick near the end **4.6 ns** (was 110 µs) | 2094 | `go test -bench . ./internal/gxcore/internal/piecepicker` |
| uTP loopback | **131–139 MB/s**, 5.6 KB and 99 allocs per op | 224 | `BenchmarkTransferLoopback` |
| Storage | **~5.8 GB/s** buffered, **~7.4 GB/s** mmap; 16 KiB writes on NFS **81 MiB/s** | — | `BenchmarkStorageWrite`, `BenchmarkStorageWriteMmap`, NFS harness |
| Indexer lanes | 30 titles, 6 at a time: **110 s**, 205/0 requests, 5,935 releases (aggregate: 450 s, 35/25, 2,737) | 1796 | `TestMeasureManagerLanes` |
| Latency guard | alone **8,503 KiB/s kept**; with a competing download the other device gets **+55 % (router counters)** or **+66 % (trials only)** | 1794 | `TestMeasureLatencyGuard` |
| Merge on real data | **25.0 % from the network** (Debian and Arch ISOs), also after a restart and in any add order | — | `TestMeasureMergePhase3` |
| DHT | **1,290 nodes in ~80 s**, 314/1,432 queries answered (21.9 %), peers found for a real infohash | — | `make measure-dht` |
| Real swarm throughput | **9.3–9.4 MB/s** on the Ubuntu ISO over an FTTC 100/15 line (100 Mbit/s down, 15 Mbit/s up), against 6.3–7.1 MB/s of libtorrent on the same swarm (one campaign) | — | `make measure-realswarm` |
| Web UI rows | **9,831 B per tick**, ~1.7 KB per row | — | `/ui/rows` on the live page |
| Web UI transfer (2026-10-21, build 1945) | dashboard **33.3 KB → 8.0 KB**, downloads **70.7 KB → 12.3 KB**, archive **473.2 KB → 38.8 KB**, maintenance **118.7 KB → 17.2 KB** gzipped; CSS+JS **285.2 KB → 75.3 KB** and cached immutable (repeat request **304, 0 bytes**) | 1945 | N97, live service: `curl -H 'Accept-Encoding: gzip' -w '%{size_download}' 'http://127.0.0.1:5000/?view=archive'`; asset revalidation with `If-None-Match` |
| Prometheus scrape | **~0.24 ms, 121 KB, 813 allocs** per scrape | 232 | `BenchmarkMetricsScrape` |

## 3. Benchmark reference

Micro-benchmarks with `-benchmem`; run them with `-count=6` and compare with `benchstat`
when a change is meant to be an optimisation. A single pass is noise.

| Benchmark | Current figure | Note |
|---|---|---|
| `BenchmarkEngineLoggerFiltered` vs `Forwarded` | **36–38 ns/op, 40 B, 1 alloc** vs 778–799 ns/op, 424 B, 6 allocs | the level methods (`Debug`, `Debugf`, …) now check the threshold **before** `fmt.Sprint`/`Sprintf`, so a record the handler would drop costs no formatting; the remaining alloc is the variadic slice of the call itself, which `DebugEnabled()` removes where the arguments are expensive. Before the change: 151–175 ns/op, 3 allocs/op. Measured 2026-10-28 on the N97 (`go test ./internal/gxcore/logging/ -run XXX -bench BenchmarkEngineLogger -benchmem -count=3`) |
| `BenchmarkBlocked` (IP filter) | 18.6 / 37.7 ns/op, 0 allocs | sorted merged intervals with binary search |
| `BenchmarkLoad` (IP filter, 172,000 P2P and eMule rules) | 56.5–58.6 ms, 39.7 MB, 882,032 allocs → **32.6–33.1 ms, 6.6 MB, 32 allocs** | ranges parsed on the bytes of the line instead of `strings.Split`; `FuzzParseCIDRMatchesReference` holds it to the old parser (4.95 M lines in 60 s); measured 2026-10-26 on the N97 |
| `BenchmarkV2HighlightLogLine` vs `…Regexp` | **0.91–0.95 µs, 576 B, 2 allocs** vs 58.6–67.3 µs, 2.3 KB, 18 allocs per line | log page highlighting: one word scan instead of six `\b` regexps; output identical on 36,521 live log lines and a 45 s fuzz; measured 2026-10-26 on the N97 |
| `BenchmarkSeedDefaultTranslationsReseed` | 841–1,025 ms, 45.3 MB, 1,027,628 allocs → **3.1–3.7 ms, 2.5 MB, 35 allocs** | a restart with unchanged catalogs only hashes them; measured 2026-10-26 on the N97 |
| `BenchmarkPickerRarestSteady` | ~105 / 143.5 µs/op, 0 allocs | piece picker, steady state |
| `BenchmarkPickerSmallestGapWebseeds` (15,000 pieces) | 238–246 µs, 64 KB, 1 alloc → **79–81 µs, 0 allocs** per pick | the pick every peer makes while a webseed downloads: gaps into reused buffers, pieces read in place, counting sort on the gap length; measured 2026-10-26 on the N97 |
| `BenchmarkTickUnchokeSeeding` | 1.3 / 1.64 µs/op, 1 alloc | unchoker tick; 50 synthetic peers ≈1.1 µs, 896 B, 1 alloc |
| `BenchmarkPrefillFromUnrelatedV2` (16 candidates) | 1.49 / 1.58 µs/op, 128 B, 16 allocs | one-shot cost of the Phase 2 merge scan; no steady-state path |
| `BenchmarkMergeCandidatesUnrelated` (32 torrents) | ~265 ns/op, 0 allocs | filter paid when nothing is reusable |
| `BenchmarkNewConnID` (uTP) | 31 ns/op, 0 allocs | the old version built a 64K-entry table per dial (512 KiB, 152,353 ns/op) |
| `BenchmarkTransferLoopback` (uTP, 16 KiB block) | 131–139 MB/s, 5.6 KB, 99 allocs | connection-id probe, lazy `Event` channels, reusable read window, `netip.AddrPort` key |
| `BenchmarkTransferLoopback` after the single RTO timer (2026-10-28) | **151–159 MB/s, 4.0 KB, 75 allocs** against 130–161 MB/s, 5.5 KB and 99 allocs of the previous commit, measured alternately on the same host (`-count=3` each) | one `time.AfterFunc` per packet replaced by one timer per connection armed on the oldest outstanding packet; the first version scanned every unacknowledged packet per write and lost 30 % throughput, the front-only read restored it |
| `BenchmarkUTPConnRemoteAddr` | 113 ns/op, 40 B, 3 allocs | addresses converted once at wrap |
| `BenchmarkStorageWrite` / `StorageWriteMmap` | ~5.8 / ~7.4 GB/s | the disk is far above any network speed: `O_DIRECT` and io_uring are not justified |
| `BenchmarkPieceWriterRunV2` (4 MiB piece) | **1.93–1.95 ms, 2151–2177 MB/s, 32,832 B, 12 allocs** (3.47–3.55 ms, 1181–1207 MB/s, 14 allocs before) | one hash per piece: verification dispatches on the layout (`Piece.Verify`) instead of running SHA-1 and then the v2 merkle tree; v1 unchanged; measured 2026-10-30 on the N97 (`go test -run XXX -bench BenchmarkPieceWriterRun -benchmem -count=3 ./internal/gxcore/internal/piecewriter/`) |
| `BenchmarkArchiveSaveBatch` (1,000 releases, ~400 B magnets) | 45.2–46.1 ms, 3.09 MB, 50,536 allocs (44.0–44.4 ms before the guid identity); with the magnet ahead of the guid 43.9–45.0 ms against 44.8–46.1 ms, interleaved runs on a busy host | prepared statements per chunk; measured 2026-10-21 on the N97 |
| `BenchmarkFeedArchiveSearch` (100,000 releases) | 7.4 ms | compact index on bbolt |
| `BenchmarkMetricsScrape` (one torrent) | ~0.24 ms, 121 KB, 813 allocs | computed only when scraped |
| `BenchmarkCriteriaMatch` (RSS rule, 4 names) | 142 ns/op, 0 B, 0 allocs | scans the title in place |
| `BenchmarkRateHistoryAdd` / `LifetimeAccumulate` | 42 / 6.4 ns/op, 0 allocs | the statistics page costs nothing on the sampling path |
| `BenchmarkRateHistorySnapshot1h` / `StatsPageTranslate` | 11.3 µs, 20 KiB, 1 alloc / 255 µs, 89 KiB, 1,919 allocs | one snapshot and one page translation per graph request |
| `BenchmarkSeedPolicyPass` (10 seed rules, 100 torrents) | 41.4 µs, 5.2 KB, 128 allocs per pass | what the seed rules add to one `EnforceSeedPolicy` pass (every few seconds), plus one `torrent_meta` query only when rules exist; measured 2026-10-17 on the N97 |
| `BenchmarkMergeDuplicateReleases` (24,000 releases, 8,000 copies) | 48–54 ms, 48.2 MB, 192,198 allocs (the input copy alone is ~15 ms, 13.6 MB); the previous key-only dedup on the same input 45–52 ms, 37.7 MB, 120,002 allocs | one pass per cycle and per search; identities are computed once per release, so the extra name/size match costs about the same CPU and ~10 MB of short-lived memory; measured 2026-10-24 on the N97 while the test suite was running |
| `BenchmarkPayloadContentPath` (2,000-file torrent) | Original 207 ns, 64 B, 1 alloc; NoSubfolder 1.65 µs, 0 allocs | the `content_path` every list refresh now computes per torrent (content layout); measured 2026-10-17 on the N97 |
| `BenchmarkNamingTags` (audio + HDR label for one name) | 16.5 µs/op, 2.08 KB, 33 allocs | once per episode at the 6-hour library check (~21 ms for 1,278 files); keeps the existing spelling so the check does not rename a file back and forth; measured 2026-10-25 on the N97 |
| `BenchmarkSearchProgressFanOut` (one indexer, episode panel vs cycle) | 280–325 µs/op and 279–412 µs/op, ~19.5 KB, 293 allocs/op both | what following a manual episode search adds to its fan-out: one context lookup per search, then an atomic count and a callback per answered source; the difference stays inside the noise of the indexer round trip; measured 2026-10-29 on the N97 (`go test -run XXX -bench BenchmarkSearchProgressFanOut -benchmem -count=3 .`) |

### Second performance pass (2026-10-28, N97)

Figures **before** and **after** each change of the second pass. Same benchmark, same
machine, three runs each; the picker rows were also measured on the previous commit in
a separate worktree, so the comparison is not between two machines.

| Benchmark | Before | After | What it measures |
|---|---|---|---|
| `BenchmarkFindSeriesMatchUnmatched` (500 series, one alias each) | 3.63–3.71 ms/op, 1.52 MB, 33,500 allocs | **120–121 µs/op, 8.9 KB, 518 allocs** | resolving a release nobody monitors: the configured names and aliases are indexed once per series list instead of being re-normalised for every release |
| `BenchmarkFindSeriesMatchMatched` (year suffix, last series) | 2.72–2.90 ms/op, 987 KB, 29,473 allocs | **128–132 µs/op, 8.5 KB, 515 allocs** | same scan when the release is monitored but the exact pass misses |
| `BenchmarkRescore` (2,000 tracked torrents) | 215–237 ms/op, 6.3 MB, 84,153 allocs | **54–60 ms/op, 6.2 MB, 82,264 allocs** | one full re-score: one transaction per 500 rows with prepared statements, instead of three autocommit `UPDATE`s per torrent (one fsync each with `synchronous=FULL`) |
| `BenchmarkArchiveGaps` (50 series, 2 seasons, 40 episodes each) | 5.74–6.77 ms/op, 522 KB, 17,580 allocs | **4.36–4.57 ms/op, 515 KB, 17,164 allocs** | one gap calculation: the season targets and the present episodes now come from a single pass, not two scans (one of them an aggregate SQLite had to sort) |
| `BenchmarkSyncArchiveFileScored` (existing episode, file present) | 134–137 µs/op, 2.0 KB, 70 allocs on the RAM disk; **1.63–1.70 ms/op on the NVMe** | unchanged on purpose (the single-record path keeps one commit) | recording one library file: four statements, each its own transaction, plus the stat of the present copy |
| `BenchmarkSyncArchiveFilesScored` (500 files of one series) | one commit per file: 135 µs/file on the RAM disk, 1.63–1.70 ms/file on the NVMe | **20 µs/file on the RAM disk, 25–39 µs/file on the NVMe** | the same scan batched in one transaction per series: the library rescan of 2,000 files goes from about 3.3 s to about 60 ms of commits on the NVMe |
| `BenchmarkEngineListSnapshot` (2,000 torrents) | 1.42 ms/op, 1.00 MB, 4 allocs | **593–610 µs/op, 1.00 MB, 1 alloc** | one read of the torrent snapshot: the sort is kept with the cache and rebuilt only when the cache changes, so a read is a copy |
| `BenchmarkEngineSnapshotTwelveCalls` (2,000 torrents, the reads of one 750 ms tick) | 12 copies and 12 sorts of the whole table | **7.1–7.4 ms, 12.3 MB, 12 allocs** per tick | what the worker's monitors pay together; the reads also no longer wait for a poll in flight |
| `BenchmarkArchiveMovieQualityPerCall` → `PerCandidate` (200 movie candidates, archive of 800 files) | **17.4–18.2 s, 434 MB, 5.47 M allocs** (one walk of the archive and one stat per file, per candidate) | **139–146 ms, 3.1 MB, 45,900 allocs** (one walk per cycle, shared by every candidate) | deciding what to do with 200 movies: the archive volume is read once instead of once per film |
| `BenchmarkArchiveReadDuringFullScan` (50,000 releases, one reader scanning while `Count` runs) | **116–122 ms/op, 32.8 MB, 549,784 allocs** | **257–349 µs/op, 618 B, 17 allocs** | a short read while a full scan runs: with one connection the counter waited for the scan; the archive now has a second read handle |
| `BenchmarkPickerRarestSteady` | 112.7–118.6 µs/op, 0 allocs | **14.6–35.7 ns/op, 0 allocs** | one piece pick when the rarest pieces have one holder: the minimum is found by scanning with an early exit instead of sorting the table |
| `BenchmarkPickerRarestShuffled` (worst case) | 1.12–1.16 ms/op, 0 allocs | **337–349 µs/op, 0 allocs** | same pick with the availability order fully scrambled, so the sort could not be adaptive |
| `BenchmarkPickerSequentialNearlyDone` | 106.8–112.9 µs/op | 106.7–113.7 µs/op (unchanged path) | sequential pick, not touched by the change: it fixes the noise floor of the comparison |
| `BenchmarkPickerRarestManyPeers` (150 peers) | 12.8–13.0 µs/op, 0 allocs | 12.9–13.2 µs/op, 0 allocs | one pick in a dense swarm; the first version of the scan was 20 µs here and was corrected before landing (the rarity is compared before asking the peer's bitfield) |

Reproduce with `go test -run XXX -bench '<name>' -benchmem -count=3`; the picker
figures come from `internal/gxcore/internal/piecepicker`, the others from the root
package. The old side of the picker rows was measured in a worktree at commit
`c52a1a71` so both sides ran on the same host.

### Per-torrent limits round (2026-10-29, N97)

The per-torrent limits fix (an omitted field keeps its value, and the cache shows the
new values at once) touches only cold paths: the limits request itself and the cache
update that follows it. The read paths were re-measured anyway, build `1.2.2085`, three
runs each, to show they did not move:

| Benchmark | Result | What it measures |
|---|---|---|
| `BenchmarkEngineListSnapshot` (2,000 torrents) | **641–656 µs/op, 1.00 MB, 1 alloc** | one read of the torrent snapshot: unchanged from the 593–610 µs of the second pass, still one allocation |
| `BenchmarkEngineSnapshotTwelveCalls` (2,000 torrents, the reads of one 750 ms tick) | **6.9–7.6 ms, 12.3 MB, 12 allocs** | the twelve reads of one tick: unchanged |

Reproduce with
`CGO_ENABLED=0 go test -run '^$' -bench 'BenchmarkEngineListSnapshot|BenchmarkEngineSnapshotTwelveCalls' -benchmem -count=3 .`

### Piece picker: peer departure and sequential frontier (2026-10-29, N97)

A peer leaving used to walk every piece of the torrent (three set removals and a
bitfield test per piece); it now visits only the bits the peer holds, since its
downloads are closed before the departure reaches the picker. The sequential pick
starts from a cursor past the finished prefix instead of index 0. The two unsorted
copies of the piece table (16 bytes per piece) are gone, and with them
`BenchmarkPickerRarestShuffled`, which measured a sort that no longer exists. Same
machine, six runs each, `GOMAXPROCS=2`, before and after on the same checkout:

| Benchmark | Before | After | What it measures |
|---|---|---|---|
| `BenchmarkPickerDisconnect` (27,250 pieces, peer holds half, 50 peers) | 413–440 µs/op, 0 allocs | **95–126 µs/op, 0 allocs** | one peer departure on the torrent loop |
| `BenchmarkPickerBitfieldManyPeers` (4,000 pieces, 150 peers) | 57.0–62.5 µs/op, 1 alloc | **35.4–37.0 µs/op, 1 alloc** | a new peer's bitfield arriving, then its departure |
| `BenchmarkPickerSequentialNearlyDone` (27,250 pieces, all but the last done) | 106–115 µs/op | **4.5–4.7 ns/op** | one sequential pick near the end of a download |
| `BenchmarkPickerRarestManyPeers` (150 peers) | 12.1–12.8 µs/op | 10.5–11.1 µs/op | one rarest pick in a dense swarm (index order without the pointer table) |
| `BenchmarkPickerRarestSteady` | 8–46 ns/op | 11–35 ns/op | rarest pick with an early exit: unchanged, noise |

Reproduce with
`go test -run '^$' -bench 'BenchmarkPicker' -benchmem -count=6 ./internal/gxcore/internal/piecepicker/`

### Peer writer: small messages batched into one write (2026-10-29, N97)

Every protocol message used to be its own `Write`: a refill of 250 block requests
(17 bytes each) was 250 syscalls and, with `TCP_NODELAY`, 250 segments. The writer now
takes the run of small messages at the head of its queue in one hand-off and sends it
with one `Write`; block uploads still go one by one. `BenchmarkPeerWriterRequestsTCP`
sends 400 refills of 250 requests over loopback TCP; the syscalls were counted with
`strace -f -c` on the test binary of the previous commit and of this one. The wall
times were taken separately, on 2026-10-30 with the machine quiet (load average
1.7–2.6), running the two binaries alternately six times each (`GOMAXPROCS=2`): the
first pass, with the machine loaded above 20, was noise.

| Measure (100,000 requests) | Before | After |
|---|---|---|
| `write` syscalls | 100,309 | **1,972** |
| all traced syscalls (`write`, `read`, `futex`, …) | 144,329 | **4,842** |
| writes per message, `BenchmarkPeerWriterRequests` (counting conn, 6 runs) | 1.000 | **0.014–0.021** |
| heap per refill of 250 | 27.4 KiB, 750 allocs | **15.6–15.9 KiB, 500 allocs** |
| time per refill of 250 over loopback TCP, `BenchmarkPeerWriterRequestsTCP` | 1.70–2.85 ms | **0.28–0.46 ms** |

Reproduce with `go test -c ./internal/gxcore/internal/peerconn/peerwriter/` and
`strace -f -c -e trace=write,read,futex ./peerwriter.test -test.run '^$' -test.bench RequestsTCP -test.benchtime=400x`.

### Seeding dials leechers (2026-10-29, N97)

A completed torrent never opened an outgoing connection: it kept the peers a tracker sent
only while downloading, so it could serve only peers that connected to it, and a leecher
behind a NAT never got the data. The change dials up to `seedDialBudget` (10) candidates
while seeding, asks for peers on the completed announce, and remembers the peers that
answer with a full bitfield so a seed is not dialed twice. The dial path is cold (a few
addresses per announce); the harnesses that exercise serving were re-run on the checkout
that became build `1.2.2096`:

| Harness | Result | What it measures |
|---|---|---|
| `make measure-seeding` | 2 MiB, 3 leechers: **3.00× corpus** in 24 ms | the local swarm served by the seed |
| `make measure-choking` | libtorrent leecher: **50,331,648 bytes (100 % of the file) in 5.587 s** | a real external client served on its incoming connection |
| `BenchmarkEngineListSnapshot` (2,000 torrents) | **0.76–1.13 ms/op, 1.00 MB, 1 alloc** | one read of the torrent snapshot |
| `BenchmarkEngineSnapshotTwelveCalls` (2,000 torrents) | **9.0–12.1 ms, 12.3 MB, 12 allocs** | the twelve reads of one tick |

The snapshot benchmark allocates exactly what the 29 October figures in §3 allocated; its
wall time came out 20–70 % higher, with the daemon stopped just before the run and the
machine still settling. The patch does not touch that path, so the difference is the host,
not the change. The real-swarm half of the campaign is still open: what is missing is a
public torrent with leechers that only a seed's outgoing dial can reach.

Reproduce with `make measure-seeding`, `make measure-choking` and
`CGO_ENABLED=0 go test -run '^$' -bench 'BenchmarkEngineListSnapshot|BenchmarkEngineSnapshotTwelveCalls' -benchmem -count=3 .`

### Seed mode: cost of the lazy check (2026-10-29, N97)

A torrent added in seed mode hashes a piece the first time a peer asks for it: the
first block of that piece waits for one read of the whole piece and its SHA-1, then
the piece is served from the read cache like any other. `BenchmarkSeedModeLazyHash`
measures that wait with the file in the page cache (the CPU side; a cold read adds
the disk time of one piece), three runs each, `GOMAXPROCS=2`:

| Piece size | Wait before the first block | Throughput | Heap per check |
|---|---|---|---|
| 256 KiB | **0.21 ms** | 1.22–1.25 GB/s | 256 KiB, 3 allocs |
| 1 MiB | **0.74–0.81 ms** | 1.29–1.42 GB/s | 1 MiB, 3 allocs |
| 4 MiB | **3.7–3.9 ms** | 1.08–1.14 GB/s | 4 MiB, 3 allocs |
| 16 MiB | **13.0–13.1 ms** | 1.28–1.29 GB/s | 16 MiB, 3 allocs |

Each piece pays it once per seed-mode session; at most 4 checks run at once per
torrent, so the transient heap is bounded by 4 pieces. The full check that seed mode
skips reads and hashes the whole torrent before the first byte is served.

Reproduce with
`go test -run '^$' -bench BenchmarkSeedModeLazyHash -benchmem -count=3 ./internal/gxcore/torrent/`

### Verification: one hash per write (2026-10-30, N97)

Every piece write verified the buffer with SHA-1 and, when the piece was BitTorrent
v2, hashed it a second time as a BEP 52 merkle tree, throwing the first result away.
The full check and seed mode's lazy check did the same. `Piece.Verify` now dispatches
on the layout, so a piece pays for one digest only. `BenchmarkPieceWriterRunV1`/`V2`
hash and write one 4 MiB piece (the write goes to a discarding file, so the figures
are the CPU of the check plus the pipeline), three runs each:

| Benchmark | Before | After |
|---|---|---|
| `BenchmarkPieceWriterRunV2` | 3.47–3.55 ms, 1181–1207 MB/s, 32,968 B, 14 allocs | **1.93–1.95 ms, 2151–2177 MB/s, 32,832 B, 12 allocs** |
| `BenchmarkPieceWriterRunV1` | 1.55–1.65 ms, 2545–2703 MB/s, 232 B, 3 allocs | 1.54–1.56 ms, 2693–2720 MB/s, 232 B, 3 allocs (unchanged, inside the noise) |

The saving is 1.55 ms per 4 MiB piece: at 100 MB/s of a v2 download (24 pieces per
second) about 3.9 % of a core, at 1 Gbit/s about 4.9 %. The full check and seed mode
cost the same per piece, so they save the same share; `BenchmarkSeedModeLazyHash` above
measures v1 pieces and stays as it is. v1 torrents are untouched.

Reproduce with
`CGO_ENABLED=0 go test -run XXX -bench 'BenchmarkPieceWriterRun' -benchmem -count=3 ./internal/gxcore/internal/piecewriter/`

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
| Idle release after a burst | 550 MB RSS → 35–40 MB after 5 min without downloads (build 248, 7 torrents) |
| Piece picker | 9 % of the profile |

**Choices behind these figures.** The read path uses a progress-based deadline, so a
dead uTP connection fails at once instead of looping; the socket handles each datagram in
the receive buffer and copies only what stays queued; `waitEvents` is a plain select.
The IP filter keeps the parsed rules instead of the file, `GOGC=50` trims the heap
headroom, and the buffers of a finished burst are released. The collector costs 0.5 %
(`gcBgMarkWorker`) and 2.2 % (`mallocgc`) of the profile. After 5 minutes with the
total transfer rate below the queue's slow rate the daemon calls `debug.FreeOSMemory`
(at most every 10 minutes) to
return the 1 MB piece buffers that the per-torrent `sync.Pool`s would otherwise keep
reachable until the next collection: on the N97 the RSS went from 550 MB to 35–40 MB
with 7 torrents and no active transfer.

**Profile under load, 26 October 2026** (N97, five public ISOs together, 180 s, production
tick). Where the CPU goes: syscalls and the Go scheduler's wake-ups about 50 % (one
goroutine per connection, every segment that arrives), RC4 of the MSE stream 9–10 %,
SHA-1 piece checks 5–7 % (with SHA-NI), TLS of the HTTPS webseeds 2–3 %: costs of the
protocol and of the traffic itself. What was the engine's own: zeroing every pool buffer
before use (1.9 %), now skipped where the buffer is overwritten whole — a block read
with `io.ReadFull`, a piece whose blocks cover it (pieces with BEP 47 padding are still
zeroed, `TestPaddedTorrentTransfer`); and the webseed pick, which allocated and sorted the
gap list for every peer pick (2.4 %, 3× cheaper now). The statistics the tick reads sum the done bytes with a
popcount of the bitfield when no file is skipped, instead of a test per piece. Three alternated rounds on loopback
and two on the public swarm do not separate the builds: the public runs of the same
binary spread from 2.0 to 4.3 s per 100 MB with the peers and uTP share they get, so the
gain is stated by the profile share and the micro-benchmarks, not by a throughput figure.
Kept as they are: the per-peer read cache allocates its 128 KiB blocks per upload (it
follows the upload rate, 1–2 MB/s on this line), and RC4 is chosen whenever the peer
offers it, as libtorrent does; plaintext would save that share at the price of the
obfuscation, which is the user's choice (`-encryption`).

**Piece picker.** With ~200 peers it reads the peer bitfield (O(1)), keeps a count per
piece and a counter of high-priority pieces, and touches only the pieces a disconnecting
peer had. On three public ISOs at 9.4 MB/s it is 9 % of the CPU;
`BenchmarkRarestManyPeers` is 13.8 µs and the bitfield update+disconnect 65–68 µs.
Pinned by `TestHoldersMatchAReferenceModel` (20,000 random have/don't-have/disconnect
steps).

**Automatic peer cap** (2026-10-21, N97, build 1932, engine 244). The daemon sizes the
per-torrent cap from the machine — ten connections per core plus fifteen, bounded
to 15–200, halved below 4 GiB of RAM and under 768 MiB available — and trims a
torrent that keeps at least ten peers with no progress for two minutes to half.
It is applied live with `SetMaxConnections` (no session restart) every 30 s; the
configured value stays the ceiling and a per-torrent limit wins
(`cmd/gx-torrent/connauto.go`). Same public swarm (Ubuntu 24.04.3 desktop ISO,
90 s window, two interleaved rounds, `GX_MEASURE_AUTO=1` /
`GX_MEASURE_MAXCONN`), three policies:

| Policy | Peak peers | Downloaded | CPU per 100 MB |
|---|---|---|---|
| automatic (55 on this host) | 25 / 37 | 221 / 604 MB | 4.7 / 3.7 s |
| fixed 200 | 78 / 89 | 314 / 625 MB | 4.5 / 3.9 s |
| fixed 25 | 25 / 25 | 497 / 548 MB | 4.5 / 5.7 s |

A cap in this range costs no throughput — the spread is swarm luck, and one
public pass cannot rank a parameter (§1) — while the established peers, and with
them the absolute CPU (about 0.1 % of a core per peer), stay bounded. The
automatic cap did not bind in these runs (peers stayed below 55), so the table
shows that 25–55 per torrent is safe, not the trimming path: that is covered by
`TestDaemonAutoConnectionsFollowProgress`.

**Session budget with many torrents** (2026-10-21, N97 4 cores/16 GiB, build 1934,
engine 245). The cap of one torrent does not bound the CPU when many run:
the session also has a budget of 75 connections per core (300 here) shared by
weight among the running torrents, and a seed closes seed-to-seed connections at
once. Nine public torrents together (Debian 13.7 netinst and DVD, Kali, Ubuntu
22.04/24.04/25.10 desktop, 24.04 server, Kubuntu, Xubuntu), all active, 150 s
window, two interleaved rounds per policy, CPU from `getrusage` of the test
process (`GX_MEASURE_REAL=<list>`). The live Gextto was downloading on the same
line, so the bytes carry its noise too:

| Policy | Peak peers | Downloaded | CPU (share of a core) | CPU per 100 MB |
|---|---|---|---|---|
| budget (33 per torrent with 9) | 145 / 184 | 1028 / 1234 MB | 26.9 / 41.6 % | 3.9 / 5.1 s |
| fixed 55 per torrent | 243 / 272 | 1235 / 1114 MB | 36.3 / 41.3 % | 4.4 / 5.6 s |

On average the budget holds 36 % fewer peers (165 against 258) and spends 12 %
less CPU (34 % against 39 % of a core, 4.5 against 5.0 s per 100 MB) for 4 % fewer
bytes, inside the round-to-round spread of the same policy (±10 %). Before the
pass ran on a change in the number of torrents, a torrent just added ran
without a cap for up to 30 s: in the first runs of this campaign the automatic
policy peaked higher than the fixed one for that reason.

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

**Profile of 26 October 2026** (build 2035–2037, live service, 3 torrents): at rest
gexttod spends 0.35 % of a core; what is left is the stale-temp sweep after a start, the
latency guard's TCP probes (real network measurements) and the recovery pass that
detaches completed singles, which now remembers a "not to detach" verdict for 30 s
instead of querying the database and stat-ing the archive for every seeding torrent at
every 750 ms tick. The CPU worth taking was in three places: the restart (translation
merge, above), the pages (the Series page matched every gap against every series name
and ran five queries per series; the log tail ran six regular expressions on each of
500 lines every 5 s) and the dashboard's seen-release count, a `COUNT(DISTINCT)` over
64,000 rows now reused for 30 s.

**Memory of a full cycle — 28 October 2026** (Intel N97, build 1.2.2067, live service,
`GEXTTO_PPROF=127.0.0.1:6062`, `/proc/<pid>/status`, the cgroup's `memory.stat` and a
`POST /api/run_now` cycle of 31,275 releases): the process itself is small. At rest
after a start **RSS 65 MB** (42 MB anon, `heap in use` 8.2 MB sampled, 41 goroutines);
during the cycle **RSS/`VmHWM` 232 MB**, anon 155 MB, `heap in use` 38 MB at the
peak of the feed pass (`mergeDuplicateReleases` 10 MB, `parse_feed_body` 14 MB
cumulative), 65 goroutines; after the cycle **`heap in use` 6.8 MB** and 42 goroutines,
RSS settling in the 100–160 MB band while the nine downloads of that cycle are
processed (the instance that had been up 1h45m before this measurement was at 89 MB).
Nothing is retained: the heap in use after the cycle is the same as before it and the
goroutine count returns to its baseline.

The large figure shown by `systemctl status` (1.7 GB, peak 4.5 GB) is **page cache, not
the process**: with the service running for 45 minutes, `memory.current` was 2.0 GB
with `file` 1.9 GB against `anon` 68 MB, and the peak is `file` as well. The daemon
reads the six databases, the 310 MB release archive and the media folders, and the
kernel keeps that in the cgroup's page cache until something needs the memory; it is
reclaimable, it drops by itself during the cycle (302 MB → 69 MB as the heap grew) and
a restart of the unit recreates the cgroup and takes it back to ~0.3 GB. `MemoryMax` on
the unit would count it, so a limit, if one is ever set, must leave room for it.

**gx-torrent at rest** (same session): **1.45 % of a core**, of which 41 % syscalls and
~50 % `runtime.findRunnable` — uTP state packets and sends, incoming MSE handshakes
(`mse.keyPair` 4.6 %) and the DHT loop, which is real traffic. The ~330 `nanosleep`/s are
the Go scheduler's own wake-ups. No spinning loop, nothing to change.

### 4.3 Feed archive

**Current figures** (live `gextto_archive.db`, `dbstat`, `BenchmarkArchiveSaveBatch`):

| | Value |
|---|---|
| Size | 258 MB, 307,477 rows after the 2026-10-21 dedup (was 480 MB, 610,254) |
| Identity | one row per torrent (infohash); the indexer guid for `.torrent`-only listings, the exact link as a last resort |
| Insert | 45.2–46.1 ms per 1,000 releases (3.09 MB, 50,536 allocs; 44.0–44.4 ms before the guid identity) |
| Archive API page (100 rows) | 7–23 ms |
| Standalone feed search | 7.4 ms on 100,000 releases |

**Choices behind these figures.** Uniqueness is on `magnet_key`, the first 16 bytes of
the SHA-256 of the exact magnet: the dedup is unchanged without a 400-byte index. Rows
with the same infohash are the same content, so the newest is kept, base32 hashes are
stored as hex and a partial UNIQUE index enforces it; a known torrent under another
magnet refreshes its row. A `.torrent`-only listing is keyed by the indexer guid
(when present), because Jackett regenerates its `/dl/?path=...` URL at every request;
URL rows with no guid still never merge. Inserts are prepared per chunk
(`INSERT OR IGNORE` plus a conditional refresh), faster than a two-conflict upsert. The plain title index is gone: titles match through FTS or
`lower(title) LIKE`.

### 4.4 Indexer managers

**Current figures** (`TestMeasureManagerLanes`, the live Prowlarr and Jackett, 30
titles, 6 at a time):

| | Value |
|---|---|
| Cycle | 110 s (per title p50 2.1 s, max 100 s) |
| Requests | 205 answered, 0 failed |
| Releases | 5,935 |

**Why one indexer at a time** (same library, same host): an aggregate with a broken
indexer enabled waits for the timeout (60 s) and returns nothing; a healthy indexer
called directly answers in 0.25–2.4 s with 40–100 releases; the aggregate, with the
broken indexer disabled in the manager, answers in 2–4 s with 156–240. Six concurrent
aggregates with timeouts snowball until even a single search waits 120 s, recovered
once the backlog drained.

**indexarr as one aggregate** (27 October 2026, live library on the N97, the run that
changed the behaviour): expanded into one lane per indexarr source, every title became
46 requests and a 104-title search took 3 min 40 s, held back by the two slots of its
slowest source; through indexarr's own `/api` the same title is answered in 2.6 s, the
indexer phase goes from 3 min 40 s to 1 min 23 s and the whole cycle from 12 to 5
minutes.

**Choices behind these figures.** Each manager indexer is a lane, read from the manager
at run time (10 min cache, never copied into the configuration): Prowlarr
`/api/v1/search?indexerIds=`, Jackett the indexer's Torznab endpoint. Prowlarr serves
one search per indexer, so a lane has one slot (Jackett two), its own timeout (90 s
behind Prowlarr, 45 s direct) and its own health; a title skips a lane whose queue would
outlast its 100 s budget. A tracker present in both managers is searched once, through
the faster lane; Prowlarr's failing indexers are skipped; the aggregate is the fallback
when the list cannot be read.

### 4.5 The home line and the latency guard

> **Historical figures.** The numbers below were measured with the trial/router
> design at build 1794. The current controller probes Google/Cloudflare, ramps while
> the ping is good, cuts when it rises, and accepts an optional user line maximum
> (`torrent_line_max_*_kib`); it no longer relies on the router to decide. The test
> remains the gate, but these figures are the historical baseline, not a claim about
> the current code.

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

**Controller on a model line** (`TestLatencyGuardSitsBelowTheKnee`,
`GX_LOG_GUARD_KNEE=1`; 2026-10-26, Intel N97, build 2039). The line answers 20 ms up
to its capacity and 60 ms above it or uncapped; ten minutes (300 probes) after a
one-minute warm-up. Before: the cap climbed +25 % after every cut and jumped back over
the capacity; after: it remembers the knee and sits at 90 % of it, probing once a
minute.

| Line | Ping high, before | Ping high, after | Line used, before → after |
|---|---|---|---|
| 800 KiB/s | 228 of 300 | 66 of 300 | 96 % → 94 % |
| 4,000 KiB/s | 174 of 300 | 29 of 300 | 95 % → 96 % |
| 20,000 KiB/s | 172 of 300 | 6 of 300 | 95 % → 95 % |

The extra cost is a few comparisons per 2 s probe: no measurable CPU or memory.

### 4.6 Swarm: seeding, choking, pipelining

**Current figures.** The unchoker holds its properties on the synthetic harness (fast
downloaders in download, fast uploaders in seed, immediate fast unchoke, optimistic
unchoke that does not steal a slot and rotates, budget, fairness); the tick with 50 peers
costs ~1.1 µs, 896 B, 1 alloc.

Local swarm (2 MiB, 3 leechers): **3.00× corpus** in ~41–42 ms; with mesh the leechers
exchange 2.52 MB and the seed drops to **1.84×**. Super-seeding is opt-in: it does not
reduce the upload on a measurable swarm and costs ~24 % locally.

Re-run of the same harness on 2026-10-21, build 1962: 2 MiB, 3 leechers, **3.00×
in 11 ms**; the choking campaign served a real libtorrent leecher **48 MiB in
4.84 s (100 % of the file)** on loopback. The real-swarm half is still out of
reach: the host has no inbound port, so the harness finds 0 peers. The local
half is the control, not a substitute for that campaign.

Pipelining at RTT 50 ms, 256 KiB pieces: 305 KiB/s with one block in flight, 3.8 MiB/s
with the default queue, **6,756 KiB/s** with two pieces per peer (×1.75 over
single-piece). The same run on 2026-10-21 (build 1962): **302 KiB/s** at one
block, **6,745 KiB/s** at the default depth, and **161.6 / 161.9 / 162.4 MB/s**
on the loopback swarm at shallow/default/deep depth; the loopback pair is at the
local ceiling and does not rank the queue depth.

Real swarm (Ubuntu 24.04.3, the same machine, 40 s window, FTTC 100/15 line): gx-core **9.3–9.4 MiB/s**
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

**Many files in one torrent (file pool, 2026-10-18, N97).** Is a pool of open files
(an LRU, like libtorrent's) needed? The allocator opens every file of a torrent when it
starts and keeps it open while it runs; a stop closes them all. `TestMeasureManyFiles`
(`GX_MEASURE_FILES`, a torrent of 4 KiB files already on disk, run under
`systemd-run --user --scope -p MemoryMax=6G -p CPUQuota=200%`, `nice`/`ionice`,
`GOMAXPROCS=2`; load stayed under 0.6 apart from the compile):

| Files | Descriptors while running | After a stop | Check of the data on disk |
|---|---|---|---|
| 500 | +500 | back to the base | 94 ms |
| 2,000 | +2,000 | back to the base | 150 ms |
| 5,000 | +5,000 | back to the base | 137 ms |
| 12,000 | **failed** before the fix: `too many open files` at 8,762, torrent in error | — | — |
| 12,000 | +12,000 (after the fix) | back to the base | 361 ms |
| 20,000 | +20,000 (after the fix) | back to the base | 406 ms |

The live daemon held 97 descriptors with 6 torrents (largest 71 files). The cost is
one descriptor per file and nothing measurable on the check; the real limit was the
ceiling: `setNoFile` set **both** the soft and the hard `RLIMIT_NOFILE` to
`MaxOpenFiles` (10240), lowering the 65536 the service grants, so a torrent with
more than ~10,200 files (or running torrents adding up to that) could not start. It
now only raises the soft limit. Decision: **no file pool**; the descriptor ceiling of
the service (65536) covers every realistic session. With mmap storage each file is
also a mapping: `vm.max_map_count` is 1,048,576 here, and a failed mapping falls back
to a plain file.

**Many files at full speed, loopback (2026-10-18, N97).** `TestMeasureManyFilesDownload`
(`GX_MEASURE_DL=files:avgsize`, `GX_MEASURE_DIR` on the NVMe, same limits): a seeder
daemon serves a torrent of many files (sizes spread ±50 % around the average, 512 KiB
pieces spanning several files) and a second daemon downloads it over loopback with
nothing throttled. CPU and RSS are the two daemons together.

| Files | Size | Seeder check | Download | Average | CPU | Peak RSS | Peak descriptors | Host load (max) |
|---|---|---|---|---|---|---|---|---|
| 1,000 | 0.15 GiB | 1.4 s | 1.9 s | 85 MB/s | 0.65 core | 100 MiB | 2,013 | 0.6 |
| 6,000 | 0.92 GiB | 5.8 s | 7.9 s | 125 MB/s (peak 140) | 0.88 core | 114 MiB | 12,015 | 1.5 |
| 6,500 | 3.72 GiB | 10.2 s | 23.8 s | 168 MB/s | 1.37 cores | 121 MiB | 13,015 | 1.7 |

The host stayed responsive throughout: available memory never moved from ~11.2 GiB and
the load stayed under 2. Thousands of files cost neither memory nor speed: the rate is
the loopback single-pair ceiling of §2.

The same harness on the real swarm (`TestMeasureRealManyFiles`, Internet Archive item
`talawa-images-archive`, CC0, 16,928 files, 1.24 GB) found no BitTorrent peers: the
data came only from the two archive.org web seeds at ~0.2 MB/s, with the daemon idle
(6 % CPU, 70 MiB, 16,946 descriptors, host load 0.4). archive.org answers one file in
~3 s: `curl` fetched 20 files at 0.08 MB/s one after the other and 0.46 MB/s eight at
a time. The run was stopped after 5 minutes: on that source the limit is the server,
not the daemon.

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

**Auto-tune connection cap**: on a public swarm the bound (50–150 since the
automatic peer cap) is **safe** — a lower
cap used less than half the memory with comparable throughput, and the spread across
caps was swarm luck (one 60 s pass cannot rank a parameter); the controlled local sweep
(8 seeders, 64 MiB, loopback) is **flat** across the cap (99–104 MiB/s), because one seed
source already saturates the path. **The cap bounds connections (memory and CPU), never
the rate.**

**Storage and disk**: buffered **~5.8 GB/s**, mmap **~7.4 GB/s** (ranged mmap sync ~29
GB/s through the page cache), far above any network speed. Removing `O_SYNC` and batching
durability took 16 KiB writes on NFS from **35 to 81 MiB/s** (2.3×), up to ~118× at small
blocks on NVMe, with the invariant that a persisted bit refers to data already fsynced
(`syncPieces()` on the run loop, `writeBitfield()` ordered snapshot → fsync → write).
The SQLite `cache_size` stays at SQLite's default 2 MB: on the real archive DB (510 MB,
FTS5) the latency is flat within noise even on a broad `MATCH` (~1.14 s), because the OS
page cache serves the file — `gexttod` RSS 67.6 MB against 109.7 MB at the same uptime.

**Registering a release again (2026-10-29, N97).** A new download of an infohash
that was already downloaded clears the completion left by the previous attempt
(`beginNewDownloadAttempt`): one indexed read on the normal paths, two reads and one
`UPDATE` when the row still says "completed" and its archived copy is gone.
`go test -run XXX -bench BenchmarkRegisterTorrentScored -benchmem -count=5` (the same
checkout with and without the change; `MarkTorrentCompleted`, which the loop needs to put
the row back into "completed", is outside the timed region):

| Case | Before | After |
|---|---|---|
| First add, no row yet | 115–163 µs | 154–199 µs |
| Re-register on a queued row | 123–145 µs | 149–194 µs |
| Re-register after a completion whose copy is gone | 167–233 µs | 272–320 µs |

The path runs once per started download, not per cycle or per request, and the figures
are dominated by SQLite's per-statement cost on this disk: the added work is one read, or
two reads and one write for a re-download.

### 4.10 Rejected designs and ideas not added

Closed with a measurement, so they are not retried without new data:

| Idea | Why it was closed |
|---|---|
| `io_uring` / AIO | the disk is at 5.8–7.4 GB/s, far above any network speed; Go exposes it only with a dependency or cgo, and the mmap path is already asynchronous |
| Write-back cache in the engine | the storage keeps up with the download (`write_cache=0` in the live service), so there is nothing to absorb; reopen only with storage slower than the network |
| Batched uTP datagram I/O (`recvmmsg`/`sendmmsg`) | measured again on a uTP-heavy path (2026-10-21): on the loopback benchmark `Socket.writeTo` is 44 % of the CPU (58 % is `Syscall6`), but in the real-swarm profile it is **8.3 %** of the samples (36 % syscalls, reads and TCP included). Batching would remove at most that share, while adding a copy per datagram and a sender queue; at the live rate it saves well under 1 % of a core. Reopen only on a uTP-only session above 8 MB/s |
| Contiguous-write coalescing | the page cache already merges adjacent writebacks |
| Rarity buckets, watermark cursor in `pickFileEdge`, `FADV_RANDOM`, `saveLocked` outside `d.mu`, `/proc/meminfo` under `d.mu` | no measurement justified the cost (the `pickSequential` cursor did: §3, *Piece picker: peer departure and sequential frontier*) |
| `PieceDownloader` bitmask instead of its three maps | `BenchmarkPieceDownloaderLifecycle` (one 4 MiB piece, 256 blocks, 2026-10-29, N97): 0.66–1.10 ms/op, **18 KB and 15 allocs per piece**, the time almost all the 4 MiB copy into the piece buffer. At 100 MB/s that is about 450 KB/s of garbage: a bitmask would save nothing measurable |
| Upsert with two conflict targets (archive) | +17 ms per 1,000 releases against `INSERT OR IGNORE` + refresh |
| `idx_archive_title`, expression index on `lower(magnet_hash)` | never used by the queries; only write time and space |
| Multi-path `get_peers` | `peers_confirmed_multi` was 0 in both real runs |
| Anti-Sybil cap on by default | the routing table is not concentrated and legitimate CGNAT/mobile subnets would be capped |
| Choking changes | reciprocation works locally and against libtorrent |
| Super-seeding by default | no upload reduction on the measurable swarms, and ~24 % slower locally |
| Ordering the merge candidates by reusable bytes | superseded by the dedup, no measurable gain |
| Latency guard: fixed 100 MB/s ceiling, current-rate ceiling, 64 KiB/s floor, low-peak anchor | the cap either locked to a slow sample or was an arbitrary number; the current controller ramps on the Google/Cloudflare ping, uses the user's line maximum (or 2× the delivered rate) and cuts only on a real latency rise |

**Not a measurement but a model:** replacing `internal/dht` with a third-party library
was considered and not done — the internal one shares a UDP socket with uTP, chooses
that socket's family and serves every torrent from a single request channel, where an
external library expects its own socket and a traversal per torrent. The state is in
[`evoluzione.md`](evoluzione.md) §6.

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

### eD2k grab note off the request path — 28 October 2026

Intel N97, build **1.2.2067**, command
`CGO_ENABLED=0 go test -run TestAddEd2kReleaseNotesTheGrabToTheIndexer -v -count=3 .`
(fake aMule core and a local indexer that answers with a 302): `addEd2kRelease` returns
in **0.31–0.54 ms** and the indexer note runs in its own goroutine. Before, the note was
awaited inline with a 4 s budget, so a batch of ten eD2k results could add **40 s** to
the request. The test fails if the call waits more than 500 ms.

### eD2k and Kad through aMule — live, 20 October 2026

The native `gx-ed2k` daemon measured in the sections below is retired (its last
build is commit `9f4e2b9`); eD2k now goes through aMule. Its numbers stay
here as the history of the decision.

EC client against a real `amuled` 2.3.3 (Debian trixie package) on the N97,
throw-away `--config-dir`, ports not forwarded (Low ID, Kad firewalled),
`nodes.dat` and `server.met` copied from an existing aMule profile:

- login, status, preferences, server list, add / pause / resume / delete of a
  link, bandwidth limits, log and the refusal of a wrong password all pass
  (`GEXTTO_AMULE_EC=… go test ./internal/amule/ -run Live`, 0.05 s);
- after `Connect`, Kad and the eD2k server (eMule Sunrise, ~47 600 users) were
  up within ~15 s;
- `FBI S06E13`: **14** Kad results and **12** global server results, each
  collected over a 40 s window, with the episode (WEB, HDTV, 1080p variants) at
  1–2 sources on a firewalled client.

**End to end with the service (same day).** aMule 3.1.0 in Docker
(`ngosang/amule`, bridge network, High ID, Kad firewalled), driven by the running
Gextto: a global server search for `eMule0.70b Installer` returned 5 results in
the eD2k page (the installer with 74 sources); `eMule0.70b-Installer.exe`
(4.27 MB) added from the page completed in **~3.5 minutes** (11:57:38 →
12:01:06, aMule log) into the incoming folder. aMule 3.x removed it from the
download queue at once and only shared it, which is why completions are looked
up among the shared files.

One run, one network, not a coverage sample: the coverage question for aMule is
an open point in [`evoluzione.md`](evoluzione.md).

### eD2k/Kad search PoC (loopback)

The search daemon is verified against **deterministic fake peers**, not the live
network: a fake Kad UDP node and a fake eD2k TCP server answer real wire packets
(Kad `BOOTSTRAP_RES`, `REQ/RES`, `SEARCH_RES`; server login and
`OP_SEARCHRESULT`). On the N97, 2026-10-19, build in this checkout:

```bash
go test ./internal/ed2k/ -count=1          # 2.7 s including the UDP/TCP round trips
go test ./cmd/gx-ed2k/ -count=1
go test . -run 'ED2K|Ed2k|MultiEngine' -count=1
```

The tests assert the link parser, both tag byte orders, `nodes.dat`/`server.met`
parsing, keyword hashing, bootstrap, iterative lookup, result parsing, server
login/search, the API contract (`download: false`), the fallback gate and DB
routing. **Live coverage is not measured yet**: that is the Fase 0/Fase 1 gate
in [`evoluzione.md`](evoluzione.md); it needs a sample of real gaps and a manual
Kad search, and its numbers will be added here.

### eD2k/Kad search — live, 19 October 2026

One live run from the N97 (`bin/gx-ed2k`, standalone, UPnP on, `nodes.dat` from
`upd.emule-security.org`):

- **UPnP**: mapping accepted by the router, UDP and TCP mapped, external IP
  `79.40.60.48`; the sensor then reports **limited** because
  `peer_packets=0` — UPnP succeeded but no Kad peer answered, which is exactly
  the "port open, outbound UDP blocked" case it is meant to flag.
- **eD2k servers**: login and HighID (`server_id=809248847`, `low_id=false`),
  `server.met` parsed (16 servers), search `the matrix 1999` returned **14
  results** (name, size, MD4, sources), including Italian releases; the query
  took **10.5 s** with five servers in parallel and a short idle window after
  the first result. Packed server results (`0xD4` + zlib) and tagged
  `server.met` (0xE0) are supported.
- **Kad**: 175 valid contacts loaded, but **no UDP response** to
  `BOOTSTRAP_REQ`, `PING` or `HELLO_REQ` from the first 20 nodes. The first
  diagnosis blamed outbound UDP; that was wrong. A packet-level probe showed
  outbound UDP working (STUN replies; 22/32 bootstrap responses). The real
  cause was byte order: eMule writes `nodes.dat`/contact IPv4 and the other
  Kad 32-bit fields little-endian, and 128-bit values as four native-order
  chunks; the client read them big-endian, so it sent probes to reversed
  addresses. The fix and the new live numbers are below.
- **Gextto integration**: with `ed2k_enabled` on, the managed daemon started
  (fingerprint/scope), `/api/search` with `{"query":"the matrix 1999","kad":true}`
  returned **248 results (234 torrent + 14 eD2k)** in **25.7 s**; without `kad`
  only the 226 torrent ones. `POST /api/search/add` on an eD2k result answers
  **501** with the link and the reason, and queues nothing.

### eD2k/Kad search — live after the wire-format fix, 19 October 2026

Same host and network as the run above; a packet probe against the contacts
cached by aMule (`~/.aMule/nodes.dat`) and then the rebuilt daemon
(`gx-ed2k 1.1.11`) gave:

- **Packet probe**: 200 valid contacts; 22/32 bootstrap requests answered with
  a well-formed `BOOTSTRAP_RES` (20 contacts each); 293/318 `PING`s answered;
  430 `SEARCH_RES` datagrams for `matrix` (mostly `0xE5` zlib-packed, which the
  client now inflates).
- **Daemon**: 421 nodes in the routing table, `peer_packets=142`,
  `reachability=open` ("Kad peers answered"), UPnP mapping accepted.
- **Search**: `the matrix 1999` returned 253 results through Gextto with
  `kad:true`: 19 eD2k (4 `kad`, 7 `kad+server`, 8 `server`) plus torrents.
  The `kad+server` entries match by MD4, which confirms that Kad file hashes
  and eD2k server hashes are decoded in the same order.
- **Wire format**: Kad 32-bit fields are little-endian; IPv4 is a
  native-order word (bytes reversed relative to dotted notation); 128-bit
  node IDs, keyword targets and file hashes are four native-order 32-bit
  chunks; `0xE5` datagrams carry a zlib stream after the opcode.

The earlier "outbound UDP blocked" note is superseded: UDP was fine, the
datagrams were addressed to byte-reversed IPs.

### Phase 0 sample — 30 gaps, 19 October 2026

Reproduce with `go run ./cmd/measure-ed2k -gextto … -daemon … -limit 30
-spacing 65s -min-sources 3`: it samples one already-aired gap per series,
uses the alias when present, and applies the same floor as the fallback.


A first sample of the Fase 0 gate: 30 gaps spread across the 475 open
episodes, queried as `<series> SxxEyy` against the daemon (Kad + eD2k
servers), 12 s timeout. At that point the client still used the longest token
as the Kad lookup target and did not send an expression tree.

| Measure | Result |
|---|---|
| Queries with at least one Kad-origin result | 27/30 |
| Exact episode + recognisable quality + plausible size | 5/30 |
| Same, with at least 2 sources | 4/30 |
| Queries where the top 500 results were all unrelated | many |

The default fallback floor is **3 sources** (`ed2k_min_sources`), so the
"2 sources" row is already generous; with the real floor the accepted set is
smaller.

Caveats: the result set is capped at 500, the match is a regex on the name
(no download validation), localized series titles were used as-is, and the
sample is a single run. The even spread also put nine episodes of the same
series (`I misteri di Murdoch`) in the 30, so the next run should stratify by
series (one episode per show). Most of the 27 "Kad hits" were unrelated files
that shared the longest token (for example every show with `S04E01`).

### Robustness — pacing and one retry

The client now spaces Kad search datagrams by 15 ms and, when a keyword round
returns nothing, learns one more lookup round and asks the closest contacts
again for a bounded 2 s. The stratified sample below was taken just before
this change, so it measures the pre-retry behaviour.

### Search precision — plain keyword vs restrictive tree

A/B on the same routing table (`gx-ed2k` 1.1.12, `fbi`/`elsbeth` targets):

| Query | Plain keyword | Restrictive AND tree |
|---|---|---|
| `FBI S06E13` | 500 results, no exact match in the first page | 35 results, includes `FBI.S06E13...` |
| `Elsbeth S04E02` | 500 results, mostly other Elsbeth episodes | 42 results, `S04E02` variants with 6–8 sources |

The tree target is the first real title word; episode markers, years and
quality tags are skipped for the target but kept in the AND filter, and stop
words are dropped so `The.Matrix.1999.mkv` still matches.

### Phase 0 stratified sample — 30 series, 19 October 2026

One already-aired gap per series (30 series in the gap list), query
`<series or alias> SxxEyy`, 15 s timeout, 65 s between searches, gx-ed2k
1.1.16 (before the retry/pacing change), 5 series queried by their alias.

| Measure | Result |
|---|---|
| Queries (one per series) | 30 |
| With at least one Kad-origin result | 7/30 (23%) |
| Exact episode + recognisable quality + plausible size | 6/30 (20%) |
| Same, with at least 2 sources | 5/30 (17%) |
| Same, with at least 3 sources (default fallback floor) | 4/30 (13%) |

The four series that clear the real floor are `9-1-1`, `CIA`, `Elsbeth` and
`Scrubs`; `Surrealestate` (Agenzia Roman) and `FBI S03E12` have an exact,
qualified result but only 1–2 sources. `Strike S04E01` has an exact name but no
recognisable quality in the name. The other 23 series returned no exact Kad
result; many of those queries still returned 500 eD2k **server** results with
unrelated names, which confirms that the restrictive tree filtered the Kad
bucket rather than filling it with noise.

Caveats: one run, one daemon and one network; DHT coverage varies with the
routing table and peers throttle a burst; the 500-result cap and the regex
matching are approximations; no downloaded file was validated.

**Second stratified sample (gx-ed2k 1.1.23, different episodes).** The same 30
series with `-sample random -seed 2026`, 65 s between searches, after the broad
retry and the episode-variant filter:

| Measure | Result |
|---|---|
| Queries (one per series) | 30 |
| With at least one Kad-origin result | 4/30 (13%) |
| Exact episode + recognisable quality + plausible size | 3/30 (10%) |
| Same, with at least 3 sources (default fallback floor) | **2/30 (7%)** |

`CIA S02E02` (3 results with ≥3 sources) and `The Pitt S02E14` (1) clear the
floor; `Surrealestate S02E04` has one exact result but a single source. The
broad retry does recover individual cases (`Tracker S04E01` went from 0 to 5
Kad results in a spot check), but it does not change the aggregate.

**Gate conclusion.** Two independent 30-series samples put the real-fallback
coverage at **7–13%** (2/30 and 4/30 with ≥3 sources). That is below the level
that justifies the 6.5–10 month eMule transfer engine. Kad stays an optional
**manual** search provider, `ed2k_fallback` stays off, and the T0/T1 spikes stay
isolated from the daemon. The question is reopened only by a material change in
metadata/aliases or by a much larger sample showing a different picture.

**Localized vs original title.** The library name is not always the name on
Kad. Two spot checks against the same routing table:

| Localized query | Kad hits | Original/alias query | Kad hits |
|---|---|---|---|
| `I misteri di Murdoch S13E12` | 0 | `Murdoch Mysteries S13E12` | 1 (`...Fox.Hunt.DVDRip...`, 2 sources) |
| `Agenzia Roman S03E01` | 0 | `Surrealestate S03E01` | 1 (`...Buckaroo.Web.h264-Rbb...`, 1 source) |

The automatic gap filler now queries the configured name and its aliases in
parallel, so the gate sample must use the same alias-aware query to be fair.

**Throttling.** A second 30-query batch run a few minutes later returned almost
no Kad results from the same endpoint, while `PING` still answered and a fresh
client on another UDP port found 35 `FBI S06E13` results. Peers appear to
throttle rapid searches. The gate run must space queries (roughly one every
1–2 minutes) and be repeated cleanly before its numbers are final.

### Dead torrent links

The quarantine of `.torrent` links that keep failing (see
[`evoluzione.md`](evoluzione.md) §2) costs **one query per cycle**, not one per
release: the whole set is read once and checked in memory. With 300 quarantined
rows — as many dead links as the archive held on the machine on 2026-10-17 —
`BenchmarkQuarantinedTorrentLinks` measures **≈0.42 ms and ≈88 KB allocated per
cycle** on the N97 (`go test -run '^$' -bench QuarantinedTorrentLinks -benchmem .`),
against a cycle of minutes. `RecordTorrentLinkFailure` runs only for the links
that actually fail (tens per cycle at most) and `ClearTorrentLinkFailure` only
for the ones that are downloaded.

### Upgrade decisions on the live library — 22 October 2026

Copy of the live databases (read-only backup), N97, `nice -n 15`.

| Check | Result |
|---|---|
| Archived episodes whose stored score differs from a rescore with the current code | **0 / 1861** |
| Archived episodes that their own release title or file name would re-download | **0 / 1861** |
| Custom groups with capitals that never received their bonus (before the migration) | **4 / 6** (`Me7alh`, `MeM.GP`, `Pir8`, `TheBlackKing`) |
| *Impatto sulla libreria*: feed releases replayed through the dry-run decision | **259** releases, **55** series, **1.46 s** |
| Archived episodes those releases would replace | **2** (1080p WEBRip AAC x265 → 1080p WEB-DL DDP 5.1 H.264, «source» rule) |

### Log volume and upgrade probes — 29 October 2026

The machine running the service (N97, 4 cores), the live logs of the last 24
hours (2026-10-28 11:48 → 2026-10-29 11:48), read with `awk`/`grep`:

| Measure | Before | After |
|---|---|---|
| `gx-torrent.log`, lines in 24 h | **212** | — |
| of which the applied speed limits (a value that changes every few seconds) | **76** | 0 at INFO (DEBUG with `-debug`) |
| of which peer caps, peer noise, cache retunes, memory releases | **61** | 0 at INFO (DEBUG with `-debug`) |
| of which one line per failed tracker (10 in the same second when a public tracker list went down) | **18** | 3 (one sentence per pass, up to 3 names) |
| of which "reached the seeding limit", one per paused-and-resumed torrent | **12** | 1 per torrent per run |
| of which the start-up banner | **40** (8 × 5 restarts) | **5** (one line per start) |
| `gextto.log`, lines in 24 h | **499** | — |
| of which the restart sequence (`stopped`, `still running`, divider, banner) | **112** (28 restarts) | 84 (the daemon line merged into the banner) |

Cost of reading the real quality of one library file with ffprobe on the same
machine (local disk, warm cache), the command the upgrade decision runs once per
file and then remembers in the database:

| File | `ffprobe -v error -print_format json -show_format -show_streams` |
|---|---|
| 1080p WEB-DL mkv, 1.9 GB | 274 ms (cold), 81 ms, 80 ms |

The probe runs only for a candidate the decision already judged an upgrade and
whose library row has no MediaInfo yet; the result is stored in `episodes` or
`movies`, so the periodic backfill and the next searches do not repeat it.

*Impatto sulla libreria* (the dry-run preview, which never stores the probe
because it does not write), live library, same machine:

| Run | Releases checked | Wall time |
| --- | --- | --- |
| First, with the library files not read yet in this process | 63 (55 series) | **5.6 s** |
| Again, answers still in the process cache | 63 (55 series) | **2.0 s** |

### Test suite duration — 22 October 2026

`make test` on the N97 (4 cores, Go build cache warm, service running):

| Run | Wall time |
| --- | --- |
| `make test-serial` (plain `go test ./...`, root package serial) | 65 s, root package 59.7 s |
| `make test` (root package in 4 processes, `scripts/go-test-sharded.sh`) | 22–24 s (35 s with the test binary rebuilt) |
| `make test`, waits shortened in the tests (`TestMain`, below), no test cache | 27 s (`make test-serial`: 34 s) |
| `make test`, same, warm test cache | 15 s |
| `make test-ui`, 2 groups → 3 groups (3 Chromium) | about 75 s → 63–72 s |

The waits that only pace real servers are package variables that `TestMain`
shortens in the root package (feed retries and per-host gap, rate-limit penalty,
notifier retries and Telegram gap, TMDB and comic retries, adopted gx-torrent
poll) and in `cmd/gx-torrent` (`feedHostGap`). Per package, with no cache: root
60 s → 28 s serial; `cmd/gx-torrent` 34 s → 18 s; `internal/gxcore/torrent`
18 s → 0.9 s (the IPv6 DHT test waited the 15 s DHT re-query period; the test now
sets it to 100 ms with `dht.SetSearchRetryPeriod`). A route test went from 6.8 s
to 0.6 s once each route was registered once: every `Router()` call appended the
whole list again. `cmd/gx-torrent` stays in one process: its tests share fixed
port ranges.

The DHT re-query period became an atomic read; `wasContactedRecently`, 5 runs
of 2,000,000 calls on the N97: before 92.2–98.4 ns/op (median 92.9), after
91.2–101.2 ns/op (median 93.4), 0 allocations both — unchanged.

The root package holds 1,284 tests; run serially they spent half their time waiting
(CPU 52 s for 100 s of wall time under `nice` and `GOMAXPROCS=2`). 25 tests over 1 s
were 64 s of the 89 s total; the eD2k tests waited a full aMule search window against a
fake core that answers at once (now shortened in the tests: 33 s → 3.5 s).

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
- `scripts/measure-baseline.sh` runs the cheap harnesses — and the network ones when their
  environment is set — and writes a dated baseline, the reference a later round compares
  against. A round that changes nothing still re-runs it, so a regression shows up as a
  difference from the previous date.
