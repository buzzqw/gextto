# Motore gx-core — panoramica

Questa cartella contiene il **motore** di gx-core (il binario `gx-torrent`),
parte del modulo principale come
`github.com/buzzqw/gextto/internal/gxcore`. Lo usa solo il demone
`cmd/gx-torrent`. Una parte del codice deriva da lavoro con licenza MIT: vedi
[`LICENSE`](LICENSE).

Questa tabella è l'**inventario delle funzionalità** del motore e dei file che
le implementano.

Test del motore (girano dentro questo modulo):

- `internal/blocklist/blocklist_test.go` — formati del filtro IP, merge degli intervalli ai bordi (0.0.0.0/255.255.255.255) e lookup senza allocazioni;
- `internal/magnet/magnet_test.go` — parsing del magnet: v2 (`urn:btmh:`), `so=` (BEP 53), `ws=` e `dht=` (convenzioni magnet);
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
  (round-trip v4/v6, errori, framing nell'estensione, fuzz) e header BEP 21
  `upload_only` nell'extension handshake (round-trip bencode);
- `internal/announcer/announce_test.go` — BEP 21 lato tracker: un seed parziale
  annuncia `event=paused`, `stopped` resta `stopped`, un downloader resta
  `empty`;
- `internal/peerprotocol/donthave_test.go` — codec BEP 54 `lt_donthave`
  (round-trip, annuncio nell'handshake, payload corto rifiutato);
- `internal/piecepicker/donthave_test.go` — `HandleDontHave` azzera il bitfield
  e la disponibilità del pezzo;
- `torrent/torrent_donthave_test.go` — `handleDontHave` azzera il bitfield del
  peer;
- `torrent/torrent_selection_test.go` — maschere di selezione `so=` (BEP 53:
  indici sui file non-padding) e flag `Config.FileSelection`;
- `torrent/torrent_holepunch_test.go` — decisione del relè (connect a entrambi,
  errori `NotConnected`/`NoSupport`/`NoSelf`/`NoSuchPeer`).

Funzionalità e aree del motore:

| Area | File | Cosa |
|---|---|---|
| Porta unica | `torrent/session_listen.go`, `internal/btconn/accept.go`, `torrent/session.go`, `torrent/torrent_start.go`, `torrent/session_load.go`, `torrent/torrent.go`, `torrent/torrent_run.go` | `Config.ListenPort`: un solo listener di sessione. L'handshake BitTorrent/MSE sceglie il torrent dall'info hash (`AcceptRouted`) e consegna la connessione al torrent; tutti i torrent annunciano la stessa porta |
| Uscita e proxy | `internal/netx`, `internal/btconn/dial.go`, `internal/trackermanager`, `internal/tracker/udptracker/transport.go`, `torrent/session*.go` | `Config.OutgoingInterface` (killswitch VPN: senza indirizzo non esce nulla) e `Config.Proxy` (SOCKS5 o HTTP CONNECT per peer, tracker HTTP e web seed; tracker UDP rifiutati) |
| Filtro IP | `internal/blocklist/blocklist.go`, `torrent/session_blocklist.go` | Formati intervallo, P2P e eMule `.dat` oltre al CIDR; `Session.LoadBlocklist` da file locale. Le regole IPv4/IPv6 stanno in **intervalli ordinati e uniti** (ricerca binaria): un lookup non alloca e una regola costa pochi byte (prima un segment tree a puntatori, ~300 B/regola) |
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
| Connessioni/upload per-torrent | `torrent/torrent.go`, `torrent/torrent_peer.go`, `torrent/torrent_connection.go`, `torrent/session_listen.go`, `torrent/torrent_run.go`, `internal/unchoker/unchoker.go`, `torrent/session_limits.go` | `Torrent.SetMaxConnections`/`SetMaxUploads`: tetto alle connessioni instaurate (chiude subito le eccedenti) e `Unchoker.SetNumUnchoked` per gli slot di upload |
| Streaming | `internal/piecepicker/piecepicker.go`, `torrent/session_stream.go` | `PiecePicker.SetStreamWindow` e `Torrent.FilePieceRange`/`SetFileStreamWindow`: i pezzi della finestra letta da un player vengono scelti per primi. `Torrent.PiecesDone(begin,end)`: check mirato del range, senza allocare uno stato per ogni pezzo |
| File completati da fermo | `torrent/torrent.go` (`fileStatsFromBitfield`, `fileStatsFromPieces`) | `FileStats` di un torrent fermo (pezzi non in memoria) calcola i byte completati di ogni file dal bitfield verificato, padding incluso negli offset, invece di rispondere con un errore: Gextto recupera così gli episodi già completi di un pack parcheggiato o abbandonato. Test `filestats_bitfield_test.go` |
| Super-seeding | `torrent/torrent_superseed.go`, `torrent/torrent_peer.go`, `torrent/torrent_messagehandler.go`, `torrent/torrent_run.go`, `torrent/torrent_pieces.go`, `torrent/session_torrent.go`, `torrent/session_add.go`, `torrent/session_load.go`, `internal/resumer/boltdbresumer` | `Torrent.SetSuperSeeding`: BEP 16 sul torrent in corso, attivo solo a torrent completato. Annuncia un pezzo alla volta e serve solo i pezzi *offerti* a quel peer; i pezzi offerti non si ripetono, `have`/`not-interested` e un tick di ritentativo fanno avanzare, e a esaurimento il peer viene liberato col bitfield pieno. Il flag è persistito nel resume. Il motore traccia anche il bitfield remoto quando il piece picker è `nil` (seed), che il super-seeding richiede |
| Holepunching | `internal/peerprotocol/holepunch.go`, `internal/peerprotocol/extension.go`, `internal/peersource`, `torrent/config.go`, `torrent/torrent.go`, `torrent/torrent_peer.go`, `torrent/torrent_messagehandler.go`, `torrent/torrent_handshake.go`, `torrent/torrent_holepunch.go`, `torrent/torrent_stats.go`, `torrent/torrent_commands.go`, `torrent/session_rpc_handler.go` | BEP 55 `ut_holepunch`: `Config.Holepunch` annuncia l'estensione e abilita il relè peer. Quando un dial fallisce, `tryHolepunchRendezvous` (una volta per endpoint, max 8 relè) chiede l'introduzione; il relè risponde con `connect` a entrambi e ogni lato dial su uTP (`peersource.Holepunch`, sorgente `SourceHolepunch`). `planHolepunchRendezvous` è pura e testata. Log di debug (`holepunch rendezvous …`) per seguire invio e ricezione del rendezvous |
| Ban di sessione | `torrent/session.go`, `torrent/torrent_write.go`, `torrent/torrent_peer.go`, `torrent/torrent_connection.go`, `torrent/session_listen.go`, `torrent/torrent_holepunch.go` | `Session.BanIP`/`IsBannedIP`: un peer che invia un pezzo corrotto viene bannato, **con scadenza** (`sessionBanTTL`, 30 min), da **tutta la sessione** e non solo dal torrent; `Torrent.ipBanned` unifica il controllo (per-torrent + sessione) in tutti i punti di connessione/dial/holepunch |
| BitTorrent v2 | `internal/metainfo`, `internal/merkle`, `internal/piece`, `internal/verifier`, `internal/piecewriter`, `internal/magnet`, `internal/peerprotocol`, `torrent/session_add.go`, `torrent/torrent_hashrequest.go` | Supporto **v2-only da `.torrent` e da magnet** (`urn:btmh:`): parsing `meta version`/`file tree`/`piece layers`, info-hash SHA-256 (troncato a 20 byte per l'identità), modello a **pezzi per-file** (coda più corta, nessun padding nel pezzo), verifica via **nodo Merkle** SHA-256 e **fetch dei `piece layers` dai peer** con i messaggi `hash request`/`hashes`/`hash reject` (id 21/22/23). Il bit riservato v2 è annunciato nell'handshake solo per i torrent con identità v2; il seed risponde dalle sue `piece layers` e, per un file presente per intero, anche alle richieste a livello blocco (`base = 0`, albero ricostruito dai dati e messo in cache); un file incompleto riceve `hash reject`. Gli **ibridi** restano scaricati come v1 |
| Tracker scrape | `internal/tracker/tracker.go`, `internal/tracker/tier.go`, `internal/tracker/httptracker/httptracker.go`, `internal/tracker/udptracker/{action,messages,request,requestbase,transaction,connection,transport,udptracker}.go`, `torrent/torrent_scrape.go` | `Tracker.Scrape` (BEP 48): **HTTP** su `/scrape` (mappa `…/announce`→`…/scrape` conservando la passkey) e **UDP** con l'azione BEP 15. Il transport UDP è generalizzato a `udpRequest` (`GetDest`/`SetConnectionID`/`Done`), così accoglie anche lo scrape oltre all'announce. `Tier.Scrape` delega al tracker corrente e avanza sull'errore come `Announce`; `Torrent.Scrape` scrapa tutti i tracker in parallelo (errore per-tracker). Test in `internal/tracker/{httptracker,udptracker}` |
| Bus di eventi | `torrent/events.go`, `torrent/session.go`, `torrent/session_add.go`, `torrent/torrent_write.go`, `torrent/torrent_peer.go`, `torrent/torrent_close.go`, `torrent/torrent_stop.go`, `internal/announcer/periodic.go` (`SetResultHook`) | `Session.Subscribe()` → canale di `Event`. Tipi: `torrent_added`, `torrent_completed`, `torrent_stopped`, `torrent_error`, `torrent_piece` (alta frequenza), `peer_connected`/`peer_disconnected`, `tracker_announce` (con seeders/leechers o l'errore). L'engine non blocca mai su un subscriber lento (scarta se il buffer da 64 è pieno). Esposto dal demone come SSE `GET /ui/events` e usato dalla pagina per aggiornarsi subito |
| Creazione torrent | `torrent/torrent_create.go` | `CreateTorrent(path, opt)`: costruisce un `.torrent` v1 da un file o cartella, esponendo il già presente `metainfo.NewInfoBytes`/`NewBytes` (nome, tracker, web seed, private, piece length, commento). Usato dal demone (form `POST /ui/create-torrent` e `POST /api/v1/create-torrent`) |
| Diagnosi storage | `torrent/torrent_write.go`, `torrent/session.go`, `torrent/session_stats.go` | L'errore di scrittura di un pezzo viene arricchito con la **cartella di storage** (`storage %q could not write piece #N`, con `%w`) e contato in `SessionStats.StorageErrors` (esposto come `storage_errors` nei dati del demone), per distinguere un disco/mount guasto da un problema di rete |
| Priorità graduata (streaming) | `internal/piecepicker/piecepicker.go`, `torrent/session_stream.go`, `cmd/gx-torrent/stream.go` | `PiecePicker.SetReadaheadWindow`/`pickReadahead`: la finestra del player (`SetStreamWindow`) ha priorità sulla finestra di **readahead** (nuova `Torrent.SetFileReadaheadWindow`), che precede rarest-first/allowed-fast. Il demone aggiorna entrambe (`setStreamWindows`) mentre serve il file |
| Coda richieste adattiva | `internal/peer/peer.go` (`RequestQueue`), `torrent/torrent_start.go`, `torrent/torrent_messagehandler.go` | La profondità delle richieste in volo parte da `DefaultRequestsOut` (o `reqq`) e **cresce di uno per blocco consegnato** fino a `MaxRequestsOut`; **reset su choke e su snub**. Un peer lento resta alla base, uno veloce scava più a fondo |
| BEP 27 (privati) | `torrent/torrent.go` (`privateSwarm`), `torrent/torrent_start.go`, `torrent/torrent_messagehandler.go` | Regola centralizzata in `torrent.privateSwarm()`: **DHT** (announcer), **PEX** (invio *e* ricezione) disattivati sui torrent privati; la **LSD** (demone) salta i privati su invio e ricezione. Il **PEX in ricezione** era l'unico percorso non filtrato (fix 2026-10-11), con guard test `torrent_private_test.go` |
| BEP 53 (`so=`) + magnet `ws=`/`dht=` | `internal/magnet/magnet.go`, `torrent/session_add.go`, `torrent/torrent_selection.go`, `torrent/torrent.go`, `torrent/session_torrent.go` | Il parametro `so` del magnet (Select Only, BEP 53: indici e range tipo `0,2,4-7`, de-duplicati) è parsato in `Magnet.SelectOnly` e diventa il **seed** del torrent (`AddTorrentOptions.SelectOnly`/`torrent.selectOnly`, guardato da `selectOnlyMu`). `skippedInfoFiles` usa il seed **solo** quando `Config.FileSelection` non restituisce una maschera: la scelta dell'app ha sempre la precedenza, così l'utente può ripristinarla. `Torrent.SelectOnly`/`ClearSelectOnly` leggono e azzerano il seed. Il demone lo converte in `FilePriorities` appena arriva il metadata (`docs/gx-torrent.md`). Il parametro `ws=` (web seed, convenzione Magnet URI) è parsato in `Magnet.WebSeeds` e passato come web seed del torrent, persistito nel resume (`Spec.URLList`). Restano `xs=`/`as=`. Il parametro `dht=` (nodo DHT di bootstrap, convenzione Magnet URI) è parsato in `Magnet.DHTNodes` e aggiunto alla DHT di sessione (`session.dht.AddNode`) |
| BEP 21 (`upload_only`, `event=paused`) | `internal/peerprotocol/extension.go`, `torrent/torrent_peer.go` (`sendExtensionHandshake`), `torrent/torrent_pieces.go` (`checkCompletion`), `torrent/torrent_selection.go` (`uploadOnly`), `torrent/torrent_announce.go` (`PartialSeed`), `internal/tracker/event.go`, `internal/announcer/announce.go` | L'extension handshake BEP 10 include `upload_only: 1` quando il torrent è completo (`torrent.uploadOnly` = `completed`): seed pieno o **parziale** (tutti i pezzi *voluti* presenti, selection). Inviato alla connessione e **rinviato a tutti i peer** al passaggio a seed. Il valore ricevuto resta in `peer.ExtensionHandshake.UploadOnly`. **Lato tracker**: un seed parziale (`tracker.Torrent.PartialSeed`, da `announcerFields`) annuncia `event=paused` (`tracker.EventPaused`) su ogni announce non-`stopped`; il `stopped` resta invariato. Manca solo il campo `downloaders` dello scrape (lo fornisce il tracker, opzionale) |
| BEP 54 (`lt_donthave`) | `internal/peerprotocol/extension.go` (`DontHaveMessage`, `ExtensionKeyDontHave`), `torrent/torrent_messagehandler.go` (`handleDontHave`), `internal/piecepicker/piecepicker.go` (`HandleDontHave`) | `lt_donthave` è annunciato nell'extension handshake; un `DontHave` in arrivo (`op 20`, subop, **indice a 4 byte** big-endian) azzera il bit del pezzo nel bitfield del peer e la sua disponibilità nel picker (`removeHavingPeer`); se la **Fast Extension** non è negoziata viene cancellato anche il download attivo di quel pezzo da quel peer (`closePieceDownloader`, come per un choke). Solo **ricezione**: gx-core non scarta pezzi di torrent attivi, quindi non invia `DontHave` |

Il DHT vive in `internal/dht` (modulo a sé). Caratteristiche:

- `Config.PacketConn`, per usare il socket uTP;
- `Stats()` con nodi, traffico e **diagnostica per RPC** (`queries_in`/`queries_out`/`replies_in` per ping/get_peers/find_node/announce_peer) più i contatori di **screening** (`blocked_hosts`, `self_promotions`, `dupe_references`, `nodes_killed`) e la **porta esterna riflessa** (`reflected_port`, con consensus su ≥2 osservazioni, BEP 42); il demone li espone come `dht_<chiave>` in API e UI, ed è la base di misura dell'hardening anti-Sybil (test `internal/dht/stats_test.go`, dettaglio in `docs/evoluzione.md` §4.10–§4.11);
- **dual-stack IPv6 (BEP 32)**: con `Config.UDPProto == "udp"` il DHT parla
  entrambe le famiglie; i contatti v4 e v6 vengono separati nei campi
  `nodes`/`nodes6` delle risposte (`nodesForInfoHash`, `replyFindNode`), letti
  entrambi in ingresso (`nodeResponseFields`) e richiesti con `want: ["n4","n6"]`
  in `get_peers`. `"udp4"` (default) e `"udp6"` restano mono-famiglia;
- **read-loop robusto**: `readFromSocket` restituisce il buffer all'arena sul
  percorso d'errore e controlla `stop` prima di bloccarsi su `Pop`, così
  `Stop()` non può più restare appeso quando il socket condiviso viene chiuso;
- **security extension BEP 42** (`dht_sec.go`): l'ID nodo è derivato dall'IP
  esterno (`SecureNodeID`/`NodeIDSecure`, CRC32C Castagnoli, vettori del BEP
  verificati in `dht_sec_test.go`), così non può essere scelto per avvicinarsi a
  un info-hash bersaglio. L'IP esterno arriva da `Config.PublicIP` (impostato dal
  motore da `DHTHost` o dall'interfaccia uscente) o, se sconosciuto, dal campo
  `ip` delle risposte (`noteExternalIP`, adozione una tantum con ID sicuro
  persistito). Gli indirizzi locali restano esenti;
- **Igiene dei token** (`dht.go`, `hostToken`/`checkToken`): il token di
  `announce_peer` è legato a **indirizzo sorgente + scope** (l'info-hash per
  `get_peers`/`announce_peer`, `nil` per i percorsi BEP 44), quindi un token
  preso per uno sciame non si riusa su un altro (*query non corrispondente*);
  restano il doppio segreto rotante e i contatori `announce_ok_token`/
  `announce_bad_token`; test `dht_token_test.go`;
- **BEP 44** (`bep44.go`): archivio chiave/valore nel DHT — item **immutabili**
  (target = SHA-1 del valore bencodato) e **mutabili** (target = SHA-1 della
  chiave ed25519 + salt, firma ed25519, `seq` monotono, `cas`). Implementate le
  query `get`/`put` (token; limiti 1000/64 byte; errori 205/206/207/301/302) e
  l'API locale `PutImmutable`/`PutMutable`/`Get`; item con TTL 2 h. I vettori
  ufficiali del BEP sono verificati in `bep44_test.go`;
- **BEP 43** (read-only): con `Config.ReadOnly` il nodo non risponde alle query e
  aggiunge `ro: 1` alle proprie; un nodo che riceve una query con `ro` non
  aggiunge il mittente alla routing table. Nel demone: flag `-dht-read-only`
  (`GX_TORRENT_DHT_READ_ONLY`);
- **BEP 33** (DHT scrape): `bloom.go` (filtro 2048 bit, k=2, vettore ufficiale
  verificato) e `peer_store.go` (IP unici per info-hash con flag seed). Una
  `get_peers` con `scrape=1` risponde con **`BFpe`/`BFsd`** (256 byte); `noseed=1`
  filtra i seed; `announce_peer` con `seed=1` marca il peer. Lato client
  `scrape_client.go` (`DHT.Scrape`) interroga i nodi più vicini, aggrega i filtri
  e stima (`EstimateScrape`); il motore lo espone con `Session.DHTScrape` /
  `Torrent.DHTScrape`, il demone con `GET /api/v1/torrents/{hash}/dhtscrape` e la
  pagina con il pulsante **DHT swarm** (`GET /ui/dhtscrape`).

uTP usa la libreria `github.com/anacrolix/utp` come dipendenza.
