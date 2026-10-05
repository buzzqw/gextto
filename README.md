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
  torrent transfers, post-processing and the library archive. Embedded libtorrent
  is the default; qBittorrent-nox is an optional alternative.
- **Series, movies and comics:** monitor titles, search configured sources and
  manage downloads and archived media from one responsive web UI or the terminal
  TUI.
- **Quality-aware automation:** score releases by quality, source, codec, audio,
  HDR, language and size; protect better existing files and explain rejected
  candidates. `ffprobe` can enrich archived-file decisions.
- **Flexible sources and integrations:** use RSS, Torznab indexers such as
  Jackett and Prowlarr, web search and optional FlareSolverr; connect Simkl,
  Jellyfin and Plex.
- **Operational tools included:** health checks, logs, backups, maintenance,
  notifications, NAS paths, seeding controls and a blocklist.

The UI is available at `http://<host>:5000/` and supports Italian, English,
German, French, Spanish and Polish. The full [English manual](docs/MANUAL.en.md)
and [manuale italiano](docs/MANUAL.it.md) cover setup and every section of the UI.

## Resource footprint

Gextto is designed to remain lightweight while idle: it is one daemon with
embedded libtorrent, uses bounded concurrency for source and background work,
and backs off failing providers instead of retrying continuously. Actual CPU and
memory use depend on monitored titles, sources, active torrents and archive
scans; treat any machine-specific measurement as indicative, not a guarantee.

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
