# Gextto HTTP API

This is the table of the public routes registered by the daemon. It is a
surface reference, not a stable integration contract for every payload: before
automating an action, check the request and the response on the running build.

The document covers two servers: the **Gextto daemon** (`/api/…`, the routes
below) and the **gx-torrent daemon**, which exposes its own native `/api/v1` and
a qBittorrent-compatible `/api/v2` (see *gx-torrent daemon routes* at the end).

## Before using the API

- By default the API is open, as on a trusted LAN. With
  *Configuration → Access and services → Require login* enabled, a client
  outside the local network must send the API key in the `X-Api-Key` header (or
  `?apikey=` in the address) or the session cookie obtained from `/login`;
  without credentials the `/api/…` routes answer `401`. For access from the
  Internet use HTTPS anyway (a reverse proxy with a certificate, or a VPN).
- `/login` and `/logout` exist only when protected access is enabled.
- Routes that change configuration, files, torrents or databases normally use
  `POST`/`DELETE`; treat them as administrative actions and keep backups. A
  browser request that declares itself cross-site (an `Origin`/`Referer` from
  another site, or `Sec-Fetch-Site: cross-site`) is refused with `403`: this
  stops a page on another origin from driving the API through the browser of a
  user on the LAN. Same-origin calls and non-browser clients (curl, the TUI,
  scripts) are unaffected.
- Placeholders in curly braces, for example `{hash}`, are part of the path and
  must be encoded correctly by the client.
- Some routes are historical aliases (`run_now`/`run-now`, for example). New
  integrations should prefer the more readable form when available.
- Long operations (`/api/scan-all-archives`,
  `/api/maintenance/backfill-media-info`, `/api/rename-all`) answer `202` with a
  `job_id`: follow state, progress and outcome with `GET /api/jobs/{id}` and
  cancel with `POST /api/jobs/{id}/cancel`. The full list is at `GET /api/jobs`.

The official server-rendered UI is at `/`; `/v2` is a technical alias. The
internal HTMX routes under `/v2` are not part of this integration table and may
change. `/ui` is not served. For operational checks use
`GET /api/health` and `GET /api/status` first.

## Gextto routes

| Method | Path | Description |
|---|---|---|
| GET | `/` | web interface (the v2 page at the root) |
| POST | `/api/access/generate-key` | generate a new API access key |
| GET | `/api/acquisitions` | acquisition history |
| GET | `/api/archive` | archive entries (search and pages) |
| GET | `/api/ed2k/status` | aMule integration state: enabled, reachable, download, last_error and, when aMule answers, status (version, eD2k server and High/Low ID, Kad state, speeds and limits, network size, session_sent/session_recv in bytes since amuled started, total_sent/total_recv over all its runs, shared_files) |
| GET | `/api/ed2k/downloads` | aMule's download queue: hash, name, size, done, progress, speed, state (queued, downloading, paused, checking, finished, error), sources and active sources, priority, ed2k link, last data received (Unix time); enabled is false while the integration is off |
| POST | `/api/ed2k/downloads/{hash}/priority` | set a download's priority: JSON {"priority": "low", "normal", "high" or "auto"} |
| POST | `/api/ed2k/downloads/{hash}/{action}` | pause, resume or remove an aMule download (remove cancels a download in progress and deletes its partial data; a completed file only leaves aMule's list) |
| GET | `/api/ed2k/limits` | aMule's global bandwidth limits: download_kib, upload_kib (0 = unlimited) |
| POST | `/api/ed2k/limits` | set aMule's global bandwidth limits: JSON {"download_kib": N, "upload_kib": N}, 0 = unlimited |
| GET | `/api/ed2k/log` | aMule's own log lines |
| GET | `/api/ed2k/shared` | the files aMule shares with what it uploaded for each (session and total bytes, requests and accepted requests session/total, queued clients, live upload rate and last upload time, priority, ed2k link), the upload queue (client, software, file, speed, upload position) and the shared folders; enabled is false while the integration is off |
| POST | `/api/ed2k/shared/reload` | ask aMule to rescan its shared folders, so files added or removed outside aMule show up (the reload button of the aMule GUI) |
| POST | `/api/archive/add` | add an entry to the archive |
| POST | `/api/archive/batch-download` | download several archive entries (magnet or ed2k_link) |
| POST | `/api/archive/delete` | delete archive entries |
| GET | `/api/backup` | list backups |
| POST | `/api/backup` | create a backup |
| GET | `/api/backup/list` | list backups |
| POST | `/api/backup/verify` | verify a backup archive |
| POST | `/api/backup/send-telegram` | send a backup through Telegram |
| GET | `/api/backup/settings` | backup settings |
| POST | `/api/backup/settings` | save the backup settings |
| POST | `/api/backup/test-ftp` | test the FTP backup destination |
| GET | `/api/blocklist` | blocked releases |
| POST | `/api/blocklist/{hash}/remove` | remove a blocklist entry |
| GET | `/api/browse_dir` | list a directory for the folder picker (?path=; files=video also lists its video files) |
| GET | `/api/browser-handlers/download` | download a browser integration script or .desktop file |
| GET | `/api/calendar` | upcoming episodes (calendar) |
| GET | `/api/comics` | list comics |
| POST | `/api/comics` | add a comic |
| POST | `/api/comics/check-links` | check the comic source links |
| POST | `/api/comics/cycle` | run a comics cycle now |
| POST | `/api/comics/download` | download comic issues |
| GET | `/api/comics/downloads` | comic downloads in progress |
| POST | `/api/comics/downloads/tag` | tag a comic download |
| POST | `/api/comics/downloads/{id}/pause` | pause a comic download |
| POST | `/api/comics/downloads/{id}/remove` | remove a comic download |
| POST | `/api/comics/downloads/{id}/resume` | resume a comic download |
| POST | `/api/comics/explore` | search comics on the source |
| GET | `/api/comics/history` | comic download history |
| POST | `/api/comics/history/delete` | delete a comic history entry |
| POST | `/api/comics/history/clear` | clear the comic history |
| POST | `/api/comics/weekly/history/clear` | clear the weekly-pack history |
| POST | `/api/comics/links` | save the comic source links |
| GET | `/api/comics/weekly` | weekly comic packs |
| POST | `/api/comics/weekly/links` | save the weekly-pack links |
| POST | `/api/comics/weekly/settings` | weekly-pack settings |
| DELETE | `/api/comics/{id}` | remove a comic |
| POST | `/api/comics/{id}/enabled` | enable or disable a comic |
| GET | `/api/quality-profiles` | named quality profiles (the list behind the v3 qualityprofile endpoint) |
| POST | `/api/quality-profiles` | save the named quality profiles |
| GET | `/api/v3/system/status` | Sonarr/Radarr v3 system status for a request manager |
| GET | `/api/v3/qualityprofile` | v3 quality profiles: the named profiles, with stable ids |
| GET | `/api/v3/qualityProfile` | same as /api/v3/qualityprofile (the spelling Jellyseerr uses) |
| GET | `/api/v3/languageprofile` | v3 language profile (the default language) |
| GET | `/api/v3/rootfolder` | v3 root folders (archive root and each series archive path) |
| GET | `/api/v3/tag` | v3 tags |
| GET | `/api/v3/series/lookup` | v3 series lookup (?term=, also tvdb:ID: a configured series comes back with its id and monitored seasons) |
| POST | `/api/v3/series` | v3 add series (only the seasons marked monitored are followed) |
| PUT | `/api/v3/series` | v3 update series: adds the newly monitored seasons to an existing series |
| GET | `/api/v3/movie/lookup` | v3 movie lookup (?term=, also tmdb:ID: a configured movie comes back with its id, monitored and hasFile) |
| POST | `/api/v3/movie` | v3 add movie (the response carries the library id) |
| PUT | `/api/v3/movie` | v3 update movie: monitors a movie already in the library |
| GET | `/api/v3/queue` | v3 queue (the torrent session) |
| GET | `/api/import/preview` | manual import: the video files of a path with their match and destination (?path=) |
| POST | `/api/import` | manual import: rename, move and register one file |
| GET | `/library/bulk/profile` | Series/Movies "Edit selected" panel: the value fields filled from a quality profile (?profile=ID&view=) |
| POST | `/library/bulk` | Series/Movies "Edit selected" panel: apply the ticked values to the ticked rows |
| GET | `/import/preview` | manual import page: preview fragment |
| POST | `/import/apply` | manual import page: confirm one file |
| GET | `/api/config` | read the configuration |
| POST | `/api/config` | save the configuration |
| POST | `/api/config/add-all-from-archive` | add the series found in the archive folder to the configuration |
| POST | `/api/config/add_all_from_archive` | alias of add-all-from-archive |
| GET | `/api/config/check-ports` | check the configured transfer port range |
| GET | `/api/config/library` | library folders |
| POST | `/api/config/library` | save the library folders |
| POST | `/api/config/movies` | save the movie defaults |
| POST | `/api/config/series` | save the series defaults |
| POST | `/api/config/settings` | save one setting |
| DELETE | `/api/config/settings/{key}` | reset one setting |
| GET | `/api/config/source-filters` | per-source filters |
| POST | `/api/config/source-filters` | save the per-source filters |
| GET | `/api/cycle-history` | acquisition cycle history |
| GET | `/api/cycles` | acquisition cycle history |
| POST | `/api/database/cleanup` | clean the database |
| POST | `/api/database/rescore` | rescore the archived releases |
| POST | `/api/db/action` | vacuum, analyze or integrity-check the databases |
| GET | `/api/db/info` | database size and table counts |
| POST | `/api/db/prune` | prune the database |
| POST | `/api/db/prune-by-ids` | prune the selected rows |
| POST | `/api/db/prune-keyword` | prune the rows matching a keyword |
| POST | `/api/db/prune/preview` | preview a prune |
| GET | `/api/debug/torrent_match` | explain a torrent match (debug) |
| GET | `/api/download-tags` | download tags |
| POST | `/api/download-tags` | add a download tag |
| DELETE | `/api/episodes/{series}/{season}/{episode}` | delete an episode file |
| POST | `/api/episodes/{series}/{season}/{episode}/force` | force the search of an episode |
| POST | `/api/episodes/{series}/{season}/{episode}/ignore` | ignore an episode |
| POST | `/api/episodes/{series}/{season}/{episode}/redownload` | redownload an episode |
| POST | `/api/episodes/{series}/{season}/{episode}/search` | search one episode |
| GET | `/api/episodes/{series}/{season}/{episode}/sources` | candidate sources for an episode |
| GET | `/api/event-hooks` | event hooks (webhooks and commands) |
| POST | `/api/event-hooks` | save the event hooks |
| GET | `/api/feed.xml` | magnet feed of the recent releases |
| GET | `/api/feed/status` | feed status |
| POST | `/api/flaresolverr/test` | test FlareSolverr |
| GET | `/api/gaps` | missing episodes |
| GET | `/api/health` | health check |
| GET | `/api/i18n` | UI translations |
| POST | `/api/i18n` | save a translation |
| GET | `/api/i18n/active` | active language and catalogue |
| POST | `/api/i18n/active` | set the active language |
| GET | `/api/i18n/export/{lang}` | export a catalogue |
| POST | `/api/i18n/import/{lang}` | import a catalogue |
| POST | `/api/i18n/language` | set the interface language |
| GET | `/api/i18n/languages` | available languages |
| DELETE | `/api/i18n/{lang}` | delete a language |
| POST | `/api/indexer/test` | test an indexer |
| POST | `/api/jellyfin/refresh` | refresh the Jellyfin library |
| POST | `/api/jellyfin/test` | test Jellyfin |
| GET | `/api/jobs` | background jobs |
| GET | `/api/jobs/{id}` | state and progress of one job |
| POST | `/api/jobs/{id}/cancel` | cancel a job |
| GET | `/api/last_cycle` | last cycle summary |
| GET | `/api/last_cycles` | recent cycles |
| GET | `/api/license` | license text |
| POST | `/api/log-level` | set the log level |
| GET | `/api/log_level` | log level |
| POST | `/api/log_level` | set the log level (alias) |
| GET | `/api/logs` | recent log lines |
| GET | `/api/logs/stream` | live log (Server-Sent Events) |
| POST | `/api/maintenance/backfill-media-info` | probe the missing media info |
| POST | `/api/maintenance/clean-duplicates` | remove the duplicate library files |
| POST | `/api/maintenance/clean-trash` | empty the recycle bin |
| POST | `/api/maintenance/housekeeping` | run the housekeeping now |
| POST | `/api/maintenance/restore-source` | restore files from the recycle bin |
| POST | `/api/maintenance/rename-folder/scan` | scan the folders to rename |
| POST | `/api/maintenance/rename-folder/apply` | apply a folder rename |
| GET | `/api/manual-search` | manual search state and results |
| GET | `/api/media-info` | media info of the library files |
| POST | `/api/media-info/probe` | probe a file with ffprobe |
| POST | `/api/missing/search` | search the missing episodes |
| POST | `/api/mkdir` | create a directory |
| GET | `/api/movies` | list movies |
| GET | `/api/movies/history` | movie download history |
| GET | `/api/movies/seen` | seen movies |
| GET | `/api/movies/seen/grouped` | seen movies, grouped |
| DELETE | `/api/movies/{id}` | remove a movie |
| GET | `/api/movies/{id}` | movie details |
| POST | `/api/movies/{id}` | update a movie |
| POST | `/api/movies/{id}/metadata` | apply metadata to a movie |
| POST | `/api/movies/{id}/metadata/search` | search movie metadata |
| POST | `/api/movies/{id}/redownload` | redownload a movie |
| POST | `/api/movies/{id}/search` | search a movie |
| GET | `/api/network/interfaces` | network interfaces |
| GET | `/api/notifications/stream` | live notifications (Server-Sent Events) |
| POST | `/api/plex/refresh` | refresh the Plex library |
| POST | `/api/plex/test` | test Plex |
| GET | `/api/process-metrics` | CPU and memory of the Gextto process |
| GET | `/api/providers/status` | status of the providers |
| POST | `/api/providers/status` | clear the provider status |
| GET | `/api/ramdisk` | RAM disk state |
| POST | `/api/ramdisk/create` | create the RAM disk |
| POST | `/api/ramdisk/select` | select the RAM disk |
| GET | `/api/ramdisk_check` | check whether a path is a RAM disk |
| GET | `/api/recent-downloads` | recent downloads |
| POST | `/api/rename-all` | rename the whole library |
| GET | `/api/rename-progress` | rename progress |
| POST | `/api/run-now` | start an acquisition cycle now |
| GET | `/api/run_now` | start a cycle now (alias) |
| POST | `/api/run_now` | start a cycle (alias) |
| POST | `/api/scan-all-archives` | scan every series archive |
| POST | `/api/score/preview` | score a release with the current weights |
| POST | `/api/search` | manual search |
| POST | `/api/search/dashboard` | manual search for the dashboard (with the rejected ones) |
| POST | `/api/search/add` | add a search result |
| POST | `/api/search/archive` | search the archive |
| POST | `/api/search/explain` | explain a release decision |

With the aMule integration enabled (`ed2k_enabled`), manual search results can
carry `network: "ed2k"`, `ed2k_link` and `sources` next to the torrent fields.
eD2k is searched only when the request asks for it: send
`{"query":"…","kad":true}` to `POST /api/search` or `/api/search/dashboard` (the
UI has a **Kad** checkbox); the network is the one set in `ed2k_search_network`.
Such a result has no `magnet`: `POST /api/search/add` hands its link to aMule,
answers `409` when the integration is off or aMule already has the file, and
`502` when aMule cannot be reached. Use `GET /api/ed2k/status` for the live
state.
| GET | `/api/seed-rules` | seed rules: per-source/category seeding limits |
| POST | `/api/seed-rules` | save the seed rules (list of name, source, category, ratio, seed_days, idle_minutes; null = global, 0 = no limit) |
| POST | `/api/send-magnet` | send a magnet to the engine |
| GET | `/api/series` | list series |
| GET | `/api/series/all-missing` | missing episodes (alias of gaps) |
| GET | `/api/series/calendar` | upcoming episodes |
| GET | `/api/series/seen` | seen episodes |
| GET | `/api/series/seen/grouped` | seen episodes, grouped |
| GET | `/api/series/{name}` | series details |
| POST | `/api/series/{name}` | update a series |
| DELETE | `/api/series/{name}` | remove a series |
| GET | `/api/series/{name}/episodes` | episodes of a series |
| GET | `/api/series/{name}/info` | metadata from the provider |
| POST | `/api/series/{name}/metadata` | refresh the metadata |
| POST | `/api/series/{name}/path` | set the series folder |
| POST | `/api/series/{name}/rename-execute` | rename the series files |
| POST | `/api/series/{name}/rename-preview` | preview a series rename |
| POST | `/api/series/{name}/scan-archive` | scan the series archive |
| POST | `/api/series/{name}/search-missing` | search the missing episodes of a series |
| POST | `/api/series/{name}/toggle-season` | enable or disable a season |
| POST | `/api/service/restart` | restart the Gextto service |
| GET | `/api/services` | status of the external services |
| POST | `/api/set-speed-limits` | set the global speed limits |
| GET | `/api/setup` | first-run setup status |
| POST | `/api/setup/complete` | complete the setup |
| POST | `/api/simkl/auth/poll` | poll the SimKL login |
| POST | `/api/simkl/auth/revoke` | revoke the SimKL token |
| POST | `/api/simkl/auth/start` | start the SimKL login |
| GET | `/api/simkl/calendar` | SimKL calendar |
| POST | `/api/simkl/mark-watched` | mark watched on SimKL |
| POST | `/api/simkl/scrobble` | scrobble to SimKL |
| GET | `/api/simkl/settings` | SimKL settings |
| POST | `/api/simkl/settings` | save the SimKL settings |
| GET | `/api/simkl/status` | SimKL status |
| GET | `/api/simkl/watchlist` | SimKL watchlist |
| POST | `/api/simkl/watchlist/import` | import the SimKL watchlist |
| GET | `/api/sources/health` | health of the sources |
| GET | `/api/stats` | library statistics |
| GET | `/api/status` | daemon status, version and build |
| GET | `/api/system/lt_mem_suggest` | suggested engine memory limits |
| GET | `/api/system/stats` | host statistics |
| GET | `/api/tag-dir-rules` | folder tag rules |
| POST | `/api/tag-dir-rules` | save the folder tag rules |
| POST | `/api/test-notification` | send a test notification |
| POST | `/api/tmdb/add` | add a title from TMDb |
| POST | `/api/tmdb/discover` | discover titles on TMDb |
| POST | `/api/tmdb/search` | search TMDb |
| GET | `/api/torrent-backend` | torrent backend status |
| GET | `/api/torrent-backend/qbittorrent/update` | qBittorrent update status |
| POST | `/api/torrent-backend` | activate a torrent backend |
| POST | `/api/torrent-backend/preflight` | preflight checks before switching backend |
| POST | `/api/torrent-backend/qbittorrent/update` | update qBittorrent |
| POST | `/api/torrent-backend/test` | test a torrent backend |
| POST | `/api/torrent-backend/portcheck` | check the backend peer port |
| GET | `/api/torrent-migrations` | torrent migration status |
| POST | `/api/torrent-migrations/cancel` | cancel a migration |
| POST | `/api/torrent-migrations/plan` | plan a migration |
| GET | `/api/torrent-events` | recent torrent events |
| GET | `/api/torrent-no-rename` | torrents excluded from renaming |
| GET | `/api/torrent-tags` | torrent tags |
| POST | `/api/torrent-tags` | set a torrent tag |
| GET | `/api/torrents` | list torrents |
| POST | `/api/torrents` | add a torrent |
| POST | `/api/torrents/add` | add a torrent |
| GET | `/api/torrents/history` | torrent history |
| GET | `/api/torrents/ipfilter_status` | IP filter status |
| POST | `/api/torrents/ipfilter_update` | update the IP filter |
| POST | `/api/torrents/optimize_settings` | apply the suggested engine settings |
| POST | `/api/torrents/peers` | peers of a torrent (legacy body form) |
| POST | `/api/torrents/pin` | pin a torrent |
| POST | `/api/torrents/remove` | remove a torrent (legacy form) |
| POST | `/api/torrents/remove_completed` | remove the completed torrents |
| POST | `/api/torrents/sequential` | sequential download on or off |
| GET | `/api/torrents/session-stats` | engine session statistics |
| POST | `/api/torrents/set_limits` | set the torrent limits (legacy form) |
| GET | `/api/torrents/stats` | torrent statistics |
| GET | `/api/torrents/temp-limits` | temporary speed limit and latency guard |
| POST | `/api/torrents/temp-limits` | set the temporary speed limit |
| POST | `/api/torrents/unpin` | unpin a torrent |
| DELETE | `/api/torrents/{hash}` | remove a torrent |
| GET | `/api/torrents/{hash}` | torrent details |
| GET | `/api/torrents/{hash}/export.torrent` | download the .torrent file |
| GET | `/api/torrents/{hash}/files` | files of a torrent |
| POST | `/api/torrents/{hash}/files/priority` | set the file priorities |
| POST | `/api/torrents/{hash}/limits` | per-torrent speed limits |
| GET | `/api/torrents/{hash}/magnet` | export the magnet link |
| POST | `/api/torrents/{hash}/mark_failed` | mark a torrent as failed |
| POST | `/api/torrents/{hash}/no_rename` | exclude a torrent from renaming |
| POST | `/api/torrents/{hash}/pause` | pause a torrent |
| GET | `/api/torrents/{hash}/peers` | peers of a torrent |
| GET | `/api/torrents/{hash}/pieces` | piece map |
| GET | `/api/torrents/{hash}/pieces/runs` | pieces being downloaded |
| POST | `/api/torrents/{hash}/selective` | selective download profile |
| POST | `/api/torrents/{hash}/reannounce` | reannounce to the trackers |
| POST | `/api/torrents/{hash}/recheck` | recheck the data |
| POST | `/api/torrents/{hash}/remove` | remove a torrent with options |
| POST | `/api/torrents/{hash}/restart` | restart a torrent |
| POST | `/api/torrents/{hash}/resume` | resume a torrent |
| POST | `/api/torrents/{hash}/storage` | move a torrent to another folder |
| GET | `/api/torrents/{hash}/stream` | stream a file from a torrent |
| POST | `/api/torrents/{hash}/super-seeding` | super-seeding on or off |
| GET | `/api/torrents/{hash}/trackers` | trackers of a torrent |
| POST | `/api/torrents/{hash}/trackers` | set the trackers |
| POST | `/api/torrents/{hash}/web-seeds` | set the web seeds |
| GET | `/api/torrents/{hash}/why` | diagnose a torrent: reason and hint when it is not transferring |
| GET | `/api/trash` | recycle bin entries |
| POST | `/api/trash/delete` | delete recycle bin entries |
| POST | `/api/tvdb/search` | search TheTVDB |
| GET | `/api/tvdb/series/{id}` | TheTVDB series details |
| GET | `/api/update` | update status |
| POST | `/api/update/apply` | apply the update |
| POST | `/api/update/check` | check for updates |
| POST | `/api/upload-torrent` | upload a .torrent file |
| GET | `/api/watched-folders` | watched folders |
| POST | `/api/watched-folders` | save the watched folders |
| GET | `/favicon.ico` | favicon |
| GET | `/feed.xml` | magnet feed (RSS) |
| GET | `/feed/calendar.ics` | calendar feed (iCal) |
| GET | `/magnet` | add from a magnet link |

Total routes: 267

The temporary speed limit (`POST /api/torrents/temp-limits`) is the speed
exception: it wins over the rule chosen in the settings (fixed limits, Auto or the
schedule) until it expires or is removed, whether `minutes` is set or not (0 =
until removed). It also accepts `auto: true|false`: with `auto` the bandwidth is
handed to the guided tuning with no cap, the numeric `download_kib` /
`upload_kib` are ignored, `minutes` sets how long it lasts and the two modes are
exclusive. The rule itself is the setting `torrent_speed_mode` (`auto`, `fixed`
or `schedule`), read and written through `/api/config/settings` like any other;
it is stored as `torrent_speed_auto` and `torrent_sched_enabled`.
`GET /api/torrents/temp-limits` also returns `latency_guard`, the state of the
Auto speed's latency guard: `active`, `measuring`, `mode` (`free`, `trial` or
`control`), `latency_ms`, `baseline_ms`, the caps it applies
(`download_cap_kib`, `upload_cap_kib`, 0 = none), `router_counters` (the
router's WAN counters answer), `others_on_line` and the others' traffic
(`others_download_kib`, `others_upload_kib`).

The controls behind it are settings: `torrent_auto_latency_guard` (on/off),
`torrent_auto_latency_source` (`both` = the Google/Cloudflare ping decides and
the router is diagnostic, `ping` = the router is not read, `router` = legacy
yield-to-others), `torrent_auto_latency_target` (empty = Google 8.8.8.8/8.8.4.4
and Cloudflare 1.1.1.1/1.0.0.1 on port 443) and `torrent_line_max_down_kib` /
`torrent_line_max_up_kib` (the line's real maximum in KiB/s; 0 = unknown, the
controller discovers it). The caps are driven by the latency, not by the
router; the controller ramps while the ping is good and cuts when it rises.

## gx-torrent daemon routes

The `gx-torrent` daemon (also usable standalone, without Gextto) serves its own
API on its listen address (default `127.0.0.1:8890`). Two dialects are exposed:

- the **native API** under `/api/v1`;
- the **qBittorrent-compatible API** under `/api/v2`, so a client written for
  qBittorrent can talk to it unchanged. It is mounted **only in standalone
  mode**: when Gextto manages the daemon, Gextto is the single master and the
  whole `/api/v2` is not exposed (the routes below answer 404).

When the daemon is reachable on the network, protect it with the shared token
`gxtorrent_token`: the `/api/v1` clients send the header `X-Gx-Token: <token>`,
or log in on the daemon page. The qBittorrent-compatible `/api/v2` POSTs do not
use the token; they are protected by an `Origin`/`Referer` check instead, so a
qBittorrent client keeps working unchanged. What the token is and how to
generate it is in [`api-keys.md`](api-keys.md) /
[`api-keys.it.md`](api-keys.it.md). The daemon also serves its own
server-rendered UI under `/ui`; those HTML routes are not part of this table.

### Native API (`/api/v1`)

| Method | Path | Description |
|---|---|---|
| POST | `/api/v1/add` | add a torrent (magnet, URL or JSON body); content_layout = Original, Subfolder or NoSubfolder (empty = Original) |
| POST | `/api/v1/add-file` | add a .torrent file upload (same content_layout) |
| GET | `/api/v1/config` | daemon configuration |
| POST | `/api/v1/config` | change the daemon configuration |
| POST | `/api/v1/create-torrent` | create a .torrent from a folder |
| GET | `/api/v1/dht-feed` | recent DHT discovery feed |
| GET | `/api/v1/dht-feed/publish` | sign and publish a BEP 46 feed (?name=…&hash=…), returns the dht:// URL |
| POST | `/api/v1/dht-feed/publish` | sign and publish a BEP 46 feed (JSON name/hashes), returns the dht:// URL |
| GET | `/api/v1/events` | engine events (Server-Sent Events) |
| GET | `/api/v1/health` | health, version, build and fingerprint |
| POST | `/api/v1/ipfilter` | update the IP filter |
| GET | `/api/v1/metrics` | Prometheus metrics |
| POST | `/api/v1/pins/clear` | clear the pinned torrents |
| GET | `/api/v1/portcheck` | check the peer port |
| GET | `/api/v1/stats` | session statistics |
| GET | `/api/v1/stats/history` | statistics history |
| GET | `/api/v1/torrents` | list torrents |
| DELETE | `/api/v1/torrents/{hash}` | remove a torrent |
| GET | `/api/v1/torrents/{hash}` | one torrent's details |
| POST | `/api/v1/torrents/{hash}/{action}` | pause, resume, verify, rename, move, limits, ... |
| GET | `/api/v1/torrents/{hash}/{what}` | files, peers or trackers |

The `{what}` of `GET /api/v1/torrents/{hash}/{what}` is one of `files`,
`peers`, `trackers`, `webseeds`, `pieces`, `torrent-file`, `scrape`,
`dhtscrape`, `collections`. The `{action}` of
`POST /api/v1/torrents/{hash}/{action}` is one of `pause`, `resume`,
`verify`, `reannounce`, `restart`, `park`, `unpark`, `pin`, `top`, `rename`,
`rename-file`, `rename-folder`, `move`, `associate`, `seed-limits`,
`conn-limits`, `super-seeding`, `file-priorities`, `trackers`, `set-trackers`,
`webseeds`.

### qBittorrent-compatible API (`/api/v2`)

| Method | Path | Description |
|---|---|---|
| GET | `/api/v2/app/buildInfo` | version and build info |
| GET | `/api/v2/app/cookies` | cookies used for the .torrent fetches |
| GET | `/api/v2/app/defaultSavePath` | default save path |
| GET | `/api/v2/app/networkInterfaceAddressList` | addresses of the interfaces |
| GET | `/api/v2/app/networkInterfaceList` | network interfaces |
| GET | `/api/v2/app/preferences` | preferences document |
| POST | `/api/v2/app/setCookies` | set the cookies |
| POST | `/api/v2/app/setPreferences` | change the preferences |
| POST | `/api/v2/app/shutdown` | shut the daemon down |
| GET | `/api/v2/app/version` | application version |
| GET | `/api/v2/app/webapiVersion` | Web API version |
| POST | `/api/v2/auth/login` | log in and get the session cookie |
| POST | `/api/v2/auth/logout` | log out |
| GET | `/api/v2/log/main` | main log |
| GET | `/api/v2/log/peers` | peers log |
| POST | `/api/v2/rss/addFeed` | add an RSS feed |
| POST | `/api/v2/rss/addFolder` | create an RSS folder |
| GET | `/api/v2/rss/items` | RSS feeds and folders tree |
| POST | `/api/v2/rss/markAsRead` | mark the articles as read |
| GET | `/api/v2/rss/matchingArticles` | articles matching a rule |
| POST | `/api/v2/rss/moveItem` | move a feed or a folder |
| POST | `/api/v2/rss/refreshItem` | refresh a feed now |
| POST | `/api/v2/rss/removeItem` | remove a feed or a folder |
| POST | `/api/v2/rss/removeRule` | remove a download rule |
| POST | `/api/v2/rss/renameRule` | rename a download rule |
| GET | `/api/v2/rss/rules` | download rules |
| POST | `/api/v2/rss/setFeedRefreshInterval` | set the feed refresh interval |
| POST | `/api/v2/rss/setFeedURL` | change a feed URL |
| POST | `/api/v2/rss/setRule` | create or update a download rule |
| POST | `/api/v2/search/delete` | delete a search |
| POST | `/api/v2/search/enablePlugin` | enable or disable a plugin |
| POST | `/api/v2/search/installPlugin` | install a search plugin |
| GET | `/api/v2/search/plugins` | installed search plugins |
| GET | `/api/v2/search/results` | results of a search |
| POST | `/api/v2/search/start` | start a search |
| GET | `/api/v2/search/status` | search status |
| POST | `/api/v2/search/stop` | stop a search |
| POST | `/api/v2/search/uninstallPlugin` | uninstall a search plugin |
| POST | `/api/v2/search/updatePlugins` | update the search plugins |
| GET | `/api/v2/sync/maindata` | incremental state for the qBittorrent sync |
| GET | `/api/v2/sync/torrentPeers` | peers of one torrent |
| POST | `/api/v2/torrentcreator/addTask` | start a torrent creation task |
| POST | `/api/v2/torrentcreator/deleteTask` | delete a creation task |
| GET | `/api/v2/torrentcreator/status` | creation task status |
| GET | `/api/v2/torrentcreator/torrentFile` | download the created .torrent |
| POST | `/api/v2/torrents/add` | add torrents (contentLayout, or the older root_folder; none = the torrent_content_layout preference) |
| POST | `/api/v2/torrents/addPeers` | add peers |
| POST | `/api/v2/torrents/addTags` | add tags to torrents |
| POST | `/api/v2/torrents/addTrackers` | add trackers |
| POST | `/api/v2/torrents/addWebSeeds` | add web seeds |
| POST | `/api/v2/torrents/bottomPrio` | send torrents to the bottom of the queue |
| GET | `/api/v2/torrents/categories` | categories |
| GET | `/api/v2/torrents/count` | number of torrents, as text |
| POST | `/api/v2/torrents/createCategory` | create a category |
| POST | `/api/v2/torrents/createTags` | create tags |
| POST | `/api/v2/torrents/decreasePrio` | lower the queue priority |
| POST | `/api/v2/torrents/delete` | remove torrents |
| POST | `/api/v2/torrents/deleteTags` | delete tags |
| GET | `/api/v2/torrents/downloadLimit` | per-torrent download limits |
| POST | `/api/v2/torrents/downloadLimit` | set the per-torrent download limits |
| POST | `/api/v2/torrents/editCategory` | rename a category |
| POST | `/api/v2/torrents/editTracker` | edit a tracker URL |
| POST | `/api/v2/torrents/editWebSeed` | replace a web seed |
| GET | `/api/v2/torrents/export` | download a .torrent |
| POST | `/api/v2/torrents/filePrio` | set the file priorities |
| GET | `/api/v2/torrents/files` | files of a torrent |
| POST | `/api/v2/torrents/increasePrio` | raise the queue priority |
| GET | `/api/v2/torrents/info` | torrent list |
| POST | `/api/v2/torrents/pause` | pause torrents |
| GET | `/api/v2/torrents/pieceHashes` | piece hashes |
| GET | `/api/v2/torrents/pieceStates` | piece states |
| GET | `/api/v2/torrents/properties` | torrent properties |
| POST | `/api/v2/torrents/reannounce` | reannounce to the trackers |
| POST | `/api/v2/torrents/recheck` | recheck the data |
| POST | `/api/v2/torrents/removeCategories` | remove categories |
| POST | `/api/v2/torrents/removeTags` | remove tags from torrents |
| POST | `/api/v2/torrents/removeTrackers` | remove trackers |
| POST | `/api/v2/torrents/removeWebSeeds` | remove web seeds |
| POST | `/api/v2/torrents/rename` | rename a torrent |
| POST | `/api/v2/torrents/renameFile` | rename a file |
| POST | `/api/v2/torrents/renameFolder` | rename a folder |
| POST | `/api/v2/torrents/resume` | resume torrents |
| POST | `/api/v2/torrents/setAutoManagement` | automatic management on or off |
| POST | `/api/v2/torrents/setCategory` | set the category |
| POST | `/api/v2/torrents/setDownloadLimit` | set a torrent download limit |
| POST | `/api/v2/torrents/setForceStart` | force start on or off |
| POST | `/api/v2/torrents/setLocation` | move a torrent to another folder |
| POST | `/api/v2/torrents/setShareLimits` | set the share limits |
| POST | `/api/v2/torrents/setSuperSeeding` | super-seeding on or off |
| POST | `/api/v2/torrents/setUploadLimit` | set a torrent upload limit |
| POST | `/api/v2/torrents/start` | start torrents |
| POST | `/api/v2/torrents/stop` | stop torrents |
| GET | `/api/v2/torrents/tags` | tags |
| POST | `/api/v2/torrents/toggleFirstLastPiecePrio` | toggle the first and last piece priority |
| POST | `/api/v2/torrents/toggleSequentialDownload` | sequential download on or off |
| POST | `/api/v2/torrents/topPrio` | send torrents to the top of the queue |
| GET | `/api/v2/torrents/trackers` | trackers of a torrent |
| GET | `/api/v2/torrents/uploadLimit` | per-torrent upload limits |
| POST | `/api/v2/torrents/uploadLimit` | set the per-torrent upload limits |
| GET | `/api/v2/torrents/webseeds` | web seeds of a torrent |
| POST | `/api/v2/transfer/banPeers` | ban peers |
| GET | `/api/v2/transfer/downloadLimit` | global download limit |
| GET | `/api/v2/transfer/info` | global transfer info |
| POST | `/api/v2/transfer/setDownloadLimit` | set the global download limit |
| POST | `/api/v2/transfer/setSpeedLimitsMode` | set the alternative speed limits mode (1 on, 0 off) |
| POST | `/api/v2/transfer/setUploadLimit` | set the global upload limit |
| GET | `/api/v2/transfer/speedLimitsMode` | alternative speed limits mode |
| POST | `/api/v2/transfer/toggleSpeedLimitsMode` | toggle the alternative speed limits |
| GET | `/api/v2/transfer/uploadLimit` | global upload limit |

Total routes: 19 (gx-torrent `/api/v1`) + 109 (gx-torrent `/api/v2`).

### Integrating with other systems

**Prometheus and Grafana.** `GET /api/v1/metrics` serves the daemon's state in
the Prometheus text format (0.0.4): transfer rates and lifetime totals,
torrents in total (`gxtorrent_torrents`), by state
(`gxtorrent_torrents_by_state{state="seeding"}`, the states are a partition)
and the slow ones as a condition (`gxtorrent_slow_torrents`, also downloading),
peers, queue slots, caches,
free disk, the IP filter, every engine session counter (`gxtorrent_session_*`,
uTP and disk counters included) and the process
(`process_resident_memory_bytes`, `process_cpu_seconds_total`,
`process_start_time_seconds`). It is computed only when asked: one scrape costs
about 0.2 ms.

The names follow the Prometheus conventions, so `rate()` and the standard
dashboards read them correctly: a counter ends in `_total` (lifetime totals,
`process_cpu_seconds_total`, the session counters whose key is a `_total`) and
nothing else does; rates carry their unit
(`gxtorrent_download_rate_bytes_per_second`); gauges with a unit end in it
(`_bytes`, `_seconds`). Every family has one `HELP` and one `TYPE` line, label
values are escaped, and the response carries
`text/plain; version=0.0.4; charset=utf-8`.

With a token, send it as a header:

```yaml
scrape_configs:
  - job_name: gx-torrent
    metrics_path: /api/v1/metrics
    static_configs: [{ targets: ["192.168.1.10:8890"] }]
    # with gxtorrent_token set:
    http_headers: { X-Gx-Token: { values: ["<token>"] } }
```

**Events for automations.** `GET /api/v1/events` is a Server-Sent Events
stream of the engine events (a torrent added, completed, failed, a peer
connected, …), one JSON object per `data:` line, with a comment every 30 s to
keep proxies from closing it. The stream belongs to one engine session: when
gx-torrent reopens it for a setting only the engine reads, the response ends
and the client is expected to reconnect (an `EventSource` does it by itself),
subscribing to the new session. It is the token-authenticated twin of the
page's `/ui/events`, for Home Assistant, Node-RED or a script:

```bash
curl -N -H 'X-Gx-Token: <token>' http://127.0.0.1:8890/api/v1/events
```

**qBittorrent 5 calls.** Besides the qBittorrent Web API v2 surface above, the
daemon answers `torrents/count` (the number, as text),
`transfer/setSpeedLimitsMode` (`mode=1` turns the alternative limits on, `0`
off) and the web seed editing calls: `torrents/addWebSeeds` and
`torrents/removeWebSeeds` take `hash` and `urls` separated by `|`;
`torrents/editWebSeed` takes `hash`, `origUrl` and `newUrl`. They answer 404 for
an unknown torrent, 400 for a URL that is not http(s), 409 when `origUrl` is not
a web seed of the torrent.

