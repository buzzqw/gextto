# Motore gx-core (ex rain) — inventario

Questa cartella contiene il **motore** di gx-core (il binario `gx-torrent`):
nasce dalla copia di
[`github.com/cenkalti/rain`](https://github.com/cenkalti/rain) v2.4.2 (licenza
MIT, vedi `LICENSE`) ed è ora **codice nostro**, parte del modulo principale
come `github.com/buzzqw/gextto/internal/gxcore`. Lo usa solo il demone
`cmd/gx-torrent`.

**Obiettivo:** non dipendere più da rain. Il distacco è avvenuto (cartella,
modulo, identità di rete e **marcatori nel codice** rimossi); questa tabella è
il **changelog delle modifiche** rispetto alla base upstream. L'attribuzione MIT
originale resta in `LICENSE`.

Rimossi rispetto all'originale:

- la CLI (`main.go`, `internal/command`, `internal/console`);
- i test upstream e i loro dati (`torrent/testdata`);
- logo, screenshot e file di CI (`.github`, `.goreleaser.yml`, `.golangci.yml`).

Test del fork mantenuti (girano dentro questo modulo):

- `internal/blocklist/blocklist_test.go` — formati del filtro IP;
- `internal/mse/mse_test.go` — scrittura MSE in place: round-trip di cifratura e zero allocazioni per blocco;
- `internal/peerconn/peerconn_test.go` — contatore dei byte di protocollo;
- `internal/peerconn/peerwriter/peerwriter_test.go` — la finestra di dedup delle richieste servite resta limitata e sfratta la più vecchia;
- `internal/piececache/cache_test.go` — scadenza TTL lazy e sweeper, ricarica dopo scadenza, eviction per dimensione e uso concorrente;
- `internal/bitfield/bitfield_test.go` — `Count` con popcount hardware uguale al conteggio bit per bit, anche a cavallo delle parole;
- `internal/piecepicker/sequential_skip_test.go` — i pezzi dei file esclusi non
  vengono mai scelti, nemmeno in modalità sequenziale o dal percorso
  "file edge";
- `internal/bandwidth/limiter_test.go` — il limite cambia a caldo;
- `internal/storage/filestorage/preallocate_linux_test.go` — niente
  preallocazione su tmpfs;
- `internal/unchoker/sim_test.go` — harness deterministico del choking: proprietà
  dell'algoritmo (downloader/uploader più veloci, optimistic, budget di slot,
  fairness) e benchmark del tick, per confrontare varianti e misurarne il costo;
- `torrent/torrent_superseed_test.go` — rotazione dei pezzi offerti e selezione
  dei pezzi del super-seeding (mai un pezzo già posseduto o già offerto);
- `internal/peerprotocol/holepunch_test.go` — codec BEP 55 `ut_holepunch`
  (round-trip v4/v6, errori, framing nell'estensione, fuzz);
- `torrent/torrent_holepunch_test.go` — decisione del relè (connect a entrambi,
  errori `NotConnected`/`NoSupport`/`NoSelf`/`NoSuchPeer`).

Modifiche rispetto alla base upstream:

| Area | File | Cosa |
|---|---|---|
| Porta unica | `torrent/session_listen.go`, `internal/btconn/accept.go`, `torrent/session.go`, `torrent/torrent_start.go`, `torrent/session_load.go`, `torrent/torrent.go`, `torrent/torrent_run.go` | `Config.ListenPort`: un solo listener di sessione. L'handshake BitTorrent/MSE sceglie il torrent dall'info hash (`AcceptRouted`) e consegna la connessione al torrent; tutti i torrent annunciano la stessa porta |
| Uscita e proxy | `internal/netx`, `internal/btconn/dial.go`, `internal/trackermanager`, `internal/tracker/udptracker/transport.go`, `torrent/session*.go` | `Config.OutgoingInterface` (killswitch VPN: senza indirizzo non esce nulla) e `Config.Proxy` (SOCKS5 o HTTP CONNECT per peer, tracker HTTP e web seed; tracker UDP rifiutati) |
| Filtro IP | `internal/blocklist/blocklist.go`, `torrent/session_blocklist.go` | Formati intervallo, P2P e eMule `.dat` oltre al CIDR; `Session.LoadBlocklist` da file locale |
| Selezione file | `torrent/torrent_selection.go`, `internal/allocator`, `internal/piece`, `internal/piecepicker`, `torrent/torrent_pieces.go`, `torrent/torrent_verification.go`, `torrent/torrent_allocation.go`, `torrent/torrent_stats.go` | `Config.FileSelection` e `Config.PartsDir`. I file esclusi stanno in `PartsDir/<id>`; il piece picker salta i pezzi non voluti; il completamento e `Stats.Bytes.Selected*` considerano solo i file scelti |
| uTP | `torrent/session.go`, `torrent/session_listen.go`, `internal/netx` | `Config.UTP`: un socket UDP sulla porta unica, condiviso con il DHT; in uscita uTP e TCP in parallelo; peer uTP segnalati in `Peer.UTP`; contatori in `SessionStats` |
| Preallocazione | `internal/storage/filestorage`, `torrent/session_storage.go`, `torrent/session_add.go`, `torrent/session_load.go` | `Config.Preallocate`: i file nuovi vengono riservati con `fallocate` invece di essere creati sparsi. Il flag viaggia nel provider di storage, così vale sia per l'aggiunta sia per il ricaricamento |
| Byte di protocollo | `internal/peerconn/peerconn.go`, `torrent/session_stats.go` | Contatore dei byte grezzi in lettura/scrittura per l'overhead di protocollo |
| MSE in place | `internal/mse/mse.go` | `inPlaceStreamWriter`: cifra RC4 nello stesso buffer del chiamante, eliminando l'allocazione per blocco di `cipher.StreamWriter` sul percorso di upload |
| Dedup richieste servite | `internal/peerconn/peerwriter/peerwriter.go` | `servedWindow`: finestra limitata (1024) al posto della mappa illimitata; evita la crescita senza limite e il rifiuto permanente delle ritrasmissioni |
| Popcount | `internal/bitfield/bitfield.go` | `Bitfield.Count` con `math/bits.OnesCount64` (istruzione hardware) invece della tabella di lookup da 256 byte |
| Statistiche | `internal/netx/netx.go`, `torrent/session_listen.go`, `torrent/session_stats.go`, `torrent/torrent_stats.go` | Contatori uTP/TCP e connessioni in entrata TCP/uTP, peer correnti per trasporto, nodi DHT, byte di protocollo, padding, totali di operazioni I/O (`ReadOpsTotal`/`WriteOpsTotal` dai meter di read/write cache) |
| Limiti a caldo | `internal/bandwidth`, `internal/peer`, `internal/peerconn/*`, `internal/urldownloader`, `torrent/session.go`, `torrent/session_limits.go` | `Session.SetSpeedLimits`: i peer usano un `bandwidth.Limiter` il cui ritmo cambia senza riaprire la sessione (prima `*ratelimit.Bucket` fisso) |
| Cache a caldo | `internal/piececache/cache.go`, `internal/resourcemanager`, `torrent/session_limits.go` | `Session.SetCacheSizes`: read cache (`SetMaxSize`, sfratta l'eccedenza) e buffer di scrittura (`SetLimit`) ridimensionati sulla sessione in corso. `Cache` usa anche una scadenza lazy con un solo sweeper periodico, al posto di un `time.Timer` per pezzo |
| Preallocazione su tmpfs | `internal/storage/filestorage/filestorage_linux.go` | Su tmpfs i file restano sparsi anche con `Preallocate` (test `preallocate_linux_test.go`) |
| Verifica dopo errore | `torrent/torrent_stop.go` | `handleStopped` riavvia per la verifica solo dopo uno stop pulito: se l'allocazione dei file fallisce (file sparito, cartella non scrivibile) il torrent resta fermo con l'errore, invece di ripartire in un ciclo stretto che ricrea file vuoti a ogni giro. Test `TestVerifyStopsOnceWhenFilesCannotBeAllocated` in `cmd/gx-torrent` |
| Tracker a caldo | `torrent/session_torrent.go`, `torrent/torrent_announce.go` | `Torrent.SetTrackers`: sostituisce la lista dei tracker a runtime (lista vuota = rimuovi tutto); i tracker rimossi ricevono un announce `stopped` best-effort; la lista è persistita nel resume |
| Web seed a caldo | `torrent/torrent_webseed.go`, `torrent/session_torrent.go`, `internal/piecepicker/piecepicker.go` | `Torrent.AddWebseeds`/`RemoveWebseeds`: aggiungono/rimuovono web seed a runtime (persistiti nel resume); il picker tiene aggiornata la lista delle sorgenti |
| Ordine a caldo | `internal/piecepicker/piecepicker.go`, `torrent/torrent_commands.go`, `torrent/session_torrent.go` | `PiecePicker.SetOrder` e `Torrent.SetSequential`/`SetFirstLast`: cambiano sequenziale/prima-ultima sul torrent in corso, ricalcolando i bordi dei file e persistendo il flag nel resume |
| Diagnostica pezzi | `internal/piecepicker/piecepicker.go`, `torrent/session_torrent.go` | `PiecePicker.PieceDownloading` e `Torrent.PieceStates`: stato per pezzo (`have`/`downloading`/`skipped`/mancante) per la mappa pezzi; dopo il completamento il motore azzera il picker, quindi gli stati si ricavano dai `pieces` |
| Limiti per-torrent | `internal/bandwidth/limiter.go`, `torrent/torrent.go`, `torrent/torrent_peer.go`, `torrent/torrent_start.go`, `torrent/session_limits.go` | `Limiter.SetParent`/`SetLimitKiB` e `Torrent.SetSpeedLimits`: ogni torrent ha un limitatore proprio che eredita quello di sessione (`-1`), è illimitato (`0`) o ha un tetto (`>0`); peer e web seed usano quello del torrent, quindi cambia a caldo |
| Connessioni/upload per-torrent | `torrent/torrent.go`, `torrent/torrent_peer.go`, `torrent/torrent_connection.go`, `torrent/session_listen.go`, `torrent/torrent_run.go`, `internal/unchoker/unchoker.go`, `torrent/session_limits.go` | `Torrent.SetMaxConnections`/`SetMaxUploads`: tetto alle connessioni instaurate (chiude le eccedenti al tick) e `Unchoker.SetNumUnchoked` per gli slot di upload |
| Streaming | `internal/piecepicker/piecepicker.go`, `torrent/session_stream.go` | `PiecePicker.SetStreamWindow` e `Torrent.FilePieceRange`/`SetFileStreamWindow`: i pezzi della finestra letta da un player vengono scelti per primi. `Torrent.PiecesDone(begin,end)`: check mirato del range, senza allocare uno stato per ogni pezzo |
| File completati da fermo | `torrent/torrent.go` (`fileStatsFromBitfield`, `fileStatsFromPieces`) | `FileStats` di un torrent fermo (pezzi non in memoria) calcola i byte completati di ogni file dal bitfield verificato, padding incluso negli offset, invece di rispondere con un errore: Gextto recupera così gli episodi già completi di un pack parcheggiato o abbandonato. Test `filestats_bitfield_test.go` |
| Super-seeding | `torrent/torrent_superseed.go`, `torrent/torrent_peer.go`, `torrent/torrent_messagehandler.go`, `torrent/torrent_run.go`, `torrent/torrent_pieces.go`, `torrent/session_torrent.go`, `torrent/session_add.go`, `torrent/session_load.go`, `internal/resumer/boltdbresumer` | `Torrent.SetSuperSeeding`: BEP 16 sul torrent in corso, attivo solo a torrent completato. Annuncia un pezzo alla volta e serve solo i pezzi *offerti* a quel peer; i pezzi offerti non si ripetono, `have`/`not-interested` e un tick di ritentativo fanno avanzare, e a esaurimento il peer viene liberato col bitfield pieno. Il flag è persistito nel resume. Il fork traccia anche il bitfield remoto quando il piece picker è `nil` (seed), che il super-seeding richiede |
| Holepunching | `internal/peerprotocol/holepunch.go`, `internal/peerprotocol/extension.go`, `internal/peersource`, `torrent/config.go`, `torrent/torrent.go`, `torrent/torrent_peer.go`, `torrent/torrent_messagehandler.go`, `torrent/torrent_handshake.go`, `torrent/torrent_holepunch.go`, `torrent/torrent_stats.go`, `torrent/torrent_commands.go`, `torrent/session_rpc_handler.go` | BEP 55 `ut_holepunch`: `Config.Holepunch` annuncia l'estensione e abilita il relè peer. Quando un dial fallisce, `tryHolepunchRendezvous` (una volta per endpoint, max 8 relè) chiede l'introduzione; il relè risponde con `connect` a entrambi e ogni lato dial su uTP (`peersource.Holepunch`, sorgente `SourceHolepunch`). `planHolepunchRendezvous` è pura e testata. Log di debug (`holepunch rendezvous …`) per seguire invio e ricezione del rendezvous |
| Ban di sessione | `torrent/session.go`, `torrent/torrent_write.go`, `torrent/torrent_peer.go`, `torrent/torrent_connection.go`, `torrent/session_listen.go`, `torrent/torrent_holepunch.go` | `Session.BanIP`/`IsBannedIP`: un peer che invia un pezzo corrotto viene bannato, **con scadenza** (`sessionBanTTL`, 30 min), da **tutta la sessione** e non solo dal torrent; `Torrent.ipBanned` unifica il controllo (per-torrent + sessione) in tutti i punti di connessione/dial/holepunch |
| BitTorrent v2 | `internal/metainfo`, `internal/merkle`, `internal/piece`, `internal/verifier`, `internal/piecewriter`, `internal/magnet`, `internal/peerprotocol`, `torrent/session_add.go`, `torrent/torrent_hashrequest.go` | Supporto **v2-only da `.torrent` e da magnet** (`urn:btmh:`): parsing `meta version`/`file tree`/`piece layers`, info-hash SHA-256 (troncato a 20 byte per l'identità), modello a **pezzi per-file** (coda più corta, nessun padding nel pezzo), verifica via **nodo Merkle** SHA-256 e **fetch dei `piece layers` dai peer** con i messaggi `hash request`/`hashes`/`hash reject` (id 21/22/23). Il bit riservato v2 è annunciato nell'handshake solo per i torrent con identità v2; il seed risponde dalle sue `piece layers` (le richieste a livello blocco sono rifiutate). Gli **ibridi** restano scaricati come v1 |

Anche `nictuku/dht` (licenza BSD) è incluso in `third_party/dht`. Modifiche:

- `Config.PacketConn`, per usare il socket uTP;
- `Stats()` con i nodi conosciuti e il traffico;
- **dual-stack IPv6 (BEP 32)**: con `Config.UDPProto == "udp"` il DHT parla
  entrambe le famiglie; i contatti v4 e v6 vengono separati nei campi
  `nodes`/`nodes6` delle risposte (`nodesForInfoHash`, `replyFindNode`), letti
  entrambi in ingresso (`nodeResponseFields`) e richiesti con `want: ["n4","n6"]`
  in `get_peers`. `"udp4"` (default) e `"udp6"` restano mono-famiglia;
- **read-loop robusto**: `readFromSocket` restituisce il buffer all'arena sul
  percorso d'errore e controlla `stop` prima di bloccarsi su `Pop`, così
  `Stop()` non può più restare appeso quando il socket condiviso viene chiuso.

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
- **Prima/ultima parte indipendente dal sequenziale.** Upstream marca i bordi
  dei file solo in modalità sequenziale. Il fork ha separato le due cose con un
  flag `firstLast` (`AddTorrentOptions.FirstLast`, campo `torrent.firstLast`,
  chiave resumer `first_last`, parametro extra di `piecepicker.New`), così
  l'opzione "prima/ultima parte" funziona senza forzare l'ordine sequenziale.
- **Preallocazione + provider di storage.** Upstream ha spostato la creazione
  dello storage in `torrent/session_storage.go`; il flag `Preallocate` ora è un
  campo del provider e viene applicato in `GetStorage`.
- **Rimozione torrent.** `Session.RemoveTorrent` ha un nuovo parametro
  `keepData`; il demone passa `false` per continuare a rimuovere solo il
  symlink in `LinkDir/<id>`.

Le correzioni storiche del fork (IP degli handshake chiusi, race del gestore di
log, verbi di formato) sono ora in gran parte superate dai fix upstream.

Come aggiornare la base in futuro: vedi `docs/motore-allineamento.md`.
