# Gextto UI (SSR + HTMX) — stato e report

Data: 2026-10-01 · Build di riferimento: `gexttod 0.1.0 (build 1229)` · libtorrent 2.0.11.0

## 1. Obiettivo e approccio

L'interfaccia ufficiale usa *SSR + HTMX* e non dipende più dal vecchio client
`gextto-ui.js`.

La v2:

- è l'interfaccia ufficiale su **`/`**; **`/v2`** resta un alias tecnico;
- riusa le stesse API JSON, gli stessi view-model Go (`uiDashboardDataFrom`,
  `uiTorrentsDataFrom`, `uiSettingsPageFrom`, `uiSeriesDetailFrom`,
  `uiMovieDetailFrom`, `uiTableSpecFor`, …) e lo stesso CSS di base
  (`/v2/static/gextto-ui.css`), quindi dati e look restano coerenti;
- non duplica l'accesso ai dati: le tabelle sono renderizzate dal server
  chiamando internamente gli handler delle API esistenti;
- traduce l'HTML lato server con lo stesso meccanismo di localizzazione.

Accesso:

```
http://127.0.0.1:5000/
http://127.0.0.1:5000/?view=downloads
```

## 2. Architettura

| File | Ruolo |
| --- | --- |
| `uiweb_v2.go` | shell, navigazione, dispatch delle pagine, i18n server-side, Scarico, Configurazione, Log, shell/panels |
| `uiweb_v2_table.go` | renderer generico delle tabelle (formati, azioni, filtro, ordinamento, paginazione) + ponte interno verso le API JSON |
| `uiweb_v2_sections.go` | renderer dei pannelli (Manutenzione, Integrazioni) + forward generico di azioni/form |
| `uiweb_v2_search.go` | Esplora: ricerca release server-side + "Aggiungi" |
| `uiweb_v2_detail.go` | dettagli Serie/Film + modale sorgenti puntata |
| `uiweb_v2_settings_extras.go` | editor strutturati: feed, gruppi checkbox, editor a righe, rinomina, traduzioni |
| `uiweb_v2_maintenance.go` | widget Manutenzione: cestino, verifica sorgenti |
| `uiweb_v2_widgets.go` | widget completati: duplicati, ottimizzazione DB, RAM disk, rinomina cartella, progresso rinomina, OAuth/PIN, job in background, upload torrent, Esplora TMDB, traduzioni per chiave, anteprima rinomina |
| `uiweb_v2_test.go` / `uiweb_v2_bench_test.go` | test e benchmark |
| `uiweb/v2/templates/v2.html` | tutti i template v2 (unico file) |
| `uiweb/v2/static/htmx.min.js` | HTMX vendorizzato (nessuna CDN) |
| `uiweb/v2/static/v2-core.js` | **unico** script v2: tema, font, focus dei modali, scroll log |
| `uiweb/v2/static/v2.css` | rifiniture d'interfaccia sopra il CSS condiviso |
| `web_router.go` | **1 sola riga**: `registerV2Routes(s, mux)` |

Punti chiave:

- **Ponte interno**: gli handler chiamano gli handler API esistenti tramite il
  router in-process (`v2InternalJSON`), quindi validazione e comportamento sono
  gli stessi delle API, senza chiamate di rete.
- **i18n server-side**: dopo il render l'HTML viene tradotto con un tokenizer
  (`v2TranslateHTML`) che gestisce text node interi, attributi
  `title`/`placeholder`/`aria-label`, fallback inglese e non
  tocca `<script>`/`<style>`. Con lingua italiana l'HTML esce invariato.
- **JS minimale**: HTMX fa richieste e swap; `v2-core.js` copre solo preferenze
  (tema/font in `localStorage`) e comportamento accessibile dei modali.
- **Route isolate**: `/v2` è registrato con `v2Handle`, che non entra nella
  tabella delle route API: `docs/API.md` e il test di parità restano invariati.
  La UI ufficiale è alla radice `/`; i frammenti HTMX interni sono sotto `/v2`
  e la vecchia route `/ui` è stata rimossa.

## 3. Copertura

Tutte le 16 voci di menu sono migrate, più le sotto-pagine di dettaglio.

| Menu | Stato | Note |
| --- | --- | --- |
| Dashboard | ✅ | metriche, sessione, ultimo ciclo, consumo, ultimi download, **Prossime uscite** |
| Scarico | ✅ | tabella torrent + HTTP, aggiornamento automatico ogni **5 s** già attivo, progressione/velocità HTTP, ordinamento/filtro server, azioni riga, blocco, dettaglio/rimozione modali, **limiti/storage**, **tracker/file priority/web seed**, **storico download**, **aggiunta magnet/URL/.torrent**, **tag in massa** |
| Serie TV | ✅ | form TMDB + elenco + **dettaglio**: hero poster/metadati/cast con link TVDB/TMDB, stagioni on/off, episodi per stagione con azioni, sorgenti puntata, modifica serie con **Sfoglia** per il percorso NAS, azioni serie, **anteprima/esecuzione rinomina** |
| Film | ✅ | form TMDB + elenco + **dettaglio**: hero poster/metadati/cast con link TMDB, modifica, requisiti linguistici mostrati in formato leggibile, azioni, corrispondenze archivio e storico |
| Mancanti | ✅ | tabella gap + form di ricerca + Cerca/Ignora |
| Esplora | ✅ | ricerca release + Aggiungi, **calendario TMDB**, **tendenze/categorie TMDB**, **ricerca TMDB** con "Aggiungi alla libreria" |
| Archivio | ✅ | tabella + ricerca + paginazione, aggiunta, download/eliminazione e spiegazione della decisione |
| Fumetti | ✅ | tabella fumetti + **coda download HTTP**, esplorazione GetComics, link finder/download, weekly pack, storico e modifica |
| Configurazione | ✅ | campi, ricerca, **feed RSS**, **gruppi checkbox**, **editor a righe** (indexer, filtri sorgente, regole tag→cartella, event hook, cartelle osservate), **rinomina**, **traduzioni** (elenco + **modifica per chiave**, import YAML, export, elimina lingua) |
| Integrazioni | ✅ | scheda Simkl (stato, PIN, impostazioni, watchlist/calendario), impostazioni Jellyfin/Plex/FlareSolverr, **editor indexer**, link |
| Manutenzione | ✅ | azioni, pulizia DB, impostazioni backup, tabella backup, **cestino** (elenco/elimina/svuota), **verifica sorgenti**, **duplicati** (anteprima/pulizia), **ottimizzazione DB** (VACUUM/ANALYZE), **RAM disk**, **rinomina cartella** (scansione/accettazione/applicazione), **progresso rinomina**, **job in background** (avanzamento e annullamento) |
| Salute | ✅ | metriche, percorsi, dischi, errori, **sorgenti** (manuale) e **provider** |
| Log | ✅ | filtro, limite righe, aggiornamento automatico ogni 5 s, **colorazione dei livelli lato server**, scroll automatico |
| Blocklist | ✅ | tabella + rimozione |
| Manuale | ✅ | render server-side (IT/EN) |
| Licenza | ✅ | render server-side |

Interfaccia migliorata (solo in v2, tramite `v2.css`): barra superiore sticky,
stato attivo della navigazione più chiaro, tabelle con hover/zebra e header
sticky, backdrop dei modali con blur, focus ring sempre visibile, toolbar che
vanno a capo correttamente, adattamento mobile delle azioni in alto.

## 4. Residui consapevoli

Dopo questa tornata la scansione live delle 16 voci `/v2?view=…` non mostra
segnaposto di pagina. Restano però i seguenti flussi esplicitamente non ancora
portati:

- **"Ultimi trovati nei feed"** in Dashboard: resta su richiesta (è una vista
  diagnostica, non un flusso operativo).
- **Aggiunta manuale avanzata**: il flusso TMDB è disponibile server-side e
  include i principali campi di qualità, lingua, sottotitoli ed esclusioni; i
  campi API più specialistici non sono esposti nel form.
- **Gruppo "feed" / TMDB/TVDB**: le viste sono server-side; il calendario carica
  in modo asincrono con HTMX (`hx-trigger="load"`) per non bloccare la pagina.
- **Azioni lunghe**: la v2 avvia l'azione e mostra l'avanzamento nel pannello
  **Operazioni in background** (polling + annullamento), senza una barra
  dedicata alla singola azione.

## 5. Differenze accettate (parità)

- **Ordinamento** delle tabelle generiche è server-side;
  il filtro elenco è server-side.
- **Modali**: focus-trap, Escape e click sul backdrop funzionano (`v2-core.js`).
- **Log**: aggiornamento a intervalli invece del follow SSE; colorazione
  equivalente.
- **Azioni lunghe**: la v2 espone un pannello **Operazioni in background** con
  avanzamento e annullamento (polling `/api/jobs`); le azioni brevi eseguono e
  ricaricano.

## 6. Test

- `go test .` → **verde** (include la suite v2 completa, compreso
  `uiweb_v2_widgets_test.go`).
- `go test ./...` → **verde**, tutti i package.
- `scripts/check-ui-settings-index.sh` → OK (149 impostazioni, 12 tab).
- `scripts/installer-selftest.sh` → tutti i check passati.
- `go vet .` pulito; `gofmt -l` pulito.
- Verifica live sul daemon con dati reali: 16/16 voci `/v2?view=…` → 200 e
  **0 segnaposto non migrati**; integrazioni con Simkl;
  manutenzione con duplicati/db/ramdisk/rinomina/progresso/job; scarico con
  upload e tag; Esplora con calendario, tendenze e ricerca TMDB reali; anteprima
  rinomina reale (`9-1-1`: 16/16 già corretti); IT→EN con traduzioni reali.

Test v2 aggiunti (`uiweb_v2_test.go`, `uiweb_v2_widgets_test.go`):

1. shell, navigazione, assenza di `gextto-ui.js`;
2. Scarico + azioni (refresh, row action, ordinamento) + upload + tag in massa;
3. frammenti dettaglio/rimozione;
4. Configurazione: pagina, body, ricerca, salvataggio, chiave negata, editor
   strutturati (feed, checkbox, righe, rinomina, traduzioni) e modifica per chiave;
5. Log + asset statico + fallback no-JS + view sconosciuta;
6. tabelle generiche (6 viste), pannelli/form delle pagine libreria + library
   toggle + azione generica + path forgiato;
7. pannelli Manutenzione/Integrazioni + `HX-Redirect` 204 + cestino; widget
   duplicati, db, ramdisk, rinomina cartella, progresso e job;
8. cambio lingua persistito;
9. dettagli Serie/Film + metadati poster/cast + salvataggio serie + modale sorgenti + anteprima rinomina;
10. dettaglio torrent: tab tracker/file/peer/limiti/storage e azioni di modifica;
11. Fumetti: pannelli GetComics, weekly, storico, link finder/download e modifica;
12. traduzione HTML identica al client (testo/attributi/script/italiano);
13. Esplora TMDB: calendario, discovery, modalità giornaliera, prompt di ricerca vuoto, aggiunta con id mancante.

## 7. Benchmark (`go test -run '^$' -bench BenchmarkV2 -benchmem`)

CPU: Intel N97 · Go 1.26 · CGO on

| Benchmark | Tempo | Allocazioni |
| --- | --- | --- |
| `V2TranslateHTML` (pagina con 200 righe, dizionario 3.000 voci) | ~0,42 ms | 4.248 |
| `V2FormatCell` (6 colonne) | ~2,9 µs | 16 |
| `V2RenderTableFragment` (500 righe) | ~1,05 ms | 5.798 |
| `V2RenderShell` (shell + contenuto) | ~0,30 ms | 1.348 |

Costo di rendering server ~1–3 ms per pagina: trascurabile su LAN. La traduzione
è il percorso più costoso e vale solo per lingue ≠ IT.

## 8. Review logica e funzionale

**Correttezza**

- Le route UI non alterano il contratto API; `/` e `/v2` usano lo stesso handler.
- Le azioni generiche accettano solo path `/api/…` (test su path forgiato → non
  inoltrato).
- Il salvataggio impostazioni replica i vincoli già usati dalle API
  (`gh7_setting_key_allowed`, `validateBackendSetting`, secret vuoto non
  sovrascritto, JSON-array preservato) ed è coperto da test.
- `v2TranslateHTML` preserva `<script>`/`<style>` e con IT restituisce l'HTML
  byte-per-byte.
- Gli editor a righe salvano con la stessa forma della classica (wrap/postKey/
  array) e usano nomi `campo__indice` per non mescolare le righe.

**Punti di attenzione residui** (vedi §4). Nessun problema bloccante: build, vet,
gofmt e suite completa sono verdi.

## 9. Promozione a UI di default

La promozione è stata eseguita: `/` usa `V2Page` e `/v2` resta alias tecnico.

## 10. Rimozione della UI classica

Completata: sono stati rimossi template, JavaScript, CSS e route `/ui` della UI
precedente. Il CSS necessario è ora embedded in `uiweb/v2/static/`; i view-model
Go e gli handler API condivisi sono stati mantenuti.

## 11. Prossimi passi

Il porting copre tutte le voci di menu, i dettagli Serie/Film, il dettaglio
torrent, i flussi secondari dei fumetti e i widget speciali di
Manutenzione/Integrazioni; resta da completare solo la diagnostica feed del
Dashboard (vedi §4).
Passi successivi consigliati:

1. barra di avanzamento contestuale alla singola azione lunga (oltre al pannello
   job già presente);
2. esporre nel form eventuali campi specialistici ancora disponibili solo via API;
3. convertire i benchmark in test di performance in CI.
