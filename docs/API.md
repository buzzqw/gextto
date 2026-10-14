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
  `POST`/`DELETE`; treat them as administrative actions and keep backups.
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
change. The old `/ui` route is no longer available. For operational checks use
`GET /api/health` and `GET /api/status` first.

## Gextto routes

| Method | Path |
|---|---|
| GET | `/` |
| POST | `/api/access/generate-key` |
| GET | `/api/acquisitions` |
| GET | `/api/archive` |
| POST | `/api/archive/add` |
| POST | `/api/archive/batch-download` |
| POST | `/api/archive/delete` |
| GET | `/api/backup` |
| POST | `/api/backup` |
| GET | `/api/backup/list` |
| POST | `/api/backup/verify` |
| POST | `/api/backup/send-telegram` |
| GET | `/api/backup/settings` |
| POST | `/api/backup/settings` |
| POST | `/api/backup/test-ftp` |
| GET | `/api/blocklist` |
| POST | `/api/blocklist/{hash}/remove` |
| GET | `/api/browse_dir` |
| GET | `/api/browser-handlers/download` |
| GET | `/api/calendar` |
| GET | `/api/comics` |
| POST | `/api/comics` |
| POST | `/api/comics/check-links` |
| POST | `/api/comics/cycle` |
| POST | `/api/comics/download` |
| GET | `/api/comics/downloads` |
| POST | `/api/comics/downloads/tag` |
| POST | `/api/comics/downloads/{id}/pause` |
| POST | `/api/comics/downloads/{id}/remove` |
| POST | `/api/comics/downloads/{id}/resume` |
| POST | `/api/comics/explore` |
| GET | `/api/comics/history` |
| POST | `/api/comics/history/delete` |
| POST | `/api/comics/history/clear` |
| POST | `/api/comics/weekly/history/clear` |
| POST | `/api/comics/links` |
| GET | `/api/comics/weekly` |
| POST | `/api/comics/weekly/links` |
| POST | `/api/comics/weekly/settings` |
| DELETE | `/api/comics/{id}` |
| POST | `/api/comics/{id}/enabled` |
| GET | `/api/config` |
| POST | `/api/config` |
| POST | `/api/config/add-all-from-archive` |
| POST | `/api/config/add_all_from_archive` |
| GET | `/api/config/check-ports` |
| GET | `/api/config/library` |
| POST | `/api/config/library` |
| POST | `/api/config/movies` |
| POST | `/api/config/series` |
| POST | `/api/config/settings` |
| DELETE | `/api/config/settings/{key}` |
| GET | `/api/config/source-filters` |
| POST | `/api/config/source-filters` |
| GET | `/api/cycle-history` |
| GET | `/api/cycles` |
| POST | `/api/database/cleanup` |
| POST | `/api/database/rescore` |
| POST | `/api/db/action` |
| GET | `/api/db/info` |
| POST | `/api/db/prune` |
| POST | `/api/db/prune-by-ids` |
| POST | `/api/db/prune-keyword` |
| POST | `/api/db/prune/preview` |
| GET | `/api/debug/torrent_match` |
| GET | `/api/download-tags` |
| POST | `/api/download-tags` |
| DELETE | `/api/episodes/{series}/{season}/{episode}` |
| POST | `/api/episodes/{series}/{season}/{episode}/force` |
| POST | `/api/episodes/{series}/{season}/{episode}/ignore` |
| POST | `/api/episodes/{series}/{season}/{episode}/redownload` |
| POST | `/api/episodes/{series}/{season}/{episode}/search` |
| GET | `/api/episodes/{series}/{season}/{episode}/sources` |
| GET | `/api/event-hooks` |
| POST | `/api/event-hooks` |
| GET | `/api/feed.xml` |
| GET | `/api/feed/status` |
| POST | `/api/flaresolverr/test` |
| GET | `/api/gaps` |
| GET | `/api/health` |
| GET | `/api/i18n` |
| POST | `/api/i18n` |
| GET | `/api/i18n/active` |
| POST | `/api/i18n/active` |
| GET | `/api/i18n/export/{lang}` |
| POST | `/api/i18n/import/{lang}` |
| POST | `/api/i18n/language` |
| GET | `/api/i18n/languages` |
| DELETE | `/api/i18n/{lang}` |
| POST | `/api/indexer/test` |
| POST | `/api/jellyfin/refresh` |
| POST | `/api/jellyfin/test` |
| GET | `/api/jobs` |
| GET | `/api/jobs/{id}` |
| POST | `/api/jobs/{id}/cancel` |
| GET | `/api/last_cycle` |
| GET | `/api/last_cycles` |
| GET | `/api/license` |
| POST | `/api/log-level` |
| GET | `/api/log_level` |
| POST | `/api/log_level` |
| GET | `/api/logs` |
| GET | `/api/logs/stream` |
| POST | `/api/maintenance/backfill-media-info` |
| POST | `/api/maintenance/clean-duplicates` |
| POST | `/api/maintenance/clean-trash` |
| POST | `/api/maintenance/housekeeping` |
| POST | `/api/maintenance/restore-source` |
| POST | `/api/maintenance/rename-folder/scan` |
| POST | `/api/maintenance/rename-folder/apply` |
| GET | `/api/manual-search` |
| GET | `/api/media-info` |
| POST | `/api/media-info/probe` |
| POST | `/api/missing/search` |
| POST | `/api/mkdir` |
| GET | `/api/movies` |
| GET | `/api/movies/history` |
| GET | `/api/movies/seen` |
| GET | `/api/movies/seen/grouped` |
| DELETE | `/api/movies/{id}` |
| GET | `/api/movies/{id}` |
| POST | `/api/movies/{id}` |
| POST | `/api/movies/{id}/metadata` |
| POST | `/api/movies/{id}/metadata/search` |
| POST | `/api/movies/{id}/redownload` |
| POST | `/api/movies/{id}/search` |
| GET | `/api/network/interfaces` |
| GET | `/api/notifications/stream` |
| POST | `/api/plex/refresh` |
| POST | `/api/plex/test` |
| GET | `/api/process-metrics` |
| GET | `/api/providers/status` |
| POST | `/api/providers/status` |
| GET | `/api/ramdisk` |
| POST | `/api/ramdisk/create` |
| POST | `/api/ramdisk/select` |
| GET | `/api/ramdisk_check` |
| GET | `/api/recent-downloads` |
| POST | `/api/rename-all` |
| GET | `/api/rename-progress` |
| POST | `/api/run-now` |
| GET | `/api/run_now` |
| POST | `/api/run_now` |
| POST | `/api/scan-all-archives` |
| POST | `/api/score/preview` |
| POST | `/api/search` |
| POST | `/api/search/dashboard` |
| POST | `/api/search/add` |
| POST | `/api/search/archive` |
| POST | `/api/search/explain` |
| POST | `/api/send-magnet` |
| GET | `/api/series` |
| GET | `/api/series/all-missing` |
| GET | `/api/series/calendar` |
| GET | `/api/series/seen` |
| GET | `/api/series/seen/grouped` |
| GET | `/api/series/{name}` |
| POST | `/api/series/{name}` |
| DELETE | `/api/series/{name}` |
| GET | `/api/series/{name}/episodes` |
| GET | `/api/series/{name}/info` |
| POST | `/api/series/{name}/metadata` |
| POST | `/api/series/{name}/path` |
| POST | `/api/series/{name}/rename-execute` |
| POST | `/api/series/{name}/rename-preview` |
| POST | `/api/series/{name}/scan-archive` |
| POST | `/api/series/{name}/search-missing` |
| POST | `/api/series/{name}/toggle-season` |
| POST | `/api/service/restart` |
| GET | `/api/services` |
| POST | `/api/set-speed-limits` |
| GET | `/api/setup` |
| POST | `/api/setup/complete` |
| POST | `/api/simkl/auth/poll` |
| POST | `/api/simkl/auth/revoke` |
| POST | `/api/simkl/auth/start` |
| GET | `/api/simkl/calendar` |
| POST | `/api/simkl/mark-watched` |
| POST | `/api/simkl/scrobble` |
| GET | `/api/simkl/settings` |
| POST | `/api/simkl/settings` |
| GET | `/api/simkl/status` |
| GET | `/api/simkl/watchlist` |
| POST | `/api/simkl/watchlist/import` |
| GET | `/api/sources/health` |
| GET | `/api/stats` |
| GET | `/api/status` |
| GET | `/api/system/lt_mem_suggest` |
| GET | `/api/system/stats` |
| GET | `/api/tag-dir-rules` |
| POST | `/api/tag-dir-rules` |
| POST | `/api/test-notification` |
| POST | `/api/tmdb/add` |
| POST | `/api/tmdb/discover` |
| POST | `/api/tmdb/search` |
| GET | `/api/torrent-backend` |
| GET | `/api/torrent-backend/qbittorrent/update` |
| POST | `/api/torrent-backend` |
| POST | `/api/torrent-backend/preflight` |
| POST | `/api/torrent-backend/qbittorrent/update` |
| POST | `/api/torrent-backend/test` |
| POST | `/api/torrent-backend/portcheck` |
| GET | `/api/torrent-migrations` |
| POST | `/api/torrent-migrations/cancel` |
| POST | `/api/torrent-migrations/plan` |
| GET | `/api/torrent-events` |
| GET | `/api/torrent-no-rename` |
| GET | `/api/torrent-tags` |
| POST | `/api/torrent-tags` |
| GET | `/api/torrents` |
| POST | `/api/torrents` |
| POST | `/api/torrents/add` |
| GET | `/api/torrents/history` |
| GET | `/api/torrents/ipfilter_status` |
| POST | `/api/torrents/ipfilter_update` |
| POST | `/api/torrents/optimize_settings` |
| POST | `/api/torrents/peers` |
| POST | `/api/torrents/pin` |
| POST | `/api/torrents/remove` |
| POST | `/api/torrents/remove_completed` |
| POST | `/api/torrents/sequential` |
| GET | `/api/torrents/session-stats` |
| POST | `/api/torrents/set_limits` |
| GET | `/api/torrents/stats` |
| GET | `/api/torrents/temp-limits` |
| POST | `/api/torrents/temp-limits` |
| POST | `/api/torrents/unpin` |
| DELETE | `/api/torrents/{hash}` |
| GET | `/api/torrents/{hash}` |
| GET | `/api/torrents/{hash}/export.torrent` |
| GET | `/api/torrents/{hash}/files` |
| POST | `/api/torrents/{hash}/files/priority` |
| POST | `/api/torrents/{hash}/limits` |
| GET | `/api/torrents/{hash}/magnet` |
| POST | `/api/torrents/{hash}/mark_failed` |
| POST | `/api/torrents/{hash}/no_rename` |
| POST | `/api/torrents/{hash}/pause` |
| GET | `/api/torrents/{hash}/peers` |
| GET | `/api/torrents/{hash}/pieces` |
| GET | `/api/torrents/{hash}/pieces/runs` |
| POST | `/api/torrents/{hash}/selective` |
| POST | `/api/torrents/{hash}/reannounce` |
| POST | `/api/torrents/{hash}/recheck` |
| POST | `/api/torrents/{hash}/remove` |
| POST | `/api/torrents/{hash}/restart` |
| POST | `/api/torrents/{hash}/resume` |
| POST | `/api/torrents/{hash}/storage` |
| GET | `/api/torrents/{hash}/stream` |
| POST | `/api/torrents/{hash}/super-seeding` |
| GET | `/api/torrents/{hash}/trackers` |
| POST | `/api/torrents/{hash}/trackers` |
| POST | `/api/torrents/{hash}/web-seeds` |
| GET | `/api/torrents/{hash}/why` |
| GET | `/api/trash` |
| POST | `/api/trash/delete` |
| POST | `/api/tvdb/search` |
| GET | `/api/tvdb/series/{id}` |
| GET | `/api/update` |
| POST | `/api/update/apply` |
| POST | `/api/update/check` |
| POST | `/api/upload-torrent` |
| GET | `/api/watched-folders` |
| POST | `/api/watched-folders` |
| GET | `/favicon.ico` |
| GET | `/feed.xml` |
| GET | `/feed/calendar.ics` |
| GET | `/magnet` |

Total routes: 267

## gx-torrent daemon routes

The `gx-torrent` daemon (also usable standalone, without Gextto) serves its own
API on its listen address (default `127.0.0.1:8890`). Two dialects are exposed:

- the **native API** under `/api/v1`;
- the **qBittorrent-compatible API** under `/api/v2`, so a client written for
  qBittorrent can talk to it unchanged.

When the daemon is reachable on the network, protect it with the shared token
`gxtorrent_token`: the `/api/v1` clients send the header `X-Gx-Token: <token>`,
or log in on the daemon page. The qBittorrent-compatible `/api/v2` POSTs do not
use the token; they are protected by an `Origin`/`Referer` check instead, so a
qBittorrent client keeps working unchanged. What the token is and how to
generate it is in [`api-keys.md`](api-keys.md) /
[`api-keys.it.md`](api-keys.it.md). The daemon also serves its own
server-rendered UI under `/ui`; those HTML routes are not part of this table.

### Native API (`/api/v1`)

| Method | Path |
|---|---|
| POST | `/api/v1/add` |
| POST | `/api/v1/add-file` |
| GET | `/api/v1/config` |
| POST | `/api/v1/config` |
| POST | `/api/v1/create-torrent` |
| GET | `/api/v1/dht-feed` |
| GET | `/api/v1/health` |
| POST | `/api/v1/ipfilter` |
| POST | `/api/v1/pins/clear` |
| GET | `/api/v1/portcheck` |
| GET | `/api/v1/stats` |
| GET | `/api/v1/stats/history` |
| GET | `/api/v1/torrents` |
| DELETE | `/api/v1/torrents/{hash}` |
| GET | `/api/v1/torrents/{hash}` |
| POST | `/api/v1/torrents/{hash}/{action}` |
| GET | `/api/v1/torrents/{hash}/{what}` |

The `{what}` of `GET /api/v1/torrents/{hash}/{what}` is one of `files`,
`peers`, `trackers`. The `{action}` of
`POST /api/v1/torrents/{hash}/{action}` is one of `pause`, `resume`,
`verify`, `reannounce`, `restart`, `park`, `unpark`, `pin`, `top`, `rename`,
`rename-file`, `rename-folder`, `move`, `associate`, `seed-limits`,
`conn-limits`, `super-seeding`, `file-priorities`, `trackers`, `set-trackers`,
`webseeds`.

### qBittorrent-compatible API (`/api/v2`)

| Method | Path |
|---|---|
| GET | `/api/v2/app/buildInfo` |
| GET | `/api/v2/app/cookies` |
| GET | `/api/v2/app/defaultSavePath` |
| GET | `/api/v2/app/networkInterfaceAddressList` |
| GET | `/api/v2/app/networkInterfaceList` |
| GET | `/api/v2/app/preferences` |
| POST | `/api/v2/app/setCookies` |
| POST | `/api/v2/app/setPreferences` |
| POST | `/api/v2/app/shutdown` |
| GET | `/api/v2/app/version` |
| GET | `/api/v2/app/webapiVersion` |
| POST | `/api/v2/auth/login` |
| POST | `/api/v2/auth/logout` |
| GET | `/api/v2/log/main` |
| GET | `/api/v2/log/peers` |
| POST | `/api/v2/rss/addFeed` |
| POST | `/api/v2/rss/addFolder` |
| GET | `/api/v2/rss/items` |
| POST | `/api/v2/rss/markAsRead` |
| GET | `/api/v2/rss/matchingArticles` |
| POST | `/api/v2/rss/moveItem` |
| POST | `/api/v2/rss/refreshItem` |
| POST | `/api/v2/rss/removeItem` |
| POST | `/api/v2/rss/removeRule` |
| POST | `/api/v2/rss/renameRule` |
| GET | `/api/v2/rss/rules` |
| POST | `/api/v2/rss/setFeedRefreshInterval` |
| POST | `/api/v2/rss/setFeedURL` |
| POST | `/api/v2/rss/setRule` |
| POST | `/api/v2/search/delete` |
| POST | `/api/v2/search/enablePlugin` |
| POST | `/api/v2/search/installPlugin` |
| GET | `/api/v2/search/plugins` |
| GET | `/api/v2/search/results` |
| POST | `/api/v2/search/start` |
| GET | `/api/v2/search/status` |
| POST | `/api/v2/search/stop` |
| POST | `/api/v2/search/uninstallPlugin` |
| POST | `/api/v2/search/updatePlugins` |
| GET | `/api/v2/sync/maindata` |
| GET | `/api/v2/sync/torrentPeers` |
| POST | `/api/v2/torrentcreator/addTask` |
| POST | `/api/v2/torrentcreator/deleteTask` |
| GET | `/api/v2/torrentcreator/status` |
| GET | `/api/v2/torrentcreator/torrentFile` |
| POST | `/api/v2/torrents/add` |
| POST | `/api/v2/torrents/addPeers` |
| POST | `/api/v2/torrents/addTags` |
| POST | `/api/v2/torrents/addTrackers` |
| POST | `/api/v2/torrents/bottomPrio` |
| GET | `/api/v2/torrents/categories` |
| POST | `/api/v2/torrents/createCategory` |
| POST | `/api/v2/torrents/createTags` |
| POST | `/api/v2/torrents/decreasePrio` |
| POST | `/api/v2/torrents/delete` |
| POST | `/api/v2/torrents/deleteTags` |
| GET | `/api/v2/torrents/downloadLimit` |
| POST | `/api/v2/torrents/downloadLimit` |
| POST | `/api/v2/torrents/editCategory` |
| POST | `/api/v2/torrents/editTracker` |
| GET | `/api/v2/torrents/export` |
| POST | `/api/v2/torrents/filePrio` |
| GET | `/api/v2/torrents/files` |
| POST | `/api/v2/torrents/increasePrio` |
| GET | `/api/v2/torrents/info` |
| POST | `/api/v2/torrents/pause` |
| GET | `/api/v2/torrents/pieceHashes` |
| GET | `/api/v2/torrents/pieceStates` |
| GET | `/api/v2/torrents/properties` |
| POST | `/api/v2/torrents/reannounce` |
| POST | `/api/v2/torrents/recheck` |
| POST | `/api/v2/torrents/removeCategories` |
| POST | `/api/v2/torrents/removeTags` |
| POST | `/api/v2/torrents/removeTrackers` |
| POST | `/api/v2/torrents/rename` |
| POST | `/api/v2/torrents/renameFile` |
| POST | `/api/v2/torrents/renameFolder` |
| POST | `/api/v2/torrents/resume` |
| POST | `/api/v2/torrents/setAutoManagement` |
| POST | `/api/v2/torrents/setCategory` |
| POST | `/api/v2/torrents/setDownloadLimit` |
| POST | `/api/v2/torrents/setForceStart` |
| POST | `/api/v2/torrents/setLocation` |
| POST | `/api/v2/torrents/setShareLimits` |
| POST | `/api/v2/torrents/setSuperSeeding` |
| POST | `/api/v2/torrents/setUploadLimit` |
| POST | `/api/v2/torrents/start` |
| POST | `/api/v2/torrents/stop` |
| GET | `/api/v2/torrents/tags` |
| POST | `/api/v2/torrents/toggleFirstLastPiecePrio` |
| POST | `/api/v2/torrents/toggleSequentialDownload` |
| POST | `/api/v2/torrents/topPrio` |
| GET | `/api/v2/torrents/trackers` |
| GET | `/api/v2/torrents/uploadLimit` |
| POST | `/api/v2/torrents/uploadLimit` |
| GET | `/api/v2/torrents/webseeds` |
| POST | `/api/v2/transfer/banPeers` |
| GET | `/api/v2/transfer/downloadLimit` |
| GET | `/api/v2/transfer/info` |
| POST | `/api/v2/transfer/setDownloadLimit` |
| POST | `/api/v2/transfer/setUploadLimit` |
| GET | `/api/v2/transfer/speedLimitsMode` |
| POST | `/api/v2/transfer/toggleSpeedLimitsMode` |
| GET | `/api/v2/transfer/uploadLimit` |

Total routes: 17 (gx-torrent `/api/v1`) + 104 (gx-torrent `/api/v2`).
