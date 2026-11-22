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

## Gextto, acquisition and UI

- **`FindSeriesMatch` recomputes its fingerprint on every call**:
  `configuredSeriesIndex` hashes every configured name at each lookup instead
  of answering from a revision counter, and a 47k-release cycle calls it about
  190k times. Gate: a safe revision counter for in-place series edits, then
  `BenchmarkFindSeriesMatchUnmatched/Matched`.
- **`encoding/xml` copies every feed text node**: ~100–110 MB of the 275 MB
  feed path, about 40 % of its allocations and 7 % of a cycle's CPU. Gate: a
  byte-level scanner over the in-memory body, with the existing feed corpus
  green.
- **A multi-season pack is not inspected file by file**: the range is parsed now
  (`SeasonEnd`/`SeasonLast`), but the inspector still decides one season at a
  time and the skipped episodes are stored without their season, so a pack
  covering several seasons is downloaded whole and decided at completion. Gate:
  skipped episodes kept as season/episode pairs, then the per-file decision in
  the inspector and its tests.
- **Notebook-wide tables on a phone**: the episodes and the download history of a series are blocks on a phone now, but Archivio, Fumetti, Blocklist, Salute and Manutenzione still show tables wider than the screen behind a sideways scroll; give each one the same per-row block, or fold its secondary columns, one page at a time, with its own check in `mobile.spec.ts`.
- **Cross-seed manager**: adding a torrent whose data is already in the library
  should reuse the blocks and seed it on the second tracker, with per-tracker
  ratio and seed rules, instead of downloading a second copy; the archive
  already holds the same release from several trackers. Gate: the v1/v2 block
  reuse and the hardlink library, which are the building blocks.
- **Faster flows**: undo instead of a confirmation for reversible removals, with a dialog that explains itself for the rest. The missing count on the Dashboard is already there.
