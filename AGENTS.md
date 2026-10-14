# AGENTS.md — guida per agenti su gextto

Istruzioni operative per lavorare su questo repository. OpenCode carica
automaticamente questo file, che è l'**unica** fonte delle regole operative.

## Regole commit e PR
- Autore dei commit: `buzzqw <azanzani@gmail.com>`.
- Non committare `bin/` né directory dati (`data/`, `gextto-data/`).
  `build_number` e `gx-torrent.build_number` invece **sono committati**: sono
  le sorgenti uniche dei numeri di build di Gextto e di gx-core, condivise tra
  checkout e CI (vedi *Build e test*).
- **Committa e pusha spesso**: cambiamenti piccoli e verificati si committano e
  si spingono su `origin/main` subito, senza accumulare lavoro non pushato.
- **Non committare né pushare se i test non passano tutti**: esegui la suite
  (`make test`, che include `check-ui`, `installer-test`, `test-dht` e
  `go test ./...`) e procedi solo se è **completamente verde**. Se un test
  fallisce, prima lo si sistema. Non c'è bisogno di far girare tutta la suite di
  test se la modifica riguarda esclusivamente la documentazione (es. file `.md`).
- **Riferisci sempre le azioni sull'ambiente.** Ogni volta che committi, pushi,
  ricompili (`make build`, `make gx-torrent`, …) o riavvii il servizio
  (`systemctl --user restart gextto`), **avvisa esplicitamente** nel messaggio,
  indicando il **numero del commit** (short hash, es. `a685e5f`) e il ramo/remoto
  del push (`origin/main`). Non dare per scontato che l'utente lo veda da solo:
  senza questa segnalazione l'utente non sa se il lavoro è stato committato,
  pushato, ricompilato e riavviato.

## Che cos'è gextto
- Daemon Go per acquisizione media e archiviazione. Il package principale è
  nella root; il `main` è in `cmd/gexttod`.
- UI server-side in `uiweb/v2` (SSR + HTMX), i18n it/en/de/fr/es/pl. I test
  end2end dell'UI sono Playwright (Node) in `uiweb/end2end`, non Go.
- **Motori torrent**: `gx-torrent` (default, Go puro), libtorrent integrato
  (fallback automatico), qBittorrent-nox esterno. Contratto in
  `torrent_engine.go`; adapter `gxtorrent_engine.go`/`gxtorrent_runtime.go`,
  `libtorrent.go`, `qbittorrent_engine.go`. Matrice capacità in
  `capabilityLevels` (`torrent_engine.go`): è la **fonte unica**. Da lì derivano
  l'API (`GET /api/torrent-backend`) e i flag
  della scheda di dettaglio (`v2DetailCapsFor` in `uiweb_v2.go`, test
  `TestV2DetailCapsFollowCapabilityMatrix`). Le operazioni che un motore non ha
  rifiutano con `ErrCapabilityUnavailable`; i blocchi solo-libtorrent usano
  `requireEmbedded`. La **visibilità delle impostazioni** è un asse diverso
  (`uiSettingAllowedBackends`/`uiLibtorrentEngineOnlySettings`): quali chiavi un
  motore legge, non quali operazioni supporta.
- `gx-torrent` è **sempre avviato e sorvegliato** da Gextto quando è il motore
  attivo (nessuna opzione per disattivarlo). Un demone esterno già in ascolto
  sull'URL viene usato così com'è.
- Il demone gestito **sopravvive ai riavvii di Gextto** (scope systemd
  `gextto-gx-torrent-*.scope`): al riavvio Gextto lo riaggancia se binario e
  opzioni sono uguali (`fingerprint` in `/api/v1/health`), altrimenti lo
  riavvia. Si ferma da solo dopo 15 minuti senza Gextto. Dettagli in
  `docs/gx-torrent.md`, *Startup*.

## Motore gx-core (`internal/gxcore`)
- `internal/gxcore` è il **motore** di gx-core (il binario `gx-torrent`), codice
  nostro originale, scritto in questo repository (le basi permissive
  iniziali sono state riscritte file per file), parte del modulo
  principale come `github.com/buzzqw/gextto/internal/gxcore`. Lo usano
  `cmd/gx-torrent` e i test.
- Panoramica e inventario delle funzionalità del motore:
  `internal/gxcore/GEXTTO.md`.
- Il DHT vive in `internal/dht`, un modulo a sé sotto il modulo principale.
- Analisi delle lacune rispetto a libtorrent e qBittorrent e piano di
  miglioramento: **`docs/evoluzione.md`** (§6–§7); evoluzione verso client
  standalone (gx-core): **`docs/gx-torrent-evoluto.md`**.

## Build e test
- `make build` — incrementa `build_number` (committato) e compila
  `bin/gexttod` **e** `bin/gx-torrent`. La build è **Go puro di default**
  (`CGO_ENABLED=0`, nessun libtorrent: `gx-torrent` è il motore predefinito).
  Per includere il motore **libtorrent integrato** usa `make build-libtorrent`
  (`GEXTTO_LIBTORRENT=1`), che attiva CGO e richiede compilatore C++17 e header
  libtorrent-rasterbar; i warning di deprecazione di libtorrent sono normali.
  `gx-torrent` viene sostituito solo se il suo codice è cambiato
  (`bin/gx-torrent.code-sha256`, `GEXTTO_FORCE_GXTORRENT=1` per forzarlo). Il
  confronto compila con `-buildvcs=false`: senza, Go marchia la revisione git
  nel binario e ogni commit sembrerebbe un cambiamento di gx-torrent.
- Senza cgo, `libtorrent_nocgo.go` (`//go:build !cgo`) fornisce gli stub del
  bridge e `LibtorrentCompiled()` è `false`: il backend `embedded` non è
  selezionabile (l'opzione non compare nella UI e la selezione è rifiutata);
  un `torrent_backend=embedded` salvato fa partire il demone su gx-torrent.
- `build_number` è la **sorgente unica** del numero di build: checkout e CI
  leggono lo stesso file (`build_number`, il valore corrente). La CI non usa più
  `github.run_number`: build e `release.json` prendono il valore dal file, così
  la versione installata ha lo **stesso** `1.1.<n>` del checkout. Per far
  avanzare il numero si fa `make build` e si committa il file aggiornato. Dopo
  un `git pull` che cambia `build_number`, ricompila per allineare il badge.
- gx-core (il demone) ha un **numero di build proprio**
  (`gx-torrent.build_number`, **committato**), separato da quello di Gextto:
  cresce di uno a ogni build reale di gx-torrent fatta con `make build` o
  `make gx-torrent` (`scripts/next-gx-build-number.sh` con
  `GEXTTO_BUMP_BUILD=1`) e resta invariato quando gx-torrent non viene
  ricostruito. La CI e `build-release.sh` **leggono** il valore committato
  senza incrementarlo, così la release ha lo stesso `1.1.<n>` del checkout;
  `release.json` lo pubblica come `gx_version`/`gx_build`. Dopo una build che
  ricompila gx-torrent si committa anche `gx-torrent.build_number`. È ciò che
  `gx-torrent --version`, `/api/v1/health` e la sua pagina web riportano come
  `1.1.<n>`; la UI di Gextto continua a usare `1.1.<build>`.
- `make gx-torrent` — solo il demone (`CGO_ENABLED=0`), con lo stesso numero di
  build proprio.
- Non usare `go build ./cmd/gx-torrent/` dalla root: scrive un binario
  `gx-torrent` (o `gx-torrent.exe` su Windows) nella root (ora ignorati). Per il
  demone usa `make gx-torrent` (produce `bin/gx-torrent`).
- **Windows**: il motore (`internal/gxcore`) e il demone (`cmd/gx-torrent`) sono
  Go puro e **cross-compilano** per Windows (amd64/arm64):
  `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/gx-torrent/`. Lo strato
  di piattaforma del demone è nei file `cmd/gx-torrent/*_linux.go` /
  `*_windows.go` / `*_other.go` (`fsinfo_*`, `mem_*`, `storageclass_*`,
  `gateway_*`, `defaultdir_*`, `link_*`, `storage_xdev_*`): su Windows memoria,
  spazio libero, filesystem di rete, classe HDD/SSD e gateway di default sono
  letti con le API Win32, il link di libreria è una **junction** (nessun
  privilegio symlink) e il move tra volumi riconosce `ERROR_NOT_SAME_DEVICE`.
  Attenzione: da Go 1.23 `os.Lstat` riporta una junction come `ModeIrregular`,
  non `ModeSymlink`: per riconoscere un link usa sempre `isDirLink`.
  Restano senza equivalente nativo solo le ottimizzazioni `fadvise` e `fallocate`
  (fallback `Truncate`). Il demone gira come **servizio Windows nativo**
  (`svc.IsWindowsService` → `svc.Run`, `service_windows.go`/`service_other.go`),
  avviato da `startDaemon` (estratto e condiviso col percorso interattivo).
  `scripts/package-gx-torrent-windows.sh` produce il pacchetto
  `gx-torrent-windows-<arch>.zip` (exe + `install-service.ps1` +
  `uninstall-service.ps1` + README), generato da `build-release.sh` e pubblicato
  nel release (`dist/gx-torrent-windows-*`). La CI (`ci.yml`, job `build-test`) ha degli
  step che cross-compilano **sia il motore sia il demone** per Windows, così il
  supporto non regredisce; il job `windows-platform` esegue su `windows-latest`
  i test di piattaforma (`platform_windows_test.go` e i test portabili, `go test
  -run "Windows|MountPoint|Setup|CrossDevice|DaemonStart" ./cmd/gx-torrent/`);
  il job `windows-service` (non bloccante) installa davvero il servizio con
  `scripts/gx-torrent-service-selftest.ps1` (install/start/health/stop). Il buffer della junction è costruito
  da `mountPointReparseData` (`junction.go`), testato anche su Linux. Gextto intero (`cmd/gexttod`) **non** è Windows:
  TUI, gestione processi/systemd, metadati fs e installer/updater sono Unix
  (vedi `docs/gx-torrent.md`, *Standalone use → Platforms*).
- `make test` — `check-ui` + `installer-test` + `test-dht` +
  `CGO_ENABLED=0 go test ./...` (Go puro; i test del motore libtorrent sono dietro
  `//go:build cgo`). `make test-libtorrent` (`CGO_ENABLED=1 go test ./...`) esegue
  anche quelli e richiede libtorrent.
- **Giro di controllo a ogni chiusura di ciclo** (motore o demone): `make test`,
  `make test-engine` e `make vet`, più gli invarianti qui sotto. Il resto della
  verifica obbligatoria (TUI, performance) è nei punti seguenti.
- `make fuzz-smoke` — fuzzing breve dei parser esposti alla rete (DHT, bencode
  `.torrent`, magnet, extension BEP 10, file tree BEP 52, tracker, Torznab, RSS,
  MSE), `FUZZ_TIME` secondi per target (default 30s, es. `FUZZ_TIME=2m` per un
  passaggio serio). Non fa parte di `make test`; i crash trovati vanno corretti e
  i loro input committati come seed in `testdata/fuzz`. Vedi `docs/evoluzione.md`
  §6.
- Test mirati: `go test ./cmd/gx-torrent/` e `go test -run GxEngine .`.
- Test del motore (`internal/gxcore`): `make test-engine` (`go test` dalla root sui
  pacchetti del motore). Fa parte del modulo, quindi `go test ./...` lo include
  già; `make test-engine` resta per eseguire solo quei pacchetti.
- Invarianti da non rompere: `scripts/check-ui-settings-index.sh` (indice
  impostazioni UI) e `scripts/installer-selftest.sh`.
- **Igiene dei test**: nessun test deve lasciare file fuori dalle proprie
  cartelle temporanee. Usa `t.TempDir()` (o `os.MkdirTemp` con
  `t.Cleanup`/`defer os.RemoveAll`), mai percorsi relativi alla CWD. Un `Daemon`
  costruito a mano senza `StatePath` non deve scrivere stato (`saveLocked` esce
  subito): senza la guardia creerebbe un `.tmp` nella directory di lavoro e lo
  lascerebbe lì perché il rename verso il percorso vuoto fallisce.
- **La TUI di Gextto deve sempre continuare a funzionare.** Dopo ogni ciclo di
  modifiche esegui `go test ./internal/tui/` e il test d'integrazione
  `go test . -run TestTUILibraryClientAgainstTheRouter` (pilota il client TUI
  contro l'API reale), più lo smoke `gexttod tui` contro il daemon. Includi
  sempre la verifica TUI nel giro finale di controllo.
- **Performance (CPU e RAM).** Dopo ogni ciclo di modifiche al motore o al
  demone fai un **controllo approfondito di performance**: CPU, RAM (RSS e
  allocazioni) e latenza/throughput dei trasferimenti. Ogni sostituzione o
  ottimizzazione deve essere **almeno equivalente** alla precedente, se non
  migliore: misura prima e dopo (`go test -bench … -benchmem`, profili `pprof`
  CPU/heap, footprint del demone e di `gx-torrent`, `make measure-seeding`) e
  annota i numeri in `docs/evoluzione.md`.

## Release, installer e aggiornamenti
- Il pacchetto di release si compila **su Ubuntu 22.04** (glibc 2.35, libtorrent
  2.0.5) con `scripts/build-release.sh`: in locale gira in un container
  docker/podman, in CI (`continuous.yml`, `release.yml`) i job `package` usano
  `container: ubuntu:22.04` per x86_64 e aarch64 (`ubuntu-24.04-arm`). Un
  binario compilato su un sistema più nuovo non parte su Debian 12/Ubuntu 22.04.
  Il bridge C++ deve compilare anche con libtorrent 2.0.5 (`LIBTORRENT_VERSION_NUM`).
- Ogni architettura esce in due varianti (matrice `variant` dei job `package`):
  `gextto-linux-<arch>.tar.gz` (Go puro, predefinita) e
  `gextto-linux-<arch>-libtorrent.tar.gz` (`GEXTTO_LIBTORRENT=1`, con il motore
  libtorrent integrato). `install.sh --libtorrent` installa la seconda e
  `gexttod --update` resta sulla variante installata (`LibtorrentCompiled()`).
- `scripts/package-gx-torrent.sh` produce l'archivio **standalone**
  `gx-torrent-linux-<arch>.tar.gz` (binario + unit systemd + README), che
  `build-release.sh` genera nella variante Go pura e la CI pubblica accanto a
  quelli di Gextto: è il client usabile da solo, come qbittorrent-nox, senza
  Gextto. `scripts/package-gx-torrent-windows.sh` fa lo stesso per Windows
  (`gx-torrent-windows-<arch>.zip`, amd64/arm64, servizio nativo).
- `scripts/build-daemon.sh` imprime nel binario `constants.Commit` e
  `constants.BuiltAt` (anche in `gx-torrent`); `scripts/release-manifest.sh` scrive `release.json`
  (versione, commit, ultimi commit, con `"gx": true` sui commit che toccano il
  codice di gx-torrent, cioè i `GX_PATHS` tenuti allineati da
  `TestReleaseManifestCoversDaemonPackages`) pubblicato accanto ai pacchetti: è
  ciò che legge il controllo aggiornamenti in-app di Gextto (`update_check.go`)
  e di gx-torrent standalone (`cmd/gx-torrent/update.go`). Solo
  `build-release.sh` imprime `constants.GxReleaseChannel`: una build locale di
  gx-torrent non si aggiorna da sola.
- `install.sh` installa anche `gextto-update.path`/`.service`: la UI scrive
  `<data>/update-request`, la unit root esegue `gexttod --update` (log in
  `/var/log/gextto-update.log`), che tiene `gexttod.prev` e lo ripristina se la
  nuova versione non parte.
- Utente del servizio: per default è **l'utente locale che esegue l'installer**
  (`SUDO_USER`), così può creare cartelle nei media di quell'utente; `--user
  gextto` (o `GEXTTO_USER`) usa l'account di sistema isolato e senza login, con
  `--media-group` per l'accesso ai media altrui. Il gruppo della unit è il gruppo
  primario dell'utente (`id -gn`), non necessariamente il nome utente.
- Prova reale dell'installer: container con systemd (`--privileged
  --cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup:rw`) e
  `install.sh --local-archive dist/gextto-linux-x86_64.tar.gz`.
- La procedura guidata di primo avvio è `uiweb_v2_setup.go` (`/?view=setup`).

## Code scanning (CodeQL)
- Workflow `.github/workflows/codeql.yml` (linguaggio Go; gira su push/PR su
  `main` e una volta a settimana). Non serve `sudo` né CGO per l'analisi.
- **Model pack locale**: `.github/codeql/extensions/gextto-models` dichiara i
  validatori di percorso che CodeQL non riconosce da solo (`pathStartsWith`,
  `folderRenameWithin`, `comicDownloadFilename`). L'action non carica i pack
  locali da sola: servono gli `CODEQL_ACTION_EXTRA_OPTIONS` già impostati nello
  step *Analyze*. Se aggiungi un nuovo validatore di percorso, aggiungilo al
  model pack (`models/gextto.model.yml`) invece di sopprimere a tappeto.
- **Triage**: correggi gli alert reali (data-flow da input non fidato verso un
  sink); per i by-design o i falsi positivi dimetti con un motivo esplicito:
  `gh api -X PATCH repos/buzzqw/gextto/code-scanning/alerts/<n> -f state=dismissed
  -f dismissed_reason='false positive|won't fix|used in tests' -f dismissed_comment='…'`.
  Convenzioni: `go/path-injection` e `go/request-forgery` su percorsi/URL
  configurati dall'operatore (save path assoluti, fetch di `.torrent`/filtri IP)
  sono funzionalità intenzionali, API protetta da token; la crittografia di
  protocollo (RC4 in MSE, SHA-1 nel DHT) non è modificabile.
- Alert aperti: `gh api 'repos/buzzqw/gextto/code-scanning/alerts?state=open&per_page=100'`.

## Servizio in esecuzione su questa macchina
- È un **servizio systemd utente** (non di sistema): `~/.config/systemd/user/gextto.service`,
  esegue `/home/andres/gextto/bin/gexttod` dal checkout, `GEXTTO_DATA_DIR=/home/andres/gextto-data`,
  UI su `0.0.0.0:5000`. `gx-torrent` lo avvia `gexttod`, in uno scope systemd
  separato che resta vivo ai riavvii del servizio.
- **Quando serve, builda e riavvia**: dopo modifiche a motore/demone (o per
  verificare) esegui `make build` e `systemctl --user restart gextto`; non
  lasciare il servizio su un binario vecchio. Non c'è bisogno di buildare né di
  riavviare se la modifica riguarda esclusivamente la documentazione, **tranne
  se tocca i manuali** (`docs/MANUAL.*.md`): essendo incorporati nel binario
  (`go:embed`) e serviti dalla UI, modifiche ai manuali richiedono `make build`
  e riavvio del servizio per essere visibili. Il riavvio può richiedere ~1 minuto
  (caricamento del filtro IP e handover dello scope `gx-torrent`), quindi
  verifica dopo un'attesa, non subito.
- Dopo una build: `systemctl --user restart gextto` (niente `sudo`). Se
  `bin/gx-torrent` non è cambiato il log dice "gx-torrent … was already
  running: kept as it is"; altrimenti il demone viene riavviato.
- Log: `gextto-data/gextto.log` e `gextto-data/gx-torrent/gx-torrent.log`
  (entrambi ruotati a 5 MB × 4); `gx-torrent.crash.log` solo per i crash.
- Verifica: `systemctl --user status gextto`;
  `curl -s http://127.0.0.1:5000/api/status` (campo `version` = `1.1.<build>` di
  Gextto); `curl -s http://127.0.0.1:8890/api/v1/health` (demone gx-torrent:
  `version` = `1.1.<suo build>`, `pid`, `fingerprint`).

## Convenzioni di modifica
- Nuova opzione di un motore: demone (`cmd/gx-torrent`), adapter
  (`gxtorrent_engine.go`), matrice `capabilityLevels` (`torrent_engine.go`),
  poi documentazione. Le opzioni legate alla creazione del picker valgono solo
  per i torrent aggiunti dopo: dichiarale `"partial"`, mai falso successo.
- **Capacità dei motori**: `capabilityLevels` è l'unica fonte di verità. I flag della
  scheda di dettaglio si **derivano** dalla matrice (`v2DetailCapsFor`): non
  aggiungere mappe a mano, aggiungi una capacità. `TestCapabilitiesBooleanMatchesLevels`
  fissa che il booleano di `Capabilities()` è vero per `full`/`partial` e falso
  per `none`; `TestV2DetailCapsFollowCapabilityMatrix` blocca i flag UI attesi.
- Nuova impostazione UI: aggiungi default (`uiweb_settings_defaults.go`), indice
  (`uiweb_settings.go`, con sezione e `Group` del pannello: lo script di
  verifica rifiuta le voci senza gruppo), unità/valori speciali/dipendenza
  (`uiweb_settings_meta.go`, non nell'etichetta), tooltip (`uiweb_tooltips.go`),
  le traduzioni di etichetta e descrizione nei cataloghi `internal_translations*.yml`
  e la riga nell'Appendice A dei manuali. Il test
  `TestSettingsEveryPreviousOptionIsStillAvailable` conta le opzioni: se ne
  aggiungi una aggiorna i conteggi.
- Una nuova feature visibile va esposta **sia** nella UI di Gextto
  (`uiweb/v2`) **sia**, dove ha senso, nella pagina web del demone
  (`cmd/gx-torrent/ui.go`): non lasciarla raggiungibile solo via API.
- **Il `README` è documentazione a tutti gli effetti e va aggiornato a ogni
  modifica impattante per l'utente**, soprattutto quando è particolarmente utile
  o quando mette in risalto la bontà e la potenza di Gextto o di gx-core: non va
  lasciato indietro. Vale insieme per `README.md` e `README.it.md` (stessa
  sostanza nelle due lingue) e per `docs/MANUAL.it.md`/`docs/MANUAL.en.md` quando
  cambia il comportamento visibile (i manuali sono **embeddati** nel binario:
  vedi *Servizio in esecuzione*, quindi servono `make build` e riavvio).
  Documento tecnico motore: `docs/gx-torrent.md`; architettura:
  `docs/ARCHITECTURE.md`.
- **La documentazione va sempre tenuta aggiornata.** Nessuna modifica (codice,
  configurazione, comportamento visibile, roadmap) è da considerarsi finita
  finché i documenti di riferimento non la riflettono; vale anche per i
  documenti creati di recente (es. `docs/bep.md` per la conformità BEP).
- **Ogni evoluzione, miglioria o altro lavoro aggiorna anche i documenti di
  riferimento**, non solo il codice. In particolare: `docs/evoluzione.md`
  (analisi e lacune), `docs/gx-torrent-evoluto.md` (evoluzione del demone),
  `internal/gxcore/GEXTTO.md` (motore gx-core),
  `docs/gx-torrent.md` (documento tecnico del demone), `docs/bep.md` (conformità
  BEP), `docs/ARCHITECTURE.md`, `README.md`/`README.it.md` e l'indice
  `docs/README.md`. Una voce data per
  «assente»/«da fare» va chiusa o riqualificata quando il codice la implementa;
  tabelle di stato, capacità e conteggi (righe, test, versioni, percorsi) vanno
  riallineati quando cambiano. Vale anche per il lavoro non visibile all'utente
  (motore, coda, DHT, storage): quei documenti sono il posto dove resta tracciato.
- **`docs/evoluzione.md` è uno dei documenti più lavorati del repository e tiene traccia solo delle evoluzioni, MAI dei bug.** Il documento è la sede esclusiva per: (1) **lo stato attuale e le misurazioni effettuate** di Gextto, del demone e del motore (cosa c'è oggi, con i numeri misurati e il perché delle scelte); (2) **il futuro delle migliorie da fare** (backlog attivo, direttrici strategiche, backlog smartness e roadmap). **Si scrive in termini di _cosa c'è_, non di cosa è stato fatto**: niente cronache di fix, di implementazioni («aggiunto», date di completamento, hash di commit), log di review per-commit o dettagli superati dal codice — quelli appartengono esclusivamente alla storia dei commit git. Restano invece le **misurazioni** (con macchina e data della misura, che ne fanno parte) e le **decisioni**, con la loro motivazione.
- Quando aggiungi o sposti documenti, aggiorna l'indice `docs/README.md`.

## Accessibilità dell'interfaccia
- Le pagine del demone gx-torrent (`internal/webui/assets`) usano il **design
  system condiviso**: `theme.css` (token di colore per tema chiaro e scuro,
  componenti), `common.js` (selettore del tema, notifiche) e lo sprite
  `icons.svg`, serviti su `/assets/`. Niente colori fissi nelle pagine: usa le
  variabili (`--text`, `--surface`, `--accent`, …) e i componenti esistenti
  (`.topbar`, `ul.items`, `dialog`, `.chip`, `.badge`, `.fsec`).
- **Ogni modifica alla UI deve controllare e rispettare l'accessibilità.**
  Vale per `uiweb/v2` (SSR + HTMX), per la pagina del demone
  (`cmd/gx-torrent/ui.go`) e per ogni pagina HTML generata.
- Requisiti minimi: HTML semantico e attributi ARIA corretti; ogni campo con un
  `label` associato; contrasto sufficiente e focus visibile; navigazione da
  tastiera completa (tab order logico, `Esc`/`Enter` coerenti, nessun controllo
  usabile solo con il mouse); testo alternativo per icone e immagini;
  messaggi di stato/errore annunciabili (`role="alert"`/`aria-live`).
- Verifica sempre prima di chiudere il lavoro: test automatici o controllo
  manuale di tastiera, focus, lettura di etichette e contrasto. Un controllo
  nuovo o modificato senza questa verifica non è da considerarsi finito.
- Vale anche per le stringhe tradotte (i18n): etichette e descrizioni devono
  restare comprensibili e con `label`/`aria-label` adeguati in tutte le lingue.
- **I campi dell'interfaccia devono essere sempre tradotti in tutte le lingue
  disponibili** (`it`, `en`, `de`, `fr`, `es`, `pl`). Etichette, descrizioni,
  tooltip, placeholder, messaggi e nomi dei campi non possono restare in una
  sola lingua: ogni stringa nuova va nei cataloghi (`internal_translations*.yml`
  per Gextto, `uiCatalog` per il demone) con la traduzione in **ogni** lingua.
  Guardie: `TestBundledCatalogsAreWellFormed`/`TestBundledCatalogsShareKeys` e
  `TestUIIsTranslatable` (Gextto), `TestUIPageIsFullyTranslatable` e
  `TestUICatalogTranslatedInEveryLanguage` (demone).
- **Ogni opzione deve avere il proprio tooltip, tradotto in tutte le lingue.**
  Vale per entrambe le UI (Gextto `uiweb/v2` e pagina del demone). Il tooltip va
  definito esplicitamente: in Gextto `uiweb_tooltips.go` per ogni chiave
  dell'indice `uiweb_settings.go`; nel demone un `title` (o `aria-label`) sul
  controllo, o sul `<label>` che lo racchiude, con la voce nei cataloghi i18n.
  Guardie automatiche: `TestSettingTooltipsAreTranslated` (Gextto),
  `TestEveryOptionHasATooltip` e `TestUIPageIsFullyTranslatable` (demone). Un
  tooltip mancante o non tradotto fa fallire i test.
