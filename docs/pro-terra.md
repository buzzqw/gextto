# Rapporto tecnico Pro Terra — approfondimento sul sorgente Gextto

## Scopo

Questo rapporto è il seguito di [`gextto-terra.md`](gextto-terra.md): parte da una
seconda lettura del sorgente, questa volta mirata a sicurezza, concorrenza,
lifecycle e integrità dei dati. Non afferma vulnerabilità sfruttate in
produzione e non modifica il comportamento del daemon; elenca miglioramenti
concreti, ordinati per gravità, con evidenza (`file:riga`), rischio, proposta e
verifica.

A differenza del rapporto precedente, che si concentrava su esposizione di rete,
budget degli indexer, shutdown e documentazione API, qui emergono temi più
profondi: l'accesso non sincronizzato all'handle nativo di libtorrent, il
trust-model degli event hook e dei percorsi utente, e alcune scritture database
non transazionali.

## Stato dell'intervento

Le correzioni di correttezza, concorrenza e integrità dati sono state applicate
al codice (il daemon gira in una LAN fidata, quindi i punti di *hardening*
puramente di rete non sono stati introdotti):

- **Arresto (punto 5) — fatto.** Le goroutine avviate dagli handler (ciclo
  manuale `RunNow`, rinomina globale `RenameAll`, riparazione rinomina archivio)
  sono ora registrate con `AppState.trackOperation()` e attendute prima della
  distruzione della sessione. Questo chiude la causa principale della corsa
  sull'handle libtorrent descritta al punto 1.
- **Concorrenza (punto 6) — fatto.** `requireEmbedded` legge `torrent_engine`
  sotto `engine_mu`; `RenameProgress` è protetto da `rename_progress_mu`;
  `qbittorrentEngine.categoryReady` usa `e.mu`; `safeGo` non riporta più due
  volte il completamento e il restart è interrompibile allo shutdown; gli
  handle SQLite vengono chiusi esplicitamente.
- **Integrità dati (punti 7–8) — fatto.** Gli upgrade di serie e film
  (backup + update) sono transazionali; `clearPlaceholders` e `Archive.Delete`
  non usano più un hash magnet vuoto per i `DELETE`.
- **Robustezza (punto 9) — fatto.** `MarkTorrentRemovedAt` e il caricamento
  `movies_config` non ignorano più gli errori in silenzio; `gibSetting` satura
  invece di traboccare; `HTTPDownload` ha un limite di dimensione.
- **Punto 1 (parziale).** La causa scatenante è risolta con il tracciamento
  delle goroutine, ma la disciplina `sessionMu` non è ancora estesa a tutti i
  ~90 metodi che leggono `c.session`: resta raccomandata come difesa in
  profondità.
- **Punti 2–4 (sicurezza) — rinviati.** Allowlist degli event hook, filtro SSRF
  e redazione delle chiavi in `ConfigView` non sono stati introdotti perché il
  daemon opera solo in una LAN fidata; restano valide raccomandazioni se in
  futuro verrà esposto oltre la LAN.

## Priorità

| Priorità | Area | Intervento |
|---|---|---|
| Critica | Bridge CGo | Sincronizzare ogni lettura di `LibtorrentClient.session`. |
| Critica | Sicurezza | Vincolare il campo `Program` degli event hook. |
| Alta | Sicurezza | Filtrare gli URL remoti (SSRF) e le origini dei percorsi. |
| Alta | Sicurezza | Non esporre le chiavi indexer/TMDB/TVDB in `ConfigView`. |
| Alta | Arresto | Tracciare le goroutine avviate dagli handler. |
| Media | Concorrenza | Proteggere `torrent_engine`, `rename_progress`, `categoryReady`. |
| Media | Integrità dati | Rendere transazionali upgrade e scritture correlate. |
| Media | Integrità dati | Gestire l'hash magnet mancante nei `DELETE` per hash vuoto. |
| Bassa | Robustezza | Correggere errori ignorati e limiti di dimensione. |

## 1. Sincronizzare l'accesso all'handle nativo di libtorrent

**Evidenza.** In [`libtorrent.go`](../libtorrent.go), `session unsafe.Pointer` è
protetto da `sessionMu sync.RWMutex` (righe 199–204), ma l'unico lettore che
acquisisce il lock è `listUncached` (righe 1226–1241). Tutti gli altri metodi
leggono `c.session` senza lock: `Remove` (riga 2238), `MoveStorage` (1587),
`PollEvents` (1374), `Peers`/`Trackers`/`Files` (1613/1650/1676),
`controlPaused` (1711), `AddWithOptions` (880), `SetGlobalSpeedLimits` (721) e
via dicendo. `Shutdown` (2210–2234) scrive `c.session = nil` sotto
`sessionMu.Lock()`, così come il finalizer (498–505).

**Rischio.** È una data race reale (rilevabile con `go test -race`) e, peggio,
una lettura non più valida dell'handle subito prima/durante `cgoLtDestroy` porta
a una chiamata CGo su una sessione distrutta. Lo stesso commento del codice
(199–202) avverte che Boost **aborta il processo** con "invalid session handle
used". Il meccanismo di protezione introdotto per `List` non è esteso agli altri
percorsi, quindi il problema che doveva evitare resta aperto per `Remove`,
`MoveStorage`, `PollEvents` e gli altri.

**Proposta.** Centralizzare la lettura dell'handle: un helper
`withSession(func(s unsafe.Pointer))` che acquisisce `sessionMu.RLock()`, oppure
estendere la disciplina `RLock` a ogni metodo che tocca `c.session`. Verificare
che nessun metodo mantenga il lock oltre la chiamata CGo e che nessun altro
codice legga il campo direttamente.

**Verifica.** `go test -race` sul pacchetto; un test di stress che chiama
`Shutdown` in concorrenza con `List`/`Remove`/`PollEvents` e verifica che non
avvenga alcuna chiamata su handle distrutto. Il test deve fallire con il codice
attuale.

## 2. Vincolare il programma degli event hook

**Evidenza.** `POST /api/event-hooks` →
[`SaveEventHooks`](../web_handlers_g3.go) (riga 484) persiste una lista di
`EventHook`. [`ValidateHooks`](../hooks.go) (righe 135–159) controlla solo che
`Program` sia non vuoto e **assoluto**, e i limiti di lunghezza; non esiste
un allowlist di eseguibili. [`RunHook`](../hooks.go) (riga 536) esegue
`exec.CommandContext(ctx, program, args...)` con `program` libero e `args`
derivati da `SplitArgs`. Con `Program: "/bin/bash"` e `Args: "-c <comando>"` si
ottiene una shell arbitraria.

**Rischio.** Esecuzione di comandi arbitrari come utente del daemon. Poiché
l'interfaccia non ha autenticazione, chiunque raggiunga la porta può configurare
un hook e poi innescarne l'esecuzione con un normale evento del motore (un
ciclo, un download, `/api/run_now`). È la via più diretta per trasformare
l'accesso alla porta in controllo del sistema.

**Proposta.** Introdurre un allowlist esplicito di eseguibili consentiti (o un
flag di configurazione che abilita percorsi liberi, disattivato di default).
In alternativa, escludere gli hook dal `POST` anonimo e richiedere un segreto
amministrativo separato per modificarli. Documentare nel manuale e in
`SECURITY.md` che gli hook eseguono codice come utente del servizio.

**Verifica.** Test su `ValidateHooks` con programmi non in allowlist; test di
integrazione che dimostri il rifiuto di `/bin/sh`; documento aggiornato.

## 3. Filtrare gli URL remoti (SSRF) e vincolare i percorsi utente

**Evidenza.** Tre endpoint scaricano un URL fornito dall'utente e lo trattano
come `.torrent`/magnet: `POST /api/send-magnet` ([`SendMagnet`](../web_handlers_g2.go),
righe 798–815), `POST /api/archive/add` e `POST /api/search/add`
([`gh0_downloadAndAdd`](../web_handlers_g0.go), righe 1055–1066). L'unica
validazione è il prefisso `http(s)://` ([`gh0_isTorrentURL`](../web_handlers_g0.go),
riga 1038). `defaultHTTPClient` segue i redirect e non ha restrizioni di rete.
Anche le URL di integrazione sono scrivibili in modo anonimo
(`flaresolverr_url`, `jellyfin_*`, `plex_*`, `qbittorrent_url`, `indexers`,
`websearch_engines`) e consumate da endpoint di test/refresh.

**Rischio.** SSRF: il daemon può essere indotto a richiedere indirizzi interni
(metadati cloud `169.254.169.254`, servizi LAN, il proprio listener motore) con
il corpo della risposta salvato su disco o riflesso negli errori. Vale anche
per `IpfilterUpdate` quando `IpFilterPath` è una URL ([`web_handlers_g7.go`](../web_handlers_g7.go),
righe 848–880).

**Proposta.** Un unico helper HTTP condiviso che (a) rifiuti schemi diversi da
http/https, (b) risolva l'host e blocchi indirizzi privati/link-local/metadata
salvo esplicita configurazione, (c) limiti i redirect e (d) impedisca il
de-referenziamento del listener locale. Applicarlo a `SendMagnet`,
`gh0_downloadAndAdd`, `IpfilterUpdate` e ai client di integrazione. Rendere
l'eccezione per host privati (Jackett/Prowlarr in LAN) esplicita e documentata,
non implicita.

**Verifica.** Test con URL verso `127.0.0.1`, `169.254.169.254`, `[::1]` e
`10.x` che attestano il rifiuto; test che l'eccezione configurata consenta solo
gli host dichiarati.

### 3.1 Percorsi utente non vincolati

`GET /api/browse_dir` ([`web_handlers_g6.go`](../web_handlers_g6.go), 516–559)
elenca qualunque directory raggiungibile; `POST /api/mkdir`
([`web_handlers_g0.go`](../web_handlers_g0.go), 983–999) crea directory con
`os.MkdirAll` senza limitazione alle radici configurate; `POST /api/upload-torrent`
([`web_handlers_g3.go`](../web_handlers_g3.go), 778–782) accetta un `save_path`
libero, a differenza di `MoveTorrentStorage` che applica
`gh7_path_starts_with`. **Proposta:** riusare la stessa regola di
"percorso dentro le radici configurate" già presente per il rename/move in tutti
e tre i punti. **Verifica:** test negativi per percorsi fuori radice e per
caratteri come `\0`.

## 4. Non esporre le chiavi in `ConfigView`

**Evidenza.** `GET /api/config` ([`ConfigView`](../web_handlers_core.go)) restituisce
`"api_key": indexer.APIKey` (riga 760) e i valori `tmdb_api_key` (895) e
`tvdb_api_key` (898) in chiaro, mentre per Jellyfin/Plex/qBittorrent espone solo
un flag `*_configured` (891 e simili). Lo stesso handler usa già
`indexer_api_keys_configured` (864) e `tmdb_configured` (890) come booleani.

**Rischio.** Un singolo `GET` anonimo rivela chiavi API di indexer e
TMDB/TVDB. È incoerente con la redazione applicata agli altri segreti.

**Proposta.** Sostituire i campi in chiaro con i booleani già calcolati
(`indexer_api_keys_configured`, `tmdb_configured`, `tvdb_configured`); se un
client ha davvero bisogno della chiave, esporla solo a valle di autenticazione.
Allineare la UI a usare i flag e non i valori.

**Verifica.** Test che `ConfigView` non contenga più i segreti ma conservi i
booleani; controllo che nessun consumer interno dipenda dai campi rimossi.

## 5. Tracciare le goroutine avviate dagli handler

**Evidenza.** `stopBackgroundWorkers` ([`web_serve.go`](../web_serve.go), 108–116)
attende solo le 10 goroutine registrate in `bgWG`. Tre goroutine non tracciate
toccano il motore torrent e possono sopravvivere alla distruzione della
sessione:

- ciclo manuale: [`RunNow`](../web_handlers_g1.go) (riga 707) usa
  `s.BackgroundContext()` (non `r.Context()`) e poi `RunCycleDomain`;
- rinomina globale: [`RenameAll`](../web_handlers_g2.go) (riga 631);
- riparazione rinomina archivio: [`web_background.go`](../web_background.go)
  (riga 1624).

`Serve` restituisce, `ShutdownEmbedded` distrugge la sessione, ma una di queste
goroutine può ancora chiamare `torrents.List()`/`AddWithPath()`/`Remove()`.
Inoltre `shutdownServers` (118–124) ignora l'errore di `Shutdown` dopo 10s e
lascia proseguire un handler bloccato su operazioni lunghe che non osservano
`r.Context()`.

**Rischio.** Use-after-destroy sul motore (aggravato dal punto 1) o errori
spuri durante l'arresto; la chiusura può restare appesa al lock `cycle_lock`
trattenuto da un ciclo lento.

**Proposta.** Un unico `sync.WaitGroup` per le operazioni avviate dagli handler
(oppure derivare i loro contesti da `bgContext` e far verificare la
cancellazione prima di ogni chiamata al motore). Attendere questo gruppo prima
di `ShutdownEmbedded`. Registrare in modo strutturato gli eventuali worker
residui allo scadere del limite systemd.

**Verifica.** Test con ciclo e rinomina artificialmente bloccati: nessuna
chiamata al motore deve avvenire dopo l'inizio della chiusura; `go test -race`
su avvio/arresto ripetuti.

## 6. Proteggere lo stato condiviso rimanente

**Evidenza e proposta.**

- `requireEmbedded` ([`torrent_engine.go`](../torrent_engine.go), 399–404) legge
  `s.torrent_engine` senza `engine_mu`, mentre `activeEngine`/`setActiveEngine`
  lo usano. Il backend può cambiare a runtime: **leggere sotto
  `engine_mu.RLock()`**.
- `RenameProgress` ([`web.go`](../web.go), 406) è una struct senza mutex, scritta da
  `RenameAll` e copiata da `RenameProgressView`: **proteggerla con un mutex o uno
  snapshot atomico**. Lo stesso check-and-set su `Running` (612–616) consente due
  rinomine concorrenti.
- `cycle_lock` è trattenuto per l'intero `RunCycle` (minuti): **valutare il
  lock solo intorno alla sezione critica di stato**, lasciando il lavoro lungo
  fuori lock o con `TryLock` + stato "in corso" idempotente.
- `qbittorrentEngine.categoryReady` ([`qbittorrent_engine.go`](../qbittorrent_engine.go),
  1283–1291) è una read-modify-write senza lock: **proteggerla con `e.mu`**.
- `safeGo` ([`safety.go`](../safety.go), 20–40) dorme `workerRestartDelay` senza
  `SleepBackground`: **usare `SleepBackground`** così il restart non ritarda
  l'arresto.
- I quattro handle `*sql.DB` aperti in `cmd/gexttod/main.go` non vengono mai
  chiusi: **aggiungere un `defer Close()` esplicito**.

## 7. Rendere transazionali le scritture correlate

**Evidenza.** `checkSeriesScoredInner` ([`database.go`](../database.go), 827–1001)
e `CheckMovieScoredWith` (1307–1383) eseguono, fuori transazione,
`INSERT`/`SELECT`/`saveUpgradeBackup`/`UPDATE`: se l'`UPDATE` fallisce dopo il
backup, resta una riga `upgrade_backup` orfana e l'upgrade è applicato a metà.
`checkSeriesPack` e `MarkReleaseCompleted` usano correttamente `tx`.

**Rischio.** Stato incoerente tra backup di upgrade e record aggiornato; in caso
di errore il ripristino successivo può ripristinare un episodio già sostituito.

**Proposta.** Avvolgere le sequenze backup+update in una transazione con il
pattern `committed`/deferred-rollback già presente. Estendere la stessa regola a
`ForgetRemovedTorrent`, `MarkTorrentCompleted`, `PurgeSeries`/`PurgeMovie`, che
eseguono più `DELETE`/`UPDATE` correlati fuori transazione.

**Verifica.** Test che iniettano un errore sull'`UPDATE` e verificano
l'assenza di righe orfane (rollback completo).

## 8. Gestire l'hash magnet mancante nei `DELETE`

**Evidenza.** `clearPlaceholders` ([`database.go`](../database.go), 3649) fa
`digest, _ := utils.MagnetHash(magnet)` ignorando `ok`. Per un rilascio senza
magnet (es. solo `.torrent` URL) `digest` è vuoto e il predicato diventa
`WHERE (lower(magnet_hash)=lower('') OR magnet_link=?)`, che può cancellare le
righe con `magnet_hash` legittimamente vuoto. Stesso schema in `archive.go:389`
(`Delete`).

**Proposta.** Gestire `ok`: quando l'hash non è disponibile, cancellare solo per
`magnet_link` esatto, mai con un confronto su hash vuoto.

**Verifica.** Test con rilascio senza magnet che non elimini righe estranee.

## 9. Correggere errori ignorati e limiti

- `MarkTorrentRemovedAt` ignorato in `ReconcileMissingTorrents`
  ([`database.go`](../database.go), 3328): un `UPDATE` fallito fa sparire la voce
  dalla cronologia download senza segnale.
- `tableColumns` ignorato in [`config.go`](../config.go) (1869) più l'errore di
  `Query` (1890): la lista film monitorati può caricarsi vuota in silenzio.
- `ALTER TABLE ... ADD COLUMN` ignorati in `SaveLibrary` (2555–2566): una
  migrazione fallita prosegue con schema vecchio senza segnale.
- `HTTPDownload` ([`httpx.go`](../httpx.go), 113–122) usa `io.Copy` senza limite di
  dimensione, a differenza degli altri helper: applicare `copyLimited`.
- `postJSON` ([`notifier.go`](../notifier.go), 295) usa `context.Background()` e
  ignora la cancellazione: propagare il contesto del chiamante.
- `gibSetting` ([`config.go`](../config.go), 1125) converte `float64 → uint64` senza
  limite superiore (overflow); `maxInt64(timeframeHours,0)*60` e le durate derivate
  possono traboccare per valori estremi: clampare gli ingressi.

**Verifica.** Test unitari mirati per ciascun punto, più un passaggio
`staticcheck`/`errcheck` in CI per impedire regressioni di errori ignorati.

## Piano consigliato

1. Bloccare il punto 1 (race sull'handle libtorrent) e il punto 5 (goroutine
   non tracciate): eliminano il rischio di abort del processo e di
   use-after-destroy, e sono osservabili con `go test -race`.
2. Bloccare il punto 2 (event hook) e il punto 4 (segreti in `ConfigView`):
   riducono l'impatto dell'assenza di autenticazione con modifiche circoscritte.
3. Introdurre l'helper SSRF condiviso (punto 3) e vincolare i percorsi utente
   (3.1).
4. Correggere la disciplina di locking (punto 6) e le transazioni database
   (punti 7–8).
5. Chiudere con gli errori ignorati e i limiti (punto 9) e agganciare
   `-race`/`staticcheck` alla CI.

I primi tre gruppi sono circoscritti e riducono il rischio operativo e di
sicurezza senza cambiare la logica di selezione delle release né la compatibilità
di build.
