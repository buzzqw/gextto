# Gextto — EXpert Torrent Transfer Orchestrator

**Tell Gextto what you want to watch. It finds the best release, downloads it,
verifies it, names it and shelves it in your library — then keeps watching for
a better copy.**

Gextto is a self-hosted media automation daemon: it searches, downloads,
verifies, renames and archives series, movies and comics. It has its own
BitTorrent engine in Go, a server-rendered web UI and a terminal client. Two
binaries, no C dependencies, no Node.js build, six languages.

[![License: EUPL-1.2](https://img.shields.io/badge/license-EUPL--1.2-blue.svg)](LICENSE)
![Go](https://img.shields.io/badge/Go-pure%2C%20zero%20CGO-00ADD8?logo=go)
![Tests](https://img.shields.io/badge/tests-2%2C189%20Go%20%C2%B7%2057%20E2E%20%C2%B7%2023%20fuzz-success)
![Platforms](https://img.shields.io/badge/platforms-Linux%20%C2%B7%20Windows%20(gx--torrent)-informational)
![BEPs](https://img.shields.io/badge/BitTorrent%20BEPs-34%20implemented-success)
![Languages](https://img.shields.io/badge/UI-IT%20%C2%B7%20EN%20%C2%B7%20DE%20%C2%B7%20FR%20%C2%B7%20ES%20%C2%B7%20PL-lightgrey)

> **Italiano:** [README.it.md](README.it.md) · **Manual:**
> [English](docs/MANUAL.en.md) · [italiano](docs/MANUAL.it.md) · **Issues:**
> [bugs and feature requests](https://github.com/buzzqw/gextto/issues) ·
> **Contribute:** [developer guide](docs/DEVELOPERS.md)

## What it does

Add a title and Gextto runs the chain by itself: it searches the sources you
configured, scores the candidates, downloads the winner, checks the data and
puts the file in the library under the right name. When a better release
appears it replaces the old file; when a download stalls it looks for another
one.

- Candidates are scored on resolution, source, codec, audio, HDR, language and
  size. A dead swarm cannot win over a working alternative, and every rejected
  release keeps its reason in the log.
- The library is protected by hardlinks while seeding, a mount guard that tells
  an unmounted share from deleted files, and a recycle bin.
- The engine is our own Go code: BitTorrent v1, v2 and hybrid, TCP and uTP on
  one port, holepunching, IPv6 DHT, streaming. On a real Ubuntu swarm it
  downloads at 9.3–9.4 MB/s where libtorrent gets 6.3–7.1 MB/s, and it idles at
  60–64 MB. The same binary is [gx-torrent](https://github.com/buzzqw/gextto/releases), a
  standalone client for Linux and Windows.
- Queue, cache and bandwidth follow the machine and the line. On Auto the whole
  line is Gextto's while nobody else needs it, and it gives way when someone
  does.

### At a glance

| | |
|---|---|
| **Speed** | 9.3–9.4 MB/s on a real Ubuntu swarm, against 6.3–7.1 MB/s for libtorrent on the same swarm |
| **Footprint** | 60–64 MB and 1–2 % of a core idle; 44–48 % of one Intel N97 core at 8–9 MB/s |
| **Protocol** | BitTorrent v1, v2 and hybrid (BEP 52) · 34 BEPs implemented and tested ([list](docs/bep.md)) |
| **Network** | TCP + uTP on one port · UPnP, NAT-PMP, PCP · BEP 55 holepunching · SOCKS5/HTTP proxy · VPN killswitch |
| **Automation** | TV series, movies, comics · Torznab (Jackett, Prowlarr) · RSS · web search · stuck-download replacement |
| **Integrations** | Jellyfin, Plex, Telegram, e-mail, Discord/Slack/ntfy/Gotify/Pushover, iCal |
| **Interfaces** | Web UI (SSR + HTMX, pure Go, installable PWA) · terminal TUI over SSH · JSON API |
| **Build** | Pure Go (`CGO_ENABLED=0`) · Linux x86_64/aarch64 · gx-torrent also on Windows |

It runs on a small machine: every figure here comes from a 4-core Intel N97 with
16 GB. The health probes do not write to the NAS, so its disks can sleep.

## A look

A test instance downloading five public, legal torrents: Debian, Ubuntu Server
and the Blender open movies *Big Buck Bunny*, *Sintel* and *Elephants Dream*.

![Downloads: five torrents in progress, filters, sorting and the bandwidth button](docs/screenshots/en/downloads.png)

| | |
|---|---|
| ![Bandwidth menu](docs/screenshots/en/bandwidth.png) | ![Torrent details, piece map](docs/screenshots/en/detail.png) |
| **Bandwidth.** What applies now and where it comes from; an exception for now, Auto or a limit, in one click. | **Torrent details.** General, trackers, content, peers, the piece map, limits and storage. |
| ![Dashboard](docs/screenshots/en/dashboard.png) | ![Speed rule in the settings](docs/screenshots/en/speed-rule.png) |
| **Dashboard.** The session at a glance, the live traffic and what needs checking. | **Settings.** One choice for the speed rule; the rows that depend on it appear only when they matter. |

<img src="docs/screenshots/en/phone.png" alt="Downloads on a phone" width="260" align="right">

On a phone the same pages reflow into cards, the menus open in place and the
actions on the selection stay within thumb reach. The web UI is also installable
as an app (PWA), and a terminal client covers the same work over SSH.
<br clear="right">

## Install on Linux

For 64-bit Linux with systemd: Debian 12+, Ubuntu 22.04+, Fedora, openSUSE
Leap 15.6+/Tumbleweed, Arch. Run as root:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

It installs to `/opt/gextto`, keeps data in `/var/lib/gextto` and serves the UI
on port 5000, with `gx-torrent` alongside `gexttod` — pure Go, nothing else to
install. The installer runs the program once first, so a missing library or an
old glibc is reported at once. The service runs as **the user invoking the
installer** by default; `--user gextto` uses an isolated system account and
`--media-group <group>` grants access to someone else's NAS library.

> [!IMPORTANT]
> Gextto is meant for a trusted network: the web UI and API are open by
> default. For access from outside, enable the optional login
> (*Configuration → Access and services*; the LAN stays free) and still use
> HTTPS via reverse proxy or VPN. Writes from a page of another origin are
> refused (CSRF protection). Read the [security policy](docs/SECURITY.md)
> before exposing it remotely.

### Install from source

Go 1.26.8+; the build is pure Go and the web UI is embedded. See the
[developer manual](docs/DEVELOPERS.md).

```bash
make build                 # gexttod, gx-torrent and gx-ed2k in bin/
```

`make build` advances the **committed** build number shared with CI, so a
checkout and a release show the same `1.1.<n>`; `gx-torrent` has its own
counter, advanced only when its code changes. `make test` runs the full suite,
`make test-ui` the browser suite, `make fuzz-smoke` the fuzzers.

### First safe run

1. Open `http://<server>:5000`: the first visit starts the **setup wizard**
   (password, folders with permission check, indexer and TMDB, first title).
   Reopenable at `/?view=setup`.
2. Keep **dry-run** on, add one test title and run a manual search.
3. Check **Health** and **Logs**, then enable active mode.

NAS setup and troubleshooting: the [user manual](docs/MANUAL.en.md).

### Update and uninstall

An **Update available** button appears when a release is published;
*Maintenance → Updates* shows the changes and **Update now** backs up the
databases, installs, and restores the previous version if the new one does not
answer. Re-running the installer updates from a shell too, with the same
rollback and the same service user. The full list of changes is in the
[changelog](changelog.en.md).

```bash
sudo bash install.sh --help       # all options
sudo bash install.sh --dry-run    # print what would happen, change nothing
sudo bash install.sh --uninstall  # stop and remove the program (keeps data)
sudo bash install.sh --uninstall --purge   # also remove the data directory
```

`GEXTTO_DATA_DIR`, `GEXTTO_PORT`, `GEXTTO_USER`, … stay supported;
`GEXTTO_LOCAL_ARCHIVE=/path/gextto-linux-x86_64.tar.gz` installs offline. From a
checkout: `git pull --ff-only && ./scripts/update.sh`.

## Features

### Finding and choosing releases

- **Quality-aware scoring.** Deterministic scores on quality, source, codec,
  audio, HDR/Dolby Vision, language and size; higher-grade library files are
  protected; an optional ceiling stops upgrades. The settings page scores
  reference releases live with the current weights. `ffprobe` reads the real
  quality of archived media.
- **TV, movies, comics.** Standard `ComicInfo.xml` for CBZ archives (Komga,
  Kavita, readers).
- **Season packs only when useful.** A pack is downloaded only to fill a real
  gap or upgrade an episode; one that would add nothing is refused before it is
  downloaded, and a pack still airing (*IN CORSO n/m*) is read within the
  episodes that exist.
- **Stuck-download recovery.** A download that stops progressing, or a magnet
  stuck waiting for metadata, is superseded by a valid alternative after a
  configurable window; the abandoned release is blocklisted.
- **Sources.** RSS feeds, HTML listings (ext.to, TorrentGalaxy, Corsaro),
  Torznab indexers and the Jackett/Prowlarr managers, web search, optional
  FlareSolverr. The managers are searched **one indexer at a time**, each at the
  pace it really serves, a tracker present in both only once, a slow tracker
  costing only its own results.
- **eD2k/Kad search, when torrents find nothing.** An optional, off-by-default
  search provider (`gx-ed2k`, our own pure-Go daemon) searches Kad and eD2k
  servers. Results show name, size, MD4 hash and source count; a pasted
  `ed2k://` link can be checked. The automatic gap-filling fallback is a second,
  separate switch and accepts only an exact episode match with a recognised
  quality and enough sources. Transfers are not implemented: the daemon says
  `download: false` and the "add" action stays disabled, so a missing episode is
  never replaced by a wrong file.

### Library safety

- **Hardlinks while seeding**: completed files appear in the library at once,
  with no duplicate disk use.
- **NAS and mount guard**: an unmounted share is never mistaken for deleted
  files, a burst of "missing" files is capped, a hung share cannot freeze the
  engine; transient failures retry and interrupted moves resume. Starting a
  download is crash-safe.
- **Recycle bin**: files replaced by an upgrade and data removed "with data" go
  to the trash first, after the torrent has left the engine.

### Bandwidth and the shared line

- **One rule, one exception**: the rule is a single choice — **Auto**, fixed
  limits or a per-weekday schedule (each day **Off / Limit / Auto**). From the
  Downloads page you make an exception for now — Auto or a limit, for 1 to 12 hours
  or until you remove it — and it always wins over the rule. *Auto* leaves the
  line uncapped so its real peak is learned.
- **Auto respects latency** (on by default): when the latency to the Internet
  rises, Gextto backs off for a few seconds to see whose queue it is. Its own:
  it keeps the line. Someone else's: it yields step by step and takes the line
  back when they are done. The router's UPnP counters, when present, tell who
  else is on the line; the Downloads page shows the guard while it acts.
- **Seeding that follows the tracker**: ratio, time and **inactivity** limits
  (a seed that uploads nothing for N minutes stops), and **seed rules** by
  source/indexer and category — a private tracker's rules apply to its torrents
  only, with the same behaviour on gx-torrent and qBittorrent. How to set them:
  [seeding limits](docs/MANUAL.en.md#seeding-limits-per-source-rules-and-idle-seeds).

### Integrations, control and interfaces

- **Notifications and media servers**: Telegram, e-mail, webhooks (Discord,
  Slack, ntfy, Gotify, Pushover), Jellyfin and Plex folder refreshes, an iCal
  feed of arrivals and air dates.
- **Traceability**: every download carries an **Acquisition ID** across
  search, selection, transfer, post-processing and archive logs; the dashboard
  **To check** panel keeps each warning while its download is active; a live
  30-minute bandwidth chart; alerts when a source keeps failing or slows down.
- **Interfaces**: responsive web UI installable as a PWA, keyboard-navigable
  and axe-core checked; poster view for series and movies; 20+ contrast-checked
  themes plus custom ones; a full terminal TUI for SSH that adapts to a phone
  screen. Six languages (IT, EN, DE, FR, ES, PL).

The web UI listens on `http://<host>:5000/`. Setup and every section are covered
by the [English manual](docs/MANUAL.en.md) and the
[manuale italiano](docs/MANUAL.it.md).

## Numbers

These figures come from the live installation and from real swarms, indexers
and the home line. Each one is in
[`docs/measurements.md`](docs/measurements.md) with the machine, the date and
the command to reproduce it.

| Area | Result |
|---|---|
| **Real swarm** | **9.3–9.4 MB/s** on the Ubuntu ISO, against **6.3–7.1 MB/s** for libtorrent on the same swarm (one measured campaign) |
| **Engine memory** | **60–64 MB** idle. With 10 torrents downloading to a NAS: **~507 MB** outside the two caches; the write cache sizes itself from free RAM |
| **Engine CPU** | **44–48 % of one core** at 8–9 MB/s with ~110–130 peers: **0.35–0.43 % per peer**, of which the piece picker takes 9 %; 1–2 % at rest |
| **Indexer searches** | Jackett and Prowlarr searched one indexer at a time: a cycle of 30 titles in **110 s**, **205 requests and none failed**, **5,935 releases** |
| **Sharing the line** | A saturated download adds ~40 ms of latency to the whole house. Alone on the line Gextto keeps all of it (**8.5 MB/s**); with another device downloading, that device gets **6.8–7.3 MB/s** of the 9 MB/s line |
| **Reusing local data** | A torrent sharing files with one already on disk takes only **25 %** of the file from the network (Debian and Arch ISOs), also after a restart and in any adding order |
| **Disk writes** | 16 KiB writes on NFS at **81 MiB/s** with batched durability; opt-in mmap ~7.0 GB/s on tmpfs |
| **Feed archive** | **447,000 releases in 320 MB**, one row per torrent; 1,000 releases archived in **41 ms**; the standalone archive searches 100,000 releases in **7.4 ms** |
| **Daemon at rest** | **~0.7 %** CPU at rest; **~5,000 metadata calls a day** for 55 series; the health probes never write to a NAS, so its disks can sleep |

The same document records what was measured and dropped — io_uring, batched
uTP I/O on this load, a write-back cache — so the same ideas are not tried
twice.

## Torrent engine

Gextto keeps the control plane (queue, scoring, post-processing, archiving) and
delegates transfers to one selectable engine (*Configuration → Torrent engine*);
switching is a coordinated migration.

| Engine | Where it runs | When to pick it |
|---|---|---|
| **gx-torrent** (default) | Separate supervised Go process | Pure Go, self-contained, v1/v2/hybrid, DHT and MSE/PE; also standalone on Linux and Windows; no WebTorrent/WebRTC |
| qBittorrent-nox | External daemon via its Web API, or one Gextto downloads and runs privately | You already run it or prefer its UI ([setup guide](docs/qbittorrent-nox.md)); also the first step of the automatic fallback |

**Automatic fallback.** If the active engine cannot stay up, Gextto first rolls
an updated `gx-torrent` back to the previous binary, then switches to a managed
qBittorrent-nox (downloaded on demand, SHA-256 verified) or an external one, and
finally to a recovery state with the UI up. A cooldown prevents ping-pong;
`GEXTTO_ENGINE=qbittorrent` (or `gx-torrent`) forces the engine for one run.

### gx-core and gx-torrent

`gx-core` (`internal/gxcore`) and its DHT (`internal/dht`) are original code
written in this repository under EUPL-1.2, with no third-party-derived code
([`THIRD-PARTY-NOTICES`](THIRD-PARTY-NOTICES) lists only linked libraries).
`gx-torrent` runs it as a supervised process that survives orchestrator
restarts: Gextto re-attaches when binary and options match.

- **Protocol.** BitTorrent v1, v2-only and hybrid (BEP 52) from `.torrent` and
  magnet (`urn:btmh:`), with SHA-256 Merkle verification. **34 BEPs implemented
  and tested** — [`docs/bep.md`](docs/bep.md) is the source of truth: wire
  protocol, Fast Extension, metadata, PEX with IPv6, multitracker, LSD,
  super-seeding, web seeds, private torrents, local-data hints, `lt_donthave`,
  UDP trackers with scrape, and the DHT suite (IPv6, scrape, secure node IDs,
  read-only, K/V store, `urn:btpk:`). Beyond the BEPs: `ws://`/`wss://` trackers
  and **DHT feeds** (`dht://<pubkey>`).
- **Local reuse.** Pieces and 16 KiB blocks another local torrent already
  verified — same layout or the same v2 file — are copied and re-checked instead
  of downloaded; private torrents are never mixed (BEP 27).
- **Network.** TCP and uTP (BEP 29) in parallel on one port, one UDP socket for
  uTP and DHT; adaptive uTP retransmission (a latency floor and per-connection
  backoff); `ut_holepunch`
  (BEP 55) as client and relay; UPnP, NAT-PMP and **PCP**; a port check that
  tells cone from symmetric NAT; SOCKS5 and HTTP CONNECT; an
  `outgoing-interface` killswitch for VPNs.
- **Self-management.** The active-download count follows throughput; slow and
  stalled torrents step aside; the disk cache sizes itself from RAM and storage
  class (NVMe, SSD, HDD, NFS); downloads pause before a folder fills up while
  seeding goes on; per-peer request queues adapt and the slowest idle
  connection is the one pruned.
- **Footprint by design.** Pieces are written through and the OS page cache
  coalesces I/O; batched `fdatasync`, never `O_SYNC` per write; sparse files,
  `fallocate`, RAM-disk temp folders, opt-in mmap; zero-allocation hot paths
  (in-place RC4, word-wise bitfields, zero-copy uTP); the IP blocklist is
  binary-searched with no allocation.
- **Streaming and safety.** Separate playback and readahead windows, live
  per-file priorities, HTTP Range with the right `Content-Type`; an opt-in
  **DLNA/UPnP media server** for TVs, with the NAS library and its posters;
  SSRF-safe web seeds and `xs=`/`as=`; quarantine for peers sending corrupt
  blocks; live per-peer diagnostics, I/O rates and Server-Sent Events.

Engine statistics are in **Health → Torrent engine** (`/?view=health`), in the
daemon's `GET /api/v1/stats` and in `GET /api/torrents/session-stats`.

## gx-torrent as a standalone client

gx-torrent is also a complete client without Gextto, like `qbittorrent-nox` or
`transmission-daemon` — **the same binary** Gextto drives, a simpler Gextto with
everything a standalone client needs.

- **qBittorrent Web API v2** (`/api/v2/...`, qBittorrent 5 calls included)
  for Sonarr, Radarr, Lidarr, Readarr and mobile managers; its own JSON API;
  **Prometheus metrics**
  (`/api/v1/metrics`) for Grafana or Home Assistant, and an **event stream**
  (`/api/v1/events`) for Node-RED, Home Assistant or scripts
  ([examples](docs/API.md#integrating-with-other-systems)).
- **Web UI** laid out like qBittorrent's: light and dark themes; status,
  category, tag and tracker filters; a column chooser (24 columns) with
  resizable columns kept on the server; a full context menu; a resizable detail
  panel (files grouped by folder, rename, peers); magnet preview while typing;
  undo after removal; keyboard shortcuts, drag-and-drop, phone-friendly cards
  and a **Statistics** page (session and all-time totals, 1 h/24 h/7 d graph).
  Every page axe-core checked.
- **RSS and automation.** A **Wanted** page
  that says per title what is downloaded, downloading or seen but skipped, and
  why each release would be taken or refused; an **RSS & Indexers** page with
  ordered PASS/FAIL rules, live match previews, rule templates, feed tests,
  pasted feed lists, and a local **archive** of everything the feeds returned.
- **Operations.** Limits, the per-weekday scheduler, latency auto-speed, one-click
  optimisation, IP filter with scheduled refresh, categories and tags, port test;
  seeding limits by ratio, time and **inactivity**, and qBittorrent's **content
  layout** (original, create subfolder, no subfolder) when adding.
- **Notifications** on the same channels as Gextto, with a test button.
- **Updates itself** from the CI builds when its own code changed, verified by
  SHA-256, with automatic rollback.
- **Platforms.** Linux x86_64/aarch64 (systemd unit, `run.sh`, unprivileged
  Docker image) and a native Windows service on amd64/arm64.

Open `http://<host>:8890/` for its setup wizard. Full reference: the
[gx-torrent technical document](docs/gx-torrent.md).

## Tests

What runs on every push (`make test`, `make test-ui`, `make fuzz-smoke` and CI):

| Layer | What runs |
|---|---|
| **Unit and integration** | 2,189 Go test functions in 426 files: engine, DHT, uTP, storage, scoring, post-processing, API, UI rendering. The TUI is driven against the real router |
| **End-to-end** | 57 Playwright tests drive the real daemon in a browser — pages, mobile, six languages, security, settings — including 14 accessibility tests with axe-core WCAG audits |
| **Fuzzing** | 23 network-facing parsers (DHT, bencode, magnet, BEP 10/52, trackers, Torznab, RSS, MSE) fuzzed in CI; every crash found becomes a committed seed |
| **Real-world harnesses** | 14 opt-in measurements: real public swarms, the public DHT, a real libtorrent peer, local merge on real ISOs, choking and pipelining, the live Jackett/Prowlarr, the latency of the home line |
| **Static guards** | Every UI button points at a registered route, every internal call resolves on the real router, every setting is writable, translated in six languages and has its tooltip, every API route is documented |
| **Platforms and packaging** | CI cross-compiles engine and daemon for Windows and runs the Windows platform tests and a real service install; the installers are self-tested (every flag, a dry-run that changes nothing) and the standalone Docker image is built and smoke-tested; CodeQL scans every push |
| **Benchmarks** | 45 Go benchmarks on the hot paths (picker, storage, uTP, archive, merge, logging), run before and after every data-path change |

## Accessibility

Keyboard navigation, accessible names, dialog focus management, semantic
sortable tables, live announcements, responsive reflow and contrast-checked
themes, covered by axe-core and Playwright (`cd uiweb/end2end && npm ci && npm
run test:a11y`). This is not a legal certification: manual screen-reader and
assistive-technology testing is still required
([scope, tests and limits](docs/ACCESSIBILITY.md)).

## Documentation

| Need | Document |
|---|---|
| Operate the service | [English manual](docs/MANUAL.en.md) · [Manuale italiano](docs/MANUAL.it.md) |
| What changed | [Changelog](changelog.en.md) |
| Measurements and method | [Measurements](docs/measurements.md) |
| State, decisions and what is next | [Evolution](docs/evoluzione.md) |
| Architecture and design | [Architecture](docs/ARCHITECTURE.md) |
| BitTorrent specifications (BEPs) | [BEP compliance](docs/bep.md) |
| NAS, reverse proxy, recovery | [Advanced guide](docs/ADVANCED.en.md) |
| HTTP integrations | [API reference](docs/API.md) |
| Move an existing installation | [Migration guide](docs/MIGRATION.md) |
| Terminal client | [TUI reference](docs/tui.md) |
| gx-torrent engine and standalone client | [gx-torrent technical document](docs/gx-torrent.md) · [gx-core](docs/gx-core.md) |
| eD2k/Kad search daemon | [gx-ed2k technical document](docs/gx-ed2k.md) |
| qBittorrent-nox backend | [Setup guide](docs/qbittorrent-nox.md) |
| Build or contribute | [Developer manual](docs/DEVELOPERS.md) |
| Network and data safety | [Security policy](docs/SECURITY.md) |

Full tree: the [documentation index](docs/README.md).

## Support the project

If Gextto is useful and you would like to support its development, a PayPal
donation is welcome — thank you. A **Donate** button is also always visible in
the UI, bottom right.

[![Donate with PayPal](https://img.shields.io/badge/Donate-PayPal-0070BA.svg?logo=paypal)](https://www.paypal.com/cgi-bin/webscr?cmd=_donations&business=azanzani@gmail.com&item_name=Support+Gextto+Project)

## Legal and fair use

Gextto is a **download automation tool**. It does not host, index or distribute
any copyrighted content.

- It connects to **indexers you configure** (Jackett, Prowlarr, public RSS
  feeds). It ships no index of its own.
- What you download is **your responsibility**. Use Gextto only for content you
  have the right to access — public domain, Creative Commons, or media you own.
- BitTorrent and the engines Gextto drives are neutral technology; this project
  does not encourage or facilitate piracy.
- Released under the **EUPL 1.2** open-source licence.

## License

Gextto is licensed under the [EUPL-1.2](LICENSE). The engine core (`gx-core`,
`internal/gxcore`) and the DHT (`internal/dht`) are original code in this
repository under the same licence. See [THIRD-PARTY-NOTICES](THIRD-PARTY-NOTICES)
for the linked third-party libraries (also on the web UI *Licence* page).

## Acknowledgements

Thanks to **[DeepSeek](https://www.deepseek.com/)** for its support and patience
while this project grew.

