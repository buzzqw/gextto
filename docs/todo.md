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
- **Windows killswitch on a multihomed host** — `OutgoingInterface` binds the source
  address, which on Windows does not pin the egress adapter, so in a VPN configuration the
  traffic can leave by another interface (as in libtorrent issue 8941, where the fix is
  `IP_UNICAST_IF`/`IPV6_UNICAST_IF`). Gate: a Windows host with a VPN and a second adapter
  where a packet capture shows the leak.
- **Hybrid torrents from the v2 swarm** — hybrids are downloaded as v1 only; when the
  swarm of a hybrid has peers in the v2 one and none in the v1 one, the download waits
  without needing to. Gate: a hybrid torrent where the v2 swarm is the only one with peers.

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
- **Portable folder names for existing series** — a series added before the add dialog
  dropped the characters Windows, exFAT and SMB shares refuse keeps its folder; a check
  in Maintenance reports those folders and never moves files by itself. Gate: an
  installation with at least one such folder (none here).
- **Functions too long to review** — `torrentEventWorker`, `HandleTorrentEvent`,
  `formatEventLang`, `seriesRenameApply`, `loadConfigDB` and the candidate pass
  (`candidatePass.try`) are each several hundred lines with state shared across the
  whole body: split them by phase, as the acquisition cycle was split into
  `collectGapReleases`, `readGapFillPolicy` and `candidatePass`. Gate: a behaviour test
  of the event worker (completion, storage move, retry after a restart) that pins what
  the split must keep.

## Proposals

Ideas proposed but not decided yet. An accepted proposal moves to the matching area with
its gate; a rejected one moves to *Not planned*.

- **Relink duplicated copies** — a library file identical to a seeding download but not
  hardlinked to it is linked again to save the space. Gate: an installation where the
  downloads and the library are on the same filesystem (here they are not: the library
  is on the NAS, the downloads on the local disk) and a byte-for-byte comparison before
  linking.
- **Packages for the root** — 114k non-test lines share `package gextto`, so helpers are
  told apart by prefixes (`gh0_`…`gh7_`, `tev_`, `bg_`) instead of by package. Gate: a
  decision on the first boundary (HTTP handlers, acquisition, storage) and one area
  migrated with the suite green.
- **Settings as typed keys** — 110+ keys are read as `cfg.Settings["literal"]` with no Go
  constant, so a typo compiles and returns the default. Gate: constants generated from
  the settings index and one family of accessors migrated.
- **gx-torrent page: translation by key** — the daemon page is still translated after it
  renders (`cmd/gx-torrent/i18n_html.go` re-tokenises the finished HTML), so a value
  inside a phrase stays in English. Gextto's pages already translate while they render
  (`tr`). Gate: the same `tr` helper on the daemon templates and a guard like
  `TestTemplatesTranslateEveryText`.
- **`*Database` split** — 233 methods cover series, movies, torrents, providers,
  blocklist, queues, cycles and maintenance. Gate: `internal/quality` extracted first (the
  moved methods call the root package's scoring).
- **Error wrapping and log wording** — 280 log calls build their sentence with `Sprintf`
  although the logger takes structured fields, and 490 `fmt.Errorf` calls drop the cause
  because they do not use `%w`. Mechanical, area by area, tests green.
- **TLS origination to the proxy** — the proxy is reached in plain text (HTTP CONNECT or
  SOCKS5); an `https://` proxy that terminates TLS itself is not supported, as in
  libtorrent before PR 9039. Gate: an operator who runs an HTTPS proxy.
- **Duplicate connections per IPv6 subnet** — the duplicate-IP gate compares the full
  address, so one host with rotating privacy addresses can hold several slots; libtorrent
  issue 8934 asks for one host per /64, but a /64 is a subnet and may hold many real users.
  Gate: a measurement of the slots one host takes on a public IPv6 swarm.

## Not planned

Closed by choice, not waiting for a measurement:

- A night refresh of the episode titles: the periodic library check already retries the
  "Episodio N" names and falls back to the English title; the files that keep the
  placeholder have no title at the provider in any language.

- WebTorrent/WebRTC peers: niche, and the reference WebSocket tracker serves only
  WebRTC peers to a WebSocket client.
- Native I2P: reachable through a SOCKS5/HTTP proxy.
- Alternative MSE stream ciphers (the AES-128-CTR of libtorrent PR 8585): the cipher is
  negotiated between the two ends, so it is worth nothing on a swarm where no other client
  speaks it.
- HTTP/2 for announces: an announce is one small request per interval and the shared
  HTTP/1.1 transport already reuses the connection (libtorrent PR 8025).
- Pieces above 16 MiB when creating a torrent: older clients refuse them, and a torrent of
  several terabytes still works with more pieces.
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
