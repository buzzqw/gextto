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
- **Mobile search missing on the daemon page**: below 760 px the search field is hidden,
  the `/` shortcut focuses an invisible input, and the name filter only sees the first 300
  loaded rows, so a torrent further down is reported as absent. Gate: decide between loading
  all rows when a search term is present and counting matches on the server.
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
