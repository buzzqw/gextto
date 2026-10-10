# Allineare il motore gx-core con l'upstream (ex rain)

> [!NOTE]
> **Aggiornamento 2026-10:** il motore è ora **codice nostro**. È nato da una
> copia di rain v2.4.2, ma vive dentro il modulo principale come
> `internal/gxcore` (`github.com/buzzqw/gextto/internal/gxcore`): non è più un
> modulo a sé né un `replace` in `go.mod`, e il prodotto si chiama **gx-core**.
> Non c'è quindi più un "rebase del fork" da fare periodicamente: **si guarda
> l'upstream a mano** (una volta a trimestre) e si **prende per valore** con un
> cherry-pick, come per libtorrent/anacrolix. Questa guida serve per il caso in
> cui si valuti un salto ampio di base. La licenza MIT originale resta in
> `internal/gxcore/LICENSE`.

Guida operativa per recepire le novità del progetto upstream **rain** (a cui il
motore deve solo l'origine MIT) nel motore in `internal/gxcore`. L'ultimo
allineamento storico è da **v1.13.0 (2024-09-18) a v2.4.2 (2026-10-03)**.

## 1. Come è fatto il motore

- `internal/gxcore` nasce da una **copia** di rain v2.4.2 (non un fork git, senza
  la storia di upstream), ora **parte del modulo principale**: gli import sono
  `github.com/buzzqw/gextto/internal/gxcore/...`.
- I file di `cmd/gx-torrent` importano
  `github.com/buzzqw/gextto/internal/gxcore/torrent`.
- Lo usano **solo** il demone `cmd/gx-torrent` e i suoi test. Gextto parla al
  demone via REST: l'adapter è `gxtorrent_engine.go`, l'avvio/sorveglianza
  `gxtorrent_runtime.go`.
- Il patchset gextto è piccolo e localizzato (~1.100 righe su ~20 file);
  i file nuovi sono `internal/netx/netx.go`, `torrent/session_listen.go`,
  `torrent/torrent_selection.go`. Il resto è codice upstream intatto.

Le modifiche sono elencate in `internal/gxcore/GEXTTO.md` e marcate nel codice
con `gextto fork`. **Prima di toccare la base, rileggi quella tabella**: è
l'inventario di ciò che va riapplicato.

## 2. Politica di versione

- Pinnare una **release taggata** (`vX.Y.Z`), non un commit di master.
- Aggiornare a ondate, non in continuo: un allineamento ogni tanto, fatto bene,
  con test verdi. Registrare versione e data nel titolo del commit.
- Non aggiornare la base e introdurre feature gextto nello stesso momento:
  prima l'allineamento "a parità di comportamento", poi le novità.

## 3. Procedura di allineamento (rebase a 3 vie)

Non fare copia-incolla manuale: usa git per ottenere un merge a 3 vie con
conflitti espliciti (base v1 → fork gextto → nuova base v2).

```sh
# 0) lavora su un branch
git checkout -b rain-vX.Y.Z

# 1) clona upstream e prendi la nuova release
git clone https://github.com/cenkalti/rain.git /tmp/rain-upstream
git -C /tmp/rain-upstream checkout vX.Y.Z

# 2) repo scratch con tre commit: base vecchia, fork, nuova base
rm -rf /tmp/rain-rebase && mkdir /tmp/rain-rebase && cd /tmp/rain-rebase
git init -q && git config user.email a@b.c && git config user.name rb
git -C /tmp/rain-upstream archive vVECCHIA | tar -x -C .
git add -A && git commit -qm "base vVECCHIA"; BASE=$(git rev-parse HEAD)
find . -mindepth 1 -maxdepth 1 ! -name .git -exec rm -rf {} +
rsync -a --exclude '.git' /path/gextto/internal/gxcore/ ./
git add -A && git commit -qm "gextto fork"; FORK=$(git rev-parse HEAD)
git branch gextto
git remote add up /tmp/rain-upstream && git fetch -q up vNUOVA
UP=$(git rev-parse FETCH_HEAD)

# 3) rebase del patchset sulla nuova base
git rebase --onto $UP $BASE gextto
```

Risolvi i conflitti (vedi §4), poi `git add -A && git rebase --continue`.

**Trappole già viste:**

- **Module path `/v2`**: il vecchio `github.com/cenkalti/rain/...` va in
  `github.com/cenkalti/rain/v2/...`. Attento al sed: `s#rain/#rain/v2/#`
  raddoppia i path già `/v2` → `rain/v2/v2/`. Usa un pattern che non tocchi
  `/v2/`: `s#"github.com/cenkalti/rain/#"github.com/cenkalti/rain/v2/#` solo sui
  file senza `/v2`, e verifica con `grep -rn 'rain/v2/v2/'`.
- **File rimossi dal fork** (CLI, `internal/console`, `internal/command`, test
  upstream, `testdata`, logo, CI): upstream li avrà modificati → conflitti
  modify/delete. Risolvi con `git rm`. Dopo, ripulisci eventuali file **nuovi**
  di upstream nelle cartelle rimosse (es. `internal/console` è stato diviso in
  più file in v2).
- **Test del fork da NON cancellare** con la pulizia: `internal/blocklist/…`,
  `internal/peerconn/…`, `internal/piecepicker/sequential_skip_test.go` e i
  `testdata` relativi. Non usare `find -name '*_test.go' -delete` senza
  escluderli.
- **`go.mod` / `go.sum`**: parti dal `go.mod` di upstream, riaggiungi le
  dipendenze del fork (`github.com/anacrolix/utp` per uTP) e poi
  `go mod tidy` dal modulo gextto. Il `replace github.com/nictuku/dht` sta nel
  `go.mod` di gextto, non in quello di rain.
- **`keepData`**: `Session.RemoveTorrent` ha ora un secondo parametro. Il demone
  passa `false` per restare sul comportamento "rimuovi solo il symlink".

## 4. Punti di integrazione da ricontrollare a mano

Sono i file dove le nostre patch incontrano modifiche upstream. Dopo la rebase,
verifica ognuno:

- **Piece picker** (`internal/piecepicker/`): upstream aggiunge nuovi percorsi
  di scelta (sequenziale, file edge, webseed). **Ogni** percorso che controlla
  `Done || Writing` deve controllare anche `Skip` (`PickableBy`,
  `pickSequential`, `pickAllowedFast`, `pickRarest`, `pickEndgame`,
  `pickStalled`, `AvailableForWebseed`, percorso webseed). Cerca i punti:
  `grep -rn 'Done ||' internal/piecepicker/`.
- **Storage/preallocazione** (`torrent/session_storage.go`,
  `internal/storage/filestorage/`): la creazione dello storage ora passa dal
  provider `fileStorageProvider`. Il flag `Preallocate` va portato lì
  (`GetStorage`), non più impostato a mano in `session_add.go`/`session_load.go`.
- **Rimozione torrent** (`torrent/session.go`, `cmd/gx-torrent/daemon.go`):
  nuovo parametro `keepData`.
- **Add options** (`torrent/session_add.go`): eventuali nuovi campi di
  `AddTorrentOptions` vanno esposti dal demone (vedi §5).
- **Config** (`torrent/config.go`): i campi gextto vivono dentro la struct
  `Config`; mantieni i tag `yaml:"..."` come upstream.
- **Import** (`cmd/gx-torrent/*.go`): il path del modulo.

Comandi utili dopo la rebase:

```sh
go build ./...
cd internal/gxcore && go test ./internal/blocklist/ ./internal/peerconn/ ./internal/piecepicker/
cd - && go test ./cmd/gx-torrent/ && go test -run GxEngine .
```

## 5. Recepire le novità in gextto (arricchimento)

Le novità di rain non "arrivano" da sole in gextto: se aprono un'opzione, va
esposta. Flusso completo per una nuova opzione per-torrent (esempio reale:
`sequential`):

1. **Demone** (`cmd/gx-torrent/`):
   - aggiungi il campo a `addRequest` e a `torrentMeta` (`daemon.go`);
   - valorizzalo in `add()` (`torrent.AddTorrentOptions{...}` e meta);
   - se è un default da sessione, aggiungilo a `QueueConfig` (`queue.go`): il
     `setConfig` accetta automaticamente la chiave e la espone in
     `GET/POST /api/v1/config`;
   - riportalo in `torrentInfo` / `infoLocked` se serve mostrarlo;
   - parse del form in `api.go` (`handleAdd`, `handleAddFile`) e in `ui.go`;
   - checkbox/etichetta nella pagina web (`cmd/gx-torrent/ui.go`).
2. **Adapter gextto** (`gxtorrent_engine.go`):
   - invia il campo in `gxAddForm` (per-torrent) e/o via `pushConfig` (default);
   - togli l'opzione dagli "unsupported" in `gxWarnUnsupportedOptions`;
   - implementa/aggiorna il metodo dell'interfaccia `TorrentEngine` (es.
     `SetSequential`);
   - se l'impostazione è in `uiweb_settings.go`/`uiweb_settings_defaults.go`,
     assicurati che esista il tooltip (`uiweb_tooltips.go`).
3. **Documenta**: `docs/gx-torrent.md` (opzione, API, configurazione, limiti) e
   `internal/gxcore/GEXTTO.md` se cambia l'inventario delle patch.
4. **Matrice capacità** (`torrent_engine.go`): aggiorna `capabilityLevels` per
   gx-torrent e, se serve, `v2DetailCapsFor`. È questo che rende l'opzione
   **visibile** in gextto: se lasci `"none"` (o `"partial"` sbagliato) l'UI
   continua a nasconderla anche se il motore la supporta. Ricorda che le
   opzioni legate alla creazione del picker sono `"partial"` (valgono solo per
   i torrent aggiunti dopo).

Ricorda il vincolo del motore: le scelte che dipendono dalla creazione del picker
(come `sequential`) valgono **solo per i torrent aggiunti dopo**. Se serve su
torrent già in corso, non è supportato e va detto chiaramente (nessun falso
successo: usa `ErrCapabilityUnavailable`).

## 6. Test che devono restare verdi

| Ambito | Comando | Copre |
|---|---|---|
| Adapter | `go test -run GxEngine .` | stati, eventi, park/probe, coda, storage_moved, opzioni inviate al demone |
| Demone | `go test ./cmd/gx-torrent/` | pianificatore coda, ciclo di vita reale, selezione file, trasferimenti (porta unica, cifratura, proxy, uTP, LSD), filtro IP, opzione sequenziale |
| Fork | `cd internal/gxcore && go test ./internal/blocklist/ ./internal/peerconn/ ./internal/piecepicker/` | formati filtro IP, byte di protocollo, **Skip rispettato dal picker** |
| Tutto | `go test ./...` | non-regressione generale |

I test in `internal/gxcore` sono **guardie di integrazione**: vanno tenuti
anche se il resto dei test upstream è stato rimosso, perché verificano proprio
le nostre patch (es. `sequential_skip_test.go` fallisce se un aggiornamento di
rain introduce un percorso di scelta pezzi che ignora `Skip`).

## 7. Checklist finale dell'allineamento

- [ ] `go build ./...` pulito.
- [ ] `go mod tidy` eseguito; `go.mod`/`go.sum` coerenti; nessun `rain/v2/v2`.
- [ ] `GEXTTO.md` aggiornato (versione base + tabella patch).
- [ ] Punti di integrazione §4 ricontrollati (grep di `Done ||`,
      `Preallocate`, `RemoveTorrent`).
- [ ] Tutti i test §6 verdi.
- [ ] Le nostre feature ancora funzionanti a mano: porta unica, uTP+LSD,
      selezione file, preallocazione, filtro IP, proxy/interfaccia uscente.
- [ ] Due test del fork "storici" presenti (`blocklist_test.go`,
      `peerconn_test.go`) più i nuovi.
- [ ] `docs/gx-torrent.md` aggiornato (limiti rimossi, nuove opzioni).
- [ ] Commit separato per (a) rebase della base e (b) arricchimento gextto.
