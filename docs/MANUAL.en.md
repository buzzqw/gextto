# Gextto User Manual

This is the operational guide for Gextto's web UI and terminal TUI. The web
interface is available in English, Italian, German, French, Spanish and Polish; this document uses
English labels. For Italian labels, read [MANUAL.it.md](MANUAL.it.md). The
non-Italian UIs use their bundled catalogs and fall back to this English manual
for long-form documentation. The in-app **Manual** page follows the language
selected in its header.

> [!IMPORTANT]
> Start in **dry-run**. Verify paths, source access and one test title before
> enabling active downloads. The UI is an unauthenticated administrative
> interface: keep it trusted or protect it with a firewall and authenticated
> HTTPS reverse proxy.

## How to use this guide

Gextto is a single daemon: it searches sources, evaluates releases, manages
libtorrent, renames files and archives them. Normal operation does not require
separate orchestrator processes.

The web interface works on a phone too: below 900 px the shell switches to a
compact layout with a scrollable top navigation, the dashboard metrics in two
columns, full-width dialogs and tables that scroll horizontally inside their
panel. Every action stays reachable by touch.

The official UI is served at `http://<host>:5000/`; the old `/v2` prefix
redirects to the root and the old `/ui` route no longer exists. The **Downloads** page refreshes
the session automatically every 5 seconds; the **Auto: on/off** button can disable
or re-enable polling. CPU/RAM tiles and the transfer rates in the top bar use the
same 5-second refresh.

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

The current suite runs 12 axe-core/Playwright checks on the main sections,
including dialogs, keyboard sorting, polling and 320 px reflow. This result is
not a WCAG or legal-compliance declaration: an attestation also requires manual
screen-reader, keyboard, magnification and assistive-technology testing, plus
an assessment of the applicable requirements. See
[`accessibility-analysis.md`](accessibility-analysis.md) for the detailed scope
and known limitations.

This manual distinguishes between:

- **manual search**: inspect results and queue one choice;
- **automatic cycle**: search monitored titles and decide what to download;
- **archive**: files already imported or stored in the library;
- **torrent session**: downloads still managed by libtorrent.

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

**Security.** The web port is an unauthenticated administrative interface. Keep
it on loopback, or restrict it with a firewall/reverse proxy before exposing it
to a network. In the standard install, `/opt/gextto/gexttod --version` tells you
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

- If no data directory exists yet, complete the initial setup wizard.
- **Active vs dry-run**: dry-run never starts real downloads; enable *active
  mode* in *Configuration → Daemon* only when you are ready.
- Add series/movies from **Explore** (TMDB) or **Series / Movies → Add**.

### Recommended first cycle

1. In *Configuration → Paths*, check the download, temporary, library and trash
   directories.
2. In *Configuration → Sources*, add one working source and click **Verify**.
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

- `gexttod --version` — installed version, build number, release marker and
  bundled libtorrent;
- `gexttod --help` — usage summary;
- `gexttod --update` — download and install the latest payload;
- `gexttod --config <file>` and `gexttod --dry-run` — used by the service and for
  local tests.

#### Terminal TUI

Start the TUI with `/opt/gextto/gexttod tui` after starting the daemon (use your
installation directory if different). It is a separate
process that uses only the daemon's HTTP API and SSE stream: it never reads the
databases directly and can be used from an SSH shell.

```bash
/opt/gextto/gexttod tui
/opt/gextto/gexttod tui --url http://host:5000 --lang en
GEXTTO_URL=http://host:5000 /opt/gextto/gexttod tui
```

The options are `--url`/`-u` and `--lang`/`-l`; the URL environment variable is
`GEXTTO_URL`. The seven tabs
are Status, Torrents, Logs, Health, Archive, Missing and Blocklist. Full key,
prompt and action documentation is in [`docs/tui.md`](tui.md).

**Installation and updating.** Every push to `main` publishes a tested
`continuous` Linux payload. The official installer downloads that payload,
verifies its SHA-256 checksum and installs it without compiling Go or C++ on the
target server:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

The installer must run as root and requires systemd. Use `GEXTTO_REPO` and
`GEXTTO_RELEASE` to select another repository or release. It stores the service
data in `/var/lib/gextto`, the program in `/opt/gextto`, and the generated API
token in `/etc/gextto/gextto.env`.

For a no-root installation from a source checkout:

```bash
make build
GEXTTO_DATA_DIR="$HOME/gextto-data" \
  GEXTTO_LISTEN=127.0.0.1:5000 \
  scripts/install-user-service.sh
systemctl --user status gextto.service
```

The user service listens on port 5000 on all interfaces by default. Restrict
access with a firewall or reverse proxy; use
`loginctl enable-linger "$USER"` if it must run after logout.
The installer and `gexttod --update` install the same release payload.
`--update` downloads `gextto-linux-<arch>.tar.gz`, verifies the
published `.sha256` when present, stages the files, then swaps the executable
(with the embedded web UI), the bundled `lib/` and `run.sh` with atomic renames.
Data and
configuration in `GEXTTO_DATA_DIR` (default `/var/lib/gextto`) are never touched:
a failed download, checksum or extraction leaves the running installation
untouched, and a failed swap is rolled back. The `VERSION` marker next to the
executable is updated and shown by `--version`.

The official repository currently publishes Linux assets for `x86_64`. An
`aarch64` asset works when supplied by a custom repository via `GEXTTO_REPO`.

- `--channel stable` / `--release <tag>` — choose the release to install;
- `--install-dir <dir>` — install elsewhere (default: the binary's directory);
- `--archive <file>` — install from a local archive (offline);
- `--force` — reinstall even if the version is unchanged;
- `--no-restart` — do not restart `gextto.service`.

Run `/opt/gextto/gexttod --update` as root to restart `gextto.service`
automatically; otherwise the exact
`systemctl` command is printed. The systemd unit is not overwritten, so local
customisations (user, ports, paths) are preserved. The same payload is the
standalone Linux package described in the README (*Standalone Linux package*).

## 2. Dashboard

- **Interface controls** — the top bar offers theme, text size and a **Font**
  dropdown. It applies a generic preset, a detected local font or a manually
  entered family to the interface and logs. The choice is local to the browser;
  installed-font detection depends on browser support for the Local Font Access
  API and may require permission.
- **Manual global search** — searches archive + indexers + web engines.
- **Cycle buttons** — run a full cycle or a single domain (Series, Movies,
  Comics) or a backup now.
- **Stat cards** — configured series/movies, downloaded files, free space,
  archive magnets, session torrents, seen-from-feed groups.
- **Network and active downloads** — CPU/RAM plus a live network sparkline.
- **Consumption and disks**, **upcoming releases**, **last downloads**,
  **recent activity** and **latest finds from sources**.

### Manual search from the Dashboard

Manual search is useful for understanding what Gextto sees before changing a
configuration or starting a download:

1. enter a title or technical query;
2. wait for sources to finish, or for slow-source timeouts to appear;
3. compare title, source, quality, size and seed/peer counts;
4. use **Filter loaded results** to narrow the list locally;
5. open **Why not this one?** on interesting results;
6. click **Queue** only after checking the reason and archive comparison.

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

## 3. Downloads

- **Add** a magnet/.torrent URL or upload a `.torrent` file; optionally set a
  save path, “Download now” and “Do not rename”.
- **Downloads in session**: download/upload rate, torrent and peer counts; the
  list refreshes automatically every 5 seconds.
- Comic HTTP downloads show status, progress, downloaded bytes and speed in the
  same list as torrents.
- **Tag filter** and **temporary speed limits** (DL/UL for N minutes).
- **Table columns**: Name, Status, Progress, ↓, ↑, ETA, Peers, Ratio — click a
  header to sort. **Bulk actions**: pause, resume, recheck, remove.
- **Row actions**: pause/resume, recheck, details, remove.
- **Details** tabs: General, Tracker, Content, Peers, Limits, Storage; includes
  copy magnet, per-torrent limits/seed-days, reannounce, pin, restart,
  mark-failed, move storage. **General** also offers **Export .torrent**,
  **Super seeding** and web-seed add/remove; **Tracker** lets you edit the whole
  list (`tier|url` per line); **Content** sets the **per-file priority**
  (Skip/Normal/High/Maximum).
- When you use **Move storage**, the log records the request, destination and
  command acceptance; the final outcome is written when libtorrent completes or
  rejects the move. With **Check**, the log distinguishes command start from
  completion and includes state and verified bytes.
- **Download history** lists finished downloads (native completions and
  migrated ones). Columns: name (with a **NAS** badge when the file has been
  archived), type/season/episode, **NAS tag** (folder rule), score, status
  (*Completed*), the **library/NAS path** and the completion time.

### Stalled torrents

When a torrent stops increasing its completed-byte count for the configured
period, it enters **stalled** state. Gextto pauses it in libtorrent too, so it no
longer occupies an active slot. It remains in the session and is resumed and
reannounced on the next retry. The values are under *Configuration → libtorrent*:

- **Consider stalled after** — 60 minutes by default;
- **Stalled retry** — 60 minutes by default;
- **Stalled removal** — 20160 minutes (14 days) by default, `0` = never.

Peers without byte progress do not reset the timer. In the log, look for lines
with `is stuck at`, which also give the reason, then `is still stuck` at each
retry and `is downloading again` when it recovers; `Gave up on` appears only
after the final limit.

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
seeding limits. Removing a torrent from the session does not necessarily remove
the file from the library.

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
- A program that edits the video **in place** (rewrites bytes in the same file,
  like some taggers or `mkvpropedit`) would also change the seeding file. Gextto
  never does that; if you run such tools on the library, turn the option off.

Turn it off under *Configuration → Seeding and completion → Hardlink instead of
copy while seeding*.

Trash must be configured under *Configuration → Paths*. Without a NAS
destination, Gextto does not treat the Download directory as an archive and does
not move the folder to Trash automatically. The automatic move of the source
folder to Trash happens only with a NAS destination, a valid **Trash**
destination and automatic removal of completed torrents enabled; when Trash is
not configured the source files are never deleted automatically (only the entry
is removed from the torrent session).

## 4. Series

Add a series via TMDB search or manually (title, quality, languages, seasons,
aliases, exclusions, NAS path, subtitles, timeframe).

- The list shows Name, Seasons, Quality, Language, Episodes (downloaded/total),
  Completeness, Last download; filter it with the search box.
- Bulk actions: set language, delete selected.
- **Series detail**: poster/plot/cast, badges (year, network, status,
  completeness), cast links to TVDB/TMDB and, if configured, a **“disabled
  seasons”** badge. The archive path is shown in the header and the edit form
  includes **Browse** for choosing a server-side folder.
- Actions: search missing, scan archive, refresh from TMDB, rename preview /
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
3. choose quality, language, subtitles and monitored seasons;
4. set a per-series archive path only if you do not want the global path;
5. save in dry-run and inspect the detail page.

The most important fields are:

| Field | Effect |
|---|---|
| Quality | restricts acceptable releases and contributes to the score |
| Language | requires the configured language when it is recognisable in the title |
| Subtitles | controls preference without accepting hardcoded subtitles |
| Seasons | decides which seasons are monitored; ranges are supported |
| Aliases | links alternate names to the same series |
| Exclusions | words in a title that must block the release |
| No upgrades | a better release no longer replaces the archived files of this series |
| Anime (absolute numbering) | for series whose releases number episodes without seasons, see below |

**Anime.** Many anime releases number episodes from the start, without seasons:
`[SubsPlease] One Piece - 1071 (1080p)`, `One Piece Ep 1071 SUB ITA`,
`One.Piece.1071.SUB.ITA`. With **Anime (absolute numbering)** on, Gextto
recognises these titles for the series, maps the number to season and episode
using the TMDB seasons (episode 30 with seasons of 12, 12 and 24 = S03E06) and,
when it looks for a missing episode, also searches for the absolute number.
`Title S01E1071`, produced by some indexers, is read as an absolute number too
when season 1 has fewer episodes. Without a TMDB key the absolute number is used
as an episode of season 1. Only series marked this way are affected: for the
others a title with a number is never read like this.

Leave future seasons enabled if you want calendar and missing searches to keep
working. Use *Ignore* only for episodes that should no longer be searched;
reactivating them makes them candidates again in later cycles.

### Missing episodes and packs

From **Search missing** or an episode detail:

1. check that the season is monitored;
2. search for the single episode or the series domain;
3. compare individual releases and season packs;
4. if an episode exists on disk but not in the database, scan the archive;
5. use **Force** only for an intentional manual action.

Gextto normally avoids downloading older episodes when later episodes already
exist, unless this is a recognised gap or a genuine upgrade. A season pack can
fill several episodes, but archive comparison still happens episode by episode.
When the same cycle offers a single episode and a pack containing it with the
same score, Gextto picks the release covering more episodes (at equal score a
REMUX is still preferred).

## 5. Movies

- Tabs **Monitored / Downloaded**; sortable columns (name, year, quality,
  language).
- The editor supports quality, base language, subtitles, exclusions and
  **Required languages**. The form shows a readable value such as `ita,eng`, not
  the internal JSON representation.
- **Movie detail**: poster, plot, cast with TMDB links, edit, re-download,
  **Search now**, a “best matches” table from the sources and an **archive
  matches** table where each row offers **Why not this one?**.

### Adding and selecting a movie

1. find the movie in **Explore** and check year and original title;
2. add it to the library;
3. set quality, language, subtitles and exclusions;
4. use **Search now** to inspect results without waiting for a cycle;
5. open **Why not this one?** before queueing a borderline release.

Movies are identified by title and year when available. Avoid creating duplicates
with different spellings: correct the monitored movie metadata instead of adding
it again.

Required languages are entered as comma-separated codes, for example `ita,eng`.
The database/API may keep the canonical JSON form, but the UI converts it to a
readable value before displaying it. Internal rules distinguish mandatory
requirements from optional preferences; do not edit the JSON manually unless you
are using the API directly.

## 6. Explore, Archive, Comics

- **Explore** — TMDB trending/today/popular/top-rated/now playing/upcoming, TMDB
  search, generic release search, add-to-library. Cards are shown in a five-column
  wall; a title already in the library carries a **Già in lista** badge and its
  add button is disabled, so you never create a duplicate by mistake.
- **Archive** — full-text search of past releases with pagination, batch queue,
  copy magnet, delete; **Series/Movies from feed** tabs. The **Source** column
  shows only the provider/domain (the full URL is in the tooltip), and every row
  offers **Why not this one?** like the search table.
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
  weekly-pack settings and resend/delete/force history are also available. Direct
  HTTP downloads appear in **Downloads in session** with status, bytes, progress,
  speed and pause/resume actions.
  In **CBZ** files downloaded directly (HTTP or Mega) Gextto adds a
  `ComicInfo.xml` with series, issue number and year taken from the title (e.g.
  “Poison Ivy #41 (2025)”): Komga, Kavita and tablet readers use it to group and
  sort issues. A file that already has its own `ComicInfo.xml` is left alone;
  comics downloaded by torrent stay identical because they are seeding, and CBR
  (RAR) files cannot be modified.

## 7. Configuration

Tabs: **Daemon, Sources, libtorrent, Torrent engine, Scores, Rename, Advanced,
Acquisition, Notifications, Paths, Translations**. Unsaved changes are
highlighted with a “Save all” bar. The complete per-tab list of every entry is in
[Appendix A](#appendix-a-reference--configuration).

- **Sources** — RSS feed list, indexers (Jackett/Prowlarr) with a *Verify*
  button, FlareSolverr URL + test, web engines, content filters, blacklist.
  For Jackett, use its base URL, for example `http://host:9117`, together with
  the API key. Gextto builds the Torznab endpoint
  `/api/v2.0/indexers/all/results/torznab/api`. The health check uses `t=caps`
  with the same key, so it checks the API rather than only Jackett's home page.
  Results may identify the source as `jackett:TrackerName`.
- **libtorrent** — connections/performance, protocols/trackers, security,
  RAM disk and ports, speed limits and scheduler; apply/optimise/update check.
  If you move a torrent's storage to a folder that **already holds the data**,
  it is not rejected: Gextto associates the torrent with the existing files and
  re-checks them (seeds) instead of re-downloading. **Preallocate disk space**
  (on by default) reserves the full size of every new
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
  group and set its bonus/penalty), and a live simulator. The group name must
  match the release's final group tag, for example `TBK`. Audio language is a
  per-title eligibility requirement, not a fixed Italian bonus, so English can
  be preferred instead. Preferred subtitles add a small optional bonus without
  making a release mandatory. One effective score is used for acquisition,
  searches, upgrades, post-processing, archive records and rescoring; it also
  includes the size bonus. Use **Maintenance → Rescore** after changing weights.
  - **Rename** — rename enable, template editor with tokens and live preview,
  TMDB/TVDB keys, language and upgrade thresholds. Verification recovers
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
   also removed. The databases are compacted with `VACUUM` afterwards. Archive
  retention is separate and is configured under **Configuration → Advanced**.
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

### Torrent engine (embedded, qBittorrent-nox)

Gextto always keeps its own database, queue, scoring, post-processing, renaming
and archive; only the transfer plane is pluggable, chosen in
*Configuration → Torrent engine*. A torrent is owned by a single engine at a
time, so switching is a controlled migration, not two clients working on the
same data.

- **Embedded libtorrent** (default) — the bundled in-process session; every
  *libtorrent* setting applies.
- **qBittorrent-nox** — Gextto drives an existing qBittorrent-nox through its Web
  API. Set the URL, user/password, category, tag and poll interval, and the
  `local=remote` **path mappings** whenever the two processes see different
  paths; with no mappings Gextto checks that the required paths exist locally. If
  a path cannot be translated, activation is refused so a move can never target
  the wrong folder. **Managed mode** downloads the latest qBittorrent-nox static
  release, installs it in the application folder (next to `gexttod`), starts and
  stops it with the service and updates it with backup and rollback; a watchdog
  restarts it after an unexpected exit and, after repeated crashes, falls back to
  the embedded engine. If qBittorrent is down at startup, Gextto keeps retrying
  instead of failing.
If the saved configuration selects a backend that is no longer supported,
Gextto falls back to the embedded engine and logs a warning.

The configuration tile shows the active engine, its status and a
reachability/path test, so you can validate a backend before switching.
Whichever engine is active, the **Downloads** screen and every automation stay
the same.

### Performance: RAM and CPU

The daemon's own logic is negligible; with libtorrent active almost all the cost
is the engine while it has torrents in the session. If you use an external engine
(qBittorrent-nox), that cost lives in its own process instead. To lower RAM and
CPU:

- **RAM** — the values that matter are the **disk cache** (`cache_size`, 16 KiB
  blocks) and `max_queued_disk_bytes`. The **Optimise** button (or *continuous
  optimisation*) sizes them to the host RAM; the values suggested by
  `/api/system/lt_mem_suggest` work too. The daemon returns memory to the OS
  (`malloc_trim`) after completions, after every cycle and every 15 minutes, so
  the RSS does not stay at the download peak.
- **CPU** — enable the **dynamic queue** and *Do not count slow torrents in
  active slots*: zero-rate torrents do not hold a slot and **stalled** ones are
  paused and retried instead of spinning. If CPU is busy, lower
  `connections_limit` and `aio_threads`. Fewer active torrents — and no "dead"
  torrent without seeders — mean less DHT/tracker churn.
- **Diagnosis** — `GET /api/torrents/{hash}/why` explains why a torrent is not
  downloading (`dead_swarm`, `no_peers`, `no_connected_seed`, `stalled`, …);
  *Health* shows the real process and session RAM/CPU.

### Configuring a Torznab indexer

For Jackett:

1. create or verify at least one indexer inside Jackett;
2. copy the API key from the Jackett page;
3. in Gextto click **+ Jackett**;
4. enter the base URL, for example `http://jackett:9117`, and the API key;
5. save and click **Verify**;
6. check *Health → API status*.

Gextto automatically uses `/api/v2.0/indexers/all/results/torznab/api` for
Jackett. Do not append that path when using the base URL. If you use a custom
full Torznab endpoint, keep it configured as an explicit endpoint. Prowlarr uses
its own search endpoint and normally uses port `9696`.

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

In *Configuration → Acquisition* you can see level, deadline and last error. Use
the per-source reset after fixing the cause; do not use it to hide a wrong API
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
title, use **Allow upgrades** in the series/movie editor.

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
  (`/api/maintenance/clean-trash` with `force=false`).
- **Video duplicates** — *Preview duplicates* and *Clean duplicates* find video
  files clearly inferior (strictly lower resolution) left next to the best
  version in the same folder, e.g. an old 480p next to the new 1080p, and move
  them to the trash. The check uses the filename and technical data declared in
  the name; it does not compare file contents or calculate hashes. At the
  **same resolution** the version matching the
  preferred language (*Configuration → Rename → default language*) is kept and a
  duplicate that explicitly declares a different language is moved to trash;
  files with no language tag are left untouched. Files of torrents still in the
  session are protected, and emptied sub-folders are removed. The rename
  verification also performs this cleanup automatically, and upgrade cleanup now
  also honours the "hard" upgrade reasons (resolution/source/HDR/repack).
- **Restore source** — *Restore source* remounts the `[Source]` token (WEB-DL,
  HDTV, BluRay…) in archived names that lost it, recovering it from the original
  release title in the database. It never invents a source: unknown stays
  untouched. Preview first, then execute; no re-download is involved.
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
media archives or the complete libtorrent session state. Before restoring:

1. stop or pause automatic cycles;
2. keep a copy of the current database;
3. verify the backup date and size;
4. restore only to a compatible installation;
5. check paths and permissions before enabling downloads again.

Restoring a database does not automatically move media files. If the library was
moved, fix paths or scan the archive before starting upgrades and missing searches.

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
- **Web logs** — live SSE stream with text filter, line count and follow/pause.
  Lines are English and explicitly formatted as `date time  LEVEL message ·
  key: value`, with highlighted keywords (NAS, download, sources, filters,
  errors). Every cycle prints a **SOURCE REPORT** with each source's outcome,
  the filter decisions, and download events (start, metadata, completion, any
  NAS move and archive import). Torrent messages always include the readable
  name or title; torrent hashes are hidden from the user-facing log.
- **Charts** — CPU/RAM/download/upload/disk/ram-disk sparklines and daily
  consumption.
- **Activity** — recent torrent events and downloads.

### TUI: quick usage

In the TUI, `1`-`7` or `Tab` switch tabs, `r` refreshes and `?` opens help. In
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

## 11. Notifications

Configure Telegram, e-mail (SMTP) or a webhook (with HMAC secret) and send a
test. Completion notifications include size, download time and average speed.

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
| Change names without re-downloading | Rename preview |
| Fix an external service | Health → source → Verify |
| Remove inferior files | Preview duplicates → Cleanup |
| Save configuration and databases | Backup |

### Glossary

- **Release**: a result found by a feed, indexer or web engine.
- **Cycle**: one automatic search and selection pass.
- **Gap**: a missing episode recognised in the archive.
- **Placeholder**: a temporary row representing an incomplete download.
- **Upgrade**: replacing an archived file with a better release.
- **Backoff**: a progressive pause for requests to a failing source.
- **Seed**: sharing a torrent after completion.
- **NAS**: network destination used for the library or configured paths.

## 13. Troubleshooting

- **A source is unreachable** — check *Configuration → Sources → Verify* and the
  sources health panel. For Jackett verify the base URL, API key and that at
  least one indexer is enabled in Jackett. Torznab errors are detected even when
  Jackett returns HTTP 200. Cloudflare-protected sites need a working
  FlareSolverr.
- **Jackett is reachable but shows an API error** — copy the API key again, check
  that at least one indexer is enabled in Jackett, and verify that the URL in
  Gextto is the correct base URL. Do not append
  `/api/v2.0/indexers/all/results/torznab/api` twice.
- **Nothing downloads** — confirm *active mode*, that the series/movie is
  enabled, and check the quality/language filters and the free-space guard.
- **A torrent is stalled** — check the three values under *Configuration →
  libtorrent*. It is intentionally paused and excluded from active slots; wait
  for the retry or use **Resume/Restart** manually.
- **A watched-folder file is not imported** — keep the `.torrent` or `.magnet`
  extension. Gextto waits for size and timestamp stability, then retries import
  errors automatically; check the watcher log.
- **A file is not renamed** — `mediainfo` should be installed (technical tags);
  check *Rename* settings and the TMDB key.
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
- **FTP backup fails** — use *Test FTP*: it reports the failing step (connection,
  login, remote path, upload, delete) and logs it.
- **Logs** — see `data/gextto.log` (rotated at 5 MB) or the in-app log viewer.

## Appendix A. Reference — Configuration

Each tab collects the editable settings. The *What it does* column mirrors the description shown in the UI.

### Daemon

| Setting | What it does |
|---|---|
| Automatic series/movie search (seconds) | Interval between automatic series/movie searches, in seconds (21600 = 6 hours). |
| Max release age (days) | Ignore releases older than N days (0 = no limit). Releases without a date are treated as published today. |
| Max gaps per series/cycle | Maximum gaps to search per series in one cycle (0 = unlimited). |
| Gap filling enabled | Enable filling recognised gaps (missing episodes) from available releases. |
| Deep search interval (hours) | How many hours between targeted live indexer searches for gaps. |
| Max deep searches per cycle | Maximum number of live searches per cycle. |
| Online title search in the cycle | Search titles on indexers during the cycle: auto (only when no feeds are configured), yes (always), no (never, use only feeds and the local archive). |
| Active | Enable or disable the Gextto daemon: automatic cycles and downloads. |

### Sources

| Setting | What it does |
|---|---|
| Blacklist (one word per line) | Forbidden words, one per line: releases containing them are discarded. |
| RSS feeds | List of RSS feeds read on every cycle (one URL per line). |
| Web engines | Web search engines used by gap-filling when feeds and indexers find nothing. |
| Content filters | Releases containing these words or scripts (e.g. [porno]) are excluded. |

### libtorrent

| Setting | What it does |
|---|---|
| Client enabled | Enable or completely disable the built-in libtorrent client. |
| Automatic queue and resource management | Automatically adjusts how many torrents are active based on load. |
| Continuous optimization (periodic) | Periodically applies cache, buffer and queue optimization based on resources. |
| Preallocate disk space | Reserves all disk space up front before starting the download. |
| Minimum dynamic download slots | Minimum number of dynamic downloads. The queue changes by at most one at a time. |
| Maximum dynamic download slots | Maximum number of dynamic downloads. Consecutive consistent samples are required before increasing the queue. |
| Don't count stalled torrents in active slots | Torrents that are not transferring data do not consume an active slot. |
| Sequential download | Download files sequentially instead of in scattered pieces. |
| Active downloads | Base value for active downloads; with the dynamic queue it is adapted at runtime. |
| Active seeds | Base value for active seeds; with the dynamic queue it drops to 1 when downloads are queued. |
| Active torrents limit | Base value for the active torrent limit; with the dynamic queue it becomes max(base, downloads + seeds + 2). |
| Total connections limit | Maximum simultaneous peer connections at session level. |
| Upload slots | Number of unchoked upload peers (-1 = automatic). |
| Half-open limit | Maximum half-open connections (-1 = automatic). |
| Max connections per torrent | Connection limit per torrent (-1 = unlimited). |
| Max uploads per torrent | Upload limit per torrent (-1 = unlimited). |
| Disk AIO threads | Dedicated threads for disk operations (-1 = automatic). |
| Disk cache (blocks, -1 auto) | Disk cache size in blocks (-1 = automatic). |
| Cache expiry (s) | Seconds of inactivity after which a block leaves the cache. |
| Alert queue | Libtorrent alert queue size. |
| DHT | Enable the DHT network to find peers without a tracker. |
| PEX | Peer Exchange: exchange peers with other clients. |
| LSD | Local Service Discovery: finds peers on the local network. |
| UPnP | Opens router ports automatically with UPnP. |
| NAT-PMP | Opens router ports automatically with NAT-PMP. |
| uTP | Enable the uTP (UDP) protocol in addition to TCP. |
| Prefer RC4 | Prefers RC4 encryption on connections. |
| Announce to all trackers | Announce to all trackers, not just the first of each tier. |
| Announce to all tiers | Announce to all tiers, not just the first. |
| Multiple connections per IP | Allows multiple connections from the same IP address. |
| Announce interval (s) | Minimum interval (seconds) between two announces to the same tracker. |
| Connect boost | Number of extra connection attempts when the torrent starts. |
| DHT bootstrap nodes | Initial DHT nodes (host:port separated by comma). |
| Encryption | Encryption policy: 0 disabled, 1 enabled, 2 forced. |
| Apply IP filter | Apply the IP filter to trackers as well. |
| IP filter (file/URL) | Local file or URL of the IP blocklist. |
| Listen interfaces | Where libtorrent accepts connections: 0.0.0.0:6881-6891 for all interfaces, 127.0.0.1:6881 local only, or wg0:6881/tun0:6881 for a VPN. The suggested value works in most cases. |
| Outgoing interface | VPN killswitch: interface used for all outgoing BitTorrent traffic. |
| RAM disk folder | RAM disk to use for in-progress downloads, if available. |
| Use the RAM disk | Downloads to RAM the torrents that fit the threshold; larger ones go to disk. |
| Max size per torrent (GB) | Maximum size of a single torrent allowed on the RAM disk (GB). |
| Free margin to keep (GB) | Free space to leave on the RAM disk once the download completes (GB). |
| Minimum free space (bytes, 0 = from margin) | Minimum free space in bytes required to use the RAM disk. 0 = use the configured margin. |
| Min port | Minimum libtorrent session port (requires service restart). |
| Max port | Maximum libtorrent session port (requires service restart). |
| Global download (KiB/s, 0 = unlimited) | Global download limit in KiB/s (0 = unlimited). |
| Global upload (KiB/s, 0 = unlimited) | Global upload limit in KiB/s (0 = unlimited). |
| Speed schedule enabled | Enable the time window with different speed limits. |
| Schedule — start time (HH:MM) | Schedule start time (HH:MM). |
| Schedule — end time (HH:MM) | Schedule end time (HH:MM). |
| Schedule — days (0=Mon … 6=Sun, e.g. 0,1,2,3,4) | Active days: 0=Mon … 6=Sun (e.g. 0,1,2,3,4). |
| Schedule — download (KiB/s) | Download limit in KiB/s during the schedule. |
| Schedule — upload (KiB/s) | Upload limit in KiB/s during the schedule. |
| Advanced libtorrent settings | Advanced libtorrent settings, one per line in key=value format. |
| Tab actions | **Optimize** computes cache and buffers based on RAM; **Apply now** immediately reapplies the settings to the active session. |

### Torrent engine

| Setting | What it does |
|---|---|
| Torrent engine | Active torrent engine (built-in libtorrent or qBittorrent-nox). |
| qBittorrent-nox — Web API URL | qBittorrent-nox Web UI URL (e.g. http://127.0.0.1:8080). |
| qBittorrent-nox — username | qBittorrent-nox Web UI username. |
| qBittorrent-nox — password | qBittorrent-nox Web UI password (not shown). |
| qBittorrent-nox — category | Category applied to torrents added to qBittorrent-nox. |
| qBittorrent-nox — tag | Tag applied to torrents added to qBittorrent-nox. |
| qBittorrent-nox — request timeout (seconds) | Timeout in seconds for HTTP requests to qBittorrent-nox. |
| qBittorrent-nox — polling interval (ms) | Interval in milliseconds between torrent status reads. |
| qBittorrent-nox — path mappings | Path mapping between Gextto and qBittorrent-nox, one per line (local=remote). |
| qBittorrent-nox — downloaded and updated by Gextto | Gextto downloads the latest qBittorrent-nox release itself, installs it in the application folder (next to gexttod), starts/stops it with the service and updates it (with backup and rollback). The engine in use is still chosen in “Torrent engine”: this option does not change it. |
| Tab actions | Install/Optimize qBittorrent-nox, qBittorrent-nox status, Apply engine and Test connection. |

### Scores

Weights are grouped into: resolution (2160p/1080p/720p/576p), source (BluRay, Remux, WEB-DL, WEBRip, HDTV, DVDRip), codec (H.265, H.264), audio (TrueHD, DTS-HD, DTS, DDP, AC3, 5.1, AAC, MP3), bonus (Dolby Vision, HDR, PROPER, REPACK, REAL). For each: **higher = more preferred**. Custom groups are added here. Use *Maintenance → Recalculate scores* after changing weights.

### Rename

| Setting | What it does |
|---|---|
| Rename episodes | Renames downloaded files using TMDB metadata. |
| TVDB language (e.g. ita, eng) | Preferred language for TVDB metadata (e.g. ita, eng). |
| TMDB language (e.g. it-IT) | Language used for TMDB metadata (e.g. it-IT, en-US). |
| Default language (e.g. ita) | Default preferred language for series and movies (e.g. ita, eng). |
| Upgrade cleanup | Replaces lower versions already archived with better upgrades. |
| Min score difference for cleanup | Minimum score difference to replace an existing file with a better one (cleanup). |
| Min score difference for upgrade | Minimum score difference to replace a file with a better upgrade. |
| Stop upgrading above this score (0 = never) | Upgrade ceiling: once the library file scores at least this much it is no longer replaced by better releases; only a REPACK or PROPER, which fixes a defective release, is still accepted. With the default scores: 1080p WEB-DL H.264 ≈ 1280, 1080p WEB-DL H.265 DD+ ≈ 1480, 2160p WEB-DL ≈ 2480. It adds to the minimum difference: that one says *by how much* a release must improve, this one *up to where*. To stop upgrades for a single title use “No upgrades” on its page. “Why not this one?” says when a release is skipped because of this ceiling. |
| TMDB API key | TMDB API key for titles, posters and metadata. |
| TVDB API key | TheTVDB v4 API key for series search and metadata. |
| Rename format | Template editor with tokens and preview to compose file names. |

### Advanced

| Setting | What it does |
|---|---|
| Minimum free space to download (GB) | Minimum free space (GB) on the download folder: below this threshold the cycle does not start downloads. |
| Trash — retention days (0 = delete everything) | Retention days for non-forced cleanups; 0 deletes the whole trash content. Manual UI actions always empty the trash immediately. |
| Automatic archive cleanup | Enable automatic archive cleanup according to max age and minimum number to keep. |
| Archive — max age (days) | Maximum age of archive releases, in days (0 = no limit). |
| Archive — keep at least N entries | Minimum number of recent releases to always keep in the archive. |
| Feed pages to read | How many listing pages to read per feed (3 is a good compromise). |
| Rename verification (hours) | How many hours between checks that archived/renamed files are still present. |
| Debug (detailed logs) | Enables detailed logs and periodic diagnostics for debugging. |
| Per-source filters | Keywords to accept or reject for a single source, enabled per row. |
| Tag → folder rules | Associates a torrent tag with a temporary and a final folder. |
| Event hooks | Runs a program on selected events (name, events, program, arguments, timeout). |
| Watched folders | Automatically adds the .torrent/.magnet files found in the given folders (recursive, delete after). |

### Acquisition

| Setting | What it does |
|---|---|
| Series delay (minutes, 0 = none) | Delays the start of series downloads by this many minutes. 0 starts immediately. |
| Movie delay (minutes, 0 = none) | Delays the start of movie downloads by this many minutes. 0 starts immediately. |
| Bypass the delay above this score (0 = never) | If a release reaches at least this score, the configured delay is ignored. |
| Periodic housekeeping enabled | Enables periodic cleanup of technical data and history. |
| Housekeeping — interval (hours) | Interval between two automatic housekeeping runs, in hours. |
| Housekeeping — search-cycle statistics kept | Number of search cycle statistics to keep. |
| Housekeeping — feed-seen entries (days, 0 = never) | Deletes feed-seen release history rows older than N days. |
| Housekeeping — download history (days, 0 = keep) | Deletes download history rows of removed torrents older than N days. |
| Housekeeping — error entries (days) | Deletes error torrent entries older than N days (minimum 1). |
| Housekeeping — gap search log (days, 0 = never) | Deletes the missing-episode search log older than N days. |
| Housekeeping — upgrade backups (days, 0 = never) | Deletes backups of files replaced by upgrades older than N days. |
| Automatic MediaInfo backfill | Periodically analyses with ffprobe the files already present that have no MediaInfo yet. |
| MediaInfo backfill — interval (minutes) | Minutes between two MediaInfo backfill passes. |
| MediaInfo backfill — files per run | Maximum number of files analysed in each MediaInfo pass. |

### Seeding and completion

| Setting | What it does |
|---|---|
| Consider stalled after (minutes) | After this many minutes without progress the torrent is considered stalled. |
| Stalled retry (minutes) | Interval between reannounce attempts for stalled torrents. |
| Stalled removal (minutes, 0 = disabled) | After this period without progress, the torrent is removed automatically. Set 0 to completely disable automatic removal due to stalling. |
| Global seed ratio (0 = infinite) | Upload/download ratio after which to stop seeding (0 = infinite). |
| Maximum seed time (minutes, fallback) | Seeding limit in minutes, used only when Maximum seed (days) is 0. |
| Max seed (days) | Primary seeding limit in days; when greater than 0 it takes precedence over the limit in minutes. |
| Remove completed items after seeding | On: at the end of seeding the completed torrent is removed from the session (like “Clean completed”). Off: at the end of seeding the torrent stays in the list as Completed and you remove it with “Clean completed”. It does not affect where files are moved. |
| Move episodes/packs to the archive (do not copy) | On: at the end of seeding the downloaded source is deleted (the file stays in the library). Off: the downloaded source is copied to the library and kept. |
| Hardlink instead of copy while seeding | On (default): a file that keeps seeding enters the library as a hardlink, without using the space twice; when downloads and library are on different filesystems it is copied. See “Hardlinks instead of copies”. |

### Notifications

| Setting | What it does |
|---|---|
| Telegram enabled | Send notifications to Telegram. |
| Telegram bot token | Telegram bot token (from @BotFather). |
| Telegram chat ID | ID of the chat/channel where notifications are sent. |
| Webhook URL | Webhook URL where events are sent. |
| Webhook secret | HMAC secret to sign webhook requests. |
| Email enabled | Send notifications by email. |
| SMTP | SMTP server as host:port (e.g. smtp.gmail.com:587). |
| Email sender | Sender address for notification emails. |
| Email recipient | Email recipients (comma-separated). |
| Email password | SMTP password/app password (not shown). |

### Paths

| Setting | What it does |
|---|---|
| Archive folder | Default archive folder for content without a dedicated path. |
| Trash folder | Folder where replaced/duplicate files are moved (if left empty, uses the trash subfolder in the data folder). |
| Cleanup action | What to do with replaced files: move to trash or delete. |
| Download folder | Default download folder for every engine. |
| Temporary folder | Temporary folder for in-progress downloads. |
| Copy .torrent files to | Copy the .torrent files of downloads here (empty = no copy). |

### Access

Gextto is meant for a trusted LAN: access is **open by default**. When you
reach it from outside (reverse proxy, port forwarding, VPN) you can require a
login from clients that are not on the local network.

| Setting | What it does |
|---|---|
| Require login | Off (default): no checks. On: clients outside the local network must log in or use the API key. Until a password or a key is set everything stays open (and the log says so). |
| No login from the local network | On (default): 127.0.0.1, 192.168.x.x, 10.x.x.x, 172.16–31.x.x and local IPv6 addresses need no login. Behind a reverse proxy the real client address forwarded by the proxy counts (`X-Forwarded-For`, `X-Real-IP`, `Forwarded`), so a client coming from the Internet through the proxy still has to log in. |
| Username | Login username (default `admin`). |
| Password | Stored only as a bcrypt hash; changing it closes every open session. Leave the field empty to keep it. |
| API key (scripts, TUI, calendar) | For non-browser access: header `X-Api-Key: <key>` or `?apikey=<key>` in the URL. The TUI reads it from the `GEXTTO_API_KEY` variable. |

A browser session lasts 30 days; `/logout` ends it. After five wrong passwords
from the same address further attempts are blocked for a minute and each failure
is logged. If you lock yourself out, start Gextto with the environment variable
`GEXTTO_AUTH_DISABLE=1`: access control is off while the variable is set and you
can fix the settings. For access from outside always use HTTPS (reverse proxy
with a certificate, or a VPN), otherwise the password travels in clear text.

### Translations

| Setting | What it does |
|---|---|
| Translations | Export/import the UI string translations as YAML for Italian or English; import adds or updates keys without deleting the others. |


## Appendix B. Reference — Integrations

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
| 📺 Series S02E03 · Title | **original broadcast** (TMDB) of the current season, from the last week to the next two months. It is not the date of a localised release; ✓ when the episode is already in the library. |
| 🎬 Movie | release of a monitored movie **in your country** (TMDB: digital, then home video, then cinema; the country follows the TMDB language, e.g. `it-IT`). When that country has no date yet the original one is shown, marked “(original release)”. |

A TMDB key is required; the calendar is rebuilt at most every 30 minutes. With
access control on (see *Access*) add `?apikey=<key>` to the address.

### Torznab indexers

| Field / action | What it does |
|---|---|
| Name | Indexer label (e.g. `jackett` / `prowlarr`). |
| Base URL | Base URL of the service; Gextto appends the Torznab path. |
| API key | Indexer API key. |
| Type | Auto-detected, Prowlarr or Jackett. |
| Enabled | Enables or disables the indexer. |
| Test | Tests the indexer (for Jackett it uses `t=caps`). |

### FlareSolverr

| Field / action | What it does |
|---|---|
| URL | URL of the FlareSolverr service used to bypass Cloudflare. |
| Test FlareSolverr | Checks that FlareSolverr responds. |

### Event hooks (Configuration → Advanced)

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
| Magnet / Torrent handler | Downloads the scripts to open magnets and `.torrent` files directly in Gextto. |
| Magnet / Torrent `.desktop` | `.desktop` versions for Linux desktop integration. |
| `install.sh` | Downloads the installation script. |
| Source check / Refresh | Checks feeds, indexers and web engines; with a query it also measures results, without changing settings. |

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
| Trash — Empty trash | Deletes every trash item. |
| Database cleanup — Cycles to keep | Number of cycle statistics to keep. |
| Database cleanup — Error days | Keeps error torrent entries for at least N days before removing them. |

### Backup

| Item | What it does |
|---|---|
| Backups to keep | Number of local ZIP files to keep in the `backups` folder. |
| Interval (hours) | Automatic backup interval in hours (0 disables the interval). |
| Time (HH:MM) | Local daily time of the automatic backup; when set it takes precedence over the interval. |
| FTP host | FTP server host/address (empty = no FTP upload). |
| FTP user | FTP server user. |
| FTP password | FTP password (not shown; empty keeps the stored one). |
| FTP path | Remote destination folder for the ZIP. |
| Cloud folder | Local path of a folder already synced by a cloud service. |
| Send to Telegram | Sends a copy to Telegram using the configured bot. |
| Test FTP | Checks connection, login, remote path, a test upload and removal. |
| Available backups / Verify | Table of snapshots with label, name, size and date; the Verify button checks the archive. |
