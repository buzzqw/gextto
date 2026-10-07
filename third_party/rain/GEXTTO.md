# rain — copia modificata per gextto

Questa cartella contiene una copia di
[`github.com/cenkalti/rain`](https://github.com/cenkalti/rain) v1.13.0 (licenza
MIT, vedi `LICENSE`), collegata al modulo principale con
`replace github.com/cenkalti/rain => ./third_party/rain` in `go.mod`. La usa
solo il demone `cmd/gx-torrent`.

Rimossi rispetto all'originale:

- la CLI (`main.go`, `internal/console`);
- i test upstream e i loro dati (`torrent/testdata`, 11 MB);
- il logo.

Modifiche, tutte marcate nel codice con `gextto fork`:

| Area | File | Cosa |
|---|---|---|
| Porta unica | `torrent/session_listen.go`, `internal/btconn/accept.go`, `torrent/session.go`, `torrent/torrent_start.go`, `torrent/session_load.go`, `torrent/torrent.go`, `torrent/torrent_run.go` | `Config.ListenPort`: un solo listener di sessione. L'handshake BitTorrent/MSE sceglie il torrent dall'info hash (`AcceptRouted`) e consegna la connessione al torrent; tutti i torrent annunciano la stessa porta |
| Uscita e proxy | `internal/netx`, `internal/btconn/dial.go`, `internal/trackermanager`, `internal/tracker/udptracker/transport.go`, `torrent/session*.go` | `Config.OutgoingInterface` (killswitch VPN: senza indirizzo non esce nulla) e `Config.Proxy` (SOCKS5 o HTTP CONNECT per peer, tracker HTTP e web seed; tracker UDP rifiutati) |
| Filtro IP | `internal/blocklist/blocklist.go`, `torrent/session_blocklist.go` | Formati intervallo, P2P e eMule `.dat` oltre al CIDR; `Session.LoadBlocklist` da file locale |
| Selezione file | `torrent/torrent_selection.go`, `internal/allocator`, `internal/piece`, `internal/piecepicker`, `torrent/torrent_pieces.go`, `torrent/torrent_verification.go`, `torrent/torrent_allocation.go`, `torrent/torrent_stats.go` | `Config.FileSelection` e `Config.PartsDir`. I file esclusi stanno in `PartsDir/<id>`; il piece picker salta i pezzi non voluti; il completamento e `Stats.Bytes.Selected*` considerano solo i file scelti |
| Correzioni | `torrent/torrent_stop.go`, `torrent/torrent_pieces.go`, `internal/logger/logger.go`, `internal/infodownloader/infodownloader.go` | Vedi elenco sotto |

Correzioni:

- gli IP degli handshake chiusi restavano segnati come "connessi" e non
  venivano più contattati dopo un completamento o uno stop;
- il gestore di log globale veniva riscritto senza lock (race tra sessioni);
- verbi di formato errati.
