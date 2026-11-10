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

- **Release parsing CPU** — the quality/language scans, the subtitle rewrite and
  the common episode shapes are in place; the x/season-word/in-progress matchers
  and the absolute-episode parser still use regexes. Gate: a profile where the
  regexp share climbs back above 20 % of a cycle; then replace the next pattern
  and re-measure on the same corpus.
- **Cycle allocation churn** — a 38k-release cycle still allocates about 1.4 GB,
  now mostly in `encoding/xml`'s tokenizer and in the HTML parser, which sets
  the peak and the fragmented heap left behind. Gate: a corpus of real feeds
  that a replacement reader must parse exactly like `encoding/xml` (entities,
  CDATA, namespaces, attributes), because the remaining gain is about 12 % of
  the allocations and 7 % of the CPU, not enough for a rewrite without that
  proof.
- **Functions too long to review** — `torrentEventWorker`, `HandleTorrentEvent`,
  `formatEventLang`, `seriesRenameApply`, `loadConfigDB` and the candidate pass
  (`candidatePass.try`) are each several hundred lines with state shared across the
  whole body: split them by phase, as the acquisition cycle was split into
  `collectGapReleases`, `readGapFillPolicy` and `candidatePass`. Gate: a behaviour test
  of the event worker (completion, storage move, retry after a restart) that pins what
  the split must keep.
- **Two web engines still fail** — bitsearch answers HTTP 500 to every query; bt4g
  answers 403 without FlareSolverr and no result even through it. Both stay enabled
  and are reported unreachable in the cycle. LimeTorrents is fixed (three addresses
  tried in order: store, www.limetorrents.fun, net). Gate: a working endpoint or a
  parser that reads what the block page serves; otherwise they go, and that is the
  operator's decision.

## Measurements

- **The documents after a round** — the round of 2026-11-07 is in
  `docs/measurements.md`, `README.md`, `README.it.md`, `docs/README.md` and the
  benchmark rule in `AGENTS.md`; every future round replaces those rows and the
  tables, and nothing else. Gate: a round finished.

## Proposals

Ideas proposed but not decided yet. An accepted proposal moves to the matching area with
its gate; a rejected one moves to *Not planned*.

- **A client-wide cap on unverified data** — anacrolix/torrent holds at most
  `MaxUnverifiedBytes` (64 MiB, `config.go`) of received bytes that are not yet hashed and
  written; gx-core bounds a piece and a torrent, not the client. That is why the library's own
  memory stays at 11 MiB at full speed ([measurements](measurements.md)). Gate: a run with
  several torrents downloading at once where the hashing backlog is the peak.

- **Next search inside the restart line** — after a restart the «Next search at…» line still
  follows the restart sentence, so a restart reads as two lines. Gate: the scheduler's first
  due time known when the start line is written, without delaying the start.
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
- **IP address reported to trackers** — send an operator-chosen address in the announce
  (HTTP `ip=` and the UDP equivalent). Gate: a NAT or VPN setup where the tracker records
  the wrong address.
- **Incomplete file extension** — append `.!qB` while a file is incomplete and rename it
  at completion, with resume, recheck, move and streaming kept consistent. Gate: a
  workflow that reads the folder during a download.
- **Bundle more languages** — the pack loader is in place (`<data>/lang/*.json`, English
  fallback, partial packs allowed); the six compiled catalogs are still the only complete
  ones. Gate: a translator for each language.
- **Piece buffers in segments** — a piece now takes its buffer at its first block, but the
  piece being filled and the ones waiting to be written still hold the whole piece: taking
  256 KiB segments as blocks land would hold only the bytes received. It touches the hash
  (v1 and v2), the write, web seeds and the local block merge. Gate: a measurement where
  those pieces dominate the memory, with the full-speed download still at 290 MB/s.
## Not planned

Closed by choice, not waiting for a measurement:

- Embedded tracker (an HTTP announce endpoint of its own): the operator chose not to
  build it in this cycle.
- HTTPS on the web interface: the operator chose to keep a TLS terminator in front.
- Build-time source patching, libtorrent 1.2/2.x variants and musl/glibc «static-ish»
  builds: gx-core is our own pure-Go code with no libtorrent, and the daemon already
  links nothing (`CGO_ENABLED=0`), so there is no third-party tree to patch and no
  second way to build it.
- Distribution parity with qbittorrent-nox-static (one-line installer, user unit, extra
  architectures, provenance/SBOM, published image, mounting under a sub-path): packaging
  work, not native client features, and not the direction chosen by the operator.
- Notification texts in the interface language: the daemon's page is translated, the
  messages it sends (Telegram, Discord, Slack, Gotify, ntfy, Pushover, webhook, e-mail,
  script) stay in English on purpose — a channel can be shared by several people and the
  text is also read by scripts.
- The web engine `torrentz.eu.com` (checked on 2026-11-10): it is an article page about
  Torrentz with a **Google Custom Search** widget on it (`cse.google.com/cse.js`), and no
  search backend of its own — `/search?q=`, `/search/<q>/`, `/any?q=` and `/rss?q=` all
  answer `404`, `/?q=` returns the same 14 KB page, and no line of it carries a magnet or
  a torrent row. The `#gsc.tab=0` in the address is the widget's own tab. There is nothing
  for an engine to read: the results would be Google's, not torrents. Gate: a server-side
  search that returns torrents with magnets, in HTML, from the site itself.
- One switch for test mode (merging `dry_run` into «Automatic cycle»): the operator does
  not want to invest in it — users want to download, not to test; `--dry-run` stays for
  the test suites.
- Preventing a second row when the same film or series is added under another spelling
  (a colon present or absent, one hyphen more, a double space): the operator does not
  mind the duplicated rows, and the release match already ignores punctuation.
- Listing every vanished episode inside the one line per series: the operator found the
  list excessive; the line says how many library files are missing.

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
- `share_mode` (libtorrent's on-demand mode for ratio farming): the operator chose not to
  build it — it leaves torrents incomplete on disk and Gextto is a library, not a seedbox.
- Removing the dead `share_mode`/`upload_mode` fields from the torrent view: nothing reads
  or sets them, they only show up as `false` in `/api/torrents`, and the operator chose to
  leave them where they are.
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
