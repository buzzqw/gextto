# Gextto — Expert Torrent Transfer Orchestrator

**Tell Gextto what you want to watch. It finds the best release, downloads it,
verifies it, names it and shelves it in your library — then keeps watching for
a better copy.**

Gextto is a self-hosted media automation daemon with a fast web UI and its own
BitTorrent engine, written from scratch in pure Go. One binary per component,
no C dependencies, six interface languages.

[![License: EUPL-1.2](https://img.shields.io/badge/license-EUPL--1.2-blue.svg)](LICENSE)
![Go](https://img.shields.io/badge/Go-pure%2C%20zero%20CGO-00ADD8?logo=go)
![Platforms](https://img.shields.io/badge/platforms-Linux%20%C2%B7%20Windows%20(gx--torrent)-informational)
![BEPs](https://img.shields.io/badge/BitTorrent%20BEPs-34%20implemented-success)
![Languages](https://img.shields.io/badge/UI-IT%20%C2%B7%20EN%20%C2%B7%20DE%20%C2%B7%20FR%20%C2%B7%20ES%20%C2%B7%20PL-lightgrey)

> **Italiano:** [README.it.md](README.it.md) · **Manual:**
> [English](docs/MANUAL.en.md) · [italiano](docs/MANUAL.it.md) · **Issues:**
> [bugs and feature requests](https://github.com/buzzqw/gextto/issues) ·
> **Contribute:** [developer guide](docs/DEVELOPERS.md)

## Why Gextto

- **It decides like an expert, and explains itself.** Every candidate release is
  scored on resolution, source, codec, audio, HDR/Dolby Vision, language and
  size. The winner is downloaded; every rejected candidate keeps the exact
  reason in the log. Upgrades stop once the file is good enough.
- **It protects your library.** Completed files enter the library as hardlinks
  while seeding, a mount guard tells an unmounted NAS apart from a deleted
  file, replaced files go to a recycle bin, and interrupted moves resume after
  a restart.
- **It ships its own engine.** `gx-core` is a BitTorrent implementation built
  for this project — BitTorrent v2, uTP, NAT holepunching, IPv6 DHT,
  streaming — running in a process far lighter than qBittorrent (see
  *Resource footprint*).
- **It doubles as a standalone client.** The same engine ships as
  **[gx-torrent](https://github.com/buzzqw/gextto/releases)**, a complete
  client for Linux and Windows with its own web UI, a qBittorrent-compatible
  API for Sonarr/Radarr, and an RSS suite on par with the best.
- **It tunes itself.** Queue, disk cache and upload adapt to the machine and
  the line; downloads pause before the disk fills up; the engine keeps the
  peers that deliver and reuses data already on disk. No manual tuning needed.
- **It is easy to audit.** Pure Go with zero CGO, a single supervised process
  per component, about 1,600 test functions, and accessibility checks with
  axe-core.

### At a glance

| | |
|---|---|
| **Engine memory** | ~25 MB idle, ~100 MB at 100 MB/s with 50 torrents (qBittorrent-nox: up to ~5 GB) |
| **Protocol** | BitTorrent v1, v2 and hybrid (BEP 52) · 34 BEPs implemented and tested ([list](docs/bep.md)) |
| **Network** | TCP + uTP on one port · UPnP, NAT-PMP, PCP · BEP 55 holepunching · SOCKS5/HTTP proxy · VPN killswitch |
| **Automation** | TV series, movies, comics · Torznab (Jackett, Prowlarr) · RSS · web search · stuck-download replacement |
| **Integrations** | Jellyfin, Plex, Telegram, e-mail, Discord/Slack/ntfy/Gotify/Pushover, iCal |
| **Interfaces** | Web UI (SSR + HTMX, installable PWA) · terminal TUI over SSH · JSON API |
| **Build** | Pure Go (`CGO_ENABLED=0`) · optional libtorrent engine · Linux x86_64/aarch64 · gx-torrent also on Windows |

## Features

### Release scoring and automation

- **Quality-aware scoring.** Each candidate is scored deterministically on
  quality, source, codec, audio, HDR/Dolby Vision, language and size.
  Higher-grade library files are protected, rejections are logged with the
  reason, and an optional ceiling stops upgrades once a file is good enough.
  `ffprobe` can analyse archived media to inform decisions.
- **TV, movies, comics.** Standard `ComicInfo.xml` metadata is written for CBZ
  archives (Komga, Kavita, readers).
- **Stuck-download recovery.** Downloads that stop progressing, or magnets
  stuck waiting for metadata, are superseded by a valid alternative after a
  configurable window (*Configuration → Quality and upgrades → Stuck
  downloads*). The original keeps trying in the background; the abandoned
  release is blocklisted so the next search picks a different one.

### Library safety

- **Hardlinks while seeding.** When downloads and library share a filesystem,
  completed files appear in the library instantly as hardlinks — no duplicate
  disk usage during seeding.
- **NAS and mount guard.** An unmounted share is never mistaken for deleted
  files. Transient failures (disk full, network timeouts) retry transparently
  and interrupted moves resume on restart.
- **Recycle bin.** Files replaced by a true quality upgrade, and data of a
  torrent removed "with data", move to the configured trash first. The torrent
  leaves the engine before any file is touched (nothing held open on NFS) and
  cross-filesystem copies run in the background, so the page answers at once.

### Sources, integrations and control

- **Sources.** RSS feeds, HTML listings (ext.to, TorrentGalaxy, Corsaro),
  Torznab indexers (Jackett, Prowlarr), dedicated web search, optional
  FlareSolverr.
- **Notifications and media servers.** Telegram, e-mail, webhooks (Discord,
  Slack, ntfy, Gotify, Pushover) including daemon restarts and engine
  warnings; folder-level refreshes for Jellyfin and Plex; an iCal feed of
  library arrivals and air dates.
- **Traceability.** Every download carries a unique **Acquisition ID** across
  search, selection, transfer, post-processing and archive logs. The dashboard
  **To check** panel collects edge cases; threshold alerts warn when an
  indexer fails repeatedly or slows down.
- **Interfaces.** Responsive web UI installable as a mobile PWA, fully
  keyboard-navigable and axe-core checked; a single **Theme** picker with 20+
  contrast-checked themes plus custom server-side themes that follow you to
  every device; a full terminal TUI for SSH management. Six languages (IT, EN,
  DE, FR, ES, PL).

The web UI listens on `http://<host>:5000/`. Setup and every UI section are
covered by the [English manual](docs/MANUAL.en.md) and the
[manuale italiano](docs/MANUAL.it.md).

## Torrent engine

Gextto keeps the control plane (queue, scoring, post-processing, archiving)
and delegates transfers to one selectable engine (*Configuration → Torrent
engine*). A single engine runs at a time; switching is a clean, coordinated
migration with no I/O conflicts.

### gx-core and gx-torrent (default, recommended)

**gx-torrent** is the default engine: pure Go (`CGO_ENABLED=0`), no C/C++
dependencies, running as a separate supervised process that survives
orchestrator restarts and updates (Gextto re-attaches when binary and options
match). It has its own web interface and receives protocol work first.

Its core, **`gx-core`** (`internal/gxcore`), is a BitTorrent implementation
developed in this repository. `gx-core` and the DHT (`internal/dht`) are
original code written from scratch in this tree — no third-party-derived code,
no third-party rate-limiter or UUID helpers (provenance in
[`THIRD-PARTY-NOTICES`](THIRD-PARTY-NOTICES)) — and carry the same EUPL-1.2
licence as the rest of Gextto.

**Protocol.** BitTorrent v1, v2-only and hybrid (BEP 52) from `.torrent` files
and magnet links (`urn:btmh:`), with per-file piece layout, SHA-256 Merkle
verification and `piece layers` fetched from peers. **34 BEPs, implemented and
tested — [`docs/bep.md`](docs/bep.md) is the source of truth** — covering the
wire protocol, Fast Extension, `ut_metadata`, Extension Protocol, PEX with
IPv6, multitracker, LSD, super-seeding, Web Seeds, partial seeds, compact
peers, private torrents, local-data hints (BEP 38), peer priority, padding,
`lt_donthave`, IPv6/UDP trackers with extensions, retry and scrape, and the
DHT suite (mainline, IPv6, scrape, secure node IDs, read-only nodes, K/V
store, `urn:btpk:` updates) plus v2 and magnet `so=` selection. Deprecated or
niche BEPs and WebTorrent/I2P are out of scope, with rationale in the same
document.

**Local reuse.** A torrent's `similar`/`collections` hints (BEP 38) are kept on
export and matched against local torrents
(`GET /api/v1/torrents/{hash}/collections`). Pieces another local torrent
already verified — same layout, or the same BitTorrent v2 file under a
different piece size — are copied and re-checked in the background instead of
downloaded; partially available sources contribute their verified 16 KiB
blocks and only the rest comes from the network (on the Debian and Arch ISOs
with a 75%-complete source, 25% of the file came from the network instead of
100%). Private torrents are never mixed (BEP 27).

**Network.** TCP and uTP (BEP 29) in parallel with automatic fallback on a
single port — one UDP socket carries uTP and DHT, handshakes routed by
info-hash. Full `ut_holepunch` (BEP 55) across NATs, as client and relay.
UPnP, NAT-PMP and **PCP** (RFC 6887); a port check that also reads the
external port seen by the DHT (cone vs symmetric NAT); SOCKS5 and HTTP CONNECT
proxies; an interface-binding killswitch (`outgoing-interface`) that keeps all
BitTorrent traffic on the VPN. The DHT (`internal/dht`) covers mainline IPv6,
scrape, CRC32C secure node IDs, read-only mode (off by default) and the signed
K/V store with outbound `get`/`put`.

**Self-management.** The active-download count follows measured throughput
(dynamic queue); slow torrents stop occupying slots and stalled ones step
aside. The disk cache is sized from free RAM, active torrents and disk type
(HDD, SSD, NFS); latency auto-speed trims upload before the line saturates.
When a download folder drops below the free-space threshold (1 GiB by
default), unfinished downloads there pause and resume on their own — seeding
continues, nothing ends in a write error. Each peer's request queue grows with
what it delivers, a fast peer works on two pieces at once, and at the
connection cap the useless, slowest connection is dropped — never a random
one. Pieces of deselected files never count a peer as interesting.

**Footprint by design.** Pieces are written through; the reclaimable OS page
cache does the coalescing, so there is no gigabyte-sized in-process cache. A
read cache with lazy expiry and a write limiter sized from RAM and disk type
keep usage in the tens of MB. `fdatasync` is batched before the resume
bitfield is persisted (never `O_SYNC` per write); sparse files, `fallocate`
preallocation, RAM-disk temp folders, and an opt-in mmap mode on Linux
(`-mmap`). Hot paths avoid allocations (in-place MSE/PE RC4, word-wise
bitfield counts, adaptive per-peer queue depth). The IP blocklist (CIDR,
ranges, P2P, eMule) is merged into sorted intervals and binary-searched with
zero allocations; URL lists refresh on a schedule.

**Streaming and safety.** The picker separates the player window
(`SetStreamWindow`) from the readahead horizon (`SetReadaheadWindow`); per-file
priorities apply live and MKV/MP4 are served over HTTP Range. Web seeds and
magnet `xs=`/`as=` sources default to no private-address access (checked on
the resolved IP, rebinding-safe). Strict BEP 27 private torrents (no DHT, PEX
or LSD), 30-minute quarantine for corrupt-sending peers, trackers dropped
after BEP 31 `never` or prolonged failure, per-piece diagnostics, BoltDB
resume data, live Server-Sent Events (`/ui/events`).

### Engine comparison

| Engine | Where it runs | When to pick it |
|---|---|---|
| gx-torrent (default) | Separate supervised Go process | Pure Go, self-contained, active development. v1, hybrid and v2-only, DHT and MSE/PE; also standalone on Linux and Windows; no WebTorrent/WebRTC |
| qBittorrent-nox | External daemon via its Web API | You already run qBittorrent-nox or prefer its UI. Needs path mappings and an extra process ([setup guide](docs/qbittorrent-nox.md)) |
| libtorrent (optional build) | Inside Gextto's process, **only if compiled in** (`make build-libtorrent`) | Advanced libtorrent controls or compatibility; also the automatic fallback. A crash takes Gextto down too |

Engine statistics — torrents by state, speed and queue slots, port/router,
DHT/uTP/LSD, encryption, proxy, IP filter, cache, **incoming connections** and
**disk I/O** — are in **Health → Torrent engine** (`/?view=health`), in the
daemon's `GET /api/v1/stats`, and in `GET /api/libtorrent/session-stats`.

## Resource footprint

Gextto stays light when idle: one daemon (plus the small supervised
`gx-torrent` process when that engine is active), bounded concurrency for
sources and background work, backoff instead of tight retry loops against
failing providers. CPU and memory depend on monitored titles, sources, active
torrents and archive scans; single-machine numbers are indicative.

**Engine memory, measured** — 50 real torrents, ~100 MB/s on a 16 GB machine:

| Engine | Idle (torrents loaded) | Peak while transferring |
|---|---:|---:|
| gx-torrent | ~25 MB | ~100 MB |
| qBittorrent-nox | ~40 MB | ~5 GB |
| libtorrent (in-process, optional build) | ~0.5 GB | 3–5 GB |

libtorrent and qBittorrent keep a large in-process disk cache (GBs of
anonymous memory). gx-torrent writes pieces through and leaves coalescing to
the reclaimable OS page cache, so its footprint stays in the tens/hundreds of
MB under load. Values vary with torrents, peers and storage; on HDD/NFS
Gextto grants gx-torrent a larger buffer, used only when writes lag behind
downloads.

## Install on Linux

For 64-bit Linux with systemd: Debian 12+, Ubuntu 22.04+, Fedora, openSUSE
Leap 15.6+/Tumbleweed, Arch. Run as root:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

It installs to `/opt/gextto`, keeps service data in `/var/lib/gextto`, and
serves the UI on port 5000. The default engine `gx-torrent` (pure Go) is
installed alongside `gexttod` and started in managed mode. The release package
is pure Go — nothing else to install. An external qBittorrent-nox can be used
instead ([setup guide](docs/qbittorrent-nox.md)); the libtorrent engine needs your own build (see below).

The installer first runs the downloaded program once: a missing library or an
old glibc is reported immediately instead of leaving a service that never
starts. By default the service runs as **the local user invoking the
installer**, so it can write to your library without extra permissions;
`--user gextto` uses the isolated login-less system account instead. For a
library on someone else's NAS, `--media-group <group>` adds the service user
to that group.

> [!IMPORTANT]
> Gextto is meant for a trusted network: the web UI and API are open by
> default. For access from outside, enable the optional login
> (*Configuration → Access and services*; the LAN stays free) and still use
> HTTPS via reverse proxy or VPN. Read the [security policy](docs/SECURITY.md)
> before exposing it remotely.

### Install from source

Go 1.26.8+ required; the default build is **pure Go**. Only the optional
**libtorrent** engine additionally needs a C++17 compiler and the
libtorrent-rasterbar headers. The web UI is embedded — no frontend build
needed. See the [developer manual](docs/DEVELOPERS.md).

```bash
make build                 # pure Go (default), with gx-torrent
make build-libtorrent      # also link the optional libtorrent engine
```

`make build` increments the **committed** build number (`build_number`, shared
with CI so checkout and release show the same `1.1.<n>`) and writes `gexttod`
plus the pure-Go `gx-torrent` to `bin/`. `make fast` rebuilds without
incrementing; `make gx-torrent` builds only the daemon. Check the result with
`./bin/gexttod --version`. The `gx-torrent` daemon has its **own** build
counter, advanced only when its code changes; `./bin/gx-torrent --version`
and `/api/v1/health` report it as `1.1.<n>`.

## gx-torrent as a standalone client

gx-torrent is also a complete client usable without Gextto, like
`qbittorrent-nox` or `transmission-daemon`. It is **the same binary** Gextto
drives as its engine. In short, it is a **simpler, reduced version of Gextto**:
fewer features, but all the ones a standalone client needs.

- **`*arr` ready.** qBittorrent Web API v2 (`/api/v2/...`) for Sonarr, Radarr,
  Lidarr, Readarr and mobile managers, plus its own JSON API (`/api/v1/...`)
  and sign-in page.
- **Modern web UI.** Light and dark themes in the spirit of VueTorrent/Flood:
  sidebar with status/category/tag filters and a live transfer graph, dense
  sortable multi-select list with context menu and keyboard shortcuts, torrent
  detail in a side panel, drag-and-drop of `.torrent` files and pasted
  magnets, phone-friendly cards. Every page axe-core checked.
- **RSS and automation.** A **Wanted** page (cards per title with missing
  episodes, guided add dialog) and an **RSS & Indexers** page (feeds, rules,
  indexers, options as tabbed cards; rule editor split into *which items* /
  *what to do* with live match preview). Ordered PASS/FAIL rules, per-row
  reload, live indexer tests, optional FlareSolverr, and a local **archive**
  of everything feeds returned (100,000 releases searchable in ~7 ms, with an
  option to query Jackett/Prowlarr live).
- **Operations.** Global and per-torrent limits, daily scheduler, latency
  auto-speed (anti-bufferbloat), one-click queue/cache/bandwidth optimisation,
  URL or file IP filter with scheduled refresh, categories and tags,
  real-time diagnostics.
- **Platforms.** Linux x86_64/aarch64 (`gx-torrent-linux-<arch>.tar.gz`, systemd
  unit or `run.sh`, unprivileged Docker image) and native Windows service
  amd64/arm64 (`gx-torrent-windows-<arch>.zip`, `install-service.ps1`,
  junction-based linking — no symlink privilege needed). Gextto itself stays
  Linux-only.

Open `http://<host>:8890/` for the setup wizard (language, folders with a
server-side picker, port test, limits, login, indexers); it prints the
address to open at startup. Full reference: the [gx-torrent technical
document](docs/gx-torrent.md).

## First safe run

1. Open `http://<server>:5000`: the first visit starts the **setup wizard**
   (password, folders with permission check, indexer and TMDB, first title,
   activation). Skippable, reopenable at `/?view=setup`.
2. Set storage paths, one source, and TMDB/TVDB credentials if needed.
3. Keep **dry-run** on; add one test title and run a manual search or cycle.
4. Check **Health** and **Logs**, including permissions and source results.
5. Enable active mode only when the outcome is correct.

NAS setup and troubleshooting: the [user manual](docs/MANUAL.en.md).

## Update and uninstall

With the official install, an **Update available** button appears above
**Donate** when a release is published: *Maintenance → Updates* shows the
installed version, the changes, and an **Update now** button. A database
backup is taken first; if the new version fails to start, the previous one is
restored automatically. The check (a small GitHub request every 6 hours) can
be disabled in *Configuration → System → Updates*.

Or re-run the installer: it verifies the checksum, replaces the binary
atomically, restarts the service, and rolls back on failure. Data and
configuration are untouched.

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

```bash
sudo bash install.sh --help       # all options
sudo bash install.sh --dry-run    # print what would happen, change nothing
sudo bash install.sh --uninstall  # stop and remove the program (keeps data)
sudo bash install.sh --uninstall --purge   # also remove the data directory
```

Environment overrides (`GEXTTO_DATA_DIR`, `GEXTTO_PORT`, `GEXTTO_USER`, …) stay
supported. `GEXTTO_LOCAL_ARCHIVE=/path/gextto-linux-x86_64.tar.gz` installs
from a local payload (offline or CI). From a source checkout:

```bash
git pull --ff-only
./scripts/update.sh
curl -fsS http://127.0.0.1:5000/api/health
```

(`--no-restart` leaves the restart to you.)

## Accessibility

The web UI offers keyboard navigation, accessible control names, dialog focus
management, semantic sortable tables, live announcements, responsive reflow
and improved light-theme contrast. Regressions are covered by axe-core and
Playwright:

```bash
cd uiweb/end2end
npm ci
npm run test:a11y
```

The suite currently covers the main sections with 13 tests. That is not a
legal certification: manual screen-reader, keyboard and assistive-technology
testing is still required. Scope and limits:
[accessibility analysis](docs/evoluzione.md#8-accessibility).

## Documentation

| Need | Document |
|---|---|
| Operate the service | [English manual](docs/MANUAL.en.md) · [Manuale italiano](docs/MANUAL.it.md) |
| NAS, reverse proxy, recovery | [Advanced guide](docs/ADVANCED.en.md) |
| HTTP integrations | [API reference](docs/API.md) |
| Move an existing installation | [Migration guide](docs/MIGRATION.md) |
| Terminal client | [TUI reference](docs/tui.md) |
| gx-torrent engine and standalone client | [gx-torrent technical document](docs/gx-torrent.md) |
| qBittorrent-nox backend | [Setup guide](docs/qbittorrent-nox.md) |
| Build or contribute | [Developer manual](docs/DEVELOPERS.md) |
| Network and data safety | [Security policy](docs/SECURITY.md) |
| Accessibility scope and testing | [Accessibility analysis](docs/evoluzione.md#8-accessibility) |

Full tree: the [documentation index](docs/README.md).

## Support the project

If Gextto is useful and you would like to support its development, a PayPal
donation is welcome — thank you. A **Donate** button is also always visible in
the UI, bottom right.

[![Donate with PayPal](https://img.shields.io/badge/Donate-PayPal-0070BA.svg?logo=paypal)](https://www.paypal.com/cgi-bin/webscr?cmd=_donations&business=azanzani@gmail.com&item_name=Support+Gextto+Project)

## Legal and fair use

Gextto is a **download automation tool**. It does not host, index or
distribute any copyrighted content.

- It connects to **indexers you configure** (Jackett, Prowlarr, public RSS
  feeds). It ships no index of its own.
- What you download is **your responsibility**. Use Gextto only for content
  you have the right to access — public domain, Creative Commons, or media
  you own.
- BitTorrent and the engines Gextto drives are neutral technology; this
  project does not encourage or facilitate piracy.
- Released under the **EUPL 1.2** open-source licence.

## License

Gextto is licensed under the [EUPL-1.2](LICENSE). The engine core (`gx-core`,
`internal/gxcore`) and the DHT (`internal/dht`) are original code in this
repository under the same licence. See [THIRD-PARTY-NOTICES](THIRD-PARTY-NOTICES)
for the copyright and licence texts of linked third-party libraries (also on
the web UI *Licence* page).
