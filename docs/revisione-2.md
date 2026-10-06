# Revisione tecnica — Gextto

Analisi di bug, criticità, illogicità, loop logici e inefficienze.
Data: 2026-10-06 · Commit analizzato: `97142c1`.

## 1. Perimetro e metodo

**Letto per intero o quasi:** `orchestrator.go` (ciclo di acquisizione),
`web_background.go` (`torrentEventWorker`, `cycleWorker`), parti di
`web_torrent_events.go` (`HandleTorrentEvent`, `MonitorStalled`,
`EnforceSeedPolicy`), `database.go` (`checkSeriesScoredInner`), `sqlite.go`,
`config.go` (`LoadConfig`), `postprocess.go` (copia/spostamento), `cleaner.go`
(rimozione), `safety.go`, `web.go`/`web_router.go`.

**Secondo passaggio (§9-§10):** `engine.go`, `rss.go` (`FetchFeed`, dettaglio
magnet), `parser.go` (provato con titoli reali), `update.go`, `hooks.go`,
`notifier.go`, `httpx.go`, `comics.go` (`RunComicsCycle`, download), `watcher.go`,
`libtorrent.go` (sincronizzazione della sessione).

**Ancora non letto in dettaglio:** `websearch.go`, `config.go` (punteggio),
`archive.go`, `importer.go`, il bridge C++, `qbittorrent_engine.go` (solo
`List`/`sync`), `uiweb*`, TUI. Le conclusioni valgono solo per ciò che è stato
letto.

**Esito dei controlli automatici:** `go test ./...` passa (≈40 s); `go vet`
non segnala nulla nel codice Go (solo warning di deprecazione nel C++:
`half_open_limit`, `cache_size`). `staticcheck` e `deadcode` non sono
utilizzabili: il primo è compilato per un'altra versione di Go, il secondo
richiede un package `main`.

**Come leggere le severità.** Ogni voce indica anche il grado di certezza:
*Confermato* = verificato leggendo il codice; *Da verificare* = plausibile, ma
dipende da comportamenti runtime che non ho eseguito. Non ho riprodotto nulla
a runtime.

> Autenticazione e sicurezza di rete non sono l'oggetto dell'analisi: le note
> di sicurezza raccolte per strada sono nei §8 e §10, solo come promemoria.

---

## 2. Bug e criticità (priorità alta)

### B1 · Un percorso d'archivio non raggiungibile può azzerare lo stato degli episodi — *Da verificare, impatto alto*
`database.go` `checkSeriesScoredInner` (~riga 960). Se `archive_path` non esiste
(`errors.Is(statErr, os.ErrNotExist)`), la funzione esegue
`UPDATE episodes SET downloaded_at=NULL, archive_path=NULL, size_bytes=0,
media_info_json=''` e marca l'episodio come recuperabile. Il commento dichiara
che permessi ed errori di mount temporanei sono preservati. Con una NAS
**smontata** il mount point è di norma una directory vuota: `stat` restituisce
`ENOENT`, non un errore di I/O. Il guard non distingue "file cancellato" da
"volume assente".
- *Effetto:* ogni candidato valutato in quel momento perde lo storico (incluso
  `media_info_json`) e può essere riscaricato. Il danno è limitato ai candidati
  valutati nel ciclo, ma si ripete a ogni ciclo finché la NAS è giù.
- *Da verificare:* se esiste un controllo "radice archivio montata" a monte
  (non l'ho trovato nei file letti).
- *Correzione:* prima di resettare, verificare che la radice dell'archivio
  della serie esista e non sia vuota (o confrontare `st_dev` con la radice).
  Meglio ancora: non scrivere in una funzione di sola "check" (vedi I1).

### B2 · Errore transitorio di completamento = stato terminale e torrent staccato — *Confermato*
`web_background.go` (~riga 976): se `HandleTorrentEvent` fallisce su
`torrent_finished`/`storage_moved`, il worker chiama `MarkTorrentError`,
`RestoreUpgrade` e **`torrents.Remove(hash, false)`**, poi `MarkTorrentRemovedAt`.
Nessun retry. Un errore momentaneo (NAS lenta, ENOSPC temporaneo, DB occupato,
timeout `ffprobe`) trasforma un download completo in "error + rimosso dalla
sessione". Il file resta su disco, ma non c'è più alcun percorso automatico che
lo riprenda; l'episodio torna "mancante" e al ciclo successivo può essere
riscaricato in doppio (il file orfano resta nella cartella download).
- *Correzione:* distinguere errori permanenti (identità pack errata, file
  assente) da transitori; per i transitori, retry con backoff e stato
  `completion_pending` senza `Remove`.

### B3 · Torrent aggiunto al motore ma non registrato nel DB se la scrittura fallisce — *Confermato*
`orchestrator.go` (~riga 1000): dopo `torrents.AddWithPath(...)` riuscito, se
`db.RegisterTorrentScored` o `db.RemovePending` falliscono la funzione fa
`return nil, err` e **abortisce tutto il ciclo**. Il torrent è nella sessione ma
non ha riga `torrent_meta`; il placeholder dell'approvazione resta scritto.
Il worker eventi lo tratterà come torrent "esterno" (nessuna metadata →
completato ma non archiviato, vedi `HandleTorrentEvent`). Inoltre, i candidati
rimanenti del ciclo sono persi.
Incoerenza: l'errore di `Add` è gestito con `continue`; l'errore di
registrazione no.
- *Correzione:* in caso di fallimento registrare l'errore, `Remove` del torrent
  appena aggiunto (o retry della registrazione) e `continue`.

### B4 · `last_comics_check_ts` viene aggiornato anche se il ciclo fumetti fallisce — *Confermato*
`orchestrator.go` (~riga 85): se `RunComicsCycle` ritorna errore viene loggato e
`stats.Error("comics")` incrementato, ma `SetSetting("last_comics_check_ts", now)`
viene eseguito comunque. Con l'intervallo di default di 604800 s (7 giorni) un
singolo errore di rete sposta il successivo controllo di una settimana.
- *Correzione:* aggiornare il timestamp solo in caso di successo (o usare un
  intervallo di retry breve).

### B5 · Errori del DB fumetti abortiscono cicli che non riguardano i fumetti — *Confermato*
`orchestrator.go` (~righe 60-80): `comics.Setting(...)` viene letto **sempre**,
anche con dominio `series` o `movies`, e un errore fa `return nil, err`. Un
problema sul DB fumetti blocca quindi il ciclo serie/film. Lo stesso per
`SetSetting`. Le letture vanno fatte solo se il dominio le richiede e gli
errori devono degradare (log + skip), non abortire.

### B6 · Il ciclo si ferma per "poco spazio" senza salvare le statistiche — *Confermato*
`orchestrator.go` (~riga 740): con `min_free_space_gb` attivo e spazio sotto
soglia (o non determinabile) si fa `return stats, nil` **senza `db.SaveCycle`**
e senza il "CYCLE REPORT". L'errore `min_free_space` viene registrato in `stats`
ma non persistito: la UI/`/api/cycles` non mostrano mai perché non si scarica.
Inoltre il controllo è fatto sul volume predefinito, prima di sapere dove andrà
il download: una regola per tag, l'overflow RAM disk o un percorso diverso possono
puntare a un volume con spazio, ma il ciclo è già stato interrotto. Il controllo
per-percorso più sotto (`downloadPathFreeSpaceFloor`) rende quello globale
ridondante e, in parte, contraddittorio.
- Si perdono anche tutte le fasi successive (pending ready, svuotamento ecc.).

### B7 · Configurazione ricaricata male nel worker eventi — *Confermato*
`web_background.go` (~riga 672-679): se `LoadConfig` fallisce, `cfg = fallback`
cioè la **configurazione di avvio**, non l'ultima valida. Un errore momentaneo
di lettura (file/DB in scrittura, parse) fa ripartire il worker con
impostazioni vecchie (percorsi, seed policy, auto-remove, limiti di velocità)
fino al ricaricamento successivo (≤ 60 s). In particolare
`cfg.Libtorrent.AutoRemoveCompleted` e i percorsi di destinazione possono
cambiare "all'indietro" mentre si completano download.
- *Correzione:* tenere `cfg` precedente in caso di errore e loggare.

### B8 · Rename repair eseguito fuori da `cycle_lock` — *Confermato*
`web_background.go` (~riga 1730): il repair dei nomi parte in una goroutine
separata (`seriesRenameApply(... execute=true ...)`) e `cycle_lock` viene
rilasciato subito dopo. Il repair continua quindi mentre il ciclo successivo
può partire, mentre arrivano eventi di completamento e mentre l'utente
lancia rinomine manuali. La sua guardia (`renameRepairRunning`) impedisce solo
repair sovrapposti **tra loro**, non con le altre operazioni sull'archivio:
rischio di rinomine/spostamenti concorrenti sullo stesso file.
Il commento giustifica la goroutine con la lentezza dell'NFS, ma l'effetto è
rinunciare alla mutua esclusione.

---

## 3. Illogicità e codice incoerente

### L1 · Espressione booleana ridondante nella selezione dei candidati — *Confermato*
`orchestrator.go` (~riga 640):
```go
condition := ((complete && oldComplete) ||
    (!complete && oldComplete && incumbentWins(...)) ||
    (!complete && !oldComplete && intSetSuperset(...))) &&
    incumbentWins(...)
```
Il secondo termine contiene già `incumbentWins`, ripetuto poi nel `&&` esterno:
la chiamata interna è inutile (e `incumbentWins` viene valutato fino a 2 volte).
Il vero significato è "vince l'incumbent E (uno dei tre casi di copertura)".
Suggerisce che la logica sia cresciuta per patch; va riscritta con un
nome esplicito (`covers(old, new)`), e coperta da test tabellari.

### L2 · Asimmetria tra "supersede" e "remove" nella selezione — *Da verificare*
Nello stesso blocco, la regola che decide se un nuovo candidato è scartato
(`superseded`) e quella che decide se rimuove gli esistenti (`remove`) usano
condizioni non speculari (la seconda guarda `len(rangeSet) > 1` e
`(!oldComplete || complete)`, la prima no). Il risultato dipende dall'ordine di
arrivo dei release: due ordini diversi dello stesso insieme possono produrre
selezioni diverse. Non ho eseguito un test di permutazione; consiglio un test
di proprietà (shuffle dell'input → stesso output).

### L3 · Una funzione `Check*` che scrive — *Confermato*
`checkSeriesScoredInner` / `checkMovieScoredWith` fanno `INSERT OR IGNORE INTO
series`, `UPDATE episodes` (reset per file mancante), scrivono il backup di
upgrade e il placeholder dell'approvazione. Nome e comportamento divergono;
`rollbackReleasePlaceholder`/`RollbackRelease` esistono proprio per rimediare
agli effetti collaterali quando il passo successivo fallisce (spazio, Add
fallito). Il modello "approva scrivendo, poi annulla" è fragile (vedi B3) e il
dry-run richiede un flag diffuso (`dryRun`) in decine di rami.

### L4 · `deep gap pass`: il timestamp è scritto prima del lavoro — *Confermato*
`orchestrator.go` (~riga 410): `last_deep_gap_fill_ts` viene salvato **prima**
di eseguire le ricerche online. Se il ciclo è annullato o fallisce dopo quel
punto, il passaggio "deep" è comunque considerato fatto e la prossima
occasione arriva dopo `gap_deep_interval_hours` (6 h di default). Inoltre
`MarkGapSearched` viene chiamato per tutti i candidati anche se `ctx` è stato
annullato a metà (le ricerche tornano vuote e il gap risulta "cercato" per 23 h).

### L5 · Due soglie temporali non coordinate per i gap — *Confermato*
`gap_deep_interval_hours` (6) vs `GapRecentlySearched(..., 23)` (hard-coded). Con
`deepMaxPerCycle=5` e molti gap, i primi 5 vengono cercati nel pass deep,
marcati per 23 h, poi il pass successivo (6 h dopo) salta quei 5 e prende i
5 successivi: ordine implicito dell'elenco e nessuna priorità/fairness. In
pratica una serie con molti gap può essere servita a ondate lente e il valore
"23" non è configurabile.

### L6 · Stato "completato" per eventi diversi che duplicano il lavoro — *Da verificare*
Il worker reagisce sia a `torrent_finished` sia a `storage_moved` con lo stesso
blocco (notifica `torrent_completed`, `SizeOfPath`, `ProbeResult`, ecc.).
Esistono guardie (`tevIgnoreRepeatedCompletion`, `tevSeedingCompletionAlreadyRecorded`)
ma sono solo nella branch `torrent_finished` di `HandleTorrentEvent`; il blocco
notifica/ffprobe nel worker viene eseguito ogni volta che `processed == true`.
Da verificare che `storage_moved` dopo un `torrent_finished` già elaborato torni
`processed=false`, altrimenti notifica e `ffprobe` sono doppi.

### L7 · Lingue mescolate nei log e nelle notifiche — *Confermato, minore*
`startedDetail` contiene "qualità", "episodi" in italiano mentre il resto del
log è in inglese; `Origin: "Feed RSS"`, `"Archivio"`, `"Indexer / web"` sono
valori di dato in italiano usati come chiavi di confronto
(`result.Origin == "Feed RSS"` in `web_handlers_g7.go`). Cambiare la lingua o
rinominare l'etichetta rompe la logica.

---

## 4. Loop logici e rischi di ciclo

### C1 · Loop di ripartenza del worker eventi dopo un panic — *Da verificare*
`safeGoLoop` riavvia il worker dopo un panic, ma `torrentEventWorker` rifà tutta
l'inizializzazione (riconteggio di `startupHashes`, `ForceRecheck` dei torrent
"completi ma non archiviati") a ogni riavvio. Se un evento "veleno" (es. dato
corrotto che causa panic in `HandleTorrentEvent`) viene drenato da `PollEvents`
prima del panic, l'evento è perso; se il panic dipende dallo stato del torrent
(non dall'evento), il worker entra in un ciclo panic→restart→panic con
`workerRestartDelay`. La protezione `recentlyRechecked` mitiga il solo
ForceRecheck. Mancano contatori/backoff crescente e un circuit breaker.

### C2 · Ricostruzione degli eventi a ogni tick — *Confermato*
Il worker, ogni 750 ms, per ogni torrent completo e non "attivo" ricostruisce
un evento `torrent_finished`/`storage_moved` consultando il DB
(`TorrentMeta` + `TorrentStatus`). Evita perdite di eventi, ma crea una
macchina a stati "per polling" parallela a quella a eventi: due fonti di
verità. Il risultato corretto dipende da `status != completed/error/removed`
e da tre mappe in memoria (`moveRequests`, `storageMoveRetries`,
`postSeedMoves`) che non sopravvivono a un riavvio. Un `storage_moved` perso
con la mappa vuota viene ricostruito, ma il percorso dipende da euristiche
(`bg_torrentNeedsArchiveImport`).

### C3 · Esecuzione sincrona di lavoro lento dentro il loop dell'unico worker — *Confermato*
Notifiche HTTP/SMTP/Telegram, `SizeOfPath` (su NFS), `ProbeResult` (`ffprobe`),
`RefreshMediaLibraries` (HTTP verso Jellyfin/Plex) e le rinomine di
post-processing sono eseguite **inline** nel loop che tiene anche
`MonitorStalled`, `MonitorMetadata`, `RetryStorageMoves`, `EnforceSeedPolicy` e
la riapplicazione dei limiti di banda. Un server di notifica lento o una NAS
bloccata fermano tutta la supervisione per la durata. Non ho visto timeout per
`SizeOfPath`. È il tipico anello "una dipendenza lenta rallenta tutto".

---

## 5. Inefficienze

### I1 · `DELETE` su SQLite per ogni torrent a ogni tick — *Confermato*
`MonitorStalled` (`web_torrent_events.go` ~righe 620-660) esegue
`db.DeleteStallWatch(hash)` per **ogni torrent non in stallo** a ogni tick
(750 ms): sia per i torrent in `seeding/paused/…` (branch iniziale) sia per
quelli che stanno scaricando con progresso normale. Il database ha una sola
connessione (`SetMaxOpenConns(1)`): ogni scrittura serializza anche le query
della UI e del ciclo. Con N torrent sono N DELETE ogni 0,75 s, quasi sempre
senza effetto.
- *Correzione:* cancellare solo se l'hash è presente in `watch`/era persistito.

### I2 · Query SQLite per ogni torrent completo a ogni tick — *Confermato*
Vedi C2: per ogni torrent seeding (progress ≥ 99.99, non in move) il worker
chiama `db.TorrentMeta` e `db.TorrentStatus` ogni 750 ms. Con centinaia di
torrent in seeding sono centinaia di query ogni tick. Si può cachare lo stato
"già archiviato" in memoria con invalidazione sugli eventi.

### I3 · `torrents.List()` chiamata ~10 volte per tick — *Confermato, mitigato*
Il loop principale e ogni funzione chiamata (`MonitorMetadata`, `MonitorStalled`,
`RetryStorageMoves`, `DetachErrorTorrents`, `DetachCompletedArchivedSingles`,
`RemoveSeededCompleted`, `EnforceSeedPolicy` ×2, …) richiamano `List()`. Il
backend libtorrent ha una cache da 500 ms e qBittorrent un throttle sul
`PollInterval`, quindi l'impatto di rete è contenuto, ma ogni chiamata copia
l'intero slice (e qBittorrent la ordina con `sort.Slice` a ogni chiamata).
Meglio acquisire uno snapshot per tick e passarlo alle funzioni.

### I4 · `LoadConfig` pesante invocata da molti punti — *Confermato*
`LoadConfig` esegue `MigrateLegacyFiles` e `loadConfigDB` a ogni chiamata
(9 chiamate: worker eventi, cycle worker, optimize, media, 3 handler, `web.go`).
Il cycle worker la richiama anche nel loop di attesa post-riavvio ogni 60 s.
Funziona, ma ogni worker mantiene la propria copia: due worker possono lavorare
con configurazioni diverse nello stesso istante (cfr. B7).

### I5 · Stat di tutta l'archiviazione solo per un log di debug — *Confermato*
`cycleWorker` (ramo `RenameEpisodes == false`): sotto `cycle_lock` esegue
`os.Stat` su **ogni** file archiviato (NFS) per calcolare `missing` che finisce
soltanto in `logging.Debug`. Costo I/O alto, nessun effetto funzionale, e blocca
il ciclo successivo.

### I6 · Selezione dei candidati O(n²) con ricalcolo ripetuto — *Confermato*
`orchestrator.go` ~riga 620-700: per ogni release in ingresso si scorre `best` e
si ricalcolano `episodeSet`/`hasCompleteRange` (allocazione di mappe) per ogni
coppia; in più `ReleaseScore` viene ricalcolato per i log dei candidati.
Con feed grandi (archivio + RSS + pending + gap) il costo cresce
quadraticamente. Indicizzare `best` per `(serie, stagione)` e precalcolare gli
insiemi.

### I7 · Ricerche d'archivio una per query — *Da verificare*
Una `archive.Search` per ogni serie/alias/film, e una per ogni gap
(`"Serie SxxExx"`), tutte sequenziali e sulla stessa connessione SQLite
singola. Non ho misurato; con migliaia di gap può dominare il ciclo "gratis".

### I8 · `RecordSeenBatch` ignora silenziosamente gli errori — *Confermato, minore*
Errore → `logging.Debug`. Un problema persistente non è visibile a livello INFO/WARN.

---

## 6. Debito strutturale (manutenibilità)

| # | Evidenza | Rischio |
|---|---|---|
| S1 | `RunCycleDomain` ≈ 1100 righe, `torrentEventWorker` ≈ 600, `seriesRenameApply` ≈ 380 | Impossibile testare per unità; B3/B4/B5/L4 nascono da qui. |
| S2 | Codice duplicato `gh2_*` (g2) e `gh7_*` (g7): `gh2_storedSeriesEpisodeSources` vs `gh7_stored_series_episode_sources`, `gh7_release_matches_series_episode` vs `releaseMatchesConfiguredEpisode` | Le due copie divergono; la sola `EpisodeSources` instradata usa la g7. `gh2_storedSeriesEpisodeSources` è ancora usata da `SeriesSearchMissing` (g2:970), mentre `EpisodeSources` usa la copia g7: stessa logica, due implementazioni. |
| S3 | `web_handlers_g0…g7` "portati dal progetto di riferimento" | Nomi senza significato di dominio; helper con prefissi diversi (`gh2_`, `gh7_`, `bg_`, `tev_`) che replicano utility (`find_series`, `contains_int64`, `optionalInt64Equal` presente in 3 forme). |
| S4 | Rotte duplicate: `/api/config/add_all_from_archive` e `/add-all-from-archive`; `/api/cycles`, `/api/cycle-history`, `/api/last_cycles` → stesso handler; `/api/log-level` vs `/api/log_level` | Superficie API doppia da mantenere. |
| S5 | Parsing impostazioni sparso: `value == "yes" \|\| "true" \|\| "1"` ripetuto in decine di punti (gap_filling, sequential, temp limit, …) con varianti (`ToLower` o no) | Comportamento diverso per `"True"`/`"YES"`. Esempio: `gap_filling` non fa `ToLower`, `libtorrent_sequential` sì. |
| S6 | Gestione errori con `_ =` in 81 punti (`_ = db.SetTorrentReason`, `_ = db.MarkGapSearched`, `_ = SaveSetting(...)`) | Fallimenti silenziosi, stato non allineato. |
| S7 | Tre mappe di stato in memoria nel worker (`moveRequests`, `storageMoveRetries`, `postSeedMoves`) | Perse al riavvio; ricostruite per euristica (C2). |
| S8 | File di traduzione `internal_translations_*.yml` da 3800 righe ciascuno | Rischio di chiavi disallineate: esiste `i18n_catalog_test.go`, ma non l'ho letto. |

---

## 7. Cose che ho verificato e risultano a posto

- Nessun deadlock evidente con `SetMaxOpenConns(1)`: dentro le transazioni
  (`Begin`) le funzioni usano `tx` e non `d.db` (controllo meccanico su
  `database.go`).
- Copia atomica con file temporaneo, `fsync`, controllo di dimensione e
  `os.OpenRoot` (`copyFileAtomically`). Limite: nessun checksum; `os.Rename`
  sovrascrive un eventuale target esistente.
- Gestione del backoff del backend qBittorrent (throttle anche sugli errori).
- Fail-closed sulla blocklist: se non leggibile il ciclo non parte.
- SQL parametrizzato (solo letterali interni nella SQL dinamica letta).

---

## 8. Note di sicurezza residue (promemoria)

- Un'interfaccia senza autenticazione e **senza controllo di `Origin`/CSRF**
  è potenzialmente raggiungibile da qualunque pagina web aperta nel browser di un
  utente in LAN (richieste cross-site) e da attacchi DNS rebinding. Non ho
  trovato nei file letti alcun controllo di `Content-Type`/`Origin` sui `POST`
  (i body sono letti con `io.ReadAll`), ma non ho provato lo sfruttamento. L'handler `SaveConfigRoot`/hook eventi può eseguire comandi:
  l'esposizione effettiva è quindi "esecuzione di comandi dalla rete locale",
  non solo "UI amministrativa". Mitigazione a basso costo: rifiutare richieste
  con `Origin`/`Host` non attesi.
- `copyFileAtomically` sovrascrive il target senza avvisare: dipende dalla
  correttezza dei chiamanti.

---

## 9. Secondo passaggio — parser, feed, notifiche, hook, fumetti

### P1 · Il parser sbaglia titoli reali comuni — *Confermato (eseguito su titoli di prova)*
Ho eseguito `ParseRelease` su 18 titoli con un test temporaneo (poi rimosso).
Risultati errati:

| Titolo | Risultato | Problema |
|---|---|---|
| `Movie.1917.2019.1080p.WEB-DL` | anno **1917** | il numero nel titolo è preso per l'anno |
| `2001.A.Space.Odyssey.1968.1080p.BluRay` | anno **2001** | idem (vale per 1984, 2012, 2010…) |
| `Show.Name.2024.05.12.1080p.WEB` | serie, **S=2024 E=133** | episodio giornaliero letto come "giorno dell'anno"; episodio inesistente nel DB |
| `Show.Name.S2024E05.1080p` | **movie**, nessuna serie | stagione a 4 cifre non riconosciuta |
| `Show Name - Stagione 2 Episodio 3 ITA` | **movie** | formato verbale italiano non riconosciuto |
| `Show.Name.2160p.S02E10.HDR.DV.ATMOS` | serie `"Show Name 2160p"` | il token di risoluzione resta nel nome serie |
| `Show.Name.S01-S03.1080p.BluRay` | S=1, E=0, `EpisodeRange=[0]`, pack | multi-stagione trattato come stagione 1; "episodio 0" fittizio |
| `Show.Name.S01.COMPLETE…` | E=0, `EpisodeRange=[0]` | un pack ha l'episodio **0** (che è anche un numero valido per gli speciali) |
| `…WEB-DL` | gruppo **"dl"** | il suffisso `-DL` è preso per release group |
| `…WEBRip.x265.10bit`, `…2024.05.12.1080p` | audio **5.1** | il pattern "5.1" si accende su `x26[5.1]0bit` e `0[5.1]2`: audio inventato |
| `…HDCAM.XviD` | risoluzione **720p** | risoluzione inventata per un CAM |

*Effetti:* l'anno errato (primi due casi) può far fallire il match con il film
monitorato (`MovieConfig.Year`) → release scartata per sempre; il caso
giornaliero crea "episodi" S2024E133 e quindi falsi gap; il falso 5.1
distorce il punteggio audio; `EpisodeRange=[0]` può far credere che un pack
copra l'episodio 0 (`releaseHasEpisode`/`episodeSet`, §3 L1/L2).
*Da verificare:* quanto `SeriesNamesMatch`/`tokensMatchWithReleaseSuffix`
assorba il caso "Show Name 2160p" (la funzione ha una lista di suffissi).
*Correzione:* test tabellari con questi titoli come regressione;
anno = ultima occorrenza plausibile prima dei tag tecnici; supporto
`SxxxxEyy`/data giornaliera/"Stagione N Episodio M"; regex audio con confini
(`\b5\.1\b` e non preceduta/seguita da cifre).

### P2 · `FetchFeed` perde o conserva pagine in modo incoerente — *Confermato*
`rss.go` ~riga 1230: se una pagina successiva fallisce al **download** si fa
`break` e si tengono le pagine già raccolte; se fallisce il **parsing**
(`fetch_traditional_listing`) si fa `return nil, err` e si buttano tutte.
Inoltre l'errore sul download non viene nemmeno loggato.

### P3 · Paginazione presumibilmente a base 0 — *Da verificare*
La pagina 0 è l'URL originale, la 1 aggiunge `page=1`. Su siti con paginazione a
base 1 la "pagina 1" è la stessa della 0: scatta l'early-stop "nessun nuovo
infohash" e il crawl non va mai oltre la prima pagina. Dipende dal sito.

### P4 · Tipo di sorgente scelto da una sottostringa dell'URL — *Confermato*
`FetchFeed` sceglie il parser con `strings.Contains(lower, "ext.to"|"extto"|
"corsaro"|"torrentgalaxy")` sull'**intero** URL (query inclusa). Un feed
qualunque con una di queste stringhe nel percorso o nella query usa il parser
HTML sbagliato.

### P5 · Cache dei magnet indicizzata solo dal titolo — *Confermato*
`fetch_detail_magnets` usa `cache.Get(item.title)`: due release con lo stesso
titolo (uploader diversi, repack con nome identico, sorgenti diverse)
condividono il magnet in cache → si scarica il torrent sbagliato. I risultati
dalla cache non contano nel calcolo "vecchi/totali", quindi falsano anche
l'early-stop per età.

### P6 · Telegram: retry senza attesa iniziale e attesa finale inutile — *Confermato*
`notifier.go` ~righe 233-242: il secondo tentativo parte **subito**, e
`time.Sleep` avviene dopo la ritentata fallita (1 s, poi 2 s) quando non c'è
più nulla da ritentare. Il ramo webhook invece dorme *prima* del retry:
due implementazioni diverse della stessa politica.

### P7 · Notifiche sincrone senza timeout: `smtp.SendMail` può bloccare per sempre — *Confermato*
`NotifyEvent` invia Telegram, webhook ed email **in sequenza nel thread
chiamante** (worker eventi e ciclo). `smtp.SendMail` non ha timeout (usa un
`net.Dial` senza limite): un host SMTP che non risponde congela il worker
eventi (e con esso stall monitor, seed policy, limiti di banda) a tempo
indeterminato. Telegram/webhook hanno il solo timeout di 90 s del client, per
3 tentativi. Conferma di §4 C3 con la causa concreta. Inoltre, un errore di
serializzazione del webhook fa `return err` e salta l'email.

### P8 · Il token Telegram finisce nei log — *Confermato*
L'URL è `https://api.telegram.org/bot<TOKEN>/sendMessage`. `HTTPRequest`
restituisce l'errore grezzo di `net/http` (`*url.Error`, che contiene l'URL
completo) e i chiamanti lo scrivono nei log
(`"completion notification failed", "error", err`). `RedactURLSecrets` oscura
soltanto `apikey=`/`api_key=` e il modulo `logging` non redige nulla da solo:
il token nel **percorso** (e i segreti nei percorsi dei webhook Discord/Slack)
non vengono mascherati. Contraddice `SECURITY.md` ("No secrets in logs").

### P9 · Hook: variabili espanse *prima* dello split degli argomenti — *Confermato*
`RunHook`: `args := SplitArgs(Expand(hook.Args, vars))`. Il valore di
`{title}`, `{name}`… (preso da feed non fidati) viene sostituito nella stringa
e poi diviso per spazi/virgolette: un titolo con spazi diventa più argomenti, e
uno con `"`, `'` o `--opzione` rompe il quoting o inietta argomenti. Non c'è
shell, ma l'argument injection verso lo script configurato è possibile.
*Correzione:* tokenizzare prima, poi espandere ogni token separatamente.

### P10 · Hook: timeout inefficace con processi figli, nessun limite di concorrenza — *Confermato*
- `exec.CommandContext` uccide solo il processo diretto; senza `cmd.WaitDelay`,
  `cmd.Output()` resta in attesa finché un eventuale nipote tiene aperta la
  pipe: il "timeout" non scade davvero.
- Il timeout può arrivare a 86 400 s (24 h).
- `Dispatch` lancia una goroutine per evento e gli hook di quell'evento girano
  in serie: un hook bloccato ritarda tutti gli altri; nessun limite al numero
  di processi in parallelo se gli eventi arrivano a raffica.
- La goroutine non ha `recover`: un panic in `Variables`/`walkVariables` (che
  attraversa `map[string]any` arbitrari) termina l'intero daemon.

### P11 · Nessun `recover` nelle goroutine di fan-out — *Confermato*
Ci sono solo 4 `recover()` nel codice non di test (`safety.go`, `jobs.go`). Le
28 `go func` non coperte da `safeGo` includono i fan-out di `engine.go`
(feed, ricerche), `orchestrator.go` (gap-fill), `rss.go` (dettagli, Prowlarr),
`websearch.go`, `hooks.go`. In Go un panic in una goroutine **non catturata
termina il processo**: un parser alle prese con HTML/XML di un feed ostile o
malformato può quindi abbattere il daemon, mentre `safeGoLoop` protegge solo i
worker top-level.

### P12 · Ciclo fumetti: download sincroni e non annullabili dentro il ciclo principale — *Confermato*
`RunComicsCycle` non riceve `context.Context`. I download diretti
(`DownloadDirect` → HTTP) e quelli Mega (`exec.Command(...).Run()`, senza
timeout né contesto) sono eseguiti **inline** e il ciclo fumetti gira
*prima* di serie e film, sotto `cycle_lock`. Un file grande o un `megadl`
bloccato ritarda l'intero ciclo serie/film e non è interrompibile allo
spegnimento (nonostante i controlli `cycleCancelled`). La versione
asincrona, `StartDirectDownload*`, esiste ma è codice morto (non raggiunto).
In più `downloadMegaInner` individua il file scaricato come "il primo file
nuovo nella cartella rispetto a una foto precedente": se nella stessa cartella
scrive un altro download, può restituire il file sbagliato.

### P13 · Due parser HTML — *Confermato*
`comics.go` contiene ~700 righe di parser HTML e selettori CSS scritti a mano
(`parseHTML`, `parseCSSCompound`, `matchCSSGroup`…), mentre `rss.go` usa
`golang.org/x/net/html` (già dipendenza). Doppia manutenzione e, per quello
artigianale, rischio di comportamenti divergenti o di ricorsione profonda su
HTML ostile.

### P14 · Aggiornamento: la verifica del checksum "fallisce in aperto" — *Confermato*
`update.go` `verifyRemoteChecksum`: se il `.sha256` non è raggiungibile,
risponde con un errore HTTP o ha un formato non valido, stampa "not published
by this release (continuing)" e **procede**. Un errore transitorio sul solo
file checksum disattiva la verifica; inoltre il checksum proviene dalla stessa
origine dell'archivio, quindi protegge dalla corruzione ma non dall'autenticità
(nessuna firma). L'aggiornamento poi sostituisce il binario ed esegue il
riavvio del servizio, di solito come root. `tar -xzf` non usa
`--no-same-owner`/`--no-same-permissions`: da root conserva proprietario e bit
speciali dell'archivio.

### P15 · Sessione libtorrent letta senza lock in ~40 metodi — *Confermato, impatto basso*
Solo `listUncached` e `Shutdown` prendono `sessionMu`; altri ~40 metodi
(`Remove`, `PollEvents`, `SetPin`, `Add*`, `MoveStorage`…) leggono `c.session`
e chiamano cgo senza lock, mentre il commento in `Shutdown` avverte che "una
chiamata cgo su sessione distrutta abortisce il processo". L'ordine di
spegnimento (server HTTP, poi worker, poi sessione) lo rende raro; resta una
finestra se `server.Shutdown` scade (10 s) con handler ancora attivi, perché i
suoi errori sono ignorati. Il cambio di backend a runtime non distrugge la
sessione, quindi non è un rischio.

### P16 · Watcher: nessun controllo di stabilità/dimensione — *Confermato, minore*
`watcher.go` scarta solo `.part`/`.tmp`/file nascosti; un `.torrent` ancora in
copia può essere letto troncato (fallisce l'aggiunta, ma può generare errori e
tentativi ripetuti), e `MagnetFromFile`/lettura non hanno un limite di
dimensione. `visit` ricorsiva senza profondità massima e con nome generico
(`visit`, `is_candidate`) nello stesso package condiviso da 130 file.

### P17 · Stile: convenzioni di naming miste — *Confermato, minore*
Funzioni in `snake_case` (`parse_feed_body`, `fetch_body`, `local_name`,
`is_candidate`, `host_semaphore`) accanto a `camelCase`; prefissi di gruppo
(`gh2_`, `gh7_`, `bg_`, `tev_`). Il package unico espone nomi generici
(`visit`, `attribute`, `deref`, `Run`, `Expand`).

---

## 10. Altre note di sicurezza emerse (promemoria)

- **P8** (token Telegram nei log) e **P14** (checksum che fallisce in aperto)
  sono le due più concrete di questo passaggio.
- **P9**: l'argument injection negli hook dipende da dati controllati da terzi
  (titoli di feed).
- Nessuna restrizione SSRF su `HTTPRequest`/`indexer/test`: coerente con l'uso
  in LAN, ma da tenere presente se la UI viene esposta.

---

## 11. Piano d'intervento suggerito (aggiornato)

1. **Subito (correttezza dei dati e stabilità):** B1, B2, B3, B7, B8, P1
   (parser), P11 (recover nelle goroutine), P7 (timeout SMTP).
2. **Rapidi e a basso rischio:** B4, B5, B6, I1, I5, L1 (riscrittura con test),
   P2, P6, P8 (redazione del token), P9 (tokenizza prima di espandere), P10
   (`WaitDelay` + tetto di concorrenza), P14 (fail-closed sul checksum).
3. **Medio termine:** P5 (chiave cache = titolo+origine/hash), P12 (fumetti
   asincroni con `context`), P13, I2/I3 (snapshot per tick e cache stato), C3 (coda per
   notifiche/ffprobe fuori dal loop), S5 (un solo parser booleano), S2/S3/S4.
4. **Strutturale:** spezzare `RunCycleDomain` e `torrentEventWorker` in fasi con
   input/output espliciti; rendere "approva" senza effetti collaterali e una
   fase `commit` separata (elimina B3/L3).
5. **Test mancanti consigliati:** permutazione dell'ordine in `best` (L2);
   NAS smontata (B1); fallimento di `RegisterTorrentScored` dopo `Add` (B3);
   panic ripetuto del worker (C1); tabella dei titoli di P1 come test di
   regressione del parser; hook con titoli contenenti spazi e virgolette (P9).

## 12. Domande aperte

1. **B1:** esiste altrove un controllo che l'archivio sia montato prima di
   toccare il DB? Se sì, indicatemi dove.
2. **B2:** è voluto che un errore di completamento sia terminale (nessun retry
   automatico)? In caso contrario, qual è lo stato preferito (`completion_pending`)?
3. **L6:** una `storage_moved` dopo `torrent_finished` deve notificare di nuovo?
4. **B8:** il repair in goroutine fuori lock è una scelta consapevole per la
   lentezza NFS? Se sì, valutare un lock dedicato per-serie anziché nessuno.
5. **P1:** i formati `Stagione N Episodio M` e `SxxxxEyy` devono essere
   supportati? Per i film con titolo numerico (1917, 2012…) com'è il match con
   `MovieConfig.Year`?
6. **P14:** è accettabile rendere obbligatorio il `.sha256` (fail-closed)?
   Ci sono release storiche che non lo pubblicano?

---

## 13. Stato delle correzioni (2026-10-06)

Decisioni sulle domande del §12: 1 → controllo aggiunto; 2 → retry dei soli
errori transitori; 3 → la doppia notifica è un errore; 4 → esclusione per
serie; 5 → correzione del parser; 6 → checksum **non** modificato.

| Voce | Stato | Dove | Test |
|---|---|---|---|
| B1 | Corretto | `database.go` `archiveFileConfirmedMissing` | `archive_mount_test.go`, `TestMissingArchivedEpisodeIsEligibleForRecovery` |
| B2 | Corretto | `web_background.go` `isTransientCompletionError`, `completionRetries` | `completion_retry_test.go` |
| L6 | Corretto | `web_background.go` (`alreadyAnnounced`, `completionNotified`) | — (logica nel loop del worker) |
| B8 | Corretto | `web.go` `TryAcquireArchiveRename`, `AcquireArchiveImport`; `seriesRenameApply` | `archive_rename_lock_test.go` |
| P1 | Corretto in parte | `parser.go` | `parser_regression_test.go` |
| P14 | Non corretto (scelta) | — | — |

**B1.** Il reset di un episodio con file mancante avviene solo se la cartella del
file esiste ancora (volume montato) oppure, se è sparita anche quella, se il
primo antenato esistente non è vuoto. Una NAS smontata lascia il mount point
vuoto: la riga resta intatta.

**B2.** Un errore di completamento transitorio (EIO, ENOSPC, ESTALE, timeout,
rete, database occupato) non segna più il torrent in errore e non lo stacca:
viene ritentato dopo 1, 2, 4, 8, 16, 32 minuti, e solo dopo 6 tentativi diventa
definitivo come prima. Gli errori di identità e i file sorgente assenti restano
subito definitivi. Il tentativo successivo avviene tramite il meccanismo già
esistente di recupero degli eventi persi, ora frenato dal backoff. Limite: i
contatori sono in memoria, quindi un riavvio li azzera.

**L6.** La notifica `torrent_completed` finale viene inviata una sola volta per
torrent: si salta se il torrent era già "completed" con un percorso d'archivio
registrato prima dell'evento, o se il worker l'ha già inviata. `ffprobe` resta
eseguito (serve quando il file viene rielaborato). La notifica "in seeding"
(`seeding: true`) è un evento distinto e non è toccata.

**B8.** Il marcatore "import in corso" ora conta gli import (prima il primo che
terminava cancellava il marcatore anche per l'altro). Una rinomina che scrive
prenota la serie: se c'è un import o un'altra rinomina della stessa serie
viene saltata (come già accadeva con l'import); un import che parte durante una
rinomina aspetta che finisca. La rinomina non aspetta mai, quindi non c'è
rischio di deadlock con gli acquire annidati degli import. Il repair periodico
resta fuori da `cycle_lock`, ma ora è mutuamente esclusivo per serie.

**P1, cosa è cambiato:**
- anno = ultimo anno prima del primo tag tecnico (`1917.2019` → 2019,
  `2001.A.Space.Odyssey.1968` → 1968, `Wonder.Woman.1984.2020` → 2020);
- stagioni fino a 4 cifre (`S2024E05`), anche in `ParseEpisodeKey`;
- `Stagione N Episodio|Puntata M` e `Stagione N [Completa]` (pack);
- tag di risoluzione rimosso dalla coda del nome serie (`Show.2160p.S02E10`);
- audio 5.1 solo se "5.1" è isolato (niente più falsi positivi da `x265.10bit`
  o dalle date);
- risoluzione: `hd` solo come parola (non più da `HDR`, `HDCAM`, `Chad`…),
  `pal` solo come parola (non più da `Capital`, `Hospital`); `HDTV` resta 720p;
- gruppo: la coda di `WEB-DL`/`Blu-Ray` non è più un release group.

**P1, rettifiche alla mia analisi:**
- Il formato giornaliero (`2024.05.12` → S2024 E133, giorno dell'anno) è una
  scelta voluta e documentata nel codice ("ordine di riconoscimento come
  legacy"): non l'ho modificato.
- `EpisodeRange=[0]` per i pack è una sentinella esplicita (commento nel codice:
  "Only titles matched by seasonPackRe use the {0} range as a pack sentinel"),
  non un errore.
- Non gestito: i pack multi-stagione `S01-S03` restano interpretati come
  stagione 1.

### Seconda tornata di correzioni

| Voce | Stato | Dove | Test |
|---|---|---|---|
| P11 | Corretto | `safety.go` `recoverGoroutine` in 20 goroutine (engine, orchestrator, rss, hooks, handler, restart, qBittorrent); `websearch.go` consegna comunque un esito | `TestRecoverGoroutineKeepsDaemonAlive` |
| P7 | Corretto | `notifier.go` `sendMailWithTimeout` (30 s, stesso STARTTLS/AUTH di `smtp.SendMail`) | `TestSMTPTimeoutOnSilentServer` |
| P8 | Corretto | `notifier.go` `redactRequestError` (solo schema+host); `utils.RedactURLSecrets` maschera `/bot<id>:<token>` | `TestNotificationErrorsHideTokens` |
| P9 | Corretto | `hooks.go`: prima `SplitArgs`, poi `Expand` per argomento | `TestHookTitleStaysOneArgument` |
| P10 | Corretto | `hooks.go`: gruppo di processi ucciso al timeout, `WaitDelay` 5 s, max 4 hook concorrenti | `TestHookTimeoutKillsChildren` |
| P6 | Corretto | `notifier.go`: retry Telegram con attesa *prima* del tentativo | — |
| B4/B5 | Corretto | `orchestrator.go` `runComicsIfDue`: impostazioni fumetti lette solo se servono, errori non abortiscono il ciclo, timestamp completo solo in caso di successo; in caso di errore ritento dopo circa un'ora | — |
| B6 | Corretto | `orchestrator.go` `finishCycleWithoutDownloads`: statistiche salvate e report anche quando manca spazio | — |
| B3 | Corretto | `orchestrator.go` (ciclo principale e passaggio gap prioritario): registrazione fallita → torrent ritirato, placeholder annullato, ciclo prosegue; errore su `RemovePending` solo loggato | — |
| B7 | Corretto | `web_background.go`: in caso di errore resta l'ultima configurazione valida | — |
| I1 | Corretto | `web_torrent_events.go` `MonitorStalled`: `DELETE` solo per le voci realmente persistite | test esistenti di stallo |
| I5 | Corretto | `web_background.go`: lo stat dell'archivio a solo scopo di debug gira solo con la diagnostica attiva | — |
| L1 | Corretto | `orchestrator.go`: `covers && incumbentWins` (equivalente, una sola valutazione) | test esistenti di selezione |
| P2 | Corretto | `rss.go` `FetchFeed`: pagina successiva non leggibile o non analizzabile → si tengono le pagine già lette, con log | — |

Verifica: `go test ./...` verde; `go test -race .` verde, nessuna race.

Note:
- Anche la goroutine del repair rinomine ora azzera il flag "in esecuzione"
  con `defer`: prima un panic l'avrebbe lasciato bloccato per sempre.
- B6: il controllo globale dello spazio resta prima della scelta del percorso
  (comportamento invariato); ora però il ciclo viene salvato e riportato.
- Gli hook con `{title}` non tra virgolette ricevono ora il titolo come **un**
  solo argomento (prima veniva spezzato per spazi): è il comportamento corretto,
  ma uno script che contava sullo split va adattato.

### Terza tornata di correzioni

| Voce | Stato | Dove | Test |
|---|---|---|---|
| P5 | Corretto | `rss.go` `detailCacheKey` (chiave = URL della pagina di dettaglio); `internal/cache`: eviction dei più vecchi, non di un sottoinsieme a caso | `TestEvictionKeepsNewestEntries`, `rss_test.go` aggiornato |
| P4 | Corretto | `rss.go` `feedKindSubject`: il parser si sceglie da host+percorso, non dalla query string | — |
| C3 | Corretto | `notifier.go` `Async()`: i worker accodano le notifiche (coda da 256, oltre si scarta con warning); `RefreshMediaLibraries` in goroutine con coalescenza | `notifier_async_test.go` |
| L4 | Corretto | `orchestrator.go`: `last_deep_gap_fill_ts` salvato solo a passaggio completato e non annullato | — |
| L5 | Corretto | `orchestrator.go`: `gap_research_hours` mai inferiore all'intervallo del deep pass | — |
| P12 | Corretto | `comics.go`: watchdog di inattività (2 min) sui download HTTP, timeout di 2 h e kill del gruppo di processi per `megadl`, `RunComicsCycle` interrompibile tra un titolo e l'altro | `comics_stall_test.go` |
| I2 | Corretto | `web_background.go` `settledTorrents`: un torrent completo senza nulla da recuperare non viene riesaminato per 60 s (o finché cambia percorso/arriva un evento) | test esistenti |
| C1 | Corretto | `safety.go` `workerRestartBackoff`: panic ripetuti → attesa che raddoppia da 5 s fino a 5 min; dopo 10 min di funzionamento il contatore riparte | `TestWorkerRestartBackoffGrowsAndCaps` |
| P3 | Corretto (ext.to) | `rss.go` `listingPageNumber` | `TestFetchFeedExtToPagesStartAtOne` |
| I8 | Corretto | `orchestrator.go`, `web_handlers_g7.go`: errore di `RecordSeenBatch` a livello WARN | — |
| P16 | Corretto in parte | `watcher.go`: profondità massima 8 della scansione ricorsiva, nomi `watchedVisit`/`watchedCandidate` | `TestScanFolderStopsAtMaxDepth` |

**P16, rettifica:** il limite di dimensione e il controllo di stabilità
c'erano già, nel worker (`watchedFoldersWorker`: file oltre
`maxWatchedFileBytes` ignorati, lettura solo dopo due osservazioni identiche
di dimensione e data). `ReadDir` non segue i link simbolici, quindi non c'erano
cicli possibili. Restava solo la ricorsione senza limite.

Note:
- Le notifiche dai worker sono ora "best effort": se la coda è piena o il
  processo si chiude prima della consegna, la notifica si perde (con log). Il
  pulsante "notifica di prova" resta sincrono e mostra l'errore.
- Un fumetto che smette di ricevere dati viene interrotto dopo 2 minuti invece
  di occupare il ciclo fino al timeout di 30 minuti.

Verifica: `go test ./...` verde; `go test -race .` verde.

**P3, verificato su extto.org (2026-10-06):** la paginazione parte da 1;
`page=1` restituisce la stessa lista dell'URL base (50 infohash su 50 uguali),
`page=2` la pagina successiva. Gextto chiedeva `page=1` come seconda pagina,
l'early-stop "nessun nuovo infohash" scattava e il crawl si fermava sempre alla
prima pagina, anche con 3 pagine configurate. Corretto in `rss.go`
`listingPageNumber` (solo ext.to), test `TestFetchFeedExtToPagesStartAtOne`.
Il Corsaro non era raggiungibile e non è tra i feed configurati: numerazione
invariata.

### L3 · Approvazione in due fasi

| Voce | Stato | Dove | Test |
|---|---|---|---|
| L3 | Corretto (cicli di acquisizione) | `orchestrator.go` `evaluateReleaseApproval`, `commitReleaseApproval`, `withdrawAddedTorrent` | `approval_phases_test.go` |

Prima l'approvazione scriveva subito nel database (serie, placeholder
dell'episodio o del film, backup dell'upgrade) e ogni passo successivo fallito
doveva annullarla. Ora il ciclo principale e il passaggio prioritario dei gap:
1. **decidono** in sola lettura (la stessa modalità già usata da dry-run e
   anteprima decisioni);
2. controllano lo spazio e **aggiungono** il torrent al motore;
3. solo se il torrent è partito **confermano**: rieseguono la decisione in
   scrittura, poi registrano il torrent.

Spazio insufficiente, aggiunta fallita o torrent rifiutato non lasciano più
nulla da annullare. Se alla conferma la decisione non regge più o la scrittura
fallisce, il torrent appena aggiunto viene ritirato e il ciclo prosegue.
Il rollback resta solo per il caso raro di registrazione fallita dopo la
conferma, e per gli scarti a fine download (`RollbackRelease`/`RestoreUpgrade`
in `web_torrent_events.go`, che sono un'altra cosa).

Non cambiato: le funzioni `Check*` mantengono nome e firma (sono usate da molti
test); in modalità scrittura continuano a scrivere. La conferma esegue le query
di decisione una seconda volta, solo per i release approvati.

Verifica: `go test ./...` verde; `go test -race .` verde.

### Quarta tornata di correzioni

| Voce | Stato | Dove | Test |
|---|---|---|---|
| L2 | Corretto | `orchestrator.go` `mergeSeriesCandidate`, `releaseStrictlyBetter` | `candidate_selection_test.go` (caso episodio/pack + 2000 input rimescolati) |
| L7 | Corretto in parte | `web_handlers_g7.go` costanti `episodeOrigin*` usate da g2, g7, `uiweb_v2_detail.go` | — |
| S5 | Corretto | `config.go` `settingTruthy`, usata da tutte le impostazioni sì/no | `TestSettingTruthyIsUniform` |
| I4 | Corretto in parte | `web_background.go`: il worker di ottimizzazione usa la configurazione in cache | — |
| P15 | Corretto | `libtorrent.go` `enterSession`/`exitSession`/`drainSession` su 43 metodi | `libtorrent_session_guard_test.go` |

**L2, confermato e corretto.** Il dubbio era fondato. A parità di punteggio,
un episodio e il pack completo della stessa stagione davano:
- pack arrivato per primo → solo il pack;
- episodio arrivato per primo → **entrambi** scaricati.

La regola "il nuovo è coperto, si scarta" faceva vincere chi c'era già a
parità, mentre "il nuovo copre, rimuove il vecchio" richiedeva che il nuovo
fosse strettamente migliore. Ora a parità vince chi copre di più in entrambe
le direzioni (il REMUX resta preferito a parità di punteggio). Il test rimescola
2000 combinazioni casuali e verifica lo stesso risultato. Con la vecchia regola
fallisce: verificato reintroducendola.

**L7.** Le origini (`"Feed RSS"`, `"Archivio"`, `"Indexer / web"`) arrivano
anche all'API e all'interfaccia, quindi i valori restano invariati. Ora però
passano tutti da costanti, e il confronto non si rompe più se si cambia
l'etichetta in un punto solo. Il testo del dettaglio "download avviato"
(`qualità`, `episodi`) è un messaggio per l'utente: non toccato.

**S5, bug trovato.** `weekly_enabled` (fumetti): l'interfaccia lo mostrava
attivo anche con `true` o `1`, ma il ciclo fumetti controllava solo `yes`.
L'interfaccia poteva quindi dire "attivo" mentre lo scarico settimanale non
partiva. Ora tutte le impostazioni sì/no accettano `yes`/`true`/`1`/`on`,
senza distinguere maiuscole e minuscole e ignorando gli spazi. Fanno eccezione
le opzioni passate così come sono a libtorrent (`libtorrent.go`), che hanno
un formato proprio.

**I4.** `LatestConfig` ha già una cache invalidata a ogni salvataggio, e la
usano quasi tutti. Le chiamate dirette rimaste (worker del ciclo, backup,
`RunNow`, aggiunta da TMDB) girano al massimo una volta al minuto e alcune
modificano la configurazione ottenuta (per esempio `DryRun`): condividere la
copia in cache sarebbe rischioso. Ho convertito solo il worker che la legge
senza modificarla.

**P15.** Le chiamate alla sessione libtorrent fuori da `List` ora si
registrano in un contatore. `Shutdown` prima chiude l'accesso alle nuove
chiamate (che si comportano come se la sessione non ci fosse), poi aspetta
fino a 30 s quelle in corso, e solo dopo distrugge la sessione. Ho usato un
contatore e non un `RLock`: questi metodi si chiamano a vicenda, e un
`RLock` annidato si blocca non appena `Shutdown` chiede il lock in
scrittura.

Verifica: `go test ./...` verde; `go test -race .` verde.

**Non fatto, con motivo:**
- **I6** (selezione O(n²)): con L2 la logica è isolata in
  `mergeSeriesCandidate`. Il costo resta quadratico solo all'interno della
  stessa serie e stagione, quindi è trascurabile con i volumi reali.
- **I7** (una ricerca d'archivio per query): non misurato; senza un profilo
  di un ciclo reale non so se valga la pena.
- **P13** (parser HTML dei fumetti): riscrivere circa 700 righe di parser va
  provato contro le pagine reali del sito dei fumetti. Da concordare.
- **P17, S1-S4, S6-S8**: rifattorizzazioni di stile o struttura senza bug
  noti. Da fare a parte, se si vuole.

Decisioni successive: **I7** non è un problema nell'uso reale (chiuso);
**P13** non si corregge (scelta).

### C2 · Stato degli spostamenti persistente e ricontrollo ridotto

| Passo | Stato | Dove | Test |
|---|---|---|---|
| 1. Spostamenti salvati nel DB | Corretto | `database.go` tabella `torrent_moves`, `LoadTorrentMoves`, `SaveTorrentMove`; `web_background.go` `restoreTorrentMoves`, `syncTorrentMoves` | `torrent_moves_restart_test.go` |
| 2. Ricontrollo ogni 30 s | Corretto | `web_background.go` `completionRecoveryInterval` | test esistenti |

**Passo 1.** Dei tre elenchi in memoria se ne salvano due, perché sono
un'intenzione che deve sopravvivere al riavvio: gli spostamenti da ritentare
(destinazione, fine seeding sì/no, tentativi) e la protezione di fine seeding.
Il terzo ("spostamento richiesto") vale solo per la sessione in corso e non
si salva: dopo un riavvio nessuno spostamento è in corso. Il worker continua a
lavorare sulle mappe in memoria, e a fine giro scrive nel database solo ciò
che è cambiato (nessuna scrittura se cambiano solo orari o flag volatili).
All'avvio gli spostamenti salvati vengono ripristinati con 60 s di attesa,
perché il motore potrebbe non aver ancora caricato i torrent. Poi il
meccanismo esistente li chiude se il torrent è già a destinazione, o li
rilancia. Una protezione di fine seeding rimasta senza il suo spostamento è
obsoleta e viene scartata. I 18 punti che modificano le mappe non sono stati
toccati.

**Passo 2.** Il ricontrollo dei torrent completi senza evento gira subito
all'avvio e poi ogni 30 s, invece che a ogni giro da 0,75 s. Gli eventi
restano il canale principale. Anche i ritentativi dei completamenti falliti
(B2) passano da qui: il loro primo backoff è di 1 minuto, quindi la cadenza
di 30 s li ritarda al massimo di mezzo minuto.

Verifica: `go test ./...` verde; `go test -race .` verde. Nessuna prova sul
daemon reale: il binario in esecuzione è quello precedente alle modifiche.

**Ancora da fare:** I3 (rinviato), I6, P17, S1-S4, S6-S8.
