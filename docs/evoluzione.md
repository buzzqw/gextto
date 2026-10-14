# Improvements and evolution of Gextto and gx-torrent

Single working document for the **state, the measurements and the backlog** of
Gextto and of the gx-torrent / gx-core engine: what exists today, the numbers
behind the choices and what is planned next — not the problems that have been
solved (those stay in the git history). The current state of the engine
is in [`gx-torrent.md`](gx-torrent.md), [`bep.md`](bep.md) and
[`../internal/gxcore/GEXTTO.md`](../internal/gxcore/GEXTTO.md); the standalone
plan in [`gx-torrent-evoluto.md`](gx-torrent-evoluto.md).

## Index

1. [State and open items](#1-state-and-open-items)
2. [Gextto improvements implemented](#2-gextto-improvements-implemented)
3. [Strategic direction](#3-strategic-direction)
4. [gx-torrent: gaps and improvement plan](#4-gx-torrent-gaps-and-improvement-plan)
5. [gx-torrent: performance optimisations](#5-gx-torrent-performance-optimisations)
6. [gx-torrent: choking and seeding measurements](#6-gx-torrent-choking-and-seeding-measurements)
7. [Holepunching: test harness](#7-holepunching-test-harness)
8. [Accessibility](#8-accessibility)
9. [High-value extensions from the comparison with BiglyBT](#9-high-value-extensions-from-the-comparison-with-biglybt)
10. [Smartness backlog: ideas from the 2026-10-13 night](#10-smartness-backlog-ideas-from-the-2026-10-13-night)
11. [Open-items resolution program](#11-open-items-resolution-program-proposal-2026-10-14)
12. [Web streaming, DLNA media server and the choking/seeding campaign](#12-web-streaming-dlna-media-server-and-the-chokingseeding-campaign-2026-10-14)

---

## 1. State and open items

What Gextto and gx-torrent have today on the observability, measurement, decision,
engine-identity and performance fronts, with the numbers measured so far. The open
items picked for the next phase are in §11; the strategic direction is §3.

### 1.1 Observability: metrics over time

The observability data is stored in the database and shown in *Health*, without
Prometheus: the **Searches** panel with the duration and the outcome of every source
cycle by cycle (and a warning — in the log and as a notification — when a source
keeps failing, when a search takes too long or when no cycle completes cleanly for
three intervals), the **Title queue** panel with the titles waiting, since when and
until when, for series and movies, and **Failed imports** per day. The indexer health
is in *Sources*. The panels refresh themselves every 60 s.

**Download history.** Log lines about a torrent carry `acq: <id>` (a short ID derived
from the hash, which stays hidden) and end up in `acquisition_events`: *History* tab
in the torrent detail, 📜 per episode and a panel in the series page, API
`/api/acquisitions` (`acquisition.go`).

### 1.2 Measurement harnesses

Available outside CI, to decide with numbers:

- `scripts/bench-engines.sh` — RAM/throughput probe of gx-core: swarm throughput on
  loopback (with peak RSS) and disk write throughput of a directory (`--dir`, point
  it at a mount NFS to compare). `scripts/bench-engines.sh [--size MiB] [--dir DIR]
  [--leechers N]`;
- `make measure-seeding` / `make measure-choking` — choking and seeding, locally or
  with a real libtorrent leecher (§6);
- `make measure-pipeline` — the adaptive request queue (pipelining), local swarm at
  shallow and deeper depths (§6.4);
- `make measure-realswarm` — a public torrent with DHT, trackers and uTP enabled,
  sampling the uTP counters (§4.3, §6.8);
- `make measure-merge3` — Swarm Merging Phase 3 on real ISOs (§10.1);
- `make measure-dht` — the **public DHT**: routing-table fill and query success
  (`GX_MEASURE_DHT=5m`), the basis of the anti-Sybil and multi-path numbers (§4.4).

### 1.3 Decisions in force

- **Six languages everywhere:** it/en/de/fr/es/pl across the web UI (chrome, labels,
  tooltips, hints, server-side messages and the strings the client JS generates), the
  API/HTMX errors shown to the user, the notifications (Telegram/email/webhook), the
  TUI and the gx-torrent web page (the language is passed by Gextto with `-lang` and
  `?lang=xx`). Daemon logs stay **in English by choice**; API error strings not shown
  to the user may stay in the original language. Guard: `TestUIIsTranslatable` fails
  if a UI string or a `uiText` literal is not a key of the catalogues (with an
  explicit allowlist); `TestClientI18nKeysTranslated` covers the client keys.
- **First-start wizard (onboarding)** at `/?view=setup` (`uiweb_v2_setup.go`): TMDB or
  TVDB as the source, with the language chosen at the first step.
- **"Default" button on every setting.** Every non-secret field with a registered
  default (even empty) shows ↺ next to "Save": it fills the field with the default and
  the user confirms by saving; the value is shown next to it if compact
  (`Resettable`/`DefaultInline` in `uiweb_v2.go`, `HasDefault` in `uiweb_pages.go`).
  Excluded: fields disabled for the active engine, handled by auto-tuning, secrets,
  without a default.
- **One line per replacement in the upgrade logs:** `♻️ FBI S03E01 updated (1.4 GB):
  … · previous version «…» moved to the trash`; a normal addition stays `📁 … added to
  the library …`.
- **Deliberately not in the product** — the reasons are recorded so the ground is not
  re-covered: a **write-back cache** in the engine (measured `write_cache=0`: storage
  and download are in step, so there would be no gain; reopen only with storage
  genuinely slower than the network); **specialist fields** in the add form (no
  request, it would be preventive fluff); a **UI benchmark as a performance test in CI**
  (SSR rendering costs ~1–3 ms/page, it is not a problem); a **cache cap on slow
  storage (NFS)** (the cache self-regulates, `cmd/gx-torrent/cache.go`, tuner
  `cacheSizes`).

### 1.4 The engine is presented as our own code

gx-core is our engine and the documentation presents it as such: no external origin of
the code is advertised. The whole engine (`internal/gxcore`), the DHT (`internal/dht`)
and the uTP transport (`internal/utp`) are original code in-tree under EUPL-1.2, and
the per-component `LICENSE` files are gone: `THIRD-PARTY-NOTICES` lists linked
third-party libraries only. The peer ID prefix is `-GX` and the network identity is
`"gx-core " + Version`. Generic references to the *functionality* of other clients
remain (for example "as libtorrent does"), as do the optional alternative engines
(libtorrent behind cgo).

No dependency does the work of the engine: the rate limiter is a token bucket in
`internal/gxcore/internal/bandwidth`, the torrent id a version-1 UUID minted in
`internal/gxcore/torrent/torrent_id.go`, the logger `internal/gxcore/logging`, the
backoff `internal/gxcore/internal/backoff`, the DHT address decoding and throttle our
own. `go.mod` carries none of the engine's old helpers (`juju/ratelimit`
**LGPL-3.0**, `gofrs/uuid`, `fatih/structs`, `hokaccha/go-prettyjson`); the
replacements are behaviour-equivalent (the token bucket keeps the same refill/debt
semantics, the id stays unique and well-formed).

Every file of `internal/gxcore` (~130) and `internal/dht` (8) is rewritten in this
repository with APIs, wire formats and behaviours preserved. What still matches the
old code is protocol-mandated format (BEP wire layouts, bencode tags, API shapes) and
compatibility-kept strings (error/log texts, resume-DB and flag spellings), plus
algorithm bodies where any correct Go implementation converges. Where it mattered,
equivalence is proved by execution: MSE handshake cross-check over `net.Pipe`,
fast-set byte identity, resume-DB and RPC shapes byte-identical. How this state was
reached is in the git history.

### 1.5 In-depth performance check (CPU and RAM)

**Practice.** Periodically carry out an in-depth performance check of the application
and the engine, optimising CPU and RAM: benchmarks (`go test -bench … -benchmem`),
`pprof` CPU/heap profiles, footprint of the daemon and of `gx-torrent` (RSS,
goroutines, allocations), `make measure-seeding` for seeding. Constraint: every
replacement or change must be **at least equivalent** to the previous one, if not
better, and must be measured before and after. The numbers below are the current
state (Intel N97); the figures a change replaced are kept where they justify the
design.

**Engine logger.** Records below the handler threshold are discarded **before** being
formatted, so at WARNING (normal daemon use) the `Debugf`/`Infof` calls on the hot
paths allocate nothing: `BenchmarkEngineLoggerFiltered` vs `Forwarded` ~151 vs
~785 ns/op, 3 vs 6 allocs. The handler still receives the same records (ERROR/WARNING;
the swarm-noise counts are not lost).

**IP filter.** The IPv4 rules, like the IPv6 ones, are stored as **sorted and merged
intervals** with binary search (`sortMergeV4`/`v4Contains`); the pointer-based segment
tree (`internal/blocklist/stree`) is gone. With the Aetherinox blocklist (34 MB,
**666,763 rules** → 343,024 intervals): live heap **5.4 MB** (was 190 MB),
`BenchmarkBlocked` **18.6 ns/op, 0 alloc/op** (the old lookup allocated a `map` and a
slice on the path of every peer/tracker connection), `gx-torrent` RSS **~105 MB**
(RssAnon 92 MB) against 295 MB with the same number of rules loaded. The 34 MB raw
file stays in the daemon's cache (rare re-reading on manual reload): removable later.

**SQLite page cache.** `cache_size` is SQLite's default **2 MB** (`sqlite.go`). On the
real archive DB (510 MB, FTS5; `Archive.Browse` and `CountMatches`, tried up to 16 MB
of cache) the latency is **flat**, within the noise even on the broad `MATCH`
(~1.14 s), because it is the OS page cache that serves the file. `gexttod` RSS
**67.6 MB** (RssAnon 46.2 MB) against 109.7 MB at the same uptime.

**Torznab XML.** Answers are decoded with `Strict=false` plus the HTML entity table (a
raw `&` or `&egrave;` in a scraped title no longer makes the whole answer unreadable)
at no cost: on a 100-item answer, strict **2.24–2.32 ms/op, 642.8 kB/op,
17179 allocs/op**, lenient **2.22–2.29 ms/op, 642.8 kB/op, 17179 allocs/op**.
Prowlarr's aggregate search is JSON (one `json.Unmarshal` into a slice of small
structs), outside the XML path.

**Hot paths and the Phase 2 merge.** With the machine not idle, so the absolute numbers
are higher than the quiet rounds but the same order, and no regression on the paths
touched by the v2 swarm merge and the parser fuzzing:

| Benchmark | Result | Note |
|---|---|---|
| `BenchmarkPickerRarestSteady` | **143.5 µs/op, 0 B/op, 0 allocs/op** | piece picker, steady state (~102 µs when quiet) |
| `BenchmarkTickUnchokeSeeding` | **1.64 µs/op, 896 B/op, 1 alloc/op** | unchoker tick (~1.3 µs when quiet) |
| `BenchmarkBlocked` | **37.7 ns/op, 0 B/op, 0 allocs/op** | IP filter lookup (18.6 ns when quiet; load-dependent) |
| `BenchmarkEngineLoggerFiltered` vs `Forwarded` | **175 vs 730 ns/op, 3 vs 6 allocs/op** | the below-threshold fast path avoids the message formatting |
| `BenchmarkPrefillFromUnrelatedV2` | **1.58 µs/op, 128 B/op, 16 allocs/op** (16 candidates) | one-shot cost of the Phase 2 merge scan (allocation, resume, later trigger), **no steady-state path** |

Matching 16 unrelated v2 candidates costs ~1.6 µs once per torrent (one small map per
candidate) and the picker/peer/piece-write paths are not touched, so the merge cannot
affect seeding or download throughput. The cheap candidate filter that the resume and
later-trigger paths pay when nothing is reusable is `BenchmarkMergeCandidatesUnrelated`:
**~98 ns/op, 0 allocs/op** over 32 unrelated torrents (§10.1).

**The engine rewrite and `own-deps` are allocation-neutral.** The 54 engine benchmarks
compared with `benchstat` (`-count=6`) show identical `B/op` and `allocs/op`
everywhere, `BenchmarkHasAnyNotIn` 3.77 → **3.15 ns/op** (−16%) and `BenchmarkBlocked`
20.9 → **18.8 ns/op** (−10%); the rest is equal (`BenchmarkPickerRarestSteady`
115.6 → 105.5 µs, `BenchmarkPickerRarestShuffled` 1.071 → 1.057 ms,
`BenchmarkStorageWrite`/`StorageWriteMmap` 45.7/35.7 → 46.2/35.7 µs,
`BenchmarkTickUnchokeSeeding` 1.266 µs, `BenchmarkRequestRelease` 1.762 → 1.761 µs,
`BenchmarkUpdateInterestNothingWanted` 2.787 → 2.794 µs,
`BenchmarkPrefillFromUnrelatedV2` 1.487 → 1.488 µs). The three benchmarks that a single
pass had shown +13…17% slower (picker file-edge scan, interest update, prefill) are
**run-to-run drift**: re-measured interleaved (`-count=20` each) they are statistically
equal (p ≥ 0.15). Footprint at ~29 min uptime, real 34 MB blocklist and 4 active
torrents: `gexttod` RSS 113 MB (RssAnon 89 MB), `gx-torrent` RSS 165 MB (RssAnon
150 MB) — workload-dependent (peers/active pieces), within the range of the other
rounds, with no new steady-state allocation.

**End-to-end, seeding and pipelining.** `make measure-seeding`, 2 MiB and 3 leechers:
3.00× corpus, all done in 11 ms. `make measure-pipeline` in a local swarm, 8 MiB × 2:
16 MiB in 101 ms (~162 MiB/s); with RTT 50 ms, default depth (def=50/max=250) 0.608 s /
6736 KiB/s and shallow (def=1/max=1) 13.55 s / 302 KiB/s. The harness runs the seeder
and every leecher in one process and the engine's outgoing interface is
process-wide: each leecher creates its connection before the next daemon replaces
the setting, so the distinct source addresses are kept and the swarm reaches the
intended 3.00× (§6.6).

**Real swarm and DHT.** `make measure-realswarm`, one run each: Big Buck Bunny 276 MB
in 90 s (75 peers, 23 uTP, 0 SYN drops), Debian netinst 793 MB in 180 s (80 peers,
18 uTP, 0 SYN drops), Ubuntu 24.04 1.08 GB in 180 s (85 peers, 31 uTP, 0 SYN drops).
The first runs on 180 s windows recorded the uTP counters: Big Buck Bunny dials 342,
accepts 57, RTO resends 2563, fast 557; Debian dials 277, accepts 14, RTO resends 932,
fast 41; `utp_syn_drops` 0 in both (the 50-SYN backlog bound never binds) — real uTP
peers connect, the downloads complete and the resends show loss on a noisy swarm
without a storm. `make measure-dht` on the public network (4 min): **4262 nodes**,
21.9% query success, 875 reflected pings, 2 peers found from a cold start. The adaptive
RTO on the silent-peer harness (§4.3): the minimum-RTT floor removes a 6.2× resend
storm and the per-connection backoff sends **50% fewer** resends than the fixed
backoff.

---

## 2. Gextto improvements implemented

**All approved and implemented.** Comparison with
Sonarr/Radarr (series/movies), Mylar3/Kapowarr
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

---

## 3. Strategic direction

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

`gx-torrent` is the BitTorrent engine in **pure Go**: the **gx-core engine**
(`internal/gxcore`) plus the **daemon and adapter** (`cmd/gx-torrent`,
`gxtorrent_engine.go` — REST, web page, self-managed queue, the `TorrentEngine`
contract with Gextto). The per-capability state is in `capabilityLevels`
(`torrent_engine.go`), the single source of truth for the UI.

Gextto already provides, around every engine: queue/slots, stalled/retry, seed
policy, RSS, search, categories/tags, bandwidth scheduler, post-processing, moves
and RAM disk, IP filter, proxy — those are **not** engine gaps.

### 4.1 What was missing (now done or decided)

| Area | What was missing | Outcome |
|---|---|---|
| Per-torrent limits | download/upload speed; connections and upload slots | done (`SetLimits`, `SetMaxConnections`/`SetMaxUploads`) |
| Priority | per-piece, piece diagnostics, file priority | done (`pieces` API; graduated file priorities 0–7) |
| Trackers / web seed | removal; hot add/remove | done (`set-trackers`, `webseeds`) |
| Sequential | runtime toggle | done (`PiecePicker.SetOrder`) |
| Protocol | IPv6; BitTorrent v2-only | done (dual-stack, `peers6`, PEX v6, DHT BEP 32, LSD v6; v2 from `.torrent` and magnet, BEP 52) |
| Super-seeding | BEP 16 | done |
| Holepunching | BEP 55 | done (peer relay, uTP dial) |
| Upload/share mode | — | **skipped** (infinite seed with ratio 0 is used) |
| Seeding/choking quality | — | measured locally (§6); real-swarm scaling still open |

**Not gaps** (provided by Gextto around every engine): RSS, search plugin,
categories/tags, scheduler, ratio, post-processing, encryption, IP filter, proxy,
DHT/PEX/LSD, UPnP/NAT-PMP, fast resume, file selection.
Limit semantics: **-1 = inherit the global one, 0 = unlimited** (like libtorrent).

### 4.2 Remaining wishlist

- **WebTorrent/WebRTC**: niche, a lot of work.
- **Alternative storages** (blob, bolt, sqlite): not needed.
- **Seeding/choking scaling**: a campaign on a real swarm (gx-torrent vs
  libtorrent/qBittorrent, same policy, median over several runs).

### 4.3 Cherry-picks and follow-ups

Policy (also in `AGENTS.md`): the algorithm is **rewritten** (third-party code is
not copied — the idea is not covered by copyright, the expression is) or the module
is adopted as an **MPL-2.0 dependency**. The uTP transport was first vendored as
an MPL-2.0 dependency and then **rewritten as our own code** in
`internal/utp/transport` (EUPL-1.2); its helper primitives were reimplemented
in-tree too, so **no MPL component remains**.
Rules: one item = one commit; **measure before/after**; changes to `internal/gxcore`
recorded in `GEXTTO.md`; targeted tests; with a visible effect also both UIs and the docs.

| Item | Note | Status |
|---|---|---|
| Reader with seek + readahead and **graduated piece priority** | streaming/priority design, rewritten in-tree | done (stream > readahead > rarest) |
| **Adaptive request queue** (depth grows per delivered block, reset on choke) | reference design, rewritten in-tree | done; to validate on a real swarm |
| **External DHT replacing `internal/dht`** | own module vs a dependency | **postponed**: the API model differs (per-torrent traversal vs request channel) and it would drop the dual-stack/shared-socket patches; reopen if BEP 44/BEP 42 hardening or the own module becomes a burden |
| **Tracker scrape** (BEP 48) | reference design, rewritten in-tree | done (HTTP + UDP, `Torrent.Scrape`) |
| **Engine event bus** (SSE `GET /ui/events`) | own event bus (alerts-style) | done |
| **`.torrent` creation from a folder** | reference design, rewritten in-tree | done (`CreateTorrent`, form + API) |
| **Bad-storage / write-error diagnosis** | reference design, rewritten in-tree | done (folder in the error, `storage_errors` counter) |
| **LSD IPv6** (BEP 14, `ff15::efc0:988f`) | BEP 14, rewritten in-tree | done |
| **Ranged mmap flush** | reference design, rewritten in-tree | done |
| **Global in-flight budget — liveness** | reference design, rewritten in-tree | done |
| **Drop the worst connection** (deterministic order) | reference design, rewritten in-tree | done |
| **uTP behind `internal/utp`** (isolation) | own implementation | done: the engine talks to our `Socket` (dial/accept counters, `net.PacketConn` for the shared DHT socket); the implementation in `internal/utp/transport` is **our own code** (EUPL-1.2), swappable. Its helper primitives are reimplemented in `gextto_support.go`, so no third-party transport helper and no MPL component is linked |
| **uTP connection-id pick** (perf) | own (`internal/utp/transport`) | done: `newConnID` no longer builds a 64K-entry table per dial (512 KiB and 152,353 ns/op); it probes from a random id and stops at the first free one, O(1) with the connections a socket really has — `BenchmarkNewConnID`: 31 ns/op, 0 B/op, 0 allocs/op (`TestNewConnIDAvoidsCollisions`) |
| **uTP data path** (perf) | own (`internal/utp/transport`) | done: profiling a loopback transfer (`BenchmarkTransferLoopback`, 16 KiB block) produced four optimisations — the connection-id probe, lazy `Event` channels (the per-send `acked` flag no longer allocates one per packet), a reusable read window (`readPos` offset instead of reslicing), and a `netip.AddrPort` connection key instead of a `UDPAddr.String()` per inbound packet. Overall **45.6→62.0 MB/s, 82,020→23,692 B/op (−71%), 173→116 allocs/op (−33%)** on loopback |
| **uTP dial preference** (fewer wasted dials) | own (`internal/netx`) | done: per-host memory of race winners (≥4 races, ≥75% share, decayed past 64); the winner gets a 150 ms head start, the loser still races delayed so single-transport peers connect. Steady state: 1 raced dial per peer instead of 2 (`TestDialPeerSkipsLoserWhenPreferredWins` asserts zero loser dials; `TestDialPeerFallsBackToDelayedLeg` keeps the fallback) |
| **`UTPConn` address snapshot** (fewer allocs) | own (`internal/netx`) | done: TCP-view addrs converted once at wrap (`NewUTPConn`) instead of per `RemoteAddr()` call. `BenchmarkUTPConnRemoteAddr`: 259→113 ns/op, 152→40 B/op, 6→3 allocs/op (rest is `String()`); Intel N97 |
| **uTP resend counters** (congestion signal) | own (`internal/utp/transport`) | done: per-socket RTO/fast resends counted in the stack (`ResendStats`, proven by a stalled-server test), through `SessionStats` into the daemon API (`utp_resends_timeout/fast`); shown in both UIs (Salute «uTP» row and daemon status bar) when non-zero |
| **uTP initial RTO from measured latency** (faster loss recovery) | own (`internal/utp/transport`, `internal/netx`) | done: fresh connections seed the RTO from the per-host winning-dial latency EMA instead of the fixed 400 ms (clamped [5 ms, 400 ms], samples always win). `TestInitialLatencySeedBoundsRTO`: 5 ms seed → RTO [10,20] ms vs [800,1600] ms default. Loopback swarm validation on the branch: seeding 2.99× corpus; pipeline RTT 50 ms shallow 304 KiB/s in 13.5 s / default 6757 KiB/s in 0.6 s — identical to the pre-branch §6.7 numbers, no regression; real-swarm validation open |
| **uTP header hardening + fuzz** (robustness) | own (`internal/utp/transport`) | done: `header.Unmarshal` rejects <20B inputs instead of panicking (30 s fuzz clean, 36k execs); `FuzzHeaderUnmarshal` in `make fuzz-smoke` |
| **uTP SYN-drop counter** (burst signal) | own (`internal/utp/transport`) | done: backlog evictions counted per socket (`SynDrops`), through `SessionStats` into the daemon API (`utp_syn_drops`); tells whether the 50-SYN bound ever binds, shown in both UIs when non-zero |
| **uTP socket health in UI** (diagnostics) | own | done: socket dial/accept/error counters flow engine → daemon `/api/v1/stats` → Salute «uTP» row (hidden while idle, warns on dial errors with zero accepts: UDP likely firewalled) and daemon status bar («uTP socket», translated it/de/fr/es/pl); pattern of «Riuso locale» |
| **uTP adaptive RTO** (retransmission control) | own (`internal/utp/transport`) | done: the RTO consumes the loss signal. (1) A **minimum-RTT floor of 5 ms**: without it a fast path (loopback/LAN, RTT of tens of µs) collapses the RTO to sub-ms and a briefly stalled peer becomes a resend storm — silent-peer harness, 1 s: 18,184 resends at a 1 µs floor vs 2,919 at 5 ms (**6.2×**); `TestMinimumLatencyFloor`. (2) A **per-connection backoff** that doubles on each RTO timeout (cap 4×) and halves on each delivered packet, so a silent or congested peer is retried with growing patience while a link that delivers again relaxes to the measured RTT: silent-peer A/B, 3×4 s: 17,663 resends fixed vs 8,853 adaptive (**50% fewer**, backoff reaching 4); `TestAdaptiveRTOBacksOffOnTimeouts`, `TestAdaptiveRTOMeasurement` (opt-in `GX_MEASURE_UTP=1`). A healthy connection never timeout-resends, so the backoff stays 1: the RTO is today's, and the loopback swarm numbers are unchanged |

### 4.4 DHT: hardening and multi-path

The gx-core DHT (`internal/dht`, a module of its own) covers BEP 5/32/33/42/43/44/46
with a routing table as a bucket-less binary tree; compared with Tixati the gap is
**hardening and diagnostics**, not BEP coverage (see [`bep.md`](bep.md)).

- **Diagnostics — done.** `Stats()` exposes per-RPC and screening counters, the
  reflected address, the NAT type, `subnet_share_max`, `nodes_insecure_id`,
  `subnet_cap_hint` and `peers_confirmed_multi` (see
  [`../internal/gxcore/GEXTTO.md`](../internal/gxcore/GEXTTO.md)). It is the
  measurement basis of the items below.
- **Token hygiene — done.** The `announce_peer` token is bound to source address +
  scope (the info-hash), so a token from one swarm is not reused on another.
- **Anti-Sybil cap per subnet** (`Config.MaxNodesPerSubnet`, default 0 = off):
  implemented with an **O(1) counter**; nodes beyond the threshold stay in the
  address book with `noTree`, and `subnet_cap_hint` *advises* a value.
  **Measured 2026-10-14** (5 min, real info-hash): `subnet_share_max` **4–7**,
  `nodes_capped` **0** — no node exceeds any candidate threshold (8/16/24), so
  enforcement would be a no-op and CGNAT/mobile legitimately share subnets.
  **Decision: keep it as a hint, off by default.**
- **Multi-path lookup**: design only (`get_peers` on several branches in parallel,
  peers preferred when confirmed by ≥2 paths). The routing table already deduplicates
  repeated queries, and the two real measurements (random and real info-hash) gave
  **`peers_confirmed_multi` 0**: the gain is **not proven**, so the rewrite is
  **closed with data** (§10.2).
- **Harness**: `make measure-dht` (`GX_MEASURE_DHT=5m`,
  `GX_MEASURE_DHT_INFOHASH=<40 hex>`, opt-in, outside CI). On the public DHT we
  measured ~740 known nodes, `subnet_share_max` 14, `nodes_insecure_id` ≈42%, and
  the discovery of **108 peers in ~30 s** on the Ubuntu 24.04 torrent.
- **Out of scope**: DHT over I2P; the BEP 44 **outgoing client** (no consumer yet).

### 4.5 Port mapping, NAT detection, file priority

| Item | State |
|---|---|
| **PCP** (RFC 6887) port opening | done (own minimal client, tried after UPnP and before NAT-PMP) |
| **NAT type / reflected port** | done (`portcheck`: `reflected_port`, `nat_mapping`, `natHint`). Live: the BEP 42 `ip` field does not arrive on the public DHT (0 observations), so the datum shows only where nodes include it |
| **Graduated file priority** (0–7) | done (`piece.Piece.Priority`, `pickHigh` stage, `capabilityLevels.file_priorities = full`); **remaining**: measure with mixed-priority files |

### 4.6 Quality gaps vs libtorrent (craftsmanship, not features)

| Area | gx-core today | Gap |
|---|---|---|
| Choking / seeding | speed-based + optimistic, deterministic harness | quality algorithms absent; measurements **local only** |
| Pipelining | adaptive queue (50→250), 2 pieces per peer with a shared budget, write bounded to 4 pieces/torrent | 10 Gbps "to be measured" |
| Disk I/O | `O_SYNC` removed, `fdatasync`, mmap opt-in | O_DIRECT/io_uring/contiguous writes absent (not justified by a measurement) |
| File priority | graduated 0–7 | closed (residual measurement) |
| DHT | BEP 5/32/33/42/43/44/46, dedup | hardening not active, multi-path unproven |
| v2 / merge | v2 download, merge phases 1–3 (§9.2, §10.1) | v2 seed serves block-layer hashes only for whole files (`hash reject` otherwise) |
| Adversarial robustness | snub, session ban with TTL, IP filter | less battle-tested than libtorrent |

Out of scope by choice (in `bep.md`): BEP 8/17/18/22/26/28/30/34/35/45/50/51,
WebTorrent, native I2P (reachable via a SOCKS5/HTTP proxy).

### 4.7 Standalone daemon: Wanted, RSS, feed archive, IP filter (done)

- **Wanted** (`cmd/gx-torrent/wanted.go`, `/ui/rss` → *Wanted*): each title has
  type, seasons, language, quality, exclusions, episode filter, category and path;
  it is added by hand or via **Search the feeds**. The engine searches the indexers
  and the feeds each cycle, keeps a register in `state.json`
  (`wanted_seen`/`wanted_taken`), shows the **missing** episodes and runs a throttled
  **gap search**. For a movie it takes one release. The list does not generate
  feeds/rules, so the feeds stay free for the advanced rules.
- **RSS panel**: per-feed state (last check, elements, new, added, error),
  synchronous reload, enable/disable, configurable interval, ordered PASS/FAIL rules
  with up/down/disable, `once`, preview on the archive.
- **HTML listings as feeds** (`internal/rss/listing.go`): pages such as ext.to and
  TorrentGalaxy are resolved (direct magnets, `/post-detail/`, feed links) instead of
  failing on invalid XML; measured 50 elements in 0.3 s / 1.9 s on the two real URLs.
- **Feed archive** (`feed_archive.go`): bbolt with a compact index;
  `BenchmarkFeedArchiveSearch` (100k elements) from **362 ms to 7.4 ms** (49×) and
  2,800× less allocation.
- **IP filter** (`ipfilter_refresh.go`): a URL in `settings.json` is downloaded at
  startup and every `ipfilter-refresh-hours`; a local file is reloaded when it changes;
  state and *Load now* in the settings.
- **Optimise** (`optimize.go`): queue (Gextto base), cache (automatic), bandwidth
  (80% of the measured peak upload, auto-speed active).
- **Per-day bandwidth scheduler** (`internal/bwschedule`, shared by Gextto and
  gx-torrent): every weekday has its own window and limits (`schedule-week` in
  the daemon, `libtorrent_sched_week` in Gextto); a window past midnight
  belongs to the day it starts on. The older single window stays as fallback.
  Gextto's two duplicated evaluators now call the same function.
- **Notifications on Gextto's channels** (`internal/notify`): Telegram, e-mail
  (SMTP/STARTTLS) and webhook in the JSON, Discord, Slack, Gotify, ntfy and
  Pushover formats, for completed downloads, feed matches and feed errors,
  each switchable, with a test button.
- **Usability of the standalone pages**: Browse… on the folders, port test in
  *Network*, multi-folder DLNA library with *Add folder…*, Gextto's blocklist
  as default IP filter, export/import of the RSS setup instead of the raw
  JSON, total torrents per feed, `/regex/` wanted titles, an explanation of the
  rules, an *Update available* indicator in the status bar, and the gx-core
  version shown in Gextto's header and dashboard.

### 4.8 Verification criterion

- Per point: `go test ./cmd/gx-torrent/`, `go test -run GxEngine .`, `make test-engine`.
- With a visible effect: `docs/gx-torrent.md`, `MANUAL.*`, `README`.
- Every improvement visible in **both UIs** (Gextto `uiweb/v2` + `cmd/gx-torrent/ui.go`),
  never only via API.
- Comparisons with libtorrent/qBittorrent **only by measurement**.

---

## 5. gx-torrent: performance optimisations

Items ordered by **expected value = (impact × certainty) / (effort × risk)**, one
item = one commit.

| # | Intervention | Status |
|---|---|---|
| 1 | LSD without `d.mu`/`t.Stats()` (read the lock-free snapshot) | done (`d2a4060`) |
| 2 | Streaming: targeted `Torrent.PiecesDone(begin,end)` | done (`ff2d4bf`) |
| 3 | Remove `O_SYNC` + batched durability | done |
| 4 | MSE `StreamWriter` in place (0 alloc/block) | done (`5af2ee0`) |
| 5 | `servedRequests` bounded window (≈1024) | done (`a8b3e77`) |
| 6 | Piece cache: lazy TTL (sharding only if profiling shows contention) | done (`60b090e`) |
| 7 | `Bitfield.Count` with `math/bits` | done (`8caf6ad`) |
| 8 | Optionals: `findLocked` O(1) (done), ETag/304 (not to do) | decided |

**Item 3 (durability).** `O_SYNC` is removed and durability is batched, with the
invariant *a persisted bit refers to data already fsynced*: `syncPieces()` on the run
loop, `writeBitfield()` ordered snapshot → fsync → write, `updateStats` in two phases
(no Bolt transaction open during the fsyncs). `fdatasync` for the piece data;
**mmap opt-in** (`-mmap`, Linux) serves the files from a shared mapping.

Follow-ups **not added**, each without a measurement to justify it: io_uring/AIO (Go
does not expose it without a dependency or cgo; the mmap path is already
asynchronous) and explicit contiguous-write coalescing (the page cache already merges
adjacent writebacks).

**Measurements (Intel N97).** `O_SYNC` removal on NFS: 16 KiB 35→81 MiB/s (2.3×),
64 KiB 1.5×, 256 KiB 1.6×, 1 MiB 1.2×; on NVMe up to ~118× at small blocks;
forecast on NFS pieces 32 KiB–2 MiB: **~1.2×–1.8×**. mmap on tmpfs **~7.0 GB/s** vs
**~5.9 GB/s** for `pwrite`. Ranged mmap flush: clean mapping 477 µs → **13 ns**, one
dirtied piece unchanged (~1%). Seeding/choking measurements in §6.

**Deliberately excluded, with justification (measured at this load)**: rarity
buckets / picker refactor, watermark cursor in `pickSequential`/`pickFileEdge`,
`PieceDownloader` bitmask, `clear(b.Data)` in `bufferpool`, `FADV_RANDOM`,
`saveLocked` outside `d.mu`, `/proc/meminfo` under `d.mu`, `HandleDisconnect` O(N).
**Postponed**: qBittorrent `renameFile`/`renameFolder` (needs rename support in the
engine, a feature of its own).

---

## 6. gx-torrent: choking and seeding measurements

Measurement of the seeding/choking quality (§4.1). Underlying rule: **measure
first**, then decide whether to touch the engine core.

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

**Real swarm.** `cmd/gx-torrent/measure_realswarm_test.go` (opt-in,
`make measure-realswarm GX_MEASURE_REAL=<public.torrent>`) downloads a **public
torrent** with the DHT, the trackers and uTP enabled
(`GX_MEASURE_REAL_SECONDS` sets the window, default 120 s) and reports peak peers,
the uTP peer split and the uTP socket counters. It is the control-path check the
local swarm cannot give: real uTP peers connect, and on healthy connections the
RTO must not fire, so the resend counters stay low and the adaptive backoff stays
at 1. A run that sees no peer at all fails, so it cannot pass hollow.

**Isolation with latency (done).**
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
**Confirmation (2026-10-13):** the same run reproduces it exactly (`shallow` 300 KiB/s
in 13.7 s, `default` 3850 KiB/s in 1.06 s, ~12.8×), so the adaptive queue is stable.
**Ceiling read from the numbers:** with 256 KiB pieces (16 blocks) a queue of ≤250 is
bounded by the piece's block count, and the `default` run was already at the ceiling;
the next lever for high-bandwidth-delay links was therefore **multi-piece per peer**.

**Multi-piece per peer (done).** A peer may download up to
`maxPiecesPerPeer` (2) pieces at once (`peer.ConcurrentPieces`/`MaxConcurrentPieces`,
`fillPeerDownloads`, one `pieceDownloaders[peer][index]` map per peer), sharing a
single in-flight request budget across the pieces (`torrent.requestBlocks`, the
extra piece opens only when the window has room, `pipeHasRoom`); the picker never
returns a piece the peer already requests. Re-running the same latency harness
(256 KiB pieces, RTT 50 ms):

| Profile | single piece | multi-piece |
| --- | --- | --- |
| `shallow` def=1/max=1 | 301 KiB/s | 301 KiB/s (window of 1 respected) |
| `default` def=50/max=250 | 3853 KiB/s | **6756 KiB/s** (×1.75) |

So multi-piece raises the throughput at RTT 50 ms where the single-piece block
count was the limit (the `shallow` profile keeps its window of 1). The write ring
buffer (§10.3) is what keeps the disk from becoming the bottleneck while more
pieces are in flight; RAM and CPU equivalent.

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

Requirement §4.6 point 3. Same well-seeded public torrent (**Ubuntu 24.04.3
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

**DHT cold start.** The peer-search retry while a torrent looks for peers uses a
**15 s** interval (`DHTMinAnnounceInterval`); the Ubuntu torrent over DHT-only
resolves its metadata in ~49 s with 12 peers. The remaining time is the
routing-table fill (~30 s cold), not the lookup.

---

## 7. Holepunching: test harness

Support for `scripts/holepunch-netns-test.sh` (engine behaviour in
`docs/gx-torrent.md`).

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

**Usage**: `make gx-torrent`, then `sudo scripts/holepunch-netns-test.sh`
(`--keep` for debugging, `--size 16` for a larger torrent). A successful example in
`scripts/risultato.txt`. Debug: `HOLEPUNCH_DEADLINE` (default 90 s),
`HOLEPUNCH_B_DEADLINE` (60 s), `HOLEPUNCH_DIAG=1` (conntrack dump). On
failure it prints the `holepunch` lines of the logs (rendezvous/connect).

**Limits**: emulation with a stateful firewall, not a real cone NAT; outcome
`holepunch`/`incoming` according to who wins the race; IP dedup of peers by the
engine (in a real swarm the peers are on different IPs).

---

## 8. Accessibility

Reference: **WCAG 2.1 AA** (compatible 2.2 practices), checked on `uiweb/v2`
with manual tests plus the automatic ones in `uiweb/end2end/`.

**State**: **partially accessible** interface with a greatly improved automated
baseline; the automatic checks pass but they are not a WCAG/EN 301 549/legal
certification. A formal conclusion requires manual tests with keyboard,
screen reader, magnification and assistive technologies on supported browsers and
devices.

**Method**: static review (HTML/CSS/JS), semantics/names/ARIA/focus,
responsive layout/visible focus/reduced-motion, light/dark theme contrast, axe-core
via Playwright, regression tests (dialogs, keyboard sort, polling, narrow
viewports). Suite: **13 green tests** on dashboard, downloads, settings,
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

## 9. High-value extensions from the comparison with BiglyBT

Comparative analysis with **BiglyBT** (Java client, heir of Azureus/Vuze). The
majority of its capabilities are **application-level or historical** (chat/friends, Vuze
DDB, media server, plugins, WebTorrent) and must not be ported. Two *core*
functions were worth porting; both are **done**, with the plan and the
measurements kept below as the record.

### 9.1 Ping-Based Auto-Speed (anti-bufferbloat) — done

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

### 9.2 Swarm Merging (file sharing between torrents) — Phases 1–3, resume path and later trigger done

**Idea.** Make different swarms that contain **the same file** collaborate,
reusing the pieces already present in another torrent A instead of re-downloading them:
it revives torrents without seeders when identical content exists elsewhere. In
gx-core it is missing on v1; in v2 the intrinsic protocol deduplication already exists.

**Hooks already present.** `piece.Piece.Data` is a `filesection.Piece` and every
`FileSection{File, Offset, Length, Padding}` points to a file+**absolute** offset
(`internal/filesection/section.go`): writing a piece is `File.WriteAt(buf, off)`.
The **BEP 38** hints (`similar`/`collections`) are already read and merged
(`internal/metainfo/bep38.go`) and the local torrents that
share them are already found (`cmd/gx-torrent/bep38.go`). The merge does **not**
drive its candidates from them, though: it matches by **structure** (an
identical piece layout, or an equal v2 per-file Merkle root), which is what makes
the reuse safe; the hints are the *reporting* side of the BEP.
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

- **Phase 2 (v2-first) — done (`prefillV2From`/`mergeV2Files` in
  `torrent/localmerge.go`):** match by **per-file Merkle root** (`V2Files[].PiecesRoot`
  plus length) → identical files even with **different layouts**, without
  corruption. The destination pieces are reassembled from the source's
  **verified** pieces (the source pieces overlapping the destination piece's
  file-local range must be set in the source bitfield) and every destination
  piece is then **re-verified against its own hash** (`VerifyV2`) before it is
  written, so no unverified byte reaches the storage; the existing verifier then
  marks the pieces. An identical-layout pair stays Phase 1's job. The BEP 27
  guard is checked on both the snapshot and the parsed info. Tests:
  `torrent/localmerge_v2_test.go` (different layout, partial source, different
  content, private).
- **Phase 3 (block granularity) — done (`storePartialBlocks`/
  `prefillBlocks`):** the destination pieces that straddle verified and
  unverified source pieces reuse the verified 16 KiB blocks (`partialBlocks`);
  the downloader reads them from disk and requests only the rest. See §10.1.
- **Resume path — done:** the merge also runs when a torrent is
  **reloaded from its resume data** instead of being freshly allocated
  (`handleAllocationDone` + `startMergeVerifier` with the reloaded bitfield).
  The pieces the reloaded bitfield already verified are kept and the merge fills
  the rest, so a restart no longer drops the block-level reuse; the verifier
  takes the reloaded and merged pieces through `Skip`/`Only`, so **nothing is
  re-read or re-hashed** on that path.
- **Later-sibling trigger — done:** the merge is also re-triggered
  when a sibling **publishes its data after** this torrent's allocation (the
  one-shot allocation merge only saw the then-present siblings). Every torrent
  that gains verified pieces wakes the others (`notifySiblings`, only when its
  verified count grows, so two incomplete torrents cannot ping each other
  forever with no-op merges); a downloading torrent queues the merge, waits for
  the in-flight piece writes to drain (`mergeWaiting`/`pendingWrites`: an
  adoption during the verifier's bitfield swap would be lost), then runs it in
  the same exclusive window as the resume path (piece fetches and webseeds are
  cancelled, so nothing else writes) and restarts the pipeline at the end.
- **Phase 4, v1 cross-layout — done:** reuse between two **v1**
  torrents with a **different piece length**, matched by file **path and
  length** (`v1FilesMatch`, a heuristic — unlike the v2 Merkle root; see
  `beyond-bep.md` §5). Only **whole** destination pieces are reused: v1 has no
  per-block hash to justify a partial write, so a piece is assembled from
  verified source pieces and checked against its own hash **before** writing; a
  wrong match therefore never writes, and a candidate is abandoned after
  `maxV1VerifyFailures` (64) hash failures so it cannot read the whole source
  for nothing.

**No ED2K-style content hash.** The v1 cross-layout match is by file path and
length, not by a chunk content hash (BiglyBT/ED2K): those hashes are not in the
`.torrent`, so they would need a private index. The piece hash still verifies
every reuse.

**Critical guard: BEP 27.** The merge is disabled if A **or** B is private
(`info.Private`): mixing private swarms violates the tracker rules and pollutes
ratio/statistics. It is a guard, not an option.

**Test/measurement.** `torrent/localmerge_test.go` (reuse, different content, private
guard), `torrent/localmerge_resume_test.go` (the resume path through
`handleAllocationDone`: missing pieces filled and partial blocks re-derived, the
reloaded pieces kept without a re-read; the later-sibling trigger and its
write-drain gate; the notify count guard) and `cmd/gx-torrent/swarmmerge_test.go`
(end-to-end: B populated from disk, A with identical content and a different
name completes **without peers**; `TestDaemonSwarmMergeResumePath` reloads A from
resume across a daemon restart; `TestDaemonSwarmMergeLaterSibling` adds A first
and completes it when B appears afterwards). The tests also pass with `-race`.
Effort: Phase 1 **medium** (done), Phase 2 **large**.

**Cost.** The merge runs once per torrent, at the end of the allocation (and on
a resume from the reloaded bitfield, and — re-triggered — when a sibling
publishes its data afterwards): the
per-piece comparison (hash and length) happens **only** for the torrents with the
same `piece length` and the same total length (the outer filter is O(torrent)
with two field reads), and only the pieces that pass the comparison involve
I/O. Within one
pass each reused piece is read and written **once** even when several candidates
hold the same content (an index shared by Phase 1 and Phase 2), and a later
candidate fills only the gaps a previous one could not. The pieces the merge
re-verified against this torrent's own hashes are also handed to the verifier
(`Verifier.Skip`), which sets their bit without re-reading and re-hashing them:
only the pieces that were already on disk are verified again. The skip set is
consumed and cleared at the allocation-time verification, so a later manual
re-check verifies everything.
The later-sibling wake-up is a non-blocking send on a buffered per-torrent
channel, and only fires when a torrent's verified count grows (no steady-state
cost, no ping-pong); the merge it starts is the only path that briefly pauses
the fetch pipeline (piece fetches and webseeds are cancelled for the duration,
then restarted), the price of reusing data that appeared after the allocation.
On the **resume path** the same filter runs before starting the pieces from the
reloaded bitfield; when nothing is reusable it costs 115 ns and 0 allocations
over 32 unrelated session torrents (`BenchmarkMergeCandidatesUnrelated`), and
when something is, the merge is the intended work (§10.1).

### 9.3 Verification criterion (like §4.8)

For every point: targeted tests (`./cmd/gx-torrent/`, `GxEngine`, `make test-engine`,
`make test`); with a visible effect also the documentation (`docs/gx-torrent.md`,
`MANUAL.*`, `README*`) and **both UIs** (Gextto `uiweb/v2` + the daemon
page), never only via API. **Before/after measurement** of CPU, RAM and throughput, as per the
performance rule of `AGENTS.md`; the new UI controls respect
accessibility and i18n.

---

## 10. Smartness backlog: ideas from the 2026-10-13 night

The night session (parser fuzzing, Swarm Merging Phase 2, the DHT anti-Sybil
counter, the performance round) produced ideas that would make Gextto, gx-torrent
and gx-core **smarter**, not only faster or more correct. Each is classified
here; the implemented ones are dated, the others are a proposal to be agreed.

| # | Idea | Value | Risk | Status |
|---|---|---|---|---|
| 1 | **Continuous fuzzing** of every network-facing parser, with the found inputs kept as seeds, run by hand or in a nightly job | high (a DoS caught before a release) | low | **done**: `make fuzz-smoke` (`FUZZ_TIME=2m`), 21 targets (DHT, bencode, magnet, BEP 10/52, tracker, Torznab, RSS, MSE, peer-wire handshake), the seeds are committed under `testdata/fuzz`; `.github/workflows/fuzz.yml` runs it nightly (2m per target) and on a push that touches an engine/parser path (30s), uploading the failing inputs as an artifact |
| 2 | **Merge: read and write each reused piece once** across all candidates; a later candidate only fills the gaps a previous one could not | medium (no duplicate I/O when several torrents hold the same content) | low | **done**: `mergedIdx` shared by Phase 1 and Phase 2 |
| 3 | Merge: order the candidates by **reusable bytes** so the richest source is read first | low | low | **closed:** superseded by the dedupe (#2) — ordering changes only *which* source provides a piece, not the work; no measurable gain, so no speculative code |
| 4 | **Merge Phase 3: reuse at block granularity** via v2 Merkle proofs, so a source that has only part of a file (or only its block layer) still contributes | high (revives more seedless torrents) | high | **done**: real-data harness, 100% → 25% from the network, see §10.1 |
| 5 | DHT **adaptive subnet cap**: choose the threshold from the observed `subnet_share_max` distribution instead of a fixed number (still opt-in) | medium | medium | **partial**: `subnet_cap_hint` (the suggested cap from the live concentration) is exposed in `Stats()`; automatic enforcement not pursued (a /24 can be legitimate CGNAT/mobile) |
| 6 | DHT **multi-path `get_peers`** with peers confirmed by ≥2 paths | medium | medium | **measured + instrumented**: `peers_confirmed_multi` counts peers reported by ≥2 distinct nodes; the full rewrite awaits data, see §10.2 |
| 7 | **Merge metrics**: expose reused pieces/bytes and torrents completed without peers (stats, daemon page, Gextto) | medium | low | **done**: `merged_pieces`/`merged_bytes` in the engine session stats → daemon API → Gextto «Riuso locale» |
| 8 | **Multi-piece pipelining per peer** + write ring buffer, gated by the harness | high | high | **done**: write ring buffer + 2 pieces per peer; measured ×1.75 (3.85→6.76 MiB/s at RTT 50 ms), details in §6.4/§10.3 |
| 9 | A **single bounded bencode scanner** for every hand-written path (length and depth checked), plus an allocation-free screen before the decoder that trusts declared lengths | medium/high | low | **done (extended)**: `internal/utils/utils.go` (length bounded, recursion depth ≤64) and `internal/dht/bencode.go` `validBencode` cover DHT packets; a shared `internal/gxcore/internal/bencodecheck` screens every engine path that hands untrusted bytes to zeebo/bencode — which allocates a declared string length up front — so a tiny crafted input cannot ask for gigabytes: the `.torrent`/file-tree/piece-layer parsers (`metainfo`), the BEP 10 extension messages (`peerprotocol`) and the HTTP/UDP tracker replies (`httptracker`, `udptracker`) |
| 10 | Merge: **no double verification** — the pieces the merge just re-verified are handed to the verifier as already done | medium (no re-read and re-hash of the reused bytes) | low | **done**: `Verifier.Skip`, cleared after the allocation-time check |
| 11 | Merge **off the run loop**: the candidate scan stays on the run loop, the copy runs in `Verifier.Prepare` (cancellable, snapshot of the deselected pieces) | high | low | **done** |
| 12 | Merge on **empty storage**: `Verifier.Only` restricts the check to the merged pieces when no file existed | medium (no read+SHA-1 of the whole torrent after a partial merge) | low | **done** |
| 13 | **Word-wise interest** over the wanted set (`HasAnyOutside` + `skipBits`) | high (run-loop CPU on large torrents) | low | **done**: 231 → 2.8 µs per peer on 100k pieces (×80), 0 alloc |
| 14 | **Smarter eviction**: `peerUseful` keeps only interested peers when seeding, with a 30 s grace (`newPeerGrace`) | medium | low | **done** |
| 15 | **mmap fault guard**: `recoverFault` turns a fault (full disk, truncated file) into an I/O error on the torrent under `-mmap` | high for `-mmap` users | low | **done**: write bench equivalent (36.3–38.0 → 35.9–37.5 µs), 0 alloc |
| 16 | **Disk guard** in gx-torrent: downloads on a folder below the free-space threshold pause and resume by themselves, seeding goes on (Gextto only refused to *start* downloads) | high (no write errors, no torrents in error on a full disk) | low | **done**: `diskguard.go`, `min_free_mb` (default 1 GiB), daemon setting + badge, pushed from Gextto's `min_free_space_gb` |
| 17 | **Wanted state hygiene**: `pruneWantedTimers` bounds the state file while keeping the taken ledger (a re-added title is not downloaded again) | low | low | **done** |

The criterion is the same as §4.8/§9.3: one item = one commit, measurement
before/after for anything on a data path, both UIs and i18n for anything visible
to the user, and the reference documents kept aligned.

### 10.1 Merge Phase 3 — block-granularity reuse (done)

Phase 2 reuses a destination piece only when **every** source piece overlapping
it is verified. Phase 3 reuses the rest at **16 KiB block** granularity: the
verified spans of a partially covered destination piece are written to disk
(`storePartialBlocks`, `filesection.Piece.WriteAt`) and the blocks they cover
are recorded per piece (`torrent.partialBlocks`, handed from the verifier
goroutine to the run loop through `mergeOut`). When the piece is picked, the
downloader reads those blocks back (`prefillBlocks` → `PieceDownloader.Prefill`)
and requests only the others; the assembled piece is verified whole
(`VerifyV2`) as usual.

**Why it is safe.** BEP 52 leaves are the 16 KiB blocks of the *file*, not of
the piece: two torrents with the same `pieces root` share the whole Merkle tree
below the piece layer, whatever their piece length. A span copied from a
source piece the source verified is therefore the file's true content. The
final whole-piece check still runs; if it fails on a piece with local blocks,
the blocks are dropped, the peer is not banned (the mismatch cannot be blamed
on it) and the piece is downloaded whole, where smart ban applies again.

**Test environment (real data, public torrents).** `scripts/phase3-env.sh`
downloads the Debian netinst and Arch Linux ISOs over BitTorrent (aria2c),
builds with python3-libtorrent two **v2-only** torrents of each ISO — source
with 256 KiB pieces, destination with 1 MiB pieces, same file so same root — and
corrupts one source piece in four, so every destination piece straddles
verified and unverified source pieces. `TestMeasureMergePhase3`
(`GX_MEASURE_MERGE3=<dir>`, opt-in) runs a gx-torrent seeder of the destination
and a leecher holding the partial source, adds the destination to the leecher
and measures the bytes taken from the network; `GX_MEASURE_MERGE3_PEER` with
`scripts/phase3-seed.py` uses a **libtorrent** seeder instead (interoperability).

| Run (loopback) | before (Phase 2) | after (Phase 3) |
| --- | --- | --- |
| Debian 756 MiB: from the network | 792,723,456 B (100%) | **198,180,864 B (25.0%)** |
| Debian: total time | 2.81 s | **1.31 s** |
| Arch 1.5 GiB: from the network | 1,640,497,152 B (100%) | **410,255,360 B (25.0%)** |
| Arch: total time | 5.82 s | **2.71 s** |
| Arch: CPU (whole process, seeder included) | 17.7 s | **10.0 s** |
| Arch: peak RSS | 109 MiB | 111 MiB |
| Debian with a libtorrent 2.0.11 seeder | — | 25.0% from the network, complete |

All 756 (Debian) and 1565 (Arch) destination pieces were reused at block level
and every one passed its hash. The reused blocks are not persisted as such, but
the merge now re-runs on the **resume path** (`handleAllocationDone` +
`startMergeVerifier`, §9.2): after a restart from the resume data the blocks of
the pieces not yet completed are re-derived from the source and are **not**
downloaded again.

**Resume-path measurement (2026-10-13, `TestMeasureMergeResumePath` on the
Debian ISO, same harness/data as above).** A destination merged with no peer,
then paused and reloaded by a restarted leecher session, downloads after the
restart:

| Run (loopback) | before (no resume merge) | after (resume merge) |
| --- | --- | --- |
| Debian 756 MiB: from the network after the restart | 792,723,456 B (100%) | **198,180,864 B (25.0%)** |
| merged_bytes re-derived | 0 | 594,542,592 B |
| partial (block-level) pieces re-derived | 0 | 756 (every destination piece) |
| total time | 2.61 s | 1.00 s |

Exactly the no-restart Phase 3 figure: the reloaded bitfield is kept (no piece
re-read or re-hashed via `Verifier.Skip`/`Only`) and the block-level reuse is
re-derived, so a restart no longer re-downloads the file. On the resume path the
added cost when nothing is reusable is only the candidate filter: **115 ns and
0 allocations** over 32 unrelated session torrents
(`BenchmarkMergeCandidatesUnrelated`).

**Later-sibling measurement (2026-10-13, `TestMeasureMergeLaterSibling` on the
Debian ISO, same harness/data).** The destination is added **first**, with no
sibling and no peer; the source is added afterwards and wakes it:

| Run (loopback) | before (no trigger) | after (later-sibling trigger) |
| --- | --- | --- |
| later merge observed | no | yes |
| merged_bytes | 0 | 594,542,592 B |
| partial (block-level) pieces | 0 | 756 (every destination piece) |
| Debian 756 MiB: from the network | 792,723,456 B (100%) | **198,180,864 B (25.0%)** |
| total time | 2.61 s | 0.91 s |

Same 25.0% as the no-restart Phase 3, the resume path and the first-run merge:
the order in which the torrents are added no longer decides whether the reuse
happens.

### 10.2 DHT multi-path — measurement and plan

The iterative lookup already fans out to the ~8 closest nodes and re-enqueues
while peers are still needed; the missing part is **confirmation across paths**
(prefer a peer seen from ≥2 independent paths) to resist a node that lies or
swallows results. Design sketch: an explicit per-lookup state machine like the
BEP 44 one (`bep44_client.go`), with the reporters of each candidate peer
tracked, a bounded fan-out and a deadline; expose `peers_confirmed_multi` and
`lookup_paths` in `Stats()` (they surface as `dht_<key>` with no UI work).

Per the repo rule this is gated by the real-swarm measurement
(`make measure-dht`): query-out/reply-in and peers found set the baseline.

**Measurement (2026-10-13, 3 minutes, public DHT):** nodes settled at ~254 from
a peak of ~536 (stale nodes cleaned), **`subnet_share_max` = 8** (below the hint
threshold of 16, so no cap is suggested), `nodes_insecure_id` ≈ 40 %, `capped` =
0, `queries_out` 2899 / `replies_in` 834 (**≈ 29 % reply success**), `dropped`
1290 (rate limit), **`reflected_observations` 619** (the sending side of the BEP
42 `ip` field works: observers now report our address) and `peers_found` 0 (the
probe used a random info-hash).

**Read:** the routing table is **not concentrated** and the anti-Sybil cap is not
triggered, so the concentration lever is already sufficient; the ~71 % of
unanswered queries are nodes that do not reply, not proof of poisoning. There is
therefore **no data yet that justifies a multi-path lookup**: the next step was the
instrumentation, not the rewrite.

**Instrumentation (done).** `notePeerReport`/`expirePeerConfirm`
(`internal/dht/confirm.go`) track which responding nodes reported each
(info-hash, peer), bounded to `peerConfirmMax` entries and expiring after
`peerConfirmWindow`; `peers_confirmed_multi` (in `Stats()`, therefore
`dht_peers_confirmed_multi` in the daemon) counts the peers a second distinct
node confirmed. **Reporters are counted per subnet** (`reporterKey`, the same /24 or /48 unit as the anti-Sybil cap), so one host on many ports or a block of Sybil nodes in one /24 cannot confirm its own peers and inflate the datum that has to justify the rewrite. It is read-only: it changes nothing about which peers are used.
On a real swarm a non-trivial value is the evidence needed before spending the
cost of a full multi-path lookup (an explicit per-lookup state machine with a
bounded fan-out and a deadline, like `bep44_client.go`). Tests:
`TestPeerConfirmCountsDistinctReporters`, `TestPeerConfirmIsBounded`,
`TestPeerConfirmExpiry`, `TestReporterKeyIsTheSubnet`.

**Second measurement (2026-10-14, 5 minutes, real Debian info-hash
`7acf8fb5…`):** nodes ~296 (peak 525), `subnet_share_max` 4–7, `nodes_insecure_id`
≈ 42 %, `nodes_capped` 0, queries_out 4703 / replies_in 1670 (35.5 %),
`reflected_observations` 1063, **`peers_found` 0**, **`peers_confirmed_multi` 0**.
Same read as the first run, on a populated info-hash rather than a random one:
the routing table is not concentrated and **no peer was confirmed by two
distinct subnets**, so there is still **no evidence to justify a multi-path
rewrite**. The item is therefore **closed with data** (a controlled adversarial
swarm would be the only next step, and it is out of scope for a home client).

### 10.3 Multi-piece pipelining — measurement and outcome

The measured baseline (§6.4, 2026-10-13) is ~12.8× from the adaptive queue at
RTT 50 ms, and it also shows the ceiling: with 256 KiB pieces (16 blocks) the
queue cannot exceed the piece's block count, so multi-piece per peer is the next
lever for high-bandwidth-delay links.

**Bounded write ring buffer (done).** The per-torrent "one write at a
time" is replaced by a **bounded per-torrent write ring buffer**:
`beginPieceWrite`/`endPieceWrite` (`torrent_write.go`) suspend the piece channels
only once `maxPendingWrites` (4) pieces are being written, so the network keeps
flowing while pieces are written. The picker skips a piece marked `Writing`, so a
piece is never written twice, and `Config.ParallelWrites` (`semWrite`) still caps
the disk parallelism globally; the per-torrent bound limits the in-flight piece
buffers. Tests: `torrent_writering_test.go` (suspend at the cap, resume below, no
underflow), the full `cmd/gx-torrent` e2e suite and `-race` on the transfer tests.
**Measurement:** `make measure-seeding` on loopback is **equivalent** (the disk is
not the bottleneck there); the gain is on slow storage, and it is the prerequisite
for multi-piece.

**Multi-piece per peer (done).** A peer downloads up to
`maxPiecesPerPeer` (2) pieces at once: `pieceDownloaders` is a per-peer map keyed
by piece index (`torrent.go`, `closePieceDownloader`, `handlePieceMessage` look
the piece up by `msg.Index`), `peer.ConcurrentPieces`/`MaxConcurrentPieces` gate
the picker (`findPiece`), and the in-flight budget is shared across a peer's
pieces (`torrent.requestBlocks`, `RequestBlocks` returns the count sent; the
extra piece opens only when the window has room, `pipeHasRoom`).
`fillPeerDownloads` tops a peer up to the cap on
Have/Bitfield/HaveAll/Unchoke/completion, one queued RAM request per peer, and
endgame duplicates are cancelled as soon as a copy starts writing (peer and
webseed, `markDirty` widened for out-of-order writes); the picker never hands a
peer the same piece twice. **Measurement (the latency harness):** `default`
**3853 → 6756 KiB/s** (×1.75), `shallow` 301 KiB/s (window of 1 respected) at RTT
50 ms with 256 KiB pieces. Tests: e2e suite + `-race` on the transfer tests,
`piecedownloader` budget test. RAM and CPU equivalent.

### 10.4 Beyond the BEPs: WebSocket tracker, DHT feeds, v1 cross-layout merge (done)

The analysis and the design are in [`beyond-bep.md`](beyond-bep.md); these are
the protocol extensions outside the BEP catalogue. Implemented:

- **WebSocket tracker (ws/wss).** `internal/gxcore/internal/tracker/wstracker`
  is a bounded, original RFC 6455 client plus the WebTorrent/bittorrent-tracker
  tracker (JSON announce/scrape, binary fields as 20-code-point strings, full
  and compact peers, BEP 31 retry); `trackermanager.Get` accepts `ws`/`wss` and
  the sockets are closed with the session. It dials through the same egress as
  the HTTP tracker (proxy, resolver, blocklist) and supports `wss`. Tests:
  announce, scrape, failure, `wss`, a rejected handshake and a frame fuzzer
  (`FuzzWSReadMessage`, in `make fuzz-smoke`). **Caveat:** the reference
  `bittorrent-tracker` returns only WebRTC peers to a WebSocket client, so a
  native client gets no TCP peers from it; the standalone value is limited to
  trackers that return standard peers, and the real unlock is WebRTC (not done).
- **DHT feeds (BEP 46).** Engine: `bep46Feed` decodes a mutable item value as a
  single torrent (`{"ih"}`) or a feed (`{"items":[{"ih","name"}]}`), and
  `Session.ResolveDHTFeed` resolves it (signature verified by the DHT client).
  Daemon: a feed URL `dht://<pubkey-hex>[?salt=<hex>]` becomes a normal feed
  source (resolved to magnet items, so rules/archive/dedup/add are reused) and
  `GET /api/v1/dht-feed` resolves one for external consumers. Tests: the value
  codec (engine) and the URL parse and item mapping (daemon). The **publisher**
  side (signing a feed) is not implemented.
- **v1 cross-layout merge** (Swarm Merging Phase 4, §9.2): two v1 torrents with
  a different piece length share whole pieces, matched by file path and length
  and verified against each destination piece's hash before writing. No
  per-block reuse for v1 (no per-block hash), and a candidate is dropped after
  64 failed verifications.

---

## 11. Open-items resolution program (proposal, 2026-10-14)

Proposal under discussion. Scope: the open items selected for the next phase —
§1.5 (periodic performance check), §4.3 (cherry-picks/follow-ups), §4.4 and
§10.2 (DHT hardening and multi-path), §4.6 (quality gaps vs libtorrent) and the
§10 smartness backlog. Everything else in this document is done or closed.

**Diagnosis.** The items are not independent: almost all of them are
**implemented or designed but not validated on a real swarm**, or they need a
**go/no-go decided on data** (the DHT levers, the choking algorithm, the disk
I/O). Per `AGENTS.md` there is **no speculative code**: implement only what a
measurement justifies; otherwise close the item with the recorded data (as was
done for the merge candidate ordering, §10 item 3). The program therefore turns
each item into a measured experiment with an explicit **gate** and **decision**.

### WP0 — Real-swarm harness (enabler)

- **Objective:** one reproducible baseline every other gate can compare against.
- **Method:** keep `make measure-realswarm` / `measure-dht` / `measure-pipeline`
  / `measure-seeding`; add `scripts/measure-baseline.sh` that runs the cheap ones
  and writes a dated summary (throughput, uTP resends, DHT reply rate, peers
  found) to `evoluzione.md`; fix a public test torrent (Ubuntu/Debian) and a DHT
  info-hash for reproducibility.
- **Gate:** baseline recorded. **Deliverable:** script + baseline entry.
  **Effort** 0.5 day, **risk** none.

### WP1 — Performance round (§1.5)

- **Method (idle machine):** `go test -bench … -benchmem`; `pprof` CPU/heap on
  `gexttod` and `gx-torrent` during a real transfer; RSS and goroutine counts;
  `make measure-seeding`.
- **Gate/decision:** code changes only for a **measured** hotspot; otherwise
  record "no regression, no change".
- **Deliverable:** a "Performance round N" entry with the numbers.
  **Effort** 0.5–1 day, **risk** none.

### WP2 — Real-swarm validation of built features (§4.3) + close decisions

- **Items:** the **adaptive request queue** (pipelining) and the **uTP adaptive
  RTO** validated on a live torrent with `make measure-realswarm` (healthy paths
  keep the resend counters low, throughput not regressed); **graduated file
  priority** measured locally with mixed priorities.
- **Decision:** **external DHT — close** (our own module keeps the dual-stack
  and shared-socket patches and the per-torrent traversal model that a dependency
  would drop).
- **Deliverable:** measurement records + the external-DHT decision.
  **Effort** 0.5 day, **risk** low.

### WP3 — DHT cluster (§10.2, §4.4, §10 items 5–6)

- **A — data.** `make measure-dht` on a **real active info-hash** (not a random
  one) long enough that `peers_found` and `peers_confirmed_multi` are
  non-trivial; record reply rate, `subnet_share_max`, `nodes_insecure_id`,
  `subnet_cap_hint`.
- **B — anti-Sybil cap.** Run with `MaxNodesPerSubnet` at candidate thresholds
  (8/16/24); **enforce only if** it does not lower `peers_found` while lowering
  the concentration; otherwise keep the hint (close).
- **C — multi-path.** Decision gate on the confirmed-multi share and on connect
  success by confirmation. If justified, implement a **bounded per-lookup state
  machine** (like `bep44_client.go`) with a fan-out and a deadline and expose
  `lookup_paths`; else close with data.
- **Deliverable:** records + either the enforcement / state machine (with tests)
  or two "closed with data" notes. **Effort** measurement 0.5–1 day;
  implementation 2–4 days if justified. **Risk** medium (DHT correctness).

### WP4 — Quality gaps vs libtorrent (§4.6)

- **4a Choking/seeding campaign.** On a real swarm, gx-torrent vs
  libtorrent/qBittorrent with the same policy on the same public torrent;
  metrics: upload efficiency while seeding, reciprocation, first-block time,
  stall rate. **Gate:** implement an improved choking algorithm only if a gap
  shows.
- **4b 10 Gbps pipelining.** Extend `measure-pipeline` / the latency harness to
  deeper queues and higher rates; multi-piece is already in; implement more only
  if the measured ceiling is below target.
- **4c Disk I/O (O_DIRECT/io_uring).** Measure the disk path (`measure-seeding`
  + a slow-storage harness); implement only if the disk is the bottleneck.
- **4d v2 seed: block-layer hashes for a partial file.** Today `serveHashes`
  refuses (`hash reject`) a `base=0` request unless the **whole** file is present
  (`v2BlockTree`). Implement serving **only the requested leaf range**, computing
  each leaf from the downloaded pieces and still rejecting a leaf whose piece is
  missing; the upper layers come from the attached `piece layers`. Tests: unit
  (partial file, missing piece, out-of-range, tail) + interop with a libtorrent
  leecher. This is a **clear feature gap**, so it is implemented regardless.
- **4e Adversarial robustness.** Test-only harness: a lying peer, a bogus
  bitfield, a `hash request` flood; the existing smart ban and the fuzz targets
  are the control.
- **Deliverable:** campaign records + 4d (code/tests/docs) + 4e tests.
  **Effort** campaign 1 day, 4d 2–3 days, 4e 0.5 day. **Risk** 4d medium
  (protocol), the rest low.

### Sequence and decision points

**WP0 → WP1 → WP2 → WP3 (A→B→C) → WP4.** Cheap "close with data" first
(WP1/WP2), then the DHT cluster (WP3), then the one clear feature (WP4d) and the
campaign (WP4a). Explicit decisions: WP2 external DHT (close), WP3-B cap
(enforce/hint), WP3-C multi-path (implement/close), WP4a choking
(implement/close), WP4b/4c (implement only if measured), WP4d (always).

One WP = its own commits, measurement recorded **before and after** in this
document; the reference docs updated with each change (`bep.md` for 4d,
`gx-torrent.md`, [`../internal/gxcore/GEXTTO.md`](../internal/gxcore/GEXTTO.md)).

### 11.1 Results — first run (2026-10-14, Intel N97, public torrents/DHT)

**WP0.** `scripts/measure-baseline.sh` added: it runs the cheap harnesses
(benchmarks, local swarm, pipelining) and the network ones when their env is
set, and writes a dated baseline. First baseline stored from the branch.

**WP1 (performance).** The baseline surfaced a regression introduced by the v1
cross-layout filter: `BenchmarkMergeCandidatesUnrelated` **1360 → 375 ns/op,
0 allocs/op** after replacing a per-candidate map with a nested scan (was
~98 ns before the v1 work; the remaining cost is the per-candidate file scan
the feature needs, on one-shot paths only). The nested scan is quadratic, so
it is kept only up to `len(a)*len(b) <= 1024` (lengths compared before paths);
larger packs index the smaller torrent in a map. `BenchmarkV1FilesMatchNoMatch`
(unrelated torrents, same file length): 8 files **290 → 278 ns**, 64 files
**18.2 → 3.4 µs**, 2000 files **17.3 ms → 0.13 ms** (109 KB, 9 allocs);
`BenchmarkMergeCandidatesUnrelated` **375 → 265 ns/op**, still 0 allocs. No
other hotspot appeared.

**WP2 (real swarm).** `make measure-realswarm` on the Debian 13.7 netinst
torrent, 2 minutes: download **completed to 100 %**, peers **peak 80**
(uTP peak 26), connections uTP 53 / TCP 118 out, 6 in; uTP socket **dials 265
(91 errors)**, accepts 6 (0 errors), **resends RTO 1211, fast 0**, **SYN drops
0**. The engine finishes a real swarm; the uTP dial error rate (91/265 ≈ 34 %)
is the expected cost of racing uTP against TCP-only peers and is worth a closer
look in a later round, not a regression. **External DHT: closed** (keep the own
module: per-torrent traversal + dual-stack/shared-socket patches).

**WP3 (DHT, 5 minutes, real Debian info-hash).** nodes settled ~296 from a
peak of 525, **`subnet_share_max` 4–7** (below the hint threshold of 16),
`nodes_insecure_id` ≈ 42 %, **`nodes_capped` 0**, queries_out 4703 / replies_in
1670 (**35.5 % reply success**), dropped 1273, **`reflected_observations` 1063**
(BEP 42 `ip` sending works), **`peers_found` 0**, **`peers_confirmed_multi` 0**.

- **WP3-B anti-Sybil cap: keep as a hint, off.** With `subnet_share_max` ≤ 7 no
  node exceeds any candidate threshold (8/16/24), so enforcement would be a
  no-op (`nodes_capped` 0); CGNAT/mobile legitimately share subnets. No code.
- **WP3-C multi-path: closed with data.** `peers_confirmed_multi` 0 and a low
  concentration give **no evidence** for a multi-path rewrite; the routing table
  is not concentrated and the unanswered queries are silent nodes, not poisoning
  (see §10.2). No rewrite.

**WP4a (choking/seeding campaign).** Local evidence is healthy: without mesh the
seeders uploads 3.00× the corpus (3 pure leechers), and **with mesh the leechers
reciprocate** — they upload **2.52 MB** among themselves and the seeders upload
drops to **1.84×** the corpus (−39 %). A real-swarm comparison against
libtorrent/qBittorrent (same policy, same public torrent, metrics: upload
efficiency, reciprocation, first-block time, stall rate) **remains the gate**
for any choking change — not implemented.

**WP4b (10 Gbps pipelining).** The local latency harness tops out at
**~162 MB/s** for a single peer pair (deep profile); 10 Gbps (1.25 GB/s) needs a
multi-peer swarm and a fast link, neither available here. Multi-piece is already
in (§10.3); no further change justified by these numbers.

**WP4c (disk I/O).** `BenchmarkStorageWrite` **~5.8 GB/s** buffered and
**~7.4 GB/s** mmap (ranged mmap sync ~29 GB/s, page cache), far above any
network speed: the disk is **not** the bottleneck, so `O_DIRECT`/`io_uring` are
**not justified**. Closed with data.

**WP4d (v2 partial-file hash serving).** Implemented: `servePartialBlockHashes`
+ `blockLeavesRange` in `torrent_hashrequest.go`; tests verify the reply against
the file root and, for every servable request (zero padding past the end of the
file included), compare it byte-for-byte with the full block tree
(`torrent_hashrequest_serve_test.go`). The same path answers a complete file
while its block tree is still being built, and a request is capped at 512
hashes so the leaves read on the torrent loop stay bounded.

**WP4e (adversarial).** Hostile hash-request headers (bogus root, bad length,
huge proof, overflow) are refused without panic; the WebSocket frame fuzzer is
in `make fuzz-smoke`.

**Decision summary:** external DHT → **close**; anti-Sybil cap → **hint only**;
multi-path → **close with data**; choking → **gated on a real-swarm campaign**;
10 Gbps → **close with data**; disk I/O → **close with data**; **v2 partial-file
hash serving → done**.

---

## 12. Web streaming, DLNA media server and the choking/seeding campaign (2026-10-14)

### 12.1 Web streaming (improved)

`cmd/gx-torrent/stream.go` (the endpoint Gextto redirects to) now:

- sends the **Content-Type from the file extension** (`video/mp4`,
  `video/x-matroska`, `audio/flac`, …) plus `Content-Disposition: inline`, so a
  browser plays the stream instead of downloading it, and answers **HEAD**;
- keeps a shared **serveTorrentFile** core, reused by the UI player and by the
  DLNA server;
- refcounts the concurrent streams **per file** (`beginStream`/`endStream`;
  the engine windows are per file), so a player closing does not clear the
  stream window another reader of the same file still needs; a HEAD probe does
  not touch the windows, and `Content-Disposition` is RFC 6266-encoded.

Tests: `TestMediaContentType`, `TestContentDisposition`, `TestStreamRefcount`,
`TestParseByteRange`. No data-path change.

### 12.2 DLNA/UPnP media server (new)

`cmd/gx-torrent/dlna.go` + `dlna_ssdp.go`: an **opt-in** DLNA/UPnP AV media
server that exposes the **completed** torrents' media to a TV/console on the
LAN. SSDP (M-SEARCH replies, alive at start and every 15 min, byebye) +
ContentDirectory:1 (`Browse` with BrowseMetadata/BrowseDirectChildren and
paging, a content-derived `SystemUpdateID`) + ConnectionManager:1 + GENA
`SUBSCRIBE` with the initial event; media served with HTTP Range by the shared
core plus the DLNA `contentFeatures`/`transferMode` headers; subtitles attached
to the video with the same name (`res` + `sec:CaptionInfoEx`). Off by default,
no authentication, **LAN-only by construction** (HTTP and SSDP from a
non-local address are refused) and `/res` serves only the files the listing
offers. Settings `dlna-enabled`, `dlna-listen`, `dlna-name`: the **daemon
page** in standalone (with the running URL shown), the **Gextto** options
`gxtorrent_dlna*` in managed (passed as flags/env); an address or name change
restarts the server. **Library folders** (`dlna_library.go`): the archive on
the NAS is shown beside the torrents as a browsable folder tree (Gextto passes
its archive root and series archive folders automatically, trash excluded),
read through `os.Root` (no `..`/symlink escape), NAS system folders skipped,
listings cached 30 s; on the real NFS archive 125 series list in ~0.5 s and a
5 GB 2160p episode streams by Range. **Artwork** (`dlna_art.go`): videos and
folders carry `upnp:albumArtURI` (`JPEG_TN` + `JPEG_SM`) from the Jellyfin/Kodi
images beside the media (`-thumb`, `-poster`, `folder.jpg`, season posters
`season01-poster.jpg` in the series folder), scaled in Go with a box filter and
an LRU cache of 8 MB; measured on a 1080p still → `JPEG_SM`: 34 ms, 4.1 MB
allocated (12.4 MB before averaging the JPEG YCbCr planes directly instead of
converting to RGBA). Verified end to end against a real daemon (SSDP
discovery, Browse, Range 206, a non-offered file refused). Open: transcoding,
frames extracted from the video (no ffmpeg), search, Xbox/WMP (`X_MS_MediaReceiverRegistrar`).

### 12.2b In-app updates for gx-torrent and Gextto (new)

**gx-torrent standalone** (`cmd/gx-torrent/update.go`, `internal/updatecheck`)
updates itself from *Settings → Maintenance*: a timer (`update-check-hours`,
default 6) or **Check for updates** reads `release.json`; the manifest now
flags the commits that touch the daemon's code (`"gx"`, `gx_torrent_commit`),
so only real daemon changes are offered. **Update** downloads the release
archive, verifies SHA-256, test-runs the binary, swaps it (keeping `.prev`),
re-executes in place (same PID) and shows the new commits in a popup; three
failed starts restore `.prev`. Off when managed by Gextto and for local builds
(release channel stamped only by `build-release.sh`). Verified end to end with
two real binaries and a local release: check → **Update** → restart in the
same process (1.1.500 → 1.1.501, PID unchanged) → popup → confirmed after 30 s
→ "up to date".

**Gextto**: the check period is a setting (`update_check_hours`, default 6),
the panel has one button that reads **Check for updates** and becomes
**Update**, and the version that comes after an update shows a **Gextto
updated** popup with the commits it brought (`update-notes.json`, written when
the update is requested).

### 12.3 Choking/seeding campaign

- **Local reciprocation.** Without mesh the seeder uploads **3.00×** the corpus
  (3 pure leechers). With mesh the leechers **reciprocate** (2.52 MB uploaded
  among themselves) and the seeder's upload drops to **1.84×** (−39 %).
- **Controlled vs libtorrent.** `TestMeasureChokingLibtorrent` (`scripts/
  libtorrent-leech.py`): our seeder serves a real **libtorrent** leecher on
  loopback — **48 MiB uploaded (100 % of the file) in 4.84 s** (~10.4 MB/s), the
  leecher verified complete.
- **Real swarm.** `TestMeasureChokingRealSwarm` seeds the Debian ISO for 3 min:
  **0 peers**, because seeding needs an **inbound** port and this host is
  behind NAT with no reachable port (the earlier download run found 80 peers
  outbound). The harness is kept for a host with a reachable port.

**Decision: no choking change.** Reciprocation works locally and an external
libtorrent client is served well; the only missing datum (a real swarm where
peers download *from* us) is blocked by the test host's NAT, not by the
algorithm. The campaign stays as the gate for any future change.


