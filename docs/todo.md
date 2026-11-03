# Open items

The maintained list of work that is not done. One item per line: what to do and what
unblocks it. When an item lands it leaves this file; when a new one is accepted it is
added here. There is no history and no design essay: the state of the product is in
[`gx-torrent.md`](gx-torrent.md), [`gx-core.md`](gx-core.md) and the manuals, the numbers
in [`measurements.md`](measurements.md).

**Rule.** No speculative work: an item starts only when its gate is met. An item with no
gate is ready to pick up. Every idea proposed by the operator or by the assistant in a
working session is written here in the same cycle: under the matching area once accepted,
under [Proposals](#proposals) while it is still a decision, and under
[Not planned](#not-planned) when rejected. An idea must not stay only in the chat. Do not
add a diary: a sentence that would still be true after the item lands does not belong
here.

## Engine (gx-core, gx-torrent)

- **Reused read-cache buffers** — each block read for an upload allocates and clears a new
  buffer, about a tenth of the CPU with the collector. Gate: a way to recycle a buffer that a
  cache eviction cannot pull from under a block being copied.
- **Peer turnover** — at the connection cap, close the slowest peers periodically and
  try new addresses (`prunePeers` frees only a useless peer today). Gate: a real swarm
  larger than the cap where turnover beats the current download rate on the same
  torrent.
- **Choking on a real swarm** — campaign against libtorrent/qBittorrent on the same
  public torrent, with the same policy and metrics (upload efficiency while seeding,
  reciprocation, first-block time, stall rate). Gate: a host with a reachable inbound
  port; only a gap justifies touching `internal/unchoker`.
- **Mixed-mode TCP/uTP arbitration** — throttle TCP when uTP shares the line, as
  libtorrent does; today only the transfer caps react. Gate: a mixed swarm where the
  current caps lose throughput or latency.
- **uTP acknowledgements and datagrams** — coalescing ACKs beyond the current short delay
  needs 20 ms or more, which inflates the RTT the RTO is built on; the alternative is batched datagram
  I/O (`recvmmsg`/`sendmmsg`). Gate: a uTP-heavy session with an inbound port where
  `recvfrom`/`sendto` is a real share of the syscall time.
- **Graduated file priority (0–7)** with mixed-priority files. Gate: a torrent with
  mixed priorities where the current order is wrong.
- **Parallel requests per web seed** — one range download runs per web seed at a time;
  a source that answers each file in seconds (archive.org) is bound by that latency.
  Gate: a torrent served mostly by web seeds where parallel requests beat the current
  version on the same source.
- **DHT feeds** — the resolver and the publisher exist; what is missing is a real
  multi-client audience. Gate: at least two independent clients consuming a published
  feed.
- **DLNA** — transcoding and frames extracted from the video are missing; browsing,
  Search, Range playback, subtitles, library folders, artwork and Xbox/WMP are in
  place. Gate: a client that needs transcoding or a thumbnail taken from the video.
- **10 Gbps pipelining** — a validation, not code: the local single-pair ceiling is far
  below the target, multi-piece included. Gate: a multi-peer swarm on a fast link.

## Gextto, acquisition and UI

- **Managed amuled (optional)** — a convenience starter for eD2k: a private config
  directory, a generated EC password and the ports. It is not a Gextto engine and it is
  not required. Gate: users who want eD2k without configuring aMule by hand.
- **Remote aMule** — import completed files from a remote core with a path mapping like
  `qbittorrent_path_mappings`. Gate: an operator running aMule on another host.
- **Season packs before the rename** — probe the pack's files before the rename; today
  they are compared by name after placement. Gate: a per-file probe that does not slow
  the completion of a large pack.
- **Sonarr/Radarr v3** — acceptance test of the Jellyseerr subset against a real
  Jellyseerr; its calls decide any correction. Gate: a Jellyseerr instance driven by a
  real request.
- **Latency guard in `ping` mode** — a user with little bandwidth who only needs a low
  latency (a call, a game) fills no queue, so a ping alone cannot see them; in
  `both`/`router` the WAN counters do. Gate: a real call or game next to a saturated
  download, measured in `both` and in `ping`.

## Proposals

Ideas proposed but not decided yet. An accepted proposal moves to the matching area with
its gate; a rejected one moves to *Not planned*.

- **Escalating dead-swarm block** — a release that comes back after its block expires and
  is refused again for an empty swarm would stay away longer the next time (a week, then
  a month) instead of a week every time. It needs a strike count that survives the
  expiry, while today the expired rows are purged as soon as they are read, by design.
  Gate: a decision on the second period and on how much history to keep for the count.
- **Relink duplicated copies** — a library file identical to a seeding download but not
  hardlinked to it is linked again to save the space. Gate: an installation where the
  downloads and the library are on the same filesystem (here they are not: the library
  is on the NAS, the downloads on the local disk) and a byte-for-byte comparison before
  linking.
- **Portable folder names for existing series** — the add dialog now drops the characters
  Windows, exFAT and SMB shares refuse, but a series added earlier keeps its folder and
  the path stored in the library. A check in Maintenance could report them and rename the
  folder with its files. Gate: a decision on moving files automatically or only reporting.

## Not planned

Closed by choice, not waiting for a measurement:

- A night refresh of the episode titles: the periodic library check already retries the
  "Episodio N" names and falls back to the English title; the files that keep the
  placeholder have no title at the provider in any language.

- WebTorrent/WebRTC peers: niche, and the reference WebSocket tracker serves only
  WebRTC peers to a WebSocket client.
- Native I2P: reachable through a SOCKS5/HTTP proxy.
- Alternative storages (blob, bolt, SQLite).
- `share_mode`: it serves ratio farming, not a library.
- Full `*arr` v3 compatibility, multi-user authentication, Usenet, write-back cache,
  specialist fields in the add form, a content layout in Gextto's Add dialog, a UI
  benchmark in CI, a cache cap on NFS, Trakt.

Engine designs rejected on a measurement — `io_uring`/AIO, contiguous-write coalescing,
multi-path `get_peers`, the anti-Sybil cap on by default, super-seeding by default,
async `state.json`, a lock-free read cache — are in
[`measurements.md`](measurements.md#closed-designs) with the number that closed each one. Do not
reopen one without new data.
