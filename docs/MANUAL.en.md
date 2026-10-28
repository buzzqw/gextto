# Gextto User Manual

This is the operational guide for Gextto's web UI and terminal TUI. The web
interface is available in English, Italian, German, French, Spanish and Polish; this document uses
English labels. For Italian labels, read [MANUAL.it.md](MANUAL.it.md). The
non-Italian UIs use their bundled catalogs and fall back to this English manual
for long-form documentation. The **Manual** entry opens the updated manual
online in the interface language; the **Changelog** entry opens the release
notes. Neither is embedded in the binary, so updating them does not require
rebuilding Gextto.

> [!IMPORTANT]
> Start in **dry-run**. Verify paths, source access and one test title before
> enabling active downloads. The UI is an administrative interface, open by
> default: keep it on a trusted network or, for access from outside, turn on the
> login (*Configuration → Access and services*) and use HTTPS.

## How to use this guide

Gextto is a single daemon: it searches sources, evaluates releases, manages
the active torrent engine, renames files and archives them. Normal operation does not require
separate orchestrator processes.

The web interface works on a phone too: below 900 px the shell switches to a
compact layout with a scrollable top navigation, the dashboard metrics in two
columns, full-width dialogs and tables that scroll horizontally inside their
panel. Every action stays reachable by touch. Pages and UI files travel
compressed and the stylesheets and scripts stay in the browser cache, so the
interface stays usable on a slow network as well.

The official UI is served at `http://<host>:5000/`; the old `/v2` prefix
redirects to the root and the old `/ui` route no longer exists. The **Downloads** page refreshes
the session automatically every 5 seconds; **Refresh every 5 s** in the **Tools**
menu turns that off or back on, and the **Aggiorna** button in the top bar
(next to **Add torrent**) reloads at once. The transfer rates and the process
loads in the top bar use the same 5-second refresh. The bar shows **CPU and RAM per process**: **Gextto** (the
daemon), **gx-torrent** (the torrent engine, when the daemon is local) and
**aMule** (when **eD2k and Kad through aMule** is on and `amuled` runs on the
same machine). Badges for remote processes are absent, because those figures
cannot be read from there.

The recommended path for a new installation is:

1. configure paths and sources;
2. keep the daemon in **dry-run**;
3. add one test title;
4. run a manual search or cycle;
5. inspect Health and Logs;
6. enable active mode only after checking the results.

## Accessibility

The web UI has been improved for keyboard and assistive-technology use:

- search, configuration and language controls have accessible names;
- dialogs manage focus entry, `Tab`, `Escape` and focus restoration to the
  control that opened them;
- sortable table headers work from the keyboard and expose their current sort;
- asynchronous updates, errors, progress and notifications use live regions;
- tables, progress bars, tabs and landmarks expose additional semantics;
- the layout supports narrow viewports, reflow and improved light-theme
  contrast.

Run the automated checks from a source checkout:

```bash
cd uiweb/end2end
npm ci
npm run test:a11y
```

The current suite runs 14 axe-core/Playwright checks on the main sections,
including dialogs, keyboard sorting, polling and 320 px reflow. This result is
not a WCAG or legal-compliance declaration: an attestation also requires manual
screen-reader, keyboard, magnification and assistive-technology testing, plus
an assessment of the applicable requirements. See
[`ACCESSIBILITY.md`](ACCESSIBILITY.md) for the detailed scope, the tests and
the known limitations.

This manual distinguishes between:

- **manual search**: inspect results and queue one choice;
- **automatic cycle**: search monitored titles and decide what to download;
- **archive**: files already imported or stored in the library;
- **torrent session**: downloads still managed by the active torrent engine.

### First-run checklist

Before enabling real downloads, verify that:

- `http://<host>:5000` is reachable;
- *Health* reports no path-permission or free-space problem;
- the temporary directory is writable;
- the library/NAS directory is mounted and writable by the service user;
- at least one source succeeds in *Verify*;
- a manual search returns plausible releases;
- dry-run produced no unexpected errors.

If you use a NAS, first create a test file in the destination as the same user
that runs `gextto.service`. A path visible to your shell user may not be visible
to the systemd service user.

**Security.** The web port is an administrative interface, open by default. Keep
it on loopback or the home network; for access from outside turn on the optional
login (see *Access*; the local network stays free) behind an HTTPS reverse proxy
or a VPN. In the standard install, `/opt/gextto/gexttod --version` tells you
which build is running.
See [`SECURITY.md`](SECURITY.md) for the full network model.

- [Accessibility](#accessibility)
- [1. First start](#1-first-start)
- [2. Dashboard](#2-dashboard)
- [3. Downloads](#3-downloads)
- [4. Series](#4-series)
- [5. Movies](#5-movies)
- [6. Explore, Archive, Comics](#6-explore-archive-comics)
- [7. Configuration](#7-configuration)
- [8. Integrations](#8-integrations)
- [9. Maintenance](#9-maintenance)
- [10. Health, Logs, Charts](#10-health-logs-charts)
- [11. Notifications](#11-notifications)
- [12. Quick reference](#12-quick-reference)
- [13. Troubleshooting](#13-troubleshooting)

---

## 1. First start

Gextto runs as a single service. Open the web UI at `http://<host>:5000`.

- On a new installation the first visit opens the **Initial setup**, in five
  steps: *Access* (a password, optionally with open access from the local
  network), *Folders* (library, downloads, temporary files and trash; **Browse**
  opens the server folder picker, which can also create folders; **Check** tests
  as the service user whether each folder exists and is writable, the disk type
  including NAS mounts, the free space and whether it is on the same disk as the
  library), *Sources* (a Prowlarr/Jackett/indexarr indexer and the TMDB key,
  recommended, or alternatively the TVDB key with its PIN when needed),
  *First title* (the search on TMDB, or on TVDB without a TMDB key, with its add
  button) and *Start* (automatic
  cycle and leaving test mode). It can be skipped and reopened at any time from
  `/?view=setup`; an installation that already has series, movies or an archive
  does not show it.
- **Active vs dry-run**: the automatic cycle is **on by default**, so Gextto
  searches on its own; with **dry-run** enabled (the *Download for real* choice
  in the setup wizard) it starts no real downloads. The cycle switch is *Automatic
  cycle (search and download)* in *Configuration → General*.
- Add series/movies from **Explore** (TMDB) or **Series / Movies → Add**.
- Metadata (seasons, episodes, posters, calendar) comes from TMDB or TVDB: see
  *Configuration → Metadata: TMDB or TVDB*.
- The **Donate** button (PayPal) is always visible at the bottom right: if
  Gextto is useful to you, you can support development; the address is also in
  the README.

### Recommended first cycle

1. In *Configuration → Archive and space*, check the download, temporary, library and trash
   directories.
2. In *Configuration → Sources*, add one working source and check it in
   *Integrations → Check sources*.
   Add the others only after the first one works.
3. Keep real downloads disabled and add one series with one season or one test
   movie.
4. From the Dashboard, run a cycle for the relevant domain.
5. Open *Health* and *Logs*: you should see the cycle, queried sources and the
   reason why a release was accepted or rejected.
6. Run a manual search and inspect one result with **Why not this one?**.
7. When paths, filters and results are correct, enable *Active mode*.

Do not change quality, paths and sources at the same time when troubleshooting:
one change at a time makes the cause reproducible.

### Paths and responsibilities

Gextto uses separate paths for separate roles:

| Path | Use | May it be temporary? |
|---|---|---|
| Download | data for active torrents | no, while a torrent is active |
| Temporary/incomplete | metadata and incomplete data | yes, but it must be writable |
| Library/NAS | final archived files | no |
| Trash | files removed during upgrades/cleanup | yes, according to retention |
| Watched folder | `.torrent`/`.magnet` files to import | yes, but not while copying |

Do not use the temporary directory as the final library and do not delete files
from an active torrent manually: use the torrent-session actions.

### Command line and updates

The daemon is normally run by the systemd service. When invoked directly it
understands:

- `gexttod --version` — installed version, build number and release marker;
- `gexttod --help` — usage summary;
- `gexttod --update` — download and install the latest payload of the installed
  channel (continuous or stable); the previous versions are kept as
  `gexttod.prev` and `gx-torrent.prev` (re-running `install.sh` keeps them too)
  and put back by themselves if the new ones do not start;
- `gexttod --config <file>` and `gexttod --dry-run` — used by the service and for
  local tests.

#### Terminal TUI

Start the TUI with `/opt/gextto/gexttod tui` after starting the daemon (use your
installation directory if different). It is a separate
process that uses only the daemon's HTTP API and SSE stream: it never reads the
databases directly and can be used from an SSH shell. On a narrow terminal — as
an SSH app on a phone typically is — the interface compacts itself (below 60
columns the header, the columns and the long values adapt instead of being cut).

```bash
/opt/gextto/gexttod tui
/opt/gextto/gexttod tui --url http://host:5000 --lang en
GEXTTO_URL=http://host:5000 /opt/gextto/gexttod tui
```

The options are `--url`/`-u` and `--lang`/`-l`; the URL environment variable is
`GEXTTO_URL`. The nine tabs
are Status, Torrents, Logs, Health, Archive, Missing, Blocklist, Library and Maintenance. Full key,
prompt and action documentation is in [`docs/tui.md`](tui.md).

**Installation and updating.** Every push to `main` publishes a tested
`continuous` Linux payload. The official installer downloads that payload,
verifies its SHA-256 checksum and installs it without compiling anything on the
target server:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

The installer must run as root and requires systemd. Use `GEXTTO_REPO` and
`GEXTTO_RELEASE` to select another repository or release. It stores the service
data in `/var/lib/gextto` and the program in `/opt/gextto`; an optional
`/etc/gextto/gextto.env` file, if present, is loaded as environment overrides
(the API token is not stored there: set it in *Configuration → Access and
services*). By default the service runs as **the user who runs the installer**;
`--user NAME` (or `GEXTTO_USER`) selects another user, and `--user gextto` the
isolated, login-less system account.

For a no-root installation from a source checkout:

```bash
make build          # pure Go, with gx-torrent
GEXTTO_DATA_DIR="$HOME/gextto-data" \
  GEXTTO_LISTEN=127.0.0.1:5000 \
  scripts/install-user-service.sh
systemctl --user status gextto.service
```

For Docker (Gextto, gx-torrent, Jackett, Prowlarr, FlareSolverr and aMule together) see [Docker: full stack](DOCKER.en.md).

The user service listens on port 5000 on all interfaces by default. Restrict
access with a firewall or reverse proxy; use
`loginctl enable-linger "$USER"` if it must run after logout.
The installer and `gexttod --update` install the same release payload.
`--update` downloads `gextto-linux-<arch>.tar.gz`, verifies the
published `.sha256` (required: without a checksum the update is refused), stages the files, then swaps the executable
(with the embedded web UI), the bundled `lib/` and `run.sh` with atomic renames.
Data and
configuration in `GEXTTO_DATA_DIR` (default `/var/lib/gextto`) are never touched:
a failed download, checksum or extraction leaves the running installation
untouched, and a failed swap is rolled back. The `VERSION` marker next to the
executable is updated and shown by `--version`.

The official repository publishes Linux assets for **x86_64 and aarch64**
(archive, `.sha256` and manifest) with every release. The installer and
`gexttod --update` pick the asset for the machine's architecture.

- `--channel stable` / `--release <tag>` — choose the release to install;
- `--install-dir <dir>` — install elsewhere (default: the binary's directory);
- `--archive <file>` — install from a local archive (offline);
- `--force` — reinstall even if the version is unchanged;
- `--no-restart` — do not restart `gextto.service`.

Run `/opt/gextto/gexttod --update` as root to restart `gextto.service`
automatically; otherwise the exact
`systemctl` command is printed. The systemd unit is not overwritten, so local
customisations (user, ports, paths) are preserved. The same payload is the
release package the installer uses (README, *Install on Linux*).

## 2. Dashboard

- **Interface controls** — the top bar offers **Theme**, text size and
  **Font**. **Theme** opens a single dialog: at the top, the grid of themes with
  a preview of their colours (*Dark*, *Light*, *Cream* — a warm light theme for
  long reading — and the classic Nord, Dracula, Solarized, Gruvbox, Monokai,
  One Dark, Tokyo Night, Catppuccin, GitHub, Rosé Pine, Everforest, OLED black,
  High contrast, in dark and light variants); a theme applies as soon as you
  select it, arrow keys included. Below, *Your themes* lists the saved themes and
  **Customize** starts from the chosen theme with four pickers (background,
  panels, text, accent): the other shades are computed from them, the text on
  accent buttons turns light or dark to stay readable and a notice flags a
  contrast that is too low. Give it a name and press *Save theme*: saved themes
  live on the server and show up on every device (a name already in use is
  overwritten, ✕ deletes the theme). The active theme, the text size and the
  font stay a per-browser choice; on a phone **Theme** is in the *Other* menu.
  **Font** applies a generic preset, a detected local font or a manually entered
  family to the interface and logs. Installed-font detection depends on browser
  support for the Local Font Access API and may require permission.
- **Summary in the top bar** — at the far right of the top bar controls row,
  under the clock, the Dashboard shows two compact badges: **Free space** and
  **Archived magnets** (with the ones seen in the feeds). They appear only on
  the Dashboard; other pages do not show them. On a wide screen **Italiano**
  opens that row under the first badge (Gextto CPU and RAM).
- **Manual global search** — searches archive + indexers + web engines.
- **Automatic search** — run a full cycle or a single domain (Series, Movies,
  Comics); shows next run, interval and last-cycle outcome; backup now.
- **In session** — one card on top: torrents downloading, stalled and seeding,
  the last-30-days bandwidth and direct access to Downloads, with the **last
  30 minutes traffic chart** below (the **download** and **upload** curves, the
  peak, the current speed and the line of the **active bandwidth limit**: base,
  schedule or exception; it is not drawn when the speed is *Auto*, i.e. when
  Gextto measures the line and adapts, and in that case the card shows the
  **Auto** badge, while the measured ping appears next to the **↓ Download**
  speed in the top bar, not "No limit"). Speeds
  under 1 KB/s count as zero: that is
  just protocol chatter, and the chart stays flat instead of twitching. With
  eD2k on, aMule has its own **dashed lines** and an **eD2k ↓ ↑** entry next to
  the **Torrent** one; the speeds in the top bar are the sum of torrents and
  aMule. It refreshes on its own.
- **To check** — below the strip: **stuck** downloads (with the reason, the
  next attempt and when they will be abandoned; the name stops at its
  season/episode and the full one appears on hover), **pending moves to the
  library** (destination and attempts) and the warnings, errors and 🛟 pack
  salvages, one per download. A warning **stays while the download is still in
  the session** (or has a pending stall or move): when the download leaves the
  session with nothing pending it **disappears on its own**, and a cycle that
  ended well (file in the library, superseded copy) **never shows** — no
  24-hour expiry. **Clean** marks the ones still shown as read (a new warning
  of the same download shows again). *History* opens the full
  story of that download. It refreshes every minute; when all is well it says
  "Nothing to check".
- **Stat cards** — library (series/movies), free space, archive magnets
  and engine.
- **Network and active downloads** — CPU/RAM plus a live network sparkline.
- **Upcoming releases**, **last downloads**, **recent activity** and
  **latest finds from sources**.

### Manual search from the Dashboard

Manual search is useful for understanding what Gextto sees before changing a
configuration or starting a download:

1. enter a title or technical query;
2. when the aMule integration is on (see [eD2k and Kad with aMule](#ed2k-and-kad-with-amule)),
   tick **eD2k** next to the search button to search eD2k/Kad as well, on the
   network chosen in Configuration;
3. wait for sources to finish, or for slow-source timeouts to appear;
4. compare title, source, quality, size and seed/peer counts;
5. use **Filter loaded results** to narrow the list locally;
6. open **Why not this one?** on interesting results;
7. click **Queue** only after checking the reason and archive comparison.

The local filter works on results already received, including title, source and
technical quality fields. Typing in it does not query indexers again or change
the original search query.

Manual search may show releases that are not eligible so they can be inspected.
Visibility does not mean that the automatic cycle would download the release.

### How to read a cycle

A normal cycle goes through search, filters, archive comparison, selection and
queueing. The number of releases found is not the number of downloads: a release
may be excluded because it is unmonitored, too old, blocked, already present or
inferior to the archived file.

**Copies of the same release.** The same torrent often comes from several
sources: the feed and the search of the same indexer, Jackett and Prowlarr,
indexarr. Gextto merges them into one release:
- when they have the same infohash, even when a source declares it next to a
  `.torrent` link or writes it in base32;
- when a source gives no infohash but name and size match, within the rounding
  indexers use for sizes.

Two torrents with different infohashes always stay apart, even with the same
name. Names that differ in group, resolution, language, PROPER or episode also
stay apart: when in doubt, Gextto does not merge.

Of a merged release it uses the best copy: a live swarm, more seeders, a known
infohash. It keeps the other copies as spares: when the chosen `.torrent` link
fails or is in quarantine, it tries the same release from another source right
away, instead of leaving the episode to the next cycle.

## 3. Downloads

- **Search a release**: the panel at the top of Downloads searches the archive,
  indexers, web engines and — when **eD2k** is ticked and aMule is on —
  eD2k/Kad, without leaving the page. Results open under the field and an add
  keeps you in Downloads.
- The **Add torrent** form opens from the button of the same name in the top bar,
  present on **every page** (from another page it takes you to Downloads and
  opens the form): while closed it takes no room. It accepts a magnet/.torrent
  URL or a `.torrent` file; optionally set a save path, “Download now” and “Do
  not rename”.
- **Downloads in session**: download/upload rate, torrent and peer counts; the
  list refreshes automatically every 5 seconds.
- Comic HTTP downloads show status, progress, downloaded bytes and speed in the
  same list as torrents.
- **aMule's eD2k downloads** appear in the same list with the coloured **eD2k**
  badge: state, progress, speed, **uploaded bytes**, **estimated time** and
  **ratio** of uploaded to downloaded, plus sources (transferring / total) and
  **Pause**, **Resume** and **🗑️**. On a download in progress 🗑️ cancels it and deletes
  the partial data; on a completed file it only takes it off aMule's list (the
  file stays in the incoming folder). **ⓘ** opens the details: state, sources,
  last data received, hash, the eD2k link to copy and the **priority in aMule**
  (low, normal, high, automatic). The counter at the top also shows **eD2k: N**
  while the integration is on; eD2k downloads also count on the dashboard and in
  the number next to **Downloads**.
- **View row**: a box to select everything, name filter, tag filter, **Sort by**
  with its direction (↑/↓), **⟳** to refresh at once and the **Tools** menu:
  **Clean completed**, **Orphaned files…**, **Refresh every 5 s** and a link to
  **Removal after seeding** (the "Remove completed items after seeding" option is
  set in Configuration → Seeding; the ✓ mark says whether it is on).
- **Selection bar**: it appears only once you tick at least one torrent and it
  survives the automatic refresh. It shows how many you picked, **✕** to clear
  them, **Pause**, **Resume**, **Check again**, the **Tag** menu (one click on a
  tag assigns it; at the bottom you create a new tag or remove the current one)
  and **Remove from session**.
- **Bandwidth** (top right of the panel). The speed follows a **rule** chosen in
  Configuration → Speed and network (Auto, fixed limits or hourly schedule); here
  you make an **exception** for now, which beats the rule until it ends or you
  remove it. The button shows what applies: ⚡ an exception (with the minutes
  left), ⏰ a schedule window, otherwise the rule. With eD2k on, **aMule follows
  the same limits** (rule, exception, schedule), applied within a minute of each
  change. The menu:
  - **Now**: what applies and where it comes from; with an exception on it says
    when it ends and which rule comes back afterwards. **Remove the exception**
    goes back to the rule at once.
  - **Auto**: Gextto measures the line and adapts, no cap. It is greyed out with
    “It is already the rule” when the rule is already Auto at that moment.
  - **Limit download** / **Limit upload**: pick a value (512 KB/s to 10 MB/s for
    download, 64 KB/s to 1 MB/s for upload, or **No limit**): the exception starts
    at once and the other direction stays as it was.
  - **Duration** (while an exception is on, Auto included): 1 h, 3 h, 6 h, 12 h
    or **Until I remove it**. A new exception lasts until you remove it.
  - **Custom values…**: DL and UL in KiB/s and the duration in minutes (0 = until
    you remove it), then **Apply**.
  - **Change the rule** opens the choice in Configuration.
- **Pin**: a torrent forced outside the queue shows the **📌 pinned** badge; it is
  unpinned with **Unlock pin** in its details (where **Pin** is too).
- **Table columns**: Name, Status, Progress, ↓, ↑, ETA, Peers, Ratio — sorted
  from **Sort by**.
- **Row actions**: pause/resume, recheck, remove.
- **Details**: open by clicking the **torrent name** (or anywhere on the row; on
  the keyboard the name is the button that opens them) as a frame **above the
  download history**, not a centered window: the session and the history stay
  visible while you set the values.
- **Details** tabs: General, Tracker, Content, Peers, Pieces, Limits, Storage;
  includes copy magnet, per-torrent limits/seed-days, reannounce, pin, restart,
  mark-failed, move storage. Hovering a value in the **General** tab, an action
  button or a table header shows what the value means or what the button does. **General** also offers **Export .torrent**,
  **Super seeding** (with gx-torrent it only applies once the torrent is complete
  and it deliberately lowers the seed's upload) and web-seed add/remove; with
  gx-torrent the **General** tab also lists the **BEP 38 hints**
  (`collections`/`similar`) with the local torrents that share them, when the
  torrent carries any; **Tracker** lets you edit the whole
  list (`tier|url` per line); **Content** sets the **per-file priority**
  (Skip/Normal/High/Maximum: high-priority files are fetched first). With
  gx-torrent changing **only the level** is immediate, without stopping the
  torrent; excluding or re-including a file restarts it for a moment to move its
  data. **Pieces** shows the piece map (green have, yellow
  downloading, grey missing, dimmed skipped) on engines that expose it
  (gx-torrent), and it stays readable while the torrent is paused or parked:
  it is drawn from the verified pieces. With gx-torrent every file has a **▶** button that opens the
  file's **HTTP streaming** (with Range): the pieces of the part being read are
  downloaded first, so playback can start before the download finishes. The same
  streaming engine backs the **DLNA/UPnP media server** (option *gx-torrent —
  DLNA/UPnP server*): TVs and consoles on the local network see the **completed**
  media with no path to configure, and the **library folders** too (the
  archive on the NAS: series and movies, browsable by folder; option
  *gx-torrent — DLNA: library folders*, empty = automatic). Videos and
  folders show the **posters and thumbnails** already next to the files, in the
  Jellyfin, Kodi and Emby layout (`episode-thumb.jpg`, `movie-poster.jpg`,
  `folder.jpg`, `poster.jpg`, the season posters `season01-poster.jpg`): they are
  scaled on the fly and nothing is written to the library. `.srt`/`.vtt` subtitles named like a video
  are attached to it. Players can also **search by title** (ContentDirectory
  Search), and **Xbox 360 and Windows Media Player** accept the server through
  the `X_MS_MediaReceiverRegistrar` service. On the gx-torrent settings page the
  **Download folder** button appends the configured download folder to the list.
  It is off by default and has no authentication, so it
  answers **only** devices on the local network (requests from public addresses
  are refused) and serves only the listed media. The gx-torrent settings page
  shows the address it runs on.
- When you use **Move storage**, the log records the request, destination and
  command acceptance; the final outcome is written when the engine completes or
  rejects the move. With **Check**, the log distinguishes command start from
  completion and includes state and verified bytes.
- **Download history** lists finished downloads (native completions and
  migrated ones). Columns: name (with a **NAS** badge when the file has been
  archived), type/season/episode, **NAS tag** (folder rule), score, status
  (*Completed*), the **library/NAS path** and the completion time.

### Stalled torrents

When a torrent stops increasing its completed-byte count for the configured
period, it enters **stalled** state. Gextto pauses it in the engine too, so it no
longer occupies an active slot. It remains in the session and is resumed and
reannounced on the next retry. The values are under *Configuration → Seeding and completion → Stalled
torrents*:

- **Consider stalled after** — 60 minutes by default;
- **Stalled retry** — 60 minutes by default;
- **Stalled removal** — 20160 minutes (14 days) by default, `0` = never.

Peers without byte progress do not reset the timer. In the log, look for lines
with `is stuck at`, which also give the reason, then `is still stuck` at each
retry and `is downloading again` when it recovers; `Gave up on` appears only
after the final limit.

When it abandons a **season pack**, Gextto first brings into the library the
episodes whose files are already complete (every piece verified), as it would
with a finished pack, and only then removes the torrent and its partial data:
the log shows `🛟 … already complete, added to the library before giving up`
with the list of episodes. The next search only looks for the ones still
missing. The salvage needs a library folder (the series one or the general
one); it works with every engine, even for a parked torrent.

### States and recommended actions

| State | Meaning | Recommended action |
|---|---|---|
| Queued | registered but not started yet | wait for the cycle/session |
| Downloading | transfer in progress | check speed and peers |
| Stalled | no real byte progress | wait for the automatic retry |
| Seeding | download complete, seeding is active | leave it or remove it according to policy |
| Error | the torrent reported an error | read the reason before removing it |
| Archived/NAS | the final file was copied to the library | check the path; do not delete an active source manually |

**Remove** acts on the torrent session and may ask whether to delete files.
**Clean completed** is more selective: it removes torrents that reached their
seeding limits. A torrent set to **seed forever** (ratio 0 or days 0) is never
removed, not even when paused or already archived. Removing a torrent from the
session does not necessarily remove the file from the library.

### Seeding limits: per-source rules and idle seeds

Gextto decides when a torrent stops seeding, in the same way for gx-torrent and
for qBittorrent. For each limit (ratio, time, inactivity) the first level that
sets it wins:

1. the **torrent's** own limit (detail view: ratio and seed days);
2. the first **seed rule** that matches the torrent (*Configuration → Seeding
   and completion → Seed rules*);
3. the **global** limit on the same tab.

**Seed forever.** 0 means infinite, for the ratio and for the time (days and
minutes at 0). **Either one being infinite is enough** for the seed never to
end: the torrent keeps seeding, is not stopped for inactivity either and, in
*Move* mode, is not moved into the library (a copy, or a hardlink, goes in). A
time limit does not stop a torrent with an infinite ratio, and vice versa. So to
end the seed both the ratio **and** the time must be greater than 0. The same
holds for eD2k downloads: with an unlimited seed the file is copied and aMule
keeps sharing it.

A rule names a **source** (the indexer the release came from, as shown in the
download list: part of the name is enough, case-insensitive, so `mircrew` also
matches `mircrew indexerr`) and/or a **category** (TV series, Movies, Comics,
Other). It is the equivalent of qBittorrent 5.2's per-category limits, plus the
source: a private tracker's rules follow the tracker, not the kind of content.
In a rule an empty field uses the global limit and 0 means infinite (the
torrent seeds forever even where the global limit would stop it). A torrent that
came from Jackett's aggregate search (source `jackett` with no indexer name)
does not carry the tracker's name: the category or the global limit applies.

**Stop an idle seed after** stops a seeding torrent that has sent no data for
that many minutes, like qBittorrent's inactive seeding limit. Only time spent
actively seeding counts: a queued or paused torrent is not stopped for being
idle, and the count restarts when Gextto restarts (a restart can only make a
seed longer, never shorter). The end of the seed is followed by what follows the
other limits: archiving into the library and, when enabled, removal. A torrent
set to seed forever is never stopped for inactivity.

### Completion of files and folders

The outcome depends on the torrent shape and on whether a **Library/NAS**
destination is configured:

| Completed content | NAS configured | NAS not configured |
|---|---|---|
| Single episode as a file | the video is archived and renamed using the series rules | it remains in the download path according to the single-file behavior |
| Single episode inside a folder | the video is copied to the NAS and renamed; the source folder stays intact while seeding and is then moved to **Trash** | the folder stays intact in the Download directory, even after seeding ends |
| Season pack inside a folder | episode videos are searched recursively, copied to the series NAS path and renamed; the source pack remains for seeding and is then moved to **Trash** | the pack stays intact in the Download directory, even after seeding ends |

For season packs, the archive receives video files with a recognisable episode
identity, such as `S01E02` or `1x02`. Subtitles, NFO files, artwork and other
sidecars are not copied to the archive: they remain in the source folder until
that folder is moved to Trash. The source folder is never moved or deleted while
the torrent is still seeding.

#### Hardlinks instead of copies

When a file must keep seeding while it enters the library (episodes inside a
folder, season packs, episodes in copy mode), Gextto first tries a **hardlink**:
a second name, in the series folder, for the data already downloaded. The file
appears in the library at once and does not use the space a second time.

- Both names are equal: deleting the file in the download folder by mistake
  does **not** touch the library file (only seeding stops), and vice versa. The
  data is gone only when both names are deleted.
- It works only when downloads and library are on the **same filesystem** (same
  disk or same NAS share). When it is not possible (different disks, RAM disk, a
  share without hardlinks) Gextto copies as before and logs the reason once.
- The same NAS is not enough when the two folders are **separate mounts** (for
  example `192.168.1.119:/volume1/Trasferimento` and
  `192.168.1.119:/volume1/SerieTVArchivio`): the kernel refuses a hardlink
  across mount points even when the data is on the same volume. Mount a common
  parent (for example `192.168.1.119:/volume1`) or keep the library inside the
  download share; the log now says so explicitly.
- A program that edits the video **in place** (rewrites bytes in the same file,
  like some taggers or `mkvpropedit`) would also change the seeding file. Gextto
  never does that; if you run such tools on the library, turn the option off.

Turn it off under *Configuration → Seeding and completion → Hardlink instead of
copy while seeding*.

Trash must be configured under *Configuration → Archive and space*. Without a NAS
destination, Gextto does not treat the Download directory as an archive and does
not move the folder to Trash automatically. The automatic move of the source
folder to Trash happens only with a NAS destination, a valid **Trash**
destination and automatic removal of completed torrents enabled; when Trash is
not configured the source files are never deleted automatically (only the entry
is removed from the torrent session).

## 4. Series

Add a series via TMDB search or manually (title, quality, languages, seasons,
aliases, exclusions, NAS path, subtitles, timeframe). The NAS path is pre-filled
with *library folder + title*: keep “Create folder” on to create it when adding
(clear the check to edit the path by hand). “Subfolders per season” stores
episodes in *Season 01*, *Season 02*, … inside the series folder.

- The list shows Name, Seasons, Quality, Language, Episodes (downloaded/total),
  Completeness, Last download; filter it with the search box.
- The list can be read in two ways, with the **List / Posters** switch above the
  table: *Posters* shows one card per title with its cover and the essential data
  (quality, language, episodes, status), and the title or the cover opens the
  detail. The choice is remembered and also applies to the Movies page, so the
  list opens the way you prefer.
- Bulk actions: set language, delete selected.
- **Series detail**: poster/plot/cast, badges (year, network, status,
  completeness), cast links to TVDB/TMDB and, if configured, a **“disabled
  seasons”** badge. The archive path is shown in the header and the edit form
  includes **Browse** for choosing a server-side folder.
- Actions: search missing, scan archive, refresh from TMDB (or TVDB), rename preview /
  execute, edit.
  The rename preview lists the *Old → New* names and has a **Force rename**
  button: it reprocesses files that already pass the quick check, recomputing
  the exact target name from the template and the TMDB/TVDB metadata, useful
  when a file looks correct but does not match the configured template.
- **Episodes**: per-season accordion; per episode you can search, copy magnet,
  ignore/reactivate, force, re-download or delete; the manual search marks
  results already present in the feed.

### Adding and configuring a series

For a new series, TMDB search is the safest method:

1. open **Explore**, search for the title and select the correct result;
2. check title, year, network and poster;
3. choose the **Quality profile**: it decides the new title's quality, minimum
   audio, language, subtitles, exclusions and “Allow upgrades” (the form has no
   other fields for these values, so they cannot contradict each other); add
   any **Extra exclusions** for this title only, which are added to the
   profile's, and the monitored seasons;
4. set a per-series archive path only if you do not want the global path;
5. save in dry-run and inspect the detail page.

The most important fields are:

| Field | Effect |
|---|---|
| Quality profile | when adding and on the title page: gives the title the quality, minimum audio, language, subtitles and “Allow upgrades” of the profile and adds its exclusions to the title's own (see *Quality profiles*). On the page the profile matching the title's values is preselected, exclusions included; when none matches, **Current values** appears, and saving with it changes nothing. A profile is a template: changing it later does not touch titles already created |
| Quality | restricts acceptable releases and contributes to the score |
| Minimum audio | comes from the quality profile when adding and is changed with *Edit selected* (it is not on the title page); a preset that already includes the channels: `Lossless 5.1+`, `Lossless 7.1`, `DTS / DD+ 5.1+`, `DTS / DD+ 7.1`, `AC3 / 5.1`, `AAC / Opus 5.1+`; **Any** imposes no limit and is the default, including for existing series |
| Language | requires the configured language when it is recognisable in the title |
| Subtitles | controls preference without accepting hardcoded subtitles |
| Seasons | decides which seasons are monitored; ranges are supported |
| TMDB ID / TVDB ID | the series ids in the two services. **TMDB is the default source** of metadata (episode titles, overviews, posters) when the TMDB key is set; TVDB is used when only that key is configured. They are independent fields: saving one does not clear the other, and a wrong id is fixed here |
| Aliases | links alternate names to the same series |
| Exclusions | words in a title that must block the release |
| Allow upgrades | **Yes** (default): a better release replaces the archived file; **No**: the archived files of this series stay. It comes from the quality profile when adding and is changed with *Edit selected*: it is not on the title page, so there is no second place to set it |

Leave future seasons enabled if you want calendar and missing searches to keep
working. Use *Ignore* only for episodes that should no longer be searched;
reactivating them makes them candidates again in later cycles.

### Missing episodes and packs

Every aired episode that is not in the library is in the **Missing** tab of
**Series** (next to *Monitored series*; the number says how many). They are
grouped by series, each one collapsible, with a line per season and the
episodes as buttons (`S04E03`):

- **a click on an episode** searches it and opens the results, first from the
  local archive and then online, each with **Download**;
- **Search missing** searches every missing episode of that series, in the
  background too: the tab shows how far it is and you can cancel it; when it
  ends it lists what it found right there, each with its episode and
  **Download**. It downloads nothing by itself. The same button on the series
  page starts the same background search and shows its progress and results
  there;
- **Filter by series** shows only the series containing the typed text.

Episodes that have not aired yet are not missing: a series still in production
that has never broadcast does not appear, and an episode with a known future air
date waits for its day.

When an episode has no copy within the quality profile, the list shows the only
copy in the requested language with the **🆘 last resort** badge: it is the one
Gextto would take by itself, and the upgrade replaces it as soon as a copy
within the profile appears. This also holds for a copy that only the local
archive has.

The old address of the *Missing* page leads to this tab.

From **Search missing** or an episode detail:

1. check that the season is monitored;
2. search for the single episode or the series domain;
3. compare individual releases and season packs;
4. if an episode exists on disk but not in the database, scan the archive;
5. use **Force** only for an intentional manual action.
6. if torrents and archive find nothing and **Use eD2k when torrents find
   nothing** is on, the search asks eD2k/Kad too: the results show the
   **eD2k/Kad** source and the **Download** button hands them to aMule.

With the eD2k fallback on, the search can take longer: aMule collects results
for the time set in **eD2k search duration**, and a series search runs one query
per season.

Gextto normally avoids downloading older episodes when later episodes already
exist, unless this is a recognised gap or a genuine upgrade. A season pack can
fill several episodes, but archive comparison still happens episode by episode.
When the same cycle offers a single episode and a pack containing it with the
same score, Gextto picks the release covering more episodes. At an equal score a
REMUX is still preferred and, at a further tie, the release with **known
seeders** wins (a healthy swarm is less likely to stall).

While a pack downloads, Gextto reads the file list as soon as it arrives and
compares it with the library episode by episode. If the pack adds nothing it
discards it and, if the approval had already replaced an episode, it restores
the previous version; if only some files are useful it can download just those.
The discard log shows the comparison (old and new quality and score) and the
episodes kept or skipped. The two options are in
*Configuration → Seeding and completion → Pack inspection*; inspection is off by
default.

An episode that already has a torrent downloading is not missing: it is not
searched again and it does not appear among the series' missing episodes, even
when the torrent was added by hand, from a watched folder or from a phone. The
same applies to a whole season while its season pack downloads. If the download
fails the episode is missing again.

Exception: if a download stays **stuck at 0 B/s or still waiting for metadata**
(its file list never arrived) beyond the configured window (*Configuration →
Quality and upgrades → Stuck downloads*), the search may start an alternative
for the same episode within the allowed points, **without removing** the stuck
torrent: if it recovers, archive deduplication will still keep the best copy.
This also covers season packs: a stuck pack makes every episode it covers
eligible, and the alternative may be a single episode or another pack.

## 5. Movies

- Tabs **Monitored / Downloaded**; sortable columns (name, year, quality,
  language). As for the series, the **List / Posters** switch
  reads the table as cards with the cover (the choice is shared with the Series
  page).
- The editor supports quality, **Language**, **Subtitles** with
  **Subtitles required** (Yes: a release without those subtitles is rejected; No:
  they are only preferred) and exclusions; **Allow upgrades** and minimum audio
  are changed with *Edit selected*. Each value has one
  field only: the old “Required languages” were folded into **Language** (which
  takes `chi`, `ita,eng`… beyond the list entries too) without changing any
  decision.
- **Movie detail**: poster, plot, cast with TMDB links, edit, re-download,
  **Search now**, a “best matches” table from the sources and an **archive
  matches** table where each row offers **Why not this one?**.

### Adding and selecting a movie

1. find the movie in **Explore** and check year and original title;
2. add it to the library;
3. choose the **Quality profile** and add any exclusions for this movie only;
4. use **Search now** to inspect results without waiting for a cycle;
5. open **Why not this one?** before queueing a borderline release.

Movies are identified by title and year when available. Avoid creating duplicates
with different spellings: correct the monitored movie metadata instead of adding
it again.

Several languages are written as comma-separated codes, for example `ita,eng`.

### Importing a file that already exists

**Import file** (a **Maintenance** tab) is for a video already on disk that Gextto did
not download: an episode fetched by hand, an old season, a movie copied from
another disk. Type the path, or press **Browse**: it lists the server's
folders (starting from the download folder) with the videos in each one; a
click on a video picks it, **Select** takes the whole folder. Then press
**Analyze**: Gextto reads the
names, proposes the configured series or movie, the season and the episode and
shows the destination folder. You can correct the series, season, episode (or
movie and year) before confirming. **Import now** renames the file with the
configured rules, moves it to the library, records the episode (or movie) and
refreshes Jellyfin/Plex when configured. If the destination already holds a file
with the same name the import stops: tick **Replace the existing file** to move
the old copy to the trash and put the new one in place. Multi-episode files are
recorded for every episode they contain.

### Editing several titles at once

In the **Series** and **Movies** tables (*List* view) every row has a
checkbox: **Edit selected** sets the same values on every ticked row in one go,
for example moving several movies to `2160p` or pausing several series.

1. Tick the rows in the table. The checkbox in the header ticks every visible
   row: combine it with *Filter the list…* to take, for example, every title in
   `720p`.
2. Press **Edit selected**, next to *Refresh list*: the fields open below.
   Tick only the fields to change
   (quality, minimum audio, language, subtitles, exclusions), typing the new
   value. A ticked field left empty is cleared; a field that is not ticked stays
   as it is in each title. *Allow upgrades* and *State* change only if you pick
   something other than "Leave as is".
3. *Start from a profile* copies a profile's quality, minimum audio, language,
   subtitles, exclusions and upgrades into the fields and ticks them, except the
   **exclusions**: they belong to each title (on the title page the profile's are
   added to them), so they stay unticked and are ticked only if you want to
   replace them with the profile's.
4. Press **Apply to the selected titles**: the page reloads with the new values
   in the columns. The other values of each title (seasons, folder, aliases…)
   are not touched.

A movie's hard requirements (**Subtitles required**, and the required languages
where present) follow the value they qualify: changing the language or the
subtitles with **Edit selected** updates the requirement too, so the title does
not keep asking for the old one.

The *Posters* view has no checkboxes: switch to *List*. **Refresh list**
changes nothing: it reloads the table with the filter you typed.

## 6. Explore, Archive, Comics

- **Explore** — TMDB trending/today/popular/top-rated/now playing/upcoming, TMDB
  search, generic release search, add-to-library. The lists exist only on TMDB:
  with only the TVDB key the title search goes to TVDB. Cards are shown in a five-column
  wall; a title already in the library carries a **Già in lista** badge and its
  add button is disabled, so you never create a duplicate by mistake. Under the
  release search there is a field to **paste one or more `ed2k://` links** (one
  per line or separated by spaces): Gextto checks them, drops duplicates and
  shows each as a result with name, size and hash. **Add all** hands the whole
  block to aMule, or use the per-row **Add** button. When **eD2k** is ticked,
  eD2k results appear in the same table with the **eD2k** badge and the source
  count. While aMule is off or not answering the row says **Search only**.
- **Archive** — full-text search of past releases with pagination, batch queue,
  copy magnet, delete; **Series/Movies from feed** tabs. The local archive search
  is **instant**: it starts on its own as you type (from three characters on)
  and uses the FTS5 index, so it does not burden the database. For shorter terms
  (e.g. `IT`) press **Enter** or **Search**: the threshold only gates the
  automatic search, not the explicit one. A `-word` filter
  excludes titles containing it. The **Also search the web** checkbox extends the
  search to indexers and online engines: it is much slower and therefore only
  runs when you press **Search**, not on every keystroke. With aMule on there is
  also **Also search eD2k**: it too runs only from **Search**, and the eD2k
  results appear in a block below the archive, each with **Download**. Long
  names stay on one line cut with an ellipsis (the full text is in the tooltip)
  and the **Actions** column with **Download** always stays visible, on phones
  too. The
  **Source** column
  shows only the provider/domain (the full URL is in the tooltip), and every row
  offers **Why not this one?** like the search table. The title wraps over
  multiple lines and the row actions are compact icons (⬇️ queue, 🔍 why not
  this one, 🧲 copy magnet, 🗑️ delete) so the release name gets the room. The
  same torrent (same infohash) appears **once**, with its newest listing: copies
  of the same content under another magnet or title no longer fill the results.
- **Seen from feed** — every release seen in the sources, grouped by title
  (movies/series) with count, best resolution and score; expand a group to
  queue a single release. Populated by each cycle, even for unmonitored titles.
- **Why not this one?** — search results include a read-only explanation for a
  release. It shows the decision, score and score components, passed or blocking
  rules, source filters, quality/language/subtitle checks, blocklist, active
  downloads and comparison with the archived file. It does not queue the
  release, create placeholders or modify the database. The result covers the
  candidate checks; cycle selection can also depend on gap filling, smart
  episode, delay, free space and other candidates.
- **Comics** — in **Add comic**, enter a title and click **Find**; choose the
  exact GetComics result and confirm. Gextto saves the selected post, tag,
  cover and metadata, then uses that post for downloading instead of replacing
  it with a similarly named issue. The monitored list, post link extraction,
  weekly-pack settings and resend/delete/force history are also available. A
  weekly pack is queued only once: before downloading it Gextto looks in its
  own history for an earlier download (and checks whether it is still in the
  download list), so a pack you already downloaded is not fetched again even
  when you deleted its folder after reading it. The log line also says why it
  downloads it: a new pack of the week, or the download link that became
  available after the pack was found without one, or that it was already
  downloaded and is left alone. Direct
  HTTP downloads appear in **Downloads in session** with status, bytes, progress,
  speed and pause/resume actions. A comic completed by its own pipeline shows as
  **Completed · archived** at 100% even when the pack's torrent is paused and
  re-checked at 0%.
  In **CBZ** files downloaded directly (HTTP or Mega) Gextto adds a
  `ComicInfo.xml` with series, issue number and year taken from the title (e.g.
  “Poison Ivy #41 (2025)”): Komga, Kavita and tablet readers use it to group and
  sort issues. A file that already has its own `ComicInfo.xml` is left alone;
  comics downloaded by torrent stay identical because they are seeding, and CBR
  (RAR) files cannot be modified.

- **eD2k** — the aMule page: see the next section.

### eD2k and Kad with aMule

Gextto uses **aMule** for the eD2k and Kad networks: `amuled` does the network
work (servers, Kad, sources, queues, credits, transfers) and Gextto drives it
through the **External Connections (EC)** protocol, the same one `amulecmd` and
`amuleweb` use. Everything is optional and off by default; with aMule off,
torrents do not change. If you are upgrading from a version that ran our
`gx-ed2k` daemon, the update removes that binary and its settings: eD2k and Kad
now go through aMule.

**Setting it up.** Install aMule (`amule-daemon` on Debian/Ubuntu) and enable
external connections in `amule.conf`:

```ini
[ExternalConnect]
AcceptExternalConnections=1
ECAddress=127.0.0.1
ECPort=4712
ECPassword=<md5 of the password>
```

`ECPassword` is the MD5 of the password (`printf '%s' 'secret' | md5sum`). With
aMule in **Docker** (`ngosang/amule`) mount the download folder in the container
**at the same path** as on the host and point `INCOMING_DIR` and `TEMP_DIR`
there, so Gextto finds the completed files; use a dedicated incoming folder,
because aMule shares everything it holds. To open the ports on the router with
**UPnP** the container must use the host network (`network_mode: host`) with
`UPnPEnabled=1` in `amule.conf`. Start
`amuled`, then in **Configuration → eD2k and Kad** switch on **eD2k
and Kad through aMule** and enter address, port and the password **in clear**.
Gextto must see aMule's incoming folder at the same path (true when both run on
the same machine): completed files are imported from there. Open aMule's TCP
and UDP ports on the router for a high ID and a Kad that is not firewalled,
which means more sources.

**The eD2k page** (menu **Discovery → eD2k**):

- **aMule**: version, the eD2k server you are connected to with **high/low
  ID**, Kad state (connected, behind a firewall, stopped), speeds and limits,
  network users and files, upload queue, bytes exchanged in this session and
  over all time. **Connect**, **Disconnect**,
  **Start/Stop Kad** and **Leave the server** drive aMule; **Refresh** reads
  the state again. When aMule does not answer, the page says so and explains
  what to check.
- **eD2k servers**: aMule's list with users, files and ping; **Connect** on a
  row connects aMule to that server. A new `server.met`, or a `nodes.dat` for
  Kad, can be downloaded from an address here too.
- **Search eD2k and Kad**: pick the network (Kad, Kad + eD2k servers, every
  eD2k server, only the connected server) and search; aMule collects results for a few seconds. Each
  result shows name, size and sources (total / complete) and has **Download**;
  **already known** marks a file aMule already has. aMule has **one** result
  list, shared with its own interface and with every other connected tool: if
  another search replaces it while Gextto is searching, Gextto runs the search
  again and, if the interference continues, asks to try again in a few seconds
  instead of showing unrelated files. Above the results there is a filter
  (**Words in the name**, **Minimum sources**, **Minimum size**, **Hide
  already known**, **Sort the results**): it works on the results aMule already
  returned, so it does not start a new search.
- **Paste eD2k links**: one or more `ed2k://` links, even mixed with text;
  **Download with aMule** adds them all.
- **aMule log**: **Load the log** shows the last lines of amuled's log.
- **aMule folders**: when the incoming or the temporary folder is missing (a
  network disk unmounted, a folder removed) the page says so and **Create the
  folders** makes them; without them aMule fills its log with *Failed to get
  file system statistics* and never imports completed files.
- **Shared files**: what aMule shares, with the bytes uploaded in this session
  and over all time, requests and accepted ones (session and since the start),
  queued clients, the file's upload speed, the last upload, priority and
  the `ed2k://` link to copy; below it the clients you are uploading to, with
  speed and queue position. The counters are aMule's: the session column starts
  from zero every time `amuled` restarts, the total one does not. **Reload
  shared files** asks aMule to rescan the
  folders (useful after adding or removing files by hand); the shared folders
  are listed read-only.

Added downloads appear in **Downloads**. A link to a file that is not a video
(a pdf, a program, an archive) is only downloaded by aMule and stays in the
incoming folder, like a torrent added by hand. When aMule completes a video,
Gextto waits until the file is actually in the incoming folder, with the expected
size and stable, then treats it like a completed torrent: it renames it, imports it into the library,
checks it with MediaInfo and sends the notification. The same quality check as a
torrent runs before the import: if the library already holds an equal or better
file, the download is discarded to the trash and never imported. eD2k episodes and movies follow the
**Move episodes/packs into the archive** option: when it is on they are moved
into the library and removed from the incoming folder (aMule stops sharing
them); when it is off they enter by **hardlink or copy** and aMule keeps
sharing them (a hardlink takes no extra space when both folders are on the same
disk). For eD2k the move happens at completion, because seeding is managed by
aMule and Gextto has no end-of-seed moment to wait for. An eD2k download has no
seed limits of its own: the seed rules and the global limits apply. With an
unlimited seed (ratio or time at 0) the file is copied and stays shared even
with *Move* on, and its row shows the "seed ∞" badge.

For the complete guide — installing aMule, Docker, troubleshooting and the
protocol details — see [`docs/amule.md`](amule.md).

## 7. Configuration

The page is split into four areas: **What to look for** (General, Sources,
eD2k and Kad, Quality and upgrades), **How to download** (Torrent engine, Speed and network,
Queues and performance, Seeding and completion), **Where to store** (Library and
renaming, Archive and space) and **System** (Automatic maintenance,
Notifications, Access and services, Diagnostics and translations). Unsaved changes are
highlighted with a “Save all” bar. The complete per-tab list of every entry is in
[Appendix A](#appendix-a-reference--configuration).

- **Sources** — RSS feed list, indexers (Jackett, Prowlarr, indexarr or a direct
  Torznab) with a *Verify* button, FlareSolverr URL + test, web engines, content
  filters, blacklist. For Jackett, use its base URL, for example
  `http://host:9117`, together with the API key. Gextto builds the Torznab
  endpoint `/api/v2.0/indexers/all/results/torznab/api`. The health check uses
  `t=caps` with the same key, so it checks the API rather than only Jackett's
  home page. For indexarr the base URL is normally `http://host:8788`: the
  aggregate search uses its native `/api` endpoint, while the indexer list and
  the individual lanes use the Jackett-compatible paths. Results may identify
  the source as `jackett:TrackerName` or `indexarr:TrackerName`.
- **eD2k and Kad** — the connection to aMule (address, port and external
  connections password), the search network (Kad, servers or both), the
  automatic fallback when torrents find nothing and giving up on stalled
  downloads. The tab's **aMule** panel has the **Preallocate the whole file in
  aMule** switch and the values
  are applied to aMule at once, without a separate save. aMule's state and
  commands are on the eD2k page.
- **Speed, network and performance** — connections/performance,
  protocols/trackers, security, RAM disk and ports, speed limits and scheduler;
  apply and optimise.
  If you move a torrent's storage to a folder that **already holds the data**,
  it is not rejected: Gextto associates the torrent with the existing files and
  re-checks them (seeds) instead of re-downloading. **Preallocate disk space**
  (off by default) reserves the full size of every new
  torrent up front: it avoids fragmentation on NAS/HDD and surfaces "no space"
  immediately; it can be turned off per torrent in the add form. **Copy .torrent
  files to** names a folder where Gextto copies the `.torrent` of every started
  torrent (once metadata is available), handy to reuse it with another client or
  as a backup. The RAM disk section lists available `tmpfs`/`ramfs` mounts and
  lets you
  choose one. If no path is configured, its button creates and configures
  `/dev/shm/gextto`. The contents of `/dev/shm` are temporary and are lost
  when the machine reboots. Choosing a path automatically calculates the
  maximum torrent size, free-space margin and minimum free space; the values
  remain editable. The **Test ports** button also checks local port binding;
  it does not replace router/firewall port-forwarding verification.
  Under *Security, proxy and network* the **VPN killswitch interface** binds
  listening and outgoing traffic to a chosen interface (e.g. `tun0`, `wg0`);
  the list is read from the server, and the change applies after a restart.
- **Scores** — category weights, custom-group management (add, edit or remove a
  group and set its bonus/penalty), and **Test a release**. The upgrade rules
  come first, then examples, the release test and the quality profiles; the
  weights follow in closed cards. The group name must
  match the release's final group tag, for example `TBK`. Audio weights are one
  card per group: Lossless (TrueHD, DTS-HD, FLAC, PCM), DTS/DD+ (including
  DTS-HD HRA), AC3/5.1, AAC/Opus and MP3; channels and Atmos/DTS:X live in a
  same Audio card. Video weights live in one **Video** card (4K, Full HD, HD,
  SD, Video codec, Source and HDR/DV subgroups); **Release fixes** holds
  PROPER, REPACK and REAL, while **Upgrades and replacement** (at the top of
  the page) holds the thresholds and the replacement switch. The audio total stays below the upgrade threshold, so audio contributes
  to the comparison but does not download another release by itself. Audio language is a per-title eligibility requirement, not a fixed
  Italian bonus, so English can be preferred instead. Preferred subtitles add a small optional bonus without
  making a release mandatory. One effective score is used for acquisition,
  searches, upgrades, post-processing, archive records and rescoring; it also
  includes the size bonus, which on its own is never enough for an upgrade.
  Library scores are recomputed automatically after a change of the weights or
  of the scoring rules.
  - **Rename** — rename enable, template editor with tokens and live preview,
  TMDB/TVDB keys, language and upgrade thresholds. One file holding several
  contiguous episodes is named with the range (`S01E01-E02`), so media servers
  list both episodes; a non-contiguous range falls back to the first episode.
  Verification recovers
  the source token from the original release title stored in the database and
  drops empty placeholder blocks (`[]`), so a lost `[WEB-DL]` is restored instead
  of staying `unknown`.
- **Advanced** — free-space guard, trash retention, archive retention, feed
  pages, rename-verify interval, move episodes, debug flags.
- **Acquisition** — download delay for series/movies (with a high-score
  bypass), housekeeping interval, **Watched folders** (Gextto scans the chosen
  directories, waits for two stable observations, and adds copied `.torrent`/
  `.magnet` files; import failures retry with backoff until they succeed, then
  files are removed or renamed `.imported`; the recursive scan goes at most 8
  levels deep), **Sources in backoff**
  with level, deadline, last error and per-source reset, and the automatic
  **MediaInfo backfill** (configurable files-per-run and interval), plus a
  Maintenance button for an immediate scan.
  **Housekeeping is not Archive cleanup**: it keeps the database tidy, but it
  does not delete library files, completed downloads, or releases listed in
   the Archive. *Search-cycle statistics kept* concerns only the counters for
   each cycle—releases scanned, candidates, started downloads, filled gaps, and
   errors: with `200`, cycle 201 removes the oldest cycle's counters, not the
   downloaded release. *Feed-seen entries* removes only historical feed rows
   older than the selected number of days (`0` = no cleanup); *Download history*
   in the **Downloads** section removes only rows for torrents already removed
   (`0` = keep), without affecting the Archive. Each run also
   automatically removes torrents in error, gap logs, and upgrade backups
   older than their configured retention periods (defaults: 7 days for errors
   and 30 days for gap logs and upgrade backups); expired source backoffs are
   also removed. Afterwards only the databases with at least 20% (or 64 MB) of
   free space to reclaim are compacted with `VACUUM`. Housekeeping runs once per
   interval even when Gextto restarts: a restart does not trigger it again. Archive
  retention is separate and is configured under **Configuration → Automatic
  maintenance → Release archive cleanup**: the same housekeeping worker applies
  it on the same cadence, but only when «Automatic archive cleanup» is on. It
  removes release-catalogue rows only; downloaded files, the library, download
  history and active torrents stay untouched.
- **Paths** — library root, trash, download/temp/RAM-disk dirs, per-tag rules.
   A selected RAM-disk path remains configured, but a directory created under
   `/dev/shm` must be recreated after a reboot.
 - **Translations** — advanced panel to export saved Italian or English
   translations as YAML, edit them, and import them again. The format is a
   `key: value` map, for example `"Testa porte": "Test ports"`.
   Import updates or adds keys present in the file and does not delete missing
    keys. It does not change the active language; use the selector at the top.
    The interface translates strings at runtime and falls back to the Italian
    source when a translation is missing.

### Torrent engine (gx-torrent, qBittorrent-nox)

Gextto always keeps its own database, queue, scoring, post-processing, renaming
and archive; only the transfer plane is pluggable, chosen in
*Configuration → Torrent engine*. A torrent is owned by a single engine at a
time, so switching is a controlled migration, not two clients working on the
same data.

If the active engine cannot stay up, Gextto switches to another engine by
itself: first **qBittorrent-nox** (downloaded on demand and started with a
private profile, or the external one already configured), and finally a
**recovery** state with the transfers stopped and the UI still reachable. An
updated `gx-torrent` that will not start is restored to the previous binary
first.

The default engine's core, **`gx-core`** (`internal/gxcore`), and the DHT
(`internal/dht`) are original code written in this repository, so they are
covered by the same EUPL-1.2 licence as Gextto (see the *Licence* page of the
web interface).

In the interface header, next to Gextto's version, the **active engine with its
version** is shown: `gx-core` followed by its version number (for example
`1.1.242`) with gx-torrent, `qBittorrent-nox v…` with
qBittorrent (the brand and version of the engine actually moving the bytes, so
what is transferring the files is visible at a glance).

- **gx-torrent** (default) — a pure-Go BitTorrent daemon, started and supervised
  by Gextto in a separate process; it has no C/C++ dependency. Set the
  **Web API URL**, the **listen address** (default `127.0.0.1:8890`: web page
  and API on this host only; use `0.0.0.0:8890` **and a token** to expose them
  on the LAN), **token** and **proxy**. At that address it also serves an
  **operational web page** (add magnet, pause/resume, verify, reannounce, queue,
  remove, IP filter, a tab with the Gextto log). The daemon **keeps running
  when Gextto restarts** (for example for an update): Gextto adopts it again
  and the transfers are not interrupted; it is restarted only when its program
  or options changed, and never in the middle of a file move. If Gextto stays
  down, the daemon stops by itself after 15 minutes. The **disk cache is
  adaptive**: the daemon recomputes it every few minutes from available memory,
  active downloads/seeds and the storage class (larger on HDD/NFS, small on
  SSD) and applies it in place, like the speed limits, without dropping peers;
  an **IP filter** downloaded from a URL refreshes once a day; at boot it is
  downloaded again only when the cached copy is older than 24 hours. If the daemon cannot stay up (6 abnormal starts
  in 10 minutes) Gextto falls back to qBittorrent-nox by itself. It supports
  **sequential download** and **first/last piece** priority (set when adding a
  torrent, or for every new torrent in *Configuration → Queues and performance →
  Download mode*; they apply to new torrents). A magnet that carries the `so=`
  parameter (BEP 53) downloads only the listed files from the start, `ws=` adds
  web seeds and `dht=` adds a DHT bootstrap node. It supports
  **v2-only** torrents (BEP 52), both from a `.torrent` and from a magnet link;
  it does not support WebTorrent/WebRTC.
- **qBittorrent-nox** — Gextto drives an existing qBittorrent-nox through its Web
  API. Set the URL, user/password, category, tag and poll interval, and the
  `local=remote` **path mappings** whenever the two processes see different
  paths; with no mappings Gextto checks that the required paths exist locally. If
  a path cannot be translated, activation is refused so a move can never target
  the wrong folder. It works with **qBittorrent-nox 4.6 and later, including
  5.3**: it speaks both the 4.x and the 5.x Web API conventions (login, adding
  paused, share limits) and needs no different settings. **Managed mode**
  downloads the latest qBittorrent-nox static
  release, installs it in the application folder (next to `gexttod`), starts and
  stops it with the service and updates it with backup and rollback; a watchdog
  restarts it after an unexpected exit and, after repeated crashes, runs the
  engine fallback chain. If qBittorrent is down at startup, Gextto keeps retrying
  instead of failing. The full steps (installation, Web UI, mappings, managed
  mode, common problems) are in [`docs/qbittorrent-nox.md`](qbittorrent-nox.md).
If the saved configuration selects a backend that is no longer supported,
Gextto falls back to gx-torrent and logs a warning.

The configuration tile shows the active engine, its status and a
reachability/path test, so you can validate a backend before switching. The
**Health → Torrent engine** panel summarises the active engine (queue, speed,
port/router, DHT/uTP/LSD, encryption, proxy, IP filter, cache, incoming
connections, disk I/O with queued jobs and read/write totals, session totals),
all in the interface language.
Whichever engine
is active, the **Downloads** screen and every automation stay the same.

### Performance: RAM and CPU

Gextto does not need a powerful machine: it runs on a mini PC or a NAS and leaves
CPU and RAM to the rest of the system. At rest you do not notice it; during a
transfer the cost is the engine's, and it lives in a separate process and gives the
memory back when the download ends. There is nothing to tune: the cache sizes
itself from free memory and the storage class.

If the machine is small:

- enable the **dynamic queue** and *Do not count slow torrents in active slots*:
  zero-rate torrents do not hold a slot and **stalled** ones are paused and
  retried instead of spinning;
- do not set a cache limit: the automatic value is almost always the right one. A
  manual value is only for a specific reason.

When a torrent is not downloading, **Health** shows the engine state and the
torrent page explains why (dead swarm, no peers, tracker unreachable).

### Quality profiles

In **Configuration → Quality and upgrades**, under *Quality profiles*, name the combinations
of quality, minimum audio, language, subtitles, exclusions and upgrades («Allow
upgrades») you use. The profile **Quality** is picked from a list of the
resolutions Gextto recognises (360p … 2160p, a minimum with «+», the 720p-1080p
range, or Any); a value typed by hand in the past stays selected until you change
it. **Language** and **Subtitles** suggest the usual choices (ita, eng, ita,eng,
multi, any) but also take a value typed by hand, such as `fre` or `ita,eng,fre`.
An empty language means the default language of Gextto (saving writes the
default of that moment). Gextto generates a
profile for every combination of quality, minimum audio, language, subtitles and
“Allow upgrades” already present in the library (a single title's exclusions and
the `any` quality, the same as no limit, do not make profiles of their own) and
exposes them to Jellyseerr through `/api/v3/qualityprofile`; the first profile is
**Default**. The automatic generation happens **until you save the list**: from
then on the profiles are the ones you saved, and new combinations are not added
by themselves. Two profiles cannot share a name: saving a name that is already
used is refused with a message, instead of losing a row.
**Profiles are templates, not rules.** When you use a profile (by choosing it in
the **Quality profile** field of the add form of a series or a movie, from
Jellyseerr or from *Edit selected* in the Series and Movies tables), Gextto
**copies** its values into the title (the exclusions written in the add form are
added to the profile's); from then
on the title's own fields decide, as always. If you later change the profile,
the titles already created do not change: to update many of them at once use
*Edit selected* in the Series and Movies tables. Gextto does not need a Sonarr-style mode where the
profile governs its titles: the Quality field already takes a minimum
(`1080p`), a maximum (`<1080p`) or a range (`720p-1080p`), and language,
subtitles, exclusions and upgrades are set per title. Each profile keeps its number when
you rename it or remove or reorder others: Jellyseerr remembers the number, not the name. If
you remove every profile, Gextto generates them again from the library on the
next read.

### Configuring a Torznab indexer

For Jackett:

1. create or verify at least one indexer inside Jackett;
2. copy the API key from the Jackett page;
3. in Gextto click **+ Jackett** (or add a row to the *Torznab indexers* list
   and choose **Jackett** as the type);
4. enter the base URL, for example `http://jackett:9117`, and the API key;
5. save and click **Verify**;
6. check *Health → API status*.

For indexarr:

1. copy the API key from indexarr;
2. in Gextto add a row to the *Torznab indexers* list and choose **indexarr** as
   the type;
3. enter the base URL, for example `http://indexarr:8788`, and the API key;
4. save and click **Verify**;
5. check *Health → API status*.

An indexarr source with the eD2k protocol returns `ed2k://` links instead of
magnets or `.torrent` files. Gextto recognises them from `<link>` (and from the
`ed2k` attribute), shows them as the **eD2k** network and queues them in aMule
when you download: they never go through the torrent client. The aggregate
search includes them when the title is not already covered by a torrent; add
`indexer_type=ed2k` to the Torznab request to see them all. To give the eD2k
source time to open its topics, Gextto asks indexarr for a wait of up to 45
seconds only on its own searches (`live_wait=45`); other sources and other
programs are unchanged. When it queues an indexarr file in aMule, Gextto opens its
download link once, without making the download wait: indexarr records the
download and the source that provided it in *Log → Download*. The file transferred stays the `ed2k://` link,
and if the download link does not answer the download starts all the same.

With Jackett and Prowlarr Gextto reads the manager's indexer list and
queries them **one at a time** (the single indexer's Torznab endpoint for Jackett,
the `indexerIds` search for Prowlarr): each tracker gets its own
timeout and health, so the slowest one no longer holds the others. If the list
cannot be read it falls back to the aggregate search
`/api/v2.0/indexers/all/results/torznab/api` for Jackett. **indexarr** is always
queried in a single pass instead, one search per title on its `/api`: it queries
its own sources in parallel, merges duplicates and names the site of every
result, so expanding it into one search per site would only multiply the
requests. Do not append the Torznab path to
the base URL: the manager's address is enough (Prowlarr normally on port `9696`,
indexarr on `8788`). A custom full Torznab endpoint stays an explicit endpoint.

An indexer can be reachable but unhealthy: Jackett may return HTTP 200 with a
Torznab error caused by a wrong API key or no enabled indexer. In that case
*Health* shows **API error** and the detail; cycle logs also show source backoff
when applicable.

### Score, filters and “Why not this one?”

The main checks are independent:

1. monitored title;
2. blocklist and duplicates;
3. global filters and source filter;
4. release sanity, including hardcoded subtitles and minimum size;
5. title quality, language, subtitles and exclusions;
6. comparison with archived files and active downloads.

The score helps choose between eligible candidates; it does not make a release
valid when a blocking filter fails. In **Why not this one?**:

- **Passed** means that check succeeded;
- **Blocking** is sufficient reason to reject the candidate;
- **Informational** depends on the complete cycle context;
- archive comparison says whether this is a first download or a possible upgrade.

The explanation is diagnostic, not a reservation: opening it does not create
torrent rows, queue a magnet or change configuration.

### Delays and source backoff

The acquisition delay can hold a release before starting so a better result can
appear. A high score may bypass the delay according to configuration. Source
backoff is different: repeated errors temporarily prevent new requests to the
problematic source.

The delay is set in *Configuration → General → Delay before downloading*. The
backoff state (level, deadline and last error) is in *Health → Provider
status*, which **lists only the configured and enabled indexers** (healthy ones
as "No problem detected"): a removed or switched-off source disappears even if it
had an error. The *Technical detail* column holds the raw error. Use **Reset** per source after fixing the cause; do not use it to hide a wrong API
key, or backoff will start again.

### Configuring a NAS safely

For a NAS library:

1. mount the filesystem before Gextto starts;
2. grant read/write access to the service user;
3. set a conservative minimum-free-space guard;
4. scan the archive after copying existing files;
5. check the actual path in history after the first completion.

If the NAS is not mounted, do not temporarily replace the path with a local root
without understanding the effect: files may be archived in the wrong place. Fix
the mount and let the download wait instead.

An unmounted NAS does not erase history: when an archived file cannot be found,
Gextto marks the episode for recovery only if the file's folder still exists or
the mounted path is not empty. An empty mount point is treated as a missing
volume, and the episode data is left intact.

Release sanity checks are **automatic** and not configurable: hardcoded
subtitles (`HC`) and absurd sizes (a per-resolution floor derived from a real
archive) are refused. Rejections are routine and are logged at `DEBUG` with the
title, resolution, size and reason, so the production `INFO` log stays clean;
enable debug logging to inspect them. The older-episode protection is fixed too:
Gextto never re-downloads an earlier episode outside a recognised gap while it
already owns later ones (gap-fill and manual actions always pass). To freeze a
title, set **Allow upgrades** to No with *Edit selected*.

The real file data (`ffprobe`: HDR, codec, audio, languages) is stored per
episode/movie and **used in upgrade comparisons**, so the archived file is read
as it really is, not just from its name. It is additive (never downgrades a
file). New files are probed on completion; the rest are covered by the scheduled
incremental **MediaInfo backfill** (or the Maintenance button for an immediate
scan). If `ffprobe` is missing, the backfill pauses by itself.

## 8. Integrations

The complete list of fields and actions is in [Appendix B](#appendix-b-reference--integrations).

- **Simkl** — credentials, PIN flow, watchlist import, calendar and mark-watched.
- **Jellyfin / Plex** — server URL + token and a library refresh button.
- **Metrics and events** — gx-torrent serves `GET /api/v1/metrics` for
  Prometheus and `GET /api/v1/events` for Home Assistant or a script. With the
  token, send the `X-Gx-Token` header. Examples in `docs/API.md`.
- **Event hooks** — run an external program on Gextto events
  (`download_started`, `torrent_completed`, `season_pack_completed`,
  `torrent_error`, …). Fields accept placeholders such as `{title}`, `{hash}`,
  `{path}`, `{series}`, `{episode}`; the same values are exported as `GEXTTO_*`
  environment variables. Programs run without a shell and use a 60-second
  default timeout (maximum 24 hours; `0` also means the default). Each
  placeholder becomes a single argument even when its value contains spaces: a
  title such as `The Office` arrives whole, not split in two. When the timeout
  expires, every child process started by the program is stopped too; at most 4
  hooks run at the same time, the others wait their turn.

### Jellyseerr / Sonarr / Radarr

Gextto exposes a subset of the Sonarr/Radarr v3 API for Jellyseerr:
`/api/v3/system/status`, `qualityprofile`, `languageprofile`, `rootfolder`,
`tag`, `series/lookup` with `POST series`, `movie/lookup` with `POST movie` and
`queue`. In Jellyseerr use the Gextto server as the URL and Gextto's **API key**
(Configuration → Access) as the key. This is not full compatibility: requests
Gextto does not handle answer 404, and quality profiles/custom formats do not
reproduce the *arr* semantics.

What happens to a request: for a new series Gextto follows only the seasons
requested in Jellyseerr (no season given = from season 1 on); if the series is
already in the library, the requested seasons are added to the ones it already
follows, none removed. A movie already in the library is not added twice: if it
was disabled it is enabled again. Jellyseerr's "search now" (`/api/v3/command`)
is not handled: the added title is searched by Gextto's normal acquisition
cycles.

### Connecting an external service

Integrations are not required to download and archive media. Enable them one at
a time and use the test button whenever one is available:

- **Simkl**: complete the PIN flow, verify that the correct account is shown,
  and then use watchlist import;
- **Jellyfin/Plex**: enter a URL reachable by the daemon and a token with the
  minimum required permissions, then test a library refresh;
- **Hooks**: first configure a harmless program that writes a log, verify its
  placeholders, and only then connect it to automation or notification scripts.

Hooks run without a shell: pipes, redirections and operators such as `&&` are not
interpreted. If you need them, create a real executable script and pass values
through placeholders or `GEXTTO_*` variables. Do not put tokens or passwords in
arguments if the command may be recorded in system logs.

## 9. Maintenance

The complete list of actions and parameters is in [Appendix C](#appendix-c-reference--maintenance).

- **Updates** — installed version and channel, latest published version and
  what changed (the commits after the installed one, linked to GitHub). The
  check runs by itself every *Update check interval* hours (6 by default;
  *Settings → System → Updates*). **Check for updates** asks GitHub right away;
  when a new build exists the button turns into **Update** (with **Check
  again** beside it), which backs up the databases and asks
  the `gextto-update` system service, set up by the installer, to update:
  Gextto waits for the copies to the library to finish, restarts with the new
  version and the panel reports it by itself. The update succeeds only when the
  new version's web interface answers: if the service goes down, even a few
  seconds later, the previous version comes straight back; if it is alive but
  slow to answer (a long database migration, for example) it is only reported.
  The log of the last update is at the bottom of the panel. On the first visit after the restart a **Gextto updated** popup
  shows the old version, the new one and the commits it brought; **OK** (or
  `Esc`) closes it for good. When a new version exists an **Update available** button also
  appears above **Donate**. On a source installation the panel says to use
  `git pull` and `make build`.
- **Exact version** — at the bottom of the page one line shows the version,
  the full **build** number and the **commit** the binary was compiled from
  (the short hash; the hover tooltip shows the full one). It tells exactly
  which build is installed: the third version number is always the build
  (for example `1.2.2065`), so the full build and the
  commit are the exact identity of the running program.

- Backup now, clean trash, rescore, scan archives, **Refresh MediaInfo**
  (probes archived files without data via `ffprobe` and stores it) and restart
  the service.
- Long operations (**Scan archives**, **Refresh MediaInfo**, **Rename all**) run
  in the background: the page is not blocked, you can keep using Gextto, cancel
  them from the message and see the outcome when they finish.
- **Rename folder contents** — enter a folder and press **Browse** to select it
  on the server. Gextto recursively scans video files, detects series and movies
  from their names, compares titles with TMDB/TVDB and shows a rename proposal
  with alternative matches when available.
- **Trash cleanup** — both **Clean trash** in *Maintenance* and **Empty trash**
  in the **Trash** panel are forced manual actions: they remove the content
  immediately. `trash_retention_days` (0 = delete everything) only applies to
  non-forced cleanups requested through the API
  (`/api/maintenance/clean-trash` with `force=false`). After a cleanup a notice
  says how many items were removed, how much space was freed and their names
  (up to 8); when something could not be removed the notice says so instead of
  claiming success.
- **Video duplicates** — *Preview duplicates* and *Clean duplicates* find video
  files clearly inferior (strictly lower resolution) left next to the best
  version in the same folder, e.g. an old 480p next to the new 1080p, and move
  them to the trash. The check uses the filename and technical data declared in
  the name; it does not compare file contents or calculate hashes. At the
  **same resolution** the version matching the
  preferred language (*Configuration → Library and renaming → Default language*) is kept and a
  duplicate that explicitly declares a different language is moved to trash;
  files with no language tag are left untouched. Files of torrents still in the
  session are protected, and emptied sub-folders are removed. The rename
  verification also performs this cleanup automatically, and upgrade cleanup now
  also honours the "hard" upgrade reasons (resolution/source/HDR/repack). It
  keeps the spelling of the tags already in the name (for example `EAC3` or
  `ddp`, `DV` or `DV HDR10`): it does not rename a file just to change the form
  of the same codec, and a file that is already correct is not renamed a second
  time within the same check.
- **Import file** — a tab to import a video already on disk that Gextto did not
  download: see *Importing a file that already exists*.
- **Archive SQL maintenance** — an advanced, destructive tab for the release
  Archive. A selector at the top chooses the **language for the operations**;
  the tab lists the operations bundled in `scripts/sql`: releases older than 6
  months or 1 year, releases in the selected language or without it, empty
  titles, duplicates by title and source, plus read-only diagnostics (recent
  releases, age distribution, releases per source, archive health, duplicate
  titles). The archive keeps one row per listing seen by the scrapers: for a
  release with a magnet the identity is the infohash, for a `.torrent`-only
  listing it is the indexer's stable identifier. Jackett regenerates the
  download link at every request; without that identifier the same release
  would look new on every pass. **Duplicates by title and source** cleans up the
  rows already accumulated, comparing source and title and keeping the newest;
  it never touches magnets. A deletion operation first shows the
  preview with the number of matching rows and the first 200; only then
  **Delete permanently** appears, with a confirmation. While the query runs an
  indicator shows the work in progress; when it finishes the panel scrolls to
  the result. It is not a free SQL
  console: it runs only the bundled files and does not touch the library,
  active torrents or settings. The language is inferred from the title, so a
  release with no marker falls into the “not in the selected language” list.
  Back up before a large deletion; afterwards `VACUUM` compacts the archive
  file.
- **Restore source** — *Restore source* remounts the `[Source]` token (WEB-DL,
  HDTV, BluRay…) in archived names that lost it, recovering it from the original
  release title in the database. It never invents a source: unknown stays
  untouched. Preview first, then execute; no re-download is involved.
- **gx-torrent log** — panel with the latest lines of the torrent engine log
  (`gx-torrent/gx-torrent.log`), separate from Gextto's log and kept across
  service restarts. You can filter its text, show only warnings and errors, and
  pick between the active file and the rotations (`.1`, `.2`, `.3`). Useful when
  a download or a transfer does not behave as expected.
 - **Database**: prune by cycles/error age, **seen-from-feed retention** (days; 0
   keeps everything), keyword prune with a list of the matching rows, and
   **VACUUM / ANALYZE** across all databases.
    Seen-from-feed cleanup removes only historical feed rows, not files or
    downloads; it also applies the standard cleanup of the last 50 cycles and
    torrent errors past the configured retention period (7 days by default).
- **Backups**: retention, schedule (manual, every N hours or a fixed daily
   HH:MM), FTP host/user/path + **Test FTP** (checks connection, path and a probe
   upload), cloud/sync folder copy, Telegram delivery, list of available backups.
  A snapshot contains the databases and configuration; media files and torrent
  session state are not included.

### Manually renaming a folder

Scanning is a preview and does not modify files. For each row you can:

- choose another TMDB/TVDB result;
- accept or reject the proposal individually;
- use **Accept all proposals** and then **Apply selected**.

Only selected files that really exist and have a valid destination in the same
subfolder are renamed. Conflicts, unrecognised titles and existing destinations
are left untouched. Associated sidecars (subtitles, images, NFO files and
similar) follow the new name when possible.

### Backups: what they protect and what they do not

A Gextto backup protects databases and configuration. It does not contain videos,
media archives or the complete torrent session state. Before restoring:

1. stop or pause automatic cycles;
2. keep a copy of the current database;
3. verify the backup date and size;
4. restore only to a compatible installation;
5. check paths and permissions before enabling downloads again.

Restoring a database does not automatically move media files. If the library was
moved, fix paths or scan the archive before starting upgrades and missing searches.

**What a backup contains.** A ZIP named `gextto-backup-<date>_<time>.zip` in
`<data>/backups` (and on FTP, if configured) with:

- the four databases as transaction-consistent snapshots:
  `gextto_series.db` (series, episodes, movies, downloads, history, provider
  health), `gextto_config.db` (every setting, the indexers, themes,
  translations), `gextto_archive.db` (the feed archive), `gextto_comics.db`;
- the other configuration files of the data folder: the setup marker, the
  Cloudflare domains, the gx-torrent engine state (`gx-torrent/state.json`,
  `lifetime.json`).
- the files and folders outside the data folder listed in *Files and folders
  to include* (one per line), under `extra/` with their original path; paths
  that no longer exist are skipped.

It leaves out what is downloaded or rebuilt: the torrent session and its
resume data (`gextto_torrents_state`, `gx-torrent/session.db`), the IP filter,
the scraper caches, the comics, the logs and the default recycle bin
(`<data>/trash`). Outside the data folder it does not hold
`/etc/gextto/gextto.env` (your environment overrides, if you added any): keep
a copy of it yourself.

**Restoring on the same or a new machine.**

1. Install Gextto with the same options as before (`--user`, `--data-dir`,
   `--port`, …), or stop the existing service: `sudo systemctl stop gextto`.
2. Keep the current data folder aside: `sudo mv /var/lib/gextto
   /var/lib/gextto.old` (use your data folder if it is elsewhere).
3. Extract the backup into a fresh data folder and give it to the service user:
   ```bash
   sudo install -d /var/lib/gextto
   sudo unzip /path/gextto-backup-<date>.zip -d /var/lib/gextto
   sudo chown -R <service user>:<its group> /var/lib/gextto
   ```
4. Put back `/etc/gextto/gextto.env` if you had one, then
   `sudo systemctl start gextto`.
5. Open **Health**: the databases are checked at start-up. With gx-torrent the
   downloads that were running are added back from their magnet in the database,
   and the data already in the download folders is re-checked and reused.

### Cleanup and irreversible operations

Use previews first whenever available. In particular:

- *Preview duplicates* shows what would be moved to trash;
- *Rename preview* shows old and new names;
- *Database cleanup* affects historical rows, not the library;
- trash cleanup can permanently delete files;
- removing a torrent may ask whether to delete its data.

Do not confuse **Trash**, **Archive**, **Download history** and the **torrent
session**: they are different sets, and cleaning one does not automatically clean
the others.

## 10. Health, Logs, Charts

- **Health** — process/system status, runtime metrics (3 columns), Gextto
  service state, **indexer reachability**, path permissions, source health,
  recent errors, disks. For Jackett the check uses the Torznab `caps` endpoint,
  so an API-key or configured-indexer problem is distinguished from simple host
  reachability.
- **Searches** — the same-named panel on the Health page keeps the history of
  the last cycles: when, duration, releases checked, downloads started, errors
  and how many sources did not answer (hover the number for the reason). It
  helps spot a source that keeps failing or a cycle that got slower. API:
  `GET /api/last_cycles`. Gextto warns on its own (WARN line and notification)
  when a source has failed in three of the recent searches, when the last search
  took more than three hours, or when no cycle without errors has completed for
  three intervals (at least two hours); it also reports when everything is
  healthy again. Separately, when the `.torrent` links of one source fail on two
  different releases in the same cycle — usually an expired login, such as
  mircrew's 502 "login failed" — Gextto writes a WARN line and sends a
  notification (at most every 12 hours per source) with the reason the indexer
  returned and what to do: sign in again and, until it works, disable that
  source in the searches. A link that resolves re-arms the warning.
- **Title queue** — panel with the titles held by the *delay profile* (series and
  movies) before the download: title, how long they have been waiting
  (`first_seen_at`) and how long until `due_at`. Shows a queue that keeps growing
  or a title stuck.
- **Failed imports** — panel with the number of failed imports per day (last 7),
  derived from the acquisition events; useful to spot a disk or NAS refusing
  moves.
- **Web logs** — live SSE stream with text filter, line count and follow/pause.
  Lines are English and explicitly formatted as `date time  LEVEL message ·
  key: value`, with highlighted keywords (NAS, download, sources, filters,
  errors). Every cycle prints a **SOURCE REPORT** with each source's outcome,
  the filter decisions, and download events (start, metadata, completion, any
  NAS move and archive import). Torrent messages always include the readable
  name or title; torrent hashes are hidden from the user-facing log. Periodic
  lines do not repeat at every check: the gx-torrent memory release appears when
  the idle period starts and then every six hours, and a warning about a broken
  source returns at most every six hours.
- **Acquisition ID and download history** — lines about a download end with
  `acq: 7f3a2c`: a short ID that stays the same from the start of the download
  to its arrival in the library, for torrents and eD2k downloads alike (also
  those started by hand from "Download" or a pasted link). Typing it in the log filter shows only that
  download. The same lines (INFO, WARN and ERROR, even with the log set to
  `warn`) are stored as its **history**: the *History* tab in the torrent
  detail, the 📜 button on every episode and the *Download history* panel on
  the series page. Identical repeated lines become one with a counter (×N); the
  history is kept for 180 days and stays readable after the log rotates and the
  torrent is removed. API: `GET /api/acquisitions?hash=…`, `?acq=…` or
  `?series=…&season=…&episode=…`.
- **Charts** — CPU/RAM/download/upload/disk/ram-disk sparklines and daily
  consumption.
- **Activity** — recent torrent events and downloads.

### TUI: quick usage

In the TUI, `1`-`9` or `Tab` switch tabs, `r` refreshes and `?` opens help.
With eD2k on, the **Status** tab shows an **aMule** line (version, server and
High/Low ID, Kad, upload rate and queue) and the **Torrents** tab lists aMule's
downloads with the others: `Enter` opens the detail (state, sources, priority,
last data received, `ed2k://` link), `y` copies the link, `p` pauses or
resumes, `d` takes it off aMule. In
**Logs**, `/` filters lines and `f` enables or pauses follow; when paused, the
viewport remains fixed while new log lines arrive. Archive, Missing and
Blocklist support arrow/page scrolling and selection. See [`docs/tui.md`](tui.md)
for the complete key reference.

### Health: interpreting source status

For each indexer the table distinguishes:

- **OK**: valid HTTP response and no Torznab application error;
- **API error**: the service responds, but the API key or indexer configuration is
  rejected;
- **Unreachable**: no response arrived before the timeout.

This distinction matters: restarting Gextto does not fix a wrong API key, while a
network problem may require checking DNS, containers, ports or the firewall.

With eD2k on, the **Health** page also has an **eD2k and Kad (aMule)** card with
the connection state, the speeds, the bytes exchanged in this session, the
missing aMule folders and the latest problem lines from aMule's log
(disconnections, no valid servers, errors), so a problem is visible without
opening the log.

## 11. Notifications

Configure Telegram, e-mail (SMTP) or a webhook — including Discord, Slack,
ntfy, Gotify or Pushover — and send a test. Completion notifications include
size, download time and average speed. For a season pack the notice separates
**new** episodes from those that **replaced an inferior version** already in the
library: the latter are not counted as new, so downloading a single episode and
then its season pack does not look like a duplicate.

### Status notifications (Gextto and gx-torrent)

Besides download events, Gextto notifies service problems:

- **Gextto restart** — on every start after the first, with the run number and
  how long it was down; if the previous stop was not recorded (crash, `kill -9`)
  it is reported as an unclean shutdown;
- **stopped by an error** — when the service exits because of a fatal error;
- **gx-torrent** — unexpected exit of the managed daemon, restart after the
  exit, restoring the previous binary after an update that will not start,
  a live daemon that stops answering, and the automatic switch to another
  engine (qBittorrent-nox first, downloaded on demand, finally recovery with the
  transfers stopped);
- **health problems** — when the health check finds a problem and when it is
  healthy again.
- **MirCrew login** — when the forum login of the `mircrew-indexer` service
  stays broken for more than 15 minutes, and again when it works. The service
  keeps answering searches from its local index, so without this check a broken
  login would produce no other warning.

A dead process cannot notify its own crash: a Gextto crash is therefore reported
on the next start. If Gextto does not come back (for example the service is
disabled) no notification is sent: use a systemd watchdog (`OnFailure=`) for
that case.

### Webhook: which URL and which format

The **Webhook** channel sends events to an external service with no dedicated
app: pick the service **Format**, paste the **URL** and, when needed, put the
credential in the **Webhook token** field (and in **Webhook user** for
Pushover). The URL stays clean, with no token inside.

| Service | Webhook URL | Format | Credential |
|---|---|---|---|
| Your endpoint | your URL | **Gextto (signed JSON)** | "Webhook secret" signs the body (HMAC SHA-256, `x-gextto-signature` header) |
| Discord | the channel webhook URL | **Discord** | none |
| Slack | the incoming webhook URL | **Slack** | none |
| ntfy | `https://ntfy.sh/<topic>` (or your server) | **ntfy** | "Webhook token" = Bearer token, if the topic is protected |
| Gotify | `https://gotify.example/message` | **Gotify** | "Webhook token" = application token |
| Pushover | `https://api.pushover.net/1/messages.json` | **Pushover** | "Webhook token" = application token, "Webhook user" = user key |

Notes:

- with the **Gextto** format the message is a JSON `{"event":…,"data":…}` and,
  when a secret is set, it is signed; with the other formats Gextto sends the
  ready-made **plain text** and the HMAC signature is not applied (it is
  Gextto-specific);
- the credential goes in the **Webhook token** field, not in the URL; only
  Discord and Slack do not need one;
- use the **test** button in the Notifications tab: a wrong URL or token is
  reported at once instead of failing silently on the first delivery.

### Configuration procedure

1. save credentials in the **Notifications** tab;
2. enable only the events you want to receive;
3. send the UI test;
4. check both the provider response and the Gextto log;
5. test a completion event only with an unimportant download.

For a webhook, verify the HMAC secret on the receiving side and do not confuse
an HTTP test with delivery of a real event. For SMTP, check host, port, TLS,
user and sender: a reachable server may still reject the sender or require a
different authentication method.

Event notifications are sent in the background: a slow or unreachable provider
does not slow down downloads, seeding or archiving, and a delivery error is
written to the log. The test from the UI instead waits for the answer and shows
the error at once. An SMTP delivery that stops responding is abandoned after 30
seconds.

## 12. Quick reference

### Which action to use

| Goal | Action |
|---|---|
| Understand what exists online | Manual search |
| Fill missing episodes | Search missing / Series cycle |
| Choose one specific release | **Why not this one?** → Queue |
| Make existing files known | Scan archive |
| Re-download an already archived episode | ↻ Re-download (moves the file to the trash and queues it again) |
| Change names without re-downloading | Rename preview |
| Fix an external service | Health → source → Verify |
| Remove inferior files | Preview duplicates → Cleanup |
| Save configuration and databases | Backup |

### Glossary

- **Release**: a result found by a feed, indexer or web engine.
- **Cycle**: one automatic search and selection pass.
- **Gap**: a missing episode recognised in the archive.
- **Forced gap**: an explicit request to re-download an episode (the ↻
  Re-download action). It bypasses the cover of a season pack and the "a later
  episode is already archived" rule, so the episode can be fetched again even
  when it would not normally count as missing.
- **Placeholder**: a temporary row representing an incomplete download.
- **Upgrade**: replacing an archived file with a better release.
- **Backoff**: a progressive pause for requests to a failing source.
- **Seed**: sharing a torrent after completion.
- **NAS**: network destination used for the library or configured paths.

## 13. Troubleshooting

- **A source is unreachable** — check *Integrations → Check sources* and the
  sources health panel. For Jackett verify the base URL, API key and that at
  least one indexer is enabled in Jackett; for indexarr check the API key and
  that the base URL is its own (normally port `8788`). Torznab errors are
  detected even when the manager returns HTTP 200. Cloudflare-protected sites
  need a working FlareSolverr.
- **Jackett is reachable but shows an API error** — copy the API key again, check
  that at least one indexer is enabled in Jackett, and verify that the URL in
  Gextto is the correct base URL. Do not append
  `/api/v2.0/indexers/all/results/torznab/api` twice. The same applies to
  indexarr: use the base URL (`http://host:8788`), not a complete path, and
  choose **indexarr** as the type.
- **Nothing downloads** — confirm *active mode*, that the series/movie is
  enabled, and check the quality/language filters and the free-space guard.
- **An archived episode will not re-download** — use **↻ Re-download** in the
  series detail: it moves the file to the trash, deletes the episode row and
  records a *forced gap*. This is needed when the season is covered by a pack or
  when a later episode is already archived; without the forced gap the episode
  does not count as missing. An `SxxE00` special, instead, is a single file and
  does **not** cover the season.
- **A torrent is stalled** — check the values under *Configuration →
  Seeding and completion → Stalled torrents*. It is intentionally paused and excluded from active slots; wait
  for the retry or use **Resume/Restart** manually.
- **A watched-folder file is not imported** — keep the `.torrent` or `.magnet`
  extension. Gextto waits for size and timestamp stability, then retries import
  errors automatically; check the watcher log.
- **A file is not renamed** — `mediainfo` should be installed (technical tags);
  check *Rename* settings and the TMDB key (or the TVDB key and its PIN).
  Without metadata the episode title becomes “Episodio N”.
- **A release is visible but not selected** — open **Why not this one?** and
  check monitored title, global filters, sanity, quality/language and archive
  comparison first. If those pass, read the informational cycle-selection step:
  other candidates, delay, free space and gap filling can still change the result.
- **A file exists on the NAS but Gextto considers it missing** — check the episode
  filename, the series path and permissions, then use *Scan archive*. The scan
  recognises video names with season/episode information, not arbitrary files that
  cannot identify their content.
- **A download is complete but not in the library** — inspect the log for move,
  permission and free-space errors; do not delete the source until the archived
  path is visible in history. Temporary errors (NAS unreachable, disk briefly
  full, timeouts, database busy) are retried automatically after 1, 2, 4, 8, 16
  and 32 minutes: only after the last attempt does the torrent go into error. A
  move to the archive interrupted by a Gextto restart resumes on its own about
  a minute after start-up. Download progress is saved every 2 minutes: after
  an unclean stop (crash, power loss) downloads resume where they were, and any
  the engine still lost are added back at start-up, reusing the data already
  downloaded.
- **An episode did not arrive: where to look** — start from the series and
  press 📜 on the episode.
  - *No events*: Gextto never downloaded it. Use 🔍 (manual search) and **Why
    not this one?** to see whether the release is missing from the sources or
    was rejected by filters, quality, language or delays; check the source
    status in *Health* too.
  - *📥 Downloading and then nothing*: the download is running or stuck. Open
    the torrent from *Downloads*: "⏸️ is stuck" means few users are sharing it,
    "❌ Gave up" that it was abandoned and the next search will look for
    another version.
  - *🎉 downloaded but no 📁*: the file is downloaded but did not reach the
    library. Look for "⚠️ still cannot be moved" or "Could not move": usually
    an unreachable NAS, permissions or space; the attempts resume on their own.
  - *"discarded as inferior"*: the library already had a version at least as
    good, so the new one was discarded. This is expected.
  - *📁 added / ♻️ updated*: it is in the library; if the name is not the
    expected one see the renaming item above.
  For the full detail open *Show in the log* from the torrent: it filters the
  log on the lines of that download only.
- **FTP backup fails** — use *Test FTP*: it reports the failing step (connection,
  login, remote path, upload, delete) and logs it.
- **Logs** — see `data/gextto.log` (rotated at 5 MB) or the in-app log viewer.

## Appendix A. Reference — Configuration

The page is split into four areas, each with its sections: **What to look
for** (General, Sources, Quality and upgrades), **How to download** (Torrent
engine, Speed and network, Queues and performance, Seeding and completion),
**Where to store** (Library and renaming, Archive and space) and **System**
(Automatic maintenance, Notifications, Access and services, Diagnostics and
translations). On a computer the sections are in the left column, with the
number of settings; on a phone you pick them from the *Section* list. The page
address follows the open section, so it can be bookmarked. Links to the old
tabs (*Acquisition*, *Advanced*, *Translations*) open the section that took
over their content. Entering Configuration hides the main menu bar; the **Menu** button at the top
left brings it back over the content (Esc closes it).

When a section gathers several groups of settings (for example **Search cycle**
and **Missing episodes** under *Automatic maintenance*), the group title stays
**stuck to the top** while you scroll, with a coloured accent bar.

Under the section title there is the **group index** (clickable chips) to jump
straight to a group. The **Modified only** checkbox, next to *Show technical
keys*, hides every setting still at its default value: handy to review only what
you changed. Rows highlight on hover.

How to read a row:

- **Name and description** on the left; the control in the middle with its
  **unit** (seconds, minutes, GB, KiB/s…) and, below it, the meaning of the
  **special values** (for example “0 = never”).
- Yes/no options are **switches**; times use the time picker. In **Speed and
  network**, **How the speed is decided** picks the rule: *Auto* (Gextto measures
  the line and adapts), *Fixed limits* (the two global limits) or *Hourly
  schedule*. With the schedule you get the table with one row per weekday, where
  every day has **one choice** among *Off*, *Limit* (your values, the only ones
  using the speed fields) and *Auto*, plus start and end, and **Outside the hourly
  windows: Auto**, which says whether Auto or the fixed limits apply outside the
  windows. The older single-window settings no
  longer appear on the page: until the table is saved it shows their values and
  they stay in use.
- Options that depend on a switch or a choice (for example the speed schedule or
  RAM disk fields) **appear only when they matter**, even before saving.
- A value different from the default shows **changed**, the *Default* value
  and the **↺ Default** button, which puts the default back in the field (it
  still has to be saved).
- The technical key (e.g. `refresh_interval`) is shown with **Show technical
  keys**; the search finds it anyway.
- Settings the active torrent engine does not use are collected, closed, in
  **Not used by the active engine** (or in the panel of the inactive engine):
  they stay saved and become active again when you switch engine. With
  qBittorrent-nox this includes the network, protocol, queue, cache and RAM disk
  options only gx-torrent reads: qBittorrent keeps its own configuration. Rare
  options are in the closed **For experts** panel.

Each row is saved with its own **Save**. A field changed and not yet saved is
marked **not saved** and the *Unsaved changes* bar appears at the bottom, with
**Save all** and **Discard changes**. Changing section or leaving the page with
pending changes asks for confirmation. The editors (feeds, filters, rules,
hooks) have their own *Save* button and also show **not saved** until you save
them.

### What to look for

#### General

*When and how often Gextto looks for new releases and missing episodes.*

**Search cycle**

| Setting | What it does |
|---|---|
| Automatic cycle (search and download) | The master switch of the cycle: when off, Gextto neither searches nor downloads on its own. On by default; with dry-run on it searches without downloading. |
| Automatic search interval | Interval between automatic series/movie searches, in seconds (21600 = 6 hours). *(Unit: seconds)* |
| Online title search in the cycle | Search titles on indexers during the cycle: auto (only when no feeds are configured), yes (always), no (never, use only feeds and the local archive). |
| Maximum release age | Ignore releases older than N days (0 = no limit). Releases without a date are treated as published today. *(Unit: days; 0 = no limit)* |

**Missing episodes**

| Setting | What it does |
|---|---|
| Search for missing episodes | Enable filling recognised gaps (missing episodes) from available releases. |
| Missing episodes per series in each cycle | Maximum gaps to search per series in one cycle (0 = unlimited). *(0 = unlimited)* Shown only while «Search for missing episodes» is on. |
| Deep search interval | How many hours between targeted live indexer searches for gaps. *(Unit: hours)* Shown only while «Search for missing episodes» is on. |
| Deep searches per cycle | Maximum number of live searches per cycle. The episodes never searched online come first, one per series in rotation (each pass starts after the series served last), then the ones searched longest ago. Shown only while «Search for missing episodes» is on. |
| Last resort: accept a resolution outside the profile | On by default. When a missing episode has no copy within the quality profile, Gextto takes the one in the requested language even with a resolution outside the profile or unknown. Language, subtitles, audio and exclusions always apply. The download carries the «🆘 last resort» badge in *Downloads*, the log and the notification say so, and the upgrade replaces it as soon as a copy within the profile appears. Shown only while «Search for missing episodes» is on. |

**Delay before downloading**

| Setting | What it does |
|---|---|
| Delay for series | Delays the start of series downloads by this many minutes. 0 starts immediately. *(Unit: minutes; 0 = none)* |
| Delay for movies | Delays the start of movie downloads by this many minutes. 0 starts immediately. *(Unit: minutes; 0 = none)* |
| Skip the delay from this score | If a release reaches at least this score, the configured delay is ignored. *(0 = never)* |

#### Sources

*Where releases come from: RSS feeds, web engines, filters and watched folders.*

**Feeds and blacklist**

| Setting | What it does |
|---|---|
| Blacklist (one word per line) | Forbidden words, one per line: releases containing them are discarded. |
| Feed pages to read | How many listing pages to read per feed (3 is a good compromise). |

**Editors**

| Setting | What it does |
|---|---|
| RSS feeds | List of RSS feeds read on every cycle (one URL per line). |
| Web engines | Web search engines used by gap-filling when feeds and indexers find nothing. |
| Excluded content filters | Releases containing these words or scripts (e.g. [porno]) are excluded. |
| Source filters | Keywords to accept or reject for a single source, enabled per row. |
| Watched folders | The .torrent and .magnet files found in the given folders are **downloaded** and added to the session automatically (recursive, delete after). |

#### eD2k and Kad

*Connection to aMule, search on Kad and the eD2k servers, automatic fallback and stalled downloads.*

eD2k and Kad go through **aMule** (see [eD2k and Kad with aMule](#ed2k-and-kad-with-amule)):
Gextto drives an `amuled` that is already running, through external connections
(EC). It is off by default: without your consent Gextto does not contact the
network.

| Setting | What it does |
|---|---|
| eD2k and Kad through aMule | Connects Gextto to aMule: search, `ed2k://` links, downloads in Downloads and the eD2k page. Off by default. |
| aMule — address | Machine running `amuled`, usually `127.0.0.1`. Gextto must see aMule's incoming folder at the same path. |
| aMule — external connections (EC) port | `ECPort` in `amule.conf`, 4712 by default. |
| aMule — external connections (EC) password | The password in clear; aMule keeps its MD5 in `ECPassword`. Not displayed. |
| aMule folders | The incoming (download) and temporary folders are shown read-only: they belong to aMule and are changed in aMule/amuled. After the change restart Gextto (or reopen this page) so it uses the new path. |
| eD2k search network | **Kad** (default, no server needed), **Kad + eD2k servers** (both searches, results merged), **eD2k servers (global)** (every server) or **eD2k server (connected)**. Applies to the manual search; the automatic fallback always searches Kad and the servers. The same choice is on the eD2k page. |
| Use eD2k when torrents find nothing | A second, separate switch: when the torrent search for the episode and the season produces nothing, Gextto searches the same episode on eD2k/Kad (with the series name and every alias). It applies both to the deep gap-filling pass and to **Search missing** and single-episode search. Off by default. |
| Minimum sources for the fallback | eD2k source floor for accepting a result automatically. Manual search shows everything. *(Unit: sources; 0 = no minimum)* |
| eD2k search duration | How many seconds aMule collects results before Gextto shows them. *(Unit: seconds)* |
| Give up on a stalled eD2k download after | An eD2k download chosen by Gextto that receives no data for this many days is cancelled in aMule, the release is blocklisted and the episode is searched again. Downloads added by hand and paused ones stay. *(Unit: days; 0 = never)* |

The automatic fallback only accepts a result that matches the episode exactly,
has a recognised quality, a plausible size and at least the source floor. In
every other case the result stays visible in manual search and the gap stays
open: a missing episode is better than a wrong file in the library. If aMule is
unreachable, torrents continue unaffected.

On the **Sources** page the **eD2k and Kad** entry shows the connection sensor:
**open** (connected to a server with a high ID), **limited** (firewall/NAT: low
ID or Kad only), **not connected**, **aMule unreachable** or **unknown**. When
it is limited, open aMule's TCP and UDP ports on the router.

#### Quality and upgrades

*How releases are compared and when a better version replaces the one in the library.*

The page starts with what is needed most often and leaves the weights last:

1. **Upgrades and replacement** — the rules that decide when a better release
   replaces the one in the library (table below).
2. **Examples with the current weights** — reference scores (1080p WEB-DL,
   1080p H.265 DD+, 2160p Dolby Vision) and the stop threshold.
   **Impact on the library** — **Check now** replays the releases already seen
   in the feeds with the current rules and weights and says how many archived
   episodes would be replaced, with a few examples. Use it after changing the
   weights or thresholds: a high number means the next cycles would re-download
   a lot. It downloads nothing; series with “Allow upgrades” set to No do not count.
3. **Test a release** — paste a release name: Gextto shows the score item by
   item, the copy already in the library with its score and whether it would
   replace it, with the reason. It downloads nothing.
4. **Quality profiles** — the templates of quality, language, subtitles and
   exclusions (see *Quality profiles*); the list is the one exposed to
   Jellyseerr.
5. **Video**, **Audio**, **Release fixes** — the weights, in closed cards: open
   them only to change the preferences. For each: **higher = more preferred**.
6. **Custom groups**.

The **Custom groups** panel adds a bonus or a penalty to a release group (the name
must match the final tag of the release, e.g. TBK; case does not matter). The
library scores are recomputed automatically when you change the weights or when
a Gextto update changes the scoring rules, so a change is never mistaken for an
upgrade; *Maintenance → Recalculate scores* is still there to force it.

An upgrade **by score** needs a real quality gain: resolution, source, codec,
audio, HDR/DV, fixes and group must be worth at least the minimum difference
more. File size, subtitles and swarm health help choose between equivalent
releases, but on their own they never re-download an episode or a movie of the
same quality. The same rule holds for season packs and after the download: a
file of the same quality does not replace the archived copy just because its
score is higher. When the archived copy does not declare the source or the
group (for example a file imported or renamed with the title only), those
fields are not credited to the release found: without proof that the copy is
worse, it is not replaced. `ffprobe` fills resolution, codec, audio and channels
when the name has lost them.

The rule that prefers a WEB-DL source to a WEBRip or HDTV one follows the
weights too: it holds only while the WEB-DL **Source** weight is higher. Give
WEB-DL and WEBRip the same weight and Gextto treats them as the same quality: a
WEB-DL no longer replaces a WEBRip (HDR, resolution, REPACK/PROPER and the score
still count).

**MediaInfo check before entering the library.** When a completed download is
about to replace an archived episode or movie, Gextto reads with `ffprobe` the new
file, still in the download folder, and the copy in the library, before moving,
linking or copying anything. Resolution, video codec, audio, channels and
HDR10/HLG are taken from the real streams, not from the name. If the name promised
more than there is (a «2160p» that is an upscaled 1080p, an «HDR» without HDR, a
«DDP 5.1» that is stereo AAC) and the real quality is not an upgrade, the download
is discarded and blocklisted and the library copy stays as it is; the log explains
it with «Upgrade not confirmed by MediaInfo». Source, Dolby Vision and
REPACK/PROPER stay as named, because `ffprobe` does not tell them apart reliably.
Without `ffprobe`, or with the archived copy unreachable, the decision taken before
the download stands. Season packs do not go through this check.

**Upgrades and replacement**

| Setting | What it does |
|---|---|
| Min score difference for upgrade | Minimum score difference to replace a file with a better upgrade. Only quality counts: size, subtitles and swarm health are not enough to reach it. |
| Stop upgrading above this score | Upgrade ceiling: once the library file scores at least this much it is no longer replaced by better releases; only a REPACK or PROPER, which fixes a defective release, is still accepted. With the default scores: 1080p WEB-DL H.264 ≈ 1280, 1080p WEB-DL H.265 DD+ ≈ 1480, 2160p WEB-DL ≈ 2480. It adds to the minimum difference: that one says *by how much* a release must improve, this one *up to where*. To stop upgrades for a single title set “Allow upgrades” to No with *Edit selected* (or in the profile you add it with). “Why not this one?” says when a release is skipped because of this ceiling. *(0 = always upgrade: no ceiling)* |
| Replace versions already archived | Replaces lower versions already archived with better upgrades. |
| Min score difference for cleanup | Minimum score difference to replace an existing file with a better one (cleanup). |

**Stuck downloads**

| Setting | What it does |
|---|---|
| Try an alternative after (stall) | If a download has been stuck for at least these minutes — at 0 B/s or still waiting for its file list (metadata, typical of dead magnets) — the search may start an alternative version of the same episode or movie. The stuck download stays in the session and may still recover; archive deduplication will then keep the best copy. *(0 = never)* |
| Score drop for the alternative | How many points below the stuck release an alternative may score: only an adequate alternative is started, never a poor version. The stuck download is not removed. *(0 = disabled)* |

### How to download

#### Torrent engine

*Which engine downloads the torrents and how Gextto reaches it.*

**Torrent engine**

| Setting | What it does |
|---|---|
| Torrent engine | Active torrent engine (gx-torrent or qBittorrent-nox). |

**gx-torrent**

| Setting | What it does |
|---|---|
| gx-torrent — Web API URL | gx-torrent API URL (e.g. http://127.0.0.1:8890); Gextto drives the daemon there. |
| gx-torrent — listen address (LAN) | Listen address of the managed daemon. Default `127.0.0.1:8890`: the web page and API stay on this host only. To expose them on the LAN use `0.0.0.0:8890` and set `gxtorrent_token` (without a token Gextto starts the daemon in `-insecure`). The port is aligned with the URL. |
| gx-torrent — automatic management (cache and queue) | Turns on gx-torrent self-management: dynamic queue and adaptive cache (sized on available memory, active downloads/seeds and storage type). On by default. Turn it off to set cache and slots by hand. |
| gx-torrent — proxy (socks5:// or http://) | Proxy for peers, HTTP trackers and web seeds; with a proxy, DHT and UDP trackers are disabled (not shown). |
| gx-torrent — announced client identity | What peers and trackers see: `gx-core` with the real version (default) or a qBittorrent, Transmission, Rain or anacrolix/torrent preset. It changes only the announced identity, not the protocol: some private trackers only accept whitelisted clients, check their rules. |
| gx-torrent — announced client version | Version used with the preset; empty uses the preset's default. |
| gx-torrent — request timeout | Timeout in seconds for HTTP requests to gx-torrent. *(Unit: seconds)* |
| gx-torrent — polling interval | Minimum interval in milliseconds between torrent status reads (the daemon owns the queue). *(Unit: ms)* |
| gx-torrent — DLNA/UPnP server | Enables the daemon's DLNA/UPnP media server: the torrents' **completed** media is visible to TVs and consoles on the local network. Local network only, no authentication; off by default. Works with the gx-torrent engine. |
| gx-torrent — DLNA: HTTP bind | Address and port the media server listens on (e.g. `:8200` for all interfaces, `192.168.1.10:8200` for one). Empty = `:8200`. Discovery uses UDP 1900 on the same interface. |
| gx-torrent — DLNA: server name | Name shown to the DLNA players on the network. Empty = `gx-torrent`. |
| gx-torrent — DLNA: library folders | Folders (e.g. the archive on the NAS) the media server shows beside the torrents, browsable by folder; separate them with `;`. Empty = automatic: the archive folder and the folders holding the series archives. Read only; the trash, hidden folders and NAS system folders (`@eaDir`, `#recycle`) are excluded. |

**qBittorrent-nox**

| Setting | What it does |
|---|---|
| qBittorrent-nox — downloaded and updated by Gextto | Gextto downloads the latest qBittorrent-nox release itself, installs it in the application folder (next to gexttod), starts/stops it with the service and updates it (with backup and rollback). The engine in use is still chosen in “Torrent engine”: this option does not change it. |
| qBittorrent-nox — Web API URL | qBittorrent-nox Web UI URL (e.g. http://127.0.0.1:8080). |
| qBittorrent-nox — user | qBittorrent-nox Web UI username. |
| qBittorrent-nox — password | qBittorrent-nox Web UI password (not shown). |
| qBittorrent-nox — category | Category applied to torrents added to qBittorrent-nox. |
| qBittorrent-nox — tag | Tag applied to torrents added to qBittorrent-nox. |
| qBittorrent-nox — request timeout | Timeout in seconds for HTTP requests to qBittorrent-nox. *(Unit: seconds)* |
| qBittorrent-nox — polling interval | Interval in milliseconds between torrent status reads. *(Unit: ms)* |
| qBittorrent-nox — path mapping | Needed only when qBittorrent sees the folders under different paths (Docker, NAS, another host): one line per path, local=remote form. **Propose** reads qBittorrent's download and temporary folders and fills the lines; check them and press **Save**. Same machine with the same folders — or managed mode —: leave it empty. |

**Actions**

| Setting | What it does |
|---|---|
| Apply engine | Checks the configured engine and tells whether a restart is needed (*Torrent engine* panel). |
| Install / Optimize qBittorrent-nox, qBittorrent-nox status, Connection test | In the *qBittorrent-nox* panel: install or update the managed binary, show where it is installed and whether an update exists, check the connection. |

#### Speed and network

*Bandwidth limits, time slots, ports and protocols of the torrent session.*

**Speed limits**

| Setting | What it does |
|---|---|
| How the speed is decided | The speed rule: **Auto** (Gextto measures the line and adapts, no cap), **Fixed limits** (the two limits below) or **Hourly schedule** (windows per day, each with limits or Auto). In Downloads, from the **Bandwidth** menu, you can always make a temporary exception, which beats the rule. It is not a setting of its own: it turns «Speed schedule enabled» and «Outside the hourly windows: Auto» on or off. |
| Global download limit | Global download limit in KiB/s (0 = unlimited). *(Unit: KiB/s; 0 = unlimited)* Shown with «Fixed limits» and «Hourly schedule». |
| Global upload limit | Global upload limit in KiB/s (0 = unlimited). *(Unit: KiB/s; 0 = unlimited)* Shown with «Fixed limits» and «Hourly schedule». |
| Speed schedule enabled | Set from «How the speed is decided» and not shown on the page; still available through the API. |
| Outside the hourly windows: Auto | With the hourly schedule: outside the windows the speed is Auto instead of the fixed limits. Shown only with «Hourly schedule». |
| Schedule — per day | One choice per day: **Off** (no window), **Limit** (the window with your download and upload values) or **Auto** (the window in which Gextto measures the line and adapts). Only «Limit» enables the two speed fields; the times stay editable. A window past midnight belongs to the day it starts on. *(a «Limit» day has its own values (0 = unlimited); «Auto» lets Gextto measure the line and adapt; a window past midnight belongs to the day it starts on)* Shown only while «Speed schedule enabled» is on. |
| Schedule — start time | Schedule start time (HH:MM). Not shown on the page: replaced by the «Schedule — per day» table, which shows its value until saved. |
| Schedule — end time | Schedule end time (HH:MM). Not shown on the page: replaced by the «Schedule — per day» table, which shows its value until saved. |
| Schedule — days | Active days: 0=Mon … 6=Sun (e.g. 0,1,2,3,4). *(no day = never active)* Not shown on the page: replaced by the «Schedule — per day» table, which shows its value until saved. |
| Schedule — download | Download limit in KiB/s during the schedule. *(Unit: KiB/s; 0 = unlimited)* Not shown on the page: replaced by the «Schedule — per day» table, which shows its value until saved. |
| Schedule — upload | Upload limit in KiB/s during the schedule. *(Unit: KiB/s; 0 = unlimited)* Not shown on the page: replaced by the «Schedule — per day» table, which shows its value until saved. |
| Auto respects latency | With the speed on Auto, Gextto measures the ping to Google and Cloudflare every 2 seconds (8.8.8.8, 8.8.4.4, 1.1.1.1, 1.0.0.1; the lowest time is used). While the ping is good it raises download and upload; when it rises it cuts 15% and goes down to 128/64 KiB/s. It remembers the speed at which the ping degraded and settles just below it, trying to pass it once a minute. If the ping stays good at full speed the cap is removed; when only background traffic (DHT) is left, the cap is dropped. The first time it applies a cap it writes it to the log at once; after that it writes at most one line a minute, only when the speed changes by at least 10% and the trend confirms it. On an asymmetric line upload counts as load from a tenth of the capacity (between 32 and 256 KiB/s) and is capped before it saturates; if it starts loading during the control phase the cap starts at once. The measured ping appears next to the **↓ Download** speed in the top bar. On by default. |
| Auto — what measures the latency | `both` (default): the ping decides, the router counters are only diagnostics. `ping`: ping only, the router is not read. `router`: old behaviour, yield only to the other devices the router sees. |
| Auto — host to measure latency | Host (or host:port) to measure the latency to with a TCP connection every 2 seconds, only while Auto is on. Empty = Google (8.8.8.8, 8.8.4.4) and Cloudflare (1.1.1.1, 1.0.0.1), port 443; the lowest time is used, so a distant or slow server cannot fake the measurement. |
| Auto — line maximum, download | The real maximum download speed of the line, in KiB/s (e.g. 9000 for 9 MB/s). 0 = unknown: Auto discovers it. If you enter it, Auto never goes above it and finds the right point sooner. |
| Auto — line maximum, upload | The real maximum upload speed of the line, in KiB/s (e.g. 800). 0 = unknown: Auto discovers it. If you enter it, Auto never goes above it and finds the right point sooner. |

**Ports and interfaces**

| Setting | What it does |
|---|---|
| Min port | Minimum torrent session port (requires service restart). |
| Max port | Maximum torrent session port (requires service restart). |
| Listen interfaces | Where the engine accepts connections: 0.0.0.0:6881-6891 for all interfaces, 127.0.0.1:6881 local only, or wg0:6881/tun0:6881 for a VPN. The suggested value works in most cases. |
| Outgoing interface | VPN killswitch: interface used for all outgoing BitTorrent traffic. |
| Test ports | Checks that the peer port is listening and that the router forwards it (the message says "open", "listening but not forwarded" or "closed"). Available with the **gx-torrent** engine. |

**Peer and tracker discovery**

| Setting | What it does |
|---|---|
| DHT | Enable the DHT network to find peers without a tracker. |
| PEX | Peer Exchange: exchange peers with other clients. |
| LSD | Local Service Discovery: finds peers on the local network. |
| UPnP | Opens router ports automatically with UPnP. |
| NAT-PMP | Opens router ports automatically with NAT-PMP (PCP, RFC 6887, is tried too). |
| uTP | Enable the uTP (UDP) protocol in addition to TCP. |
| Holepunching (BEP 55) | Opens a direct connection to peers behind NAT through a relaying peer (BEP 55). Requires uTP. |
| DHT bootstrap nodes | Initial DHT nodes (host:port separated by comma). |

**Encryption and IP filter**

| Setting | What it does |
|---|---|
| Encryption | Encryption policy: 0 disabled, 1 enabled, 2 forced. |
| Apply IP filter | Apply the IP filter to trackers as well. |
| IP filter (file/URL) | Local file or URL of the IP blocklist. The default is the eMule Security list (`https://upd.emule-security.org/ipfilter.gz`), in P2P format and updated every day. |

**Actions**

| Setting | What it does |
|---|---|
| IP filter → Load / update now | Downloads the list from the configured URL (or uses the local file) and applies it to the active engine right away, gx-torrent included. |

#### Queues and performance

*How many torrents stay active, connections, RAM disk and cache.*

**Automatic management**

| Setting | What it does |
|---|---|
| Adapt values over time | Adjusts over time the values the system chose (the minimum free space, for instance) when conditions change. It never touches values you set by hand; turn it off to keep them fixed. |

**Queue and slots**

| Setting | What it does |
|---|---|
| Automatic queue and resource management | Automatically adjusts how many torrents are active based on load. |
| Minimum dynamic download slots | Minimum number of dynamic downloads. The queue changes by at most one at a time. Shown only while «Automatic queue and resource management» is on. |
| Maximum dynamic download slots | Maximum number of dynamic downloads. Consecutive consistent samples are required before increasing the queue. Shown only while «Automatic queue and resource management» is on. |
| Don't count stalled torrents in active slots | Torrents that are not transferring data do not consume an active slot. |
| Active downloads | Base value for active downloads; with the dynamic queue it is adapted at runtime. |
| Active seeds | Base value for active seeds; with the dynamic queue it drops to 1 when downloads are queued. |
| Active torrents limit | Base value for the active torrent limit; with the dynamic queue it becomes max(base, downloads + seeds + 2). |

**Download mode**

| Setting | What it does |
|---|---|
| Sequential download | Download files sequentially instead of in scattered pieces. |
| First/last piece of files | Download the start and end of every file first in all new torrents, then continue normally: useful to watch a video while it downloads. Works with every engine; torrents already running are not changed. |
| Preallocate disk space | Reserves all disk space up front before starting the download. |

**Connections**

| Setting | What it does |
|---|---|
| Automatic connections (sized from the machine) | On (default): Gextto lets the engine decide how many connections to keep for each torrent from the machine's cores and RAM, and trims the ones on torrents that have made no progress for a couple of minutes despite many peers. With many torrents active together the engine also keeps a total for the whole machine (75 connections per core), shared among the torrents with downloads first; a torrent on its own always gets all of its connections. The value below stays the ceiling. Turn it off to use a fixed number. gx-torrent only. |
| Max connections per torrent | Ceiling on the connections per torrent (-1 = unlimited, the default). With automatic connections on it is the most the engine may use; with it off it is the fixed limit. It applies to every torrent without a limit of its own (*Limits* tab of the detail) and takes effect at once, running torrents included; gx-torrent only. |

**RAM disk**

| Setting | What it does |
|---|---|
| Use the RAM disk | Downloads to RAM the torrents that fit the threshold; larger ones go to disk. |
| RAM disk folder | RAM disk to use for in-progress downloads, if available. Shown only while «Use the RAM disk» is on. |
| Maximum size per torrent | Maximum size of a single torrent allowed on the RAM disk (GB). *(Unit: GB)* Shown only while «Use the RAM disk» is on. |
| Free margin to keep | Free space to leave on the RAM disk once the download completes (GB). *(Unit: GB)* Shown only while «Use the RAM disk» is on. |
| Minimum free space | Minimum free space in bytes required to use the RAM disk. 0 = use the configured margin. *(Unit: bytes; 0 = use the margin)* Shown only while «Use the RAM disk» is on. |

The choice is made as early as possible: a torrent whose size is already known
and exceeds the threshold goes straight to disk; one with an **unknown** size (as
with most magnets) stays on disk until the metadata arrives, and is then moved
onto the RAM disk **only if** the real size fits. An oversized file never touches
the RAM disk. The rule applies to every download — automatic and manually added —
and to every engine that uses the RAM disk; qBittorrent, which does not support
it, always downloads to disk.

Gextto also checks the machine's available memory: a `tmpfs` lives in RAM, so a
6 GB mount can look almost empty while the system has little memory left. Before
starting, the RAM disk's usable space is capped by the available memory
(`MemAvailable`): a download goes to the RAM disk only if both the `tmpfs` space
and the system memory can hold it, otherwise it stays on disk like oversized
files. When the memory cannot be read, only the `tmpfs` space counts, as before.

**For experts**

| Setting | What it does |
|---|---|
| Disk cache | Disk cache size in blocks (-1 = automatic). *(Unit: blocks; -1 = auto)* |
| Cache expiry | Seconds of inactivity after which a block leaves the cache. *(Unit: seconds)* |

**Actions**

| Setting | What it does |
|---|---|
| Optimize | Computes cache and buffers from the RAM. |
| Optimize for the system | Proposes the values that fit this hardware (memory, disks, network share, RAM disk), one row each with the reason; nothing is written until you apply the selected rows. A value you saved by hand is never proposed again. |

#### Seeding and completion

*How long to seed, what to do when a download finishes, how to inspect downloading packs and how to handle stalled torrents.*

**Seeds**

| Setting | What it does |
|---|---|
| Global seed ratio | Upload/download ratio after which seeding stops. 0 = infinite: this alone makes the seed never end and the file is not moved, even with a time limit. *(0 = infinite)* |
| Maximum seeding | Primary seeding limit in days; when greater than 0 it takes precedence over the limit in minutes. With days and minutes at 0 the time is infinite. *(Unit: days; 0 = use the limit in minutes)* |
| Maximum seeding (fallback) | Seeding limit in minutes, used only when Maximum seed (days) is 0. With days and minutes at 0 the time is infinite: the seed never ends and the file is not moved. *(Unit: minutes)* |
| Stop an idle seed after | Stops a seeding torrent that has sent no data for this many minutes (only time spent actively seeding counts). Like the other limits, the end of the seed is followed by archiving or removal. With qBittorrent the value is also sent as `inactiveSeedingTimeLimit`. *(Unit: minutes; 0 = off)* |

**Editor**

| Setting | What it does |
|---|---|
| Seed rules | Seeding limits (ratio, days, inactivity) by source/indexer and category; the first matching rule applies, an empty field uses the global limit, 0 removes the limit. |

**When a download completes**

| Setting | What it does |
|---|---|
| Remove completed items after seeding | On: at the end of seeding the completed torrent is removed from the session (like “Clean completed”). Off: at the end of seeding the torrent stays in the list as Completed and you remove it with “Clean completed”. It does not affect where files are moved. |
| Move episodes/packs to the archive (do not copy) | On: the downloaded source is moved into the library and removed from the download folder (for torrents at the end of the seed; for eD2k at completion, because seeding is managed by aMule). Off: the source is copied or linked and kept for sharing. |
| Hardlink instead of copy while seeding | On (default): a file that keeps seeding enters the library as a hardlink, without using the space twice; when downloads and library are on different filesystems it is copied. See “Hardlinks instead of copies”. |

**Pack inspection**

| Setting | What it does |
|---|---|
| Inspect downloading packs and discard useless ones | On: as soon as the engine knows the file list, Gextto checks whether at least one episode is really wanted (missing from the library or a real upgrade). If none is, the torrent is removed with its partial data and blocklisted, so it is not downloaded again. Off (default): the pack is downloaded in full and decided only when the download ends. |
| In useful packs, download only the files that are needed | On (with inspection): when the pack holds only some useful episodes, the engine downloads only those files and skips the others (priority 0); subtitles and files without an episode number stay active. Off: the useful pack is downloaded in full. |

**Stalled torrents**

| Setting | What it does |
|---|---|
| Consider stalled after | After this many minutes without progress the torrent is considered stalled. *(Unit: minutes)* |
| Retry stalled torrents every | Interval between reannounce attempts for stalled torrents. *(Unit: minutes)* |
| Remove stalled torrents after | After this period without progress, the torrent is removed automatically. Set 0 to completely disable automatic removal due to stalling. *(Unit: minutes; 0 = never)* |
| Remove torrents without seeders after | Stall with zero seeders (dead swarm): removed after this shorter period, and the episode is searched again right away. Set 0 to use the general threshold. *(Unit: minutes; 0 = use the general threshold)* |
| Let a release without seeders back in after | A release abandoned because its swarm had no seeders stays blocklisted for this period, then it is back among the candidates: a 4K that has found seeders in the meantime can be downloaded as an upgrade. The copy already in the library stays meanwhile. Set 0 to keep it blocked forever. *(Unit: days; 0 = forever)* |

### Where to store

#### Library and renaming

*How files are named and organized in the library.*

**Rename**

| Setting | What it does |
|---|---|
| Rename episodes | Renames downloaded files using TMDB metadata (or TVDB without a TMDB key). |
| Movies as single files (flatten folders) | If Yes, a movie that arrives inside a torrent folder is moved to the movies folder as a single file, with its subtitles and artwork. If No, it stays in its folder. |
| Check renamed files every | How many hours between checks that archived/renamed files are still present. *(Unit: hours)* |
| Remove audio tracks in other languages | If Yes, after importing Gextto keeps only the audio tracks in the chosen language and removes the others, without re-encoding the video: quality, HDR and sync stay those of the original file. A file is touched only when it has more than one audio track and at least one is tagged in the chosen language; it is replaced only after a check on tracks, duration and sync. The option can be enabled only when ffprobe, ffmpeg and mkvmerge are installed; otherwise the row stays blocked with a warning. Every cleaned file sends a notification on the configured channels (Telegram, webhook, email) with the file, the removed tracks and the space saved. |
| Audio language to keep | Choose from the list (Italian, English, French…) or type the two or three letter code, for example ita or eng. Only tracks with this tag are kept; an untagged track is not considered. |
| When several tracks are in the chosen language | Keep them all, only the best one (more channels and bitrate) or only the first. |

**Metadata languages**

| Setting | What it does |
|---|---|
| Default language | Default preferred language for series and movies (e.g. ita, eng). |
| TVDB language | Preferred language for TVDB metadata (e.g. ita, eng). |
| TMDB language | Language used for TMDB metadata (e.g. it-IT, en-US). |

**Editors**

| Setting | What it does |
|---|---|
| Name composition | Template editor with tokens and preview to compose file names. |
| Tag → folder rules | Associates a torrent tag with a temporary and a final folder. |

#### Archive and space

*Working folders, trash and minimum disk space.*

**Folders**

| Setting | What it does |
|---|---|
| Archive folder | Default archive folder for content without a dedicated path. |
| Download folder | Default download folder for every engine. |
| Temporary folder | Temporary folder for in-progress downloads. |
| Copy .torrent files to | Copy the .torrent files of downloads here (empty = no copy). |

**Trash**

| Setting | What it does |
|---|---|
| Trash folder | Folder where replaced/duplicate files are moved (if left empty, uses the trash subfolder in the data folder). |
| What to do with replaced files | What to do with replaced files: move to trash or delete. |
| Trash retention | Retention days for non-forced cleanups; 0 deletes the whole trash content. Manual UI actions always empty the trash immediately. *(Unit: days; 0 = empty everything)* |

**Disk space**

| Setting | What it does |
|---|---|
| Minimum free space to download | Minimum free space (GB) on the download folder: below this threshold the cycle does not start downloads and gx-torrent pauses the running ones, which resume by themselves when space returns. *(Unit: GB; 0 = no cycle check, gx-torrent keeps its default of 1 GiB)* |

### System

#### Automatic maintenance

*Periodic cleanup of technical data, MediaInfo analysis and orphaned files.*

**Housekeeping**

| Setting | What it does |
|---|---|
| Periodic housekeeping enabled | Enables periodic cleanup of technical data and history. |
| Housekeeping — interval | Interval between two automatic housekeeping runs, in hours. *(Unit: hours)* Shown only while «Periodic housekeeping enabled» is on. |
| Housekeeping — search-cycle statistics kept | Number of search cycle statistics to keep. Shown only while «Periodic housekeeping enabled» is on. |
| Housekeeping — seen in feeds | Deletes feed-seen release history rows older than N days. *(Unit: days; 0 = never)* Shown only while «Periodic housekeeping enabled» is on. |
| Housekeeping — download history | Deletes download history rows of removed torrents older than N days. *(Unit: days; 0 = keep forever)* Shown only while «Periodic housekeeping enabled» is on. |
| Housekeeping — error cards | Deletes error torrent entries older than N days (minimum 1). *(Unit: days)* Shown only while «Periodic housekeeping enabled» is on. |
| Housekeeping — gap search log | Deletes the missing-episode search log older than N days. *(Unit: days; 0 = never)* Shown only while «Periodic housekeeping enabled» is on. |
| Housekeeping — upgrade backups | Deletes backups of files replaced by upgrades older than N days. *(Unit: days; 0 = never)* Shown only while «Periodic housekeeping enabled» is on. |

**Release archive cleanup**

| Setting | What it does |
|---|---|
| Automatic archive cleanup | Enables automatic archive cleanup on every housekeeping pass: it removes only release-catalogue rows older than the maximum age. Downloaded files, the library, download history and active torrents are untouched. |
| Archive — maximum age | Maximum age of archive releases, in days (0 = no limit). *(Unit: days; 0 = no limit)* Shown only while «Automatic archive cleanup» is on. |
| Archive — keep at least N entries | Minimum number of recent releases to always keep, even when older than the maximum age. Shown only while «Automatic archive cleanup» is on. |

**MediaInfo analysis**

| Setting | What it does |
|---|---|
| Automatic MediaInfo backfill | Periodically analyses with ffprobe the files already present that have no MediaInfo yet. |
| MediaInfo backfill — interval | Minutes between two MediaInfo backfill passes. *(Unit: minutes)* Shown only while «Automatic MediaInfo backfill» is on. |
| MediaInfo backfill — files per run | Maximum number of files analysed in each MediaInfo pass. Shown only while «Automatic MediaInfo backfill» is on. |

**Orphaned files**

| Setting | What it does |
|---|---|
| Move orphaned data in the temp folder to the trash | Every 30 minutes, moves to the trash the files and folders in the download temp folder that belong to no listed torrent and have not changed for at least the given number of days. Nothing is deleted: they stay in the trash until you empty it. |
| Orphaned data — idle for at least | How many days an item in the temp folder must stay untouched, with no listed torrent, before it is moved to the trash. *(Unit: days)* Shown only while «Move orphaned data in the temp folder to the trash» is on. |

#### Notifications

*Telegram, email, webhooks and programs to run on events.*

**Telegram**

| Setting | What it does |
|---|---|
| Telegram enabled | Send notifications to Telegram. |
| Telegram bot token | Telegram bot token (from @BotFather). Shown only while «Telegram enabled» is on. |
| Telegram chat ID | ID of the chat/channel where notifications are sent. Shown only while «Telegram enabled» is on. |

**Email**

| Setting | What it does |
|---|---|
| Email enabled | Send notifications by email. |
| SMTP | SMTP server as host:port (e.g. smtp.gmail.com:587). Shown only while «Email enabled» is on. |
| Email sender | Sender address for notification emails. Shown only while «Email enabled» is on. |
| Email recipient | Email recipients (comma-separated). Shown only while «Email enabled» is on. |
| Email password | SMTP password/app password (not shown). Shown only while «Email enabled» is on. |

**Webhook**

| Setting | What it does |
|---|---|
| Webhook URL | Webhook URL where events are sent. |
| Webhook secret | HMAC secret to sign webhook requests. |
| Webhook format | Format of the payload sent to the webhook URL (Gextto, Discord, Slack, ntfy, Gotify, Pushover). |
| Webhook token | Provider credential (ntfy, Gotify, Pushover); not shown. Not needed for Gextto, Discord and Slack. |
| Webhook user (Pushover) | Pushover only: the user key. Not needed for the other formats. |

**Editors**

| Setting | What it does |
|---|---|
| Event hooks | Runs a program on selected events (name, events, program, arguments, timeout). |

#### Access and services

*Login, API keys and credentials of external services.*

Gextto is meant for a trusted LAN: access is **open by default**. When you
reach it from outside (reverse proxy, port forwarding, VPN) you can require a
login from clients that are not on the local network.

**Login**

| Setting | What it does |
|---|---|
| Require login | Off (default): no checks. On: clients outside the local network must log in or use the API key. Until a password or a key is set everything stays open (and the log says so). |
| No login from the local network | On (default): 127.0.0.1, 192.168.x.x, 10.x.x.x, 172.16–31.x.x and local IPv6 addresses need no login. Behind a reverse proxy the real client address forwarded by the proxy counts (`X-Forwarded-For`, `X-Real-IP`, `Forwarded`), so a client coming from the Internet through the proxy still has to log in. Shown only while «Require login» is on. |
| Username | Login username (default `admin`). Shown only while «Require login» is on. |
| Password | Stored only as a bcrypt hash; changing it closes every open session. Leave the field empty to keep it. Shown only while «Require login» is on. |

**Writes from another origin.** Regardless of the login, Gextto refuses
changing requests that the browser declares to come from another site
(`Origin`/`Referer`, `Sec-Fetch-Site: cross-site`): a malicious page cannot
reconfigure Gextto (settings, event hooks, torrent actions) through the browser
of someone who has the interface open. Reads (GET) and clients without a browser
(curl, the TUI) are untouched.

**API keys**

| Setting | What it does |
|---|---|
| API key (scripts, TUI, calendar) | For non-browser access: header `X-Api-Key: <key>` or `?apikey=<key>` in the URL. The TUI reads it from the `GEXTTO_API_KEY` variable. |
| gx-torrent — access token (page and API on the LAN) | Under the **Access** tab: shared secret required by the page and the API; mandatory when the daemon listens on the network (not shown). |

Both secrets are generated from the row itself: **Generate** creates a long
random value and saves it, **Copy** puts it in the clipboard. A new gx-torrent
token needs the daemon restarted to be applied. What they are and how to use
them is explained in [Access keys](api-keys.md).

**Metadata services**

| Setting | What it does |
|---|---|
| TMDB API key (recommended) | Free TMDB API key: the main source of titles, posters and metadata. When set, it takes precedence over TVDB. |
| TVDB API key (alternative to TMDB) | TheTVDB v4 API key: without the TMDB key it provides all the metadata. |
| TVDB subscriber PIN | TheTVDB subscription PIN, asked at login only by “user-supported” keys. |

#### Metadata: TMDB or TVDB

Gextto uses metadata to know how many episodes each season has (missing
episodes, season packs, completeness), the series status, the
calendar, episode titles in file names, posters and the series and movie
pages. One of the two keys is enough:

- **TMDB (recommended)** — free key from themoviedb.org → Settings → API. When
  it is set Gextto always uses TMDB, even if the TVDB key is set too.
- **TVDB (alternative)** — v4 key from thetvdb.com → Dashboard → API keys.
  Without the TMDB key it provides every feature listed above except the
  trending and popular lists of **Explore**, which exist only on TMDB.
  “User-supported” keys require a paid TheTVDB subscription: enter its PIN in
  *TVDB subscriber PIN*. Episode titles follow the *TVDB language* (e.g. `ita`);
  an untranslated episode keeps its original title.

Without either key Gextto still downloads, but without the data above: missing
episodes stop at the last one already downloaded and files get generic titles
(“Episodio N”). TMDB and TVDB use different ids: a series added with TVDB keeps
its TVDB id, and if you later set the TMDB key Gextto finds the series on TMDB
by name.

A browser session lasts 30 days; `/logout` ends it. After five wrong passwords
from the same address further attempts are blocked for a minute and each failure
is logged. If you lock yourself out, start Gextto with the environment variable
`GEXTTO_AUTH_DISABLE=1`: access control is off while the variable is set and you
can fix the settings. For access from outside always use HTTPS (reverse proxy
with a certificate, or a VPN), otherwise the password travels in clear text.

#### Diagnostics and translations

*Detailed logs and interface texts.*

**Diagnostics**

| Setting | What it does |
|---|---|
| Debug (detailed logs) | Enables detailed logs and periodic diagnostics for debugging. |

**Updates**

| Setting | What it does |
|---|---|
| Check for updates | Downloads the latest build information from GitHub on a timer (how often is set by «Update check interval») and, when a newer one exists, shows «Update available». It sends no data and never updates on its own: the update only starts from Maintenance. |
| Update check interval | How many hours apart Gextto checks whether a new build is out (1 to 720, default 6). The check downloads only a small file from GitHub; «Check for updates» in Maintenance checks at once. |

**Editors**

| Setting | What it does |
|---|---|
| Translations | Export/import the UI string translations as YAML for Italian or English; import adds or updates keys without deleting the others. |

## Appendix B. Reference — Integrations

The page is grouped — **Services** (Simkl), **Media server** (Jellyfin, Plex),
**Sources** (indexers, FlareSolverr, source check) and **Links** (iCal calendar,
browser handlers) — with the group title stuck to the top while you scroll.

### Simkl

| Field / action | What it does |
|---|---|
| Client ID | Client ID of the Simkl app. |
| Calendar days | How many days ahead to show in the Simkl calendar. |
| Watchlist status | Status assigned to imported series: Plan to watch, Watching or Completed. |
| Mark as watched | Marks downloaded episodes as watched on Simkl. |
| Start access / Confirm | Starts the PIN flow and confirms the code shown by Simkl. |
| Revoke | Revokes access and removes the stored token. |
| Import watchlist | Imports the series from the Simkl watchlist into the library. |
| Watchlist / Calendar | Read-only tables with the watchlist and the upcoming releases. |

### Jellyfin

| Field / action | What it does |
|---|---|
| Jellyfin URL | Jellyfin server URL (e.g. `http://127.0.0.1:8096`). |
| Jellyfin API key | API key generated in Jellyfin → Dashboard → API Keys. |
| Jellyfin — path mapping | Only when Jellyfin sees the library under different paths (e.g. Docker): one line per folder, `gextto_path=jellyfin_path`. |
| Test connection | Checks that Jellyfin responds. |
| Refresh library | Asks Jellyfin to refresh the whole library. |

### Plex

| Field / action | What it does |
|---|---|
| Plex URL | Plex server URL (e.g. `http://127.0.0.1:32400`). |
| Plex token | `X-Plex-Token` used to access the library. |
| Plex — path mapping | Only when Plex sees the library under different paths (e.g. Docker): one line per folder, `gextto_path=plex_path`. |
| Test connection | Checks that Plex responds. |
| Refresh library | Asks Plex to refresh the whole library. |

After each import Gextto asks Jellyfin and Plex to rescan **only the folder that
changed** (the season or movie that just arrived), not the whole library: the NAS
is spared a full scan per episode and the file shows up in seconds. For Plex the
folder must be inside one of the folders of one of its libraries. When the
targeted request is not possible (unknown path, server refusal) Gextto asks for
the full refresh as before. When the server runs in Docker and sees the files
under another path, fill in the path mapping.

### iCal calendar

`/feed/calendar.ics` is a calendar to subscribe to from Thunderbird, Google
Calendar, Apple Calendar or a phone (link under *Integrations → iCal calendar*).
Gextto was born for an Italian audience, where series often arrive months or
years after the original broadcast, so the calendar tells three things apart:

| Entry | What it means |
|---|---|
| 📥 Series S01E04 | episode or movie that **arrived in the library**, on the day Gextto downloaded it (last 30 days). The only date that says when it is really available. |
| 📺 Series S02E03 · Title | **original broadcast** (TMDB, or TVDB) of the current season, from the last week to the next two months. It is not the date of a localised release; ✓ when the episode is already in the library. |
| 🎬 Movie | release of a monitored movie **in your country** (TMDB: digital, then home video, then cinema; the country follows the TMDB language, e.g. `it-IT`). When that country has no date yet the original one is shown, marked “(original release)”. With only the TVDB key the movie's first release is shown, always marked “(original release)”. |

A TMDB or TVDB key is required; the calendar is rebuilt at most every 30 minutes. With
access control on (see *Access*) add `?apikey=<key>` to the address.

### Torznab indexers

| Field / action | What it does |
|---|---|
| Name | Indexer label (e.g. `jackett` / `prowlarr` / `indexarr`). |
| Base URL | Base URL of the service; Gextto appends the Torznab path. |
| API key | Indexer API key. |
| Type | Auto-detected, Prowlarr, Jackett or indexarr. |
| Enabled | Enables or disables the indexer. |
| Test | Tests the indexer (for Jackett and indexarr it uses `t=caps`). |

A **direct** Torznab indexer (not managed by Jackett, Prowlarr or indexarr), for example the
`mircrew-indexer` service on the same machine or the LAN, is added here with
its base address (e.g. `http://192.168.1.10:9118`) or one ending in `/api`:
Gextto uses `<base>/api`, the same rule as gx-torrent, so the address to type is
identical in both. Automatic searches have a **100-second budget per title**: an indexer behind
Prowlarr may wait up to **90 seconds** (Prowlarr serves one search at a time per
indexer), a direct indexer or one behind Jackett up to **45**, indexarr up to
**90** (one search per title over the whole manager). A lane whose queue would
outlast the budget is skipped without counting as a failure, so one slow tracker
does not stop the cycle. When an
indexer on a local or private-network address drops the connection or does not
answer, Gextto does **not** retry through FlareSolverr (Cloudflare does not
protect a LAN service): the error stays visible as it is. For `mircrew-indexer`,
*Health → Provider status* shows whether the service answers and whether its
login to the MirCrew forum worked; if it did not, log in again from the
service's web UI.

Gextto reads the indexer list and its capabilities from the manager (the
Torznab *caps*; for Prowlarr the list of its indexers, re-read every 10 minutes)
and sends only the parameters that the single indexer declares. For example Jackett's "all" aggregate does not accept the
TMDB id in series and movie searches, and Prowlarr's public indexers cannot
search by id: in these cases the search uses title, season and episode instead
of failing or coming back empty. If an indexer still rejects an id, the search
is repeated right away without it; the error shown in *Health* carries the
indexer's own explanation (for example "HTTP 400: Torznab 100: Invalid API
Key") instead of the bare HTTP code.

### FlareSolverr

| Field / action | What it does |
|---|---|
| URL | URL of the FlareSolverr service used to bypass Cloudflare. |
| Test FlareSolverr | Checks that FlareSolverr responds. |

### Event hooks (Configuration → Notifications)

| Field / action | What it does |
|---|---|
| Name | Hook label. |
| Enabled | Enables or disables the hook. |
| Events | Events that trigger the hook (empty = all). |
| Program | Executable to run (without a shell). |
| Arguments | Arguments with placeholders `{title}`, `{hash}`, `{path}`, `{series}`, `{episode}`; each placeholder stays a single argument even with spaces. The same values are exposed as `GEXTTO_*` variables. |
| Timeout (s) | Timeout in seconds (default 60, maximum 24 hours; `0` = default). |

### Browser handlers and source check

| Item | What it does |
|---|---|
| Magnet / Torrent manager | Downloads the scripts to open magnets and `.torrent` files directly in Gextto. |
| Magnet / Torrent `.desktop` | `.desktop` versions for Linux desktop integration. |
| `install.sh` | Downloads the installation script. |
| Check sources / Refresh | Checks feeds, indexers and web engines; with a query it also measures results, without changing settings. |

## Appendix C. Reference — Maintenance

### Quick actions

| Action | What it does |
|---|---|
| Backup now | Immediately creates a database backup snapshot. |
| Clean trash | Empties the trash completely (forced action: `trash_retention_days` does not apply). |
| Recalculate scores | Recomputes the score of archived releases with the current weights. |
| Scan archives | Re-reads the archive folders and updates the library. |
| Update MediaInfo | Analyses with `ffprobe` the archived files that have no MediaInfo. |
| Rename all | Renames every archived file using the configured format. |
| Housekeeping | Cleans technical data and history without touching the library. |
| Restart service | Restarts the Gextto daemon. |

### Rename folder content

| Item | What it does |
|---|---|
| Folder / Browse | Type or pick the folder to analyse from the server. |
| Scan and propose | Recursively analyses the videos, recognises series/movies with TMDB/TVDB and proposes new names (preview only). |
| Per-row result | Selects an alternative TMDB/TVDB match. |
| Accept / reject | Approves or discards the single proposal. |
| Accept all proposals | Approves every proposal at once. |
| Apply selected | Renames the selected files that really exist with a valid destination. |
| Rename progress | Progress of the background operation. |

### Library

| Item | What it does |
|---|---|
| Video duplicates — Preview duplicates | Lists inferior duplicates (lower resolution) without deleting anything. |
| Video duplicates — Clean duplicates | Moves the inferior copies found to the trash. |
| Database optimisation — VACUUM / ANALYZE | Compacts the databases and refreshes the query planner statistics. |
| Database optimisation — Refresh sizes | Re-reads size and status of the database files. |
| Database optimisation — Check integrity and indexes | Checks integrity, foreign keys and indexes (including the archive FTS). |
| RAM disk | Shows writable `tmpfs`/`ramfs` paths, lets you select one, create a dedicated one and refresh the list. |

### Cleanups

| Item | What it does |
|---|---|
| Trash — Open trash | Shows the trash items so you can delete them one by one. |
| Trash — Empty trash | Deletes every trash item and shows a notice with the removed files and the freed space. |
| Database cleanup — Cycles to keep | Number of cycle statistics to keep. |
| Database cleanup — Error days | Keeps error torrent entries for at least N days before removing them. |

### Log

| Item | What it does |
|---|---|
| gx-torrent log | Latest lines of the torrent engine log (`gx-torrent/gx-torrent.log`), with text filter, warnings/errors only, line count and a choice between the active file and the rotations. |

### Backup

| Item | What it does |
|---|---|
| Backup now | Immediately creates a backup archive, without waiting for the interval or the scheduled time. The same command is in Quick actions. While it works it shows a notice and stays disabled. |
| Save settings | Saves the backup settings of this section (retention, schedule, FTP, cloud folder, extra paths, Telegram): it does not create an archive. |
| Backups to keep | Number of local ZIP files to keep in the `backups` folder. |
| Interval (hours) | Automatic backup interval in hours (0 disables the interval). |
| Time (HH:MM) | Local daily time of the automatic backup; when set it takes precedence over the interval. |
| FTP host | FTP server host/address (empty = no FTP upload). |
| FTP user | FTP server user. |
| FTP password | FTP password (not shown; empty keeps the stored one). |
| FTP path | Remote destination folder for the ZIP. |
| Cloud folder | Local path of a folder already synced by a cloud service. |
| Files and folders to include | Absolute paths of files or folders to add to the ZIP (one per line). They end up in the archive's `extra/` folder with their original path; paths that no longer exist are skipped. |
| Send to Telegram | Sends a copy to Telegram using the configured bot. |
| Test FTP | Checks connection, login, remote path, a test upload and removal. |
| Available backups / Verify | Table of snapshots with label, name, size and date; the Verify button checks the archive, shows a notice while it works and confirms when the ZIP is intact. |
