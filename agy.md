# Confronto Approfondito: FlexGet vs Gextto (e gx-torrent)
## Studio Architetturale, Analisi delle Differenze e Spunti di Perfezionamento

> [!NOTE]
> Documento di analisi comparativa tra **FlexGet** (ecosistema Python di automazione multimediale basato su pipeline/task) e **Gextto** (ecosistema Go integrato per l'acquisizione e la gestione della libreria multimediale, con il proprio motore BitTorrent nativo **gx-core** / **gx-torrent** ed eD2k **crossnet**).
> Conformemente alle linee guida del repository, nessun nome reale di film, serie televisiva o episodio è citato in questo documento: tutti gli esempi utilizzano titoli fittizi (es. *Series A*, *Movie 1*).

---

## 1. Panoramica e Visione dei Due Progetti

### FlexGet
**FlexGet** (avviato nel 2008 in ambiente Python) è storicamente definito come il "coltellino svizzero" dell'automazione dei contenuti. Non è un demone multimediale integrato né un client BitTorrent: è un **framework generico di pipeline di dati** (Data Processing Pipeline).
- **Paradigma**: orientato ai task eseguiti a intervalli o da cron/demone (`config.yml`). Ogni task acquisisce elementi grezzi (*Entry*), li fa passare attraverso una catena modulare di plugin (*Input -> Metainfo -> Filter -> Download -> Modify -> Output*) e termina l'esecuzione.
- **Rapporto con il download**: non possiede un proprio motore di trasferimento BitTorrent né gestisce librerie su disco in modo nativo; invia magnet e torrent a client esterni come Transmission, Deluge, qBittorrent o rTorrent via API/RPC.
- **Pubblico di riferimento**: utenti avanzati e sviluppatori che amano comporre flussi personalizzati scrivendo configurazioni YAML dettagliate e script, spesso per aggregare sorgenti non convenzionali (RSS, feed Atom, pagine HTML scraped, liste CSV).

### Gextto e gx-torrent
**Gextto** è una piattaforma integrata, autosufficiente e opinata, scritta in **Go puro** (`CGO_ENABLED=0`), specificamente progettata per l'acquisizione autonoma, la deduplicazione, la verifica di qualità e l'organizzazione durevole di serie TV, film e fumetti.
- **Paradigma**: demone a esecuzione continua, reattivo e altamente concorrente. Include un orchestratore continuo (`orchestrator.go`), una pipeline di valutazione candidati (`cycle_candidate.go`), deduplicazione multi-fonte (`release_dedup.go`), corsie concorrenti con circuit breaker per indexer (`indexer_lanes.go`) e database SQLite d'archivio indicizzato con FTS5.
- **Rapporto con il download**: Gextto **integra e supervisiona il proprio motore BitTorrent nativo** (`gx-torrent`, basato su `gx-core`), scritto interamente nel repository in Go puro (zero librerie esterne C/C++). Inoltre gestisce in modo esclusivo il merging multi-rete con la rete eD2k (`crossnet.go` via aMule).
- **gx-torrent standalone**: il binario `gx-torrent` può operare sia supervisionato da Gextto (`-mode=managed`), sia come client torrent autonomo completo (`-mode=standalone`), dotato di interfaccia web moderna, scheduler di banda, gestione RSS, pagina *Wanted* con integrazione TVmaze e API compatibile qBittorrent v2.
- **Pubblico di riferimento**: appassionati e home-server administrator che cercano un sistema robusto, ad altissime prestazioni, capace di gestire nativamente le complessità dei rilasci italiani e internazionali (BDMux, DLMux, SubIta, tracce multiple, repack), con post-processing lossless avanzato (pulizia tracce audio packet-by-packet, estrazione RAR, hardlink).

---

## 2. Tabella di Confronto Sinottica

| Dimensione | FlexGet | Gextto + gx-torrent |
| :--- | :--- | :--- |
| **Linguaggio e Runtime** | Python 3 (interprete, GIL, dipendenze virtualenv/pip) | Go puro compilato staticamente (`CGO_ENABLED=0`), zero dipendenze |
| **Modello di Esecuzione** | Task sincroni/schedulati a pipeline lineare (*Input -> Filter -> Output*) | Demone reattivo asincrono, goroutine parallele, SSE in tempo reale |
| **Motore Torrent** | Assente. Delega esterna a client terzi (o `libtorrent` wrapper opzionale) | **gx-core proprietario** integrato: BitTorrent v1/v2 (BEP 52), DHT, uTP, LSD, BEP 55, super-seeding |
| **Reti Supportate** | Solo BitTorrent (e NZB via SABnzbd) | **BitTorrent + eD2k** con swarm merging simultaneo (`crossnet.go`) |
| **Configurazione** | File YAML dichiarativo complesso (`config.yml`), Jinja2 templating | UI Web interattiva (SSR + HTMX), API REST, zero YAML per l'utente, tuning guidato con sonde hardware |
| **Interfaccia Utente** | Web UI secondaria / basica; focalizzato sulla CLI (`flexget execute`, log) | Web UI moderna, fluida (SSE), reattiva, multilingua (it, en, de, fr, es, pl), TUI da terminale |
| **Parsing Rilasci** | Plugin interno a regex o wrapper `guessit` esterno | Parser nativo (`parser.go`) ad alte prestazioni con token caching, leet-folding, roman numerals, mux italiani |
| **Verifica Qualità Post-Download** | Assente a livello di stream (si fida del nome del file o del `.torrent`) | **`mediainfo_gate.go`**: analisi `ffprobe` reale (codec, risoluzione, stream, HDR) prima di sostituire i file |
| **Post-Processing Audio/Video** | Ridenominazione e spostamento file; script utente esterni | **`audio_cleanup.go`**: rimozione chirurgica tracce audio indesiderate packet-by-packet, verifica MD5 senza ricodifica |
| **Gestione Archivi** | Plugin `decompress` generico (invoca unrar/7z esterni) | Estrazione RAR integrata in Go puro (`rar_extract.go` / `rarstore`) per archivi non compressi |
| **Libreria & Seeding** | Spostamento o copia (a seconda del client di output) | Hardlink prioritari (`hardlink.go`) per mantenere il seeding continuo a costo zero di spazio disco |
| **Manutenzione Database & Disco** | Pulizia manuale o job CLI (`flexget database vacuum`) | **`maintenance_night.go`**: rilevamento idle notturno, vacuum, optimize, integrity check, cleanup progressivo |

---

## 3. Analisi Dettagliata delle Aree Funzionali

### 3.1 Pipeline di Elaborazione vs Ciclo Continuo
- **FlexGet**:
  Ogni esecuzione elabora gli elementi in una sequenza rigida. L'oggetto centrale è l'`Entry`, una mappa chiave-valore arricchita progressivamente. FlexGet implementa il plugin `seen` per non riprocessare lo stesso URL/titolo due volte. Se un elemento fallisce durante l'output, interviene `retry_failed` con backoff esponenziale.
- **Gextto**:
  Non ragiona a "task isolati", ma a **cicli di acquisizione continui** (`orchestrator.go`). Il ciclo raccoglie candidati da feed RSS, scrape web, manager Torznab (Jackett, Prowlarr, indexarr) e dall'archivio locale FTS5 (`archive.go`). I candidati passano attraverso il delay profile (`pending_queue.go`), la quarantena link (`torrent_link_quarantine.go`), la deduplicazione multi-indexer (`release_dedup.go`), la comparazione con i record archiviati e l'eventuale approvazione.
- **Punto di riflessione**:
  L'architettura di Gextto è notevolmente più resiliente per un servizio 24/7 perché possiede un modello a stati esplicito nel database, con transazioni atomiche e recupero automatico post-crash (`torrent_add_intent.go`).

### 3.2 Motore di Trasferimento e BitTorrent Stack
- **FlexGet**:
  È un orchestratore puro. Quando deve scaricare un torrent, si limita a passare il link o il file a qBittorrent o Transmission via HTTP RPC. Se il client esterno è saturo, bloccato o ha problemi di connessione, FlexGet ne subisce passivamente l'effetto.
- **Gextto & gx-torrent**:
  Avere il controllo diretto del motore (`gx-core`) consente ottimizzazioni impossibili per FlexGet:
  1. **Piece Diagnostics e Streaming HTTP**: possibilità di riprodurre il video in streaming durante il download con readahead sequenziale dei pezzi (`cmd/gx-torrent/stream.go`).
  2. **Swarm Merging e Crossnet**: riuso di blocchi da file locali o scaricamento condiviso tra BitTorrent ed eD2k sullo stesso payload (`crossnet.go`).
  3. **Super-Seeding (BEP 16)** e **BitTorrent v2 (BEP 52)** gestiti nativamente.
  4. **Allocazione adattiva della memoria**: cache su misura per HDD o NVMe calcolata sulla RAM libera della macchina.

### 3.3 Intelligence delle Serie TV e Algoritmi di Upgrade
- **FlexGet (`series` plugin)**:
  Il plugin `series` di FlexGet è uno dei suoi componenti più raffinati. Introduce tre concetti cardine:
  1. **`target` + `timeframe`**: se la qualità target (es. `1080p webdl`) compare subito, viene accettata all'istante; se compare una qualità inferiore consentita (es. `720p hdtv`), FlexGet entra in attesa per un tempo prefissato (es. 4 ore). Se entro 4 ore arriva il target, scarica il target; altrimenti si accontenta del 720p.
  2. **`upgrade: yes`**: continua a monitorare e scaricare versioni migliori anche per episodi già archiviati.
  3. **`propers: <duration>`**: accetta release di tipo `PROPER` o `REPACK` solo se rilasciate entro un certo intervallo temporale (es. 3 giorni) dalla release originale, ignorando le dispute di scena tardive.
- **Gextto**:
  Gextto gestisce gli upgrade in modo molto sofisticato con `upgrade_until_score` (`upgrade_until.go`), comparazione tra candidato e titolare in archivio (`decision.go`), e delay profiles (`pending_queue.go`). Inoltre possiede la ricerca mirata dei vuoti di puntate (`collectGapReleases`).
  La differenza principale è che in Gextto il delay profile è orientato alla sorgente/protocollo (attendere che arrivino indexer più affidabili), mentre in FlexGet il ritardo è condizionato matematicamente alla distanza tra la qualità attuale e la qualità target desiderata.

### 3.4 Pre-filtro dei Contenuti e Integrità dello Swarm
- **FlexGet**:
  - `content_filter`: ispeziona la lista interna dei file del `.torrent` (usando `convert_magnet` per risolvere i metadati magnet prima dell'elaborazione) e scarta i torrent contenenti estensioni indesiderate (`.exe`, `.iso`, archivi compressi non desiderati, video sample).
  - `torrent_alive`: interroga i tracker o la rete DHT prima di inviare il torrent al client, verificando che ci siano almeno `min_seeds` attivi. Se lo swarm è morto (0 seeder), il torrent viene rifiutato temporaneamente (`reject_for: 15 minutes`), evitando di appesantire la coda di download con elementi fermi a zero.
  - `torrent_scrub`: sanifica i file `.torrent` eliminando chiavi bencode proprietarie (fast resume di libtorrent, metadati uTorrent/Azureus) per prevenire conflitti nei client.
- **Gextto & gx-torrent**:
  - Gextto possiede `pack_precheck.go` (ispezione file list del torrent per scartare pack senza l'episodio cercato) e `pack_inspector.go` (ispezione all'arrivo dei metadati DHT).
  - Per i torrent ordinari singoli (film o episodi singoli), Gextto avvia il download e rileva l'eventuale stallo a posteriori tramite i timeout di inattività (`stalled_torrent.go`).
  - gx-torrent supervisiona la salute dei tracker (`tracker_health.go`) rimuovendo quelli non responsivi.

### 3.5 Integrazione Liste e Watchlist Esterne
- **FlexGet**:
  Eccelle nell'integrazione con sorgenti esterne grazie a decine di plugin: Trakt (watchlist utente, serie collezionate), IMDb (liste personalizzate e top 250), TMDB, Letterboxd, AniList. L'utente può semplicemente aggiungere un titolo su Trakt o Letterboxd dallo smartphone e FlexGet genera automaticamente i task di ricerca tramite il plugin `discover`.
- **Gextto & gx-torrent**:
  - Gextto è incentrato su un database locale di serie, film e fumetti gestiti tramite la propria UI o API. Dispone di ricerca attiva su indexer Torznab (Jackett, Prowlarr, indexarr) per completare episodi mancanti.
  - gx-torrent standalone integra l'API di TVmaze per popolare le date di messa in onda e la lista reale degli episodi nella schermata *Wanted*. Non dispone tuttavia di una sincronizzazione nativa bidirezionale con Trakt o Letterboxd.

---

## 4. Punti di Forza e Superiorità di Gextto e gx-core

Nel confronto diretto emergono chiaramente le aree in cui **Gextto e gx-torrent surclassano nettamente FlexGet**:

1. **Motore Torrent Proprietario in Go Puro (`gx-core`)**:
   FlexGet non sa scaricare un singolo byte: deve appoggiarsi a un client esterno. gx-core è un motore autonomo, testato con benchmark scientifici contro qBittorrent e Transmission (`docs/measurements.md`), a basso consumo di CPU e RAM, privo di dipendenze CGO.
2. **Supporto Crossnet BitTorrent + eD2k**:
   Nessun software moderno integra BitTorrent ed eD2k contemporaneamente. Gextto accoppia i file tra le due reti, scarica i pezzi mancanti da aMule e li unisce al torrent (`crossnet.go`).
3. **Chirurgia Audio Lossless Packet-by-Packet (`audio_cleanup.go`)**:
   FlexGet non effettua alcuna ispezione dello stream contenitore. Gextto analizza i flussi con `mkvmerge`/`ffmpeg`, estrae le tracce audio non volute e rimuxa il file senza alcuna perdita qualitativa o ricodifica video, con verifica MD5 su ogni pacchetto.
4. **Verifica di Qualità Post-Download con Gate Obbligatorio (`mediainfo_gate.go`)**:
   In FlexGet un titolo falso (es. un 720p rinominato con il tag "2160p HDR" da un uploader scorretto) sovrascrive un file legittimo di qualità inferiore. In Gextto, prima di cancellare la copia d'archivio, `ffprobe` sonda la reale risoluzione, il codec video, l'audio e l'HDR: se i metadati effettivi non confermano il salto di qualità, la nuova release viene rigettata, inserita in blocklist e la copia d'archivio viene preservata.
5. **Esperienza Utente e Accessibilità**:
   FlexGet richiede la modifica manuale di file YAML delicati (dove un errore di indentazione impedisce l'avvio). Gextto offre una Web UI completa in 6 lingue, SSR + HTMX per reattività istantanea, TUI interattiva da terminale, tuning guidato con sonde hardware e report dettagliati di diagnostica.

---

## 5. Cosa Possiamo Prendere, Apprendere e Perfezionare da FlexGet

Nonostante la superiorità architetturale di Gextto nella gestione end-to-end, FlexGet presenta oltre 15 anni di affinamento su casi limite ed euristiche di automazione dei flussi. Da questo studio emergono **7 aree concrete di arricchimento e perfezionamento per Gextto e gx-torrent**:

### 5.1 Timeframe Condizionato al Target Qualitativo (`target` + `timeframe`)
- **Cosa fa FlexGet**:
  Consente di specificare una qualità ideale (*target*) e una finestra temporale (*timeframe*). Se compare il target, il download parte a latenza zero. Se compaiono qualità accettabili ma inferiori al target, entrano in attesa nella finestra temporale; allo scadere, se il target non è apparso, la migliore tra le qualità disponibili viene sbloccata.
- **Come applicarlo a Gextto**:
  Attualmente il `delay_profile` di Gextto ritarda le release in base alla sorgente/protocollo per dare tempo ad altri indexer di pubblicare.
  *Perfezionamento*: Introdurre nella logica di decisione di `cycle_candidate.go` e `pending_queue.go` una condizione di **target match bypass**: se una release soddisfa la risoluzione, sorgente e traccia audio ideale (il target "perfetto"), il delay profile viene azzerato e il download avviato subito. Se invece è una qualità accettabile ma migliorabile (es. 720p WEB-DL quando la serie ammette 1080p), la release viene parcheggiata in `pending` per un periodo massimo (es. 3 o 6 ore). Se durante quel lasso di tempo arriva la release target, quest'ultima vince immediatamente cancellando il pending inferiore; se il timer scade, la release parcheggiata viene rilasciata e scaricata.

### 5.2 Finestra Temporale di Validità per Scene PROPER e REPACK
- **Cosa fa FlexGet**:
  Il parametro `propers: <tempo>` (es. `propers: 14 days`) impedisce che una release marcata `PROPER` o `REPACK` sostituisca un file storico archiviato da mesi o anni. Le "proper war" della scena spesso generano rilasci correttivi solo per minime discrepanze di frame o desync marginali.
- **Come applicarlo a Gextto**:
  In `parser.go` e `decision.go`, `IsProper` e `IsRepack` attribuiscono un punteggio extra a parità di risoluzione/sorgente.
  *Perfezionamento*: Limitare il peso promozionale di Proper/Repack in base all'età dell'elemento già archiviato (`archived_at`): se l'episodio in archivio è stato acquisito entro $N$ giorni (es. 14 giorni), la Proper/Repack è considerata un upgrade legittimo; se l'episodio risiede stabilmente in archivio da più tempo, la Proper/Repack viene considerata equivalente e non innesca la sostituzione automatica del file, evitando churn inutile del disco e cancellazione di file già verificati.

### 5.3 Pre-Flight Swarm Health Check (`torrent_alive` per Gextto e gx-core)
- **Cosa fa FlexGet**:
  Il plugin `torrent_alive` scarta o rinvia i torrent che hanno 0 seeder attivi sui tracker/DHT prima che vengano aggiunti al client.
- **Come applicarlo a Gextto e gx-torrent**:
  Attualmente, quando un candidato viene approvato da Gextto, viene inviato a gx-torrent; se lo swarm è deserto, il torrent rimane in stato di download occupando uno slot di sessione finché non interviene la soglia di stallo.
  *Perfezionamento*:
  - **In gx-core / gx-torrent**: Introdurre un probe rapido sui metadati dello swarm nei primi 15-30 secondi dall'aggiunta. Se dopo l'interrogazione di DHT e tracker attivi i seeder confermati sono 0 e non ci sono peer con pezzi utili, segnalare tempestivamente l'assenza di sorgenti (`swarm_unreachable`/`dead_swarm`).
  - **In Gextto**: Durante la `candidatePass`, se un risultato da feed o Torznab riporta 0 seeder espliciti, assegnargli una priorità inferiore o rimandarlo al ciclo successivo per favorire candidate con seeder attivi confermati, preservando le connessioni per swarm vivaci.

### 5.4 Pre-Download File-Tree Safety Guards (`content_filter` esteso)
- **Cosa fa FlexGet**:
  `content_filter` verifica l'albero file del torrent prima di confermarne l'acquisizione. Se il torrent contiene file con estensioni pericolose (`.exe`, `.bat`, `.scr`, `.com`, `.ps1`), file compressi protetti da password o video di durata ridicola (< 1 minuto / soli sample), il torrent viene rifiutato a priori.
- **Come applicarlo a Gextto**:
  Gextto possiede già `pack_precheck.go` per i season pack e `media_guard.go` per verificare i file al momento dell'importazione.
  *Perfezionamento*: Centralizzare una guardia preventiva non appena i metadati del torrent sono noti (sia da `.torrent` che dopo la risoluzione magnet in gx-core): se l'albero contiene eseguibili, script di shell sospetti, o se non è presente alcun file video di dimensione coerente con la qualità dichiarata (es. una release spacciata per film da 1080p che pesa solo 80 MB o contiene solo un sample), il torrent viene abortito e rimosso prima che vengano scaricati pezzi di payload.

### 5.5 Sincronizzazione Watchlist Esterne (Trakt / Letterboxd / IMDb)
- **Cosa fa FlexGet**:
  Permette all'utente di definire un input su una lista pubblica (es. `trakt_list: watchlist`) e collegarlo al downloader.
- **Come applicarlo a Gextto**:
  Gextto possiede già la pagina *Film* e *Serie* con gestione dei titoli monitorati e il completamento delle puntate mancanti.
  *Perfezionamento*: Introdurre un worker opzionale configurabile da UI (sezione *Ricerche e Indexer* o *Manutenzione*) che interroga a intervalli regolari (es. ogni 6 o 12 ore) un URL di lista pubblica (feed RSS di Letterboxd, endpoint pubblico Trakt o lista IMDb esportabile) e importa automaticamente i titoli nella libreria di Gextto con profilo di qualità predefinito. L'utente ottiene così la comodità di aggiungere un film dalla propria app mobile di tracking preferita e trovarlo scaricato e archiviato su Gextto.

### 5.6 Subtitle Sidecar Fetcher Integrato nel Post-Processing
- **Cosa fa FlexGet**:
  Utilizza il plugin `subliminal` per cercare e scaricare sottotitoli mancanti nelle lingue desiderate subito dopo il download del file video.
- **Come applicarlo a Gextto**:
  In `parser.go` Gextto rileva meticolosamente `HasSubtitle` e `SubtitleLanguages` (es. sottotitoli italiani o inglesi incorporati).
  *Perfezionamento*: Nella fase di importazione (`postprocess.go`), se una serie o un film è impostato per richiedere sottotitoli italiani e il file acquisito ha solo traccia audio originale senza sottotitoli (o ha solo sottotitoli in lingua straniera), consentire l'interrogazione di un provider leggero di sottotitoli (es. OpenSubtitles REST API v2 o SubDL) per scaricare e collocare il file `.ita.srt` accanto al video nella libreria, prima o durante il salvataggio dei sidecar.

### 5.7 Bencode Torrent Scrubbing in gx-core
- **Cosa fa FlexGet**:
  Il plugin `torrent_scrub` ripulisce i metadati del file `.torrent`, rimuovendo chiavi custom e resume data proprietari iniettati da client di upload (es. fast-resume data di rTorrent/libtorrent, chiavi Azureus/Vuze, commenti di spam).
- **Come applicarlo a gx-core**:
  *Perfezionamento*: Quando `gx-torrent` salva o archivia i file `.torrent` nel proprio storage di stato, accertarsi che la riscrittura bencode sanifichi eventuali dizionari proprietari non conformi agli standard BEP. Ciò garantisce che i torrent esportati o conservati tra riavvii mantengano infohash e strutture bencode perfettamente pulite e prive di rigonfiamenti di byte non necessari.

---

## 6. Cosa NON Conviene Prendere da FlexGet (Anti-Pattern da Evitare)

Nel considerare miglioramenti, è fondamentale preservare i punti di forza identitari di Gextto ed evitare gli errori architetturali che hanno reso FlexGet ostico e fragile per molti utenti:

1. **Nessun File di Configurazione YAML / Jinja2**:
   La configurazione YAML di FlexGet è fonte frequente di errori per gli utenti (indentazione errata, tipi non validati a runtime, complessità sintattica). Gextto deve categoricamente preservare il proprio approccio: interfaccia Web chiara, validazione preventiva (`backend_config_validation.go`), opzioni indicizzate e tradotte, tuning guidato privo di attrito.
2. **Nessuna Frammentazione a Centinaia di Plugin Disomogenei**:
   Il modello a plugin generici di FlexGet soffre costantemente di plugin abbandonati o rotti a causa di modifiche ad API esterne di siti terzi. Gextto centralizza le ricerche su standard aperti e robusti come Torznab (Jackett, Prowlarr, indexarr), garantendo isolamento, testabilità e manutenibilità a lungo termine.
3. **Nessuna Cessione del Motore Torrent a Terzi**:
   L'intuizione di avere `gx-core` interno al progetto (in Go puro, senza librerie esterne C/C++) garantisce indipendenza da versioni instabili di client terzi, assicura conformità BEP trasparente e consente funzionalità avanzate (come lo swarm merging locale ed eD2k) che nessun client generico offrirebbe.

---

## 7. Sintesi delle Proposte per `docs/todo.md`

Dall'analisi svolta derivano 5 proposte concrete, pragmatiche e in linea con l'architettura di Gextto, registrate sotto la sezione *Proposals* di `docs/todo.md`:

1. **Target quality timeframe in pending queue**: ritardo selettivo nella coda `pending` per le release inferiori alla qualità target, con bypass e acquisizione immediata (zero ritardo) per i rilasci che soddisfano pienamente la qualità target ideale.
2. **Proper and repack freshness window**: applicazione del bonus qualitativo per Proper/Repack limitata a una finestra temporale (es. 14 giorni dall'acquisizione dell'episodio), evitando sostituzioni automatiche su contenuti d'archivio storici stabilizzati.
3. **Pre-flight swarm health probe**: verifica rapida tramite DHT / tracker scrape dello stato dei seeder prima di accodare candidati con dati di seed incerti o pari a zero, evitando il blocco degli slot su torrent inattivi.
4. **Pre-download file tree safety guard**: estensione del controllo preventivo della lista file a tutti i torrent non appena i metadati sono noti (da `.torrent` o magnet risolti da gx-core), scartando release contenenti eseguibili o prive di stream video validi.
5. **External watchlist sync**: worker opzionale in background per l'importazione periodica di titoli desiderati da watchlist o feed pubblici (es. Trakt, Letterboxd) direttamente nella coda di acquisizione di Gextto.

---

## 8. Specifiche Tecniche di Implementazione e Modelli Go

L'analisi del codice sorgente di FlexGet effettuata dal subagente specializzato ha isolato gli algoritmi e le strutture dati dei quattro moduli chiave. Di seguito vengono definiti i progetti architetturali e i modelli per il porting nativo in Go puro per Gextto e `gx-core`.

### 8.1 `series` Timeframe & Target: Macchina a Stati per Gextto

#### Come opera FlexGet:
FlexGet serializza l'intera `Entry` (titolo, magnet/torrent, metadati) nella tabella SQLite `backlog`.
Ad ogni esecuzione della task, il plugin `backlog` re-inietta tutti i candidati in attesa.
La logica (`flexget/components/series/series.py`) valuta se esiste una release che soddisfa `target`:
- Se sì: accetta immediatamente (`entry.accept('target')`) senza attendere.
- Se no: calcola `expires = first_seen + timeframe`. Se `now < expires`, salva il miglior candidato nel backlog e rimanda l'episodio al ciclo successivo; se `now >= expires`, la restrizione target cade e il miglior candidato del backlog viene promosso e scaricato.

#### Architettura Go per Gextto (`cycle_candidate.go` e `database.go`):
In Gextto la tabella `pending_downloads` memorizza già `(series_id, season, episode, best_magnet, best_quality_score, ready_at, best_title, first_seen_at, timeframe_hours, due_at, status)`.
Quando arriva una release per un episodio in attesa, `QueuePendingScored` (`database.go:3144`) aggiorna già `best_magnet` e `best_quality_score` se la nuova release ha un punteggio più alto, preservando `first_seen_at` e `due_at`.

Il limite attuale di Gextto è che in `cycle_candidate.go:judge()` / `holdForDelay()`:
```go
// Attualmente in cycle_candidate.go:
delayMinutes = cfg.DelayMinutes("series")
if series.Timeframe > 0 {
    delayMinutes = saturatingMul(series.Timeframe, 60)
}
```
la release viene trattenuta **anche se possiede già la massima qualità desiderata**.

*Modifica architetturale proposta*:
```go
// Definizione del target match in cycle_candidate.go
func (p *candidatePass) isTargetQuality(series *SeriesConfig, release *models.Release) bool {
    if series == nil || release == nil {
        return false
    }
    // Se la serie specifica una TargetQuality (es. "1080p webdl ita"),
    // o se la release possiede i requisiti massimi (es. 1080p/2160p + WEB-DL/BluRay + Audio Italiano)
    if series.TargetQuality != "" && release.Quality.MatchesRequirement(series.TargetQuality) {
        return true
    }
    // Euristiche di default per release "top-tier": 1080p+ WEB-DL con audio nativo ita
    return release.Quality.IsIta && 
           (release.Quality.Resolution == "1080p" || release.Quality.Resolution == "2160p") &&
           (release.Quality.Source == "webdl" || release.Quality.Source == "bluray" || release.Quality.Source == "remux")
}
```
In `holdForDelay()`: se `p.isTargetQuality(series, release)` è vero, si imposta `delayMinutes = 0` e si ritorna `held = false`: il download parte all'istante, azzerando le ore di attesa inutile. Se invece è una qualità di riserva (es. 720p HDTV), entra normalmente nella finestra temporale in attesa che compaia il target.

---

### 8.2 `torrent_alive`: Protocollo di Scrape BEP 15 / HTTP / DHT

#### Come opera FlexGet:
In `torrent_alive.py`, FlexGet implementa manualmente i pacchetti binari BEP 15 UDP su socket grezzi e lo scrape HTTP (BEP 48).
**FlexGet non supporta lo scrape DHT**: si affida unicamente a tracker UDP e HTTP.
- **Protocollo UDP (BEP 15)**:
  1. *Connect*: invia pacchetto di 16 byte (Connection ID fisso `0x41727101980` a 64-bit uint big-endian + Action `0` a 32-bit + Transaction ID a 32-bit random). Riceve 16 byte con il Connection ID dinamico.
  2. *Scrape*: invia 36 byte (Connection ID + Action `2` + Transaction ID + 20 byte InfoHash).
  3. *Risposta*: estrae 12 byte per torrent (3 interi a 32-bit: `seeders`, `completed`, `leechers`). Timeout: 5 secondi.
- Se i seeder confermati sono minori di `min_seeds`, memorizza la tupla `(url, title)` con TTL in `remember_rejected` per un'ora (`reject_for: 1 hour`).

#### Architettura Go per Gextto e `gx-core`:
`gx-core` possiede già un'infrastruttura superiore a FlexGet:
- `internal/gxcore/internal/tracker/udptracker`: implementazione Go pura di BEP 15 con retry ed exponential backoff.
- `internal/gxcore/internal/tracker/httptracker`: client HTTP scrape BEP 48.
- `internal/gxcore/torrent/torrent_dhtscrape.go`: **BEP 33 DHT Scrape nativo** (`t.DHTScrape(timeout)`), capace di stimare i seeder tramite la DHT anche quando i tracker sono offline o per magnet sprovvisti di tracker.

*Integrazione in Gextto (`cycle_candidate.go` / `torrent_link_quarantine.go`)*:
Prima di iniziare il download di un candidato con `seeders == 0` o assenti nei metadati Torznab, invocare un helper `ProbeSwarmSeeds(ctx context.Context, hash string, announceList []string) (int, error)` con timeout di 3 secondi. Se i seeder confermati sono 0, aggiungere l'URL/hash a `dead_torrent_links` con quarantena di 1-2 ore, evitando di impegnare slot con torrent fermi.

---

### 8.3 `content_filter`: Ispezione Bencode e Ratio Dimensionale

#### Come opera FlexGet:
`content_filter.py` interroga `Torrent.get_filelist()` che legge la struttura bencode:
- Gestisce `info.name` (single-file) e `info.files` (multi-file), con fallback dei set di caratteri da `encoding` del torrent a `cp1252` e `utf-8`.
- Regole principali:
  - `reject`: se anche un solo file fa match con pattern vietati (`*.exe`, `*.scr`, `*.bat`, `*.wmv`, `*sample*`), rigetta il torrent.
  - `require_mainfile`: calcola `(dimensione_file_più_grande / dimensione_totale_torrent) * 100`. Se inferiore all'85-90%, rigetta il torrent (protegge da finte release contenenti archivi suddivisi in decine di parti o sample gonfiati).

#### Architettura Go per Gextto (`pack_precheck.go` esteso):
Gextto possiede già `torrentFileList(torrentPath string)` in `pack_precheck.go`. Possiamo generalizzarla con una struct sicura:

```go
type TorrentFileEntry struct {
    Path      string
    SizeBytes int64
}

type TorrentInspection struct {
    TotalSize int64
    Files     []TorrentFileEntry
}

func InspectTorrentMeta(torrentPath string) (*TorrentInspection, error) {
    // Decodifica bencode con zeebo/bencode
    // Calcola TotalSize e popola Files
}

func ValidateTorrentContentSafety(insp *TorrentInspection) (bool, string) {
    if insp == nil || len(insp.Files) == 0 {
        return false, "empty torrent payload"
    }
    
    var largestFile int64
    hasMediaFile := false
    
    for _, f := range insp.Files {
        lower := strings.ToLower(f.Path)
        // Blacklist di sicurezza
        if strings.HasSuffix(lower, ".exe") || strings.HasSuffix(lower, ".scr") ||
           strings.HasSuffix(lower, ".bat") || strings.HasSuffix(lower, ".cmd") ||
           strings.HasSuffix(lower, ".iso") || strings.HasSuffix(lower, ".lnk") {
            return false, fmt.Sprintf("malicious extension in payload: %s", f.Path)
        }
        if isVideoExtension(lower) {
            hasMediaFile = true
            if f.SizeBytes > largestFile {
                largestFile = f.SizeBytes
            }
        }
    }
    
    if !hasMediaFile {
        return false, "no valid video stream file found"
    }
    
    // Regola require_mainfile: per film o release singole, il video principale
    // deve pesare almeno il 75% del torrent (salvo pack con più puntate)
    if len(insp.Files) > 1 && !isPackTorrent(insp.Files) {
        ratio := float64(largestFile) / float64(insp.TotalSize)
        if ratio < 0.75 {
            return false, fmt.Sprintf("no dominant main video file (ratio %.2f < 0.75)", ratio)
        }
    }
    return true, ""
}
```

---

### 8.4 `torrent_scrub`: Sanificazione Bencode a Due Livelli

#### Come opera FlexGet:
In `torrent_scrub.py`, FlexGet distingue nettamente due operazioni:
1. **Top-Level Scrubbing (Info-Hash Invariato)**:
   Rimuove dal dizionario root le chiavi estranee:
   - `libtorrent_resume`: stato di fast-resume iniettato da libtorrent (chunk completati, priorità, path locali). Se importato su un altro client può causare crash, fallimento del check dei pezzi o errori I/O.
   - `rtorrent`, `log_callback`, `err_callback`: metadati ruTorrent.
   - `azureus_properties`: metadati Vuze (miniature thumbnail JPEG, categorie).
   - `nodes`, `uTorrent`: chiavi spurie.
   *Poiché `info` non viene toccato, `SHA-1(info)` resta identico al 100% e il torrent mantiene lo stesso sciame.*
2. **Deep Scrubbing (Info-Hash Modificato)**:
   Rimuove campi all'interno dell'info-dict come `info.source` (tag del tracker privato) o `info.md5sum`. Questo altera l'infohash ed è incompatibile con i tracker privati (che rigettano l'announce).

#### Architettura Go per Gextto e `gx-core`:
In `gx-core`, il package `metainfo` decodifica e riserializza già in modo pulito. Tuttavia, quando Gextto opera con client esterno (`qBittorrent-nox`), i `.torrent` salvati su disco possono ereditare resume data sporchi da indexer e tracker.

*Funzione di sanificazione proposta per Gextto*:
```go
// ScrubTorrentBytes rimuove metadati di resume e proprietà spurie
// preservando i byte esatti di 'info' per non alterare l'info-hash.
func ScrubTorrentBytes(raw []byte) ([]byte, error) {
    var rawMap map[string]bencode.RawMessage
    if err := bencode.DecodeBytes(raw, &rawMap); err != nil {
        return nil, err
    }
    
    // Lista delle chiavi top-level dannose o proprietarie
    bannedRootKeys := []string{
        "libtorrent_resume",
        "rtorrent",
        "azureus_properties",
        "log_callback",
        "err_callback",
        "codepage",
    }
    
    modified := false
    for _, key := range bannedRootKeys {
        if _, exists := rawMap[key]; exists {
            delete(rawMap, key)
            modified = true
        }
    }
    
    if !modified {
        return raw, nil
    }
    
    return bencode.EncodeBytes(rawMap)
}
```
Questa funzione garantisce che qualunque file `.torrent` manipolato o passato al backend sia esente da fast-resume inconsistenti, senza mai invalidare la compatibilità di hash con lo sciame.

