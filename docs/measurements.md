# Measurements

How gx-torrent compares with qBittorrent, Transmission and Deluge, what Gextto costs on a
small machine, and how to repeat every number. The figures are the latest ones, not a
history: a new round replaces the row it measures.

## Short answer

Measured on 2026-11-06 on a low-power mini PC (Intel N97), in Docker, with the same
torrents and limits for every client.

| Question | 1st | 2nd | 3rd | 4th | Why it matters |
|---|---|---|---|---|---|
| Fastest download with nothing in the way | **gx-torrent**, 285 MB/s | Deluge, 250 MB/s | qBittorrent, 241 MB/s | Transmission, 56 MB/s | a fast disk and a fast peer |
| Download on a 100 Mbit/s line, 4 peers, TCP | **gx-torrent**, 11.2 MB/s | Deluge, 10.7 MB/s | qBittorrent, 10.6 MB/s | Transmission, 9.4 MB/s | the common case at home |
| The same line, uTP only | Deluge, 11.8 MB/s | qBittorrent, 11.6 MB/s | **gx-torrent**, 11.2 MB/s | Transmission, 9.6 MB/s | peers that prefer uTP |
| Smallest peak own memory while downloading at full speed | Transmission, 11 MiB | qBittorrent, 25 MiB | **gx-torrent**, 30 MiB | Deluge, 59 MiB | what the client cannot give back to the system |
| Smallest peak own memory while downloading on a 100 Mbit/s line | qBittorrent, 9.5 MiB | Transmission, 10 MiB | Deluge, 39 MiB | **gx-torrent**, 40 MiB | the same, when the line is the limit |
| Fastest upload | **gx-torrent**, 242 MB/s | qBittorrent, 236 MB/s | Deluge, 207 MB/s (up to 259) | Transmission, 125 MB/s | only matters above a gigabit link |
| Fastest recheck of 4 GiB | **gx-torrent**, 3.6 s | Transmission, 3.7 s | Deluge, 4.6 s | qBittorrent, 5.6 s | after a move or a crash |
| Lowest CPU at rest, nothing loaded | qBittorrent, 0.04 % | **gx-torrent**, 0.06 % | Deluge, 0.29 % | Transmission, 0.32 % | a machine that is always on |
| Lowest CPU at rest, 100 torrents | qBittorrent, 0.06 % | Transmission, 0.33 % | Deluge, 0.35 % | **gx-torrent**, 0.66 % | the same, with a long list |
| Lightest with 100 torrents | Transmission, 19 MB RSS | **gx-torrent**, 29 MB RSS | Deluge, 68 MB RSS | qBittorrent, 119 MB RSS | a long seeding list |

The order is the order of the medians: where the ranges of two clients overlap the positions are
a tie, and a gap of a few percent is inside the spread of the rounds. The detailed tables below
give the ranges. **gx-torrent** is in bold.

**Own memory** is the anonymous memory the client really holds, without the file pages that RSS
also counts; the 100-torrent row is the only one that uses RSS. The full-speed row is the 4 GiB
download from one peer, the 100 Mbit/s row the 512 MiB download from four peers shaped to that
line.

The upload row is the last full round. After the read-cache change of 2026-11-06 gx-torrent's
own memory while uploading is 22 MiB and the speed 288–318 MB/s, but the other clients were not
run again ([details](#what-this-round-changed)).

In short: gx-torrent is the fastest downloader over TCP, is level with Transmission at a recheck
(3.6 s against 3.7), and uploads as fast as the libtorrent clients. Its read cache now starts
small: while uploading it uses 22 MiB of own memory instead of 58, at 288–318 MB/s. It is level
with Deluge on the 100 Mbit/s line, and with 100 torrents it spends the most CPU at rest
(0.66 % against 0.06–0.35 %). Transmission spends less CPU on every download, at a lower speed.
The tables are the round of 2026-11-06 on commit `40e58927`; the read-cache change is measured
separately [below](#what-this-round-changed).

## What is best for me

One line per need, with the client that answers it and why. Every figure behind these lines is
in this document.

| Your need | Pick | Why |
|---|---|---|
| A library, not a downloader: episodes, gaps, renames, upgrades, archive | **Gextto** | it decides what to fetch and where it goes; none of the other clients does |
| One torrent from a browser, on a NAS, a server or a phone, with a qBittorrent-style API | **gx-torrent, standalone** | the same engine with its own page and `/api/v2`, no library to configure |
| The fastest download over TCP | **gx-torrent** | 285 MB/s, against 56–250 for the others |
| A recheck of a 4 GiB file without a long wait | **gx-torrent or Transmission** | 3.6–3.7 s, the two are not separated; qBittorrent 5.6 s |
| Seeding on a fast LAN (gigabit and above) | **gx-torrent** | one write is one syscall: 242 MB/s, level with the libtorrent clients, at the lowest CPU per GiB (2.26 s against 2.46–2.70) |
| A call or a game while something downloads | **Gextto**, Auto with latency source `both` | the guard leaves room for the other flow and stops taking more as soon as the router sees it |
| The smallest memory at full speed | Transmission, 11 MiB against 30 | the one row where another client wins, and it costs speed |
| Ratio farming, a seedbox, no library | qBittorrent or Deluge | Gextto is a library: it seeds with ratio, day and idle rules and has super-seeding, which *lowers* the upload by design, but the on-demand `share_mode` is not planned |
| Windows | **gx-torrent, standalone** | native service, amd64 and arm64; Gextto itself is Unix |

In short: a client that downloads and stops there is gx-torrent, the fastest of the four over
TCP and level with Transmission at a recheck (3.6 s against 3.7 and 5.6); a client that names,
upgrades and keeps the files is Gextto, which is the only one that does it — and carries
gx-torrent inside.

## Compared with qBittorrent, Transmission and Deluge

### What was run

Every client runs in its own container with the same limits, against the same reference
peers, on an isolated network. Each table is the **median of three interleaved rounds**,
with the range in brackets where it matters. Every downloaded file was compared byte by
byte with the original.

| Client | Version | Engine |
|---|---|---|
| gx-torrent | commit `40e58927` | gx-core, pure Go |
| qBittorrent | 5.1.0 (Debian 13) | libtorrent |
| Transmission | 4.1.0 beta 2 (Debian 13) | its own |
| Deluge | 2.2.0 (Debian 13) | libtorrent 2.0.11 |

| Item | Value |
|---|---|
| Machine | Intel N97 (4 cores), 15 GiB RAM, Lexar NM620 NVMe, ext4, Debian 13, kernel 7.1.13, Docker 29.9.3 |
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
| gx-torrent | 0.06 | 7.7 MiB | 22 MiB |
| qBittorrent | 0.04 | 7.1 MiB | 35 MiB |
| Transmission | 0.32 | 2.4 MiB | 17 MiB |
| Deluge | 0.29 | 38 MiB | 63 MiB |

### Download at full speed

A 4 GiB file from one reference peer over TCP, with no rate limit and a cold disk cache: the
ceiling of each client on this machine.

| Client | Speed | Time | CPU per GiB | Peak own memory | Peak RSS |
|---|---|---|---|---|---|
| **gx-torrent** | **285 MB/s** (285–286) | 15.0 s | 3.10 s | 30 MiB | 42 MiB |
| Deluge | 250 MB/s (230–254) | 17.2 s | 2.85 s | 59 MiB | 3,963 MiB |
| qBittorrent | 241 MB/s (219–242) | 17.8 s | 2.71 s | 25 MiB | 3,510 MiB |
| Transmission | 56 MB/s (56–56) | 77.0 s | 2.20 s | 11 MiB | 26 MiB |

Transmission's 56 MB/s repeats in every round; its cause was not investigated.

### Download on a 100 Mbit/s line

A 512 MiB file from four peers, each shaped to 25 Mbit/s and 40 ms: a line of 12.5 MB/s
before protocol overhead.

| Client | Speed | Time | CPU (% of a core) | CPU per GiB | Peak own memory | Peak RSS |
|---|---|---|---|---|---|---|
| **gx-torrent** | **11.23 MB/s** | 47.8 s | 19 % | 17.9 s | 40 MiB | 54 MiB |
| Deluge | 10.66 MB/s | 50.4 s | 27 % | 27.8 s | 39 MiB | 575 MiB |
| qBittorrent | 10.55 MB/s | 50.9 s | 26 % | 26.0 s | 9.5 MiB | 543 MiB |
| Transmission | 9.44 MB/s | 56.9 s | 14 % | 15.8 s | 10 MiB | 25 MiB |

### The same line, uTP only

The same four peers with TCP blocked at the peer.

| Client | Speed | Time | CPU (% of a core) | CPU per GiB | Peak own memory | Peak RSS |
|---|---|---|---|---|---|---|
| Deluge | 11.80 MB/s | 45.5 s | 41 % | 37.9 s | 39 MiB | 571 MiB |
| qBittorrent | 11.55 MB/s | 46.5 s | 39 % | 36.0 s | 9.7 MiB | 543 MiB |
| gx-torrent | 11.21 MB/s | 47.9 s | 39 % | 37.5 s (37–38) | 51 MiB | 65 MiB |
| Transmission | 9.59 MB/s | 56.0 s | 22 % | 24.2 s | 8.6 MiB | 24 MiB |

On uTP gx-torrent now spends what the libtorrent clients spend and is 3–5 % slower than them.

### Recheck and upload

Both start from the same 4 GiB file with the disk cache dropped: first an explicit recheck,
then an upload to one unshaped reference peer.

| Client | Recheck | CPU | Upload speed | CPU per GiB | Peak own memory | Peak RSS |
|---|---|---|---|---|---|---|
| gx-torrent | 3.6 s (1,182 MB/s) | 3.9 s | 242 MB/s (242–262) | 2.26 s | 58 MiB* | 72 MiB* |
| Transmission | 3.7 s (1,151 MB/s) | 2.5 s | 125 MB/s (124–125) | 2.67 s | 35 MiB | 50 MiB |
| Deluge | 4.6 s (939 MB/s) | 3.9 s | 207 MB/s (178–259) | 2.46 s | 44 MiB | 4,099 MiB |
| qBittorrent | 5.6 s (761 MB/s) | 3.7 s | 236 MB/s (203–240) | 2.70 s | 13 MiB | 4,087 MiB |

On the recheck gx-torrent (3.35–3.81 s) and Transmission (3.66–3.93 s) overlap: the two are tied
and ahead of Deluge (4.6 s) and qBittorrent (5.6 s).

The upload ranges are wide for every client, because repeated 4 GiB writes by the receiving peer
slow this disk over a session. The gap between gx-torrent and qBittorrent (242 against 236 MB/s,
3 %) is smaller than the spread of their rounds, and Deluge reaches 259: none of the three is
separated.

\* 22 MiB of own memory and 36 MiB of RSS after the read-cache change below.

### 100 torrents

A hundred torrents of 2 MiB each, with their files already in place: time to add them, time
until all are seeding, then 60 s at rest and the time to come back after a restart.

| Client | Add | All seeding | Restart | CPU at rest | Own memory | RSS |
|---|---|---|---|---|---|---|
| Transmission | 0.9 s | **0.9 s** | **0.2 s** | 0.33 % | 4.2 MiB | 19 MiB |
| gx-torrent | 1.5 s | 2.1 s (2.0–4.3) | 0.5 s | 0.66 % | 14 MiB | 29 MiB |
| Deluge | 3.2 s | 3.3 s | 1.0 s | 0.35 % | 42 MiB | 68 MiB |
| qBittorrent | 0.9 s | 100.8 s | 0.6 s | **0.06 %** | 90 MiB | 119 MiB |

Transmission reaches seeding at once because it trusts existing files until asked to recheck.

### What this round changed

After the round above, gx-torrent's read cache starts at 8 MiB instead of 32 and grows by
8 MiB for each active seed: before a seed exists there is nothing to reuse, so the daemon does
not reserve the memory a seed may need. The other clients were not run again, so the tables keep
the last full round. These are gx-torrent only, same harness and payload, two valid rounds before
(the third found the network down) and three after, 2026-11-06.

| Measure | Before | After |
|---|---|---|
| Peak own memory, upload | 55–59 MiB (median 57) | **22–24 MiB** (median 22) |
| Peak RSS, upload | 70–74 MiB (median 72) | **36–38 MiB** (median 37) |
| Upload speed | 179–283 MB/s | **288–318 MB/s** |
| Upload CPU per GiB | 2.32–2.50 s | **2.31–2.45 s** |

The upload speed did not fall: the fastest before round (283 MB/s) is close to the slowest after
one (288 MB/s), and one before round was the slow outlier the disk produces. The recheck is
unchanged. At rest and with 100 torrents the read cache is empty, so those rows do not move.

### What these numbers do not say

- **No ranking on a real swarm.** These runs use few peers, no encryption from the peers and
  no discovery. Behaviour among hundreds of peers, choking included, is not measured here.
- **Settings are defaults**, with three exceptions the setup needs: discovery off everywhere,
  a fixed peer port, and Transmission's uTP off in the TCP scenarios (it otherwise waits on
  uTP against a TCP-only peer).
- **Transmission is a beta**, the version Debian 13 ships.
- **Small differences are not results.** A gap inside the ranges above means nothing.
- **Deluge's web front end** runs in a separate container, so its CPU and memory are not
  counted.

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

Same N97 machine, outside Docker unless stated. Every figure is the latest round on this
machine (2026-11-03 for the engine measurements, 2026-11-06 for the live at-rest ones).

### Footprint

| What | At rest | Under load |
|---|---|---|
| gx-torrent 1.1.331, live (DHT on, full IP filter, 3 seeds) | 0.3–0.5 % of a core, 28–30 MB RSS (16–18 MB anon) | 18–37 % of a core at 8.2–8.8 MB/s with 116–132 peers, 2.0–4.3 s of CPU per 100 MB |
| gx-torrent, 10 torrents to a NAS | | about 507 MB outside the two caches, 1.1 GB peak with the NFS write cache |
| gexttod 1.2.2260, same host | 0.4 % of a core, 45 MB anon (70 MB RSS) just started; 97 MB anon (123 MB RSS) after a full search | peak 168 MB anon during a full cycle of 47,342 releases (series and movies), about 69 s of CPU, and back to the figure at rest; a series-only cycle of ~38,800 releases peaks at 149–157 MB anon and leaves 85 MB |

At-rest figures: three samples of 2, 5 and 10 minutes on 2026-11-06 for gx-torrent (DHT on, full
IP filter, three seeds, nothing transferring) and 90 s for gexttod with the integrity check off
and no transfer. The
gexttod row is the latest full cycle and the latest series-only cycle on 2026-11-06, with
GOGC=50, transparent huge pages off for the process and
`GODEBUG=madvdontneed=1`.

- **After a burst** the daemon returns its piece buffers: with 7 torrents and no transfer the
  RSS falls from 550 MB to 35–40 MB.
- **The free heap comes back to the OS.** The same series cycle before the change peaked at
  237 MB anon and left 150–190 MB resident for hours; now it peaks at 149–157 MB anon and
  settles at 85 MB anon (168 MB and 97 MB for a full cycle of 47,342 releases). The explicit
  release after a cycle or a completed transfer returns 12–92 MB per call, and the 15-minute
  housekeeping call releases what the background workers free afterwards. Go does not
  compact, so the spans still pinned by live objects stay.
- **A cycle allocates 1.37–1.43 GB instead of 2.01 GB** (~38,800 releases, 2026-11-06;
  the spread is the varying size of the feed and indexer replies). The deduplication, the
  blocklist and the kind filter work in place, the combined feed and indexer batches are
  sized once, the XML reader no longer translates namespaces, the Torznab error check
  skips a body without the word, and the indexarr pages share one reused buffer instead
  of allocating a new one per page. What remains is the XML tokenizer inside
  `encoding/xml` (about 170 MB of internal copies per cycle; 7 % of the CPU) and the HTML
  parser.
- **Archive writes and gap queries** are now a small part of a cycle: the identities of the
  releases just read are probed against the archive's two unique indexes and their rows
  skipped, and the priority gap pass searches a season once instead of once per episode. A
  series cycle of ~38,900 releases fell from 39–43 s of CPU to 29 s on the same host
  (2026-11-06). The probe covers the batch, not the archive: 1,000 releases a large archive
  already holds cost **9.3 ms against 19.3 ms** with no probe at all, where reading the whole
  archive first cost 65 ms and grew with it (50,000 rows, 2026-11-06).
- **`systemctl status` shows a large number** that is mostly page cache, not the process;
  `anon` is the real figure (85 MB after a series search, 97 MB after a full one on
  2026-11-06). A `MemoryMax` on the unit would count the file pages, so it must leave room
  for them.
- **At rest gexttod** opens no directory and writes nothing to the NAS, so its disks can spin
  down.
- **Where the CPU goes under load** (five public ISOs): about 50 % syscalls and scheduler
  wake-ups, 9–10 % MSE encryption, 5–7 % piece hashing, 2–3 % TLS of web seeds, 9 % the piece
  picker with about 200 peers.

### Name parsing

The quality/language reader runs on every release name and on every filename the library
touches. On this machine, 2026-11-06, `BenchmarkParseQuality` over five typical names:
**95 µs against 696 µs** before the word, separator and episode scans replaced the regexes
(7.3×), and **15.0 KB / 208 allocations against 21.6 KB / 361**. The release-title matchers
skip the ten anchored regexes for a name with no season marker, and the SxxExx, date and
pack shapes are read without them.

In a cycle the regexp share of the CPU fell from about **39 % to 11 %** (profiles of
2026-11-06); `ParseQuality` itself is now about 2 % of the sampled cycle. SQLite and the
feed/HTML parsing are the larger costs.

### The web page

The gx-torrent main page, rendered in German on this machine (2026-11-05): **0.78 ms and
141 KB allocated** per page, 0.01 ms per small fragment, with the translations already in
place. Translating the finished HTML afterwards cost about 2 ms and a tokenizer pass per
page.

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
| Seeding a fresh Ubuntu release (137 seeds, 8 leechers), inbound port closed | 0 bytes uploaded by gx-torrent and qBittorrent, 4 alternated rounds of 3 minutes; qBittorrent held 6–11 leechers, gx-torrent (10 seeding dials) 0–1 | by hand: `scripts/gx-core-test.sh` next to qBittorrent 5.2.3 |
| Public DHT, 5 minutes | 389 nodes, 35.1 % of queries answered, no concentrated subnet | `make measure-dht` |
| Real libtorrent leecher, loopback | the whole 50 MiB file in 4.8 s | `make measure-choking` |
| Request queue at 50 ms RTT | 303 KiB/s with one block in flight, 6,743 KiB/s with the default queue | `make measure-pipeline` |
| Local swarm, 3 leechers | 3.00× the file uploaded; with leechers meshed the seed drops to 1.84× | `make measure-seeding` |
| Indexers, 30 titles | 110 s, 205 requests, 0 failed, 5,935 releases | `TestMeasureManagerLanes` |

The real-swarm figure comes from one campaign.

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
of 2026-11-03. Compare numbers only within one round: the absolute values follow the machine's
state more than the code.

| Path | Cost | What it means |
|---|---|---|
| Disk write, buffered / mmap (`BenchmarkStorageWrite…`) | 5.5–5.6 / 7.0–7.2 GB/s | far above any network speed; mmap is opt-in |
| 16 KiB writes on NFS, batched durability | 81 MiB/s (35 without) | one fsync per piece removed |
| Recheck of 4 GiB, cold, gx-torrent 1.1.296 | 3.8–4.0 s, about 4 s of CPU | reading and hashing side by side, read-ahead on |
| Upload to one peer, cold, gx-torrent 1.1.296 | 180–240 MB/s, 3.2 s of CPU per GiB | read-ahead on, next block loaded in the background |
| uTP download on a 100 Mbit/s line, gx-torrent 1.1.296 | 11.2 MB/s, 48 s of CPU per GiB | state packets batched over 2 ms |
| Piece check, v1 / v2, 4 MiB | 2.6 / 2.1 GB/s | one hash per piece |
| uTP over loopback (`BenchmarkTransferLoopback`) | 178–188 MB/s | single connection |
| uTP over loopback, 16 KiB writes | 11.2 µs of CPU per MB (3.7 s of system time for ~730 MB); throughput 126 MB/s | one segmented `sendmsg` per write (UDP GSO); the wall time is round-trip bound |
| Picker, rarest pick with 150 peers | 8.6 µs | one pick in a dense swarm |
| Peer departure, 27,250 pieces | 94–105 µs | touches only the pieces the peer held |
| Refill of 250 requests over TCP | 248–256 µs, about 5 writes instead of 250 | small messages batched |
| Read cache hit | 355–393 ns, no allocation | every uploaded block |
| Read cache miss, 16 KiB block | 675 ns and 168 B | the buffer of an evicted block is read into again; the remaining bytes are the benchmark's own key |
| Webseed, 8 files, 200 ms per request, 4 ranges | 0.8 s | one host serves four ranges at once; `GX_MEASURE=1 go test ./cmd/gx-torrent/ -run MeasureWebseed -v` |
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

The suite has 3,010 Go tests in 72 packages, 79 Playwright tests and 89 Go benchmarks (count
of 2026-11-06; build times of 2026-10-17, test times of 2026-10-22, CI of 2026-10-17).

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
| Peer turnover (closing the slowest peers at the cap) | on the Ubuntu ISO swarm the engine never filled the cap: peaks of 13–17 peers at 20, 5–6 at 8, against a default of 55, so turnover never acted | a real swarm that keeps a torrent at its cap |
| Auto cutting the caps when the router sees another device | a call (5 pings/s) next to a download filling the line: median 15 ms idle, 36–50 ms loaded, 2–6 of 300 lost; the spikes above 150 ms were there with Gextto idle too, from another device on the line | a call that degrades next to a download |
| Throttling TCP while uTP peers share the line (libtorrent's mixed mode) | Ubuntu 26.10, 3-minute rounds: mixed 5.9 and 7.7 MB/s, TCP only 3.9 and 6.7, uTP only 2.0 and 3.4; in the clean round mixed also had the lowest p90/p99 ping (67/163 ms against 79/850 for TCP only) | a mixed swarm where mixed loses speed or latency against one transport |
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
