# Improvements and evolution of Gextto and gx-torrent

Single working document for **what Gextto and gx-torrent are and what is next**: the
capabilities as they exist today and the open items with their gate. The numbers behind
the choices are in [`measurements.md`](measurements.md); the story of how we got here is
in the git history, and the problems that have been solved are not recorded here.

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
- **Toward the indexer managers, not instead of them** (Jackett/Prowlarr): Gextto
  consumes the manager's aggregate search and does not duplicate the indexer list
  (§4).
- **Field**: whoever has a server with a media server (Jellyfin/Plex) and wants
  "find, download, rename, archive" from a single daemon; very likely they already
  have Jackett or Prowlarr.
- **Selection policy**: global defaults (resolution, source, codec, audio, HDR,
  language, size) plus **per-title overrides and exclusions** plus
  **explainability** — the explanation API says why a release is accepted, rejected
  or preferred ("Why not this one?" in the UI). No quality profiles and no
  custom-format regex engine like `*arr`: those are the most complex — and the most
  misconfigured — part of that model.
- **Non-goals**: `*arr` `/api/v3` compatibility and the quality-profile/
  custom-format semantics; multi-user authentication (remote exposure goes through
  a reverse proxy, `docs/SECURITY.md`). The differences with Sonarr/Radarr are
  published, not hidden.

**Where Gextto is ahead or on par:** series, movies **and comics** in a single
daemon; the torrent engine integrated (gx-torrent in pure Go, or
qBittorrent-nox, §6); explainability of the choices; search also from web engines;
lightness (a Go binary and SQLite against a .NET stack); simplicity (health, backup
and maintenance built in).

**Honest snapshot against Sonarr/Radarr** (2026-10): they are better at
**selection** (profiles, custom formats, delay profiles, a profile per series and
per movie), at the **edge cases of import and rename** (multi-episode, specials,
daily, manual import with matching and replacement) and at the **ecosystem/API**
(Overseerr/Jellyseerr speak `/api/v3`). Gextto is better at single scope,
integrated engine, explainability, footprint and simplicity. The verdict is
unchanged: **an alternative for new installations**, without claiming parity. What
it would take to close each gap is in §8.

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
is in the series tab.

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

**Web UI, TUI and languages.** The web UI is server-rendered with HTMX (`uiweb/v2`)
and has a terminal client (`gexttod tui`). Six languages — it/en/de/fr/es/pl — cover
the web UI (chrome, labels, tooltips, hints, server-side messages and the strings
the client JS generates), the API/HTMX errors shown to the user, the notifications,
the TUI and the gx-torrent web page (the language is passed by Gextto with `-lang`
and `?lang=xx`). Daemon logs stay **in English by choice**; API error strings not
shown to the user may stay in the original language. Guard: `TestUIIsTranslatable`
fails if a UI string or a `uiText` literal is not a key of the catalogues (with an
explicit allowlist); `TestClientI18nKeysTranslated` covers the client keys.

**gx-torrent web page.** The standalone client's page is laid out the way a
qBittorrent Web UI user expects to find things — filters on the left, one compact
line per torrent, the detail panel below, a complete context menu, the speeds and
global limits in the status bar — so switching costs nothing; it is not a copy, and
where qBittorrent is weak the page goes further: per-user column choice and widths
applied before the first paint (24 columns, availability, time active, last
activity and wasted included) and, standalone, kept on the server so columns,
filters and theme follow the operator across browsers; menu toggles that show a
**mixed** selection and bring it to one state; every control usable from the
keyboard (submenus, column and panel resizing included); the technical counters
kept one click away instead of crowding the status bar; an Add dialog that
previews a magnet (name and info hash) and takes per-torrent limits; an 8-second
**undo** after a remove that kept the files; files grouped by folder with one
priority control each, and folders renamed like files; guided empty states and a
section filter in Settings; the table refresh travels as JSON (`/ui/rows`) with the
chrome as translated HTML, which is six times smaller per tick (see
[`measurements.md`](measurements.md)). Details in
[`gx-torrent.md`](gx-torrent.md), *Web interface*. Two points of the page are
still open: **virtualising the table** beyond the 300-row window with *Show
more* (for sessions with more than a thousand torrents), and a **content layout**
in the Add dialog, which needs engine support because today the save path is per
torrent or per category.

The RSS and wanted pages follow one principle: **nothing is decided out of
sight**. A rule's preview updates as it is typed, warns when it takes several
titles and **names the criterion that leaves the other archived items out**
(`rss.Criteria.RejectReason`); a feed row says which rules decide its downloads
(links to their editor), shows when a rule may match again after its cooldown and
turns an error into an actionable hint (API key, Cloudflare/FlareSolverr, a page
without links, a timeout); ready-made rule templates and a feed `Test` (items of
a URL previewed before saving) plus a setup import that shows what the file holds
before it replaces the current configuration keep every change visible before it
lands; *New feed* takes a **pasted list** too — one address per line, a name
before the address when wanted — and creates one feed per line, deriving a
readable name from the address and skipping what is already configured; a feed
filter that the rules make unused is marked so; a wanted title lists
its releases with the
verdict for each (taken, had, or the filter that turns it away), live while it
is added, and a verdict of *would be taken* can be acted on: the release is
taken by hand from the same list, which can also ask the indexers now for the
title (the interactive search). Rule terms come in *all of* / *one of* / *none of*, all found with the
same separator-insensitive, word-start match (`rss.HasTerm`), and the open
episode range `1x01-` reaches the later seasons as in qBittorrent, which is what
a wanted title's *Seasons* `1+` promises. Episodes already in the client count as
had for a wanted series, whatever brought them there.

**Accessibility.** Reference **WCAG 2.1 AA** (compatible 2.2 practices). The
interface is **partially accessible**, with an automated baseline that passes but is
not a WCAG/EN 301 549 certification. What is in place: language via the `lang`
attribute, landmarks (skip-nav, `aside`/`nav`/`header`/`main`), `aria-current`;
accessible names (not only placeholder or tooltip) and labels on icon controls;
dialogs with focus inside, Tab/Shift+Tab trap, Escape and focus restore;
keyboard-sortable headers with `aria-sort`; focus preserved under polling; visible
focus; tables with caption and scope (also dynamic); progressbar with a value; ARIA
for tabs, dialogs and landmarks; live regions for states, errors, notifications and
async work; decorative symbols never instead of text; settings search with synonyms
(`memo`, `memory`, `RAM`, `cache`); light-theme contrasts darkened where needed;
mobile reflow with internal table scroll; reduced motion respected. The method and
what still needs manual validation are in §6 of [`measurements.md`](measurements.md).

**Access and security** (`auth.go`, *Configuration → Access*): authentication is
**off by default**. When on, anyone not on the LAN must log in (bcrypt password, a
signed cookie valid 30 days, a new password closes the sessions) or use the API key
(`X-Api-Key` or `?apikey=`; the TUI takes it from `GEXTTO_API_KEY`). The LAN is
exempt by default; behind a reverse proxy the forwarded addresses count
(`X-Forwarded-For`, `X-Real-IP`, `Forwarded`). Five wrong passwords block the IP for
one minute; without a password or key the check stays open with a warning in the
log, and `GEXTTO_AUTH_DISABLE=1` gets back in. Both secrets (the API key and the
gx-torrent token) can be generated and copied from *Access and services → API
keys*; what they are and how to use them is in [`api-keys.md`](api-keys.md).

**Media servers and notifications.** After an import Jellyfin receives
`/Library/Media/Updated` with the folder and Plex
`/library/sections/{id}/refresh?path=` for the section that contains it
(`media_refresh.go`); the requests during a refresh are merged. Unknown folder, no
Plex section or server refusal fall back to a full refresh as before, and the manual
buttons remain complete. `jellyfin_path_mappings` / `plex_path_mappings` map paths
per server for Docker. Notifications (Telegram, email, webhook) are translated, and
the webhook also speaks **Discord, Slack, ntfy, Gotify and Pushover** through a
format selector (`notify_webhook_format`).

**Calendar** (`calendar_ics.go`, `GET /feed/calendar.ics`, linked in Integrations).
For the Italian audience, where a series is dubbed months or years later, it
distinguishes three dates: 📥 **arrived in the library** in the last 30 days (day of
arrival), 📺 **original air date** from TMDB for the current season (✓ if already in
the library) and 🎬 **Italian release of movies** from TMDB (digital → home video →
cinema), with a declared fallback to the original date. The "Upcoming releases" tile
of the Dashboard declares that it shows the original air date.

**Health and history.** The observability data is stored in the database and shown
in *Health*, without Prometheus: the **Searches** panel with the duration and the
outcome of every source cycle by cycle (and a warning — in the log and as a
notification — when a source keeps failing, when a search takes too long or when no
cycle completes cleanly for three intervals), the **Title queue** panel with the
titles waiting, since when and until when, for series and movies, and **Failed
imports** per day; the panels refresh themselves every 60 s and the indexer health
is in *Sources*. Log lines about a torrent carry `acq: <id>` (a short ID derived
from the hash, which stays hidden) and end up in `acquisition_events`: *History* tab
in the torrent detail, 📜 per episode and a panel in the series page, API
`/api/acquisitions`. A replacement produces a single line in the log —
`♻️ FBI S03E01 updated (1.4 GB): … · previous version «…» moved to the trash` —
while a normal addition stays `📁 … added to the library …`. The **Downloading**
entry in the bar shows the ongoing downloads (unfinished torrents plus active HTTP,
not seeds), refreshed every 5 s, and on the phone it is a counter on the icon.

**First start and settings.** The guided first-start procedure
(`/?view=setup`, `uiweb_v2_setup.go`) uses TMDB or TVDB and asks for the language at
the first step. Every non-secret setting with a registered default (even empty)
shows ↺ next to "Save": it fills the field with the default and the user confirms by
saving; the value is shown next to it when compact (`Resettable`/`DefaultInline` in
`uiweb_v2.go`, `HasDefault` in `uiweb_pages.go`). Excluded: fields disabled for the
active engine, handled by auto-tuning, secrets, or without a default.

**Streaming and DLNA.** Web streaming (`cmd/gx-torrent/stream.go`, the endpoint
Gextto redirects to) sends the **Content-Type from the file extension**
(`video/mp4`, `video/x-matroska`, `audio/flac`, …) with
`Content-Disposition: inline`, so a browser plays instead of downloading, and
answers HEAD; a shared `serveTorrentFile` core is reused by the UI player and by the
DLNA server, and the concurrent streams are refcounted per file
(`beginStream`/`endStream`) so a player closing does not clear the window another
reader of the same file still needs. The **DLNA/UPnP AV server**
(`cmd/gx-torrent/dlna.go`, `dlna_ssdp.go`) is **opt-in** and exposes the completed
torrents' media to a TV or console on the LAN: SSDP (M-SEARCH replies, alive at
start and every 15 min, byebye), ContentDirectory:1 (`Browse`, BrowseMetadata,
BrowseDirectChildren, paging, content-derived `SystemUpdateID`), ConnectionManager:1
and GENA `SUBSCRIBE` with the initial event; media are served with HTTP Range by the
shared core plus the DLNA `contentFeatures`/`transferMode` headers, and subtitles
with the same name as the video are attached (`res` + `sec:CaptionInfoEx`). It is
**LAN-only by construction** (HTTP and SSDP from a non-local address are refused)
and `/res` serves only the files the listing offers. Settings `dlna-enabled`,
`dlna-listen`, `dlna-name`, in the daemon page when standalone (with the running URL
shown) and as Gextto options (`gxtorrent_dlna*`) when managed; an address or name
change restarts the server. **Library folders** (`dlna_library.go`) show the NAS
archive beside the torrents as a browsable tree (Gextto passes its archive root and
the series folders automatically, trash excluded), read through `os.Root` with no
`..`/symlink escape, skipping the NAS system folders, with listings cached 30 s: on
the real NFS archive 125 series list in ~0.5 s and a 5 GB 2160p episode streams by
Range. **Artwork** (`dlna_art.go`): videos and folders carry `upnp:albumArtURI`
(`JPEG_TN` + `JPEG_SM`) from the Jellyfin/Kodi images beside the media (`-thumb`,
`-poster`, `folder.jpg`, season posters `season01-poster.jpg`), scaled in Go with a
box filter and an 8 MB LRU cache. Open in §8: transcoding, frames extracted from
the video (no ffmpeg), search, Xbox/WMP (`X_MS_MediaReceiverRegistrar`).

**In-app updates.** gx-torrent standalone
(`cmd/gx-torrent/update.go`, `internal/updatecheck`) updates itself from *Settings →
Maintenance*: a timer (`update-check-hours`, default 6) or **Check for updates**
reads `release.json`, where the commits that touch the daemon's code are flagged
(`"gx"`, `gx_torrent_commit`), so only real daemon changes are offered. **Update**
downloads the archive, verifies SHA-256, test-runs the binary, swaps it keeping
`.prev`, re-executes in place (same PID) and shows the new commits in a popup; three
failed starts restore `.prev`. It is off when managed by Gextto and for local builds
(the release channel is stamped only by `build-release.sh`). Gextto has the same
check period as a setting (`update_check_hours`, default 6), one button that reads
**Check for updates** and becomes **Update**, and after an update a **Gextto
updated** popup with the commits it brought (`update-notes.json`).

### 4. Indexers: the manager as the single source

Gextto treats the manager (**Jackett/Prowlarr**) as **a single aggregated source**
and does not import its list of indexers: the manager is the only source of truth for
indexers, categories, credentials, FlareSolverr and rate limits.

- The manager type can be **declared** (`manager` in `config.go` and the UI) instead
  of inferred from URL, port or name; the inference remains as fallback.
- A **timeout per source** keeps a single request below the search budget, and the
  manager's health (Prowlarr `indexerstatus`) is shown in *Sources*: a broken or
  disabled indexer is visible instead of giving zero results in silence
  (`indexer_health.go`).
- **Categories were removed**: an aggregate serves both series and movies, so a
  static category per indexer is useless or harmful (a TV query with the "movie"
  category returns nothing).
- A prototype that duplicated the indexer list was built, measured and **reverted**:
  no stability gain, only configuration drift.
- Prowlarr's search API **ignores** season/episode and external-id (measured): they
  are not forwarded, and `type` has no observed effect either. **Jackett's** indexer
  list is not reachable with the Torznab key (its API redirects to the login), which
  is irrelevant since only the aggregate is needed; Jackett exposes Torznab at
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

Around every engine Gextto provides queue/slots, stalled/retry, seed policy, RSS,
search, categories/tags, the bandwidth scheduler, post-processing, moves and the RAM
disk, the IP filter and the proxy: **those are not engine gaps**, and limits follow
the libtorrent semantics (**−1 = inherit the global one, 0 = unlimited**).

**What the engine has.** Protocol: BitTorrent v1 and v2-only (from `.torrent` and
magnet, BEP 52), dual-stack IPv6 (`peers6`, PEX v6, DHT BEP 32, LSD IPv6/BEP 14) and
uTP. Per torrent: download/upload limits, connection and upload-slot limits, per-piece
priority and diagnostics, **graduated file priority 0–7**, tracker removal and hot
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
binary-tree routing table, tokens bound to source address and scope, an opt-in
anti-Sybil cap per subnet, and the **feeds** of BEP 46. Robustness: parser fuzzing in
CI (`make fuzz-smoke`, 23 targets, seeds in `testdata/fuzz`), a bounded bencode screen
before the decoder on every hand-written path, snub and session ban with TTL, IP
filter, and hostile hash-request headers (bogus root, bad length, huge proof, overflow)
refused without a panic. Diagnostics: uTP socket, dial, accept, error, RTO and SYN-drop counters, merge
metrics (`merged_pieces`/`merged_bytes`) and storage errors flow from the engine stats
to the daemon API into **both UIs**.

The **standalone daemon** adds: *Wanted* (type, seasons, language, quality, exclusions,
episode filter, category and path; added by hand or with **Search the feeds**, a register
in `state.json` with a throttled gap search, and no feed generation so the feeds stay
free for the advanced rules), the RSS panel (per-feed state, synchronous reload,
enable/disable, configurable interval, ordered PASS/FAIL rules with `once` and a preview
on the archive), **HTML listings as feeds** (ext.to and TorrentGalaxy pages resolved to
direct magnets and feed links instead of failing on invalid XML), the **feed archive**
(bbolt with a compact index), the IP filter with a scheduled refresh, *Optimise*
(queue, cache, bandwidth at 80% of the measured peak with auto-speed active), the
**per-day bandwidth scheduler** (`internal/bwschedule`, shared with Gextto: every
weekday has its own window and limits, a window past midnight belongs to the day it
starts on, and the older single window stays as fallback), notifications on Gextto's
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
are not in the `.torrent` and would need a private index. The measurements are in §5 of
[`measurements.md`](measurements.md), the cost when nothing is reusable in §2.

**Ping-Based Auto-Speed** (anti-bufferbloat, standalone). A probe measures every second
the RTT towards the gateway (or the configured target) with a TCP connect, with a
baseline that is the minimum of the last 60 s; when the latency exceeds the baseline by
50% **and** 20 ms the upload cap is reduced by 15%, and it is raised by `cap/20` when the
line is at rest, never below `autospeed-min`, never above the configured or scheduled
cap. It is a feedback control on **latency**, only on the cap: the data path is
untouched, and `applyEffectiveSpeedLimitsLocked` (base → scheduler → auto-speed) remains
the only caller of `SetSpeedLimits`. It needs a positive upload limit, is off by default
and exists only standalone, like the bandwidth scheduler (in managed mode Gextto governs
the limits). A managed version on Gextto's side would measure the RTT and modulate the
limits through `SetLimits` (§8).

**Extensions outside the BEPs**: a
**WebSocket tracker** (ws/wss, an original bounded RFC 6455 client plus the
WebTorrent/bittorrent-tracker protocol, dialled through the same egress as the HTTP
tracker — proxy, resolver, blocklist), **DHT feeds** (a `dht://<pubkey-hex>[?salt=]` URL
becomes a normal feed source resolved to magnets, so rules, archive, dedup and add are
reused; `GET /api/v1/dht-feed` resolves one for external consumers), **v2 partial-file
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
what the campaign in [`measurements.md`](measurements.md) §3 and the open items in §8 are for. What is left is therefore
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

gx-core is our engine and the documentation presents it as such: no external origin of
the code is advertised. The whole engine (`internal/gxcore`), the DHT (`internal/dht`)
and the uTP transport (`internal/utp`) are original code in-tree under EUPL-1.2, and the
per-component `LICENSE` files are gone: `THIRD-PARTY-NOTICES` lists linked third-party
libraries only. The peer ID prefix is `-GX` and the network identity is
`"gx-core " + Version`.

Nothing does the engine's work from outside the repository: the rate limiter is a token
bucket in `internal/gxcore/internal/bandwidth`, the torrent id a version-1 UUID minted
in `internal/gxcore/torrent/torrent_id.go`, the logger `internal/gxcore/logging`, the
backoff `internal/gxcore/internal/backoff`, the uTP transport and the DHT address
decoding and throttle our own.

Every file of `internal/gxcore` (~170) and `internal/dht` (17) is rewritten in this
repository with APIs, wire formats and behaviours preserved. What still matches the old
code is protocol-mandated format (BEP wire layouts, bencode tags, API shapes) and
compatibility-kept strings (error/log texts, resume-DB and flag spellings), plus
algorithm bodies where any correct Go implementation converges. Where it mattered,
equivalence is proved by execution: MSE handshake cross-check over `net.Pipe`, fast-set
byte identity, resume-DB and RPC shapes byte-identical. How this state was reached is in
the git history.

**Policy.** Gextto prefers its own code: what the engine needs is written here, not
copied from anywhere. The pieces the engine talks to stay ours and swappable — it
speaks to a uTP `Socket` (dial and accept counters, a `net.PacketConn` facade for the
shared DHT socket), so the transport is isolated and can be replaced.

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
libtorrent engine Gextto used to embed is **gone**: it is not built and not
shipped, and the transfer settings are engine-neutral (`torrent_*`). When the
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
`gx-torrent.prev` (now installed by `gexttod --update`), the previous binary is restored and
gx-torrent is retried instead of switching engines. A managed daemon that is **alive but
stops answering** its own health endpoint for five minutes is killed and counted as a
failure (`gxUnresponsiveLimit`), so a frozen API is not mistaken for a healthy engine. A
cooldown (`DATA_DIR/engine-fallback.json`, 30 minutes) stops the chain from ping-ponging
between two engines that both crash: within the window it goes straight to recovery.
`GEXTTO_ENGINE` forces the engine for one run, so a service whose saved backend is broken is
recovered from a shell without editing the database.

**Waiting for a measurement or a host.**

- **Choking on a real swarm.** The controlled evidence is healthy (§3 of [`measurements.md`](measurements.md)): a real
  libtorrent leecher is served at full speed on loopback and reciprocation works
  (leechers exchange 2.52 MB, the seed's upload drops to 1.84×). What is missing is a
  swarm where peers download *from* us, which needs a host with a **reachable inbound
  port** (this one is behind a NAT: the seeding harness finds 0 peers). Gate: a campaign
  against libtorrent/qBittorrent on the same public torrent with the same policy and
  metrics (upload efficiency while seeding, reciprocation, first-block time, stall
  rate). Only a gap would justify touching `internal/unchoker`.
- **The uTP dial error rate.** In the live run 91 of 265 dials failed: expected when
  racing uTP against TCP-only peers, but worth a closer look in a later round.
- **Graduated file priority (0–7)** with mixed-priority files: so far measured only
  indirectly.
- **DLNA**: transcoding, frames extracted from the video (no ffmpeg), search and
  Xbox/WMP (`X_MS_MediaReceiverRegistrar`) are missing; browsing, Range playback,
  subtitles, library folders and artwork are in place (§3).
- **Auto-speed on the Gextto side**: today it exists only standalone; a managed version
  would have Gextto measure the RTT and modulate the limits through `SetLimits` (§6).
- **DHT feeds**: the resolver exists, the **publisher** side (signing a feed) does not,
  and the BEP 44 outgoing client has no consumer yet (§4 of [`measurements.md`](measurements.md)).
- **WebTorrent/WebRTC**: the WebSocket tracker's practical value stays limited until
  WebRTC is done, since the reference tracker serves only WebRTC peers to a WebSocket
  client (§6).
- **10 Gbps pipelining**: the local single-pair ceiling is ~162 MB/s (multi-piece
  included); measuring further needs a multi-peer swarm on a fast link, neither
  available here.

**Product-surface refinements.**

- **DLNA library folders.** On the gx-torrent settings page, the *Library folders*
  field of *Media server (DLNA)* should propose the configured **Download
  folder** at least, so the folder that already holds the completed media is a
  click away instead of a path to type.
- **Watch folders.** The *Watch folders* section should say that the `.torrent`
  files it finds are **downloaded**, not only "added automatically", so what the
  feature does to the media is explicit.
- **Poster and card views.** In *Series* and *Movies* the list should be
  switchable to a **poster view** (one cover per title, the essential data on the
  card, everything else when it is opened), as Sonarr and Radarr offer, with the
  choice remembered per view: the covers come from the same TMDB data the
  *Explore* search already uses.

**Closing the gap with Sonarr/Radarr** (§1), by value per effort, each after a study and
without copying code (Sonarr is GPLv3, Gextto EUPL-1.2):

- **Named profiles on the Gextto model**: the global policy becomes "Default", each
  combination of overrides a generated profile, with an automatic migration from the
  per-title data (`series.quality/language/subtitle/exclude`, `movies_config.*`).
  Reproducing the *Sonarr* semantics (ordered qualities, cutoff, custom formats) costs
  more; custom formats (a regex engine plus its UI) would be evaluated separately.
- **A `/api/v3` subset for Jellyseerr** (`system/status`, `qualityprofile`,
  `languageprofile`, `rootfolder`, `tag`, `series/lookup` + `POST series`,
  `movie/lookup` + `POST movie`, `queue`), after named profiles, root folders and tags,
  and tested against a real Jellyseerr; full compatibility is not realistic.
- **Import and rename edge cases** (multi-episode, specials, daily, manual import with
  matching and replacement): study the behaviour and reimplement it, building our own
  corpus from real names.

**Closed with data**, so the ground is not re-covered: replacing `internal/dht` with an
external DHT (the API model differs and it would drop the dual-stack/shared-socket
patches); **enforcing** the anti-Sybil cap (no node exceeds any candidate threshold, so
it stays a hint, off by default); the **multi-path `get_peers`** rewrite
(`peers_confirmed_multi` was 0 in both real measurements); **ordering the merge
candidates by reusable bytes** (superseded by the dedup, no measurable gain);
`O_DIRECT`/`io_uring` (the disk is not the bottleneck); a write-back cache in the engine
(`write_cache=0`); a UI benchmark as a CI performance test (SSR rendering is ~1–3 ms per
page).

**Where the current round landed (2026-10-14, Intel N97, public torrents and DHT).**
`scripts/measure-baseline.sh` runs the cheap harnesses — and the network ones when their
environment is set — and writes a dated baseline, which is the reference the next round
compares against. That round found and fixed the v1-filter regression, implemented the
v2 partial-file hash serving (§6) and closed the DHT questions with the numbers now in
[`measurements.md`](measurements.md). What remains is the real-swarm choking campaign, which needs the host
with a reachable inbound port, and whatever that campaign justifies.
