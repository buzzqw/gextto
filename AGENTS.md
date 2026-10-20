# AGENTS.md — guida per agenti su gextto

Istruzioni operative per lavorare su questo repository. OpenCode carica
automaticamente questo file, che è l'**unica** fonte delle regole operative.
Le regole valgono per ogni modifica, anche piccola: se un punto sembra in
conflitto con un altro, vale quello più specifico.

## Indice

1. [Prima di ogni commit](#prima-di-ogni-commit) — la checklist da non saltare
2. [Regole commit e PR](#regole-commit-e-pr)
3. [Che cos'è gextto](#che-cosè-gextto)
4. [Motore gx-core](#motore-gx-core-internalgxcore)
5. [Build e test](#build-e-test)
6. [Release, installer e aggiornamenti](#release-installer-e-aggiornamenti)
7. [Code scanning](#code-scanning-codeql)
8. [Servizio su questa macchina](#servizio-in-esecuzione-su-questa-macchina)
9. [Convenzioni di modifica](#convenzioni-di-modifica)
10. [`docs/evoluzione.md`](#docsevoluzionemd)
11. [Accessibilità dell'interfaccia e i18n](#accessibilità-dellinterfaccia)
12. [Dove sta cosa](#dove-sta-cosa) — indice dei documenti

## Prima di ogni commit

1. **Changelog** in italiano e inglese (`changelog.md`, `changelog.en.md`):
   stessa voce, spiegata per chi usa Gextto, senza dettagli interni.
2. **Manuali e README** se cambia qualcosa di visibile o di operativo
   (`docs/MANUAL.it.md`, `docs/MANUAL.en.md`, `README.md`, `README.it.md`).
3. **Documenti tecnici** toccati dalla modifica (vedi *Dove sta cosa* in fondo).
4. **Test verdi in locale**: `make test`, `make vet` e, se hai toccato la UI,
   `make test-ui`; più TUI, performance e invarianti descritti in *Build e test*.
5. **Commit** con autore `buzzqw <azanzani@gmail.com>`, poi push su
   `origin/main`.
6. **Riferisci** nel messaggio commit, push, build e riavvio con lo short hash
   e il ramo.

## Regole commit e PR
- **Autore**: `buzzqw <azanzani@gmail.com>`.
- **Changelog in entrambe le lingue.** Ogni commit aggiunge o aggiorna la voce
  in `changelog.md` e `changelog.en.md` (stesse sezioni, stessa sostanza). La
  voce parla a chi usa Gextto: cosa cambia, per chi, cosa deve fare. Niente
  hash, nomi di funzioni, elenchi di file. Un commit di sola build, test o
  documentazione interna non ha una voce propria: la sua parte visibile va
  nella voce del commit funzionale che accompagna.
- **Commit piccoli e verificati, pushati subito** su `origin/main`.
- **Non committare** `bin/` né le directory dati (`data/`, `gextto-data/`).
  `build_number` e `gx-torrent.build_number` invece **sono committati**: sono
  le sorgenti uniche dei numeri di build di Gextto e di gx-core, condivise tra
  checkout e CI (vedi *Build e test*).
- **Test prima del commit.** Con `make test` (che include `check-ui`,
  `installer-test`, `test-dht` e `go test ./...`) e `make vet`; se la modifica
  tocca la UI anche `make test-ui` (Playwright in `uiweb/end2end`, fuori da
  `make test` perché serve Node e un browser, ma gira in CI). Se tocchi build o
  impacchettamento prova anche `scripts/build-release.sh` o gli script
  `package-*` coinvolti. Si procede solo se è **tutto verde**; un test rosso si
  sistema prima. Per una modifica di sola documentazione (`.md`) non serve la
  suite completa.
- **Lo stato della CI si controlla a richiesta.** `gh run list` è lento: non
  interrogarlo a ogni ciclo. Questo non esonera dal lanciare **in locale** gli
  stessi controlli indicati sopra.
- **Riferisci sempre le azioni sull'ambiente.** Quando committi, pushi,
  ricompili (`make build`, `make gx-torrent`, …) o riavvii il servizio
  (`systemctl --user restart gextto`), scrivilo nel messaggio con lo **short
  hash** del commit e il ramo/remoto (`origin/main`). L'utente deve sapere se
  il lavoro è committato, pushato, ricompilato e riavviato.
- **Manuali, README e documenti tecnici** vanno aggiornati nello stesso commit
  quando la modifica cambia qualcosa di visibile o di operativo: la regola
  completa è in [Convenzioni di modifica](#convenzioni-di-modifica).

## Che cos'è gextto
- Daemon Go per acquisizione media e archiviazione. Il package principale è
  nella root; il `main` è in `cmd/gexttod`.
- UI server-side in `uiweb/v2` (SSR + HTMX), i18n it/en/de/fr/es/pl. I test
  end2end dell'UI sono Playwright (Node) in `uiweb/end2end`, non Go.
- **Motori torrent**: `gx-torrent` (default, Go puro) e `qBittorrent-nox`
  (esterno o gestito). Contratto in
  `torrent_engine.go`; adapter `gxtorrent_engine.go`/`gxtorrent_runtime.go` e
  `qbittorrent_engine.go`. Matrice capacità in
  `capabilityLevels` (`torrent_engine.go`): è la **fonte unica**. Da lì derivano
  l'API (`GET /api/torrent-backend`) e i flag
  della scheda di dettaglio (`v2DetailCapsFor` in `uiweb_v2.go`, test
  `TestV2DetailCapsFollowCapabilityMatrix`). Le operazioni che un motore non ha
  rifiutano con `ErrCapabilityUnavailable`. La **visibilità delle impostazioni**
  è un asse diverso (`uiSettingAllowedBackends`): quali chiavi un motore legge,
  non quali operazioni supporta.
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
- **Preferenza di progetto: codice nostro.** Quello che serve si scrive in questo
  repository, non si copia da altri.
- Panoramica e inventario delle funzionalità del motore:
  `internal/gxcore/GEXTTO.md`.
- Il DHT vive in `internal/dht`, un modulo a sé sotto il modulo principale.
- **Le funzioni standalone sono solo di gx-core.** Pagine e motori che non
  appartengono al demone gestito — RSS/feed/regole/indexer, Wanted/Cercati,
  Archivio dei feed, Impostazioni, wizard, ricerca indexer, cartelle watch —
  esistono **solo** in `-mode=standalone`; in `-mode=managed` sono spenti e le
  loro rotte rispondono 404, perché lì comanda Gextto (coda, categorie, RSS,
  ricerca, limiti) e due motori di acquisizione sulla stessa sessione non
  vanno bene. Non sono una lacuna da colmare: non vanno abilitati in managed.
  Chi vuole feed/regole/cercati usa un gx-torrent standalone (o l'istanza di
  prova di `scripts/gx-core-test.sh`). Dettagli: `docs/gx-core.md`.
- Analisi delle lacune rispetto a libtorrent e qBittorrent e piano di
  miglioramento: **`docs/evoluzione.md`** (§6–§7); evoluzione verso client
  standalone (gx-core): **`docs/gx-core.md`**.

## Build e test

Comandi di build, la verifica da chiudere prima di un commit e gli invarianti da
non rompere.

- `make build` — incrementa `build_number` (committato) e compila
  `bin/gexttod` **e** `bin/gx-torrent`. La build è **Go puro**
  (`CGO_ENABLED=0`: `gx-torrent` è il motore predefinito).
  `gx-torrent` viene sostituito solo se il suo codice è cambiato
  (`bin/gx-torrent.code-sha256`, `GEXTTO_FORCE_GXTORRENT=1` per forzarlo). Il
  confronto compila con `-buildvcs=false`: senza, Go marchia la revisione git
  nel binario e ogni commit sembrerebbe un cambiamento di gx-torrent.
- `build_number` è la **sorgente unica** del numero di build: checkout e CI
  leggono lo stesso file (`build_number`, il valore corrente). La CI non usa
  `github.run_number`: build e `release.json` prendono il valore dal file, così
  la versione installata ha la **stessa** versione del checkout. La versione
  mostrata è `1.1.<build>` fino alla build 1999; dalla 2000 il terzo numero
  riparte da 0 ogni 1000 build e cresce quello centrale (`1.2.0`, `1.2.999`,
  `1.3.0`, …), così il terzo numero non supera mai 999. La build intera resta
  visibile: tra parentesi nella versione mostrata dopo il rollover
  (`1.2.925 (build 2925)`) e in fondo alla pagina **Manutenzione**, con la
  versione, la build e il commit da cui è compilata. Per far avanzare il numero
  si fa `make build` e si committa il file aggiornato. Dopo
  un `git pull` che cambia `build_number`, ricompila per allineare il badge.
- gx-core (il demone) ha un **numero di build proprio**
  (`gx-torrent.build_number`, **committato**), separato da quello di Gextto:
  cresce di uno a ogni build reale di gx-torrent fatta con `make build` o
  `make gx-torrent` (`scripts/next-gx-build-number.sh` con
  `GEXTTO_BUMP_BUILD=1`) e resta invariato quando gx-torrent non viene
  ricostruito. La CI e `build-release.sh` **leggono** il valore committato
  senza incrementarlo, così la release ha la stessa versione del checkout;
  `release.json` lo pubblica come `gx_version`/`gx_build`. Dopo una build che
  ricompila gx-torrent si committa anche `gx-torrent.build_number`. È ciò che
  `gx-torrent --version`, `/api/v1/health` e la sua pagina web riportano:
  `1.1.<build>` fino alla build 999, poi `1.2.0` e di nuovo 0..999 nel terzo
  numero. La formattazione della versione è `versionFromBuild` in
  `internal/constants/constants.go`, replicata da `scripts/version.sh` per
  `release.json`. Gextto e gx-torrent usano la stessa funzione, ciascuno con il
  proprio punto di rollover.
- `make gx-torrent` — solo il demone (`CGO_ENABLED=0`), con lo stesso numero di
  build proprio.
- Non usare `go build ./cmd/gx-torrent/` dalla root: scrive un binario
  `gx-torrent` (o `gx-torrent.exe` su Windows) nella root (ora ignorati). Per il
  demone usa `make gx-torrent` (produce `bin/gx-torrent`).
- **Windows**: il motore (`internal/gxcore`) e il demone (`cmd/gx-torrent`) sono
  Go puro e **cross-compilano** per Windows (amd64/arm64):
  `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/gx-torrent/`.
  - Lo strato di piattaforma è nei file `cmd/gx-torrent/*_linux.go`,
    `*_windows.go`, `*_other.go` (`fsinfo_*`, `mem_*`, `storageclass_*`,
    `gateway_*`, `defaultdir_*`, `link_*`, `storage_xdev_*`): memoria, spazio
    libero, filesystem di rete, classe HDD/SSD e gateway di default sono letti
    con le API Win32; il link di libreria è una **junction** (nessun privilegio
    symlink) e il move tra volumi riconosce `ERROR_NOT_SAME_DEVICE`.
  - Da Go 1.23 `os.Lstat` riporta una junction come `ModeIrregular`, non
    `ModeSymlink`: per riconoscere un link usa sempre `isDirLink`. Restano senza
    equivalente nativo solo `fadvise` e `fallocate` (fallback `Truncate`).
  - Il demone gira come **servizio Windows nativo** (`svc.IsWindowsService` →
    `svc.Run`, `service_windows.go`/`service_other.go`), avviato da
    `startDaemon` (estratto e condiviso col percorso interattivo).
  - `scripts/package-gx-torrent-windows.sh` produce
    `gx-torrent-windows-<arch>.zip` (exe + `install-service.ps1` +
    `uninstall-service.ps1` + README), generato da `build-release.sh` e
    pubblicato nel release (`dist/gx-torrent-windows-*`).
  - CI: il job `build-test` cross-compila **motore e demone** per Windows; il
    job `windows-platform` esegue su `windows-latest` i test di piattaforma
    (`go test -run "Windows|MountPoint|Setup|CrossDevice|DaemonStart"
    ./cmd/gx-torrent/`); il job `windows-service` (non bloccante) installa
    davvero il servizio con `scripts/gx-torrent-service-selftest.ps1`
    (install/start/health/stop). Il buffer della junction è costruito da
    `mountPointReparseData` (`junction.go`), testato anche su Linux.
  - Gextto intero (`cmd/gexttod`) **non** è Windows: TUI, gestione
    processi/systemd, metadati fs e installer/updater sono Unix (vedi
    `docs/gx-torrent.md`, *Standalone use → Platforms*).
- `make test` — `check-ui` + `check-scripts` + `installer-test` + `test-dht` +
  `CGO_ENABLED=0 go test ./...` (Go puro).
- **Guardie statiche della UI** (girano con `go test ./...`): `TestUIActionPathsAreRegisteredRoutes`
  (ogni `Path: "/api/…"` dei bottoni/azioni della UI deve essere una rotta
  registrata: un endpoint rimosso non può lasciare un bottone morto che risponde
  404), `TestInternalJSONPathsAreRegisteredRoutes` (metodo e path di ogni
  chiamata `v2InternalJSON` e dei campi `Endpoint`/`GetPath`/`PostPath` delle
  pagine v2 devono risolversi sul router reale: il giro HTTP in-process non
  fallisce in build), `TestSettingsIndexIsWritable` (ogni opzione mostrata nella
  pagina impostazioni è salvabile) e `TestEnd2EndSelectorsReferenceRealSettings`
  (i selettori `#input-<key>`/`#v2-setting-<key>` e le `tab=` dei test
  Playwright devono esistere nell'indice/scheda: un rename non può lasciare la
  suite rossa solo in CI).
- `make test-ui` — suite end2end Playwright della UI (`uiweb/end2end`): costruisce
  il daemon, lo avvia in dry-run e pilota l'interfaccia reale in un browser
  (pagine, mobile, i18n, accessibilità axe). **Non fa parte di `make test`**
  (serve Node e un browser) ma la CI la esegue: va lanciata quando la modifica
  tocca la UI (`uiweb/v2`, template, JS/CSS, i18n, route v2/`/settings`).
- **Giro di controllo a ogni chiusura di ciclo** (motore o demone): `make test`,
  `make test-engine`, `make vet` e — quando la UI è toccata — `make test-ui`, più
  gli invarianti qui sotto. Il resto della verifica obbligatoria (TUI,
  performance) è nei punti seguenti. Lo stato della CI non si interroga a ogni
  ciclo: `gh run list` è lento e va lanciato **a richiesta**.
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
  impostazioni UI), `scripts/check-scripts.sh` (lint degli script: `bash -n`, più
  `shellcheck` quando è installato) e `scripts/installer-selftest.sh`.
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
  annota i numeri in `docs/measurements.md`.
- **Ogni volta che fai dei benchmark, aggiorna `docs/measurements.md`.** Vale per
  qualunque misura (benchmark Go, `make measure-*`, profili, RSS): il documento è
  il posto dove i numeri restano, con macchina e data — se una misura non è lì,
  non è stata fatta.

## Release, installer e aggiornamenti

Come nasce un pacchetto, come si installa e come un'installazione si aggiorna
(o torna indietro).

- Il pacchetto di release si compila **su Ubuntu 22.04** (glibc 2.35) con
  `scripts/build-release.sh`: in locale gira in un container
  docker/podman, in CI (`continuous.yml`, `release.yml`) i job `package` usano
  `container: ubuntu:22.04` per x86_64 e aarch64 (`ubuntu-24.04-arm`). Un
  binario compilato su un sistema più nuovo non parte su Debian 12/Ubuntu 22.04.
- Ogni architettura esce in un solo pacchetto Go puro:
  `gextto-linux-<arch>.tar.gz`; `gexttod --update` aggiorna l'installazione allo
  stesso pacchetto.
- `scripts/package-gx-torrent.sh` produce l'archivio **standalone**
  `gx-torrent-linux-<arch>.tar.gz` (binario + unit systemd + README), che
  `build-release.sh` genera e la CI pubblica accanto a
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
  `/var/log/gextto-update.log`), che tiene `gexttod.prev` (e `gx-torrent.prev`,
  come `install.sh` quando si rilancia) e lo ripristina se la
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
  validatori di percorso e di redirect che CodeQL non riconosce da solo
  (`pathStartsWith`, `folderRenameWithin`, `comicDownloadFilename`,
  `v2SearchRedirectTarget`). L'`input` di un `barrierModel` è il nodo che ferma
  il flusso (`ReturnValue`) e il `kind` è quello della query (`path-injection`,
  `url-redirection`, …), non un'etichetta libera. L'azione non carica i pack
  locali da sola: servono gli `CODEQL_ACTION_EXTRA_OPTIONS` già impostati nello
  step *Analyze*. Se aggiungi un nuovo validatore, aggiungilo al model pack
  (`models/gextto.model.yml`) invece di sopprimere a tappeto.
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
  lasciare il servizio su un binario vecchio. Il riavvio può richiedere ~1 minuto
  (caricamento del filtro IP e handover dello scope `gx-torrent`), quindi
  verifica dopo un'attesa, non subito.
- Dopo una build: `systemctl --user restart gextto` (niente `sudo`). Se
  `bin/gx-torrent` non è cambiato il log dice "gx-torrent … was already
  running: kept as it is"; altrimenti il demone viene riavviato.
- **Nessun documento è nella build.** Manuali e changelog sono mantenuti online
  e la UI apre i link a GitHub nella lingua dell'interfaccia
  (`docs/MANUAL.it.md`/`docs/MANUAL.en.md`, `changelog.md`/`changelog.en.md`):
  una modifica a un documento — `README`, `docs/*.md`, `AGENTS.md`,
  `changelog*.md` — si committa e si pusha, **senza `make build` né riavvio**.
  La pagina `/?view=manual` reindirizza al manuale online; non reintrodurre
  `go:embed` per i manuali, o si torna a ricompilare a ogni modifica.
- Log: `gextto-data/gextto.log` e `gextto-data/gx-torrent/gx-torrent.log`
  (entrambi ruotati a 5 MB × 4); `gx-torrent.crash.log` solo per i crash.
- Verifica: `systemctl --user status gextto`;
  `curl -s http://127.0.0.1:5000/api/status` (campo `version` = versione di
  Gextto: `1.1.<build>` fino a 1999, poi `1.2.0`, `1.2.999`, `1.3.0`, …);
  `curl -s http://127.0.0.1:8890/api/v1/health` (demone gx-torrent:
  `version` = la sua versione con la stessa regola dal build 1000, `pid`,
  `fingerprint`).

## Convenzioni di modifica

Le regole qui sotto dicono *dove* mettere una modifica. In caso di dubbio,
guarda come è già risolta una funzionalità simile.

### Capacità e opzioni di un motore
- Nuova opzione: demone (`cmd/gx-torrent`), adapter (`gxtorrent_engine.go`),
  matrice `capabilityLevels` (`torrent_engine.go`), poi documentazione. Le
  opzioni legate alla creazione del picker valgono solo per i torrent aggiunti
  dopo: dichiarale `"partial"`, mai falso successo.
- `capabilityLevels` è l'**unica fonte di verità**: i flag della scheda di
  dettaglio si **derivano** dalla matrice (`v2DetailCapsFor` in `uiweb_v2.go`),
  non con mappe a mano. `TestCapabilitiesBooleanMatchesLevels` fissa che il
  booleano di `Capabilities()` è vero per `full`/`partial` e falso per `none`;
  `TestV2DetailCapsFollowCapabilityMatrix` blocca i flag UI attesi.

### Nuova impostazione UI
Aggiungi, nell'ordine: default (`uiweb_settings_defaults.go`), indice
(`uiweb_settings.go`, con sezione e `Group` del pannello: lo script rifiuta le
voci senza gruppo), unità/valori speciali/dipendenza (`uiweb_settings_meta.go`,
non nell'etichetta), tooltip (`uiweb_tooltips.go`), traduzioni di etichetta e
descrizione in **tutti** i cataloghi `internal_translations*.yml` e la riga
nell'Appendice A dei manuali.
- `TestSettingsEveryPreviousOptionIsStillAvailable` conta le opzioni: se ne
  aggiungi una, aggiorna i conteggi.
- Una chiave dell'indice è **scrivibile per costruzione**
  (`gh7_setting_key_allowed` accetta `uiSettingIndexed`): non va in nessuna
  allowlist. `settingWritableAPIOnly` serve solo per le chiavi senza riga nella
  pagina (editor dedicati o sola API).
- Ogni scrittura — API, pagina impostazioni, wizard — passa da
  `validateSettingValue`: una nuova regola di validazione va lì, non nel singolo
  handler.

### Una feature visibile
Va esposta **sia** nella UI di Gextto (`uiweb/v2`) **sia**, dove ha senso, nella
pagina web del demone (`cmd/gx-torrent/ui.go`): mai raggiungibile solo via API.

### Documentazione e manuali
- **Manuali** (`docs/MANUAL.it.md` e `docs/MANUAL.en.md`): vanno aggiornati nello
  stesso commit quando cambia qualcosa di visibile o di operativo — template,
  JS/CSS, route, etichette, impostazioni, ma anche acquisizione, coda, archivio,
  seed, ricerca, demone, CLI/TUI. Se la modifica riguarda gx-torrent, controlla
  anche `docs/gx-torrent.md` e la sua pagina web. I manuali sono **online**:
  aggiornarli non richiede `make build` né riavvio.
- **README** (`README.md` e `README.it.md`, stessa sostanza): è documentazione a
  tutti gli effetti e va aggiornato a ogni modifica impattante per l'utente,
  soprattutto quando mette in risalto Gextto o gx-core.
- **Documenti tecnici**: ogni evoluzione o miglioria aggiorna anche i documenti
  di riferimento, non solo il codice. Vedi *Dove sta cosa* in fondo. Una voce
  data per «assente»/«da fare» va chiusa o riqualificata quando il codice la
  implementa; tabelle di stato, capacità e conteggi vanno riallineati. Vale anche
  per il lavoro non visibile (motore, coda, DHT, storage).
- **API**: ogni nuova rotta, rotta rimossa, cambio di metodo o parametri va
  allineato in `docs/API.md` nello stesso lavoro — per Gextto (`/api/*`,
  `web_router.go`) e per il demone (`/api/v1/*` nativo e `/api/v2/*` compatibile
  qBittorrent). Guardie: `TestAPIDocumentationMatchesRouter` (Gextto) e
  `TestAPIRoutesAreDocumented` (`cmd/gx-torrent`). Procedura in
  `docs/DEVELOPERS.md`, *Web UI and API changes*.
- Quando aggiungi o sposti documenti, aggiorna l'indice `docs/README.md`.

## `docs/evoluzione.md`

Solo due contenuti: lo **stato attuale** (cosa c'è e perché) e le **migliorie
future** con il loro gate. Non è un changelog né un manuale: niente bug, fix,
cronache di implementazione, date, numeri di build, hash di commit, istruzioni
d'uso o misure. Si scrive al presente e si descrive _cosa c'è_, non cosa è stato
fatto: una frase che fra sei mesi sarebbe falsa perché racconta una transizione
va tagliata. Quando una funzionalità arriva si aggiorna lo stato e si toglie il
punto aperto; le decisioni restano con la loro motivazione, i numeri in
`measurements.md`.

## Accessibilità dell'interfaccia
- Le pagine del demone gx-torrent (`internal/webui/assets`) usano il **design
  system condiviso**: `theme.css` (token di colore per tema chiaro e scuro,
  componenti), `common.js` (selettore del tema, notifiche) e lo sprite
  `icons.svg`, serviti su `/assets/`. Niente colori fissi nelle pagine: usa le
  variabili (`--text`, `--surface`, `--accent`, …) e i componenti esistenti
  (`.topbar`, `ul.items`, `dialog`, `.chip`, `.badge`, `.fsec`).
- **Procedura, test e criteri di accessibilità: `docs/ACCESSIBILITY.md`.** Ogni
  modifica alla UI deve controllare e rispettare l'accessibilità.
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

## Dove sta cosa

Indice dei documenti: `docs/README.md`.

| Cosa | Documento |
| --- | --- |
| Uso dell'interfaccia, comportamento visibile | `docs/MANUAL.it.md`, `docs/MANUAL.en.md` |
| Presentazione del progetto, build rapida | `README.md`, `README.it.md` |
| Architettura e mappa dei file | `docs/ARCHITECTURE.md` |
| Stato attuale e migliorie future del motore | `docs/evoluzione.md` |
| Misure di CPU/RAM/latenza (solo numeri, con macchina e data) | `docs/measurements.md` |
| Demone gx-torrent e motore gx-core | `docs/gx-torrent.md`, `docs/gx-core.md`, `internal/gxcore/GEXTTO.md` |
| Conformità BEP del motore | `docs/bep.md` |
| Rotte HTTP di Gextto e del demone | `docs/API.md` |
| Accessibilità (procedura e test) | `docs/ACCESSIBILITY.md` |
| Sviluppo: UI, API, test | `docs/DEVELOPERS.md` |
| TUI | `docs/tui.md` |
| Changelog pubblico | `changelog.md`, `changelog.en.md` |

Documenti online: la UI li apre da GitHub nella lingua dell'interfaccia. Non sono
nella build, quindi aggiornarli non richiede `make build` né riavvio.
