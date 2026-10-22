# Improvements and evolution of Gextto and gx-torrent

What Gextto and gx-torrent are today and what is next: the capabilities as they
exist and the open items with their gate. The numbers behind the choices are in
[`measurements.md`](measurements.md).

The technical reference of the engine is [`gx-torrent.md`](gx-torrent.md), its BEP
compliance [`bep.md`](bep.md), the engine inventory
[`../internal/gxcore/GEXTTO.md`](../internal/gxcore/GEXTTO.md) and the standalone client
[`gx-core.md`](gx-core.md).

## Index

**I. What Gextto is**

1. [Positioning and strategic direction](#1-positioning-and-strategic-direction)
2. [Acquisition and library](#2-acquisition-and-library)
3. [Product surface](#3-product-surface)
4. [Indexers: the manager as the single source](#4-indexers-the-manager-as-the-single-source)
5. [What Gextto deliberately does not do](#5-what-gextto-deliberately-does-not-do)

**II. The engine (gx-torrent / gx-core)**

6. [What the engine has and what is missing](#6-what-the-engine-has-and-what-is-missing)
7. [The engine is our own code](#7-the-engine-is-our-own-code)

**III. What is next**

8. [Open items and the measurement-gated program](#8-open-items-and-the-measurement-gated-program)

The numbers themselves are in [`measurements.md`](measurements.md): the harnesses, the
benchmarks and the footprint, the swarm, the DHT, the merge on real data and the
accessibility verification.

---

## Part I — What Gextto is

### 1. Positioning and strategic direction

Gextto acquires, renames and archives series, movies and comics: a single Go daemon
with a SQLite database, an integrated torrent engine and a server-rendered web UI.

- **For new installations, not a drop-in replacement for Sonarr/Radarr**: no import
  of their databases, no in-place migration, no rollback. It is not an `*arr` under
  another name.
- **Toward the indexer managers, not instead of them** (Jackett/Prowlarr): the
  manager stays the only source of truth for the indexers, read at run time and
  never copied into the configuration; Gextto searches its indexers one at a
  time, with the aggregate as the fallback when the list cannot be read (§4).
- **Field**: whoever has a server with a media server (Jellyfin/Plex) and wants
  "find, download, rename, archive" from a single daemon; very likely they already
  have Jackett or Prowlarr.
- **Selection policy**: global defaults (resolution, source, codec, grouped
  audio with separate channels and object audio, HDR, language, size) plus
  **per-title overrides and exclusions** (including minimum audio and channels)
  plus
  **explainability** — the explanation API says why a release is accepted, rejected
  or preferred ("Why not this one?" in the UI). Named quality profiles exist
  only as **templates**: their values are copied into a title, whose own fields
  stay the only rule; *Edit selected* in the Series and Movies tables changes
  many titles at once. Profiles that
  govern their titles, ordered qualities with a cutoff and a custom-format regex
  engine like `*arr` are not adopted: the per-title fields already express a
  minimum, a maximum or a range of resolutions, the languages, the exclusions
  and the upgrade policy, and that model is the most complex — and the most
  misconfigured — part of `*arr`.
- **Non-goals**: `*arr` `/api/v3` compatibility and the quality-profile/
  custom-format semantics; multi-user authentication (remote exposure goes through
  a reverse proxy, `docs/SECURITY.md`). The differences with Sonarr/Radarr are
  published, not hidden.

**Where Gextto is ahead or on par:** series, movies **and comics** in a single
daemon; the torrent engine integrated (gx-torrent in pure Go, or
qBittorrent-nox, §6); explainability of the choices; search also from web engines;
lightness (a Go binary and SQLite against a .NET stack); simplicity (health, backup
and maintenance built in).

**Honest snapshot against Sonarr/Radarr:** they are still deeper at **selection**
(ordered qualities, cutoffs, custom formats, a delay per profile) and at the
**ecosystem/API** (full `/api/v3`, of which Gextto implements the Jellyseerr
subset). Gextto now has **named profiles generated from its per-title
overrides**, **manual import** (matching an existing file, choosing what it
replaces and where it goes) and the v3 subset; it parses multi-episode, specials
and daily releases, records every episode of a multi-episode file and names it
with the episode range. It is better at single scope, integrated engine,
explainability, footprint and simplicity. The verdict is unchanged: **an
alternative for new installations**, without claiming parity. What remains is in
§8.

**Differences that are choices, not gaps:** indexers through an external manager
(cleaner and safer separation) and **no usenet** (torrent, HTTP and web seed; in the
Italian context usenet is marginal).

### 2. Acquisition and library

**Hardlinks instead of copies during seeding** (`hardlink.go`, `hardlink_seeding`,
on by default). Episodes in a folder, season packs and episodes in copy mode enter
the library as hardlinks: the link is created with a hidden name and then renamed,
so the torrent file is never touched. When the link is impossible (different
filesystems, RAM disk, shares without hardlinks) the file is copied as before, with
a log line per pair of disks. The two names are peers, not like symlinks: deleting
the name in the download leaves the archive intact (only the seed is interrupted),
and the data disappears only by deleting both. Movies are not involved: they are
already moved with the torrent, so there is no copy.

**Upgrades** (`upgrade_until.go`, `upgrade_until_score`, 0 = off): with a score at
or above the threshold the copy is no longer replaced; REPACK and PROPER are always
accepted. It applies to the cycle, missing episodes, season packs and movies, and
"Why not this one?" lists the rejections by threshold. Reference scores: 1080p
WEB-DL H.264 ≈ 1280, 1080p WEB-DL H.265 DD+ ≈ 1480, 2160p ≈ 2480. The minimum
difference (`upgrade_min_score_diff`) remains, deciding *by how much* a release must
improve against *up to where* it is accepted, and the per-title "No upgrade" block
is in the series and movie forms, in the add form and in the quality profiles. A
"score" upgrade also needs the technical quality score (configured weights, no size,
subtitle or swarm-health bonus) to improve by the minimum difference, so a
same-quality release never replaces an archived copy. The score fingerprint includes
`scoreAlgorithmVersion`: a change of the built-in scorer or parser rescores the
library once, like a change of the weights. *Impatto sulla libreria*
(`upgrade_impact.go`) replays the archived feed releases through the dry-run series
decision and reports the episodes they would replace.

Before a completed single episode or movie that replaces an archived copy is moved,
linked or copied into the library, `mediainfo_gate.go` probes both files with
ffprobe and compares the measurable attributes (resolution, video codec, audio
family and channels, HDR10/HLG); an upgrade the streams do not confirm is
discarded, blocklisted and the archived row restored. Source, Dolby Vision and
REPACK/PROPER stay as named. Without ffprobe the earlier decision stands.

Open point: season packs are not probed before the rename; their files are still
compared by name after placement. Gate: a per-file probe that does not slow the
completion of a large pack.

**A pack is downloaded only when it is useful.** A whole-season pack may be approved
only to fill a real gap or to upgrade an existing episode: its target episodes come
from the same archive gap pass the cycle uses (`ArchiveGapsForSeries`, which already
excludes the seasons a complete pack covers, the ignored episodes and the active
downloads), never from the raw TMDB episode count — that count also holds
unaired/planned episodes (13 for a 9-episode season) and would approve a pack that
adds nothing, downloaded and then rejected file by file at import. A pack the title
marks *"IN CORSO n/m"* (an airing season) is read with the range 1..n instead of the
complete-season sentinel, so it fills a gap only among the episodes that exist. A
user-initiated pack keeps the broad range. The content the pack physically holds is
still only known at import (`StagePackFile`), where a pack whose every file is worse
than the library copy is rejected and its source moved to the trash.

While a pack is downloading, the optional inspection (`pack_inspection`) reads its
file list as soon as the metadata arrives and compares it episode by episode with
the library: a pack with nothing useful is removed and **its upgrade backup is
restored**, so the row of a discarded release does not stay in the library with the
score of the pack that was thrown away. A partly useful pack downloads only the
wanted files (`pack_inspection_prioritize`). The log names both sides of every
verdict (existing quality/score against the candidate) and lists the kept and
skipped episodes, and a metadata wait reports peers, received bytes and the
absolute start of the current wait, so a restarted ten-minute counter is not
mistaken for a loop.

**A dead `.torrent` link is left alone.** A release that only offers an HTTP
`.torrent` link (no magnet) is unusable when the link keeps failing — an indexer
whose session is expired, a proxy answering 502, a torrent withdrawn from the
site. The archive would propose the same URL on every cycle, so the failure
repeats forever and the cycle never counts as clean. A link that fails in two
different cycles is quarantined (`dead_torrent_links`, `torrent_link_quarantine.go`)
and skipped before the score, so it cannot win over a working alternative. The
wait grows with the strikes — 6 hours, 24 hours, then a week — and stays short
at first so a repaired indexer is noticed the same day; a success deletes the
row. Failures inside the same cycle (the archive and a feed proposing the same
release) count once. An answer that means "not now" is not a strike: mircrew's
404 `no magnet for this request` (the topic's magnet is hidden until its first
post is thanked) and a login failure both leave the link eligible and retry it
on the next cycle (`errTorrentNotAvailable`), so a valid release is not hidden
for hours by a transient state. Those transient answers still count towards
the source notice below: a login that keeps failing must reach the operator.
A source that fails on two different links in
one cycle is reported on its own: a warning in the log and, at most every 12
hours, a notification naming the source and the action — sign in again
(mircrew answers 502 with "MirCrew login failed: credenziali rifiutate") and,
until it works, disable that indexer in the searches. A link that resolves
re-arms the source (`indexer_failure.go`).

Changing a score weight from the settings page or the API leaves every stored score
computed with the old weights, which would look like a genuine improvement and
re-download the library. The cycle recomputes them before it compares anything
(`SyncScoresWithSettings`, guarded by a weights fingerprint, so a normal cycle does
not touch the library).

**Starting a download survives a crash** (`torrent_add_intent.go`). A release the
cycle approves is recorded in three steps — the engine add, the approval in the
library (placeholder or upgrade backup) and the `torrent_meta` row — and all of
them go through `startApprovedRelease`, both in the cycle and in the missing-episode
priority pass. Before the add an *intent* is written (`torrent_add_intents`) and it
is cleared once the outcome is recorded; a failure at any step withdraws the torrent
and undoes the approval. If the process dies in between, the next cycle resolves the
leftover intents against the engine snapshot: a torrent that only lacked its row is
registered and goes on downloading, one whose approval never reached the library is
taken out of the session keeping its data (a new add of the same release resumes it),
and a placeholder whose torrent is gone is rolled back so the episode is searched
again. An empty snapshot from a backend that cannot report its health is not taken
as evidence: those intents wait. The decision itself is still evaluated twice on
purpose — a read-only evaluation before the add and the committing one after it —
so a refused add writes nothing and the commit never trusts a stale state.

**Swarm health in the choice, not in the quality score.** The cycle score is
quality + subtitles + size; the swarm enters the acquisition decision only, through
`AcquisitionScore` (`config.go`): a definitively empty swarm (0 seeders and 0 peers) is
discounted below the upgrade margin so it cannot claim a slot over a viable
alternative, a swarm with only leechers is discounted mildly (leechers still complete
each other), and a healthy swarm earns a small bonus capped below
`upgrade_min_score_diff`. A source that does not report seeders (the `-1` sentinel,
most RSS/feed releases) stays neutral, and the adjustment is never folded into
`ReleaseScore`, so archived files and the UI keep the plain score. Quality stays
sovereign: a dead 4K still beats a healthy 1080p.

**A vanished library volume is not a mass deletion.** Resetting an episode because
its file is gone (`archiveFileConfirmedMissing`) is bounded: at most
`archiveResetBurstLimit` rows (20) may be made eligible again within
`archiveResetBurstWindow` (10 minutes), after which the cycle keeps the remaining
records and warns once to check the mount. The post-processing destination probe
(`ValidateDestination`) runs under a timeout (`destinationProbeTimeout`, 30 s), so a
hung network share reports an error and is retried instead of blocking the torrent
event worker forever.

**What counts as missing**: an episode with an active torrent — added by hand, from
a watched folder or from the phone — and a season with a season pack in progress are
not missing, neither in the cycle nor in the series tab; a download in error makes
them missing again.

**Re-downloading an episode.** The ↻ *Re-download* action in the series detail moves
the archived file to the trash, deletes the episode row and records a **forced gap**
(`forced_gaps`). A forced gap bypasses the cover of a season pack and the "a later
episode is already archived" rule, and clears itself as soon as the episode is present
again, so it is the one path that re-fetches an episode already in the library. An
`SxxE00` special shares `episode=0` with season packs in the schema, but it is a single
file and does **not** cover the season (a real `S01`/`S02` pack still does).

**Comics** (`comicinfo.go`): in CBZ files downloaded directly over HTTP or Mega,
`ComicInfo.xml` is added with series, number and year from the GetComics title.
Files that already have it, CBR files and comics arriving by torrent (being seeded)
are left untouched, and an error never compromises the download.

### 3. Product surface

The manual describes the pages; here are the capabilities and the design rules that are
not obvious from them.

**Web UI and TUI.** Server-rendered with HTMX (`uiweb/v2`), no Node build, plus a
terminal client (`gexttod tui`) that compacts below 60 columns for an SSH session on a
phone. Six languages (it/en/de/fr/es/pl) cover the web UI, the API/HTMX errors users
see, notifications, the TUI and the daemon page; logs stay in English. Guards:
`TestUIIsTranslatable`, `TestClientI18nKeysTranslated` and one catalogue check per
language.

**One validation for every write.** The API, the settings page and the wizard all pass
through `validateSettingValue`. A typo such as `score_res_1080p=abc` is refused instead
of being stored and silently disabling a rule; an empty value is valid and restores the
default. Guard: `TestNumericSettingRejectsGarbage`.

**Settings that explain themselves.** Read-only insights
(`uiweb_v2_settings_insights.go`) sit next to the controls: the Sorgenti tab links
indexers, feeds and web engines with the active count and the last cycle outcome; the
stuck-download panels show one timeline; the Qualità e upgrade tab leads with the upgrade
rules, scores reference releases with the current weights, tests a release name against
the library copy and reports the impact of the current rules on the library; the weight
cards follow, closed. Settings search also reaches the indexer editor by concept
("Prowlarr").

**Guided tuning.** The performance tab proposes values from RAM, CPU, volumes and storage
class (`tuning.go`, `system_probe.go`, `sysinfo.go`): current → proposed, with the reason.
Nothing is written until the user applies it, and every write goes through
`validateSettingValue`. `setting_provenance` records `auto` or `manual`: a value the user
saved is never proposed again. An adaptive loop refits only `auto` values at the start of
a cycle, past a hysteresis. The peak observer learns the line only from an uncapped
direction and proposes 80 % of it; the free-space guard is bounded to 20–50 GB; a restart
that changed the binary is classified as an update, not instability (`restart_cause.go`).

**Two configuration levels.** *Facilitata* shows the limits the operator owns (folders,
speed and space, seed policy, access, metadata keys) and hides what the system manages;
*Esperto* shows everything. It is one filter over the same index, remembered in a cookie;
the settings search follows the level. Default is Esperto, the wizard sets Facilitata.

**Library and dashboard.** Series and movies can be read as a table or as poster cards
(cookie-remembered, covers from TMDB/TVDB with a half-day cache). The dashboard draws the
last 30 minutes of bandwidth as a server-rendered SVG from a bounded ring sampled every
10 s, refreshed by the chrome SSE stream.

**The standalone page** is laid out like qBittorrent's: filters on the left, one line per
torrent, the detail panel below, a full context menu, per-user columns and widths kept on
the server, controls usable from the keyboard, an 8-second undo after a remove, and the
table refreshed as JSON (`/ui/rows`, about six times smaller than the HTML rows). Details
in [`gx-torrent.md`](gx-torrent.md), *Web interface*.

**RSS and Wanted.** Nothing is decided out of sight: a rule's preview updates as it is
typed and names the criterion that leaves other archived items out; a feed row says which
rules decide its downloads; templates and a feed test show a change before it lands.
Rules are ordered PASS/FAIL with regex, include/exclude, size, seeds, age,
`require_episode` and smart-episode, and the same separator-insensitive term match
(`rss.HasTerm`) across *all of* / *one of* / *none of*. A wanted title lists what is
downloaded, downloading and seen-but-skipped, with a chip per season that is never
collapsed; the episode grid (`/ui/wanted-episodes`) is the ledger. Details in
[`gx-torrent.md`](gx-torrent.md), *RSS and Wanted*.

**Accessibility.** Reference WCAG 2.1 AA (compatible 2.2 practices): landmarks,
accessible names, dialog focus, keyboard sort, live regions, contrast-checked themes,
reflow and reduced motion. The automated baseline (14 axe-core tests) passes; it is not a
legal certification, and manual screen-reader testing is still required
([`measurements.md`](measurements.md#accessibility)).

**Access and security.** Login is off by default; when on, clients outside the LAN must
log in (bcrypt, 30-day signed cookie) or send the API key (`X-Api-Key`, `?apikey=`, or
`GEXTTO_API_KEY` for the TUI). Forwarded addresses count only from declared proxies; five
wrong passwords block the IP for a minute; `GEXTTO_AUTH_DISABLE=1` is the way back in.
Cross-site writes are refused (`security_origin.go`, `net/http.CrossOriginProtection`),
which matters because the LAN bypasses the login.

**Media servers, notifications, calendar.** Jellyfin gets `/Library/Media/Updated`, Plex a
targeted section refresh, with path mappings for Docker and merged requests; notifications
are translated and the webhook speaks Discord, Slack, ntfy, Gotify and Pushover. The iCal
feed distinguishes arrival in the library, original air date and the Italian release of a
movie, with a declared fallback to the original date.

**Health and history.** Health shows the searches per cycle (duration, outcome, failed
sources, with warnings when a source keeps failing, a search takes over three hours or no
clean cycle completes for three intervals), the delay-profile queue and the failed imports
per day. Torrent log lines carry an `acq:` ID and are stored in `acquisition_events`
(History tab, per-episode, series panel, `/api/acquisitions`), kept 180 days. The
*Downloading* entry counts unfinished torrents plus active HTTP, not seeds.

**Speed policies: a rule and an exception.** The rule is one choice in Configuration
(`torrent_speed_mode`, not stored: it reads and writes `torrent_speed_auto` and
`torrent_sched_enabled`, so older settings need no migration): Auto, fixed limits, or a
per-weekday schedule with one choice per day (Off, Limit or Auto) and, outside the
windows, Auto or the fixed limits. The exception is set from the Bandwidth menu in
Downloads: Auto or a limit, for 1/3/6/12 h or until removed. It wins over the rule,
schedule included, until it expires or is removed (`speedPolicy` tries the exception,
then `speedRule`); one model, so a limit never silently loses to a schedule window. Auto
is one name for one mechanism: the guided tuning measures the line, the latency guard may
cap it while the ping is high and ramps it back up when the line is at rest, so the peak
is learned when the guard is free.

**Latency guard (Auto respects latency, on by default).** While Auto is in force,
`latency_guard.go` probes Google and Cloudflare every 2 s (8.8.8.8, 8.8.4.4, 1.1.1.1 and
1.0.0.1 on port 443, TCP connect) and takes the **lowest** RTT: one server being far,
slow or unreachable cannot fake a bad line. The current latency is the median of the
last five samples (10 s); the idle reference is a trial's measure, kept for 6 h, or the
lowest of the 10-minute window. A high latency (above `baseline × 1.5` **and**
`baseline + 20 ms`) starts an 8 s trial at 60 %; after it the controller ramps **up**
(+25 % or 512/64 KiB/s every 3 s) while the ping is good and cuts 15 % when it degrades,
with floors of 128 KiB/s down and 64 KiB/s up. It does not release just because the
latency fell: that is the moment the line can be pushed again. The ceiling is the
**line maximum entered by the user** (`torrent_line_max_down_kib`, `_up_kib`; 0 =
unknown), or twice the rate actually delivered when the line is unknown — there is no
hard-coded speed cap. An idle line or leaving Auto drops the cap.

The router is optional: `torrent_auto_latency_source` is `both` (default: the ping
decides, the UPnP WAN counters are only a diagnostic), `ping` (the router is not read at
all), or `router` (legacy: yield only to the devices the router sees, release when they
leave). Background noise below 256 KiB/s is not "someone else"; the detector needs a
majority of five reads. Peaks below 128 KiB/s are not recorded, so a dead swarm cannot
become the tuning's ceiling. The first cap of a session is logged at once with the
speed, the line maximum and the ping; the periodic line adopts it as its last
one, so the same numbers are not repeated. After that an autotune step is
considered at most once a minute, only when the speed changes by at least 10 % and
the trend confirms it (a small reversal waits, a reversal of 20 % or more counts).
The info log gets the first step of each mode (trial, control, free) and then one
digest every 15 minutes — number of steps, download and upload range, current caps
— while every single step stays in the debug log: the control loop moves the cap
every minute and a line per step buried the rest of the log. The Downloads badge
shows latency, reference and caps; `GET /api/torrents/temp-limits` carries
`latency_guard`. Measurements: [`measurements.md`](measurements.md) §4.5 (historical
figures, from the trial/router design).

**Streaming and DLNA.** The streaming endpoint sends the `Content-Type` from the file
extension, answers HEAD and refcounts readers per file. The DLNA/UPnP server is opt-in:
SSDP, ContentDirectory, ConnectionManager, GENA events, HTTP Range, subtitles, library
folders on the NAS and artwork from the Jellyfin/Kodi images; LAN-only by construction.
Details in [`gx-torrent.md`](gx-torrent.md), *DLNA media server*.

**In-app updates.** gx-torrent standalone checks `release.json` (only commits that touch
the daemon are offered), verifies SHA-256, test-runs the new binary, swaps it keeping
`.prev` and re-executes in place; three failed starts restore `.prev`. Gextto has the same
check period and a popup with the commits it brought. Local builds do not self-update.

### 4. Indexers: the manager as the single source

The manager (**Jackett/Prowlarr**) is the only source of truth for indexers,
categories, credentials, FlareSolverr and rate limits: Gextto stores none of it.

**Searched one indexer at a time** (`indexer_lanes.go`). The manager's list is read at
run time, cached for 10 minutes and never copied into the configuration: Prowlarr
`/api/v1/indexer` with `/api/v1/indexerstatus`, Jackett Torznab `t=indexers`. Each
indexer becomes a **lane**: Prowlarr `/api/v1/search?indexerIds=<id>`, Jackett
`/api/v2.0/indexers/<id>/results/torznab/api`. The reason is how the managers behave.
Prowlarr serves each indexer one search at a time, an aggregate answers when its
slowest indexer has, and a search Gextto abandons at its timeout **keeps running
inside Prowlarr** and queues the next ones behind it. Six concurrent aggregates with
timeouts snowball until even a single search waits 120 s (measured, recovered once
the backlog drained). Each lane therefore has its own slots: 1 for a Prowlarr indexer,
which serves no more anyway, and 2 for Jackett, which answers in parallel and caches.
At most one search per Prowlarr indexer is ever outstanding. Each lane also has its own
timeout (90 s for Prowlarr, since waiting costs less than abandoning; 45 s for Jackett)
and its own health: the backoff and the cycle warning name "prowlarr · TorrentLeech",
not the whole manager. When a lane's queue, at its average time per search, would
outlast what is left of a title's budget, that title skips the lane at once; this is
not a failure, and a slow tracker serves the titles it can without holding the cycle.
A tracker listed in both managers (here LimeTorrents and TorrentLeech) is searched
**once**, through the lane that answers faster. A lane never measured is tried first,
so each is tried once. The indexers Prowlarr reports as failing are skipped, the
capabilities are each indexer's own instead of the intersection an aggregate needs,
and an interactive search does not queue behind the cycle's. If the list cannot be
read, the manager is queried as an aggregate, as before. The earlier prototype that
**duplicated** the indexer list into Gextto's configuration (reverted below) is not
this: nothing is duplicated, the list is read from the manager every time.

- The manager type can be **declared** (`manager` in `config.go` and the UI) instead
  of inferred from URL, port or name; the inference remains as fallback.
- A **timeout per source** keeps a single request below the search budget (an
  aggregate fallback gets 90 s, the title budget is 100 s), and the
  manager's health (Prowlarr `indexerstatus`) is shown in *Sources*: a broken or
  disabled indexer is visible instead of giving zero results in silence
  (`indexer_health.go`).
- **Categories were removed**: an aggregate serves both series and movies, so a
  static category per indexer is useless or harmful (a TV query with the "movie"
  category returns nothing).
- A prototype that duplicated the indexer list was built, measured and **reverted**:
  no stability gain, only configuration drift.
- Prowlarr's search API **ignores** season/episode and external-id (measured): they
  are not forwarded, and `type` has no observed effect either. **Jackett's** own
  indexer API redirects to the login, but its Torznab `t=indexers` lists the
  configured indexers with the Torznab key (what the lanes use); Jackett exposes Torznab at
  `/api/v2.0/indexers/all/results/torznab/api` instead of the generic `/api` (404),
  and `internal/torznab` infers the right one from the `:9117` port or from the
  name, with an automatic fallback on 404, shared between Gextto and gx-torrent.

**Measurements (same library, same host).** The numbers behind the choices:

| Scenario | Latency | Results |
|---|---|---|
| Aggregate with a broken indexer enabled | 60 s (timeout) | 0 |
| Healthy indexer called directly | 0.25–2.4 s | 40–100 |
| Aggregate after disabling the broken one in the manager | 2–4 s | 156–240 |

**FlareSolverr.** Persistent sessions per domain, with a short TTL and destruction,
instead of a browser per request; when the mode is not supported it falls back to
stateless. It is **never used for the indexer managers**: Jackett and Prowlarr handle
Cloudflare on their own, and a 403 or network error there is reported as it is
instead of wasting 20–30 s masking the cause. `solution.status` is respected: a page
"solved but in error" is not interpreted as content.

### 5. What Gextto deliberately does not do

Recorded with the reason, so the ground is not re-covered:

- **`*arr` `/api/v3` compatibility**, quality profiles and custom formats: the
  differences are published instead (§1); what a partial compatibility would require
  is in §8.
- **Multi-user authentication**: remote exposure is the reverse proxy's job
  (`docs/SECURITY.md`).
- **Usenet**: torrent, HTTP and web seed; in the Italian context usenet is marginal.
- **Write-back cache in the engine**: the measurement gives `write_cache=0` (storage
  and download stay in step), so there would be no gain; to be reopened only with
  storage genuinely slower than the network.
- **Specialist fields in the add form**: no request, it would be preventive fluff.
- **A content layout in Gextto's Add dialog**: Gextto addresses a download by its
  content root and renames it into the library anyway, so the layout of the download
  folder buys nothing there and a root-less download would break the import. The
  engine has it (Original, Subfolder, NoSubfolder) for gx-torrent standalone and its
  qBittorrent API (§6).
- **A UI benchmark as a performance test in CI**: SSR rendering costs ~1–3 ms/page,
  it is not a problem.
- **A cache cap on slow storage (NFS)**: the cache self-regulates
  (`cmd/gx-torrent/cache.go`, tuner `cacheSizes`).
- **Trakt**: deliberately removed (`TestTraktIntegrationRoutesRemoved`).
- Already present, listed because they were asked for: webhook HMAC, event hook,
  title aliases, blocklist, delay profiles, season packs, subtitles in the score,
  Discover/Popular TMDB.

---

## Part II — The engine (gx-torrent / gx-core)

### 6. What the engine has and what is missing

`gx-torrent` is the BitTorrent engine in **pure Go**: the **gx-core engine**
(`internal/gxcore`) plus the **daemon and adapter** (`cmd/gx-torrent`,
`gxtorrent_engine.go` — REST, web page, self-managed queue, the `TorrentEngine`
contract with Gextto). The per-capability state lives in `capabilityLevels`
(`torrent_engine.go`), the single source of truth for the UI: an operation a backend
does not have is refused with `ErrCapabilityUnavailable`.

Around every engine Gextto provides queue/slots, stalled/retry, seed policy (ratio,
time and inactivity, resolved torrent → seed rule by source/category → global), RSS,
search, categories/tags, the bandwidth scheduler, post-processing, moves and the RAM
disk, the IP filter and the proxy: **those are not engine gaps**, and limits follow
the libtorrent semantics (**−1 = inherit the global one, 0 = unlimited**).

**What the engine has.** Protocol: BitTorrent v1 and v2-only (from `.torrent` and
magnet, BEP 52), dual-stack IPv6 (`peers6`, PEX v6, DHT BEP 32, LSD IPv6/BEP 14) and
uTP. Per torrent: download/upload limits, connection and upload-slot limits, per-piece
priority and diagnostics, **graduated file priority 0–7**, the qBittorrent **content
layout** at add time (Original, Subfolder, NoSubfolder), tracker removal and hot
add/remove, tracker scrape (BEP 48, HTTP and UDP), a runtime sequential toggle, web
seed, **super-seeding** (BEP 16) and **holepunching** (BEP 55, peer relay over uTP). Port
mapping and NAT: **PCP** (RFC 6887, our own minimal client, tried after UPnP and before
NAT-PMP), UPnP and NAT-PMP, and NAT detection (`portcheck`: `reflected_port`,
`nat_mapping`, `natHint`) — the BEP 42 `ip` field does not arrive on the public DHT
(0 observations), so the datum shows only where nodes include it. Storage: mmap opt-in
with a **ranged flush**, `O_SYNC` removed with `fdatasync`, bad-storage and write-error
diagnosis in the error line, a guard that turns an mmap fault into an I/O error on the
torrent, and a **disk guard** that pauses and resumes downloads when a folder falls
below the free-space threshold while seeding continues. Streaming: a reader with seek
and readahead with graduated piece priority (stream > readahead > rarest). Request
queue: adaptive depth per delivered block, reset on choke; **two pieces per peer** with
a shared in-flight budget; a bounded per-torrent write ring buffer. Session: an engine
event bus (SSE `GET /ui/events`), a global in-flight budget and a deterministic drop of
the worst connection; `.torrent` creation from a folder (`CreateTorrent`, form and API).
DHT: our own module (`internal/dht`, BEP 5/32/33/42/43/44/46) with a bucket-less
binary-tree routing table, host names (bootstrap routers, a magnet's `dht=` nodes)
resolved off the main loop so a hung resolver never stalls it, a non-blocking `AddNode`
(a peer's PORT hint is dropped rather than waited on), tokens bound to source address and scope, an opt-in
anti-Sybil cap per subnet, and the **feeds** of BEP 46. Robustness: parser fuzzing in
CI (`make fuzz-smoke`, 23 targets, seeds in `testdata/fuzz`), a bounded bencode screen
before the decoder on every hand-written path, snub and session ban with TTL, IP
filter, and hostile hash-request headers (bogus root, bad length, huge proof, overflow)
refused without a panic. Diagnostics: uTP socket, dial, accept, error, RTO and SYN-drop counters, merge
metrics (`merged_pieces`/`merged_bytes`) and storage errors flow from the engine stats
to the daemon API into **both UIs**.

The **standalone daemon** adds: *Wanted* (type, seasons, language, quality, exclusions,
episode filter, category and path; season packs count as their whole season;
added by hand or with **Search the feeds**, a register
in `state.json` with a throttled gap search, and no feed generation so the feeds stay
free for the advanced rules), the RSS panel (per-feed state, synchronous reload,
enable/disable, configurable interval, ordered PASS/FAIL rules with `once` and a preview
on the archive), **HTML listings as feeds** (ext.to and TorrentGalaxy pages resolved to
direct magnets and feed links instead of failing on invalid XML), the **feed archive**
(bbolt with a compact index), the IP filter with a scheduled refresh, *Optimise*
(queue, cache, bandwidth at 80% of the measured peak with auto-speed active), the
**per-day bandwidth scheduler** (`internal/bwschedule`, shared with Gextto: every
weekday has its own window, with **one choice per day — Off, Limit or Auto** (Auto
leaves the cap to the auto-tune so the line is measured there), a window past midnight
belongs to the day it starts on, and the older single window stays as fallback),
notifications on Gextto's
channels (`internal/notify`: Telegram, email and webhook in the JSON, Discord, Slack,
Gotify, ntfy and Pushover formats, each switchable with a test button), and the
standalone pages' usability (Browse… on the folders, the port test, the multi-folder
DLNA library, Gextto's blocklist as the default IP filter, export/import of the RSS
setup, `/regex/` wanted titles, the *Update available* indicator in the status bar, the
**Statistics** page with this session and all-time totals — kept across restarts —,
disk and cache, the network, the queue and a speed graph over 1 h/24 h/7 d).
The standalone **self-update** is described in §3.

**Swarm Merging** (file sharing between torrents). Different swarms holding **the same
file** collaborate: a torrent reuses the pieces another one already has instead of
downloading them again, which revives seedless torrents when the content exists
elsewhere in the session. The match is by **structure** — an identical piece layout, or
an equal v2 per-file Merkle root — never by the BEP 38 `similar`/`collections` hints,
which are only read and reported: the structure is what makes the reuse safe. The
reused bytes are always **verified against the destination's own hashes** before being
written, so no unverified byte reaches the storage. What is in place: **Phase 1** (same
layout, A's hash verifies the piece), **Phase 2** (v2 per-file Merkle root, so identical
files with different layouts), **Phase 3** (reuse at **16 KiB block** granularity from
partially covered pieces, `storePartialBlocks`/`prefillBlocks`), **Phase 4** (two v1
torrents with different piece lengths, matched by file path and length, whole pieces
only, a candidate dropped after 64 failed verifications), the **resume path** (the merge
also runs when a torrent is reloaded from resume data, so a restart does not lose the
block-level reuse) and the **later-sibling trigger** (a torrent that gains verified
pieces wakes the others, with a write-drain gate and no ping-pong). Within one pass each
reused piece is read and written **once** even with several candidates, the pieces the
merge verified are handed to the verifier without re-reading them (`Verifier.Skip`), and
the merge runs off the run loop. Private torrents are excluded: if A **or** B is
private the merge is disabled (**BEP 27 guard**, not an option). There is deliberately
no ED2K-style content hash: for v1 the match is by path and length, because those hashes
are not in the `.torrent` and would need a private index. The measurements are in §4.8 of
[`measurements.md`](measurements.md), the cost when nothing is reusable in §3.

**Ping-Based Auto-Speed** (anti-bufferbloat, standalone). A probe measures every second
the RTT towards the gateway (or the configured target) with a TCP connect, with a
baseline that is the minimum of the last 60 s; when the latency exceeds the baseline by
50% **and** 20 ms the upload cap is reduced by 15%, and it is raised by `cap/20` when the
line is at rest, never below `autospeed-min`, never above the configured or scheduled
cap. It is a feedback control on **latency**, only on the cap: the data path is
untouched, and `applyEffectiveSpeedLimitsLocked` (base → scheduler → auto-speed) remains
the only caller of `SetSpeedLimits`. It needs a positive upload limit, is off by default
and exists only standalone, like the bandwidth scheduler (in managed mode Gextto governs
the limits). The probe, the baseline and the cap step are `internal/autospeed`, shared
with Gextto's latency guard (§3, *Latency guard*), which passes its own window.

**Extensions outside the BEPs**: a
**WebSocket tracker** (ws/wss, an original bounded RFC 6455 client plus the
WebTorrent/bittorrent-tracker protocol, dialled through the same egress as the HTTP
tracker — proxy, resolver, blocklist), **DHT feeds** (a `dht://<pubkey-hex>[?salt=]` URL
becomes a normal feed source resolved to magnets, so rules, archive, dedup and add are
reused; `GET /api/v1/dht-feed` resolves one for external consumers and
`POST /api/v1/dht-feed/publish` signs and stores one), **v2 partial-file
hash serving** (BEP 52: `serveHashes` answers the requested leaf range, computing each
leaf from the downloaded pieces and rejecting a leaf whose piece is missing, so a
partially downloaded v2 file can serve block hashes too; a request is capped at 512
hashes so the leaves read on the torrent loop stay bounded) and the v1 cross-layout
merge above. Caveats: the reference `bittorrent-tracker` returns only
WebRTC peers to a WebSocket client, so a native client gets no TCP peers there — the
real unlock would be WebRTC, which is not done — and only the **resolver** side of BEP 46
exists, not the publisher (signing).

**What is missing.** Feature-wise the engine covers what libtorrent covers for this
use. What libtorrent has and gx-core does not is **exposure**: twenty years of runs in
the wild, and that is not a backlog item — it is acquired by running, which is exactly
what the campaign in [`measurements.md`](measurements.md) §4.6 and the open items in §8 are for. What is left is therefore
tuning and measurement, not capability: the **choking quality** on a real swarm (the
deterministic harness is the net, the swarm is the verification), the **pipelining**
ceiling beyond a local pair (the 10 Gbps target needs hardware that is not here), the
**disk I/O** (`fdatasync` and opt-in mmap today, measured at ~5.8 GB/s buffered and
~7.4 GB/s mmap; `O_DIRECT`/`io_uring` and contiguous writes are not justified by a
measurement), the **graduated file priority** with mixed priorities, and the **DHT**
levers (multi-path closed with data, the anti-Sybil cap kept as a hint, §4 of
[`measurements.md`](measurements.md)). Each has its gate in §8.

Wishlist, outside those: **WebTorrent/WebRTC** (niche, a lot of work) and alternative
storages (blob, bolt, sqlite: not needed). Postponed: **replacing `internal/dht` with an
external DHT** — the API model differs (per-torrent traversal against a request channel)
and it would drop the dual-stack/shared-socket patches, so it stays our own module.
Out of scope by choice (in [`bep.md`](bep.md)): BEP 8/17/18/22/26/28/30/34/35/45/50/51,
WebTorrent, native I2P (reachable through a SOCKS5/HTTP proxy). What remains open, with
its gate and its evidence, is in §8.

### 7. The engine is our own code

The engine (`internal/gxcore`), the DHT (`internal/dht`) and the uTP transport
(`internal/utp`) are original code in this repository under EUPL-1.2;
`THIRD-PARTY-NOTICES` lists only the linked libraries. The peer ID prefix is `-GX`
and the network identity is `"gx-core " + Version`.

The parts a client usually borrows are ours too: rate limiter, torrent id, logger,
backoff, uTP transport, DHT decoding and throttle. The wire formats, the resume
database and the flag spellings are kept compatible; where equivalence mattered it
is proved by execution (MSE handshake over `net.Pipe`, fast-set bytes, resume-DB and
RPC shapes). New engine code is written here, not copied, and the transport stays
behind its own interface so it can be replaced.

---

---

## Part III — What is next

### 8. Open items and the measurement-gated program

**Rule.** There is no speculative code: an item is implemented only when a measurement
justifies it, otherwise it is closed with the recorded data. Most of what remains is
**implemented or designed but not validated on a real swarm**, or needs a **go/no-go
decided on data**, so every item carries the gate it waits for.

**The engine fallback.** There are two selectable engines: **`gx-torrent`**, the
engine of this project, and **qBittorrent-nox** (external or managed). The
libtorrent engine is **not** embedded: it is not built and not shipped, and the
transfer settings are engine-neutral (`torrent_*`). When the
active engine fails — a crash budget spent, or an activation refused at startup —
Gextto walks a chain and persists the choice (`engine_fallback.go`):

1. **qBittorrent.** The managed runtime is downloaded on demand from
   [`userdocs/qbittorrent-nox-static`](https://github.com/userdocs/qbittorrent-nox-static)
   (the last libtorrent **v2** build for the architecture, SHA-256 verified against the
   GitHub digest), configured with a private profile under the application directory and
   started by Gextto; an operator who points Gextto at an **external** qBittorrent gets that
   one used as is, with nothing to download.
2. **recovery**: no alternative is available, so the transfers stay stopped, the UI stays up
   and the operator is notified.

Before the engine handover the supervisor tries the cheapest fix: if the updater kept
`gx-torrent.prev` (installed by `gexttod --update`), the previous binary is restored and
gx-torrent is retried instead of switching engines. A managed daemon that is **alive but
stops answering** its own health endpoint for five minutes is killed and counted as a
failure (`gxUnresponsiveLimit`), so a frozen API is not mistaken for a healthy engine. A
cooldown (`DATA_DIR/engine-fallback.json`, 30 minutes) stops the chain from ping-ponging
between two engines that both crash: within the window it goes straight to recovery.
`GEXTTO_ENGINE` forces the engine for one run, so a service whose saved backend is broken is
recovered from a shell without editing the database.

**eD2k and Kad through aMule.** Gextto has no eD2k engine of its own: it drives
an `amuled` over the External Connections protocol (`internal/amule`, our own
client). aMule brings what makes eD2k work on the real network (hundreds of
queued sources, UDP reasks, credits, Kad as a full node, AICH); Gextto adds the
search with its release parsing, pasted `ed2k://` links, aMule's downloads in
Downloads, the eD2k page (status, servers, Kad) and the import of completed files
through the same completion flow as torrents, by hardlink or copy so aMule keeps
sharing them. Everything is opt-in (`ed2k_enabled`); a second switch
(`ed2k_fallback`) adds eD2k as the last source when the torrent plane finds
nothing, both in the automatic gap-filling pass and in the manual **Search
missing** and single-episode search (the series search asks once per season, the
single search once per episode). Accepted automatic results need an exact
episode, a recognised quality and enough sources; a manual search shows every
match. eD2k names are a claim, the MD4 hash is the identity, and MediaInfo
validation after the download is the second line of defence.

The decision to use aMule instead of a native client rests on two facts kept in
[`measurements.md`](measurements.md): the measured Kad coverage of missing
episodes is modest, and a native client competitive with eMule needs a source
and queue scheduler of the same depth, which is not worth its cost for a
last-resort source. The native daemon is preserved in the history, at commit
`9f4e2b9` (the last `gx-ed2k` build, removed from the tree right after).

Open points:

- **Managed amuled (optional, not a Gextto engine).** Gextto does not ship or
  require aMule: the operator runs and configures it, and Gextto reads it over
  EC. A convenience starter (a private config directory, a generated EC
  password and the ports) would remove the manual setup for those who want it.
  Gate: users who want eD2k without configuring aMule by hand.
- **Remote aMule.** Completed files are imported from aMule's incoming folder at
  the same path, which holds when both run on one machine. A remote core would
  need a path mapping like `qbittorrent_path_mappings`.

The fallback is an explicit opt-in, not a pending decision: measured Kad
coverage of real gaps is **7–13 %** (`measurements.md` §4.10) and one real
library reports about **10 of 100** missing episodes found through it. That is
enough to keep the switch, not to enable it by default: a new installation must
not contact eD2k unless its operator asks.

**Waiting for a measurement or a host.**

- **Choking on a real swarm.** The controlled evidence is healthy (§4.6 of [`measurements.md`](measurements.md)): a real
  libtorrent leecher is served at full speed on loopback and reciprocation works
  (leechers exchange 2.52 MB, the seed's upload drops to 1.84×). What is missing is a
  swarm where peers download *from* us, which needs a host with a **reachable inbound
  port** (this one is behind a NAT: the seeding harness finds 0 peers). Gate: a campaign
  against libtorrent/qBittorrent on the same public torrent with the same policy and
  metrics (upload efficiency while seeding, reciprocation, first-block time, stall
  rate). Only a gap would justify touching `internal/unchoker`.
- **uTP acknowledgements stay one per data packet.** A state packet goes out
  500 µs after a data packet (`pendingSendStateDelay`), so only bursts share one. Under
  a saturated line with ~130 peers each connection receives a packet every ~13 ms, and
  the ACK sends cost as much as the datagram reads (~3 % of a core at 8 MB/s,
  [`measurements.md`](measurements.md)). Coalescing them would need a delay of 20 ms or
  more, which inflates the RTT the sender's RTO is built on. The CPU saving does not
  justify that change in the transport's congestion behaviour. Batched datagram I/O
  (`recvmmsg`/`sendmmsg`) cuts syscalls without touching the protocol timing, but on a
  measured saturated swarm uTP made under 1 % of the syscall time (TCP carried the
  bytes). Gate: the same `strace -c` on a uTP-heavy session (open inbound port) showing
  `recvfrom`/`sendto` as a real share.
- **Graduated file priority (0–7)** with mixed-priority files: so far measured only
  indirectly.
- **DLNA**: transcoding and frames extracted from the video (no ffmpeg) are
  missing; browsing, Search, Range playback, subtitles, library folders,
  artwork and Xbox/WMP (`X_MS_MediaReceiverRegistrar`) are in place (§3).
- **Latency guard: what a ping cannot see.** A latency probe tells a full queue from a
  good line, but it cannot tell who filled it: a user of little bandwidth who only needs
  a low latency (a call, a game) fills no queue. In `both`/`router` the router's WAN
  counters see that user; in `ping` the router is deliberately not read, so such a user
  is not protected (the operator chose to tune on the ping alone). Gate: a measurement
  with a real call or game next to a saturated download, in `both` and in `ping`. The
  current controller no longer releases after a calm spell at the ceiling: it keeps the
  cap while Auto is on and drops it when the line is idle or Auto ends.
- **DHT feeds**: the resolver and the publisher (signing and storing a feed, with
  the BEP 44 outgoing client as its consumer) exist; what remains is a real
  multi-client audience for the feeds (§4.7 of [`measurements.md`](measurements.md)).
- **WebTorrent/WebRTC**: the WebSocket tracker's practical value stays limited until
  WebRTC is done, since the reference tracker serves only WebRTC peers to a WebSocket
  client (§6).
- **10 Gbps pipelining**: the local single-pair ceiling is ~162 MB/s (multi-piece
  included); measuring further needs a multi-peer swarm on a fast link, neither
  available here.

- **Parallel requests per web seed.** One range download runs per web seed at a time;
  on a source that answers each file slowly (archive.org, ~3 s per request) a torrent of
  many small files is bound by that latency, and eight parallel requests measured ~2.5×
  faster with `curl` ([`measurements.md`](measurements.md) §4.9). Gate: a torrent served
  mostly by web seeds where the parallel version beats the current one on the same source.

**Product surface.** The torrent table is virtualised above 500 rows, the DLNA
library folder field proposes the configured Download folder, and the Watch
folders section says that the `.torrent` files are downloaded. No open item
remains here.

**The gap with Sonarr/Radarr** (§1), by value per effort, after a study and without
copying code (Sonarr is GPLv3, Gextto EUPL-1.2):

- **Named profiles as templates**: *Configuration → Sources → Quality profiles*,
  with a generated profile for every override combination in the library,
  "Default" first and a stable id per profile. A profile is copied into a title
  (from Jellyseerr or from *Edit selected* in the Series and Movies tables,
  which also edits many titles at once); it does not govern it. The *Sonarr* semantics (ordered qualities,
  cutoff, custom formats) stay out by choice (§1).
- **A `/api/v3` subset for Jellyseerr** (`system/status`, `qualityprofile`,
  `languageprofile`, `rootfolder`, `tag`, `series/lookup` + `POST series`,
  `movie/lookup` + `POST movie`, `PUT series`/`PUT movie`, `queue`), following the
  calls Jellyseerr makes (lookup by `tvdb:`/`tmdb:` id, only the monitored
  seasons, the library id in the answer); full compatibility is not claimed, and
  a real Jellyseerr run remains the acceptance test.
- **Manual import**: the *Import file* tab of Maintenance matches an existing file, lets the
  operator correct series/movie/season/episode and destination, replaces an existing
  copy only on request and records every episode of a multi-episode file.

**Closed with data**, so the ground is not re-covered: replacing `internal/dht` with an
external DHT (the API model differs and it would drop the dual-stack/shared-socket
patches); **enforcing** the anti-Sybil cap (no node exceeds any candidate threshold, so
it stays a hint, off by default); the **multi-path `get_peers`** rewrite
(`peers_confirmed_multi` was 0 in both real measurements); **ordering the merge
candidates by reusable bytes** (superseded by the dedup, no measurable gain);
`O_DIRECT`/`io_uring` (the disk is not the bottleneck); a write-back cache in the engine
(`write_cache=0`); a UI benchmark as a CI performance test (SSR rendering is ~1–3 ms per
page); a **file pool** (an LRU of open files): every file of a running torrent stays
open, one descriptor each, and that is cheap and linear up to 20,000 files in one
torrent — the only limit was the descriptor ceiling, which the engine now raises and
never lowers (`setNoFile`).

**Baseline.** `scripts/measure-baseline.sh` runs the cheap harnesses — and the network
ones when their environment is set — and writes a dated baseline, the reference a later
round compares against; the numbers stay in [`measurements.md`](measurements.md). What
remains is the real-swarm choking campaign, which needs the host with a reachable
inbound port, and whatever that campaign justifies.
