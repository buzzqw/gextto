# BitTorrent (BEP) compliance of gx-core

Support status for the BitTorrent specifications (BEPs) of the **gx-core** engine
(`internal/gxcore`) and the `gx-torrent` daemon (`cmd/gx-torrent`).

This document is the **single source of truth for BEP status**: the status reflects
the code, not comparisons with other clients. The per-file detail is in
[`internal/gxcore/GEXTTO.md`](../internal/gxcore/GEXTTO.md) (engine inventory)
and in [`gx-torrent.md`](gx-torrent.md) (daemon); rationale, measurements and
backlog in [`evoluzione.md`](evoluzione.md).

Status legend: **yes** = implemented and tested; **partial** = only a part;
**no** = absent; **excluded** = out of scope by choice.

Long explanations are kept in the notes below each table, so that the columns
stay narrow and the tables remain readable.

## Peer wire and metadata

| BEP | Specification | Status |
|---|---|---|
| 3 | The BitTorrent Protocol Specification (core TCP/UDP wire) | yes |
| 4 | Reserved bits in the handshake | yes |
| 6 | Fast Extension (`have all`/`have none`, `suggest`, `allowed fast`) | yes |
| 9 | `ut_metadata` (metadata from magnet) | yes |
| 10 | Extension Protocol (handshake `m`) | yes |
| 11 | Peer Exchange (`ut_pex`, IPv4 + `added6`/`dropped6`) | yes |
| 12 | Multitracker Metadata Extension (tiered `announce-list`) | yes |
| 14 | Local Service Discovery (LSD, IPv4 and IPv6) | yes |
| 16 | Super-seeding (initial seeding, one piece at a time) | yes |
| 19 | WebSeed ("GetRight style", HTTP Range) | yes |
| 20 | Peer ID Conventions | yes |
| 21 | Extension for Partial Seeds (`upload_only`, `event=paused`) | yes |
| 23 | Compact Peer Lists for Trackers | yes |
| 24 | Tracker Returns External IP (`ip`) | yes |
| 27 | Private Torrents (`private: 1`, no DHT/PEX/LSD) | yes |
| 38 | Finding Local Data Via Torrent File Hints (`similar`/`collections`) | yes |
| 40 | Canonical Peer Priority (tie-break) | yes |
| 47 | Padding Files (`.pad/`) | yes |
| 54 | The `lt_donthave` extension (a peer no longer has a piece) | partial (reception) |

> **BEP 38 (`similar`/`collections`).** The hints are read from the info
> dictionary **and** from the top level (merged, deduplicated), re-emitted in
> the `.torrent` export and shown together with the local torrents that share a
> hint. The **reuse of the shared data** — the point of the BEP — is
> **Swarm Merging**, which is *not* driven by the hints but by the structure
> (an identical piece layout, or an equal v2 per-file Merkle root): matching on
> the identity of the bytes is what makes the reuse safe. Phases 1–3 are done
> (`evoluzione.md` §6 and `measurements.md` §4.8), including block-granularity reuse, the
> resume path and the re-trigger when a sibling appears after the allocation.
> The daemon's BEP 38 view is `GET /api/v1/torrents/{hash}/collections`.
>
> **BEP 54 (`lt_donthave`).** Implemented on the **reception** side and
> advertised in the BEP 10 handshake: a peer that drops a piece it had
> announced has its availability cleared and the in-flight fetch cancelled
> (without the fast extension; with it, the peer rejects the request
> individually). Sending is **intentionally not implemented**: gx-core never
> revokes a verified piece — there is no piece eviction or discard path — so
> the only honest trigger for a `donthave` (dropping data it told a peer it
> had) never occurs. If such a path is ever added, the extension is already
> negotiated and the message type exists (`peerprotocol.DontHaveMessage`);
> hence **partial**, not `no`.

## Transport, encryption and NAT

| BEP | Specification | Status |
|---|---|---|
| 29 | uTP (uTorrent Transport Protocol) | yes |
| 55 | Holepunch Extension (`ut_holepunch` over uTP) | yes |
| — | MSE/PE (Protocol Encryption, de-facto) | yes |

## Trackers

| BEP | Specification | Status |
|---|---|---|
| 7 | IPv6 Tracker Extension (`peers6`) | yes |
| 15 | UDP Tracker Protocol (connect/announce/scrape) | yes |
| 31 | Tracker Failure Retry Extension | yes |
| 41 | UDP Tracker Protocol Extensions (`URLData`, option `0x2`) | yes |
| 48 | Tracker Scrape (HTTP `/scrape` and UDP) | yes |
| 34 | DNS Tracker Preferences (SRV records) | no |

> **BEP 31** honours `retry in` (in minutes) and `never`; `never` drops the
> tracker from the tier.
>
> **BEP 34 (DNS Tracker Preferences)** is not implemented: it lets a tracker
> host advertise its endpoints as SRV records (`_udp.…`, `_http._tcp.…`) so a
> client can find a tracker without a literal `host:port`. Almost no public
> tracker publishes SRV records, and the ones that do are also reachable
> through their normal announce URL, so the feature would add a DNS dependency
> and a second discovery path for a case that does not occur in practice.

## DHT (Mainline / BEP 5)

| BEP | Specification | Status |
|---|---|---|
| 5 | Mainline DHT Protocol (Kademlia IPv4) | yes |
| 32 | IPv6 Extension for DHT (`nodes6`, `want`) | yes |
| 33 | DHT Scrape (Bloom filters `BFpe`/`BFsd`) | yes |
| 42 | DHT Security Extension (Secure Node ID, CRC32C) | yes |
| 43 | Read-only DHT Nodes (`ro: 1`) | yes |
| 44 | Storing arbitrary data in the DHT (K/V Ed25519) | yes |
| 45 | Multiple-address Operation for DHT (multi-homed) | no |
| 46 | Updating Torrents via DHT Mutable Items (`urn:btpk:`) | yes |
| 51 | DHT Infohash Indexing (`sample_infohashes`) | no |

> **BEP 42** is complete in **both directions**: the node ID is derived from the
> external IP and the `ip` field is read *and* sent in every response
> (`reflected_sent`); the `ip` field is at the **top level** of the message, as
> per the specification and as libtorrent does. BEP 5 honours `implied_port` in
> `announce_peer`.
>
> **BEP 44** offers a local store, a `get`/`put` responder and an **outgoing
> iterative client** (bounded) to read and publish items; the `put` goes to the
> **8 closest nodes** to the target among those that provided a token.
>
> **BEP 46** is reached through the magnet parameter `xs=urn:btpk:`, resolved by
> the BEP 44 client on add.
>
> **BEP 45** and **BEP 51** are deliberately not implemented. BEP 45
> (multi-homed DHT) lets a node reachable on several interfaces announce
> several addresses per family; a home/NAS client with one public address gains
> nothing from it, and the dual-stack case is already covered by BEP 32
> (`nodes6`/`want`). BEP 51 (`sample_infohashes`) exposes a node's whole
> infohash index so that crawlers can enumerate the swarm: it is a service for
> indexers, and it adds a privacy and load surface (the node publishes **what it
> is storing**) that a home client neither needs nor wants. Both are marginal
> for this client, cost > benefit, and stay in
> [Remaining items](#remaining-items-and-roadmap).

## BitTorrent v2 and magnet

| BEP | Specification | Status |
|---|---|---|
| 52 | BitTorrent v2 (SHA-256 Merkle trees, `piece layers`) | yes |
| 53 | Magnet URI extension: `so=` (Select Only) | yes |

> **BEP 52** covers v1, hybrids and v2-only, both from `.torrent` and from
> magnet. As a seed it answers `hash request` for the piece layer from the
> attached `piece layers` and, for the block layer (`base = 0`), both for a file
> present in full and, for the parts already verified, for a partially
> downloaded file (the requested 16 KiB leaf hashes are computed from the
> verified pieces and anchored with the piece layer's uncles; a request that
> touches a missing piece or asks for more than 512 hashes is refused).
>
> The magnet parameter `so=` is **BEP 53**. `ws=` (web seed), `xs=` (exact
> source), `as=` (acceptable source) and `dht=` (DHT bootstrap node) come
> from the **Magnet URI convention** (not from a BEP): gx-core handles `ws=`,
> `dht=`, `xs=` and `as=` (with `xs=`/`as=` it downloads the `.torrent` from the
> source, verifying that the info hash matches, so metadata does not depend on the
> swarm).

### How v2 differs from v1 (the rules that matter)

- **Info-hash**: SHA-256 of the info dict; trackers and handshake use the
  **20-byte truncated** form (wire identical to v1); magnets use `urn:btmh:`.
- **`pieces root`**: root of a branch-2 Merkle tree over a file's **16 KiB
  blocks**; the last block may be shorter (hash of the actual bytes) and the
  leaves past the end of the file are **32-byte zero hashes**.
- **`piece layers`** (top level, outside `info`): for each `pieces root`, the
  hash of a layer chosen so that one hash covers `piece length` bytes; hashes
  past the end of the file are omitted.
- **Alignment**: files are mapped onto piece boundaries and pieces **do not**
  span two files; the **last piece of a file may be shorter** than `piece
  length` and carries no padding. gx-core therefore uses a **per-file piece
  model** (not concatenated with synthetic padding).
- **Peer extensions**: `hash request` (21), `hashes` (22), `hash reject` (23),
  used to fetch a peer's `piece layers` (BEP 9 carries the info dict).
- **Hybrids**: an info dict with both fields; v1 = SHA-1 and v2 = SHA-256 over
  the complete dict.

## Out of scope / obsolete

| BEP / technology | Reason |
|---|---|
| 8 (Tracker Peer Obfuscation) | deferred by the BEP process |
| 17 (HTTP Seeding "Hoffman") | superseded by BEP 19 |
| 18 (Search Engine) / 22 (Local Tracker Discovery) / 26 (Zeroconf) / 28 (Tracker exchange) | deferred |
| 30 (Merkle tree torrent extension, v1) | superseded by BEP 52 (v2) |
| 35 (Torrent signing) | standard that never took off |
| 36 / 39 / 49 (RSS feeds, updating via feed, distributed feeds) | application level: Gextto already has RSS/Torznab |
| 50 (Publish/Subscribe Protocol) | niche (pub/sub over DHT); cost > benefit |
| WebTorrent (WebRTC) | requires the WebRTC stack; external component, not in the core |
| Native I2P SAM | SOCKS5/HTTP proxies can be used; native SAM out of scope |

## Remaining items and roadmap

1. **BEP 34 (DNS Tracker Preferences)**, **BEP 45 (DHT multi-address)** and
   **BEP 51 (`sample_infohashes`)**: not implemented by choice — SRV trackers are
   practically unused, multi-homed DHT is marginal for a single-address
   home/NAS client (dual-stack is already BEP 32), and infohash indexing is a
   crawler service with a privacy/load cost. Reasons in the notes above.
2. **10 Gbps throughput (pipelining)**: the adaptive request queue already
   exists; every change must be **measured before and after** (CPU, RAM,
   throughput), as per the performance rule in `AGENTS.md`.

**BEP 38 is complete** (hints, block reuse at allocation/resume/later trigger):
the detail is in the note above, in [`gx-torrent.md`](gx-torrent.md)
(*Local data reuse*) and in [`evoluzione.md`](evoluzione.md) §6 and [`measurements.md`](measurements.md) §4.8.

**Beyond the BEPs.** Extensions that are *not* part of the BEP catalogue — the
WebSocket tracker (`ws://`/`wss://`), WebTorrent/WebRTC peers, the v1
cross-layout swarm merge and decentralized feeds on BEP 46 — are outside these
tables: `bep.md` stays the BEP-only source of truth.

## Maintenance

Update this table on every new BEP implemented or excluded, together with the
linked documents ([`internal/gxcore/GEXTTO.md`](../internal/gxcore/GEXTTO.md),
[`gx-torrent.md`](gx-torrent.md), [`evoluzione.md`](evoluzione.md)); an entry
given as absent must be closed or reclassified when the code implements it.
