# gx-torrent — analisi delle lacune e piano di miglioramento

Documento di lavoro: confronta `gx-torrent` (rain + demone) con libtorrent,
qBittorrent e anacrolix/torrent, elenca le lacune reali e registra le scelte
fatte. Aggiornato: 2026-10-08.

## Contesto

`gx-torrent` è nato per avere un motore BitTorrent **in puro Go**, alternativo a
libtorrent. È composto da due strati:

- **rain** (`third_party/rain`, fork v2.4.2): il motore di trasferimento;
- **demone + adapter** (`cmd/gx-torrent`, `gxtorrent_engine.go`): API REST, pagina
  web, coda autogestita, ed il ponte con Gextto (`TorrentEngine`).

Gextto fornisce già, per tutti i motori: queue e slot, stalled/retry, seed policy
(ratio/giorni), RSS, ricerca, categorie/tag, scheduler di banda, post-processing,
spostamenti e RAM disk, filtro IP multi-formato, proxy. Queste **non sono lacune
del motore** e non vanno inseguite.

Lo stato per capacità è in `capabilityLevels` (`torrent_engine.go`), unica fonte
di verità usata dall'UI.

## Lacune reali

### vs libtorrent / qBittorrent

| Area | Cosa manca | Note |
|---|---|---|
| Limiti per-torrent | velocità download/upload | `SetLimits` rifiuta un rate > 0 (`ErrCapabilityUnavailable`) |
| Limiti per-torrent | connessioni e upload slot | `SetMaxConnections`/`SetMaxUploads` rifiutano |
| Modalità | upload/share mode, super-seeding | rain non li ha |
| Priorità | per-pezzo e diagnostica pezzi | solo incluso/escluso per file |
| Tracker | rimozione | ~~rain aggiunge ma non rimuove~~ (fatto: `set-trackers`) |
| Web seed | add/remove a caldo | rain legge solo il `url-list` del `.torrent` |
| Sequenziale | toggle a runtime | rain fissa l'ordine all'aggiunta |
| Protocollo | IPv6 | listener/DHT/uTP solo IPv4 |
| Protocollo | BitTorrent v2-only | rain gestisce v1 e ibridi |
| Qualità | algoritmi di seeding/choking | rain usa slot/unchoker di default |

### vs anacrolix/torrent

anacrolix espone, per sua natura di libreria: encryption, DHT, PEX, uTP,
WebTorrent, WebSeeds, BitTorrent v2, holepunching, **Reader con seek/readahead**
(streaming), storage backend alternativi (blob/file/bolt/mmap/sqlite/FUSE).
Quindi, oltre alle voci sopra, mancano a gx-torrent: **streaming con
readahead**, **WebTorrent/WebRTC**, **holepunching (BEP 55)**, **storage
alternativi**.

`gx-torrent` ha però cose che le librerie non offrono: daemon sorvegliato, UI
web, REST, UPnP/NAT-PMP, porta unica, proxy SOCKS/HTTP, filtro IP in più formati,
killswitch VPN, preallocazione, cache adattiva.

## Cosa NON è una lacuna

RSS, search plugin, categorie/tag, scheduler, ratio, post-processing, download
sequenziale come *modalità* (c'è, manca solo il toggle a caldo), cifratura, IP
filter, proxy, DHT/PEX/LSD, UPnP/NAT-PMP, fast resume, selezione file: già
presenti o già gestiti da Gextto.

## Decisioni

| # | Miglioria | Decisione | Ordine |
|---|---|---|---|
| 1 | Rimozione/sostituzione tracker | **Fatto** — `Torrent.SetTrackers` nel fork + azione `set-trackers` | 1 |
| 2 | Upload/share mode | **Saltato** — si usa il seed infinito (ratio 0) | — |
| 3 | Toggle sequential/first-last a caldo | **Sì** | 3 |
| 4 | Web seed add/remove via API | **Sì** | 2 |
| 5 | Diagnostica pezzi | **Sì** — API demone **e** UI Gextto | 4 |
| 6 | Limiti velocità per-torrent | **Sì** | 5 |
| 7 | Streaming HTTP Range + priorità pezzi | **Sì** — endpoint sul demone | 7 |
| 8 | Limiti connessioni/upload per-torrent | **Sì** — utile con coda/cache automatiche | 6 |
| 9 | IPv6 | **No** | — |
| 10 | BitTorrent v2-only | **Wishlist** | — |
| 11 | Super-seeding, holepunching, WebTorrent | **Wishlist** | — |
| 12 | Qualità seeding/choking | **Wishlist** — da misurare prima | — |

Convenzione semantica scelta per i limiti: **-1 = eredita il globale, 0 =
illimitato** (come libtorrent e come i campi già presenti nell'UI).

## Piano di lavoro

Un commit per punto, con test e documentazione. Ordine: **1 → 4 → 3 → 5 → 6 → 8 → 7**.

1. **Tracker (fatto).** `Torrent.SetTrackers([]string)` sostituisce la lista a
   caldo; lista vuota rimuove tutto; i tracker rimossi ricevono un announce
   `stopped` best-effort; la lista è persistita nel resume. Il demone espone
   `set-trackers`, l'adapter lo usa; capacità `trackers` = `full`.
2. **Web seed.** rain ha già `webseedsource` e lo scaricatore: esporre
   add/remove dal demone (`POST /api/v1/torrents/{hash}/webseeds`) e farli
   gestire all'adapter. Capacità `web_seeds` = `full`.
3. **Sequenziale a caldo.** rain fissa l'ordine all'aggiunta: ricalcolare la
   priorità dei pezzi nel picker quando `sequential`/`first_last` cambiano, senza
   riaprire il torrent. Capacità `sequential`/`first_last` = `full`.
4. **Diagnostica pezzi.** Esporre lo stato per pezzo dal demone
   (`GET /api/v1/torrents/{hash}/pieces`) e aggiungere la vista nella UI di
   Gextto (`PieceRuns`). gx-torrent diventa il primo motore con `piece_diagnostics`.
5. **Limiti velocità per-torrent.** Un `bandwidth.Limiter` per torrent (il fork
   ha già il limiter regolabile a caldo) usato dai peer di quel torrent;
   `SetLimits` accetta rate > 0. Capacità `limits` = `full`.
6. **Limiti connessioni/upload per-torrent.** Cap per torrent su dial/accept e
   slot di upload, oltre ai budget globali; `SetMaxConnections`/`SetMaxUploads`.
7. **Streaming.** Endpoint HTTP con Range sul demone e priorità/readahead ai
   pezzi della finestra richiesta; il picker privilegia il range, così un player
   (o Jellyfin) può leggere mentre il download prosegue.

### Wishlist

- **BitTorrent v2-only**: richiede il supporto v2 in rain (grande).
- **Super-seeding, holepunching, WebTorrent**: funzioni di nicchia, molto lavoro.
- **Qualità seeding/choking**: prima misurare gx-torrent vs libtorrent/qBittorrent
  sullo stesso sciame (rapporto, throughput in upload, tempo a 1:1), poi
  valutare un investimento sul core di rain.

## Criterio di verifica

- Per ogni punto: test mirati (`go test ./cmd/gx-torrent/` e
  `go test -run GxEngine .`) e test del fork dove tocca rain (`make test-rain`).
- Per i punti con effetto visibile: aggiornare `docs/gx-torrent.md` (API e
  limiti), `docs/MANUAL.it.md`/`docs/MANUAL.en.md` e `README`.
- Confronti con libtorrent/qBittorrent solo su misura, non a impressione.
