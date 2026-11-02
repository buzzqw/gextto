# Measurements and verification of Gextto, gx-torrent and gx-core

The numbers we use to decide, with the machine they were taken on and the command to
repeat them. The document is a **snapshot**: it keeps the latest figure of each item,
never a history. A new round replaces the row it measures.

- **§2 Comparative benchmarks** come first: the before → after of every change that still
  shapes the current code. This is what a change is judged by.
- **§3 Current baselines** are the static figures of the system as it runs today.
- **§4 Benchmark reference** is the unit cost of each measured path.
- **§5 Investigations and decisions** explains why the current design is what it is.
- **§6 Verification** is what was verified without being a benchmark.

Everything is measured on an Intel N97 mini-PC — 4 cores, 16 GB of RAM, Debian 13
(trixie), Go 1.27.3 — unless a row says otherwise.

The state of the product and the open items are in [`evoluzione.md`](evoluzione.md);
the engine is in [`gx-torrent.md`](gx-torrent.md) (inventory
[`../internal/gxcore/GEXTTO.md`](https://github.com/buzzqw/gextto/blob/main/internal/gxcore/GEXTTO.md))
and its BEP compliance in [`bep.md`](bep.md).

## Index

1. [How to measure](#1-how-to-measure)
2. [Comparative benchmarks](#2-comparative-benchmarks)
3. [Current baselines](#3-current-baselines)
4. [Benchmark reference](#4-benchmark-reference)
5. [Investigations and decisions](#5-investigations-and-decisions)
6. [Verification](#6-verification)
7. [Adding a measurement](#7-adding-a-measurement)

---

## 1. How to measure

Two rules apply everywhere: measure the system **as it really runs** (queue and slots,
seed policy, scheduler and auto-management on — never "with the queues off", which would
be a laboratory and not production), and compare two clients only with the same swarm,
the same role, the same policies and the same metrics.

A number is usable only with four things written next to it: the **machine**, the
**date**, the **build**, and the **command** (or harness) that produced it. A single pass
on a public swarm cannot rank a parameter: peers are luck. Parameters are compared with
the controlled local swarm or with repeated, interleaved runs (`-count`, `benchstat`);
a public swarm says whether the feature works, not which value is better.

**An idle host.** A benchmark round runs with the machine at rest: the service stopped,
containers and browsers idle, load average below 1. A busy host is not a measurement, and
a difference smaller than the spread of the same binary on the same host is not a result.

**Profilers.** The engine and the daemon have an opt-in loopback pprof endpoint:
`GX_TORRENT_PPROF=127.0.0.1:6061` on gx-torrent, `GEXTTO_PPROF=127.0.0.1:6062` on
Gextto. Both are off unless the variable is set, so a profile never costs anything in
normal use.

### A reproducible round

`scripts/bench-all.sh` runs the whole Go benchmark suite in one pass: the root package,
`cmd/gx-torrent`, `internal/gxcore/...`, `internal/utp/...` and the separate
`internal/dht` module, with `-benchmem` and `-count` runs, `GOMAXPROCS=2`. It writes the
raw log and the `benchstat` table to `gextto-data/bench/` and keeps `latest.txt` for the
next comparison.

```bash
make bench-all              # on this host
make bench-all-docker       # in a pinned golang image, CPUs restricted to the container
scripts/bench-all.sh --compare old-raw.txt new-raw.txt
```

`GX_BENCH_COUNT` (default 6), `GX_BENCH_PROCS` (default 2), `GX_BENCH_OUT`,
`GX_BENCH_CPUSET` and `GX_BENCH_IMAGE` tune the round. The container pins the Go
toolchain, not the host: the CPU model and the load still belong in the record.

### Harnesses

Cheap probes and opt-in tests, all outside CI:

| Harness | What it measures |
|---|---|
| `make bench-all` | every Go benchmark of Gextto, gx-torrent and gx-core, raw log plus `benchstat` table |
| `scripts/bench-engines.sh [--size MiB] [--dir DIR] [--leechers N]` | RAM and throughput of gx-core: loopback swarm throughput with peak RSS, and disk write throughput of a directory (`--dir` on the NFS mount to compare) |
| `make measure-seeding` | local swarm, seeder plus 3 leechers by default, distinct `127.0.0.x` addresses. `GX_MEASURE_SUPERSEED=1` compares super-seeding, `GX_MEASURE_MESH=1` connects the leechers to each other, `GX_MEASURE_LEECHERS=N` sets the size |
| `make measure-choking` | the choking campaign: a real **libtorrent** leecher on loopback (`scripts/libtorrent-leech.py`, needs `python3-libtorrent`), plus a real-swarm half that needs an inbound port |
| `make measure-pipeline` | the adaptive request queue at shallow (4/4), default (50/250) and deep (200/500) depth; the knobs travel in `NetworkOptions` (`MaxRequestsOut`/`DefaultRequestsOut`) |
| `make measure-realswarm GX_MEASURE_REAL=<public.torrent>` | a public torrent with DHT, trackers and uTP (`GX_MEASURE_REAL_SECONDS`, default 120 s): peers, uTP split, socket counters. A comma-separated list runs all the torrents together, all active, and adds the CPU of the process and the CPU per 100 MB. A run that sees no peer fails, so it cannot pass hollow. The test daemons tick every 50 ms; `GX_TEST_TICK=3s` gives them the production tick |
| `make measure-merge3` | Swarm Merging Phase 3 on real ISOs (§5.8) |
| `make measure-dht` | the public DHT: routing-table fill and query success (`GX_MEASURE_DHT=5m`, `GX_MEASURE_DHT_INFOHASH=<40 hex>`) |
| `TestMeasureLatencyGuard` | the latency guard against a second instance and a competing download on the real line (`GX_MEASURE_LATENCY_GUARD`, `GX_MEASURE_COMPETITOR_URL`) |
| `TestMeasureManagerLanes` | Jackett/Prowlarr lanes against the aggregate on the live managers (`GX_MEASURE_LANES=<data dir>`) |
| `TestMeasureManyFiles` | descriptors, peak RSS and check time of one torrent with many files already on disk (`GX_MEASURE_FILES=500,2000,5000`; run it niced and memory-capped, §3.5) |
| `TestMeasureManyFilesDownload` | a torrent of many files downloaded at full speed from a second daemon over loopback (`GX_MEASURE_DL=files:avgsize`), data under `GX_MEASURE_DIR` on a real disk (§3.5) |
| `TestMeasureMergePhase3` | block-granularity merge on real ISOs (`GX_MEASURE_MERGE3=<dir>`, `GX_MEASURE_MERGE3_PEER` for a libtorrent seeder) |
| unchoker simulation (`internal/gxcore/internal/unchoker/sim_test.go`) | the real `Unchoker` driven by synthetic peers: fast downloaders in download, fast uploaders in seed, immediate fast unchoke, optimistic unchoke that does not steal a slot and rotates, budget respected, fairness |
| `TestMeasurePipeliningLatency` | an in-process latency proxy before the seed (no `tc`/`netem`, no root): every chunk released at arrival plus latency, in order and without limiting bandwidth |
| `scripts/holepunch-netns-test.sh` | BEP 55 end to end between two clients behind a stateful firewall, with a public relay (`sudo`, network namespaces, `--keep` to debug) |

## 2. Comparative benchmarks

Before and after the change that produced the current code, on the same host and with
the same command: interleaved runs or `-count` plus `benchstat`. This is the first
section because a change is judged by the delta it produces. The steady-state figure of
the same benchmark is in §4.

### 2.1 Engine, picker and merge

| Change | Before | After | Measured |
|---|---|---|---|
| Log level checked before formatting (`BenchmarkEngineLoggerFiltered`) | 151–175 ns/op, 3 allocs | **36–38 ns/op, 40 B, 1 alloc** | 2026-10-28 |
| IP filter parsed on the bytes of the line (`BenchmarkLoad`, 172,000 rules) | 56.5–58.6 ms, 39.7 MB, 882,032 allocs | **32.6–33.1 ms, 6.6 MB, 32 allocs** | 2026-10-26 |
| Log page highlighting: one word scan instead of six regexps (`BenchmarkV2HighlightLogLine`) | 58.6–67.3 µs, 2.3 KB, 18 allocs per line | **0.91–0.95 µs, 576 B, 2 allocs** | 2026-10-26 |
| Restart with unchanged translation catalogs (`BenchmarkSeedDefaultTranslationsReseed`) | 841–1,025 ms, 45.3 MB, 1,027,628 allocs | **3.1–3.7 ms, 2.5 MB, 35 allocs** | 2026-10-26 |
| Series names and aliases indexed once per list (`BenchmarkFindSeriesMatchUnmatched`, 500 series) | 3.63–3.71 ms, 1.52 MB, 33,500 allocs | **120–121 µs, 8.9 KB, 518 allocs** | 2026-10-28 |
| Same scan when the release is monitored but the exact pass misses (`BenchmarkFindSeriesMatchMatched`) | 2.72–2.90 ms, 987 KB, 29,473 allocs | **128–132 µs, 8.5 KB, 515 allocs** | 2026-10-28 |
| One transaction per 500 rows instead of three autocommit `UPDATE`s per torrent (`BenchmarkRescore`, 2,000 torrents) | 215–237 ms, 6.3 MB, 84,153 allocs | **54–60 ms, 6.2 MB, 82,264 allocs** | 2026-10-28 |
| Gap calculation in one pass instead of two scans (`BenchmarkArchiveGaps`, 50 series) | 5.74–6.77 ms, 522 KB, 17,580 allocs | **4.36–4.57 ms, 515 KB, 17,164 allocs** | 2026-10-28 |
| Library rescan batched in one transaction per series (`BenchmarkSyncArchiveFilesScored`, 500 files) | 135 µs/file on the RAM disk, 1.63–1.70 ms/file on the NVMe | **20 µs/file on the RAM disk, 25–39 µs/file on the NVMe** | 2026-10-28 |
| Torrent snapshot read: sort kept with the cache (`BenchmarkEngineListSnapshot`, 2,000 torrents) | 1.42 ms, 1.00 MB, 4 allocs | **593–610 µs, 1.00 MB, 1 alloc** | 2026-10-28 |
| The twelve reads of one 750 ms tick (`BenchmarkEngineSnapshotTwelveCalls`, 2,000 torrents) | 12 copies and 12 sorts of the whole table | **7.1–7.4 ms, 12.3 MB, 12 allocs** | 2026-10-28 |
| Archive volume walked once per cycle, not once per movie (`BenchmarkArchiveMovieQualityPerCandidate`, 200 candidates) | 17.4–18.2 s, 434 MB, 5.47 M allocs | **139–146 ms, 3.1 MB, 45,900 allocs** | 2026-10-28 |
| Second read handle on the archive (`BenchmarkArchiveReadDuringFullScan`, 50,000 releases) | 116–122 ms, 32.8 MB, 549,784 allocs | **257–349 µs, 618 B, 17 allocs** | 2026-10-28 |
| Piece picker, rarest with early exit (`BenchmarkPickerRarestSteady`) | 112.7–118.6 µs/op, 0 allocs | **11–35 ns/op, 0 allocs** | 2026-10-28 |
| Piece picker, peer departure visits only the bits the peer held (`BenchmarkPickerDisconnect`, 27,250 pieces) | 413–440 µs/op, 0 allocs | **95–126 µs/op, 0 allocs** | 2026-10-29 |
| Piece picker, new peer bitfield then departure (`BenchmarkPickerBitfieldManyPeers`, 4,000 pieces, 150 peers) | 57.0–62.5 µs/op, 1 alloc | **35.4–37.0 µs/op, 1 alloc** | 2026-10-29 |
| Piece picker, sequential pick starts past the finished prefix (`BenchmarkPickerSequentialNearlyDone`, 27,250 pieces) | 106–115 µs/op | **4.5–4.7 ns/op** | 2026-10-29 |
| Piece picker, rarest in a dense swarm (`BenchmarkPickerRarestManyPeers`, 150 peers) | 12.1–12.8 µs/op | **10.5–11.1 µs/op** | 2026-10-29 |
| Webseed gap list into reused buffers (`BenchmarkPickerSmallestGapWebseeds`, 15,000 pieces) | 238–246 µs, 64 KB, 1 alloc per pick | **79–81 µs, 0 allocs per pick** | 2026-10-26 |
| Fixed-size peer message bodies read into stack arrays (`BenchmarkPeerReaderRequests`, 4,096 messages) | 531–572 ns and 4 allocs per message | **407–451 ns and 2 allocs per message** | 2026-10-30 |
| One timer per connection armed on the oldest outstanding packet (`BenchmarkTransferLoopback`, 16 KiB block) | 130–161 MB/s, 5.5 KB, 99 allocs | **151–159 MB/s, 4.0 KB, 75 allocs** | 2026-10-28 |
| Connection id without a 64K-entry table per dial (`BenchmarkNewConnID`) | 152,353 ns/op, 512 KiB per dial | **31 ns/op, 0 allocs** | — |
| One hash per piece: verification dispatches on the layout (`BenchmarkPieceWriterRunV2`, 4 MiB piece) | 3.47–3.55 ms, 1181–1207 MB/s, 14 allocs | **1.93–1.95 ms, 2151–2177 MB/s, 12 allocs**; v1 unchanged at 1.54–1.56 ms | 2026-10-30 |
| Release identity: name and size match once per release (`BenchmarkMergeDuplicateReleases`, 24,000 releases) | 45–52 ms, 37.7 MB, 120,002 allocs | 48–54 ms, 48.2 MB, 192,198 allocs (deliberate trade: about +10 MB of short-lived memory) | 2026-10-24 |
| Archive identity declared for a `.torrent`-only listing (`BenchmarkArchiveSaveBatch`, 1,000 releases) | 45.2–46.1 ms, 3.09 MB, 50,536 allocs | **40.1–40.4 ms, 3.18 MB, 50,538 allocs** | 2026-10-31 · build 2124 |

### 2.2 Storage and archive

| Change | Before | After | Measured |
|---|---|---|---|
| Resume flush: a clean file costs no syscall (`BenchmarkPlainSyncCleanSkipped` vs `…Fdatasync`) | 337–361 ns (`fdatasync` on a clean file) | **9.5–12.5 ns** (dirty flag clear, no syscall) | 2026-10-30 |
| Feed archive dedup on the infohash (live database) | 480 MB, 610,254 rows | **258 MB, 307,477 rows** | 2026-10-21 |
| Archive content-filter page: `id,title` instead of the whole `magnet` text | 364,348 rows, 138,090,257 B (~131.7 MiB) copied per request | **28.8 MB of titles and one `id IN (…)` query** for the visible rows | build 2072 |
| Durability without `O_SYNC`, batched by `syncPieces()` | 35 MiB/s for 16 KiB writes on NFS | **81 MiB/s (2.3×)**, up to ~118× at small blocks on NVMe | — |

### 2.3 Daemon and web UI

| Change | Before | After | Measured |
|---|---|---|---|
| Series page: names and aliases indexed once, five queries per series removed | 112 ms of CPU per request | **37 ms** | build 2038 |
| Log tail: one word scan instead of six regexps on 500 lines every 5 s | 28 ms per refresh | **5 ms** | build 2038 |
| Dashboard seen-release count reused for 30 s | 25–36 ms per request | **18–19 ms** | build 2038 |
| Web UI transfer, gzipped | dashboard 33.3 KB, downloads 70.7 KB, archive 473.2 KB, maintenance 118.7 KB | **8.0 / 12.3 / 38.8 / 17.2 KB** | build 1945 |
| CSS and JS served compressed and cached immutable | 285.2 KB | **75.3 KB**, repeat request `304, 0 bytes` | build 1945 |
| eD2k grab note off the request path | up to 40 s on a batch of ten results | **0.31–0.54 ms** for the add, the note runs in its own goroutine | build 2067 |
| Log volume in 24 h, `gx-torrent.log` | 212 lines, 76 speed-limit changes, 61 peer/cache/memory lines, 18 tracker lines, 12 seeding-limit lines, 40 banner lines | **0 speed and peer lines at INFO** (DEBUG with `-debug`), **3 tracker lines**, **1 seeding line per torrent per run**, **5 banner lines** | 2026-10-29 |
| Log volume in 24 h, `gextto.log` | 499 lines, 112 restart-sequence lines | **84 restart-sequence lines** (the daemon line merged into the banner) | 2026-10-29 |

### 2.4 Peer writer: small messages batched into one write

Every protocol message used to be its own `Write`: a refill of 250 block requests
(17 bytes each) was 250 syscalls and, with `TCP_NODELAY`, 250 segments. The writer now
takes the run of small messages at the head of its queue in one hand-off and sends it
with one `Write`; block uploads still go one by one.

| Measure (100,000 requests) | Before | After |
|---|---|---|
| `write` syscalls | 100,309 | **1,972** |
| all traced syscalls (`write`, `read`, `futex`, …) | 144,329 | **4,842** |
| writes per message, `BenchmarkPeerWriterRequests` (counting conn) | 1.000 | **0.014–0.021** |
| heap per refill of 250 | 27.4 KiB, 750 allocs | **15.6–15.9 KiB, 500 allocs** |
| time per refill of 250 over loopback TCP, `BenchmarkPeerWriterRequestsTCP` | 1.70–2.85 ms | **0.28–0.46 ms** |

Reproduce with `go test -c ./internal/gxcore/internal/peerconn/peerwriter/` and
`strace -f -c -e trace=write,read,futex ./peerwriter.test -test.run '^$' -test.bench RequestsTCP -test.benchtime=400x`.

### 2.5 Registering a release again

A new download of an infohash that was already downloaded clears the completion left by
the previous attempt (`beginNewDownloadAttempt`): one indexed read on the normal paths,
two reads and one `UPDATE` when the row still says "completed" and its archived copy is
gone. The path runs once per started download, not per cycle. `MarkTorrentCompleted`,
which the loop needs to put the row back into "completed", is outside the timed region.

| Case | Before | After |
|---|---|---|
| First add, no row yet | 115–163 µs | **154–199 µs** |
| Re-register on a queued row | 123–145 µs | **149–194 µs** |
| Re-register after a completion whose copy is gone | 167–233 µs | **272–320 µs** |

Reproduce with
`go test -run XXX -bench BenchmarkRegisterTorrentScored -benchmem -count=5`.

### 2.6 Build and test suite

| Change | Before | After |
|---|---|---|
| `make test-serial` (plain `go test ./...`, root package serial) | 65 s, root package 59.7 s | **34 s** with the test waits shortened |
| `make test` (root package in 4 processes, `scripts/go-test-sharded.sh`) | 65 s serial | **22–24 s** (35 s with the test binary rebuilt, 15 s warm) |
| `make test-ui` (Playwright, Chromium) | about 75 s | **63–72 s** |

The waits that only pace real servers are package variables that `TestMain` shortens in
the root package (feed retries and per-host gap, rate-limit penalty, notifier retries and
Telegram gap, TMDB and comic retries, adopted gx-torrent poll) and in `cmd/gx-torrent`
(`feedHostGap`). `internal/gxcore/torrent` dropped from 18 s to 0.9 s once the IPv6 DHT
test stopped waiting the 15 s re-query period; `cmd/gx-torrent` stays in one process
because its tests share fixed port ranges.

## 3. Current baselines

### 3.1 gx-torrent

| | Current figure | Build · date | Reproduce with |
|---|---|---|---|
| At rest | **0.93–0.98 %** of a core over 60 s, **29–32 MB RSS**, 10 threads, 92 descriptors; ~24 % of the 30 s CPU profile in the DHT | 1.2.278–279 · 2026-10-30 | live service, 3 seeds, no peers, full IP filter; `/proc/<pid>/stat`, `GX_TORRENT_PPROF=127.0.0.1:6060` |
| Under load, public ISOs | **2.0–4.3 s of CPU per 100 MB**, 18–37 % of a core at 8.2–8.8 MB/s, 116–132 peers | 261 · 2026-10-26 | 5 public ISOs for 180 s, automatic peer cap, `GX_TEST_TICK=3s`, data under `~/.cache` |
| Loopback transfer | **234–274 MB/s**, 7.5–8.3 s of CPU per 1.09 GiB for seeder and leecher together, 100 MiB peak RSS | 261 · 2026-10-26 | `GX_MEASURE_DL=8:134217728`, `GOMAXPROCS=2`, `CPUQuota=200%`, interleaved rounds |
| Automatic peer cap | **55 per torrent, 300 in all** on this 4-core/16 GiB host; with 9 active torrents **−36 % peers and −12 % CPU** against a fixed 55 per torrent, throughput within the swarm noise (−4 %) | 1934 · engine 245 | `GX_MEASURE_AUTO=1` / `GX_MEASURE_MAXCONN`, 9 public ISOs, 150 s, two interleaved rounds |
| uTP loopback | **151–159 MB/s**, 4.0 KB and 75 allocs per op | 2026-10-28 | `BenchmarkTransferLoopback` |
| Storage | **~5.8 GB/s** buffered, **~7.4 GB/s** mmap; 16 KiB writes on NFS **81 MiB/s** | — | `BenchmarkStorageWrite`, `BenchmarkStorageWriteMmap`, NFS harness |
| Read cache | **330–370 ns** and 0 allocs per hit; parallel form 800–840 ns/op (~2.5 M hits/s) | 2026-10-30 | `BenchmarkCacheGetHit`, `-benchtime 200000x -count=3` |
| `state.json` save | **0.44–0.48 ms** at 100 torrents (131 KB allocated, 66 KB file), **4.1–4.3 ms** at 1,000, **19–21 ms** at 5,000, **16–17 ms** with 1,000 torrents and 20,000 feed entries | 2026-10-30 | `BenchmarkSaveStateLocked`, `-benchtime 20x -count=3` |

### 3.2 gexttod

| | Current figure | Build · date | Reproduce with |
|---|---|---|---|
| At rest | **0.46 %** of a core over 120 s, **71.1 MB RSS** | 2078 | live service, CPU ticks from `/proc/<pid>/stat`, `VmRSS` |
| First minutes after a start | **0.39 %** of a core, stale-temp sweep 0.21 s, 8 new TLS connections in 300 s | 1790 | profile from start, `strace -e connect` |
| Directory opens at rest | **0** per 20 s; no write probe on the NAS | 1785 | `strace`, `/proc` over 60 s |
| Metadata calls | ~5,000 a day for 55 series | — | live service |
| Add intent (crash-safe add) | **76–79 µs, 3.2 kB, 44 allocs** per started download | — | written before every engine add; negligible next to the engine add |
| Memory of a full cycle | at rest after a start **65 MB RSS** (42 MB anon, 8.2 MB `heap in use`, 41 goroutines); during the cycle **232 MB RSS**, 155 MB anon, 38 MB heap, 65 goroutines; after the cycle **6.8 MB heap**, 42 goroutines | 2067 · 2026-10-28 | live service, `GEXTTO_PPROF`, `/proc/<pid>/status`, cgroup `memory.stat`, `POST /api/run_now` of 31,275 releases |
| Page cache of the service | `memory.current` 2.0 GB with `file` 1.9 GB against `anon` 68 MB; peak 4.5 GB, all `file` | 2067 · 2026-10-28 | cgroup `memory.stat`; the pid's own RSS is the figure above |

The large number shown by `systemctl status` is **page cache, not the process**. It is
reclaimable, it drops by itself during a cycle (302 MB → 69 MB as the heap grew) and a
restart of the unit recreates the cgroup and takes it back to ~0.3 GB. `MemoryMax` on the
unit would count it, so a limit, if one is ever set, must leave room for it.

### 3.3 Web UI and feed archive

| | Current figure | Build · date |
|---|---|---|
| CPU per request | Series **37 ms**, log tail **5 ms**, dashboard **18–19 ms**, archive **39–40 ms**, downloads **10 ms** | 2038 |
| Transfer, gzipped | dashboard **8.0 KB**, downloads **12.3 KB**, archive **38.8 KB**, maintenance **17.2 KB**; CSS and JS **75.3 KB**, cached immutable | 1945 |
| Rows refresh (gx-torrent page) | **9,831 B per tick** (3,432 B JSON + 6,399 B notable), about 1.7 KB per row | — |
| Feed archive | **258 MB**, **307,477 rows**, one row per torrent (infohash) | 2026-10-21 |
| Archive insert | **40.1–40.4 ms per 1,000 releases** (3.18 MB, 50,538 allocs) | 2124 |
| Archive search (standalone) | **7.4 ms** on 100,000 releases | — |
| Archive API page (100 rows) | **7–23 ms** | — |
| Prometheus scrape | **~0.24 ms, 121 KB, 813 allocs**, computed only when scraped | — |
| Media integrity targets (5,000 episodes) | **99.3–100.5 µs/op** (2,160 B, 45 allocs) | 1.2.2138 |
| Media integrity detail (64 KiB of ffmpeg lines) | **167–176 µs/op** (63 kB, 403 allocs) | 1.2.2179 |

### 3.4 Network and swarm

| | Current figure | Reproduce with |
|---|---|---|
| Real swarm throughput | **9.3–9.4 MiB/s** on the Ubuntu ISO over an FTTC 100/15 line, against 6.3–7.1 MiB/s of libtorrent on the same swarm | `make measure-realswarm` |
| Real-swarm sessions | 276 MB in 90 s (75 peers), 793 MB in 180 s (80 peers), 1.08 GB in 180 s (85 peers), `utp_syn_drops` 0 | `make measure-realswarm` |
| DHT | **1,290 nodes in ~80 s**, 314/1,432 queries answered (21.9 %), peers found for a real infohash | `make measure-dht` |
| DHT, 5-minute Debian run | ~296 nodes (peak 525), 4,703 queries, 1,670 replies (**35.5 %**), `subnet_share_max` 4–7, `nodes_insecure_id` ~42 % | `GX_MEASURE_DHT=5m make measure-dht` |
| Merge on real data | **25.0 % from the network** (Debian and Arch ISOs), also after a restart and in any add order | `TestMeasureMergePhase3` |
| Indexer lanes | 30 titles, 6 at a time: **110 s**, 205/0 requests, 5,935 releases (aggregate: 450 s, 35/25, 2,737) | `TestMeasureManagerLanes` |
| Latency guard | alone **8,503 KiB/s kept**; with a competing download the other device gets **+55 %** (router counters) or **+66 %** (trials only) | `TestMeasureLatencyGuard` |
| Local swarm | **3.00× corpus** in 24 ms, 2 MiB and 3 leechers | `make measure-seeding` |
| Choking, real libtorrent leecher | **50,331,648 bytes (100 % of the file) in 5.587 s** | `make measure-choking` |
| Pipelining at RTT 50 ms, 256 KiB pieces | **305 KiB/s** with one block in flight, **3.8 MiB/s** with the default queue, **6,756 KiB/s** with two pieces per peer | `make measure-pipeline` |

### 3.5 Many files in one torrent

`TestMeasureManyFiles` opens a torrent of 4 KiB files already on disk, under
`systemd-run --user --scope -p MemoryMax=6G -p CPUQuota=200%`, niced and ioniced,
`GOMAXPROCS=2`:

| Files | Descriptors while running | After a stop | Check of the data on disk |
|---|---|---|---|
| 500 | +500 | back to the base | 94 ms |
| 2,000 | +2,000 | back to the base | 150 ms |
| 5,000 | +5,000 | back to the base | 137 ms |
| 12,000 | +12,000 | back to the base | 361 ms |
| 20,000 | +20,000 | back to the base | 406 ms |

`TestMeasureManyFilesDownload` serves a torrent of many files from a second daemon over
loopback (sizes spread ±50 % around the average, 512 KiB pieces spanning several files),
nothing throttled; CPU and RSS are the two daemons together:

| Files | Size | Seeder check | Download | Average | CPU | Peak RSS | Peak descriptors | Host load (max) |
|---|---|---|---|---|---|---|---|---|
| 1,000 | 0.15 GiB | 1.4 s | 1.9 s | 85 MB/s | 0.65 core | 100 MiB | 2,013 | 0.6 |
| 6,000 | 0.92 GiB | 5.8 s | 7.9 s | 125 MB/s (peak 140) | 0.88 core | 114 MiB | 12,015 | 1.5 |
| 6,500 | 3.72 GiB | 10.2 s | 23.8 s | 168 MB/s | 1.37 cores | 121 MiB | 13,015 | 1.7 |

The host stayed responsive throughout: available memory never moved from ~11.2 GiB and
the load stayed under 2. Thousands of files cost neither memory nor speed: the rate is
the loopback single-pair ceiling of §3.1.

### 3.6 Gextto on a Synology DS420j (trial, not a supported target)

Machine: DS420j, Realtek RTD1296 (4 × Cortex-A53), 987 MB RAM, DSM 7.4.1, kernel 4.4,
ext4 on HDD RAID, Surveillance Station running. 2026-11-01, Gextto 1.2.2174, gx-torrent
1.1.294, cross-compiled `linux/arm64`, run as a plain user with `nice -n 10` and
`GOMEMLIMIT=250MiB`, no `systemd-run`, sampled every 10 s from `/proc`. Before the trial:
~370 MB available, 297 MB of swap in use.

| Phase | gexttod RSS | gx-torrent RSS | Min. MemAvailable | Swap | CPU busy | iowait |
|---|---|---|---|---|---|---|
| Idle, IP filter loaded (162,155 rules) | 37–66 MB | 20–21 MB | 318 MB | 302 MB | 3–7 % | 0–7 % |
| Search cycle: 4 titles, 1,703 releases, 49 s, remote indexer | 66 MB peak (whole phase) | 129 MB | 240 MB | 306 MB | 20 % peak | 22 % peak |
| Download: 6 large torrents, ~2 min | 37 MB | 209 MB | 160 MB | 316 MB | 19 % peak | 48 % peak |
| Download: 4 torrents, 8 min, 8–9 MB/s total, 16 MB/s disk peak | 38 MB | 90 MB | 290 MB | 319 MB | 14 % avg, 19 % peak | 6 % avg, 28 % peak |

One pass on public swarms: the 9 MB/s is what those swarms and this line gave, not a
ceiling of the NAS. Not measured: seeding, post-processing of completed files, operation
over several days. `ffprobe` is missing on DSM, so the MediaInfo backfill stays paused.

## 4. Benchmark reference

Micro-benchmarks with `-benchmem`. Run them with `-count=6` and compare with `benchstat`
when a change is meant to be an optimisation; a single pass is noise. `make bench-all`
runs the whole set.

| Benchmark | Current figure | Note |
|---|---|---|
| `BenchmarkEngineLoggerFiltered` vs `Forwarded` | **36–38 ns/op, 40 B, 1 alloc** vs 778–799 ns/op, 424 B, 6 allocs | the level methods check the threshold before `fmt.Sprint`/`Sprintf`; the remaining alloc is the variadic slice of the call |
| `BenchmarkBlocked` (IP filter) | 18.6 / 37.7 ns/op, 0 allocs | sorted merged intervals with binary search |
| `BenchmarkLoad` (IP filter, 172,000 rules) | **32.6–33.1 ms, 6.6 MB, 32 allocs** | ranges parsed on the bytes of the line; `FuzzParseCIDRMatchesReference` holds it to the old parser |
| `BenchmarkV2HighlightLogLine` vs `…Regexp` | **0.91–0.95 µs, 576 B, 2 allocs** vs 58.6–67.3 µs, 2.3 KB, 18 allocs per line | output identical on 36,521 live log lines and a 45 s fuzz |
| `BenchmarkSeedDefaultTranslationsReseed` | **3.1–3.7 ms, 2.5 MB, 35 allocs** | a restart with unchanged catalogs only hashes them |
| `BenchmarkPickerRarestSteady` | 11–35 ns/op, 0 allocs | rarest pick with an early exit |
| `BenchmarkPickerRarestManyPeers` (150 peers) | 10.5–11.1 µs/op, 0 allocs | one rarest pick in a dense swarm |
| `BenchmarkPickerDisconnect` (27,250 pieces) | 95–126 µs/op, 0 allocs | one peer departure on the torrent loop |
| `BenchmarkPickerBitfieldManyPeers` (4,000 pieces, 150 peers) | 35.4–37.0 µs/op, 1 alloc | a new peer's bitfield arriving, then its departure |
| `BenchmarkPickerSequentialNearlyDone` | 4.5–4.7 ns/op | one sequential pick near the end of a download |
| `BenchmarkPickerSmallestGapWebseeds` (15,000 pieces) | 79–81 µs, 0 allocs per pick | gaps into reused buffers, pieces read in place, counting sort on the gap length |
| `BenchmarkTickUnchokeSeeding` | 1.3 / 1.64 µs/op, 1 alloc; 50 peers ≈1.1 µs, 896 B, 1 alloc | unchoker tick |
| `BenchmarkPrefillFromUnrelatedV2` (16 candidates) | 1.49 / 1.58 µs/op, 128 B, 16 allocs | one-shot cost of the Phase 2 merge scan |
| `BenchmarkMergeCandidatesUnrelated` (32 torrents) | ~265 ns/op, 0 allocs | filter paid when nothing is reusable |
| `BenchmarkNewConnID` (uTP) | 31 ns/op, 0 allocs | no 64K-entry table per dial |
| `BenchmarkTransferLoopback` (uTP, 16 KiB block) | 151–159 MB/s, 4.0 KB, 75 allocs | connection-id probe, lazy `Event` channels, reusable read window, `netip.AddrPort` key, one RTO timer per connection |
| `BenchmarkUTPConnRemoteAddr` | 113 ns/op, 40 B, 3 allocs | addresses converted once at wrap |
| `BenchmarkPeerReaderRequests` | **407–451 ns and 2 allocs per message** | 4,096 messages per op; the fixed-size bodies are read into stack arrays |
| `BenchmarkPlainSyncCleanSkipped` vs `…Fdatasync` | **9.5–12.5 ns** vs 337–361 ns | ~330 ns saved per clean file per resume tick |
| `BenchmarkStorageWrite` / `StorageWriteMmap` | ~5.8 / ~7.4 GB/s | far above any network speed: `O_DIRECT` and io_uring are not justified |
| `BenchmarkPieceWriterRunV2` (4 MiB piece) | **1.93–1.95 ms, 2151–2177 MB/s, 32,832 B, 12 allocs** | one hash per piece; `BenchmarkPieceWriterRunV1` 1.54–1.56 ms, unchanged |
| `BenchmarkArchiveSaveBatch` (1,000 releases) | **40.1–40.4 ms, 3.18 MB, 50,538 allocs** | prepared statements per chunk |
| `BenchmarkFeedArchiveSearch` (100,000 releases) | 7.4 ms | compact index on bbolt |
| `BenchmarkMetricsScrape` (one torrent) | ~0.24 ms, 121 KB, 813 allocs | computed only when scraped |
| `BenchmarkAutoSpeedUpdate` | **49–77 ns/op, 0 B, 0 allocs** | one controller step, the work of the queue tick every 3 s; the TCP probe is outside |
| `BenchmarkCriteriaMatch` (RSS rule, 4 names) | 142 ns/op, 0 B, 0 allocs | scans the title in place |
| `BenchmarkRateHistoryAdd` / `LifetimeAccumulate` | 42 / 6.4 ns/op, 0 allocs | the statistics page costs nothing on the sampling path |
| `BenchmarkRateHistorySnapshot1h` / `StatsPageTranslate` | 11.3 µs, 20 KiB, 1 alloc / 255 µs, 89 KiB, 1,919 allocs | one snapshot and one page translation per graph request |
| `BenchmarkSeedPolicyPass` (10 seed rules, 100 torrents) | 41.4 µs, 5.2 KB, 128 allocs per pass | plus one `torrent_meta` query only when rules exist |
| `BenchmarkMergeDuplicateReleases` (24,000 releases, 8,000 copies) | 48–54 ms, 48.2 MB, 192,198 allocs | one pass per cycle and per search |
| `BenchmarkPayloadContentPath` (2,000-file torrent) | 207 ns, 64 B, 1 alloc; NoSubfolder 1.65 µs, 0 allocs | the `content_path` every list refresh computes per torrent |
| `BenchmarkNamingTags` (audio + HDR label) | 16.5 µs/op, 2.08 KB, 33 allocs | once per episode at the 6-hour library check |
| `BenchmarkSearchProgressFanOut` (one indexer) | 280–325 µs/op and 279–412 µs/op, ~19.5 KB, 293 allocs | what following a manual episode search adds to its fan-out |
| `BenchmarkCacheGetHit` / `…HitParallel` | **330–370 ns, 0 alloc, 0 B**; parallel 800–840 ns/op (~2.5 M hits/s) | the read path of every uploaded block: 0.2 % of a core at 100 MB/s |
| `BenchmarkSaveStateLocked` | **0.44–0.48 ms** at 100 torrents, **4.1–4.3 ms** at 1,000, **19–21 ms** at 5,000, **16–17 ms** with 1,000 torrents and 20,000 feed entries | one `state.json` save, the work done while holding `d.mu` |
| `BenchmarkEngineListSnapshot` (2,000 torrents) | **641–656 µs/op, 1.00 MB, 1 alloc** | one read of the torrent snapshot |
| `BenchmarkEngineSnapshotTwelveCalls` (2,000 torrents) | **6.9–7.6 ms, 12.3 MB, 12 allocs** | the twelve reads of one tick |
| `BenchmarkFindSeriesMatchUnmatched` / `…Matched` | **120–121 / 128–132 µs/op** | 500 series, one alias each |
| `BenchmarkRescore` (2,000 torrents) | **54–60 ms/op, 6.2 MB, 82,264 allocs** | one full re-score |
| `BenchmarkArchiveGaps` (50 series, 2 seasons, 40 episodes each) | **4.36–4.57 ms/op, 515 KB, 17,164 allocs** | one gap calculation |
| `BenchmarkSyncArchiveFileScored` (existing episode, file present) | 134–137 µs/op on the RAM disk; **1.63–1.70 ms/op on the NVMe** | single-record path, one commit on purpose |
| `BenchmarkSyncArchiveFilesScored` (500 files of one series) | **20 µs/file on the RAM disk, 25–39 µs/file on the NVMe** | one transaction per series |
| `BenchmarkArchiveMovieQualityPerCandidate` (200 candidates) | **139–146 ms, 3.1 MB, 45,900 allocs** | one walk of the archive per cycle, shared by every candidate |
| `BenchmarkArchiveReadDuringFullScan` (50,000 releases) | **257–349 µs/op, 618 B, 17 allocs** | a short read while a full scan runs |
| `BenchmarkRegisterTorrentScoredFirstAdd` / `…RepeatQueued` / `…AfterCompletion` | **154–199 / 149–194 / 272–320 µs** | dominates SQLite's per-statement cost on this disk |
| `BenchmarkPieceDownloaderLifecycle` (4 MiB piece, 256 blocks) | 0.66–1.10 ms/op, **18 KB and 15 allocs per piece** | the time is almost all the 4 MiB copy into the piece buffer |
| `BenchmarkQuarantinedTorrentLinks` (300 rows) | ≈0.42 ms and ≈88 KB allocated per cycle | one query per cycle, not one per release |
| `BenchmarkMediaIntegrityTargets` (5,000 archived episodes) | **99.3–100.5 µs/op, 2,160 B, 45 allocs** | one target per call; the `ffmpeg` decode dominates the run |
| `BenchmarkMediaIntegrityDetail` (64 KiB of ffmpeg lines) | **167–176 µs/op, 63 kB, 403 allocs** | building the readable detail from the stderr buffer |

### 4.1 Seed mode: cost of the lazy check

A torrent added in seed mode hashes a piece the first time a peer asks for it: the first
block of that piece waits for one read of the whole piece and its SHA-1, then the piece
is served from the read cache like any other. The table measures that wait with the file
in the page cache (the CPU side; a cold read adds the disk time of one piece),
`GOMAXPROCS=2`:

| Piece size | Wait before the first block | Throughput | Heap per check |
|---|---|---|---|
| 256 KiB | **0.21 ms** | 1.22–1.25 GB/s | 256 KiB, 3 allocs |
| 1 MiB | **0.74–0.81 ms** | 1.29–1.42 GB/s | 1 MiB, 3 allocs |
| 4 MiB | **3.7–3.9 ms** | 1.08–1.14 GB/s | 4 MiB, 3 allocs |
| 16 MiB | **13.0–13.1 ms** | 1.28–1.29 GB/s | 16 MiB, 3 allocs |

Each piece pays it once per seed-mode session; at most 4 checks run at once per torrent,
so the transient heap is bounded by 4 pieces. The full check that seed mode skips reads
and hashes the whole torrent before the first byte is served.

Reproduce with
`go test -run '^$' -bench BenchmarkSeedModeLazyHash -benchmem -count=3 ./internal/gxcore/torrent/`.

## 5. Investigations and decisions

Each entry states the question, the numbers that answered it and the decision. The
figures are the latest; the benchmark and the command are in §3 and §4.

### 5.1 Engine CPU and memory

**Current figures** (load test, 9–10 releases, Auto speed, a collector every 5 s and a
CPU profile at ≥ 8 MB/s): at rest 29–32 MB RSS and under 1 % of a core (§3.1); under
load 44–48 % of a core at 8–9 MB/s with ~110–130 peers; **5.3 % of a core per MB/s and
0.35–0.43 per peer**; memory under load RssAnon peak ~1.1 GB with 10 torrents and the
NFS write cache, ~507 MB outside the two caches; the piece picker is 9 % of the profile.

**Choices behind these figures.** The read path uses a progress-based deadline, so a dead
uTP connection fails at once instead of looping; the socket handles each datagram in the
receive buffer and copies only what stays queued; `waitEvents` is a plain select. The IP
filter keeps the parsed rules instead of the file, `GOGC=50` trims the heap headroom, and
the buffers of a finished burst are released. The collector costs 0.5 %
(`gcBgMarkWorker`) and 2.2 % (`mallocgc`) of the profile. After 5 minutes with the total
transfer rate below the queue's slow rate the daemon calls `debug.FreeOSMemory` (at most
every 10 minutes) to return the 1 MB piece buffers that the per-torrent `sync.Pool`s
would otherwise keep reachable until the next collection: the RSS goes from 550 MB to
35–40 MB with 7 torrents and no active transfer.

**Where the CPU goes** (five public ISOs together, 180 s, production tick): syscalls and
the Go scheduler's wake-ups about 50 % (one goroutine per connection, every segment that
arrives), RC4 of the MSE stream 9–10 %, SHA-1 piece checks 5–7 % (with SHA-NI), TLS of
the HTTPS webseeds 2–3 %: costs of the protocol and of the traffic itself. What was the
engine's own: zeroing every pool buffer before use (1.9 %), now skipped where the buffer
is overwritten whole — a block read with `io.ReadFull`, a piece whose blocks cover it
(pieces with BEP 47 padding are still zeroed, `TestPaddedTorrentTransfer`); and the
webseed pick, which allocated and sorted the gap list for every peer pick (2.4 %, 3×
cheaper now). The statistics the tick reads sum the done bytes with a popcount of the
bitfield when no file is skipped, instead of a test per piece. The public runs of the
same binary spread from 2.0 to 4.3 s per 100 MB with the peers and uTP share they get, so
the gain is stated by the profile share and the micro-benchmarks, not by a throughput
figure.

Kept as they are: the per-peer read cache allocates its 128 KiB blocks per upload (it
follows the upload rate, 1–2 MB/s on this line), and RC4 is chosen whenever the peer
offers it, as libtorrent does; plaintext would save that share at the price of the
obfuscation, which is the user's choice (`-encryption`).

**Piece picker.** With ~200 peers it reads the peer bitfield (O(1)), keeps a count per
piece and a counter of high-priority pieces, and touches only the pieces a disconnecting
peer had. On three public ISOs at 9.4 MB/s it is 9 % of the CPU. Pinned by
`TestHoldersMatchAReferenceModel` (20,000 random have/don't-have/disconnect steps).

**Automatic peer cap.** The daemon sizes the per-torrent cap from the machine — ten
connections per core plus fifteen, bounded to 15–200, halved below 4 GiB of RAM and under
768 MiB available — and trims a torrent that keeps at least ten peers with no progress
for two minutes to half. It is applied live with `SetMaxConnections` (no session restart)
every 30 s; the configured value stays the ceiling and a per-torrent limit wins
(`cmd/gx-torrent/connauto.go`). Same public swarm (Ubuntu ISO, 90 s window, two
interleaved rounds, `GX_MEASURE_AUTO=1` / `GX_MEASURE_MAXCONN`), three policies:

| Policy | Peak peers | Downloaded | CPU per 100 MB |
|---|---|---|---|
| automatic (55 on this host) | 25 / 37 | 221 / 604 MB | 4.7 / 3.7 s |
| fixed 200 | 78 / 89 | 314 / 625 MB | 4.5 / 3.9 s |
| fixed 25 | 25 / 25 | 497 / 548 MB | 4.5 / 5.7 s |

A cap in this range costs no throughput — the spread is swarm luck, and one public pass
cannot rank a parameter (§1) — while the established peers, and with them the absolute
CPU (about 0.1 % of a core per peer), stay bounded. The automatic cap did not bind in
these runs (peers stayed below 55), so the table shows that 25–55 per torrent is safe,
not the trimming path: that is covered by `TestDaemonAutoConnectionsFollowProgress`.

**Session budget with many torrents.** The cap of one torrent does not bound the CPU when
many run: the session also has a budget of 75 connections per core (300 here) shared by
weight among the running torrents, and a seed closes seed-to-seed connections at once.
Nine public torrents together, all active, 150 s window, two interleaved rounds per
policy, CPU from `getrusage` of the test process (`GX_MEASURE_REAL=<list>`):

| Policy | Peak peers | Downloaded | CPU (share of a core) | CPU per 100 MB |
|---|---|---|---|---|
| budget (33 per torrent with 9) | 145 / 184 | 1028 / 1234 MB | 26.9 / 41.6 % | 3.9 / 5.1 s |
| fixed 55 per torrent | 243 / 272 | 1235 / 1114 MB | 36.3 / 41.3 % | 4.4 / 5.6 s |

On average the budget holds 36 % fewer peers (165 against 258) and spends 12 % less CPU
(34 % against 39 % of a core, 4.5 against 5.0 s per 100 MB) for 4 % fewer bytes, inside
the round-to-round spread of the same policy (±10 %). A torrent just added runs without a
cap for up to 30 s, so the first runs of a campaign can show the automatic policy peaking
higher than the fixed one.

**uTP retransmission.** A 5 ms minimum-RTT floor keeps a fast path from collapsing the
RTO; a per-connection backoff doubles on timeout and halves on delivery; a healthy
connection never timeout-resends. The listen socket keeps a 50-SYN backlog bound that
never binds on the real swarms tested (`utp_syn_drops` 0).

### 5.2 Daemon at rest

**Current figures** (live service; `GEXTTO_PPROF`, `strace` and `/proc`): idle CPU
**0.46 %** of a core over 120 s with **71.1 MB RSS**, 0 directory opens per 20 s and no
write probe; `torrentEventWorker` 90 ms per 60 s; the first minutes after a start 0.39 %
of a core, stale-temp sweep 0.21 s; ~5,000 metadata calls a day for 55 series.

**Choices behind these figures.** The `.torrent` copies are trusted for 5 minutes (or
until the name or the copy folder changes) instead of being stat'ed at every 1.5 s poll.
The Salute tile and the health check share a 30 s cache and use `access(W_OK)` on network
shares instead of writing, so the NAS disks can spin down. A consistent pack-identity
verdict is reused for 60 s. The stale-temp sweep uses `WalkDir` and stats only temporary
names. One TMDB transport is shared and the calendar is bounded to 8 lookups with a
15 min cache. The HTTP transports are shared and rebuilt only when the egress rules
change: a transport per fetch opened a TCP and TLS connection for every feed poll and
`.torrent` download and, with `IdleConnTimeout` at zero, left its idle connections
behind (`outboundClient`, `netx.HTTPTransport`). `RemoveSeededCompleted`,
`DetachCompletedArchivedSingles` and
`DetachErrorTorrents` query only what the session view does not already rule out. The
recovery pass remembers a "not to detach" verdict for 30 s instead of querying the
database and stat-ing the archive for every seeding torrent at every 750 ms tick.

### 5.3 Feed archive

**Current figures** (live `gextto_archive.db`): 258 MB, 307,477 rows, one row per
torrent (infohash); insert 40.1–40.4 ms per 1,000 releases; Archive page 7–23 ms;
standalone feed search 7.4 ms on 100,000 releases.

**Choices behind these figures.** Uniqueness is on `magnet_key`, the first 16 bytes of
the SHA-256 of the exact magnet: the dedup is unchanged without a 400-byte index. Rows
with the same infohash are the same content, so the newest is kept, base32 hashes are
stored as hex and a partial UNIQUE index enforces it; a known torrent under another
magnet refreshes its row. A `.torrent`-only listing is keyed by the indexer guid (when
present), because Jackett regenerates its `/dl/?path=...` URL at every request; URL rows
with no guid still never merge. Inserts are prepared per chunk (`INSERT OR IGNORE` plus a
conditional refresh), faster than a two-conflict upsert. The plain title index is gone:
titles match through FTS or `lower(title) LIKE`. SQLite's `cache_size` stays at the
default 2 MB: on the real archive DB the latency is flat within noise even on a broad
`MATCH` (~1.14 s), because the OS page cache serves the file.

### 5.4 Indexer managers

**Current figures** (`TestMeasureManagerLanes`, the live Prowlarr and Jackett, 30
titles, 6 at a time): cycle 110 s (per title p50 2.1 s, max 100 s), 205 requests answered
and 0 failed, 5,935 releases.

**Why one indexer at a time** (same library, same host): an aggregate with a broken
indexer enabled waits for the timeout (60 s) and returns nothing; a healthy indexer
called directly answers in 0.25–2.4 s with 40–100 releases; the aggregate, with the
broken indexer disabled in the manager, answers in 2–4 s with 156–240. Six concurrent
aggregates with timeouts snowball until even a single search waits 120 s, recovered once
the backlog drained.

**Why one aggregate through indexarr.** Expanded into one lane per indexarr source, every
title became 46 requests and a 104-title search took 3 min 40 s, held back by the two
slots of its slowest source; through indexarr's own `/api` the same title is answered in
2.6 s, the indexer phase goes from 3 min 40 s to 1 min 23 s and the whole cycle from 12
to 5 minutes.

**Choices behind these figures.** Each manager indexer is a lane, read from the manager
at run time (10 min cache, never copied into the configuration): Prowlarr
`/api/v1/search?indexerIds=`, Jackett the indexer's Torznab endpoint. Prowlarr serves one
search per indexer, so a lane has one slot (Jackett two), its own timeout (90 s behind
Prowlarr, 45 s direct) and its own health; a title skips a lane whose queue would outlast
its 100 s budget. A tracker present in both managers is searched once, through the faster
lane; Prowlarr's failing indexers are skipped; the aggregate is the fallback when the
list cannot be read.

### 5.5 The home line and the latency guard

**Current figures** (`TestMeasureLatencyGuard`, a second instance downloading three
public ISOs, 90 s phases; the other device is an HTTP download read and discarded):
alone on the line **8,503 KiB/s kept**, the trial at 8 %; with a competing download the
other device gets **6,769 KiB/s (+55 %)** by router counters, or **7,253 KiB/s (+66 %)**
by trials only.

**Choices behind these figures.** The guard reads the engine's cumulative `peer_wire_*`
counters, because the reported rate is averaged over tens of seconds, and probes a public
host every 2 s while Auto is in force. It learns the idle reference by trial: a latency
that falls after Gextto backs off means the queue was its own; one that stays high means
someone else is on the line. The router's UPnP WAN counters decide first when they answer.
The lowest reference is kept for 6 h, so one disturbed trial cannot blind the guard.

**Controller on a model line** (`TestLatencyGuardSitsBelowTheKnee`,
`GX_LOG_GUARD_KNEE=1`; 2026-10-26, build 2039). The line answers 20 ms up to its capacity
and 60 ms above it or uncapped; ten minutes (300 probes) after a one-minute warm-up:

| Line | Ping high, before | Ping high, after | Line used, before → after |
|---|---|---|---|
| 800 KiB/s | 228 of 300 | 66 of 300 | 96 % → 94 % |
| 4,000 KiB/s | 174 of 300 | 29 of 300 | 95 % → 96 % |
| 20,000 KiB/s | 172 of 300 | 6 of 300 | 95 % → 95 % |

The current controller probes Google/Cloudflare, ramps while the ping is good, cuts when
it rises, accepts an optional user line maximum (`torrent_line_max_*_kib`) and remembers
the knee, sitting at 90 % of it and probing once a minute. The extra cost is a few
comparisons per 2 s probe: no measurable CPU or memory.

### 5.6 Swarm: seeding, choking, pipelining

**Current figures.** The unchoker holds its properties on the synthetic harness (fast
downloaders in download, fast uploaders in seed, immediate fast unchoke, optimistic
unchoke that does not steal a slot and rotates, budget, fairness); the tick with 50 peers
costs ~1.1 µs, 896 B, 1 alloc. Local swarm (2 MiB, 3 leechers): **3.00× corpus** in
~24 ms; with mesh the leechers exchange 2.52 MB and the seed drops to **1.84×**.
Super-seeding is opt-in: it does not reduce the upload on a measurable swarm and costs
~24 % locally. Pipelining at RTT 50 ms, 256 KiB pieces: 305 KiB/s with one block in
flight, 3.8 MiB/s with the default queue, **6,756 KiB/s** with two pieces per peer
(×1.75 over single-piece).

**Choices behind these figures.** Choking stays the standard rate-based one: a real
libtorrent leecher is served at full speed on loopback, and the real-swarm half needs an
inbound port this host does not have. Super-seeding stays opt-in. Multi-piece per peer
shares one in-flight budget, and the write ring buffer keeps the disk from becoming the
bottleneck.

**Seeding dials leechers.** A completed torrent kept the peers a tracker sent only while
downloading, so it could serve only peers that connected to it, and a leecher behind a
NAT never got the data. While seeding the engine now dials up to `seedDialBudget` (10)
candidates, asks for peers on the completed announce and remembers the peers that answer
with a full bitfield, so a seed is not dialed twice. The dial path is cold (a few
addresses per announce); the local swarm served by the seed is **3.00× corpus in 24 ms**
and the real libtorrent leecher gets the whole file in 5.587 s. The real-swarm half of
the campaign stays open: it needs a public torrent with leechers that only a seed's
outgoing dial can reach.

### 5.7 DHT

**Current figures** (`GX_MEASURE_DHT=2m GX_MEASURE_DHT_INFOHASH=<Ubuntu>`): a fresh node
fills the table to **1,290 nodes in ~80 s**, runs 1,432 `get_peers` with 314 replies
(**21.9 %**) and returns 2 peers for the Ubuntu infohash (1 confirmed by more than one
subnet). A 5-minute Debian run settles at ~296 nodes (peak 525) with 4,703 queries and
1,670 replies (**35.5 %**), `subnet_share_max` 4–7 and `nodes_insecure_id` ~42 %.

**Choices behind these figures.** The routing table is not concentrated, so the
anti-Sybil cap stays a hint, off by default. Multi-path `get_peers` is closed: the
instrumentation exists, but `peers_confirmed_multi` is 0 in the real runs. A torrent
resolves its metadata in ~49 s with 12 peers; the rest is the routing-table fill (~30 s
cold), not the lookup.

### 5.8 Merge on real data

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
reusable, the candidate filter costs 265 ns and 0 allocations over 32 unrelated session
torrents.

### 5.9 Rejected designs and ideas not added

Closed with a measurement, so they are not retried without new data:

| Idea | Why it was closed |
|---|---|
| `io_uring` / AIO | the disk is at 5.8–7.4 GB/s, far above any network speed; Go exposes it only with a dependency or cgo, and the mmap path is already asynchronous |
| Write-back cache in the engine | the storage keeps up with the download (`write_cache=0` in the live service), so there is nothing to absorb; reopen only with storage slower than the network |
| Batched uTP datagram I/O (`recvmmsg`/`sendmmsg`) | on the loopback benchmark `Socket.writeTo` is 44 % of the CPU (58 % is `Syscall6`), but in the real-swarm profile it is **8.3 %** of the samples (36 % syscalls, reads and TCP included). Batching would remove at most that share, while adding a copy per datagram and a sender queue; at the live rate it saves well under 1 % of a core. Reopen only on a uTP-only session above 8 MB/s |
| Contiguous-write coalescing | the page cache already merges adjacent writebacks |
| Rarity buckets, watermark cursor in `pickFileEdge`, `FADV_RANDOM`, `/proc/meminfo` under `d.mu` | no measurement justified the cost (the `pickSequential` cursor did: §2.1) |
| `PieceDownloader` bitmask instead of its three maps | `BenchmarkPieceDownloaderLifecycle`: 0.66–1.10 ms/op, **18 KB and 15 allocs per piece**, the time almost all the 4 MiB copy into the piece buffer. At 100 MB/s that is about 450 KB/s of garbage: a bitmask would save nothing measurable |
| Upsert with two conflict targets (archive) | +17 ms per 1,000 releases against `INSERT OR IGNORE` + refresh |
| `idx_archive_title`, expression index on `lower(magnet_hash)` | never used by the queries; only write time and space |
| Async `state.json` (snapshot under the lock, marshal and write on a writer goroutine) | `BenchmarkSaveStateLocked`: 0.44 ms at 100 torrents, 4.1 ms at 1,000, 20 ms at 5,000, and the idle service rewrote the file once in two minutes. At 1,000 torrents the exclusive hold is 4 ms every few minutes (0.003 % of a core): a snapshot plus writer goroutine would have to get the ordering of concurrent saves right for less than that. Reopen only with thousands of torrents *and* frequent changes, or when a save shows up as a UI stall |
| Lock-free / sharded read cache | `BenchmarkCacheGetHit`: 330–370 ns and zero allocations per hit, 2.5 M hits/s with four goroutines contending. The lock costs ~0.2 % of a core at 100 MB/s of upload, which is the whole budget such a rewrite could win |
| One PEX loop per torrent instead of one goroutine per peer | a goroutine with a ticker costs ~4.3 KB and ~3 µs of CPU per wake on this machine; at the 60 s PEX period that is 0.0025 % of a core and 2–3 MB at 500 peers, against the kernel socket buffers of the same peers. The staged list is already bounded |
| Multi-path `get_peers` | `peers_confirmed_multi` is 0 in the real runs |
| Anti-Sybil cap on by default | the routing table is not concentrated and legitimate CGNAT/mobile subnets would be capped |
| Choking changes | reciprocation works locally and against libtorrent |
| Super-seeding by default | no upload reduction on the measurable swarms, and ~24 % slower locally |
| Ordering the merge candidates by reusable bytes | superseded by the dedup, no measurable gain |
| Latency guard: fixed 100 MB/s ceiling, current-rate ceiling, 64 KiB/s floor, low-peak anchor | the cap either locked to a slow sample or was an arbitrary number; the current controller ramps on the Google/Cloudflare ping, uses the user's line maximum (or 2× the delivered rate) and cuts only on a real latency rise |

**Not a measurement but a model:** replacing `internal/dht` with a third-party library was
considered and not done — the internal one shares a UDP socket with uTP, chooses that
socket's family and serves every torrent from a single request channel, where an external
library expects its own socket and a traversal per torrent. The state is in
[`evoluzione.md`](evoluzione.md) §6.

**Postponed, with a reason:** qBittorrent's `renameFile`/`renameFolder` (needs rename
support in the engine: a feature of its own); graduated file priority with mixed-priority
files (open item in [`evoluzione.md`](evoluzione.md) §8); qBittorrent's
`setAutoManagement` tuning.

## 6. Verification

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
interface. Verified in a systemd container (install, upgrade, restore offline in
dry-run).

### eD2k and Kad through aMule

eD2k goes through aMule. EC client against a real `amuled` 2.3.3 (Debian trixie
package) on the N97, throw-away `--config-dir`, ports not forwarded (Low ID, Kad
firewalled), `nodes.dat` and `server.met` copied from an existing aMule profile:

- login, status, preferences, server list, add / pause / resume / delete of a link,
  bandwidth limits, log and the refusal of a wrong password all pass
  (`GEXTTO_AMULE_EC=… go test ./internal/amule/ -run Live`, 0.05 s);
- after `Connect`, Kad and the eD2k server (eMule Sunrise, ~47,600 users) were up within
  ~15 s;
- `Series A S06E13` (placeholders): **14** Kad results and **12** global server results,
  each collected over a 40 s window, with the episode (WEB, HDTV, 1080p variants) at
  1–2 sources on a firewalled client.

**End to end with the service.** aMule 3.1.0 in Docker (`ngosang/amule`, bridge network,
High ID, Kad firewalled), driven by the running Gextto: a global server search for
`eMule0.70b Installer` returned 5 results in the eD2k page (the installer with 74
sources); `eMule0.70b-Installer.exe` (4.27 MB) added from the page completed in
**~3.5 minutes** into the incoming folder. aMule 3.x removed it from the download queue
at once and only shared it, which is why completions are looked up among the shared
files.

**The fallback gate.** Two independent 30-series samples put the real-fallback coverage
at **7–13 %** (2/30 and 4/30 with at least 3 sources, the default floor). That is below
the level that justifies a 6.5–10-month transfer engine: Kad stays an optional **manual**
search provider, `ed2k_fallback` stays off, and the T0/T1 spikes stay isolated from the
daemon. The question is reopened only by a material change in metadata/aliases or by a
much larger sample showing a different picture. Localized and original titles differ
(0 Kad hits for a localized name against 1 for the original in two spot checks), so the
automatic gap filler queries the configured name and its aliases in parallel.

### Gap fill on eD2k

aMule runs one search at a time and answers in about a minute on this library, so the
unit of cost is the query, not the episode. A series with 37 gaps across two seasons was
answered by **2 season queries, about 1 minute each**, which returned 35 usable copies;
one query per episode would have been 37 waits, spread over the deep pass at five gaps
per cycle (`gap_deep_max_per_cycle`) and shared with every other series with gaps. The
automatic pass makes the same query (`ed2kSeasonFallback`) for the season of the gaps it
picked and distributes the copies to every gap of it; the per-query time is aMule's,
unchanged by the number of episodes the answer covers.

### Dead torrent links

The quarantine of `.torrent` links that keep failing (see [`evoluzione.md`](evoluzione.md)
§2) costs **one query per cycle**, not one per release: the whole set is read once and
checked in memory. With 300 quarantined rows it measures about 0.42 ms and 88 KB
allocated per cycle (§4), against a cycle of minutes.
`RecordTorrentLinkFailure` runs only for the links that actually fail (tens per cycle at
most) and `ClearTorrentLinkFailure` only for the ones that are downloaded.

### Upgrade decisions on the live library

Copy of the live databases (read-only backup), N97, `nice -n 15`.

| Check | Result |
|---|---|
| Archived episodes whose stored score differs from a rescore with the current code | **0 / 1861** |
| Archived episodes that their own release title or file name would re-download | **0 / 1861** |
| Custom groups with capitals that never received their bonus | **4 / 6** |
| Feed releases replayed through the dry-run decision | **259** releases, **55** series, **1.46 s** |
| Archived episodes those releases would replace | **2** (1080p WEBRip AAC x265 → 1080p WEB-DL DDP 5.1 H.264, «source» rule) |

The upgrade decision reads the real quality of a library file with `ffprobe` once per
file and remembers it: `ffprobe -v error -print_format json -show_format -show_streams`
on a 1080p WEB-DL mkv of 1.9 GB takes **274 ms cold, 81 ms warm**. The probe runs only
for a candidate the decision already judged an upgrade and whose library row has no
MediaInfo yet; the result is stored in `episodes` or `movies`, so the periodic backfill
and the next searches do not repeat it. *Impatto sulla libreria* (the dry-run preview,
which never stores the probe because it does not write): 63 releases over 55 series in
**5.6 s** cold and **2.0 s** with the answers in the process cache.

### Media integrity scan

The file check (*Settings → Maintenance → File integrity check*) walks the library one
target at a time, so the query between two `ffmpeg` decodes must not add load: with 5,000
archived episodes, `MediaIntegrityTargets` returns one target in 99.3–100.5 µs/op. The
`ffmpeg` decode dominates the run; the periodic worker probes the configured number of
files per interval (one by default) and the manual *Check now* starts a pass, so the load
stays off the acquisition cycle and the daemon at rest. Later passes skip the files whose
size and modification time match the stored outcome: the cost is then one `stat` per
library file, not a decode.

The decode runs at about real time on the N97. A 60 s slice of a 12.2 GB 4K release
(3840×1606 HEVC 10-bit Dolby Vision, 24 fps, on the NFS library share) decoded in
**50.7 s of wall time** (1 m 49 s CPU across the four cores): about **1.18× real time**,
so the full 62-minute episode needs roughly **73 minutes**. The bound therefore grows
with the media duration (3× the runtime, minimum 30 minutes, cap 6 hours). The quick
check for 4K files (the default for 2160p) reads the whole file but decodes only the
keyframes, on one thread: frame threading together with `-skip_frame nokey` makes the
HEVC decoder report `PPS changed between slices` on healthy files. The same 60 s slice
took **2.5 s of wall time** (2.6 s CPU), and a whole 62.5-minute 5 GB 4K episode took
**4 m 09 s** with **no error output**, against roughly 74 minutes for the full decode.
Truncation and unreadable data are still found; a small defect between two keyframes can
be missed.

```
ffmpeg -nostdin -hide_banner -nostats -v error -threads 1 -skip_frame nokey -i "<file>" -map '0:v?' -map '0:a?' -f null -
```

The check judges a file by what `ffmpeg` writes on stderr, so with a source whose frames
are out of order the same line arrives thousands of times (a full 54-minute episode
produced 22,998, all the null muxer's timestamp complaint, with exit code 0). Building
the readable detail from a 64 KiB buffer of those lines takes 167–176 µs/op. Dropping
the line cut in the middle by the buffer limit and ignoring the sink's own lines cost
nothing measurable: the first runs once per file, the second is one prefix check per
line.

### Robustness

Torznab answers are decoded leniently (a raw `&` or an HTML entity in a title):
**2.22–2.32 ms** against **2.24–2.32 ms** strict on a 100-item answer, with identical
allocations. The uTP header refuses inputs shorter than 20 bytes instead of panicking,
the bencode screen bounds lengths and depth before the decoder, and the mmap fault guard
turns a fault into an I/O error with the same write cost (**35.9–37.5 µs, 0 alloc**).

### Packaging

The release archive root is **0755** (`mktemp -d` made it 0700, which tar applied to the
extracted folder) and `build-release.sh` checks it; `install.sh` keeps `gexttod.prev`
**and** `gx-torrent.prev`, as `gexttod --update` does.

## 7. Adding a measurement

- Next to each figure: the machine, the date, the build and the command that produced
  it, so the measurement can be repeated and compared.
- A new round **replaces** the row it measures: update §2 for a comparison, §3 for a
  baseline and §4 for a benchmark. Do not keep an older pass next to the new one; if a
  number is gone from the document, the command that produced it is still there.
- If a measurement decides something, add it to §5 and, when rejected, to §5.9.
- Compare before and after with `-count`/`benchstat` or an interleaved A/B. One pass on a
  public swarm cannot rank a parameter.
- `make bench-all` runs the whole Go suite in one reproducible pass, on the host or in a
  pinned container; `scripts/measure-baseline.sh` runs the cheap harnesses and the
  network ones when their environment is set. A round that changes nothing still re-runs
  them, so a regression shows up as a difference from the previous run.
