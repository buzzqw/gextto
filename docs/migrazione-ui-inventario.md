# Inventario funzionale UI — base di parità per la migrazione

Generato da `ui/app/src/lib.rs` (UI Leptos attuale). È il **contratto di parità**:
una pagina è considerata migrata solo quando offre tutte le sue azioni, i suoi
campi e i suoi dati, con gli stessi endpoint. Nessuna voce può sparire.

## 1. Pagine di navigazione

| # | id | Etichetta | Stato nuova UI |
|---|---|---|---|
| 1 | `dashboard` | Dashboard | shell + partial (fatto) |
| 2 | `downloads` | Scarico | shell + partial (fatto) |
| 3 | `series` | Serie TV | migrata |
| 4 | `movies` | Film | migrata |
| 5 | `gaps` | Mancanti | migrata |
| 6 | `search` | Esplora | migrata |
| 7 | `archive` | Archivio | migrata |
| 8 | `comics` | Fumetti | migrata |
| 9 | `settings` | Configurazione | migrata |
| 10 | `integrations` | Integrazioni | migrata |
| 11 | `maintenance` | Manutenzione | migrata |
| 12 | `health` | Salute | migrata |
| 13 | `logs` | Log | migrata |
| 14 | `blocklist` | Blocklist | migrata |
| 15 | `manual` | Manuale | migrata |
| 16 | `license` | Licenza | migrata |

Totale pagine: **16**.

## 2. Impostazioni (`setting_key`)

Chiavi rese dalla UI attuale: **143** (tutte coperte da `SETTINGS_INDEX`;
`scripts/check-ui-settings-index.sh` ne garantisce la copertura).
La nuova UI dovrà esporle tutte, per sezione, con gli stessi tooltip.

## 3. Endpoint API usati dalla UI

Endpoint referenziati dalla UI: **155**. Sono il contratto JSON/azione
da preservare; la nuova UI li riusa (nessuna riscrittura del backend).

```text
/api/archive/add
/api/archive/batch-download
/api/archive/delete
/api/backup
/api/backup/settings
/api/backup/test-ftp
/api/blocklist
/api/blocklist/{hash}/remove
/api/browse_dir
/api/calendar
/api/comics
/api/comics/cycle
/api/comics/download
/api/comics/downloads
/api/comics/downloads/tag
/api/comics/downloads/{id}/remove
/api/comics/downloads/{}/pause
/api/comics/downloads/{}/resume
/api/comics/explore
/api/comics/history
/api/comics/history/delete
/api/comics/links
/api/comics/weekly
/api/comics/weekly/links
/api/comics/weekly/settings
/api/comics/{id}
/api/comics/{id}/enabled
/api/config
/api/config/check-ports
/api/config/library
/api/config/settings
/api/config/source-filters
/api/database/rescore
/api/db/action
/api/db/info
/api/db/prune
/api/db/prune-keyword
/api/download-tags
/api/episodes/{}/{}/{}/force
/api/episodes/{}/{}/{}/ignore
/api/episodes/{}/{}/{}/redownload
/api/episodes/{}/{}/{}/search
/api/event-hooks
/api/feed/status
/api/flaresolverr/test
/api/gaps
/api/health
/api/i18n/active
/api/i18n/export/{selected}
/api/i18n/import/{selected}
/api/jellyfin/refresh
/api/maintenance/backfill-media-info
/api/maintenance/clean-duplicates
/api/maintenance/clean-trash
/api/maintenance/housekeeping
/api/missing/search
/api/mkdir
/api/movies/history
/api/movies/{id}
/api/movies/{id}/metadata/search
/api/movies/{id}/redownload
/api/movies/{id}/search
/api/movies/{movie_id}
/api/movies/{movie_id}/metadata
/api/network/interfaces
/api/plex/refresh
/api/process-metrics
/api/providers/status
/api/ramdisk
/api/ramdisk/create
/api/ramdisk/select
/api/recent-downloads
/api/rename-all
/api/rename-progress
/api/run_now
/api/scan-all-archives
/api/score/preview
/api/search
/api/search/add
/api/search/archive
/api/search/explain
/api/send-magnet
/api/series/{}/info
/api/series/{}/metadata
/api/series/{}/rename-execute
/api/series/{}/rename-preview
/api/series/{}/scan-archive
/api/series/{}/search-missing
/api/series/{}/toggle-season
/api/service/restart
/api/services
/api/setup
/api/setup/import
/api/simkl/auth/poll
/api/simkl/auth/revoke
/api/simkl/auth/start
/api/simkl/calendar
/api/simkl/settings
/api/simkl/status
/api/simkl/watchlist
/api/simkl/watchlist/import
/api/sources/health
/api/stats
/api/status
/api/tag-dir-rules
/api/test-notification
/api/tmdb/add
/api/tmdb/discover
/api/tmdb/search
/api/torrent-backend
/api/torrent-backend/preflight
/api/torrent-backend/test
/api/torrent-events
/api/torrent-migrations/plan
/api/torrent-tags
/api/torrents
/api/torrents/apply_settings
/api/torrents/ipfilter_status
/api/torrents/ipfilter_update
/api/torrents/optimize_settings
/api/torrents/pin
/api/torrents/remove_completed
/api/torrents/temp-limits
/api/torrents/unpin
/api/torrents/{hash_for_paths}/files
/api/torrents/{hash_for_paths}/trackers
/api/torrents/{hash}
/api/torrents/{hash}/limits
/api/torrents/{hash}/mark_failed
/api/torrents/{hash}/peers
/api/torrents/{hash}/reannounce
/api/torrents/{hash}/recheck
/api/torrents/{hash}/remove
/api/torrents/{hash}/restart
/api/torrents/{hash}/storage
/api/torrents/{hash}/{suffix}
/api/torrents/{}/export.torrent
/api/torrents/{}/files/priority
/api/torrents/{}/no_rename
/api/torrents/{}/super-seeding
/api/torrents/{}/trackers
/api/torrents/{}/web-seeds
/api/trakt/auth/poll
/api/trakt/auth/refresh
/api/trakt/auth/revoke
/api/trakt/auth/start
/api/trakt/calendar
/api/trakt/settings
/api/trakt/status
/api/trakt/watchlist
/api/trakt/watchlist/import
/api/trash
/api/trash/delete
/api/upload-torrent
/api/watched-folders
```

## 4. Regole di parità

1. Ogni pagina, tab, modale, campo e azione dell'inventario deve esistere nella nuova UI.
2. Ogni azione distruttiva conserva conferma e semantica attuali.
3. Ogni endpoint resta compatibile per client esterni.
4. Una pagina passa a `migrata` solo con test Go + Playwright e verifica manuale.
5. `dashboard` e `downloads` sono il primo slice; le altre puntano alla UI classica.
