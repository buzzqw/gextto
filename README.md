# Gextto

Copyright (c) 2026 buzzqw and Gextto contributors.

**Gextto** is a self-hosted daemon for the automatic acquisition and archiving of
TV series, movies and comics.

A single Go binary bundles everything: the scraping engine, the SQLite archive,
the web UI/API (embedded with `//go:embed`) and an embedded **libtorrent**
session. No runtime and no external services required.

It watches what you configure, searches RSS/HTML sources, Torznab indexers
(Jackett/Prowlarr) and public search engines, scores every release by quality,
downloads the best one and renames/archives it into your library (NAS or local
disk).

> 🇮🇹 Italiano: [`README.it.md`](README.it.md)
> 📖 Full manual: [`docs/MANUAL.en.md`](docs/MANUAL.en.md) ·
> [`docs/MANUAL.it.md`](docs/MANUAL.it.md)

[![Donate](https://img.shields.io/badge/❤️_Support_Gextto-PayPal-00457C.svg)](https://www.paypal.com/cgi-bin/webscr?cmd=_donations&business=azanzani@gmail.com&item_name=Support+Gextto+Project)

---

## What Gextto is

- **One daemon, no external orchestrator** — scraping, downloading, renaming,
  archiving and the UI live in the same process.
- **Self-contained and self-updating** — one release archive (daemon, web UI,
  bundled libtorrent) that `gexttod --update` installs atomically, keeping
  databases and configuration intact.
- **Multiple sources** — generic RSS feeds, HTML listings (with FlareSolverr
  fallback for Cloudflare), Torznab indexers (Jackett/Prowlarr) and web engines.
- **Quality scoring** — resolution, source, codec, audio, HDR/Dolby Vision,
  groups and configurable weights, with a built-in simulator. One score is used
  consistently for acquisition, searches, upgrades, post-processing, archive
  records and rescoring, including size and movie-subtitle bonuses.
- **Automatic upgrades** — replaces an archived file when a better release
  appears (resolution jump, HDTV→WEB-DL, HDR, repack) beyond a configurable
  score threshold.
- **Series & movies** — TMDB metadata, posters, per-season monitoring, missing
  episode search, calendar, manual search.
- **Torrents** — embedded libtorrent: queue, limits, tags, peers, trackers,
  files, storage moves, seed policy, fastresume, VPN killswitch and restart
  recovery. **Stalled torrents are really paused and excluded from active slots**,
  then resumed/reannounced automatically. Add options include pause, sequential, skip-check, queue-top,
  first/last piece and metadata-only; per-file priorities, web seeds, tracker
  editing, super seeding and `.torrent`/magnet export are in the torrent
  details.
- **Comics** — GetComics monitoring and weekly packs. Every comic added (weekly
  pack, monitored title or *Download Now*) gets the **`Comic`** tag, so the
  *NAS paths per category (tag)* rule routes it to the configured folder.
- **Integrations** — Trakt, Simkl, Jellyfin, Plex, Telegram/e-mail/webhook
  notifications.
- **Web UI** — responsive single-page app, dark/light theme, fully **Italian and
  English** (runtime translation layer with YAML import/export), with log viewer,
  health, charts and maintenance tools.
- **Readable decisions** — search results can explain why a release passes or
  fails the checks, including score, rules and a read-only archive comparison;
  the explanation does not queue or modify data.
- **Seen from feed** — every release seen in the sources, grouped by title,
  browsable even for titles you do not monitor.
- **Automatic sanity rules** — hardcoded subtitles and absurd file sizes
  (per-resolution floors derived from a real archive) are rejected; no numbers
  to configure. Rejections are routine and logged at `DEBUG`, so the production
  `INFO` log stays clean.
- **Acquisition tuning** — delay before grabbing with a pending queue and a
  per-title "allow upgrades" switch.
- **Real media inspection** — `ffprobe` results (HDR, codec, audio, languages)
  are stored per file and **used in upgrade comparisons**, so decisions read the
  real archived file, not just its name. New files are probed on completion and
  a scheduled incremental backfill covers the rest; additive only, it never
  downgrades a file.
- **Resilience** — escalating provider backoff with a visible reset list and
  scheduled housekeeping/VACUUM.
- **Automation without clutter** — external event hooks live under
  *Integrations* and watched folders under *Configuration*. Watched files are
  checked for stability and import failures retry with backoff instead of being
  abandoned after a fixed attempt limit.
- **Monotonic library** — Gextto never pulls an older episode outside a
  recognised gap while it already owns later ones (a genuine quality upgrade
  still passes); gap-fill and manual actions always win.
- **Backups** — manual or scheduled (local, FTP, cloud folder, Telegram).
  They include databases and configuration, not media files or torrent state.
- **Light on resources** — the Go heap stays small (typically 4–20 MB) and its
  goroutines are idle between requests; CPU is spent almost entirely by the
  embedded libtorrent session while a transfer is in progress. libtorrent's disk
  cache and piece buffers grow during a download and are returned to the
  operating system once transfers complete (the daemon calls `malloc_trim`), so
  the resident size falls back to its idle value instead of staying at the
  download peak.

## Installation

### Install on a Linux server

The official installer supports Debian, Ubuntu, Fedora, openSUSE and Arch Linux.
On every push to `main`, GitHub Actions publishes a tested `continuous` Linux
payload. The installer downloads that payload, verifies its SHA-256 checksum and
installs the daemon and bundled libtorrent without compiling on the target host.
It also creates the service account, systemd service, runtime directories and the
empty databases on the first start. It never imports legacy data.

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | bash
```

Run the same command again to install the latest continuous build and restart
Gextto, or let the installed daemon update itself (see *Updating Gextto*).
Existing databases, configuration, downloads, archive paths and logs are kept in
`/var/lib/gextto`; the program and web UI live in `/opt/gextto`.

The installer uses the latest continuous artifact by default. Select another
repository or release with `GEXTTO_REPO` and `GEXTTO_RELEASE`.

Useful overrides (optional):

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | \
  GEXTTO_DATA_DIR=/srv/gextto GEXTTO_PORT=5000 GEXTTO_RELEASE=continuous bash
```

The service is `gextto.service`:

```bash
sudo systemctl status gextto.service
sudo journalctl -u gextto.service -f
```

For a per-user install without root, [`scripts/install-user-service.sh`](scripts/install-user-service.sh)
writes and starts a `systemctl --user` unit.

### Updating Gextto

There are two supported update paths. Both install the same release payload and
both leave **data and configuration untouched** — everything in
`/var/lib/gextto` (databases, logs, downloads, archive paths) survives an
update.

| Method | Command | Notes |
|---|---|---|
| Installer | re-run the `install.sh` command above | installs the latest published payload and refreshes the systemd unit |
| Daemon | `sudo gexttod --update` | updates only the payload: `gexttod`, `lib/`, `run.sh` |

The installed program lives in `/opt/gextto`:

```
gexttod     the daemon; the web UI is embedded (rpath $ORIGIN/lib)
lib/        the bundled libtorrent
run.sh      launcher (sets LD_LIBRARY_PATH)
VERSION     the release marker shown by --version
```

`gexttod --update` downloads `gextto-linux-<arch>.tar.gz`, verifies the
published `.sha256` when the release provides one, and stages the new payload
before touching the current installation. If the download, the checksum or the
extraction fails, the running installation is left as it was; if a swap fails,
the previous files are restored. The service is restarted automatically when
the command runs as root, otherwise the exact `systemctl` command is printed.

```bash
gexttod --version                       # version, build number and libtorrent
sudo gexttod --update                   # latest continuous build
sudo gexttod --update --channel stable  # latest tagged release
sudo gexttod --update --release v0.2.0  # a specific tag
```

From a source checkout, [`scripts/update.sh`](scripts/update.sh) rebuilds the
daemon (`make build`) and restarts the service; add `--release` to install the
published payload instead.

The systemd unit is intentionally **not** overwritten by `--update`: local
customisations (user, ports, paths) are preserved. Use the installer to
regenerate the unit. The `VERSION` marker written next to the executable is the
release name (`continuous`, a tag, or `source-main`); the numeric build number
is compiled in and identifies the exact build.

### Standalone Linux package

The packaging script produces a self-contained `gextto-linux-<arch>.tar.gz` (with
a `.sha256` next to it) containing:

```
gexttod     the daemon; the web UI is embedded (rpath $ORIGIN/lib)
lib/        the bundled libtorrent shared library
run.sh      launcher (sets LD_LIBRARY_PATH)
README.md   quick start and prerequisites
```

Extract it and run it directly, without a compiler:

```bash
mkdir gextto && tar -xzf gextto-linux-x86_64.tar.gz -C gextto
cd gextto
./run.sh --version
GEXTTO_DATA_DIR="$PWD/data" GEXTTO_DRY_RUN=1 GEXTTO_ACTIVE=0 ./run.sh
```

Because the archive bundles libtorrent and the daemon embeds the web UI, no
system libtorrent is required. The archive is built by
[`scripts/package-linux.sh`](scripts/package-linux.sh) and is what
`gexttod --update` installs. It needs a modern 64-bit Linux (glibc, libstdc++,
OpenSSL 3, zlib, libzstd); `ffprobe` is optional. Prebuilt assets are currently
published for **x86_64** only; on `aarch64` the installer and
`gexttod --update` report that no asset is available. For a managed service
install use the installer above.

### Build from a checkout (development)

For contributors, install Go ≥ 1.26, a C++17 toolchain and the
`libtorrent-rasterbar` development headers, then build the daemon:

```bash
make build            # -> bin/gexttod
# or
CGO_ENABLED=1 go build -o bin/gexttod ./cmd/gexttod
```

The web UI is embedded in the binary with `//go:embed`, so there is no separate
UI build step; an on-disk bundle in `GEXTTO_UI_DIR` (or `<exe>/ui`) still takes
precedence for development.

To run locally in dry-run mode, without real downloads:

```bash
GEXTTO_DATA_DIR="$PWD/data" GEXTTO_ACTIVE=0 GEXTTO_DRY_RUN=1 ./bin/gexttod --dry-run
```

For a real service installation, use the installer described above.

### Check it

```bash
curl --fail http://127.0.0.1:5000/api/status
curl --fail http://127.0.0.1:5000/api/health
```

## How to use it

Everything is managed from the web UI, on the configured address (default
`http://<host>:5000`).

### First run

1. Open the UI. If no data directory exists yet, complete the **initial setup**.
2. Keep the daemon in **dry-run** until your sources are configured: in dry-run
   no downloads start.
3. When ready, enable **active mode** in *Configuration → Daemon*.

### Configure the sources

In *Configuration → Sources* add:

- RSS feeds / HTML listings (URL and pages to follow);
- Torznab indexers (Jackett/Prowlarr) with the **Verify** button;
- web search engines and, if needed, the FlareSolverr URL;
- content filters and blocklist.

The same section holds scoring, renaming, paths and libtorrent settings.

For Jackett, normally use its base URL, for example `http://host:9117`, together
with the API key. Gextto queries the Torznab endpoint and uses `t=caps` for the
health check, so it also validates the key and configured indexers. Torznab
errors are detected even when Jackett returns HTTP 200, and results may identify
the source as `jackett:TrackerName`.

### Add series and movies

- From **Explore** (TMDB search) in one click, or
- from **Series / Movies → Add** manually.

For each title pick minimum quality, language, seasons/years, aliases, exclusions
and the **NAS path**. Comics are managed from **Comics**.

### Cycles and downloads

Gextto works in cycles: search, evaluate, download, rename, archive.

- From the **Dashboard** you can start a full cycle, a single domain (Series,
  Movies, Comics) or an immediate backup.
- Cycles also run automatically at the configured interval.
- In **Downloads → Torrent session** you see the torrents in the client (with a
  **NAS** badge when already archived); under the name you find the **reason** for
  the download and its **source** (indexer/RSS/web). **Clean completed** removes
  torrents that reached their seed limit.
- **Download history** lists torrents that **left the session**, with the outcome
  (NAS path or the reason they were rejected).

### Reading the logs

The log follows the operation instead of using an info hash as the only readable
identity:

1. **CYCLE STARTED** identifies the mode and domain (`full`, Series, Movies or
   Comics).
2. **Step 1/2** and **Step 2/2** show how many sources and titles are being
   analysed; unreachable sources include their name and reason.
3. **Gap fill** separates archive hits from episodes that require an
   online search.
4. **Download started / Gap filled** shows title, episodes, source and
   score. A skipped candidate includes the decision reason.
5. **CYCLE REPORT** and **CYCLE DOWNLOADS** summarise duration, releases,
   downloads, upgrades, gaps and errors.
6. Torrent events explain metadata, NAS/RAM-disk moves, completion, renaming,
   seeding, recovery and removal.

Each line uses `date time LEVEL [component] message · key: value`. Torrent lines
always include a readable name or title; the hash is only a technical correlation
field for errors. `INFO` describes the normal path, `WARN`/`ERROR` explain the
failed operation and affected resource, and `DEBUG` adds diagnostic detail when
enabled (routine filter and sanity rejections live here too).

### UI sections

| Section | Purpose |
|---|---|
| **Dashboard** | Manual search, cycle buttons, stats, network, upcoming releases |
| **Downloads** | Torrent session, add magnet/.torrent, history |
| **Series / Movies** | Library, details, missing episodes, manual search |
| **Missing** | Missing episodes and gap filling |
| **Calendar** | Upcoming releases from monitored series |
| **Explore** | TMDB discovery and release search |
| **Archive** | Past releases; *Seen from feed* for movies/series |
| **Comics** | GetComics and weekly packs |
| **Configuration** | Sources, libtorrent, scoring, renaming, paths, notifications |
| **Integrations** | Trakt, Simkl, Jellyfin, Plex, event hooks |
| **Maintenance** | Backups, duplicates, scoring, restart |
| **Health, Logs, Charts** | Diagnostics and monitoring |

A detailed walkthrough of every screen is in the
[manual](docs/MANUAL.en.md).

### Command line

The daemon is normally started by the systemd service. When you run `gexttod`
directly it also accepts these options:

| Option | What it does |
|---|---|
| `-h`, `--help` | print the usage summary |
| `-V`, `--version` | print the installed version, build number and bundled libtorrent |
| `--config <file>` | use a specific configuration file (default `gextto.json`) |
| `--dry-run` | start without real downloads |
| `--update` | download and install the latest payload (see *Updating Gextto*) |

`--update` options:

| Option | What it does |
|---|---|
| `--repo <owner/name>` | GitHub repository to download from (default `buzzqw/gextto`) |
| `--channel <name>` | `continuous` (default) or `stable` |
| `--release <tag>` | install a specific release tag |
| `--install-dir <dir>` | installation directory (default: the binary's directory) |
| `--archive <file>` | install from a local archive instead of downloading |
| `--force` | reinstall even if the version is unchanged (only meaningful with `--release`: the rolling `continuous` and `stable` channels always fetch the latest asset) |
| `--no-restart` | do not restart `gextto.service` after installing |

Examples:

```bash
gexttod --version                          # what is installed now
sudo gexttod --update                      # latest continuous build
sudo gexttod --update --channel stable     # latest tagged release
sudo gexttod --update --release v0.2.0     # a specific tag
gexttod --update --install-dir /srv/gextto --no-restart
gexttod --update --archive ./gextto-linux-x86_64.tar.gz   # offline
```

When testing an update without touching a real installation, combine
`--install-dir` with a throwaway directory and `--no-restart`; `--archive`
avoids the network entirely.

`gexttod --version` prints the product version, the **monotonic build number**
generated at build time (from the `VERSION` and `build_number` files) and the
bundled libtorrent. The optional `[marker]` is the release label written next to
the executable by the installer or the release archive (`continuous`, a tag or
`source-main`).

### Where to find the details

The README is the practical overview; the [manual](docs/MANUAL.en.md) documents
every screen. Quick index:

| Topic | README | Manual |
|---|---|---|
| Install and service | *Installation* | [1. First start](docs/MANUAL.en.md#1-first-start) |
| Update, version, package | *Updating Gextto*, *Command line* | [1. First start](docs/MANUAL.en.md#1-first-start) |
| First run and modes | *First run* | [1. First start](docs/MANUAL.en.md#1-first-start) |
| Dashboard, cycles, stats | *Cycles and downloads* | [2. Dashboard](docs/MANUAL.en.md#2-dashboard) |
| Torrents, stalled, history | *Cycles and downloads* | [3. Downloads](docs/MANUAL.en.md#3-downloads) |
| Series, episodes, gaps | *Add series and movies* | [4. Series](docs/MANUAL.en.md#4-series) |
| Movies | *Add series and movies* | [5. Movies](docs/MANUAL.en.md#5-movies) |
| Explore, Archive, Comics | *UI sections* | [6. Explore, Archive, Comics](docs/MANUAL.en.md#6-explore-archive-comics) |
| Sources, scoring, renaming | *Configure the sources* | [7. Configuration](docs/MANUAL.en.md#7-configuration) |
| Trakt, Jellyfin, hooks | *UI sections* | [8. Integrations](docs/MANUAL.en.md#8-integrations) |
| Backups, duplicates, DB | *UI sections* | [9. Maintenance](docs/MANUAL.en.md#9-maintenance) |
| Health, logs, charts | *Reading the logs* | [10. Health, Logs, Charts](docs/MANUAL.en.md#10-health-logs-charts) |
| Notifications | *UI sections* | [11. Notifications](docs/MANUAL.en.md#11-notifications) |
| Common problems | — | [12. Troubleshooting](docs/MANUAL.en.md#12-troubleshooting) |

### Data and logs

- Default data directory: `data/` (override with `GEXTTO_DATA_DIR`).
- Logs in `data/gextto.log`, with 5 MB rotation (active file + 3 backups),
  streamed live in the UI. The viewer supports filtering and follow/pause.
- Databases: `gextto_series.db`, `gextto_archive.db`, `gextto_config.db`,
  `gextto_comics.db`.
- Torrent session state in `data/gextto_torrents_state/` (fastresume).

### Environment variables

| Variable | Purpose |
|---|---|
| `GEXTTO_DATA_DIR` | Data directory (databases, logs, downloads) |
| `GEXTTO_LISTEN` | Web UI/API address (default `0.0.0.0:5000`) |
| `GEXTTO_ENGINE_LISTEN` | Internal engine channel (default `127.0.0.1:8889`) |
| `GEXTTO_UI_DIR` | Directory of the compiled web UI (packaged installs) |
| `GEXTTO_INSTALL_DIR` | Installation directory used by `--update` |
| `GEXTTO_REPO` | GitHub repository used by installer and `--update` (default `buzzqw/gextto`) |
| `GEXTTO_RELEASE` | Installer payload (`continuous` by default, or a release tag) |
| `GEXTTO_ARCH` | Installer asset architecture override (`x86_64` or `aarch64`) |
| `GEXTTO_ACTIVE` | `1` enables the acquisition cycles |
| `GEXTTO_DRY_RUN` | `1` disables real downloads |
| `GEXTTO_API_TOKEN` | Optional bearer token for the API/UI |
| `GEXTTO_LOG` | Log filter (`info`, or `debug` when debug is enabled) |
| `GEXTTO_CHANNEL` / `GEXTTO_VERSION` | Updater channel (`continuous`, `stable`) or a specific release tag |
| `GEXTTO_PORT` / `GEXTTO_ENGINE_PORT` | Installer: UI/API port (default `5000`) and engine port (default `8889`) |
| `GEXTTO_USER` | Installer: service account to create/use (default `gextto`) |
| `GEXTTO_SKIP_PACKAGES` | Installer: `1` skips system package installation |

## Reliability

- **Panic isolation**: background workers run under a watchdog that logs and
  restarts them if they panic; HTTP handlers return a clean `500` instead of
  aborting the connection. A defect cannot take the daemon down.
- **Completion integrity checks**: before a finished download is renamed and
  recorded as archived, the file is validated (non-empty, not zero-filled,
  recognised video container). A corrupt or truncated file is quarantined and
  the torrent is marked as failed, so it never replaces a good copy.
- **No premature action**: seed policy, relocation, pause and removal only apply
  to torrents that are genuinely complete (verified bytes, not a momentary
  state). A storage move that leaves an incomplete download paused resumes it
  automatically.
- **Startup data re-check**: a torrent that fastresume restores as complete but
  whose files are missing or zero-filled on disk is force-rechecked at startup,
  so a relocated or truncated file is re-downloaded instead of being trusted.
- **Safe RAM-disk staging**: a download is moved off the RAM disk based on the
  bytes **still to be written**, not its full size, so a nearly complete torrent
  is not relocated mid-transfer (which risked a reset or a zero-filled file).
- **No unsolicited outbound calls**: integration endpoints (Trakt, Simkl) return
  a clear error before any network request when they are not configured, instead
  of probing the third-party API with empty credentials.
- **Real BitTorrent tests**: `make test-real` starts a local seeder, a local
  tracker and a leecher session and verifies a real transfer byte-for-byte, with
  no external network. The full suite is `make test`.

## Development

### Build, test and run

```bash
make build          # production daemon build (bin/gexttod)
make fast           # quick build without trimpath/ldflags
make vet            # go vet
make test           # CGO_ENABLED=1 go test ./...
make test-real      # hermetic local seeder/tracker/leecher transfers
make fmt            # gofmt -w .
```

`scripts/acceptance.sh` runs the isolated smoke test described in
[`docs/ACCEPTANCE.md`](docs/ACCEPTANCE.md): it uses a temporary data directory
and dedicated ports in dry-run, so it never touches a real installation.

Continuous integration (`.github/workflows/`) runs `go vet` and the full test
suite on every push and pull request, plus CodeQL analysis; tagged releases and
the continuous build are packaged for Linux x86_64 and published automatically.
The build number is the workflow run number in CI, so every published payload is
distinct.

### Packaging

[`scripts/package-linux.sh`](scripts/package-linux.sh) builds the standalone
archive that the installer and `gexttod --update` consume:

```bash
make build
scripts/package-linux.sh --binary bin/gexttod
# -> gextto-linux-<arch>.tar.gz + gextto-linux-<arch>.tar.gz.sha256
```

The archive contains `gexttod`, the bundled libtorrent in `lib/`, a `run.sh`
launcher and a short README. The web UI is embedded in the binary and the daemon
is linked with an `$ORIGIN/lib` rpath, so it runs straight from the extracted
archive; `install.sh` copies the same payload into `/opt/gextto`.

To exercise the updater locally without replacing your checkout binary, point it
at a throwaway directory and use the freshly built archive:

```bash
scripts/package-linux.sh --output /tmp/gextto-linux-x86_64.tar.gz
mkdir -p /tmp/gextto-install
bin/gexttod --update --archive /tmp/gextto-linux-x86_64.tar.gz \
  --install-dir /tmp/gextto-install --no-restart
/tmp/gextto-install/gexttod --version
```

### How the pieces fit

| Component / resource | Role | Functions it powers |
|---|---|---|
| Go + `net/http` (`web.go`, `web_router.go`) | daemon runtime and HTTP server | cycles, REST API, SSE log stream, static UI |
| Embedded web UI (`webui/`, `//go:embed`) | single-page front-end | dashboard, library screens, settings, bilingual UI |
| SQLite (`modernc.org/sqlite`, pure Go) | local persistence | series/episodes, movies, archive, comics, config, cycle stats, torrent metadata |
| libtorrent (`libtorrent_bridge.cpp`, `libtorrent_cgo.go`, `libtorrent.go`) | embedded BitTorrent engine | queue and limits, seeding policy, trackers, files, peers, fastresume, VPN killswitch |
| `rss.go`, `websearch.go`, `httpx.go` | source acquisition | RSS/HTML listings, Torznab indexers, web engines, FlareSolverr fallback |
| `tmdb.go`, `tvdb.go` | metadata providers | posters, seasons, episode dates, discovery |
| `mediainfo.go` | real media inspection | codec/HDR/audio/language data that feeds upgrade comparisons |
| `integrations.go` | media-server integrations | Trakt, Simkl, Jellyfin, Plex |
| `notifier.go` | notifications | completion/error alerts, HMAC-signed webhooks, event hooks |
| `backup.go` | scheduled backups | database and configuration snapshots (local, FTP, cloud, Telegram) |
| `parser.go`, `decision.go`, `config.go`, `internal/rules` | domain logic | release parsing, quality scoring, sanity checks, upgrades, decision traces |
| `cli.go`, `update.go` | operations | `--version`/`--help`, release download with checksum, atomic payload swap and rollback |
| installer + packaging + systemd | operations | source/release install, service unit, standalone archive |
| `importer.go` | migration CLI | one-off import of an older installation's databases |

Development builds stay small and never accumulate forever:

- Builds are incremental and the shared module cache is reused, so `make fast` is
  quick; `go clean -cache` starts from scratch when needed.
- Developer-only helper scripts are intentionally local and ignored by Git; they
  are not required by the installer or by a production installation.

## Security

The web port is an administrative interface. Bind it to loopback or set
`GEXTTO_API_TOKEN` before exposing it to a network; the installer generates a
random token on a fresh install and stores it in `/etc/gextto/gextto.env`. See
[SECURITY.md](SECURITY.md) for the network model, what the daemon guarantees (no
shell, parameterised SQL, bounded requests, atomic updates) and how to report a
vulnerability.

## ❤️ Support the project

Gextto is free, open-source software built entirely in spare time.
If it saves you hours of configuration, RAM, or disk wear — consider buying the
author a coffee.

Every donation directly funds new features, bug fixes, and keeping the project
alive.

<div align="center">

[![Donate with PayPal](https://img.shields.io/badge/Donate-PayPal-00457C?style=for-the-badge&logo=paypal)](https://www.paypal.com/cgi-bin/webscr?cmd=_donations&business=azanzani@gmail.com&item_name=Support+Gextto+Project)

*Thank you. Seriously.*

</div>

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

Licensed under the **European Union Public Licence v. 1.2** — see [`LICENSE`](LICENSE).
