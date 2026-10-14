# Improvements and evolution of Gextto and gx-torrent

Single working document for **what Gextto and gx-torrent are, what is measured and
what is next**: the capabilities as they exist today, the numbers behind the choices
and the open items with their gate. Not the story of how we got here (the git history
keeps it), nor the chronicle of the problems that have been solved.

The technical reference of the engine is [`gx-torrent.md`](gx-torrent.md), its BEP
compliance [`bep.md`](bep.md), the engine inventory
[`../internal/gxcore/GEXTTO.md`](../internal/gxcore/GEXTTO.md) and the standalone plan
[`gx-torrent-evoluto.md`](gx-torrent-evoluto.md).

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

**III. Measurements**

8. [Measurement harnesses](#8-measurement-harnesses)
9. [Benchmarks and footprint](#9-benchmarks-and-footprint)
10. [Swarm: seeding, choking, pipelining](#10-swarm-seeding-choking-pipelining)
11. [DHT](#11-dht)
12. [Merge on real data](#12-merge-on-real-data)
13. [Accessibility verification](#13-accessibility-verification)

**IV. What is next**

14. [Open items and the measurement-gated program](#14-open-items-and-the-measurement-gated-program)

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
daemon; the torrent engine integrated (gx-torrent in pure Go, libtorrent or
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
it would take to close each gap is in §14.

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
what still needs manual validation are in §13.

**Access and security** (`auth.go`, *Configuration → Access*): authentication is
**off by default**. When on, anyone not on the LAN must log in (bcrypt password, a
signed cookie valid 30 days, a new password closes the sessions) or use the API key
(`X-Api-Key` or `?apikey=`; the TUI takes it from `GEXTTO_API_KEY`). The LAN is
exempt by default; behind a reverse proxy the forwarded addresses count
(`X-Forwarded-For`, `X-Real-IP`, `Forwarded`). Five wrong passwords block the IP for
one minute; without a password or key the check stays open with a warning in the
log, and `GEXTTO_AUTH_DISABLE=1` gets back in.

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
box filter and an 8 MB LRU cache. Open in §14: transcoding, frames extracted from
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
  is in §14.
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
does not have is refused with `ErrCapabilityUnavailable`, and only the embedded
engine's own blocks use `requireEmbedded`.

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
CI (`make fuzz-smoke`, 21 targets, seeds in `testdata/fuzz`), a bounded bencode screen
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
setup, `/regex/` wanted titles, the *Update available* indicator in the status bar).
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
are not in the `.torrent` and would need a private index. The measurements are in §12,
the cost when nothing is reusable in §9.

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
limits through `SetLimits` (§14).

**Extensions outside the BEPs** (analysis in [`beyond-bep.md`](beyond-bep.md)): a
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
what the campaign in §10 and the open items in §14 are for. What is left is therefore
tuning and measurement, not capability: the **choking quality** on a real swarm (the
deterministic harness is the net, the swarm is the verification), the **pipelining**
ceiling beyond a local pair (the 10 Gbps target needs hardware that is not here), the
**disk I/O** (`fdatasync` and opt-in mmap today, measured at ~5.8 GB/s buffered and
~7.4 GB/s mmap; `O_DIRECT`/`io_uring` and contiguous writes are not justified by a
measurement), the **graduated file priority** with mixed priorities, and the **DHT**
levers (multi-path closed with data, the anti-Sybil cap kept as a hint, §11). Each has
its gate in §14.

Wishlist, outside those: **WebTorrent/WebRTC** (niche, a lot of work) and alternative
storages (blob, bolt, sqlite: not needed). Postponed: **replacing `internal/dht` with an
external DHT** — the API model differs (per-torrent traversal against a request channel)
and it would drop the dual-stack/shared-socket patches, so it stays our own module.
Out of scope by choice (in [`bep.md`](bep.md)): BEP 8/17/18/22/26/28/30/34/35/45/50/51,
WebTorrent, native I2P (reachable through a SOCKS5/HTTP proxy). What remains open, with
its gate and its evidence, is in §14.

### 7. The engine is our own code

gx-core is our engine and the documentation presents it as such: no external origin of
the code is advertised. The whole engine (`internal/gxcore`), the DHT (`internal/dht`)
and the uTP transport (`internal/utp`) are original code in-tree under EUPL-1.2, and the
per-component `LICENSE` files are gone: `THIRD-PARTY-NOTICES` lists linked third-party
libraries only. The peer ID prefix is `-GX` and the network identity is
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
repository with APIs, wire formats and behaviours preserved. What still matches the old
code is protocol-mandated format (BEP wire layouts, bencode tags, API shapes) and
compatibility-kept strings (error/log texts, resume-DB and flag spellings), plus
algorithm bodies where any correct Go implementation converges. Where it mattered,
equivalence is proved by execution: MSE handshake cross-check over `net.Pipe`, fast-set
byte identity, resume-DB and RPC shapes byte-identical. How this state was reached is in
the git history.

**Policy for anything adopted from elsewhere** (also in `AGENTS.md`): an algorithm is
**rewritten** — third-party code is not copied, the idea is not covered by copyright
while the expression is — or the module is adopted as an **MPL-2.0 dependency**. The uTP
transport was first vendored as an MPL-2.0 dependency and then rewritten as our own code
in `internal/utp/transport`, with its helper primitives reimplemented in-tree
(`gextto_support.go`), so **no MPL component remains**. The engine talks to our own uTP
`Socket` — dial and accept counters, a `net.PacketConn` facade for the shared DHT
socket — so the transport stays isolated and swappable.

---

## 8. Measurement harnesses

The measurements below are taken by hand, outside CI: they serve to decide with
numbers, and a change on a data path is kept only if it is **at least equivalent** to
what it replaces. Two rules apply everywhere: measure the system **as it really runs**
(queue and slots, seed policy, scheduler and auto-management on — never "with the
queues off", which would be a laboratory and not production), and compare two clients
only with the same swarm, the same role, the same policies and the same metrics.

**Cheap probes and harnesses.**

- `scripts/bench-engines.sh` — RAM and throughput probe of gx-core: swarm throughput on
  loopback (with peak RSS) and disk write throughput of a directory (`--dir`, point it
  at a mount NFS to compare):
  `scripts/bench-engines.sh [--size MiB] [--dir DIR] [--leechers N]`;
- `make measure-seeding` — local swarm, seeder plus 3 leechers by default, with
  distinct `127.0.0.x` addresses (the engine refuses two connections from the same IP).
  `GX_MEASURE_SUPERSEED=1` compares super-seeding, `GX_MEASURE_MESH=1` connects the
  leechers to each other, `GX_MEASURE_LEECHERS=N` sets the swarm size; the output is
  the seed upload as `Nx corpus` with averages and times. Absolute numbers depend on
  the machine and are not comparable between runs, and a peer may take two retry ticks
  (10 s) in super-seeding: the harness reports instead of failing;
- `make measure-choking` — the choking campaign: the controlled half serves a real
  **libtorrent** leecher on loopback (`scripts/libtorrent-leech.py`, needs
  `python3-libtorrent`), the real-swarm half seeds a public torrent and needs a
  reachable inbound port;
- `make measure-pipeline` — the **adaptive request queue**, local swarm at shallow
  (def=4/max=4), default (50/250) and deep (200/500) depths, reporting bytes and time;
  the knobs travel in `NetworkOptions` (`MaxRequestsOut`/`DefaultRequestsOut`). On
  loopback the RTT is ~0, so the depth is not the bottleneck and the profiles give
  nearly identical numbers (~200 ms, ~40 MB/s with one leecher): it is a
  regression/sanity check, not a WAN demonstration;
- `make measure-realswarm GX_MEASURE_REAL=<public.torrent>` — a public torrent with the
  DHT, the trackers and uTP enabled (`GX_MEASURE_REAL_SECONDS`, default 120 s),
  reporting peak peers, the uTP peer split and the uTP socket counters. It is the
  control-path check the local swarm cannot give: a run that sees no peer at all fails,
  so it cannot pass hollow;
- `make measure-merge3` — Swarm Merging Phase 3 on real ISOs (see §12);
- `make measure-dht` — the **public DHT**, routing-table fill and query success
  (`GX_MEASURE_DHT=5m`, `GX_MEASURE_DHT_INFOHASH=<40 hex>`), the basis of the anti-Sybil
  and multi-path numbers (§11);
- the **deterministic unchoker harness** (`internal/gxcore/internal/unchoker/sim_test.go`,
  run by `make test-engine`): it drives the real `Unchoker` with synthetic peers and
  fixes its properties (fast downloaders in download, fast uploaders in seed, immediate
  `FastUnchoke`, optimistic unchoke that does not steal a regular slot and rotates, the
  budget respected, fairness). A safety net for future variants of the algorithm.

**Isolation with latency.** `TestMeasurePipeliningLatency` puts an **in-process latency
proxy** in front of the seed (no `tc`/`netem`, no root): every chunk is released at
arrival plus latency, in order and **without limiting the bandwidth**, so the RTT is
added without throttling the link.

**Holepunching.** `scripts/holepunch-netns-test.sh` verifies end-to-end that gx-torrent
opens a hole (BEP 55, `ut_holepunch`) between two clients behind NAT/firewall with a
public peer as **relay** (engine behaviour in [`gx-torrent.md`](gx-torrent.md)). It is
root-only and runs entirely inside dedicated network namespaces, so the host network is
not touched: `gx-lab` (public: tracker, seed, relay `10.1.0.1:51410`) plus two stateful
firewalls `gx-r1`/`gx-r2` (outbound allowed, new inbound DROPped, `ESTABLISHED,RELATED`
accepted, TCP towards the peer ports refused so the hole opens over uTP) and the clients
`gx-a` (leecher) and `gx-b` (seed). A minimal Python tracker introduces them; the seed
completes and stays interested, then the leecher asks the relay for an introduction and
the two cross-dial over uTP. Success is a peer with origin `holepunch` (the dialling
side) or `incoming` (the other) in `/api/v1/torrents/<hash>/peers`. Choices worth
knowing: a **stateful firewall** instead of a cone NAT (conntrack would create a local
entry conflicting with the SNAT; the firewall reproduces the same observable and makes
the test deterministic), seed and relay on different IPs (the engine deduplicates peers
by IP), `-outgoing-interface` set to the listening IP so the announce carries the right
address, a long tracker interval so a re-announce is not mistaken for the holepunch, and
success detected on both sides because the dial is a race. Usage: `make gx-torrent`, then
`sudo scripts/holepunch-netns-test.sh` (`--keep` to debug, `--size 16` for a larger
torrent); debug with `HOLEPUNCH_DEADLINE` (default 90 s), `HOLEPUNCH_B_DEADLINE` (60 s)
and `HOLEPUNCH_DIAG=1` (conntrack dump). Limits: it emulates with a stateful firewall
rather than a real cone NAT, and which side reports `holepunch` or `incoming` depends on
who wins the race.

## 9. Benchmarks and footprint

**What is measured here:** the cost of the code — CPU, allocations, RSS — with
`go test -bench … -benchmem`, `pprof` CPU/heap profiles and the footprint of the daemon
and of `gx-torrent` (RSS, goroutines, allocations). The swarm behaviour (throughput,
seeding, choking, pipelining) is in §10. Intel N97; the figures a change replaced are
kept where they justify the design.

**Engine micro-benchmarks** (quiet machine / machine under load):

| Benchmark | Result | Note |
|---|---|---|
| `BenchmarkEngineLoggerFiltered` vs `Forwarded` | **151–175 vs 730–785 ns/op, 3 vs 6 allocs/op** | records below the handler threshold are discarded **before** being formatted, so at WARNING (normal daemon use) the `Debugf`/`Infof` calls on the hot paths allocate nothing; the handler still receives the same records (ERROR/WARNING, no swarm-noise count lost) |
| `BenchmarkBlocked` (IP filter lookup) | **18.6 / 37.7 ns/op, 0 alloc/op** | sorted merged intervals with binary search; the old lookup allocated a `map` and a slice on the path of every peer/tracker connection |
| `BenchmarkPickerRarestSteady` | **~105 / 143.5 µs/op, 0 allocs/op** | piece picker, steady state |
| `BenchmarkTickUnchokeSeeding` | **1.3 / 1.64 µs/op, 1 alloc/op** | unchoker tick |
| `BenchmarkPrefillFromUnrelatedV2` (16 candidates) | **1.49 / 1.58 µs/op, 128 B/op, 16 allocs/op** | one-shot cost of the Phase 2 merge scan (allocation, resume, later trigger), **no steady-state path** |
| `BenchmarkMergeCandidatesUnrelated` (32 torrents) | **~98 ns/op, 0 allocs/op** | the filter the resume and later-trigger paths pay when nothing is reusable (§12) |
| `BenchmarkNewConnID` (uTP) | **31 ns/op, 0 B/op, 0 allocs/op** | the connection id probes from a random value and stops at the first free one, O(1) with the connections a socket really has; it used to build a 64K-entry table per dial (512 KiB and 152,353 ns/op) |
| `BenchmarkTransferLoopback` (uTP, 16 KiB block) | **45.6 → 62.0 MB/s, 82,020 → 23,692 B/op (−71%), 173 → 116 allocs/op (−33%)** | four optimisations from the profile: the connection-id probe, lazy `Event` channels (the per-send `acked` flag no longer allocates one per packet), a reusable read window (`readPos` offset instead of reslicing) and a `netip.AddrPort` connection key instead of a `UDPAddr.String()` per inbound packet |
| `BenchmarkUTPConnRemoteAddr` | **259 → 113 ns/op, 152 → 40 B/op, 6 → 3 allocs/op** | the TCP-view addresses are converted once at wrap (`NewUTPConn`) instead of at every `RemoteAddr()` call; the rest is `String()` |

Matching 16 unrelated v2 candidates costs ~1.6 µs once per torrent (one small map per
candidate) and the picker/peer/piece-write paths are not touched, so the merge cannot
affect seeding or download throughput.

**uTP retransmission.** The RTO consumes the loss signal. A **minimum-RTT floor of
5 ms**: without it a fast path (loopback or LAN, RTT of tens of µs) collapses the RTO
to sub-ms and a briefly stalled peer becomes a resend storm — on the silent-peer
harness (1 s) 18,184 resends at a 1 µs floor against 2,919 at 5 ms (**6.2×**). A
**per-connection backoff** doubles on each RTO timeout (cap 4×) and halves on each
delivered packet, so a silent or congested peer is retried with growing patience and a
link that delivers again relaxes to the measured RTT — silent-peer A/B (3×4 s) 17,663
resends fixed against 8,853 adaptive (**50% fewer**). A healthy connection never
timeout-resends, so the backoff stays 1 and the swarm numbers are unchanged. Fresh
connections seed the RTO from the per-host winning-dial latency EMA instead of the fixed
400 ms, clamped to [5 ms, 400 ms], and the samples always win: a 5 ms seed gives an RTO
of [10, 20] ms against [800, 1600] ms by default.

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
equal (p ≥ 0.15). The `uTP` connection-id probe and the word-wise interest update
(`HasAnyOutside` + `skipBits`, 231 → **2.8 µs** per peer on 100k pieces, ×80, 0 alloc)
are the two places where the rewrite is measurably faster.

**The v1 cross-layout filter has a cost of its own.** Replacing a per-candidate map
with a nested scan had pushed `BenchmarkMergeCandidatesUnrelated` from ~98 to
**1360 ns/op**; the scan is now nested only while `len(a)*len(b) <= 1024` (lengths
compared before paths) and larger packs index the smaller torrent in a map:
**375 → 265 ns/op, still 0 allocs**. `BenchmarkV1FilesMatchNoMatch` (unrelated torrents
with the same file length): 8 files 290 → 278 ns, 64 files 18.2 → 3.4 µs, 2000 files
17.3 ms → **0.13 ms** (109 KB, 9 allocs).

**The disk is far from being the bottleneck.** `BenchmarkStorageWrite` gives
**~5.8 GB/s** buffered and **~7.4 GB/s** mmap (ranged mmap sync ~29 GB/s through the
page cache), far above any network speed, which is why `O_DIRECT` and `io_uring` are
not justified.

**Footprint.** The IP filter stores the IPv4 rules, like the IPv6 ones, as **sorted and
merged intervals** with binary search; the pointer-based segment tree
(`internal/blocklist/stree`) is gone. With the Aetherinox blocklist (34 MB,
**666,763 rules** → 343,024 intervals) the live heap is **5.4 MB** (was 190 MB) and
`gx-torrent` RSS **~105 MB** (RssAnon 92 MB) against 295 MB, with the same number of
rules loaded; the 34 MB raw file stays in the daemon's cache (rare re-reading on manual
reload) and is removable later. The SQLite `cache_size` is SQLite's default **2 MB**
(`sqlite.go`): on the real archive DB (510 MB, FTS5; `Archive.Browse` and
`CountMatches`, tried up to 16 MB of cache) the latency is **flat**, within the noise
even on the broad `MATCH` (~1.14 s), because it is the OS page cache that serves the
file — `gexttod` RSS **67.6 MB** (RssAnon 46.2 MB) against 109.7 MB at the same uptime.
At ~29 min uptime, real 34 MB blocklist and 4 active torrents: `gexttod` RSS 113 MB
(RssAnon 89 MB), `gx-torrent` RSS 165 MB (RssAnon 150 MB) — workload-dependent
(peers and active pieces), within the range of the other rounds, with no new
steady-state allocation.

**Robustness at no performance cost.** Torznab answers are decoded with `Strict=false`
plus the HTML entity table (a raw `&` or `&egrave;` in a scraped title no longer makes
the whole answer unreadable), and the cost is nil: on a 100-item answer, strict
**2.24–2.32 ms/op, 642.8 kB/op, 17179 allocs/op**, lenient **2.22–2.29 ms/op,
642.8 kB/op, 17179 allocs/op**. Prowlarr's aggregate search is JSON (one
`json.Unmarshal` into a slice of small structs), outside the XML path. The same holds
for the other robustness work: the uTP header refuses inputs shorter than 20 bytes
instead of panicking, the bencode screen bounds lengths and depth before the decoder,
and the mmap fault guard turns a fault into an I/O error with an **equivalent** write
bench (36.3–38.0 → 35.9–37.5 µs, 0 alloc).

**Other measurements.** The feed archive (`feed_archive.go`, bbolt with a compact
index): `BenchmarkFeedArchiveSearch` on 100k elements **362 ms → 7.4 ms** (49×) with
2,800× less allocation. HTML listings used as feeds (`internal/rss/listing.go`, ext.to
and TorrentGalaxy resolved to direct magnets and feed links instead of failing on
invalid XML): **50 elements in 0.3 s / 1.9 s** on the two real URLs. The auto-speed
probe is ~1 connection/s and does not touch the data path; with auto-speed off nothing
changes, so there is no regression. Still to measure: **graduated file priority** with
mixed-priority files (§14).

**Optimisations in place.** Items ordered by **expected value = (impact × certainty) /
(effort × risk)**, one item = one commit:

| Intervention | Effect |
|---|---|
| Remove `O_SYNC`, batch durability | 16 KiB **35 → 81 MiB/s** on NFS (2.3×), 64 KiB 1.5×, 256 KiB 1.6×, 1 MiB 1.2×, up to ~118× at small blocks on NVMe; the invariant is that a persisted bit refers to data already fsynced (`syncPieces()` on the run loop, `writeBitfield()` ordered snapshot → fsync → write, `updateStats` in two phases with no Bolt transaction open during the fsyncs) |
| mmap opt-in (`-mmap`, Linux) | **~7.0 GB/s** on tmpfs against **~5.9 GB/s** for `pwrite`; a **ranged flush** takes a clean mapping from 477 µs to **13 ns**, with one dirtied piece unchanged (~1%) |
| LSD without `d.mu`/`t.Stats()` (lock-free snapshot) | no lock on the local-service-discovery path |
| Streaming: targeted `Torrent.PiecesDone(begin,end)` | the streaming reader does not walk the whole bitfield |
| MSE `StreamWriter` in place | 0 alloc per block |
| `servedRequests` bounded window (~1024) | bounded memory in the tracker path |
| Piece cache with lazy TTL | no sharding needed (profiling showed no contention) |
| `Bitfield.Count` with `math/bits` | one word per iteration |
| Optionals: `findLocked` O(1) | constant-time lookup |
| Write ring buffer (4 pieces/torrent) | the network keeps flowing while pieces are written; the prerequisite for multi-piece per peer (§10) |
| Disk guard, mmap fault guard, word-wise interest, bounded bencode screen | see above and §6 |

Follow-ups **not added**, each without a measurement to justify it: io_uring/AIO (Go
does not expose it without a dependency or cgo, and the mmap path is already
asynchronous), explicit contiguous-write coalescing (the page cache already merges
adjacent writebacks), rarity buckets and a picker refactor, a watermark cursor in
`pickSequential`/`pickFileEdge`, a `PieceDownloader` bitmask, `clear(b.Data)` in
`bufferpool`, `FADV_RANDOM`, `saveLocked` outside `d.mu`, `/proc/meminfo` under `d.mu`
and `HandleDisconnect` O(N). **Postponed**: qBittorrent's
`renameFile`/`renameFolder` (it needs rename support in the engine: a feature of its
own).

## 10. Swarm: seeding, choking, pipelining

**Metrics.** Choking (upload slots): who is unchoked and for how long, fairness at
equal speed, optimistic unchoke rotation, behaviour while downloading (rewarding fast
downloaders) and while seeding (rewarding fast uploaders). Seeding end-to-end: bytes
uploaded by the seed against the corpus (`Nx corpus`), throughput up and down,
completion time, ratio and time to 1:1 in a real campaign.

**Deterministic unchoker bench.** All the properties hold on the real `Unchoker` driven
by synthetic peers, and the tick with 50 peers costs ~1.1 µs, 896 B/op, 1 alloc:
**negligible**. Hardening applied along the way: an interested but stalled peer (zero
speed) receives the next piece at the tick instead of waiting for `not-interested`
alone.

**Local swarm, first round (Intel N97, corpus 2 MiB).** Seeding without mesh (seed → 3
leechers, 5 repetitions): normal ~41–42 ms with the seed at **3.00× corpus**;
super-seeding ~51–52 ms (**~24% slower**) with the same 3.00×. Mesh with 2 peers
(indicative): both **1.00×**, the leechers exchange the pieces among themselves; mesh
with 3+ peers on one host does not converge. A later run measured the reciprocation
more precisely: with mesh the leechers upload **2.52 MB among themselves** and the
seed's upload drops from 3.00× to **1.84× (−39%)**.

**Honest reading.** The engine's choking is **correct and negligible as a cost**. On
small or fast swarms super-seeding **does not reduce** the upload (the exchange is
already enough) and costs throughput and variability: the BEP 16 advantage is for the
*initial seeding* of large torrents with many slow peers, which one host cannot
reproduce. Consequence: **super-seeding stays opt-in, not default**, and the scaling
question goes to a real campaign.

**Pipelining, isolated with latency.** With RTT 50 ms, 256 KiB pieces (16 blocks) and a
4 MiB corpus, one leecher:

| Profile | blocks in flight | time | average |
| --- | --- | --- | --- |
| `shallow` def=1/max=1 | 1 | 13.4 s | 305 KiB/s |
| `default` def=50/max=250 | ≤50 | 1.1 s | **3.8 MiB/s** |

~12× with the adaptive queue (a second run reproduced it exactly: 300 KiB/s in 13.7 s
against 3850 KiB/s in 1.06 s, ~12.8×). The ceiling is readable from the numbers: with
256 KiB pieces a queue of ≤250 is bounded by the piece's block count, so the
`default` run was already at it — the next lever for high-bandwidth-delay links was
**multi-piece per peer**. A peer now downloads up to two pieces at once, sharing a
single in-flight budget (the extra piece opens only when the window has room) and never
receiving the same piece twice; re-running the same harness:

| Profile | single piece | multi-piece |
| --- | --- | --- |
| `shallow` def=1/max=1 | 301 KiB/s | 301 KiB/s (window of 1 respected) |
| `default` def=50/max=250 | 3853 KiB/s | **6756 KiB/s** (×1.75) |

So multi-piece raises the throughput at RTT 50 ms where the single-piece block count was
the limit, while the `shallow` profile keeps its window of 1, and RAM and CPU stay
equivalent. The **bounded write ring buffer** (4 pieces per torrent) is what keeps the
disk from becoming the bottleneck while more pieces are in flight: on loopback the
seeding harness is **equivalent** (the disk is not the bottleneck there) and the gain is
on slow storage.

**End-to-end on loopback.** `make measure-seeding`, 2 MiB and 3 leechers: **3.00×
corpus**, all done in 11 ms. `make measure-pipeline` in a local swarm, 8 MiB × 2:
16 MiB in 101 ms (**~162 MiB/s**); with RTT 50 ms, default depth 0.608 s /
**6736 KiB/s** and shallow 13.55 s / 302 KiB/s. The harness runs the seeder and every
leecher in one process and the engine's outgoing interface is process-wide: each
leecher creates its connection before the next daemon replaces the setting, so the
distinct source addresses are kept and the swarm reaches the intended 3.00×.

**Campaign on a real swarm, first run (2026-10-12).** The same well-seeded public
torrent (Ubuntu 24.04.3 desktop amd64, ~6.0 GB), the same machine, the same 40 s window
after a 5 s ramp-up post-metadata, three public UDP trackers in the magnet, sequential
runs (never in parallel), two runs each of **libtorrent 2.0.11** (`python3-libtorrent`)
and **gx-core** (`gx-torrent -mode standalone`):

| Client | run | metadata | peers (window) | downloaded | average |
| --- | --- | --- | --- | --- | --- |
| libtorrent 2.0.11 | 1 | 3 s | 83 (81 seed) | 292.9 MB | **7,149 KiB/s** |
| libtorrent 2.0.11 | 2 | 3 s | 66 (61 seed) | 258.8 MB | **6,317 KiB/s** |
| gx-core | 1 | 3 s | 80 (77 seed) | 385.1 MB | **9,401 KiB/s** |
| gx-core | 2 | 3 s | 81 (79 seed) | 381.2 MB | **9,305 KiB/s** |

**Honest reading:** on this swarm gx-core is **on par with or above** libtorrent
(median ~9.3 against ~6.7 MiB/s), with immediate metadata and a comparable number of
peers. It is **not** an isolated demonstration of choking or pipelining (the choice of
peers differs, one machine, two runs): it is the proof that gx-core really downloads
from a real swarm and that neither is a bottleneck against libtorrent at this load. The
isolation of the two algorithms is above (the in-process latency proxy for pipelining,
the deterministic bench for choking); `tc netem` is not needed.

**Downloads and uTP on real swarms** (`make measure-realswarm`, one run each): Big Buck
Bunny 276 MB in 90 s (75 peers, 23 uTP, 0 SYN drops), Debian netinst 793 MB in 180 s (80
peers, 18 uTP, 0 SYN drops), Ubuntu 24.04 1.08 GB in 180 s (85 peers, 31 uTP, 0 SYN
drops). Real uTP peers connect, the downloads complete and `utp_syn_drops` stays 0 (the
50-SYN backlog bound never binds); the resends show loss on a noisy swarm without a
storm — Big Buck Bunny dials 342, accepts 57, RTO resends 2563, fast 557; Debian dials
277, accepts 14, RTO resends 932, fast 41.

**Validation on a live torrent (2026-10-14).** `make measure-realswarm` on the Debian
13.7 netinst torrent, 2 minutes: the download **completes to 100%**, peers peak **80**
(uTP peak 26), connections out 53 uTP / 118 TCP and 6 in, uTP socket **dials 265 (91
errors)**, accepts 6 (0 errors), **RTO resends 1211, fast 0, SYN drops 0**. The engine
finishes a real swarm, and a healthy path never timeout-resends; the dial error rate
(~34%) is the expected cost of racing uTP against TCP-only peers and is worth a closer
look in a later round, not a regression.

**Choking campaign, controlled against libtorrent.** Our seeder serves a real
libtorrent leecher on loopback: **48 MiB uploaded (100% of the file) in 4.84 s**
(~10.4 MB/s) with the leecher verifying complete. The real-swarm half (seeding the
Debian ISO for 3 min) found **0 peers**, because seeding needs an **inbound** port and
this host is behind a NAT with no reachable port (the download run finds 80 peers
outbound); the harness is kept for a host with a reachable port.

**Decision: no choking change.** Reciprocation works locally and an external libtorrent
client is served well; the only missing datum — a real swarm where peers download
*from* us — is blocked by the test host's NAT, not by the algorithm. The campaign stays
as the gate for any future change.

**DHT cold start.** The peer-search retry while a torrent looks for peers uses a **15 s**
interval (`DHTMinAnnounceInterval`); the Ubuntu torrent over DHT only resolves its
metadata in ~49 s with 12 peers, and the remaining time is the routing-table fill
(~30 s cold), not the lookup.

## 11. DHT

The gx-core DHT (`internal/dht`, a module of its own) covers BEP 5/32/33/42/43/44/46
with a routing table as a bucket-less binary tree; against Tixati the gap is
**hardening and diagnostics**, not BEP coverage (see [`bep.md`](bep.md)).

**What it has.** `Stats()` exposes per-RPC and screening counters, the reflected
address, the NAT type, `subnet_share_max`, `nodes_insecure_id`, `subnet_cap_hint` and
`peers_confirmed_multi`, and it is the measurement basis of everything below. The
`announce_peer` token is bound to source address **and** scope (the info-hash), so a
token from one swarm is not reused on another. The **anti-Sybil cap per subnet**
(`Config.MaxNodesPerSubnet`, default 0 = off) is implemented with an O(1) counter:
nodes beyond the threshold stay in the address book with `noTree`, and
`subnet_cap_hint` advises a value. Out of scope: DHT over I2P and the BEP 44
**outgoing client** (no consumer yet).

**Measurements.** `make measure-dht` on the public DHT (opt-in, outside CI):

- ~740 known nodes, `subnet_share_max` 14, `nodes_insecure_id` ≈42%, and the discovery
  of **108 peers in ~30 s** on the Ubuntu 24.04 torrent;
- a 3 minute run (2026-10-13): nodes settled at ~254 from a peak of ~536 (stale nodes
  cleaned), `subnet_share_max` **8** — below the hint threshold of 16, so no cap is
  suggested — `nodes_insecure_id` ≈40%, `capped` 0, `queries_out` 2899 / `replies_in`
  834 (**≈29% reply success**), `dropped` 1290 (rate limit), `reflected_observations`
  619 (the sending side of the BEP 42 `ip` field works) and `peers_found` 0 (a random
  info-hash);
- a 5 minute run on a real info-hash (Debian, 2026-10-14): nodes ~296 (peak 525),
  `subnet_share_max` 4–7, `nodes_insecure_id` ≈42%, `nodes_capped` 0, `queries_out`
  4703 / `replies_in` 1670 (**35.5%**), `reflected_observations` 1063, `peers_found` 0
  and **`peers_confirmed_multi` 0**;
- a further run on the public network: **4262 nodes**, 21.9% query success, 875
  reflected pings, 2 peers found from a cold start.

**Read.** The routing table is **not concentrated** and no node exceeds any candidate
threshold (8/16/24), so enforcing the anti-Sybil cap would be a no-op while CGNAT and
mobile legitimately share subnets: **the cap stays a hint, off by default**. The ~65–71%
of unanswered queries are nodes that do not reply, not proof of poisoning.

**Multi-path `get_peers` — closed with data.** The idea is to confirm peers across
independent paths and prefer those seen from ≥2 (an explicit per-lookup state machine
like the BEP 44 one, with a bounded fan-out and a deadline). The instrumentation exists
and is read-only: `notePeerReport`/`expirePeerConfirm` (`internal/dht/confirm.go`) track
which responding nodes reported each (info-hash, peer), bounded to `peerConfirmMax`
entries and expiring after `peerConfirmWindow`, and **reporters are counted per subnet**
(the same /24 or /48 unit as the cap), so one host on many ports or a Sybil block cannot
confirm its own peers and inflate the datum. In both real measurements
`peers_confirmed_multi` was **0**, so **no peer was confirmed by two distinct subnets**
and there is no evidence justifying the cost of the rewrite: the item is closed with
data. A controlled adversarial swarm would be the only next step, and it is out of
scope for a home client.

## 12. Merge on real data

**Phase 3, block-granularity reuse.** Phase 2 reuses a destination piece only when
**every** source piece overlapping it is verified; Phase 3 reuses the rest at **16 KiB
block** granularity: the verified spans of a partially covered destination piece are
written to disk (`storePartialBlocks`) and the blocks they cover are recorded per piece
(`torrent.partialBlocks`); when the piece is picked, the downloader reads those blocks
back (`prefillBlocks` → `PieceDownloader.Prefill`) and requests only the others, with
the assembled piece verified whole as usual. It is safe because BEP 52 leaves are the
16 KiB blocks of the **file**, not of the piece: two torrents with the same
`pieces root` share the whole Merkle tree below the piece layer, whatever their piece
length, so a span copied from a verified source piece is the file's true content. The
final whole-piece check still runs; if it fails on a piece with local blocks, the blocks
are dropped, the peer is not banned (the mismatch cannot be blamed on it) and the piece
is downloaded whole.

**Test environment (real data, public torrents).** `scripts/phase3-env.sh` downloads the
Debian netinst and Arch Linux ISOs over BitTorrent (aria2c) and builds, with
python3-libtorrent, two **v2-only** torrents of each — source with 256 KiB pieces,
destination with 1 MiB pieces, same file so same root — corrupting one source piece in
four, so every destination piece straddles verified and unverified source pieces.
`TestMeasureMergePhase3` (`GX_MEASURE_MERGE3=<dir>`, opt-in) runs a gx-torrent seeder of
the destination and a leecher holding the partial source, adds the destination to the
leecher and measures the bytes taken from the network; `GX_MEASURE_MERGE3_PEER` with
`scripts/phase3-seed.py` uses a **libtorrent** seeder instead, for interoperability.

| Run (loopback) | before (Phase 2) | after (Phase 3) |
| --- | --- | --- |
| Debian 756 MiB: from the network | 792,723,456 B (100%) | **198,180,864 B (25.0%)** |
| Debian: total time | 2.81 s | **1.31 s** |
| Arch 1.5 GiB: from the network | 1,640,497,152 B (100%) | **410,255,360 B (25.0%)** |
| Arch: total time | 5.82 s | **2.71 s** |
| Arch: CPU (whole process, seeder included) | 17.7 s | **10.0 s** |
| Arch: peak RSS | 109 MiB | 111 MiB |
| Debian with a libtorrent 2.0.11 seeder | — | 25.0% from the network, complete |

All 756 (Debian) and 1565 (Arch) destination pieces were reused at block level and every
one passed its hash.

**On a resume (2026-10-13).** A destination merged with no peer, then paused and reloaded
by a restarted leecher session, downloads after the restart:

| Run (loopback) | before (no resume merge) | after (resume merge) |
| --- | --- | --- |
| Debian 756 MiB: from the network after the restart | 792,723,456 B (100%) | **198,180,864 B (25.0%)** |
| `merged_bytes` re-derived | 0 | 594,542,592 B |
| partial (block-level) pieces re-derived | 0 | 756 (every destination piece) |
| total time | 2.61 s | 1.00 s |

Exactly the no-restart figure: the reloaded bitfield is kept (no piece re-read or
re-hashed, via `Verifier.Skip`/`Only`) and the block-level reuse is re-derived, so a
restart no longer re-downloads the file. The added cost when nothing is reusable is only
the candidate filter: **115 ns and 0 allocations** over 32 unrelated session torrents.

**When the sibling arrives later (2026-10-13).** The destination is added first, with no
sibling and no peer, and the source is added afterwards:

| Run (loopback) | before (no trigger) | after (later-sibling trigger) |
| --- | --- | --- |
| later merge observed | no | yes |
| `merged_bytes` | 0 | 594,542,592 B |
| partial (block-level) pieces | 0 | 756 (every destination piece) |
| Debian 756 MiB: from the network | 792,723,456 B (100%) | **198,180,864 B (25.0%)** |
| total time | 2.61 s | 0.91 s |

The same 25.0% as the first run, the resume path and the no-restart merge: the order in
which the torrents are added no longer decides whether the reuse happens.

## 13. Accessibility verification

**Method**: static review (HTML/CSS/JS), semantics/names/ARIA/focus,
responsive layout/visible focus/reduced-motion, light/dark theme contrast, axe-core
via Playwright, regression tests (dialogs, keyboard sort, polling, narrow
viewports). Suite: **13 green tests** on dashboard, downloads, settings,
maintenance, health, logs (`cd uiweb/end2end && npm ci && npm run test:a11y`;
a safety net, not a substitute for the manual assessment).

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

## 14. Open items and the measurement-gated program

**Rule.** There is no speculative code: an item is implemented only when a measurement
justifies it, otherwise it is closed with the recorded data. Most of what remains is
**implemented or designed but not validated on a real swarm**, or needs a **go/no-go
decided on data**, so every item carries the gate it waits for.

**Waiting for a measurement or a host.**

- **Choking on a real swarm.** The controlled evidence is healthy (§10): a real
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
  and the BEP 44 outgoing client has no consumer yet (§11).
- **WebTorrent/WebRTC**: the WebSocket tracker's practical value stays limited until
  WebRTC is done, since the reference tracker serves only WebRTC peers to a WebSocket
  client (§6).
- **10 Gbps pipelining**: the local single-pair ceiling is ~162 MB/s (multi-piece
  included); measuring further needs a multi-peer swarm on a fast link, neither
  available here.

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
§9, §10 and §11. What remains is the real-swarm choking campaign, which needs the host
with a reachable inbound port, and whatever that campaign justifies.
