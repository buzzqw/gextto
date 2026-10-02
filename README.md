# Gextto — EXpert Torrent Transfer Orchestrator

**Gextto (EXpert Torrent Transfer Orchestrator)** is a self-hosted daemon that
finds, selects, downloads, verifies, names and archives TV series, movies and
comics. Its web interface is the control plane; the daemon keeps running as a
service.

> **Italiano:** [README.it.md](README.it.md) · **Complete guide:**
> [English manual](docs/MANUAL.en.md)

## At a glance

- One service owns the search cycle, SQLite databases, torrent queue,
  post-processing and library archive.
- The default transfer engine is embedded libtorrent. qBittorrent-nox is an
  optional alternative, not an additional required service.
- Release selection uses quality, source, codec, audio, HDR, language and size.
  `ffprobe` can enrich those decisions with the properties of archived files.
- The responsive web UI and terminal TUI expose health, logs, backups,
  maintenance and integrations with Trakt, Simkl, Jellyfin and Plex.

### Torrent engines

Gextto currently supports the embedded libtorrent engine and the qBittorrent-nox
Web API adapter. The former Anacrolix backend is no longer included. Existing
configurations that still set `torrent_backend=anacrolix` fall back to the
embedded engine at startup and log a warning.

## Web interface and menu

The web UI is a complete control plane, not only a status page. It is responsive
on desktop and mobile, and the language selector currently supports **Italiano,
English, Deutsch, Français, Español and Polski**. The long-form manual is
bundled in Italian and English; the other UI languages use the English manual as
their documentation fallback. The top bar also provides settings search, CPU/RAM
and transfer metrics, theme/font controls, refresh and service status.

The **Font** dropdown includes safe generic presets and, in browsers that support
the Local Font Access API, can list the font families installed on the user's
device. The choice is applied to the interface and logs immediately and is saved
locally in that browser. Browsers without the API still support the presets and a
manual font-family name; browser permission may be required to inspect installed
fonts.

The official interface is served at `http://<host>:5000/`; `/v2` remains a
technical alias and the old `/ui` route has been removed. The **Downloads** page
refreshes the session every 5 seconds, including progress and speed for comic HTTP
downloads; the live transfer metrics in the top bar use the same interval.

| Menu | What it provides |
|---|---|
| **Dashboard** | Cycle controls for all domains or one domain, next-cycle timing, configured-title and free-space metrics, active torrents, recent downloads, upcoming series releases, feed results, disk/resource charts and quick links. |
| **Downloads** | Full torrent and HTTP session: add magnets or `.torrent` files, queue/progress/ETA/peer/ratio data, pause/resume/recheck/remove, per-torrent details, trackers, content priorities, limits, storage moves, seeding controls, comic downloads and history. |
| **Series** | Monitored TV library with seasons, episodes, quality/language/subtitle rules, aliases, browseable NAS path, missing searches, upgrades, manual search, archive scan, TVDB/TMDB cast links and controlled rename/repair actions. |
| **Movies** | Monitored movie library with title/year identity, quality and readable language requirements, exclusions, immediate search, best matches, archive matches, TMDB cast links, upgrade decisions, re-download and metadata editing. |
| **Missing** | Gap-oriented view of missing episodes and seasons, with filters, candidate searches and actions to force, ignore, reactivate or re-download intentionally. |
| **Explore** | TMDB discovery (trending, popular, top-rated, now-playing/upcoming), title lookup, release search and add-to-library actions, with duplicate protection for titles already monitored. |
| **Archive** | Full-text archive/release search, pagination, source and quality details, bulk queueing, magnet copying/deletion and “why not this one?” explanations; also exposes feed-seen series and movies. |
| **Comics** | GetComics-based monitored comics, post selection, metadata/cover/tag handling, link extraction, weekly packs, HTTP downloads and history actions. |
| **Configuration** | Daemon mode, sources, RSS/indexers/web engines/FlareSolverr, libtorrent, torrent engine, scoring, rename templates, acquisition, notifications, paths/NAS, advanced retention and translations. |
| **Integrations** | Trakt and Simkl authentication/watchlists/scrobbling, Jellyfin/Plex library refresh and event hooks for external programs. |
| **Maintenance** | Backups and restore-related actions, trash cleanup, archive scans, MediaInfo backfill, scoring recalculation, duplicate cleanup, manual folder rename, database maintenance and service restart. |
| **Health** | Database integrity, paths and permissions, free space, provider/backend status, CPU/RAM and service diagnostics. |
| **Logs** | Live and historical daemon log tail with filtering, line count and follow/refresh controls, useful for cycles, provider failures and torrent lifecycle events. |
| **Blocklist** | Review and manage releases blocked by quality, identity, provider or user decisions, so rejected candidates do not return silently. |
| **Manual** | The bundled operational guide, rendered inside the UI; it follows the selected language, with Italian and English full versions and English fallback for other languages. |
| **License** | EUPL-1.2 project license and bundled/optional third-party notices. |

The mobile shell keeps the most important operational pages reachable: Dashboard,
Downloads, Series, Movies, Health and Logs remain immediately available, while
the remaining discovery and system pages are available through the navigation.

## Resource efficiency

Gextto has been extensively optimized to keep its resource footprint predictable
and modest, especially when idle or waiting for the next scheduled cycle:

- the daemon is a single service and uses embedded libtorrent, without requiring
  a separate torrent stack;
- feed, indexer, web-search and background work use bounded concurrency rather
  than unbounded goroutines;
- provider backoff, retry windows and cooldowns prevent repeated failures from
  turning into CPU- and network-heavy retry storms;
- the torrent queue, speed policies, RAM-disk reconciliation and database work
  are applied incrementally instead of busy-looping.

This results in low CPU usage outside active searches and transfers and a
contained RAM footprint for a self-hosted media automation service. Actual
usage depends on the number of monitored titles, configured sources, active
torrents, archive scans and optional integrations.

### Indicative measurements

These are reference observations from a Linux x86_64 installation using the
embedded libtorrent engine, not hardware-independent benchmarks:

- after restart, with five restored torrents and no active downloads, the daemon
  process RSS was about **75–85 MiB**. The systemd cgroup value can be much
  higher because it also accounts for filesystem cache: one observation showed
  about **584 MiB** cgroup memory, including roughly **525 MiB** of file cache
  and only about **51 MiB** of anonymous memory;
- over roughly five minutes in that mostly idle state, accumulated CPU time was
  about **7 seconds** (around **2% of one CPU core on average**);
- an active search/download cycle can temporarily use substantially more memory:
  a peak of about **1.4 GiB** cgroup memory was observed during a high-load
  cycle, while the process resident baseline returned to the lower range
  afterward.

Use these figures as sizing examples, not guarantees. For the daemon's actual
resident footprint, prefer process RSS; cgroup totals also include reclaimable
filesystem cache. The main variables are active torrent count, libtorrent cache
and connections, concurrent providers, `ffprobe`/archive scans and the size of
the current cycle.

## Install on Linux

The official installer targets 64-bit Linux systems with systemd. Run it as
root:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

It installs the program in `/opt/gextto`, stores service data in
`/var/lib/gextto`, and exposes the UI on port 5000.

> [!IMPORTANT]
> The web UI is an unauthenticated administrative interface. Keep it on a
> trusted network, or put an authenticated HTTPS reverse proxy and firewall rules
> in front of it. Read the [security policy](docs/SECURITY.md) before exposing
> it remotely.

### Updating, uninstalling, dry-run

Re-running the installer updates an existing installation: it replaces the
binary, restarts the service and, if the new version fails to start, rolls back
to the previous one.

```bash
sudo bash install.sh --help       # all options
sudo bash install.sh --dry-run    # print what would happen, change nothing
sudo bash install.sh --uninstall  # stop and remove the program (keeps data)
sudo bash install.sh --uninstall --purge   # also remove the data directory
```

Environment overrides (`GEXTTO_DATA_DIR`, `GEXTTO_PORT`, `GEXTTO_USER`, …) stay
supported. `GEXTTO_LOCAL_ARCHIVE=/path/gextto-linux-x86_64.tar.gz` installs from
a local payload (offline or CI).

### Install from source

Source builds require Go 1.26+, a C++17 compiler and libtorrent-rasterbar
development headers. The normal build embeds the web UI; no separate frontend
build is required. See the [developer manual](docs/DEVELOPERS.md).

```bash
make build
```

`make build` increments the local build number and writes the versioned daemon
to `bin/gexttod`. To rebuild without incrementing the number, use `make fast`.
Verify the exact binary, product version, build number and linked libtorrent
version with:

```bash
./bin/gexttod --version
```

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

## Update

For an official installation, run the installer again. It verifies the release
checksum when published and replaces the payload atomically; data and
configuration are left untouched.

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

For a source checkout:

```bash
git pull --ff-only
./scripts/update.sh
curl -fsS http://127.0.0.1:5000/api/health
```

The source update script runs the versioned build, restarts the detected
service and leaves the data directory and configuration untouched. Use
`./scripts/update.sh --no-restart` when the restart must be performed manually.

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

## License

Gextto is licensed under the [EUPL-1.2](LICENSE). See [NOTICE](NOTICE) for
notices about bundled and optional third-party components.
