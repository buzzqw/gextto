# Aggiunta di qBittorrent-nox come backend torrent alternativo

## Stato del documento

- **Stato:** proposta tecnica / piano d'opera
- **Backend ufficiale:** libtorrent integrato in Gextto
- **Backend alternativo proposto:** qBittorrent-nox tramite Web API
- **Obiettivo:** permettere di selezionare il motore torrent senza cambiare la UI, il database o la logica automatica di Gextto
- **Vincolo principale:** non eseguire mai contemporaneamente lo stesso torrent con entrambi i backend

---

## 1. Sintesi esecutiva

Gextto deve mantenere libtorrent come backend predefinito e ufficiale. qBittorrent-nox deve essere aggiunto come backend opzionale, attivabile dalla configurazione.

La UI **Scarico**, il database, lo scoring, il post-processing, la gestione degli errori e le automazioni devono continuare a vivere in Gextto. qBittorrent-nox deve fornire il piano di trasferimento: connessioni, pezzi, verifica dei dati, fastresume, cache disco, tracker e peer.

L'architettura consigliata è quindi:

```text
                    Gextto
       UI / API / database / automazioni
                         |
                 TorrentBackend
                  /           \
       libtorrent integrato   qBittorrent-nox
          backend ufficiale       Web API
```

La sostituzione deve essere trasparente a livello di:

- hash e identità del torrent;
- percorso dei dati;
- stato persistito;
- progresso già scaricato;
- UI Gextto;
- automazioni di coda, retry, stop/resume, move e completamento.

Non è invece possibile conservare le connessioni peer attive o lo stato interno della sessione libtorrent durante lo switch. Il passaggio deve quindi essere una migrazione controllata: pausa, esportazione dello stato, arresto del vecchio backend, importazione nel nuovo backend, verifica e ripresa.

## 2. Obiettivi

### 2.1 Obiettivi funzionali

1. Conservare libtorrent come comportamento predefinito.
2. Aggiungere una modalità `qbittorrent` selezionabile senza ricompilare Gextto.
3. Mantenere invariata la UI **Scarico** di Gextto.
4. Permettere all'utente di usare anche la Web UI di qBittorrent-nox.
5. Sincronizzare in Gextto le modifiche effettuate dalla UI qBittorrent.
6. Conservare la gestione automatica di:
   - coda dinamica;
   - torrent stalled;
   - stop/resume;
   - retry e backoff;
   - recheck;
   - errori e file mancanti;
   - spostamento dello storage;
   - completamento e post-processing;
   - seed ratio e seed time;
   - recupero dopo riavvio.
7. Consentire lo switch libtorrent ↔ qBittorrent preservando i medesimi torrent.
8. Evitare perdita di dati, doppio download e doppio post-processing.

### 2.2 Obiettivi non funzionali

- operazioni idempotenti;
- riconciliazione automatica dopo riavvio o perdita di connessione a qBittorrent;
- nessun accesso diretto al database interno di qBittorrent;
- supporto a una versione qBittorrent documentata e testata;
- log espliciti con backend, hash e azione;
- compatibilità con installazione nativa e Docker;
- test di integrazione ripetibili in CI o in ambiente di staging.

### 2.3 Non obiettivi della prima versione

Non sono requisiti per il primo rilascio:

- VPN binding gestito da Gextto;
- replica di ogni parametro interno di libtorrent;
- conservazione delle connessioni peer durante lo switch;
- supporto contemporaneo a più istanze qBittorrent;
- importazione automatica senza autorizzazione di torrent aggiunti manualmente a qBittorrent;
- controllo diretto del database o dei file di stato proprietari di qBittorrent.

## 3. Situazione attuale di Gextto

Gextto non usa libtorrent soltanto per trasferire i pezzi. L'integrazione attuale include una parte significativa della logica applicativa.

### 3.1 Componenti rilevanti

- `libtorrent.go` — sessione, aggiunta torrent, stato, storage, limiti, file, tracker, peer e fastresume;
- `libtorrent_cgo.go` e `native/libtorrent_bridge.cpp` — ponte nativo;
- `appstate.go` — stato applicativo con riferimento diretto a `*LibtorrentClient`;
- `cmd/gexttod/main.go` — costruzione e shutdown del backend;
- `web.go` — stato del server e dipendenze dei gestori;
- `orchestrator.go` — acquisizione e accodamento;
- `web_torrent_events.go` — monitoraggio stalled, metadata, spostamenti, retry, seed policy e completamenti;
- `web_background.go` — consumo degli eventi, recupero dei completamenti persi e post-processing;
- `web_handlers_g1.go`, `web_handlers_g2.go`, `web_handlers_g4.go`, `web_handlers_g6.go`, `web_handlers_g7.go` — API e operazioni torrent;
- `comics.go` — download torrent dei fumetti;
- `docs/API.md` — API pubblica della UI e del daemon.

### 3.2 Accoppiamento da rimuovere

Numerose funzioni ricevono direttamente `*LibtorrentClient`. Prima di aggiungere qBittorrent occorre introdurre un contratto backend e sostituire progressivamente questi riferimenti con un servizio torrent astratto.

Non è sufficiente aggiungere un `if backend == qbittorrent` dentro ogni handler: aumenterebbe il rischio di divergenze e renderebbe impossibile testare la parità tra i due backend.

## 4. Principi architetturali

### 4.1 Gextto è il sistema di controllo

Gextto rimane la fonte di verità per:

- release e metadati;
- associazione torrent ↔ serie/film/fumetto;
- stato applicativo del download;
- coda funzionale e priorità derivanti dallo scoring;
- retry e backoff;
- post-processing;
- rinomina e archiviazione;
- notifiche;
- seed policy applicata al dominio Gextto;
- decisione di rimuovere o conservare un torrent completato.

### 4.2 Il backend è il piano di trasferimento

Il backend deve fornire:

- aggiunta di magnet e file `.torrent`;
- elenco e stato dei torrent;
- progresso, velocità e dimensioni;
- pause/start/resume;
- rimozione con o senza dati;
- verifica dei pezzi;
- spostamento storage;
- limiti di velocità e connessioni;
- priorità file;
- tracker e peer;
- modalità sequenziale e primo/ultimo pezzo, quando disponibile;
- eventi o sincronizzazione incrementale;
- statistiche di sessione, quando disponibili.

### 4.3 Un solo proprietario operativo alla volta

Durante il normale funzionamento deve esserci un solo backend attivo. In particolare è vietato:

- avviare libtorrent e qBittorrent con lo stesso torrent;
- indicare la stessa directory temporanea a due motori contemporaneamente;
- lasciare qBittorrent attivo durante una migrazione verso libtorrent;
- avviare libtorrent prima che qBittorrent abbia terminato la migrazione.

La violazione di questo principio può causare file modificati simultaneamente, stato incoerente, fastresume non valido e perdita di dati.

## 5. Modello `TorrentBackend`

È consigliabile introdurre un'interfaccia principale, eventualmente suddivisa in capability più piccole.

Esempio concettuale:

```go
type TorrentBackend interface {
    Name() string
    Start(ctx context.Context) error
    Shutdown(ctx context.Context) error

    List(ctx context.Context) ([]models.TorrentView, error)
    AddMagnet(ctx context.Context, magnet string, options AddOptions) (string, error)
    AddTorrentFile(ctx context.Context, path string, options AddOptions) (string, error)

    Pause(ctx context.Context, hash string) error
    Resume(ctx context.Context, hash string) error
    Restart(ctx context.Context, hash string) error
    Remove(ctx context.Context, hash string, deleteFiles bool) error
    Recheck(ctx context.Context, hash string) error

    MoveStorage(ctx context.Context, hash, destination string) error
    SetLimits(ctx context.Context, hash string, limits TorrentLimits) error
    SetGlobalLimits(ctx context.Context, limits GlobalLimits) error

    Files(ctx context.Context, hash string) ([]models.FileView, error)
    Peers(ctx context.Context, hash string) ([]models.PeerView, error)
    Trackers(ctx context.Context, hash string) ([]models.TrackerView, error)

    Poll(ctx context.Context) ([]models.TorrentEvent, error)
    SessionStats(ctx context.Context) (map[string]int64, error)
}
```

Il codice reale non deve necessariamente usare un'interfaccia monolitica. È preferibile separare le capability se alcuni comandi non sono disponibili in entrambi i backend:

- `TorrentCore` — add/list/start/stop/remove;
- `TorrentStorage` — files/move/recheck;
- `TorrentLimits` — limiti e seed policy;
- `TorrentNetwork` — tracker/peer/reannounce;
- `TorrentSession` — cache, queue e statistiche;
- `TorrentEvents` — poll e riconciliazione.

Il backend libtorrent deve essere adattato senza alterarne il comportamento. Il backend qBittorrent deve tradurre le operazioni nell'API HTTP di qBittorrent.

## 6. Backend qBittorrent-nox

### 6.1 Modalità di collegamento

qBittorrent-nox deve essere eseguito come processo o container separato. Gextto comunica con la Web API tramite HTTP locale o rete privata.

Configurazione concettuale:

```text
torrent_backend = embedded | qbittorrent
qbittorrent_url = http://127.0.0.1:8080
qbittorrent_username = ...
qbittorrent_password = ...
qbittorrent_poll_interval_ms = 1500
qbittorrent_request_timeout_secs = 15
```

Le credenziali devono essere gestite come segreti e non devono essere incluse nei log o nelle risposte API.

### 6.2 Login e sessione HTTP

Il client qBittorrent deve:

1. effettuare il login alla Web API;
2. conservare il cookie di sessione;
3. rilevare `401`, `403` e risposte di sessione scaduta;
4. riloggarsi una sola volta e ritentare l'operazione;
5. applicare timeout e backoff;
6. non ritentare indiscriminatamente operazioni non idempotenti di aggiunta.

Il client deve negoziare o configurare esplicitamente la versione supportata della Web API. qBittorrent 5.x ha differenze nominali rispetto alle versioni precedenti, per esempio nella terminologia di start/stop rispetto a resume/pause. Queste differenze devono restare dentro l'adapter.

### 6.3 Sincronizzazione dello stato

La sorgente operativa di qBittorrent deve essere letta mediante l'API di sincronizzazione incrementale, in particolare `sync/maindata` nelle versioni che la espongono.

Il ciclo di sincronizzazione deve:

1. conservare il response id precedente;
2. chiedere soltanto le modifiche successive;
3. aggiornare la cache in memoria dei torrent;
4. tradurre gli stati qBittorrent in stati Gextto;
5. generare eventi normalizzati;
6. notificare la UI Gextto via i meccanismi già esistenti;
7. eseguire periodicamente una riconciliazione completa per correggere eventuali delta persi.

Il polling è la fonte di verità. Gli script esterni di qBittorrent possono essere usati come acceleratori per eventi di completamento, ma non devono essere l'unico meccanismo: uno script può fallire, essere disabilitato o non essere eseguito durante un riavvio.

## 7. Stati e mappatura degli eventi

Il backend deve normalizzare gli stati invece di farli propagare direttamente nella UI.

Mappatura concettuale:

| Stato qBittorrent | Stato Gextto | Note |
|---|---|---|
| downloading | downloading | trasferimento attivo |
| stalledDL | stalled | candidato a pausa/retry secondo la policy Gextto |
| queuedDL | queued | in attesa della coda |
| pausedDL | paused | pausa esplicita o policy |
| checkingDL | checking | verifica in corso |
| error | error | errore del backend o del torrent |
| missingFiles | error/missing_files | dati assenti o percorso non valido |
| uploading | seeding | dati completi e upload attivo |
| stalledUP | seeding/stalled | dipende dalla seed policy |
| queuedUP | queued_seed | coda di seed |
| checkingUP | checking | verifica in corso |
| moving | moving | spostamento in corso |
| unknown | unknown | non deve essere trattato come completato |

La mappatura deve essere testata contro la versione qBittorrent supportata. Gli stati qBittorrent possono cambiare durante un singolo ciclo, quindi Gextto non deve basare il post-processing su una sola lettura positiva.

## 8. Coda automatica

### 8.1 Decisione progettuale

La coda applicativa deve rimanere in Gextto. qBittorrent può offrire una propria coda di sicurezza, ma non deve diventare la fonte di verità della coda di Gextto.

Questo evita che un torrent stalled occupi indefinitamente uno slot e impedisca a un torrent sano di partire. Gextto dispone già di logica per monitorare i torrent lenti e deve continuare a decidere quali torrent siano realmente attivi.

### 8.2 Algoritmo consigliato

Per ogni ciclo di gestione:

1. leggere i torrent gestiti;
2. separare download, seed, checking, moving, paused e errori;
3. calcolare gli slot disponibili secondo configurazione e limiti temporanei;
4. escludere torrent stalled oltre la soglia;
5. rispettare pin, priorità manuale, gap-fill e release già in post-processing;
6. ordinare i candidati secondo la coda Gextto;
7. inviare start ai candidati selezionati;
8. inviare stop ai torrent che devono uscire dagli slot;
9. verificare al ciclo successivo che qBittorrent abbia applicato i comandi;
10. registrare ogni decisione nel log.

Le operazioni devono essere idempotenti: non inviare continuamente start a un torrent già attivo o stop a un torrent già fermo.

### 8.3 Interazione con la Web UI qBittorrent

Se l'utente modifica la priorità o lo stato dalla UI qBittorrent, Gextto deve:

- importare la modifica;
- aggiornare lo stato operativo;
- rispettarla se è compatibile con la policy Gextto;
- registrare un conflitto se una policy automatica deve prevalere;
- evitare di riscrivere immediatamente il valore senza spiegazione.

Per la prima versione è preferibile documentare che la coda globale è controllata da Gextto, mentre qBittorrent può modificare i parametri del singolo torrent. In seguito si potrà introdurre una modalità di precedenza manuale con scadenza.

## 9. Stalled, stop/resume e retry

### 9.1 Rilevamento stalled

Gextto deve continuare a misurare:

- bytes ricevuti nell'intervallo;
- velocità download;
- stato qBittorrent;
- presenza di peer/seed utili;
- tempo dall'ultimo progresso;
- numero di tentativi precedenti.

Un torrent non deve essere dichiarato definitivamente fallito soltanto perché è momentaneamente senza peer.

### 9.2 Policy suggerita

1. `downloading` senza progresso per una finestra configurata → `stalled`;
2. rimozione dagli slot attivi;
3. stop qBittorrent, quando necessario per liberare risorse;
4. attesa con backoff;
5. eventuale reannounce/recheck;
6. resume quando il torrent è eleggibile;
7. errore terminale soltanto dopo la stessa policy già adottata da Gextto.

Il comportamento deve rimanere in Gextto, non dipendere esclusivamente dall'opzione qBittorrent “do not count slow torrents”.

### 9.3 Errori

Gli errori devono essere classificati almeno in:

- qBittorrent non raggiungibile;
- autenticazione fallita;
- torrent non trovato;
- file mancanti;
- percorso non accessibile;
- spazio insufficiente;
- errore di verifica;
- errore di spostamento;
- errore di rete temporaneo;
- errore applicativo di post-processing.

Un errore HTTP del backend non deve essere confuso con un errore del torrent. Il primo deve attivare retry del client; il secondo deve passare nella macchina a stati di Gextto.

## 10. Cache e risorse

### 10.1 Cache disco

qBittorrent utilizza una cache disco configurabile attraverso le preferenze applicative. L'adapter deve esporre a Gextto un modello astratto, non copiare direttamente i nomi delle impostazioni qBittorrent.

Impostazioni concettuali da mappare:

- dimensione cache disco;
- durata o scadenza cache;
- uso della cache del sistema operativo;
- coalescenza di letture e scritture;
- modalità di I/O asincrono;
- numero di thread di I/O, se disponibile;
- budget massimo di scritture accodate, se disponibile.

La cache libtorrent di Gextto e quella qBittorrent non sono necessariamente espresse nella stessa unità o hanno gli stessi limiti. L'operazione **Ottimizza** dovrà quindi avere un profilo per backend:

```text
Embedded libtorrent: usa cache_size, max_queued_disk_bytes e parametri libtorrent
qBittorrent: usa le preferenze Web API supportate e lascia automatico ciò che non è esposto
```

Non è corretto promettere la stessa identica disposizione di memoria. È invece possibile mantenere lo stesso obiettivo operativo: cache proporzionata alla RAM, senza crescita incontrollata e senza sovraccaricare il disco.

### 10.2 RAM disk

Se Gextto usa un RAM disk, il percorso deve essere visibile nello stesso modo a Gextto e qBittorrent. In Docker ciò significa montaggio condiviso con la stessa semantica dei percorsi.

La riconciliazione deve verificare:

- esistenza del percorso;
- permessi di qBittorrent;
- spazio disponibile;
- spazio riservato ai torrent già presenti;
- eventuale necessità di spostamento post-seed.

## 11. Spostamento dello storage

Lo spostamento è una delle operazioni più delicate.

### 11.1 Contratto

`MoveStorage` deve restituire uno stato osservabile e non soltanto un errore sincrono:

```text
requested -> moving -> moved
                    \-> retry_wait
                    \-> failed
```

Gextto deve:

1. verificare che la destinazione sia autorizzata;
2. inviare la richiesta qBittorrent;
3. attendere il cambiamento del percorso osservato;
4. verificare che i file siano presenti;
5. non avviare il post-processing prima della conclusione;
6. ritentare con backoff in caso di errore;
7. lasciare il torrent in uno stato recuperabile dopo riavvio.

### 11.2 Percorsi condivisi

Il percorso configurato in Gextto deve essere identico a quello visto da qBittorrent. Non è sufficiente che i due percorsi puntino allo stesso volume: l'API qBittorrent deve ricevere il path corretto dal punto di vista del processo qBittorrent.

La configurazione deve distinguere eventualmente:

```text
Gextto path:       /var/lib/gextto/downloads
qBittorrent path:  /data/downloads
```

Se i path differiscono, deve esistere una tabella di mapping esplicita e testata. In assenza di mapping, Gextto deve rifiutare l'attivazione del backend per evitare spostamenti nel posto sbagliato.

## 12. Completamento e post-processing

Il completamento non deve dipendere da un singolo evento qBittorrent.

### 12.1 Condizioni minime

Gextto può considerare un torrent candidato al completamento solo se:

- il backend riporta completamento;
- il torrent non è in verifica o spostamento;
- i dati richiesti sono presenti;
- il percorso è leggibile;
- la validazione media non rileva un file vuoto o preallocato non scritto;
- il record Gextto non è già stato processato.

### 12.2 Recupero dopo riavvio

All'avvio Gextto deve:

1. leggere tutti i torrent qBittorrent gestiti;
2. confrontarli con il database Gextto;
3. ricostruire gli eventi di completamento mancati;
4. evitare duplicati usando hash e stato persistito;
5. riprendere gli spostamenti pendenti;
6. ripristinare le policy di seed e coda.

Questo mantiene il comportamento già previsto da Gextto quando un evento libtorrent viene perso durante un riavvio.

## 13. Uso contemporaneo delle due UI

### 13.1 UI Gextto

La UI Gextto deve rimanere sempre disponibile e deve mostrare:

- backend attivo;
- stato di connessione al backend;
- ultima sincronizzazione;
- eventuale ritardo di polling;
- stato di migrazione;
- stato di ogni torrent;
- indicatori di conflitto o parametro non disponibile.

Le azioni comuni dalla UI Gextto devono passare sempre dal `TorrentBackend` attivo.

### 13.2 UI qBittorrent

L'utente può aprire la Web UI qBittorrent-nox per operazioni avanzate. Gextto deve poi importare:

- pausa/ripresa;
- limiti di velocità;
- priorità file;
- posizione;
- categoria e tag;
- tracker e peer, quando rilevanti;
- stato di verifica;
- stato di errore;
- progresso e statistiche.

I torrent aggiunti manualmente alla UI qBittorrent devono essere trattati, per default, come **non gestiti**. Devono diventare gestiti da Gextto solo tramite un'azione esplicita di adozione oppure se hanno categoria/tag Gextto valido.

Questo evita che un torrent personale dell'utente venga automaticamente rinominato, spostato o rimosso dal post-processing Gextto.

### 13.3 Regole di precedenza

Le regole consigliate sono:

| Dato | Fonte primaria |
|---|---|
| Release e associazione titolo | Gextto |
| Stato operativo corrente | qBittorrent/libtorrent, importato da Gextto |
| Coda globale | Gextto |
| Peer e tracker | backend attivo |
| Path durante uno spostamento Gextto | Gextto, verificato dal backend |
| Limiti manuali del torrent | ultimo comando valido, con flag di override |
| Post-processing | Gextto |
| Torrent sconosciuti | qBittorrent, non gestiti |

Per evitare conflitti, il database può conservare per ogni proprietà:

- valore osservato;
- valore desiderato;
- origine dell'ultima modifica;
- timestamp;
- eventuale override manuale;
- scadenza dell'override.

## 14. Migrazione trasparente tra backend

### 14.1 Manifest di migrazione

Prima di ogni switch Gextto deve produrre un manifest persistente, per esempio:

```json
{
  "from_backend": "embedded",
  "to_backend": "qbittorrent",
  "created_at": "2026-01-01T12:00:00Z",
  "items": [
    {
      "hash": "...",
      "name": "...",
      "torrent_file": "/var/lib/gextto/torrents/....torrent",
      "magnet": "magnet:?...",
      "save_path": "/var/lib/gextto/incomplete/...",
      "progress": 42.5,
      "paused": false,
      "file_priorities": [1, 4, 1],
      "download_limit": 0,
      "upload_limit": 0,
      "seed_ratio": 1.0,
      "seed_days": 0
    }
  ]
}
```

Il manifest deve essere sufficiente per ripetere o riprendere la migrazione dopo un crash.

### 14.2 Stati della migrazione

```text
idle
  -> preparing
  -> pausing
  -> exporting
  -> stopping_source
  -> starting_target
  -> importing
  -> checking
  -> reconciling
  -> resuming
  -> completed
```

Stati di errore:

```text
preparing_error
source_not_stopped
target_unavailable
import_error
path_mismatch
verification_error
rollback_required
```

### 14.3 Procedura embedded → qBittorrent

1. Bloccare l'aggiunta di nuovi torrent durante lo switch.
2. Sospendere il ciclo di acquisizione.
3. Pausare i torrent attivi.
4. Attendere che non ci siano move/recheck critici in corso.
5. Esportare o assicurarsi di avere il `.torrent` per ogni torrent gestito.
6. Salvare il manifest.
7. Arrestare correttamente libtorrent e salvare fastresume.
8. Avviare qBittorrent-nox.
9. Verificare URL, credenziali e percorsi condivisi.
10. Importare ogni torrent nel proprio `save_path`.
11. Ripristinare priorità, limiti, tag/categoria e stato desiderato.
12. Eseguire o attendere il controllo dei dati esistenti.
13. Confrontare hash, dimensione, path e progresso.
14. Riattivare i torrent che erano attivi.
15. Riattivare acquisizione e post-processing.
16. Eliminare il manifest soltanto dopo una riconciliazione completa.

### 14.4 Procedura qBittorrent → embedded

La procedura è simmetrica:

1. sospendere acquisizione e nuovi comandi;
2. fermare qBittorrent;
3. estrarre il manifest tramite Web API/cache Gextto;
4. assicurarsi che i `.torrent` siano disponibili;
5. creare la sessione libtorrent;
6. aggiungere i torrent con gli stessi percorsi;
7. verificare i dati presenti;
8. applicare limiti e priorità;
9. ripristinare lo stato attivo/pausato;
10. avviare la riconciliazione;
11. riaprire la normale operatività.

### 14.5 Rollback

Se un import fallisce, Gextto non deve tentare di avviare immediatamente il backend sorgente con file ancora controllati dal target.

Il rollback deve essere possibile soltanto dopo:

- arresto del target;
- verifica che nessun processo stia scrivendo i dati;
- ripristino del manifest;
- riavvio del backend sorgente;
- recheck dei torrent interessati.

## 15. Configurazione proposta

La configurazione dovrebbe distinguere il backend attivo dalle impostazioni specifiche.

Esempio concettuale:

```json
{
  "torrent_backend": "embedded",
  "qbittorrent": {
    "enabled": false,
    "url": "http://127.0.0.1:8080",
    "username": "admin",
    "password": "",
    "api_version": "5",
    "poll_interval_ms": 1500,
    "request_timeout_secs": 15,
    "managed_category": "gextto",
    "managed_tag": "gextto",
    "path_mappings": []
  }
}
```

Impostazioni da mantenere comuni:

- numero massimo di download attivi;
- numero massimo di seed attivi;
- seed ratio e seed time;
- soglia stalled;
- retry e backoff;
- limiti globali temporanei;
- percorsi Gextto;
- policy di completamento.

Impostazioni specifiche del backend devono essere mostrate soltanto nella sezione avanzata e non devono alterare il modello comune.

## 16. API Gextto da preservare

Le API esistenti devono mantenere lo stesso contratto indipendentemente dal backend:

- `GET /api/torrents`;
- `GET /api/torrents/{hash}`;
- `POST /api/torrents/add`;
- `POST /api/torrents/{hash}/pause`;
- `POST /api/torrents/{hash}/resume`;
- `POST /api/torrents/{hash}/recheck`;
- `POST /api/torrents/{hash}/storage`;
- `POST /api/torrents/{hash}/limits`;
- `GET /api/torrents/{hash}/files`;
- `POST /api/torrents/{hash}/files/priority`;
- `GET /api/torrents/{hash}/trackers`;
- `POST /api/torrents/{hash}/trackers`;
- `GET /api/torrents/{hash}/peers`;
- `GET /api/torrent-events`;
- `GET /api/torrents/stats`;
- endpoint di export `.torrent` e magnet.

Dove qBittorrent non supporta una funzione esattamente equivalente, Gextto deve:

1. mantenere il contratto API;
2. restituire un errore esplicativo o una capability non disponibile;
3. non simulare un successo se l'operazione non è stata applicata;
4. aggiornare la UI con un messaggio comprensibile.

## 17. Sicurezza

1. qBittorrent-nox deve ascoltare su localhost o su una rete privata.
2. La Web UI non deve essere esposta direttamente su Internet.
3. Se esposta tramite reverse proxy, usare HTTPS.
4. Le credenziali qBittorrent non devono finire nei log.
5. Il token API Gextto e le credenziali qBittorrent devono essere separati.
6. Il processo qBittorrent deve avere accesso soltanto alle directory necessarie.
7. I path ricevuti da qBittorrent devono essere validati contro i root configurati.
8. I torrent sconosciuti non devono essere processati automaticamente.
9. La Web UI qBittorrent deve essere considerata equivalente a un accesso amministrativo al motore torrent.
10. La disconnessione da qBittorrent deve fermare le azioni distruttive, non cancellare dati.

## 18. Piano d'opera

### Fase 0 — specifica e baseline

- congelare il comportamento del backend libtorrent esistente;
- elencare tutte le operazioni oggi usate da Gextto;
- aggiungere test di regressione per coda, stalled, move, completamento e retry;
- definire la versione qBittorrent supportata;
- definire i path standard per installazione nativa e Docker.

**Risultato:** baseline funzionante e criteri misurabili.

### Fase 1 — modello backend

- introdurre `TorrentBackend` e capability interfaces;
- introdurre un servizio di dispatch nel `AppState`;
- convertire i consumer più semplici (`List`, `Stats`, `Pause`, `Resume`, `Remove`);
- mantenere `LibtorrentClient` come adapter principale;
- aggiornare i test API a usare il backend astratto.

**Risultato:** nessuna modifica funzionale per libtorrent.

### Fase 2 — client Web API qBittorrent

- login/logout e gestione cookie;
- timeout, retry e backoff;
- lettura versione/API;
- add/list/info;
- start/stop/remove;
- files, peers, trackers;
- limiti;
- recheck;
- move storage;
- sync incrementale;
- metriche e log.

**Risultato:** adapter qBittorrent testabile senza ancora abilitarlo in produzione.

### Fase 3 — traduzione degli stati

- definire la mappa degli stati;
- normalizzare progress, velocità, errori e path;
- generare `TorrentEvent` Gextto;
- gestire torrent mancanti, duplicati e sconosciuti;
- implementare riconciliazione completa.

**Risultato:** qBittorrent alimenta la stessa macchina a eventi di libtorrent.

### Fase 4 — automazioni

- coda Gextto sopra qBittorrent;
- monitoraggio stalled;
- retry e stop/resume;
- seed policy;
- recheck dopo errore;
- move con stato asincrono e retry;
- completamento e post-processing;
- recupero dopo riavvio.

**Risultato:** parità funzionale sugli automatismi prioritari.

### Fase 5 — configurazione e UI

- selettore backend;
- test connessione qBittorrent;
- indicazione backend attivo;
- stato sincronizzazione;
- capability disponibili;
- gestione credenziali;
- link alla Web UI qBittorrent;
- messaggi di conflitto e migrazione.

**Risultato:** l'utente può usare Gextto senza conoscere il backend attivo.

### Fase 6 — migrazione

- manifest persistente;
- export metadata `.torrent`;
- procedura embedded → qBittorrent;
- procedura qBittorrent → embedded;
- lock e stop acquisizione;
- riconciliazione post-import;
- rollback controllato;
- migrazione dry-run.

**Risultato:** switch ripetibile e recuperabile.

### Fase 7 — cache e ottimizzazione

- mappare cache Gextto su qBittorrent;
- introdurre profili per backend;
- non applicare opzioni libtorrent non supportate;
- aggiungere metriche RAM, cache e I/O;
- test con RAM limitata e molti torrent.

**Risultato:** comportamento operativo prevedibile senza promettere identità interna dei parametri.

### Fase 8 — staging e rilascio

- deploy qBittorrent-nox in ambiente separato;
- test con torrent locali e tracker di test;
- test di riavvio e indisponibilità API;
- migrazione di torrent non importanti;
- attivazione opt-in;
- mantenimento del fallback libtorrent;
- rilascio documentato.

## 19. Strategia di test

### 19.1 Test unitari

- mapping degli stati;
- mapping degli errori HTTP;
- login e rinnovo sessione;
- retry idempotenti;
- parsing dei dati Web API;
- differenze API qBittorrent 4/5, se supportate;
- calcolo slot coda;
- rilevamento stalled;
- manifest di migrazione;
- path mapping;
- conflitti tra desiderato e osservato.

### 19.2 Test con fake backend

Il fake backend deve simulare:

- torrent che progredisce;
- torrent stalled;
- errore temporaneo;
- errore permanente;
- move asincrono;
- evento perso;
- qBittorrent che scompare;
- stato che cambia dalla UI esterna;
- restart con stato persistito.

### 19.3 Test di integrazione qBittorrent reale

In un ambiente containerizzato o di staging:

1. avviare qBittorrent-nox;
2. aggiungere un torrent locale tramite Gextto;
3. verificare il download;
4. cambiare pausa e limiti dalla UI qBittorrent;
5. verificare l'aggiornamento in Gextto;
6. spostare lo storage;
7. forzare un recheck;
8. interrompere qBittorrent;
9. riavviarlo;
10. verificare riconciliazione e resume;
11. completare il torrent;
12. verificare un solo post-processing e una sola notifica.

### 19.4 Test di migrazione

Devono essere coperti almeno questi casi:

- embedded → qBittorrent con torrent completo;
- embedded → qBittorrent con download parziale;
- embedded → qBittorrent con magnet già risolto;
- qBittorrent → embedded con download parziale;
- switch con torrent paused;
- switch con torrent stalled;
- switch durante un move;
- switch durante un recheck;
- crash durante importazione;
- path non disponibile;
- file `.torrent` mancante;
- hash o metadata incompatibili;
- rollback dopo errore.

## 20. Criteri di accettazione

L'integrazione può essere considerata pronta quando:

1. libtorrent continua a superare tutti i test esistenti;
2. qBittorrent può essere attivato senza modificare l'API Gextto;
3. la UI Scarico mostra gli stessi torrent con entrambi i backend;
4. lo stesso hash viene conservato durante lo switch;
5. i dati già scaricati non vengono riscaricati, salvo pezzi realmente mancanti;
6. il progresso viene verificato dopo l'importazione;
7. la coda Gextto esclude gli stalled secondo la propria policy;
8. stop/resume e retry sono idempotenti;
9. gli spostamenti vengono verificati e ritentati;
10. un completamento viene processato una sola volta;
11. un riavvio non perde torrent né eventi applicativi;
12. le modifiche dalla UI qBittorrent arrivano in Gextto;
13. i torrent sconosciuti non vengono gestiti accidentalmente;
14. qBittorrent indisponibile non causa cancellazioni;
15. il rollback è documentato e testato;
16. cache e limiti sono visibili e non riportano valori falsi;
17. i log indicano sempre backend, hash, azione e risultato.

## 21. Rischi e contromisure

| Rischio | Impatto | Contromisura |
|---|---:|---|
| qBittorrent non raggiungibile | alto | retry, stato offline, nessuna cancellazione |
| API incompatibile tra versioni | alto | versione supportata e adapter versionato |
| path diversi tra processi/container | alto | mapping esplicito e preflight obbligatorio |
| torrent controllato da due motori | critico | lock, stop completo e controllo processo |
| evento di completamento perso | alto | polling + riconciliazione all'avvio |
| stalled blocca la coda | medio | scheduler Gextto indipendente |
| qBittorrent UI cambia un valore Gextto | medio | precedenza esplicita e audit dell'origine |
| magnet senza metadata esportabile | medio | persistenza obbligatoria del `.torrent` |
| move interrotto | alto | stato persistente e retry con backoff |
| cache non equivalente | medio | profilo backend e test di risorse |
| torrent sconosciuto processato | alto | categorie/tag e adozione esplicita |
| qBittorrent riporta stato incompleto | medio | controllo file e recheck prima del post-processing |

## 22. Decisioni consigliate

1. **qBittorrent-nox deve essere backend opt-in**, non sostituzione di libtorrent.
2. **Gextto deve possedere la coda e le automazioni**, non delegarle integralmente a qBittorrent.
3. **La Web API qBittorrent deve essere incapsulata** in un adapter isolato.
4. **Il database Gextto resta la fonte di verità applicativa.**
5. **Il progresso operativo viene letto dal backend attivo.**
6. **Ogni torrent gestito deve avere metadata `.torrent` persistenti.**
7. **Lo switch deve essere una macchina a stati persistente**, non un cambio immediato di configurazione.
8. **La UI Scarico deve rimanere invariata** per le funzioni comuni.
9. **La Web UI qBittorrent deve essere supportata come UI avanzata**, con sincronizzazione verso Gextto.
10. **Le capability non equivalenti devono essere esplicite**, mai simulate in silenzio.
11. **La prima versione deve supportare una sola istanza qBittorrent.**
12. **La versione qBittorrent deve essere fissata e testata**, evitando compatibilità indefinita.

## 23. Riferimenti

- Gextto API: [`API.md`](API.md)
- Backend attuale: [`../libtorrent.go`](../libtorrent.go)
- Eventi torrent: [`../web_torrent_events.go`](../web_torrent_events.go)
- Worker completamenti: [`../web_background.go`](../web_background.go)
- Configurazione libtorrent: [`../config.go`](../config.go)
- qBittorrent Web API 5.0: <https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API-(qBittorrent-5.0)>
- qBittorrent Web API storica: <https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API>
- qBittorrent-nox: <https://github.com/qbittorrent/qBittorrent/wiki/installing-qbittorrent>

## 24. Conclusione

L'aggiunta di qBittorrent-nox è tecnicamente sostenibile e compatibile con l'obiettivo di mantenere libtorrent come backend ufficiale.

La soluzione corretta non è duplicare in qBittorrent tutta la logica di Gextto, ma fare in modo che:

```text
Gextto decide e automatizza
qBittorrent trasferisce e persiste il proprio stato torrent
```

In questo modo lo switch può essere trasparente per l'utente a livello di UI, database, hash, progresso e automazioni. L'unico aspetto non preservabile è la sessione peer istantanea: durante il passaggio il torrent deve essere verificato e poi ripreso dal backend scelto.

---

## 25. Aggiornamento — analisi e stato di implementazione

### 25.1 Stato attuale (implementato, non breaking)

Il primo passo del piano è stato realizzato **senza toccare il percorso di trasferimento**: Gextto continua a girare su libtorrent embedded.

- `internal/qbittorrent/` — client autonomo della Web API v2:
  - login con cookie `SID` e **un** re-login automatico su `401/403` (sessione scaduta);
  - negoziazione versione (`app/version`, `app/webapiVersion`);
  - add magnet e `.torrent` (multipart), `torrents/info`, `sync/maindata` incrementale;
  - pause/resume con **fallback** tra endpoint moderni (`stop`/`start`, 5.x) e legacy (`pause`/`resume`, 4.x);
  - delete (con/senza dati), recheck, reannounce, `setLocation`, limiti per-torrent e globali, files, trackers, peers, `transfer/info`;
  - mapping di stato qBittorrent → vocabolario Gextto (`downloading`, `stalled`, `seeding`, `checking_files`, `moving`, `error`, ...).
- Contratto `TorrentBackend` (`Name`, `Capabilities`) e adapter libtorrent.
- Endpoint additivi:
  - `GET /api/torrent-backend` → backend attivo, backend configurato, capability, URL qBittorrent;
  - `POST /api/torrent-backend/test` → login di prova con versioni e conteggio torrent, errori espliciti.
- Impostazioni ammesse in salvataggio: `torrent_backend`, `qbittorrent_url`, `qbittorrent_username`, `qbittorrent_password`, `qbittorrent_category`, `qbittorrent_request_timeout_secs`.
- Test unitari con server qBittorrent finto (login, refresh sessione, add, fallback pause, delete/move, sync, mapping stati).

### 25.2 Migliorie proposte al documento

1. **Capability matrix** esplicita (`embedded` vs `qbittorrent`) come deliverable della Fase 1; oggi è implicita.
2. Chiarire che `torrent_backend` **resta `embedded` per default** e che l'attivazione come motore è un passo separato e governato (il documento lo dice, ma va reso operativo).
3. **Preflight path mapping obbligatorio** prima di qualunque comando: se manca il mapping, rifiutare l'attivazione (il documento lo indica; da rendere un criterio di accettazione bloccante).
4. Aggiungere la voce di stato `moving` all'etichettatura UI di Gextto (oggi non presente) quando l'adapter diventa motore attivo.
5. Definire la **matrice di idempotenza** comando→endpoint→effetto atteso, per evitare start/stop oscillanti durante la coda.
6. Documentare esplicitamente che `pause/resume` e `stop/start` convivono tra versioni e che il fallback è intenzionale (non un doppio comando).

### 25.3 Prossimi passi consigliati

1. Rendere qBittorrent un **motore selezionabile** dietro `torrent_backend`, mantenendo `embedded` come fallback e con `torrents/info` come fonte di verità operativa.
2. Preflight path mapping + test di connessione bloccante all'attivazione.
3. Coda/stalled/retry in Gextto sopra `sync/maindata` (polling come verità, script esterni solo come acceleratori).
4. Completamento + post-processing una sola volta, con recupero all'avvio.
5. Solo per ultima, la **migrazione** tra backend (manifest + macchina a stati), che è la parte a rischio più alto.

---

## 26. Implementazione 2026-09 — motore qBittorrent selezionabile (stato reale)

### 26.1 Cosa è stato implementato

Il piano è stato portato avanti oltre la sola connettività: `torrent_backend=qbittorrent`
ora **installa davvero un motore** per il piano di trasferimento, mentre Gextto
mantiene coda, automatismi e post-processing. Il default resta `embedded`.

| Componente | File | Ruolo |
|---|---|---|
| Contratto motore | `torrent_engine.go` | `TorrentEngine`/`TorrentSession`, adapter `embeddedEngine`, matrice capability, path mapping |
| Adapter qBittorrent | `qbittorrent_engine.go` | polling come verità, eventi normalizzati, comandi idempotenti, traduzione path, capability esplicite |
| Selezione + preflight | `torrent_engine_select.go` | attivazione da `torrent_backend`, preflight bloccante sui path |
| Client Web API | `internal/qbittorrent/qbittorrent.go` | login/sessione, add, file priority, tracker, share limits, categorie/tag, preferenze, limiti |
| Endpoint | `web_handlers_torrent_backend.go`, `web_router.go` | stato, matrice, test, preflight, attivazione |

Cambiamenti non-breaking:
- `embeddedEngine` **incorpora** `*LibtorrentClient`: nessun metodo è stato riscritto, il comportamento è identico per metodo promosso.
- Gli automatismi (`MonitorStalled`, `MonitorMetadata`, `RetryStorageMoves`,
  `ReconcileRamdisk`, `EnforceSeedPolicy`, `RemoveSeededCompleted`,
  `HandleTorrentEvent`, detach) ora accettano la **narrow interface**
  `TorrentSession`: il motore è sostituibile senza duplicare la logica.
- Il worker usa `state.activeEngine()` e lo ri-risolve ad ogni tick (con mutex):
  il backend è sostituibile a runtime senza che handler e automazioni divergano.
  Gli hook libtorrent-only (`PromoteMetadata`, `EnsureAutoManaged`,
  `EnforceDeferredOptions`, `recentlyRechecked`) sono dietro un'interfaccia
  opzionale e vengono saltati dagli altri backend; `AdjustQueue` è invece nel
  contratto comune e ogni backend applica la propria coda.
- Gli handler HTTP generici (lista, add, pause/resume, remove, recheck, move,
  files/peers/trackers, limiti, priorità, pin, sequential) passano dal motore
  attivo. Gli endpoint libtorrent-only (`apply_settings`, `optimize_settings`,
  `upload-mode`, `share-mode`, `flags`, `scrape`, `dht-announce`, `ipfilter`,
  session-stats, `super-seeding`) rispondono **409 capability unavailable**
  quando il backend non è embedded.

### 26.2 Matrice di capability (fonte unica in `torrent_engine.go`)

| Capability | embedded | qbittorrent | anacrolix |
|---|---|---|---|
| add/list/pause/resume/remove/recheck | full | full | full |
| files/peers/trackers/events/stats | full | full | full |
| move | full | full | partial |
| limits | full | full | partial |
| sequential / first_last | full | full | none |
| seed_policy | full | partial | full |
| sync incrementale | none | full | none |
| ramdisk / fastresume | full | none | none |
| piece diagnostics | partial | none | full |
| ip_filter / session_stats | full | partial | none |

La matrice è verificata da `TestCapabilityParityIsComplete` ed esposta in
`GET /api/torrent-backend`.

### 26.3 Migliorie rispetto al documento originale

1. La **matrice di capability** è ora un artefatto verificabile, non implicita.
2. Il **preflight path mapping è bloccante**: con mapping espliciti ogni path
   richiesto deve essere coperto; senza mapping i path sono assunti condivisi ma
   devono esistere.
3. Le operazioni non equivalenti **non vengono simulate**: errore esplicito
   `ErrCapabilityUnavailable`.
4. Il fallback è automatico e sicuro: attivazione rifiutata ⇒ si resta su
   libtorrent con un warning; qBittorrent non raggiungibile ⇒ cache stale, mai
   cancellazioni.
5. `pause/resume` e `stop/start` convivono tra versioni con fallback intenzionale.
6. Aggiunta l'etichettatura UI `moving` (`ui/app/src/lib.rs`).

### 26.4 Gap completati in questo passaggio

- **Un solo motore attivo alla volta (critico)**: con `torrent_backend != embedded`
  `NewLibtorrentClient` non crea alcuna sessione libtorrent e non ripristina
  fastresume (`alternativeBackendActive`). Elimina la possibilità che due motori
  scrivano sugli stessi file.
- **Cambio di backend governato**: `POST /api/torrent-backend` valida
  (preflight) e risponde `restart_required`; non installa mai un secondo motore a
  runtime. Il nuovo motore entra in funzione al riavvio.
- **Coda Gextto sopra qBittorrent**: `AdjustQueue` è nel contratto comune;
  l'adapter qBittorrent impone gli slot di download attivi con pause/start,
  disattiva la coda interna di qBittorrent (`queueing_enabled=false`) e tocca solo
  i torrent che ha messo in pausa lui (nessun conflitto con la pausa utente).
  Anche anacrolix implementa la coda.
- **Persistenza `.torrent`**: l'adapter esporta e conserva in `StateDir` il
  `.torrent` appena i metadati sono disponibili (endpoint `torrents/export`),
  rendendo esportazione e migrazione indipendenti dalla ritenzione di qBittorrent.
- **Profilo Ottimizza per qBittorrent**: `POST /api/torrents/optimize_settings`
  applica ora un profilo cache disco MiB proporzionato alla RAM
  (`disk_cache`, `disk_cache_ttl`, `use_os_cache`) invece di un errore.
- **Migrazione**: `GET /api/torrent-migrations`, `POST /api/torrent-migrations/plan`
  (dry-run + manifest persistente `torrent-migration.json`) e
  `POST /api/torrent-migrations/cancel`. All'avvio, se il manifest ha come target
  il backend attivo, i torrent vengono **reimportati automaticamente** dal
  `.torrent`/magnet (idempotente, il manifest viene marcato completato). Il
  passaggio dei file tra namespace diversi resta governato con riavvio, ma non
  richiede più di ri-aggiungere a mano i torrent.
- **Selettivo e diagnostica**: `POST /api/torrents/{hash}/selective` applica i
  profili `all`/`video`/`skip_extras` (priorità file), `GET .../pieces` espone i
  run di pezzi sui backend che li supportano.
- **Interfaccia**: nuova tab **Motore torrent** con selettore backend,
  credenziali qBittorrent (password write-only), path mapping, pannello di stato
  con matrice capability e pulsanti Test connessione / Verifica prerequisiti.

### 26.5 Gap residui

- **Rollback automatico della migrazione**: l'import all'avvio è idempotente e
  il manifest è riproducibile, ma il rollback verso il backend di origine resta
  un'operazione governata (è la parte a rischio più alto, §14.5).
- **`optimize_settings` su anacrolix**: non applicabile (nessuna cache libtorrent).
- **Path physical validation di qBittorrent**: Gextto verifica il path riportato
  e ritenta; non può leggere il filesystem del processo remoto.

### 26.6 Interfaccia (Fase 5)

La tab **Motore torrent** (`ui/app/src/lib.rs`) permette di configurare e usare i
backend senza toccare i file di configurazione:
- selettore `torrent_backend` (embedded / qbittorrent / anacrolix);
- URL, utente, password, categoria, tag, timeout, polling, path mapping;
- pannello con backend attivo/configurato, matrice capability e stato;
- pulsanti **Test connessione** (`/api/torrent-backend/test`) e **Verifica
  prerequisiti** (`/api/torrent-backend/preflight`).

`GET /api/config` espone le nuove chiavi (la password solo come
`qbittorrent_password_configured`), e `scripts/check-ui-settings-index.sh`
verifica che ogni campo sia nell'indice di ricerca.

### 26.7 Test

- `internal/qbittorrent`: file priority, tracker, share limits, force-start,
  preferenze, limiti, toggle sequenziale idempotente, export `.torrent`.
- `qbittorrent_engine_test.go`: mapping/stati, pause/resume idempotenti, move +
  evento `storage_moved`, eventi metadata/completamento, offline con cache
  conservata, capability non supportate, files/peers/trackers, add con path
  tradotto e categorie, scoperta hash su `.torrent`, persistenza `.torrent`,
  scheduler di coda idempotente, profilo Ottimizza.
- `torrent_engine_test.go`: parsing/validazione/traduzione path, preflight,
  matrice, selezione backend, soppressione della sessione libtorrent con backend
  alternativo, e un test handler end-to-end con motore finto in `AppState`.
- `torrent_migration_test.go`: manifest vuoto, persistenza/caricamento,
  rifiuto di target incompleti, elenco dei torrent gestiti.
