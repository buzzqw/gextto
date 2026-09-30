# Revisione tecnica 1 — Gextto

## 1. Scopo e perimetro

Questo documento raccoglie le anomalie e il debito tecnico emersi durante la
revisione del codice e propone correzioni incrementali senza modificare il
comportamento funzionale già stabilizzato.

La revisione copre:

1. organizzazione del client JavaScript della UI;
2. internazionalizzazione;
3. generazione sicura e manutenibile dell'HTML dinamico;
4. attività asincrone e gestione dei job;
5. concorrenza, race condition e test.

La sicurezza della rete locale e l'autenticazione della UI sono **fuori
perimetro**: Gextto viene utilizzato in una LAN trusted, come deciso per questa
revisione.

## 2. Stato verificato

Al momento della revisione:

- `go test ./...` passa;
- `go vet ./...` passa;
- il client UI è un unico file IIFE, `uiweb/static/gextto-ui.js`, di circa
  256 KiB e oltre 5.000 righe;
- il CSS principale è centralizzato in `uiweb/static/gextto-ui.css`;
- i template server-rendered sono in `uiweb/templates/`;
- la UI usa polling, partial HTML server-rendered, dati JSON e dialoghi creati
  dinamicamente;
- esistono già `safeGo`, gestione dello shutdown dei worker, mutex per alcune
  strutture condivise e test di sicurezza dei worker;
- `scripts/build-fast.sh` è stato corretto per mantenere il numero di build
  corrente invece di ricadere sul default `1000`.

Non è stato rilevato un errore bloccante nei test o nei log operativi recenti.
Le criticità sotto indicate sono principalmente di manutenzione, osservabilità,
coerenza e robustezza futura.

## 3. Priorità degli interventi

| Priorità | Significato |
|---|---|
| P1 | Correzione consigliata prima di aggiungere molte altre funzioni; può generare regressioni o diagnosi difficili. |
| P2 | Debito tecnico importante, ma compatibile con il normale esercizio. |
| P3 | Miglioria di qualità, ergonomia o manutenzione da pianificare. |

Le priorità non indicano la gravità di un incidente di sicurezza, che è fuori
dal perimetro di questo documento.

---

## 4. Client JavaScript monolitico

### 4.1 Situazione attuale

`uiweb/static/gextto-ui.js` contiene nello stesso IIFE:

- bootstrap della shell;
- tema, scala del testo e font;
- traduzioni e `MutationObserver`;
- polling e richieste API;
- dashboard;
- torrent e download HTTP;
- serie, film, archivio e fumetti;
- configurazione;
- manutenzione;
- log;
- dialoghi, tabelle, accessibilità e gestione focus.

Il file non è necessariamente troppo grande per il browser: compresso è molto
più piccolo del valore su disco e viene servito come asset statico. Il problema
principale è la manutenzione. Una modifica a una funzione globale può avere
effetti laterali su pagine non correlate, e diventa difficile stabilire quali
stati siano condivisi intenzionalmente.

### 4.2 Problemi concreti

- stato globale distribuito nello stesso scope;
- funzioni con nomi generici e possibili collisioni future;
- inizializzazione condizionata dalla pagina corrente mescolata a codice comune;
- logica di rendering, rete e gestione eventi spesso nello stesso blocco;
- test concentrati sull'output HTTP, con poca verifica isolata del comportamento
  client;
- ogni nuova funzione aumenta il rischio di regressione in un'altra sezione.

### 4.3 Architettura proposta

Separare il sorgente in moduli con responsabilità esplicite:

```text
uiweb/static/
  gextto-ui.js              # entrypoint e bootstrap minimo
  ui-core.js                # API, polling, escape, storage, notifiche
  ui-dialogs.js             # dialoghi, focus trap, apertura/chiusura
  ui-i18n.js                # cataloghi e interpolazione
  ui-shell.js               # navigazione, tema, dimensione, font, metriche
  ui-tables.js              # tabelle, ordinamento, paginazione
  ui-torrents.js            # sessione torrent e download HTTP
  ui-library.js             # serie, film, mancanti, archivio
  ui-discovery.js           # TMDB, feed, ricerca release
  ui-comics.js              # fumetti e weekly pack
  ui-maintenance.js         # log, backup, scansioni, rename, database
```

Il modulo `ui-core.js` dovrebbe esportare un contratto piccolo e stabile:

- `api()` e `pollRequest()`;
- `esc()` e formattatori;
- `storageGet()` e `storageSet()`;
- `notify()`;
- `openAccessibleDialog()` e `closeAccessibleDialog()`;
- registrazione dei componenti per pagina.

I moduli di pagina non dovrebbero accedere direttamente a variabili private di
altri moduli. Le dipendenze devono essere importate esplicitamente.

### 4.4 Strategia di migrazione

Non conviene riscrivere il file in una sola volta. La sequenza consigliata è:

1. estrarre prima `ui-core.js` senza cambiare il comportamento;
2. estrarre dialoghi e accessibilità;
3. estrarre shell e preferenze locali;
4. estrarre log e manutenzione, che sono relativamente indipendenti;
5. estrarre torrent/download;
6. separare infine libreria, discovery e fumetti;
7. lasciare nell'entrypoint solo bootstrap e registrazione dei moduli.

Per non introdurre un frontend obbligatorio, la prima versione può usare moduli
ES nativi serviti direttamente dal filesystem embedded. Se il numero di richieste
diventasse rilevante, si potrà aggiungere un bundle opzionale in fase di build,
conservando i moduli leggibili come sorgente.

### 4.5 Criteri di accettazione

- nessuna regressione nei percorsi e nelle azioni UI esistenti;
- il caricamento di una pagina senza JavaScript continua a mostrare il contenuto
  server-rendered essenziale;
- nessun modulo modifica variabili private di un altro modulo;
- test statici che verificano che ogni pagina registri il proprio bootstrap una
  sola volta;
- il peso compresso dell'asset non aumenta senza una motivazione misurata;
- focus, Escape, tastiera e dialoghi mantengono il comportamento attuale.

**Priorità proposta: P2.**

---

## 5. Internazionalizzazione

### 5.1 Situazione attuale

La UI nasce in larga parte con stringhe italiane nei template e nel JavaScript.
Per le lingue diverse dall'italiano, il client:

1. carica il catalogo della lingua attiva;
2. carica anche il catalogo inglese come fallback;
3. percorre il DOM con `translateUINode()`;
4. traduce testo, attributi e alcuni attributi JSON;
5. osserva i nodi aggiunti con `MutationObserver` per tradurli dopo il rendering.

Questo consente di mantenere una UI multilingue senza riscrivere subito tutti i
template, ma crea una dipendenza implicita tra il testo italiano, le chiavi dei
cataloghi e il codice JavaScript.

### 5.2 Anomalie e rischi

- una semplice variazione ortografica rompe una traduzione basata sulla stringa
  completa;
- le stringhe dinamiche richiedono regex dedicate e possono restare parzialmente
  in italiano;
- template server-rendered e dialoghi client-side possono usare cataloghi
  diversi;
- `MutationObserver` esamina continuamente nodi aggiunti e testo modificato;
  con pagine molto dinamiche può generare lavoro inutile;
- i fallback sono difficili da controllare automaticamente;
- attributi, titoli, messaggi toast e contenuti delle tabelle possono divergere
  tra le lingue senza che il compilatore segnali il problema.

### 5.3 Architettura target

Usare chiavi stabili invece del testo italiano come identificatore:

```text
dashboard.title
font_picker.title
font_picker.detect
logs.follow
downloads.empty
```

La struttura consigliata è:

- cataloghi YAML/JSON versionati per `it`, `en`, `de`, `fr`, `es`, `pl`;
- una lingua base obbligatoria, preferibilmente inglese o italiano definita
  esplicitamente;
- fallback deterministico: lingua attiva → lingua base → chiave leggibile;
- interpolazione parametrica, per esempio `{{count}} torrent`, invece di regex
  sul testo già renderizzato;
- helper server-side per titoli, label, aria-label, placeholder e messaggi;
- helper client-side per testo generato dopo una risposta API;
- nessuna traduzione globale del DOM come percorso normale.

### 5.4 Migrazione senza big bang

1. introdurre un controllo cataloghi che segnali chiavi mancanti e duplicate;
2. aggiungere chiavi per tutte le nuove funzioni, compreso il selettore font;
3. migrare prima shell, dialoghi e messaggi toast;
4. migrare le pagine una alla volta;
5. mantenere temporaneamente `translateUINode()` solo come compatibilità per le
   stringhe legacy;
6. ridurre progressivamente il catalogo basato su testo;
7. rimuovere il `MutationObserver` quando le pagine usano chiavi direttamente.

### 5.5 Test necessari

- ogni chiave usata dal codice deve esistere nel catalogo base;
- nessuna chiave deve avere una traduzione vuota nelle lingue dichiarate
  complete;
- snapshot o test di rendering per shell, dialogo font, log e torrent in tutte
  le lingue;
- controllo che testo, `title`, `aria-label` e placeholder usino la stessa
  lingua;
- test per interpolazioni numeriche, plurali e valori contenenti HTML.

**Priorità proposta: P1 per il controllo dei cataloghi, P2 per la migrazione
completa.**

---

## 6. HTML dinamico e sicurezza del rendering

### 6.1 Situazione attuale

Il client usa molto `innerHTML` per:

- tabelle;
- paginazione;
- messaggi e toast;
- dialoghi;
- risultati TMDB, release, fumetti e manutenzione;
- partial HTML ricevuti dal server.

La funzione `esc()` viene usata in molti punti e i partial server-rendered sono
generati dai template Go. Questo riduce il rischio attuale, ma l'approccio resta
difficile da verificare perché ogni nuova interpolazione deve ricordarsi di
applicare l'escaping corretto nel contesto corretto.

### 6.2 Problemi da correggere

- `innerHTML` mescola markup e dati;
- un attributo HTML richiede regole di escaping diverse dal testo di un nodo;
- JSON inserito in `data-*` richiede escaping e parsing coerenti;
- le stringhe di errore provenienti da API o provider possono contenere testo non
  previsto;
- l'uso di partial HTML rende più difficile distinguere contenuto trusted e dati
  esterni;
- non esiste un controllo automatico che impedisca l'introduzione di un nuovo
  inserimento non escapato.

### 6.3 Correzione proposta

Adottare una politica a due livelli:

**Nuovo codice:**

- usare `textContent` per il testo;
- usare `setAttribute` solo con valori validati;
- creare nodi con `document.createElement()` e helper riutilizzabili;
- usare `DocumentFragment` per tabelle e liste;
- limitare `innerHTML` a markup statico controllato;
- non inserire mai direttamente valori provenienti da API, database, provider o
  filesystem in una stringa HTML.

**Codice esistente:**

- centralizzare `esc()` in `ui-core.js`;
- aggiungere una funzione `htmlTrusted()` esplicita per i soli frammenti statici;
- annotare i punti che ricevono partial server-rendered;
- sostituire progressivamente le tabelle più esposte, iniziando da release,
  provider, blocklist e risultati di ricerca;
- aggiungere test con payload contenenti `<script>`, virgolette, `&`, newline e
  URL con caratteri speciali.

### 6.4 Criteri di accettazione

- test automatico che verifica che valori ostili siano visualizzati come testo;
- nessun dato di provider o database deve diventare un nodo HTML eseguibile;
- le funzioni che accettano markup devono avere un nome o un tipo che ne renda
  evidente la natura trusted;
- il rendering non deve perdere sort, focus, paginazione o accessibilità;
- l'escaping deve essere applicato nel contesto corretto, non con una sola
  sostituzione generica usata ovunque.

**Priorità proposta: P1 per i nuovi punti di codice, P2 per la migrazione dello
storico.**

---

## 7. Attività asincrone e gestione dei job

### 7.1 Situazione attuale

Il programma usa correttamente goroutine per non bloccare il server e possiede
già alcune protezioni:

- `safeGo`/`safeGoLoop` per worker di lunga durata;
- `bgStop`, `bgCancel` e `WaitGroup` per lo shutdown;
- mutex dedicato per il progresso della rinomina;
- polling UI per alcune attività.

Il modello non è però uniforme. Alcune operazioni avviano una goroutine e
rispondono immediatamente, mentre altre espongono uno stato globale o un
progresso specifico. Il client deve quindi dedurre se un'operazione sia terminata
guardando log, toast o endpoint diversi.

### 7.2 Problemi concreti

- risposta HTTP positiva prima dell'esito reale dell'operazione;
- stato globale non sufficiente per due operazioni dello stesso tipo;
- rischio di doppio avvio se l'utente preme due volte o ricarica la pagina;
- errori tardivi visibili solo nei log;
- cancellazione non uniforme;
- retry e idempotenza non sempre rappresentati nella UI;
- difficoltà nel correlare un log all'azione che lo ha generato;
- test più complessi perché il completamento dipende da sleep e polling.

### 7.3 Job manager proposto

Introdurre un modello comune:

```go
type JobState string

const (
    JobQueued   JobState = "queued"
    JobRunning  JobState = "running"
    JobSucceeded JobState = "succeeded"
    JobFailed   JobState = "failed"
    JobCanceled JobState = "canceled"
)

type Job struct {
    ID          string
    Kind        string
    State       JobState
    Progress    float64
    Message     string
    Error       string
    CreatedAt   time.Time
    StartedAt   *time.Time
    FinishedAt  *time.Time
    Cancel      context.CancelFunc
}
```

Il manager dovrebbe offrire:

- `Create(kind, options)`;
- `Start(jobID)` tramite worker pool limitato;
- `Get(jobID)`;
- `Cancel(jobID)`;
- retention degli ultimi job completati;
- chiave di deduplicazione per impedire doppie operazioni equivalenti;
- log con `job_id`, `kind` e risultato;
- chiusura ordinata durante lo shutdown.

Endpoint suggeriti:

```text
POST /api/jobs
GET  /api/jobs/{id}
POST /api/jobs/{id}/cancel
GET  /api/jobs?kind=...&state=...
```

La risposta alla creazione dovrebbe restituire `202 Accepted` e l'ID del job.
La UI può fare polling con backoff o usare uno stream già disponibile per gli
eventi. L'operazione sincrona va mantenuta solo quando è realmente breve.

### 7.4 Prime operazioni da migrare

1. rinomina massiva e riparazione cartelle;
2. scansione archivio e scansione completa librerie;
3. backfill MediaInfo;
4. backup e restore;
5. manutenzione database e ricalcolo scoring;
6. diagnostica multipla delle sorgenti;
7. azioni di pulizia che possono attraversare molti file.

Le operazioni torrent e il ciclo automatico possono essere integrate in un secondo
momento, perché possiedono già un proprio stato di sessione e lifecycle.

### 7.5 Criteri di accettazione

- due richieste duplicate non avviano due lavori equivalenti;
- ogni lavoro ha un ID e uno stato osservabile;
- un errore tardivo appare sia nello stato del job sia nel log correlato;
- la cancellazione non lascia worker orfani;
- lo shutdown attende o annulla i job secondo una politica esplicita;
- il refresh della pagina conserva lo stato del lavoro;
- il sistema non crea goroutine illimitate in funzione del numero di richieste.

**Priorità proposta: P1 per le operazioni distruttive o lunghe, P2 per le altre.**

---

## 8. Concorrenza, race condition e test

### 8.1 Situazione attuale

Il codice contiene molte attività concorrenti: provider, RSS, ricerca web,
worker di background, torrent, stream HTTP, TUI e operazioni di manutenzione.
Sono già presenti mutex e test dedicati, e `go vet` non segnala problemi. Questo
non dimostra però l'assenza di data race: `go vet` non è un race detector e i test
normali possono non esercitare l'interleaving problematico.

### 8.2 Aree da verificare

- mappe di retry e stato post-seed condivise tra eventi e worker;
- cache di configurazione mentre vengono salvate impostazioni;
- stato dei torrent tra polling, callback engine e shutdown;
- strutture di progresso della manutenzione;
- registri di job e stream di notifiche;
- contatori e snapshot metriche aggiornati da più goroutine;
- chiusura della sessione libtorrent mentre un worker la sta usando;
- file di stato e database durante backup, restore o manutenzione.

### 8.3 Piano race-safe

Per ogni stato condiviso scegliere esplicitamente una sola strategia:

- `sync.Mutex`/`sync.RWMutex` per strutture mutate;
- `atomic` per contatori e flag semplici;
- snapshot immutabili sostituiti in un'unica assegnazione per configurazione e
  metriche;
- canali per consegnare eventi a un owner unico;
- context cancellabile per ogni worker;
- nessuna mappa condivisa senza documentazione del lock che la protegge.

### 8.4 Test e CI

Aggiungere almeno:

```bash
go test -race ./...
```

Se il costo della matrice completa fosse troppo alto, organizzare la CI in:

1. test veloci e `go vet` su ogni push;
2. test completi normali su ogni pull request;
3. race test completi in una coda dedicata o almeno giornaliera;
4. test reali libtorrent/anacrolix in job separati e non concorrenti con i test
   che usano le stesse porte o directory temporanee.

I test concorrenti devono usare timeout, `t.Cleanup`, directory temporanee e
porte casuali. Non devono dipendere dal servizio Gextto già in esecuzione.

**Priorità proposta: P1 per introdurre il race test in CI, P2 per la bonifica
delle singole aree.**

---

## 9. Piano di lavoro raccomandato

### Fase 0 — Baseline

- congelare i test di comportamento correnti;
- salvare metriche di caricamento UI, memoria e CPU;
- introdurre una convenzione per log e ID di operazione;
- aggiungere controlli cataloghi e `go test -race` come job non bloccante.

### Fase 1 — Rischio basso

- estrarre `ui-core.js`, dialoghi e shell;
- introdurre helper DOM sicuri per i nuovi componenti;
- aggiungere test XSS sui rendering dinamici;
- correggere le nuove stringhe usando chiavi i18n.

### Fase 2 — Coerenza UI

- migrare cataloghi e interpolazioni;
- ridurre `MutationObserver` alle sole aree legacy;
- estrarre tabelle, log e manutenzione in moduli indipendenti;
- aggiungere test di rendering multilingua.

### Fase 3 — Job asincroni

- creare il job manager;
- migrare rinomina e scansioni;
- aggiungere cancellazione, deduplicazione e stato persistente o retained;
- collegare UI, log e notifiche allo stesso `job_id`.

### Fase 4 — Concorrenza

- eseguire race test ripetuti;
- correggere mappe e snapshot condivisi;
- verificare shutdown, restart e ripristino dei torrent;
- rendere bloccanti in CI i race test sui componenti stabilizzati.

## 10. Risultato atteso

Al termine degli interventi Gextto dovrebbe avere:

- client più facile da modificare senza regressioni incrociate;
- traduzioni deterministiche e verificabili prima del rilascio;
- rendering dinamico con confini chiari tra dati e markup;
- operazioni lunghe osservabili, cancellabili e correlabili ai log;
- maggiore confidenza sulla concorrenza grazie a race test ripetibili;
- nessun aumento non motivato del consumo di RAM, CPU o richieste browser.

La raccomandazione è **non riscrivere tutto**. Conviene procedere per estrazioni
piccole, mantenendo i test verdi a ogni fase e separando sempre refactoring
strutturale e modifica funzionale.
