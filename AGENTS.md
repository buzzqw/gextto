# AGENTS.md — guida per agenti su gextto

Istruzioni operative per lavorare su questo repository. OpenCode carica
automaticamente questo file (V2 legge **solo** `AGENTS.md`, non `CLAUDE.md`).

## Regole commit e PR
- Mai "Claude" o "Anthropic" in autore, committer, `Co-Authored-By`, trailer o
  messaggi di commit.
- Mai "Claude" nel titolo o nel corpo delle PR; il titolo descrive la modifica
  (es. `fix(logs): …`).
- Autore dei commit: `buzzqw <azanzani@gmail.com>`.
- Non committare `build_number`, `bin/` né directory dati (`data/`,
  `gextto-data/`).

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

## Build e test
- `make build` — incrementa `build_number` (non committato) e compila
  `bin/gexttod` **e** `bin/gx-torrent`. Richiede CGO/libtorrent; i warning di
  deprecazione di libtorrent sono normali.
- `make gx-torrent` — solo il demone (`CGO_ENABLED=0`).
- Non usare `go build ./cmd/gx-torrent/` dalla root: scrive un binario
  `gx-torrent` nella root (ora ignorato). Per il demone usa `make gx-torrent`
  (produce `bin/gx-torrent`).
- `make test` — `check-ui-settings-index` + `installer-selftest` + `go test ./...`.
- Test mirati: `go test ./cmd/gx-torrent/` e `go test -run GxEngine .`.
- Test del fork (modulo annidato, non incluso in `./...`):
  `cd third_party/rain && go test ./internal/blocklist/ ./internal/peerconn/ ./internal/piecepicker/`.
- Invarianti da non rompere: `scripts/check-ui-settings-index.sh` (indice
  impostazioni UI) e `scripts/installer-selftest.sh`.

## Servizio in esecuzione su questa macchina
- È un **servizio systemd utente** (non di sistema): `~/.config/systemd/user/gextto.service`,
  esegue `/home/andres/gextto/bin/gexttod` dal checkout, `GEXTTO_DATA_DIR=/home/andres/gextto-data`,
  UI su `0.0.0.0:5000`. Il figlio `gx-torrent` lo avvia `gexttod`.
- Dopo una build: `systemctl --user restart gextto` (niente `sudo`).
- Verifica: `systemctl --user status gextto`;
  `curl -s http://127.0.0.1:5000/api/status` (campo `version` = `1.1.<build>`);
  `curl -s http://127.0.0.1:8890/api/v1/health` (demone gx-torrent).

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
