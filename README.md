# Gextto — EXpert Torrent Transfer Orchestrator

**Gextto (EXpert Torrent Transfer Orchestrator)** is a self-hosted daemon that
finds, selects, downloads, verifies, names and archives TV series, movies and
comics. Its web interface is the control plane; the daemon keeps running as a
service.

> **Italiano:** [README.it.md](README.it.md) · **Manual:**
> [English](docs/MANUAL.en.md) · **Issues:**
> [bugs and feature requests](https://github.com/buzzqw/gextto/issues) ·
> **Contribute:** [developer guide](docs/DEVELOPERS.md)

## What Gextto offers

- **One service, one control plane:** the daemon handles scheduled searches,
  torrent transfers, post-processing and the library archive. gx-torrent is the
  default engine (pure Go, run as a supervised process); embedded libtorrent and
  qBittorrent-nox are optional alternatives.
- **Series, movies and comics:** monitor titles, search configured sources and
  manage downloads and archived media from one responsive web UI, installable on
  a phone, or the terminal TUI. Anime numbered by absolute episode ("Title -
  1071") are mapped to season and episode; CBZ comics get a `ComicInfo.xml` for
  Komga, Kavita and readers.
- **Quality-aware automation:** score releases by quality, source, codec, audio,
  HDR, language and size; protect better existing files and explain rejected
  candidates. An optional ceiling stops upgrades once a file is good enough.
  `ffprobe` can enrich archived-file decisions.
- **Flexible sources and integrations:** use RSS, Torznab indexers such as
  Jackett and Prowlarr, web search and optional FlareSolverr; connect Simkl,
  Jellyfin and Plex (refreshing only the folder that changed) and subscribe to an
  iCal calendar of library arrivals, original broadcasts and local movie
  releases.
- **Careful with your library:** an existing file is replaced only by a real
  upgrade, and the replaced file goes to the Trash (when configured). An
  unmounted NAS is recognised and never mistaken for deleted files; temporary
  errors (NAS unreachable, disk briefly full, timeouts) are retried
  automatically; a move to the archive interrupted by a restart resumes on its
  own. While a file seeds it enters the library as a hardlink, without using the
  space twice (when downloads and library share a filesystem).
- **Operational tools included:** health checks, logs, backups, maintenance,
  notifications, NAS paths, seeding controls and a blocklist.

The UI is available at `http://<host>:5000/` and supports Italian, English,
German, French, Spanish and Polish. The full [English manual](docs/MANUAL.en.md)
and [manuale italiano](docs/MANUAL.it.md) cover setup and every section of the UI.

## Torrent engines

Gextto keeps the control plane (queue, scoring, post-processing, archiving) and
swaps only the transfer engine in *Configuration → Torrent engine*. One engine
runs at a time, so switching is a controlled migration, never two clients on the
same data.

| Engine | Where it runs | Strengths | Trade-offs — pick it when |
|---|---|---|---|
| **gx-torrent** (default) | Separate supervised Go process, no libtorrent | Pure Go, no `libtorrent-rasterbar`; its own web page reachable on the LAN; **transfers keep running while Gextto restarts or updates**; a crash stays in its process; **sequential and first/last download**; **per-piece diagnostics**; **HTTP streaming with Range**; **BEP 16 super-seeding**; a **small, adaptive memory footprint** (cache sized from available RAM, active downloads/seeds and the storage type); falls back to libtorrent automatically if it cannot stay up | BitTorrent v1 and hybrid only (no v2-only torrents); file priority levels; no large in-process disk cache | you want a self-contained engine with minimal C/C++ dependencies |
| **libtorrent** (embedded) | Same process as Gextto (`libtorrent-rasterbar`) | Full feature set: sequential, per-torrent limits, super-seeding, web seeds, piece diagnostics; every advanced knob | Gextto and the engine share one process; needs the libtorrent library | you need every advanced control or maximum compatibility |
| **qBittorrent-nox** | External daemon, driven through its Web API | Reuse an existing qBittorrent and its Web UI/ecosystem; supports sequential and its own limits | Needs path mappings when the two processes see different paths; an extra process and dependency | you already run qBittorrent-nox or prefer its own UI |

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
| **gx-torrent** | ~25 MB | ~100 MB |
| **embedded libtorrent** (in Gextto's process) | ~0.5 GB | 3–5 GB |
| **qBittorrent-nox** | ~40 MB | ~5 GB |

libtorrent and qBittorrent keep a large in-process disk cache (GBs of anonymous
memory). gx-torrent has **no large in-process write-back cache by design**: it
writes pieces through and lets the reclaimable OS page cache do the coalescing,
so its own footprint stays in the tens/hundreds of MB even under load; it does
not hold data in RAM just to sit on it. Numbers vary with torrents, peers and
storage; on HDD/NFS Gextto gives gx-torrent a larger buffer, used only when the
writes fall behind the download.

## Install on Linux

The official installer targets 64-bit Linux systems with systemd. Run it as
root:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

It installs the program in `/opt/gextto`, stores service data in
`/var/lib/gextto`, and exposes the UI on port 5000. The default torrent engine,
`gx-torrent` (pure Go, no libtorrent needed), is installed next to `gexttod` and
started in managed mode; embedded libtorrent stays as the automatic fallback and
selectable alternative.

> [!IMPORTANT]
> Gextto is meant for a trusted network: by default the web UI and the API are
> open. If you reach it from outside, turn on the optional login
> (*Configuration → Access and services*; the local network stays free) and still use HTTPS
> through a reverse proxy or a VPN. Read the [security policy](docs/SECURITY.md)
> before exposing it remotely.

### Install from source

Source builds require Go 1.26+, a C++17 compiler and libtorrent-rasterbar
development headers. The normal build embeds the web UI; no separate frontend
build is required. See the [developer manual](docs/DEVELOPERS.md).

```bash
make build
```

`make build` increments the local build number and writes the versioned daemon
to `bin/gexttod`, plus the pure-Go `gx-torrent` engine next to it. To rebuild
without incrementing the number, use `make fast`. To build **only** the
`gx-torrent` daemon (no C++/libtorrent needed), use `make gx-torrent`. Verify
the exact binary, product version, build number and linked libtorrent version
with:

```bash
./bin/gexttod --version
```

## First safe run

1. Open `http://<server>:5000` and complete setup.
2. Configure storage paths, one source, and TMDB/TVDB credentials if needed.
3. Keep **dry-run** enabled; add one test title and run a manual search or
   cycle.
4. Check **Health** and **Logs**, including filesystem permissions and source
   results.
5. Enable active mode only after the outcome is correct.

The detailed setup checklist, NAS guidance and troubleshooting are in the
[user manual](docs/MANUAL.en.md).

## Update and uninstall

For an official installation, run the installer again. It verifies the release
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

The current automated suite covers the main UI sections with 12 accessibility
tests. This is not, by itself, a legal accessibility certification: manual
screen-reader, keyboard and assistive-technology testing is still required.
See the [accessibility analysis](docs/accessibility-analysis.md) for scope and known
limitations.

## Documentation

| Need | Document |
|---|---|
| Operate the service | [English manual](docs/MANUAL.en.md) · [Manuale italiano](docs/MANUAL.it.md) |
| NAS, reverse proxy, recovery | [Advanced guide](docs/ADVANCED.en.md) |
| HTTP integrations | [API reference](docs/API.md) |
| Move an existing installation | [Migration guide](docs/MIGRATION.md) |
| Terminal client | [TUI reference](docs/tui.md) |
| Build or contribute | [Developer manual](docs/DEVELOPERS.md) |
| Network and data safety | [Security policy](docs/SECURITY.md) |
| Accessibility scope and testing | [Accessibility analysis](docs/accessibility-analysis.md) |

For the full documentation tree, start from the [documentation index](docs/README.md).

## Support the project

If Gextto is useful to you and you would like to support its development, you
can make a contribution via PayPal. Thank you!

[![Donate with PayPal](https://img.shields.io/badge/Donate-PayPal-0070BA.svg?logo=paypal)](https://www.paypal.com/cgi-bin/webscr?cmd=_donations&business=azanzani@gmail.com&item_name=Support+Gextto+Project)

## ⚖️ Legal & fair use

Gextto is a **download automation tool**. It does not host, index, or distribute
any copyrighted content.

- Gextto connects to **indexers you configure** (Jackett, Prowlarr, public RSS
  feeds). It has no built-in index.
- What you download is **entirely your responsibility**. Use Gextto only for
  content you have the right to access — public domain, Creative Commons, or
  media you own.
- The torrent integration (libtorrent) is a neutral technology. Gextto does not
  encourage or facilitate piracy.
- This project is released under the **EUPL 1.2** open-source license.

> *"With great automation comes great responsibility."*

## License

Gextto is licensed under the [EUPL-1.2](LICENSE). See [NOTICE](NOTICE) for
notices about bundled and optional third-party components.
