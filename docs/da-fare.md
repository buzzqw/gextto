# Da fare (backlog)

Punti aperti. Ripristinato l'8-10-2026: era finito per errore in `docs/archive/`
pur essendo un backlog **attivo**. Attualmente **non ci sono punti aperti**: gli
ultimi due sono stati chiusi l'8-10-2026. Quelli sotto restano come memoria delle
decisioni.

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
