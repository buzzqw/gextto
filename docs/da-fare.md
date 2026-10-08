# Da fare (backlog)

Punti aperti, in ordine di rilevanza. Ripristinato l'8-10-2026: era finito per
errore in `docs/archive/` pur essendo un backlog **attivo**. Contiene solo ciò
che risolve un problema reale; il resto è annotato sotto come chiuso.

## Aperti

### 1. Log: unificare "sostituzione" e "aggiunto alla libreria"
Oggi due righe ravvicinate e ridondanti durante un upgrade:
- `cleaner.go:241` `logInferiorFileReplaced`: `🗑️ Replaced with a better version: «nuovo»; the old file «vecchio» …`
- `web_torrent_events.go:2454`: `📁 <serie> <ep> added to the library (X GB): <path>`

Vanno fuse in **una sola riga parlante** (episodio aggiornato, versione
precedente nel cestino, dimensione, percorso finale). Le due righe stanno in
moduli diversi (`cleaner.go` vs `web_torrent_events.go`): prima va tracciato il
percorso di upgrade (`DiscardIfInferior*` → completamento), poi emessa dove si
conoscono sia il vecchio sia il nuovo.

Esempio di risultato finale:

```
♻️ FBI S03E01 aggiornato (1.4 GB): /SerieTV/FBI/S03/FBI.S03E01.1080p.mkv · versione precedente «FBI.S03E01.720p.mkv» nel cestino
📁 FBI S03E02 aggiunto alla libreria (1.1 GB): /SerieTV/FBI/S03/FBI.S03E02.1080p.mkv
```

La seconda riga è il caso normale (nessuna sostituzione) e resta com'è.

## Opzionale (solo se si riprende il lavoro sulle prestazioni)

- Script di benchmark RAM/throughput dei motori, riproducibile in `scripts/`
  (workload noto, web seed locale con Range, staging locale vs NFS). Non serve
  di per sé: è utile solo per prendere decisioni future con numeri.

## Chiusi / decisioni

- **Pannello "Operazioni in background"** — **rimosso** (2026-10-08): era
  alimentato da soli tre job (`scan-archives`, `rename-all`,
  `media-info-backfill`) e restava vuoto nell'uso normale, occupando spazio. Il
  `JobManager` e l'API `/api/jobs` restano; la rinomina globale ha già la sua
  barra dedicata (`Progresso rinomina`).
- **Write-back cache nel fork di rain** — chiuso: la misura mostra
  `write_cache=0` (storage e download di pari passo), quindi non ci sarebbe
  guadagno. Da riaprire solo con uno storage realmente più lento della rete.
- **Campi specialistici nel form di aggiunta** — chiuso: nessuna richiesta
  concreta, sarebbe fuffa preventiva.
- **Benchmark UI come test di performance in CI** — chiuso: il rendering SSR
  costa ~1–3 ms/pagina, non è un problema.
- **Cap cache su storage lento (NFS)** — superato: la cache si autoregola
  (`cmd/gx-torrent/cache.go:261`, tuner `cacheSizes`).
- **Dashboard: diagnostica "Ultimi trovati nelle sorgenti"** — già fatto
  (`uiweb/v2/templates/v2.html`, route `GET /dashboard/feed`, `uiweb_v2.go`).
- **Verifica live del flusso RAM disk con gx-torrent** — fatto (2026-10-07).
