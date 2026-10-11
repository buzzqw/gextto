# Migliorie ed evoluzione di Gextto e gx-torrent

Documento unico di lavoro del **2026-10-10**: raccoglie backlog, migliorie
implementate, piani di miglioramento, misure, revisioni tecniche e direzione
strategica di Gextto e del motore gx-torrent.

Sostituisce i seguenti file, spostati in `docs/archive/` come copie locali non
tracciate in git (vedi Appendice A per la mappa):

- `proposte-migliorie.md`, `da-fare.md`, `lavoro-sessione.md`,
  `scelte-di-progetto.md`, `accessibility-analysis.md`,
  `holepunch-test-harness.md`
- `gx-torrent-migliorie.md`, `gx-torrent-ottimizzazioni-prestazionali-v2.md`,
  `gx-torrent-misure-seeding.md`
- `revisione-1.md` (già locale, già in `.gitignore`)
- `archive/revisione-2.md`, `archive/UI_V2.md`, `archive/gextto-terra.md`,
  `archive/pro-terra.md` (già archiviati)

Restano documenti a sé, perché descrivono lo stato corrente e non un piano:
`docs/gx-torrent.md` (riferimento tecnico del motore), manuali, API,
architettura, sicurezza, migrazione, guide avanzate, TUI.

La **§14** (client standalone gx-nox) è stata estratta in
[`docs/gx-torrent-evoluto.md`](gx-torrent-evoluto.md), per tenere separati il piano
strategico e il documento tecnico del motore.

## Indice

1. [Backlog attivo](#1-backlog-attivo)
2. [Migliorie Gextto implementate](#2-migliorie-gextto-implementate)
3. [Direzione strategica](#3-direzione-strategica)
4. [gx-torrent: lacune e piano di miglioramento](#4-gx-torrent-lacune-e-piano-di-miglioramento)
5. [gx-torrent: ottimizzazioni prestazionali](#5-gx-torrent-ottimizzazioni-prestazionali)
6. [gx-torrent: misure di choking e seeding](#6-gx-torrent-misure-di-choking-e-seeding)
7. [Holepunching: harness di test](#7-holepunching-harness-di-test)
8. [Revisione tecnica 1: client, i18n, job, concorrenza](#8-revisione-tecnica-1-client-i18n-job-concorrenza)
9. [Revisione tecnica 2: bug e debito al commit 97142c1](#9-revisione-tecnica-2-bug-e-debito-al-commit-97142c1)
10. [Rapporti Terra e Pro Terra](#10-rapporti-terra-e-pro-terra)
11. [UI SSR + HTMX: report di migrazione](#11-ui-ssr--htmx-report-di-migrazione)
12. [Handover tra sessioni di lavoro](#12-handover-tra-sessioni-di-lavoro)
13. [Accessibilità](#13-accessibilita)
14. [gx-torrent standalone (gx-nox): rimando](#14-gx-torrent-standalone-gx-nox-rimando)
15. [Appendice A: file consolidati](#appendice-a-file-consolidati)

---

## 1. Backlog attivo

Ex `docs/da-fare.md`. Ripristinato l'8-10-2026: era finito per errore in
`docs/archive/` pur essendo un backlog **attivo**.

**Verifiche ricorrenti (ogni chiusura di ciclo).** `make test` (`check-ui` +
`installer-test` + `test-dht` + `CGO_ENABLED=0 go test ./...`), `make test-engine`
e `make vet`; invarianti `scripts/check-ui-settings-index.sh` /
`scripts/installer-selftest.sh`. **La TUI di Gextto deve sempre funzionare**:
`go test ./internal/tui/` + `go test . -run TestTUILibraryClientAgainstTheRouter`
(+ smoke `gexttod tui` contro il daemon) — vedi §12.1 di
[`gx-torrent-evoluto.md`](gx-torrent-evoluto.md).

### 1.1 Osservabilità: metriche nel tempo (fatto)

ID di correlazione e runbook sono fatti (vedi §1.3). Le metriche nel tempo sono
salvate nel database e mostrate in *Salute*, senza Prometheus:

- ~~durata delle ricerche e sorgenti che non rispondono, ciclo per ciclo~~ —
  fatto (2026-10-10): `CycleStats` salva durata ed esito per sorgente nel ciclo
  (`cycle_history`), con il pannello **Ricerche** in *Salute* (`uiweb.go`,
  `RecentCycleStats`, `ScrapeAll`);
- ~~avviso oltre una soglia~~ — fatto (2026-10-10): `cycle_monitor.go` avvisa
  (riga WARN + notifica) se una sorgente ha fallito in 3 delle ultime ricerche,
  se l'ultima ricerca supera 3 ore o se non c'è un ciclo senza errori da 3
  intervalli (minimo 2 ore), e segnala il ritorno alla normalità;
- ~~titoli in coda con il tempo di attesa~~ — fatto (2026-10-10): pannello **Coda
  titoli** in *Salute* (`Database.PendingQueue`, `pending_queue.go`), con
  titolo, attesa da `first_seen_at` e tempo al `due_at` (serie e film);
- ~~import falliti al giorno~~ — fatto (2026-10-10): pannello **Import falliti**
  in *Salute* (`Database.FailedImportsDaily`, `acquisition.go`), che conta per
  giorno gli ERROR di import dagli `acquisition_events`;
- la salute degli indexer del manager è già in Sources (`indexer_health.go`).

**Freschezza della UI.** I due pannelli si aggiornano da soli ogni 60 s (HTMX
`hx-trigger`, endpoint `GET /partial/health/queue`); i riquadri hanno già
`TestV2HealthTilesPollIndependently`. Il render è sempre fresco:
`TestV2HealthRefreshesWhenDataChanges` fissa che un dato aggiunto dopo il primo
render compaia al render successivo (niente cache stantia).

### 1.2 Benchmark del motore gx-core (fatto)

- **`scripts/bench-engines.sh`** — sonda riproducibile RAM/throughput di gx-core:
  throughput dello sciame su loopback (harness esistente, con RSS di picco) e
  throughput di scrittura su disco di una cartella (`--dir`, puntala a un mount
  NFS per confrontare). Uso: `scripts/bench-engines.sh [--size MiB] [--dir DIR]
  [--leechers N]`. Fuori dalla CI: serve solo a decidere con numeri.

### 1.3 Chiusi / decisioni

- **i18n in sei lingue** — fatto (2026-10-10). Pacchetto completo it/en/de/fr/es/pl:
  UI web (chrome, etichette, tooltip, hint, messaggi server-side e stringhe
  generate dal client JS via `window.__v2i18n` + `t()` in `v2-core.js`); errori
  API/HTMX mostrati all'utente localizzati con `uiText`; notifiche
  (Telegram/email/webhook) con `messages.Pick` su catalogo it→{de,fr,es,pl}
  (`internal/messages/catalog.go`); TUI a 6 lingue (`internal/tui/i18n.go`);
  pagina web di gx-torrent tradotta con lo stesso approccio (catalogo +
  `translateHTML`, lingua passata da Gextto con `-lang` e `?lang=xx`).
  Guardia: `TestUIIsTranslatable` fallisce se una stringa UI di template o
  view-model Go (o un letterale di `uiText`) non è chiave dei cataloghi, con
  allowlist esplicito; `TestClientI18nKeysTranslated` copre le chiavi client.
  Restano **in inglese per scelta** i log del daemon; le stringhe tecniche degli
  errori API non mostrate all'utente possono rimanere nella lingua originale.
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
  con un default registrato (anche vuoto) mostra ↺ accanto a «Salva»: riempie il
  campo col default e l'utente conferma salvando; il valore del default è mostrato
  accanto se compatto (`Resettable`/`DefaultInline` in `uiweb_v2.go`, `HasDefault`
  in `uiweb_pages.go`). Esclusi: campi disabilitati per il motore attivo, gestiti
  dall'auto-tuning, segreti o senza default.
- **Log di upgrade unificati** — fatto (commit `988d60a`). Una sostituzione
  produce una sola riga: `♻️ FBI S03E01 updated (1.4 GB): … · previous version
  «…» moved to the trash`. L'aggiunta normale resta `📁 … added to the library …`.
- **Pannello "Operazioni in background"** — risolto (commit `65cb9bf`). Resta e
  funziona (polling, annullamento), ma è **nascosto quando non c'è alcun job**.
- **Write-back cache nel motore gx-core** — chiuso: la misura mostra
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

### 1.4 Micro-ottimizzazioni UI/i18n (fatte, misurate il 2026-10-10)

Follow-up dei commit `06c9128`/`6cf568e`/`7f43b44`. Nessuno è un collo di
bottiglia: vanno prese solo come rifiniture, con i numeri sotto.

| # | Voce | Misura (disco locale, `CGO_ENABLED=0 go test`) | Guadagno atteso |
|---|---|---|---|
| 1 | Probe di scrivibilità nel chrome | `stat`+`write_probe` (create+fsync+delete): **~15,6 µs/op** (200 it.); `uiShellChromeFrom` intero **~164 µs/op** → il probe è ~10% del chrome in test | ~15 µs/render, niente fsync per render; su datadir NFS varrebbe millisecondi |
| 2 | Test di identità della cache dizionario | riuso confermato: due `Dictionary("de")` = **stesso puntatore** | solo qualità del test, nessun effetto runtime |
| 3 | Memo client i18n per `AppState` | **+1 entry per stato**, ~**2,4 KB/entry** (20 stati → 20 entry, 47.240 byte) | nullo in produzione (1 entry); pulizia solo per i test |

1. **Probe di scrivibilità cachato nel chrome. Fatto.** `uiShellChromeFrom` ora
   usa `dataDirWritable` (`uiweb_shell.go`), che riusa l'esito per
   `dataDirWritableTTL` (30 s): niente `create+fsync+delete` a ogni pagina,
   partial e tick SSE. Test `TestDataDirWritableCache` (riuso entro il TTL,
   ricalcolo dopo).
2. **Test di identità della cache dizionario. Fatto.**
   `TestDictionaryCacheAndInvalidate` confronta l'identità con
   `reflect.ValueOf(first).Pointer() == reflect.ValueOf(second).Pointer()`: una
   rebuild a ogni chiamata ora fallisce.
3. **Pulizia di `v2ClientI18nMemo`. Fatto.** `resetV2ClientI18nMemo()`
   (`uiweb_v2_client_i18n.go`) svuota la cache; `newTestAppState` lo chiama, così
   i test non accumulano entry (in produzione resta una sola). Test
   `TestV2ClientI18nMemoIsReset`.

### 1.5 gx-torrent su Windows e magnet v2 (chiuse, revisione del 2026-10-10)

La revisione dei commit Windows/v2 ha corretto R-001…R-013 (junction, `isDirLink`,
scadenza delle hash request, sessione 0, job CI `windows-platform`; commit
`95495e5`, `ebae504`, `1fae2ea`, `0363a02`). Le migliorie emerse (M-01…M-08)
sono state **tutte implementate**:

| Item | Stato |
| --- | --- |
| M-01 test del flusso link su Windows (`pointLink`→`readLink`→move→rimozione, `protectLegacyDir` non sposta la junction) | fatto (`48edb97`, `TestWindowsLinkFlow`) |
| M-02 allargare `windows-platform` ad altri test (Setup, CrossDevice) con test portabili | fatto (`48edb97`) |
| M-03 layer v2 in parallelo: chunk da ≤512 hash per peer diverso, con retry | fatto (`92f0a84`) |
| M-04 wizard: errore mostrato in pagina se i limiti di banda non si applicano | fatto (`fbc9eaf`) |
| M-05 `replaceDirLink` Windows: junction costruita completa e poi scambiata | fatto (`d4e1485`) |
| M-06 servizio Windows nativo (`windows/svc`) e pacchetto zip con `sc create` | fatto (`b145b73`, `9120fc5`; selftest del servizio in CI `8403c71`) |
| M-07 gateway Windows: usare la rotta scelta da Windows (`GetBestRoute`) | fatto (`d4e1485`) |
| M-08 interop v2 `base=0`: servire il layer dei blocchi come seed | fatto (`ab0a603`: risposta `base=0` dai pezzi verificati, in cache per-file, solo a file completo; interop con client grezzo `e9d8a78`) |

**M-06 (servizio Windows)** — fatto: `main` ora riconosce `svc.IsWindowsService()`
(`service_windows.go` / `service_other.go`) e avvia lo stesso demone
(`startDaemon`, estratto e condiviso col percorso interattivo) dentro `svc.Run`,
fermandolo su stop dell'SCM. `scripts/package-gx-torrent-windows.sh` produce
`gx-torrent-windows-<arch>.zip` con `gx-torrent.exe`, `install-service.ps1`
(`New-Service`), `uninstall-service.ps1` e `README.txt` (alternativa `sc create`).
Lo zip è ora generato **dentro** `build-release.sh` (variante Go pura, dove Go è
disponibile) e pubblicato da `continuous.yml`/`release.yml` come artifact e nel
release (`dist/gx-torrent-windows-<arch>.zip`). `TestDaemonStartServeStop` copre
il ciclo di vita condiviso (`startDaemon`/`shutdown`) su Linux e, col filtro
`windows-platform`, su Windows. L'installazione reale è coperta dal job CI
`windows-service` (`8403c71`, non bloccante perché l'SCM è asincrono):
`scripts/gx-torrent-service-selftest.ps1` registra il servizio con `New-Service`
e lo stesso `binPath` del pacchetto, lo avvia, attende `/api/v1/health` e ne
verifica lo stop pulito.

### 1.6 Motore presentato come codice nostro (fatto)

**Regola.** gx-core è il nostro motore e lo presentiamo come tale: la
documentazione non pubblicizza l'origine del codice. L'unica attribuzione è
quella **richiesta dalle licenze permissive** dei componenti derivati, nei file
`internal/gxcore/LICENSE` (MIT) e `internal/dht/LICENSE` (BSD-3) e in
`THIRD-PARTY-NOTICES`. Restano anche i riferimenti generici alla *funzionalità*
di altri client (per esempio "come fa libtorrent", "come fa anacrolix") e le
normali dipendenze di terze parti, con il loro import e la loro licenza
(`github.com/anacrolix/utp` per uTP, `libtorrent` per il motore alternativo).

**Fatto (2026-10-11).** Rimossi changelog "rispetto alla base" e marcatori di
origine dal codice e dai documenti (inclusi archivio e script); le dipendenze
riconducibili agli autori del codice originario sono state sostituite con codice
nostro: il DHT è ora `internal/dht` (decodifica indirizzi e throttle propri), il
logger dell'engine è `internal/gxcore/logging` e il backoff è
`internal/gxcore/internal/backoff`. Il peer ID prefix è `-GX`. Le licenze MIT/BSD
dei componenti derivati sono state ripristinate nei rispettivi `LICENSE` e
raccolte in `THIRD-PARTY-NOTICES`; l'identità di rete è `"gx-core " + Version`.

### 1.7 Controllo approfondito di performance (CPU e RAM)

**Punto.** Fare periodicamente un controllo approfondito di performance
dell'applicativo e del motore, con ottimizzazione di CPU e RAM. Vincolo: ogni
sostituzione o modifica deve essere **almeno equivalente** alla precedente, se
non migliore, e va misurata prima e dopo.

**Come.** Benchmark (`go test -bench … -benchmem`), profili `pprof` CPU/heap,
footprint del demone e di `gx-torrent` (RSS, goroutine, allocazioni),
`make measure-seeding` per il seeding. Annotare i numeri qui.

**Prima tornata (2026-10-11, Intel N97).** Identità di rete e dipendenze
sostituite con codice nostro (§1.6) senza regressioni. Ottimizzazione
applicata: il logger del motore scarta i record sotto la soglia dell'handler
**prima** di formattarli, così a WARNING (uso normale del demone) le chiamate
`Debugf`/`Infof` sulle hot path non allocano più il messaggio. Micro-benchmark
`BenchmarkEngineLoggerFiltered` vs `Forwarded`: ~151 ns / 3 alloc contro
~785 ns / 6 alloc per chiamata. I record che l'handler riceve non cambiano
(ERROR/WARNING restano, i conteggi del rumore di sciame non si perdono).

**Seconda tornata (2026-10-11, Intel N97) — filtro IP.** Diagnosi: con la
blocklist Aetherinox (34 MB, **666 763 regole**) `gx-torrent` teneva ~295 MB di
RSS, di cui **~190 MB** nel segment tree a puntatori del filtro IP
(`internal/blocklist/stree`) più i 34 MB del file grezzo tenuti in cache dal
demone. Sostituzione: le regole IPv4, come già le IPv6, stanno in **intervalli
ordinati e uniti** con ricerca binaria (`sortMergeV4`/`v4Contains`),
`stree` rimosso. Misura isolata sul file reale: heap vivo **190 MB → 5,4 MB**
(666 763 regole → 343 024 intervalli dopo il merge); `BenchmarkBlocked`:
**18,6 ns/op, 0 alloc/op** (prima `stree.Contains` allocava una `map` e una
slice a ogni lookup, sul percorso di ogni connessione peer/tracker). In
esercizio: RSS di `gx-torrent` **295 MB → ~105 MB** (RssAnon 275 → 92 MB) a
parità di regole caricate. Restano in cache i 34 MB del file grezzo (rilettura
rara su ricarica manuale): rimovibili in seguito.

**Terza tornata (2026-10-11, Intel N97) — page cache SQLite.** La `cache_size`
delle connessioni SQLite (`sqlite.go`) passa da 8 MB a **2 MB** (default di
SQLite). Misura isolata sul DB archivio reale (510 MB, FTS5; query
`Archive.Browse` e `CountMatches`, provate fino a 16 MB di cache): la latenza è
**piatta**, entro il rumore anche sul `MATCH` ampio (~1,14 s), perché a servire
il file è la page cache dell'OS, non quella di SQLite. Live su `gexttod`, a parità
di uptime (2 minuti): RSS **109,7 → 67,6 MB** (RssAnon 88,4 → 46,2 MB, RssFile
invariato); il delta sono le page cache delle 4 connessioni.

---

## 2. Migliorie Gextto implementate

Ex `docs/proposte-migliorie.md` (stato al 2026-10-07: **tutte approvate e
implementate**). Confronto con Sonarr/Radarr (serie/film), Mylar3/Kapowarr
(fumetti), Bazarr (sottotitoli), Prowlarr (indexer).

| # | Proposta | Decisione | Commit |
|---|---|---|---|
| 1 | Hardlink invece della copia durante il seed | sì, con garanzia anti-perdita | `20b28eb` |
| 2 | Aggiornamento mirato di Jellyfin e Plex | sì | `1bfaad4` |
| 3 | Soglia "upgrade until" | sì, oltre alla differenza minima | `95f6f97` |
| 4 | Autenticazione facoltativa | sì, spenta di default, LAN esente | `7dae752` |
| 5 | Calendario iCal | sì, con date italiane dichiarate | `e78bcc2`, `a919fcd` |
| 6 | `ComicInfo.xml` nei fumetti | sì | `6a7f739` |
| 7 | Mancanti: escludere gli episodi in download | sì | `e815180` |
| 8 | Badge dei download attivi nella barra | sì | `893bc94`, `42db4b2` |
| 8.1 | Traduzioni dei nuovi elementi in tutte le lingue | richiesta del proprietario | `698efbf` |
| 9 | Aggiornamento della documentazione | richiesta del proprietario | commit `docs:` finale |

### 2.1 Hardlink invece della copia durante il seed

- **Prima**: un file in seed veniva copiato in libreria (spazio occupato due volte).
- **Ora** (`hardlink.go`, `hardlink_seeding`, default attivo): episodi in cartella,
  season pack ed episodi in modalità copia entrano in libreria come hardlink. Il
  link nasce con nome nascosto e viene rinominato: il file del torrent non viene
  mai toccato. Se il link non è possibile (filesystem diversi, RAM disk,
  condivisioni senza hardlink) si copia come prima, con una riga di log per
  coppia di dischi.
- **Sicurezza**: i due nomi sono alla pari (non come i symlink). Cancellare il
  nome nel download lascia intatto l'archivio (si interrompe solo il seed); i
  dati spariscono solo cancellando entrambi. Verificato con test.
- I film non sono coinvolti: vengono già spostati col torrent (nessuna copia).

### 2.2 Aggiornamento mirato di Jellyfin e Plex

- **Ora** (`media_refresh.go`): dopo un import Jellyfin riceve
  `/Library/Media/Updated` con la cartella e Plex
  `/library/sections/{id}/refresh?path=` per la sezione che la contiene. Le
  richieste durante un aggiornamento vengono unite. Cartella sconosciuta, nessuna
  sezione Plex o rifiuto del server → aggiornamento completo come prima.
- Mappature `jellyfin_path_mappings` / `plex_path_mappings` per server in Docker.
  I pulsanti manuali restano completi.

### 2.3 Soglia "upgrade until"

- **Ora** (`upgrade_until.go`, `upgrade_until_score`, 0 = spenta): con punteggio
  ≥ soglia la copia non viene più sostituita; restano accettati REPACK e PROPER.
  Vale per ciclo, mancanti, season pack e film. «Perché non questa?» indica gli
  scarti per soglia. Riferimenti: 1080p WEB-DL H.264 ≈ 1280, 1080p WEB-DL
  H.265 DD+ ≈ 1480, 2160p ≈ 2480.
- La differenza minima (`upgrade_min_score_diff`) resta (*di quanto* deve
  migliorare vs *fino a dove*). Il blocco per titolo («Niente upgrade») è anche
  nella scheda serie web.

### 2.4 Autenticazione facoltativa

- **Ora** (`auth.go`, *Configurazione → Accesso*): spenta di default. Accesa: chi
  non è in LAN fa login (password bcrypt, cookie firmato 30 giorni, nuova
  password chiude le sessioni) o usa la chiave API (`X-Api-Key` o `?apikey=`; la
  TUI la prende da `GEXTTO_API_KEY`).
- LAN esente di default. Dietro reverse proxy contano gli indirizzi inoltrati
  (`X-Forwarded-For`, `X-Real-IP`, `Forwarded`).
- 5 password sbagliate = blocco IP per 1 minuto. Senza password né chiave il
  controllo resta aperto (avviso nel log). `GEXTTO_AUTH_DISABLE=1` per rientrare.

### 2.5 Calendario iCal

- **Ora** (`calendar_ics.go`, `GET /feed/calendar.ics`, link in Integrazioni).
  Per il pubblico italiano (serie doppiate mesi/anni dopo) distingue:
  - 📥 **arrivati in libreria** negli ultimi 30 giorni (giorno dell'arrivo);
  - 📺 **messa in onda originale** TMDB della stagione in corso (✓ se in libreria);
  - 🎬 **uscita italiana dei film** TMDB (digitale → home video → cinema), con
    ripiego dichiarato sulla data originale.
- Il riquadro "Prossime uscite" della Dashboard dichiara che è la messa in onda
  originale.

### 2.6 Fumetti: `ComicInfo.xml`

- **Ora** (`comicinfo.go`): nei CBZ scaricati direttamente (HTTP, Mega) viene
  aggiunto `ComicInfo.xml` con serie/numero/anno dal titolo GetComics. Non
  toccati: file che lo hanno già, CBR, fumetti via torrent (in seed). Un errore
  non compromette mai il download.

### 2.7 Mancanti senza gli episodi già in download

- Gli episodi con torrent attivo (anche aggiunto a mano, da cartella osservata o
  telefono) e le stagioni con season pack in corso non sono più mancanti, né nel
  ciclo né nella scheda serie. Un download in errore li fa tornare mancanti.

### 2.8 Badge dei download in corso

- La voce "Scarico" mostra i download in corso (torrent non finiti + HTTP attivi,
  non i seed), ogni 5 s; sul telefono è un contatore sull'icona.

### 2.9 Escluse perché già presenti o tolte di proposito

Webhook HMAC, event hook, alias titoli, blocklist, ritardi (delay profile), pack
di stagione, sottotitoli nel punteggio, Discover/Popular TMDB: già presenti.
Trakt: rimossa di proposito (`TestTraktIntegrationRoutesRemoved`).

---

## 3. Direzione strategica

Ex `docs/scelte-di-progetto.md` (estratto l'8-10-2026 dal piano di adozione
`SONARR_RADARR_REPLACEMENT_PLAN.md`, rimosso: qui le parti durevoli).

### 3.1 Posizionamento

- Gextto è per **installazioni nuove**, non drop-in di Sonarr/Radarr: niente
  import dei loro database, niente migrazione in place, niente rollback.
- Integrazione **verso** i gestori di indexer (Jackett/Prowlarr), non
  duplicandoli dentro Gextto.
- **Non-goal**: compatibilità API `*arr` / `/api/v3`; quality profile e custom
  format; autenticazione multi-utente (l'esposizione remota passa dal reverse
  proxy in `docs/SECURITY.md`). Le differenze si pubblicano, non si nascondono.
- Target: chi ha un server + media server (Jellyfin/Plex) e vuole "trova,
  scarica, rinomina, archivia" con un solo demone; molto probabilmente ha già
  Jackett o Prowlarr.

### 3.2 Gestori di indexer (Jackett/Prowlarr)

Gextto tratta il manager come **un'unica sorgente aggregata** e non importa la
sua lista di indexer: il manager è l'unica fonte di verità per indexer,
categorie, credenziali, FlareSolverr e rate limit.

Misure (stessa libreria, stesso host):

| Scenario | Latenza | Risultati |
|---|---|---|
| Aggregato con un indexer rotto abilitato | 60 s (timeout) | 0 |
| Indexer sano chiamato direttamente | 0,25–2,4 s | 40–100 |
| Aggregato dopo aver disabilitato il rotto nel manager | 2–4 s | 156–240 |

Conseguenze (tutte implementate):

- un prototipo che duplicava la lista indexer è stato costruito, misurato e
  **revertito**: nessun guadagno di stabilità, solo deriva di configurazione;
- le **categorie** sono state rimosse: un aggregato serve serie e film, una
  categoria statica per indexer è inutile o dannosa (query TV con categoria
  "film" → zero risultati);
- il tipo di manager si può **dichiarare** (`manager` in `config.go` e UI)
  invece di dedurlo da URL/porta/nome (resta il fallback);
- la salute del manager (Prowlarr `indexerstatus`) è in Sources: un indexer
  rotto/disabilitato è visibile invece di dare zero risultati in silenzio
  (`indexer_health.go`);
- un **timeout per sorgente** limita una singola richiesta sotto il budget della
  ricerca;
- l'API di ricerca di Prowlarr **ignora** stagione/episodio ed external-id
  (misurato): non vengono inoltrati; anche `type` non ha effetto osservato;
- la lista indexer di **Jackett non è raggiungibile** con la chiave Torznab (la
  sua API risponde con redirect al login): irrilevante, serve solo l'aggregato.

### 3.3 FlareSolverr

- **Sessioni persistenti** per dominio, con TTL breve e distruzione, invece di un
  browser per richiesta; se non supportate → modalità stateless.
- **Mai per i gestori di indexer**: Jackett/Prowlarr gestiscono Cloudflare da
  soli; un 403 o errore di rete lì viene riportato com'è invece di sprecare
  20–30 s mascherando la causa.
- Si rispetta `solution.status`: pagina "risolta ma in errore" non interpretata
  come contenuto.

### 3.4 Politica di selezione

Gextto **non** implementa profili di qualità, custom format o motore regex/score
come `*arr`. Sceglie invece default globali (risoluzione, sorgente, codec,
audio, HDR, lingua, vincoli di dimensione) + **override ed esclusioni
per-titolo** + **spiegabilità** (l'API di spiegazione dice perché un release è
accettato/rifiutato/preferito). Motivo: i profili sono la parte più complessa —
e più configurata male — del modello `*arr`.

### 3.5 Confronto con Sonarr/Radarr (fotografia onesta, 2026-10)

**Dove Sonarr/Radarr sono avanti:**

- **Profili di qualità e custom format**: score per risoluzione/sorgente/tipo/
  release group (anche regex), delay profile, profili diversi per serie e film.
  Gextto ha politica unica + "upgrade until" + override per-titolo: più facile,
  meno potente per regole fini. *Fattibilità:* dare **nomi ai profili sul modello
  Gextto** è un passo medio (la globale diventa "Default", ogni combinazione di
  override un profilo generato, migrazione automatica dai dati per-titolo in
  `series.quality/language/subtitle/exclude` e `movies_config.*`). Riprodurre la
  **semantica Sonarr** (qualità ordinate + cutoff + custom format) è più costoso;
  i custom format (motore regex + UI) vanno valutati a parte.
- **Maturità di import e rinomina**: multi-episodio, specials,
  daily, import manuale con matching e sostituzione. *Fattibilità:* si studia il
  comportamento e si reimplementa; Sonarr è GPLv3, Gextto EUPL 1.2: **non si
  copia codice né file di test**, ci si costruisce un corpus proprio da nomi
  reali.
- **Ecosistema e API**: `/api/v3` consumata da Overseerr/Jellyseerr; Gextto non
  la espone. *Fattibilità:* la compatibilità completa non è realistica; un
  **sottoinsieme** per Jellyseerr sì (`system/status`, `qualityprofile`,
  `languageprofile`, `rootfolder`, `tag`, `series/lookup` + `POST series`,
  `movie/lookup` + `POST movie`, `queue`) — ma dopo profili nominati, root folder
  e tag, e provato con un Jellyseerr reale.
- **Notifiche**: *fatto* — il webhook parla anche **Discord, Slack, ntfy, Gotify
  e Pushover** via selettore di formato (`notify_webhook_format`); restano i
  provider più esoterici.
- **Import di librerie esistenti**: parziale in Gextto, non prioritario.
- **Community, wiki, Docker, cadenza**: questione di tempo, non di architettura.

**Differenze che sono scelte, non lacune:** indexer via gestore esterno
(separazione più pulita e sicura); **niente usenet** (torrent, HTTP, web seed;
nel contesto italiano l'usenet è marginale).

**Dove Gextto è avanti (o pari):** tutto in uno (serie, film **e fumetti**) in un
solo demone; motore torrent integrato (gx-torrent Go puro, libtorrent oppure
qBittorrent-nox); spiegabilità; ricerca anche da motori web; leggerezza (un
binario Go + SQLite vs .NET); semplicità (health, backup, manutenzione integrati).

**Verdetto:** Sonarr/Radarr restano migliori su selezione (profili/custom
format), casi limite di import ed ecosistema/API. Gextto è migliore su scope
unico, motore integrato, spiegabilità, footprint e semplicità. Posizione
corretta: **alternativa per installazioni nuove**, senza rivendicare parità.

---

## 4. gx-torrent: lacune e piano di miglioramento

Ex `docs/gx-torrent-migliorie.md` (stato al 2026-10-08: lavoro completato,
ricontrollo finale fatto).

### 4.1 Contesto

`gx-torrent` = motore BitTorrent **in puro Go**, alternativo a libtorrent:

- **motore gx-core** (`internal/gxcore`): trasferimento;
- **demone + adapter** (`cmd/gx-torrent`, `gxtorrent_engine.go`): REST, pagina
  web, coda autogestita, ponte con Gextto (`TorrentEngine`).

Gextto fornisce già per tutti i motori: queue/slot, stalled/retry, seed policy,
RSS, ricerca, categorie/tag, scheduler di banda, post-processing, spostamenti e
RAM disk, filtro IP, proxy. Queste **non sono lacune del motore**.

Stato per capacità: `capabilityLevels` (`torrent_engine.go`), unica fonte di
verità usata dall'UI.

### 4.2 Lacune reali

**vs libtorrent / qBittorrent** (stato post-lavoro):

| Area | Cosa mancava | Esito |
|---|---|---|
| Limiti per-torrent | velocità download/upload | fatto (`SetLimits`) |
| Limiti per-torrent | connessioni e upload slot | fatto (`SetMaxConnections`/`SetMaxUploads`) |
| Modalità | upload/share mode | saltato (si usa seed infinito ratio 0); super-seeding fatto |
| Priorità | per-pezzo e diagnostica pezzi | fatto (API `pieces`, solo incluso/escluso per file) |
| Tracker | rimozione | fatto (`set-trackers`) |
| Web seed | add/remove a caldo | fatto (azione `webseeds`) |
| Sequenziale | toggle a runtime | fatto (`PiecePicker.SetOrder`) |
| Protocollo | IPv6 | fatto (socket dual-stack, `peers6`, PEX v6, DHT BEP 32); LSD v6 fatto |
| Protocollo | BitTorrent v2-only | fatto (`.torrent` e magnet, BEP 52) |
| Qualità | algoritmi di seeding/choking | misurato, vedi §6 |

**vs anacrolix/torrent**: lo **streaming HTTP Range è fatto** (§4.3 #7) e anche il
**reader con seek e readahead** è fatto (finestra graduata, §4.7); mancano gli
**storage alternativi** (mmap/blob/bolt/sqlite) e WebTorrent/WebRTC (wishlist).
gx-torrent ha invece daemon
sorvegliato, UI web, REST, UPnP/NAT-PMP, porta unica, proxy SOCKS/HTTP, filtro
IP multi-formato, killswitch VPN, preallocazione, cache adattiva.

**Non lacune**: RSS, search plugin, categorie/tag, scheduler, ratio,
post-processing, sequenziale come modalità (mancava solo il toggle a caldo),
cifratura, IP filter, proxy, DHT/PEX/LSD, UPnP/NAT-PMP, fast resume, selezione
file.

### 4.3 Decisioni

| # | Miglioria | Decisione |
|---|---|---|
| 1 | Rimozione/sostituzione tracker | **Fatto** — `Torrent.SetTrackers` nel motore + `set-trackers` |
| 2 | Upload/share mode | **Saltato** — seed infinito (ratio 0) |
| 3 | Toggle sequential/first-last a caldo | **Fatto** — `PiecePicker.SetOrder` + `SetSequential` |
| 4 | Web seed add/remove via API | **Fatto** — `AddWebseeds`/`RemoveWebseeds` + azione `webseeds` |
| 5 | Diagnostica pezzi | **Fatto** — API `pieces`, scheda Pezzi in Gextto e nel demone |
| 6 | Limiti velocità per-torrent | **Fatto** — `Limiter.SetParent`/`SetLimitKiB` + `SetSpeedLimits`, riapplicati al riavvio |
| 7 | Streaming HTTP Range + priorità pezzi | **Fatto** — `SetStreamWindow`, `/ui/stream`, redirect Gextto, pulsante ▶ |
| 8 | Limiti connessioni/upload per-torrent | **Fatto** — `SetMaxConnections`/`SetMaxUploads` + `Unchoker.SetNumUnchoked` |
| 9 | IPv6 | **Fatto** — socket TCP/UDP dual-stack, risoluzione e indirizzi v6, `peers6`, PEX `added6`/`dropped6`, tracker UDP v6, `yourip` v6, blocklist CIDR v6, DHT dual-stack (BEP 32) |
| 10 | BitTorrent v2-only | **Fatto** — parsing `meta version`/`file tree`/`piece layers`, info-hash SHA-256 troncato, pacchetto `merkle`, modello a pezzi per-file, verifica Merkle, download da `.torrent` e da **magnet**: i `piece layers` si scaricano dai peer con `hash request`/`hashes`/`hash reject` (BEP 52) e si verificano contro i `pieces root` |
| 11 | Super-seeding (BEP 16) | **Fatto** — `SetSuperSeeding` + azione `super-seeding`; capacità `full` |
| 12 | Holepunching (BEP 55) | **Fatto** — `ut_holepunch` nel motore, relè peer, dial uTP, avvio dopo dial fallito; WebTorrent resta wishlist |
| 13 | Qualità seeding/choking | **Misurato (locale)** — choking corretto ed economico; super-seeding senza guadagno su sciami piccoli, opt-in; scaling su sciame reale, vedi §6 |

Semantica limiti: **-1 = eredita il globale, 0 = illimitato** (come libtorrent).

### 4.4 Wishlist residua

- **BitTorrent v2-only**: **supportato end-to-end** — `.torrent` con `piece
  layers` e **magnet** (`urn:btmh:`), modello a pezzi per-file, verifica via
  nodo Merkle SHA-256, identità troncata e **fetch dei `piece layers` dai peer**
  (`hash request`/`hashes`, BEP 52). Test seed/leech per `.torrent` e magnet.
  Progetto dettagliato in [`gx-torrent-v2.md`](gx-torrent-v2.md).
- **WebTorrent/WebRTC**: nicchia, molto lavoro.
- **Qualità seeding/choking**: prima tornata fatta (vedi §6); per lo scaling
  serve campagna su sciame reale (gx-torrent vs libtorrent/qBittorrent, stessa
  politica, mediana su più run).

### 4.5 Criterio di verifica (applicato)

- Per punto: `go test ./cmd/gx-torrent/`, `go test -run GxEngine .`, motore dove
  tocca (`make test-engine`).
- Con effetto visibile: `docs/gx-torrent.md`, `MANUAL.*`, `README`.
- Ogni miglioria visibile in **entrambe le UI** (Gextto `uiweb/v2` + pagina del
  demone `cmd/gx-torrent/ui.go`), mai solo via API.
- Confronti con libtorrent/qBittorrent solo su misura.

### 4.6 Ricontrollo finale (fatto)

- `capabilityLevels`: gx-torrent `full` per `limits`, `trackers`, `sequential`,
  `piece_diagnostics`, `web_seeds`, `super_seeding`, `session_stats`; anche
  `first_last` e `holepunch` sono `full`.
- Note/testi aggiornati: `v2DetailCapsFor`, "Limiti noti", `gx-torrent.md`,
  `API.md`, `MANUAL.*`, `README*`.
- Ogni feature visibile in entrambe le UI (tracker, web seed, limiti,
  diagnostica pezzi, streaming, super-seeding).
- Test percorso felice + casi limite (lista vuota, duplicati, not-found, 416,
  completato/fermo); `make test` e `make test-engine` verdi (+ pacchetto
  `unchoker` in `make test-engine`).
- Inventario delle modifiche in `internal/gxcore/GEXTTO.md`;
  `go vet`, `gofmt` verdi; semantica `-1`/`0`/`>0` coerente.
- Nota residua: attesa pezzi nello streaming a polling (200 ms, timeout 2 min),
  senza test end-to-end su sciame reale; coperti dati presenti e mappatura
  byte→pezzi.

### 4.7 Cherry-pick da anacrolix (shortlist concordata, 2026-10-10)

Fonte: `github.com/anacrolix/torrent` v1.61 (Go, **MPL-2.0**). Due vie di
prelievo, coerenti con la politica del repo (**non si copia il codice di terzi;
si riscrive l'algoritmo** — l'idea non è coperta da copyright, l'espressione
sì — oppure si adotta il modulo come **dipendenza** MPL-2.0, come già
`github.com/anacrolix/utp`). Nessun cambio di licenza di Gextto/gx-core.

Regole: un item = un commit; **misura prima/dopo**; modifiche a `internal/gxcore`
registrate in `GEXTTO.md`; test mirati (`./cmd/gx-torrent/`, `GxEngine`,
`make test-engine`, `make test`); con effetto visibile anche le due UI (Gextto
`uiweb/v2` e pagina del demone).

| # | Voce | Fonte anacrolix | Via | Sforzo | Stato |
|---|---|---|---|---|---|
| 1 | Reader con seek + readahead e **priorità di pezzo graduata** | `torrent.Reader`, `ReadaheadContext`, `PiecePriority`, `File.SetPriority` | riscrivere | M | **fatto (priorità)** |
| 2 | **Coda richieste adattiva** | `client-piece-request-order.go`, `RequestIndex`, `reqq` | riscrivere | M | **fatto** |
| 3 | **`anacrolix/dht`** al posto del DHT attuale (BEP 42/43/44, BEP 33) | modulo `github.com/anacrolix/dht` | dipendenza | M | **BEP 42/43/44/33 fatti nel motore** |
| 4 | **Tracker scrape** (BEP 48) | `tracker` (scrape) | riscrivere | S | **fatto** |

Ordine di lavoro: **1+2 insieme** (stesso intervento: lo streaming), poi **4**
(pronto), poi **3** (valutato, sotto). Stato: **1** e **4** fatti, **2** fatto
(da misurare su sciame), **3** rimandato con motivazione.

**1 — Reader con seek, readahead e priorità di pezzo. Fatto (priorità).** Lo
streaming ora usa **due finestre graduate**: `SetFileStreamWindow` (i byte che il
player legge adesso) ha priorità sul **nuovo** `SetFileReadaheadWindow` (il
prefetch), che a sua volta precede il rarest-first. Nel picker:
`SetReadaheadWindow`/`pickReadahead`, provati prima delle allowed-fast e del
rarest; test `TestReadaheadWindowAfterStreamWindow`. Il demone aggiorna entrambe
le finestre mentre scrive (`setStreamWindows`). Il **reader con seek** non è un
tipo nuovo del motore: la funzione è già coperta dal gestore HTTP `Range`
(ogni richiesta imposta la finestra), e i test del picker coprono le due
finestre.

**2 — Coda richieste adattiva. Fatto.** La profondità delle richieste in volo per
peer era fissa a `DefaultRequestsOut` (50), con tetto `MaxRequestsOut` (250) o il
`reqq` annunciato. Ora `peer.RequestQueue` **cresce di uno a ogni blocco
consegnato** (`boostRequestQueue`, chiamata sul refill in
`torrent_messagehandler.go`) fino al tetto, e **si azzera sul choke**
(`resetRequestQueue`): un peer lento resta alla base, uno veloce scivola verso
`MaxRequestsOut`; con `reqq` piccolo il tetto resta quello. Logica in
`torrent_start.go` (`maxAllowedRequests`/`requestQueueCap`/`boost`/`reset`), test
`TestAdaptiveRequestQueue`. Da **validare su sciame reale** (come da regola del
repo) prima di considerarla stabile.

**3 — `anacrolix/dht` e modernizzazione del DHT. BEP 42 fatta;
adozione rimandata.** Oggi
`internal/dht` è il DHT del motore (modulo a sé, `replace` in `go.mod`):
dual-stack BEP 32, `Stats()`, `Config.PacketConn` per il socket uTP
condiviso, read-loop robusto. Candidato: `github.com/anacrolix/dht/v2`
(**MPL-2.0**, mantenuto; valutato **v2.24.0**, maggio 2026). Cosa c'è a monte
(verificato su pkg.go.dev): `ServerConfig.Conn net.PacketConn` (**socket uTP
condiviso**, come `Config.PacketConn` del motore), `IPBlocklist iplist.Ranger`
(filtro IP), **BEP 42** (`SecureNodeId`/`NodeIdSecure`), **BEP 44** (`bep44`,
`Store`, `Put`/`Get`), **BEP 33** (`Scrape()`), più BEP 5/43.

Perché **non è drop-in**: il modello API è diverso. Il DHT attuale espone un canale
`PeersRequestResults` + un tick periodico + `PeersRequestPort` (così è scritto
`session_dht.go`); `anacrolix/dht` usa `AnnounceTraversal(ih)` → `*Announce` con
canale `Peers`, più `Bootstrap`/`TableMaintainer`/`AddNode`. Andrebbero riscritti:
`session_dht.go` (ricerca → traversal per torrent), la creazione in `session.go`
(`NewServer(ServerConfig)`), i **stats** (`ServerStats` puntato vs la mappa di
`dht.Stats()` usata da `session_stats.go` e dalla UI) e la mappatura della
blocklist su `iplist.Ranger`. Inoltre le patch del motore (dual-stack BEP 32,
`PacketConn`, read-loop) vanno **riverificate** a monte.

Guadagno: dipendenza mantenuta, BEP 42/44/33, e **niente modulo a sé + `replace`**.
Costo: **rework del layer DHT** (M/L), doppia validazione (dual-stack, socket
condiviso) e perderemmo il controllo del DHT. L'uso DHT di gx-core è minimale
(solo ricerca e announce), quindi il beneficio non è immediato. **Verdetto: non
ora**; da riaprire se servono BEP 44 (mutable items) o il security BEP 42, o se il
modulo a sé diventa un peso — come task a sé con piano di validazione (sciame reale,
dual-stack, socket condiviso, stats). Licenza MPL-2.0 (dipendenza non modificata,
come `anacrolix/utp`), nessun problema.

**Modernizzazione BEP 42 + 44 + 43 + 33 (2026-10-11).** Il DHT è stato aggiornato
con la **security extension BEP 42** (`dht_sec.go`): l'ID nodo è derivato dall'IP
esterno (CRC32C Castagnoli; i vettori ufficiali del BEP sono verificati in
`dht_sec_test.go`). L'IP esterno arriva da `DHTHost` routabile o dall'interfaccia
uscente; se sconosciuto, il nodo lo apprende dal campo `ip` delle risposte e
adotta un ID sicuro (persistito). Gli indirizzi locali sono esenti. Aggiunti
anche **BEP 44** (`bep44.go`: archivio chiave/valore immutabile e mutabile firmato
ed25519, query `get`/`put`, API locale; vettori ufficiali in `bep44_test.go`),
**BEP 43** (read-only: `Config.ReadOnly`, flag demone `-dht-read-only`) e **BEP 33**
(`bloom.go` + `peer_store.go` + `scrape_client.go`: filtri `BFpe`/`BFsd`, stima
dello sciame, client verso i nodi più vicini; vettore ufficiale in
`bloom_test.go`), esposta dal motore (`Torrent.DHTScrape`), dal demone
(`GET /api/v1/torrents/{hash}/dhtscrape`) e dalla pagina (pulsante **DHT swarm**).
La **BEP 44** resta senza **client iterativo di rete**: gx-core non ha un
consumatore per l'archivio DHT, quindi non si aggiunge codice speculativo (lato
server + API locale sono pronti se servirà).

**4 — Tracker scrape (BEP 48).** **Fatto.** `Scrape` è stato aggiunto
all'interfaccia `tracker.Tracker` e implementato in **HTTP** (`/scrape`, con
`scrapeURL` che mappa `…/announce` → `…/scrape` tenendo la passkey) e **UDP**
(nuova azione BEP 15, request generalizzato a `udpRequest` con
`GetDest`/`SetConnectionID`/`Done`). `Tier.Scrape` delega al tracker corrente e
avanza sull'errore come `Announce`. Esposto come `Torrent.Scrape` (scrape
concorrente di tutti i tracker, errore per-tracker) e dal demone con
`GET /api/v1/torrents/{hash}/scrape`. Test: `httptracker_test.go`,
`udptracker_test.go`. Resta il pulsante nella pagina del demone.

**Nota (voci 1‑2, 2026-10-10).** Dopo lo studio, la **#2** si è rivelata
implementabile **senza** il pipelining multi-pezzo di anacrolix:
`DefaultRequestsOut` (50) è una base reale sotto il tetto `MaxRequestsOut` (250),
quindi la profondità può adattarsi **entro** l'architettura attuale (un pezzo per
peer). La **#1** resta la priorità graduata (stream > readahead); il "reader con
seek" è coperto dal gestore HTTP `Range`. Entrambe vanno **misurate su sciame
reale** prima di considerarle chiuse.

### 4.8 Oltre la shortlist (nuovi punti, 2026-10-10)

Quattro punti emersi dal confronto con anacrolix/libtorrent, **oltre** alla
shortlist §4.7. Stesse regole: riscrittura dell'algoritmo (o dipendenza MPL-2.0),
un item = un commit, misura/test, `GEXTTO.md`, entrambe le UI.

| # | Voce | Fonte | Via | Sforzo | Stato |
|---|---|---|---|---|---|
| 1 | **Bus di eventi del motore** (stato pezzi, peer/messaggi, tracker) | anacrolix `SubscribePieceStateChanges`, `PeerMessageEvent`, `StatusEvent`; libtorrent *alerts* | riscrivere | M | **fatto** |
| 2 | **Creazione `.torrent` da una cartella** (`mktorrent`-style) | anacrolix `metainfo` | riscrivere | M | **fatto** |
| 3 | **Bad-storage / errori di scrittura a runtime** | anacrolix `bad-storage.go`, `SetOnWriteChunkError` | riscrivere | S/M | **fatto (diagnosi)** |
| 4 | **LSD IPv6** (BEP 14, gruppo `ff15::efc0:988f`) | anacrolix `bep14.go` (`Ip6`) | riscrivere | S | **fatto** |

**1 — Bus di eventi del motore. Fatto.** `Session.Subscribe()` (`torrent/events.go`)
restituisce un canale di `Event`; l'engine non blocca mai su un subscriber lento
(scarta se il buffer da 64 è pieno). Tipi: `torrent_added`,
`torrent_completed`, `torrent_stopped`, `torrent_error`, **`torrent_piece`**
(alta frequenza), **`peer_connected`/`peer_disconnected`**, **`tracker_announce`**
(con seeders/leechers, o l'errore). Emessi in `session_add.go`,
`torrent_write.go`, `torrent_peer.go`, `torrent_close.go`, `torrent_stop.go` e dal
gancio sull'announcer (`SetResultHook`). Esposto dal demone come **SSE `GET
/ui/events`** e consumato dalla pagina, che si aggiorna subito sugli eventi di
stato invece di aspettare il polling (2 s) — il polling resta come rete di
sicurezza.

**2 — Creazione `.torrent` da una cartella. Fatto.** La parte di
`internal/metainfo` che scrive l'info dict esisteva già (`NewInfoBytes` +
`NewBytes`); mancava l'esposizione. Aggiunta `torrent.CreateTorrent(path, opt)`
(`torrent_create.go`) e, nel demone, un **form nella pagina** (`POST
/ui/create-torrent`) e l'endpoint **`POST /api/v1/create-torrent`**, che
restituiscono il `.torrent` come download. Verifica: il `.torrent` creato è
riletto da `metainfo.New`.

**3 — Bad-storage / errori di scrittura. Fatto (diagnosi).** Il motore **già**
ferma il torrent su errore di scrittura senza loop (`handleStopped` con
`lastError`). Il valore aggiunto è la diagnosi: l'errore ora identifica la
**cartella di storage** (`storage %q could not write piece #N`) e i guasti sono
contati (`SessionStats.StorageErrors`, esposto come `storage_errors` nei dati del
demone). Non è una macchina a stati separata come in anacrolix.

**4 — LSD IPv6 (BEP 14). Fatto.** Correzione: **BEP 14 è la Local Service
Discovery**, già presente (`cmd/gx-torrent/lsd.go`), non la negoziazione dei
messaggi. L'aggiunta è la **seconda famiglia**: annuncio e ascolto anche sul
gruppo IPv6 `[ff15::efc0:988f]:6771` (best-effort: senza IPv6 multicast resta la
v4), come anacrolix (`config.Ip6`). Stato LSD con `ipv6` in `/api/v1/status`.

---

## 5. gx-torrent: ottimizzazioni prestazionali

Ex `docs/gx-torrent-ottimizzazioni-prestazionali-v2.md`. Sostituisce la v1
(rimossa l'08-10-2026): diagnosi in gran parte corrette come lettura del codice
ma con tre difetti — non distingueva il codice originale dalle modifiche, presentava
benchmark non riproducibili (nessuna `Benchmark*` esisteva nel repo), ordinava
per priorità interventi irrilevanti o rischiosi. Qui **solo ciò che vale la
pena fare**, con come/rischi/verifica; una voce = un task/commit.

### 5.1 Stato di avanzamento

| # | Voce | Stato | Commit |
|---|---|---|---|
| 1 | LSD senza `d.mu` e senza `t.Stats()` | **fatto** | `d2a4060` |
| 2 | Streaming: `PiecesDone` mirato | **fatto** | `ff2d4bf` |
| 4 | MSE `StreamWriter` in-place | **fatto** | `5af2ee0` |
| 5 | `servedRequests` a finestra limitata | **fatto** | `a8b3e77` |
| 6 | Piece cache: TTL lazy (sharding condizionale, non fatto) | **fatto** | `60b090e` |
| 7 | `Bitfield.Count` con `math/bits` | **fatto** | `8caf6ad` |
| 3 | Rimozione `O_SYNC` + invariante di durabilità | **non da fare** | — |
| 8 | Opzionali (`findLocked` O(1), ETag/304) | **non da fare** | — |

Ordine per **valore atteso = (impatto × certezza) / (sforzo × rischio)**.
Regole per voce (da `AGENTS.md`): modifiche a `internal/gxcore` registrate in
`GEXTTO.md`; un commit per voce; test mirati
(`./cmd/gx-torrent/`, `GxEngine`, `make test-engine`, `make test`); invarianti
degli script; mai `bin/`/`gx-torrent.build_number`/`data/`; con comportamento
visibile aggiornare README/MANUAL/`gx-torrent.md`.

| # | Intervento | Impatto | Sforzo | Rischio | Codice |
|---|---|---|---|---|---|
| 1 | LSD senza `d.mu` / `t.Stats()` | Alto (latenza demone) | S | S | cmd (demone) |
| 2 | Streaming: check pezzi mirato | Medio (GC/coupling) | S | S | demone+motore |
| 3 | Rimozione `O_SYNC` + durabilità | **Molto alto** (disco) | M/L | M | motore |
| 4 | MSE in-place | Medio (upload) | S | S | motore |
| 5 | `servedRequests` a finestra | Medio (memoria) | S/M | M | motore |
| 6 | Piece cache TTL lazy (+ sharding) | Medio (seed) | M | M | motore |
| 7 | `Bitfield.Count` con `math/bits` | Basso | S | S | motore |
| 8 | Opzionali: mappa in `findLocked`, ETag/304 | Basso | S | S | cmd (demone) |

`S` ≈ mezza giornata, `M` ≈ 1–2 giorni, `L` ≈ oltre.

### 5.2 Voce 1 — LSD senza `d.mu`, senza interrogare i run loop (fatta)

`dueHashes` (`cmd/gx-torrent/lsd.go`) prendeva `d.mu` e, tenendolo, chiamava
`t.Stats()` su **ogni** torrent — query bloccante senza timeout al run loop. Con
storage lento (mount di rete) il mutex globale congelava tutte le REST.
Riscritta leggendo dallo snapshot lock-free (`snapshotViews`); stati
annunciabili: `downloading`, `downloading_metadata`, `seeding`,
`checking_files`, `moving`. Differenza: un torrent in `error` non viene più
annunciato (corretto). Verifica: test che `paused`/`stalled`/privati non
compaiono, `downloading` pubblico sì, e che `dueHashes` non prende `d.mu`.

### 5.3 Voce 2 — Streaming: check pezzi mirato (fatta)

`piecesPresent` (`stream.go`) chiamava `t.PieceStates()`: allocava un vettore
lungo quanto il torrent e scansionava tutti i pezzi sul run loop ogni 200 ms per
controllarne 1–2. Aggiunto `Torrent.PiecesDone(begin, end)` mirato
(`session_torrent.go`), che scansiona solo l'intervallo (con `len==0 → false`,
come prima). `PieceStates` resta per la mappa pezzi di API/UI. Follow-up non
fatto: notifica a evento invece del polling (non necessaria finché non alloca).

### 5.4 Voce 3 — Rimozione `O_SYNC` + invariante di durabilità (non da fare)

`filestorage.go` apre ogni file con `O_RDWR|O_SYNC`: ogni `write(2)`
va su storage stabile — su HDD/rete il throughput è limitato dalle IOPS. **Ma
non è un quick win**: il motore affida a `O_SYNC` tutta la durabilità, e `storage.File`
non ha `Sync()`; il bitfield è persistito solo a completamento/stop/verifica e
ogni `ResumeWriteInterval` (2 min), non "a ogni pezzo". Il progetto (conservato
come riferimento) richiedeva: estendere l'interfaccia con `Sync()`, togliere
`O_SYNC`, helper `syncPieces` testabile, ordine critico in `writeBitfield`
(**prima snapshot bitfield, poi sync, poi scrittura** — mai il contrario) e
ristrutturazione in due fasi di `session_stats.updateStats` (niente transazione
Bolt aperta durante gli fsync). Invariante: *un bit persistito si riferisce a
dati già fsyncati* (su crash si riscaricano ≤2 min, mai corruzione). Misure
vedi §5.10: su questo host il guadagno stimato era ~1,2x–1,8x su NFS (non il
"+300–800%" della v1).

### 5.5 Voce 4 — MSE `StreamWriter` in-place (fatta)

`cipher.StreamWriter` faceva `make` per ogni blocco cifrato (decine di MiB/s di
spazzatura in upload). Sostituito con writer in-place (`XORKeyStream(p, p)`) in
`internal/mse/mse.go`: i chiamanti non riusano il buffer dopo la scrittura.
Verifica: `mse_test.go` con `AllocsPerRun == 0` + round-trip; pacchetto aggiunto
a `make test-engine`.

### 5.6 Voce 5 — `servedRequests` a finestra limitata (fatta)

Mappa che cresceva indefinitamente coi blocchi serviti (memoria ∝ GB caricati su
seed longevi) e rifiutava per sempre anche re-request legittimi. Sostituita con
finestra mappa+FIFO da N≈1024 (`peerwriter.go`): dedup anti-abuso immediata,
memoria limitata. Test: dedup entro finestra, eviction oltre, nessuna crescita.

### 5.7 Voce 6 — Piece cache: TTL lazy (fatta; sharding solo se serve)

La cache serve **solo** l'upload (`cachedpiece` ← `RequestMessage`), non
download né streaming locale (`stream.go` usa `os.Open`/`ReadAt`). Costi reali:
un `time.Timer` per pezzo con `Reset` a ogni accesso + lock esclusivo anche su
hit. Fatto: TTL lazy (`expireAt` + ticker di sweep ~30 s, `Close()` lo ferma).
Non fatto: sharding in 16 partizioni — solo se il profiling mostra contesa sotto
forte seeding (refactor ampio di file del motore, non "a naso"). Verifica: sweep,
rinnovo su hit, eviction invariata; `internal/piececache` in `make test-engine`.

### 5.8 Voce 7 — `Bitfield.Count` con `math/bits` (fatta)

Somma byte-a-byte su tabella 256 byte → `bits.OnesCount64/8` a blocchi di 8,
tabella rimossa. Micro-ottimizzo non su percorso critico (`Count` usato in
`torrent_stats.go` e `All()`), ma banale e sicuro. Property test vs tabella.

### 5.9 Voce 8 — Opzionali (non da fare)

- **`findLocked` O(1)**: `ListTorrents()` + scan per ogni lookup per hash.
  A 4 torrent la risposta `/api/v1/torrents` è 3.822 byte in 0,5–0,7 ms:
  guadagno nullo (<0,05% CPU). Misurabile solo a centinaia/migliaia di torrent
  (~1 MB di lista): rimandato a quel carico — **poi fatto** il 2026-10-11 con
  indice hash→torrent e fallback auto-riparante (vedi §5.13).
- **ETag/304 su `/api/v1/torrents`**: `handleList` serve già lo snapshot
  precompilato, costo = solo `json.Marshal`; durante attività i tassi cambiano a
  ogni tick e il 304 non scatterebbe. Solo con misura che lo giustifichi.

### 5.10 Esclusi, con motivazione (misurati)

- **Rarity buckets / refactor picker**: costo reale a 27.250 pezzi ~0,11
  ms/pick in regime normale (pdqsort adattivo), <1% di un core a decine di
  pick/s. Risparmio ~0,1 ms/pick per refactor pesante (`HandleHave`/`Cancel`/
  `removeHaving`, endgame): non conviene.
- **Cursore watermark in `pickSequential`/`pickFileEdge`**: ~0,10 ms/pick e
  ~0,04 ms per il full scan di `pickFileEdge`: non merita il rischio.
- **Bitmask in `PieceDownloader`**: mappe solo per download concorrenti (≈ n.
  peer); bitmask 64 bit non copre pezzi da 2–4 MiB: trascurabile.
- **`clear(b.Data)` in `bufferpool`**: ~0,8 µs per 16 KiB → <0,1% di un core a
  1000 blocchi/s; pool condiviso, non dimostrato che tutti i percorsi riempiano
  il buffer: solo con audit completo.
- **`FADV_RANDOM`**: la giustificazione v1 (penalizza lo streaming) non vale —
  `stream.go` legge fuori dallo storage del motore. Al più `FADV_SEQUENTIAL` in
  verifica.
- **`saveLocked` fuori da `d.mu`**: non sul polling (solo mutazioni e
  tick-quando-dirty ~1/min); `state.json` 2,5 KB, ~0,1–0,3 ms. E la proposta v1
  azzerava `dirty` prima della scrittura perdendolo sull'errore.
- **`/proc/meminfo` sotto `d.mu`**: decine di µs, irrilevante.
- **`HandleDisconnect` O(N)**: poche disconnessioni/s, decine di µs: irrilevante.

### 5.11 Correzioni sostanziali alla v1 (riferimento)

- 10 voci su ~16 erano **codice invariato**, non lavoro gextto.
- `O_SYNC` non è un quick win; `storage.File` non ha `Sync()`.
- "Bitfield salvato a ogni pezzo" è falso (solo completamento/stop/verifica/
  `ResumeWriteInterval`).
- I moltiplicatori "15.500x/65.000x/600.000x" venivano da microbenchmark
  eterogenei, non end-to-end; nessun `Benchmark*` esisteva nel repo.
- Piece cache sul percorso **upload**, non download/streaming.
- `handleList` serve già uno snapshot: non "clona i torrent".

### 5.12 Misure prima dell'implementazione (Intel N97)

**Voce 3 — `O_SYNC`**: download dir su **NFS4** (1 GbE), alternativa ext4/NVMe.
N MiB in blocchi di B, `O_SYNC` per write vs bufferizzate + fsync finale:

NFS: 16 KiB 35,1 → 81,3 MiB/s (**2,3x**); 64 KiB 60,2 → 89,6 (1,5x);
256 KiB 67,4 → 108,7 (1,6x); 1 MiB 86,8 → 102,4 (1,2x).
NVMe: 16 KiB 7,3 → 862,7 (~118x); 256 KiB 150,5 → 975,5 (6,5x);
1 MiB 371,8 → 962,5 (2,6x).
**Previsione** (pezzi 32 KiB–2 MiB su NFS): **~1,2x–1,8x**, tetto link
~100–110 MiB/s. Guadagno maggiore con pezzi piccoli e disco veloce. Seed in
lettura non toccato.

**Picker** (`picker_bench_test.go`, 27.250 pezzi): `pickRarest` 110 µs steady /
1,11 ms peggiore (rimescolato ogni pick, shuffle incluso); `pickSequential`
~100 µs; `pickFileEdge` full scan 38 µs. La v1 misurava il caso rimescolato
spacciandolo per tipico.

**`saveLocked`**: 2,5 KB, ~0,1–0,3 ms per operazione utente esplicita.
Conclusione: fuori dalle voci fatte (1, 2, 4, 5, 6, 7) **nulla altro della v1
merita** a questo carico.

### 5.13 Controllo 2026-10-11 (`controllo2`): voci applicate

Revisione esterna (`/home/andres/controllo2.md`) verificata sul codice e applicata.
Nessuna modifica cambia la matrice capacità o l'indice delle impostazioni UI; per
le voci motore la suite `internal/gxcore/...` resta verde e non si osserva
regressione (le modifiche sono micro-ottimizzazioni o I/O aggregato, non algoritmi
sul percorso caldo).

Motore (`internal/gxcore`):

- **O-04** `buildV2BlockTreeData`: la capacità di `leaves` era il numero di
  *pezzi*, ma le foglie a `base=0` sono *blocchi* da 16 KiB: preallocato
  `ceil(f.Length/BlockSize)` per evitare decine di riallocazioni su file grandi.
- **M-14** stessa funzione: controllo di `t.closeC` nel ciclo di lettura, così un
  torrent rimosso a metà costruzione dell'albero (lettura di GB) non continua a
  leggere il disco.
- **C-07** `handlePeerSnubbed`: aggiunto `resetRequestQueue`, coerente con il
  commento della funzione ("after a choke (or a stall)"); prima la coda adattiva
  restava profonda dopo uno stallo. Test `TestSnubResetsRequestQueue`.

Demone (`cmd/gx-torrent`) e DHT:

- **C-02** streaming: `waitForPieces`/`streamRange` accettano il `context` della
  richiesta, così seek/chiusura del player non lasciano la goroutine a fare
  polling per 2 minuti con il file aperto.
- **O-01** `trusted-proxies`/`allowed-hosts`: cache del parsing (chiave = stringa
  grezza), azzera le allocazioni per richiesta; il valore cambiato è visto subito.
- **O-02** feed: `markFeedSeen`/`markSmartEpisode` non chiamano più `saveLocked`
  per item; un'unica scrittura atomica a fine `pollFeeds` (prima fino a 2N
  riscritture di `state.json` per ciclo).
- **O-03** DHT BEP 42: tabella CRC32C Castagnoli come variabile di pacchetto.
- **O-05** `findLocked`: indice hash/ID con fallback auto-riparante, ricostruito
  quando la sessione viene riaperta (§5.9).
- **C-01** rate limit login: `loginFails` limitata e prunata (cap 2000, evizione dei
  più vecchi) contro crescita illimitata.
- **C-05** BEP 44 `itemStore`: cap `bep44MaxItems` + prune degli scaduti e
  evizione del più vecchio in `put`.
- **C-06** BEP 33: filtro Bloom vuoto stima 0 (prima il clamp dava ~0,5 → 1).
- **C-03** Windows: un percorso UNC è rete per costruzione, senza dipendere da
  `GetDriveType` (share offline → `DRIVE_NO_ROOT_DIR` → junction su UNC).
- **C-04** `qbitAuth` delega a `standaloneAPIAllowed`: bypass LAN e cookie
  `gx_session` valgono anche per `/api/v2` come per UI e `/api/v1`.
- **M-09/M-10/M-11/M-13** parità qBittorrent: `content_path` in `torrents/info`,
  `rid` incrementale in `sync/maindata`, priorità `autoTMM` all'add, flag
  booleani via `formBool`; ricerche Torznab concorrenti.

### 5.14 Rimandato a dopo (dal controllo `controllo2`)

- **`renameFile`/`renameFolder` qBittorrent**: richiede supporto di rinomina nel
  motore (rinomina su disco + metadati), feature a sé, non in questa tornata.

### 5.15 Revisione `analisi4.md` (2026-10-11): voci verificate e applicate

Revisione esterna (`/home/andres/analisi4.md`) su installer standalone e gestore
RSS, verificata sul codice prima di applicare. **Applicate** le tre voci RSS
d'impatto reale, nella forma corretta (le proposte del documento erano in parte
inefficaci):

- **B.1 `ignore_days`**: era un filtro sulla *data di pubblicazione* rispetto
  all'ultimo match, così un episodio uscito poco dopo il precedente non veniva
  più preso. Ora è un **cooldown wall-clock** (`internal/rss/rules.go`,
  `ruleWithinIgnoreWindow`), e `markRuleMatch` registra l'istante del match
  (`cmd/gx-torrent/feed.go`), non la `pubDate`. Etichetta UI aggiornata
  (*Attesa tra match*). Test `TestEvaluateIgnoreDaysWindow`,
  `TestPollFeedsIgnoreDaysWindow`.
- **B.2 film "download once"**: *Segui un titolo* su un film rende la regola
  `once` (`rss.Action.Once`): prende una release e disattiva la regola, anche a
  più risultati nello stesso ciclo (`disableRules`, `onceHit` in `pollFeeds`).
  Test `TestPollFeedsOnceDisablesRule`.
- **B.3 titolo nei criteri**: la proposta del documento (aggiungere il titolo a
  `Match.Include`) **non funziona**: `Include` è un OR, quindi allarga i match.
  Applicata invece una **regex tollerante** che richiede le parole del titolo in
  ordine (`followTitleRegex`, es. `(?i)mamma[\s._\-]+mia`). Test
  `TestFollowTitleRegex`.

- **B.4 feed scoped**: aggiunto `rss.FeedCoveredByRules`; il fallback ai filtri
  locali del feed vale **solo se nessuna regola copre quel feed** (la proposta
  del documento lo applicava anche quando una regola lo copriva, aggirandola).
  Test `TestScopedRulesDoNotDisableOtherFeeds`.
- **B.5 REPACK+PROPER al primo download**: `smartEpisodeVerdict` registra anche i
  token inferiori. Test `TestSmartEpisodeFirstRepackAndProper`.
- **B.6 filtro episodi**: `ParseEpisodeFilter` accetta `S01E01-E05` e la sola
  stagione `S01`/`s1`.
- **B.7 log `/ui/rss-suggest`**: `searchFeedSuggest` non logga gli errori per
  indexer mentre si digita.
- **A.1–A.4 installer**: `install-service.sh`/`uninstall-service.sh` nel
  pacchetto Linux (`package-gx-torrent.sh`) con `StateDirectory=gx-torrent`
  nell'unit, l'attesa dello stop in `uninstall-service.ps1` e i due script di
  packaging nel `installer-selftest.sh`.

Con questo la revisione `analisi4.md` è **applicata per intero** (A.1–A.4,
B.1–B.7).

### 5.16 Cercati (`wanted`) nel demone standalone (2026-10-11)

Su richiesta del proprietario, la pagina `/ui/rss` del demone standalone cambia
modello: da "feed + regole" a **Cercati** (titoli che si vogliono). Ogni titolo
ha tipo (serie/film), stagioni, lingua, qualità, esclusi, filtro episodi,
categoria e percorso; si aggiunge a mano o con **Cerca nei feed**, che interroga
indexer e feed RSS configurati e propone i nomi trovati (filtrati per
lingua/qualità). Il **titolo è richiesto** come regex tollerante ai separatori e
lingua/qualità sono in **AND** (l'`Include` del motore delle regole è un OR, non
andava bene).

Il motore (`cmd/gx-torrent/wanted.go`) a ogni ciclo cerca ogni titolo negli
indexer e nei feed RSS (`applyWanted` sugli item dei feed, ricerca per titolo
sugli indexer) e prende gli episodi che compaiono; un **registro** in
`state.json` (`wanted_seen` / `wanted_taken`) dice cosa è apparso e cosa è stato
scaricato, la tabella mostra i **mancanti** e una **gap search** (query
`Titolo SxxEyy`, throttled a 30 min, max 3 a ciclo) li ricerca finché non li
trova: è il "gx-core sa che manca e lo scarica" richiesto. Per un film prende una
release e non ne cerca altre. La lista non genera feed/regole, così i feed
restano liberi per le regole avanzate. UI in `ui_wanted.go` + sezione in
`rss.html`; test in `wanted_test.go`.

---

## 6. gx-torrent: misure di choking e seeding

Ex `docs/gx-torrent-misure-seeding.md`. Supporto alla voce §4.3 #13. Regola di
fondo: **misurare prima**, poi decidere se toccare il core del motore.

### 6.1 Vincolo: non forzare l'automazione

Le misure riflettono il sistema **come gira davvero** (coda/slot, seed policy,
scheduler accesi): harness deterministico sul puro algoritmo (nessuna sessione);
sciame locale col demone e automanagement attivi; campagna reale con Gextto
(`AdjustQueue`, seed policy, limiti). Mai "a code spente": sarebbe laboratorio,
non produzione.

### 6.2 Metriche

Choking (slot di upload): chi viene sbloccato e per quanto, fairness a pari
velocità, rotazione optimistic unchoke, comportamento in download (premia i
downloader veloci) e in seed (premia gli uploader veloci).
Seeding (end-to-end): byte caricati dal seed vs corpus (`Nx corpus`),
throughput up/down, tempo di completamento, ratio e tempo a 1:1 in campagna reale.

### 6.3 Banco 1 — harness deterministico dell'unchoker

`internal/gxcore/internal/unchoker/sim_test.go` (in `make test-engine`): pilota
l'`Unchoker` reale con peer sintetici e fissa le proprietà (download veloci in
download, upload veloci in seed, `FastUnchoke` immediato, optimistic che non
ruba slot regolari e ruota, budget rispettato, fairness) + `BenchmarkTickUnchokeSeeding`.
Uso: `go test …/internal/unchoker -v` e `-bench TickUnchoke`. Rete di sicurezza
per varianti future dell'algoritmo.

### 6.4 Banco 2 — sciame locale (opt-in)

`cmd/gx-torrent/measure_test.go`, fuori da `make test`, con `GX_MEASURE=1`:

```
make measure-seeding                        # seeder + 3 leecher
GX_MEASURE_SUPERSEED=1 make measure-seeding # confronto super-seeding
GX_MEASURE_MESH=1 make measure-seeding      # leecher collegati tra loro
GX_MEASURE_LEECHERS=2 make measure-seeding  # sciame di 2 peer
```

Seeder + N leecher su loopback con IP `127.0.0.x` distinti (il motore rifiuta due
connessioni dallo stesso IP). Default: ogni leecher solo verso il seeder — si
misura l'**upload del seed** (ciò che il super-seeding cambia). Mesh
sperimentale su un host (connessioni multiple su loopback instabili: peer che
non si connettono/non completano): solo fotografia indicativa, pochi peer,
ripetere. Output: `uploaded=Nx corpus`, medie, tempi. Limiti: numeri assoluti
dipendenti dalla macchina (non confrontabili tra run diversi); un peer può
impiegare due tick di retry (10 s) nel super-seeding: l'harness riporta invece
di fallire su un peer lento.

### 6.5 Banco 3 — campagna su sciame reale

Per gx-torrent vs **libtorrent/qBittorrent**: stesso sciame e ruolo, stesse
politiche (slot, banda, n. peer), stesse metriche (byte seed `Nx corpus`,
throughput, tempo a 1:1, seed generati), mediana su più run, automazione attiva.
Unico banco che mostra il beneficio del super-seeding (~1x invece di Nx) e la
qualità del choking con scambio reale.

### 6.6 Risultati (prima tornata, locale; Intel N97, corpus 2 MiB)

Choking deterministico: tutte le proprietà passano; tick con 50 peer ~1,1 µs,
896 B/op, 1 alloc — trascurabile.
Seeding senza mesh (seed → 3 leecher, 5 ripetizioni): normale ~41–42 ms, seed
**3,00x**; super-seeding ~51–52 ms (**~24% più lento**), seed **3,00x**.
Mesh a 2 peer (indicativo): entrambi **1,00x** (i leecher si scambiano i pezzi).
Mesh a 3+ peer su un host non converge.

Lettura onesta: il choking del motore è **corretto e trascurabile come costo**;
su sciami piccoli/veloci il super-seeding **non riduce** l'upload (lo scambio
basta già) e costa throughput/variabilità. Il vantaggio BEP 16 è per l'*initial
seeding* di torrent grandi con molti peer lenti, non riproducibile su un host.
Conseguenza: super-seeding **opt-in**, non default; scaling via campagna reale.
Hardening applicato: un peer interessato ma in stallo (velocità zero) riceve il
pezzo successivo al tick invece di attendere il solo `not-interested`.

### 6.7 Cosa decidere in base ai risultati

- Choking peggiore di libtorrent su sciame reale → si interviene su
  `internal/unchoker` col banco deterministico come rete e lo sciame come verifica.
- Super-seeding più lento in locale ma più leggero su sciame reale → resta
  opzione di *initial seeding* (BEP 16), non default.
- Nessuna differenza misurabile → non si tocca il core (costo rebase non
  giustificato).

---

## 7. Holepunching: harness di test

Ex `docs/holepunch-test-harness.md`. Supporto a
`scripts/holepunch-netns-test.sh` (comportamento motore in `docs/gx-torrent.md`).

**Scopo**: verificare end-to-end che gx-torrent apra un buco (BEP 55,
`ut_holepunch`) tra due client dietro NAT/firewall usando un peer pubblico come
**relay**. Root-only, tutto in network namespace dedicati: la rete host non
viene toccata.

**Topologia**: `gx-lab` (pubblico: tracker `10.0.0.1:13800`, seed, relay
`10.1.0.1:51410`) + due firewall stateful `gx-r1`/`gx-r2` (lasciano uscire,
DROPPano nuove connessioni in entrata, accettano `ESTABLISHED,RELATED`,
rifiutano TCP verso le porte peer così il buco si apre su uTP) + client
`gx-a 10.10.0.2` (leecher) e `gx-b 10.20.0.2` (seed).

**Sequenza**: tracker minimo Python (interval 30 s) → seed con dati → B entra
leecher, scarica, completa restando connesso al relay (leecher interessato) →
solo allora entra A: non raggiunge B, chiede introduzione al relay, `connect` a
entrambi, dial incrociato su uTP. Successo: peer con origine **`holepunch`**
(chi diala) o `incoming` (l'altro lato) in
`/api/v1/torrents/<hash>/peers`.

**Scelte**: firewall stateful invece di NAT (conntrack non emula un NAT
cone port-translating: il dial diretto crea una entry locale che confligge col
SNAT; lo stateful riproduce lo stesso osservabile e rende il test
deterministico); seed e relay su IP diversi (il motore deduplica i peer per IP);
`-outgoing-interface` = IP di ascolto (altrimenti announce con IP sbagliato);
relay leecher interessato (un torrent completato non diala e scarta i non
interessati); tracker con interval lungo (un re-annuncio attribuirebbe la
connessione al dial tracker invece che all'holepunch); successo rilevato su
entrambi i lati (corsa del dial).

**Bug storici dello script**: unico NAT condiviso (buco impossibile);
`/peers` senza `source` (campo poi aggiunto all'API); ruoli sbagliati (relay
seed + B seed); `progress` 0–100 letto come 0–1; `grep` su log binari (serve
`-a`).

**Uso**: `make gx-torrent`, poi `sudo scripts/holepunch-netns-test.sh`
(`--keep` per debug, `--size 16` per torrent più grande). Esempio riuscito in
`scripts/risultato.txt`. Debug: `HOLEPUNCH_DEADLINE` (default 90 s),
`HOLEPUNCH_B_DEADLINE` (60 s), `HOLEPUNCH_DIAG=1` (dump conntrack). Su
fallimento stampa le righe `holepunch` dei log (rendezvous/connect).

**Limiti**: emulazione con firewall stateful, non NAT cone reale; esito
`holepunch`/`incoming` secondo chi vince la corsa; dedup peer per IP del motore
(in uno swarm reale i peer sono su IP diversi).

---

## 8. Revisione tecnica 1: client, i18n, job, concorrenza

Ex `docs/revisione-1.md` (già locale/non tracciato; citato da commenti in
`jobs.go`, `web.go`, `internal_translations.yml`). Anomalie e debito emersi da
revisione del codice, con correzioni incrementali senza cambiare il
comportamento stabilizzato. Perimetro: client JS della UI, i18n, HTML dinamico,
attività asincrone/job, concorrenza e test. Fuori perimetro: autenticazione e
sicurezza LAN (LAN trusted per decisione).

Al momento della revisione: `go test ./...` e `go vet` verdi; client UI unico
file IIFE `uiweb/static/gextto-ui.js` (~253 KiB, 5.194 righe); CSS in
`gextto-ui.css`; template server-rendered; polling + partial HTML + dialoghi
dinamici. Nessun errore bloccante: criticità di manutenzione, osservabilità,
coerenza e robustezza futura. Modello i18n effettivo: chiavi = **stringhe
italiane di sorgente**, cataloghi `internal_translations*.yml`
(italiano → lingua) uniti in SQLite all'avvio; client con `translateUINode()` +
`MutationObserver` e fallback inglese.

Priorità: **P1** prima di aggiungere funzioni (regressioni/diagnosi difficili);
**P2** debito compatibile con l'esercizio; **P3** qualità/manutenzione.

### 8.1 Client JavaScript monolitico (P2)

Un solo IIFE con bootstrap, tema, i18n, polling/API, dashboard, torrent, serie/
film/archivio/fumetti, configurazione, manutenzione, log, dialoghi/tabelle/
accessibilità. Su disco è grande ma compresso è piccolo: il problema è la
manutenzione (stato globale condiviso, collisioni future, init mescolato a
codice comune, logica render/rete/eventi nello stesso blocco, test solo su
output HTTP). Proposta: moduli (`ui-core.js` con contratto stabile
`api`/`pollRequest`/`esc`/`storage`/`notify`/dialoghi, poi dialoghi, shell, log/
manutenzione, torrent, libreria/discovery/fumetti), migrazione per estrazioni
senza big bang (prima ES nativi, poi eventuale bundle in build). Accettazione:
nessuna regressione, no-JS con contenuto essenziale, nessun accesso a variabili
private altrui, peso compresso invariato, focus/tastiera/dialoghi invariati.

### 8.2 Internazionalizzazione (P1 controllo cataloghi, P2 migrazione)

Rischi del modello a stringa: una variazione ortografica rompe la traduzione,
stringhe dinamiche con regex restano parzialmente in italiano, cataloghi
server/client divergenti, `MutationObserver` costoso su pagine dinamiche,
fallback non controllabili. Target: chiavi stabili (`dashboard.title`),
cataloghi versionati, fallback deterministico (attiva → base → chiave),
interpolazione parametrica, helper server/client, niente traduzione globale del
DOM. Migrazione graduale: controllo cataloghi → chiavi per nuove funzioni →
shell/dialoghi/toast → pagine una alla volta → `translateUINode` solo legacy →
rimozione observer. Test: ogni chiave nel catalogo base, nessuna traduzione
vuota, snapshot multilingua, stessa lingua per testo/title/aria/placeholder,
plurali e HTML nei valori.

### 8.3 HTML dinamico e sicurezza del rendering (P1 nuovo codice, P2 storico)

`innerHTML` ovunque (tabelle, toast, dialoghi, TMDB/release/fumetti, partial
server): `esc()` usato in molti punti ma ogni interpolazione deve ricordarselo
nel contesto giusto (testo vs attributo vs `data-*` JSON, errori da provider).
Nuovo codice: `textContent`, `setAttribute` validati, `createElement`,
`DocumentFragment`, `innerHTML` solo per markup statico, mai valori da
API/DB/provider/filesystem in stringhe HTML. Storico: `esc()` centralizzato,
`htmlTrusted()` esplicito per frammenti statici, tabelle esposte migrate per
prime (release, provider, blocklist, ricerche), test con payload ostili
(`<script>`, virgolette, `&`, newline, URL speciali). Accettazione: valori
ostili resi come testo, funzioni trusted riconoscibili dal nome, nessun calo di
sort/focus/paginazione/accessibilità.

### 8.4 Job manager (P1 operazioni distruttive/lunghe, P2 altre)

Modello non uniforme: 202 prima dell'esito, stato globale singolo, doppi avvii,
errori tardivi solo nei log, cancellazione non uniforme, retry/idempotenza non
rappresentati, log non correlati, test con sleep/polling. Proposto: `Job`
(`queued/running/succeeded/failed/canceled`, progress, message/error,
**result** strutturato, timestamp, cancel via context) + manager (concorrenza
limitata, dedup per chiave, retention, log con `job_id`, shutdown ordinato) +
endpoint (`GET /api/jobs`, `GET /api/jobs/{id}`, `POST …/cancel`; niente `POST
/api/jobs` generico: i job nascono dalle operazioni reali validate). Prime da
migrare: rinomina massiva/repair, scansioni archivi/librerie, backfill
MediaInfo, backup/restore, manutenzione DB/scoring, diagnostica sorgenti,
pulizie. Accettazione: niente doppi lavori, stato osservabile, errori tardivi
in stato+log, cancellazione senza orfani, refresh che conserva lo stato, niente
goroutine illimitate.

### 8.5 Concorrenza e race (P1 race in CI, P2 bonifica)

`go vet` non è un race detector. Aree: mappe retry/post-seed, cache config,
stato torrent tra polling/callback/shutdown, progressi manutenzione, registri
job/notifiche, contatori/metriche, chiusura sessione libtorrent, stato/DB
durante backup/restore. Strategia: un solo meccanismo per stato (mutex, atomic,
snapshot immutabili, canali, context, mappe sempre col lock documentato). CI:
`go test -race ./...` (se pesante: veloci+vet per push, completi per PR, race
in coda dedicata/giornaliera, motori reali in job separati). Test con timeout,
`Cleanup`, tmpdir e porte casuali, mai dipendenti dal servizio in esecuzione.

### 8.6 Piano raccomandato (Fase 0–4)

0 Baseline (test comportamento, metriche, convenzione log/ID, cataloghi +
race non bloccante). 1 Rischio basso (`ui-core.js`/dialoghi/shell, helper DOM,
test XSS, chiavi nuove stringhe). 2 Coerenza UI (cataloghi, observer solo
legacy, moduli tabelle/log/manutenzione, test multilingua). 3 Job (manager,
rinomina/scansioni, cancel/dedup/retention, `job_id` in UI/log/notifiche).
4 Concorrenza (race ripetuti, mappe/snapshot, shutdown/restart, race bloccanti).
Atteso: client modificabile senza regressioni incrociate, traduzioni verificabili
pre-rilascio, confini dati/markup, operazioni osservabili/cancellabili/correlate,
fiducia sulla concorrenza, consumi invariati. **Non riscrivere tutto**: piccole
estrazioni, test verdi a ogni fase, refactoring separato dalle funzioni.

### 8.7 Stato di attuazione

Fase 0: race non bloccante (`make test-race`, job CI `continue-on-error`) e
controllo cataloghi (`i18n_catalog_test.go`: YAML valido, niente duplicati/
vuoti, insiemi coerenti) fatti; metriche caricamento e convenzione `job_id` da
fare. Fase 1: test XSS end-to-end (`security.spec.ts`: injection, attribute/
tag breakout, `javascript:`) e `esc()` uniforme (anche `'`) fatti; estrazione
moduli, helper DOM e chiavi nuove stringhe rimandati/ricepiti come convenzione.
Fase 2: catalogo inglese completato (1.345 traduzioni mancanti, ora 3.130 chiavi
come le altre; test impone copertura) e test multilingua end-to-end
(`i18n.spec.ts`) fatti; observer e fallback client invariati. Fase 3:
`JobManager` + test completi (ciclo vita, dedup, cancel, concorrenza, retention,
panico→failed, chiusura) + endpoint read-only (in `API.md`) fatti; migrate
**rinomina massiva** (cancel tra serie, `/api/rename-progress` invariato,
`job_id` in risposta), **scansione archivi** (da sincrona bloccante a job,
`202` + `job_id`), **backfill MediaInfo** (report in `Job.Result`); UI dei job
(seguono fino a esito, pulsante Annulla; `jobs.spec.ts`) fatta; `Fail` esplicito
e `SetResult` aggiunti. Restano una alla volta le altre operazioni. Fase 4:
`make test-race ./...` verde (**0 data race**); bonifica e race bloccante dopo
la Fase 3. Test browser obsoleti riallineati senza mascherare regressioni; fix
reale: tabelle scrollabili raggiungibili da tastiera; suite browser 30/30.

Difetti trovati: chiave fantasma solo-spagnola `Nuevo File Archiviato`
(rimossa; ora il controllo l'intercetterebbe); inglese sottoinsieme con 1.345
voci mancanti (generate automaticamente — **richiedono revisione linguistica**
pre-rilascio); `esc()` senza `'` (corretto); test non idempotente sotto race
(reso ripetibile con cleanup).

### 8.8 Osservazioni emerse (ridimensionano il piano)

- **Modularizzazione completa del client: non consigliata così com'è.** 11
  moduli ES = refactor ampio e rischioso (IIFE→moduli, call-site, test browser)
  per beneficio solo manutentivo; il file compresso non è un problema browser.
  Solo quando la manutenzione diventa il collo di bottiglia, con bundle in build.
  Intanto `esc()` + test XSS coprono il rischio reale.
- **Chiavi i18n stabili globali: non necessarie ora.** Il modello a stringa
  italiana funziona; il buco era la copertura inglese (risolta). 3.130 voci × 6
  lingue riscritte = rischio regressioni. Ibrido: stabili solo per nuove
  stringhe dinamiche, legacy invariate, controllo automatico anti-buchi.
- **`POST /api/jobs` omesso di proposito** (validazione nelle operazioni reali).
- **Backup, pulisci trash, vacuum restano sincroni per scelta** (effetti esterni,
  cancellazioni definitive, VACUUM non interrompibile): non migrare.
- Miglioramenti: `Job.Result`, cancellazione ai punti sicuri (mai a metà
  unità), `Fail` esplicito, traduzioni EN automatiche da rileggere.
- Fase 3: nessuna migrazione obbligatoria rimasta. Fase 4: bonifica + race
  bloccante sui componenti stabilizzati. Prossimi passi: revisione linguistica
  EN; UI su `/api/jobs`; shutdown esplicito per operazione migrata.

---

## 9. Revisione tecnica 2: bug e debito al commit 97142c1

Ex `docs/archive/revisione-2.md` (analisi del 2026-10-06; metodo: lettura quasi
integrale di orchestrator/worker/eventi/database/config/postprocess/cleaner/
safety/web+router, poi engine/rss/parser/update/hooks/notifier/httpx/comics/
watcher/libtorrent; `go test` e `go vet` verdi; severità con grado di certezza
*Confermato*/*Da verificare*, nulla riprodotto a runtime).

### 9.1 Bug e criticità (stato: tutti corretti)

| Voce | Problema | Correzione |
|---|---|---|
| B1 | Path non raggiungibile azzera stato episodi (NAS smontata = dir vuota = `ENOENT`) | reset solo se cartella esistente, o primo antenato esistente non vuoto (`archiveFileConfirmedMissing`; `archive_mount_test.go`) |
| B2 | Errore transitorio di completamento = terminale + torrent staccato, nessun retry | retry transitori (EIO/ENOSPC/ESTALE/rete/DB occupato) dopo 1,2,4,8,16,32 min, poi definitivo; identità/file assenti subito definitivi (`isTransientCompletionError`, `completionRetries`; contatori in memoria) |
| B3 | Torrent aggiunto ma non registrato se la scrittura DB fallisce (ciclo abortito, torrent "esterno") | registrazione fallita → torrent ritirato, placeholder annullato, `continue` |
| B4 | `last_comics_check_ts` aggiornato anche se il ciclo fumetti fallisce (+7 giorni di buco) | timestamp solo a successo; errore → retry ~1 h |
| B5 | Errori DB fumetti abortiscono cicli serie/film | impostazioni fumetti lette solo se servono, errori degradati |
| B6 | Stop per "poco spazio" senza salvare statistiche né report | `finishCycleWithoutDownloads`: salva e riporta comunque (controllo globale invariato) |
| B7 | Config ricaricata male nel worker eventi (fallback = config di avvio) | resta l'ultima valida |
| B8 | Rename repair fuori da `cycle_lock` (concorrenza con ciclo/eventi/rinomine) | lock per-serie (`TryAcquireArchiveRename`/`AcquireArchiveImport`), marcatore import con conteggio; repair resta fuori dal lock ma mutuamente esclusivo per serie |

### 9.2 Illogicità e incoerenze (stato: corrette)

| Voce | Problema | Correzione |
|---|---|---|
| L1 | Booleana ridondante nella selezione (`incumbentWins` valutato 2 volte) | riscritta `covers && incumbentWins`, test esistenti |
| L2 | Supersede/remove non speculari → esito dipendente dall'ordine (confermato: episodio prima del pack = entrambi scaricati) | `mergeSeriesCandidate`/`releaseStrictlyBetter`: a parità vince chi copre di più (REMUX preferito); test con 2000 input rimescolati (fallisce con la vecchia regola) |
| L3 | Funzioni `Check*` che scrivono (approva-subito/annulla-poi fragile) | approvazione in due fasi: decisione in sola lettura → add torrent → conferma in scrittura + registrazione; rollback solo per registrazione fallita post-conferma (`approval_phases_test.go`) |
| L4 | `last_deep_gap_fill_ts` scritto prima del lavoro | salvato solo a passaggio completato e non annullato |
| L5 | Soglie gap non coordinate (deep 6 h vs 23 h hard-coded) | `gap_research_hours` mai sotto l'intervallo deep |
| L6 | Doppia notifica `torrent_finished` + `storage_moved` | `torrent_completed` una sola volta (`alreadyAnnounced`, `completionNotified`; `seeding:true` resta distinto) |
| L7 | Lingue mescolate in log e valori di dato (`"Feed RSS"` usato come chiave) | costanti `episodeOrigin*` condivise g2/g7/dettaglio (valori invariati: arrivano ad API/UI) |

### 9.3 Loop logici (stato: corretti)

| Voce | Problema | Correzione |
|---|---|---|
| C1 | Restart worker dopo panic senza backoff (ciclo panic→restart su evento "veleno") | `workerRestartBackoff`: attesa 5 s→5 min, reset dopo 10 min sani |
| C2 | Ricostruzione eventi a ogni tick (750 ms, doppia fonte di verità, mappe in memoria) | spostamenti salvati in DB (`torrent_moves`, ripristino a +60 s) + ricontrollo ogni 30 s invece che ogni giro; eventi restano il canale principale (`torrent_moves_restart_test.go`) |
| C3 | Lavoro lento inline nel loop (notifiche SMTP/HTTP, `SizeOfPath`, ffprobe, refresh Jellyfin/Plex) | notifiche async (`notifier.Async`, coda 256 best-effort; test di prova resta sincrono), `RefreshMediaLibraries` in goroutine con coalescenza |

### 9.4 Inefficienze (stato: corrette o chiuse con motivo)

| Voce | Problema | Esito |
|---|---|---|
| I1 | `DELETE` per ogni torrent a ogni tick su SQLite single-conn | solo per voci realmente persistite |
| I2 | Query per ogni torrent completo a ogni tick | cache `settledTorrents` 60 s (o fino a cambio percorso/evento) |
| I3 | `torrents.List()` ~10 volte per tick | rinviato (cache backend 500 ms mitiga) |
| I4 | `LoadConfig` pesante da molti punti, copie divergenti | convertito il worker che legge senza modificare; `LatestConfig` ha già cache invalidata al salvataggio |
| I5 | Stat di tutto l'archivio per un log di debug, sotto lock | solo con diagnostica attiva |
| I6 | Selezione O(n²) con ricalcoli | non fatto: quadratico solo entro serie+stagione, trascurabile |
| I7 | Una ricerca d'archivio per query | chiuso: non un problema nell'uso reale |
| I8 | `RecordSeenBatch` ignora errori in Debug | portato a WARN |

### 9.5 Secondo passaggio: parser, feed, notifiche, hook, fumetti, update

| Voce | Problema | Correzione |
|---|---|---|
| P1 | Parser su titoli reali (anno da `1917`/`2001`, `S2024E05` non riconosciuto, `Stagione 2 Episodio 3`, `2160p` nel nome, gruppo `dl` da `WEB-DL`, audio 5.1 da `x265.10bit`, `720p` da CAM) | anno = ultimo prima dei tag tecnici; stagioni a 4 cifre; `Stagione N Episodio/Puntata M` e `[Completa]`; risoluzione fuori dal nome; 5.1 isolato; `hd`/`pal` solo come parola; coda `WEB-DL`/`Blu-Ray` mai gruppo (`parser_regression_test.go`). Non errori: giornaliero S2024E133 (legacy voluto), sentinella `EpisodeRange=[0]` per i pack; non gestito: pack multi-stagione `S01-S03` |
| P2 | `FetchFeed`: pagina non scaricabile → tiene le precedenti; non analizzabile → butta tutto, senza log | tiene sempre le pagine lette, con log |
| P3 | Paginazione presunta base-0 (ext.to: `page=1` = prima pagina → early-stop, crawl fermo) | `listingPageNumber` per ext.to (verificato live); Corsaro invariato |
| P4 | Parser scelto da sottostringa sull'intero URL (query inclusa) | scelta da host+percorso (`feedKindSubject`) |
| P5 | Cache magnet indicizzata dal solo titolo (collisioni) | chiave = URL dettaglio; eviction dei più vecchi (non a caso) |
| P6 | Retry Telegram senza attesa iniziale + sleep finale inutile | attesa prima del tentativo |
| P7 | `smtp.SendMail` senza timeout nel thread chiamante (congela il worker) | `sendMailWithTimeout` 30 s (stesso STARTTLS/AUTH) |
| P8 | Token Telegram nei log (URL intero nell'errore) | `redactRequestError` (schema+host) + maschera `/bot<id>:<token>`; contraddiceva `SECURITY.md` |
| P9 | Hook: variabili espanse prima dello split (titoli con spazi/virgolette = injection argomenti) | prima `SplitArgs`, poi `Expand` per argomento (script che contavano sullo split vanno adattati) |
| P10 | Hook: timeout inefficace coi figli, concorrenza illimitata, niente recover | kill del gruppo processi, `WaitDelay` 5 s, max 4 concorrenti |
| P11 | Panic in goroutine fan-out = morte processo (solo 4 `recover`) | `recoverGoroutine` in 20 goroutine (engine/orchestrator/rss/hooks/handler/restart/qBittorrent) |
| P12 | Ciclo fumetti: download sincroni non annullabili sotto `cycle_lock` + Mega senza timeout + "primo file nuovo" ambiguo | watchdog inattività 2 min su HTTP, timeout 2 h + kill gruppo per `megadl`, interrompibile tra titoli (`comics_stall_test.go`) |
| P13 | Due parser HTML (700 righe artigianali + `x/net/html`) | non si corregge (scelta: va provato su pagine reali) |
| P14 | Checksum update che "fallisce in aperto" + tar senza `--no-same-owner` | non corretto (scelta) |
| P15 | Sessione libtorrent letta senza lock in ~40 metodi (abort su handle distrutto) | contatore + `enterSession`/`exitSession`/`drainSession` su 43 metodi: niente nuove chiamate in shutdown, attesa ≤30 s (`libtorrent_session_guard_test.go`) |
| P16 | Watcher: nessun limite profondità/dimensione/stabilità | in parte: profondità max 8, nomi specifici (limite dimensione e doppia osservazione già esistevano nel worker; `ReadDir` non segue symlink) |
| P17 | Naming misto snake_case/camelCase, prefissi di gruppo | non fatto (stile, nessun bug) |

Altre note sicurezza: P8/P14 le più concrete; P9 injection da titoli di feed;
nessuna restrizione SSRF (coerente con LAN, da ricordare se esposto).

Debito strutturale non fatto con motivo (S1–S8: funzioni da 1100/600/380
righe, duplicati g2/g7, nomi route, parsing booleani → però `settingTruthy`
uniforme fatto, S5, con bug `weekly_enabled` trovato; errori `_ =` in 81 punti,
mappe in memoria, traduzioni 3800 righe): rifattorizzazioni senza bug noti, da
fare a parte se si vuole. Cose verificate a posto: niente deadlock con
single-conn (uso di `tx`), copia atomica con fsync (senza checksum, rename
sovrascrive), backoff qBittorrent, fail-closed su blocklist illeggibile, SQL
parametrizzato.

Domande chiuse: B1 controllo aggiunto; B2 retry solo transitori; L6 doppia
notifica = errore; B8 esclusione per serie; P1 parser corretto; P14 checksum
invariato. Verifica complessiva: `go test ./...` e `go test -race .` verdi a
ogni tornata; ancora da fare: I3, I6, P17, S1–S4, S6–S8.

---

## 10. Rapporti Terra e Pro Terra

Ex `docs/archive/gextto-terra.md` e `docs/archive/pro-terra.md` (letture del
sorgente con migliorie concrete; nessuna vulnerabilità in produzione affermata;
nessun cambio di comportamento del daemon nei report stessi).

### 10.1 Terra: priorità e stato

Implementati punti 2, 3, 5 + primo passo del 7 (concorrenza indexer, contesto
radice di shutdown con attesa worker, test parità router/documentazione,
registrazione UI isolata). Punto 1 escluso su richiesta. Punti 4 e 6 restano
proposte progettuali deliberatamente non introdotte (cache archivio senza
invalidazione affidabile; sostituzione API C++ senza matrice versioni): niente
scorciatoie su correttezza decisioni o compatibilità build.

| Priorità | Area | Intervento | Esito |
|---|---|---|---|
| Alta | Esposizione HTTP | Separare/restringere il listener `EngineListen` (stesso `Router` su entrambe le porte, secondo accesso amministrativo senza auth) | escluso su richiesta |
| Alta | Ricerca provider | Budget concorrenza indexer (semaforo 4–6, default prudente) | fatto |
| Media | Arresto | Chiusura deterministica worker prima di libtorrent (contesto radice, registro operazioni, attesa quiescenza) | fatto |
| Media | Scalabilità | Indice archivio incrementale fuori dal ciclo critico (il ciclo rilegge la NAS) | proposta (indice persistito + timestamp, scansione come fallback; decisioni identiche) |
| Media | API | Riferimento API generato e verificato (route qBit mancanti dalla tabella) | fatto (test di parità) |
| Bassa | Bridge C++ | Rimuovere API libtorrent deprecate (`half_open_limit`, cache, priorità, tracker, resume) | proposta (matrice versioni + adapter, warning attesi come lista in CI) |
| Bassa | Router | Modularizzare la registrazione (`registerTorrentRoutes`, …) senza cambiare il contratto | primo passo fatto (registrazione UI isolata) |

Piano consigliato: esposizione listener → concorrenza indexer + test API →
shutdown → indice archivio + compatibilità libtorrent.

### 10.2 Pro Terra: priorità e stato

Seguito di Terra su sicurezza, concorrenza, lifecycle, integrità dati. Correzioni
applicate (LAN fidata: gli hardening puri di rete rinviati): arresto (punto 5 —
`trackOperation()` per `RunNow`/`RenameAll`/repair, attesa prima di distruggere
la sessione: chiude la corsa sull'handle); concorrenza (punto 6 —
`torrent_engine` sotto `engine_mu`, `RenameProgress` protetta, `categoryReady`
sotto `e.mu`, `safeGo` senza doppio report e restart interrompibile, handle
SQLite chiusi); integrità (punti 7–8 — upgrade serie/film transazionali,
niente `DELETE` su hash magnet vuoto); robustezza (punto 9 — errori non più
silenziosi, `gibSetting` saturato, limite dimensione `HTTPDownload`). Punto 1
parziale (causa risolta col tracciamento, disciplina `sessionMu` su ~90 metodi
raccomandata come difesa in profondità — poi fatta in revisione-2 P15).
Punti 2–4 rinviati (allowlist hook, filtro SSRF, redazione chiavi in
`ConfigView`): valide se mai esposto oltre la LAN.

| Priorità | Intervento | Dettaglio |
|---|---|---|
| Critica | Sincronizzare ogni lettura di `LibtorrentClient.session` | solo `listUncached` usava il lock; lettura senza lock + `Shutdown` che azzera = abort Boost su handle distrutto; proposta helper `withSession`/estensione `RLock` + stress test |
| Critica | Vincolare `Program` degli event hook | solo non-vuoto e assoluto: con `/bin/bash -c` = shell arbitraria da porta non autenticata; proposta allowlist o segreto separato + doc in manuale/`SECURITY.md` |
| Alta | Filtrare URL remoti (SSRF) e origini percorsi | `send-magnet`/`archive/add`/`search/add` validano solo `http(s)://`, client segue redirect; proposte helper condiviso (no privati/link-local/metadata, redirect limitati, no de-ref listener locale) + percorsi dentro le radici configurate |
| Alta | Non esporre chiavi in `ConfigView` | `api_key` indexer + `tmdb/tvdb_api_key` in chiaro (altri segreti già booleani); proposta soli flag |
| Alta | Tracciare goroutine dagli handler | `RunNow`/`RenameAll`/repair non in `bgWG`, toccano il motore dopo `ShutdownEmbedded` |
| Media | Stato condiviso | vedi sopra (fatto) |
| Media/Media | Transazioni DB + hash magnet vuoto | vedi sopra (fatto) |
| Bassa | Errori ignorati e limiti | vedi sopra (fatto); più `staticcheck`/`errcheck` in CI |

Piano: race handle + goroutine → hook + segreti → SSRF + percorsi → locking +
transazioni → errori ignorati + race/staticcheck in CI.

---

## 11. UI SSR + HTMX: report di migrazione

Ex `docs/archive/UI_V2.md` (2026-10-01, build 1229, libtorrent 2.0.11.0).
L'interfaccia ufficiale è SSR + HTMX, non dipende più da `gextto-ui.js`: è su
**`/`**, `/v2` reindirizza (301, query preservata), riusa API JSON, view-model
Go e CSS di base; tabelle renderizzate dal server via handler API in-process
(`v2InternalJSON`: stessa validazione, niente rete); i18n server-side con
tokenizer (`v2TranslateHTML`: text node, `title`/`placeholder`/`aria-label`,
fallback inglese, mai `<script>`/`<style>`, con IT byte-per-byte); JS minimale
(HTMX + `v2-core.js`: tema/font, modali accessibili); route UI isolate
(`v2Handle`, fuori dalla tabella route API: `API.md` e test di parità invariati);
vecchia route `/ui` rimossa.

Architettura (`uiweb_v2*.go`, `uiweb/v2/templates/v2.html`,
`uiweb/v2/static/{htmx.min.js,v2-core.js,v2.css}`, 1 riga in `web_router.go`):
shell/navigazione/i18n, renderer tabelle generico, pannelli Manutenzione/
Integrazioni, Esplora, dettagli Serie/Film, editor strutturati (feed, checkbox,
righe, rinomina, traduzioni), widget Manutenzione, widget speciali (duplicati,
DB, RAM disk, rinomina, OAuth/PIN, job, upload, TMDB, traduzioni per chiave,
anteprima rinomina); test in `uiweb_v2_test.go` /
`uiweb_v2_bench_test.go`.

Copertura: tutte le 16 voci di menu + dettagli (Dashboard con Prossime uscite
etichettate; Scarico con badge download ogni 5 s, limiti/storage,
tracker/file/web seed, storico, aggiunta, tag in massa; Serie/Film con hero
TMDB, stagioni on/off, Niente-upgrade, rinomina; Mancanti; Esplora con
calendario/tendenze/ricerca TMDB; Archivio con spiegazione decisione; Fumetti
con coda HTTP e weekly; Configurazione 4 aree/13 sezioni con Predefinito,
savebar, pannelli esperti/motore; Integrazioni Simkl/Jellyfin/Plex/FlareSolverr/
indexer/iCal/verifica sorgenti; Manutenzione con cestino/duplicati/DB/RAM
disk/rinomina/job; Salute; Log con colori server-side; Blocklist; Manuale e
Licenza server-side; `/login` per accesso protetto).

Residui consapevoli: "Ultimi trovati nei feed" su richiesta (diagnostica, non
operativa); aggiunta manuale avanzata solo campi principali (specialistici solo
via API); calendario via HTMX async; azioni lunghe nel pannello Operazioni in
background senza barra dedicata. Differenze accettate: ordinamento/filtro
server-side; modali con focus-trap/Escape/backdrop; log a intervalli invece di
SSE; job via polling `/api/jobs`.

Test: `go test .` e `./...` verdi (suite v2 completa); script indice
impostazioni (173 voci, 13 sezioni) e installer-selftest OK; vet/gofmt puliti;
live 16/16 voci → 200, 0 segnaposto; IT→EN con traduzioni reali. Benchmark
(N97, Go 1.26, CGO): `V2TranslateHTML` ~0,42 ms; `V2FormatCell` ~2,9 µs;
`V2RenderTableFragment` (500 righe) ~1,05 ms; `V2RenderShell` ~0,30 ms —
rendering ~1–3 ms/pagina, trascurabile su LAN (la traduzione è il costo
maggiore, solo per lingue ≠ IT). Correttezza: contratto API invariato, azioni
solo su path `/api/…`, salvataggio con gli stessi vincoli, editor a righe
compatibili. Promozione a default e rimozione UI classica completate (CSS
embedded in `uiweb/v2/static/`, view-model/handler condivisi mantenuti).
Prossimi passi: barra contestuale per azione lunga; campi specialistici nel
form; benchmark come test di performance in CI (poi chiuso, vedi §1.3).

---

## 12. Handover tra sessioni di lavoro

Ex `docs/lavoro-sessione.md` (aggiornato al 2026-10-07; copre dal commit
`d9e5d5e` del 06/10 sera al `432ce98` + commit `docs:` del 07/10).

### 12.1 Regole di lavoro (vincolanti, in aggiunta ad `AGENTS.md`)

1. Mai riferimenti a Claude/Anthropic in autore/committer/trailer/messaggi/PR
   (prevale su hook o promemoria dell'ambiente).
2. Identità git `buzzqw <azanzani@gmail.com>`, `commit.gpgsign false` in cloud
   (la firma cloud risulterebbe "Unverified").
3. Lavoro e push diretti su `main`; niente branch/PR salvo richiesta; ignorare
   branch `claude/...` assegnati dall'ambiente.
4. Niente force-push su `main`: fetch + merge prima del push, test rilanciati.
5. Italiano nelle risposte; testi UI nei cataloghi (ogni testo nuovo tradotto
   in tutte le lingue; `uiText` fuori template; TUI bilingue; notifiche
   it/en; log daemon solo inglese).
6. Pubblico italiano: mai dare per scontata la disponibilità all'air-date
   (date TMDB = "messa in onda originale").
7. Log leggibili (frasi, no dump), senza doppioni; dettagli in DEBUG; una riga
   INFO al giorno dai controlli che non trovano nulla.
8. Con "sistema"/"procedi in autonomia": decidere, fare, verificare, riferire
   in sintesi.

### 12.2 Lavoro svolto (06–07/10, per area)

- **Log e notifiche** (`d9e5d5e` e seguenti): pulizia completati che elenca i
  rimossi; rimozione manuale con una sola riga (destino cestino); niente
  notifica per pezzi danneggiati (resta WARN); backup: passo non configurato
  non è "non riuscito"; notifiche doppie eliminate (fumetti torrent ×2,
  "in attesa" a ogni ciclo, errori libtorrent 1 volta/6 h per torrent+tipo,
  archiviazione manuale già annunciata); backup Telegram >50 MB in parti da 45
  MiB con istruzioni `cat`.
- **Stalled, ciclo, FlareSolverr** (merge `281f89a`): backoff reale
  (intervallo → 3 h → 6 h → 12 h → 24 h, step salvato in DB, ogni tentativo
  loggato col prossimo orario); "📊 Downloads" solo al cambio numeri; backup una
  riga INFO; FlareSolverr con causa breve ed esito (chiusure appese in INFO coi
  domini); riepilogo ricerca coi motivi di scarto; dedup per hash feed/archivio
  ("releases checked" su release distinte); controllo libreria con una riga per
  rinomina/copia inferiore eliminata + riga giornaliera "all correctly named".
  Nota: titolo del merge con nome branch assegnato dall'ambiente (solo il
  proprietario può correggerlo con force-push).
- **TUI** (`internal/tui`): testi mancanti, lingua interfaccia vs release,
  aiuto 1-9 scorrevole, Salute in italiano (test chiavi); numeri sempre di
  scheda, ←→ in Libreria; avvio ricerca (Invio/s/f/m/c); log con `w` avvisi e ↓
  di una riga; tabella torrent (dimensione, ETA/ratio, seed/peer, niente hash);
  torrent bloccati spiegati + `F` con azzeramento ritardo (anche daemon in
  `MarkTorrentFailed`, `stalled_since`/`next_retry_at` in API); Stato con ultima
  ricerca in frase e migliaia separate; "⚠ N nuovi avvisi"; `/` anche sui
  torrent; aiuto sempre visibile in Stato.
- **Web su telefono** (`uiweb/v2`): layout ≤560 px (striscia stato toccabile,
  barra in basso una riga, menu Altro, Dashboard riordinata, Scarico con lista
  prima e opzioni dietro ⚙, azioni di gruppo solo con selezione); Log compatto
  con "Solo avvisi ed errori" (anche desktop); PWA (manifest, icone, service
  worker minimo, share target `/share`, pull-to-refresh); contatore WARN/ERROR
  (`ProblemCount`); fix template Scarico (div aperto); avvisi CodeQL (host
  validati, `nosniff`, redirect `/share` con `url.URL`, conversioni sicure).
- **Migliorie §2** (dettaglio sopra): hardlink, refresh mirato, upgrade-until,
  auth, iCal, ComicInfo, mancanti, badge, traduzioni, docs.
- **gx-torrent completato**: da scheletro non selezionabile a motore funzionante
  (coda autogestita con slot/tetto/lenti ignorati/rotazione/pin/probe/coda
  dinamica; stalled come park/probe/unpark persistente; symlink, delete sicura,
  rename-o-copia; token + loopback-only senza token, RPC del motore spento; avvio e
  sorveglianza con fallback a libtorrent dopo 3 crash/10 min; impostazioni
  token/eseguibile; matrice capacità onesta; test demone + adapter). Smoke test:
  avvio, verify→seed, pausa/ripresa, spostamento con symlink, kill -9 →
  riavvio, SIGTERM allo spegnimento. (Big Buck Bunny in `downloading_metadata`
  nel sandbox senza peer.)
- **Rete, selezione file, v2**: motore dentro `internal/gxcore` (`replace`,
  inventario `GEXTTO.md`); porta unica + DHT sulla stessa UDP + UPnP/NAT-PMP
  ogni 20 min; selezione file (esclusi in `DATA/parts`, progresso sui scelti);
  interfaccia ascolto/uscente (killswitch), proxy SOCKS5/HTTP
  (`gxtorrent_proxy`), cifratura 0/1/2, filtro IP multi-formato; **v2-only ora
  accettati end-to-end** (§4.4; all'epoca erano rifiutati con blocklist e poi
  superati); session-stats anche per gx-torrent; bug del motore (IP mai
  ricontattati, race logger); test reali demone↔demone (porta unica, cifratura,
  selezione, proxy, RAM disk a metà, filtro IP); smoke (flag di rete, porta
  6881, migrazione torrent, messaggio senza UPnP).
- **uTP, LSD, Salute**: socket UDP condiviso col DHT (`internal/dht`), dial
  parallelo uTP+TCP, LSD multicast BEP 14, pannello Motore torrent in Salute;
  test reali (solo-uTP, LSD); bug chiusura DHT su socket condiviso.
- **Dettaglio torrent + qBittorrent**: scheda con soli comandi supportati dal
  motore + nota lacune, file "Salta/Scarica", errori in scheda, righe
  sciame/motore, colonna Connessione peer; qBit: avanzamento peer (era ×0,01),
  connessione+flag, super-seeding sbloccato, "Salva tracker" che sostituisce;
  gx-torrent: avanzamento/seed da bitfield, tracker corrente, seed contati.
- **Cache automatica + log v2**: cache da RAM (lettura 1/32, scrittura 1/16) o
  da `libtorrent_cache_size`, scadenza + `fallocate`, "Ottimizza" anche per
  gx-torrent, riga in Salute; log v2-only (🔁 alternativa v1 / ⏳ in attesa /
  ⌛ non più tracciato dopo 30 giorni; memoria in RAM).
- **gx-torrent default**: `torrent_backend` mancante/vuoto = `gx-torrent`
  (installazioni esistenti invariate, `anacrolix` → `embedded`, fallback a
  libtorrent se non si attiva); servizio agnostico (`GEXTTO_LIBTORRENT` solo
  override); impostazioni rete/coda/cache condivise non più disabilitate con
  gx-torrent; README + `gx-torrent.md` aggiornati.

### 12.3 Punti aperti al 07/10 (storico; vari chiusi/superati)

> Elenco **datato 07/10/2026**, tenuto come storico. Nel frattempo sono stati
> chiusi il wizard/indexer e il supporto v2 (vedi §4.4 e
> [`gx-torrent-evoluto.md`](gx-torrent-evoluto.md) §12), quindi alcuni punti qui
> sotto vanno riletti come superati.

Da decidere: doppia notifica "completato — in seed" in `move_episodes`
(proposto di togliere la prima); backup Telegram come didascalia dell'ultima
parte; tasti TUI con significato per scheda (`d`, `R`, `i`, `e`); tabelle
Serie/Film/Archivio su telefono con dati veri (schede vs tabelle).
Da fare/verificare: PWA/condivisione su HTTPS reale; backoff stalled sul server
reale (ripresa da `notice_step`); indexer mircrew (solo in locale sul server);
titolo merge `281f89a`; «router port N opened» nel log demone (o apertura
manuale TCP+UDP); con VPN impostare interfaccia uscente (killswitch);
procedura gx-torrent (scelta motore, Big Buck Bunny, slot/pause); migliorie §2
(hardlink con `ls -li`, scansione cartella Jellyfin/Plex, login via proxy con
`X-Forwarded-For`, calendario con chiave TMDB).

### 12.4 Ambiente e insidie (sessione cloud, riferimento)

Build con `libtorrent-rasterbar-dev` (warning C++ normali); test radice ~60 s.
Dry-run con `GEXTTO_DATA_DIR` dedicato; stop via `kill` sui PID (mai
`pkill -f`, che uccide anche la shell). TUI in `tmux` + `capture-pane`;
pausa tra `Escape` e lettera (Alt+lettera). Web/telefono con Chromium +
Playwright preinstallati (config senza `channel: chrome`, `executablePath`
esplicito, data dir nuovo; 36 test verdi). Insidie: `gofmt` che riallinea
`internal/tui/i18n.go`; `go test` cached; push/force-push bloccati; `go test …
| grep … && git commit` committa anche con test rossi (controllare `$?`);
registro download fumetti globale nei test; `#` a inizio riga nei manuali =
titolo Markdown (lo trova l'e2e); manuale embedded (ricompilare prima degli
e2e). Mappa codice toccato e ripresa: vedi file originale in archivio.

---

## 13. Accessibilità

Ex `docs/accessibility-analysis.md` (analisi del 29-09-2026 su
`uiweb/v2/templates/`, `uiweb/v2/static/`, test in `uiweb/end2end/`; riferimento
WCAG 2.1 AA, pratiche compatibili 2.2).

**Stato**: interfaccia **parzialmente accessibile** con baseline automatizzata
molto migliorata; i controlli automatici passano ma non sono una certificazione
WCAG/EN 301 549/legale. Una conclusione formale richiede test manuali con
tastiera, screen reader, ingrandimento e tecnologie assistive su browser e
dispositivi supportati.

**Metodo**: review statica (HTML/CSS/JS), semantica/nomi/ARIA/focus, layout
responsive/focus visibile/reduced-motion, contrasto temi chiaro/scuro, axe-core
via Playwright, regression test (dialoghi, sort tastiera, polling, viewport
stretti). Suite: **12 test verdi** su dashboard, downloads, settings,
maintenance, health, logs (`cd uiweb/end2end && npm ci && npm run test:a11y`;
rete di sicurezza, non sostituto della valutazione manuale).

**Miglioramenti implementati**: lingua via attributo `lang`; landmark
(skip-nav, `aside`/`nav`/`header`/`main`); `aria-current="page"`;
nomi accessibili (non solo placeholder/tooltip); label su controlli a icona;
dialoghi (focus dentro, trap Tab/Shift+Tab, Escape, ripristino focus);
header ordinabili da tastiera con `aria-sort`; focus preservato sotto polling;
focus visibile; tabelle con caption/scope (anche dinamiche); progressbar con
valore; ARIA per tab/dialoghi/landmark; live region per stati/errori/notifiche/
async; simboli decorativi mai al posto del testo; ricerca impostazioni con
sinonimi (`memo`, `memory`, `RAM`, `cache`); contrasti tema chiaro scuriti dove
serviva; reflow mobile (tabelle con scroll interno); reduced-motion rispettato.

**Validazione residua** (prima di qualsiasi dichiarazione formale): workflow
primari con Tab/Shift+Tab/Invio/Spazio/frecce/Escape; almeno uno screen reader
desktop (NVDA+Firefox o VoiceOver+Safari) e TalkBack dove applicabile; zoom
200%/400% (overflow, dialoghi, tabelle, form); ogni form con screen reader
(riepilogo errori, associazione campi, stati invalidi, recovery); stati async
non solo-colore (loading/successo/warning/fallimento/vuoto/retry); entrambi i
temi, viewport mobili, reduced motion; comportamenti browser-specifici e
contenuti esterni iniettati.

**Criteri di accettazione**: nome+ruolo affidabili per ogni controllo; ogni
azione mouse da tastiera; focus contenuto/ripristinato nei dialoghi; annunci
async senza interruzioni eccessive; contrasti in entrambi i temi; semantica
usabile per tabelle/tab/progress/errori; nessun regresso su desktop e mobile.

---

## 14. gx-torrent standalone (gx-nox): rimando

**Estratto.** L'analisi completa e la decisione — evolvere `gx-torrent` con un solo
binario e due modalità oppure creare un nuovo `gx-nox` — sono in
[`docs/gx-torrent-evoluto.md`](gx-torrent-evoluto.md). Qui resta solo il rimando e
la decisione in una riga: **si evolve gx-torrent**, e "gx-nox" è il nome pubblico
della modalità standalone, non un secondo progetto. Il lavoro RSS/UI dello
standalone (editor guidati, `/ui/settings`, `/ui/browse-dir`) è tracciato in
[`docs/gx-torrent-evoluto.md`](gx-torrent-evoluto.md) §12.

---

## Appendice A: file consolidati

| File originale | Ora (copia locale, non tracciata) | Sezione qui |
|---|---|---|
| `docs/proposte-migliorie.md` | `docs/archive/proposte-migliorie.md` | §2 |
| `docs/da-fare.md` | `docs/archive/da-fare.md` | §1 |
| `docs/lavoro-sessione.md` | `docs/archive/lavoro-sessione.md` | §12 |
| `docs/scelte-di-progetto.md` | `docs/archive/scelte-di-progetto.md` | §3 |
| `docs/accessibility-analysis.md` | `docs/archive/accessibility-analysis.md` | §13 |
| `docs/holepunch-test-harness.md` | `docs/archive/holepunch-test-harness.md` | §7 |
| `docs/gx-torrent-migliorie.md` | `docs/archive/gx-torrent-migliorie.md` | §4 |
| `docs/gx-torrent-ottimizzazioni-prestazionali-v2.md` | `docs/archive/gx-torrent-ottimizzazioni-prestazionali-v2.md` | §5 |
| `docs/gx-torrent-misure-seeding.md` | `docs/archive/gx-torrent-misure-seeding.md` | §6 |
| `docs/revisione-1.md` | `docs/archive/revisione-1.md` | §8 |
| `docs/archive/revisione-2.md` | resta in `docs/archive/` (non tracciato) | §9 |
| `docs/archive/UI_V2.md` | resta in `docs/archive/` (non tracciato) | §11 |
| `docs/archive/gextto-terra.md` | resta in `docs/archive/` (non tracciato) | §10.1 |
| `docs/archive/pro-terra.md` | resta in `docs/archive/` (non tracciato) | §10.2 |
