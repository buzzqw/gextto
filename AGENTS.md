# AGENTS.md — guida per agenti su gextto

Istruzioni operative per lavorare su questo repository. OpenCode carica
automaticamente questo file (V2 legge **solo** `AGENTS.md`, non `CLAUDE.md`).

## Regole commit e PR
- Mai "Claude" o "Anthropic" in autore, committer, `Co-Authored-By`, trailer o
  messaggi di commit.
- Mai "Claude" nel titolo o nel corpo delle PR; il titolo descrive la modifica
  (es. `fix(logs): …`).
- Autore dei commit: `buzzqw <azanzani@gmail.com>`.
- Non committare `build_number`, `gx-torrent.build_number`, `bin/` né directory
  dati (`data/`, `gextto-data/`).

## Che cos'è gextto
- Daemon Go per acquisizione media e archiviazione. Il package principale è
  nella root; il `main` è in `cmd/gexttod`.
- UI server-side in `uiweb/v2` (SSR + HTMX), i18n it/en/de/fr/es/pl. I test
  end2end dell'UI sono Playwright (Node) in `uiweb/v2/end2end`, non Go.
- **Motori torrent**: `gx-torrent` (default, Go puro), libtorrent integrato
  (fallback automatico), qBittorrent-nox esterno. Contratto in
  `torrent_engine.go`; adapter `gxtorrent_engine.go`/`gxtorrent_runtime.go`,
  `libtorrent.go`, `qbittorrent_engine.go`. Matrice capacità in
  `capabilityLevels` (`torrent_engine.go`): se il motore supporta un'opzione ma
  la matrice dice `none`, l'UI la nasconde.
- `gx-torrent` è **sempre avviato e sorvegliato** da Gextto quando è il motore
  attivo (nessuna opzione per disattivarlo). Un demone esterno già in ascolto
  sull'URL viene usato così com'è.
- Il demone gestito **sopravvive ai riavvii di Gextto** (scope systemd
  `gextto-gx-torrent-*.scope`): al riavvio Gextto lo riaggancia se binario e
  opzioni sono uguali (`fingerprint` in `/api/v1/health`), altrimenti lo
  riavvia. Si ferma da solo dopo 15 minuti senza Gextto. Dettagli in
  `docs/gx-torrent.md`, *Attivazione*.

## rain (motore gx-torrent)
- `third_party/rain` è una **copia vendored** di rain v2.4.2, modulo
  `github.com/cenkalti/rain/v2`, collegata con un `replace` in `go.mod`. La
  usano solo `cmd/gx-torrent` e i test del fork.
- Le modifiche gextto sono marcate con `gextto fork`; l'inventario è in
  `third_party/rain/GEXTTO.md`.
- Per aggiornare la base di rain: segui **`docs/rain-allineamento.md`**
  (procedura di rebase a 3 vie con git, trappole note, punti d'integrazione,
  test da tenere verdi).
- `third_party/dht` è la copia modificata di `nictuku/dht`.
- Analisi delle lacune rispetto a libtorrent/qBittorrent/anacrolix e piano di
  miglioramento: **`docs/gx-torrent-migliorie.md`**.

## Build e test
- `make build` — incrementa `build_number` (non committato) e compila
  `bin/gexttod` **e** `bin/gx-torrent`; quest'ultimo viene sostituito solo se
  il suo codice è cambiato (`bin/gx-torrent.code-sha256`,
  `GEXTTO_FORCE_GXTORRENT=1` per forzarlo). Il confronto compila con
  `-buildvcs=false`: senza, Go marchia la revisione git nel binario e ogni
  commit sembrerebbe un cambiamento di gx-torrent. Richiede CGO/libtorrent; i
  warning di deprecazione di libtorrent sono normali.
- Il demone ha un **numero di build proprio** (`gx-torrent.build_number`, non
  committato), separato da quello di Gextto: cresce di uno a ogni build reale di
  gx-torrent (`scripts/next-gx-build-number.sh`, usato da `make build` e da
  `make gx-torrent`) e resta invariato quando gx-torrent non viene ricostruito.
  È ciò che `gx-torrent --version`, `/api/v1/health` e la sua pagina web
  riportano come `1.1.<n>`; la UI di Gextto continua a usare `1.1.<build>`.
- `make gx-torrent` — solo il demone (`CGO_ENABLED=0`), con lo stesso numero di
  build proprio.
- Non usare `go build ./cmd/gx-torrent/` dalla root: scrive un binario
  `gx-torrent` nella root (ora ignorato). Per il demone usa `make gx-torrent`
  (produce `bin/gx-torrent`).
- `make test` — `check-ui-settings-index` + `installer-selftest` + `go test ./...`.
- Test mirati: `go test ./cmd/gx-torrent/` e `go test -run GxEngine .`.
- Test del fork (modulo annidato, non incluso in `./...`): usa `make test-rain`
  (`go test` dalla root sui cinque pacchetti con test). **Non** eseguire
  `cd third_party/rain && go test ./...`: il modulo annidato da solo non ha un
  `go.sum` completo e fallisce in fase di setup; anche `...` dalla root include
  `internal/jsonutil`, le cui dipendenze di test mancano dal `go.sum` principale.
- Invarianti da non rompere: `scripts/check-ui-settings-index.sh` (indice
  impostazioni UI) e `scripts/installer-selftest.sh`.

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
- Nuova impostazione UI: aggiungi default (`uiweb_settings_defaults.go`), indice
  (`uiweb_settings.go`), tooltip (`uiweb_tooltips.go`) e i testi dei manuali.
- Aggiorna sempre `README.md`/`README.it.md` e `docs/MANUAL.it.md`/`docs/MANUAL.en.md`
  quando cambia il comportamento visibile. Documento tecnico motore:
  `docs/gx-torrent.md`; architettura: `docs/ARCHITECTURE.md`.
