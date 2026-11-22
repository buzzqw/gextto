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

## Engine (gx-core, gx-torrent)

- **Two torrents that share the files hash them twice on a recheck**: the
  verifier is gentle by design and drops every piece from the page cache, so a
  season pack and its single episodes read the same bytes from disk once per
  torrent. Gate: a measurement of the second recheck with and without a
  session-wide verified-piece cache keyed by file identity, offset, length and
  expected hash; implement only if the second pass drops enough to pay for the
  cache.
- **The last second on a shaped line**: on the 100 Mbit/s scenarios gx-torrent is level with the fastest clients at half the file and loses about 1.2 s in the second half (both TCP and uTP); it is not the start (the first block arrives in 13 ms locally) nor the endgame order (block sharing between duplicates was measured: no gain). Gate: a CPU and goroutine profile of the last seconds of the transfer (`GX_COMPARE_PPROF_DIR`, `first_s`/`half_s` in the harness).
- **Compressed RAR 4 and solid archives**: extract them too (RAR 4 needs its own LZ and PPMd decoder). Gate: a real release that arrived that way.
- **Memory on a shaped line**: own memory while downloading on the shaped 100 Mbit/s line is 28.3 MiB against qBittorrent's 10, anacrolix's 12 and Transmission's 13, and the profile is already there: in `gextto-data/bench/compare-pprof-2026-11-12/line.bin-tcp-line-*-heap.pprof` 13.0 of the 14.6 MiB in use sit in one place, `bufferpool.New.func1` (89 %). Gate: that pool takes its buffers back when a piece is written instead of growing with the connection count, then the scenario is measured again on both TCP and uTP.
- **The state marshal still holds `d.mu` once per coalesced save**: the tick's
  write is now ~14 ns, but `json.MarshalIndent` is ~4 ms at 1000 torrents and
  runs inside the lock every 10 s window. Gate: a deep-copy or copy-on-write
  snapshot encoded outside the lock, with `BenchmarkSaveStateLocked` and a test
  that no state is lost.
- **Webseed piece buffers are still unbudgeted**: the peer path now holds its
  write-cache charge until the bytes are written, but `handleWebseedPieceResult`
  still takes buffers straight from `piecePool`. Gate: the same ownership rule
  for the webseed path, with the budget test extended to it.
- **The DHT peer store costs three structures per contact**: 163 B/contact,
  about 85.6 MB at the default 2048×256 cap, while its comment says ~25 MB.
  Gate: fold the seed flag into the contact and replace the ring with a rotating
  slice; measure live heap at the cap.
- **Piece hashes are stored twice per torrent**: the decoded blob and the raw
  info dict both hold them, ~20 B/piece, ~50 MB for 500 torrents × 5,000
  pieces at rest. Gate: slice the blob out of the raw dict at parse time,
  keeping the info hash and ut_metadata bytes identical.
- **Share limits: any condition or all of them**: Gextto and the daemon stop at
  the first limit that fires (ratio, time, inactivity) and cannot require all
  three together. Gate: a row on both settings pages with its translations.
- **The standalone Wanted reads a multi-season pack as its first season**:
  `FindSeason` in `internal/rss` takes one number, so a pack «S01-S03» is
  offered only for season 1 although its files belong to both. Gate: a season
  range in the feed rules and in the wanted match, with the feed corpus green.
- **The verification progress is lost on a pause**: `stopVerifier` closes the
  verifier and the bitfield is `nil` during a check, so a recheck interrupted at
  90% starts again from zero. Gate: persist a verified prefix (bitfield plus
  count) or let the check finish at low I/O priority, with a test that counts the
  pieces read after stop/start on a large torrent.
- **A separate budget for metadata**: a magnet with no metadata occupies a
  download slot until it resolves, so with `active_downloads=3` only three
  magnets resolve at a time. Gate: a measurement of a bulk add of 50 magnets
  with and without an independent `active_metadata` cap (cloud-torrent uses four
  by default).
- **Local peers are not preferred**: LSD and the LAN limits exist, but the dial
  priority is only BEP 40, so a peer on the same network competes with a WAN one.
  Gate: a `prefer-local-peers` setting with a priority tier above BEP 40 for
  RFC1918/loopback addresses, and a two-client LAN measurement.

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
- **Three sorts call the score inside the comparator**: `web_system.go:136`,
  `web_series.go:240` and `web_movies.go:594` call `ReleaseScore` — which runs
  `FindSeriesMatch` — once per comparison, so a manual search with n results
  pays it O(n log n) times. Gate: the decorated-sort pattern already used at
  `web_system.go:722`.
- **A multi-season pack is not inspected file by file**: the range is parsed now
  (`SeasonEnd`/`SeasonLast`), but the inspector still decides one season at a
  time and the skipped episodes are stored without their season, so a pack
  covering several seasons is downloaded whole and decided at completion. Gate:
  skipped episodes kept as season/episode pairs, then the per-file decision in
  the inspector and its tests.

## Proposals

Each waits for the operator's choice.

- **BT4G answers about one search every ten minutes per address**: the deep gap
  pass asks up to five searches in parallel and the web engines have no per-host
  spacing — `throttleHost`/`penalizeHost` serve only the feed fetch — so the
  queries after the first get `429 Too many requests`, the engine is paused for
  half an hour and the ones already in flight still go out. Gate: the same
  per-host spacing on the web engines, then the operator's choice between using
  bt4g once per window and retiring the engine.
- **Notebook-wide tables on a phone**: the episodes and the download history of a series are blocks on a phone now, but Archivio, Fumetti, Blocklist, Salute and Manutenzione still show tables wider than the screen behind a sideways scroll; give each one the same per-row block, or fold its secondary columns, one page at a time. Gate: the operator's choice; every page with its own check in `mobile.spec.ts`.
- **Cross-seed manager**: adding a torrent whose data is already in the library
  should reuse the blocks and seed it on the second tracker, with per-tracker
  ratio and seed rules, instead of downloading a second copy. Gate: the operator's
  choice; the v1/v2 block reuse and the hardlink library are the building blocks.
- **The status mark is still part of the label**: the badges of a series carry
  an emoji in the text (`🏁 terminata`, `▶ in corso`, `⏳ rinnovo incerto`,
  `🕒 in arrivo`) because that same text is also a filter value, so the mark
  must be split from the label before it can become an icon. Gate: the
  operator's choice; the visual review runs headless with
  `scripts/screenshots.sh`.
- **Faster flows**: undo instead of a confirmation for reversible removals, with a dialog that explains itself for the rest, and the missing count on the Dashboard linking to its tab. Gate: the operator's choice.
- **Pre-flight swarm health probe**: Gextto queues a release on the seeder count
  the indexer reports and waits for the post-start stall probe to notice a dead
  swarm, while a dead torrent holds one of the three active slots. The daemon
  already answers a DHT scrape (`GET /ui/dhtscrape`) and nothing calls it. Gate:
  the operator's choice, then the probe behind that endpoint, with the unseeded
  releases deferred instead of queued.
