# Da fare (backlog)

Punti aperti, in ordine di rilevanza. Ripristinato l'8-10-2026: era finito per
errore in `docs/archive/` pur essendo un backlog **attivo**. Qui restano solo i
punti ancora aperti; quelli chiusi sono nella storia di git.

## gx-torrent / rain

### 1. Write-back cache nel fork di rain
Far **usare più RAM** a gx-torrent quando lo storage è lento (HDD/NFS).
Serve una vera cache write-back nel fork: tenere i pezzi verificati in memoria e
scriverli in blocco, con **flush su stop/verify/move** e gestione sicura del
resume (un pezzo non scritto non deve risultare completo). Oggi rain è
write-through: `WriteCacheSize` è un tetto sui pezzi *in volo*
(`third_party/rain/torrent/session.go:233`), quindi la RAM resta bassa anche
alzando i cap (misurato). Cambiamento delicato; interseca la discussione su
`O_SYNC` (una write-back con flush a lotti è l'alternativa "pulita" alla
rimozione di `O_SYNC`).

### 2. Script di benchmark RAM/throughput dei motori, riproducibile
Committare in `scripts/` lo script usato per le misure di memoria dei motori
(workload da `torrent-done`, web seed locale con Range, staging locale vs NFS),
così è ripetibile senza ricrearlo ogni volta. Oggi in `scripts/` non c'è nulla
di benchmark; i micro-benchmark aggiunti al fork (`make test-rain` con `-bench`)
coprono il picker, non i motori end-to-end.

### 3. Log: unificare "sostituzione" e "aggiunto alla libreria"
Oggi due righe ravvicinate e ridondanti:
- `cleaner.go:246`: `🗑️ Replaced with a better version: «nuovo»; the old file «vecchio» is in the trash`
- `web_torrent_events.go:2454`: `📁 <serie> <ep> added to the library (X GB): <path>`

Vanno fuse in **una sola riga parlante** che dica: episodio aggiornato, versione
precedente spostata nel cestino, dimensione e percorso finale.

## UI (da `docs/archive/UI_V2.md`, §4 e §11)

### 4. Barra di avanzamento contestuale alla singola azione lunga
Esiste il pannello "Operazioni in background" e la barra della rinomina, ma non
una barra contestuale alla singola azione avviata.

### 5. Esporre nel form i campi specialistici oggi solo via API
L'aggiunta manuale (TMDB) è server-side con i campi principali; quelli più
specialistici restano accessibili solo via API.

### 6. Convertire i benchmark UI in test di performance in CI
`go test -run '^$' -bench BenchmarkV2` esiste ma non gira in CI: nessun
`Benchmark` nei workflow. Attenzione: i runner CI sono rumorosi, meglio un job
non bloccante o un controllo sulle allocazioni più che sul tempo.

## Chiusi / decisioni

- **Dashboard: diagnostica "Ultimi trovati nelle sorgenti"** — già fatto:
  pannello in `uiweb/v2/templates/v2.html:264` e route `GET /dashboard/feed`
  (`uiweb_v2.go:131`).

- **Cap cache su storage lento (NFS)** — superato: la cache ora si autoregola
  (`cmd/gx-torrent/cache.go:261`, tuner adattivo `cacheSizes`), quindi il "cap
  come rete di sicurezza" non serve più.
- **Verifica live del flusso RAM disk con gx-torrent** — fatto (2026-10-07):
  scaricato sul RAM disk e spostato a disco con `move`, file integri.
