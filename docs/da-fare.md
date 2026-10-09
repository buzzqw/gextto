# Da fare (backlog)

Punti aperti. Ripristinato l'8-10-2026: era finito per errore in `docs/archive/`
pur essendo un backlog **attivo**. I due item recuperati dal piano di adozione
(ora in `docs/scelte-di-progetto.md`) sono gli unici aperti.

## Aperti

### 1. Wizard di primo avvio (onboarding)
Il vecchio piano di adozione lo indicava come l'item a più alto impatto. Oggi
esistono solo `GET /api/setup` e `POST /api/setup/complete` (stato e conferma),
**nessuna UI guidata**. Piano dettagliato già scritto in
`docs/internal/SETUP_WIZARD_PLAN.md`.

Percorso in ordine, ogni passo con un test che dà un esito concreto: locale e
cartelle → backend di download (test di connessione) → indexer (URL + chiave del
manager, con una ricerca di prova) → radici della libreria (anteprima scansione)
→ naming (anteprima dal vivo) → media server (test) → notifiche (test) → primo
ciclo reale. Riprendibile, saltabile, dry-run di default.

### 2. Osservabilità e runbook
- ID di correlazione attraverso ricerca → download → import;
- metriche: profondità della coda, durata della ricerca, fallimenti di indexer e
  import, spazio disco, ultimo ciclo riuscito;
- la salute degli indexer del manager è già in Sources (`indexer_health.go`);
- un breve runbook "un ciclo è fallito — dove guardare", basato su Health e Log.

### 3. Pulsante "Ripristina predefinito" su ogni impostazione
Accanto a ogni campo delle impostazioni, un pulsante **predefinito** che riporta
il campo al valore di default "certo" (quello già in `uiweb_settings_defaults.go`
e nella meta per unità/valori speciali), senza salvare a sorpresa: il campo
mostra il default e l'utente conferma. Va esteso a **tutti** i campi e i pannelli
(Compilazione, Sorgenti, libtorrent, Motore torrent, Punteggi, Rinomina,
Avanzate, Notifiche, ...), così un utente che ha combinato un pasticcio sa come
tornare indietro. Serve anche definire un default certo dove oggi manca (es. i
valori speciali `-1`/`0` documentati) e il pulsante deve funzionare sia per i
campi semplici (testo/bool/select) sia per quelli strutturati (tag, percorsi).

## Opzionale (solo se si riprende il lavoro sulle prestazioni)

- Script di benchmark RAM/throughput dei motori, riproducibile in `scripts/`
  (workload noto, web seed locale con Range, staging locale vs NFS). Non serve
  di per sé: è utile solo per prendere decisioni future con numeri.

## Chiusi / decisioni

- **Log di upgrade unificati** — fatto (commit `988d60a`). Una sostituzione
  produce ora una sola riga, con il verbo e i nomi dei file sostituiti:
  ```
  ♻️ FBI S03E01 updated (1.4 GB): /SerieTV/FBI/S03/FBI.S03E01.1080p.mkv · previous version «FBI.S03E01.720p.mkv» moved to the trash
  ```
  L'aggiunta normale (nessuna sostituzione) resta `📁 … added to the library …`.
- **Pannello "Operazioni in background"** — risolto (commit `65cb9bf`). Il
  pannello resta e continua a funzionare (polling, annullamento), ma è **nascosto
  quando non c'è alcun job**: non occupa più spazio a vuoto.
- **Write-back cache nel fork di rain** — chiuso: la misura mostra
  `write_cache=0` (storage e download di pari passo), quindi non ci sarebbe
  guadagno. Da riaprire solo con uno storage realmente più lento della rete.
- **Campi specialistici nel form di aggiunta** — chiuso: nessuna richiesta
  concreta, sarebbe fuffa preventiva.
- **Benchmark UI come test di performance in CI** — chiuso: il rendering SSR
  costa ~1–3 ms/pagina, non è un problema.
- **Cap cache su storage lento (NFS)** — superato: la cache si autoregola
  (`cmd/gx-torrent/cache.go`, tuner `cacheSizes`).
- **Dashboard: diagnostica "Ultimi trovati nelle sorgenti"** — già fatto
  (route `GET /dashboard/feed`, `uiweb_v2.go`).
- **Verifica live del flusso RAM disk con gx-torrent** — fatto (2026-10-07).
