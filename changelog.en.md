# Gextto changelog

This is what changes in Gextto from the point of view of the people who use it,
in date order. Entries are grouped by day and by visible effect: commits that
only build, test, document or maintain the project internally do not get a
separate line, but are represented in the entry of the change they accompany.
Technical detail stays in the commits and in `docs/`.

The installed version is the one shown by Gextto (`1.1.<build>`). The updated
manual is online: [English](docs/MANUAL.en.md) ·
[italiano](docs/MANUAL.it.md). For reports:
[GitHub issues](https://github.com/buzzqw/gextto/issues).

## 19 October 2026

- **Speed: a rule and an exception.** In Configuration → Speed and network a
  single choice, **How the speed is decided**, sets the rule: Auto, fixed limits or
  hourly schedule. With the schedule you also pick whether Auto or the fixed limits
  apply outside the windows. Your settings stay as they were: nothing to redo. In
  Downloads the **Bandwidth** menu says what applies now and where it comes from,
  and makes an **exception**: Auto or a limit, for 1, 3, 6 or 12 hours or until you
  remove it. The exception always beats the rule, schedule included: a limit you
  set is no longer overridden by a schedule window. The mechanism that measures
  the line is called **Auto** everywhere.
- **A tidier Downloads page.** Filter, tag and sorting share one row; **Clean
  completed**, **Orphaned files** and the automatic refresh are in the **Tools**
  menu. Pause, Resume, Check again, tags and Remove from session appear only when
  you select torrents, and the selection is no longer lost when the list refreshes
  on its own. "Remove completed items after seeding" left the toolbar: it is set in
  Configuration → Seeding, and the Tools menu takes you there. "Unlock pin" is in the
  pinned torrent's details; the list marks it with a 📌 badge.
- **Bandwidth menu.** The temporary-limit fields at the top of the panel give way
  to a **Bandwidth** button that shows the limits in force. From its menu you set
  Auto or a download or upload limit on the fly (64 KB/s to 10 MB/s) and its
  duration, in one click. Exact values stay under **Custom values…**.

- **Discarded packs leave no wrong traces.** When the inspection decides that a
  downloading pack is not needed, Gextto restores the previous version of the
  episode: the library no longer keeps the quality and title of the discarded
  release over the old file.
- **Much clearer decision logs.** An upgrade download says what quality is
  already in the library (`from: S01E01 480p h264 ...`); a discarded pack shows
  the episode-by-episode comparison; a partly useful pack lists the episodes
  kept and those skipped.
- **Readable metadata wait.** The “nobody provided the file list” message now
  reports peers, bytes received and the start time; when the list arrives, a
  line says so; if the counter restarts, the log explains why.
- **The metadata wait survives restarts.** If a magnet is waiting for its file
  list and Gextto restarts, the clock resumes where it was: after 24 hours
  without a list it is dropped as a dead link, instead of getting a fresh
  window at every restart.
- **Real quality versus recorded release.** If the file in the library and the
  recorded title disagree (for example a 480p file with a 1080p title), the
  quality check reports it and says that the file wins.
- **Manual and changelog online.** The **Manual** and **Changelog** entries open
  the updated documentation on GitHub in a new tab, in the interface language:
  Italian for the Italian UI, English for the others. Updating the manual and
  changelog no longer requires rebuilding Gextto; the old `/?view=manual`
  address leads to the online manual.

## 18 October 2026

- **Pack inspection while downloading.** Gextto reads the file list as soon as
  it arrives, compares the episodes with the library and discards a pack that
  adds nothing; with the dedicated option it downloads only the useful files,
  not the whole pack.
- **Dead `.torrent` links set aside.** A link that fails in two different cycles
  is quarantined and no longer blocks the search; a magnet the indexer cannot
  serve yet is not treated as a dead link.
- **More consistent Auto speed.** The Auto switch follows the configuration and
  search timeouts are diagnosed without filling the log with warnings.
- **Lighter engine.** gx-core no longer lowers the descriptor limit and does not
  keep an unnecessary file pool.

## 17 October 2026

- **Tidier search.** Jackett and Prowlarr are queried one indexer at a time;
  dead links are quarantined; the archive is indexed by torrent (infohash) and
  not by the magnet text.
- **Finer seed rules.** Seeds can be stopped by source or category and an idle
  seed limit can be set; the state survives a restart.
- **More observable API.** Prometheus metrics, a session-bound event stream and
  qBittorrent 5 compatible calls for gx-torrent.
- **Less wasted memory.** The engine releases the memory of the IP filter and
  uTP buffers after use; the peer reader no longer spins on dead uTP connections.
- **Diagnostics on demand.** Opt-in pprof profiles on loopback only, shared by
  Gextto and gx-torrent.
- **Backup and restore verified** end to end together with the release packages.

## 16 October 2026

- **Livelier dashboard.** A new 30-minute bandwidth chart, a single “In session”
  frame and “To check” warnings that stay until the problem is resolved, with a
  Clear button.
- **Auto speed and time window.** Day mode has three states, the “Auto” window
  is handled by the auto-tune and the proposed speed takes the observed peak
  into account.
- **More guided configuration.** Easy/Expert levels, an “Automatic management”
  card that adapts, provenance badges for values and a tuning system that
  proposes changes instead of applying them silently.
- **Season packs only when useful.** A pack is downloaded only when it contains
  at least one missing episode or a real upgrade.
- **More honest scoring.** Swarm health enters the candidate choice; archived
  episodes are scored from the real file and the original-language title avoids
  placeholder titles.
- **Safer downloads.** Approval and torrent add go through a single crash-safe
  path.

## 15 October 2026

- **Poster library.** TV series and movie lists can be shown as cards with
  poster, status and progress, on phones too.
- **More comfortable torrent detail.** The detail opens from the name and becomes
  a bottom panel with engine statistics and a working “re-download”.
- **More complete gx-torrent standalone.** More menu, Wanted with season packs,
  feed folders, interactive episode search, archive maintenance and
  accessibility checks.
- **More precise notifications.** A season pack distinguishes new episodes from
  upgraded copies.
- **Support scripts.** Smart `/tmp` cleanup with a systemd timer and
  `gx-core-test.sh` using a local build beside production.
- **User log in English** and less noise in the status heartbeat.

## 14 October 2026

- **Access keys from the UI.** Gextto API keys and the gx-torrent token can be
  generated and copied from the interface.
- **Engine fallback.** If the configured engine is unavailable, Gextto can use
  qBittorrent-nox on demand; the build with embedded libtorrent is no longer
  shipped.
- **gx-torrent keeps growing.** Statistics page, alternative VueTorrent
  interface, automatic torrent management (autoTMM), watch folders, torrent
  creator, cookies, alternative limits, RSS and search APIs.
- **Opt-in DLNA/UPnP media server** for completed content, with library folders
  and subtitles.
- **In-app update** for gx-torrent standalone too.
- **Per-day bandwidth scheduler** in the Gextto UI and gx-core version visible in
  the header.

## 13 October 2026

- **Swarm Merging.** The engine reuses data already present in other releases or
  layouts of the same content, even when a sibling appears later.
- **WebSocket tracker** (`ws`/`wss`) and **DHT feeds** (BEP 46).
- **More responsive uTP.** Adaptive RTO with a minimum-latency floor and
  loss-driven backoff; retransmission and SYN-drop counters in both UIs.
- **Multi-piece pipelining**: a peer can download two pieces at once.
- **Disk guard**: downloads pause when the folder is running out of space.
- **New gx-torrent UI** on the shared design system, standalone log and process
  CPU/RAM.
- **Clearer Wanted**: Have/Missing status and episode management in the dialog.

## 12 October 2026

- **gx-torrent standalone usable from the LAN**, with a dedicated Wanted tab,
  instant feed check and manual search.
- **FlareSolverr** for feeds and indexers; the archive becomes the main index and
  indexers are used for gaps.
- **Custom themes** with user-chosen colours and a third “cream” theme; feedback
  when the trash is emptied.
- **Anti-SSRF**: web seeds and magnet sources cannot reach the LAN.
- **Engine**: opt-in mmap storage with `fdatasync`, DHT retried every 15 seconds
  on a cold start, graduated file priorities 0–7, Swarm Merging phase 1, BEP
  44/46.
- **Downloads**: “Clean completed” no longer blocks on a huge trash copy; global
  seed limits are honoured.
- **UI**: two-column torrent detail, complete provider status, Torznab address
  consistent between Gextto and gx-torrent.

## 11 October 2026

- **More resilient DHT**: optional per-subnet node cap (anti-Sybil), tokens bound
  to the infohash, routing-table concentration visible.
- **NAT**: mapping type inferred from DHT observers and PCP (RFC 6887) support.
- **Latency-based auto-speed** (anti-bufferbloat).
- **More powerful RSS**: scoped feeds, folders, episode filters, smart episode,
  REPACK/PROPER, disableable rules and “follow a series”.
- **BEPs**: 54 (`lt_donthave`), 53 (`ws=`), 21 (`upload_only`), 38, 31.
- **Standalone**: wizard, indexers, RSS, limits and scheduler visible in the UI.
- **Security**: hardening of create-torrent, login redirect and session cookies.

## 10 October 2026

- **Windows**: native service, zip package, junction instead of symlinks,
  cross-volume move and wizard; gx-torrent also downloads and compiles on
  Windows.
- **BitTorrent v2** (BEP 52): metainfo, file tree, SHA-256 piece layers, magnet
  and v2-only torrents.
- **Dual-stack IPv6**: UDP tracker, `peers6`, PEX, DHT.
- **qBittorrent API** ever more complete, RSS, webhook notifications, bandwidth
  scheduler and standalone mode with login/CSRF/LAN.
- **Six languages** for UI, TUI and notifications; accessibility and tooltips.
- **Observability**: cycle duration and sources, threshold alerts, “To check”
  panel.
- **Abandoned packs**: already completed episodes are brought into the library
  before removing the torrent.

## 9 October 2026

- **Safer installer**: service running as the local user, update with rollback,
  uninstall, dry-run and self-test in CI.
- **Guided setup**: language, folders with Browse, TVDB as an alternative to
  TMDB, port and bandwidth tests.
- **In-app update** and portable package.
- **Pure-Go build by default**, with libtorrent opt-in.
- **Settings**: “Default” button on every field, gx-torrent as the default
  engine, options that do not match the engine are disabled.
- **Holepunching BEP 55** for gx-torrent.
- **TVDB** fully supported as an alternative to TMDB.
- **Notifications** on Gextto restart/crash and gx-torrent problems.
- **First/last piece** as a global setting; fixes to rename, seed and season
  packs.

## 8 October 2026

- **Notifications to Discord, Slack, ntfy, Gotify and Pushover**, in addition to
  Telegram and webhooks.
- **Tidier v2 UI**: Configuration by areas, sticky headers, jobs panel only when
  needed, grouped Integrations.
- **gx-torrent**: BEP 16 super-seeding, HTTP streaming with Range, per-torrent
  limits, per-piece diagnostics, web seeds and trackers editable at runtime.
- **gx-torrent survives Gextto restarts** and has its own build number, rotating
  log and a panel in the Gextto UI.
- **Database**: no rewrite on every restart or stall tick.
- **Security**: integer conversion and path hardening.

## 7 October 2026

- **gx-torrent becomes the default engine**, with parity with libtorrent: save
  paths, bandwidth, queues, categories, uTP, LSD, IP filter, cache, single peer
  port, file selection and stall handling.
- **gx-torrent UI**: working web page, torrent detail, statistics, LAN, IP
  filter, token moved to the Access tab.
- **Optional login** for clients outside the LAN.
- **iCal calendar** of releases, original broadcasts and movies.
- **Anime**: absolute episode numbering.
- **Comics**: `ComicInfo.xml` also in CBZ files downloaded directly.
- **Archive**: hardlink seeding files into the library instead of copies.
- **Upgrades**: score threshold above which a copy is no longer replaced.
- **Jellyfin/Plex**: rescan only the changed folder.
- **TUI**: new library, downloads, detail and maintenance handling.

## 6 October 2026

- **More complete TUI**: library, downloads, torrent detail and maintenance,
  with a layout suited to narrow terminals and low traffic.
- **Telegram backup** split when it exceeds the API limit.
- **Notifications** without duplicates and without alerts for damaged pieces.
- **Trash and cleanup**: orphan files in the temporary folder go to the trash;
  sparse files stay sparse.
- **Movies**: single-video torrent folders archived as flat files; leftover
  folders renamed.
- **Stalls**: the wait state survives restarts.
- **Plain-language log** and names of the files read by the MediaInfo backfill.
- **Scores** recalculated when weights change.

## 5 October 2026

- **Archive**: episode matching in subfolders, trash as default, write probe and
  upgrade notice; post-seed finalisation.
- **Cycle**: placeholder rollback if the download is skipped, RAM-disk budget
  respected, dry-run and trash more consistent.
- **UI**: table search that stays after an action, no form flicker, visible
  Prowlarr status.
- **Stalls**: persistent retry state and a warning after three hours.
- **REPACK** preferred when replacing the archive.

## 4 October 2026

- **Library**: specials preserved, sources deduplicated, episode search in a
  modal, FTS index rebuilt when out of sync.
- **Series**: alphabetical order by default and search actions on the page.
- **Dashboard**: upcoming titles linked to TMDB/TVDB.
- **Health**: mircrew-indexer service row and provider links.
- **Maintenance**: SQLite integrity and indexes.
- **Mobile**: more readable bottom navigation and improved download layout.
- **Torrent detail**: clearer tabs and stall logs.

## 3 October 2026

- **Maintenance**: VACUUM and optimisation with start/completion toasts, correct
  database sizes and verified integrity.
- **Archive**: “archive and remove” option, completion notifications and content
  cleanup.
- **Content filters** applied before archiving.
- **Scores**: custom groups at the bottom and corrected modifiers.
- **Installer** and PayPal support.
- **Log**: individual download starts and seeding summary.

## 2 October 2026

- **Trakt and the Anacrolix backend removed**: gx-torrent and qBittorrent-nox
  remain.
- **Mobile**: bottom tab bar, simplified top bar, torrent cards and Log always
  reachable.
- **Torrent safety**: safeguards on removal, trash routing, temporary file
  cleanup and concurrency locks.
- **Post-processing**: lower-quality duplicates, season packs, MediaInfo fallback
  and trash.
- **CodeQL** and v2 log selector fixed.
- **Search**: gap search and torrent results improved.

## 1 October 2026

- **The v2 UI becomes the official interface**: dashboard, downloads, archive,
  series/movie detail, search, logs and configuration in SSR + HTMX.
- **Dashboard**: clock, live metrics, downloaded movie history, progressive
  search and upcoming releases.
- **Cycle**: priority to gaps already in the archive, search progress and manual
  monitoring summary.
- **Providers and backup** clearer in checks.
- **TUI**: accessible library tab and expanded metrics.
- **Dozens of UI fixes** for alignment, tables, modals and mobile.

## 30 September 2026

- **Episode sources** in a modal in the series detail.
- **Engine-aware settings**: options that do not match the active engine are
  disabled.
- **Seed and archive**: seed before archiving, `auto_remove_completed` honoured,
  seed ratio and archive cleanup effective.
- **Housekeeping**: configurable retention.
- **UI**: job manager for long operations, table accessibility, selectable fonts
  and TMDB calendar at the top of Explore.
- **FlareSolverr**: expired and orphan sessions destroyed.
- **Mobile** more essential.

## 29 September 2026

- **New languages**: German, French, Spanish and Polish.
- **UI**: performance tiles on mobile, history pagination, comics history,
  archive actions, tooltips and diagnostics.
- **Installer**: self-test, safe updates, rollback, uninstall and dry-run.
- **Indexers, FlareSolverr and RAM disk** improved.
- **MediaInfo backfill** more reliable.
- **Log**: connection test results at INFO, fewer duplicates and technical
  details.
- **Concurrency, deterministic shutdown and data integrity**.
- **Sonarr/Radarr replacement plan** documented.

## 28 September 2026

- **Complete migration to the server-rendered UI**: all 16 pages, forms, tables
  and actions.
- **Mobile-first**: phone layout, touch targets, fonts and pastel theme.
- **Library**: series/movie forms with quality, languages, subtitles, aliases
  and NAS path; detail with cast, next episode and seasons.
- **Archive and Explore**: TMDB search, calendar, episode sources, “in library”
  badge.
- **Managed qBittorrent** with watchdog and fallback.
- **TUI**: archive/missing/blocklist tabs, key editing and improved rendering.
- **Shutdown** of workers before destroying the session.
- **Cloudflare/FlareSolverr** more robust.

## 27 September 2026

- **First public Gextto release.**
- **New Go server-rendered UI**: migration of the 16 pages, i18n editor, library,
  archive, blocklist, maintenance and integrations.
- **Torrent engines**: selectable qBittorrent, Anacrolix backend, capability
  matrix and connectivity checks.
- **Security**: CodeQL fixes, contained paths, integer conversion and secret
  handling.
- **Bilingual TUI** Italian/English.
- **Logs and tooltips** clearer.
- **Dependencies** updated and UI bundle rebuilt.
