# Gextto — EXpert Torrent Transfer Orchestrator

**Gextto (EXpert Torrent Transfer Orchestrator)** is a self-hosted daemon that
finds, selects, downloads, verifies, names and archives TV series, movies and
comics. Its web interface is the control plane; the daemon keeps running as
a service.

> **Italiano:** [README.it.md](README.it.md) · **Manual:**
> [English](docs/MANUAL.en.md) · **Issues:**
> [bugs and feature requests](https://github.com/buzzqw/gextto/issues) ·
> **Contribute:** [developer guide](docs/DEVELOPERS.md)

```text
┌────────────────────────────────────────────────────────────────────────┐
│                        Gextto Media Orchestrator                       │
│  Decision Engine · Parse & Scoring · Multi-Indexer · Post-Processing   │
│  Server-Side UI (HTMX + SSR) · Terminal TUI · Media Integrations       │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │ Engine contract (torrent_engine.go)
┌───────────────────────────────────▼────────────────────────────────────┐
│                  gx-torrent (Managed / Standalone Daemon)              │
│  Dedicated Web UI · qBittorrent Web API v2 (Sonarr/Radarr) · RSS Rules │
│  Linux Service (systemd) and Native Windows Service (NTFS Junctions)   │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │
┌───────────────────────────────────▼────────────────────────────────────┐
│                    gx-core Engine (internal/gxcore)                    │
│  BitTorrent v1 / v2 (BEP 52 Merkle) · uTP (BEP 29) · Holepunch (BEP 55)│
│  Dual-Stack IPv6 DHT (BEP 32/42/44) · Streaming Readahead · MSE In-Place│
│  Zero CGO · Memory footprint: ~25 MB idle / ~100 MB peak               │
└────────────────────────────────────────────────────────────────────────┘
```

## What Gextto offers

### 🎯 Intelligent Automation & Decision Engine
- **Quality-aware scoring:** deterministically scores releases by evaluating quality, source, codec, audio, HDR/DV, language, and file size. It protects higher-grade existing library files and records the exact reason for any rejected candidate in logs. An optional ceiling stops upgrades once a file is good enough; `ffprobe` can analyze archived media to enrich decisions.
- **Comics:** writes standard `ComicInfo.xml` metadata for CBZ comic archives (Komga, Kavita, and readers).
- **Stuck download mitigation (Anti-Stall):** downloads that stop making progress (no bytes for a while) or magnets waiting indefinitely for metadata can be superseded by a valid alternative after a configurable window (*Configuration → Quality and upgrades → Stuck downloads*), while the original torrent continues trying in the background.

### 🛡️ Library Integrity & Storage Safety
- **Zero-space hardlinks during seeding:** when downloads and media libraries share the same filesystem, newly completed files enter the library instantly as hardlinks, avoiding duplicate disk usage during seeding.
- **NAS and mount-point guard:** instantly recognizes an unmounted network share (NAS), preventing missing files from being mistaken for deletions; features transparent automated retries for transient disk full/network timeouts and automatically resumes interrupted file moves on restart.
- **Recycle bin safety:** replaced files from true quality upgrades are moved to the configured Trash before deletion.

### 🔌 Sources, Integrations & Operations
- **Flexible sources:** RSS feeds, Torznab indexers (Jackett, Prowlarr), dedicated web search engines, and optional FlareSolverr integration.
- **Notifications & Media Servers:** dispatches alerts via Telegram, e-mail, or webhooks (Discord, Slack, ntfy, Gotify, Pushover), including daemon restarts and engine warnings; sends targeted folder-level library refreshes to Jellyfin and Plex, and provides an iCal calendar feed of library arrivals and air dates.
- **Full lifecycle traceability:** every download carries a unique **Acquisition ID** preserved across search, selection, transfer, post-processing, and archiving logs; the dashboard **To check** panel aggregates edge cases, and threshold alerts warn if an indexer fails consecutively or slows down.
- **Server-Side UI (HTMX + SSR) & Native TUI:** responsive web UI installable as a mobile PWA, fully keyboard-navigable and verified with axe-core for accessibility; full terminal TUI for direct management via SSH. Available in 6 languages (IT, EN, DE, FR, ES, PL).

The web UI is available at `http://<host>:5000/`. The [English manual](docs/MANUAL.en.md) and [manuale italiano](docs/MANUAL.it.md) cover setup and every section of the UI.

## Torrent engines & the gx-core Engine

Gextto keeps the control plane (queue, scoring, post-processing, archiving) and swaps only the transfer engine in *Configuration → Torrent engine*. One engine runs at a time: switching is a clean, coordinated migration without I/O conflicts or multiple daemons competing for the same files.

### gx-core & gx-torrent (Default and recommended)

**gx-torrent** is the default engine and compiles as **pure Go** (`CGO_ENABLED=0`) with zero C/C++ dependencies. It runs as a separate supervised process, survives orchestrator restarts and updates, provides its own web interface, and receives protocol innovations first.

Its engine core, **`gx-core`** (`internal/gxcore`), is an advanced, high-performance BitTorrent implementation developed directly in this repository (with portions derived under MIT/BSD licenses; see `internal/gxcore/LICENSE`). It provides comprehensive, cutting-edge protocol support and enterprise-grade performance:

- **BitTorrent v1, v2 and Hybrids (BEP 52):** full support for v1, v2-only and hybrid torrents from both `.torrent` files and magnet links (`urn:btmh:`). Uses a modern per-file piece layout (no cross-file padding blocks), SHA-256 Merkle tree verification, and dynamic `piece layers` retrieval directly from peers (`hash request`, `hashes`, `hash reject`).
- **Broad BitTorrent (BEP) coverage — [`docs/bep.md`](docs/bep.md) is the source of truth:** core wire (BEP 3), reserved bits (4), Fast Extension (6), `ut_metadata` from a magnet (9), Extension Protocol (10), Peer Exchange `ut_pex` with IPv4 and IPv6 `added6`/`dropped6` (11), multitracker `announce-list` (12), Local Service Discovery over IPv4/IPv6 (14), Web Seeds with HTTP Range (19), peer-ID conventions (20), partial seeds `upload_only`/`event=paused` (21), compact peer lists (23), the tracker external-IP field (24), private torrents (27), canonical peer priority (40), padding files (47) and `lt_donthave` (54); trackers: IPv6 `peers6` (7), UDP tracker (15), UDP extensions `URLData` (41) and scrape (48); DHT: mainline (5), IPv6 (32), DHT scrape (33), secure node IDs (42), read-only nodes (43) and the K/V store (44); plus BitTorrent v2 (52) and the magnet `so=` selection (53). Deprecated or niche BEPs and WebTorrent/I2P are out of scope, with the rationale in the same document.
- **uTP Transport (BEP 29) & Single-Port Listening:** dual-transport engine running TCP and uTP in parallel with automatic fallback. A single UDP socket handles both uTP transport and DHT traffic on one session port, multiplexing connections and routing handshakes by info-hash so all swarms advertise the same port.
- **NAT Traversal via Holepunching (BEP 55):** full `ut_holepunch` extension implementation to establish direct peer-to-peer uTP connections across symmetric NATs and firewalls without router configuration, acting both as active client and as relay node for other peers.
- **Port Mapping & Network Integration:** automatic router port mapping with UPnP, NAT-PMP and **PCP** (RFC 6887), plus DHT-based NAT-type detection in the port check (reflected external port, cone vs symmetric), SOCKS5 and HTTP CONNECT proxy support, and an interface-binding killswitch (`outgoing-interface`) to restrict all BitTorrent traffic strictly to VPN tunnels.
- **Super-seeding (BEP 16) & Deterministic Choking:** unchoker simulation harness for throughput and fairness optimization; super-seeding support to distribute new torrents piece-by-piece to seed swarms without redundant initial uploads.
- **In-Browser Media Streaming & Graduated Priority:** real-time piece prioritization for instant media playback: piece picker distinguishes between the immediate player playhead window (`SetStreamWindow`) and the readahead horizon (`SetReadaheadWindow`), prioritizing streaming over rarest-first and serving MKV/MP4 videos directly to web browsers or VLC via HTTP Range requests.
- **Zero-Alloc In-Place MSE/PE:** stream cipher RC4 encryption executed directly in-place inside the caller's network buffer (`inPlaceStreamWriter`), eliminating per-block heap allocations during uploads.
- **Hardware Acceleration & Adaptive Queue:** bitfield bit counting accelerated by native CPU instructions (`math/bits.OnesCount64`); per-peer request queue depth (`RequestQueue`) modulated dynamically based on block delivery speed and reset on choke/snub.
- **High-Performance Memory & Disk Architecture:** dynamic in-memory read cache with lazy expiration sweeper, write buffer limiter, sparse file allocation and `fallocate` preallocation (with tmpfs safety), and RAM disk support for temporary downloads.
- **Optimized Binary-Search IP Blocklist:** supports CIDR, IP range, P2P and eMule `.dat` formats merged into sorted non-overlapping intervals in memory, performing zero-allocation binary search lookups with negligible memory footprint (~bytes per rule).
- **Advanced DHT Subsystem (`internal/dht`):** standalone mainline DHT (BEP 5) module with dual-stack IPv6 support (BEP 32), DHT scrape (BEP 33), Security Extension anti-spoofing via CRC32C Castagnoli (BEP 42), cryptographic immutable/mutable K/V storage with ed25519 signatures (BEP 44), read-only mode for transit nodes (BEP 43), and tracker scrape (BEP 48 HTTP/UDP).
- **Swarm Protection, Diagnostics & Autonomy:** strict BEP 27 private swarm compliance (automatically disables DHT, PEX in/out, and LSD); 30-minute session-wide quarantine for peers transmitting corrupt blocks; WebSeeds (BEP 19); per-piece state diagnostics; persistent resume data (BoltDB); granular storage error diagnostics; and live real-time Server-Sent Events (`/ui/events`).
- **Cross-Platform & Standalone Client:** runs both as Gextto's internal engine and as a standalone client (`gx-torrent`) with its own web UI, qBittorrent-compatible Web API (`/api/v2`), RSS rules with smart episode tracking, systemd daemon on Linux, and native Windows Service on Windows (amd64/arm64).

### Alternative engines

- **qBittorrent-nox:** for users who already run qBittorrent or prefer its desktop UI: Gextto controls it remotely via its official Web API.
- **libtorrent (optional build):** available **only if built with CGO** (`make build-libtorrent`, `GEXTTO_LIBTORRENT=1`): runs inside Gextto's process and serves as automatic fallback if gx-torrent cannot start. An engine crash brings Gextto down as well.

| Engine | Where it runs | When to pick it |
|---|---|---|
| gx-torrent (default) | Separate supervised Go process | The default: pure Go, self-contained, active development. BitTorrent v1, hybrid and v2-only (BEP 52, `.torrent` and magnet), DHT (IPv6, security BEP 42, read-only, scrape) and MSE/PE encryption; also usable on its own on Linux and Windows (see *gx-torrent as a standalone client*); no WebTorrent/WebRTC |
| qBittorrent-nox | External daemon, driven through its Web API | You already run qBittorrent-nox or prefer its own UI. Needs path mappings and an extra process |
| libtorrent (optional build) | Inside Gextto's process; **only if you compiled it in** (`make build-libtorrent`) | You need libtorrent's advanced controls or its compatibility; also the automatic fallback. A crash takes Gextto down too |

How to include libtorrent: see *Install from source* below.

The active engine's statistics — torrents by state, speed and queue slots,
port/router, DHT/uTP/LSD, encryption, proxy, IP filter, cache, **incoming
connections** and **disk I/O** (queue, read/write totals) with session totals —
are in the **Health → Torrent engine** panel (`/?view=health`); the same
counters are in the daemon's `GET /api/v1/stats` and in
`GET /api/libtorrent/session-stats`.

## Resource footprint

Gextto is designed to remain lightweight while idle: it is one daemon (plus the
small supervised `gx-torrent` process when that engine is active), uses bounded
concurrency for source and background work, and backs off failing providers
instead of retrying continuously. Actual CPU and memory use depend on monitored
titles, sources, active torrents and archive scans; treat any machine-specific
measurement as indicative, not a guarantee.

**Torrent engine memory, measured.** With 50 real torrents and ~100 MB/s of
downloads on a 16 GB machine, the engine footprint differs sharply:

| Engine | Idle (torrents loaded) | Peak while transferring |
|---|---:|---:|
| gx-torrent | ~25 MB | ~100 MB |
| qBittorrent-nox | ~40 MB | ~5 GB |
| libtorrent (in Gextto's process, optional build) | ~0.5 GB | 3–5 GB |

libtorrent and qBittorrent keep a large in-process disk cache (GBs of anonymous
memory). gx-torrent has **no large in-process write-back cache by design**: it
writes pieces through and lets the reclaimable OS page cache do the coalescing,
so its own footprint stays in the tens/hundreds of MB even under load; it does
not hold data in RAM just to sit on it. Numbers vary with torrents, peers and
storage; on HDD/NFS Gextto gives gx-torrent a larger buffer, used only when the
writes fall behind the download.

## Install on Linux

The official installer targets 64-bit Linux systems (x86_64 or aarch64) with
systemd: Debian 12+, Ubuntu 22.04+, Fedora, openSUSE Leap 15.6+/Tumbleweed and
Arch Linux. Run it as root:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

It installs the program in `/opt/gextto`, stores service data in
`/var/lib/gextto`, and exposes the UI on port 5000. The default torrent engine,
`gx-torrent` (pure Go), is installed next to `gexttod` and started in managed
mode. The release package is **pure Go**: nothing else must be installed. An
external qBittorrent-nox can be used instead; the libtorrent engine needs a build
of your own (`make build-libtorrent`, see below).

Before touching the system the installer runs the downloaded program once: a
missing library or a too-old glibc is reported right away instead of leaving a
service that never starts. By default the service runs as **the local user who
runs the installer**, so it can create folders in your library without extra
permissions; `--user gextto` uses the isolated, login-less system account
instead. When the library lives on a NAS owned by another user,
`--media-group <group>` adds the service user to that group.

> [!IMPORTANT]
> Gextto is meant for a trusted network: by default the web UI and the API are
> open. If you reach it from outside, turn on the optional login
> (*Configuration → Access and services*; the local network stays free) and still use HTTPS
> through a reverse proxy or a VPN. Read the [security policy](docs/SECURITY.md)
> before exposing it remotely.

### Install from source

Go 1.26.8+ is required; the default build is **pure Go**. Only to include the
optional **libtorrent** engine, also install a C++17 compiler and the
libtorrent-rasterbar development headers. The
normal build embeds the web UI; no separate frontend build is required. See the
[developer manual](docs/DEVELOPERS.md).

```bash
make build                 # pure Go (default), with gx-torrent
make build-libtorrent      # also link the optional libtorrent engine
```

`make build` increments the **committed** build number (`build_number`, the same
source shared with CI, so the checkout and the released version show the same
`1.1.<n>`) and writes the versioned daemon
to `bin/gexttod`, plus the pure-Go `gx-torrent` engine next to it. To rebuild
without incrementing the number, use `make fast` (or `make fast-libtorrent` for
the libtorrent variant). To build **only** the
`gx-torrent` daemon, use `make gx-torrent`. Verify the exact binary, product
version, build number and, if linked, the libtorrent version with:

```bash
./bin/gexttod --version
```

The `gx-torrent` daemon keeps its **own** build counter, separate from Gextto's:
`make build` and `make gx-torrent` advance `gx-torrent.build_number` (through
`scripts/next-gx-build-number.sh`) only when its code actually changes. That
number is what `./bin/gx-torrent --version` and the daemon's `/api/v1/health`
report as `1.1.<n>`.

## gx-torrent as a standalone client

gx-torrent is also a complete BitTorrent client on its own, usable without
Gextto in the same way as `qbittorrent-nox` or `transmission-daemon`. It is
**the same binary** Gextto drives as its engine, not a separate product:

- **Drop-in Compatibility with the `*arr` ecosystem:** implements the
  **qBittorrent Web API v2** (`/api/v2/...`), seamlessly integrating with Sonarr,
  Radarr, Lidarr, Readarr, and third-party mobile torrent managers.
- **Native JSON API and login:** exposes its own JSON API (`/api/v1/...`), the
  same one Gextto drives it with, plus a sign-in page (`/ui/login`) when
  standalone authentication is enabled.
- **Standalone Web UI & Setup Wizard:** features a dedicated, full-featured web UI
  with real-time graphs and diagnostics, categories and tags, RSS feed rules
  with smart episode tracking, and direct multi-indexer search (Jackett,
  Prowlarr, MIRCrew).
- **Cross-Platform Architecture:**
  - **Linux (x86_64, aarch64):** standalone `tar.gz` package, bundled systemd unit
    (user or system service), and unprivileged Docker container (`Dockerfile`).
  - **Native Windows (amd64, arm64):** runs as a **native Windows service**
    managed by PowerShell scripts (`install-service.ps1` / `uninstall-service.ps1`).
    Leverages native Win32 APIs for memory, storage type, and default gateway,
    and connects folders via **NTFS Junctions** (no developer mode or symlink
    elevation needed).

Every release publishes dedicated packages next to Gextto's:

| Platform | Package | How it runs |
|---|---|---|
| Linux (x86_64, aarch64) | `gx-torrent-linux-<arch>.tar.gz` | `run.sh` or the bundled systemd unit |
| Windows (amd64, arm64) | `gx-torrent-windows-<arch>.zip` | Native Windows service, installed by `install-service.ps1` |
| Container | `Dockerfile` in the repository root | Unprivileged user, data in `/data`, UI on port 8080 |

**Windows.** Extract the zip and run `install-service.ps1` from an **elevated**
PowerShell: it registers and starts the `gx-torrent` service (automatic start),
with data and log in `%ProgramData%\gx-torrent`. `uninstall-service.ps1` removes
it; the bundled `README.txt` also documents the `sc create` alternative. On
Windows gx-torrent uses the native APIs for memory, disk type, free space and
the default gateway (UPnP/NAT-PMP), and links each torrent into place with a
**junction**, so it needs neither the symbolic-link privilege nor Developer
Mode. Only gx-torrent runs on Windows; Gextto itself remains Linux-only.

**First run.** Open `http://<host>:8890/`: the wizard guides you through
language, download and temporary folders (with a **folder picker** browsing the
server), peer port with a reachability test, global bandwidth limits, user and
password, LAN access and optional indexers. Choosing Prowlarr or Jackett fills
the indexer URL with its usual local address and port (still editable); any
other Torznab service is added as a custom type. The page links to
**RSS & indexers**, where the *Wanted* list (the titles to look for, searched
across the indexers and the configured feeds, with the missing episodes fetched
again) and the feeds, rules and indexers are managed without hand-editing JSON —
and to **Settings**, the client panel: folders,
network and ports, protocols (encryption, DHT/uTP/LSD…), queue and cache,
bandwidth and the daily scheduler, language and notifications. On a desktop session the browser opens on the wizard
by itself; on a headless server or a Windows service the log prints the address
to open.

The full reference (flags, `settings.json`, API, platform notes) is the
[gx-torrent technical document](docs/gx-torrent.md) (Italian).

## First safe run

1. Open `http://<server>:5000`: the first visit starts the **setup wizard**
   (password, folders with a permission check, indexer and TMDB, first title,
   activation). You can skip it and reopen it from `/?view=setup`.
2. Configure storage paths, one source, and TMDB/TVDB credentials if needed.
3. Keep **dry-run** enabled; add one test title and run a manual search or
   cycle.
4. Check **Health** and **Logs**, including filesystem permissions and source
   results.
5. Enable active mode only after the outcome is correct.

The detailed setup checklist, NAS guidance and troubleshooting are in the
[user manual](docs/MANUAL.en.md).

## Update and uninstall

With the official installation, when a new version is published an **Update
available** button appears above **Donate**: *Maintenance → Updates* shows the
installed version, what changed (the commits) and an **Update now** button. A
database backup is taken first; if the new version does not start, the previous
one comes back by itself. The check (a small request to GitHub every 6 hours)
can be turned off in *Configuration → System → Updates*.

Alternatively, run the installer again. It verifies the release
checksum when published, replaces the binary atomically, restarts the service
and, if the new version fails to start, rolls back to the previous one. Data and
configuration are left untouched.

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

The installer also accepts these options:

```bash
sudo bash install.sh --help       # all options
sudo bash install.sh --dry-run    # print what would happen, change nothing
sudo bash install.sh --uninstall  # stop and remove the program (keeps data)
sudo bash install.sh --uninstall --purge   # also remove the data directory
```

Environment overrides (`GEXTTO_DATA_DIR`, `GEXTTO_PORT`, `GEXTTO_USER`, …) stay
supported. `GEXTTO_LOCAL_ARCHIVE=/path/gextto-linux-x86_64.tar.gz` installs from
a local payload (offline or CI).

For a source checkout:

```bash
git pull --ff-only
./scripts/update.sh
curl -fsS http://127.0.0.1:5000/api/health
```

The source update script runs the versioned build, restarts the detected
service and leaves the data directory and configuration untouched. Use
`./scripts/update.sh --no-restart` when the restart must be performed manually.

## Accessibility

The web UI includes keyboard navigation, accessible names for controls, focus
management in dialogs, semantic sortable tables, live announcements for dynamic
updates, responsive reflow and improved light-theme contrast. Accessibility
regressions are checked with axe-core and Playwright:

```bash
cd uiweb/end2end
npm ci
npm run test:a11y
```

The current automated suite covers the main UI sections with 13 accessibility
tests. This is not, by itself, a legal accessibility certification: manual
screen-reader, keyboard and assistive-technology testing is still required.
See the [accessibility analysis](docs/evoluzione.md#13-accessibilità) for scope and known
limitations.

## Documentation

| Need | Document |
|---|---|
| Operate the service | [English manual](docs/MANUAL.en.md) · [Manuale italiano](docs/MANUAL.it.md) |
| NAS, reverse proxy, recovery | [Advanced guide](docs/ADVANCED.en.md) |
| HTTP integrations | [API reference](docs/API.md) |
| Move an existing installation | [Migration guide](docs/MIGRATION.md) |
| Terminal client | [TUI reference](docs/tui.md) |
| gx-torrent engine and standalone client | [gx-torrent technical document](docs/gx-torrent.md) (Italian) |
| Build or contribute | [Developer manual](docs/DEVELOPERS.md) |
| Network and data safety | [Security policy](docs/SECURITY.md) |
| Accessibility scope and testing | [Accessibility analysis](docs/evoluzione.md#13-accessibilità) |

For the full documentation tree, start from the [documentation index](docs/README.md).

## Support the project

If Gextto is useful to you and you would like to support its development, you
can make a contribution via PayPal. Thank you! A **Donate** button is also
always visible in the UI, at the bottom right.

[![Donate with PayPal](https://img.shields.io/badge/Donate-PayPal-0070BA.svg?logo=paypal)](https://www.paypal.com/cgi-bin/webscr?cmd=_donations&business=azanzani@gmail.com&item_name=Support+Gextto+Project)

## ⚖️ Legal & fair use

Gextto is a **download automation tool**. It does not host, index, or distribute
any copyrighted content.

- Gextto connects to **indexers you configure** (Jackett, Prowlarr, public RSS
  feeds). It has no built-in index.
- What you download is **entirely your responsibility**. Use Gextto only for
  content you have the right to access — public domain, Creative Commons, or
  media you own.
- BitTorrent, and the engines Gextto drives, are a neutral technology. Gextto does not
  encourage or facilitate piracy.
- This project is released under the **EUPL 1.2** open-source license.

> *"With great automation comes great responsibility."*

## License

Gextto is licensed under the [EUPL-1.2](LICENSE). See [NOTICE](NOTICE) for
notices about bundled and optional third-party components.
