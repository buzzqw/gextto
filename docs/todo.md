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

Nothing open.

## Proposals

Each waits for the operator's choice.

- **MCP endpoint**: Gextto's API exposed as tools for an assistant, with a read-only switch. Gate: the operator's choice.
- **Usenet as an acquisition source**: NZB search through the same indexers and an NNTP download path into the same import. Gate: the operator's choice.
