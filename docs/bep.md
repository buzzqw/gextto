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
| 16 | Super-seeding (initial seeding, una parte alla volta) | sì |
| 19 | WebSeed ("GetRight style", HTTP Range) | sì |
| 20 | Peer ID Conventions | sì |
| 21 | Extension for Partial Seeds (`upload_only`, `event=paused`) | sì |
| 23 | Compact Peer Lists for Trackers | sì |
| 24 | Tracker Returns External IP (`ip`) | sì |
| 27 | Private Torrents (`private: 1`, niente DHT/PEX/LSD) | sì |
| 38 | Finding Local Data Via Torrent File Hints (`similar`/`collections`) | **parziale**: hint letti (info + top-level, uniti), esposti con i torrent locali candidati e riscritti nell'export; il riuso a livello di blocco è Swarm Merging |
| 40 | Canonical Peer Priority (tie-break) | sì |
| 47 | Padding Files (`.pad/`) | sì |
| 54 | The `lt_donthave` extension (un peer non ha più un pezzo) | sì (ricezione) |

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
| 31 | Tracker Failure Retry Extension | sì (`retry in` in minuti e `never`, che scarta il tracker dal tier) |
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
| 44 | Storing arbitrary data in the DHT (K/V Ed25519) | sì: store locale, responder `get`/`put` e **client uscente iterativo** (bounded) per leggere e pubblicare item |
| 45 | Multiple-address Operation for DHT (multi-homed) | no |
| 46 | Updating Torrents via DHT Mutable Items (`urn:btpk:`) | sì (magnet `xs=urn:btpk:`, risolto dal client BEP 44 all'aggiunta) |
| 51 | DHT Infohash Indexing (`sample_infohashes`) | no |

> BEP 42 è completo in **entrambe le direzioni**: l'ID nodo deriva dall'IP esterno
> e il campo `ip` è letto *e* inviato in ogni risposta (`reflected_sent`). BEP 5
> rispetta `implied_port` in `announce_peer`.

## BitTorrent v2 e magnet

| BEP | Specifica | Stato |
|---|---|---|
| 52 | BitTorrent v2 (alberi Merkle SHA-256, `piece layers`) | sì (v1, ibridi, v2-only da `.torrent` e da magnet) |
| 53 | Magnet URI extension: `so=` (Select Only) | sì |

> Il parametro magnet `so=` è **BEP 53**. `ws=` (web seed), `xs=` (sorgente
> esatta), `as=` (sorgente accettabile) e `dht=` (nodo DHT di bootstrap) vengono
> dalla **convenzione Magnet URI** (non da una BEP): gx-core gestisce `ws=`,
> `dht=`, `xs=` e `as=` (con `xs=`/`as=` scarica il `.torrent` dalla sorgente,
> verificando che l'info hash corrisponda, così i metadati non dipendono dallo
> sciame).

## Fuori scope / obsolete

| BEP / tecnologia | Motivo |
|---|---|
| 8 (Tracker Peer Obfuscation) | deferred dal processo BEP |
| 17 (HTTP Seeding "Hoffman") | superata da BEP 19 |
| 18 (Search Engine) / 22 (Local Tracker Discovery) / 26 (Zeroconf) / 28 (Tracker exchange) | deferred |
| 30 (Merkle tree torrent extension, v1) | superata da BEP 52 (v2) |
| 35 (Torrent signing) | standard mai affermatosi |
| 36 / 39 / 49 (Feed RSS, aggiornamento via feed, feed distribuiti) | livello applicativo: Gextto ha già RSS/Torznab |
| 50 (Publish/Subscribe Protocol) | nicchia (pub/sub su DHT); costo > beneficio |
| WebTorrent (WebRTC) | richiede lo stack WebRTC; componente esterno, non nel core |
| I2P SAM nativo | usabili proxy SOCKS5/HTTP; SAM nativo fuori scope |

> Nota: BEP 54 è **`lt_donthave`**, non STUN (errore di una versione precedente
> di questo documento e dell'analisi esterna). Non esiste una BEP per il NAT
> traversal via STUN.

## Residui e roadmap

1. **BEP 21 — fatta.** Lato peer: `upload_only` nell'extension handshake, inviato
   alla connessione e rinviato quando si diventa seed (pieno o parziale). Lato
   tracker: ogni announce di un **seed parziale** (tutti i pezzi *voluti*
   presenti, torrent non intero) porta `event=paused` (`announcer` +
   `tracker.EventPaused`). Il campo `downloaders` dello scrape resta opzionale
   (dipende dal tracker, che deve esporlo).
2. **BEP 54 — `lt_donthave` — fatta (ricezione).** `lt_donthave` è nel dizionario
   `m` dell'extension handshake; un messaggio `DontHave` in arrivo (`op 20`,
   subop, indice a 4 byte) azzera il bit del pezzo nel bitfield del peer e la sua
   disponibilità nel picker; senza Fast Extension viene cancellato anche il
   download attivo di quel pezzo da quel peer. Il lato **invio** non serve
   (gx-core non scarta pezzi di torrent attivi).
3. **Magnet `xs=`/`as=`** (convenzione Magnet URI, **non** BEP 53): **fatti.**
   `xs=` (sorgente esatta) e `as=` (sorgente accettabile) sono letti in
   `magnet.ExactSources`/`AcceptableSources`; `Session.addMagnet` prova a
   scaricare il `.torrent` da quegli URL (prima gli esatti, poi gli accettabili)
   e lo usa **solo se l'info hash corrisponde**, altrimenti ricade nel download
   dei metadati via sciame/DHT. `so=` (BEP 53) e `ws=` (web seed, con
   persistenza nel resume) erano già fatti.
4. **BEP 44 — client iterativo uscente + BEP 46 — fatti.** Il DHT ha un client
   uscente (`bep44_client.go`): `GetImmutable`/`GetMutable` fanno una traversal
   bounded sui nodi più vicini, `PublishImmutable`/`PublishMutable` raccolgono i
   token con una `get` e poi pubblicano con `put`. La **BEP 46** è cablata: un
   magnet `xs=urn:btpk:<pubkey>` (con eventuale `s=<salt>`) risolve l'info hash
   dall'item mutabile all'aggiunta (`Session.addMagnet`).
5. **BEP 45 (DHT multi-address)** e **BEP 51 (`sample_infohashes`)**: marginali
   per un client domestico/NAS (multi-interfaccia; indicizzazione per crawler).
6. **Throughput 10 Gbps (pipelining)**: la coda richieste adattiva esiste già;
   ogni modifica va **misurata prima e dopo** (CPU, RAM, throughput), come da
   regola performance di `AGENTS.md`.
7. **BEP 38 — `similar`/`collections` — parziale.** Gli hint sono letti dal
   dizionario info **e** dal livello superiore (uniti, deduplicati), riemessi
   nell'export del `.torrent` e mostrati insieme ai torrent locali che
   condividono un hint (API `GET /api/v1/torrents/{hash}/collections`, pagina
   del demone, dettaglio in Gextto). Il **riuso effettivo dei blocchi** — il
   vero guadagno del BEP — è lo **Swarm Merging** (`evoluzione.md` §15.2): qui
   c'è solo la scoperta dei candidati.
8. **BEP 31 — `retry in` — fatta.** Il valore in minuti (HTTP e UDP, announce e
   scrape) rinvia il prossimo annuncio; `"never"` scarta il tracker dal tier per
   sempre (`tracker.Tier`), senza fermare gli altri tracker dello stesso tier.

## Manutenzione

Aggiornare questa tabella a ogni nuova BEP implementata o esclusa, insieme ai
documenti collegati (`GEXTTO.md`, `gx-torrent.md`, `evoluzione.md`); una voce
data per assente va chiusa o riqualificata quando il codice la implementa.
