# rain — copia modificata per gextto

Questa cartella contiene una copia di
[`github.com/cenkalti/rain`](https://github.com/cenkalti/rain) v2.4.2 (licenza
MIT, vedi `LICENSE`), collegata al modulo principale con
`replace github.com/cenkalti/rain/v2 => ./third_party/rain` in `go.mod`. La usa
solo il demone `cmd/gx-torrent`.

Rimossi rispetto all'originale:

- la CLI (`main.go`, `internal/command`, `internal/console`);
- i test upstream e i loro dati (`torrent/testdata`);
- logo, screenshot e file di CI (`.github`, `.goreleaser.yml`, `.golangci.yml`).

Test del fork mantenuti (girano dentro questo modulo):

- `internal/blocklist/blocklist_test.go` — formati del filtro IP;
- `internal/peerconn/peerconn_test.go` — contatore dei byte di protocollo;
- `internal/piecepicker/sequential_skip_test.go` — i pezzi dei file esclusi non
  vengono mai scelti, nemmeno in modalità sequenziale o dal percorso
  "file edge".

Modifiche, tutte marcate nel codice con `gextto fork`:

| Area | File | Cosa |
|---|---|---|
| Porta unica | `torrent/session_listen.go`, `internal/btconn/accept.go`, `torrent/session.go`, `torrent/torrent_start.go`, `torrent/session_load.go`, `torrent/torrent.go`, `torrent/torrent_run.go` | `Config.ListenPort`: un solo listener di sessione. L'handshake BitTorrent/MSE sceglie il torrent dall'info hash (`AcceptRouted`) e consegna la connessione al torrent; tutti i torrent annunciano la stessa porta |
| Uscita e proxy | `internal/netx`, `internal/btconn/dial.go`, `internal/trackermanager`, `internal/tracker/udptracker/transport.go`, `torrent/session*.go` | `Config.OutgoingInterface` (killswitch VPN: senza indirizzo non esce nulla) e `Config.Proxy` (SOCKS5 o HTTP CONNECT per peer, tracker HTTP e web seed; tracker UDP rifiutati) |
| Filtro IP | `internal/blocklist/blocklist.go`, `torrent/session_blocklist.go` | Formati intervallo, P2P e eMule `.dat` oltre al CIDR; `Session.LoadBlocklist` da file locale |
| Selezione file | `torrent/torrent_selection.go`, `internal/allocator`, `internal/piece`, `internal/piecepicker`, `torrent/torrent_pieces.go`, `torrent/torrent_verification.go`, `torrent/torrent_allocation.go`, `torrent/torrent_stats.go` | `Config.FileSelection` e `Config.PartsDir`. I file esclusi stanno in `PartsDir/<id>`; il piece picker salta i pezzi non voluti; il completamento e `Stats.Bytes.Selected*` considerano solo i file scelti |
| uTP | `torrent/session.go`, `torrent/session_listen.go`, `internal/netx` | `Config.UTP`: un socket UDP sulla porta unica, condiviso con il DHT; in uscita uTP e TCP in parallelo; peer uTP segnalati in `Peer.UTP`; contatori in `SessionStats` |
| Preallocazione | `internal/storage/filestorage`, `torrent/session_storage.go`, `torrent/session_add.go`, `torrent/session_load.go` | `Config.Preallocate`: i file nuovi vengono riservati con `fallocate` invece di essere creati sparsi. Il flag viaggia nel provider di storage, così vale sia per l'aggiunta sia per il ricaricamento |
| Byte di protocollo | `internal/peerconn/peerconn.go`, `torrent/session_stats.go` | Contatore dei byte grezzi in lettura/scrittura per l'overhead di protocollo |
| Statistiche | `torrent/session_stats.go`, `torrent/torrent_stats.go` | Contatori uTP/TCP, nodi DHT, byte di protocollo, padding |

Anche `nictuku/dht` (licenza BSD) è incluso in `third_party/dht`. Modifiche:

- `Config.PacketConn`, per usare il socket uTP;
- `Stats()` con i nodi conosciuti e il traffico.

uTP usa `github.com/anacrolix/utp` (MPL-2.0) come dipendenza non modificata.

## Correzioni e integrazioni con upstream

Dalla v1.13.0 (base precedente) la v2.4.2 porta con sé mesi di fix upstream
(sicurezza, race, leak, protocollo). Le nostre modifiche sono state riapplicate
sopra la nuova base; i punti dove il nostro codice incontra quello nuovo sono:

- **Selezione file + nuovo picker.** Upstream ha aggiunto il download
  sequenziale e la priorità ai bordi dei file. Il nostro flag `Skip` è stato
  aggiunto in `myPiece.PickableBy` e in `pickSequential`, che altrimenti
  sceglierebbero pezzi di file esclusi. Guardato da
  `sequential_skip_test.go`.
- **Preallocazione + provider di storage.** Upstream ha spostato la creazione
  dello storage in `torrent/session_storage.go`; il flag `Preallocate` ora è un
  campo del provider e viene applicato in `GetStorage`.
- **Rimozione torrent.** `Session.RemoveTorrent` ha un nuovo parametro
  `keepData`; il demone passa `false` per continuare a rimuovere solo il
  symlink in `LinkDir/<id>`.

Le correzioni storiche del fork (IP degli handshake chiusi, race del gestore di
log, verbi di formato) sono ora in gran parte superate dai fix upstream.

Come aggiornare la base in futuro: vedi `docs/rain-allineamento.md`.
