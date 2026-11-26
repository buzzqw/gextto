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

## Proposals

- **Quality profiles: movie and series scope**: the same profile serves both libraries and the request-manager picker cannot filter by type. Gate: decide whether to add a `kind` field (movie/series/both, default both) and filter the pickers.
- **Quality profiles: linked titles**: a profile is copied into a title at save time and titles do not store its id, so editing a profile never reaches existing titles. Gate: decide whether to keep copy-only semantics or add a "re-apply to linked titles" action with a preview of the affected titles.
- **Rules in the qBittorrent-compatible API**: the native page now keeps rules only as an advanced fallback, but `/api/v2/rss/rules/*` still exposes them to external clients, and the one-time migration switches stored rules off for those clients too. Gate: decide whether external clients still need that surface or the API should report no rules and the compatibility note in `gx-core.md` be updated.
- **gx-core screenshots after the merge**: the manuals describe the single acquisition page, but the Series & movies screenshots still show the old state labels and the rules tab on the RSS page. Gate: regenerate `docs/screenshots/gx-core/{it,en}` from a running standalone daemon.
- **Ingress content filter**: a download can be accepted and put in the import queue with an executable disguised as a video, and the bad torrent keeps the good one from being imported. Gate: choose between block, quarantine and an allow-list, and measure the cost of false positives on archives and legitimate content.
- **Portable, explainable configuration**: operators treat profiles, indexers and rules as code and lose weeks of work when a NAS dies without a saved copy; Gextto has backup on update and feed import/export but not a versionable export of the whole configuration. Gate: decide the scope and format and how secrets are handled; it touches the three quality-profile items but is wider than reassigning a profile.

- **Persistence transactions**: replace the remaining whole-library read-modify-write paths with patch transactions or revisions. Gate: concurrent save tests beyond the series/movie endpoints.
- **Resume journal evolution**: associate layout generation and checksums with persisted piece state, and rehash selectively after unclean shutdown or sync failure. Gate: crash-consistency and resume-performance tests.
- **Lifecycle and persistence boundary evolution**: consolidate task/process supervision and repository transactions behind explicit unit-of-work and shutdown phases. Gate: lifecycle tests covering HTTP, jobs, watchdog, crossnet and managed processes.
- **Release provenance evolution**: publish signed, immutable build-once artifacts and promote the same verified bytes to stable, and state which external metadata services a run contacts. Gate: signature verification, promotion tests and a documented dependency list.

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
- **Operator-configured source penalties**: the factual reliability report and
  possible-HnR warnings are available; optional, transparent and reversible
  penalties for a source or release group remain open. Gate: an explicit policy
  chosen by the operator, append-only reasons and an undo path; no opaque score.
- **Movie editions**: model director's cuts, extended editions and different-language cuts
  as distinct versions of the same movie instead of forcing them into one upgrade ladder.
  Gate: metadata and database migration, library naming rules and a decision on whether to
  keep multiple editions side by side.
- **Additional list providers and watched-state sync**: Simkl now has a
  transactional selectable preview with external-id/title-year matching; IMDb,
  Trakt and MDbList imports plus Plex/Jellyfin watched-state suppression remain
  open. Gate: the provider/OAuth model and an explicit policy for watched and
  partially watched seasons.
