# Conformità BitTorrent (BEP) di gx-core

Stato di supporto alle specifiche BitTorrent (BEP) del motore **gx-core**
(`internal/gxcore`) e del demone `gx-torrent` (`cmd/gx-torrent`).

Questo documento è la **fonte unica dello stato per BEP**: lo stato riflette il
codice, non confronti con altri client. Il dettaglio per file è in
[`internal/gxcore/GEXTTO.md`](../internal/gxcore/GEXTTO.md) (inventario del
motore) e in [`gx-torrent.md`](gx-torrent.md) (demone); motivazioni, misure e
backlog in [`evoluzione.md`](evoluzione.md).

Legenda stato: **sì** = implementato e testato; **parziale** = solo una parte;
**no** = assente; **escluso** = fuori scope per scelta.

## Peer wire e metadati

| BEP | Specifica | Stato |
|---|---|---|
| 3 | The BitTorrent Protocol Specification (core TCP/UDP wire) | sì |
| 4 | Reserved bits nell'handshake | sì |
| 6 | Fast Extension (`have all`/`have none`, `suggest`, `allowed fast`) | sì |
| 9 | `ut_metadata` (metadati da magnet) | sì |
| 10 | Extension Protocol (handshake `m`) | sì |
| 11 | Peer Exchange (`ut_pex`, IPv4 + `added6`/`dropped6`) | sì |
| 12 | Multitracker Metadata Extension (`announce-list` a livelli) | sì |
| 14 | Local Service Discovery (LSD, IPv4 e IPv6) | sì |
| 19 | WebSeed ("GetRight style", HTTP Range) | sì |
| 20 | Peer ID Conventions | sì |
| 21 | Extension for Partial Seeds (`upload_only`) | **parziale**: peer sì; tracker (`event=paused`) in corso |
| 23 | Compact Peer Lists for Trackers | sì |
| 24 | Tracker Returns External IP (`ip`) | sì |
| 27 | Private Torrents (`private: 1`, niente DHT/PEX/LSD) | sì |
| 40 | Canonical Peer Priority (tie-break) | sì |
| 47 | Padding Files (`.pad/`) | sì |

## Trasporto, cifratura e NAT

| BEP | Specifica | Stato |
|---|---|---|
| 29 | uTP (uTorrent Transport Protocol) | sì |
| 55 | Holepunch Extension (`ut_holepunch` su uTP) | sì |
| — | MSE/PE (Protocol Encryption, de-facto) | sì |

## Tracker

| BEP | Specifica | Stato |
|---|---|---|
| 7 | IPv6 Tracker Extension (`peers6`) | sì |
| 15 | UDP Tracker Protocol (connect/announce/scrape) | sì |
| 41 | UDP Tracker Protocol Extensions (`URLData`, opzione `0x2`) | sì |
| 48 | Tracker Scrape (HTTP `/scrape` e UDP) | sì |
| 34 | DNS Tracker Preferences (record SRV) | no (usata da pochissimi tracker) |

## DHT (Mainline / BEP 5)

| BEP | Specifica | Stato |
|---|---|---|
| 5 | Mainline DHT Protocol (Kademlia IPv4) | sì |
| 32 | IPv6 Extension for DHT (`nodes6`, `want`) | sì |
| 33 | DHT Scrape (filtri Bloom `BFpe`/`BFsd`) | sì |
| 42 | DHT Security Extension (Secure Node ID, CRC32C) | sì |
| 43 | Read-only DHT Nodes (`ro: 1`) | sì |
| 44 | Storing arbitrary data in the DHT (K/V Ed25519) | **parziale**: store locale + `get`/`put` come responder; **nessun client iterativo uscente** |
| 45 | Multiple-address Operation for DHT (multi-homed) | no |
| 46 | Updating Torrents via DHT Mutable Items (`urn:btpk:`) | no |
| 51 | DHT Infohash Indexing (`sample_infohashes`) | no |

## BitTorrent v2 e magnet

| BEP | Specifica | Stato |
|---|---|---|
| 52 | BitTorrent v2 (alberi Merkle SHA-256, `piece layers`) | sì (v1, ibridi, v2-only da `.torrent` e da magnet) |
| 53 | Magnet URI Extension (`so=`, `xs=`, `ws=`, `as=`) | **parziale**: `so=` (Select Only) sì; `xs=`/`ws=`/`as=` no |

## Fuori scope / obsolete

| BEP / tecnologia | Motivo |
|---|---|
| 17 (HTTP Seeding "Hoffman") | superata da BEP 19 |
| 35 (Torrent signing) | standard mai affermatosi |
| 54 (STUN NAT Traversal) | costo/complessità oltre il beneficio |
| WebTorrent (WebRTC) | richiede lo stack WebRTC; componente esterno, non nel core |
| I2P SAM nativo | usabili proxy SOCKS5/HTTP; SAM nativo fuori scope |

## Residui e roadmap

1. **BEP 21 — parte tracker**: inviare `event=paused` a ogni announce mentre si è
   *seed parziali* (tutti i pezzi *voluti* presenti, torrent non intero). Il lato
   peer (`upload_only` nell'extension handshake) è già fatto. Il campo
   `downloaders` dello scrape resta opzionale (dipende dal tracker).
2. **BEP 44 — client iterativo uscente + BEP 46**: pubblicare e reperire item
   sui nodi più vicini tramite `get`/`put` uscenti e traversal Kademlia. Il DHT
   oggi ha già un client verso i nodi più vicini per lo scrape
   (`scrape_client.go`) e il loop `get_peers` è iterativo; manca il percorso
   uscente. Da aprire **solo con un consumatore reale** (nessun codice
   speculativo).
3. **BEP 53 — `xs=`/`ws=`/`as=`**: la sola parte oggi gestita è `so=`. `ws=`
   (web seed da magnet) è la più utile e la più semplice da aggiungere; `xs=`/
   `as=` sono raramente usati.
4. **Throughput 10 Gbps (pipelining)**: la coda richieste adattiva esiste già;
   ogni modifica va **misurata prima e dopo** (CPU, RAM, throughput), come da
   regola performance di `AGENTS.md`.

## Manutenzione

Aggiornare questa tabella a ogni nuova BEP implementata o esclusa, insieme ai
documenti collegati (`GEXTTO.md`, `gx-torrent.md`, `evoluzione.md`); una voce
data per assente va chiusa o riqualificata quando il codice la implementa.
