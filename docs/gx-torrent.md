# gx-torrent: the default torrent engine in pure Go

`gx-torrent` is a small BitTorrent daemon written in pure Go on top of the
**gx-core** engine (`internal/gxcore`). It is the default engine of
Gextto (`torrent_backend = gx-torrent`): it has no C/C++ dependency,
runs in a separate process and is controlled via REST (`127.0.0.1:8890` when
Gextto manages it, `0.0.0.0:8890` in standalone), where it also exposes an
operational web page openable from the browser (managed: only from this server,
see *Web interface* to open it to the LAN; standalone: already reachable from
another PC on the LAN). In short, as a standalone client it is a **simpler,
reduced version of Gextto** — fewer features, but all the ones needed to run
without it. Gextto has one other selectable engine,
qBittorrent-nox (external or managed): it is the automatic fallback when
gx-torrent does not start — Gextto tries the managed qBittorrent-nox (downloaded
on demand) and, as a last resort, a recovery state with the transfers stopped
(see `engine_fallback.go` and the Gextto manual).

The gx-core engine lives in `internal/gxcore`. Its features are
described in `internal/gxcore/GEXTTO.md`: single port, outgoing interface,
proxy, IP filter and file selection. The **state of
compliance with the BitTorrent specifications (BEP)** and the roadmap are in
[BEP compliance](bep.md).

Code:

| File | Role |
|---|---|
| `cmd/gx-torrent/main.go` | flags, startup, signals, clean shutdown |
| `cmd/gx-torrent/daemon.go` | gx-core session, persistent state, queue loop, operations |
| `internal/queue/queue.go` | queue scheduler (pure function) and dynamic queue |
| `cmd/gx-torrent/storage.go` | per-torrent symlink, safe move and deletion |
| `cmd/gx-torrent/api.go` | REST API with token |
| `cmd/gx-torrent/api_integrations.go` | Prometheus metrics, qBittorrent 5 calls |
| `cmd/gx-torrent/sse.go` | event stream shared by `/ui/events` and `/api/v1/events` |
| `cmd/gx-torrent/network.go` | port, interfaces, proxy, encryption, IP filter |
| `cmd/gx-torrent/portmap.go` | opening the port on the router (UPnP, NAT-PMP) |
| `cmd/gx-torrent/selection.go` | file selection |
| `cmd/gx-torrent/cache.go` | adaptive cache (available RAM, load, storage type) |
| `cmd/gx-torrent/stream.go` | HTTP streaming (Range) with piece readahead |
| `cmd/gx-torrent/ui.go` | daemon web pages, live fragments and actions |
| `cmd/gx-torrent/i18n.go` | web page translations (it/en/de/fr/es/pl) |
| `cmd/gx-torrent/lsd.go` | local network discovery (BEP 14) |
| `cmd/gx-torrent/tracker_health.go` | removal of trackers that never responded |
| `cmd/gx-torrent/lifecycle.go` | rotated daemon log, orphan watchdog, Gextto log queue |
| `gxtorrent_engine.go` | Gextto-side `TorrentEngine` adapter |
| `gxtorrent_runtime.go` | startup and supervision of the daemon by Gextto |

## Index

- [Startup](#startup)
- [Web interface](#web-interface)
  - [Network exposure](#network-exposure)
- [Standalone use](#standalone-use)
  - [Reverse proxy and allowed hosts](#reverse-proxy-and-allowed-hosts)
  - [Container](#container)
  - [Feed archive](#feed-archive)
- [Network](#network)
- [uTP and LSD](#utp-and-lsd)
- [Holepunching (BEP 55)](#holepunching-bep-55)
  - [Verification](#verification)
- [File selection and priorities](#file-selection-and-priorities)
- [Sequential download (streaming)](#sequential-download-streaming)
- [BitTorrent v2 torrents](#bittorrent-v2-torrents)
- [Local data reuse (Swarm Merging)](#local-data-reuse-swarm-merging)
- [DLNA media server](#dlna-media-server)
- [Statistics](#statistics)
- [Piece diagnostics](#piece-diagnostics)
- [HTTP streaming (HTTP Range)](#http-streaming-http-range)
- [Disk cache, preallocation and mmap storage](#disk-cache-preallocation-and-mmap-storage)
- [RAM disk](#ram-disk)
- [On-disk layout and data safety](#on-disk-layout-and-data-safety)
- [Self-managed queue](#self-managed-queue)
- [Stalled: division of tasks](#stalled-division-of-tasks)
- [Seed](#seed)
- [Super-seeding (BEP 16)](#super-seeding-bep-16)
- [Writes to the state disk](#writes-to-the-state-disk)
- [Bandwidth limits](#bandwidth-limits)
  - [Auto-speed on latency (anti-bufferbloat)](#auto-speed-on-latency-anti-bufferbloat)
- [REST API (v1)](#rest-api-v1)
- [API availability (snapshot and lock)](#api-availability-snapshot-and-lock)
- [Known limits (engine)](#known-limits-engine)
  - [IPv6](#ipv6)
- [Tests](#tests)

## Startup

1. `make build` (or `scripts/build-daemon.sh`) compiles `bin/gx-torrent` alongside
   `bin/gexttod`; `make gx-torrent` compiles only the daemon. The package, `install.sh`
   and `gexttod --update` install `gx-torrent` alongside `gexttod`. The daemon has
   its **own build number** (`gx-torrent.build_number`, committed), independent
   of Gextto's: it increases by one at every local recompilation and stays
   unchanged when the build does not touch it; CI reads the committed value, so
   the published daemon has the same number (`gx_version` in `release.json`). It is
   the version reported by `gx-torrent --version`, by its web page and by
   `/api/v1/health`: `<series>.<build>`, where the series lives in
   `gx_torrent.version_series` (raised by hand) and the third number is always
   the build; the full build is also the `build` field of `/api/v1/health`.
2. `gx-torrent` is the default engine of a new installation: there is no need to
   choose it. A configuration that has already saved an engine stays as it was:
   the default applies only to new installations.
3. Gextto **always starts and supervises** the daemon when gx-torrent is the active
   engine:
   - data in `DATA_DIR/gx-torrent`;
   - log in `DATA_DIR/gx-torrent/gx-torrent.log`, written by the daemon itself and
     rotated at 5 MB keeping 4 files; errors of individual peers (expired
     handshakes, resets) and of failed announces are at debug level;
   - default download folder equal to Gextto's configured download folder;
   - the daemon is restarted if it exits;
   - **it stays up when Gextto restarts** (for example for an
     update): under systemd it starts in its own scope
     (`gextto-gx-torrent-*.scope`), so stopping the service does not affect it.
     On restart Gextto re-attaches to it if it has the same binary and the same
     options (`fingerprint` in `/api/v1/health`): peers, queue and transfers
     continue without interruption. If the binary or a startup option have
     changed, Gextto stops it with SIGTERM (45 seconds of grace, the engine saves the
     resume data) and starts a new one. `make build` replaces
     `bin/gx-torrent` only if its code has changed
     (`bin/gx-torrent.code-sha256`), so an update that touches only
     gexttod does not stop downloads;
   - if Gextto does not come back, the daemon stops by itself after 15 minutes without
     requests (`-orphan-timeout`); if the active engine is no longer gx-torrent,
     Gextto stops the daemon left running at startup. Gextto re-attaches to or
     stops only a daemon with its own data folder;
   - after 6 abnormal starts/stops in 10 minutes Gextto stops retrying and runs
     the fallback chain (`engine_fallback.go`). It first restores `gx-torrent.prev`
     when the updater kept one, then hands the transfers to the managed
     qBittorrent-nox (downloaded on demand, or an external one already configured),
     and finally to a recovery state with the transfers stopped and the UI up.
     Only when no
     alternative is available does it continue with gx-torrent in **safe mode**
     (without uTP or holepunch) and growing waits between attempts (1 m, 5 m,
     15 m, 1 h). A clean shutdown (restart of Gextto or of the service, for
     example for an update) does not count: the budget considers only unexpected
     exits and failed starts; a daemon that stays up long enough
     resets it (10 minutes for the stop budget, 30 minutes to reset the
     growing waits). A daemon that is alive but stops answering its health
     endpoint for five minutes is killed and counted as a failure.

   If a gx-torrent already answers on the URL (for example a systemd service of yours),
   Gextto uses that one.

Settings (*Torrent engine* tab, *gx-torrent* group):

| Key | Default | Notes |
|---|---|---|
| `gxtorrent_url` | `http://127.0.0.1:8890` | URL with which Gextto reaches the daemon |
| `gxtorrent_listen` | `127.0.0.1:8890` | listen address of the managed daemon: by default page and API stay only on this server. To open them to the LAN see *Web interface → Network exposure*. The port is aligned to that of `gxtorrent_url` (Gextto must be able to reach it) |
| `gxtorrent_token` | empty | `X-Gx-Token` header; mandatory if the daemon listens on the network |
| `gxtorrent_auto` | `true` | self-management of gx-torrent: dynamic queue and adaptive cache. Disable it to set slots and cache by hand |
| `gxtorrent_request_timeout_secs` | `15` | 1–300 |
| `gxtorrent_poll_interval_ms` | `1500` | minimum interval between two state reads (250–60000) |
| `gxtorrent_proxy` | empty | proxy for gx-torrent: `socks5://[user:password@]host:port` or `http://host:port`. Peers, HTTP trackers and web seeds go through the proxy; DHT and UDP trackers are turned off. It travels in the daemon's environment, not on the command line |

The queue and bandwidth settings remain those of the *Speed and network* and
*Queues and performance* tabs:

- active downloads and seeds, total limit;
- "don't count slow torrents";
- dynamic queue with minimum and maximum;
- standard, scheduled and temporary speed limits.

Gextto forwards them to the daemon.

## Web interface

At the address indicated by `gxtorrent_url` (default `http://127.0.0.1:8890`) the
daemon does not expose only the REST API: `GET /` (or `/ui`) opens an **operational
web page** intended for whoever opens that address from the browser, like qBittorrent's
Web UI. It refreshes by itself every 2 seconds without reloading the page
(the rows travel as JSON, the filters and status as small HTML fragments).
The browser tab shows the daemon icon (a blue download arrow),
included in the page and also served by `GET /favicon.ico` without a token.
The page is **translated** (Italian, English, German, French, Spanish,
Polish): at startup Gextto passes the interface language to the daemon with
`-lang`, and `?lang=xx` overrides it for the single request.

The built-in page can be replaced by an **alternative web interface**
(*Settings → Web interface*): the daemon downloads the latest **VueTorrent**
release from GitHub into `<data>/vuetorrent` and serves it at the root address;
*Install or update VueTorrent* refreshes it to the latest release at any time.
The **standard page always stays available at `/ui`** (same login), so the switch
is reversible and the operator can never be locked out. The alternative
interface is a qBittorrent-compatible app driven only by `/api/v2`; a 404 on
VueTorrent's optional `/backend/ping` (a companion service that does not exist
here, as on plain qBittorrent) is expected.

All the daemon pages share one **design system** (`internal/webui/assets/theme.css`,
`common.js`, `icons.svg`, served under `/assets/` without a token so the sign-in
and setup pages use them too): colour tokens for a **light and a dark theme**
(automatic from the system, or forced with the switch in the top bar and
remembered in the browser), the same top bar and navigation on every page,
native `<dialog>` windows, cards, chips and toasts announced to screen readers.
Every page is checked with axe-core (WCAG 2 A/AA) in both themes.

- **Layout** (familiar to qBittorrent Web UI users, not a copy of it): a top
  bar with search (`/`), live speeds and the theme switch; the standalone
  navigation holds Torrents, Wanted, RSS & indexers, Archive, Statistics and
  Settings, plus a **More** menu beside Settings with **Log**, **Tools** and
  **Search indexers** (the main-page actions, opened here from every page);
  a **side bar** with
  collapsible sections (the collapsed ones are remembered): the **status
  filters** with their counts, always all listed (all, downloading, seeding,
  completed, running, paused, active, inactive, stalled, checking, moving,
  errored), the **categories** (all, uncategorized, each category) and
  **tags** (all, untagged, each tag) in standalone, the **trackers** (the host
  of each torrent's working tracker, plus *No working tracker*), and a
  two-minute **transfer graph**. The status counts come from the server with
  the same predicates as the page filter (`uiPageData`, `matchStatus`). A
  **toolbar** acts on the selection (resume, pause, remove, queue top/up/
  down/bottom) next to *Columns*, *Log* and *Tools* (IP filter, create
  `.torrent`, check feeds, test ports). The **status bar** keeps what matters
  at a glance — port reachability, DHT nodes, free space, the
  **alternative-limits toggle** (standalone), the update notice, and the
  **download/upload speeds with the global limits**: a click opens *Global
  speed limits* (`POST /ui/global-limits`, KiB/s, 0 = unlimited) — while the
  technical figures (totals, external address, incoming connections, IP
  filter, encryption, CPU and memory of the gx-torrent process, peer errors,
  uTP socket) are one click away in *Connection details*. The browser tab title
  carries the speeds, like qBittorrent.
- **Table**: one compact line per torrent. The **columns are chosen** by the
  user (*Columns* in the toolbar, or a right click on the header): name, size,
  progress, status, seeds and peers (connected, with the swarm the trackers
  report in brackets), speeds, ETA, ratio, category, availability, time active,
  last activity and wasted by default; tags, added
  on, completed on, downloaded, uploaded, remaining, queue position, tracker,
  save path on demand. Columns are **resizable** by dragging the header
  edge or with the arrow keys on it (double click restores the default);
  choice and widths are remembered in the browser and applied before the
  table is drawn, so a hidden column never flashes. Columns are sortable (the
  order and the filters are remembered); click / Ctrl-click / Shift-click
  selection; a **context menu** (right click, the ⋯ button or the menu key)
  with details, resume, pause, **force start**, remove, **set location**
  (`POST /ui/bulk-move`, several torrents at once), rename and a **category
  submenu** plus *Category and tags…* and **automatic management** (standalone),
  **limit speed** (`POST /ui/bulk-limits`: per-torrent limits for the whole
  selection, empty = follow the global limit), super-seeding, force recheck,
  force reannounce, **sequential download**, **first and last pieces first**,
  a *Queue* submenu (top, up, down, bottom), a *Copy* submenu (name, info hash,
  magnet) and export `.torrent`. The toggles show the state of the whole
  selection — ticked, unticked or **mixed** — and a click brings every selected
  torrent to the same state (the toggle reaches only the torrents that need
  it). Submenus open with the mouse, Enter or the right arrow and close with
  the left arrow or Escape. Keyboard navigation: arrows, Space to select,
  Enter for the detail, Del to remove, `P` to pause/resume, `N` to add,
  Ctrl+A. On a phone the rows become cards. The list refreshes every 2
  seconds (and at once on engine events) without losing selection, focus or
  scroll. The periodic refresh swaps only the rows: the table header, and with
  it the **select-all** checkbox, is never replaced, so a click is never lost
  to a refresh landing between the press and the release.
- **Add** in a dialog: several magnet links or `.torrent` URLs (one per line)
  and several files at once, destination (suggested from the existing save
  paths, with a *Browse* folder picker in standalone: `GET /ui/browse-dir` lists
  the daemon machine's directories so the path is chosen instead of typed),
  category (standalone) and the options (paused, top, sequential,
  first/last, super-seeding, per-torrent download/upload limits, automatic
  management). What is typed is previewed at once (magnet name and info hash,
  ignored lines); a `.torrent` dropped anywhere on the page or a
  magnet pasted outside a field opens it pre-filled.
- **Remove** asks in a dialog whether to delete the files too.
- **Per-torrent detail** in a panel below the list (like qBittorrent; full width
  on a phone) whose **height is resizable** from its top edge (drag, or the up
  and down arrows; remembered), and which follows the selected row. Tabs:
  *General* (actions, data, pieces, space, dates, pin, speed, connection,
  upload and seed limits, super-seeding, move, category and tags, **web
  seeds**), *Files* (progress bar per file, priority per file or for **all the
  ticked files at once**, **group by folder** with one checkbox and one
  priority control per folder, **rename** of files and folders, and **HTTP
  streaming** with a labelled *Stream* button), *Peers*
  (connection flags, transport, encryption; **add peers** by `ip:port` and
  **ban** a peer's IP for 30 minutes, `POST /ui/peers`), *Trackers* (status,
  swarm, add and **remove**), *Pieces* (coloured map). Peers, trackers and pieces refresh while
  the panel is open; the forms save in place, without reloading the page.
- **IP filter** from a URL or file (*Tools*); the field is pre-filled with the
  filter configured in Gextto.
- **Test ports** (*Tools*): checks that the peer port is listening and that the
  router forwards it (the same check as Gextto's *Test ports* button, on top of
  `GET /api/v1/portcheck`). With DHT active the result also shows the
  **reflected port** that the DHT nodes see for us (`reflected_port` field, from
  the `ip` field of BEP 42) and the deduced **NAT type** (`nat_mapping`).
- ***Log*** (toolbar): the last lines of the daemon's own log (200–2000), read
  only when you open it or press *Reload*, with text filter, DEBUG hidden by
  default and warnings/errors coloured. In standalone it reads the in-memory ring
  of the daemon's recent lines, so it works whether the log goes to stderr or to
  the rotating file already open in a terminal. In managed, when the daemon is
  launched next to Gextto, the same button shows *gextto.log*.
- **Large lists** (standalone only): above 500 torrents the table is
  **virtualised client-side**. `/ui/rows?rows=all` carries the whole session,
  filtering and sorting work on that data, and only the rows around the viewport
  are in the DOM (with spacer rows above and below), so a session of thousands
  of torrents scrolls and refreshes like a short one. Below the threshold the
  simple full-DOM table is kept, and without JavaScript the first 300 rows and
  *Show more* still work.
- The page's own actions post with `X-GX-Fetch: 1` and get a JSON outcome
  (`{"ok", "msg"}`, the message translated) instead of a redirect; classic form
  posts keep the redirect with `?msg=`.

The standalone pages follow the same structure:

- **RSS & indexers** (`/ui/rss`) in four tabs (*Feeds*, *Rules*, *Indexers*,
  *Options*; the tab is in the URL, so a save comes back to it). Each feed, rule
  and indexer is a card with its state and actions; the feeds are **grouped by
  folder**, each group opening with a header that folds it away (the folded
  folders are remembered in the browser: a list of thirty imported feeds stays
  navigable). The feed card shows the last
  check, its options as chips and opens its **items** (with the rule that takes
  each one, an *Add* button and *Add to my rules*, which opens the rule editor
  on that title, watching that feed). New and edited entries open an editor dialog;
  the rule editor is organised in *Which items* (the feeds it watches — one
  **checkbox per configured feed**, nothing checked meaning every feed, as in
  qBittorrent's RSS Downloader —, *Contains all of*,
  *Contains one of*, *Contains none of*, episodes, advanced filters) and *What
  to do* (save path, category, tags, flags), with the **preview** of the
  archived items it would take next to it. The preview **updates by itself**
  while the rule is typed and warns about what surprises: when a rule takes
  releases of **several titles** (a *one of* term like `1080p` alone takes every
  1080p release) it lists them with their counts and suggests putting the series
  name in *Contains all of*; when nothing matches yet it says the rule waits
  for new items. Every feed row says **what decides its downloads**: the rules
  that watch it, its own filter, *Take everything*, or *archive only*, and
  whether the wanted titles also take from it; the feed editor marks its
  fallback filter as **not used** (with the rules' names) when rules watch the
  feed, instead of leaving fields that would be ignored in silence. The feed
  editor takes a **pasted list** as well: one address per line (a name before the
  address when wanted, separated by a vertical bar), and it creates one feed per
  line with the options of the form (folder, filter, category, paused, disabled,
  *Take everything*), deriving a readable name from each address and skipping the
  lines already configured or unusable, which it then reports on the page.
  The options tab holds the refresh interval, the archive retention,
  FlareSolverr (with its test) and **Copy the setup**: *Export* downloads the
  feeds, rules and indexers as one JSON file (`GET /ui/rss-export`), *Import*
  loads such a file (validated) and replaces them; nothing is edited by hand.
  The Rules tab opens with a short *What are rules for?* explanation (a rule is
  a more powerful "add a series to Wanted"), and every feed row shows the
  **torrents it has brought into the archive** (not the items of its last
  read). The archive's feed filter lists only the configured feeds: the
  `wanted:<title>` buckets holding a wanted title's indexer results stay
  searchable under *all feeds*.
- **Wanted** (`/ui/wanted`): a card per title with type, filters as chips and a
  **state badge** (paused, downloading, to complete, up to date, waiting), and
  for a series the honest counts of what it has: how many episodes are
  **downloaded** (in the session and complete, or taken by the engine), how many
  are **downloading** (in the session, still running) and how many were **seen
  but not downloaded**, with a **chip per season**. A season only partly seen is
  never collapsed to a bare `S01` that reads as complete. The card lists
  episodes, never a bare count nor a false "complete" (a season is only as
  complete as the episodes seen so far), plus **Episodes** / search now / edit /
  pause / remove, and **Releases**: the releases of the title in the archive,
  each with its **verdict** — *Would be taken*, *Taken*, *Already in the
  client*, or the first filter that turns it away (season not wanted, not in
  the episode filter, no episode number, language, quality, excluded term) —
  with the counts per verdict (`GET /ui/wanted-preview?index=N`). The add and
  edit dialog shows the same preview **live** while the title and the filters
  are typed (`GET /ui/wanted-preview` with the form's fields): *what would be
  taken now* is never a surprise. A *Would be taken* release carries its own
  **Add**, which takes it now with the title's category and path; **Search the
  indexers now** (`GET /ui/wanted-preview?index=N&live=1`, and the dialog's
  *Search indexers too* checkbox) queries the indexers for the title and merges
  what they return into the same list — the interactive search for a release the
  automatic pass would not pick. The card also says when the indexers were
  last searched for the title. Episodes already in the client, whatever
  brought them there (a rule, a manual add), **count as downloaded**: the card
  counts them as *downloaded*, they are not *seen, not downloaded*, and the
  title does not download a second release of them. A movie shows its single
  release with the same states and links to its torrent. *Monitoring paused* in
  the dialog stops the searches for the title. **Episodes** opens the grid
  **inline** on the card: it lists the known episodes per season with their state
  (downloaded / downloading / missing, with a legend), a click marks one
  **downloaded** or **missing**, an episode in the session carries a **link to
  its torrent**, an episode can be added as missing, and **Force download
  missing** searches the indexers again for what is missing. *Add title* is a guided
  dialog: series or movie, the title with a search that shows how it is named in
  the archive, feeds and indexers (pick one to use it), then seasons, language
  (with quick picks), subtitles (terms matched on the release name, with quick
  picks like `sub ita`) and quality, and the other options folded away.
- **Archive** and **Settings** use the same top bar, tables and fieldsets;
  Settings keeps its *Save* button visible at the bottom while scrolling.
- **Archive database maintenance** (`Settings → Archive database`): *Delete old
  items* prunes the entries older than the given days (the same prune the Archive
  page does), and *Optimize archive* rewrites the bbolt file compactly, because
  the database reuses freed pages but never shrinks the file on its own; the
  result shows the size before and after. The compaction holds the archive lock
  for the swap, so no other transaction sees a half-replaced database.

The definitive command remains Gextto; the page is a convenience for
the operator.

### Network exposure

**By default page and API stay only on this server**
(`gxtorrent_listen = 127.0.0.1:8890`). To open them to the whole LAN set
`gxtorrent_listen = 0.0.0.0:8890` **and** a `gxtorrent_token`.

- The port of `gxtorrent_listen` is **aligned** to that of
  `gxtorrent_url`: Gextto must be able to reach the daemon, so a different port
  has no effect (it is reported in the log).
- A non-loopback listen requires `gxtorrent_token`: without a token Gextto starts
  the daemon in `-insecure` (consistent with Gextto's trusted-LAN default) and
  notes it in the log at debug level.
- With `gxtorrent_token`, the page asks for it on first access (it also accepts
  `?token=…`) and remembers it in a cookie; the API stays protected as before.

## Standalone use

```
gx-torrent [-listen 0.0.0.0:8890] [-data ~/.local/share/gx-torrent]
           [-download-dir DIR] [-token SECRET] [-allowed-roots /srv/media,/data]
           [-peer-ports 6881-6891] [-listen-interface IP|iface]
           [-outgoing-interface wg0] [-proxy socks5://host:port]
           [-encryption 0|1|2] [-no-dht] [-no-pex] [-no-utp] [-no-holepunch]
           [-no-lsd] [-no-upnp] [-no-natpmp] [-dht-bootstrap host:port,...]
           [-dht-read-only] [-web-sources-allow-private]
           [-ipfilter FILE] [-ipfilter-trackers=true] [-lang it]
           [-mode managed|standalone] [-notify-url URL] [-mmap]
           [-log-file FILE] [-insecure] [-debug] [-version]
```

`-mode` chooses how the daemon runs. Without the flag: **managed** if there is
`-fingerprint` (Gextto passes it), **standalone** otherwise. In managed the flags
passed by Gextto always take precedence and the `settings.json` file in the
data folder is **not** read (so the fingerprint stays stable); in standalone
`settings.json` provides the values that Gextto does not pass — language, `listen`,
`download-dir`, `peer-ports`, `auth-user`/`auth-password`, `local-bypass`,
`indexers` — and an explicit flag still wins.

**Listen address.** With no `listen` in `settings.json` and no `-listen` flag,
managed listens on `127.0.0.1:8890` and standalone on `0.0.0.0:8890`: standalone
is a client to drive from another machine (its web UI, *arr apps, mobile apps),
so it is reachable from the LAN by default, like `qbittorrent-nox`. Set
`-listen 127.0.0.1:8890` (or a specific interface IP) to keep it on one host
only; the packaged systemd unit already passes `-listen 0.0.0.0:8890`.

**Startup address.** At startup the daemon prints the address to open, not only
the wildcard it binds: `web UI: http://<lan-ip>:8890/` (the interface that
reaches the default gateway, so it works from another device) followed by
`web UI: http://127.0.0.1:8890/`; with a pinned `-listen` only that address is
listed. When the wizard is still to run, an additional `first run: open …`
line points at `/ui/setup`. A router that refuses the port mapping
(UPnP/PCP/NAT-PMP) is reported on a single line, e.g.
`router port 6881 not opened automatically (…): forward it by hand for incoming
peers`.

In standalone the page's POSTs (`/ui/*`) are accepted only if
`Origin`/`Referer` matches the request host (CSRF protection) and the request
host (`Host`) must be an IP or `localhost`, never a name that resolves
to this machine (DNS rebinding protection). The same CSRF protection applies to
the **POSTs of `/api/v2`** (which do not use a token): a client that does not send
`Origin`/`Referer` is still allowed. In managed the page behaves as before (the
v1 API POSTs stay protected by the token).

**Save paths.** In standalone **there is no path restriction**:
a `destination` (or a category save path, or `setLocation`) can be
any absolute path, as in qBittorrent, so the tool stays
fully usable with any folder. The optional flag
`-allowed-roots /srv/media,/data` restricts destinations to those folders, for
those who want to close the API to controlled paths; it is **not set by default**,
precisely so as not to cut the user out. The basic defence remains the login (and, on
Windows, the service runs as a dedicated account and not SYSTEM, so writing
is still confined to what that account can write).

**In-app update** (`update.go`, standalone only). *Settings → Maintenance*
shows the running version (build, commit, build date, release channel) and
reads the `release.json` CI publishes beside the archives
(`internal/updatecheck`): automatically every `update-check-hours` (default 6,
`update-check` turns it off) and on **Check for updates**. The manifest flags
each commit that touches the daemon's code (`"gx": true`, from the
`GX_PATHS` of `scripts/release-manifest.sh`, kept in sync with
`go list -deps ./cmd/gx-torrent` by `TestReleaseManifestCoversDaemonPackages`),
so a release whose new commits only touch Gextto is no update for the daemon;
the button turns into **Update** and lists those commits. Whether a release is
newer is decided by the commit (and the build time), never by ordering the
version string: the version numbering is display-only, so a manual series
change (`1.1.2500` → `1.2.2500`) changes nothing in the update decision.
**Update**
downloads `gx-torrent-<os>-<arch>` of the binary's channel and its `.sha256`
(mismatch → refused), extracts `gx-torrent` to `<binary>.new`, runs it with
`--version` (failure → refused), records `update-notes.json` in the data
folder, keeps the running binary as `<binary>.prev`, renames the new one over
it, stops the daemon cleanly and **re-executes itself in the same process**
(`syscall.Exec`: same PID, so systemd keeps tracking it; if exec fails the
process exits 1 and `Restart=on-failure` starts the new binary). The settings
page follows the installation and reloads on the new version, which shows a
**gx-torrent updated** popup (old → new version and commits) on the main and
settings pages until closed. **Rollback:** while `update-notes.json` is
pending (the first 30 s of the new version) every start is counted; at the
fourth start `<binary>.prev` is put back and executed, and the popup reports
it. The update is off when the daemon is managed by Gextto (Gextto ships it),
for a local build (`constants.GxReleaseChannel` is stamped only by
`scripts/build-release.sh`, since a checkout may be newer than the release) and
for platforms without a published archive; on Windows it checks and offers the
download link, but does not install (the service cannot replace itself).
`GX_TORRENT_UPDATE_URL` points the asset downloads at a mirror (a folder with
`release.json`, the archive and its `.sha256`). The standalone service unit
installs the binary under `/opt/gx-torrent`, owned by the service user, so
the swap needs no privilege; a binary folder the service cannot write is
reported as such.

**Platforms.** The daemon is pure Go and also runs on **Windows** (amd64 and
arm64): `GOOS=windows CGO_ENABLED=0 go build ./cmd/gx-torrent/`. System
reads use the Win32 APIs, so the cache and diagnostics stay complete:
memory (`GlobalMemoryStatusEx`) to size the cache, free space
(`GetDiskFreeSpaceEx`), network filesystem (`GetDriveType`), disk
class HDD/SSD (seek penalty via `IOCTL_STORAGE_QUERY_PROPERTY`) and default gateway
for UPnP/NAT-PMP (`GetBestRoute`, i.e. the route Windows actually uses even
with a VPN and LAN together, with a fallback to the `0.0.0.0` route at the lowest metric). The
default data folder is
`%LOCALAPPDATA%\gx-torrent`. The **library link** of each torrent
(`DataDir/<id>`) is a **junction** on Windows, not a symlink: the junction does not
require the `SeCreateSymbolicLinkPrivilege` privilege (nor Developer
Mode). `os.Readlink` reads it as a symlink, but since Go 1.23 `os.Lstat`
reports it as `ModeIrregular`: the daemon recognises it with `isDirLink`
(`link_*.go`). Removing the junction never touches the data it points to; to
re-point it after a move the new junction is built complete under a
temporary name and then swapped, so a concurrent read never sees the link
missing. A junction can however point only to a **local volume**: with a
download folder on a **network share** (`\\nas\media`, or a mapped drive
`DRIVE_REMOTE`) the daemon uses a **symlink** (the only link that can point to a
share) and, if the symlink privilege is missing, returns a clear error that
names the network folder instead of making the torrent addition fail. A
**service** runs in session 0 and does not see the user's mapped drives: for
network folders use a **UNC** path (`\\server\share\...`), not the drive
letter. On Unix the atomic symlink remains. Migration between volumes also recognises the Windows
error `ERROR_NOT_SAME_DEVICE`, so the copy fallback works as on Unix; without
the symlink privilege, a symlink of the payload pointing to a file is copied
as a file instead of making the move fail (on Unix it remains a symlink).
Only two I/O **optimisations** remain without a native equivalent — the
readahead (`fadvise`) and preallocation with `fallocate` — which use the
fallback (`Truncate`): they work, without the optimisation.

**Installation on Windows.** The daemon recognises that it is started by the
Service Control Manager (`svc.IsWindowsService`, `service_windows.go`) and runs
as a **native service**: a single command starts the HTTP server and the torrents, and
stopping the service stops them cleanly. The service runs as the **virtual
account** `NT SERVICE\gx-torrent` (not `LocalSystem`): it is created by the Service
Control Manager and has no rights beyond those granted by the installer — the
program folder read/execute and `%ProgramData%\gx-torrent` write. A **download
folder** chosen elsewhere must be granted separately
(`icacls D:\media /grant "NT SERVICE\gx-torrent:(OI)(CI)M"`), otherwise the
daemon cannot write. Running under a dedicated account (instead of SYSTEM)
also limits the writing of arbitrary files that an API client can
reach by setting `destination`. The package
`scripts/package-gx-torrent-windows.sh` produces
`gx-torrent-windows-<arch>.zip` with `gx-torrent.exe`, `install-service.ps1`
(`New-Service` + virtual account + `icacls`, run in elevated PowerShell),
`uninstall-service.ps1` and `README.txt` with the `sc create` alternative. Data and logs
in `%ProgramData%\gx-torrent`; the service runs in **session 0**, so it does not
open the browser: the guided installation remains the web wizard on `/ui/setup`.
Installer and systemd unit remain for Linux. The Linux package
`scripts/package-gx-torrent.sh` produces
`gx-torrent-linux-<arch>.tar.gz` with the binary, the unit, `run.sh` and **two
scripts**: `sudo ./install-service.sh` creates the system user `gx-torrent`,
installs in `/opt/gx-torrent` and enables the service; `sudo
./uninstall-service.sh` removes it leaving the data. The unit uses
`StateDirectory=gx-torrent`, so systemd creates and owns
`/var/lib/gx-torrent` before startup (the installer must not create it).

In short, on the Windows PC:

```powershell
# PowerShell with administrator privileges, in the folder of the extracted zip
.\install-service.ps1     # registers and starts the service (automatic startup)
Stop-Service gx-torrent    # stops
.\uninstall-service.ps1   # removes the service (data remains)
```

The zip is published in every release (`gx-torrent-windows-amd64.zip`,
`gx-torrent-windows-arm64.zip`, with `.sha256` checksum). The CI verifies it on
`windows-latest` at two levels: the `windows-platform` job runs the platform tests
(junction, memory, gateway, setup, cross-volume move, daemon lifecycle
`TestDaemonStartServeStop`); the `windows-service` job, non-blocking,
actually installs the service with the same `binPath` as the package,
starts it, waits for `/api/v1/health` and verifies its clean stop
(`scripts/gx-torrent-service-selftest.ps1`, reusable by hand too).

In standalone, if `settings.json` contains `auth-password` (bcrypt hash with the
`bcrypt:` prefix), the page asks for login (`/ui/login`, session in a
`gx_session` cookie) except from LAN sources when `local-bypass` is active
(default). Without a password the page stays open, so the first
configuration is reachable — and, since standalone listens on the LAN by
default, so is the wizard from another PC on the same network: set a password in
the wizard (or in *Settings*) if the LAN is not trusted. Other keys: `auth-user` (default `admin`),
`local-bypass` (`true`/`false`). The password also protects `/api/v1` and the
qBittorrent API `/api/v2` (same criteria as the page: no password, LAN bypass,
or `gx_session`/`SID` cookie)
and all state-changing requests on `/api/` are rejected if cross-site
(`Origin`/`Referer`). After 5 failed login attempts from the same address,
the IP is blocked for 15 minutes.

### Reverse proxy and allowed hosts

With a reverse proxy in front of the daemon, every request arrives from
`127.0.0.1` and, if `local-bypass` is active, would be considered LAN (that is,
without a password). Two keys of `settings.json` handle the case:

- `trusted-proxies`: list (comma or newline separated) of **trusted** addresses or
  CIDR networks (`127.0.0.1`, `10.0.0.0/8`). Only from these addresses is
  `X-Forwarded-For`/`X-Real-IP` believed to establish the real client; a
  request that arrives from an undeclared proxy with these headers is **not**
  considered LAN (so it requires login if there is a password). The path
  used is the last untrusted address of the chain.
- `allowed-hosts`: list of host names accepted in the `Host` header, besides
  literal IPs and `localhost`. It is needed when the proxy preserves the original name
  (`proxy_set_header Host $host`) or access is via a name like `nas.local`.
  Without this key a proxy that leaves `Host` as a non-IP name receives
  `403 host not allowed` (anti DNS-rebinding protection).

The proxy must pass the client address:
`proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;` (nginx) or
Caddy's equivalent.

At the first startup in standalone, until the setup is completed, the page
redirects to the **wizard** (`/ui/setup`): language, download folder and temporary
folder (with a **folder picker**: `GET /ui/browse-dir` lists the
server's directories and a *Browse* button lets you choose them, without typing the
path), peer port with **reachability test** (`/api/v1/portcheck`:
outcome, UPnP/NAT-PMP method, external IP), global **bandwidth limits**
(download/upload in KiB/s, 0 = unlimited), user and password, "LAN without
password" and optional indexers. The two folders are **pre-filled** with two
subfolders of the directory containing the daemon binary (the main folder
takes its name from the chosen language — `download` in English, `scarico`
in Italian, `telechargements`, `descargas`, `pobrane`; the temporary one is
always `download-temp`): they are only proposals, acceptable, editable or
clearable, so that an archive installation keeps the downloads next to itself. The
**Indexer** field is a drop-down of the already supported products (Prowlarr,
Jackett, MIRCrew, others): choosing Prowlarr or
Jackett pre-fills the URL with their usual local address and port
(`127.0.0.1:9696` / `127.0.0.1:9117`), still editable. The wizard writes
`settings.json` and marks the setup as completed; language and bandwidth limits apply
immediately (if the limits are not applied, the error is shown on the page and the
setup is not marked as completed), folder and port at the next startup (the
`peer-ports` key is read when the flag is not passed).

The daemon opens the browser on the wizard by itself, **after** the server is
listening and only if there is a graphical session: `rundll32` on Windows (skipped in
session 0, i.e. services and tasks without a logged-in user), `xdg-open` on Unix
with `gio`/`sensible-browser` fallback (only with `DISPLAY`, `WAYLAND_DISPLAY` or
`XDG_SESSION_TYPE`). In other cases the log still prints the URL to open.

In standalone the daemon also exposes a **qBittorrent-compatible API**
(`/api/v2`) for Sonarr, Radarr and mobile apps: `auth/login` and `auth/logout`
(`SID` cookie, the same login password), `app/version`,
`app/webapiVersion`, `app/preferences|setPreferences`, `transfer/info` and
`torrents/info|add|delete|pause|resume|recheck`, plus categories and tags
(`torrents/categories`, `createCategory`, `removeCategories`, `setCategory`,
`torrents/tags`, `createTags`, `deleteTags`, `addTags`, `removeTags`; category
and tag also travel on `torrents/add`), `torrents/properties|files|trackers`
and `torrents/reannounce|setLocation` (plus the 5.x aliases `stop`/`start`),
`torrents/toggleSequentialDownload|setSuperSeeding|addTrackers|removeTrackers|editTracker|filePrio|export|setForceStart|setAutoManagement|setDownloadLimit|setUploadLimit|setShareLimits`,
`transfer/downloadLimit|uploadLimit|setDownloadLimit|setUploadLimit`, `sync/maindata` (with incremental `rid`) and
`sync/torrentPeers` (which reports `rid: 0`), and `content_path` in
`torrents/info`, as Sonarr and Radarr read).

The **read side** also covers what the WebUI alternatives and the mobile apps
need: `torrents/info` accepts the `filter`, `category`, `tag`, `sort`,
`reverse`, `limit`, `offset` and `hashes` parameters and carries the richer
fields (`tracker`, `completed`, `dl_limit`/`up_limit`, `seeding_time`,
`time_active`, `num_complete`/`num_incomplete`, `availability`,
`downloaded_session`/`uploaded_session`, `total_wasted`) with the fuller
qBittorrent states (`metaDL`, `forcedDL`/`forcedUP`, `stalledUP`, `pausedUP`,
`checkingUP`, `queuedDL`/`queuedUP`); a torrent the queue is holding (the
daemon's `paused` with `auto_managed`) reads `queuedDL`/`queuedUP`, not
`pausedDL`/`pausedUP`, so the `downloading` and `seeding` filters see it exactly
as qBittorrent does — `pausedDL`/`pausedUP` are the torrents stopped outside the
queue; `torrents/pieceStates` and `torrents/pieceHashes` expose the
piece diagnostics; `torrents/webseeds` lists the HTTP sources;
`torrents/downloadLimit`/`uploadLimit` read the per-torrent limits;
`transfer/speedLimitsMode` reports whether the alternative limits are active
(the scheduled window or the manual toggle) and `transfer/toggleSpeedLimitsMode`
flips the manual mode (the qBittorrent speed-limit button); `app/networkInterfaceList`/`networkInterfaceAddressList` list the
interfaces; `log/main` and `log/peers` read the daemon log. `sync/maindata`
answers **incrementally**: when the client echoes the last `rid` it receives
only the changed torrents/categories/tags and the removed entries, otherwise a
full snapshot. `app/preferences` returns the preferences the clients read
(save/temp path, port, DHT/PEX/LSD/UPnP, encryption, protocol, queue and cache
knobs, global limits in KiB/s, IP filter, RSS interval) and `setPreferences`
applies the global limits (KiB/s, `-1` = no limit) and the queue knobs. The
**write side** covers `torrents/addPeers`, `transfer/banPeers`,
`torrents/editCategory`, `torrents/toggleFirstLastPiecePrio`, the queue
priorities (`increasePrio`, `decreasePrio`, `topPrio`, `bottomPrio`), the
renames (`rename`, `renameFile`, `renameFolder`) and `app/shutdown` (a clean
stop, standalone only). A rename closes the session (so the files are closed),
moves the content on disk with a rollback on failure, persists the new paths in
the resume and reopens: the `.torrent` identity is never changed.
The **content layout** chosen when adding (`content_layout` on `/api/v1/add`,
`contentLayout` or the older `root_folder` on `torrents/add`, the
`torrent_content_layout` preference as default, a select in the add dialog and
in the settings) uses the same path override without any move: *Original* keeps
the metainfo paths, *Subfolder* puts a single file in a folder named after it
(without the extension), *NoSubfolder* drops the root folder of a multi-file
torrent. It is applied before allocation — at once for a `.torrent`, when the
metadata arrives for a magnet (the choice is kept in the resume as
`content_layout`, so a restart in between does not lose it) — and a layout
that changes nothing or would make two files collide leaves the paths as they
are. A NoSubfolder torrent has no folder of its own: its `content_path` is the
save path, and a move or a removal with data handles its files one by one and
removes only the folders they leave empty, never a file that is not the
torrent's (`payload.go`). A rename that
would make two files share a path, turn a file into a folder or land on a file
already on disk (not part of the torrent) is refused before anything moves. The
new display name is kept as an override (`display_name` in the resume) on top
of the metadata name, so the list, the qBittorrent API and the completion hook
show it after every restart; the "data still there" check follows the renamed
content root. The **search API**
(`search/plugins`, `start`, `status`, `results`, `stop`, `delete`) is mapped
onto the configured Torznab indexers, which are exposed as the "plugins";
`installPlugin`/`uninstallPlugin`/`enablePlugin`/`updatePlugins` are accepted
and ignored (indexers are configured from `/ui/rss` or `settings.json`, there
is no plugin model). The **RSS API** (`rss/items` with the folder/feed tree and,
with `withData`, the archived articles; `addFolder`, `addFeed`, `setFeedURL`,
`removeItem`, `moveItem`, `refreshItem`, `markAsRead`; and the rules
`rules`, `setRule`, `renameRule`, `removeRule`, `matchingArticles`) is mapped
onto the daemon's feeds and rules. A rule's `mustContain` keeps its
qBittorrent meaning: plain words must **all** appear (the rule's `require`),
`a|b` alternatives are **one of** (`include`); `mustNotContain` maps to
`exclude`, and the reverse mapping renders `require` as words and `include` as
`a|b`. The **cookie API**
(`app/cookies`, `app/setCookies`) stores the cookies the daemon sends when it
fetches `.torrent` files and metadata over HTTP (private trackers): they are
used by the daemon's URL adds, the magnet `xs=`/`as=` sources and the web
seeds. The **torrent creator** API (`torrentcreator/addTask`, `status`,
`torrentFile`, `deleteTask`) builds a v1 `.torrent` from a path on the server
(a requested v2/hybrid format is produced as v1). The same cookies are editable
from the Settings page (*Cookies*), stored in settings.json. The API uses the
same access criteria as the page: without a password it is open, otherwise the LAN bypass
is needed (if `local-bypass` is active) or a session (`SID` qBittorrent or `gx_session`
of the page). In managed it is **not** exposed: only Gextto commands.
Compatibility is verified by tests using Gextto's qBittorrent client
(`internal/qbittorrent`) against the daemon.

### Container

The standalone daemon can be run in a container: the `Dockerfile` in the root
compiles `gx-torrent` (pure Go) and packages it in `debian:12-slim` with the CA
certificates, in **standalone mode**, data in `/data`, UI on `0.0.0.0:8080`
and peer port `6881` (TCP+UDP); the process runs as an unprivileged user.

```sh
docker build -t gx-torrent .
docker run -d --name gx-torrent \
  -p 8080:8080 -p 6881:6881 -p 6881:6881/udp \
  -v gx-torrent-data:/data gx-torrent
```

On first open (`http://HOST:8080/`) the page redirects to the wizard
(`/ui/setup`).

In standalone the page also has a **Categories** filter in the side bar and,
in the *General* detail, the **Category** and **Tag** fields (writing a new
category creates it). The category dialog has a **Save path managed by the
category** switch (qBittorrent **automatic torrent management**, `auto_tmm`):
when on, assigning a category — or changing the category's save path — moves the
torrent to that folder (the qBittorrent API `setAutoManagement` and the
`auto_tmm` field in `torrents/info`/`sync/maindata` use the same flag). The
**global default** is the *New torrents follow their category's folder* switch in
Settings → Folders (**off by default**): when on, a torrent added without an
explicit flag becomes auto-managed, otherwise nothing changes. In
managed these controls do not appear and the endpoint
`/ui/category` rejects the request.

The **indexer search** (standalone) uses the `indexers` key of
`settings.json`, a JSON array of Torznab endpoints
(`[{"name":"Jackett","url":"http://host:9117","apikey":"…"}]`, so Jackett and
Prowlarr): `GET /ui/search?q=…` queries all of them, merges the results and sorts them
by seed. With at least one indexer configured the **search box** appears on the page,
with the results and an *Add* button to add the torrent. In
managed it is not available. **Jackett, Prowlarr and MIRCrew** are configured
in the same way, with the **base address** (an address ending in `/api` is also
accepted): Jackett gets its aggregate Torznab path
(`/api/v2.0/indexers/all/results/torznab/api`), a direct Torznab service such as
the `mircrew-indexer` gets `<base>/api` (for MIRCrew the port is chosen by hand, it
has no default), and **Prowlarr** (`:9696` or `prowlarr` in the URL) is queried
through its aggregate search `/api/v1/search` (`query`/`type`/`limit=100`), which
answers JSON: `internal/torznab` reads it (usenet results skipped, magnet built
from the infohash when missing) and the connection test uses
`/api/v1/system/status`. Gextto applies the same rule (`torznab_endpoint` in
`rss.go`), so the address to type is identical in both. The Torznab XML is read
**leniently**: a raw `&` or an HTML entity in a scraped title or URL does not
make the whole answer unreadable. An indexer can put the **magnet in the link or
in the enclosure** instead of in a `magneturl` attribute (this is MIRCrew's case):
it is recognised and added as a magnet, without trying to download it via HTTP.
An indexer that responds with an **error document** (e.g. invalid API key:
HTTP 200 with `<error code="100" description="…">`, as Jackett does)
is reported as an **error**, not mistaken for an empty feed: a wrong key no
longer passes silently.
Automatic searches in a search feed wait for each indexer **up to 45
seconds**, because MIRCrew may search the forum before responding.

**RSS** (standalone) uses the `feeds` key: a JSON array of feeds with the
rules, for example
`[{"name":"Serie","url":"http://…rss","folder":"serie","include":"1080p,ita","exclude":"cam","category":"tv","savepath":"/srv/tv"}]`.
Each feed can have an optional `folder` (group) to organise it in the
feeds box, which groups them.
A worker (every `feed-interval-secs`, default 180 minutes, editable from the
RSS page in minutes) downloads every active feed, applies `include`/`exclude` on the
title and adds magnets and `.torrent`s not seen yet (the list of already added
items is persisted, so a restart does not re-propose them). `disabled: true`
suspends a feed's automatic checks without deleting it (qBittorrent's *disable feed*;
the row's ↻ button still works). Every item read also ends up in the **feed
archive** (below). On the page appear a *Feeds* box with *Check feeds* and
`GET /ui/feeds`/`POST /ui/feeds/poll` for the status and an immediate check;
`POST /ui/feeds/poll` with `feed=<name>` checks
**a single feed synchronously** and responds with its status (`items`, `new` =
items never seen before, `added`, `last_error`, `last_check`), so the row
refreshes with the outcome; if a round is already in progress it responds 409. In managed it
is not available.

A feed can also be the **HTML page of a listing** instead of an RSS, as
in Gextto: an uploader or search page of **ext.to** (direct magnet links:
title from the text or from `dn`, dedup by info-hash) or of **TorrentGalaxy**
(`/get-posts/user:NAME/`: the rows point to `/post-detail/` pages read with 3
parallel requests and an in-memory cache, so a round costs one request
per new release). A page that advertises a feed (`<link rel="alternate"
type="application/rss+xml">`) is followed; a page that lists nothing gives a
clear error (*the page has no torrent link: an empty listing, or a page that is
not a torrent listing* — an uploader with no release, a filter that matches
nothing) instead of the old `XML syntax error`. Requests use a browser User-Agent
(`internal/rss/listing.go`).

**Requests are spaced per site.** A list of feeds imported at once (13 uploader
listings of TorrentGalaxy, say) is not fetched back to back: two requests to the
same host are at least `feedHostGap` (2 s) apart, and a host that answers **429**
is left alone for `feedHostCooldown` (10 min) — its remaining feeds are skipped
for the cycle with a *HTTP 429: the site is limiting the requests* status and
retried at the next check, instead of failing one after the other. The round
reports it once per host. `cmd/gx-torrent/feed_pace.go`.

A feed can also be a **search**: instead of `url` a `search` (a
query) is used and the worker queries the configured Torznab **indexers**, treating the
results as feed items (same rules, dedup and actions). Example:
`[{"name":"cerca-serie","search":"Serie S01","include":"1080p"}]`.

A feed can also be a **DHT feed**: `url` of the form
`dht://<ed25519-pubkey-hex>[?salt=<hex>]` is resolved from the DHT as a BEP 46
signed mutable item listing torrents; each entry becomes a magnet item, so the
same rules/archive/add path applies. `GET /api/v1/dht-feed?key=<hex>[&salt=<hex>]`
resolves one for external consumers. It needs the DHT enabled.

The **publisher** signs and stores the same kind of feed: `Publish DHT feed…` in
the Tools menu, or `POST /api/v1/dht-feed/publish` with
`{"name": "...", "hashes": ["<40 hex>", ...]}` (empty hashes = every torrent of
the session). The daemon keeps an ed25519 key in `settings.json`, bumps the BEP
44 sequence on every successful put and returns the `dht://<pubkey>` URL, which
a feed (or Gextto) subscribes to. Only standalone mode publishes: in managed
mode acquisition belongs to Gextto.

With the `rules` key (JSON array) you switch to the **ordered rule set**, like
BiglyBT: the first rule that matches decides (`fail: true` discards, otherwise
it adds). Each rule has `name`, `feeds` (to limit it to some feeds; empty =
all), `fail`, `match` and `action`:

- `match`: `require` (every term), `include` (at least one term), `exclude`
  (no term), `regex`/`not_regex`,
  `min_size`/`max_size` (bytes), `min_seeders`/`max_seeders`, `min_peers`,
  `max_age_days`, `require_episode`, `episode_filter` and `smart_episode`
  (adds an episode of a series only once, keeping track
  of the last one: like qBittorrent's smart episode filter; a **REPACK** or
  **PROPER** of an already taken episode is accepted only once);
- `ignore_days` (0 = off): real-time **cooldown** — after a match the rule
  no longer matches for N days, like qBittorrent's *ignore days* ("ignore
  subsequent matches for N days"); it does not depend on the publication date of the
  items, so an episode released two days after the previous one is not lost;
- every rule is active by default; `enabled: false` disables it without
  deleting it (the editor shows a *Disabled* label and an
  *Enable/Disable* button). A rule covers the feeds listed in `feeds` (empty =
  all); a feed **without any rule covering it** uses its own
  `include`/`exclude`, so adding a rule for a feed does not stop the
  others. Even a **disabled** rule covers its feeds: turning off the only
  rule of a feed (or an *once* rule that turns itself off) stops the downloads
  instead of falling back to the feed filter. A feed **without Include/Exclude and
  without rules** downloads nothing: it only grows the **archive** (its row
  reports it with *archive only*, and the log notes it once, when the feed is
  first polled — not at every round), unless
  the operator turns on the **Take all** flag. Adding a feed, then, does not
  mean pulling down what it contains: it means that material is there, and
  what to download is decided by the rules and the **Wanted** list.
- `action`: `savepath`, `category`, `tags`, `paused`, `sequential`, `first_last`,
  `top`, `once` (disables the rule after the first download; it is used by *Follow a
  title* for movies).

**Terms** (`require`, `include`, `exclude`, the feed's own include/exclude and
a wanted title's languages) are found the same way everywhere
(`rss.HasTerm`): case-insensitive, with the separators of release names — dots,
underscores, dashes, brackets — counting as spaces on both sides, and from the
start of a word. So `the office` finds `The.Office.US.S03E01`, `sub ita` finds
`SUB.ITA`, `ita` finds `ITA` and `Italian` but not `Capitan`, `1080` finds
`1080p`.

The **episode filter** `episode_filter` uses qBittorrent's syntax: `1x01`
(single), `1x01-05` (range within the season), `1x01-1x05` (spanning two
seasons), `1x01-` (open: from that episode on **and every later season**, as
in qBittorrent — a wanted title's *Seasons* `1+` becomes `1x01-`), `1x??`/`1x*`
(whole season), separated by commas
or semicolons (`1x01, 1x03, 2x01-`); the `S01E01` forms are also accepted,
the range with tail `S01E01-E05` and the season alone `S01`/`s1`. The filter applies only
to titles with a recognisable episode; an invalid filter never matches
(the editor rejects it before saving).

Example:
`[{"name":"no-cam","fail":true,"match":{"regex":"(?i)cam"}},{"name":"shows","match":{"require_episode":true,"smart_episode":true},"action":{"category":"tv","savepath":"/srv/tv"}}]`.

The **`/ui/wanted`** page (tab *Wanted* in the navigation bar) is dedicated to managing
the titles the operator wants. Each title has type (series/movie),
seasons (`1+`, `2-4`, `1`), language, quality (selectable from a drop-down
among *Any*, *360p*, *480p*, *576p*, *720p*, *720p+*, *720p-1080p*, *1080p*,
*1080p+*, *2160p 4K*, *2160p+ 4K+*), excluded, episode filter, category
and path. You add it by hand, or you type a name and press **Search**: it reads
the **archive** (fast), where the RSS feeds and the periodic indexer polls deposit
everything they find, and proposes the **names found** (filtered by the
language/quality already entered and by the searched words). Tick **Search
indexers too** to also query the configured indexers live — the slow path, for a
title the archive does not know yet — and its results are archived too, so the
next search is fast. The **title is required** (regex tolerant of separators,
so fuzzy results on common words do not enter) and language/quality are in
**AND**. The result also says **where the names come from**
(`Indexer: N · Feed: M · Archive: K`), or *No indexer configured* or the indexer's
error: so you can
immediately tell an absent or unresponsive indexer from the results of the
feeds alone.

A title written between slashes is a **regular expression** (case-insensitive):
`/ted.?lasso/` takes the releases matching the expression instead of the plain
words. The indexer and archive searches use its literal words; a pattern with
alternatives (`|`) is matched only against what the feeds and the archive scan
bring. The form says so under the title field.

At the top of the *Wanted* page, the **Search all wanted** button triggers an immediate
manual search across indexers, RSS feeds and the archive for all configured titles and
gap searches missing episodes, displaying real-time feedback with items added and status.
Each row in the table provides individual **Search**, **Edit**, **Pause/Resume** and
**Remove** actions, missing episodes ledger and real-time status.

**Taking a release by hand (interactive search).** *Releases* on a title opens the
releases held for it and gives each one a verdict (**Would be taken**, *Taken*,
*Already in the client*, or the first filter that turns it away: season, episode
filter, language, subtitles, quality, excluded term); every *Would be taken* row
carries its **Add** button, which puts that release in the queue now with the
title's own category and path. **Search the indexers now**, in the same dialog,
queries the configured indexers for that title and merges what they return into
the list with the same verdicts, so a release the automatic pass would not pick
(an odd naming, a different quality) can still be taken deliberately. In the
add/edit dialog the same preview is live while the filters are typed, and the
*Search indexers too* checkbox makes it include the indexer results.
The episode manager searches **one episode**: with a season and an episode typed,
*Search this episode* opens the same list scoped to it (`GET
/ui/wanted-preview?index=N&season=S&episode=E&live=1`), which queries the
indexers for `Title SxxEyy` and keeps only that episode, so the release of a
single missing episode is picked without wading through the series.

The daemon, at every cycle, reads the **archive** for every title and turns the
matching material into downloads. The **indexers** are queried only for the
**monitored titles** — throttled (every 30 minutes per title), never for the whole
world — and for the **gap search**; what comes back is **archived**, so the next
search and the next cycle read it from the archive instead of asking the indexer
again. It keeps a **record** (in the state file) of what has appeared and what has
been downloaded, shows the **missing** ones in the table and
**searches again** (gap search, with query `Title SxxEyy`) until it finds them. When a title is removed, its search timestamps are dropped from the state file at the next cycle; the record of the episodes already taken is kept, so re-adding the title does not download them again.
For a **movie** it takes one release and does not look for others. A **season
pack** (a season marker without an episode number: `S04`, `Season 4`,
`Stagione 4`, `COMPLETE`) belongs to its season: when the season is wanted
it is downloaded as the whole season (episode 0 in the ledger, `S04 pack` in
the preview), and a taken pack covers the season, so no episode of it stays
missing. The list does
not create feeds or rules: the feeds grow the archive, the wanted titles turn it
into downloads, so language and quality apply in AND and feeds remain free for
advanced rules.

The **`/ui/rss`** page (tab *RSS & indexers*) is dedicated to feeds, indexers and rule
definitions without cluttering the view with wanted titles. The **Check feeds** button
runs a complete check and provides immediate visual feedback (`✓ N feeds checked: X new · Y added`
or error indicators) rather than running silently in the background. Below: lists with
buttons to add, **edit** (the form is filled with the row's values; an empty API key keeps it)
**test** (Torznab request `t=caps`, outcome next to the row) and remove **indexers** (drop-down
Prowlarr/Jackett/Other, pre-filled URL; the name is a **link to the indexer's web page**, i.e.
scheme and host of the Torznab URL, opened in a new tab). A **FlareSolverr URL**
can be set next to the RSS options and tested with **Test FlareSolverr**: feeds
and indexers behind a Cloudflare challenge are then fetched through it (the
direct request is tried first, FlareSolverr is the fallback, and local/LAN hosts
are never retried there), like Gextto does.
The **feeds** have a table with status (*active*/*disabled*, *adds paused*), **last check**
with items, new and added or the error shown at the right end of the name row (so
it does not take a line of its own), and per row the buttons **↻ Reload** (to the left of
*Remove*: reads that feed immediately and refreshes the row), **Edit** (the form is filled,
including the *Feed options*: folder, include/exclude, category, path, pause, disabled),
**Enable/Disable** and **Remove**. An **RSS options** box sets
the refresh interval (minutes) and the archive retention (days)
and shows the time of the next automatic check. The **rule editor** has
one rule per row with **Edit** (loads the rule into the form; saving keeps it in
its place in the order), **Enable/Disable**, arrows to change its order — the
first that matches wins — and *Remove*; the form also has *Download once*
(`once`) and **Match preview**: it tries the form's rule, saved or not,
on the last 5,000 items of the archive and shows those it would take (qBittorrent's
"matching articles" view, instantaneous and without network requests).
Regexes and numeric filters are under *Advanced filters*. For each feed a button that immediately reads the current items with the rule
that would take them and a manual *Add*.
All controls, buttons and input fields of the standalone UI
have descriptive, localised tooltips. In the first-startup wizard
(`/ui/setup`), the *Test port* button checks the port typed in the field
(via `/api/v1/portcheck?port=...`).

The same results are obtained via
`POST /ui/rss-indexer`, `POST /ui/rss-feed` (op `add` — with `index` it edits in
place —, `toggle`, `remove`), `POST /ui/rss-rule` (op `add` — with `index` it edits
—, `toggle`, `remove`, `up`, `down`), `POST /ui/rss-rule-preview` (same fields
as the form, responds `{matches, total, scanned, titles, skip}`: `titles` groups
the matches by series or release name with their counts), `POST /ui/rss-settings` (`interval` in
minutes, `archive_days`) and `POST /ui/wanted`; `POST /ui/rss-config` takes
the three JSON arrays (`feeds`, `rules`, `indexers`) or an exported `setup` file.

### Feed archive

Every item returned by a feed (RSS, HTML listing or search) is
kept in **`<data>/feed-archive.db`** (bbolt, already used for the resume: no
extra dependency), **deduplicated** by info-hash (or by `.torrent` link), with
title, link, size, seed/leech (updated when the feed re-proposes them),
origin feed, first and last appearance date and the *downloaded* flag (set
when a rule, the feed filter or the user adds it). The **`/ui/archive`** page
(*Archive* entry in the bar) searches it like Gextto's Archive:
words (all present, ignoring case and separators `. _ -`),
**phrases in quotes**, **`-words` excluded**, filter by feed and *downloaded
only*, sort by date, seed, size or title, pagination from 50,
resolution and episode badges, *in the session*/*downloaded*, and per row
*Add*, *Copy magnet* and delete; the *Cleanup* section deletes the results
of the current search (never the whole archive with an empty search) or the
items older than N days.

A **Search in indexers too** checkbox makes the configured Torznab **indexers**
(Jackett, Prowlarr) be queried *live* with the same query
(`GET /ui/search`, which reports in `X-Gx-Indexers` how many are configured) and
shows the results in a separate section with indexer and publication date and
an *Add* button: so also what the feeds have not yet
returned appears, i.e. the case where an indexer has the release but the feed's
RSS does not. The live answers are filtered to the titles carrying **every**
search term (the same rule as the local section), because an indexer may answer
loosely with loosely related items, and they are **archived**, so the next local
search finds them without querying the indexers again. Toggling the checkbox
does not start a search by itself: press *Search* (or type) as usual.

The automatic retention is `archive-days` (default 180, `0` = keep
everything), with a cap of 200,000 items; the pruning runs at the end of every complete
feed round and starts from the oldest items stopping at the first one to
keep, so it costs almost nothing. The database has a **compact index** (title
already normalised, feed, size, seed, flags) next to the JSON of the items:
the search scans only the index and decodes the JSON of only the rows of the
page — on 100,000 items **~7 ms and ~44 KB per search** (see
`docs/evoluzione.md`). The index rebuilds itself on opening if missing.

Endpoints: `GET /ui/archive/items?q=&feed=&added=1&sort=new|old|seeds|size|title&page=N`
(JSON with `items`, `total`, `page`, `pages`, `entries`, `size_bytes`),
`POST /ui/archive/add` (`id`), `POST /ui/archive/delete` (`id` repeatable, or
`all=1` with `q`/`feed`/`added`, or `days=N`).

The **bandwidth scheduler** (standalone) applies alternative global limits in a
time window, like qBittorrent, but **per weekday**: the settings page has a
table with one row per day (active, start, end, download and upload KiB/s), so
every day has its own window and limits. It is stored in `schedule-week`
(`mon=23:00-07:00/0/500;-tue=…`, a leading `-` for a day that is off) and
evaluated by `internal/bwschedule`, shared with Gextto. A window that crosses
midnight belongs to the day it starts on (Friday 23:00–07:00 covers Saturday
morning); the next day's own window wins where they overlap. Older settings
with a single window (`schedule-start`/`schedule-end`, `schedule-download`/
`schedule-upload`, `schedule-days` as `time.Weekday` numbers) keep working
until the table is saved. The limits change on the fly, without reopening the
session.
It is configured from the **`/ui/settings`** page (*Settings* link in the
bar of every page), which in **standalone** is the client's complete panel:
**Folders** (download, temporary, watch folders for `.torrent` files, like
qBittorrent's `scan_dirs`, and the two **export** folders where the `.torrent` is
copied on add and on completion), **Network** (listen address, peer port
range, incoming/outgoing interface, proxy, token), **IP filter** (below),
**Protocols** (encryption, DHT/PEX/uTP/holepunch/LSD/UPnP/NAT-PMP, bootstrap nodes,
DHT read-only), **Queue and cache** (active downloads/seeds, active limit, disk
cache, preallocation, sequential), **Optimisation** (below), **bandwidth**
(global limits, alternative limits and their manual toggle) and
**scheduler**, **language** and **notifications** (below). Bandwidth, scheduler and
queue limits apply immediately; folders, network and protocols apply **from
the next startup**. The **watch folders** are scanned every 30 seconds: each
`.torrent` found is added (to the folder's destination, or the default download
folder) and removed after a successful add; a file that is not a valid
`.torrent` is renamed to `<name>.invalid` (so it is not retried at every scan),
and a file modified in the last 5 seconds waits for the next scan because it may
still be being copied; a line is `folder` or
`folder=destination`, and the qBittorrent `scan_dirs` preference maps onto the
same list. The **DHT read-only (BEP 43)** stays off by default: a
read-only node does not respond to queries and asks not to be entered into
others' tables, useful for nodes with little bandwidth or behind symmetric NAT,
but a complete client that uses the DHT must also serve it (it is also the choice of
libtorrent and qBittorrent).

The **IP filter** (`ipfilter`) is a local file or a **URL** (P2P, eMule or
CIDR format, plain or gzip). A new standalone daemon starts with the blocklist
Gextto uses (`constants.DefaultIPFilterURL`, the eMule Security list
`https://upd.emule-security.org/ipfilter.gz`), written to settings.json the
first time the key is missing and no `-ipfilter` flag is given: clearing the
field keeps it cleared. A URL is downloaded to `<data>/ipfilter.dat` at startup
(the session immediately uses the copy downloaded last time, if any) and again
every `ipfilter-refresh-hours` (default 24, `0` = never); a local file is
reloaded when it changes on disk (size or mtime, checked every 10
minutes). The box shows loaded rules, time and path of the load,
next refresh and any error; **Load now** immediately applies the
value of the field (even unsaved) without saving the rest of the page
(`POST /ui/ipfilter` with `return=settings`).

The **Network** box has a **Test port** button (the reachability of the saved
peer port, from `/ui/portcheck`), and the download and temporary folders have
a **Browse…** picker. The DLNA **Library folders** take one folder per line;
*Add folder…* appends the chosen folder to the list.

**Notifications** use the same channels as Gextto (`internal/notify`):
**Telegram** (`notify-telegram-token`, `notify-telegram-chat`), a **webhook**
(`notify-url`, `notify-webhook-format` = `json` — the historical
`{app, event, time, …}` payload plus `text` —, `discord`, `slack`, `gotify`,
`ntfy` or `pushover`, with `notify-webhook-token`/`notify-webhook-user`) and
**e-mail** over SMTP with STARTTLS (`notify-email-smtp`, `-from`, `-to`,
`-password`). The events are *download complete*, *feed added torrents* and
*feed error*, each one switchable (`notify-on-complete`, `notify-on-feed-match`,
`notify-on-feed-error`); **Send test** (`POST /ui/notify-test`) delivers a test
message with the saved channels.

When a check finds a newer release, the status bar at the bottom of the
Torrents page shows an **Update available** indicator linking to *Settings →
Maintenance*.

**Optimisation** is the equivalent of Gextto's *Optimise* button: a single
**Optimize** button runs all three actions (`POST /ui/optimize` with
`optimize=all`), each of which changes only its own settings: **queue** =
Gextto's base (3
downloads, 3 seeds, 5 active, dynamic queue between 1 and 10 slots, slow torrents not
counted, self-management active); **cache** = automatic again (sized
on RAM and disk type, TTL 300 s); **bandwidth** = unlimited download and upload
at **80% of the upload peak measured since startup** (a saturated upstream delays
the download ACKs), with auto-speed on latency active and a minimum of a tenth
of the limit. If not enough upload has been measured yet (less than 50 KiB/s)
the action refuses with an explanation; if the upload was already at the current limit the
line capacity is unknown: the limit stays and only auto-speed is activated.
The box shows the measured ↓/↑ peaks. The three individual actions stay
available under *Advanced* (`optimize=queue|cache|bandwidth`).

On the **Torrents** page in standalone the *cache r/w* and *I/O
ops r/w* tiles do not appear (diagnostics useful to Gextto, not to the client's
user) and the **peer port** tile has a **green** dot when the port is reachable —
UPnP/NAT-PMP/PCP mapping active or incoming connections already received — and **red**
otherwise. In **standalone `settings.json` is authoritative**: a value written here wins over
the startup flag (in *managed* the file is not read and Gextto's flags remain
sovereign).

Most flags have their own environment variable `GX_TORRENT_*`, for
example:

- `GX_TORRENT_LISTEN`, `GX_TORRENT_DATA`, `GX_TORRENT_DOWNLOAD_DIR`;
- `GX_TORRENT_TOKEN`, `GX_TORRENT_ALLOWED_ROOTS`;
- `GX_TORRENT_PEER_PORTS`, `GX_TORRENT_LISTEN_INTERFACE`,
  `GX_TORRENT_OUTGOING_INTERFACE`;
- `GX_TORRENT_PROXY`, `GX_TORRENT_ENCRYPTION`;
- `GX_TORRENT_NO_DHT`, `GX_TORRENT_NO_PEX`, `GX_TORRENT_NO_UTP`,
  `GX_TORRENT_NO_HOLEPUNCH`, `GX_TORRENT_NO_LSD`, `GX_TORRENT_NO_UPNP`,
  `GX_TORRENT_NO_NATPMP`, `GX_TORRENT_DHT_BOOTSTRAP`;
- `GX_TORRENT_IPFILTER`, `GX_TORRENT_IPFILTER_TRACKERS`;
- `GX_TORRENT_WEB_SOURCES_ALLOW_PRIVATE` (flag `-web-sources-allow-private`,
  key `web-sources-allow-private`, checkbox in *Settings → Network*): allows
  web seeds and magnet sources `xs=`/`as=` to reach private LAN addresses.
  Off by default, also in managed mode: those URLs come from the torrent's
  data (even from an untrusted feed) and without this limit they could make
  the client contact the router, the NAS or other internal services (SSRF). See
  `docs/bep.md`;
- `GX_TORRENT_LANG`, `GX_TORRENT_MODE`, `GX_TORRENT_LOG_FILE`, `GX_TORRENT_DEBUG`,
  `GX_TORRENT_NOTIFY_URL` (webhook for feed notifications);
- `GX_TORRENT_MMAP` (opt-in mmap storage, Linux: flag `-mmap`).

Some flags are internal (`-insecure`, `-version`, `-fingerprint`,
`-orphan-timeout`, `-ipfilter-source`, `-gextto-log`): only Gextto uses them to
start, re-attach and supervise the daemon.

Gextto, in managed mode, passes these values itself from the *torrent*
settings (ports, interfaces, encryption, DHT, PEX, uTP, LSD, UPnP, NAT-PMP,
IP filter, DHT bootstrap nodes) and from the `gxtorrent_proxy` setting. The global
connection budget is split between the engine's
outgoing dials and incoming accepts. Proxy and token travel in the environment,
not on the command line. Changes apply from the restart of gextto.

In managed mode the daemon **has no path restrictions**: Gextto decides
the destinations, validates them and already limits them with its own rules. The flag
`-allowed-roots` exists only for the **standalone use** of the daemon (see below) and
Gextto never sets it.

The daemon **in managed** refuses to listen on a non-loopback address without a
token (unless `-insecure`); **in standalone** it is allowed, because access is
protected by login and LAN rules (or open if there is no password). The engine's internal
RPC server is disabled.

## Network

- **Single port, like libtorrent.**
  - The daemon uses the first free port of the range (`-peer-ports`,
    default 6881-6891): TCP for peers, UDP for the DHT.
  - All torrents share it. The handshake, even encrypted, chooses the
    torrent from the info hash.
  - `-peer-ports 0` returns to the original per-torrent port.
- **Opening on the router.**
  - UPnP (IGD v1 and v2), then **PCP** (RFC 6887, on the default gateway) and finally
    NAT-PMP, tried in this order; mappings are renewed every 20 minutes and
    removed at shutdown. PCP is a minimal MAP client, without external
    dependencies; on a router that does not speak it the request expires and it moves to
    NAT-PMP.
  - Outcome and external address are in `GET /api/v1/stats` (`port_mapping`).
  - If none of the three works, the log says to open the port by hand.
- **Listening interface**: `-listen-interface` accepts an IP or an interface
  name. From gextto it comes from `torrent_listen_interfaces`
  (e.g. `wg0:51413`), which takes precedence over `torrent_port_min/max`.
- **Outgoing interface (VPN killswitch).** With `-outgoing-interface wg0`
  peers, trackers, web seeds and DHT go out only from that interface. If it has no
  address, nothing goes out until it returns.
- **Proxy.**
  - `socks5://[user:password@]host:port` or `http://host:port`
    (CONNECT tunnel).
  - Peers, HTTP trackers and web seeds go through the proxy; the tracker names
    are resolved by the proxy.
  - DHT and UDP trackers are turned off, because they cannot go through a TCP
    proxy and otherwise would go out.
  - Incoming connections stay direct on the single port.
- **Encryption** (`torrent_encryption`):
  - 0: outgoing connections are in the clear;
  - 1: encryption is tried, with a fallback in the clear;
  - 2: mandatory encryption incoming and outgoing.
- **IP filter.**
  - Accepted formats: CIDR, ranges `a.b.c.d-e.f.g.h`, P2P
    (`name:start-end`) and eMule `ipfilter.dat`. eMule rules with level
    ≥ 128 allow traffic; IPv6 rules in CIDR form are applied
    (ranges/P2P/eMule remain IPv4 only).
  - The filter applies to incoming and outgoing peers, and to trackers if
    `torrent_apply_ip_filter` is active.
  - gextto's "Update IP filter" button also works with gx-torrent:
    it downloads the list if it is a URL and reloads it in the daemon.
  - The file is **updated at the startup of the service** and then **once a
    week** while the service stays active (besides the manual button).
- **DHT** (BEP 5), shared on the same UDP port as the uTP peers.
  - **IPv6 discovery BEP 32** dual-stack: v4 and v6 nodes kept separate
    (`nodes`/`nodes6`, `want: n4/n6`).
  - **Security extension BEP 42**: the node ID is derived from the external IP when it is
    known (the routable listen address from `-listen-interface`, or the IP
    of the outgoing interface), so no one can
    choose one close to a target info-hash; if the IP is not known at startup
    it is adopted from the `ip` field of DHT responses and the new ID is persisted.
    Local network addresses are exempt. The `ip` field is also **sent**
    in every response (ping, find_node, get_peers, announce_peer and the
    BEP 44 responses), so a peer behind NAT discovers its own reflected address;
    `dht_reflected_sent` counts the sends. An `announce_peer` with
    `implied_port: 1` uses the packet's source port instead of `port`.
  - **BEP 44** (key/value archive): immutable items (target = SHA-1 of the
    value) and mutable ones (ed25519 signature, monotonic `seq`, `cas`), with token, size
    limits and TTL 2 h; `get`/`put` queries and local API. There is also the
    **outgoing client**: `GetImmutable`/`GetMutable` read and
    `PublishImmutable`/`PublishMutable` publish to the closest nodes. It is the
    basis of **BEP 46**: a magnet `xs=urn:btpk:<pubkey>` (+ `s=<salt>`)
    resolves the info hash from the mutable item at addition.
  - **BEP 33** (DHT scrape): a `get_peers` with `scrape=1` receives the Bloom
    filters (`BFpe`/`BFsd`) and estimates seed/peer of the swarm from them; the client queries
    the closest nodes and aggregates. In the UI: **DHT swarm** button in the General
    tab (`GET /ui/dhtscrape`), besides `GET /api/v1/torrents/{hash}/dhtscrape`.
  - **BEP 43** (read-only): a node can not respond to queries and ask not
    to be entered into the routing tables (`ro`); it is activated with
    `-dht-read-only` (`GX_TORRENT_DHT_READ_ONLY`).
- **Trackers and BEP 31** (`retry in`).
  - A tracker that responds with `failure reason` and `retry in: N` postpones the
    next announce by N minutes (announce and scrape, HTTP and UDP).
  - `retry in: never` **discards the tracker from the tier**: it is no longer
    contacted, but the other trackers of the same tier continue to be
    used; only if the tier is exhausted does the torrent stop announcing to that
    tier.
  - **WebSocket trackers** (`ws://`/`wss://`) are supported: the
    WebTorrent/bittorrent-tracker protocol (JSON announce/scrape on the socket),
    dialed through the same egress as the HTTP tracker and closed with the
    session. Note that the reference `bittorrent-tracker` returns only WebRTC
    peers to a WebSocket client, so a native client receives no TCP peers from
    it.
- **BEP 38 hints** (`similar`/`collections`).
  - The `.torrent` hints (from the info dictionary and from the top level, merged)
    are shown in the General tab together with the **local** torrents that
    share a hint (data that could be reused), remain on
    `GET /api/v1/torrents/{hash}/collections` and are rewritten in the export of the
    `.torrent`.

## uTP and LSD

- **uTP** (`torrent_utp`, active by default) uses the same UDP port as the
  peers, shared with the DHT as in libtorrent.
  - Outgoing, uTP and TCP are attempted together and the first to connect wins.
    The per-host recent winner (≥4 races, ≥75% share) gets a 150 ms head start,
    so at steady state one dial per peer is enough; the loser still races
    delayed and single-transport peers connect anyway. Socket dials/accepts
    and their errors are counted (`utp_dials`, `utp_accepts`,
    `utp_dial_errors`, `utp_accept_errors` in `GET /api/v1/stats`); the
    retransmission counters (`utp_resends_timeout`, `utp_resends_fast`) and the
    dropped-SYN counter (`utp_syn_drops`) are shown in the Salute «uTP» row and
    in the daemon status bar.
  - The retransmission timeout adapts to the loss signal: a **minimum RTT of
    5 ms** keeps a fast path (loopback/LAN, RTT of tens of microseconds) from
    collapsing the RTO to sub-milliseconds and resending in a storm, and a
    **per-connection backoff** doubles the RTO on each timeout (up to 4×) and
    halves it on each delivered packet, so a silent or congested peer is
    retried with growing patience while a link that delivers again returns to
    the measured RTT. On the silent-peer harness the backoff halves the resends
    ([`measurements.md`](measurements.md) §4.1). A healthy connection never timeout-resends, so its RTO is
    unchanged.
  - Incoming, the handshake is routed as for TCP.
  - The peer list indicates which ones are connected over uTP.
  - With a proxy uTP is off; with the outgoing interface the UDP socket is
    bound to the VPN's IP.
- **LSD**, local network discovery (BEP 14, `torrent_lsd`).
  - Multicast announcements on 239.192.152.143:6771 (IPv4) and, if available,
    `[ff15::efc0:988f]:6771` (IPv6) every 5 minutes for every active non-private
    torrent.
  - LAN peers announcing the same torrents are added.
  - It is off with a proxy or outgoing interface, so as not to announce the torrents
    outside the tunnel.

## Holepunching (BEP 55)

A peer behind NAT that cannot receive incoming connections can still
be reached by going through a peer acting as a **relay** (`ut_holepunch`). It is the
BEP 55 standard, therefore interoperable with µTorrent, BitComet, qBittorrent and
libtorrent; it is not libtorrent's proprietary DHT-node relay.

- It is activated with the **Holepunching (BEP 55)** setting in *Configuration →
  Torrent engine → Peer and tracker discovery* (active by default). It requires uTP
  (`torrent_utp`): without uTP (or with a proxy) it is off, because the "hole" is
  opened on the UDP mappings. On the daemon the flag is `-no-holepunch`
  (`GX_TORRENT_NO_HOLEPUNCH=1`).
- When a direct dial fails, gx-torrent asks a connected peer
  (at most 8) to act as relay for that endpoint, once only; the relay,
  if it is connected to the target and the latter supports the extension, sends a
  `connect` to both and each starts a uTP connection towards the other.
- The target is searched among the connected peers by IP and port; protocol
  errors (`NoSuchPeer`, `NotConnected`, `NoSupport`, `NoSelf`) are
  reported as provided by the BEP.
- Known limit: as for `ut_metadata`/`ut_pex`, the incoming message is
  recognised via the local ID of the extension; a peer that assigns to
  `ut_holepunch` an ID other than 3 is not understood (pre-existing limit
  of the engine's extension architecture).

### Verification

The codec, the relay decision, the configuration forwarding and the source
mapping are covered by the tests (`make test-engine`,
`go test ./cmd/gx-torrent/`, `go test .`). For a **real** test on NAT there is a
root-only harness, `scripts/holepunch-netns-test.sh`:

```bash
make gx-torrent          # builds bin/gx-torrent
sudo scripts/holepunch-netns-test.sh
```

It creates two **distinct** client networks behind a **stateful firewall** (no NAT: the
clients keep peer IP and port), a minimal HTTP tracker and four daemons with
uTP and holepunch active: a public seed and a relay, on different IPs in `gx-lab`,
plus two clients (A behind gx-r1, B behind gx-r2). The firewall lets traffic out, DROPs
new incoming connections and accepts responses: it is the behaviour that
matters for BEP 55 (port-preserving "cone" filter). B enters as leecher,
downloads from the seed and completes while staying connected to the relay: an
**interested leecher** relay is needed for its data, because a seed discards uninterested peers when
it completes. Only then does A enter (leecher): A does not reach B directly, it asks
the relay for the introduction; the relay sends `connect` to both and the two dial over
uTP. Whoever dials first appears with origin **`holepunch`**, the other sees the
connection as `incoming`: the test passes if it appears on either one. The routers
refuse TCP towards the peer ports, so the hole is opened over uTP. `--keep` leaves
the topology active for debugging; the script reports what it observed (holepunch
/ direct / nothing) and, on failures, the `holepunch` lines of the logs (rendezvous
sent/received, `connect` received).

Why firewall and not NAT: Linux netfilter **does not emulate a port-translating
"cone" NAT**. A packet directed at the router's WAN IP (the leecher's direct dial,
before the rendezvous) creates a local conntrack entry that occupies the
peer's external port and prevents the target from creating its own mapping
when it receives the `connect`; with a single shared external IP the hole cannot be
opened. The stateful firewall, without address translation, reproduces the same
observable behaviour (unrequested inbound blocked, hole that opens only
after the crossed dial) and makes the test deterministic. The behaviour is
verified by the `source`/`incoming` criterion on `/api/v1/torrents/<hash>/peers`
(the daemon's web page shows the same origin).

Note: seed and relay must be on different IPs because the engine deduplicates peers by
IP (`connectedPeerIPs`); two daemons on the same IP would be seen as a single
peer. Both must be started with `-outgoing-interface` equal to the listen IP,
otherwise they announce to the tracker their "default" IP (wrong).

## File selection and priorities

You can download only some files of a torrent and give priority to the
others. The scale is that of libtorrent/qBittorrent: **0 = excluded**, **1..4 =
normal**, **5..7 = high** (high-priority files are downloaded first).
It is set from gextto's files tab or with
`POST /api/v1/torrents/{hash}/file-priorities` (`priorities=4,0,7`); the
qBittorrent-compatible API uses its 0/1/6/7 scale.

- Excluded files never appear in the destination folder. Pieces that
  span an included file are written to `DATA/parts/<id>`, like libtorrent's
  partfile.
- Progress, size and completion refer only to the chosen files:
  gextto receives "complete" when those are ready.
- Changing **which** files are excluded stops the torrent for a moment, moves the
  affected files between `parts` and the destination and starts it again; changing **only
  the priority levels** (in the same downloaded files) is instead applied to the
  torrent in progress, without stopping it or moving files.
- The engine computes the priority of each piece as the **maximum** between the files it
  touches: a piece spanning a normal and a high-priority file is high; the
  picker requests it right after the streaming/readahead window and before
  file-edge, allowed-fast and rarest.
- If the **magnet link** carries the `so=` parameter (BEP 53, *Select Only*, e.g.
  `so=0,2,4-7`), the engine immediately downloads only those files and as soon as the
  metadata arrives the daemon records the choice as normal priorities: the files tab
  shows the same excluded files and they can be changed as always.
- If the magnet carries `ws=` (web seed, Magnet URI convention), the web seeds are added to the
  torrent and remain even after a restart.
- If the magnet carries `dht=` (bootstrap DHT node), the node is added to the
  session's DHT.
- If the magnet carries `xs=`/`as=` (exact/acceptable source, Magnet
  URI convention), the engine tries to download the `.torrent` from those URLs and uses it **only
  if the info hash matches**, so the metadata arrives immediately without a swarm;
  otherwise it falls back to downloading the metadata via peer/DHT.
- A **partial seed** (all chosen files complete, torrent not whole) declares itself
  as such to the peers with `upload_only` (BEP 21), so they stop offering it
  pieces it does not want, and reports it to the **trackers** with `event=paused` at every
  announce, so it is distinguished from an active downloader.

## Sequential download (streaming)

With `sequential` the torrent downloads pieces in index order instead of
"rarest-first", and first of all the edges of each file (first and last ~1%,
up to 8 MB): it is the mode designed for streaming, so a player can
start while the download continues. It is slower overall and worsens the
health of the swarm, so it remains **optional and off by default**.

- It is set at the moment of addition: from the web page (*sequential* and
  *first/last* checkboxes), with `sequential=1` / `first_last=1` on
  `POST /api/v1/add`, or from Gextto's `AddOptions.Sequential` /
  `AddOptions.FirstLast` fields (settings *Sequential download*,
  `torrent_sequential`, and *First/last part of files*,
  `torrent_first_last`, besides the add form checkboxes).
- `first_last` downloads the edges of each file first and then continues
  **rarest-first**: it is independent of the sequential order and useful for
  streaming without giving up the health of the swarm.
- The default value for torrents added afterwards is set with
  `POST /api/v1/config` (`{"sequential":true}`): Gextto does it when it changes
  the setting. **Changing this value on the fly also applies the order to torrents
  already in progress**, like libtorrent. `first_last` has no session
  default in the daemon: with *First/last part of files* active it is Gextto that
  adds `first_last=1` to every new torrent.
- the engine can change the order on the fly also on the single torrent
  (`Torrent.SetSequential`/`SetFirstLast` internally); the state is persisted and
  reported in `GET /api/v1/torrents` (`sequential`, `first_last`).

## BitTorrent v2 torrents

The engine downloads **v1** torrents, the v1 part of **hybrids** (v1+v2) and
**v2-only** torrents added from a `.torrent` file that includes the **`piece
layers`** (BEP 52). Pieces are built **file by file**: a file's tail piece is
shorter than `piece length` and does **not** contain padding; each piece is
verified with a **SHA-256 Merkle node** over the 16 KiB blocks (missing leaves
= zero hash). The torrent identity is the SHA-256 truncated to 20 bytes, so
handshake, DHT and trackers remain identical to v1.

**v2-only** torrents without `piece layers` (the v2 **magnets**, `urn:btmh:`)
are also supported: the info dict arrives via BEP 9 and the per-file **`piece
layers`** are requested from the peers with the `hash request`/`hashes`/`hash reject`
messages (BEP 52), verified against the `pieces root` and persisted in the resume. The
reserved **v2** bit (byte 7, `0x10`) is announced in the handshake only for a torrent
with v2 identity, so libtorrent peers accept the `hash request`. The seed
responds to the **piece layer** requests (`base = piece layer`) from its
`piece layers`, without reading the files. It also responds to the **block
layer** requests (`base = 0`), which a libtorrent leecher uses to verify
blocks while downloading them: for a file **entirely present** the block tree
is built from the data and **cached per-file**; for a file that is only
**partly downloaded** the requested 16 KiB leaf hashes are computed from the
verified pieces and anchored with the piece layer's uncles, so the parts the
seed already has are still servable (the same path answers a complete file
while its block tree is still being built). The reply is byte-for-byte what
the full block tree would return. A request that touches a missing piece,
folds below the piece layer or asks for more than 512 hashes (the leaves are
read on the torrent loop) is refused (`hash reject`). The v2 wire rules are in [`bep.md`](bep.md).

## Local data reuse (Swarm Merging)

If another torrent of the session has the **same content**, its **already
verified** data is reused: at allocation the new torrent reads the bytes from
the other's storage, **re-verifies them against its own hashes** and writes them
into its own, so it restarts only from the missing pieces and can complete
**even without peers**. It is useful for a reloaded episode or for different
`.torrent`s of the same content, and «resurrects» a torrent without a seeder
when the content is already present elsewhere. The merge also runs when a
torrent is **reloaded from its resume data**: the pieces the reloaded bitfield
already verified are kept (never re-read or re-hashed) and the merge fills the
rest and re-derives the block-level reuse, so a restart does not lose it. And it
is **re-triggered when a sibling appears after the allocation**: a torrent that
publishes newly verified data wakes the others, and one that is already
downloading merges it too (the fetch pipeline pauses for the merge and restarts
at the end), so the order in which the torrents are added does not matter.
Merging is **disabled if one side is a private torrent** (BEP 27): mixing
private swarms violates the tracker rules. It is independent of the **BEP 38**
hints, which serve to *report* the candidates.

The merge has three phases:

- **Phase 1 — identical layout.** Same `piece length`, same length and same
  hashes at the same indices: the pieces are directly interchangeable and are
  matched piece by piece.
- **Phase 2 — v2, per-file Merkle root.** The files are matched by their BEP 52
  per-file `pieces root` (and length), so the same file is shared even when the
  two torrents use a **different piece length**: the destination piece's bytes
  are reassembled from the source's verified pieces and **each destination piece
  is verified against its own hash** before it is written. There is no v1
  different-layout heuristic (§6 of [`evoluzione.md`](evoluzione.md)).
- **Phase 3 — v2, 16 KiB blocks.** When the source has verified only part of
  the source pieces a destination piece spans, the verified blocks are written
  and only the missing blocks are requested from peers; the piece is verified
  whole once complete. If that check fails, the local blocks are dropped and
  the piece is downloaded whole (the peer is not banned for it). Measured on
  the Debian and Arch ISOs with a source 75% complete: 25% of the file from the
  network instead of 100% ([`measurements.md`](measurements.md) §4.8, harness
  `scripts/phase3-env.sh` + `TestMeasureMergePhase3`).
- **Phase 4 — v1, a different piece length.** Two **v1** torrents matched by
  file **path and length** share **whole** pieces even when their piece grids
  differ: each destination piece is assembled from verified source pieces and
  checked against its own hash before it is written. v1 has no per-block hash,
  so a piece only partly covered by the source is **not** reused (no block-level
  write), and a wrong metadata match is abandoned after 64 hash failures instead
  of reading the whole source.

The session statistics report `merged_pieces`, `merged_bytes` (block-level
bytes included) and `merged_partial_pieces` (pieces reused at block level);
Gextto shows them as «Riuso locale».

## DLNA media server

An **opt-in** DLNA/UPnP AV media server (`cmd/gx-torrent/dlna.go`) lets a TV or
console on the LAN browse and play the **completed** torrents' media files with
no client-side configuration. It is off by default and separate from the UI: it
binds its own HTTP listener (`dlna-listen`, default `:8200`) and answers SSDP on
UDP 1900 on the same interface.

- **Discovery**: SSDP M-SEARCH replies (only to LAN sources) and
  `ssdp:alive`/`byebye` NOTIFYs for a `MediaServer:1` device (DLNA `DMS-1.50`);
  `ssdp:alive` is sent twice at start and again every 15 minutes, before the
  1800 s max-age runs out, so a TV switched on later still finds the server.
  The device UUID is derived from the host name and the port: stable across
  restarts (a TV keeps its bookmarks) and distinct for two instances.
- **ContentDirectory:1**: `Browse` with both flags, **BrowseMetadata** (the
  object itself: many TVs ask for the root first) and **BrowseDirectChildren**,
  with **paging** (`StartingIndex`/`RequestedCount`, `TotalMatches` is the full
  count) and UPnP errors 701 (no such object) / 402 (bad arguments).
  **Search** evaluates `upnp:class` (`object.item.videoItem`,
  `object.item.audioItem`, `object.item`) and `dc:title` (`=`, `!=`, `contains`,
  `doesNotContain`, several terms ANDed) over the torrents and, recursively, the
  library folders; `GetSearchCapabilities` advertises `dc:title,upnp:class`.
  `GetSystemUpdateID` and the Browse `UpdateID` change whenever the listing
  changes, so a control point knows to refresh. **ConnectionManager:1**:
  `GetProtocolInfo` lists every served type, plus `GetCurrentConnectionIDs` and
  `GetCurrentConnectionInfo`. **X_MS_MediaReceiverRegistrar:1** answers
  `IsAuthorized`, `IsValidated` (both `1`) and `RegisterDevice`, so Xbox 360 and
  Windows Media Player accept the server. **GENA**: `SUBSCRIBE`/`UNSUBSCRIBE` on
  all three services are accepted (several TVs refuse a server whose event URL
  fails) and the initial event is sent; the callback must be the subscriber's
  own address.
- **Library folders** (`dlna_library.go`): besides the torrents the server
  shows configured folders, typically the archive on the NAS Gextto files the
  media into, as a **browsable folder tree** (subfolders, then the video/audio
  files, with their subtitles attached). With folders configured the root holds
  one container per folder plus a **Torrent** container; without, the torrents
  stay directly in the root. Folders come from `-dlna-folder` (repeatable) or
  `GX_TORRENT_DLNA_FOLDERS`, and in standalone from `dlna-folders` on the
  settings page (the **Download folder** button adds the configured download
  folder in one click); `-dlna-exclude` hides paths inside them. In managed mode Gextto
  passes its own library: the `gxtorrent_dlna_folders` list or, when it is
  empty, the archive root plus the folder holding each series' archive path,
  nested folders collapsed, with **Gextto's trash excluded**. Every access goes
  through `os.Root`: `..`, an absolute path or a symlink leading out of the
  folder are refused; hidden entries and the NAS system folders (`@eaDir`,
  `#recycle`, `#snapshot`) are neither listed nor served, and `/lib/<n>/<path>`
  serves only a file its directory listing offers (`http.ServeContent`, Range
  and HEAD, same DLNA headers). A directory listing is cached for 30 s (a TV
  pages a folder with several Browse calls); a folder that is not mounted yet
  is an empty container. On a real NFS archive the 125 series folders list in
  about 0.5 s on the first read.
- **Artwork** (`dlna_art.go`): library videos and folders carry
  `upnp:albumArtURI` (`JPEG_TN` 160×160, then `JPEG_SM` 640×480) built from
  the images Jellyfin, Kodi and Emby keep beside the media. A video takes
  `<stem>-thumb`, `-poster`, `-landscape`, a same-name image, `-fanart`,
  `-backdrop` (`.jpg`/`.jpeg`/`.png`), else its folder's image; a folder takes
  `folder`, `poster`, `cover`, `thumb`, `landscape`, `fanart`, `backdrop`. A
  season folder (`Season 01`, `Stagione 1`, `S01`, `Specials`, …) without its
  own image takes Jellyfin's `season01-poster` (`season-specials-poster`) from
  the series folder, else the series poster. `/art/<n>/<path>` serves an
  artwork image of a listed library directory and `/art/<n>/<dir>/` a folder's
  image (a subfolder always gets the URL and answers 404 without one, so a
  parent listing does not read every subfolder); `?pn=` picks the profile.
  The image is scaled with the standard library (box filter; a decoded JPEG is
  averaged directly on its YCbCr planes), re-encoded as JPEG q85 (one that
  already fits is served as is) and cached in memory (LRU, 8 MB, keyed on size
  and mtime so a changed image is redone); at most two decodes run at once and
  a header over 40 Mpixel or a file over 32 MB is refused before decoding. A
  1080p still becomes a `JPEG_SM` in ~34 ms with 4.1 MB allocated. Torrent
  items have no artwork.
- **Listing**: the root holds one container per completed torrent with at least
  one playable (video/audio) file, sorted by name; a container lists its files
  sorted by path. **Subtitles** (`.srt`, `.vtt`) are not items: one with the
  name of a video in the same folder (`ep1.srt`, `ep1.it.srt` for `ep1.mkv`) is
  attached to it as a `res` and as `sec:CaptionInfoEx` (Samsung/LG). The
  listing is rebuilt at most every 5 seconds.
- **Media**: `/res/<hash>/<file>` is served with HTTP Range (and HEAD) by the
  same core as the UI player (`serveTorrentFile`), with the DLNA headers
  `contentFeatures.dlna.org` (`DLNA.ORG_OP=01`: seeking by byte range) and
  `transferMode.dlna.org`, spelled exactly as renderers expect. Links are built
  on the Host the client used, so a multi-homed host hands each client a URL on
  its own interface. Only **fully downloaded** torrents are exposed, because a
  renderer probes the container metadata (moov/cue) and a partial file plays
  badly, and `/res` serves **only** the files the listing offers: never an
  incomplete torrent, a non-media file or a file not listed.
- **Safety**: no authentication, so it is off unless asked for, and **LAN
  only** by construction: every HTTP request and SSDP probe from an address
  that is not loopback, private (RFC 1918/ULA) or link-local is refused (403),
  so a port exposed by mistake does not hand the media to the internet. The
  settings are `dlna-enabled`, `dlna-listen`, `dlna-name` (daemon page in
  standalone, Gextto options `gxtorrent_dlna*` in managed); a change to the
  address or the name restarts the server within 10 seconds, and the settings
  page shows the URL it is running on.
- **Log**: the daemon log (and the page's log tab) records the server
  start/stop/restart, each M-SEARCH, each Browse and each played file, all
  throttled to once per client and object per minute (a player issues a Range
  request per seek).
- **Not covered**: transcoding and frames extracted from the video (artwork
  comes only from existing images). Both need a video decoder; the server stays
  pure Go and never shells out to ffmpeg.

The media `Content-Type` comes from the file extension (`video/mp4`,
`video/x-matroska`, `audio/flac`, …); a stream also sets
`Content-Disposition: inline` (RFC 6266, `filename*` for non-ASCII names) and
answers HEAD without moving the engine's stream window, so a browser plays it
instead of downloading it. Concurrent streams are counted **per file**: closing
one leaves the window of another reader of the same file, and a stream of
another file of the same torrent does not keep a closed file's window alive.

## Statistics

gextto's **Health** page has a "Torrent engine" panel, refreshed
every 15 seconds, for all engines.

With gx-torrent it shows:

- version and torrents by status;
- speed and queue slots;
- port and opening on the router (in red if unsuccessful);
- DHT (nodes), uTP (uTP and TCP connections, plus socket dials/accepts, errors, resends and dropped SYNs), LSD (peers found);
- incoming connections and disk I/O (jobs in queue, total reads/writes);
- encryption, proxy, IP filter rules;
- disk and cache, session totals.

The same data are in `GET /api/v1/stats` of the daemon and in
`GET /api/torrents/session-stats` of gextto, and the same snapshot is what
`GET /api/v1/metrics` exports for Prometheus. Shared counters use the
**libtorrent names** also for gx-torrent (`net.recv_payload_bytes`,
`peer.num_tcp_peers`, `disk.num_read_ops`, …), so the endpoint is uniform; the
engine-specific counters (cache, disk speed, DHT traffic) remain
alongside. Compared to libtorrent:

- the **protocol overhead** is available (`protocol_overhead_bytes`: bytes
  exchanged on the peer connections minus the payload, wire framing
  included); like the payload totals it is **session-scoped**, so it restarts
  when the engine session does (encryption is not in the count: the byte
  tally sits above the MSE layer);
- disk job times and failed pieces per peer are **missing**: the engine does not
  measure them and they are not implemented.

The *Test ports* button (in Gextto and on the daemon page) does not read
`/api/v1/stats`: it queries `GET /api/v1/portcheck`, which responds whether the peer port
is listening and whether the router forwards it.

The daemon also has its own **Statistics** page (`/ui/stats`, in the nav next to
*Archive*): the qBittorrent-style view — live rates, the totals of this session and of
**every** session, disk and cache, the network and the queue, a speed graph with three
ranges (`1 h`, `24 h`, `7 d`) with peaks, and the busiest torrents right now. The page
is a translated shell filled from the API, so it always shows the same data as
`GET /api/v1/stats`.

**All-time totals.** The engine's counters are per session (they start at zero when the
daemon starts); the all-time downloaded/uploaded bytes are folded from them and kept in
`<data>/lifetime.json` — atomic write, flushed at most every 60 s and on shutdown, and
a no-op when the daemon has no data dir. A counter lower than the one already seen means
the engine restarted its session: it is adopted without subtracting.

**Speed history.** The speed is sampled every queue tick into three in-memory rings —
one point per tick for the last hour, per minute for a day, per hour for a week — so any
range is cheap to draw; the rings allocate nothing while sampling and, like
qBittorrent's graph, start empty when the daemon restarts. `GET
/api/v1/stats/history?range=1h|24h|7d` returns the points and the peaks of one range (an
unknown range answers `1h`).

## Piece diagnostics

`GET /api/v1/torrents/{hash}/pieces` reports the status of every piece as
compact intervals (`begin`/`end` inclusive, `state`): `have` (downloaded),
`downloading` (in progress), `skipped` (excluded file) or `""` (missing). The daemon's
web page has a **Pieces** tab with the coloured map; Gextto shows the
same map in the **Pieces** tab of the torrent detail. gx-torrent is the first
Gextto engine to expose piece diagnostics (libtorrent responds "not
available").

## HTTP streaming (HTTP Range)

Besides sequential download, the daemon can serve a file to a player while
downloading it: `GET /ui/stream?hash=<hash>&file=<index>` returns the file and
supports a single `Range: bytes=…` header (response `206` with
`Content-Range`, `Accept-Ranges: bytes`). The pieces of the requested window, plus
about 16 MB of readahead, are requested first; if a piece has not yet
arrived the daemon waits (up to 2 minutes) instead of serving zeros. Unsatisfiable
requests respond `416`.

Pieces are ordered on **two levels**: the window the player reads now
(`SetFileStreamWindow`) has priority over the **readahead** (`SetFileReadaheadWindow`),
which in turn precedes rarest-first. So the playhead does not stay behind the
prefetch.

Authentication is that of the web page (cookie, header or `?token=`), so
even an external player (VLC, mpv) can use the full token URL. From Gextto
`GET /api/torrents/{hash}/stream?file=<index>` redirects to the daemon adding
the token server-side; in the **Content** tab of the torrent detail every file
has a **▶** button, and the daemon page has it in the **Files** tab.

## Disk cache, preallocation and mmap storage

gextto's torrent settings also apply to gx-torrent, but gx-torrent's cache
is **adaptive**: the daemon recomputes it from the real load instead of
using fixed values.

- **`torrent_cache_size` = -1** (default): **adaptive cache**. The daemon
  recomputes it every 3 minutes from:
  - **available RAM** (`MemAvailable`, reclaimable memory), not the total
    RAM: on a busy server the cache deflates by itself;
  - **active downloads** (write buffer) and **active seeds** (read cache);
  - **storage class** of the download folder: on HDD or NFS the values
    double (more coalescing, fewer round-trips), on SSD/NVMe they stay low.
  - Limits: write 96 MB–1.5 GB, read 32–512 MB, never more than 1/4 of the RAM and
    1/8 of the available memory.
  - **Hysteresis**: it is reapplied only if the target changes by more than 25%, at
    most once every 10 minutes. The engine resizes read cache and write
    buffer on the session in progress: no reopening.
  - the engine does not have a write-back cache: the value is a **cap on the pieces in flight**,
    and the real write/coalescing cache is done by the kernel. The policy serves
    above all not to oversize when the load is low and to give more
    read cache during seeding.
- **`torrent_cache_size` > 0**: **manual override**; the indicated size
  (16 KiB blocks) applies to reading and writing and the adaptive policy is silent.
- **`torrent_cache_expiry`**: after how long a block in the cache expires.
- **`torrent_preallocate`**: new files are reserved in full with
  `fallocate`; files excluded from the selection remain sparse, and on tmpfs (RAM
  disk) they always remain sparse: reserving them would immediately take their whole
  size from the RAM.
- **Disk guard** (`min_free_mb` in the queue config, default 1024 MiB, 0 = off;
  settings page *Pause downloads below free space*; Gextto pushes its
  `min_free_space_gb` when set): every 15 s the daemon reads the free space of
  each storage folder of the unfinished torrents (outside its lock, walking up
  to the nearest existing folder). Below the threshold the downloads of that
  folder are stopped and flagged `disk_wait` in the API (state `paused`, shown
  as *waiting for disk space* on the daemon page); they resume when the folder
  has the threshold plus a slack (10%, at least 256 MiB), so it does not flap
  at the limit. Seeding goes on. With preallocation on the guard stays off: the
  files are reserved in full when the torrent starts. A folder whose free space
  cannot be read never blocks.
- **`-mmap`** (experimental, Linux): the torrent's files are served from a
  **shared memory mapping** instead of with `read`/`write`. Piece writes
  become copies into the page cache (asynchronous kernel writeback) and
  reads avoid the syscall (on tmpfs the benchmark is ~7.0 GB/s against
  ~5.9 GB/s for `pwrite`, [`measurements.md`](measurements.md) §4.9). It is **opt-in** (also `GX_TORRENT_MMAP=1`) because it maps
  every open file: with torrents of thousands of files it approaches the
  `vm.max_map_count` limit. Outside Linux it is ignored (normal files are used).
  A page the disk cannot back (disk full on a sparse file, file truncated by
  another process) raises `SIGBUS`: the access is guarded and comes back as an
  I/O error on that torrent instead of crashing the daemon.
- **"Optimise settings"**: with gx-torrent it resets the cache to automatic and
  applies it immediately.
- The cache size changes on the fly; cache expiry and
  preallocation instead reopen the daemon session.
- **Idle memory release**: piece buffers are 1 MB and live in per-torrent
  `sync.Pool`s. After a burst they can stay reachable until a collection even
  when nothing is moving, because an idle daemon allocates too little to
  trigger one. After **5 minutes with the total transfer rate (download and
  upload) below the queue's slow-rate threshold** the daemon calls
  `debug.FreeOSMemory` (at most
  once every 10 minutes) and logs `memory released`; the caches rebuild on the
  next transfer. On the N97 the RSS went from 550 MB to 35–40 MB
  ([`measurements.md`](measurements.md) §4.1).
- The Health panel and `GET /api/v1/stats` show the effective cache, the
  chosen target (`cache_read_mb`, `cache_write_mb`), the **reason**
  (`cache_reason`), the storage class (`cache_storage`) and the available
  memory (`mem_available_mb`).

## RAM disk

gextto's RAM disk flow (download to tmpfs and move when there is no more
space or at the end of the download) works: the move between different filesystems
copies the files and resumes from the point it was at. It is proven by a test that moves
a download at 20% from `/dev/shm` to the disk.

- The `.torrent` copies in the folder chosen by the operator
  (`torrent_copy_dir`) take the torrent's name, as with
  libtorrent; the old `<hash>.torrent` copies are renamed at startup.
- A downloading torrent that does not yet have any verified piece is
  moved without copying its empty files: they are recreated in the
  destination, without writing gigabytes of zeros to the NAS and without the full
  check the engine would do finding files already present.
- A torrent with unknown size at addition (magnet) does not start on the RAM
  disk: it stays on disk until the metadata reveals the size, then it is
  moved to the RAM disk only if it falls within the threshold. A file too large never
  touches the tmpfs. The rule also applies to manual adds and to every engine
  with `ramdisk` support; qBittorrent (`ramdisk: none`) always downloads to disk.
- The "reserved" space on the RAM disk does not count the torrents already
  leaving it.
- The tmpfs free space is not trusted on its own: a tmpfs is RAM-backed, so a
  6 GB mount can report several GB free while the host has almost no memory
  left. Before staging a payload, Gextto caps the RAM disk's usable free space
  by the host's available memory (`MemAvailable`): a download starts on the
  tmpfs only when both the tmpfs and the machine can hold it, and otherwise
  falls back to the overflow dir like an oversized release. When the memory
  figure cannot be read the tmpfs one is used unchanged.

## On-disk layout and data safety

The engine saves every torrent in `DataDir/<id>` and at removal always runs
`os.RemoveAll(DataDir/<id>)`. For this reason gx-torrent:

- **always** creates `DATA/links/<id>` as a symlink towards the real
  destination folder. Files end up in `destination/<torrent name>`, with the
  same layout as libtorrent, so `CompletionPath` works unchanged;
- removing a torrent deletes only the symlink. Data is deleted only with
  `delete_files=1`, and only the torrent's file or folder, never the
  whole destination or other files;
- a real folder left in place of the symlink is moved to
  `DATA/orphaned/<id>` before removal, never deleted;
- destinations must be absolute paths; the flag `-allowed-roots`
  (standalone use only, Gextto does not set it) can restrict them.

**Durability.** Data files are not opened with `O_SYNC`: the engine writes
pieces in buffered mode and flushes in batch with `storage.File.Sync()` before
persisting the bitfield (order snapshot → fsync → write; `updateStats` in
two phases). A bit that survives a crash therefore always refers to data already
on disk, and at most the pieces of the last few minutes are re-downloaded. On HDD and NFS
this removes one fsync per piece write (measurements in `evoluzione.md`
§5).

**Move**:

1. the torrent is stopped;
2. the daemon waits for the engine to close the files;
3. the content is moved, with `rename` or with copy and removal if
   the filesystem changes (via `.gxpart` files);
4. the symlink is re-pointed atomically;
5. the torrent starts again.

Gextto receives `storage_moved` when the new path appears in the list, and
`storage_move_failed` on error. `AssociateStorage` re-points without
moving and re-verifies.

The daemon's own state is in `DATA/state.json`, written atomically.
For each torrent it contains:

- path, queue position, user pause, park for stall, pin;
- probe in progress, rotation, seed limits;
- swarm size from the last scrape;
- completion date and error.

It survives restarts together with the engine database (`DATA/session.db`).

## Self-managed queue

The engine does not have a queue: the daemon itself manages it, every 3 seconds (and immediately
after every command), with the same policy that Gextto applies to its engines.

- **Slots.** Active downloads (`active_downloads`) start in order of
  queue position. Seeds (`active_seeds`) have their own slots.
- **Slow torrents** (`dont_count_slow`, active by default). A torrent that after
  `slow_after_secs` (120 s) transfers less than `slow_rate` (2 KiB/s) keeps
  running but frees its slot, so the next one starts. A dead swarm never
  blocks healthy downloads. Just started, a torrent has a grace period.
- **Total limit** (`active_limit`). It is a hard cap on the running
  torrents, slow ones included. When it is exceeded, the daemon:
  1. does not restart the torrents just set aside;
  2. stops the slow seeds, then the slow downloads, from the bottom of the queue;
  3. only afterwards gives up starting healthy torrents.

  The stopped slow torrents are **rotated** to the bottom of the queue and retry after
  `slow_rotate_secs` (30 min). So every blocked download is retried
  periodically while the healthy ones proceed, without continuous starts and stops
  between one turn and the next.
- **Never touched by the queue.** The queue never starts:
  - torrents paused by the user (or by Gextto at the end of seeding);
  - those parked as stalled by Gextto;
  - those in error;
  - those moving.
- **Pin and probe.** Torrents with pin and those in probe (see below) always
  run and do not occupy slots.
- **Dynamic queue** (`dynamic_queue`, with minimum and maximum). Gextto keeps it
  on while gx-torrent's self-management (`auto`) is on, and with self-management off
  it follows Gextto's *Automatic queue and resource management* option
  (`torrent_dynamic_queue`). It is the same logic Gextto applies to
  the other engines:
  - it narrows the slots if the global download limit stays saturated (≥ 90%)
    for ten minutes;
  - it widens them quickly if the bandwidth stays unused (≤ 70%) and there are
    torrents waiting;
  - with downloads queued for 5 minutes, the seeds drop to one.
- **Move to top.** `QueueTop` at addition, or the `top` action.

Statuses reported to Gextto:

| Status | Meaning |
|---|---|
| `downloading` | running (allocation too) |
| `downloading_metadata` | magnet waiting for metadata |
| `checking_files` | check in progress |
| `seeding` | complete, sharing |
| `paused` | `auto_managed=true`: queued, waiting for a slot. `auto_managed=false`: user-paused or stopped at the end of seeding |
| `stalled` | parked by Gextto, or set aside by the queue because it was not downloading anything. It stays `stalled` until it restarts, so Gextto's stall clock does not reset |
| `moving` | move in progress |
| `error` | engine error or failed move; a `resume` or a `verify` resets it |

The `auto_managed` flag (with `parked` and `pinned`) is what Gextto summarises in the
**Auto-managed** row (*Auto-gestito* in Italian) of the torrent detail, naming *who*
controls the scheduling, translated in every interface language: *managed by the
queue* (*gestito dalla coda*) when the queue manages it, *paused* (*in pausa*) when
it is stopped outside the queue, *parked (stalled)* (*parcheggiato (in stallo)*)
when it is stalled and has been set aside, *forced outside the queue* (*forzato
fuori coda*) when it is force-started. It is therefore not a simple yes/no. The
same values are used for every engine: with qBittorrent-nox the forced case is
`force_start` and the queue owns every torrent that is neither force-started nor
stopped (qBittorrent's `auto_tmm` is the automatic management of save paths, not
of the queue, so it does not count); a torrent outside the queue that is not
transferring (stopped, checking, in error) reads *paused*. The TUI shows the same
row (`sched.*` keys of its catalog).

## Stalled: division of tasks

The long-term management of stalls remains in Gextto (`MonitorStalled`),
identical for all engines:

1. after `torrent_stall_after_min` without progress Gextto calls
   `MarkStalled`, i.e. the `park` action (stopped, out of the queue, `stalled` state);
2. the attempts follow the backoff 1 h → 3 h → 6 h → 12 h → 24 h and use
   `Restart`, i.e. the `restart` action: the torrent runs out of the queue for a
   15-minute probe window and is re-announced to trackers and DHT;
3. if bytes arrive, Gextto calls `ClearStalled` (`unpark`); otherwise
   it parks it again;
4. once the give-up has expired, Gextto removes the torrent and looks for another release.

The daemon adds **short-term** management: slow torrents out of the
slots and rotation at the total limit. Parking survives restarts of the
daemon.

For diagnosis (`dead_swarm`, `no_connected_seed`, ...):

- swarm seeders and leechers come from the last successful scrape of the trackers
  and remain known even with the torrent stopped;
- "connected seeds" are the peers from which one is actually receiving.

## Seed

The per-torrent limits (`SetLimits` with ratio and days) are saved by the
daemon and reported in the list (`-1` = global, `0` = infinite). Gextto's
`EnforceSeedPolicy` applies them, as for the other engines: pause at the end of seeding,
then move or remove. Gextto resolves each limit from the torrent, then the first
**seed rule** matching its source (indexer) and category, then the global
settings, and adds an **inactivity** limit (no upload for N minutes while
seeding); all of it lives in `seed_rules.go`, so it is the same with either engine.

In **standalone** the daemon also enforces the **global** seeding limits
(Settings, *Seeding limits*): a seeding torrent whose per-torrent limit is
`global` is paused (or removed keeping the files, per *Remove instead of
pausing*) when it reaches the configured share ratio (`max-ratio`) or seeding
time (`max-seeding-time`, in minutes). A per-torrent limit always wins over the
global one, and `0` means infinite. These are the qBittorrent `max_ratio` /
`max_seeding_time` preferences. The **inactive seeding** limit
(`max-inactive-seeding-time-enabled` / `max-inactive-seeding-time`, qBittorrent
`max_inactive_seeding_time_enabled` / `max_inactive_seeding_time`, minutes)
stops a seed whose uploaded bytes have not grown for that long while it was
seeding: the clock runs only in the seeding state and restarts with the daemon,
a torrent set to seed forever is exempt, and the action is the same pause or
removal as the other limits.

## Super-seeding (BEP 16)

On a **completed** torrent super-seeding can be activated: the engine does not
announce the whole bitfield, but **one piece at a time** per peer, and serves only the
pieces offered to that peer. It is for *initial seeding*: it pushes the swarm to
exchange the data instead of downloading it all from the seed, at the price of a
**deliberately lower** seed upload.

- `POST /api/v1/torrents/{hash}/super-seeding` with `enabled=1/0`, or the checkbox
  on the daemon page; Gextto activates it from the **General** tab of the detail.
- It applies only to a completed torrent: activated earlier, it comes into effect at
  completion; on a downloading or stopped torrent it is inert.
- It is a **seeding strategy**: it does not touch the queue, slots, seed policy or bandwidth
  limits. When Gextto's seed policy (ratio/days) stops the torrent,
  super-seeding stops with it.
- The flag is persisted in the daemon's resume, so it survives a restart.
- Like libtorrent, `have` and `not-interested` advance the announces; the pieces
  offered are not repeated and, when they are finished, the peer is released with the
  full bitfield.

Gextto reports the capability `super_seeding` for gx-torrent (`full`).

## Writes to the state disk

The daemon's state (`DATA_DIR/gx-torrent`: `session.db`, `state.json`, logs)
stays on the local disk: `session.db` is a bbolt database (mmap and lock), not
suitable for NFS. Writes are contained: the engine saves statistics and bitfield
every 2 minutes (the daemon raises to 2 minutes the 30 s default of
`ResumeWriteInterval`), with a fsync of the data before every bitfield
write: about 100 MB per day at rest; the log rotates at
5 MB × 4.

## Bandwidth limits

Global limits change on the fly (scheduling, temporary limit): the engine
uses a limiter whose rate updates while the peers use it,
so no reopening of the session and no peer lost. Gextto sends the
configuration only when it changes. Only the peer limits
(`max_peer_dial`/`max_peer_accept`), the cache expiry and the
preallocation reopen the session.

Every torrent has its **own speed limit** (download and upload, KiB/s):
`-1` inherits the global, `0` is unlimited, a positive value is the limit. The
torrent's peers use a dedicated limiter which, when the torrent inherits,
forwards to the session limiter: a change applies immediately, without reconnecting, and
the global limit continues to apply to the sum of the torrents that inherit it.
It is set with `POST /api/v1/torrents/{hash}/seed-limits` (fields
`download_limit`, `upload_limit`) or from the daemon page; Gextto does it from the
**Limits** tab of the torrent detail. Gextto keeps the values between one restart
and the next and the daemon reapplies them to the reloaded session.

Every torrent also has its own **connection limit** and **upload
slots** (`POST /api/v1/torrents/{hash}/conn-limits`, fields `max_connections` and
`max_uploads`; `-1` and `0` both mean no per-torrent cap — the global
budget `max_peer_dial`/`max_peer_accept` remains). A torrent without a cap of
its own takes the session default `max_connections_per_torrent` (queue policy,
0 = no cap): Gextto pushes it from *Max connections per torrent*, the daemon page
has *Peers per torrent*, and the qBittorrent-compatible API maps it to
`max_connec_per_torrent` (-1 = no cap). It changes in place on the running
torrents and is reapplied after a session reload; clearing a torrent's own cap
returns it to the default. The connection limit applies
to the connections established; if it is lowered below the current number,
the excess ones are closed immediately. The upload slots change the
number of peers the unchoker keeps unblocked.

**Automatic per-torrent cap** (`conn_auto` in the queue policy, on by default;
*Automatic connections* on the daemon page, *Connessioni automatiche* in
Gextto). Instead of a fixed number, the daemon sizes the cap of each torrent
without a limit of its own from the machine. Two numbers come from it: the
allowance of one torrent — about ten connections per core plus a flat fifteen,
bounded to 15–200, at most 30 below 4 GiB of RAM and halved under 768 MiB
available — and the budget of the whole session, 75 connections per core with
the same memory rules (300 on a 4-core host). Each running torrent asks for its
allowance; a seed asks for half (the upload line is the narrow one) and for the
floor of 15 once nobody has downloaded from it for two minutes; a download that
keeps at least ten peers without moving 256 KiB in two minutes asks for half,
so dead connections make room for queued addresses. When the asks exceed the
budget it is shared by weight — a moving download counts twice — with nobody
under 15 or above its ask, and what a small ask leaves is shared again among
the others. One torrent alone always gets its whole allowance, so the budget
only binds when many torrents run together. The cores are `GOMAXPROCS`, which
follows a container or systemd CPU quota. The pass runs every 30 seconds on the
queue loop, applies with `SetMaxConnections` (no session restart, no
re-announce) and logs each new allowance and each change (`peer cap for «…»: 55
→ 27 (no progress with 34 peers)`; changes of one pass to the same value share a line,
`peer cap 33 for 9 torrents (machine allowance, shared with 9 running torrents): …`).
`max_connections_per_torrent` stays the ceiling when above zero, a torrent's
own limit always wins, and the peers of such a torrent are taken out of the
budget first. The reason is in [`measurements.md`](measurements.md) §4.1: CPU
grows with the number of established peers (about 0.1 % of a core each) and
hardly with the bytes when the line is the limit.

**Seed-to-seed connections.** While a torrent seeds, a peer whose availability
(bitfield, have-all or the last have) shows that it already holds every piece
we hold is closed at once: both sides only upload, so the connection would cost
a socket, keep-alives and a place in every per-peer loop for nothing
(libtorrent does the same).

### Auto-speed on latency (anti-bufferbloat)

**Standalone** option (settings.json, like the bandwidth scheduler), inspired by
BiglyBT's *Ping Auto-Speed*. A probe measures every second the **round-trip
latency** with a TCP connection towards the **gateway** (ports 80, 443,
53) or towards a configured host; the lowest value of the last 60 s is the
**resting baseline**. When the latency exceeds `baseline × 1.5` **and**
`baseline + 20 ms`, the daemon **lowers the upload** by 15% with respect to the limit in
force (never below the configured minimum); when the line returns to rest it
raises it again by `limit/20` at a time. Moves are at most one every 3 s
(`internal/autospeed`, the controller shared with Gextto's latency guard). It reduces **only** the effective upload: the global
or scheduled limit remains the cap, and an unlimited limit (or the absence of
measurements) disables the clamp.

Keys in settings.json: `autospeed-enabled` (default `false`),
`autospeed-target` (host or `host:port`; empty = gateway), `autospeed-min`
(KiB/s, default 50). A **positive upload limit** (global or
scheduled) is needed for auto-speed to act; the page enforces it. The decisions
appear in the log with reason `auto-speed` (`effective speed limits …`). The
line is written once a value has stayed put for ten seconds, so the controller's
three-second steps do not each get a line: a ramp ends with one line carrying
the cap the session actually runs with. The
settings page shows the measured latency and the current cap.

## REST API (v1)

For monitoring and automation the daemon also serves `GET /api/v1/metrics`
(Prometheus text format, computed only when scraped) and `GET /api/v1/events`
(the engine events as Server-Sent Events, with the API token). The
qBittorrent-compatible `/api/v2`, mounted **only in standalone mode**, includes
the qBittorrent 5 calls `torrents/count`, `transfer/setSpeedLimitsMode` and the
web seed editing calls; in managed mode Gextto is the single master and
`/api/v2` is not exposed. Examples and the full route list:
[`API.md`](API.md), *Integrating with other systems*.

All requests require `X-Gx-Token` (or `Authorization: Bearer`) if the
token is set. The complete route tables of `/api/v1` and of the
qBittorrent-compatible `/api/v2` are in [`API.md`](API.md); this section
describes the payloads of the v1 resources.

| Method and path | Description |
|---|---|
| `GET /api/v1/health` | status and version |
| `GET /api/v1/stats` | counters: downloading, seeding, queued, stalled, slow, speeds, peers, effective slots |
| `GET /api/v1/portcheck` | port test: the peer port is listening and the router forwards it |
| `GET /api/v1/torrents` | full list (progress %, sizes, speeds, peers, swarm, status, path, seed limits, queue flags). Optional `?limit=`/`?offset=` page the list (the offset may be negative, counted from the end); without them the response is unchanged |
| `GET /api/v1/torrents/{hash}` | the published view of one torrent (cheap polling). Additive: the list and the `{hash}/{what}` resources are unchanged |
| `POST /api/v1/add` | form fields: `magnet`, `destination`, `paused`, `top`, `sequential`, `first_last`, `super_seeding`, `stop_at_metadata`, `seed_ratio`, `seed_days`. Responds `{hash, existing}` |
| `POST /api/v1/add-file` | multipart `torrent` plus the same fields |
| `POST /api/v1/create-torrent` | JSON `path`/`name`/`trackers`/`webseeds`/`private`/`piece_length`/`comment`: creates a `.torrent` from a file or folder of the server and returns it as a download (mktorrent-style). `piece_length` (multiple of 16 KiB, 0 = automatic) is limited to 16 MiB so as not to turn an arbitrary value into a huge allocation |
| `DELETE /api/v1/torrents/{hash}?delete_files=1` | removal |
| `POST /api/v1/torrents/{hash}/{action}` | see list below |
| `GET /api/v1/torrents/{hash}/{what}` | inspection with `{what}` in `files`, `peers`, `trackers`, `webseeds`, `pieces`, `torrent-file`, `scrape`, `dhtscrape`, `collections` (files include `priority`; `webseeds` lists the HTTP sources; `pieces` reports the status intervals; `scrape` queries the trackers for seeders/leechers, BEP 48; `dhtscrape` estimates the swarm from the DHT, BEP 33; `collections` reports the BEP 38 hints — `collections`, `similar` in hex and the local torrents that share a hint) |
| `POST /api/v1/pins/clear` | removes all pins |
| `POST /api/v1/ipfilter` | reloads the IP filter (optional `path`) |
| `GET /api/v1/config` | configuration and effective slots |
| `POST /api/v1/config` | partial JSON: unknown keys rejected |
| `GET /api/v1/metrics` | state in the Prometheus text format, computed only when scraped |
| `GET /api/v1/events` | SSE of the session events (`torrent_added`, `torrent_completed`, `torrent_stopped`, `torrent_error`, `torrent_piece`, `peer_connected`, `peer_disconnected`, `tracker_announce`) |

The page's own live updates use `GET /ui/events`, the session-authenticated twin of
`/api/v1/events` (no token: it follows the page's login), and refresh the table
immediately on the same events.

Actions available on `POST /api/v1/torrents/{hash}/{action}`:

- `pause`, `resume`, `verify`, `reannounce`;
- `restart` (with `probe_secs`), `park`, `unpark`;
- `pin` (with `pinned=1/0`), `top`;
- `rename` (with `name`: renames the torrent, content root and display name),
  `rename-file` and `rename-folder` (with `old` and `new`: renames a file or a
  folder of the torrent);
- `move` and `associate` (with `destination`);
- `seed-limits` (`seed_ratio`, `seed_days`, `download_limit`, `upload_limit` in KiB/s: -1 global, 0 unlimited);
- `conn-limits` (`max_connections`, `max_uploads`: -1 and 0 = no per-torrent cap);
- `trackers` (with `urls`, one per line);
- `set-trackers` (with `urls`, one per line: **replaces** the list, an empty list clears it);
- `webseeds` (with `urls`, one per line, and `remove=1` to remove them);
- `super-seeding` (with `enabled=1/0`: BEP 16 on the completed torrent);
- `file-priorities` (with `priorities`, comma-separated; 0 = skip, 1..4 normal, 5..7 high).

Keys accepted by `POST /api/v1/config`:

- `active_downloads`, `active_seeds`, `active_limit`;
- `dont_count_slow`, `slow_rate`, `slow_after_secs`, `slow_rotate_secs`;
- `dynamic_queue`, `dynamic_min`, `dynamic_max`;
- `speed_limit_download`, `speed_limit_upload` (KiB/s);
- `max_peer_dial`, `max_peer_accept`;
- `max_connections_per_torrent` (default peer cap of a torrent without its own;
  0 = no cap, applied in place);
- `cache_mb`, `cache_ttl_secs`, `preallocate` (cache and preallocation: `cache_mb`
  ≤ 0 = adaptive);
- `auto` (self-management: dynamic queue and adaptive cache);
- `sequential` (default for torrents added afterwards).
- `min_free_mb` (disk guard threshold in MiB, 0 = off; see *Disk cache, preallocation
  and mmap storage*). Torrents paused by it carry `disk_wait: true` in
  `GET /api/v1/torrents`.

## API availability (snapshot and lock)

Gextto queries `GET /api/v1/torrents` at every poll (1.5 s) with a timeout of
15 s: if the response does not arrive, the daemon is considered unreachable.
The list is therefore served from a **snapshot** published by the queue tick and
by the commands that change the state, **without taking `d.mu` and without touching the
run loop of any torrent**. A torrent blocked on storage I/O (for example
a long copy to a network mount) cannot therefore make the API seem dead.

The tick works in three phases, so as never to hold `d.mu` during an operation that
can block:

1. **Sampling** (without lock): for each torrent it reads stats, peers and trackers
   from its run loop and classifies the storage (the `statfs` on the network mount can
   block for a long time).
2. **Application** (with `d.mu`): it updates meta and queue from the samples,
   computes the plan and publishes the snapshot. No call to the run loop here.
3. **Actions** (without lock): it runs the `Start`/`Stop` decided by the plan; if the run
   loop is busy, the action is simply retried at the next tick.

The snapshot can be at most one tick old (3 s default); every
command that changes the state immediately restarts a tick, so the UI and Gextto
see the change almost immediately.

The **addition** of a torrent also calls the engine without `d.mu`: a magnet can
first resolve a DHT item (BEP 46, up to 10 s) or download the `.torrent` from
an `xs=`/`as=` source, and during that wait API, page and queue must stay
responsive. The id being added is in `d.adding`, so the tick does not start the torrent
before its state (pause, position, category…) is recorded; if in the
meantime the session has been rebuilt, the addition fails with a clear
error. Test `TestAddDoesNotHoldTheLockWhileResolving`.

## Known limits (engine)

Operations that the engine does not support respond with an explicit capability
error (`ErrCapabilityUnavailable`), never with a false success:

- v2 as **seed**: the `hash request` requests of the block layer
  (`base = 0`) are served for a file **entirely present** (built once by reading
  the data, then cached) and, for a file only **partly downloaded**, for the
  requested 16 KiB leaf hashes whose pieces are verified (anchored with the
  piece layer's uncles); a request that touches a missing piece, folds below the
  piece layer or asks for more than 512 hashes responds `hash reject`
  (see *BitTorrent v2 torrents*);
- IPv6: **partial support** (see *IPv6* below).

**Sequential download** and **first/last part** priority are supported
(see the dedicated section): the engine downloads first the edges of
every file (~1% of the size, up to 8 MB), so players immediately find
the index. The first/last part is **independent** of the sequential
order.

The upload slots and the per-torrent connections are **not** a limitation: they are
set with `conn-limits` (and the bandwidth limits with `seed-limits`), as seen
in *Bandwidth limits*.

### IPv6

The single-port listener (TCP) and the UDP socket shared by uTP and DHT are
**dual-stack**: if the listen host is unspecified (`0.0.0.0`, `::` or
empty) a single socket is opened that accepts both IPv4 and IPv6 and, if the machine
has no IPv6, it falls back to IPv4. A specific address remains single-family.

IPv6 peers are discovered from the trackers (`peers6` field, BEP 7), from the PEX
exchange (`added6`/`dropped6`, BEP 11) and from **DHT discovery over IPv6** (BEP 32): the DHT
shares the dual-stack socket, keeps v4 and v6 nodes separate in the fields
`nodes`/`nodes6` and requests both families (`want`). Peers, HTTP and
**UDP** trackers, and web seeds are also resolved over IPv6 (with a preference for IPv4
when the name has both). The extended handshake `yourip` field is
accepted in both IPv4 and IPv6 form, the **IP filter** accepts IPv6 CIDR rules
(the range/P2P/eMule forms remain IPv4) and the engine accepts IPv6 DHT
peers (18-byte contacts).

Only the LSD announcement over **IPv6** is missing in networks where v6 multicast is not
available: if the v6 listener does not open, the LSD stays active on IPv4. In the
other cases the LSD also announces and listens on the group `[ff15::efc0:988f]:6771`
(the BEP 14 multicast group).

## Profiling (CPU and heap)

The daemon exposes the Go profiles on demand for performance work. The endpoint
is off by default and never reachable from the network: set `GX_TORRENT_PPROF` to
a **loopback** `host:port` (for example `127.0.0.1:6060`) and restart the daemon.
A non-loopback value is logged and ignored, so the endpoint cannot be exposed by
mistake (`TestAddress` in `internal/debugpprof` pins the rule).

```bash
GX_TORRENT_PPROF=127.0.0.1:6060 ./gx-torrent -mode standalone ...
# CPU profile (30 s) and heap snapshot
go tool pprof -top -seconds 30 http://127.0.0.1:6060/debug/pprof/profile
go tool pprof -top http://127.0.0.1:6060/debug/pprof/heap
```

The profiles available under `/debug/pprof/` are `profile`, `heap`, `goroutine`,
`allocs`, `block`, `mutex`, `trace`, `cmdline` and `symbol`. The endpoint is
served by its own listener and is deliberately **not** part of the REST API: no
token, no TLS. Gextto never sets the variable, so a managed daemon stays without
a profiling port; to profile one, set it in the environment of the `gextto`
service and restart the daemon (the daemon is restarted because the environment
is inherited at start-up).

Gextto itself (`gexttod`) has the same endpoint, from the same package
(`internal/debugpprof`), behind its own variable `GEXTTO_PPROF` (for example
`127.0.0.1:6061`), with the same loopback-only rule. For the user service:
`systemctl --user set-environment GEXTTO_PPROF=127.0.0.1:6061
GX_TORRENT_PPROF=127.0.0.1:6060` and a restart; `unset-environment` and another
restart remove them.

**Garbage collector.** The daemon sets `GOGC=50` at start-up unless `GOGC` is
already in its environment (`cmd/gx-torrent/gc.go`). Most of the heap is bounded
buffers (the write cache's piece buffers, the read cache), and the collector
costs about 1 % of the CPU under a full download. Go's default of 100 lets the
heap reach twice the live data before a collection; 50 trims that headroom from
the resident memory. Measurements in `docs/measurements.md`.

## Tests

- `go test ./cmd/gx-torrent/` covers:
  - the scheduler (slots, slow, rotation with cooling, hard cap,
    forced, dynamic queue);
  - a real lifecycle on a gx-core session without network (addition, verification,
    user pause respected by the queue, move, removal that preserves the
    data, targeted deletion, parking and probe, persistence after the restart
    of the session);
  - token and configuration;
  - the web page (page, live fragments, actions, token, favicon) and the
    translations of its language;
  - the origin of the peers (including `holepunch`), the holepunch tied to uTP and the
    port test;
  - the LSD discovery, the piece diagnostics and the streaming with `Range`;
  - real transfers between two local daemons:
    - through the single port, in the clear and with forced encryption;
    - with file selection and reactivation of an excluded file;
    - via SOCKS5 and HTTP proxy;
    - with a move from RAM disk at mid-download;
    - uTP only, with the DHT on the same UDP socket;
    - with the peer found via LSD (real multicast);
  - loading of the IP filter.
  - (opt-in, `GX_MEASURE=1` / `make measure-seeding`) the seeding/choking measurement
    on a local swarm, with bytes and times per peer: method and metrics in
    `docs/measurements.md` (§4.6).
- `go test ./internal/blocklist` in `internal/gxcore` covers the formats of the
  IP filter.
- `make test-engine` also covers the deterministic choking harness
  (`internal/unchoker/sim_test.go`) and super-seeding
  (`torrent/torrent_superseed_test.go`).
- `go test -run GxEngine .` covers the adapter:
  - statuses and events;
  - cache on interruption;
  - park, probe and unpark;
  - sending of the queue policy;
  - operations and token;
  - `storage_moved`.
