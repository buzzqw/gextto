# Integrazione di `anacrolix/torrent` come backend torrent nativo

## Stato del documento

- **Stato:** proposta tecnica / piano d'opera
- **Backend ufficiale attuale:** libtorrent integrato tramite CGo
- **Backend alternativo proposto:** `github.com/anacrolix/torrent`
- **Versione di riferimento:** `v1.61.0`, da fissare e verificare prima dell'implementazione
- **Obiettivo:** aggiungere un backend BitTorrent nativo Go mantenendo libtorrent come default
- **Vincolo:** un torrent deve essere posseduto da un solo backend alla volta

---

## 1. Sintesi esecutiva

`anacrolix/torrent` è una libreria Go, non un daemon remoto. Per Gextto questo è interessante perché può vivere nello stesso processo del daemon, senza CGo aggiuntivo per il backend alternativo, senza Web API e senza un servizio esterno.

La proposta è introdurre un secondo backend dietro un contratto comune:

```text
                         Gextto
      UI / API / database / acquisizione / post-processing
                              |
                       TorrentBackend
                       /            \
              libtorrent         anacrolix/torrent
              ufficiale           backend Go opzionale
```

Gextto deve continuare a possedere:

- database dei torrent e delle release;
- coda applicativa;
- policy stalled e retry;
- spostamento dei dati;
- post-processing, rinomina e archivio;
- notifiche;
- UI **Scarico**;
- decisione di completamento, errore e rimozione.

`anacrolix/torrent` deve fornire il piano di trasferimento:

- protocollo BitTorrent;
- DHT, tracker, PEX e uTP;
- verifica dei pezzi;
- storage dei dati;
- priorità file e pezzi;
- connessioni peer;
- statistiche e callback;
- web seed e metainfo asincrona.

La sostituzione deve essere trasparente a livello di Gextto, ma non è possibile conservare le connessioni peer attive o il fastresume interno di libtorrent durante uno switch. Il passaggio deve quindi essere una migrazione controllata: pausa, manifest, chiusura del backend sorgente, ricostruzione del torrent, verifica e ripresa.

## 2. Valutazione tecnica

### 2.1 Punti di forza

La libreria espone API native per:

- `Client.AddMagnet`, `AddTorrent`, `AddTorrentFromFile` e `AddTorrentOpt`;
- elenco e lookup dei torrent;
- `Torrent.InfoHash`, metainfo e metadata asincroni;
- bytes completati/mancanti, complete e seeding;
- allow/disallow del download e dell'upload;
- priorità file e pezzo;
- verifica del torrent o del singolo pezzo;
- stato dei pezzi e sottoscrizione alle variazioni;
- peer, peer connections e statistiche;
- tracker, DHT, web seed e sorgenti;
- storage file, mmap, SQLite, Bolt e backend custom;
- callback per peer, tracker, dati utili e errori di scrittura.

Il modello è adatto a Gextto perché permette di osservare più dettagli di alto livello rispetto a un semplice client remoto.

### 2.2 Differenze rispetto a libtorrent

Non deve essere considerato un drop-in replacement binario:

- il fastresume libtorrent non è riutilizzabile direttamente;
- la sessione e gli alert libtorrent non esistono;
- il modello di storage è diverso;
- la coda automatica deve essere mantenuta da Gextto;
- i limiti di banda hanno un modello diverso;
- alcune impostazioni sono definite alla creazione del client;
- cache, preallocazione e move non hanno necessariamente la stessa semantica;
- alcune funzioni avanzate di libtorrent potrebbero non avere una capability equivalente.

La compatibilità deve essere quindi definita a livello di comportamento Gextto e non a livello dei parametri interni dei due motori.

## 3. Situazione attuale di Gextto

Il codice è oggi accoppiato a `*LibtorrentClient` in molti punti:

- `libtorrent.go` — sessione, storage e operazioni torrent;
- `appstate.go` — riferimento al backend nello stato applicativo;
- `cmd/gexttod/main.go` — costruzione e shutdown;
- `orchestrator.go` — aggiunta e accodamento;
- `web_background.go` — worker eventi e completamenti;
- `web_torrent_events.go` — stalled, metadata, move, retry e seed policy;
- `web_handlers_g1.go`, `web_handlers_g2.go`, `web_handlers_g4.go`, `web_handlers_g6.go`, `web_handlers_g7.go` — API e azioni UI;
- `comics.go` — torrent dei fumetti;
- test libtorrent, API e trasferimento reale.

Il lavoro principale consiste nel separare:

1. logica applicativa Gextto;
2. contratto comune del motore torrent;
3. implementazione libtorrent;
4. implementazione anacrolix.

Il refactoring deve lasciare invariato il comportamento del backend attuale.

## 4. Obiettivi e non obiettivi

### 4.1 Obiettivi

1. Mantenere libtorrent come backend predefinito.
2. Aggiungere `anacrolix` come backend selezionabile.
3. Conservare UI e API Gextto.
4. Conservare coda, stalled, retry, stop/resume e seed policy.
5. Conservare move asincrono, verifica e post-processing.
6. Conservare la riconciliazione dopo riavvio.
7. Preservare hash, release associata, path e dati già presenti durante una migrazione.
8. Aggiungere diagnostica per pezzo, peer, tracker e DHT.
9. Aggiungere download selettivo e priorità a livello file/pezzo.
10. Ottenere benchmark comparabili tra i backend.

### 4.2 Non obiettivi della prima versione

- replica di ogni parametro interno di libtorrent;
- cache RAM custom complessa;
- VPN binding specifico;
- accesso al database interno di anacrolix;
- daemon HTTP separato;
- più `torrent.Client` simultanei;
- conservazione delle connessioni peer durante lo switch;
- streaming video come requisito del primo rilascio.

## 5. Architettura del backend

### 5.1 Contratto comune

Il contratto dovrebbe essere diviso in capability, evitando un'interfaccia monolitica che obblighi un backend a simulare funzioni inesistenti.

```go
type TorrentBackend interface {
    Name() string
    Start(ctx context.Context) error
    Shutdown(ctx context.Context) error

    List(ctx context.Context) ([]models.TorrentView, error)
    AddMagnet(ctx context.Context, magnet string, opts AddOptions) (string, error)
    AddTorrentFile(ctx context.Context, path string, opts AddOptions) (string, error)

    Pause(ctx context.Context, hash string) error
    Resume(ctx context.Context, hash string) error
    Restart(ctx context.Context, hash string) error
    Remove(ctx context.Context, hash string, deleteFiles bool) error
    Recheck(ctx context.Context, hash string) error
    MoveStorage(ctx context.Context, hash, destination string) error

    Files(ctx context.Context, hash string) ([]models.FileView, error)
    Peers(ctx context.Context, hash string) ([]models.PeerView, error)
    Trackers(ctx context.Context, hash string) ([]models.TrackerView, error)
    Stats(ctx context.Context, hash string) (TorrentStats, error)
    Poll(ctx context.Context) ([]models.TorrentEvent, error)
}
```

Capability consigliate:

- `TorrentCore` — add/list/start/stop/remove;
- `TorrentStorage` — files/move/recheck;
- `TorrentPriority` — file e pezzi;
- `TorrentNetwork` — tracker, peer, reannounce;
- `TorrentRateControl` — limiti e connessioni;
- `TorrentEvents` — callback e polling;
- `TorrentPieceInspection` — pezzi, verifica e diagnostica;
- `TorrentMetadata` — `.torrent`, magnet e infohash.

### 5.2 Servizi sopra il backend

La logica applicativa deve essere spostata in servizi che ricevono il backend astratto:

- `TorrentScheduler` — coda e slot;
- `TorrentReconciler` — stato desiderato/osservato;
- `TorrentEventNormalizer` — eventi comuni;
- `StorageMoveManager` — move e retry;
- `TorrentCompletionManager` — completamento e post-processing;
- `SeedPolicyManager` — ratio e tempo;
- `TorrentHealthManager` — salute e diagnostica;
- `TorrentMigrationManager` — switch tra backend.

Questo evita di duplicare in `AnacrolixBackend` la logica già presente in `web_torrent_events.go` e `web_background.go`.

## 6. Implementazione di `AnacrolixBackend`

### 6.1 Client e lifecycle

Gextto deve creare un solo `*torrent.Client` per processo:

1. leggere la configurazione;
2. creare `torrent.NewDefaultClientConfig`;
3. applicare storage, listener, DHT, tracker, PEX, TCP/uTP, blocklist e limiti;
4. installare callback non bloccanti;
5. creare il client;
6. ripristinare i torrent da manifest e database;
7. avviare reconciler e scheduler;
8. esporre stato e capability alla UI.

I callback anacrolix possono essere sincroni e con lock interni. Devono soltanto copiare dati in una coda interna buffered. Non devono eseguire SQL, filesystem lento o chiamate HTTP direttamente.

### 6.2 Configurazione di rete

Configurazione concettuale:

```go
cfg := torrent.NewDefaultClientConfig()
cfg.DataDir = dataDir
cfg.ListenPort = configuredPort
cfg.DisableUTP = !settings.UTP
cfg.DisableTCP = !settings.TCP
cfg.DisablePEX = !settings.PEX
cfg.NoDHT = !settings.DHT
cfg.NoDefaultPortForwarding = !settings.UPnP
cfg.IPBlocklist = blocklist
cfg.PieceHashersPerTorrent = configuredHashers
cfg.Slogger = logger
```

I nomi definitivi devono essere verificati sulla versione fissata. La configurazione di Gextto va tradotta esplicitamente; i campi non supportati devono produrre un warning o una capability negativa, non essere ignorati in silenzio.

### 6.3 Magnet e metadata asincroni

`AddMagnet` può restituire il torrent prima che l'info sia disponibile. Il backend deve gestire:

```text
added_without_metadata
  -> metadata_fetching
  -> metadata_ready
  -> checking
  -> queued/downloading
```

Il post-processing deve attendere `GotInfo`, file disponibili e stato coerente dei pezzi. Il solo successo di `AddMagnet` non è condizione di completamento.

### 6.4 `.torrent`

L'aggiunta da file deve:

1. validare metainfo;
2. calcolare e normalizzare infohash v1/v2;
3. selezionare storage e path;
4. aggiungere con `AddTorrentFromFile` o `AddTorrentOpt`;
5. applicare priorità file;
6. applicare lo stato desiderato;
7. registrare metadata persistente in Gextto.

Ogni torrent gestito deve conservare il `.torrent` sotto il data directory Gextto.

## 7. Storage, verifica e resume

### 7.1 Storage iniziale consigliato

La prima implementazione deve usare storage file con un completion store persistente:

```text
GEXTTO_DATA_DIR/
  torrents/
    <infohash>.torrent
  anacrolix/
    piece-completion.sqlite
    metadata.json
    storage/
  downloads/
  incomplete/
```

La struttura reale deve rispettare i path già esposti da Gextto. Non si deve assumere che il layout predefinito di `storage.NewFile` sia identico a quello corrente senza test.

### 7.2 Piece completion

Valutare, sulla versione fissata:

1. `storage.NewFileWithCompletion` con SQLite;
2. `storage.NewSqlitePieceCompletion`;
3. implementazione Gextto di `PieceCompletion` per controllare transazioni e backup.

La prima scelta deve preferire le primitive ufficiali della libreria, evitando codice custom prematuro. Devono essere testati WAL, checkpoint, chiusura, crash, move e modifica esterna dei file.

### 7.3 Resume

Il resume anacrolix non è il fastresume libtorrent. La procedura sicura è:

1. persistere completamento dei pezzi;
2. chiudere ordinatamente il client;
3. ricostruire torrent, metainfo e storage;
4. sincronizzare completion store e file reali;
5. verificare i pezzi sospetti;
6. ripristinare priorità e stato desiderato.

In caso di dubbio Gextto deve preferire il recheck a una falsa dichiarazione di completamento.

### 7.4 Preallocazione e sparse files

La preallocazione deve essere una policy Gextto indipendente:

- `none`;
- `sparse`;
- `full`;
- `auto` in base a filesystem, spazio e dimensione.

La validazione già presente in `media_guard.go` deve continuare a distinguere file reali da file soltanto preallocati.

## 8. Coda e gestione automatica

### 8.1 Coda Gextto

La coda deve rimanere in Gextto. Per rimuovere un torrent dagli slot senza cancellarlo:

- `Torrent.DisallowDataDownload`;
- opzionalmente `DisallowDataUpload` per la pausa completa;
- stato desiderato persistito in Gextto.

`Torrent.Drop` va riservato a rimozione, detach definitivo o migrazione. Non deve essere usato come semplice pausa.

### 8.2 Scheduler

Il `TorrentScheduler` deve:

1. leggere torrent e policy;
2. separare download, seed, checking, moving e errori;
3. calcolare gli slot;
4. escludere stalled oltre soglia;
5. rispettare pin, priorità e gap-fill;
6. attivare/disattivare download;
7. verificare l'effetto nel ciclo successivo;
8. applicare cooldown per evitare start/stop oscillanti;
9. registrare la decisione nel log.

### 8.3 Stalled e retry

Il concetto di stalled resta applicativo. Indicatori utili:

- bytes completati invariati;
- velocità utile nulla o sotto soglia;
- assenza di seeder con pezzi utili;
- peer pending senza connessioni attive;
- errori tracker/DHT;
- tempo dall'ultimo `ReceivedUsefulData`;
- piece state senza progresso.

Macchina a stati:

```text
progressing -> suspected_stalled -> stalled
       ^                              |
       |                              v
       +--------- eligible <- retry_wait
```

Il retry deve preservare i dati parziali e può eseguire reannounce o recheck mirato. Un torrent senza peer non deve diventare errore terminale soltanto per inattività temporanea.

### 8.4 Errori storage

Usare `Torrent.SetOnWriteChunkError` per trasformare gli errori in eventi Gextto:

- spazio insufficiente;
- permesso negato;
- filesystem non montato;
- errore I/O;
- file spostato esternamente;
- storage chiuso.

Il callback accoda l'errore; il worker decide pausa, retry, move o errore terminale.

## 9. Nuove funzioni anacrolix-specifiche

### 9.1 Diagnostica per pezzo

Nuovi endpoint e vista:

```text
GET  /api/torrents/{hash}/pieces
GET  /api/torrents/{hash}/pieces/runs
POST /api/torrents/{hash}/pieces/{index}/priority
POST /api/torrents/{hash}/pieces/{index}/verify
```

Informazioni:

- indice pezzo;
- stato missing/partial/complete/checking/requested;
- bytes mancanti;
- priorità;
- ultimo cambio stato;
- peer che possiedono il pezzo quando disponibile;
- eventuale errore di verifica.

Per torrent grandi deve essere usata una forma compatta a intervalli.

### 9.2 Download selettivo

Usando `File.SetPriority`, `File.Download`, `Torrent.DownloadPieces` e `Piece.SetPriority`, aggiungere:

- scarica solo file selezionati;
- escludi sample, NFO e allegati;
- priorità alta per il video principale;
- priorità per episodio o capitolo necessario;
- profili per film, serie, pack e fumetti.

Il post-processing deve conoscere la selezione e non considerare completato un contenuto se i file richiesti non sono completi.

### 9.3 Event bus nativo

Usare:

- `Callbacks.StatusUpdated` per peer e tracker;
- `ReceivedUsefulData` per progresso reale;
- `NewPeer` e `PeerClosed`;
- `CompletedHandshake`;
- `SubscribePieceStateChanges`;
- `SetOnWriteChunkError`.

Normalizzare in un bus Gextto:

```go
type TorrentBackendEvent struct {
    Backend    string
    Hash       string
    Kind       string
    At         time.Time
    Generation uint64
    Data       map[string]any
    Err        error
}
```

Gli eventi di errore, completamento e move non devono essere persi. Gli eventi grafici frequenti possono essere coalesciti.

### 9.4 Diagnostica tracker/DHT/PEX

Arricchire `torrents/{hash}/why` e Salute con:

- ultimo announce riuscito;
- ultimo errore tracker;
- prossimo retry;
- peer trovati da tracker, DHT, PEX e incoming;
- peer connected/pending/active;
- DHT disponibile;
- metadata in attesa;
- motivo principale dell'assenza di progresso.

### 9.5 Diagnostica peer

Usare `Peer.Stats`, `PeerConn.PeerPieces` e statistiche aggregate per mostrare:

- endpoint e rete;
- client/peer ID quando disponibile;
- bytes inviati/ricevuti;
- velocità;
- pezzi disponibili;
- fonte del peer;
- handshake e direzione della connessione.

### 9.6 Verifica programmata

Creare un `PieceCheckScheduler` con:

- coda di verifica;
- numero massimo di hashers;
- priorità per torrent sospetti;
- finestre a basso carico;
- progress reporting;
- persistenza di motivo e risultato.

Usare `Piece.VerifyDataContext` per check mirati e `Torrent.VerifyDataContext` per check completi.

### 9.7 Web seed e streaming futuro

`Torrent.AddWebSeeds` può essere usato per un fallback HTTP controllato, sempre con verifica dei pezzi.

Le API `Reader`, `File.NewReader` e `Torrent.NewReader` permettono in futuro anteprima o streaming. Questa funzione non deve entrare nel primo rilascio, ma il modello di priorità deve permettere un readahead temporaneo senza alterare permanentemente la selezione file.

## 10. Cache, limiti e risorse

### 10.1 Cache

Anacrolix non espone una singola impostazione equivalente a `libtorrent.cache_size`. La cache effettiva dipende da storage, page cache OS, mmap, buffer peer, RAM e RAM disk.

Per il primo rilascio:

- usare storage file stabile;
- affidarsi alla cache OS;
- evitare cache RAM custom;
- misurare RSS, page cache e I/O;
- introdurre profili solo dopo benchmark.

Possibili evoluzioni:

1. storage mmap selettivo;
2. cache bounded sopra `storage.ClientImpl`;
3. cache piece-aware con eviction;
4. profili RAM disk/NAS;
5. metriche hit/miss e bytes dirty.

L'operazione **Ottimizza** deve avere un profilo anacrolix separato e non copiare automaticamente i valori libtorrent.

### 10.2 Limiti globali e per torrent

`ClientConfig` supporta rate limiter Go per upload e download. Le unità devono essere normalizzate a bytes/s.

Prima di promettere modifiche runtime bisogna verificare il comportamento della versione fissata. Se il limiter non è dinamico, Gextto deve usare un proprio controllo o dichiarare che il cambio richiede restart.

`Torrent.SetMaxEstablishedConns` copre il limite connessioni. I limiti di banda per torrent non devono essere considerati disponibili automaticamente: vanno implementati tramite scheduler o capability dedicata dopo benchmark.

### 10.3 Risorse

Configurare e misurare:

- `PieceHashersPerTorrent`;
- `MaxUnverifiedBytes`;
- peer established e half-open;
- torrent attivi;
- check simultanei;
- callback al secondo;
- memoria del completion store.

## 11. Storage move e path

### 11.1 Path maker

Il path maker deve:

- rispettare root Gextto;
- impedire traversal;
- gestire single e multi-file;
- produrre path deterministici;
- riconoscere dati già presenti;
- supportare categorie e tag;
- non sovrascrivere l'archivio.

Usare le protezioni del package storage, inclusa la sanitizzazione del path, ma validare anche root e policy Gextto.

### 11.2 Storage move

Il move deve essere gestito da un job Gextto:

```text
requested -> quiescing -> moving/copying -> validating -> resumed
                 |              |               |
                 +----------> retry_wait <-------+
```

Procedura:

1. disabilitare download e upload necessari;
2. attendere write pendenti;
3. muovere con rename sullo stesso filesystem o copy-then-rename;
4. aggiornare path maker e completion store;
5. verificare file e pezzi necessari;
6. ripristinare stato desiderato;
7. ritentare con backoff in caso di errore.

Un torrent in move non deve essere classificato come stalled o completato.

## 12. Metadata, manifest e migrazione

### 12.1 Metadata persistenti

Ogni torrent deve conservare:

```text
infohash v1/v2
magnet
.torrent path
display name
release id
kind
save path
temporary path
backend ownership
desired state
observed state
file priorities
seed policy
manual override
completion generation
last piece check
```

Il manifest deve essere scritto atomically e permettere di riprendere una migrazione dopo crash.

### 12.2 Switch libtorrent → anacrolix

Preservabili:

- hash;
- metainfo;
- release;
- file presenti;
- path;
- priorità traducibili;
- stato desiderato;
- seed policy;
- record Gextto.

Non preservabili:

- peer e request in volo;
- cache interna libtorrent;
- fastresume libtorrent come formato anacrolix;
- statistiche non persistite.

Procedura:

1. sospendere acquisizione e nuovi comandi;
2. mettere in pausa i torrent;
3. attendere move/check critici;
4. conservare `.torrent` e manifest;
5. chiudere libtorrent;
6. costruire storage e client anacrolix;
7. riaggiungere i torrent negli stessi path;
8. applicare priorità e stato;
9. verificare i pezzi necessari;
10. confrontare hash, path e bytes completi;
11. riattivare torrent precedentemente attivi;
12. riprendere scheduler e post-processing;
13. eliminare manifest solo dopo riconciliazione.

Il passaggio inverso deve essere simmetrico. Entrambi i backend devono essere completamente chiusi prima di consegnare i file all'altro.

## 13. UI e API

La UI **Scarico** deve rimanere invariata per:

- elenco e progresso;
- pause/resume;
- recheck;
- remove;
- storage move;
- priorità file;
- tracker e peer;
- limiti;
- eventi.

Aggiunte consigliate:

- badge backend attivo;
- stato sincronizzazione e ultima riconciliazione;
- bytes verificati;
- piece runs;
- peer attivi e seed connessi;
- fonti tracker/DHT/PEX;
- ultima verifica;
- capability disponibili;
- warning per funzioni specifiche di libtorrent.

Nuove API candidate:

```text
GET  /api/torrent-backend
POST /api/torrent-backend
POST /api/torrent-backend/test
GET  /api/torrents/{hash}/pieces
GET  /api/torrents/{hash}/pieces/runs
POST /api/torrents/{hash}/pieces/{index}/priority
POST /api/torrents/{hash}/pieces/{index}/verify
GET  /api/torrents/{hash}/diagnostics
GET  /api/torrents/{hash}/sources
GET  /api/torrents/{hash}/storage
GET  /api/torrent-migrations
POST /api/torrent-migrations
POST /api/torrent-migrations/{id}/cancel
```

Le API comuni devono conservare il contratto attuale e restituire errori espliciti quando una capability non è disponibile.

## 14. Configurazione proposta

```json
{
  "torrent_backend": "embedded",
  "anacrolix": {
    "enabled": false,
    "data_dir": "",
    "metadata_dir": "",
    "piece_completion_db": "",
    "listen_port": 6881,
    "listen_port_max": 6891,
    "tcp": true,
    "utp": true,
    "dht": true,
    "pex": true,
    "trackers": true,
    "webseeds": true,
    "upnp": true,
    "piece_hashers": 2,
    "max_unverified_bytes": 67108864,
    "poll_interval_ms": 1000,
    "check_interval_secs": 5
  }
}
```

Impostazioni comuni: root, directory temporanea, slot, stalled timeout, retry, seed policy, blocklist e politica preallocazione.

Impostazioni specifiche: storage, completion store, piece hashers, DHT bootstrap, tracker backoff, dialer/proxy, web seed e profilo cache.

## 15. Piano d'opera

### Fase 0 — compatibilità e baseline

- fissare la versione candidata;
- compilare un programma di prova;
- usare un torrent locale con tracker di test;
- verificare magnet, `.torrent`, storage e recheck;
- verificare restart con dati parziali;
- verificare piece state e callback;
- produrre benchmark iniziali.

**Output:** report di compatibilità e dipendenza approvata.

### Fase 1 — astrazione

- introdurre `TorrentBackend` e capability;
- creare adapter libtorrent;
- aggiornare `AppState` e servizi;
- convertire gradualmente handler e orchestrator;
- mantenere libtorrent come implementazione attiva;
- aggiungere test di regressione.

**Output:** nessuna regressione del backend ufficiale.

### Fase 2 — storage anacrolix

- path maker;
- storage file;
- piece completion persistente;
- manifest metadata;
- test single/multi-file;
- crash recovery;
- cleanup sicuro.

**Output:** dati anacrolix persistenti e verificabili.

### Fase 3 — core backend

- creazione e chiusura client;
- magnet e `.torrent`;
- list/info/files;
- allow/disallow download/upload;
- drop/remove;
- stats;
- tracker/peer;
- recheck;
- priorità file.

**Output:** backend utilizzabile dalla UI.

### Fase 4 — eventi e automazioni

- event bus;
- callback non bloccanti;
- polling di riconciliazione;
- mapping stati;
- metadata readiness;
- stalled e retry;
- move manager;
- completamento e seed policy.

**Output:** parità degli automatismi prioritari.

### Fase 5 — nuove funzioni

- diagnostica pezzi;
- piece state runs;
- download selettivo;
- tracker/DHT/peer diagnostics;
- verifica programmata;
- web seed fallback;
- streaming come funzione sperimentale.

**Output:** valore aggiunto specifico di anacrolix.

### Fase 6 — migrazione e rilascio

- manifest e lock;
- embedded → anacrolix;
- anacrolix → embedded;
- rollback;
- dry-run;
- test con crash;
- attivazione opt-in;
- documentazione e fallback libtorrent.

## 16. Test e criteri di accettazione

### 16.1 Test necessari

- add magnet e `.torrent`;
- metadata ritardato;
- single-file e multi-file;
- file parziali e completi;
- path con caratteri speciali e traversal;
- piece completion SQLite;
- crash durante write;
- restart;
- tracker HTTP/UDP;
- DHT, PEX, TCP e uTP;
- no seed e stalled;
- blocklist;
- spazio insufficiente e filesystem read-only;
- pause/resume e retry;
- move durante write;
- verifica pezzo e verifica completa;
- completion e post-processing una sola volta;
- switch in entrambi i sensi;
- rollback durante importazione;
- molti torrent e limiti RAM.

### 16.2 Criteri di accettazione

L'integrazione è pronta quando:

1. libtorrent supera invariati i test attuali;
2. anacrolix compila e avvia il daemon;
3. la UI Scarico funziona con entrambi i backend;
4. il progresso parziale sopravvive al restart;
5. la verifica trova dati corrotti;
6. la coda Gextto controlla gli slot;
7. uno stalled non blocca indefinitamente gli altri torrent;
8. stop/resume/retry sono idempotenti;
9. gli errori di storage diventano eventi classificati;
10. move e retry non causano scritture nel vecchio path;
11. un completamento viene processato una sola volta;
12. le statistiche e diagnostiche principali sono disponibili;
13. i torrent sconosciuti non vengono gestiti accidentalmente;
14. lo switch conserva hash, path e dati già presenti;
15. il rollback è testato;
16. le nuove funzioni piece-level non degradano il percorso standard;
17. versione, licenza e capability sono documentate.

## 17. Rischi e contromisure

| Rischio | Impatto | Contromisura |
|---|---:|---|
| Resume diverso dal fastresume libtorrent | alto | completion store e recheck controllato |
| Path storage incompatibile | critico | path maker, preflight e test multi-file |
| Cache non equivalente | medio | OS cache iniziale, profili e benchmark |
| Callback troppo frequenti | medio | coda buffered e coalescenza |
| Limiti per-torrent incompleti | medio | capability esplicita e scheduler Gextto |
| Errore di scrittura non gestito | alto | `SetOnWriteChunkError` e worker errori |
| Move durante write | critico | quiesce, lock e state machine |
| Due backend sullo stesso torrent | critico | ownership e preflight obbligatorio |
| Recheck eccessivi | medio | piece completion persistente e check mirati |
| Crescita RAM con molti torrent | medio | limiti peer, scheduler e benchmark |
| API della libreria modificata | medio | versione pin e test di compatibilità |
| Obblighi MPL-2.0 non valutati | alto | revisione licenze prima del packaging |

## 18. Decisioni consigliate

1. Integrare anacrolix come backend nativo opzionale.
2. Introdurre prima l'astrazione `TorrentBackend`.
3. Lasciare a Gextto coda e automatismi.
4. Usare storage file come baseline.
5. Persistire il completamento dei pezzi separatamente dal DB applicativo.
6. Conservare sempre `.torrent` e metadata.
7. Usare callback per bassa latenza e polling per riconciliazione.
8. Non usare `Drop` come pausa.
9. Non promettere fastresume identico a libtorrent.
10. Implementare piece diagnostics come prima funzione nuova.
11. Esporre capability non comuni in modo esplicito.
12. Fissare la versione e aggiornare solo con test e benchmark.
13. Verificare licenza MPL-2.0 prima della distribuzione.
14. Attivare inizialmente in modalità opt-in mantenendo libtorrent come fallback.

## 19. Riferimenti

- Backend attuale: [`../libtorrent.go`](../libtorrent.go)
- Eventi torrent: [`../web_torrent_events.go`](../web_torrent_events.go)
- Worker background: [`../web_background.go`](../web_background.go)
- Configurazione: [`../config.go`](../config.go)
- API Gextto: [`API.md`](API.md)
- Repository ufficiale: <https://github.com/anacrolix/torrent>
- Documentazione Go `v1.61.0`: <https://pkg.go.dev/github.com/anacrolix/torrent@v1.61.0>
- Storage package: <https://pkg.go.dev/github.com/anacrolix/torrent/storage@v1.61.0>
- Esempio HTTP basato sulla libreria: <https://github.com/anacrolix/confluence>

## 20. Conclusione

La soluzione consigliata è:

```text
libtorrent        = backend ufficiale e baseline
anacrolix/torrent = backend Go alternativo e piattaforma per nuove funzioni
Gextto            = proprietario di stato, automazioni e UI
```

`anacrolix/torrent` è tecnicamente adatto a Gextto, ma l'integrazione deve essere progettata come un vero backend con storage, lifecycle, scheduler ed eventi propri. Il vantaggio non sarebbe soltanto avere un secondo motore: sarebbe poter aggiungere controllo per pezzo, diagnostica reale di peer/tracker/DHT, verifica programmata, download selettivo e in futuro streaming, mantenendo libtorrent disponibile come riferimento e fallback.

---

## 21. Aggiornamento — analisi e stato di implementazione

### 21.1 Parere

**Pro**
- Motore nativo Go, **nessun CGo aggiuntivo**, nessun daemon esterno: il deployment resta un singolo binario.
- API ricche per **pezzo/peer/tracker/DHT** → abilita diagnostica e verifica programmata che libtorrent non espone facilmente.
- Callback e `SetOnWriteChunkError` ben adatti a trasformare gli errori storage in eventi applicativi.

**Contro / rischi**
- **Licenza MPL-2.0**: va valutata prima del packaging (il documento lo segnala correttamente); impatta la distribuzione.
- **Storage e resume diversi** dal fastresume libtorrent: il completion store persistente è codice critico (crash/move/rollback).
- **Peso della dipendenza** e del suo albero: impatta build e CI; va isolato.
- È di fatto **un secondo motore completo** da mantenere e testare in parità con libtorrent.
- `Drop` non è pausa, i limiti per-torrent non sono garantiti dinamici, la cache non è equivalente: molte "capability" vanno dichiarate o costruite.

### 21.2 Stato di implementazione

- **Fatto (condiviso)**: il contratto `TorrentBackend` (`Name`, `Capabilities`) è stato introdotto; `internal/qbittorrent` dimostra il pattern di adapter esterno testabile. La stessa astrazione accoglierà `AnacrolixBackend`.
- **Non ancora fatto**: la dipendenza `github.com/anacrolix/torrent` **non è stata aggiunta** di proposito. Inserirla ora impatterebbe `go.mod`/`go.sum` e `go mod tidy` (che considera i file con build tag), quindi build e CI. Va pinata a una versione verificata e isolata dietro **build tag + attivazione opt-in**, dopo la revisione licenza.
- **Motivazione della scelta prudente**: "migliorare ma non rompere". Un secondo motore completo non è implementabile in modo sicuro in un singolo passo insieme al resto.

### 21.3 Migliorie proposte al documento

1. Trattare la **revisione licenza MPL-2.0** come gate bloccante della Fase 0, non come rischio a fondo tabella.
2. Isolare la dipendenza con build tag (`//go:build anacrolix`) finché non è stabile, così il build predefinito e la CI restano invariati.
3. Definire la **capability matrix** `embedded` vs `anacrolix` come contratto verificabile nei test, non come aspirazione.
4. Aggiungere una **matrice di parità funzionale** (add, list, pause, remove, recheck, move, limits, files, peers, trackers, stats) con esito "pieno/parziale/assente" per capability: evita di promettere funzioni non equivalenti.
5. Chiarire che il **completion store** è responsabilità Gextto e che il recheck è preferito a una falsa dichiarazione di completamento.

### 21.4 Prossimi passi consigliati

1. Fase 0: fissare la versione, compilare un programma di prova con tracker locale, produrre un report di compatibilità, **chiarire la licenza**.
2. Aggiungere la dipendenza **dietro build tag** e implementare `AnacrolixBackend` che soddisfa `TorrentBackend`.
3. Storage file + completion store persistente con test di crash; nessuna cache RAM custom all'inizio.
4. Solo dopo parità su add/list/pause/remove/move/recheck, introdurre le funzioni nuove (diagnostica pezzi, download selettivo).
5. Migrazione per ultima, con manifest e rollback, come per qBittorrent.

---

## 22. Implementazione 2026-09 — backend nativo dietro build tag

### 22.1 Cosa è stato implementato

`AnacrolixBackend` esiste ed è selezionabile, ma **solo nei build compilati con
il tag `anacrolix`**: il build predefinito e la CI non compilano la dipendenza,
quindi il percorso libtorrent resta invariato.

| Componente | File | Ruolo |
|---|---|---|
| Backend | `anacrolix_engine.go` (`//go:build anacrolix`) | client unico, storage file, completion store persistente, manifest, eventi |
| Selezione | `torrent_engine_select.go` | `newAnacrolixEngine` registrato via `init()`; errore esplicito senza tag |
| Build | `Makefile`, `scripts/build-daemon.sh` | `make build-anacrolix`, `make test-anacrolix`, `GEXTTO_TAGS` |
| Licenza | `NOTICE` | anacrolix/torrent = MPL-2.0, solo nel build opzionale |

Cosa funziona realmente (verificato da test dietro tag):
- un `*torrent.Client` per processo, listener/DHT/PEX/uTP mappati da
  `cfg.Libtorrent`, storage file per-torrent con `storage.NewFileWithCompletion`;
- **completion store persistente** (`storage.NewDefaultPieceCompletionForDir`,
  SQLite/bolt a seconda del build) e warning se non persistente;
- add magnet e `.torrent` con path per-torrent (senza sovrascrivere
  infohash/metainfo dello spec);
- **resume dopo riavvio** tramite manifest JSON + copie `.torrent` possedute da
  Gextto; i pezzi verificati vengono riletti dal completion store;
- eventi normalizzati (`metadata_received`, `torrent_finished`, `torrent_error`)
  per diff di snapshot; pause/resume idempotenti via
  `Allow/DisallowDataDownload/Upload`; recheck via `VerifyData`; remove con o
  senza file;
- **storage move** sicuro (quiesce → rename/copy → re-add) e coda Gextto
  (`AdjustQueue`) con pause/resume degli slot attivi.

### 22.2 Capability dichiarate (nessun falso successo)

Rispetto alla tabella ottimistica del piano, queste operazioni **restituiscono
`ErrCapabilityUnavailable`** perché anacrolix non le applica a runtime come
libtorrent:

- limiti per-torrent (`SetLimits`) e limiti globali a runtime;
- flag sequenziale e first/last piece;
- pin e upload mode.

Il **move** è supportato ma parziale (richiede un `.torrent` persistito e
quiesce del torrent), quindi la matrice lo marca `partial`.

La capability matrix in `torrent_engine.go` riflette questa realtà
(`move: partial`, `limits: partial`, `sequential/first_last: none`,
`ip_filter: none`) ed è verificata dai test.

### 22.3 Valutazione pro/contro aggiornata

**Pro confermati**
- Nessun CGo aggiuntivo, nessun daemon esterno: deployment a singolo binario
  quando il tag è attivo.
- Il completion store persistente rende il resume verificabile e testabile in
  locale senza rete.
- La narrow interface `TorrentSession` ha reso l'automazione riusabile: lo
  stesso `MonitorStalled`/`EnforceSeedPolicy` gira sopra anacrolix.
- Isolamento totale dietro build tag: `go test ./...` e il binario ufficiale non
  pagano né dipendenze né rischio.

**Contro confermati / nuovi**
- La dipendenza è molto pesante (albero ampio, `go.mod`/`go.sum` estesi) e ha
  portato l'upgrade di `golang.org/x/net` e `golang.org/x/crypto`: va rivisto in
  CI come ogni bump.
- MPL-2.0: obblighi di distribuzione se il binario con tag viene distribuito
  (il tag è opt-in, il default no).
- Storage/resume diversi dal fastresume libtorrent: il completion store è codice
  critico; il move è ora implementato ma resta un'operazione da trattare con
  cautela (quiesce → move → re-add).
- Le feature "nuove" del piano (piece diagnostics esposti in UI, download
  selettivo, verifica programmata, streaming) **non** sono ancora esposte come
  endpoint: la libreria le supporta, Gextto no.

### 22.4 Gap completati in questo passaggio

1. **Storage move**: quiesce (Disallow download/upload + `Drop`), move con
   rename o copy+delete cross-filesystem, re-add dal `.torrent` persistito,
   ripristino dello stato. Il completion store condiviso evita il re-download.
   Richiede un `.torrent` persistito e rifiuta una destinazione già popolata.
2. **Coda Gextto**: `AdjustQueue` applica gli slot di download attivi con
   pause/resume, toccando solo i torrent messi in pausa dallo scheduler.
3. **Migrazione**: stesso manifest/dry-run di qBittorrent
   (`/api/torrent-migrations`), con verifica che anacrolix sia compilato.
4. **Interfaccia**: tab **Motore torrent** con selettore, mapping percorsi e
   pannello di stato (la matrice mostra anacrolix come backend con tag).

### 22.5 Gap residui

1. **Diagnostica pezzi** (`/pieces`, `/pieces/runs`) e **download selettivo**
   con profili film/serie/pack/fumetti.
2. **IP blocklist**, bootstrap DHT e proxy non mappati da `cfg.Libtorrent`.
3. **Hand-off automatico della migrazione** (il manifest è pronto; il
   trasferimento resta governato con riavvio).

### 22.6 Test

`anacrolix_engine_test.go` (tag `anacrolix`):
- conversioni priorità; capability non supportate;
- verifica dati locali fino al 100%, pause/resume, remove con conservazione file;
- **restart**: il torrent viene ricostruito dal manifest e resta completo senza
  riscaricare;
- **storage move**: il file viene spostato, il torrent resta completo e il
  `SavePath` riporta la nuova destinazione;
- diff eventi metadata/completamento.
