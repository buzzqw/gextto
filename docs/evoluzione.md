# Improvements and evolution of Gextto and gx-torrent

Single working document for **2026-10-10**: it collects the backlog, implemented
improvements, improvement plans, measurements, technical reviews and the
strategic direction of Gextto and the gx-torrent engine.

It replaces the following files, moved to `docs/archive/` as local copies not
tracked in git (see Appendix A for the map):

- `proposte-migliorie.md`, `da-fare.md`, `lavoro-sessione.md`,
  `scelte-di-progetto.md`, `accessibility-analysis.md`,
  `holepunch-test-harness.md`
- `gx-torrent-migliorie.md`, `gx-torrent-ottimizzazioni-prestazionali-v2.md`,
  `gx-torrent-misure-seeding.md`
- `revisione-1.md` (already local, already in `.gitignore`)
- `archive/revisione-2.md`, `archive/UI_V2.md`, `archive/gextto-terra.md`,
  `archive/pro-terra.md` (already archived)

The following remain separate documents, because they describe the current state
rather than a plan: `docs/gx-torrent.md` (technical reference for the engine),
manuals, API, architecture, security, migration, advanced guides, TUI.

**§14** (gx-nox standalone client) has been extracted into
[`docs/gx-torrent-evoluto.md`](gx-torrent-evoluto.md), to keep the strategic plan
and the engine's technical document separate.

## Index

1. [Active backlog](#1-active-backlog)
2. [Gextto improvements implemented](#2-gextto-improvements-implemented)
3. [Strategic direction](#3-strategic-direction)
4. [gx-torrent: gaps and improvement plan](#4-gx-torrent-gaps-and-improvement-plan)
5. [gx-torrent: performance optimisations](#5-gx-torrent-performance-optimisations)
6. [gx-torrent: choking and seeding measurements](#6-gx-torrent-choking-and-seeding-measurements)
7. [Holepunching: test harness](#7-holepunching-test-harness)
8. [Technical review 1: client, i18n, jobs, concurrency](#8-technical-review-1-client-i18n-jobs-concurrency)
9. [Technical review 2: bugs and debt at commit 97142c1](#9-technical-review-2-bugs-and-debt-at-commit-97142c1)
10. [Terra and Pro Terra reports](#10-terra-and-pro-terra-reports)
11. [SSR + HTMX UI: migration report](#11-ssr--htmx-ui-migration-report)
12. [Handover between work sessions](#12-handover-between-work-sessions)
13. [Accessibility](#13-accessibility)
14. [gx-torrent standalone (gx-nox): pointer](#14-gx-torrent-standalone-gx-nox-pointer)
15. [High-value extensions from the comparison with BiglyBT](#15-high-value-extensions-from-the-comparison-with-biglybt)
16. [Appendix A: consolidated files](#appendix-a-consolidated-files)

---

## 1. Active backlog

Ex `docs/da-fare.md`. Restored on 08-10-2026: it had ended up in
`docs/archive/` by mistake even though it is an **active** backlog.

**Recurring checks (at every cycle close).** `make test` (`check-ui` +
`installer-test` + `test-dht` + `CGO_ENABLED=0 go test ./...`), `make test-engine`
and `make vet`; invariants `scripts/check-ui-settings-index.sh` /
`scripts/installer-selftest.sh`. **The Gextto TUI must always work**:
`go test ./internal/tui/` + `go test . -run TestTUILibraryClientAgainstTheRouter`
(+ `gexttod tui` smoke test against the daemon) — see §12.1 of
[`gx-torrent-evoluto.md`](gx-torrent-evoluto.md).

### 1.1 Observability: metrics over time (done)

Correlation IDs and the runbook are done (see §1.3). Metrics over time are
stored in the database and shown in *Health*, without Prometheus:

- ~~duration of searches and sources that do not respond, cycle by cycle~~ —
  done (2026-10-10): `CycleStats` stores duration and outcome per source per cycle
  (`cycle_history`), with the **Searches** panel in *Health* (`uiweb.go`,
  `RecentCycleStats`, `ScrapeAll`);
- ~~warning above a threshold~~ — done (2026-10-10): `cycle_monitor.go` warns
  (WARN line + notification) if a source has failed in 3 of the last searches,
  if the last search exceeds 3 hours or if there has been no error-free cycle for 3
  intervals (minimum 2 hours), and signals the return to normal;
- ~~titles in the queue with their waiting time~~ — done (2026-10-10): **Title
  queue** panel in *Health* (`Database.PendingQueue`, `pending_queue.go`), with
  title, wait since `first_seen_at` and time to `due_at` (series and movies);
- ~~failed imports per day~~ — done (2026-10-10): **Failed imports** panel
  in *Health* (`Database.FailedImportsDaily`, `acquisition.go`), which counts per
  day the import ERRORs from `acquisition_events`;
- the health of the manager's indexers is already in Sources (`indexer_health.go`).

**UI freshness.** The two panels refresh themselves every 60 s (HTMX
`hx-trigger`, endpoint `GET /partial/health/queue`); the tiles already have
`TestV2HealthTilesPollIndependently`. The render is always fresh:
`TestV2HealthRefreshesWhenDataChanges` fixes that a datum added after the first
render appears on the next render (no stale cache).

### 1.2 Benchmark of the gx-core engine (done)

- **`scripts/bench-engines.sh`** — reproducible RAM/throughput probe of gx-core:
  swarm throughput on loopback (existing harness, with peak RSS) and
  disk write throughput of a directory (`--dir`, point it at a
  mount NFS to compare). Usage: `scripts/bench-engines.sh [--size MiB] [--dir DIR]
  [--leechers N]`. Outside CI: it only serves to decide with numbers.
- **`make measure-dht`** — measures on the **public DHT** the filling of the routing
  table and the success of queries (`GX_MEASURE_DHT=5m`,
  `internal/dht/measure_test.go`): it is the basis of the numbers for the anti-Sybil
  hardening and multi-path (§4.10). Outside CI. There is also
  `make measure-seeding` for choking (§6).

### 1.3 Closed / decisions

- **i18n in six languages** — done (2026-10-10). Full it/en/de/fr/es/pl package:
  web UI (chrome, labels, tooltips, hints, server-side messages and strings
  generated by the client JS via `window.__v2i18n` + `t()` in `v2-core.js`); API/HTMX
  errors shown to the user localised with `uiText`; notifications
  (Telegram/email/webhook) with `messages.Pick` on the it→{de,fr,es,pl}
  catalogue (`internal/messages/catalog.go`); TUI in 6 languages (`internal/tui/i18n.go`);
  gx-torrent web page translated with the same approach (catalogue +
  `translateHTML`, language passed by Gextto with `-lang` and `?lang=xx`).
  Guard: `TestUIIsTranslatable` fails if a UI string from a template or a Go
  view-model (or a `uiText` literal) is not a key of the catalogues, with an
  explicit allowlist; `TestClientI18nKeysTranslated` covers the client keys.
  The daemon logs remain **in English by choice**; the technical strings of
  API errors not shown to the user may remain in the original language.
- **Correlation IDs and download history** — done (2026-10-10). Log lines about
  a torrent carry `acq: <id>` (a short ID derived from the hash, which stays
  hidden) and end up in `acquisition_events`: *History* tab in the torrent
  detail, 📜 per episode and a panel in the series page, API
  `/api/acquisitions` (`acquisition.go`).
- **Runbook "an episode did not arrive"** — done (2026-10-10), in
  *Troubleshooting* of `MANUAL.it.md`/`MANUAL.en.md`, built on the history.
- **First-start wizard (onboarding)** — done and judged complete
  (2026-10-10). Guided procedure in `uiweb_v2_setup.go` (commit `c8162d8`), then
  TVDB as an alternative to TMDB and choice of language at the first step.
- **"Default" button on every setting** — done. Every non-secret
  field with a registered default (even empty) shows ↺ next to "Save": it fills
  the field with the default and the user confirms by saving; the default value is shown
  next to it if compact (`Resettable`/`DefaultInline` in `uiweb_v2.go`, `HasDefault`
  in `uiweb_pages.go`). Excluded: fields disabled for the active engine, handled
  by auto-tuning, secrets or without a default.
- **Unified upgrade logs** — done (commit `988d60a`). A replacement
  produces a single line: `♻️ FBI S03E01 updated (1.4 GB): … · previous version
  «…» moved to the trash`. A normal addition stays `📁 … added to the library …`.
- **"Background operations" panel** — resolved (commit `65cb9bf`). It remains and
  works (polling, cancellation), but it is **hidden when there is no job**.
- **Write-back cache in the gx-core engine** — closed: the measurement shows
  `write_cache=0` (storage and download in step), so there would be no
  gain. To be reopened only with storage genuinely slower than the network.
- **Specialist fields in the add form** — closed: no concrete request,
  it would be preventive fluff.
- **UI benchmark as a performance test in CI** — closed: SSR rendering
  costs ~1–3 ms/page, it is not a problem.
- **Cache cap on slow storage (NFS)** — superseded: the cache self-regulates
  (`cmd/gx-torrent/cache.go`, tuner `cacheSizes`).
- **Dashboard: "Latest found in the sources" diagnostics** — already done
  (route `GET /dashboard/feed`, `uiweb_v2.go`).
- **Live verification of the RAM disk flow with gx-torrent** — done (2026-10-07).
- **README aligned with BEP coverage — done (2026-10-11).** The
  *Torrent engine* sections of `README.md`/`README.it.md` now summarise **all** the
  BitTorrent coverage with the BEP numbers (wire 3, Fast Extension 6,
  `ut_metadata` 9, Extension Protocol 10, PEX v4/v6 11, multitracker 12, LSD
  v4/v6 14, Web Seed 19, `upload_only` 21, padding 47, `lt_donthave` 54; tracker
  IPv6 7, UDP 15, URLData 41, scrape 48; DHT 5/32/33/42/43/44; v2 52, magnet
  `so=` 53) and refer to `docs/bep.md` as the single source; the deprecated/niche
  BEPs and WebTorrent/I2P are declared out of scope. General rule added
  to `AGENTS.md`: the README is documentation and must be updated with every change
  that impacts the user.

### 1.4 UI/i18n micro-optimisations (done, measured on 2026-10-10)

Follow-up to commits `06c9128`/`6cf568e`/`7f43b44`. None of them is a bottleneck:
they are to be taken only as refinements, with the numbers below.

| # | Item | Measurement (local disk, `CGO_ENABLED=0 go test`) | Expected gain |
|---|---|---|---|
| 1 | Writability probe in the chrome | `stat`+`write_probe` (create+fsync+delete): **~15.6 µs/op** (200 iterations); whole `uiShellChromeFrom` **~164 µs/op** → the probe is ~10% of the chrome under test | ~15 µs/render, no fsync per render; on an NFS datadir it would be worth milliseconds |
| 2 | Dictionary cache identity test | reuse confirmed: two `Dictionary("de")` = **same pointer** | test quality only, no runtime effect |
| 3 | i18n client memo for `AppState` | **+1 entry per state**, ~**2.4 KB/entry** (20 states → 20 entries, 47,240 bytes) | nil in production (1 entry); cleanup only for tests |

1. **Writability probe cached in the chrome. Done.** `uiShellChromeFrom` now
   uses `dataDirWritable` (`uiweb_shell.go`), which reuses the outcome for
   `dataDirWritableTTL` (30 s): no `create+fsync+delete` on every page,
   partial and SSE tick. Test `TestDataDirWritableCache` (reuse within the TTL,
   recomputation after).
2. **Dictionary cache identity test. Done.**
   `TestDictionaryCacheAndInvalidate` compares identity with
   `reflect.ValueOf(first).Pointer() == reflect.ValueOf(second).Pointer()`: a
   rebuild on every call now fails.
3. **Cleanup of `v2ClientI18nMemo`. Done.** `resetV2ClientI18nMemo()`
   (`uiweb_v2_client_i18n.go`) empties the cache; `newTestAppState` calls it, so
   tests do not accumulate entries (in production only one remains). Test
   `TestV2ClientI18nMemoIsReset`.

### 1.5 gx-torrent on Windows and magnet v2 (closed, review of 2026-10-10)

The review of the Windows/v2 commits fixed R-001…R-013 (junction, `isDirLink`,
expiry of hash requests, session 0, CI job `windows-platform`; commits
`95495e5`, `ebae504`, `1fae2ea`, `0363a02`). The improvements that emerged (M-01…M-08)
were **all implemented**:

| Item | Status |
| --- | --- |
| M-01 test of the link flow on Windows (`pointLink`→`readLink`→move→removal, `protectLegacyDir` does not move the junction) | done (`48edb97`, `TestWindowsLinkFlow`) |
| M-02 widen `windows-platform` to other tests (Setup, CrossDevice) with portable tests | done (`48edb97`) |
| M-03 v2 layer in parallel: chunks of ≤512 hashes per different peer, with retry | done (`92f0a84`) |
| M-04 wizard: error shown on the page if the bandwidth limits do not apply | done (`fbc9eaf`) |
| M-05 `replaceDirLink` Windows: junction built complete and then swapped | done (`d4e1485`) |
| M-06 native Windows service (`windows/svc`) and zip package with `sc create` | done (`b145b73`, `9120fc5`; service selftest in CI `8403c71`) |
| M-07 Windows gateway: use the route chosen by Windows (`GetBestRoute`) | done (`d4e1485`) |
| M-08 v2 interop `base=0`: serve the block layer as a seed | done (`ab0a603`: `base=0` response from verified pieces, per-file cached, only when the file is complete; interop with a raw client `e9d8a78`) |

**M-06 (Windows service)** — done: `main` now recognises `svc.IsWindowsService()`
(`service_windows.go` / `service_other.go`) and starts the same daemon
(`startDaemon`, extracted and shared with the interactive path) inside `svc.Run`,
stopping it on SCM stop. `scripts/package-gx-torrent-windows.sh` produces
`gx-torrent-windows-<arch>.zip` with `gx-torrent.exe`, `install-service.ps1`
(`New-Service`), `uninstall-service.ps1` and `README.txt` (alternative `sc create`).
The zip is now generated **inside** `build-release.sh` (pure-Go variant, where Go is
available) and published by `continuous.yml`/`release.yml` as an artifact and in the
release (`dist/gx-torrent-windows-<arch>.zip`). `TestDaemonStartServeStop` covers
the shared lifecycle (`startDaemon`/`shutdown`) on Linux and, with the
`windows-platform` filter, on Windows. The real installation is covered by the CI job
`windows-service` (`8403c71`, non-blocking because the SCM is asynchronous):
`scripts/gx-torrent-service-selftest.ps1` registers the service with `New-Service`
and the same `binPath` as the package, starts it, waits for `/api/v1/health` and
verifies its clean stop.

### 1.6 Engine presented as our own code (done)

**Rule.** gx-core is our engine and we present it as such: the
documentation does not advertise the origin of the code. The only attribution is
the one **required by the permissive licences** of the derived components, in the files
`internal/gxcore/LICENSE` (MIT) and `internal/dht/LICENSE` (BSD-3) and in
`THIRD-PARTY-NOTICES`. Generic references to the *functionality*
of other clients also remain (for example "as libtorrent does", "as anacrolix does") and the
normal third-party dependencies, with their import and their licence
(`github.com/anacrolix/utp` for uTP, `libtorrent` for the alternative engine).

**Done (2026-10-11).** Removed "relative to the base" changelogs and origin
markers from the code and the documents (including archive and scripts); the dependencies
traceable to the authors of the original code were replaced with our own code:
the DHT is now `internal/dht` (its own address decoding and throttle), the
engine logger is `internal/gxcore/logging` and the backoff is
`internal/gxcore/internal/backoff`. The peer ID prefix is `-GX`. The MIT/BSD licences
of the derived components have been restored in their respective `LICENSE` files and
collected in `THIRD-PARTY-NOTICES`; the network identity is `"gx-core " + Version`.

### 1.7 In-depth performance check (CPU and RAM)

**Point.** Periodically carry out an in-depth performance check
of the application and the engine, optimising CPU and RAM. Constraint: every
replacement or change must be **at least equivalent** to the previous one, if not
better, and must be measured before and after.

**How.** Benchmarks (`go test -bench … -benchmem`), `pprof` CPU/heap profiles,
footprint of the daemon and of `gx-torrent` (RSS, goroutines, allocations),
`make measure-seeding` for seeding. Record the numbers here.

**First round (2026-10-11, Intel N97).** Network identity and dependencies
replaced with our own code (§1.6) with no regressions. Optimisation
applied: the engine logger discards records below the handler threshold
**before** formatting them, so at WARNING (normal daemon use) the
`Debugf`/`Infof` calls on the hot paths no longer allocate the message. Micro-benchmark
`BenchmarkEngineLoggerFiltered` vs `Forwarded`: ~151 ns / 3 allocs against
~785 ns / 6 allocs per call. The records that the handler receives do not change
(ERROR/WARNING remain, the swarm-noise counts are not lost).

**Second round (2026-10-11, Intel N97) — IP filter.** Diagnosis: with the
Aetherinox blocklist (34 MB, **666,763 rules**) `gx-torrent` held ~295 MB of
RSS, of which **~190 MB** in the pointer-based segment tree of the IP filter
(`internal/blocklist/stree`) plus the 34 MB of the raw file kept in cache by the
daemon. Replacement: the IPv4 rules, like the IPv6 ones already, are stored in
**sorted and merged intervals** with binary search (`sortMergeV4`/`v4Contains`),
`stree` removed. Isolated measurement on the real file: live heap **190 MB → 5.4 MB**
(666,763 rules → 343,024 intervals after the merge); `BenchmarkBlocked`:
**18.6 ns/op, 0 alloc/op** (previously `stree.Contains` allocated a `map` and a
slice on every lookup, on the path of every peer/tracker connection). In
operation: `gx-torrent` RSS **295 MB → ~105 MB** (RssAnon 275 → 92 MB) with
the same number of rules loaded. The 34 MB of the raw file remain in cache (rare
re-reading on manual reload): removable later.

**Third round (2026-10-11, Intel N97) — SQLite page cache.** The `cache_size`
of the SQLite connections (`sqlite.go`) goes from 8 MB to **2 MB** (SQLite's
default). Isolated measurement on the real archive DB (510 MB, FTS5; query
`Archive.Browse` and `CountMatches`, tried up to 16 MB of cache): the latency is
**flat**, within the noise even on the broad `MATCH` (~1.14 s), because it is the
OS page cache that serves the file, not SQLite's. Live on `gexttod`, with the same
uptime (2 minutes): RSS **109.7 → 67.6 MB** (RssAnon 88.4 → 46.2 MB, RssFile
unchanged); the delta is the page caches of the 4 connections.

---

## 2. Gextto improvements implemented

Ex `docs/proposte-migliorie.md` (state as of 2026-10-07: **all approved and
implemented**). Comparison with Sonarr/Radarr (series/movies), Mylar3/Kapowarr
(comics), Bazarr (subtitles), Prowlarr (indexers).

| # | Proposal | Decision | Commit |
|---|---|---|---|
| 1 | Hardlink instead of copy during seeding | yes, with an anti-loss guarantee | `20b28eb` |
| 2 | Targeted refresh of Jellyfin and Plex | yes | `1bfaad4` |
| 3 | "upgrade until" threshold | yes, in addition to the minimum difference | `95f6f97` |
| 4 | Optional authentication | yes, off by default, LAN exempt | `7dae752` |
| 5 | iCal calendar | yes, with declared Italian dates | `e78bcc2`, `a919fcd` |
| 6 | `ComicInfo.xml` in comics | yes | `6a7f739` |
| 7 | Missing: exclude episodes being downloaded | yes | `e815180` |
| 8 | Badge of active downloads in the bar | yes | `893bc94`, `42db4b2` |
| 8.1 | Translations of the new elements in all languages | owner's request | `698efbf` |
| 9 | Documentation update | owner's request | final `docs:` commit |

### 2.1 Hardlink instead of copy during seeding

- **Before**: a file being seeded was copied into the library (space used twice).
- **Now** (`hardlink.go`, `hardlink_seeding`, active by default): episodes in a folder,
  season packs and episodes in copy mode enter the library as hardlinks. The
  link is created with a hidden name and then renamed: the torrent file is never
  touched. If the link is not possible (different filesystems, RAM disk,
  shares without hardlinks) it is copied as before, with a log line per
  pair of disks.
- **Safety**: the two names are peers (not like symlinks). Deleting the
  name in the download leaves the archive intact (only the seed is interrupted); the
  data disappears only by deleting both. Verified with tests.
- Movies are not involved: they are already moved with the torrent (no copy).

### 2.2 Targeted refresh of Jellyfin and Plex

- **Now** (`media_refresh.go`): after an import Jellyfin receives
  `/Library/Media/Updated` with the folder and Plex
  `/library/sections/{id}/refresh?path=` for the section that contains it. The
  requests during a refresh are merged. Unknown folder, no
  Plex section or server refusal → full refresh as before.
- `jellyfin_path_mappings` / `plex_path_mappings` mappings per server in Docker.
  The manual buttons remain complete.

### 2.3 "upgrade until" threshold

- **Now** (`upgrade_until.go`, `upgrade_until_score`, 0 = off): with a score
  ≥ threshold the copy is no longer replaced; REPACK and PROPER remain accepted.
  It applies to the cycle, missing episodes, season packs and movies. "Why not this one?" indicates the
  rejects by threshold. References: 1080p WEB-DL H.264 ≈ 1280, 1080p WEB-DL
  H.265 DD+ ≈ 1480, 2160p ≈ 2480.
- The minimum difference (`upgrade_min_score_diff`) remains (*by how much* it must
  improve vs *up to where*). The per-title block ("No upgrade") is also
  in the web series tab.

### 2.4 Optional authentication

- **Now** (`auth.go`, *Configuration → Access*): off by default. On: anyone
  not on the LAN must log in (bcrypt password, signed cookie valid 30 days, a new
  password closes the sessions) or use the API key (`X-Api-Key` or `?apikey=`; the
  TUI takes it from `GEXTTO_API_KEY`).
- LAN exempt by default. Behind a reverse proxy the forwarded addresses count
  (`X-Forwarded-For`, `X-Real-IP`, `Forwarded`).
- 5 wrong passwords = IP block for 1 minute. Without a password or key the
  check remains open (warning in the log). `GEXTTO_AUTH_DISABLE=1` to get back in.

### 2.5 iCal calendar

- **Now** (`calendar_ics.go`, `GET /feed/calendar.ics`, link in Integrations).
  For the Italian audience (series dubbed months/years later) it distinguishes:
  - 📥 **arrived in the library** in the last 30 days (day of arrival);
  - 📺 **original air date** TMDB of the current season (✓ if in the library);
  - 🎬 **Italian release of movies** TMDB (digital → home video → cinema), with a
    declared fallback to the original date.
- The "Upcoming releases" tile of the Dashboard declares that it is the original
  air date.

### 2.6 Comics: `ComicInfo.xml`

- **Now** (`comicinfo.go`): in CBZ files downloaded directly (HTTP, Mega)
  `ComicInfo.xml` is added with series/number/year from the GetComics title. Not
  touched: files that already have it, CBR, comics via torrent (being seeded). An error
  never compromises the download.

### 2.7 Missing without episodes already being downloaded

- Episodes with an active torrent (even added by hand, from a watched folder or
  phone) and seasons with a season pack in progress are no longer missing, either in the
  cycle or in the series tab. A download in error makes them missing again.

### 2.8 Badge of ongoing downloads

- The "Downloading" entry shows the ongoing downloads (unfinished torrents + active HTTP,
  not seeds), every 5 s; on the phone it is a counter on the icon.

### 2.9 Excluded because already present or deliberately removed

Webhook HMAC, event hook, title aliases, blocklist, delays (delay profile), season
packs, subtitles in the score, Discover/Popular TMDB: already present.
Trakt: deliberately removed (`TestTraktIntegrationRoutesRemoved`).

### 2.10 Removal with data, giving up on stall and tables (2026-10-12)

Real case: "Morgane…S04E01-08" (0%) did not disappear with *Torrent + data*.
- **Wrong order in `SafeRemoveTorrent`:** the files were moved to the
  trash *before* removing the torrent from the engine. With the trash and the download on
  two different NFS shares the move is a copy (8 preallocated files,
  18.8 GB of zeros) done inside the HTTP request, while gx-torrent kept the files
  open: the "busy" `.nfsXXXX` made the copy fail and the torrent reappeared.
  Now the torrent leaves the engine first; the rename on the same filesystem remains
  synchronous, the copy between filesystems goes to the background (log at the end of the copy); a
  torrent without downloaded bytes does not go through the trash (the engine deletes it); the log
  no longer says "files deleted" when the move fails. Test
  `TestSafeRemoveTorrentNothingDownloadedSkipsTrash`.
- **Giving up on a stall without a blocklist:** after the *Gave up* the next
  search had picked up the same release (from an archive hit). Now the
  release goes into the blocklist (`stalled_dead_swarm`), as the *dead magnet* branch
  already did.
- **Tables of the Downloads panel:** at 1366 px the torrent table asked for
  1489 px (horizontal scroll even at 1600 px), the *Download history* 1451 px.
  Fixed layout + container query in `em` (they follow A−/A+): the secondary columns
  flow into the progress cell. Measured with headless Chromium at
  1024/1280/1366/1600/1920 px and text at 85/100/140%: no overflow from 1366 px
  upwards; the Archive with text at 140% no longer overflows (actions column in rem).

---

## 3. Strategic direction

Ex `docs/scelte-di-progetto.md` (extracted on 08-10-2026 from the adoption plan
`SONARR_RADARR_REPLACEMENT_PLAN.md`, removed: the durable parts are here).

### 3.1 Positioning

- Gextto is for **new installations**, not a drop-in for Sonarr/Radarr: no
  import of their databases, no in-place migration, no rollback.
- Integration **towards** the indexer managers (Jackett/Prowlarr), not
  duplicating them inside Gextto.
- **Non-goal**: `*arr` / `/api/v3` API compatibility; quality profile and custom
  format; multi-user authentication (remote exposure goes through the reverse
  proxy in `docs/SECURITY.md`). The differences are published, not hidden.
- Target: whoever has a server + media server (Jellyfin/Plex) and wants "find,
  download, rename, archive" with a single daemon; very likely they already have
  Jackett or Prowlarr.

### 3.2 Indexer managers (Jackett/Prowlarr)

Gextto treats the manager as **a single aggregated source** and does not import its
list of indexers: the manager is the only source of truth for indexers,
categories, credentials, FlareSolverr and rate limits.

Measurements (same library, same host):

| Scenario | Latency | Results |
|---|---|---|
| Aggregate with a broken indexer enabled | 60 s (timeout) | 0 |
| Healthy indexer called directly | 0.25–2.4 s | 40–100 |
| Aggregate after disabling the broken one in the manager | 2–4 s | 156–240 |

Consequences (all implemented):

- a prototype that duplicated the indexer list was built, measured and
  **reverted**: no stability gain, only configuration drift;
- the **categories** were removed: an aggregate serves series and movies, a
  static category per indexer is useless or harmful (TV query with the "movie"
  category → zero results);
- the manager type can be **declared** (`manager` in `config.go` and the UI)
  instead of inferred from URL/port/name (the fallback remains);
- the manager's health (Prowlarr `indexerstatus`) is in Sources: a
  broken/disabled indexer is visible instead of giving zero results in silence
  (`indexer_health.go`);
- a **timeout per source** limits a single request below the search budget;
- Prowlarr's search API **ignores** season/episode and external-id
  (measured): they are not forwarded; `type` also has no observed effect;
- **Jackett's** indexer list **is not reachable** with the Torznab key (its
  API responds with a redirect to the login): irrelevant, only the aggregate is needed.
  Jackett exposes Torznab at `/api/v2.0/indexers/all/results/torznab/api` instead of the
  generic `/api` (which responds 404): `internal/torznab` infers it from the `:9117` port
  or from the name with an automatic fallback on 404, shared between Gextto and `gx-torrent`.

### 3.3 FlareSolverr

- **Persistent sessions** per domain, with a short TTL and destruction, instead of a
  browser per request; if not supported → stateless mode.
- **Never for indexer managers**: Jackett/Prowlarr handle Cloudflare on their
  own; a 403 or network error there is reported as it is instead of wasting
  20–30 s masking the cause.
- `solution.status` is respected: a page "solved but in error" is not interpreted
  as content.

### 3.4 Selection policy

Gextto does **not** implement quality profiles, custom formats or a regex/score engine
like `*arr`. Instead it chooses global defaults (resolution, source, codec,
audio, HDR, language, size constraints) + **per-title overrides and exclusions** +
**explainability** (the explanation API says why a release is
accepted/rejected/preferred). Reason: profiles are the most complex part —
and the most misconfigured — of the `*arr` model.

### 3.5 Comparison with Sonarr/Radarr (honest snapshot, 2026-10)

**Where Sonarr/Radarr are ahead:**

- **Quality profiles and custom formats**: score per resolution/source/type/
  release group (even regex), delay profile, different profiles per series and movie.
  Gextto has a single policy + "upgrade until" + per-title override: easier,
  less powerful for fine rules. *Feasibility:* giving **names to profiles on the Gextto
  model** is a medium step (the global one becomes "Default", each combination of
  overrides a generated profile, automatic migration from the per-title data in
  `series.quality/language/subtitle/exclude` and `movies_config.*`). Reproducing the
  **Sonarr semantics** (ordered qualities + cutoff + custom format) is more expensive;
  custom formats (regex engine + UI) must be evaluated separately.
- **Maturity of import and rename**: multi-episode, specials,
  daily, manual import with matching and replacement. *Feasibility:* study the
  behaviour and reimplement it; Sonarr is GPLv3, Gextto EUPL 1.2: **no
  code or test files are copied**, we build our own corpus from real
  names.
- **Ecosystem and API**: `/api/v3` consumed by Overseerr/Jellyseerr; Gextto does not
  expose it. *Feasibility:* full compatibility is not realistic; a
  **subset** for Jellyseerr is (`system/status`, `qualityprofile`,
  `languageprofile`, `rootfolder`, `tag`, `series/lookup` + `POST series`,
  `movie/lookup` + `POST movie`, `queue`) — but after named profiles, root folder
  and tags, and tested with a real Jellyseerr.
- **Notifications**: *done* — the webhook also speaks **Discord, Slack, ntfy, Gotify
  and Pushover** via a format selector (`notify_webhook_format`); the more esoteric
  providers remain.
- **Import of existing libraries**: partial in Gextto, not a priority.
- **Community, wiki, Docker, cadence**: a matter of time, not architecture.

**Differences that are choices, not gaps:** indexers via an external manager
(cleaner and safer separation); **no usenet** (torrent, HTTP, web seed;
in the Italian context usenet is marginal).

**Where Gextto is ahead (or on par):** all in one (series, movies **and comics**) in a
single daemon; integrated torrent engine (gx-torrent pure Go, libtorrent or
qBittorrent-nox); explainability; search also from web engines; lightness (a
Go binary + SQLite vs .NET); simplicity (health, backup, maintenance integrated).

**Verdict:** Sonarr/Radarr remain better at selection (profiles/custom
formats), import edge cases and ecosystem/API. Gextto is better at single
scope, integrated engine, explainability, footprint and simplicity. Correct position:
**an alternative for new installations**, without claiming parity.

---

## 4. gx-torrent: gaps and improvement plan

Ex `docs/gx-torrent-migliorie.md` (state as of 2026-10-08: work completed,
final re-check done).

### 4.1 Context

`gx-torrent` = BitTorrent engine in **pure Go**, an alternative to libtorrent:

- **gx-core engine** (`internal/gxcore`): transfer;
- **daemon + adapter** (`cmd/gx-torrent`, `gxtorrent_engine.go`): REST, web
  page, self-managed queue, bridge with Gextto (`TorrentEngine`).

Gextto already provides for all engines: queue/slot, stalled/retry, seed policy,
RSS, search, categories/tags, bandwidth scheduler, post-processing, moves and
RAM disk, IP filter, proxy. These **are not gaps of the engine**.

State per capability: `capabilityLevels` (`torrent_engine.go`), the single source of
truth used by the UI.

### 4.2 Real gaps

**vs libtorrent / qBittorrent** (state after the work):

| Area | What was missing | Outcome |
|---|---|---|
| Per-torrent limits | download/upload speed | done (`SetLimits`) |
| Per-torrent limits | connections and upload slots | done (`SetMaxConnections`/`SetMaxUploads`) |
| Modes | upload/share mode | skipped (infinite seed with ratio 0 is used); super-seeding done |
| Priority | per-piece, piece diagnostics, file priority | done (`pieces` API, diagnostics, **graduated file priorities 0..7**, §4.11) |
| Trackers | removal | done (`set-trackers`) |
| Web seed | hot add/remove | done (`webseeds` action) |
| Sequential | runtime toggle | done (`PiecePicker.SetOrder`) |
| Protocol | IPv6 | done (dual-stack socket, `peers6`, PEX v6, DHT BEP 32); LSD v6 done |
| Protocol | BitTorrent v2-only | done (`.torrent` and magnet, BEP 52) |
| Quality | seeding/choking algorithms | measured, see §6 |

**vs anacrolix/torrent**: the **HTTP Range streaming is done** (§4.3 #7) and so is the
**reader with seek and readahead** (graduated window, §4.7); **mmap** is there
as a storage option (§5.4) and the **alternative storages** remain (blob, bolt,
sqlite) and WebTorrent/WebRTC (wishlist).
gx-torrent has instead a supervised
daemon, web UI, REST, UPnP/NAT-PMP, single port, SOCKS/HTTP proxy, multi-format IP
filter, VPN killswitch, preallocation, adaptive cache.

**Not gaps**: RSS, search plugin, categories/tags, scheduler, ratio,
post-processing, sequential as a mode (only the hot toggle was missing),
encryption, IP filter, proxy, DHT/PEX/LSD, UPnP/NAT-PMP, fast resume, file
selection.

### 4.3 Decisions

| # | Improvement | Decision |
|---|---|---|
| 1 | Tracker removal/replacement | **Done** — `Torrent.SetTrackers` in the engine + `set-trackers` |
| 2 | Upload/share mode | **Skipped** — infinite seed (ratio 0) |
| 3 | Hot sequential/first-last toggle | **Done** — `PiecePicker.SetOrder` + `SetSequential` |
| 4 | Web seed add/remove via API | **Done** — `AddWebseeds`/`RemoveWebseeds` + `webseeds` action |
| 5 | Piece diagnostics | **Done** — `pieces` API, Pieces tab in Gextto and in the daemon |
| 6 | Per-torrent speed limits | **Done** — `Limiter.SetParent`/`SetLimitKiB` + `SetSpeedLimits`, reapplied on restart |
| 7 | HTTP Range streaming + piece priority | **Done** — `SetStreamWindow`, `/ui/stream`, Gextto redirect, ▶ button |
| 8 | Per-torrent connection/upload limits | **Done** — `SetMaxConnections`/`SetMaxUploads` + `Unchoker.SetNumUnchoked` |
| 9 | IPv6 | **Done** — dual-stack TCP/UDP socket, v6 resolution and addresses, `peers6`, PEX `added6`/`dropped6`, UDP v6 tracker, `yourip` v6, blocklist CIDR v6, dual-stack DHT (BEP 32) |
| 10 | BitTorrent v2-only | **Done** — parsing of `meta version`/`file tree`/`piece layers`, truncated SHA-256 info-hash, `merkle` package, per-file piece model, Merkle verification, download from `.torrent` and from **magnet**: the `piece layers` are downloaded from peers with `hash request`/`hashes`/`hash reject` (BEP 52) and verified against the `pieces root` |
| 11 | Super-seeding (BEP 16) | **Done** — `SetSuperSeeding` + `super-seeding` action; capability `full` |
| 12 | Holepunching (BEP 55) | **Done** — `ut_holepunch` in the engine, peer relay, uTP dial, startup after a failed dial; WebTorrent remains wishlist |
| 13 | Seeding/choking quality | **Measured (local)** — correct and cheap choking; super-seeding with no gain on small swarms, opt-in; scaling on a real swarm, see §6 |

Semantics of limits: **-1 = inherit the global one, 0 = unlimited** (like libtorrent).

### 4.4 Remaining wishlist

- **BitTorrent v2-only**: **supported end-to-end** — `.torrent` with `piece
  layers` and **magnet** (`urn:btmh:`), per-file piece model, verification via
  SHA-256 Merkle node, truncated identity and **fetch of the `piece layers` from peers**
  (`hash request`/`hashes`, BEP 52). Seed/leech test for `.torrent` and magnet.
  Detailed design in [`gx-torrent-v2.md`](gx-torrent-v2.md).
- **WebTorrent/WebRTC**: niche, a lot of work.
- **Seeding/choking quality**: first round done (see §6); for scaling
  a campaign on a real swarm is needed (gx-torrent vs libtorrent/qBittorrent, same
  policy, median over several runs).

### 4.5 Verification criterion (applied)

- Per point: `go test ./cmd/gx-torrent/`, `go test -run GxEngine .`, the engine where
  it is touched (`make test-engine`).
- With a visible effect: `docs/gx-torrent.md`, `MANUAL.*`, `README`.
- Every improvement visible in **both UIs** (Gextto `uiweb/v2` + the daemon
  page `cmd/gx-torrent/ui.go`), never only via API.
- Comparisons with libtorrent/qBittorrent only by measurement.

### 4.6 Final re-check (done)

- `capabilityLevels`: gx-torrent `full` for `limits`, `trackers`, `sequential`,
  `piece_diagnostics`, `web_seeds`, `super_seeding`, `session_stats`; also
  `first_last` and `holepunch` are `full`.
- Notes/texts updated: `v2DetailCapsFor`, "Known limitations", `gx-torrent.md`,
  `API.md`, `MANUAL.*`, `README*`.
- Every feature visible in both UIs (trackers, web seed, limits,
  piece diagnostics, streaming, super-seeding).
- Happy-path test + edge cases (empty list, duplicates, not-found, 416,
  completed/stopped); `make test` and `make test-engine` green (+ the `unchoker`
  package in `make test-engine`).
- Inventory of the changes in `internal/gxcore/GEXTTO.md`;
  `go vet`, `gofmt` green; `-1`/`0`/`>0` semantics consistent.
- Remaining note: waiting for pieces in polling streaming (200 ms, timeout 2 min),
  without end-to-end tests on a real swarm; present data and the byte→piece
  mapping are covered.

### 4.7 Cherry-pick from anacrolix (agreed shortlist, 2026-10-10)

Source: `github.com/anacrolix/torrent` v1.61 (Go, **MPL-2.0**). Two ways of
taking from it, consistent with the repo policy (**third-party code is not copied;
the algorithm is rewritten** — the idea is not covered by copyright, the expression
is — or the module is adopted as an MPL-2.0 **dependency**, as
`github.com/anacrolix/utp` already is). No licence change for Gextto/gx-core.

Rules: one item = one commit; **measure before/after**; changes to `internal/gxcore`
recorded in `GEXTTO.md`; targeted tests (`./cmd/gx-torrent/`, `GxEngine`,
`make test-engine`, `make test`); with a visible effect also the two UIs (Gextto
`uiweb/v2` and the daemon page).

| # | Item | anacrolix source | Way | Effort | Status |
|---|---|---|---|---|---|
| 1 | Reader with seek + readahead and **graduated piece priority** | `torrent.Reader`, `ReadaheadContext`, `PiecePriority`, `File.SetPriority` | rewrite | M | **done (priority)** |
| 2 | **Adaptive request queue** | `client-piece-request-order.go`, `RequestIndex`, `reqq` | rewrite | M | **done** |
| 3 | **`anacrolix/dht`** in place of the current DHT (BEP 42/43/44, BEP 33) | module `github.com/anacrolix/dht` | dependency | M | **BEP 42/43/44/33 done in the engine** |
| 4 | **Tracker scrape** (BEP 48) | `tracker` (scrape) | rewrite | S | **done** |

Work order: **1+2 together** (same intervention: streaming), then **4**
(ready), then **3** (evaluated, below). Status: **1** and **4** done, **2** done
(to be measured on a swarm), **3** postponed with justification.

**1 — Reader with seek, readahead and piece priority. Done (priority).** Streaming
now uses **two graduated windows**: `SetFileStreamWindow` (the bytes the
player reads now) has priority over the **new** `SetFileReadaheadWindow` (the
prefetch), which in turn precedes rarest-first. In the picker:
`SetReadaheadWindow`/`pickReadahead`, tried before the allowed-fast and the
rarest ones; test `TestReadaheadWindowAfterStreamWindow`. The daemon updates both
windows while writing (`setStreamWindows`). The **reader with seek** is not a new
engine type: the function is already covered by the HTTP `Range` handler
(every request sets the window), and the picker tests cover the two
windows.

**2 — Adaptive request queue. Done.** The depth of in-flight requests per
peer was fixed at `DefaultRequestsOut` (50), with a cap of `MaxRequestsOut` (250) or the
announced `reqq`. Now `peer.RequestQueue` **grows by one with every delivered
block** (`boostRequestQueue`, called on the refill in
`torrent_messagehandler.go`) up to the cap, and **is reset on choke**
(`resetRequestQueue`): a slow peer stays at the base, a fast one slides towards
`MaxRequestsOut`; with a small `reqq` the cap stays that one. Logic in
`torrent_start.go` (`maxAllowedRequests`/`requestQueueCap`/`boost`/`reset`), test
`TestAdaptiveRequestQueue`. To be **validated on a real swarm** (as per the repo
rule) before considering it stable.

**3 — `anacrolix/dht` and DHT modernisation. BEP 42 done;
adoption postponed.** Today
`internal/dht` is the engine's DHT (a module of its own, `replace` in `go.mod`):
dual-stack BEP 32, `Stats()`, `Config.PacketConn` for the shared uTP socket,
robust read-loop. Candidate: `github.com/anacrolix/dht/v2`
(**MPL-2.0**, maintained; evaluated **v2.24.0**, May 2026). What it has upstream
(verified on pkg.go.dev): `ServerConfig.Conn net.PacketConn` (**shared uTP
socket**, like the engine's `Config.PacketConn`), `IPBlocklist iplist.Ranger`
(IP filter), **BEP 42** (`SecureNodeId`/`NodeIdSecure`), **BEP 44** (`bep44`,
`Store`, `Put`/`Get`), **BEP 33** (`Scrape()`), plus BEP 5/43.

Why it is **not drop-in**: the API model is different. The current DHT exposes a
`PeersRequestResults` channel + a periodic tick + `PeersRequestPort` (this is how
`session_dht.go` is written); `anacrolix/dht` uses `AnnounceTraversal(ih)` → `*Announce` with a
`Peers` channel, plus `Bootstrap`/`TableMaintainer`/`AddNode`. The following would need rewriting:
`session_dht.go` (search → traversal per torrent), the creation in `session.go`
(`NewServer(ServerConfig)`), the **stats** (`ServerStats` pointed vs the map of
`dht.Stats()` used by `session_stats.go` and the UI) and the mapping of the
blocklist onto `iplist.Ranger`. Moreover the engine patches (dual-stack BEP 32,
`PacketConn`, read-loop) must be **re-verified** upstream.

Gain: a maintained dependency, BEP 42/44/33, and **no module of its own + `replace`**.
Cost: **rework of the DHT layer** (M/L), double validation (dual-stack, shared
socket) and we would lose control of the DHT. The DHT use in gx-core is minimal
(only search and announce), so the benefit is not immediate. **Verdict: not
now**; to be reopened if BEP 44 (mutable items) or the BEP 42 security are needed, or if the
module of its own becomes a burden — as a task of its own with a validation plan (real swarm,
dual-stack, shared socket, stats). MPL-2.0 licence (unmodified dependency,
like `anacrolix/utp`), no problem.

**BEP 42 + 44 + 43 + 33 modernisation (2026-10-11).** The DHT has been updated
with the **security extension BEP 42** (`dht_sec.go`): the node ID is derived from the
external IP (CRC32C Castagnoli; the official BEP vectors are verified in
`dht_sec_test.go`). The external IP comes from a routable `DHTHost` or from the
outgoing interface; if unknown, the node learns it from the `ip` field of the
responses and adopts a secure ID (persisted). Local addresses are exempt. Also added:
**BEP 44** (`bep44.go`: immutable and mutable signed ed25519 key/value store,
`get`/`put` queries, local API; official vectors in `bep44_test.go`),
**BEP 43** (read-only: `Config.ReadOnly`, daemon flag `-dht-read-only`) and **BEP 33**
(`bloom.go` + `peer_store.go` + `scrape_client.go`: `BFpe`/`BFsd` filters, swarm
estimate, client towards the nearest nodes; official vector in
`bloom_test.go`), exposed by the engine (`Torrent.DHTScrape`), by the daemon
(`GET /api/v1/torrents/{hash}/dhtscrape`) and by the page (**DHT swarm** button).
**BEP 44** now also has the **iterative network client** (`bep44_client.go`):
`GetImmutable`/`GetMutable` do a bounded traversal over the nearest nodes and
`PublishImmutable`/`PublishMutable` publish by first collecting the token with
a `get`. It is the consumer that enables **BEP 46** (magnet `xs=urn:btpk:`).

**BEP 44 fix (2026-10-11).** The `get`/`put` requests decode the dictionary
`a` with its own bencode decoder (`internal/dht/bencode.go`): `bencode-go`
v1.0.0 used to **panic** (`reflect: reflect.Value.Set using unaddressable
value`) on the string values of a `map[string]interface{}`, i.e. on **every**
`get`/`put`, crashing the whole daemon (`exit status 2`, restart of
Gextto). The decoder does not use reflection, has a depth cap (32) and
returns errors instead of panicking; `readResponse` now signals the error when
it recovers a panic, instead of returning partial data with a `nil` error. Tests in
`bencode_test.go`.

**4 — Tracker scrape (BEP 48).** **Done.** `Scrape` was added
to the `tracker.Tracker` interface and implemented in **HTTP** (`/scrape`, with
`scrapeURL` mapping `…/announce` → `…/scrape` keeping the passkey) and **UDP**
(new BEP 15 action, request generalised to `udpRequest` with
`GetDest`/`SetConnectionID`/`Done`). `Tier.Scrape` delegates to the current tracker and
advances on error like `Announce`. Exposed as `Torrent.Scrape` (concurrent
scrape of all trackers, error per tracker) and by the daemon with
`GET /api/v1/torrents/{hash}/scrape`. Tests: `httptracker_test.go`,
`udptracker_test.go`. The button in the daemon page remains.

**Note (items 1‑2, 2026-10-10).** After the study, **#2** turned out to be
implementable **without** anacrolix's multi-piece pipelining:
`DefaultRequestsOut` (50) is a real base below the `MaxRequestsOut` cap (250),
so the depth can adapt **within** the current architecture (one piece per
peer). **#1** remains the graduated priority (stream > readahead); the "reader with
seek" is covered by the HTTP `Range` handler. Both must be **measured on a real
swarm** before considering them closed.

### 4.8 Beyond the shortlist (new items, 2026-10-10)

Four items that emerged from the comparison with anacrolix/libtorrent, **beyond**
the §4.7 shortlist. Same rules: rewriting of the algorithm (or an MPL-2.0
dependency), one item = one commit, measurement/tests, `GEXTTO.md`, both UIs.

| # | Item | Source | Way | Effort | Status |
|---|---|---|---|---|---|
| 1 | **Engine event bus** (piece state, peer/messages, trackers) | anacrolix `SubscribePieceStateChanges`, `PeerMessageEvent`, `StatusEvent`; libtorrent *alerts* | rewrite | M | **done** |
| 2 | **`.torrent` creation from a folder** (`mktorrent`-style) | anacrolix `metainfo` | rewrite | M | **done** |
| 3 | **Bad-storage / write errors at runtime** | anacrolix `bad-storage.go`, `SetOnWriteChunkError` | rewrite | S/M | **done (diagnosis)** |
| 4 | **LSD IPv6** (BEP 14, group `ff15::efc0:988f`) | anacrolix `bep14.go` (`Ip6`) | rewrite | S | **done** |

**1 — Engine event bus. Done.** `Session.Subscribe()` (`torrent/events.go`)
returns a channel of `Event`; the engine never blocks on a slow subscriber
(it discards if the buffer of 64 is full). Types: `torrent_added`,
`torrent_completed`, `torrent_stopped`, `torrent_error`, **`torrent_piece`**
(high frequency), **`peer_connected`/`peer_disconnected`**, **`tracker_announce`**
(with seeders/leechers, or the error). Emitted in `session_add.go`,
`torrent_write.go`, `torrent_peer.go`, `torrent_close.go`, `torrent_stop.go` and by the
hook on the announcer (`SetResultHook`). Exposed by the daemon as **SSE `GET
/ui/events`** and consumed by the page, which updates immediately on state
events instead of waiting for polling (2 s) — polling remains as a safety
net.

**2 — `.torrent` creation from a folder. Done.** The part of
`internal/metainfo` that writes the info dict already existed (`NewInfoBytes` +
`NewBytes`); the exposure was missing. Added `torrent.CreateTorrent(path, opt)`
(`torrent_create.go`) and, in the daemon, a **form on the page** (`POST
/ui/create-torrent`) and the endpoint **`POST /api/v1/create-torrent`**, which
return the `.torrent` as a download. Verification: the created `.torrent` is
re-read by `metainfo.New`.

**3 — Bad-storage / write errors. Done (diagnosis).** The engine **already**
stops the torrent on a write error without looping (`handleStopped` with
`lastError`). The added value is the diagnosis: the error now identifies the
**storage folder** (`storage %q could not write piece #N`) and the failures are
counted (`SessionStats.StorageErrors`, exposed as `storage_errors` in the daemon
data). It is not a separate state machine as in anacrolix.

**4 — LSD IPv6 (BEP 14). Done.** Correction: **BEP 14 is Local Service
Discovery**, already present (`cmd/gx-torrent/lsd.go`), not the negotiation of
messages. The addition is the **second family**: announcement and listening also on the
IPv6 group `[ff15::efc0:988f]:6771` (best-effort: without IPv6 multicast the
v4 one remains), like anacrolix (`config.Ip6`). LSD state with `ipv6` in `/api/v1/status`.

### 4.9 BEP compliance: state after the review (2026-10-11)

Item added after an external review (document `analisi5.md`, kept outside
the repo) that compared `gx-core` with libtorrent, Transmission, BiglyBT,
anacrolix/torrent and rain. The review **corrected two wrong claims** and
opened two items that have been closed here.

| BEP / area | Real state | Notes |
|---|---|---|
| **BEP 21** (`upload_only`) | **done (peer + tracker)** | The magnet is not involved: BEP 21 is the `upload_only` key in the BEP 10 extension handshake, plus `event=paused` on announce and `downloaders` in the scrape. The header is sent on connection and forwarded to all peers when the torrent becomes a seed (`torrent.uploadOnly` = `completed`, therefore also partial seeds on selection); a **partial seed** announces `event=paused` (`tracker.EventPaused`). Only the `downloaders` field of the scrape is missing (optional, provided by the tracker) |
| **BEP 53** (`so=`) + magnet `ws=`/`dht=` | **partial** | `so=` (Select Only, BEP 53): `Magnet.SelectOnly` parses indices and ranges, de-duplicated, as the torrent's seed applied by `skippedInfoFiles` **only** when `Config.FileSelection` does not provide a mask (`Torrent.SelectOnly`/`ClearSelectOnly`); the daemon turns it into `FilePriorities` on arrival of the metadata. Magnet conventions: `ws=` (web seed) → `Magnet.WebSeeds`, persisted in the resume; `dht=` (bootstrap DHT node) → `Magnet.DHTNodes`, added to the session DHT. `xs=`/`as=` remain |
| **BEP 41** (UDP tracker URLData) | **already present** | The document gave it as absent: `internal/tracker/udptracker/messages.go` already writes the TLV options (type `0x2` = URLData, not `0x1`) from `u.RequestURI()`. No work |
| **BEP 44 iterative client + BEP 46** (`urn:btpk:`) | **done** | The DHT now has the outgoing client (`internal/dht/bep44_client.go`): `GetImmutable`/`GetMutable` with bounded traversal over the nearest nodes, `PublishImmutable`/`PublishMutable` that collect the token with a `get` and publish with `put`; `handleBep44Reply` decodes the response with the panic-free decoder and continues the traversal. **BEP 46**: the magnet `xs=urn:btpk:<pubkey>` (+ `s=<salt>`) resolves the info hash from the mutable item in `Session.addMagnet`. Tests: `bep44_client_test.go`, `bep46_test.go`, magnet |
| **BEP 54** (`lt_donthave`) | **done (reception)** | BEP 10 extension `lt_donthave`: a `DontHave` clears the bit of the piece in the peer's bitfield and the availability in the picker; without Fast Extension it also cancels the active download of that piece. Reception only (gx-core does not discard pieces). Correction: BEP 54 is **not** STUN (error of the external analysis); there is no BEP for STUN |
| **BEP 31** (`retry in`) | **done** | HTTP/UDP announce and scrape extract `retry in` in minutes and respect it; the value `"never"` removes the tracker from the tier forever (`tracker.Tier`, without stopping the other trackers of the same tier). Single parsing in `tracker.NewError`/`SetRetryIn` |
| **BEP 38** (`similar`/`collections`) | **partial (candidate discovery)** | Hints read from the info dictionary **and** from the top level (united and de-duplicated in `metainfo/bep38.go`), re-emitted in the export (`NewBytesWithHints`), exposed with the local torrents that share a hint (API `GET /api/v1/torrents/{hash}/collections`, daemon page, Gextto detail, with the symmetry of the BEP). **Block reuse** is Swarm Merging (§15.2) |
| **BEP 17 / 34 / 35 / 51, WebTorrent, I2P** | **out of scope** | Superseded, irrelevant or expensive (WebRTC, SAM). Confirmed; 45/51 marginal |
| **10 Gbps pipelining** | **to be measured** | The adaptive request queue already exists (§4.7); a write ring buffer is speculative. It is worth it only with before/after measurements (performance rule of `AGENTS.md`) |

Corrections to the external document, kept as a note: (a) BEP 41 was **not**
missing and the URLData option is `0x2`; (b) BEP 21 is **not** an extension
message with an id in `m`, but a handshake header, plus the tracker changes;
(c) the magnet parser **did not** extract `ws`/`xs`, so BEP 53 was "absent" and not
"partial"; (d) the engine exposes `Config.FileSelection`, not a method
`Torrent.SetFileSelection`; (e) the comparative matrix with the external clients is
circumstantial and must not be used as a source.

**Performance (BEP 31 and 38).** No change to the *data path*: BEP 31 touches
only the announce (the `Tier` goes from `atomic.Int32` to a mutex on the index, outside
the data path) and BEP 38 reads two optional fields of the metainfo and
shows them. The picker and unchoker benchmarks remain unchanged
(`BenchmarkPickerRarestSteady` ~102 µs/op, **0 alloc/op**; unchoker 1.3 µs/op),
so a throughput measurement is not needed.

**Review of the existing BEPs (2026-10-12).** Four improvements to implementations
already present: (a) **BEP 42** — the `ip` field is now also **sent** in every
response (to ping, find_node, get_peers, announce_peer and the BEP 44 `get`/`put`), not
only read, with the `reflected_sent` counter; (b) **BEP 5** — `announce_peer`
respects `implied_port` (uses the source port); (c) **BEP 11** — the lists
`dropped`/`dropped6` are no longer added as candidate peers (they were, by
mistake, causing dials to peers that had left the swarm) and a malformed list no
longer discards the others; (d) **BEP 9** — the offset of the metadata request is
computed at 64 bits, so an enormous piece index no longer overflows and
does not request the wrong piece. Tests: `internal/dht/ipfield_test.go`,
`torrent_pex_test.go`, `torrent_metadataextension_test.go`.

### 4.10 DHT: diagnosis, hardening and multi-path (plan and feasibility)

Born from the comparison with **Tixati** (2026-10-11), whose changelog (v3.39, v3.43)
claims a very evolved DHT: anti-Sybil screening of malicious nodes,
multi-path lookups, token hygiene, DHT over I2P and fine diagnostics. The gx-core DHT
(`internal/dht`) already covers **BEP 5/32/33/42/43/44** with a routing
table as a **binary tree without buckets**; the differences with Tixati are of
**hardening and diagnostics**, not of BEP coverage (see [`bep.md`](bep.md)).
Tixati is closed-source: its *idea* is rewritten, not its code.

**Done (diagnostics, 2026-10-11).** `dht.Stats()` now exposes, besides nodes,
traffic and discarded packets, the per-RPC and screening counters:
`queries_in`/`queries_out`/`replies_in` (ping, get_peers, find_node,
announce_peer) and `blocked_hosts`/`self_promotions`/`dupe_references`/
`nodes_killed`. They were already measured as `expvar` but not exposed; the two missing
counters were added (`totalRecvPing`, `totalRecvAnnouncePeer`,
`totalSentAnnouncePeer`). The daemon propagates them as `dht_<key>` (API and UI),
so the work is visible end-to-end. Test: `internal/dht/stats_test.go`.
**Why first:** it is the measurement basis of the items below (performance rule of
`AGENTS.md`). A residue was also closed: the origin comment "Taipei Torrent"
in `dht.go` (it was a marker that §1.6 gives as removed; the attribution remains
the BSD-3 `LICENSE`).

**Items to open (each with a plan and before/after measurement):**

| # | Item | What | Risk | Effort | Verification | Status |
|---|---|---|---|---|---|---|
| 1 | Anti-Sybil hardening | limit the concentration of nodes per subnet (and deprioritise non-BEP 42 IDs when the table is full, **without** an outright rejection: BEP 42 is not applied everywhere) | M | M | `subnet_share_max`/`nodes_insecure_id`, table size and peers found on a real swarm | observability done; enforcement to be measured |
| 2 | Multi-path lookup | conduct `get_peers` on several branches in parallel and prefer peers confirmed by ≥2 paths | M/A | M | peers found and search time on a real swarm | dedup **already present** (`isOK`/`wasContactedRecently`); full multi-path to do, gain not proven |
| 3 | Token hygiene | detect replay and tokens taken from non-matching queries | S | S | unit tests on the codec | **done** |

**Token hygiene — done (2026-10-11).** The `announce_peer` token is now
bound to **source address + scope** (`hostToken`/`checkToken` take the
scope; for `get_peers`/`announce_peer` it is the info-hash), so a token obtained
for one swarm is not reused on another: it is the detection of Tixati's *non-matching
queries*. The BEP 44 paths remain bound to the address alone
(`scope = nil`), unchanged. Added the counters `announce_ok_token` /
`announce_bad_token` (in `Stats()`, therefore visible as `dht_announce_*`).
Test: `TestHostTokenBindsScope`. The rotating double secret already present remains.

**Observability of the concentration — done (2026-10-11).** Before introducing
any anti-Sybil cap, the routing table is now **observable**: `Stats()`
exposes `subnet_share_max` (the maximum number of nodes in the same /24, or /48 for
IPv6) and `nodes_insecure_id` (nodes with a non-secure ID per BEP 42), recomputed at
every `cleanup` by the run loop (`tableMetrics`/`subnetKey` in `dht_sec.go`). They are the
numbers with which to decide *whether* and *how much* to tighten; the **enforcement is deliberately
not active**, because BEP 42 is not applied everywhere and a per-/24 cap would punish
legitimate nodes behind CGNAT/mobile. Tests: `TestSubnetKey`,
`TestTableMetricsConcentration`.

**Measurement harness — done (2026-10-11).** `make measure-dht`
(`GX_MEASURE_DHT=5m`, `GX_MEASURE_DHT_INTERVAL=10s`; `internal/dht/measure_test.go`,
opt-in and outside CI) starts a real node against the public DHT and prints,
sample by sample, the filling of the routing table and the outcome of the queries.
First measurement (45 s, 2026-10-11):

| metric | value |
|---|---|
| known nodes | ~740 |
| `subnet_share_max` (nodes in the same /24) | **14** |
| `nodes_insecure_id` (non-BEP 42 IDs) | **312 / ~740 ≈ 42%** |
| query out / reply in | 1089 / 339 (~31% raw) |
| packets discarded by the rate limit | 485 |
| `reflected_observations` | 0 |

Reading to decide: **BEP 42 must not be applied rigidly** (42% of nodes do not
respect it: discarding them would halve the table), so the per-subnet cap is the right lever
and **14** is the value from which to choose the threshold; `obs=0` confirms the absence
of the `ip` field on this network.

**Multi-path lookup — design.** It requires rewriting the iterative lookup
(`get_peers` on several branches and cross-checking the results); now there is the harness on which to
measure the before/after. No speculative code.

**Dedup verification — already present (2026-10-12).** While trying to implement the
bounded/deduplicated traversal it was discovered that **the routing table already deduplicates**:
`nTree.isOK` calls `wasContactedRecently(ih)`, so a node already queried for
that same infohash **does not appear** in `lookupFiltered(ih)` for
`searchRetryPeriod` (15 s). The iterative re-queue therefore does not require the same
nodes: a `lookups` map with a 45 s window was **redundant** and added
complexity. Choice: **no code** for the dedup part (the "no
speculative code" rule). The **full multi-path** — parallel branches with peers
confirmed by ≥2 paths — remains an unproven idea: the measurement on a real swarm
is too noisy to attribute a gain to it (in 45 s runs `nodes` oscillates
587→3774 and `q_out` 946→4069 without correlation with the code).

**Shutdown bug — found and fixed (2026-10-12).** The measurement harness got
stuck in `Stop()`: `pingSlowly` did `pingRequest <- r` **without** a `select`
on the stop, so with nodes to ping the goroutine stayed hung and `wg.Wait()`
never returned. In the daemon the same path could block
`Session.dht.Stop()` on every reconfiguration. Now the send honours `stop`
(`TestPingSlowlyStopsWithoutReader`).

**Harness — improved (2026-10-12).** `GX_MEASURE_DHT_INFOHASH=<40 hex>` probes a
specific infohash (e.g. a populated public torrent) and the measurement also prints
`peers_found`, so real peer discovery can be verified and not just the filling
of the table (a random infohash has no peers, so `peers_found`
stays 0). With the harness fixed the runs terminate and can be compared.

**Discovery works: measurement on a free public torrent (2026-10-12).** With
the infohash of the **Ubuntu 24.04.3 desktop amd64** torrent
(`d160b8d8ea35a5b4e52837468fc8f03d55cef1f7`, downloaded from `releases.ubuntu.com`)
the harness finds **108 peers in ~30 s** with `q_out=395` and 79 replies: the DHT
search completes discovery on a real, well-seeded swarm. It is the reference
number for any future A/B of multi-path. (The `dht_peers_found` of the running
daemon stays low — ~6 — because its torrents are mostly private, therefore with DHT
disabled, or poorly populated: it is not a defect of the lookup.)

**Anti-Sybil cap per subnet — implemented and measured (2026-10-11).**
`Config.MaxNodesPerSubnet` (default 0 = disabled) limits the nodes **in the routing
tree** to the same /24 (IPv4) or /48 (IPv6); nodes beyond the threshold remain in
the address book with `noTree` (so they are not re-discovered in a loop) and are
counted in `nodes_capped`. It is measured with
`GX_MEASURE_DHT_SUBNET_CAP=8 make measure-dht`.

**The measurement found and fixed a real bug.** The first version discarded
all nodes beyond the threshold and the DHT re-discovered and re-processed them at every round:
a storm of queries (`q_out` **14,352** against ~1,089 without a cap, success **10%**
against 31%, nodes **4,640** against ~740). Keeping the node in `noTree` the storm
disappears (nodes ~814, `q_out` ~1,975). Two caveats: the cap **is not active by
default**, and the threshold must be chosen with more runs — in a sample with cap=8 it never
triggered (`capped=0`, `subnet_max` 6, while a run without a cap had seen **14**),
so the concentration varies a lot between runs and the **query success is
noisy** (17% and 31% with no cap differences). Test:
`TestSubnetCapKeepsTreeBounded`.

**Out of scope:** DHT over I2P (no I2P in the project) and BEP 44/46 **outgoing
client** (no consumer: the "no speculative code" rule; the server side and the local
API remain ready).

**Cold start — done (2026-10-12).** A magnet without a tracker stayed in
`downloading_metadata` for ~75 s even with the DHT working: `DHTMinAnnounceInterval`
(default 1 min) was the retry interval while the torrent looks for peers,
so the DHT table (~30 s to fill) was queried again only after
a minute. Brought to **15 s**; the same test drops to **~49 s** (test
`announcer/dht_test.go`). Measurement in §6.8.

### 4.11 Port mapping (PCP), NAT detection and file priority (feasibility)

Three gaps compared with Tixati, with a feasibility opinion founded on the current
code (`cmd/gx-torrent/portmap.go`, `torrent_engine.go`, `piecepicker`).

| Item | State today | Feasibility | Effort | Value |
|---|---|---|---|---|
| **PCP** (RFC 6887) for port opening | only UPnP + NAT-PMP (`portMapper`) | **done (2026-10-11)** | M | medium |
| **NAT type / reflected port detection** | `reflected_port`/`nat_mapping` in the `portcheck`, from the BEP 42 `ip` field | **done** | S/M | medium |
| **Graduated file priority** (+ allocate on demand) | only included/excluded (`file_priorities: none`) | **done (2026-10-12)** | L | medium |

- **PCP — done (2026-10-11).** Minimal MAP client in `cmd/gx-torrent/pcp.go`
  (RFC 6887: header, nonce, verification of the result code and the nonce, mapping
  with lifetime 0 to remove): ~120 lines of our own, no dependency. It hooks
  into the `portMapper` as a third method, tried **after UPnP and before NAT-PMP**
  (enabled when NAT-PMP is active: same gateway and no new
  UI setting). Test with a fake PCP server on loopback
  (`TestPCPMapSuccess`, `TestPCPMapNonceMismatch`, `TestPCPMapErrorResult`,
  `TestPCPMapRequestShape`).
- **NAT detection — done (2026-10-11).** The DHT captures the reflected port from the
  `ip` field of BEP 42 (`reflected_port`, the most frequent among the observers;
  `reflected_observations` counts the fields received) and infers the **mapping type**
  (`nat_mapping`) by comparing the port seen by observers in different
  /24 (or /48) subnets: all in agreement = endpoint-independent (*cone*), disagreement =
  address/port-dependent (*symmetric*); with fewer than 2 observers it stays unknown.
  Per-observer set limited; parsing in `parseReflected`/`recordReflected`
  (`internal/dht/dht_sec.go`). The daemon's `portcheck`
  (`GET /api/v1/portcheck`) reports `reflected_port`/`nat_mapping` and `natHint`
  says whether the NAT preserves or rewrites the port.

  **Live measurement (2026-10-11).** On the public DHT `reflected_observations` stays
  **0** after 500+ queries: the `ip` field of BEP 42 — which the spec says *SHOULD* be in
  every response — **does not arrive** from real traffic (the codec is correct,
  `TestReadResponseDecodesIPField`). The function is therefore ready but **not
  observable on this network**: it activates where the nodes include it (typically
  behind NAT). It is the reason `reflected_observations` exists, which says whether
  the datum is there; the consensus classification has been replaced with the
  per-observer one precisely because the consensus at ≥2 never populated. Tests:
  `TestParseReflected`, `TestRecordReflectedNATMapping`, `TestNATHint`,
  `TestReadResponseDecodesIPField`.

  **Sending side — done (2026-10-12).** The code **read** the `ip` field but did not
  **send** it: every response now includes it (`addIPField`, applied to ping,
  find_node, get_peers, announce_peer and the BEP 44 `get`/`put` responses), so
  a peer behind NAT can learn its reflected address from us. Added
  the `reflected_sent` counter (in `Stats()`, therefore `dht_reflected_sent`) to
  make the sending side observable too. Test: `TestRepliesIncludeIPField`.

  **BEP 5 `implied_port` — done (2026-10-12).** `announce_peer` with
  `implied_port: 1` now uses the **source port** of the packet instead of the `port`
  field (which a peer behind NAT often does not know). Before, the field was
  ignored. Test: `TestAnnouncePeerImpliedPort`.
- **Graduated file priority — done (2026-10-12).** Priority model in the engine
  (0 = skip, 1..4 = normal, 5..7 = high, libtorrent/qBittorrent scale), with
  the current bool mapped to 0/4; `Config.FilePriorities` (the old
  `Config.FileSelection` remains as fallback), `piece.Piece.Priority` per piece
  (the maximum among the files it touches) and a new stage **`pickHigh`** in the piece
  picker **right after readahead and before file-edge/allowed-fast/rarest**.
  Changing only the levels applies hot (`Torrent.RefreshSelection`), without
  stopping the torrent; changing which files are downloaded remains stop/relocation/
  start. From the app side: `capabilityLevels` brings `file_priorities` from `none` to
  `full`, the File tab of the daemon and of Gextto offer the scale, the qBittorrent API
  maps 0/1/6/7, i18n in all languages. The original plan (historical):

  The model was only included/excluded. Real contact points (from the code):
  - **selection model**: `Config.FileSelection func(torrentID) []bool`
    (`torrent/config.go`) and `skippedInfoFiles`/`maskFromFlags`
    (`torrent_selection.go`) are a **bool skip** per file; a priority model is needed;
  - **pieces**: `skippedPieces` → `t.pieces[i].Skip`; the picker
    (`internal/piecepicker`) already has graduated stages (**stream** window,
    **readahead**, allowed-fast, rarest) and the file priority would be a new
    stage between readahead and allowed-fast;
  - **daemon**: `file-priorities` (`priorities=4,0,4`) and the File tab; the matrix
    `capabilityLevels` brings `file_priorities` from `none` to `full`;
  - **Gextto**: adapter and File tab (today only included/excluded).

  Recommended slices (**one per commit**, measurement at every step):
  1. internal priority model (0 = skip, 1 = low, 4 = normal, 7 = high, like
     libtorrent) with the current bool mapped to 0/4; test of the mapping and of
     `skippedPieces`;
  2. new "high priority" stage in the `piecepicker` (above rarest, below
     readahead) + picker test;
  3. daemon **and** Gextto API/UI, `capabilityLevels`, translations, manuals;
  4. measurement: throughput and seeding with mixed-priority files (local harness +
     real swarm).

  **"Allocate on demand"** is instead already covered in practice: with `Preallocate`
  off (default) the files are created **sparse** and the OS allocates on write;
  at most an explicit option that declares it is needed.

  **Status:** model, picker, daemon and Gextto **done** (slices 1–3). The **measurement**
  (slice 4: throughput and seeding with mixed priority) is **to do**: the new
  `pickHigh` stage is a predicate on the hot path (no order change
  without priorities), so the measurement serves to confirm the behaviour with mixed
  files, not to justify the existence of the code.

**Recommended order:** first the DHT diagnostics (**done**), then the low-risk
items (**token and step 1 of NAT done**), then the anti-Sybil hardening and
multi-path (with measurement on a real swarm), finally PCP (**done**) and file
priority (**done**).

### 4.13 Review of the BEP work (2026-10-12)

Review of the BEP commits of the previous days; the current state of every BEP
is in the tables of [`docs/bep.md`](bep.md) (a single status list, not a log).
Fixed: BEP 42 `ip` field at the top level (detection of reflected port
and NAT, never active before: from 0 to 89 observations after the restart), invalid
port in `announce_peer`, BEP 44 `put` only to the 8 nearest nodes, addition of
BEP 46 magnet/`xs=` without a global lock, `so=` (infinite loop and unlimited
expansion from a hostile magnet), duplicate `ut_metadata` blocks, interest after
`lt_donthave` (with a guard on the null bitfield), Swarm Merging candidates
without a bitfield. The anti-SSRF protection was also added: web seed and `xs=`/`as=`
sources do not reach private addresses except with `web-sources-allow-private`
(check on the resolved IP, no measurable cost: a DNS resolution already
needed to connect).

### 4.12 Fine quality: what the engine lacks (analysis 2026-10-12)

Analysis of the **craftsmanship** gap (not of the feature list) between gx-core and
libtorrent. The BEP coverage of gx-core is broad; the gap is in the layer that
is not seen in the capability matrix: robustness on real and hostile swarms,
extreme throughput, disk I/O, fine priorities and protocol edge cases.
Sources: [`internal/gxcore/GEXTTO.md`](../internal/gxcore/GEXTTO.md),
[`bep.md`](bep.md), §5–§6 of this document.

| Area | libtorrent | gx-core today | Gap |
| --- | --- | --- | --- |
| Choking / seeding | rate-based choking, anti-leech, peer classes | speed-based + optimistic, deterministic harness (`unchoker/sim_test.go`) | quality algorithms absent; measurements **local only** (§6.6) |
| Pipelining | multi-piece per peer, ring buffer | adaptive queue (50→250), one piece per peer | 10 Gbps given as "to be measured" (`bep.md` §6); **no harness** |
| Disk I/O | mmap, pluggable storage, O_DIRECT/AIO, contiguous writes + hash on a pool | **`O_SYNC` removed, `fdatasync`, mmap opt-in (2026-10-12)**; O_DIRECT/io_uring/explicit contiguous remain absent (analysis: the page cache already coalesces and Go already overlaps I/O) | reduced (§5.4) |
| File priority | graduated 0–7 | **done (0 = skip, 1..4 = normal, 5..7 = high)**; `capabilityLevels.file_priorities = full`; measurement with mixed files to do (§4.11) | closed (residual measurement) |
| DHT | multi-path, active anti-Sybil | BEP 5/32/33/42/43/44/46, dedup; cap and multi-path off | hardening **not active** (§4.10) |
| v2 / merge | full v2 seed, per-block reuse | v2-only download; v2 seed only whole files (`hash reject`); merge phase 1 | v2 seed incomplete, merge phase 2 (§15.2) |
| Adversarial robustness | years of field experience on hostile swarms | snub, session ban with TTL, IP filter | less battle-tested surface |

The **three highest-value gaps** in real operation, in order of priority:

1. **Graduated file priority** — **done (2026-10-12)**: priority model in the
   engine (`piece.Piece.Priority`), `Config.FilePriorities`, `pickHigh` stage in the
   picker, daemon and Gextto at 0/1/4/6/7, `capabilityLevels` at `full` (§4.11). Only
   the **measurement** with mixed-priority files remains.
2. **Disk I/O** — **done (2026-10-12)**: `O_SYNC` removed, `storage.File.Sync()`
   (`fdatasync`), snapshot→fsync→bitfield invariant and `updateStats` in two phases;
   **mmap opt-in** for asynchronous I/O (§5.4). O_DIRECT/io_uring
   and explicit write coalescing remain undone, with no measurement to justify them.
3. **Demonstration on a real swarm and isolation** — **done (2026-10-12)**: gx-core
   on par with or above libtorrent on the same well-seeded public torrent (§6.8);
   **pipelining** is isolated with an in-process latency proxy (§6.4: ~12× with
   the adaptive queue at RTT 50 ms) and **choking** by the deterministic bench
   (`unchoker/sim_test.go`). Only real `tc netem` remains out (root), not
   needed for the isolation.

Out of scope by choice (documented in `bep.md`): BEP 8/17/18/22/26/28/30/34/35/
45/50/51, WebTorrent, native I2P; they remain reachable via a SOCKS5/HTTP proxy.

**Criterion**: these are gaps of **quality**, not of compliance; they are closed with
**measurement** (performance rule of `AGENTS.md`) and with the same discipline as the
other items: one item = one commit, `GEXTTO.md` updated, both UIs, i18n.

---

## 5. gx-torrent: performance optimisations

Ex `docs/gx-torrent-ottimizzazioni-prestazionali-v2.md`. It replaces v1
(removed on 08-10-2026): the diagnoses were largely correct as a reading of the code
but with three faults — it did not distinguish the original code from the changes, it presented
non-reproducible benchmarks (no `Benchmark*` existed in the repo), it ordered
irrelevant or risky interventions by priority. Here **only what is worth
doing**, with how/risks/verification; one item = one task/commit.

### 5.1 Progress state

| # | Item | Status | Commit |
|---|---|---|---|
| 1 | LSD without `d.mu` and without `t.Stats()` | **done** | `d2a4060` |
| 2 | Streaming: targeted `PiecesDone` | **done** | `ff2d4bf` |
| 4 | MSE `StreamWriter` in-place | **done** | `5af2ee0` |
| 5 | `servedRequests` with a limited window | **done** | `a8b3e77` |
| 6 | Piece cache: lazy TTL (conditional sharding, not done) | **done** | `60b090e` |
| 7 | `Bitfield.Count` with `math/bits` | **done** | `8caf6ad` |
| 3 | Removal of `O_SYNC` + durability invariant | **done (2026-10-12)** | — |
| 8 | Optionals (`findLocked` O(1), ETag/304) | **not to do** | — |

Order by **expected value = (impact × certainty) / (effort × risk)**.
Rules per item (from `AGENTS.md`): changes to `internal/gxcore` recorded in
`GEXTTO.md`; one commit per item; targeted tests
(`./cmd/gx-torrent/`, `GxEngine`, `make test-engine`, `make test`); script
invariants; never `bin/`/`gx-torrent.build_number`/`data/`; with visible
behaviour update README/MANUAL/`gx-torrent.md`.

| # | Intervention | Impact | Effort | Risk | Code |
|---|---|---|---|---|---|
| 1 | LSD without `d.mu` / `t.Stats()` | High (daemon latency) | S | S | cmd (daemon) |
| 2 | Streaming: targeted piece check | Medium (GC/coupling) | S | S | daemon+engine |
| 3 | Removal of `O_SYNC` + durability | **Very high** (disk) | M/L | M | engine |
| 4 | In-place MSE | Medium (upload) | S | S | engine |
| 5 | `servedRequests` with a window | Medium (memory) | S/M | M | engine |
| 6 | Lazy-TTL piece cache (+ sharding) | Medium (seed) | M | M | engine |
| 7 | `Bitfield.Count` with `math/bits` | Low | S | S | engine |
| 8 | Optionals: map in `findLocked`, ETag/304 | Low | S | S | cmd (daemon) |

`S` ≈ half a day, `M` ≈ 1–2 days, `L` ≈ more.

### 5.2 Item 1 — LSD without `d.mu`, without querying the run loops (done)

`dueHashes` (`cmd/gx-torrent/lsd.go`) took `d.mu` and, holding it, called
`t.Stats()` on **every** torrent — a blocking query without a timeout to the run loop. With
slow storage (network mount) the global mutex froze all the REST calls.
Rewritten by reading from the lock-free snapshot (`snapshotViews`); announceable
states: `downloading`, `downloading_metadata`, `seeding`,
`checking_files`, `moving`. Difference: a torrent in `error` is no longer
announced (correct). Verification: a test that `paused`/`stalled`/private ones do not
appear, public `downloading` does, and that `dueHashes` does not take `d.mu`.

### 5.3 Item 2 — Streaming: targeted piece check (done)

`piecesPresent` (`stream.go`) called `t.PieceStates()`: it allocated a vector as
long as the torrent and scanned all the pieces on the run loop every 200 ms to
check 1–2 of them. Added the targeted `Torrent.PiecesDone(begin, end)`
(`session_torrent.go`), which scans only the interval (with `len==0 → false`,
as before). `PieceStates` remains for the piece map of the API/UI. Follow-up not
done: event notification instead of polling (not necessary as long as it does not allocate).

### 5.4 Item 3 — Removal of `O_SYNC` + durability invariant (done 2026-10-12)

`filestorage.go` opened every file with `O_RDWR|O_SYNC`: every `write(2)` went to
stable storage — on HDD/network the throughput is limited by the IOPS.

**Done.** `O_SYNC` is removed (`filestorage.go`); `storage.File` now has `Sync()`
(no-op for `PaddingFile`, the `*os.File` of filestorage already implements it). The
durability is **batched**, with the invariant *a persisted bit refers to data
already fsynced*:

- `torrent.syncPieces()` flushes every open file, on the torrent's **run loop**
  (`t.files` is owned by it);
- `writeBitfield()` orders **bitfield snapshot → `syncPieces` → write**,
  so a bit never precedes data not yet on disk (on a crash at most
  `ResumeWriteInterval` are re-downloaded, never corruption);
- `session_stats.updateStats` is in **two phases**: it samples the bitfield and fsyncs on every
  run loop (no Bolt transaction open during the fsyncs), then writes everything in
  a single durable transaction. The bucket of a torrent removed in the meantime
  is skipped.

Tests: `filestorage/sync_test.go` (write + `Sync` + reopen),
`storage/padding_test.go`, `torrent/torrent_sync_test.go` (`syncPieces` on every
file). The end-to-end path (completion, stop, resume after restart) is
covered by the daemon tests. Historical design: extend the interface with
`Sync()`, remove `O_SYNC`, a testable `syncPieces` helper, the critical order in
`writeBitfield` and the two-phase restructuring of `session_stats.updateStats`
(no Bolt transaction open during the fsyncs) — all applied. Measurement
see §5.10: on this host the estimated gain was ~1.2x–1.8x on NFS (not the
"+300–800%" of v1).

**Follow-up — mmap, `fdatasync`, AIO (2026-10-12).**
- **`fdatasync`** instead of `fsync` for the piece data (`osFile.Sync` on
  Linux): there is no need to flush the metadata, the file is already sized.
- **mmap opt-in** (`-mmap`, `Config.Mmap`): `filestorage` serves the files from a
  shared mapping; `WriteAt` is a copy into the page cache (asynchronous writeback,
  no `pwrite`), `ReadAt` a copy from the page cache, `Sync` an `msync`.
  Benchmark on tmpfs: **~7.0 GB/s** against **~5.9 GB/s** for `pwrite`
  (`BenchmarkStorageWrite{,Mmap}`). Caveat: it maps every open file, so it is
  not advised with torrents of thousands of files (`vm.max_map_count`); outside
  Linux it is ignored.
- **AIO (io_uring): deliberately not added.** The engine writes the pieces from
  goroutines (`piecewriter` pipeline + `fadvise`/`fallocate`) and with mmap the
  write is already asynchronous via the kernel writeback; Go does not expose io_uring
  without a dependency or cgo. Without a measurement showing a real gain,
  introducing io_uring would violate the "no speculative code" rule and
  risk regressions. The "AIO" item of §4.12 is therefore covered by the
  existing asynchronous path + mmap, not by io_uring.
- **Contiguous writes:** every piece is already written with a single `WriteAt`
  (up to 2 MiB contiguous) and the page cache merges adjacent writebacks; an
  explicit coalescing buffer showed no need, so it was not
  added (no before/after measurement in favour).

### 5.5 Item 4 — MSE `StreamWriter` in-place (done)

`cipher.StreamWriter` did a `make` for every encrypted block (tens of MiB/s of
garbage in upload). Replaced with an in-place writer (`XORKeyStream(p, p)`) in
`internal/mse/mse.go`: the callers do not reuse the buffer after the write.
Verification: `mse_test.go` with `AllocsPerRun == 0` + round-trip; package added
to `make test-engine`.

### 5.6 Item 5 — `servedRequests` with a limited window (done)

A map that grew indefinitely with the served blocks (memory ∝ GB uploaded on
long-lived seeds) and refused forever even legitimate re-requests. Replaced with a
map+FIFO window of N≈1024 (`peerwriter.go`): immediate anti-abuse dedup,
limited memory. Tests: dedup within the window, eviction beyond, no growth.

### 5.7 Item 6 — Piece cache: lazy TTL (done; sharding only if needed)

The cache serves **only** the upload (`cachedpiece` ← `RequestMessage`), not
download or local streaming (`stream.go` uses `os.Open`/`ReadAt`). Real costs:
a `time.Timer` per piece with `Reset` on every access + an exclusive lock even on
hit. Done: lazy TTL (`expireAt` + sweep ticker ~30 s, `Close()` stops it).
Not done: sharding into 16 partitions — only if profiling shows contention under
heavy seeding (a broad refactor of engine files, not "by guesswork"). Verification: sweep,
renewal on hit, eviction unchanged; `internal/piececache` in `make test-engine`.

### 5.8 Item 7 — `Bitfield.Count` with `math/bits` (done)

Byte-by-byte sum over a 256-byte table → `bits.OnesCount64/8` in blocks of 8,
table removed. A micro-optimisation not on a critical path (`Count` used in
`torrent_stats.go` and `All()`), but trivial and safe. Property test vs table.

### 5.9 Item 8 — Optionals (not to do)

- **`findLocked` O(1)**: `ListTorrents()` + scan for every lookup by hash.
  With 4 torrents the `/api/v1/torrents` response is 3,822 bytes in 0.5–0.7 ms:
  nil gain (<0.05% CPU). Measurable only at hundreds/thousands of torrents
  (~1 MB of list): postponed to that load — **then done** on 2026-10-11 with a
  hash→torrent index and self-repairing fallback (see §5.13).
- **ETag/304 on `/api/v1/torrents`**: `handleList` already serves the precompiled
  snapshot, cost = only `json.Marshal`; during activity the rates change at
  every tick and the 304 would not trigger. Only with a measurement that justifies it.

### 5.10 Excluded, with justification (measured)

- **Rarity buckets / picker refactor**: real cost at 27,250 pieces ~0.11
  ms/pick in normal regime (adaptive pdqsort), <1% of a core at tens of
  picks/s. Saving ~0.1 ms/pick for a heavy refactor (`HandleHave`/`Cancel`/
  `removeHaving`, endgame): not worth it.
- **Watermark cursor in `pickSequential`/`pickFileEdge`**: ~0.10 ms/pick and
  ~0.04 ms for the full scan of `pickFileEdge`: not worth the risk.
- **Bitmask in `PieceDownloader`**: maps only for concurrent downloads (≈ no.
  peers); a 64-bit bitmask does not cover 2–4 MiB pieces: negligible.
- **`clear(b.Data)` in `bufferpool`**: ~0.8 µs per 16 KiB → <0.1% of a core at
  1000 blocks/s; shared pool, it is not proven that all paths fill
  the buffer: only with a full audit.
- **`FADV_RANDOM`**: the v1 justification (it penalises streaming) does not hold —
  `stream.go` reads outside the engine storage. At most `FADV_SEQUENTIAL` under
  verification.
- **`saveLocked` outside `d.mu`**: not on polling (only mutations and
  tick-when-dirty ~1/min); `state.json` 2.5 KB, ~0.1–0.3 ms. And the v1 proposal
  zeroed `dirty` before the write, losing it on error.
- **`/proc/meminfo` under `d.mu`**: tens of µs, irrelevant.
- **`HandleDisconnect` O(N)**: few disconnections/s, tens of µs: irrelevant.

### 5.11 Substantial corrections to v1 (reference)

- 10 items out of ~16 were **unchanged code**, not gextto work.
- `O_SYNC` is not a quick win; `storage.File` does not have `Sync()`.
- "Bitfield saved on every piece" is false (only completion/stop/verification/
  `ResumeWriteInterval`).
- The multipliers "15,500x/65,000x/600,000x" came from heterogeneous microbenchmarks,
  not end-to-end; no `Benchmark*` existed in the repo.
- Piece cache on the **upload** path, not download/streaming.
- `handleList` already serves a snapshot: it does not "clone the torrents".

### 5.12 Measurements before implementation (Intel N97)

**Item 3 — `O_SYNC`**: download dir on **NFS4** (1 GbE), alternative ext4/NVMe.
N MiB in blocks of B, `O_SYNC` per write vs buffered + final fsync:

NFS: 16 KiB 35.1 → 81.3 MiB/s (**2.3x**); 64 KiB 60.2 → 89.6 (1.5x);
256 KiB 67.4 → 108.7 (1.6x); 1 MiB 86.8 → 102.4 (1.2x).
NVMe: 16 KiB 7.3 → 862.7 (~118x); 256 KiB 150.5 → 975.5 (6.5x);
1 MiB 371.8 → 962.5 (2.6x).
**Forecast** (pieces 32 KiB–2 MiB on NFS): **~1.2x–1.8x**, link ceiling
~100–110 MiB/s. Greater gain with small pieces and fast disk. Seeding on
read is not touched.

**Picker** (`picker_bench_test.go`, 27,250 pieces): `pickRarest` 110 µs steady /
1.11 ms worst (reshuffled at every pick, shuffle included); `pickSequential`
~100 µs; `pickFileEdge` full scan 38 µs. v1 measured the reshuffled case
and passed it off as typical.

**`saveLocked`**: 2.5 KB, ~0.1–0.3 ms per explicit user operation.
Conclusion: beyond the done items (1, 2, 4, 5, 6, 7) **nothing else of v1
is worth it** at this load.

### 5.13 Check 2026-10-11 (`controllo2`): items applied

External review (`/home/andres/controllo2.md`) verified against the code and applied.
No change alters the capability matrix or the UI settings index; for
the engine items the `internal/gxcore/...` suite remains green and no
regression is observed (the changes are micro-optimisations or aggregate I/O, not algorithms
on the hot path).

Engine (`internal/gxcore`):

- **O-04** `buildV2BlockTreeData`: the capacity of `leaves` was the number of
  *pieces*, but the leaves at `base=0` are *blocks* of 16 KiB: preallocated
  `ceil(f.Length/BlockSize)` to avoid dozens of reallocations on large files.
- **M-14** same function: check of `t.closeC` in the read loop, so a
  torrent removed halfway through building the tree (reading GBs) does not continue
  reading from disk.
- **C-07** `handlePeerSnubbed`: added `resetRequestQueue`, consistent with the
  comment of the function ("after a choke (or a stall)"); before, the adaptive queue
  stayed deep after a stall. Test `TestSnubResetsRequestQueue`.

Daemon (`cmd/gx-torrent`) and DHT:

- **C-02** streaming: `waitForPieces`/`streamRange` accept the `context` of the
  request, so a seek/close of the player does not leave the goroutine polling
  for 2 minutes with the file open.
- **O-01** `trusted-proxies`/`allowed-hosts`: parsing cache (key = raw
  string), zeroes the per-request allocations; a changed value is seen immediately.
- **O-02** feed: `markFeedSeen`/`markSmartEpisode` no longer call `saveLocked`
  per item; a single atomic write at the end of `pollFeeds` (before, up to 2N
  rewrites of `state.json` per cycle).
- **O-03** DHT BEP 42: CRC32C Castagnoli table as a package variable.
- **O-05** `findLocked`: hash/ID index with a self-repairing fallback, rebuilt
  when the session is reopened (§5.9).
- **C-01** login rate limit: `loginFails` bounded and pruned (cap 2000, eviction of the
  oldest) against unlimited growth.
- **C-05** BEP 44 `itemStore`: cap `bep44MaxItems` + prune of the expired ones and
  eviction of the oldest in `put`.
- **C-06** BEP 33: empty Bloom filter estimates 0 (before, the clamp gave ~0.5 → 1).
- **C-03** Windows: a UNC path is network by construction, without depending on
  `GetDriveType` (offline share → `DRIVE_NO_ROOT_DIR` → junction on UNC).
- **C-04** `qbitAuth` delegates to `standaloneAPIAllowed`: LAN bypass and the
  `gx_session` cookie also apply to `/api/v2` as for the UI and `/api/v1`.
- **M-09/M-10/M-11/M-13** qBittorrent parity: `content_path` in `torrents/info`,
  incremental `rid` in `sync/maindata`, `autoTMM` priority on add, boolean
  flags via `formBool`; concurrent Torznab searches.

### 5.14 Postponed (from the `controllo2` check)

- **qBittorrent `renameFile`/`renameFolder`**: it requires rename support in the
  engine (rename on disk + metadata), a feature of its own, not in this round.

### 5.15 Review `analisi4.md` (2026-10-11): items verified and applied

External review (`/home/andres/analisi4.md`) on the standalone installer and the RSS
manager, verified against the code before applying. **Applied** the three RSS items
of real impact, in the correct form (the document's proposals were partly
ineffective):

- **B.1 `ignore_days`**: it was a filter on the *publication date* relative to
  the last match, so an episode released shortly after the previous one was no longer
  picked up. Now it is a **wall-clock cooldown** (`internal/rss/rules.go`,
  `ruleWithinIgnoreWindow`), and `markRuleMatch` records the instant of the match
  (`cmd/gx-torrent/feed.go`), not the `pubDate`. UI label updated
  (*Wait between matches*). Tests `TestEvaluateIgnoreDaysWindow`,
  `TestPollFeedsIgnoreDaysWindow`.
- **B.2 "download once" movie**: the *Follow a title* function of the time (today
  replaced by the **Wanted** list, §5.16) turned the rule into `once`
  (`rss.Action.Once`): it takes one release and disables the rule, even with
  multiple results in the same cycle (`disableRules`, `onceHit` in `pollFeeds`).
  Test `TestPollFeedsOnceDisablesRule`.
- **B.3 title in the criteria**: the document's proposal (adding the title to
  `Match.Include`) **does not work**: `Include` is an OR, so it widens the matches.
  Instead a **tolerant regex** requiring the words of the title in
  order was applied (`titleMatchRegex` — then `followTitleRegex` —, e.g.
  `(?i)mamma[\s._\-]+mia`). Test `TestTitleMatchRegex`.

- **B.4 scoped feeds**: added `rss.FeedCoveredByRules`; the fallback to the local
  filters of the feed applies **only if no rule covers that feed** (the document's
  proposal applied it even when a rule covered it, bypassing it).
  Test `TestScopedRulesDoNotDisableOtherFeeds`.
- **B.5 REPACK+PROPER on the first download**: `smartEpisodeVerdict` also records the
  lower tokens. Test `TestSmartEpisodeFirstRepackAndProper`.
- **B.6 episode filter**: `ParseEpisodeFilter` accepts `S01E01-E05` and just the
  season `S01`/`s1`.
- **B.7 `/ui/rss-suggest` log**: `searchFeedSuggest` does not log the errors for
  indexers while typing.
- **A.1–A.4 installer**: `install-service.sh`/`uninstall-service.sh` in the
  Linux package (`package-gx-torrent.sh`) with `StateDirectory=gx-torrent`
  in the unit, the wait for the stop in `uninstall-service.ps1` and the two
  packaging scripts in `installer-selftest.sh`.

With this the `analisi4.md` review is **fully applied** (A.1–A.4,
B.1–B.7).

### 5.16 Wanted (`wanted`) in the standalone daemon (2026-10-11)

At the owner's request, the `/ui/rss` page of the standalone daemon changes
model: from "feed + rules" to **Wanted** (titles that are wanted). Each title
has type (series/movie), seasons, language, quality, exclusions, episode filter,
category and path; it is added by hand or with **Search the feeds**, which queries
the configured indexers and RSS feeds and proposes the names found (filtered by
language/quality). The **title is required** as a regex tolerant to separators and
language/quality are in **AND** (the `Include` of the rule engine is an OR, it was not
suitable).

The engine (`cmd/gx-torrent/wanted.go`) at every cycle searches each title in the
indexers and in the RSS feeds (`applyWanted` on the feed items, search by title
on the indexers) and takes the episodes that appear; a **register** in
`state.json` (`wanted_seen` / `wanted_taken`) says what has appeared and what has been
downloaded, the table shows the **missing** ones and a **gap search** (query
`Title SxxEyy`, throttled to 30 min, max 3 per cycle) searches for them until it
finds them: it is the "gx-core knows what is missing and downloads it" requested. For a movie it takes one
release and does not look for others. The list does not generate feeds/rules, so the feeds
remain free for the advanced rules. UI in `ui_wanted.go` + section in
`rss.html`; tests in `wanted_test.go`.

**Subsequent corrections (review 2026-10-11).** The engine was invoked only
at the end of `pollFeeds`, which exited immediately when there were no feeds: a
configuration with only indexers and wanted titles was never served. Now
`pollFeeds` no longer exits early and `pollWanted` also runs without feeds. Removed the
residues of the old *Follow a title* function (endpoint `POST /ui/rss-follow`,
i18n keys, tests), the inert `Folder` field of the wanted entries and the comments that
described the old model "one title = feed + rule"; `followTitleRegex`
became `titleMatchRegex`.

### 5.17 Complete standalone RSS, feed archive, IP filter and optimisation (2026-10-12)

Review of the standalone gx-core at the owner's request, with the goal
of an RSS panel on a par with qBittorrent and Deluge/YaRSS2.

- **HTML listings as feeds** (`internal/rss/listing.go`): the pages of ext.to and
  TorrentGalaxy gave `XML syntax error … <meta> closed by </head>`. Now: direct
  magnets, `/post-detail/` pages resolved (3 workers, cache), advertised feeds
  followed, a clear error otherwise. Measured on the two real URLs: 50 elements in
  0.3 s (ext.to) and 50 in 1.9 s (TorrentGalaxy, first time; then from the cache).
- **RSS panel:** per-feed state (last check, elements, new, added,
  error), ↻ synchronous reload, edit, enable/disable (`disabled`),
  configurable interval and next check; rules editable, enable/
  disable, `once` in the form and **preview** on the archive; link to the web page
  of the indexer.
- **Bug of the disabled rules:** a disabled rule no longer covered the
  feed, which fell back on its own filter (empty = takes everything): a *once* rule
  that turned itself off made the next round download the other releases
  of the same movie. The test `TestPollFeedsOnceDisablesRule` hid it
  because it counted the asynchronous snapshot; now it counts the state and the coverage applies
  also to disabled rules.
- **Feed archive** (`feed_archive.go`, `ui_archive.go`, `archive.html`):
  bbolt (already linked), a compact index separate from the JSON. Benchmark
  `BenchmarkFeedArchiveSearch` (100,000 elements, Intel N97):

  | Version | ns/op | B/op | allocs/op |
  |---|---:|---:|---:|
  | first draft (JSON decoding of every element) | 362,613,936 | 126,176,356 | 1,300,727 |
  | compact index, JSON only for the page | 7,429,315 | 44,643 | 337 |

  49× faster, 2,800× less memory allocated.
- **IP filter** (`ipfilter_refresh.go`): a URL in `settings.json` was not
  loaded (the session did `os.Stat` on the URL). Now: local copy immediately,
  download at startup and every `ipfilter-refresh-hours`, local file reloaded if
  it changes; state and *Load now* in the settings.
- **Optimisation** (`optimize.go`): queue (Gextto base), cache (automatic),
  bandwidth (80% of the peak upload measured in the `tick`, auto-speed active).
- **Torrents UI:** cache/I/O tiles hidden in standalone, peer port dot.

**Daemon footprint** (build `-s -w`, standalone, no torrents, DHT/UPnP/
NAT-PMP/LSD off, measured after the first feed round):

| | before (9ac13b4) | after |
|---|---:|---:|
| binary | 14,774,432 B | 15,093,920 B (+2.2%: pages, translations, handlers) |
| idle VmRSS | 19,048 kB | 19,324 kB (+1.4%) |
| idle CPU | 0.0% | 0.0% |

---

## 6. gx-torrent: choking and seeding measurements

Ex `docs/gx-torrent-misure-seeding.md`. Support for item §4.3 #13. Underlying
rule: **measure first**, then decide whether to touch the engine core.

### 6.1 Constraint: do not force the automation

The measurements reflect the system **as it really runs** (queue/slots, seed policy,
scheduler on): a deterministic harness on the pure algorithm (no session);
local swarm with the daemon and automanagement active; real campaign with Gextto
(`AdjustQueue`, seed policy, limits). Never "with queues off": that would be a laboratory,
not production.

### 6.2 Metrics

Choking (upload slots): who is unchoked and for how long, fairness at equal
speed, optimistic unchoke rotation, behaviour in download (rewards fast
downloaders) and in seed (rewards fast uploaders).
Seeding (end-to-end): bytes uploaded by the seed vs corpus (`Nx corpus`),
up/down throughput, completion time, ratio and time to 1:1 in a real campaign.

### 6.3 Bench 1 — deterministic harness of the unchoker

`internal/gxcore/internal/unchoker/sim_test.go` (in `make test-engine`): it drives
the real `Unchoker` with synthetic peers and fixes the properties (fast downloads in
download, fast uploads in seed, immediate `FastUnchoke`, optimistic that does not
steal regular slots and rotates, budget respected, fairness) + `BenchmarkTickUnchokeSeeding`.
Usage: `go test …/internal/unchoker -v` and `-bench TickUnchoke`. A safety
net for future variants of the algorithm.

### 6.4 Bench 2 — local swarm (opt-in)

`cmd/gx-torrent/measure_test.go`, outside `make test`, with `GX_MEASURE=1`:

```
make measure-seeding                        # seeder + 3 leechers
GX_MEASURE_SUPERSEED=1 make measure-seeding # super-seeding comparison
GX_MEASURE_MESH=1 make measure-seeding      # leechers connected to each other
GX_MEASURE_LEECHERS=2 make measure-seeding  # swarm of 2 peers
```

**Pipelining.** `cmd/gx-torrent/measure_pipeline_test.go` is the harness dedicated
to the **adaptive request queue** (opt-in, `make measure-pipeline`): it starts the same
local swarm at different depths (`shallow` def=4/max=4, `default` 50/250,
`deep` 200/500) and reports bytes and time. The knobs travel in `NetworkOptions`
(`MaxRequestsOut`/`DefaultRequestsOut`). **Declared limitation:** on loopback the RTT
is ~0, so the depth is not the bottleneck and the profiles give almost identical
numbers (~200 ms, ~40 MB/s with 1 leecher).

**Isolation with latency (done, 2026-10-12).**
`TestMeasurePipeliningLatency` puts an **in-process latency proxy** in front of the
seed (no `tc`/`netem`, no root): every chunk is released at
arrival+latency, in order and **without limiting the bandwidth**, so RTT is added
without throttling the link. With RTT 50 ms, 256 KiB pieces (16 blocks) and a corpus
of 4 MiB, a single leecher:

| Profile | blocks in flight | time | average |
| --- | --- | --- | --- |
| `shallow` def=1/max=1 | 1 | 13.4 s | 305 KiB/s |
| `default` def=50/max=250 | ≤50 | 1.1 s | **3.8 MiB/s** |

~12× with the adaptive queue: it is the isolation that loopback (RTT ~0) cannot give.
**Choking** is isolated by the deterministic bench (`internal/unchoker/sim_test.go`,
in `make test-engine`), which drives the real `Unchoker` with synthetic peers and fixes
fairness, slots and optimistic rotation; the end-to-end seed is in §6.4/§6.6.

Seeder + N leechers on loopback with distinct `127.0.0.x` IPs (the engine refuses two
connections from the same IP). Default: every leecher only towards the seeder — the
**seed upload** is measured (what super-seeding changes). Experimental mesh
on one host (multiple connections on loopback unstable: peers that
do not connect/do not complete): only an indicative snapshot, few peers,
repeat. Output: `uploaded=Nx corpus`, averages, times. Limits: absolute numbers
dependent on the machine (not comparable between different runs); one peer may
take two retry ticks (10 s) in super-seeding: the harness reports instead
of failing on a slow peer.

### 6.5 Bench 3 — campaign on a real swarm

For gx-torrent vs **libtorrent/qBittorrent**: same swarm and role, same
policies (slots, bandwidth, no. peers), same metrics (seed bytes `Nx corpus`,
throughput, time to 1:1, seeds generated), median over several runs, automation active.
The only bench that shows the benefit of super-seeding (~1x instead of Nx) and the
quality of choking with real exchange. First run (real download, gx-core vs
libtorrent) in **§6.8**.

### 6.6 Results (first round, local; Intel N97, corpus 2 MiB)

Deterministic choking: all properties pass; tick with 50 peers ~1.1 µs,
896 B/op, 1 alloc — negligible.
Seeding without mesh (seed → 3 leechers, 5 repetitions): normal ~41–42 ms, seed
**3.00x**; super-seeding ~51–52 ms (**~24% slower**), seed **3.00x**.
Mesh with 2 peers (indicative): both **1.00x** (the leechers exchange the pieces).
Mesh with 3+ peers on one host does not converge.

Honest reading: the engine's choking is **correct and negligible as a cost**;
on small/fast swarms super-seeding **does not reduce** the upload (the exchange
is already enough) and costs throughput/variability. The BEP 16 advantage is for the *initial
seeding* of large torrents with many slow peers, not reproducible on one host.
Consequence: super-seeding **opt-in**, not default; scaling via a real campaign.
Hardening applied: an interested but stalled peer (zero speed) receives the
next piece at the tick instead of waiting for `not-interested` alone.

### 6.7 What to decide based on the results

- Choking worse than libtorrent on a real swarm → act on
  `internal/unchoker` with the deterministic bench as a net and the swarm as verification.
- Super-seeding slower locally but lighter on a real swarm → it remains
  an *initial seeding* option (BEP 16), not default.
- No measurable difference → the core is not touched (rebase cost not
  justified).

### 6.8 Campaign on a real swarm — first run (2026-10-12)

Requirement §4.12 point 3. Same well-seeded public torrent (**Ubuntu 24.04.3
desktop amd64**, ~6.0 GB), same machine, same 40 s window (after 5 s of
ramp-up post-metadata), three public UDP trackers in the magnet, **sequential** runs
(never in parallel). Two different systems, 2 runs each: **libtorrent 2.0.11**
(`python3-libtorrent`) and **gx-core** (`gx-torrent -mode standalone`).

| Client | run | metadata | peers (window) | window | downloaded | average |
| --- | --- | --- | --- | --- | --- | --- |
| libtorrent 2.0.11 | 1 | 3 s | 83 (81 seed) | 40 s | 292.9 MB | **7,149 KiB/s** |
| libtorrent 2.0.11 | 2 | 3 s | 66 (61 seed) | 40 s | 258.8 MB | **6,317 KiB/s** |
| gx-core | 1 | 3 s | 80 (77 seed) | 40 s | 385.1 MB | **9,401 KiB/s** |
| gx-core | 2 | 3 s | 81 (79 seed) | 40 s | 381.2 MB | **9,305 KiB/s** |

Honest reading: on this swarm gx-core is **on par with or above** libtorrent
(median ~9.3 against ~6.7 MiB/s), with immediate metadata and a comparable
number of peers. It is **not** an isolated demonstration of choking or pipelining (the
choice of peers differs, a single machine, two runs): it is the proof that gx-core
really downloads from a real swarm and that choking and the request queue are not a
bottleneck compared with libtorrent at this load. The isolation of the two
algorithms is in §6.4 (in-process latency proxy for pipelining, deterministic
bench for choking); real `tc netem` is not needed.

**DHT cold start — improved (2026-10-12).** The same torrent **without
trackers** (DHT only from the magnet) was resolved after **~75 s**: the cause was
`DHTMinAnnounceInterval` at **1 minute**, used as the retry interval
while the torrent looks for peers (the announce starts immediately, then waits this
interval). Brought to **15 s**. The same test now obtains the metadata in
**~49 s** with 12 peers (before ~75 s, 2 peers). The remaining time is the filling
of the DHT routing table (~30 s cold), not the lookup: the `internal/dht` harness
finds ~108 peers for the same infohash in ~30 s. The cold DHT is
therefore not broken, it was **slow to retry**.

---

## 7. Holepunching: test harness

Ex `docs/holepunch-test-harness.md`. Support for
`scripts/holepunch-netns-test.sh` (engine behaviour in `docs/gx-torrent.md`).

**Purpose**: verify end-to-end that gx-torrent opens a hole (BEP 55,
`ut_holepunch`) between two clients behind NAT/firewall using a public peer as a
**relay**. Root-only, all inside dedicated network namespaces: the host network is
not touched.

**Topology**: `gx-lab` (public: tracker `10.0.0.1:13800`, seed, relay
`10.1.0.1:51410`) + two stateful firewalls `gx-r1`/`gx-r2` (they let outbound through,
DROP new inbound connections, accept `ESTABLISHED,RELATED`,
refuse TCP towards the peer ports so the hole opens over uTP) + clients
`gx-a 10.10.0.2` (leecher) and `gx-b 10.20.0.2` (seed).

**Sequence**: minimal Python tracker (interval 30 s) → seed with data → B enters as
leecher, downloads, completes while staying connected to the relay (interested leecher) →
only then A enters: it does not reach B, asks the relay for an introduction, `connect` to
both, cross dial over uTP. Success: a peer with origin **`holepunch`**
(the one that dials) or `incoming` (the other side) in
`/api/v1/torrents/<hash>/peers`.

**Choices**: stateful firewall instead of NAT (conntrack does not emulate a cone
port-translating NAT: the direct dial creates a local entry that conflicts with the
SNAT; the stateful one reproduces the same observable and makes the test
deterministic); seed and relay on different IPs (the engine deduplicates peers by IP);
`-outgoing-interface` = listening IP (otherwise announce with the wrong IP);
interested leecher relay (a completed torrent does not dial and discards the
non-interested ones); tracker with a long interval (a re-announce would attribute
the connection to the tracker dial instead of the holepunch); success detected on
both sides (race of the dial).

**Historical bugs of the script**: a single shared NAT (hole impossible);
`/peers` without `source` (a field later added to the API); wrong roles (relay
seed + B seed); `progress` 0–100 read as 0–1; `grep` on binary logs (`-a` is
needed).

**Usage**: `make gx-torrent`, then `sudo scripts/holepunch-netns-test.sh`
(`--keep` for debugging, `--size 16` for a larger torrent). A successful example in
`scripts/risultato.txt`. Debug: `HOLEPUNCH_DEADLINE` (default 90 s),
`HOLEPUNCH_B_DEADLINE` (60 s), `HOLEPUNCH_DIAG=1` (conntrack dump). On
failure it prints the `holepunch` lines of the logs (rendezvous/connect).

**Limits**: emulation with a stateful firewall, not a real cone NAT; outcome
`holepunch`/`incoming` according to who wins the race; IP dedup of peers by the
engine (in a real swarm the peers are on different IPs).

---

## 8. Technical review 1: client, i18n, jobs, concurrency

Ex `docs/revisione-1.md` (already local/untracked; cited by comments in
`jobs.go`, `web.go`, `internal_translations.yml`). Anomalies and debt that emerged from a
review of the code, with incremental corrections without changing the
stabilised behaviour. Scope: UI JS client, i18n, dynamic HTML,
asynchronous activities/jobs, concurrency and tests. Out of scope: authentication and
LAN security (LAN trusted by decision).

At the time of the review: `go test ./...` and `go vet` green; a single UI client
IIFE file `uiweb/static/gextto-ui.js` (~253 KiB, 5,194 lines); CSS in
`gextto-ui.css`; server-rendered templates; polling + partial HTML + dynamic
dialogs. No blocking errors: maintenance, observability,
consistency and future robustness concerns. Effective i18n model: keys = **Italian
source strings**, catalogues `internal_translations*.yml`
(Italian → language) merged in SQLite at startup; client with `translateUINode()` +
`MutationObserver` and English fallback.

Priority: **P1** before adding functions (difficult regressions/diagnosis);
**P2** debt compatible with operation; **P3** quality/maintenance.

### 8.1 Monolithic JavaScript client (P2)

A single IIFE with bootstrap, theme, i18n, polling/API, dashboard, torrents, series/
movies/archive/comics, configuration, maintenance, logs, dialogs/tables/
accessibility. On disk it is large but compressed it is small: the problem is
maintenance (shared global state, future collisions, init mixed with
common code, render/network/event logic in the same block, tests only on
HTTP output). Proposal: modules (`ui-core.js` with a stable contract
`api`/`pollRequest`/`esc`/`storage`/`notify`/dialogs, then dialogs, shell, log/
maintenance, torrents, library/discovery/comics), migration by extractions
without a big bang (first native ES, then an eventual bundle in the build). Acceptance:
no regressions, no-JS with essential content, no access to other people's
private variables, unchanged compressed weight, focus/keyboard/dialogs unchanged.

### 8.2 Internationalisation (P1 catalogue check, P2 migration)

Risks of the string model: a spelling variation breaks the translation,
dynamic strings with regex partly remain in Italian, server/client catalogues
divergent, `MutationObserver` expensive on dynamic pages,
fallback not controllable. Target: stable keys (`dashboard.title`),
versioned catalogues, deterministic fallback (active → base → key),
parametric interpolation, server/client helpers, no global translation of the
DOM. Gradual migration: catalogue check → keys for new functions →
shell/dialogs/toast → pages one at a time → `translateUINode` legacy only →
removal of the observer. Tests: every key in the base catalogue, no empty
translation, multilingual snapshot, same language for text/title/aria/placeholder,
plurals and HTML in the values.

### 8.3 Dynamic HTML and rendering safety (P1 new code, P2 historical)

`innerHTML` everywhere (tables, toasts, dialogs, TMDB/release/comics, server
partials): `esc()` used in many places but every interpolation must remember it
in the right context (text vs attribute vs `data-*` JSON, provider errors).
New code: `textContent`, validated `setAttribute`, `createElement`,
`DocumentFragment`, `innerHTML` only for static markup, never values from
API/DB/provider/filesystem in HTML strings. Historical: centralised `esc()`,
explicit `htmlTrusted()` for static fragments, exposed tables migrated first
(release, provider, blocklist, searches), tests with hostile payloads
(`<script>`, quotes, `&`, newline, special URLs). Acceptance: hostile values
rendered as text, trusted functions recognisable by name, no drop in
sort/focus/pagination/accessibility.

### 8.4 Job manager (P1 destructive/long operations, P2 others)

Non-uniform model: 202 before the outcome, single global state, double starts,
late errors only in the logs, non-uniform cancellation, retry/idempotency not
represented, uncorrelated logs, tests with sleep/polling. Proposed: `Job`
(`queued/running/succeeded/failed/canceled`, progress, message/error,
structured **result**, timestamp, cancel via context) + manager (limited
concurrency, dedup by key, retention, log with `job_id`, ordered shutdown) +
endpoints (`GET /api/jobs`, `GET /api/jobs/{id}`, `POST …/cancel`; no generic
`POST /api/jobs`: jobs are born from the real validated operations). First to
migrate: bulk rename/repair, archive/library scans, MediaInfo backfill,
backup/restore, DB/scoring maintenance, source diagnostics,
cleanups. Acceptance: no double jobs, observable state, late errors
in state+log, cancellation without orphans, refresh that preserves the state, no
unlimited goroutines.

### 8.5 Concurrency and races (P1 race in CI, P2 cleanup)

`go vet` is not a race detector. Areas: retry/post-seed maps, config cache,
torrent state between polling/callbacks/shutdown, maintenance progress, job/notification
registries, counters/metrics, libtorrent session closure, state/DB
during backup/restore. Strategy: a single mechanism per state (mutex, atomic,
immutable snapshots, channels, context, maps always with a documented lock). CI:
`go test -race ./...` (if heavy: fast+vet per push, full per PR, race
in a dedicated/daily queue, real engines in separate jobs). Tests with timeout,
`Cleanup`, tmpdir and random ports, never dependent on the running service.

### 8.6 Recommended plan (Phase 0–4)

0 Baseline (behaviour tests, metrics, log/ID convention, catalogues +
non-blocking race). 1 Low risk (`ui-core.js`/dialogs/shell, DOM helpers,
XSS tests, new-string keys). 2 UI consistency (catalogues, observer legacy only,
table/log/maintenance modules, multilingual tests). 3 Jobs (manager,
rename/scans, cancel/dedup/retention, `job_id` in UI/log/notifications).
4 Concurrency (repeated races, maps/snapshots, shutdown/restart, blocking races).
Expected: client modifiable without cross regressions, translations verifiable
pre-release, data/markup boundaries, operations observable/cancellable/correlated,
confidence in concurrency, unchanged consumption. **Do not rewrite everything**: small
extractions, tests green at every phase, refactoring separate from the features.

### 8.7 Implementation state

Phase 0: non-blocking race (`make test-race`, CI job `continue-on-error`) and
catalogue check (`i18n_catalog_test.go`: valid YAML, no duplicates/
empties, consistent sets) done; loading metrics and `job_id` convention to
do. Phase 1: end-to-end XSS tests (`security.spec.ts`: injection, attribute/
tag breakout, `javascript:`) and uniform `esc()` (also `'`) done; extraction of
modules, DOM helpers and new-string keys postponed/repeated as a convention.
Phase 2: English catalogue completed (1,345 missing translations, now 3,130 keys
like the others; a test enforces coverage) and end-to-end multilingual tests
(`i18n.spec.ts`) done; observer and client fallback unchanged. Phase 3:
`JobManager` + complete tests (lifecycle, dedup, cancel, concurrency, retention,
panic→failed, closure) + read-only endpoints (in `API.md`) done; migrated
**bulk rename** (cancel between series, `/api/rename-progress` unchanged,
`job_id` in the response), **archive scan** (from blocking synchronous to a job,
`202` + `job_id`), **MediaInfo backfill** (report in `Job.Result`); the jobs UI
(follow through to the outcome, Cancel button; `jobs.spec.ts`) done; explicit `Fail`
and `SetResult` added. The other operations remain, one at a time. Phase 4:
`make test-race ./...` green (**0 data race**); cleanup and blocking race after
Phase 3. Obsolete browser tests realigned without masking regressions; real fix:
scrollable tables reachable from the keyboard; browser suite 30/30.

Defects found: phantom Spanish-only key `Nuevo File Archiviato`
(removed; now the check would catch it); English subset with 1,345
missing entries (generated automatically — **they require linguistic review**
pre-release); `esc()` without `'` (fixed); non-idempotent test under race
(made repeatable with cleanup).

**Race on `t.info` (2026-10-12).** The race job on `cmd/gx-torrent` reported
`Collections()`/`Similar()` (daemon goroutines) while the run loop published
the metadata of a magnet: the field is now `atomic.Pointer[metainfo.Info]` with
accessors `info()`/`setInfo()` (commit `a496160`).
`go test -race ./cmd/gx-torrent/` and `./internal/gxcore/torrent/`: **0 data race**.
**Phase 4** (cleanup + blocking race) of the plan above remains.

### 8.8 Observations that emerged (they resize the plan)

- **Full modularisation of the client: not advised as it stands.** 11
  ES modules = a broad and risky refactor (IIFE→modules, call-site, browser tests)
  for a purely maintenance benefit; the compressed file is not a browser problem.
  Only when maintenance becomes the bottleneck, with a bundle in the build.
  Meanwhile `esc()` + XSS tests cover the real risk.
- **Global stable i18n keys: not necessary now.** The Italian-string
  model works; the hole was the English coverage (resolved). 3,130 entries × 6
  languages rewritten = risk of regressions. Hybrid: stable only for new
  dynamic strings, legacy unchanged, an automatic anti-hole check.
- **`POST /api/jobs` deliberately omitted** (validation in the real operations).
- **Backup, clean trash, vacuum remain synchronous by choice** (external effects,
  definitive deletions, non-interruptible VACUUM): do not migrate.
- Improvements: `Job.Result`, cancellation at safe points (never halfway through a
  unit), explicit `Fail`, automatic EN translations to be re-read.
- Phase 3: no mandatory migration left. Phase 4: cleanup + blocking race
  on the stabilised components. Next steps: EN linguistic review;
  UI on `/api/jobs`; explicit shutdown per migrated operation.

---

## 9. Technical review 2: bugs and debt at commit 97142c1

Ex `docs/archive/revisione-2.md` (analysis of 2026-10-06; method: almost
complete reading of orchestrator/worker/events/database/config/postprocess/cleaner/
safety/web+router, then engine/rss/parser/update/hooks/notifier/httpx/comics/
watcher/libtorrent; `go test` and `go vet` green; severity with a degree of certainty
*Confirmed*/*To verify*, nothing reproduced at runtime).

### 9.1 Bugs and criticalities (state: all fixed)

| Item | Problem | Fix |
|---|---|---|
| B1 | Unreachable path zeroes episode state (unmounted NAS = empty dir = `ENOENT`) | reset only if the folder exists, or the first existing non-empty ancestor (`archiveFileConfirmedMissing`; `archive_mount_test.go`) |
| B2 | Transient completion error = terminal + torrent detached, no retry | transient retries (EIO/ENOSPC/ESTALE/network/DB busy) after 1,2,4,8,16,32 min, then final; identity/files absent immediately final (`isTransientCompletionError`, `completionRetries`; in-memory counters) |
| B3 | Torrent added but not registered if the DB write fails (cycle aborted, "external" torrent) | failed registration → torrent withdrawn, placeholder cancelled, `continue` |
| B4 | `last_comics_check_ts` updated even if the comics cycle fails (+7 days of hole) | timestamp only on success; error → retry ~1 h |
| B5 | Comics DB errors abort series/movie cycles | comics settings read only if needed, errors degraded |
| B6 | Stop for "low space" without saving statistics or reporting | `finishCycleWithoutDownloads`: saves and reports anyway (global check unchanged) |
| B7 | Config reloaded badly in the event worker (fallback = startup config) | the last valid one remains |
| B8 | Rename repair outside `cycle_lock` (concurrency with cycle/events/renames) | per-series lock (`TryAcquireArchiveRename`/`AcquireArchiveImport`), import marker with a count; repair remains outside the lock but mutually exclusive per series |

### 9.2 Illogicalities and inconsistencies (state: fixed)

| Item | Problem | Fix |
|---|---|---|
| L1 | Redundant boolean in the selection (`incumbentWins` evaluated twice) | rewritten `covers && incumbentWins`, existing tests |
| L2 | Supersede/remove not symmetrical → outcome dependent on order (confirmed: episode before the pack = both downloaded) | `mergeSeriesCandidate`/`releaseStrictlyBetter`: at equal, the one that covers more wins (REMUX preferred); test with 2000 reshuffled inputs (fails with the old rule) |
| L3 | `Check*` functions that write (approve-immediately/cancel-afterwards fragile) | two-phase approval: read-only decision → add torrent → write confirmation + registration; rollback only for a failed registration after confirmation (`approval_phases_test.go`) |
| L4 | `last_deep_gap_fill_ts` written before the work | saved only when the pass is completed and not cancelled |
| L5 | Gap thresholds not coordinated (deep 6 h vs 23 h hard-coded) | `gap_research_hours` never below the deep interval |
| L6 | Double notification `torrent_finished` + `storage_moved` | `torrent_completed` only once (`alreadyAnnounced`, `completionNotified`; `seeding:true` remains distinct) |
| L7 | Mixed languages in logs and data values (`"Feed RSS"` used as a key) | shared `episodeOrigin*` constants g2/g7/detail (values unchanged: they reach the API/UI) |

### 9.3 Logic loops (state: fixed)

| Item | Problem | Fix |
|---|---|---|
| C1 | Worker restart after a panic without backoff (panic→restart cycle on a "poison" event) | `workerRestartBackoff`: wait 5 s→5 min, reset after 10 healthy minutes |
| C2 | Event reconstruction at every tick (750 ms, double source of truth, in-memory maps) | moves saved in the DB (`torrent_moves`, restore at +60 s) + re-check every 30 s instead of every round; events remain the main channel (`torrent_moves_restart_test.go`) |
| C3 | Slow work inline in the loop (SMTP/HTTP notifications, `SizeOfPath`, ffprobe, Jellyfin/Plex refresh) | async notifications (`notifier.Async`, best-effort queue 256; a test probe stays synchronous), `RefreshMediaLibraries` in a goroutine with coalescing |

### 9.4 Inefficiencies (state: fixed or closed with a reason)

| Item | Problem | Outcome |
|---|---|---|
| I1 | `DELETE` for every torrent at every tick on single-conn SQLite | only for entries really persisted |
| I2 | Query for every complete torrent at every tick | `settledTorrents` cache 60 s (or until a path/event change) |
| I3 | `torrents.List()` ~10 times per tick | postponed (500 ms backend cache mitigates) |
| I4 | Heavy `LoadConfig` from many points, divergent copies | converted the worker that reads without modifying; `LatestConfig` already has a cache invalidated on save |
| I5 | Stats of the whole archive for a debug log, under lock | only with diagnostics active |
| I6 | O(n²) selection with recomputations | not done: quadratic only within series+season, negligible |
| I7 | One archive search per query | closed: not a problem in real use |
| I8 | `RecordSeenBatch` ignores errors in Debug | raised to WARN |

### 9.5 Second pass: parser, feeds, notifications, hooks, comics, update

| Item | Problem | Fix |
|---|---|---|
| P1 | Parser on real titles (year from `1917`/`2001`, `S2024E05` unrecognised, `Stagione 2 Episodio 3`, `2160p` in the name, group `dl` from `WEB-DL`, audio 5.1 from `x265.10bit`, `720p` from CAM) | year = last before the technical tags; 4-digit seasons; `Stagione N Episodio/Puntata M` and `[Completa]`; resolution outside the name; 5.1 isolated; `hd`/`pal` only as a word; tail `WEB-DL`/`Blu-Ray` never a group (`parser_regression_test.go`). Not errors: daily S2024E133 (legacy intended), sentinel `EpisodeRange=[0]` for the packs; not handled: multi-season pack `S01-S03` |
| P2 | `FetchFeed`: page not downloadable → keeps the previous ones; not parseable → throws everything away, without logs | always keeps the pages read, with logs |
| P3 | Pagination assumed base-0 (ext.to: `page=1` = first page → early-stop, crawl stalled) | `listingPageNumber` for ext.to (verified live); Corsaro unchanged |
| P4 | Parser chosen by substring on the whole URL (query included) | chosen by host+path (`feedKindSubject`) |
| P5 | Magnet cache indexed by title alone (collisions) | key = detail URL; eviction of the oldest (not randomly) |
| P6 | Telegram retry without an initial wait + useless final sleep | wait before the attempt |
| P7 | `smtp.SendMail` without a timeout in the calling thread (freezes the worker) | `sendMailWithTimeout` 30 s (same STARTTLS/AUTH) |
| P8 | Telegram token in the logs (whole URL in the error) | `redactRequestError` (scheme+host) + mask `/bot<id>:<token>`; it contradicted `SECURITY.md` |
| P9 | Hook: variables expanded before the split (titles with spaces/quotes = argument injection) | first `SplitArgs`, then `Expand` per argument (scripts that relied on the split must be adapted) |
| P10 | Hook: ineffective timeout with children, unlimited concurrency, no recover | kill of the process group, `WaitDelay` 5 s, max 4 concurrent |
| P11 | Panic in a fan-out goroutine = process death (only 4 `recover`) | `recoverGoroutine` in 20 goroutines (engine/orchestrator/rss/hooks/handler/restart/qBittorrent) |
| P12 | Comics cycle: synchronous non-cancellable downloads under `cycle_lock` + Mega without timeout + ambiguous "first new file" | inactivity watchdog 2 min on HTTP, timeout 2 h + group kill for `megadl`, interruptible between titles (`comics_stall_test.go`) |
| P13 | Two HTML parsers (700 hand-written lines + `x/net/html`) | not fixed (choice: it must be tested on real pages) |
| P14 | Update checksum that "fails in the open" + tar without `--no-same-owner` | not fixed (choice) |
| P15 | libtorrent session read without a lock in ~40 methods (abort on destroyed handle) | counter + `enterSession`/`exitSession`/`drainSession` on 43 methods: no new calls during shutdown, wait ≤30 s (`libtorrent_session_guard_test.go`) |
| P16 | Watcher: no depth/size/stability limit | partly: max depth 8, specific names (size limit and double observation already existed in the worker; `ReadDir` does not follow symlinks) |
| P17 | Mixed snake_case/camelCase naming, group prefixes | not done (style, no bug) |

Other security notes: P8/P14 the most concrete ones; P9 injection from feed titles;
no SSRF restriction (consistent with LAN, to remember if exposed).

Structural debt not done with a reason (S1–S8: functions of 1100/600/380
lines, g2/g7 duplicates, route names, boolean parsing → but the uniform
`settingTruthy` done, S5, with the `weekly_enabled` bug found; `_ =` errors in 81 places,
in-memory maps, translations 3800 lines): refactorings without known bugs, to be
done separately if wanted. Things verified as fine: no deadlock with
single-conn (use of `tx`), atomic copy with fsync (without checksum, rename
overwrites), qBittorrent backoff, fail-closed on an unreadable blocklist, parameterised SQL.

Closed questions: B1 check added; B2 retry only transient; L6 double
notification = error; B8 exclusion per series; P1 parser fixed; P14 checksum
unchanged. Overall verification: `go test ./...` and `go test -race .` green at
every round; still to do: I3, I6, P17, S1–S4, S6–S8.

---

## 10. Terra and Pro Terra reports

Ex `docs/archive/gextto-terra.md` and `docs/archive/pro-terra.md` (readings of the
source with concrete improvements; no vulnerability in production claimed;
no change of daemon behaviour in the reports themselves).

### 10.1 Terra: priority and status

Implemented points 2, 3, 5 + first step of 7 (indexer concurrency, root
shutdown context with worker wait, router/documentation parity tests,
isolated UI registration). Point 1 excluded on request. Points 4 and 6 remain
design proposals deliberately not introduced (archive cache without reliable
invalidation; replacement of the C++ API without a version matrix): no
shortcuts on decision correctness or build compatibility.

| Priority | Area | Intervention | Outcome |
|---|---|---|---|
| High | HTTP exposure | Separate/restrict the `EngineListen` listener (same `Router` on both ports, second administrative access without auth) | excluded on request |
| High | Provider search | Indexer concurrency budget (semaphore 4–6, cautious default) | done |
| Medium | Shutdown | Deterministic worker closure before libtorrent (root context, operation registry, quiescence wait) | done |
| Medium | Scalability | Incremental archive index outside the critical cycle (the cycle re-reads the NAS) | proposal (persisted index + timestamp, scan as fallback; identical decisions) |
| Medium | API | Generated and verified API reference (qBit routes missing from the table) | done (parity test) |
| Low | C++ bridge | Remove deprecated libtorrent APIs (`half_open_limit`, cache, priority, trackers, resume) | proposal (version matrix + adapter, expected warnings as a list in CI) |
| Low | Router | Modularise the registration (`registerTorrentRoutes`, …) without changing the contract | first step done (isolated UI registration) |

Recommended plan: listener exposure → indexer concurrency + API tests →
shutdown → archive index + libtorrent compatibility.

### 10.2 Pro Terra: priority and status

Follow-up to Terra on security, concurrency, lifecycle, data integrity. Corrections
applied (LAN trusted: the pure network hardening postponed): shutdown (point 5 —
`trackOperation()` for `RunNow`/`RenameAll`/repair, wait before destroying
the session: it closes the race on the handle); concurrency (point 6 —
`torrent_engine` under `engine_mu`, protected `RenameProgress`, `categoryReady`
under `e.mu`, `safeGo` without double reporting and interruptible restart, SQLite
handles closed); integrity (points 7–8 — transactional series/movie upgrades,
no `DELETE` on an empty magnet hash); robustness (point 9 — errors no longer
silent, saturated `gibSetting`, size limit `HTTPDownload`). Point 1
partial (cause resolved with the tracking, `sessionMu` discipline on ~90 methods
recommended as defence in depth — then done in revision-2 P15).
Points 2–4 postponed (hook allowlist, SSRF filter, key redaction in
`ConfigView`): valid if ever exposed beyond the LAN.

| Priority | Intervention | Detail |
|---|---|---|
| Critical | Synchronise every read of `LibtorrentClient.session` | only `listUncached` used the lock; read without lock + `Shutdown` that zeroes = Boost abort on a destroyed handle; proposal `withSession` helper/extension of `RLock` + stress test |
| Critical | Constrain the `Program` of the event hooks | only non-empty and absolute: with `/bin/bash -c` = arbitrary shell from an unauthenticated port; proposal allowlist or a separate secret + doc in manual/`SECURITY.md` |
| High | Filter remote URLs (SSRF) and path origins | `send-magnet`/`archive/add`/`search/add` validate only `http(s)://`, the client follows redirects; proposals for a shared helper (no private/link-local/metadata, limited redirects, no de-ref local listener) + paths within the configured roots |
| High | Do not expose keys in `ConfigView` | indexer `api_key` + `tmdb/tvdb_api_key` in clear (other secrets already booleans); proposal flags only |
| High | Track goroutines from the handlers | `RunNow`/`RenameAll`/repair not in `bgWG`, they touch the engine after `ShutdownEmbedded` |
| Medium | Shared state | see above (done) |
| Medium/Medium | DB transactions + empty magnet hash | see above (done) |
| Low | Ignored errors and limits | see above (done); plus `staticcheck`/`errcheck` in CI |

Plan: handle race + goroutines → hooks + secrets → SSRF + paths → locking +
transactions → ignored errors + race/staticcheck in CI.

---

## 11. SSR + HTMX UI: migration report

Ex `docs/archive/UI_V2.md` (2026-10-01, build 1229, libtorrent 2.0.11.0).
The official interface is SSR + HTMX, it no longer depends on `gextto-ui.js`: it is at
**`/`**, `/v2` redirects (301, query preserved), it reuses the JSON API, Go
view-models and base CSS; tables rendered by the server via in-process API handlers
(`v2InternalJSON`: same validation, no network); server-side i18n with a
tokenizer (`v2TranslateHTML`: text node, `title`/`placeholder`/`aria-label`,
English fallback, never `<script>`/`<style>`, with IT byte-for-byte); minimal JS
(HTMX + `v2-core.js`: theme/font, accessible modals); isolated UI routes
(`v2Handle`, outside the API route table: `API.md` and parity tests unchanged);
old `/ui` route removed.

Architecture (`uiweb_v2*.go`, `uiweb/v2/templates/v2.html`,
`uiweb/v2/static/{htmx.min.js,v2-core.js,v2.css}`, 1 line in `web_router.go`):
shell/navigation/i18n, generic table renderer, Maintenance/
Integrations panels, Explore, Series/Movie details, structured editors (feed, checkbox,
rows, rename, translations), Maintenance widgets, special widgets (duplicates,
DB, RAM disk, rename, OAuth/PIN, jobs, upload, TMDB, translations by key,
rename preview); tests in `uiweb_v2_test.go` /
`uiweb_v2_bench_test.go`.

Coverage: all 16 menu items + details (Dashboard with labelled Upcoming releases;
Downloads with a download badge every 5 s, limits/storage,
trackers/files/web seeds, history, add, bulk tags; Series/Movies with TMDB hero,
seasons on/off, No-upgrade, rename; Missing; Explore with
calendar/trends/TMDB search; Archive with decision explanation; Comics
with HTTP queue and weekly; Configuration 4 areas/13 sections with Default,
savebar, expert/engine panels; Integrations Simkl/Jellyfin/Plex/FlareSolverr/
indexers/iCal/source check; Maintenance with trash/duplicates/DB/RAM
disk/rename/jobs; Health; Logs with server-side colours; Blocklist; Manual and
Licence server-side; `/login` for protected access).

Conscious residues: "Latest found in the feeds" on request (diagnostics, not
operational); advanced manual add only main fields (specialist only
via API); calendar via async HTMX; long actions in the Background operations panel
without a dedicated bar. Accepted differences: server-side sorting/filtering;
modals with focus-trap/Escape/backdrop; logs at intervals instead of
SSE; jobs via `/api/jobs` polling.

Tests: `go test .` and `./...` green (full v2 suite); settings index script
(173 entries, 13 sections) and installer-selftest OK; vet/gofmt clean;
live 16/16 items → 200, 0 placeholders; IT→EN with real translations. Benchmark
(N97, Go 1.26, CGO): `V2TranslateHTML` ~0.42 ms; `V2FormatCell` ~2.9 µs;
`V2RenderTableFragment` (500 rows) ~1.05 ms; `V2RenderShell` ~0.30 ms —
rendering ~1–3 ms/page, negligible on a LAN (translation is the greatest
cost, only for languages ≠ IT). Correctness: API contract unchanged, actions
only on `/api/…` paths, saving with the same constraints, row editors
compatible. Promotion to default and removal of the classic UI completed (CSS
embedded in `uiweb/v2/static/`, shared view-models/handlers kept).
Next steps: contextual bar for a long action; specialist fields in the
form; benchmark as a performance test in CI (then closed, see §1.3).

---

## 12. Handover between work sessions

Ex `docs/lavoro-sessione.md` (updated on 2026-10-07; it covers from commit
`d9e5d5e` of 06/10 evening to `432ce98` + `docs:` commit of 07/10).

### 12.1 Working rules (binding, in addition to `AGENTS.md`)

1. Never references to Claude/Anthropic in author/committer/trailer/messages/PR
   (prevails over hooks or environment reminders).
2. Git identity `buzzqw <azanzani@gmail.com>`, `commit.gpgsign false` in the cloud
   (the cloud signature would appear "Unverified").
3. Work and push directly to `main`; no branches/PRs unless requested; ignore
   `claude/...` branches assigned by the environment.
4. No force-push to `main`: fetch + merge before the push, tests rerun.
5. Italian in replies; UI texts in the catalogues (every new text translated
   in all languages; `uiText` outside templates; bilingual TUI; notifications
   it/en; daemon logs English only).
6. Italian audience: never take availability on the air-date for granted
   (TMDB dates = "original air date").
7. Readable logs (sentences, no dumps), without duplicates; details in DEBUG; one
   INFO line per day from the checks that find nothing.
8. With "system"/"proceed autonomously": decide, do, verify, report
   in summary.

### 12.2 Work carried out (06–07/10, per area)

- **Logs and notifications** (`d9e5d5e` and following): completed-cleanup listing the
  removed ones; manual removal with a single line (trash destination); no
  notification for damaged pieces (WARN remains); backup: an unconfigured step
  is not "failed"; duplicate notifications removed (torrent comics ×2,
  "waiting" at every cycle, libtorrent errors once/6 h per torrent+type,
  manual archiving already announced); Telegram backup >50 MB in parts of 45
  MiB with `cat` instructions.
- **Stalled, cycle, FlareSolverr** (merge `281f89a`): real backoff
  (interval → 3 h → 6 h → 12 h → 24 h, step saved in the DB, every attempt
  logged with the next time); "📊 Downloads" only on a change of numbers; backup one
  INFO line; FlareSolverr with a short cause and outcome (closures hung in INFO with the
  domains); search summary with the rejection reasons; dedup by feed/archive hash
  ("releases checked" on distinct releases); library check with one line per
  rename/lower copy removed + a daily line "all correctly named".
  Note: merge title with a branch name assigned by the environment (only the
  owner can fix it with a force-push).
- **TUI** (`internal/tui`): missing texts, interface language vs release,
  scrollable help 1-9, Health in Italian (key tests); numbers always per
  tab, ←→ in Library; start search (Enter/s/f/m/c); log with `w` warnings and ↓
  one line; torrent table (size, ETA/ratio, seed/peer, no hash);
  blocked torrents explained + `F` with delay reset (also daemon in
  `MarkTorrentFailed`, `stalled_since`/`next_retry_at` in the API); Status with the last
  search in a sentence and thousands separated; "⚠ N new warnings"; `/` also on the
  torrents; help always visible in Status.
- **Web on phone** (`uiweb/v2`): layout ≤560 px (touchable status strip,
  bottom bar one line, More menu, reordered Dashboard, Downloads with the list
  first and options behind ⚙, group actions only with a selection); compact Log
  with "Only warnings and errors" (also desktop); PWA (manifest, icons, minimal service
  worker, share target `/share`, pull-to-refresh); WARN/ERROR counter
  (`ProblemCount`); Downloads template fix (open div); CodeQL warnings (validated
  hosts, `nosniff`, `/share` redirect with `url.URL`, safe conversions).
- **Improvements §2** (detail above): hardlink, targeted refresh, upgrade-until,
  auth, iCal, ComicInfo, missing, badge, translations, docs.
- **gx-torrent completed**: from a non-selectable skeleton to a working engine
  (self-managed queue with slots/cap/slow-ignored/rotation/pin/probe/dynamic
  queue; stalled as persistent park/probe/unpark; symlink, safe delete,
  rename-or-copy; token + loopback-only without a token, engine RPC off; startup and
  supervision with fallback to libtorrent after 3 crashes/10 min; token/executable
  settings; honest capability matrix; daemon + adapter tests). Smoke test:
  startup, verify→seed, pause/resume, move with symlink, kill -9 →
  restart, SIGTERM on shutdown. (Big Buck Bunny in `downloading_metadata`
  in the sandbox without peers.)
- **Network, file selection, v2**: engine inside `internal/gxcore` (`replace`,
  `GEXTTO.md` inventory); single port + DHT on the same UDP + UPnP/NAT-PMP
  every 20 min; file selection (excluded in `DATA/parts`, progress on the chosen ones);
  listening/outgoing interface (killswitch), SOCKS5/HTTP proxy
  (`gxtorrent_proxy`), encryption 0/1/2, multi-format IP filter; **v2-only now
  accepted end-to-end** (§4.4; at the time they were refused with a blocklist and then
  superseded); session-stats also for gx-torrent; engine bugs (IP never
  re-contacted, logger race); real daemon↔daemon tests (single port, encryption,
  selection, proxy, half RAM disk, IP filter); smoke (network flags, port
  6881, torrent migration, message without UPnP).
- **uTP, LSD, Health**: UDP socket shared with the DHT (`internal/dht`), parallel
  uTP+TCP dial, LSD multicast BEP 14, Torrent engine panel in Health;
  real tests (uTP-only, LSD); DHT closure bug on the shared socket.
- **Torrent detail + qBittorrent**: tab with only commands supported by the
  engine + a gap note, "Skip/Download" files, errors in the tab, swarm/engine
  rows, peer Connection column; qBit: peer progress (was ×0.01),
  connection+flag, super-seeding unblocked, "Save trackers" that replaces;
  gx-torrent: progress/seed from the bitfield, current tracker, seeds counted.
- **Automatic cache + v2 logs**: cache from RAM (read 1/32, write 1/16) or
  from `libtorrent_cache_size`, expiry + `fallocate`, "Optimise" also for
  gx-torrent, line in Health; v2-only logs (🔁 v1 alternative / ⏳ waiting /
  ⌛ no longer tracked after 30 days; memory in RAM).
- **gx-torrent default**: missing/empty `torrent_backend` = `gx-torrent`
  (existing installations unchanged, `anacrolix` → `embedded`, fallback to
  libtorrent if it does not start); agnostic service (`GEXTTO_LIBTORRENT` only an
  override); shared network/queue/cache settings no longer disabled with
  gx-torrent; README + `gx-torrent.md` updated.

### 12.3 Open points as of 07/10 (historical; several closed/superseded)

> List **dated 07/10/2026**, kept as historical. In the meantime the wizard/indexer
> and v2 support have been closed (see §4.4 and
> [`gx-torrent-evoluto.md`](gx-torrent-evoluto.md) §12), so some points below
> must be read as superseded.

To decide: double notification "completed — seeding" in `move_episodes`
(proposed to remove the first); Telegram backup as a caption of the last
part; TUI keys with a meaning per tab (`d`, `R`, `i`, `e`); Series/Movies/Archive
tables on the phone with real data (tabs vs tables).
To do/verify: PWA/sharing on real HTTPS; stalled backoff on the real server
(resume from `notice_step`); mircrew indexer (only locally on the server);
merge title `281f89a`; "router port N opened" in the daemon log (or manual
opening TCP+UDP); with VPN set the outgoing interface (killswitch);
gx-torrent procedure (engine choice, Big Buck Bunny, slot/pause); §2 improvements
(hardlink with `ls -li`, Jellyfin/Plex folder scan, login via proxy with
`X-Forwarded-For`, calendar with a TMDB key).

### 12.4 Environment and pitfalls (cloud session, reference)

Build with `libtorrent-rasterbar-dev` (normal C++ warnings); root tests ~60 s.
Dry-run with a dedicated `GEXTTO_DATA_DIR`; stop via `kill` on the PIDs (never
`pkill -f`, which also kills the shell). TUI in `tmux` + `capture-pane`;
pause between `Escape` and letter (Alt+letter). Web/phone with Chromium +
Playwright preinstalled (config without `channel: chrome`, explicit
`executablePath`, new data dir; 36 tests green). Pitfalls: `gofmt` that realigns
`internal/tui/i18n.go`; cached `go test`; blocked push/force-push; `go test …
| grep … && git commit` also commits with red tests (check `$?`);
global comics download registry in tests; `#` at the beginning of a line in the manuals =
Markdown title (the e2e finds it); embedded manual (recompile before the
e2e). Map of touched code and resumption: see the original file in the archive.

---

## 13. Accessibility

Ex `docs/accessibility-analysis.md` (analysis of 29-09-2026 on
`uiweb/v2/templates/`, `uiweb/v2/static/`, tests in `uiweb/end2end/`; reference
WCAG 2.1 AA, compatible 2.2 practices).

**State**: **partially accessible** interface with a greatly improved automated
baseline; the automatic checks pass but they are not a WCAG/EN 301 549/legal
certification. A formal conclusion requires manual tests with keyboard,
screen reader, magnification and assistive technologies on supported browsers and
devices.

**Method**: static review (HTML/CSS/JS), semantics/names/ARIA/focus,
responsive layout/visible focus/reduced-motion, light/dark theme contrast, axe-core
via Playwright, regression tests (dialogs, keyboard sort, polling, narrow
viewports). Suite: **12 green tests** on dashboard, downloads, settings,
maintenance, health, logs (`cd uiweb/end2end && npm ci && npm run test:a11y`;
a safety net, not a substitute for the manual assessment).

**Improvements implemented**: language via the `lang` attribute; landmarks
(skip-nav, `aside`/`nav`/`header`/`main`); `aria-current="page"`;
accessible names (not just placeholder/tooltip); labels on icon controls;
dialogs (focus inside, Tab/Shift+Tab trap, Escape, focus restore);
keyboard-sortable headers with `aria-sort`; focus preserved under polling;
visible focus; tables with caption/scope (also dynamic); progressbar with a
value; ARIA for tabs/dialogs/landmarks; live region for states/errors/notifications/
async; decorative symbols never instead of text; settings search with
synonyms (`memo`, `memory`, `RAM`, `cache`); light-theme contrasts darkened where
needed; mobile reflow (tables with internal scroll); reduced-motion respected.

**Residual validation** (before any formal declaration): primary
workflows with Tab/Shift+Tab/Enter/Space/arrows/Escape; at least one desktop
screen reader (NVDA+Firefox or VoiceOver+Safari) and TalkBack where applicable; zoom
200%/400% (overflow, dialogs, tables, forms); every form with a screen reader
(error summary, field association, invalid states, recovery); async
states not colour-only (loading/success/warning/failure/empty/retry); both
themes, mobile viewports, reduced motion; browser-specific behaviours and
injected external content.

**Acceptance criteria**: reliable name+role for every control; every
mouse action from the keyboard; focus contained/restored in the dialogs;
async announcements without excessive interruptions; contrasts in both themes; semantics
usable for tables/tabs/progress/errors; no regression on desktop and mobile.

---

## 14. gx-torrent standalone (gx-nox): pointer

**Extract.** The full analysis and the decision — evolve `gx-torrent` with a single
binary and two modes, or create a new `gx-nox` — are in
[`docs/gx-torrent-evoluto.md`](gx-torrent-evoluto.md). Here only the pointer and
the decision in one line remain: **gx-torrent is evolved**, and "gx-nox" is the public name
of the standalone mode, not a second project. The RSS/UI work of the
standalone (guided editors, `/ui/settings`, `/ui/browse-dir`) is tracked in
[`docs/gx-torrent-evoluto.md`](gx-torrent-evoluto.md) §12.

---

## 15. High-value extensions from the comparison with BiglyBT

Ex comparative analysis with **BiglyBT** (Java client, heir of Azureus/Vuze). The
majority of its capabilities are **application-level or historical** (chat/friends, Vuze
DDB, media server, plugins, WebTorrent) and must not be ported. Only two
truly interesting *core* functions remain for gx-core: both are **backlog**, not
work done; here is the plan, with the hooks already present in the code.

### 15.1 Ping-Based Auto-Speed (anti-bufferbloat) — done

**Idea.** BiglyBT continuously measures the RTT (towards the gateway or a
reliable host); when the upload saturates the modem queue the RTT spikes and the engine
**dynamically reduces the upload limit** before the connection loses
packets and worsens the browsing/VoIP of the other devices. It is a feedback
control on **latency**, not on bandwidth: it acts only on the cap, it does not
touch the data path.

**Implemented (standalone).** `cmd/gx-torrent/autospeed.go`:

- A **probe** (`runAutoSpeed`, started next to the queue loop) measures every
  second the RTT with a **TCP connect** to the gateway (ports 80/443/53, gateway
  resolved from the default route) or to the configured `autospeed-target`; the baseline at
  rest is the **minimum of the last 60 s**.
- The **controller** (`autoSpeed.clampUpload`) reduces the upload by 15%
  (`autoSpeedReduceNum/Den`) when the latency exceeds `baseline × 1.5` **and**
  `baseline + 20 ms`, and raises it by `cap/20` when the line is at rest; never
  below `autospeed-min`. It **never** exceeds the configured or scheduled cap.
- The **only** point that calls `Session.SetSpeedLimits` remains
  `applyEffectiveSpeedLimitsLocked` (`schedule.go`): base/config → scheduler →
  auto-speed clamp, with reason `auto-speed` in the log. The engine's hot
  hook was already ready (`internal/gxcore/torrent/session_limits.go`).
- Keys in settings.json: `autospeed-enabled` (default `false`),
  `autospeed-target` (empty = gateway), `autospeed-min` (KiB/s, default 50).
  A positive upload limit is needed (global or scheduled) and the page enforces
  it. The option is in the daemon page (`settings.html`), with the
  *current latency / upload cap* state; it is **standalone only** like the bandwidth
  scheduler (in managed mode it is Gextto that governs the limits).

**Test.** `cmd/gx-torrent/autospeed_test.go`: clamp on high latency and recovery at
rest, floor at the minimum, inactivity without a cap or without measurements, `reset`,
probe against a local listener, parsing of the candidates and integration
`applyEffectiveSpeedLimitsLocked` (1000 → 850 KiB/s) + UI
(`TestUISettingsSavesAutoSpeed`). The i18n coverage (`TestUIPageIsFullyTranslatable`)
covers the new strings. **Measurement**: the probe is ~1 connection/s and does not touch the
data path; with auto-speed **off** nothing changes (no regression).

**Possible follow-up.** Gextto-side (managed) version: Gextto measures the RTT and
modulates the limits via `SetLimits`, so the option also applies to managed engines.

### 15.2 Swarm Merging (file sharing between torrents) — Phase 1 done

**Idea.** Make different swarms that contain **the same file** collaborate,
reusing the pieces already present in another torrent A instead of re-downloading them:
it revives torrents without seeders when identical content exists elsewhere. In
gx-core it is missing on v1; in v2 the intrinsic protocol deduplication already exists.

**Hooks already present.** `piece.Piece.Data` is a `filesection.Piece` and every
`FileSection{File, Offset, Length, Padding}` points to a file+**absolute** offset
(`internal/filesection/section.go`): writing a piece is `File.WriteAt(buf, off)`.
The **BEP 38** hints (`similar`/`collections`) are already read and merged
(`internal/gxcore/internal/metainfo/bep38.go`) and the local torrents that
share them are already found (`cmd/gx-torrent/bep38.go`): it is the discovery of the
candidates, the starting point of the merge.
The verification already exists (`Piece.VerifyHash` v1, `Piece.VerifyV2` v2); in v2
`metainfo.V2Files[fi].Root` is the **per-file identity**, and `internal/merkle`
provides the 16 KiB leaves for the **per-block** verification. The problem is not the
disk, it is the **verification**: B's bytes are not trusted until they are validated
against A's structure.

**Phase 1 (done, `torrent/localmerge.go`).** Merge only between torrents with an
**identical layout** (same piece length, same total length and same hashes at the same
indices): the pieces are directly interchangeable and are verified with
**A**'s hash. It covers the common case "same content, different `.torrent`/name/tracker"
with a corruption risk ≈ nil. Implementation: at the end
of A's allocation (`handleAllocationDone`) every torrent publishes an
immutable snapshot of its own pieces (`localData`); A reads the snapshots of the
other torrents of the session, for every piece **already verified** by the other
(bitfield) with the same hash and length reads the bytes from the other's storage,
re-verifies them against its own hash and writes them into its own storage; the
existing verifier then marks the pieces and closes the completion. No
cross-torrent query (atomic snapshot + bitfield under lock): no deadlock.
Accounting (bitfield/completion) per torrent.

- **Phase 2 (v2-first):** match by **per-file Merkle root** and **per-block**
  verification with a Merkle proof → identical files even with different layouts,
  without corruption. Still to do.
- **No v1 with a different layout:** no ED2K/partial heuristics of BiglyBT.

**Critical guard: BEP 27.** The merge is disabled if A **or** B is private
(`info.Private`): mixing private swarms violates the tracker rules and pollutes
ratio/statistics. It is a guard, not an option.

**Test/measurement.** `torrent/localmerge_test.go` (reuse, different content, private
guard) and `cmd/gx-torrent/swarmmerge_test.go` (end-to-end: B populated from disk,
A with identical content and a different name completes **without peers**). The two tests
also pass with `-race`. Effort: Phase 1 **medium** (done), Phase 2 **large**.

**Cost.** The merge runs once per torrent, at the end of the allocation: the
per-piece comparison (hash and length) happens **only** for the torrents with the
same `piece length` and the same total length (the outer filter is O(torrent)
with two field reads), and only the pieces that pass the comparison involve
I/O. No steady-state path (picker, peers, piece writing) is touched.

### 15.3 Verification criterion (like §4.5)

For every point: targeted tests (`./cmd/gx-torrent/`, `GxEngine`, `make test-engine`,
`make test`); with a visible effect also the documentation (`docs/gx-torrent.md`,
`MANUAL.*`, `README*`) and **both UIs** (Gextto `uiweb/v2` + the daemon
page), never only via API. **Before/after measurement** of CPU, RAM and throughput, as per the
performance rule of `AGENTS.md`; the new UI controls respect
accessibility and i18n.

---

## Appendix A: consolidated files

| Original file | Now (local copy, untracked) | Section here |
|---|---|---|
| `docs/proposte-migliorie.md` | `docs/archive/proposte-migliorie.md` | §2 |
| `docs/da-fare.md` | `docs/archive/da-fare.md` | §1 |
| `docs/lavoro-sessione.md` | `docs/archive/lavoro-sessione.md` | §12 |
| `docs/scelte-di-progetto.md` | `docs/archive/scelte-di-progetto.md` | §3 |
| `docs/accessibility-analysis.md` | `docs/archive/accessibility-analysis.md` | §13 |
| `docs/holepunch-test-harness.md` | `docs/archive/holepunch-test-harness.md` | §7 |
| `docs/gx-torrent-migliorie.md` | `docs/archive/gx-torrent-migliorie.md` | §4 |
| `docs/gx-torrent-ottimizzazioni-prestazionali-v2.md` | `docs/archive/gx-torrent-ottimizzazioni-prestazionali-v2.md` | §5 |
| `docs/gx-torrent-misure-seeding.md` | `docs/archive/gx-torrent-misure-seeding.md` | §6 |
| `docs/revisione-1.md` | `docs/archive/revisione-1.md` | §8 |
| `docs/archive/revisione-2.md` | remains in `docs/archive/` (untracked) | §9 |
| `docs/archive/UI_V2.md` | remains in `docs/archive/` (untracked) | §11 |
| `docs/archive/gextto-terra.md` | remains in `docs/archive/` (untracked) | §10.1 |
| `docs/archive/pro-terra.md` | remains in `docs/archive/` (untracked) | §10.2 |
