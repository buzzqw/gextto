# Beyond the BEPs: non-standard protocol extensions and features

Analysis of the protocol extensions and features that are **outside the BEP
catalogue** (or build on a BEP the engine already implements), with what they
are and how they would be implemented. Date: **2026-10-13**.

This document complements [`bep.md`](bep.md): that one is the **single source of
truth for BEP status** and it shows that the official catalogue (BEP 0–55) is
essentially covered. The four items here are the candidates that remain
*interesting* once the BEP set is complete; none of them is a missing BEP.

Engine detail: [`gx-torrent.md`](gx-torrent.md) (daemon) and
[`../internal/gxcore/GEXTTO.md`](../internal/gxcore/GEXTTO.md) (engine
inventory). Backlog, measurements and strategic direction:
[`evoluzione.md`](evoluzione.md).

> [!NOTE]
> "Non-standard" here means **not a BEP**: the WebSocket tracker and WebTorrent
> are de-facto protocols of the wider ecosystem, the v1 cross-layout merge is a
> client feature, and the decentralized feed is built on BEP 46, which the
> engine already speaks. Nothing in this document changes the BEP tables.

## Index

1. [Summary and recommendation](#1-summary-and-recommendation)
2. [How the four were chosen](#2-how-the-four-were-chosen)
3. [WebSocket tracker (ws / wss)](#3-websocket-tracker-ws--wss)
4. [WebTorrent / WebRTC peers](#4-webtorrent--webrtc-peers)
5. [v1 cross-layout swarm merge](#5-v1-cross-layout-swarm-merge)
6. [Decentralized feeds on BEP 46](#6-decentralized-feeds-on-bep-46)
7. [Dependencies and order](#7-dependencies-and-order)
8. [Impact on the other documents](#8-impact-on-the-other-documents)
9. [References](#9-references)

---

## 1. Summary and recommendation

| # | Item | Value here | Effort | Risk | Status |
|---|---|---|---|---|---|
| 1 | **WebSocket tracker** (`ws://`/`wss://`) | medium-high (new swarms) | medium | low | **done** (see the caveat in §3) |
| 2 | **WebTorrent / WebRTC** peers | low-medium (browser swarms) | very large | high | **not now** |
| 3 | **v1 cross-layout merge** | medium-high (more reuse) | medium-high | medium | **done** (whole pieces) |
| 4 | **Decentralized feeds** on BEP 46 | medium (subscriptions) | small | low | **done** |

The order followed: **4 → 1 → 3**, leaving item 2 for a real browser-interop
requirement. Item 1 unlocks new swarms and lays the signalling groundwork for
item 2; item 4 was the cheapest and reuses code already present; item 3 was the
last frontier of the merge feature and carries a real "wasted work" risk, which
the design bounds.

> [!IMPORTANT]
> **Caveat found while implementing item 1.** The reference WebSocket tracker
> (`bittorrent-tracker`, the de-facto implementation) treats every WebSocket
> client as a WebRTC peer: `Swarm._getPeers` skips non-`ws` peers when
> `isWebRTC` is true, and `numwant` is derived from the number of WebRTC
> `offers`. A native client that speaks the protocol therefore gets **no TCP
> peers** from that tracker; the client is still correct (and useful with
> trackers that do return standard peers, and as the signalling channel of item
> 2), but the standalone peer-discovery value against `bittorrent-tracker` is
> low. This is why the two items are coupled and item 2 is the real unlock.

## 2. How the four were chosen

Starting from the fact that `bep.md` has no worthwhile BEP left (see its
*Remaining items*), I looked for gaps that are **protocol- or ecosystem-level**,
verified them against the code, and kept only those that would change what the
engine can talk to or do:

- the tracker layer accepts only `http(s)` and `udp` today
  (`trackermanager.Get`), so any `ws://`/`wss://` tracker is silently skipped;
- the engine has no WebRTC data channel, so it cannot be a peer of the browser
  swarms;
- the swarm merge needs an identical layout for v1, so two `.torrent`s of the
  same content with a different piece length do not share it;
- the BEP 44/46 client (`GetMutable`/`PublishMutable`) and the
  `xs=urn:btpk:` magnet resolution already exist, but the DHT value is used only
  as a single info hash, not as a feed.

The rest of the exclusions in `bep.md` (8, 17, 30, 34, 45, 51, 54-send, …) stay
excluded for the reasons already documented there.

### How to read the "how I'd implement it" sections

Each item lists: the **protocol**, the **current state** (with the files that
would change), a **design** that reuses existing machinery instead of adding
parallel paths, and the **tests / measurement / docs** the repository rules
require (`AGENTS.md`: targeted tests, before/after measurement on any data path,
both UIs and i18n for anything user-visible, reference documents kept in sync).

## 3. WebSocket tracker (ws / wss)

**What it is.** A tracker reachable over a WebSocket. It is the de-facto
protocol of the WebTorrent ecosystem (the `bittorrent-tracker` project), not a
BEP. The client opens `ws(s)://` to the announce URL and then exchanges
**JSON** messages:

- announce request:
  `{ "action":"announce", "info_hash":<20-byte binary string>, "peer_id":<20-byte
  binary string>, "left":N, "event":"started|completed|stopped|", "numwant":N,
  "offers":[…], "trackerid"? }`
- announce response:
  `{ "action":"announce", "info_hash":…, "complete":N, "incomplete":N,
  "interval":N, "peers":[ {"ip":"…","port":N,"peer id":…}, … ],
  "offer"?/"answer"?/"offer_id"?/"peer_id"?, "failure reason"? }`
- scrape: `{ "action":"scrape", "info_hash":[…] }` → `{ "action":"scrape",
  "files":{ "<ih hex>": {"complete":N,"incomplete":N,"downloaded":N} } }`.

Two details matter:

- `info_hash`/`peer_id` travel as a **JSON string of 20 raw bytes** (one code
  point per byte), not hex and not base64. The Go encoder must emit `\u00XX`
  escapes and the decoder must interpret each rune as a byte.
- the reference server sets `compact = -1` for WebSocket announces ("return full
  peer objects"), so a native client **can** receive `peers` as ip:port objects —
  but in practice `bittorrent-tracker` only tracks WebRTC peers, so a native
  client gains no TCP peers there (see the §1 caveat): the `offers`/`answer`
  fields are only needed for the browser peers of item 2.

**Why it matters here.** Some swarms are only announced on `wss://` trackers
(the WebTorrent trackers, and a growing number of private/public trackers that
add a WS endpoint). Today such a URL is dropped: `trackermanager.Get` returns
`unsupported tracker scheme` and the tier loses it, so we see fewer peers. It
also lays the transport and the JSON signalling groundwork for item 2.

**Current state.**

- `internal/gxcore/internal/trackermanager/trackermanager.go` — `Get()` switches
  on the scheme (`http`/`https`/`udp`; `default` → error).
- `internal/gxcore/internal/tracker/tracker.go` — the `Tracker` interface
  (`Announce`, `Scrape`, `URL`) every tracker implements.
- `internal/gxcore/internal/tracker/httptracker/` — the announce query builder
  and the peer decoders (`decodeAnnouncePeers`, compact and dictionary forms)
  that the WS tracker reuses.
- `internal/gxcore/internal/tracker/udptracker/` — the shared UDP engine, a
  model for a shared WS transport.
- `internal/gxcore/internal/netx/` — the egress the WS dial must use: the
  proxy-aware `DialContext`, the blocklist-aware resolver, and TLS policy.

**Design.**

1. New package `internal/gxcore/internal/tracker/wstracker` implementing
   `tracker.Tracker`. It owns one WebSocket connection per tracker URL and one
   in-flight request at a time (announce and scrape are request/response).
2. WebSocket client. Chosen: the **bounded** RFC 6455 client implemented in-tree
   (handshake, text/binary frames, masking, ping/pong, close, fragmentation,
   a hard frame-size cap), consistent with the "original code" policy of the
   engine and fuzzed like the other wire parsers — instead of reusing
   `golang.org/x/net/websocket` (which survives only as the test-server
   handler).
3. Connection path: dial through the session egress. Reuse the resolver +
   blocklist of `trackermanager` (the same `DialContext` the HTTP transport
   uses) and `tls.Config` for `wss`. A shared WS transport in `TrackerManager`
   pools sockets per URL, like the HTTP transport pools connections.
4. Protocol: build the announce/scrape JSON, encode `info_hash`/`peer_id` as
   20-byte binary strings, send one frame, read one frame, `json.Unmarshal` with
   a size cap. Map the response onto `tracker.AnnounceResponse` /
   `tracker.ScrapeResponse`; reuse `tracker.NewError` for `failure reason` (and
   `retry in` if present). Parse `peers` (full objects, and compact if a tracker
   ever sends it) into `[]*net.TCPAddr`; ignore `offers`/`answer` for now (no
   WebRTC), keeping the fields so item 2 can consume them.
5. Integration: add `case "ws", "wss"` to `trackermanager.Get`. The announcer,
   the tier logic and BEP 31 retry are scheme-agnostic and need no change.
6. Health/telemetry: the tracker health view is also scheme-agnostic; nothing
   user-visible is required.

**Tests / measurement / docs.**

- A test WebSocket server (the `golang.org/x/net/websocket` handler) exercising
   announce/scrape round-trips, a `failure reason`, a malformed/garbage frame, a
   huge frame (cap), and a `wss` TLS case.
- A `make fuzz-smoke` target for the frame decoder and the JSON announce
  decoder (untrusted input), with any crashing input kept under `testdata/fuzz`.
- No data-path change, so no before/after throughput measurement is needed; a
  unit assertion that a `wss://` tracker is now accepted (and dialed through the
  proxy) covers the regression.
- Docs: this document; the tracker section of [`gx-torrent.md`](gx-torrent.md);
  a note in [`bep.md`](bep.md) that `ws`/`wss` trackers are supported but are
  **not** a BEP; READMEs only if it is advertised as a feature.

**Effort/risk.** Medium (a few days): a new client plus one switch case, no
engine surgery. Low risk: purely additive, isolated behind the `Tracker`
interface, and the dial reuses the audited egress.

**Status: done.** `internal/gxcore/internal/tracker/wstracker` (a bounded,
original RFC 6455 client + the tracker), the `ws`/`wss` schemes in
`trackermanager.Get` (sockets closed with the session), tests
(`TestWSAnnounce`, `TestWSScrapeAndFailure`, `TestWSSecure`, a rejected
handshake) and the `FuzzWSReadMessage` smoke target. Remember the §1 caveat:
with `bittorrent-tracker` a native client receives no TCP peers.

## 4. WebTorrent / WebRTC peers

**What it is.** WebTorrent lets browser clients be BitTorrent peers over
**WebRTC DataChannels**. Signalling runs on a WebSocket tracker (item 1): the
client announces `offers`, the tracker relays them, peers answer, and then the
**ordinary BitTorrent wire protocol** runs over the DataChannel (handshake,
messages, pieces). The DataChannel is the transport; everything above it is what
we already implement.

**Why it matters here.** It is the only way to reach brower-only swarms (some
public WebTorrent trackers and `webtorrent`-seeded content). For a home/NAS
media daemon the payoff is limited: the content this project cares about (public
and private trackers) is dominated by TCP/uTP peers, and WebRTC peers are
browser clients that usually appear only for popular public content.

**Current state.** No WebRTC anywhere in the tree; the tracker layer does not
even keep the `offers`/`answer` fields (item 1 would). The engine's peer
transport is a `net.Conn` handed to the incoming/outgoing handshakers
(`internal/gxcore/internal/peerconn`, the acceptor and the handshakers), so a
DataChannel would have to be presented as a `net.Conn`.

**Design (sketch, if it is ever pursued).**

1. Add the signalling to `wstracker`: generate N WebRTC offers per announce
   (`offers:[{offer, offer_id}]`), reply with an `answer` for a remote offer,
   and keep the `offer_id`→pending-peer map (exactly the reference client's
   `_generateOffers`/`_onAnnounceResponse`).
2. A WebRTC stack is unavoidable: ICE, DTLS and SCTP/DataChannel. In Go that is
   `github.com/pion/webrtc` (a large dependency in a codebase that so far stays
   on a small, curated set). There is no realistic in-tree rewrite.
3. Wrap an established DataChannel as a `net.Conn` (Read/Write over
   `DataChannel.OnMessage`/`Send`, a synthetic `RemoteAddr`), then feed it to
   the same handshake path a TCP connection takes (`incomingConnC` or a routed
   connection). The rest of the engine (messages, pieces, choking) is unchanged.
4. Peer identity: WebTorrent uses a `-WW…`-style client prefix and port 0; the
   engine should treat the peer as inbound with no dialable address (it must not
   try to add the DataChannel's fake address to the address pool).

**Tests / measurement / docs.** An interop test against a real browser client is
the only meaningful one; there is no pure-Go reference easily driven in CI. That
is a strong argument to keep it out of scope until a concrete need appears.

**Effort/risk.** Very large (weeks, plus a heavyweight dependency with a fast
release cadence) and high risk (NAT/ICE behaviour, a new parsing/security
surface). **Verdict: not now.** Document it here; revisit only if browser
interop becomes a product requirement. Item 1 is a useful by-product either way.

## 5. v1 cross-layout swarm merge

**What it is.** The current merge only shares a v1 torrent with another that has
an **identical piece layout** (Phase 1). Two `.torrent`s of the same content
with a different `piece length` do **not** share anything in v1; in v2 they do,
because Phase 2 matches files by their BEP 52 per-file Merkle root. This item
extends reuse to v1 with a different layout.

**Why it matters here.** It is the last frontier of the feature just completed
(`evoluzione.md` §6): more pairs of torrents could be "revived" without a
seeder (a re-release with a different piece size, a re-pack, an old media
`.torrent` against a new one).

**Current state.** `internal/gxcore/torrent/localmerge.go`: `mergeFrom` (Phase 1,
identical layout), `prefillV2From`/`mergeV2Files`/`copyV2PieceFromSource`
(Phase 2, v2 by Merkle root), `storePartialBlocks` (Phase 3, 16 KiB blocks). The
gate is `snap.pieceLength != info.PieceLength || snap.length != info.Length`
for Phase 1 and the v2 check for Phase 2.

**Why v1 cannot reuse Phase 2's verification.** Phase 2 trusts a source file
because its **Merkle root** equals ours: the bytes are provably the same file,
so a recovered span (Phase 3) is true content. v1 has only **SHA-1 per piece**,
no per-file identity and no per-block hashes. A destination piece can still be
assembled from verified source pieces and checked against the **destination's
own hash before writing** — that part is safe. What v1 lacks is a cheap way to
know *which* source pieces hold our bytes when the grid differs, and to trust a
sub-piece span without an independent hash.

**Design.**

- **Matching.** Index the source by *file identity by metadata*: the file path
  (after the padding rules) and its length, plus the total length and the piece
  length. This is how a user's two `.torrent`s of the same re-release usually
  look. It is a **heuristic**: it can match two different files that happen to
  share a name and size.
- **Reuse.** For every destination piece, map its file-local range to the source
  file's byte range (the Phase 2 span walk, minus the `pieces root` gate), read
  the **whole** source pieces that fully cover the span, and assemble the
  destination piece. Then:
  - verify the **whole** destination piece against its own hash, and only then
    write it (identical to Phase 2's rule). A wrong name/size match fails here
    and costs only reads/hashes, never corruption;
  - **partial** destination pieces (only part of the source verified) reuse the
    verified spans at block level exactly like Phase 3 (`storePartialBlocks` +
    `partialBlocks`). The existing failure path already handles a hash mismatch
    on a pre-filled piece: it drops the local blocks, re-downloads the piece
    whole and does **not** blame the peer (`handlePieceWriteDone`). That is the
    important safety net for a heuristic v1 match.
- **Guardrails.** Keep the BEP 27 private guard; keep the "never trust an
  unverified source piece" rule; consider a cap on the wasted I/O (e.g. stop a
  candidate after N failed whole-piece verifications) so a false match cannot
  cost a full extra read of a large source.
- **The alternative some clients use** (BiglyBT/ED2K) is a content hash over
  fixed-size file chunks, which makes the match cryptographic instead of
  heuristic. That is heavier: it needs the chunk hashes published somewhere
  (they are not in the `.torrent`), so it only works inside a client that built
  its own index. Out of reach for us.

**Tests / measurement / docs.** Unit tests for the v1 different-layout reuse
(whole and partial), a false-match test that must end with the piece downloaded
whole and no corruption, and an end-to-end daemon case; a real-data measurement
like the Phase 3 harness if the feature lands. Update `evoluzione.md` §6 and
[`gx-torrent.md`](gx-torrent.md).

**Effort/risk.** Medium-high. The **risk is wasted work, not correctness**: the
final destination-hash check plus the existing "drop local blocks, download
whole" path keep corruption out, but a bad heuristic match can read and hash a
large source for nothing. That is why the guardrail above (and the fact that the
v2 path stays preferred) matters.

**Status: done (whole pieces).** `prefillV1From`/`mergeV1Files`/
`copyV1PieceFromSource` in `internal/gxcore/torrent/localmerge.go`; matched by
file path and length; **whole pieces only** (no Phase 3 blocks for v1, since
there is no per-block hash to justify a partial write); a candidate is abandoned
after `maxV1VerifyFailures` (64) hash failures, so a false match cannot read the
whole source; a mismatch leaves the storage untouched. Tests:
`TestPrefillV1MergesDifferentLayout`, `TestPrefillV1PartialSourceSkipsStraddlingPiece`,
`TestPrefillV1WrongMetadataMatchIsSafe`.

## 6. Decentralized feeds on BEP 46

**What it is.** A subscription channel published in the DHT as a **BEP 44
mutable item** (signed with an Ed25519 key, addressed by `urn:btpk:<pubkey>`),
whose value lists torrents/magnets. A subscriber polls the item and adds the new
entries. It is the decentralized counterpart of an RSS/Atom feed (BEP 36/39),
which the application layer already supports.

**Why it matters here.** The engine already has the hard parts: a BEP 44 **get
and publish client**, and the magnet `xs=urn:btpk:` resolution. A feed is a thin
layer on top, and it gives a takedown-resistant, tracker-free update channel for
the monitoring use-case (subscribe to a curator's key instead of a URL).

**Current state.**

- `internal/dht/bep44_client.go` — `GetImmutable`/`PublishImmutable`,
  `GetMutable`/`PublishMutable` (Ed25519, salt, sequence), a bounded iterative
  client; `StoredItem` carries the value and `seq`.
- `internal/gxcore/internal/magnet/magnet.go` — `BTPKPublicKey`/`BTPKSalt`,
  `HasBTPK()`.
- `internal/gxcore/torrent/session_add.go` — `resolveBTPK` calls `GetMutable`
  and `bep46InfoHash` expects the value to be a dictionary with a 20-byte `"ih"`
  (a single torrent).
- Application layer: Gextto's RSS/wanted pipeline (`internal/rss`, the Wanted
  page) already ingests feed items and drives downloads.

**Design.**

1. **Value format.** Define a small, versioned bencode value for a feed, e.g.
   `{"v":1,"name":…,"items":[{"ih":<20 bytes>,"name"?,"magnet"?}, …]}`. Keep the
   single-`ih` form working (it is the current `xs=urn:btpk:` contract) and make
   the reader accept either a single item or a feed list.
2. **Subscription object.** Persist, per subscription: the public key (+ salt),
   the last seen `seq`, the desired save path, and the wanted/filter rules. This
   mirrors an RSS feed definition and can live next to the existing feed state.
3. **Resolver.** A small loop (in the daemon, like the feed checker) that every
   N minutes calls `GetMutable(pub, salt, &seenSeq, timeout)` and processes new
   items: turn each into a magnet/torrent, hand it to the add pipeline, and
   deduplicate by info hash + the "taken" ledger Gextto already keeps.
4. **Publisher (optional).** `PublishMutable` a feed listing what this instance
   shares, so a second instance can subscribe — a self-hosted, portable
   subscription. This needs a key store (generate/import an Ed25519 key) and a
   small UI.
5. **UI.** A "DHT feed" section next to RSS/wanted (label, key, last seq, last
   error), in all six languages, with a tooltip per field, per the accessibility
   and i18n rules.
6. **Safety.** Treat the item as untrusted input: cap the value size, validate
   the info-hash length, optionally require a magnet/torrent whose hash matches,
   and keep the existing SSRF guard on any `xs=`/`as=` inside the feed.

**Tests / measurement / docs.** Unit tests for the value codec (single and
list, malformed, oversized), a fake DHT for the resolver loop and the
deduplication, and an end-to-end publish→subscribe round-trip between two
in-process DHT nodes. Docs: this document, the daemon page, and the Gextto
manual section for the new feed source (manuals are embedded → require a build
and a restart).

**Effort/risk.** Small-to-medium, low risk (a new feeder on top of audited
primitives; no data-path change). The value depends on having feeds to subscribe
to, which is why the **publisher** side (point 4) is what makes it a complete,
single-player-useful feature.

**Status: done (client side).** Engine: `bep46Feed` decodes both the single
`{"ih"}` form and a feed (`{"items":[{"ih","name"}]}`), and
`Session.ResolveDHTFeed` fetches it. Daemon: a feed URL of the form
`dht://<pubkey-hex>[?salt=<hex>]` is resolved and mapped to RSS items (magnets),
reusing the whole feed pipeline, plus `GET /api/v1/dht-feed` for external
consumers. The **publisher** side (generating and signing a feed) is not
implemented yet.

## 7. Dependencies and order

- **1 → 2.** The WebSocket tracker is the WebRTC signalling channel, so item 1
  is a strict prerequisite of item 2. Item 1 has standalone value (full peer
  objects for TCP peers), so it is not wasted if item 2 never happens.
- **3** is independent (engine-side reuse) and does not touch the tracker layer.
- **4** is independent and small on the client side; the publisher side wants
  the same settings/UI plumbing as the RSS feeds.

Suggested sequence: **4** (small, self-contained, immediate utility) → **1**
(new swarms, groundwork) → **3** (feature depth, with a measurement) → **2**
(only on a real browser-interop requirement).

## 8. Impact on the other documents

- [`bep.md`](bep.md) stays the BEP-only source of truth. The WebSocket tracker,
  WebTorrent and the v1 merge are **not** BEPs, so they belong to this document
  and to [`gx-torrent.md`](gx-torrent.md); `bep.md` only gets a pointer.
- [`gx-torrent.md`](gx-torrent.md): tracker section (WS), *Local data reuse*
  (v1 cross-layout), DHT/BEP 46 section (feeds).
- [`evoluzione.md`](evoluzione.md): the backlog entries and the measurements
  when an item is actually implemented; this document stays the analysis, not
  the changelog.
- READMEs and manuals only when a feature is user-visible (item 4's UI, item 1
  if advertised).

## 9. References

- Index of BEPs, `https://www.bittorrent.org/beps/bep_0000.html` (catalogue ends
  at BEP 55 plus the pending BEP 1000).
- `bittorrent-tracker`, WebSocket client and server
  (`lib/client/websocket-tracker.js`, `lib/server/parse-websocket.js`): the JSON
  announce/scrape and the `compact = -1` full-peer behaviour the WS tracker
  relies on.
- WebTorrent, `https://webtorrent.io` (WebRTC peers and the signalling flow).
- BEP 44 (mutable items) and BEP 46 (`urn:btpk:`), already implemented; the DHT
  client API in `internal/dht/bep44_client.go`.
- `internal/gxcore/torrent/localmerge.go`, `session_add.go` and
  `internal/gxcore/internal/trackermanager/trackermanager.go` for the current
  state referenced above.
