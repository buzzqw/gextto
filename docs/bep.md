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
| 38 | Finding Local Data Via Torrent File Hints (`similar`/`collections`) | **partial**: hints read (info + top-level, merged), exposed together with the candidate local torrents and rewritten in the export; block-level reuse is Swarm Merging |
| 40 | Canonical Peer Priority (tie-break) | yes |
| 47 | Padding Files (`.pad/`) | yes |
| 54 | The `lt_donthave` extension (a peer no longer has a piece) | yes (reception) |

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
| 31 | Tracker Failure Retry Extension | yes (`retry in` in minutes and `never`, which drops the tracker from the tier) |
| 41 | UDP Tracker Protocol Extensions (`URLData`, option `0x2`) | yes |
| 48 | Tracker Scrape (HTTP `/scrape` and UDP) | yes |
| 34 | DNS Tracker Preferences (SRV records) | no (used by very few trackers) |

## DHT (Mainline / BEP 5)

| BEP | Specification | Status |
|---|---|---|
| 5 | Mainline DHT Protocol (Kademlia IPv4) | yes |
| 32 | IPv6 Extension for DHT (`nodes6`, `want`) | yes |
| 33 | DHT Scrape (Bloom filters `BFpe`/`BFsd`) | yes |
| 42 | DHT Security Extension (Secure Node ID, CRC32C) | yes; the `ip` field of the responses is at the **top level** of the message, as per the specification and as libtorrent does (fixed on 2026-10-12: previously it sat inside `r` and no client read it or sent it in the right place) |
| 43 | Read-only DHT Nodes (`ro: 1`) | yes |
| 44 | Storing arbitrary data in the DHT (K/V Ed25519) | yes: local store, `get`/`put` responder and an **outgoing iterative client** (bounded) to read and publish items; the `put` goes to the **8 closest nodes** to the target among those that provided a token |
| 45 | Multiple-address Operation for DHT (multi-homed) | no |
| 46 | Updating Torrents via DHT Mutable Items (`urn:btpk:`) | yes (magnet `xs=urn:btpk:`, resolved by the BEP 44 client on add) |
| 51 | DHT Infohash Indexing (`sample_infohashes`) | no |

> BEP 42 is complete in **both directions**: the node ID is derived from the
> external IP and the `ip` field is read *and* sent in every response
> (`reflected_sent`). BEP 5 honours `implied_port` in `announce_peer`.

## BitTorrent v2 and magnet

| BEP | Specification | Status |
|---|---|---|
| 52 | BitTorrent v2 (SHA-256 Merkle trees, `piece layers`) | yes (v1, hybrids, v2-only from `.torrent` and from magnet) |
| 53 | Magnet URI extension: `so=` (Select Only) | yes |

> The magnet parameter `so=` is **BEP 53**. `ws=` (web seed), `xs=` (exact
> source), `as=` (acceptable source) and `dht=` (DHT bootstrap node) come
> from the **Magnet URI convention** (not from a BEP): gx-core handles `ws=`,
> `dht=`, `xs=` and `as=` (with `xs=`/`as=` it downloads the `.torrent` from the
> source, verifying that the info hash matches, so metadata does not depend on the
> swarm).

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

1. **BEP 45 (DHT multi-address)** and **BEP 51 (`sample_infohashes`)**: marginal
   for a home/NAS client (multi-interface; indexing for crawlers).
2. **10 Gbps throughput (pipelining)**: the adaptive request queue already
   exists; every change must be **measured before and after** (CPU, RAM,
   throughput), as per the performance rule in `AGENTS.md`.
3. **BEP 38 — `similar`/`collections` — partial.** The hints are read from the
   info dictionary **and** from the top level (merged, deduplicated), re-emitted
   in the `.torrent` export and shown together with the local torrents that
   share a hint (API `GET /api/v1/torrents/{hash}/collections`, daemon
   page, detail in Gextto). The **actual block reuse** — the
   real gain of the BEP — is **Swarm Merging** (`evoluzione.md` §15.2): here
   there is only the discovery of candidates.

## Maintenance

Update this table on every new BEP implemented or excluded, together with the
linked documents (`GEXTTO.md`, `gx-torrent.md`, `evoluzione.md`); an entry
given as absent must be closed or reclassified when the code implements it.
