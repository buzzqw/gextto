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

## 20 October 2026

- **eD2k: filter the results and Scarica always there.** Above the eD2k search
  results (eD2k page and archive) there is a box to filter by words in the name,
  minimum sources, minimum size and already-known files, with sorting by
  sources, size or name: it works on the results already received, without
  running the search again. The **Scarica** button stays on every result, also
  in the release search, which no longer loses its results box after the first
  search.
- **Clearer top bar.** The status badges (Gextto, gx-torrent, aMule, speeds,
  torrents, peers and next cycle) have a row of their own and are wider;
  **Add torrent** stays beside them, while language, text size, font, theme and
  **Refresh** move to the row below.
- **eD2k: consistent results even with another search running.** aMule has a
  single result list, shared with its own interface and with every other
  connected tool: if another search replaces it while Gextto is searching,
  Gextto runs the search again and, if the interference continues, says so
  instead of showing unrelated files. It applies to the search on the eD2k page
  and to the archive search with **eD2k** ticked.
- **eD2k: aMule preallocation in the configuration tab.** **Configuration →
  eD2k and Kad** has the aMule panel with the **Preallocate the whole file in
  aMule** switch and **Recommended settings**: the value is applied to aMule at
  once (it is aMule's setting, not one stored in Gextto). With aMule in Docker
  the folders are checked inside the container: if they are missing there Gextto
  reports them but does not create them from outside.
- **eD2k: missing aMule folders reported and recreated.** When aMule's incoming
  or temporary folder is missing (a network disk unmounted, a folder removed),
  the eD2k page says so and **Create the folders** makes them again: the flow of
  *Failed to get file system statistics* in the log stops and completed files
  are imported again.
- **eD2k and Kad get their own Configuration tab.** The nine aMule/eD2k rows
  (connection, search network, fallback, stalled downloads) are no longer inside
  **Sources**: there is a dedicated **eD2k and Kad** tab, and the “Open the
  configuration” links land there.
- **eD2k: session totals, preallocation and warnings.** The eD2k page shows the
  bytes aMule exchanged in this session and whether aMule preallocates files;
  **Recommended settings** switches full preallocation off (the one that blocks
  `amuled` on network disks). **Health** gains an eD2k card with state, speeds,
  totals and the latest warnings from aMule's log.
- **Pieces: the map shows on a stopped torrent too.** Opening the details of a
  paused or parked torrent, the **Pieces** tab said “metadata not yet present”
  even at 99% downloaded. The verified pieces are there: the map now shows them,
  with the missing ones in grey.
- **Search: eD2k results can be queued too.** In the manual search, eD2k results
  showed “Search only” with no **Add**, as if they could not be downloaded. They
  now queue like torrents: the `ed2k://` link goes to aMule. With the
  integration off the eD2k results do not appear, instead of sitting there
  without an action.
- **Search bar: the button is one line tall again.** **Open eD2k links** is no
  longer as tall as the link field: it keeps a normal button height.
- **eD2k: the box says eD2k, and the link button stays on one line.** In the
  Downloads search (and the other search boxes) the checkbox is no longer
  called “Kad” but **eD2k**: the network is the one chosen in Configuration →
  Sources → eD2k and Kad (Kad, servers or both), as its tooltip now says. The
  **Open eD2k links** button no longer wraps: the link field takes the available
  width and the button stays on one line.
- **eD2k page with tabs, and aMule in the TUI.** The eD2k page now has three
  tabs — **aMule and search**, **Shared files** and **aMule log** — instead of
  the panels at the bottom of the page. In the TUI, with eD2k on, the **Status**
  tab shows the **aMule** line (version, server, High/Low ID, Kad, upload and
  queue) and aMule's downloads appear in the **Torrents** tab: `Enter` opens the
  detail with sources, priority, last data received and the `ed2k://` link, `y`
  copies the link, `p` pauses or resumes, `d` takes it off aMule.
- **Top bar: CPU and RAM per process.** The two CPU and RAM boxes are now one
  **Gextto** badge with `CPU% · RAM`, and next to it **gx-torrent** (the torrent
  engine, when the daemon is local) and **aMule** (when the eD2k integration is
  on and `amuled` runs on the same machine). Each badge refreshes every 5
  seconds like the other figures; a process on another host gets no badge,
  because those figures cannot be read from there.
- **eD2k: completed downloads reach the history at once.** An eD2k file that
  completed and was imported into the library appeared in the history only at
  the next reconciliation, up to six hours later. It is now recorded as finished
  as soon as the processing ends, like a torrent removed after completion.
- **eD2k: see what aMule is uploading.** The eD2k page has a **Shared files**
  panel: for every file the bytes uploaded in this session and over all time,
  requests and accepted ones, queued clients, priority and the `ed2k://` link to
  copy; below it, the clients you are uploading to with software, speed and
  queue position. **Reload shared files** asks aMule to rescan its folders, so
  files added or removed by hand show up at once; the shared folders are listed
  read-only.
- **Several episodes in one file: the name carries the range.** When a download
  covers contiguous episodes (for example `S01E01E02`), the library file is
  renamed `S01E01-E02`: Jellyfin and Plex list both episodes and the archive
  scan registers the file under each of them. A non-contiguous range keeps the
  first episode in the name.
- **eD2k: movies completed by aMule reach the library.** A movie downloaded
  through aMule with the library folder configured was not imported: it stayed
  in a loop of failed moves. It is now copied or linked like an episode and goes
  through the same quality check before touching the NAS: if an equal or better
  version is already there, the download is discarded (to the trash) and not
  copied. With no library folder the movie stays in aMule's incoming folder and
  is marked completed.
- **eD2k and Kad move to aMule.** The `gx-ed2k` daemon is retired: for the
  eD2k/Kad network Gextto now drives **aMule** (`amuled`), installed and started
  on its own with external connections (EC) enabled. With aMule on you can
  **search** Kad or the eD2k servers, **paste `ed2k://` links** (more than one,
  even mixed with text) and **actually download them**: aMule's downloads appear
  in **Downloads** next to the torrents and the HTTP downloads, with progress,
  speed, sources and the pause, resume and remove buttons. When aMule completes
  a file, Gextto renames it and imports it into the library like a torrent (by
  hardlink or copy, so aMule keeps sharing it). The new **eD2k** page (Discovery
  menu) shows aMule's state (server and high/low ID, Kad, speeds, network), the
  server list with a button to connect, the commands to connect and disconnect
  eD2k and Kad, the download of `server.met` and `nodes.dat`, the search and the
  box for links. **What to do:** in Configuration → Sources → eD2k and Kad switch
  on **eD2k and Kad through aMule** and enter the address, port and password of
  aMule's external connections (the old gx-ed2k URL and token options and the
  UPnP one are gone). The automatic gap-filling fallback stays a second switch,
  off by default. An update removes the old `gx-ed2k` binary.
- **eD2k: completions, stalled downloads and non-video links.** Files completed
  by aMule 3.x (which drops them from its queue at once) are recognised and
  imported, also after a Gextto restart. An eD2k download chosen by Gextto that
  receives no data for 7 days (option **Give up on a stalled eD2k download
  after**, 0 = never) is cancelled in aMule, blocklisted and the episode is
  searched again; downloads added by hand and paused ones are left alone. A link
  to a file that is not a video stays aMule's download only and no longer ends up
  in quarantine. The old extto and gx-ed2k settings are deleted (aMule's
  address, port and password are kept). The manual explains how to run aMule in
  Docker and open the ports with UPnP.
- **eD2k: a link aMule already has is still followed.** When aMule answers that
  the file is already listed, or is slow to answer while it creates a large part
  file, Gextto checks the queue and, if the file is there, registers it and
  imports it into the library when it completes. With aMule on a network disk
  set `AllocateFullFile=0` in `amule.conf`: preallocation blocks aMule for tens
  of seconds.
- **eD2k: bandwidth, details, log, counters and TUI.** aMule follows the same
  bandwidth limits as the torrents (rule, exception, schedule). In Downloads ⓘ
  opens an eD2k download's details, with sources, last data received, link to
  copy and the priority in aMule. The eD2k page shows aMule's log; the dashboard
  and the menu count eD2k downloads too, and the TUI lists them with pause,
  resume and remove. The automatic fallback searches Kad and the eD2k servers
  together. Fixed an error that marked an eD2k episode as failed right after its
  library import and kept the automatic fallback from starting a download.
- **eD2k: Kad + servers, Archive and dashboard.** The eD2k page and **eD2k search
  network** offer **Kad + eD2k servers**, which runs both searches and merges the
  results. In **Archive** the **Also search eD2k** box shows aMule's results
  below the archive, with **Download**. On the dashboard the traffic chart has
  dashed lines for aMule, apart from the torrents', and the top bar also adds
  aMule's upload.

## 19 October 2026

- **eD2k/Kad search, when torrents are not enough.** An optional search
  provider, off by default, searches Kad and eD2k servers through the `gx-ed2k`
  daemon (pure Go, our own). In manual search the eD2k results appear next to
  the torrent ones with a network badge and the source count; a pasted
  `ed2k://` link is checked and shown with name, size and hash. Configuration →
  Sources has an **eD2k and Kad** group: the switch, the daemon URL and token, a
  second switch to use it in gap filling only when torrents find nothing, the
  source floor and the timeout. The automatic fallback is off and accepts only
  an exact episode match with a recognised quality. On the **Sources** page the
  **eD2k and Kad** entry shows a connection sensor (**open**, **limited** by
  firewall/NAT, **unknown**) and the daemon tries to open the Kad port with
  **UPnP** (UDP 4672 and TCP 4662): when it works eD2k servers assign a HighID.
  Learned Kad nodes are saved and reused on restart. **There is no eD2k
  transfer**: the daemon says it can only search and the add button stays
  disabled, so the library cannot fill with wrong files. The **Downloads**
  **Search a release** panel has a **Kad** checkbox: tick it to include the
  network in that single search, next to DB, web and indexers. If the provider
  is off, that search starts it without changing the global setting. The automatic
  fallback also queries the series' **alternate titles** (for example
  "Murdoch Mysteries" for the Italian title), so a localized name does not hide
  releases published under the original one.
- **Standalone gx-ed2k, with updates.** The search daemon's Linux archive now
  ships with the other releases (same download folder) and its page has
  **Check for updates**: it compares the build with the latest published one
  and, when newer, links to the archive for the right architecture. A
  **Windows** archive (`gx-ed2k-windows-<arch>.zip`) is published too, with
  `install-service.ps1`, which registers the service and opens UDP 4672/TCP
  4662 in the Windows firewall. On Linux the check can also **install** the
  update (checksum and version verified, old binary kept, automatic restart);
  on Windows the zip download remains. A build compiled from local source
  offers nothing.
- **Safer updates.** An update, from the **Update** button or by running the
  installer again, succeeds only when the new version's web interface answers.
  If the new version goes down a few seconds after starting, the previous one
  comes straight back: before, it counted as a success and the service stayed
  down. Running the installer again keeps the user of the installed service,
  instead of switching to the `gextto` account when run as root without `sudo`.
- **Traffic chart.** On the Dashboard the part of the last 30 minutes with no
  data yet stays empty, instead of a ramp rising from zero to the first value.
  Speeds under 1 KB/s count as zero: that is just protocol chatter, and the
  chart stays flat instead of twitching.
- **The Dashboard bandwidth tells the truth.** With **Auto** speed the
  **In session** card shows the **Auto** badge instead of "No limit": the
  speed is not free, Gextto is measuring the line and adapting to it.
- **Clearer startup logs.** The lines that restore saved state (stalled
  downloads, metadata waits, pending moves) now list the torrents involved, not
  just how many there are, so it is immediately clear what they refer to.
- **Translations.** The download counter reads "Torrents: 5 · HTTP: 0", right in
  every language; the engine tile on the Dashboard is called "Engine" in English
  and "Motor" in German.
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
