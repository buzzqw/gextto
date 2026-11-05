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

Nothing open: every engine item waits for a condition listed under
[Not planned](#not-planned).

## Gextto, acquisition and UI

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

- **Packages for the root** — 114k non-test lines share `package gextto`, so helpers are
  told apart by prefixes (`gh0_`…`gh7_`, `tev_`, `bg_`) instead of by package. Gate: a
  decision on the first boundary (HTTP handlers, acquisition, storage) and one area
  migrated with the suite green.
- **Settings as typed keys** — the constants are generated from the settings index
  (`settings_keys_gen.go`), but many reads still use `cfg.Settings["literal"]`, so a typo
  compiles and returns the default. Gate: one family of accessors migrated.
- **`*Database` split** — 233 methods cover series, movies, torrents, providers,
  blocklist, queues, cycles and maintenance. Gate: `internal/quality` extracted first (the
  moved methods call the root package's scoring).
- **Error wrapping and log wording** — 280 log calls build their sentence with `Sprintf`
  although the logger takes structured fields, and 490 `fmt.Errorf` calls drop the cause
  because they do not use `%w`. Mechanical, area by area, tests green.
- **Archive rows of a removed feed** — a feed deleted from *Sources* leaves its listings
  in the archive; a `.torrent` link that fails is quarantined (6h, then 24h, then a week)
  but still retried. A switched-off indexer is recognised by its URL, a removed feed is
  not: the row does not remember the feed it came from. Storing the feed URL next to the
  source label would let the archive skip those listings the same way. Gate: a feed
  removed from an installation that still sees its old links tried now and then.

## Not planned

Closed by choice, not waiting for a measurement:

- Notification texts in the interface language: the daemon's page is translated, the
  messages it sends (Telegram, Discord, Slack, Gotify, ntfy, Pushover, webhook, e-mail,
  script) stay in English on purpose — a channel can be shared by several people and the
  text is also read by scripts.
- One switch for test mode (merging `dry_run` into «Automatic cycle»): the operator does
  not want to invest in it — users want to download, not to test; `--dry-run` stays for
  the test suites.
- Preventing a second row when the same film or series is added under another spelling
  (a colon present or absent, one hyphen more, a double space): the operator does not
  mind the duplicated rows, and the release match already ignores punctuation.

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

Not feasible here: the gate needs something this installation does not have and cannot
create. Reopen one when its condition appears.

- Choking changes: a public swarm with more leechers than seeds (seeds outnumber
  leechers on the legal ones, and nobody uploads).
- DHT feeds: an audience of independent clients.
- DLNA transcoding and frames from the video: a client that needs them.
- 10 Gbps pipelining: a multi-peer swarm on a fast link.
- Windows killswitch on a multihomed host (`IP_UNICAST_IF`): a Windows host with a VPN
  where a capture shows the leak.
- Hybrid torrents from the v2 swarm: a hybrid whose only peers are in the v2 swarm.
- Managed amuled and remote aMule: operators who ask for them.
- Sonarr/Radarr v3 acceptance: a real Jellyseerr instance.
- Portable folder names for existing series: a folder with characters other systems
  refuse.
- Relinking duplicated copies: downloads and library on the same filesystem.
- TLS origination to an `https://` proxy: an operator who runs one.
- One connection per IPv6 /64: a public IPv6 swarm (this line has no IPv6).

Engine designs rejected on a measurement — `io_uring`/AIO, contiguous-write coalescing,
multi-path `get_peers`, the anti-Sybil cap on by default, super-seeding by default,
async `state.json`, a lock-free read cache — are in
[`measurements.md`](measurements.md#closed-designs) with the number that closed each one. Do not
reopen one without new data.
