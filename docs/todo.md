# Open items

The maintained list of work that is not done. One item per line: what to do and what
unblocks it. When an item lands it leaves this file; when a new one is accepted it is
added here. There is no history and no design essay: the state of the product is in
[`gx-torrent.md`](gx-torrent.md), [`gx-core.md`](gx-core.md) and the manuals, the numbers
in [`measurements.md`](measurements.md).

**Rule.** Only real work: an item names a problem that was observed (a bug, a measurement,
a request) and the work that solves it. An idea waiting for a condition that nobody has
seen is speculation and does not belong here. Every idea proposed by the operator or by the
assistant in a working session is written here in the same cycle: under the matching area
once accepted, under *Proposals* while it is still a decision. A rejected idea is not
recorded. An idea must not stay only in the chat. Do not add a diary: a sentence that would
still be true after the item lands does not belong here.

## Engine (gx-core)

- **uTP lock per connection**: one global mutex serialises every uTP socket and connection, and a temporary benchmark showed aggregate throughput falling from about 9 MB/s with one connection to about 1.3 MB/s with sixteen, most of the wait in `Conn.resendDue`. Gate: confirm on `make measure-compare` (`line-utp`) before the refactor, then per-connection locking with no lock held across syscalls.
- **v2 block-tree memory budget**: `v2BlockTree` reserves its estimate without refusing a build past the 256 MiB budget, and eviction always keeps the newest tree, so one very large v2 file (about 4 GiB of hashes for 1 TiB) exceeds it. Gate: refuse or compute on demand past the budget, with a session-wide cap and a test on a sparse large v2 file.
- **Super-seeding piece choice**: `getPieceToSuperSeed` scans every piece against every peer on each pick (3.9 ms and 141 KB per pick at 10,000 pieces and 16 peers). Gate: a per-piece availability index updated on HAVE, checked for equal choices against the current picker.
- **Completion and interest scans**: every completed piece rescans the bitfield (`wantedComplete`) and every peer (`peerHasWanted`). Gate: a profile on a 100,000-piece torrent showing it matters, then a running count of wanted pieces still missing.
- **Locks held while waiting on a run loop**: `applyTrackerListLocked`, `infoLocked` and `withTorrent` call into the torrent loop with `d.mu` held, so a loop stuck on storage could keep the daemon lock. Gate: reproduce with an unanswered `commandC` and measure how long `d.mu` stays held.

## gx-torrent daemon

- **qBittorrent search and torrent-creator jobs**: every `search/start` and creator task starts a goroutine with no concurrency limit, finished jobs are pruned only by a new search, and creator tasks keep the whole `.torrent` in memory until deleted. Gate: a bounded worker pool, a job cap and a TTL, with a test of a client that never deletes.
- **`/sync/maindata` history**: each poll builds a full map of every torrent and keeps up to 16 of them (about 96 MiB at 5,000 torrents). Gate: shared copy-on-write snapshots or per-rid diffs, benchmarked at 1k/5k/10k torrents.
- **Feed dedupe under the daemon lock**: `markFeedSeen` scans the whole `FeedSeen` map for expired entries on every insert, under `d.mu`. Gate: a periodic sweep outside the lock or time buckets, with API latency measured during a large poll.

## Gextto daemon

- **Library materialised for counts**: the Series and Movies pages and the automatic subtitle pass call `AllSeriesEpisodes`, which builds the whole library, to compute a few counts or a short target list; the movie page also matches every monitored film against every copy. Gate: aggregate SQL queries and a title index, measured on 50k episodes.
- **Matching and parsing done twice per cycle**: `RecordSeenBatch` scores every release against every title, the cycle then matches the candidates again, and gap filling parses the same listings once per episode. Gate: carry match and score on the release and group gaps by season, with a count of `ParseQuality` calls per cycle.
- **Archive and MediaInfo reads before the limit**: `MediaInfoBackfillTargets` loads every missing row before applying its limit, content filters with an empty query scan the whole archive, `BrowsePage` parses every row. Gate: paginated queries and a light list DTO, measured on 100k archive rows.
- **Single series and calendar doing global work**: `EpisodesForSeries` decodes every active torrent and filters in Go, and the ICS calendar holds its cache lock across network calls. Gate: per-series SQL predicates and a calendar built outside the lock.
- **Cache stampede and unbounded manual searches**: after a configuration change several callers reload it at once, health checks queue without rechecking the cache, and per-episode searches start one goroutine per key with the daemon's context. Gate: singleflight on the reloads and a bounded search scheduler tied to the request context.
- **Logger and acquisition history writes**: the logger holds its lock across the disk write and every acquisition event is its own SQLite transaction. Gate: a blocking-writer test and batched event commits.

## Interface

- **Off-screen rows in the daemon's virtual list**: bulk actions and the category assignment look rows up in the DOM, so a selection that includes rows not rendered loses them, and spacer rows can enter the selection. Gate: keep the selection by hash in the model, with a Playwright test on 501 rows.
- **Stale asynchronous answers**: episode editor, `.torrent` preview, details, folders and archive render whatever answer arrives last, so a slow answer for one title can land on another. Gate: a request token or `AbortController` per view, with slow/fast Playwright cases.
- **Save all and discard changes**: «Salva tutto» can report success when a request failed in transport, and «Annulla modifiche» does not restore structured editors or the custom language field. Gate: per-key success counting and a DOM snapshot restore, with a Playwright test.
- **Optimistic actions treated as success**: undo after a removal fetches the magnets after they are gone, adding to the archive ignores `ok:false`, and removing from the detail view drops the row before the answer. Gate: act on the answer, with rollback, and tests for HTTP 200 `ok:false` and 500.
- **Polling in hidden tabs**: the statistics and update pages keep polling without an in-flight guard while the tab is hidden. Gate: pause on `visibilitychange` and serialise the requests.
- **TUI selection, stream limits and maintenance errors**: a refresh can move the selection after an HTTP/eD2k row is inserted, SSE lines and events have no byte limit, and a backup error is cleared by the next successful maintenance call. Gate: keep the selection by identity, cap lines and events, record errors per endpoint.

## Release and scripts

- **Continuous release replaced before verification**: `publish-github-release.sh` drafts the live release and clobbers its assets before downloading and checking them, so a failure leaves the channel unpublished or mixed. Gate: build and verify a separate draft, then promote it, with a mock `gh` failing half way.
- **Unquoted systemd units**: the user installer, the tmp cleaner and the packaging scripts write `ExecStart` and `Environment` verbatim, so a path with spaces or `%` breaks the unit. Gate: systemd escaping checked with `systemd-analyze verify`, and binary backup before replacement.

## Proposals

- **Subtitle CDN downloads and proxy/VPN**: the public transport used for provider download links deliberately ignores the configured proxy and outgoing interface so the destination address can be checked at dial time; an installation that requires a proxy or VPN therefore fetches those files directly. Gate: decide whether to keep the direct, address-checked path and document it, or validate the destination through the proxy without losing the private-address guard.
- **qBittorrent per-torrent limit `0`**: the compatible API maps `0` to "inherit the global limit", which an audit called a bug; in qBittorrent a per-torrent `0` means no limit of its own while the global one still applies, which is what gx-torrent does now, and the audit's fix would bypass the global limit. Gate: check the behaviour against a real qBittorrent before changing anything.
- **Quality profiles: movie and series scope**: the same profile serves both libraries and the request-manager picker cannot filter by type. Gate: decide whether to add a `kind` field (movie/series/both, default both) and filter the pickers.
- **Quality profiles: linked titles**: a profile is copied into a title at save time and titles do not store its id, so editing a profile never reaches existing titles. Gate: decide whether to keep copy-only semantics or add a "re-apply to linked titles" action with a preview of the affected titles.
- **Rules in the qBittorrent-compatible API**: the native page now keeps rules only as an advanced fallback, but `/api/v2/rss/rules/*` still exposes them to external clients, and the one-time migration switches stored rules off for those clients too. Gate: decide whether external clients still need that surface or the API should report no rules and the compatibility note in `gx-core.md` be updated.
- **gx-core screenshots after the merge**: the manuals describe the single acquisition page, but the Series & movies screenshots still show the old state labels and the rules tab on the RSS page. Gate: regenerate `docs/screenshots/gx-core/{it,en}` from a running standalone daemon.

- **Persistence transactions**: replace the remaining whole-library read-modify-write paths with patch transactions or revisions. Gate: concurrent save tests beyond the series/movie endpoints.
- **Resume journal evolution**: associate layout generation and checksums with persisted piece state, and rehash selectively after unclean shutdown or sync failure. Gate: crash-consistency and resume-performance tests.
- **Lifecycle and persistence boundary evolution**: consolidate task/process supervision and repository transactions behind explicit unit-of-work and shutdown phases. Gate: lifecycle tests covering HTTP, jobs, watchdog, crossnet and managed processes.
- **Release provenance evolution**: publish signed, immutable build-once artifacts and promote the same verified bytes to stable. The declared dependency report and the reproducibility recipe are already in place. Gate: signature verification and promotion tests.

- **Focus and clicks still lost across some HTMX swaps**: the torrent list now restores
  focus by id per swap target, and the drawer opener is looked up again, but polling still
  replaces whole panels (integrity, maintenance, update, jobs) where a click can land on a
  detached node. Gate: decide whether to give those controls stable ids too or to update
  only the changed cell contents.
- **Search filters still reload the page**: the notification filter and the bulk eD2k add
  still use a full redirect, so a keyboard user starts again from the top; the clear and
  add-all buttons now have ids but a redirect cannot be caught by the focus restore.
  Gate: convert those endpoints to in-place HTMX swaps or restore focus from the URL.
- **Visual consistency of status colours and progress bars**: `--ok` still equals
  `--accent` in all four themes and three different progress bar grammars remain between
  Gextto and the daemon. Gate: an accessibility check of the new hues in every theme
  before separating the success colour from the selection colour.
- **Accessibility audit beyond the automatic guards**: sorted headers, row checkboxes,
  placeholder posters and the detail tabs are now labelled correctly, but axe still skips
  several pages and `docs/ACCESSIBILITY.md` asks for a manual screen-reader pass. Gate: an
  accessibility round with axe extended to the skipped pages and a keyboard/screen-reader
  check of the pages the suite does not visit.
- **Navigation that loses the place**: row actions and the Guided/Expert switch reload the
  whole page, and the settings search only changes the hash, so a refresh returns to the
  old section. Gate: decide how many routes to convert to in-place swaps, keeping the
  in-app unsaved-changes message.
- **Operational state that is easy to misread**: starting a cycle only shows a toast that
  expires, and the two log viewers refresh differently. The automatic-refresh badge and the
  masked API keys of the daemon are already in place. Gate: the operator picks which of the
  two remaining indicators to land first.
- **Queue deadlines and preemption**: per-title high/normal/low priority is
  available; the remaining work is a finish-by time, preemption of lower-priority
  work when a deadline is at risk and protection of active streaming. Gate:
  scheduler tests for preemption, starvation and deadline safety.
- **Season upgrade budget and pack preference**: per-series auto/singles/wait-for-pack
  strategies are available; the remaining work is to cap episode-by-episode upgrade
  cost, prefer a full-season replacement when enough episodes would change, and
  optionally accept a partial pack only when it covers every known gap. Gate: a
  coverage model shared by approval, inspection, staging and upgrade preview.
- **Movie editions**: model director's cuts, extended editions and different-language cuts
  as distinct versions of the same movie instead of forcing them into one upgrade ladder.
  Gate: metadata and database migration, library naming rules and a decision on whether to
  keep multiple editions side by side.
