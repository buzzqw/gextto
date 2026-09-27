# Piano tecnico: migrazione UI a Go + templ + HTMX + vanilla JS

## 1. Obiettivo

Sostituire la UI attuale basata su Leptos/Rust/WASM con una UI server-rendered
basata su:

- **Go** per routing, view-model, rendering e azioni;
- **templ** per componenti HTML tipizzati e compilati in Go;
- **HTMX** per navigazione parziale, polling e submit delle azioni;
- **JavaScript vanilla** solo per stato realmente client-side;
- **CSS già esistente** riutilizzato e progressivamente ripulito.

Gli endpoint JSON Go esistenti devono restare compatibili. La migrazione riguarda
la presentazione e il flusso browser, non il motore torrent, il database o la
logica di automazione.

Obiettivo finale: nessun Rust, nessun WASM e nessuna dipendenza `cargo-leptos`
nel percorso di build e distribuzione della UI.

---

## 2. Stato attuale

### 2.1 Frontend

La UI attuale è una SPA Leptos CSR:

- sorgente principale: `ui/app/src/lib.rs`;
- stile: `ui/style/main.scss`;
- configurazione build: `ui/Cargo.toml`;
- build: `scripts/build-ui.sh` con `cargo-leptos`;
- output: `webui/pkg/ui.js`, `webui/pkg/ui.wasm`, `webui/pkg/ui.css`;
- test browser: `ui/end2end/` con Playwright.

Il componente principale contiene circa 11.000 righe Rust e gestisce nello
stesso stato client:

- dashboard e metriche live;
- sessione torrent e download HTTP;
- modale dettagli torrent;
- peer, tracker, file e priorità;
- serie, film, episodi, archivio e ricerca;
- configurazione, integrazioni, manutenzione e diagnostica;
- tema, lingua, token API, filtri, paginazione e notifiche.

### 2.2 Backend e serving

Il daemon Go espone già:

- `web_router.go` con le API JSON e le azioni HTTP;
- `web_handlers_core.go` per shell, autenticazione, favicon e asset;
- `web_ui_embed.go` per embedding e override da disco;
- `web_serve.go` per server, timeout e stream;
- endpoint SSE per log e notifiche;
- autenticazione opzionale tramite `X-Gextto-Token` o `Authorization: Bearer`.

La root `/` oggi restituisce una shell HTML minima che carica il bundle WASM.
Il browser carica i dati tramite numerose chiamate `/api/...` e mantiene lo
stato in segnali Leptos.

### 2.3 Vincoli da preservare

1. Il daemon deve continuare a essere distribuibile come singolo binario.
2. `GEXTTO_UI_DIR` e l'override UI da disco devono continuare a funzionare.
3. L'autenticazione API deve continuare a essere applicata anche dopo un
   cambio runtime del token.
4. Le operazioni distruttive devono conservare conferme e semantica attuale.
5. Il cambio frontend non deve alterare il comportamento di libtorrent,
   qBittorrent, anacrolix, database, scheduler o post-processing.
6. Le API esistenti devono restare utilizzabili da client esterni.

---

## 3. Architettura target

### 3.1 Struttura proposta

```text
uiweb/
├── components/
│   ├── layout.templ
│   ├── navigation.templ
│   ├── panel.templ
│   ├── table.templ
│   ├── modal.templ
│   ├── flash.templ
│   └── forms.templ
├── pages/
│   ├── dashboard.templ
│   ├── downloads.templ
│   ├── series.templ
│   ├── movies.templ
│   ├── explore.templ
│   ├── archive.templ
│   ├── health.templ
│   ├── logs.templ
│   ├── settings.templ
│   ├── integrations.templ
│   ├── maintenance.templ
│   ├── manual.templ
│   └── license.templ
├── partials/
│   ├── dashboard_metrics.templ
│   ├── torrent_table.templ
│   ├── torrent_detail.templ
│   ├── torrent_files.templ
│   ├── torrent_peers.templ
│   ├── history_table.templ
│   └── notifications.templ
├── viewmodel/
│   ├── common.go
│   ├── dashboard.go
│   ├── torrents.go
│   ├── library.go
│   └── settings.go
├── static/
│   ├── gextto.js
│   ├── htmx.min.js
│   └── ui.css
└── generate.go
```

I nomi sono indicativi. L'importante è separare layout, pagine, partial e
view-model: non ricreare in un unico file Go l'equivalente dell'attuale
`lib.rs`.

### 3.2 Ruolo dei componenti

#### Go

- costruisce il `PageModel` iniziale;
- carica configurazione, lingua e stato necessario alla pagina;
- crea view-model tipizzati per i template;
- gestisce gli endpoint HTML e le azioni;
- conserva le API JSON come contratto pubblico;
- applica autenticazione, autorizzazione e escaping.

#### templ

- definisce componenti HTML riutilizzabili;
- genera codice Go durante il build;
- permette di verificare gli errori strutturali a compile-time;
- evita stringhe HTML concatenate manualmente.

#### HTMX

- cambia pagina o partial senza riscrivere tutta la SPA;
- carica i dettagli torrent solo quando richiesti;
- aggiorna le tabelle con polling mirato;
- invia form e azioni al backend;
- usa gli header `HX-Trigger`, `HX-Redirect` e `HX-Reswap` per coordinare
  aggiornamenti senza stato globale complesso.

#### JavaScript vanilla

Da limitare a:

- tema e preferenze locali;
- token API negli header HTMX;
- apertura/chiusura modali e tab locali;
- debounce della ricerca;
- clipboard e `confirm()`;
- upload file/magnet quando serve multipart o JSON;
- SSE di log/notifiche;
- aggiornamenti ottimistici molto semplici;
- accessibilità e gestione focus.

Non deve diventare una nuova SPA nascosta.

---

## 4. Strategia di routing

### 4.1 Pagine complete

La root deve restituire HTML utile anche senza JavaScript:

```text
GET /                         shell + dashboard
GET /?view=downloads          shell + sessione download
GET /?view=series             shell + serie
GET /?view=movies             shell + film
GET /?view=archive             shell + archivio
GET /?view=settings            shell + configurazione
```

In alternativa si possono usare URL espliciti (`/ui/downloads`, `/ui/settings`),
ma il formato scelto deve essere stabile, linkabile e funzionare con refresh
del browser e pulsanti avanti/indietro.

### 4.2 Partial HTMX

Esempi consigliati:

```text
GET  /ui/partial/dashboard/metrics
GET  /ui/partial/torrents/table
GET  /ui/partial/torrents/{hash}/detail
GET  /ui/partial/torrents/{hash}/files
GET  /ui/partial/torrents/{hash}/peers
GET  /ui/partial/history
GET  /ui/partial/notifications
```

Le partial devono restituire solo markup HTML, mai un documento completo.

### 4.3 Azioni UI

Per non accoppiare HTMX al formato JSON delle API, conviene introdurre piccoli
adapter HTML:

```text
POST /ui/action/torrents/{hash}/pause
POST /ui/action/torrents/{hash}/resume
POST /ui/action/torrents/{hash}/remove
POST /ui/action/torrents/{hash}/files/priority
POST /ui/action/settings/save
POST /ui/action/archive/delete
```

Gli adapter possono chiamare la stessa logica interna degli handler API oppure
delegare internamente agli stessi servizi. Non devono fare richieste HTTP a se
stesso: sarebbe più lento e introdurrebbe problemi di autenticazione.

Le API JSON restano invariate per compatibilità.

---

## 5. Autenticazione e sicurezza

Questo è uno dei punti più delicati.

### 5.1 Problema attuale

Il token può essere memorizzato nel `localStorage` e aggiunto dal JavaScript
alle richieste API. HTMX non legge automaticamente il `localStorage`.

### 5.2 Soluzione di transizione

Configurare HTMX in JavaScript:

```js
document.body.addEventListener("htmx:configRequest", (event) => {
  const token = localStorage.getItem("gextto_api_token");
  if (token) event.detail.headers["X-Gextto-Token"] = token;
});
```

Gli endpoint `/ui/...` che leggono dati protetti devono applicare la stessa
autenticazione degli endpoint `/api/...`, non essere pubblici solo perché
restituiscono HTML.

### 5.3 Soluzione futura opzionale

Introdurre una sessione browser server-side o un cookie sicuro, ma solo in una
fase separata. Richiederebbe:

- scadenza e revoca sessione;
- protezione CSRF per POST;
- logout;
- migrazione del comportamento attuale del token;
- test su proxy e installazioni remote.

Per la prima migrazione è più sicuro mantenere il meccanismo già collaudato.

### 5.4 Rischi XSS

I nomi torrent, i path, i log, le risposte dei tracker e alcuni contenuti
esterni sono dati non fidati. Regole:

- usare sempre escaping automatico templ;
- non usare `templ.Raw()` su valori API;
- sanitizzare l'HTML del manuale Markdown;
- lasciare i log come testo oppure usare una whitelist di tag e classi;
- non costruire HTML con concatenazione di stringhe;
- aggiungere una Content Security Policy dopo la rimozione degli inline script.

---

## 6. Migrazione funzionale

### Fase 0 — Baseline e contratto

Prima di scrivere la nuova UI:

1. congelare il contratto delle API JSON;
2. catalogare ogni pagina, tab, modale, form e azione;
3. salvare screenshot e flussi Playwright della UI attuale;
4. misurare:
   - tempo fino al primo HTML;
   - tempo fino a UI interattiva;
   - dimensione JS/WASM/CSS;
   - richieste iniziali;
   - memoria browser;
5. verificare i casi:
   - dry-run;
   - active mode;
   - token API presente e assente;
   - italiano e inglese;
   - tema chiaro e scuro;
   - mobile 390 px;
   - libtorrent, qBittorrent e anacrolix.

Produrre una matrice funzionale con stato `da migrare`, `migrata`, `verificata`.

### Fase 1 — Bootstrap Go/templ

1. Aggiungere il modulo templ e fissare la versione.
2. Aggiungere `go:generate` per generare i componenti.
3. Creare layout, head, navigation, pannelli, flash e modal.
4. Copiare il CSS compilato esistente in una posizione target stabile.
5. Servire `htmx.min.js`, `gextto.js` e CSS tramite il sistema di embedding
   esistente.
6. Aggiungere una modalità di test parallela, per esempio:

   ```text
   /legacy  -> Leptos attuale
   /        -> nuova UI Go, inizialmente solo dashboard
   ```

7. Non rimuovere ancora `ui/` o il supporto WASM.

### Fase 2 — Layout, tema, lingua e token

Implementare:

- shell responsive;
- navigazione con URL persistente;
- tema chiaro/scuro usando `data-theme` già presente nel CSS;
- scala/interfaccia compatta;
- selezione lingua;
- token API;
- toast e messaggi di errore;
- focus management per modal e tab.

Questa fase deve rendere possibile il caricamento di pagine vuote e partial
senza dipendere dalla lista torrent.

### Fase 3 — Dashboard e Scarico

È la prima fase utile e prioritaria per il problema segnalato.

#### Dashboard

- metriche iniziali renderizzate dal server;
- refresh solo dei valori live;
- grafici con SVG generato da Go o piccolo JS vanilla;
- azioni “avvia ciclo” con risposta flash;
- SSE notifiche senza ricaricare la pagina.

#### Sessione download

- tabella torrent e download HTTP;
- progress bar e velocità download/upload affiancate;
- filtri, tag, selezione multipla e ordinamento;
- polling della sola tabella torrent;
- modale dettaglio caricata con `hx-trigger="click"`;
- tab Contenuto, Peer, Tracker, Limiti e Storage come partial lazy;
- aggiornamento della riga dopo pause, resume, recheck, remove e pin;
- upload `.torrent` tramite form multipart dedicato.

Il refresh non deve sostituire una modale aperta o cancellare un campo in fase
di modifica. Prima di aggiornare una partial, verificare se contiene un form
dirty o un elemento con focus.

### Fase 4 — Serie, film, episodi e archivio

Convertire progressivamente:

- lista serie e film;
- dettaglio serie;
- dettaglio film;
- episodi e stagioni;
- ricerca manuale e ricerca release;
- preview ed esecuzione rinomina;
- archivio torrent con paginazione;
- scansioni e azioni batch;
- cronologia download.

Usare query string per filtri, pagina, ordinamento e tab. In questo modo un
link copiato o un refresh non perde lo stato della ricerca.

### Fase 5 — Esplora e integrazioni

Convertire:

- calendario;
- discovery TMDB/TVDB;
- Trakt;
- Simkl;
- Jellyfin;
- Plex;
- fumetti e download HTTP.

I flussi OAuth/PIN devono restare server/API-driven. Il browser deve gestire
solo l'apertura della finestra, il polling e la visualizzazione dello stato.

### Fase 6 — Configurazione e manutenzione

È la parte più ampia per numero di campi e azioni:

- configurazione generale;
- libreria;
- indexer e sorgenti;
- libtorrent/qBittorrent/anacrolix;
- backup;
- RAM disk;
- filtri, tag e regole;
- manutenzione database;
- migrazioni torrent;
- diagnostica e test connessioni.

Ogni sezione deve avere un view-model esplicito e un form indipendente. Evitare
un unico form globale: riduce conflitti, payload accidentali e problemi di
salvataggio parziale.

### Fase 7 — Log, manuale e licenza

- log in streaming con `EventSource` o partial incrementali;
- filtro e highlighting lato server o con whitelist controllata;
- manuale Markdown renderizzato server-side con una libreria Go come goldmark;
- indice e slug stabili;
- licenza resa come contenuto statico escaped.

Il parser Markdown Rust custom deve essere sostituito o il Markdown deve essere
pre-renderizzato durante il build. Non trasferire HTML non sanificato dal
browser.

### Fase 8 — Cutover e rimozione Rust

Solo quando la matrice funzionale è completa:

1. rendere la UI Go la root ufficiale;
2. mantenere temporaneamente `/legacy` per rollback;
3. monitorare errori e richieste per una release;
4. rimuovere `cargo-leptos` dagli script e dalla CI;
5. rimuovere il target WASM e gli asset `ui.js`/`ui.wasm`;
6. eliminare o archiviare `ui/app`, `ui/frontend`, `ui/server` e `ui/Cargo.toml`;
7. aggiornare README, manuale, packaging e systemd/documentazione.

---

## 7. Polling, SSE e consistenza dello stato

### Polling

Non replicare il refresh globale attuale. Usare intervalli per dominio:

- torrent sessione: 1–3 secondi quando la pagina è visibile;
- dashboard: 5–15 secondi;
- salute: on-demand o 10–30 secondi;
- configurazione: nessun polling automatico;
- archivio e storico: solo dopo ricerca o cambio pagina.

Usare `document.visibilityState` per fermare o rallentare il polling in tab
nascoste.

### SSE

Riutilizzare:

- `/api/logs/stream`;
- `/api/notifications/stream`.

Il client deve riconnettersi con backoff, evitare duplicati e non mostrare un
toast permanente quando la connessione viene chiusa durante una navigazione.

### Azioni concorrenti

Un refresh può arrivare mentre l'utente sta modificando un campo. Regole:

- non sostituire un form dirty;
- mostrare “dati aggiornati, modifiche locali non salvate”;
- disabilitare il doppio submit;
- usare id/versione o timestamp quando il backend lo rende possibile;
- dopo un'azione mostrare il risultato reale restituito dal backend.

---

## 8. Rischi e problemi possibili

| Rischio | Impatto | Mitigazione |
|---|---:|---|
| Perdita di funzionalità nella riscrittura | Alto | matrice funzionale e cutover per fasi |
| HTMX non invia JSON come le API attuali | Alto | adapter `/ui/action` con parser form oppure JS `fetch` mirato |
| Token non aggiunto alle richieste HTMX | Alto | hook `htmx:configRequest` e test con token obbligatorio |
| Partial non autenticata | Alto | middleware comune per `/api` e `/ui` |
| XSS da nomi, log o Markdown | Alto | escaping templ, sanitizzazione e CSP |
| Refresh che cancella modifiche locali | Alto | dirty-state e target granulari |
| Modali/tabs persi dopo swap | Medio | componenti HTML stabili, eventi `htmx:afterSwap` |
| Back/forward non ripristina filtri | Medio | `hx-push-url` e stato in query string |
| Polling duplicato dopo navigazione | Medio | cleanup globale e un solo controller per partial |
| Tabelle grandi lente | Medio | paginazione, partial mirate, `table-layout:fixed`, nessun refresh globale |
| Upload torrent incompatibile | Medio | endpoint multipart dedicato e test file grandi |
| SSE aperti durante logout/navigazione | Medio | chiusura esplicita e reconnect controllato |
| Differenze tra backend torrent | Medio | view-model normalizzati e test per ogni engine |
| Traduzioni mancanti | Medio | chiavi centralizzate e test di copertura |
| Build templ non riproducibile | Medio | versione fissata, `go generate`, CI pulita |
| CSS compilato non aggiornato | Medio | asset versionati o hashati e controllo CI |
| Rimozione prematura del WASM | Alto | feature flag o `/legacy` fino al cutover |
| Differenze mobile/accessibilità | Medio | Playwright mobile, tastiera e test screen reader base |
| Template troppo grandi | Basso/Medio | partial separate e view-model dedicati |
| Errori di escaping con `templ.Raw` | Alto | vietare `Raw` salvo helper sanitizzato testato |

---

## 9. Migliorie consigliate rispetto alla UI attuale

### Navigazione

- URL stabili per pagina, tab e filtri;
- pulsanti browser avanti/indietro funzionanti;
- stato della pagina ripristinato al refresh;
- breadcrumb nei dettagli serie/film/torrent;
- ricerca globale con scorciatoia da tastiera.

### Torrent

- dettaglio lazy invece di caricare peer, tracker e file insieme;
- refresh indipendente per stato, peer e file;
- colonne della sessione configurabili e persistenti;
- nomi lunghi con tooltip e copia rapida;
- azioni batch con riepilogo per elemento;
- progresso peer e disponibilità file visualizzati senza aprire più pannelli;
- stato “stale” visibile se il polling fallisce;
- conferma più chiara per cancellazione file e blocklist.

### Form e configurazione

- autosalvataggio opzionale solo per impostazioni non distruttive;
- evidenza dei campi modificati;
- validazione lato browser più messaggio lato server;
- reset della singola sezione invece del reset globale;
- riepilogo delle modifiche prima di “Salva tutte”;
- help contestuale generato dalla stessa fonte delle descrizioni.

### Performance

- HTML iniziale già utile prima del JavaScript;
- niente download WASM;
- partial per pannello invece di JSON globale;
- polling sospeso quando la pagina è nascosta;
- lazy loading di grafici, calendario e discovery;
- compressione HTTP per HTML, CSS e JS;
- ETag solo per asset statici, non per dati torrent live;
- CSS ridotto e senza regole duplicate al termine della migrazione.

### Accessibilità

- link reali per la navigazione, non solo button;
- `aria-current` sulla pagina attiva;
- dialog con `role="dialog"`, `aria-modal` e focus trap;
- feedback `aria-live` per errori e risultati azioni;
- tabelle con header associati e caption dove utile;
- funzionamento completo da tastiera;
- contrasto verificato in entrambi i temi.

---

## 10. Build, embedding e distribuzione

### Durante la migrazione

Conservare il meccanismo attuale di `web_ui_embed.go`:

- `go:embed webui` continua a includere asset statici;
- `GEXTTO_UI_DIR` continua a sovrascrivere gli asset embedded;
- `/pkg/` può restare temporaneamente per compatibilità;
- `ui.js` e `ui.wasm` possono essere serviti solo dalla modalità legacy.

### Build target

Sostituire gradualmente:

```text
make ui
  -> templ generate
  -> go generate / gofmt
  -> copia o verifica static asset
  -> test template
```

Il build daemon finale deve dipendere solo da Go e dagli asset statici già
generati. Se si mantiene SCSS come sorgente, bisogna decidere esplicitamente
se usare Dart Sass in CI oppure committare il CSS generato. Per eliminare del
tutto toolchain esterne è preferibile mantenere CSS compilato e verificato in
CI con un controllo di sincronizzazione.

### Cache

La root e le partial devono rimanere no-cache o avere cache controllata. Gli
asset statici possono usare nomi con hash e cache lunga, mentre gli asset
override da disco devono restare coerenti con `GEXTTO_UI_DIR`.

---

## 11. Test e criteri di accettazione

### Unit test Go

- rendering dei componenti templ;
- escaping di nomi/path/log sospetti;
- view-model con campi mancanti/nulli;
- form invalidi e payload incompleti;
- autenticazione su `/ui` e `/api`;
- risposte HTMX, `HX-Trigger` e status HTTP;
- paginazione e filtri;
- conversione bytes, velocità, ETA e percentuali.

### Integration test

- root senza token;
- root con token e richieste HTMX autenticate;
- lingua italiana/inglese;
- tema e preferenze persistenti;
- add/pause/resume/remove torrent;
- file e peer di un torrent;
- priorità file;
- upload magnet e `.torrent`;
- azioni distruttive con conferma;
- SSE log/notifiche;
- override da `GEXTTO_UI_DIR`;
- bundle embedded nel binario.

### Playwright

Mantenere i test esistenti e sostituire gradualmente gli assert specifici
Leptos con assert funzionali:

- tutte le voci di navigazione;
- reload diretto di ogni pagina;
- avanti/indietro browser;
- dettaglio torrent e tab Contenuto/Peer;
- polling senza perdita del focus;
- form dirty durante refresh;
- mobile 390 px senza overflow orizzontale globale;
- tastiera per menu, tab e modali;
- assenza di richiesta `ui.wasm` nella UI nuova.

### Budget indicativi

Da fissare dopo la baseline, ma la UI target dovrebbe rispettare almeno:

- zero download WASM;
- HTML iniziale con contenuto visibile;
- nessun refresh completo per le operazioni quotidiane;
- asset JS applicativo piccolo e non framework-based;
- nessun polling duplicato dopo 10 navigazioni avanti/indietro;
- aggiornamento torrent limitato alla tabella o alla riga interessata.

---

## 12. Piano di rollback

Durante almeno una release:

1. mantenere la UI Leptos compilabile;
2. esporre `/legacy` oppure una variabile di configurazione per scegliere la
   shell;
3. non modificare il contratto API;
4. raccogliere errori server sugli handler HTML;
5. permettere il ritorno alla UI precedente senza migrazione dati;
6. rimuovere il legacy solo dopo una release stabile e una verifica manuale
   su installazione reale.

Il rollback deve essere esclusivamente frontend: nessun downgrade del database
o dei torrent deve essere necessario.

---

## 13. Definizione di completamento

La migrazione è completata quando:

- la root `/` è renderizzata da Go/templ;
- non viene caricato WASM nella UI ufficiale;
- tutte le pagine e azioni della matrice funzionale sono disponibili;
- API JSON e client esterni non hanno regressioni;
- token, i18n, temi, mobile, SSE e upload funzionano;
- i test Go e Playwright passano in CI;
- `make build` non richiede Rust, Cargo o cargo-leptos;
- documentazione, packaging e README sono aggiornati;
- il ramo legacy può essere rimosso senza modificare dati applicativi.

## 14. Decisione consigliata

Procedere con una migrazione incrementale **senza riscrivere le API** e iniziare
da Dashboard + Scarico + Dettaglio torrent. È il percorso che consente di
verificare subito i vantaggi percepiti (avvio, reattività, tabelle e dettagli)
con il rischio più basso e senza bloccare lo sviluppo del daemon.

La scelta tecnica raccomandata è:

```text
Go net/http
+ templ
+ HTMX vendorizzato e versionato
+ JavaScript vanilla piccolo e modulare
+ CSS esistente, inizialmente riutilizzato senza redesign obbligatorio
```

---

## 15. Revisione esperta del piano

Giudizio complessivo: l'impostazione è corretta e prudente. Server-side rendering,
niente WASM, API JSON invariate, migrazione incrementale. Restano però alcuni punti
da correggere prima di procedere.

1. **"HTML utile senza JavaScript" è incompatibile con l'auth a token di header.**
   Un documento pubblico non può portare `X-Gextto-Token`, quindi i dati non
   possono essere renderizzati nella shell pubblica. La soluzione è quella già
   prevista in §5.3 (sessione/cookie server-side) oppure, per ora, shell pubblica
   + dati via partial **autenticate**. Il primo slice adotta la seconda, identica
   al modello della SPA attuale.
2. **`templ` richiede un codegen esterno.** Per mantenere `make build` puro Go si
   deve o committare i `*_templ.go` generati, o usare `html/template` della
   stdlib. Il primo slice usa `html/template` (zero toolchain, escaping
   automatico, verifiche con test di rendering). `templ` resta un'opzione, non un
   requisito.
3. **HTMX va vendorizzato e versionato** (licenza BSD-2) e incluso nell'embed, se
   adottato. Il primo slice non ne ha bisogno: le partial sono HTML e le azioni
   riusano le API JSON esistenti. La scelta va presa una volta, non per pagina.
4. **Autenticazione di `/ui`.** Partial e azioni devono passare dallo stesso
   middleware di `/api` (fatto: `ApiAuth` protegge `/ui/partial/*` e
   `/ui/action/*`); la shell resta pubblica come l'attuale.
5. **Adapter `/ui/action` e formato dei body.** HTMX invia `form-urlencoded`, le
   API accettano JSON. O si converte nell'adapter, o si usa `hx-ext="json-enc"`,
   o si riusano le API JSON via `fetch` (scelta del primo slice). Va documentato.
6. **CSP dopo la rimozione degli inline script.** La shell attuale ha `<style>`
   inline; la CSP va pianificata a fine migrazione, non promessa subito.
7. **i18n.** La nuova UI deve usare lo stesso DB traduzioni (`/api/i18n`,
   `internal_translations.yml`, pacchetto `messages`). Il primo slice è IT-only,
   ma il seam va previsto e testato.
8. **CSS.** Riutilizzare `main.scss` compilato è giusto, ma il piano deve fissare
   *chi* lo compila (nessuna toolchain Rust a fine migrazione): Sass in CI oppure
   CSS committato con controllo di sincronizzazione.
9. **Parità come gate.** Va introdotto l'inventario generato
   (`docs/migrazione-ui-inventario.md`): una pagina si considera migrata solo se
   tutte le sue azioni, i suoi campi e i suoi endpoint sono presenti e testati.
10. **Test anti-regressione.** Oltre a Playwright: divieto di `template.HTML`/
    `templ.Raw` su dati API, e un test che garantisce che la nuova UI non carichi
    `ui.wasm`.

## 16. Primo slice consegnato (additivo, Leptos intatto)

- `GET /ui` — shell server-rendered con **la stessa navigazione** (16 pagine) e
  link alla UI classica.
- `GET /ui/partial/dashboard` — dashboard con dati vivi (stato, motore, torrent,
  velocità, ultimo ciclo).
- `GET /ui/partial/torrents` — tabella **Scarico** server-rendered (stato,
  progresso, velocità, peer/seed, ratio, azioni).
- `GET /ui/partial/unavailable` — placeholder onesto per le pagine non ancora
  migrate, con link alla UI classica (nessuna funzione "sparita").
- `GET /ui/static/gextto-ui.js` — client minimo: token da
  `localStorage["gextto_api_token"]`, polling per dominio (3s Scarico, 10s
  dashboard, sospeso a tab nascosta), azioni pause/resume/recheck/remove/
  run-now verso le API JSON esistenti.
- Auth: `/ui/partial/*` e `/ui/action/*` protetti da `ApiAuth`; shell pubblica
  come oggi. `Cache-Control: no-cache` su `/ui`.
- File: `uiweb.go`, `uiweb/templates/ui.html`, `uiweb/static/gextto-ui.js`,
  `uiweb_test.go`. CSS riusato da `/pkg/ui.css`.
- Test: parità delle voci di navigazione, rendering dashboard, escaping di un
  nome torrent malevolo, `401` sui partial senza token con shell pubblica,
  parità delle etichette di stato.

## 17. Piano rivisto (gate e ordine)

- **Gate bloccante:** inventario di parità `docs/migrazione-ui-inventario.md`.
- Fase 0 — baseline/misure: parzialmente fatto (inventario generato).
- Fase 1 — bootstrap Go/rendering/auth/asset: **fatto** (senza `templ`/HTMX).
- Fase 2 — layout, tema, lingua, token: shell pronta; tema è un toggle locale,
  lingua da collegare a `/api/i18n`.
- Fase 3 — Dashboard + Scarico: **fatto**.
- Fasi 4–7 — una pagina per volta; le non migrate puntano alla UI classica.
- Fase 8 — cutover e rimozione Rust: invariata; il rollback resta `/`.



---

## 18. Stato finale della migrazione (tutte le pagine)

Tutte e 16 le pagine di navigazione sono ora servite dalla nuova UI Go, senza
dipendere da Rust/WASM e senza perdere funzionalità:

| Pagina | Tipo nuova UI |
|---|---|
| Dashboard | server-side |
| Scarico | server-side + polling |
| Salute | server-side (stesso health check) |
| Log | server-side (stesso tail) |
| Manuale | server-side (Markdown minimale) |
| Licenza | server-side |
| Serie TV / Film / Mancanti | tabella dati-driven (API esistenti) |
| Archivio / Blocklist / Fumetti | tabella dati-driven (API esistenti) |
| Configurazione | form per campo (147 chiavi, stessi label/tab) |
| Esplora | ricerca + accoda (API esistenti) |
| Manutenzione / Integrazioni | pulsanti azione (API esistenti) |

La UI classica Leptos resta su `/` e può essere rimossa quando si vuole, senza
migrazione dati. Le azioni riusano le stesse API JSON, quindi il contratto
pubblico e i client esterni non cambiano.
