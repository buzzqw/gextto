# Measurements

How gx-torrent compares with qBittorrent, Transmission and Deluge, what Gextto costs on a
small machine, and how to repeat every number. The figures are the latest ones, not a
history: a new round replaces the row it measures.

## Short answer

Measured on 2026-11-02 on a low-power mini PC (Intel N97), in Docker, with the same
torrents and limits for every client.

| Question | Best | gx-torrent | Why it matters |
|---|---|---|---|
| Fastest download with nothing in the way | **gx-torrent, 290 MB/s** | first; the others 56–250 MB/s | a fast disk and a fast peer |
| Download on a 100 Mbit/s line, 4 peers | Deluge and qBittorrent, about 10.6 MB/s | **last, 6.9 MB/s: a bug, now fixed, 11.2 MB/s** ([details](#what-the-comparison-changed)) | the common case at home |
| Least own memory at full speed | Transmission, 12 MiB | **27 MiB**, qBittorrent 24, Deluge 57; on the 100 Mbit/s line 36 against 10–39 | what the client cannot give back to the system |
| Fastest upload | Deluge and qBittorrent, about 230 MB/s | **125 MB/s**, now [180–240 MB/s](#what-the-comparison-changed) | only matters above a gigabit link |
| Fastest recheck of 4 GiB | Transmission, 3.9 s | **last, 8.8 s**, now [3.8 s](#what-the-comparison-changed) | after a move or a crash |
| Lowest CPU at rest | gx-torrent and qBittorrent, 0.05–0.06 % | among the best | a machine that is always on |
| Lightest with 100 torrents | Transmission, 19 MB | **second, 29 MB** (qBittorrent 119 MB) | a long seeding list |

In short: gx-torrent is the fastest downloader when the network is not the limit and stays
small while doing it, and it still spends more CPU on uTP than the others. The tables are the
round of 2026-11-02. The defects that round exposed are fixed since, and their effect is
measured [below](#what-the-comparison-changed); the tables are replaced at the next full round.

## Compared with qBittorrent, Transmission and Deluge

### What was run

Every client runs in its own container with the same limits, against the same reference
peers, on an isolated network. Each table is the **median of three interleaved rounds**,
with the range in brackets where it matters. Every downloaded file was compared byte by
byte with the original.

| Client | Version | Engine |
|---|---|---|
| gx-torrent | commit `13bad664` | gx-core, pure Go |
| qBittorrent | 5.1.0 (Debian 13) | libtorrent |
| Transmission | 4.1.0 beta 2 (Debian 13) | its own |
| Deluge | 2.2.0 (Debian 13) | libtorrent 2.0.11 |

| Item | Value |
|---|---|
| Machine | Intel N97 (4 cores), 15 GiB RAM, Lexar NM620 NVMe, ext4, Debian 13, kernel 7.1.13, Docker 29.9.2 |
| Client under test | CPUs 2–3, 4 GiB of memory limit |
| Reference peers | qBittorrent on CPUs 0–1, 1.5 GiB each, plaintext |
| Network | Docker bridge with no outside access; a private tracker; no DHT, PEX, LSD, UPnP or NAT-PMP |
| Host | ffmpeg stopped; a run with more than 1 core busy outside the test is thrown away and repeated |

Two kinds of memory appear below. **Own memory** (anonymous) is what the client really
holds. **RSS** also counts file pages mapped from disk, which the system takes back whenever
it needs them; that is why libtorrent clients show about 4 GB on a 4 GiB file without any
memory pressure.

### At rest

One client, no torrents, 60 s after a 15 s settle.

| Client | CPU (% of a core) | Own memory | RSS |
|---|---|---|---|
| gx-torrent | 0.06 | 7.6 MiB | 22 MiB |
| qBittorrent | 0.05 | 7.1 MiB | 35 MiB |
| Transmission | 0.30 | 2.4 MiB | 17 MiB |
| Deluge | 0.30 | 38 MiB | 63 MiB |

### Download at full speed

A 4 GiB file from one reference peer over TCP, with no rate limit and a cold disk cache: the
ceiling of each client on this machine.

| Client | Speed | Time | CPU per GiB | Peak own memory | Peak RSS |
|---|---|---|---|---|---|
| **gx-torrent** | **290 MB/s** (288–291) | 14.8 s | 3.05 s | 27 MiB | 39 MiB |
| Deluge | 250 MB/s (247–250) | 17.2 s | 2.92 s | 57 MiB | 3,979 MiB |
| qBittorrent | 242 MB/s (240–245) | 17.7 s | 2.70 s | 24 MiB | 3,972 MiB |
| Transmission | 56 MB/s (56–56) | 77.0 s | 2.18 s | 12 MiB | 26 MiB |

Transmission's 56 MB/s repeats in every round; its cause was not investigated.

### Download on a 100 Mbit/s line

A 512 MiB file from four peers, each shaped to 25 Mbit/s and 40 ms: a line of 12.5 MB/s
before protocol overhead.

| Client | Speed | Time | CPU (% of a core) | CPU per GiB | Peak own memory | Peak RSS |
|---|---|---|---|---|---|---|
| Deluge | 10.68 MB/s | 50.3 s | 30 % | 30.2 s | 39 MiB | 575 MiB |
| qBittorrent | 10.57 MB/s | 50.8 s | 26 % | 26.9 s | 9.5 MiB | 543 MiB |
| Transmission | 9.52 MB/s | 56.4 s | 14 % | 16.3 s | 9.6 MiB | 24 MiB |
| gx-torrent | **6.90 MB/s** | 77.8 s | 15 % | 22.7 s | 36 MiB | 50 MiB |

### The same line, uTP only

The same four peers with TCP blocked at the peer.

| Client | Speed | Time | CPU (% of a core) | CPU per GiB | Peak own memory | Peak RSS |
|---|---|---|---|---|---|---|
| Deluge | 11.66 MB/s | 46.1 s | 43 % | 39.7 s | 39 MiB | 575 MiB |
| qBittorrent | 11.59 MB/s | 46.3 s | 40 % | 37.0 s | 9.7 MiB | 543 MiB |
| Transmission | 9.60 MB/s | 55.9 s | 23 % | 25.4 s | 9.2 MiB | 24 MiB |
| gx-torrent | **6.88 MB/s** | 78.1 s | 45 % | 69.3 s | 44 MiB | 59 MiB |

uTP is where gx-torrent spends the most: 1.7–1.9 times the CPU per GiB of the libtorrent
clients in this round.

### Recheck and upload

Both start from the same 4 GiB file with the disk cache dropped: first an explicit recheck,
then an upload to one unshaped reference peer.

| Client | Recheck | CPU | Upload speed | CPU per GiB | Peak own memory | Peak RSS |
|---|---|---|---|---|---|---|
| Transmission | **3.9 s** (1,098 MB/s) | 2.6 s | 131 MB/s (130–144) | 2.87 s | 35 MiB | 50 MiB |
| Deluge | 4.6 s (940 MB/s) | 3.3 s | **238 MB/s** (237–292) | 2.48 s | 44 MiB | 4,101 MiB |
| qBittorrent | 5.6 s (765 MB/s) | 3.4 s | 223 MB/s (192–262) | 2.58 s | 13 MiB | 4,103 MiB |
| gx-torrent | 8.8 s (488 MB/s) | 4.0 s | 125 MB/s (115–125) | 4.79 s | 58 MiB | 73 MiB |

In this round a finished recheck waited for the 3 s queue tick before seeding again, so about
3 s of gx-torrent's time is that wait; it is gone since.

### 100 torrents

A hundred torrents of 2 MiB each, with their files already in place: time to add them, time
until all are seeding, then 60 s at rest and the time to come back after a restart.

| Client | Add | All seeding | Restart | CPU at rest | Own memory | RSS |
|---|---|---|---|---|---|---|
| Transmission | 0.9 s | **0.9 s** | **0.2 s** | 0.29 % | 4.2 MiB | 19 MiB |
| gx-torrent | 1.5 s | 3.4 s (3–24) | 0.5 s | 0.67 % | 14 MiB | 29 MiB |
| Deluge | 3.4 s | 3.4 s | 1.0 s | 0.33 % | 42 MiB | 68 MiB |
| qBittorrent | 0.9 s | 100.5 s | 0.5 s | **0.06 %** | 90 MiB | 119 MiB |

One gx-torrent round took 24 s to reach seeding where the other two took 3 s; its cause was
not investigated. Transmission reaches seeding at once because it trusts existing files
until asked to recheck.

### What the comparison changed

The round exposed defects in gx-torrent, all fixed since. Same setup, gx-torrent only, the old and
new code alternating on one binary, 2026-11-02. This is not a new ranking: the other clients were
not run again.

| Measure | Before | After | Cause |
|---|---|---|---|
| 100 Mbit/s line, TCP | 6.9 MB/s | **11.2 MB/s** | addresses with the same BEP 40 priority overwrote each other, so 2 of the 4 peers waited for the next announce |
| 100 Mbit/s line, uTP | 6.9 MB/s | **11.2 MB/s** | same cause |
| uTP CPU per GiB | 54 s | **48 s** | state packets batched over 2 ms instead of 0.5 ms |
| Recheck of 4 GiB | 8.8 s | **3.8–4.0 s** | read-ahead back on, a queue round when the check ends, reading and hashing side by side (0.8 s more CPU) |
| Upload | 110–125 MB/s | **180–240 MB/s** | read-ahead back on (most of the gain), next block read in the background (about 10 %) |
| Upload CPU per GiB | 4.9 s | **3.2 s** | same causes |
| Recheck after an add in seed mode | hashed nothing | **reads the data** | seed mode claimed every piece again on the restart |
| Download at full speed | 284 MB/s | 290 MB/s | no regression |

The public-swarm effect of the address fix was not measured. It should be smaller, because few
addresses collide and a large swarm has other peers to take over. The upload range is wide
because repeated 4 GiB writes by the receiving peer slow this disk over a session.

### What these numbers do not say

- **No ranking on a real swarm.** These runs use few peers, no encryption from the peers and
  no discovery. Behaviour among hundreds of peers, choking included, is not measured here.
- **Settings are defaults**, with three exceptions the setup needs: discovery off everywhere,
  a fixed peer port, and Transmission's uTP off in the TCP scenarios (it otherwise waits on
  uTP against a TCP-only peer).
- **Transmission is a beta**, the version Debian 13 ships.
- **Small differences are not results.** A gap inside the ranges above means nothing.
- **Deluge's web front end** runs in a separate container, so its CPU and memory are not
  counted. Two Deluge runs failed on a harness error (the web front end was not yet connected
  to the daemon); they were repeated after the fix, with the same image.

## How to repeat the comparison

```bash
make measure-compare     # about two hours; the machine must be otherwise idle
python3 scripts/compare/run.py report --tag <tag>
```

The rule is in [`AGENTS.md`](https://github.com/buzzqw/gextto/blob/main/AGENTS.md), *Build
and test*: same torrents (generated from a fixed formula, info hashes pinned in
`scripts/compare/run.py`), same pinned images (`scripts/compare/Dockerfile`), same limits,
quiet host, three interleaved rounds. Results are written to
`gextto-data/bench/compare.jsonl`. Changing the machine, a version, a limit or a payload
starts a new baseline: run every client again and replace every table above.

## Gextto and gx-torrent on their own

Same N97 machine, outside Docker unless stated. Dates are in the text only when they differ
from the most recent round (October–November 2026).

### Footprint

| What | At rest | Under load |
|---|---|---|
| gx-torrent, live (DHT on, 172,740 IP filter rules, 3 seeds) | 0.93–0.98 % of a core, 29–32 MB RSS | 18–37 % of a core at 8.2–8.8 MB/s with 116–132 peers, 2.0–4.3 s of CPU per 100 MB |
| gx-torrent, 10 torrents to a NAS | | about 507 MB outside the two caches, 1.1 GB peak with the NFS write cache |
| gexttod | 0.46 % of a core, 71 MB RSS | 232 MB RSS during a cycle of 31,275 releases, back to 65 MB after it |

- **After a burst** the daemon returns its piece buffers: with 7 torrents and no transfer the
  RSS falls from 550 MB to 35–40 MB.
- **`systemctl status` shows a large number** (2.0 GB, peak 4.5 GB) that is page cache, not
  the process; `anon` is 68 MB. A `MemoryMax` on the unit would count it, so it must leave
  room for it.
- **At rest gexttod** opens no directory and writes nothing to the NAS, so its disks can spin
  down.
- **Where the CPU goes under load** (five public ISOs): about 50 % syscalls and scheduler
  wake-ups, 9–10 % MSE encryption, 5–7 % piece hashing, 2–3 % TLS of web seeds, 9 % the piece
  picker with about 200 peers.

### Peer cap

CPU follows established peers (about 0.1 % of a core each) far more than bytes, when the line
is the limit. The daemon sizes the cap from the machine: **55 per torrent and 300 in all** on
this host. With nine public torrents, two interleaved rounds: **36 % fewer peers and 12 % less
CPU** than a fixed 55 per torrent, for 4 % fewer bytes, inside the ±10 % spread between rounds.

### Real swarms and the home line

| What | Result | Command |
|---|---|---|
| Ubuntu ISO on an FTTC 100/15 line | **9.3–9.4 MiB/s**, libtorrent 6.3–7.1 MiB/s on the same swarm; one campaign, so not a ranking | `make measure-realswarm` |
| Sharing the line (latency guard) | alone 8,503 KiB/s kept; another device downloading gets 55–66 % more | `TestMeasureLatencyGuard` |
| Public DHT, 5 minutes | 389 nodes, 35.1 % of queries answered, no concentrated subnet | `make measure-dht` |
| Real libtorrent leecher, loopback | the whole 50 MiB file in 4.8 s | `make measure-choking` |
| Request queue at 50 ms RTT | 303 KiB/s with one block in flight, 6,743 KiB/s with the default queue | `make measure-pipeline` |
| Local swarm, 3 leechers | 3.00× the file uploaded; with leechers meshed the seed drops to 1.84× | `make measure-seeding` |
| Indexers, 30 titles | 110 s, 205 requests, 0 failed, 5,935 releases; the old aggregate took 450 s, 35 answered, 25 failed, 2,737 releases | `TestMeasureManagerLanes` |

The real-swarm figure comes from one campaign and predates the [address fix](#what-the-comparison-changed).

### Reusing local data

A torrent sharing files with one already on disk fetches only what is missing. Real Debian
(756 MiB) and Arch (1.5 GiB) ISOs, source 75 % complete, one source piece in four corrupted:
**25.0 % from the network**, every piece verified, in 1.31 s and 2.71 s. The result is the
same after a restart, when the sibling is added later, and with a real libtorrent seeder. With
nothing to reuse the filter costs 265 ns over 32 torrents. `make measure-merge3`.

### Many files in one torrent

Over loopback from a second daemon (`TestMeasureManyFilesDownload`), CPU and memory of both
daemons together:

| Files | Size | Download | Average | CPU | Peak RSS |
|---|---|---|---|---|---|
| 1,000 | 0.15 GiB | 1.9 s | 85 MB/s | 0.65 core | 100 MiB |
| 6,000 | 0.92 GiB | 7.9 s | 125 MB/s | 0.88 core | 114 MiB |
| 6,500 | 3.72 GiB | 23.8 s | 168 MB/s | 1.37 cores | 121 MiB |

Each file costs one descriptor while the torrent runs, none after a stop. Checking 20,000
small files on disk takes 406 ms.

### Synology DS420j (a trial, not a supported target)

Realtek RTD1296 (4 × Cortex-A53), 987 MB RAM, HDD RAID, about 370 MB available before the
trial, `GOMEMLIMIT=250MiB`.

| Phase | gexttod RSS | gx-torrent RSS | Lowest available memory | Peak iowait |
|---|---|---|---|---|
| Idle, IP filter loaded | 37–66 MB | 20–21 MB | 318 MB | 7 % |
| Search of 4 titles, 1,703 releases | 66 MB | 129 MB | 240 MB | 22 % |
| 6 large torrents, 2 minutes | 37 MB | 209 MB | 160 MB | 48 % |
| 4 torrents, 8 minutes, 8–9 MB/s | 38 MB | 90 MB | 290 MB | 28 % |

Seeding, post-processing and several days of operation were not measured.

### The numbers behind engine decisions

A selection of the Go benchmarks: `make bench-all` runs all of them (six runs,
`GOMAXPROCS=2`) and writes the raw log and a `benchstat` table to `gextto-data/bench/`. Round
of 2026-11-02.

| Path | Cost | What it means |
|---|---|---|
| Disk write, buffered / mmap (`BenchmarkStorageWrite…`) | 5.5–5.6 / 7.0–7.2 GB/s | far above any network speed; mmap is opt-in |
| 16 KiB writes on NFS, batched durability | 81 MiB/s (35 without) | one fsync per piece removed |
| Piece check, v1 / v2, 4 MiB | 2.6 / 2.1 GB/s | one hash per piece |
| uTP over loopback (`BenchmarkTransferLoopback`) | 178–188 MB/s | single connection |
| Picker, rarest pick with 150 peers | 10–12 µs | one pick in a dense swarm |
| Peer departure, 27,250 pieces | 94–105 µs | touches only the pieces the peer held |
| Refill of 250 requests over TCP | 248–256 µs, about 5 writes instead of 250 | small messages batched |
| Read cache hit | 339–354 ns, no allocation | every uploaded block |
| `state.json` save, 100 / 1,000 / 5,000 torrents | 0.5 / 4.3–5.0 / 20–25 ms | held under the daemon lock |
| IP filter load, 172,000 rules | 30–36 ms, 6.6 MB | at start |
| Blocked-IP lookup | 20 ns | every incoming address |
| Archive insert, 1,000 releases | 43–44 ms | per feed batch |
| Archive search, 100,000 releases | 7.9–8.1 ms | standalone search |
| Re-score of 2,000 torrents | 55–57 ms | one transaction per 500 rows |
| Series match, 500 series | 122–138 µs | one release against the list |
| Prometheus scrape | 194–204 µs | only when scraped |

**Seed mode.** A torrent added in seed mode hashes a piece when a peer first asks for it. The
first block waits 0.19 ms at 256 KiB, 0.68 ms at 1 MiB, 3.4 ms at 4 MiB and 12.8 ms at 16 MiB
per piece (file in page cache; a cold read adds one piece of disk time). At most four checks
run at once per torrent.

## Build, tests and CI

| What | Time |
|---|---|
| `make build` from a cold Go cache | 42.5 s |
| `make fast`, one package changed / nothing changed | 7.1 s / 1.1 s |
| `make gx-torrent` | 0.5 s |
| `make test`, root package in 4 processes | 22–24 s (35 s with the test binary rebuilt, 15 s warm) |
| `make test-serial` | 34 s |
| `go test -count=1 ./...` from a cold cache | 117 s |
| `make test-ui` (Playwright, Chromium) | 63–72 s |
| CI (`ci.yml` / `continuous.yml` / `codeql.yml`), 2 vCPU runners | 411 s / 278 s / 210 s |

The suite has 2,866 Go tests in 69 packages, 80 Playwright tests and 83 Go benchmarks (count
of 2026-11-02; build times of 2026-10-17, test times of 2026-10-22, CI of 2026-10-17).

## Verified without a benchmark

- **Accessibility.** 14 axe-core tests pass and the gx-torrent pages had 0 violations in light
  and dark themes. Method in [`ACCESSIBILITY.md`](ACCESSIBILITY.md).
- **Backup and restore.** Round trip on a copy of the live data: all 40 tables of the four
  databases identical, integrity ok. A missing download or temp folder after a restore is a
  warning you can fix from the interface.
- **eD2k.** The EC client passes against a real `amuled` 2.3.3, and aMule 3.1.0 in Docker
  completed a download started from the page. Two samples of 30 series put real fallback
  coverage at **7–13 %** with at least 3 sources, too low to justify an own transfer engine:
  Kad stays a manual search, `ed2k_fallback` stays off. Reopen on a much larger sample or a
  material change in metadata.
- **Gap fill on eD2k.** A series with 37 gaps across two seasons needed 2 season queries of
  about a minute each, returning 35 usable copies; one query per episode would have been 37
  waits.
- **Upgrade decisions on the live library.** 0 of 1,861 archived episodes differ from a rescore
  or would re-download themselves. 259 feed releases replayed in a dry run took 1.46 s and
  would replace 2 episodes. `ffprobe` on a 1.9 GB file takes 274 ms cold, 81 ms warm.
- **Media integrity.** The full decode runs near real time (62 minutes of 4K in about 73
  minutes); the quick 4K check reads the file and decodes only keyframes, 4 m 09 s for a
  62.5-minute 5 GB episode, no false errors, and it can miss a small defect between two
  keyframes. A later pass costs one `stat` per unchanged file.
- **Robustness.** Lenient Torznab decoding costs nothing measurable, a short uTP header is
  refused instead of panicking, and the mmap fault guard turns a fault into an I/O error at
  the same write cost.

## Closed designs

Closed with a measurement, so they are not retried without new data.

| Idea | Why it was closed | Reopen when |
|---|---|---|
| `io_uring` / AIO | the disk does 5.5–7.2 GB/s, far above any network; needs cgo or a dependency | storage slower than the network |
| Write-back cache in the engine | the storage keeps up with the download | storage slower than the network |
| Batched uTP datagrams (`recvmmsg`/`sendmmsg`) | 8.3 % of samples on real swarms; saves under 1 % of a core at the live rate | a uTP-only session above 8 MB/s |
| Coalescing contiguous writes | the page cache already merges them | |
| Rarity buckets, `pickFileEdge` cursor | no measurement justified the cost | |
| `PieceDownloader` bitmask | 18 KB and 15 allocations per piece: about 450 KB/s of garbage at 100 MB/s | |
| Async `state.json` | 4 ms every few minutes at 1,000 torrents, 0.003 % of a core | thousands of torrents with frequent changes, or a visible UI stall |
| Lock-free read cache | the lock costs about 0.2 % of a core at 100 MB/s of upload | |
| One PEX loop per torrent | 0.0025 % of a core and 2–3 MB at 500 peers | |
| Multi-path `get_peers` | `peers_confirmed_multi` is 0 in real runs | |
| Anti-Sybil cap on by default | the table is not concentrated, and CGNAT subnets would be capped | |
| Choking changes | reciprocation works locally and against libtorrent | a gap on a real swarm |
| Super-seeding by default | no upload saved on measurable swarms, about 24 % slower locally | |
| Merge candidates ordered by reusable bytes | superseded by the dedup | |
| Upsert with two conflict targets (archive) | 17 ms more per 1,000 releases | |
| `idx_archive_title`, index on `lower(magnet_hash)` | never used by the queries | |
| Latency guard with a fixed ceiling or floor | locked to a slow sample or an arbitrary number | |
| A third-party DHT library | not a measurement but a model: ours shares the UDP socket with uTP and serves every torrent from one channel | |

Postponed: qBittorrent's `renameFile`/`renameFolder` (needs rename support in the engine) and
its `setAutoManagement` tuning.

## Adding a measurement

- **Compare clients only** with `make measure-compare`, and replace all the tables.
- **Compare two versions of our code** with `-count` and `benchstat`, or interleaved runs. A
  single pass on a public swarm says a feature works, not which value is better.
- **Measure the system as it really runs**, on a quiet host, with queues and auto-management
  on.
- **Write the machine, date and command next to a number**, and replace the old row.
- **Keep this page readable.** A number goes here only if a reader can decide something from
  it; the rest stays in `gextto-data/bench/` and in the command that produces it. A rejected
  idea goes under [Closed designs](#closed-designs).

The other harnesses are in the `Makefile` (`measure-*`, `bench-all`, `bench-all-docker`) and in
`scripts/measure-baseline.sh`. Profilers are opt-in on loopback: `GX_TORRENT_PPROF=127.0.0.1:6061`
and `GEXTTO_PPROF=127.0.0.1:6062`.
