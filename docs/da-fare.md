# Da fare (backlog)

Punti aperti, in ordine di rilevanza. Ripristinato l'8-10-2026: era finito per
errore in `docs/archive/` pur essendo un backlog **attivo**. Contiene solo ciò
che risolve un problema reale; il resto è annotato sotto come chiuso.

## Aperti

### 1. Il pannello "Operazioni in background" è quasi sempre vuoto
Il pannello è alimentato da **solo tre** job:
`scan-archives` (`web_handlers_g4.go:1234`), `rename-all`
(`web_handlers_g2.go:688`), `media-info-backfill`
(`web_handlers_g0.go:1363`). Tutte le altre operazioni lunghe della Manutenzione
girano **sincrone**, senza avanzamento né annullamento: `clean-duplicates`,
`housekeeping`, `clean-trash`, `rename-folder/apply`, `scan-archive` per serie,
`backup/verify`, refresh Jellyfin/Plex. Per questo, nell'uso normale, il pannello
dice sempre "Nessuna operazione in background", e le azioni lunghe non offrono
una barra contestuale.

Serve una decisione, poi l'intervento:
- **A** — portare al `JobManager` le operazioni lente (con progress e cancel);
- **B** — se non si vuole, mostrare il pannello solo quando c'è davvero un job
  (oggi è sempre presente, vuoto).

Quali operazioni meritano il porting è una scelta da fare: le più lente su NAS
sono `clean-duplicates`, `rename-folder/apply`, `scan-archive`, `clean-trash`,
`backup/verify`.

### 2. Log: unificare "sostituzione" e "aggiunto alla libreria"
Oggi due righe ravvicinate e ridondanti durante un upgrade:
- `cleaner.go:241` `logInferiorFileReplaced`: `🗑️ Replaced with a better version: «nuovo»; the old file «vecchio» …`
- `web_torrent_events.go:2454`: `📁 <serie> <ep> added to the library (X GB): <path>`

Vanno fuse in **una sola riga parlante** (episodio aggiornato, versione
precedente nel cestino, dimensione, percorso finale). Le due righe stanno in
moduli diversi: prima va tracciato il percorso di upgrade, poi emessa dove si
conoscono sia il vecchio sia il nuovo.

## Opzionale (solo se si riprende il lavoro sulle prestazioni)

- Script di benchmark RAM/throughput dei motori, riproducibile in `scripts/`
  (workload noto, web seed locale con Range, staging locale vs NFS). Non serve
  di per sé: è utile solo per prendere decisioni future con numeri.

## Chiusi / decisioni

- **Write-back cache nel fork di rain** — chiuso: la misura mostra
  `write_cache=0` (storage e download di pari passo), quindi non ci sarebbe
  guadagno. Da riaprire solo con uno storage realmente più lento della rete.
- **Campi specialistici nel form di aggiunta** — chiuso: nessuna richiesta
  concreta, sarebbe fuffa preventiva.
- **Benchmark UI come test di performance in CI** — chiuso: il rendering SSR
  costa ~1–3 ms/pagina, non è un problema; il job sarebbe processo a vuoto.
- **Cap cache su storage lento (NFS)** — superato: la cache si autoregola
  (`cmd/gx-torrent/cache.go:261`, tuner `cacheSizes`).
- **Dashboard: diagnostica "Ultimi trovati nelle sorgenti"** — già fatto
  (`uiweb/v2/templates/v2.html:264`, route `GET /dashboard/feed`, `uiweb_v2.go:131`).
- **Verifica live del flusso RAM disk con gx-torrent** — fatto (2026-10-07).
