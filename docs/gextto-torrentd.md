# Gextto Torrent Daemon (`gextto-torrentd`)

## Documento tecnico di fattibilità e proposta architetturale

**Stato:** proposta indipendente — nessuna implementazione avviata; Gextto non include più il backend Anacrolix
**Obiettivo:** realizzare un client torrent autonomo, utilizzabile da qualunque applicazione o utente tramite API e CLI, con supporto a torrent v1, v2 e ibridi. Gextto deve poterlo pilotare tramite un adapter, senza esserne un requisito o il proprietario del suo modello dati.
**Non-obiettivo:** fondere libtorrent e anacrolix nello stesso swarm o farli scaricare contemporaneamente lo stesso torrent.

---

## 1. Sintesi

Il progetto è **tecnicamente fattibile**, ma è un prodotto autonomo, non una semplice estrazione del codice attuale. La strada consigliata è creare `gextto-torrentd` come processo dedicato, scritto in Go, che usa inizialmente anacrolix come motore BitTorrent e offre API, CLI e un modello dati indipendenti. `gexttod` è uno dei client di tale API, alla pari di una CLI, un'interfaccia web o altri servizi.

Non deve tentare di riscrivere o combinare i due core BitTorrent. Deve invece separare chiaramente:

| Componente | Responsabilità |
|---|---|
| `gextto-torrentd` | client BitTorrent generico: sessione, rete, storage, resume, stato, priorità, seed, comandi e API |
| CLI/UI/automazioni esterne | client generici del daemon, senza dipendere da Gextto |
| `gexttod` | consumer specializzato: librerie media, ricerca release, regole, archivio, rinomina e notifiche |
| Adapter RPC Gextto | traduce `TorrentEngine` nell'API pubblica del daemon |

Questo riduce l'accoppiamento con CGO/libtorrent, isola crash e memoria del motore torrent e rende aggiornabile indipendentemente il piano di trasferimento. Gextto conserva il suo comportamento applicativo senza imporre concetti media al client torrent.

L'obiettivo stabile non è solo la parità con il backend libtorrent usato oggi da Gextto: è l'**unione funzionale di libtorrent e Anacrolix** nelle versioni di riferimento definite dal progetto. Gextto non include più un adapter Anacrolix; l'eventuale uso della libreria upstream in torrentd sarebbe autonomo. Un client autonomo con questa parità richiede un investimento iniziale realistico di **12–24 mesi-persona**, più hardening su swarm reali. Un MVP con operazioni di base è molto più piccolo, ma non può essere presentato come sostituto completo.

---

## 2. Situazione attuale

Gextto dispone della separazione concettuale nel contratto Go `TorrentEngine` (`torrent_engine.go`). Il piano di controllo mantiene in Gextto ricerca, coda, policy di seed, archiviazione e post-processing; i backend attualmente supportati sono libtorrent integrato e qBittorrent-nox. La proposta torrentd resta un progetto autonomo e non descrive un backend incluso in Gextto.

I backend attuali sono:

- **libtorrent integrato**: processo in-process attraverso bridge CGO/C++; è il riferimento funzionale completo;
- **qBittorrent-nox**: processo esterno pilotato tramite Web API;
- **Anacrolix**: backend Go rimosso da Gextto; un eventuale uso in `gextto-torrentd` sarebbe indipendente e fuori processo.

qBittorrent dimostra che un backend remoto è già compatibile con Gextto, comprese verifica di disponibilità e mapping sicuro dei percorsi. Nel repository Gextto, `gextto-torrentd` comparirà come quarto backend, denominato `torrentd`; il daemon resta però installabile e utilizzabile anche senza Gextto.

### Vincolo: un solo proprietario del torrent

Un torrent deve appartenere a **un solo motore alla volta**. Libtorrent e anacrolix non possono condividere handle, cache dei pezzi, socket, resume data, tracker state o scritture sullo stesso percorso.

Una migrazione deve quindi:

1. fermare e salvare lo stato del backend sorgente;
2. verificare che nessun altro processo scriva nella directory;
3. importare metainfo/magnet e percorso nel backend destinazione;
4. eseguire il controllo hash quando il formato resume non è riutilizzabile;
5. rendere il nuovo backend proprietario e solo allora rimuovere il vecchio.

---

## 3. Requisiti funzionali e baseline di compatibilità

La definizione di compatibilità non è limitata alle funzioni oggi invocate da
Gextto. `TorrentEngine` e le API `/api/torrents/*` sono soltanto il primo
catalogo di integrazione. Il requisito di prodotto per `gextto-torrentd` 1.0 è
una **copertura funzionale almeno pari all'unione delle capacità di libtorrent
2.x e della versione anacrolix scelta**.

Non è richiesto replicare ABI C++ o API Go dei due progetti: il daemon espone
una propria API stabile. È però richiesto offrire un comportamento equivalente,
documentato e verificabile per ogni capacità pubblica. Una capacità non ancora
pronta può esistere solo nelle release sperimentali con stato `none`/`partial`;
non può essere omessa o simulata in una release dichiarata completa.

La Fase 0 deve produrre un **catalogo versionato di compatibilità** che elenchi
ogni funzione pubblica delle versioni upstream di riferimento, il test di
conformità corrispondente e lo stato `full`, `partial` o `none`. Il catalogo è
un artefatto del progetto torrentd, non una tabella derivata dalla UI Gextto.

### 3.1 Capacità minime di un client completo

Oltre alle azioni di gestione, la baseline deve includere almeno:

- peer-wire BitTorrent, estensioni BEP standard, metadata exchange e PEX;
- BitTorrent v1, v2/BEP 52 e hybrid, inclusi piece layers, file tree v2 e
  selezione corretta dell'identità v1/v2;
- tracker HTTP/HTTPS/UDP, announce, scrape, tier e retry;
- DHT IPv4 e IPv6, bootstrap, routing table e announce;
- TCP e uTP, IPv4/IPv6, encryption/obfuscation dove prevista dalle librerie;
- LSD, UPnP e NAT-PMP;
- proxy SOCKS/HTTP, binding di interfaccia e IP filter;
- web seed HTTP/HTTPS;
- gestione storage, file priorities, selezione pezzi, recheck, resume e cache;
- limiti banda/connessioni, rate accounting payload/overhead, statistiche e
  alert/eventi;
- code, auto-management, seed ratio/tempo, super seeding, upload/share mode e
  controllo delle priorità;
- diagnostica per pezzo, peer, tracker e sessione quando disponibile in almeno
  uno dei due motori di riferimento.

### 3.2 Gestione torrent

- aggiunta da magnet e metainfo `.torrent` locale, con supporto obbligatorio a:
  - **BitTorrent v1 / BEP 3**, con info-hash SHA-1 e magnet `xt=urn:btih:`;
  - **BitTorrent v2 / BEP 52**, con info-hash SHA-256 e magnet `xt=urn:btmh:1220...`;
  - **torrent ibridi** (v1+v2), con entrambi gli hash e interoperabilità con peer v1 e v2;
- percorso di salvataggio esplicito, ramdisk e mapping storage;
- opzioni di aggiunta: pausa iniziale, sequenziale, seed mode, priorità di coda, primi/ultimi pezzi, stop ai metadata, preallocazione e stop dopo il controllo;
- elenco live con hash, nome, avanzamento, velocità payload e totale, dimensione, peer, seed, tracker, errori e tempi;
- pausa, ripresa, riavvio, rimozione con/senza file, recheck e reannounce;
- spostamento dello storage con evento di completamento affidabile;
- recupero e persistenza dopo riavvio, incluso resume sicuro.

### 3.3 Controllo per torrent e sessione

- limiti globali e per torrent di download/upload;
- limiti di ratio e tempo seed, upload mode, share mode e super seeding;
- connessioni e upload massimi per torrent;
- coda, pin/force-start, auto-management e policy per torrent lenti/stalled;
- download sequenziale, priorità file, priorità primi/ultimi pezzi;
- lettura file e avanzamento per file;
- lettura peer/tracker, modifica tracker e web seed;
- statistiche sessione, uso RAM/disco, DHT e diagnostica;
- IP filter/blocklist, proxy e binding obbligatorio a una specifica interfaccia VPN;
- DHT, PEX, LSD, TCP/uTP, UPnP e NAT-PMP configurabili.

### 3.4 API pubblica e integrazione client

- stream di eventi ordinato: metadata ricevuti, controllato, completato, spostato, errore storage, tracker/peer error, rimosso;
- snapshot consistenti per CLI, UI e automazioni; la frequenza di polling è scelta dal client;
- operazioni idempotenti: ripetere un comando dopo un timeout non deve aggiungere due volte un torrent né cancellare un elemento errato;
- capability matrix esplicita: ogni funzione dichiara `full`, `partial` o `none`, senza simulare successi;
- health check, versione, metriche e log strutturati;
- spegnimento pulito che salva lo stato prima di terminare.

L'API non deve contenere concetti Gextto quali serie, film, fumetti, release,
archivio o rinomina. Deve descrivere esclusivamente entità torrent generiche.
Gextto associa i propri record media agli ID torrent nel suo database, come fa
qualsiasi altro client.

### 3.5 Parità obbligatoria dell'adapter Gextto

Quando `gextto-torrentd` viene selezionato in Gextto come backend `torrentd`,
deve poter eseguire **tutte** le operazioni che Gextto esegue con il backend
libtorrent integrato. Non è accettabile un adapter che presenti una UI con
azioni disabilitate o che restituisca `capability unavailable` per una funzione
che il backend integrato esegue.

L'adapter RPC deve quindi mappare, con semantica equivalente e conferma di esito:

- aggiunta magnet e `.torrent`, tutti i percorsi e tutte le `AddOptions`;
- lista, stato ricco, eventi lifecycle e statistiche di sessione;
- pause, resume, restart, remove con/senza file, recheck e reannounce;
- move/associate storage e verifica del completamento dello spostamento;
- file, peer e tracker; priorità file, tracker e web seed;
- limiti globali e per torrent, connessioni, upload, ratio e giorni di seed;
- gestione coda, pin, torrent stalled, auto-management e diagnosi;
- sequenziale, primi/ultimi pezzi, super seeding, upload mode e share mode;
- IP filter, preferenze/session settings, binding rete, session statistics e
  tutte le informazioni che Gextto mostra nella pagina Scarico e nella pagina
  Configurazione.

La parità riguarda il **risultato osservabile**, non la chiamata interna: per
esempio un comando di libtorrent può essere implementato con più operazioni nel
daemon, purché stato, eventi, persistenza ed errori restituiti a Gextto restino
equivalenti e sicuri.

La matrice capacità di Gextto deve marcare il backend `torrentd` come `full` per
ogni capacità `full` dell'embedded libtorrent prima che sia selezionabile nella
configurazione normale. Durante sviluppo può essere disponibile solo dietro una
flag sperimentale, con la matrice onesta `partial`/`none`.

### 3.6 Identità v1, v2 e hybrid

Il daemon non può usare un solo campo `hash` come identità universale. Oggi
Gextto privilegia l'info-hash v1 quando esiste; è una scelta utile per la
compatibilità storica, ma non identifica un torrent **v2 puro**.

Il protocollo torrentd deve perciò esporre e persistere:

| Campo | Significato |
|---|---|
| `torrent_id` | ID interno stabile e opaco del daemon; non cambia se esistono uno o due info-hash |
| `infohash_v1` | hash SHA-1 esadecimale, 40 caratteri; assente nei torrent v2 puri |
| `infohash_v2` | hash SHA-256 esadecimale, 64 caratteri; assente nei torrent v1 puri |
| `torrent_version` | `v1`, `v2` oppure `hybrid` |
| `magnet` | magnet canonico, che conserva gli `xt` v1 e/o v2 disponibili |

Le richieste RPC devono poter indirizzare un torrent tramite `torrent_id` o uno
dei due info-hash. La risposta deve restituire sempre tutti i valori presenti.
Per un hybrid, `btih` e `btmh` rappresentano lo **stesso** torrent, non due
download distinti.

Gextto deve estendere gradualmente il proprio schema per memorizzare sia
`infohash_v1` sia `infohash_v2`, mantenendo il vecchio hash v1 come chiave di
compatibilità dove già usato. Una migrazione database dedicata è indispensabile
prima di rendere i torrent v2 puri gestibili da tutte le schermate e automazioni.

Il parser magnet deve validare, normalizzare e deduplicare entrambi i formati:

- `xt=urn:btih:<SHA-1>` per v1;
- `xt=urn:btmh:1220<SHA-256>` per v2;
- più parametri `xt` nello stesso magnet per un hybrid;
- `dn`, `tr`, `ws` e altri parametri ammessi, senza perdere tracker/web seed.

### 3.7 Baseline di accettazione

Il daemon non deve diventare backend predefinito finché non supera:

1. catalogo di compatibilità completo: ogni capacità libtorrent/anacrolix di riferimento è `full`, oppure la release resta esplicitamente sperimentale;
2. aggiunta e recupero dopo riavvio di magnet e `.torrent`;
3. aggiunta e recupero dopo riavvio di metainfo v1, v2 e ibridi, nonché magnet `btih`, `btmh` e magnet ibridi;
4. completamento, policy di seed e rimozione senza perdita dati;
5. stop/restart senza duplicare gli eventi di completamento;
6. storage move sicuro e verificabile da qualunque client;
7. blocco effettivo del traffico quando l'interfaccia VPN configurata scompare;
8. recheck corretto di dati parziali e completi;
9. assenza di doppia proprietà durante una migrazione;
10. compatibilità con flussi serie, film, fumetti e Weekly Pack quando il consumer è Gextto.

---

## 4. Soluzioni valutate

### A. Estrarre l'attuale libtorrent in un processo separato

Il daemon ospiterebbe l'attuale bridge CGO/C++ e libtorrent, esponendolo con RPC.

**Vantaggi:** parità iniziale più alta, semantica/resume già consolidati, crash nativo isolato da `gexttod`.

**Svantaggi:** CGO e dipendenza ABI restano presenti; non è un client indipendente da libtorrent; resta il costo di manutenzione del bridge C++ e delle API deprecate.

**Uso consigliato:** possibile fase transitoria, non obiettivo finale se la motivazione è ridurre la dipendenza da libtorrent.

### B. Fork/estensione anacrolix in `gextto-torrentd` — raccomandata

Il daemon usa `github.com/anacrolix/torrent` come libreria di rete e aggiunge i componenti mancanti nel proprio codice.

**Vantaggi:**

- Go puro: build, debug, race detection e distribuzione più semplici;
- controllo diretto su persistenza, policy, scheduler ed eventi;
- nessun vincolo ABI di una libreria C++ di sistema;
- la libreria upstream anacrolix resta una possibile base tecnica indipendente da Gextto.

**Vincolo di prodotto:** anacrolix è una base di implementazione, non il limite
del prodotto. Ogni capacità che libtorrent possiede e anacrolix non espone deve
essere implementata nel daemon, contribuita upstream o mantenuta come patch
versionata prima della dichiarazione di parità completa.

**Svantaggi:**

- la parità con libtorrent non è gratuita: ramdisk/resume, priorità avanzate, seed policy e preferenze sono oggi parziali o assenti;
- occorre validare stabilità, memoria e throughput su swarm difficili;
- un fork esteso aumenta l'onere di seguire gli aggiornamenti upstream.

**Uso consigliato:** soluzione target, con rollout graduale e libtorrent conservato come fallback finché il catalogo di compatibilità non è completo.

### C. Nuovo core BitTorrent proprietario

Implementare direttamente protocollo peer-wire, metainfo, DHT, tracker, uTP, storage, selezione pezzi e resume.

**Valutazione:** non raccomandato. È un progetto pluriennale, ad alto rischio di sicurezza e interoperabilità. Non offre un vantaggio proporzionato rispetto all'estensione di anacrolix.

### D. Daemon multi-backend

`gextto-torrentd` offre una singola API ma può ospitare worker anacrolix e, in futuro, adapter verso libtorrent, qBittorrent o Transmission.

**Valutazione:** utile come architettura, ma solo dopo che anacrolix ha una base solida. Il primo rilascio non deve diventare un framework generico: ogni backend aggiunge semantiche, test e migrazioni.

---

## 5. Architettura proposta

### 5.1 Processi e comunicazione

```text
CLI / UI / automazioni esterne ──┐
                                 ├── API locale o remota ──► gextto-torrentd
gexttod + adapter TorrentEngine ─┘                               │
      │                                                           ├── anacrolix sessione/i
      │ SQLite: release, archivio, automazioni media              ├── storage download
      └───────────────────────────────────────────────────────────┴── DB resume/stato torrentd
```

Il canale predefinito deve essere un **Unix domain socket**, ad esempio `${DATA_DIR}/torrentd.sock`, con permessi `0600` e proprietario dell'utente del servizio. TCP resta opzionale e richiede autenticazione forte e TLS.

HTTP+JSON è sufficiente per il prototipo; gRPC/protobuf è preferibile per una versione stabile grazie a schema, stream ed evoluzione dei messaggi. La scelta va isolata dietro un client Go, in modo che `TorrentEngine` non dipenda dal trasporto.

Il daemon deve poter essere installato, configurato, aggiornato e avviato senza
alcun binario, database o configurazione Gextto. Deve includere una CLI propria
per almeno configurazione, aggiunta/lista/ispezione/rimozione torrent, log,
health e shutdown. L'eventuale UI propria è un client dell'API, non parte del
core del motore.

Il codice deve vivere come modulo e artefatto rilasciabile indipendente. Non
deve importare package, modelli, migrazioni SQLite o file di configurazione di
Gextto. La dipendenza consentita è nel verso opposto: Gextto importa un client
RPC leggero oppure usa il protocollo documentato. Ciò permette a qualsiasi
applicazione di adottare torrentd senza trascinare funzionalità media.

### 5.2 Componenti interni

| Componente | Compito |
|---|---|
| API server | autenticazione locale, validazione richieste, idempotency key, version negotiation |
| Session manager | avvio/arresto sessione, DHT, listener TCP/uTP, NAT, proxy, binding VPN |
| Torrent registry | indice `torrent_id`/info-hash v1/info-hash v2 → torrent, ownership lock, stato normalizzato, capability |
| Storage manager | percorsi sicuri, preallocazione, move monitorato, quota ramdisk, controlli anti path traversal |
| Resume store | metainfo, stato, file priorities, limiti, tracker, timestamp e crash recovery |
| Queue/policy worker | code, limiti e policy torrent generiche configurabili via API; non conosce logiche editoriali/media di Gextto |
| Event journal | sequenza monotona, persistenza breve, replay dopo disconnessione, deduplicazione |
| Metrics/logging | Prometheus opzionale, health, log JSON con hash e request ID |

### 5.3 Proprietà dei dati

| Dato | Proprietario |
|---|---|
| librerie, release, archivi, score, blocklist media, rinomina | `gexttod` / database Gextto; assenti per gli altri consumer |
| metainfo torrent, resume, priorità e stato motore | `gextto-torrentd` |
| percorso scelto per un torrent | richiesto dal client, validato e registrato dal daemon |
| eventi completamento | emessi e identificati durevolmente da torrentd; ogni consumer li può consumare in modo idempotente |

Non va condiviso lo stesso file SQLite in scrittura tra processi. Ogni processo deve avere il proprio database; la sincronizzazione passa solo dall'API.

---

## 6. Contratto RPC iniziale

Il contratto deve riflettere l'interfaccia esistente, non i dettagli anacrolix. Un possibile namespace `v1`:

```text
GET    /v1/health
GET    /v1/version
GET    /v1/capabilities
GET    /v1/torrents
GET    /v1/torrents/{hash}
GET    /v1/torrents/{hash}/files
GET    /v1/torrents/{hash}/peers
GET    /v1/torrents/{hash}/trackers
GET    /v1/events?after=<sequence>

POST   /v1/torrents                         # magnet/metainfo v1-v2-hybrid, save path, AddOptions
POST   /v1/torrents/{hash}/pause
POST   /v1/torrents/{hash}/resume
POST   /v1/torrents/{hash}/restart
POST   /v1/torrents/{hash}/recheck
POST   /v1/torrents/{hash}/reannounce
POST   /v1/torrents/{hash}/move-storage
POST   /v1/torrents/{hash}/files/priorities
POST   /v1/torrents/{hash}/trackers
POST   /v1/torrents/{hash}/web-seeds
POST   /v1/torrents/{hash}/limits
POST   /v1/torrents/{hash}/connection-limits
POST   /v1/torrents/{hash}/flags
DELETE /v1/torrents/{hash}?delete_files=false
POST   /v1/session/limits
POST   /v1/session/settings
POST   /v1/session/shutdown
```

`{id}` accetta `torrent_id`, info-hash v1 o info-hash v2. Una risposta di stato
minima deve distinguere con chiarezza i tre formati:

```json
{
  "torrent_id": "tr_01J…",
  "infohash_v1": "0123456789abcdef0123456789abcdef01234567",
  "infohash_v2": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "torrent_version": "hybrid",
  "name": "Example release",
  "progress": 42.5
}
```

Nei torrent v1 o v2 puri il rispettivo hash assente è `null`; non va sostituito
con una stringa vuota o con un hash inventato. Il client RPC di Gextto converte
questo modello nel `TorrentView` esistente solo come compatibilità transitoria.

Ogni comando mutante deve accettare `request_id` generato da Gextto. Il daemon conserva per un intervallo configurabile l'esito associato all'ID. In caso di timeout, Gextto può ripetere il comando senza aggiunte duplicate o rimozioni incoerenti.

Gli eventi devono avere almeno:

```json
{
  "sequence": 1042,
  "event_id": "uuid",
  "at": "2026-10-02T14:00:00Z",
  "kind": "torrent_finished",
  "hash": "…",
  "name": "…",
  "save_path": "…",
  "message": "…"
}
```

Gextto salva l'ultimo `sequence` confermato e rende idempotente il consumo con `event_id`. Gli eventi non sostituiscono il polling/snapshot: dopo una disconnessione, il client rilegge anche lo stato completo.

---

## 7. Piano di realizzazione

### Fase 0 — decisione e spike (2–4 settimane-persona)

- definire schema API e versione compatibile;
- estrarre test di conformità dal comportamento dell'attuale `TorrentEngine`;
- prototipo anacrolix fuori processo: add magnet/metainfo v1, v2 e hybrid, list, pause/resume, eventi `metadata`/`finished`, stop pulito;
- verifica upstream che la versione anacrolix scelta supporti pienamente BEP 52 prima di fissarla come dipendenza; in caso contrario il requisito v2 blocca il rilascio, non viene degradato silenziosamente;
- benchmark con torrent legali di test;
- scelta esplicita SQLite/BoltDB per resume store e HTTP vs gRPC.

**Output:** prova di fattibilità, non backend selezionabile dagli utenti.

### Fase 1 — MVP eseguibile (6–10 settimane-persona)

- eseguibile `cmd/gextto-torrentd` e systemd unit opzionale;
- socket locale, health, versioning e autenticazione del peer locale;
- magnet e `.torrent`, stato, pause/resume/remove, limiti globali;
- resume store, eventi con sequence e snapshot;
- adapter `torrentdEngine` con capability dichiarate;
- flag sperimentale, nessuna migrazione automatica e nessun cambio di default.

### Fase 2 — parità operativa per automazione (10–16 settimane-persona)

- file priorities, tracker, peer e dettagli torrent;
- recheck, reannounce, move storage e protezioni contro archiviazione durante un move;
- seed ratio/tempo, coda, pin e gestione stalled;
- ramdisk accounting, percorsi sicuri, preallocazione e gestione spazio;
- IP filter, proxy, binding VPN e test di perdita interfaccia;
- metriche, diagnostica e migrazione manuale assistita.

### Fase 3 — parità avanzata e hardening (12–24 settimane-persona)

- web seeds, super seeding, upload/share mode, opzioni sessione avanzate;
- sequenziale e primi/ultimi pezzi affidabili, oppure capability esplicita `none`;
- recupero dopo crash/power loss, database corruption e dischi pieni;
- soak test multi-giorno, swarm con pochi peer e IPv4/IPv6;
- rollout canary, fallback sicuro e documentazione operativa.

Le stime non includono un protocollo BitTorrent proprietario: sono basate sull'uso di anacrolix e vanno rivalutate dopo la Fase 0.

---

## 8. Strategia di migrazione e rollout

1. **Nessuna modifica al default:** libtorrent integrato rimane predefinito.
2. **Opt-in esplicito:** `torrent_backend=torrentd` solo dopo preflight di socket, versione API, directory, spazio, mapping percorsi e binding VPN.
3. **Sessione vuota iniziale:** la prima attivazione non importa automaticamente torrent attivi.
4. **Migrazione uno alla volta:** Gextto presenta un piano, richiede conferma, ferma il precedente proprietario e registra audit log.
5. **Fallback controllato:** se torrentd non risponde, Gextto non avvia automaticamente libtorrent sugli stessi percorsi; sospende le aggiunte e propone recovery/migrazione.
6. **Promozione solo dopo metriche:** il backend può diventare default solo dopo un periodo definito di successo su installazioni volontarie.

---

## 9. Rischi e mitigazioni

| Rischio | Impatto | Mitigazione |
|---|---|---|
| perdita/corruzione resume | recheck lungo o ridownload | journal, scritture atomiche, backup, test power loss |
| evento completato duplicato | doppia archiviazione/rinomina | `event_id`, sequence, consumer idempotente |
| backend sconnesso | UI/stato non affidabile | health esplicito, snapshot versionato, nessuna riconciliazione distruttiva senza snapshot sana |
| due motori sullo stesso path | corruzione file | ownership lock persistente, migrazione seriale, preflight path |
| perdita VPN/IP leak | rischio privacy | binding verificato, fail closed, test dispositivo assente |
| parità incompleta | UI promette funzioni non operative | capability matrix e messaggio chiaro |
| crescita memoria/disco | degrado server | quote, limiti peer, metriche, cache configurabile, test carico |
| vulnerabilità RPC | controllo non autorizzato | socket `0600`, token per TCP, TLS, validazione input e path |
| dipendenza anacrolix | aggiornamenti difficili | upstream tracking, patch minimali, test compatibilità e verifica licenze |

---

## 10. Test e osservabilità

### Test automatici

- test unitari per stato normalizzato, idempotenza, path policy e opzioni;
- test di contratto contro embedded, qBittorrent, anacrolix e torrentd, con fixture v1, v2 e hybrid;
- test di deduplicazione: aggiungere un hybrid prima con `btih`, poi con `btmh`, deve restituire lo stesso `torrent_id` e non creare due download;
- test di migrazione schema Gextto: hash v1 storico, torrent v2 puro e hybrid devono rimanere interrogabili e rimovibili;
- integrazione con tracker locale e swarm controllato;
- test di riavvio senza metadata, download, checking, seeding, moving e rimozione;
- fault injection: socket interrotto, DB bloccato, disco pieno, file mancanti, tracker irraggiungibile e crash;
- test end-to-end delle automazioni Gextto: serie, film, fumetti, archive move, seed e cleanup.

### Metriche minime

- torrent per stato, peer e connessioni;
- payload e overhead download/upload;
- DHT/tracker success/failure, eventi replay e latenza RPC;
- coda disco, spazio libero, resume store e goroutine/memoria;
- tempo di recheck, storage move e shutdown;
- operazioni idempotenti riutilizzate e comandi falliti.

---

## 11. Decisione consigliata

Avviare **solo la Fase 0** come branch/progetto sperimentale. Il suo obiettivo è dimostrare che anacrolix fuori processo mantiene correttamente magnet, metainfo, resume, eventi e sicurezza dei percorsi senza peggiorare i flussi Gextto esistenti.

Se la Fase 0 conferma stabilità e un vantaggio concreto — isolamento del processo, aggiornabilità, assenza CGO o nuove capacità — procedere con il MVP. In caso contrario, l'alternativa più economica resta estrarre libtorrent in un daemon isolato oppure continuare con l'attuale backend in-process aggiornando la libreria di sistema.

La linea guida è: **costruire un piano di trasferimento indipendente e ben contrattualizzato, non riscrivere BitTorrent né fondere due motori**.
