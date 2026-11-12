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

Nothing open.

## Gextto, acquisition and UI

Nothing open.

## Proposals

Gaps found comparing gx-torrent and Gextto with Tixati, qBittorrent, BiglyBT and Deluge
(with its plugins). Each waits for the operator's choice.

- **Upload split between trading and seeding** (Tixati): give the upload first to peers that send data back to a torrent still downloading, and the rest to pure seeding, so seeding does not slow a download on a home line. Gate: the operator's choice, then a before-and-after measure on the shaped 100 Mbit/s scenario.
- **Per-torrent bandwidth weight** (Tixati, BiglyBT): a relative share (50 %, 200 %) next to the absolute limits, so one torrent can go first without a fixed cap. Gate: the operator's choice.
- **Client filter by peer ID** (Tixati, BiglyBT): refuse or deprioritise clients that only take (leech-only clients, fake identities), with a list the operator can edit. Gate: the operator's choice.
- **Trackers added from a list URL** (qBittorrent 5): append the trackers of a public list, refreshed periodically, to new public torrents (never to private ones). Gate: the operator's choice.
- **Peer sources per torrent** (BiglyBT): turn DHT, PEX and LSD on or off for a single torrent, not only for the whole session. Gate: the operator's choice.
- **DHT page** (Tixati): node table, active searches and whether the DHT port receives incoming packets, on the daemon page and in Gextto's diagnostics. Gate: the operator's choice.
- **Releases packed in RAR archives** (Deluge Extractor): extract a video packed in a multi-part RAR before importing it into the library, keeping the archive for seeding. Gate: the operator's choice, then a real release that arrived packed.
- **Peer country** (qBittorrent, Deluge, BiglyBT): a country column in the peer list, from an offline database with no network call. Gate: the operator's choice.
- **uTP on a shaped line**: gx-torrent is fourth of four on the 100 Mbit/s uTP-only scenario (11.15 against 11.50–11.80 MB/s); find where the gap comes from and close it. Gate: a profile of that scenario.
- **Memory on a shaped line and CPU at rest with 100 torrents**: 30 MiB of own memory against 10–13 for qBittorrent, anacrolix and Transmission, and 0.13 % of a core against 0.01–0.06 for anacrolix and qBittorrent. Gate: heap and CPU profiles of those two scenarios.
