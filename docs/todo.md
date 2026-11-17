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

- **The last second on a shaped line**: on the 100 Mbit/s scenarios gx-torrent is level with the fastest clients at half the file and loses about 1.2 s in the second half (both TCP and uTP); it is not the start (the first block arrives in 13 ms locally) nor the endgame order (block sharing between duplicates was measured: no gain). Gate: a CPU and goroutine profile of the last seconds of the transfer (`GX_COMPARE_PPROF_DIR`, `first_s`/`half_s` in the harness).
- **Compressed RAR 4 and solid archives**: extract them too (RAR 4 needs its own LZ and PPMd decoder). Gate: a real release that arrived that way.
- **Memory on a shaped line**: own memory while downloading on the shaped 100 Mbit/s line is above qBittorrent, anacrolix and Transmission; close the gap on the paths a heap profile names. Gate: a heap profile of that scenario inside the comparison harness.
- **CPU at rest with 100 torrents in the comparison**: the unchoke round now sleeps while a torrent has no peers (lower idle CPU in `TestMeasureIdleTorrents`); confirm the comparison row. Gate: the next gx-torrent round of `make measure-compare`.

## Gextto, acquisition and UI

- **emule-qt as the alternative eD2k backend** (one or the other with aMule, optional): the REST client is in `internal/emuleqt`. Next, in order: one eD2k backend contract in front of the aMule client, with the emule-qt adapter behind it and the backend chosen in *Configuration → eD2k e Kad* (aMule / emule-qt, address and API key for a core already running); then the managed core in Docker (`ghcr.io/moddermule/emuleqt`, host network, its config and data folders under Gextto's data folder, REST API on with a generated key, the `/data` path translated to the host one, a daily check for a new release with pull, recreate and rollback to the previous image when `/api/v1/health` or the contract version fails); then the eD2k page, manuals and the aMule guide covering both. The half-way merge of a torrent from the eD2k `.part` stays aMule-only until emule-qt reports its part files. Gate: none, in progress.

## Proposals

Each waits for the operator's choice.

- **MCP endpoint**: Gextto's API exposed as tools for an assistant, with a read-only switch. Gate: the operator's choice.
- **Usenet as an acquisition source**: NZB search through the same indexers and an NNTP download path into the same import. Gate: the operator's choice.
- **Series status marks and one design system**: the status badges of a series still carry an emoji in the text (`🏁 terminata`, `▶ in corso`, `⏳ rinnovo incerto`, `🕒 in arrivo`) because that same label is also a filter value, so the mark must be split from the text before it becomes an icon; then one design system (colours, spacing, type scale, radius) shared with the daemon and the two Gextto stylesheets folded into one with few breakpoints. Gate: the operator's choice; the visual review runs headless with `scripts/screenshots.sh`.
- **Settings by level**: core options shown first, an "Show advanced options" switch, a mark on rows and tabs changed from their default. Gate: the operator's choice.
- **Faster flows**: a missing count on the Dashboard linking to the tab, undo instead of confirmation for reversible removals with a styled dialog for the rest, page navigation without full reloads (`hx-boost`). Gate: the operator's choice; `hx-boost` is done alone, with the Playwright suite green before and after.
- **Target quality timeframe in pending queue**: releases below the preferred target quality wait in the pending queue for a configurable timeframe (e.g. 4 hours), while any release meeting the target quality bypasses the delay and downloads immediately. Gate: the operator's choice.
- **Pre-flight swarm health probe**: a fast DHT or tracker scrape probe for releases with missing or zero seeder counts before queuing them, deferring unseeded torrents instead of occupying active slots until stalled timeouts. Gate: the operator's choice.
- **External watchlist sync**: an optional background worker importing wanted titles from public Trakt or Letterboxd watchlists directly into Gextto's wanted queue. Gate: the operator's choice.
