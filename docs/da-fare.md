# Da fare (backlog)

Punti aperti. Ripristinato l'8-10-2026: era finito per errore in `docs/archive/`
pur essendo un backlog **attivo**. Dei due item recuperati dal piano di adozione
(ora in `docs/scelte-di-progetto.md`) resta aperta solo l'osservabilità.

## Aperti

### 1. Osservabilità: metriche nel tempo
ID di correlazione e runbook sono fatti (vedi sotto). Resta la parte metriche:
andamenti salvati nel database e mostrati in *Salute*, senza Prometheus:
- ~~durata delle ricerche e sorgenti che non rispondono, ciclo per ciclo~~ —
  fatto (2026-10-10): `CycleStats` salva durata ed esito per sorgente nel ciclo
  (`cycle_history`), con il pannello **Ricerche** in *Salute* (`uiweb.go`,
  `RecentCycleStats`, `ScrapeAll`);
- ~~avviso oltre una soglia~~ — fatto (2026-10-10): `cycle_monitor.go` avvisa
  (riga WARN + notifica) se una sorgente ha fallito in 3 delle ultime ricerche,
  se l'ultima ricerca supera 3 ore o se non c'è un ciclo senza errori da 3
  intervalli (minimo 2 ore), e segnala il ritorno alla normalità;
- restano: import falliti al giorno e titoli in coda con il tempo di attesa;
- la salute degli indexer del manager è già in Sources (`indexer_health.go`).

## Opzionale (solo se si riprende il lavoro sulle prestazioni)

- Script di benchmark RAM/throughput dei motori, riproducibile in `scripts/`
  (workload noto, web seed locale con Range, staging locale vs NFS). Non serve
  di per sé: è utile solo per prendere decisioni future con numeri.

## Chiusi / decisioni

- **ID di correlazione e storia dei download** — fatto (2026-10-10). Le righe di
  log su un torrent portano `acq: <id>` (ID corto derivato dall'hash, che resta
  nascosto) e finiscono in `acquisition_events`: scheda *Storia* nel dettaglio
  torrent, 📜 per puntata e pannello nella pagina della serie, API
  `/api/acquisitions` (`acquisition.go`).
- **Runbook "un episodio non è arrivato"** — fatto (2026-10-10), in
  *Risoluzione problemi* di `MANUAL.it.md`/`MANUAL.en.md`, costruito sulla storia.
- **Wizard di primo avvio (onboarding)** — fatto e giudicato concluso
  (2026-10-10). Procedura guidata in `uiweb_v2_setup.go` (commit `c8162d8`), poi
  TVDB come alternativa a TMDB e scelta della lingua al primo passo.
- **Pulsante «Predefinito» su ogni impostazione** — fatto. Ogni campo non segreto
  con un default registrato (anche vuoto: «no schedule», nessun bootstrap DHT)
  mostra il pulsante ↺ accanto a «Salva»: riempie il campo col default e l'utente
  conferma salvando; il valore del default è mostrato accanto se compatto
  (`Resettable`/`DefaultInline` in `uiweb_v2.go`, `HasDefault` in
  `uiweb_pages.go`). I campi disabilitati per il motore attivo, gestiti
  dall'auto-tuning, segreti o senza default non ce l'hanno.
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
