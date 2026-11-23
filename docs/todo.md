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

- **Release search with no loading state**: the manual release search can run for 90 seconds
  with an unchanged button, no indicator and the previous results still on screen, so it is
  easy to start it twice. Gate: the operator picks it among the UX items; reuse the cancel
  and disabled-button pattern already used by the missing-episode search.
- **Focus and clicks lost across HTMX swaps**: polling replaces whole panels (state chips,
  integrity, maintenance, update, jobs) and the click lands on a detached node while the
  keyboard focus falls back to `<body>`; the torrent detail drawer cannot restore focus to
  its opener after a refresh. Gate: a decision on how much polling markup to change —
  `hx-preserve` and stable ids, or updating only the changed cell contents.
- **Destructive actions without confirmation or rollback feedback**: removing an HTTP
  download deletes the row before the answer arrives and has no confirmation, and exporting
  a `.torrent` whose metadata is still missing navigates to a plain-text 404 and loses the
  list. Gate: decide the confirmation policy for partial files and whether the export should
  fetch a blob with a toast instead of navigating.
- **Mobile search missing on the daemon page**: below 760 px the search field is hidden, the
  `/` shortcut focuses an invisible input, and the name filter only sees the first 300
  loaded rows, so a torrent further down is reported as absent. Gate: decide between loading
  all rows when a search term is present and counting matches on the server.
- **Errors that disappear and are not announced**: the daemon's toasts are polite, have no
  close button and vanish after 6.5 s, while the Gextto ones are sticky alerts; a few async
  results (new warnings counter, font detection) are written into silent containers and one
  of them is still Italian in the other five languages. Gate: align both interfaces on one
  announced, readable, translatable notification pattern.
- **ARIA patterns the automatic guards do not check**: the detail tabs have no arrow-key
  navigation, `aria-controls` or `aria-labelledby`; sortable headers have no `aria-sort` and
  a 0.35-opacity indicator; every row checkbox has the same accessible name; a poster link
  is both focusable and `aria-hidden`. Gate: an accessibility round with a manual
  screen-reader check and axe extended to the pages it currently skips.
- **Catalogue values that are not really translated**: the English catalogue still holds two
  Italian values, some navigation terms are unnatural in de/fr/es/pl and German mixes two
  levels of formality. Gate: a native-speaker review of the navigation terms, plus a guard
  that rejects a value equal to the Italian source in a non-Italian catalogue.
- **Navigation that loses the place**: row actions and the Guided/Expert switch reload the
  whole page, and the settings search only changes the hash, so a refresh returns to the
  old section. Gate: decide how many routes to convert to in-place swaps, keeping the
  in-app unsaved-changes message.
- **Operational state that is easy to misread**: starting a cycle only shows a toast that
  expires, the download list gives no visible sign when automatic refresh is off, the two
  log viewers refresh differently, and the daemon settings show the API keys in clear text
  without the qBittorrent URL. Gate: the operator picks which of these small indicators and
  adjustments to land first.
