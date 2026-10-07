# Audit aggiornamento rain: da v1.13.0 a v2.4.2

> **Stato: completato.** Base ribasata a v2.4.2, patchset gextto riapplicato e
> test verdi. Novità recepite: download sequenziale (+ bordi file) come opzione
> per-torrent e default di sessione. Le correzioni upstream di sicurezza, race
> e protocollo sono incluse con la nuova base. Guardie di regressione:
> `third_party/rain/internal/piecepicker/sequential_skip_test.go` e i test su
> opzione sequenziale in `cmd/gx-torrent` e adapter. Procedura per i prossimi
> allineamenti in `docs/rain-allineamento.md`.

Documento di lavoro. Base attuale del fork: **rain v1.13.0 (2024-09-18)**.
Ultima release upstream: **v2.4.2 (2026-10-03)**. Commit nel range: **177** (128 esclusi
bump di dipendenze e CI).

Il fork gextto è piccolo: **~1.100 righe su ~20 file** modificati, il resto della
libreria è intatto. Per questo la strategia consigliata è **ribasare il fork sulla
v2.4.2 e riapplicare il patchset**, non cherry-piccare i singoli commit.

## Perché la base è vecchia

- v1.13.0 è del 2024-09-18, contemporanea all'inizio dei lavori gx-torrent (commit
  gextto del 27/09–07/10/2026): la base era già vecchia di ~2 anni.
- Nessuna motivazione nel repo (`docs/`, `README`, `CLAUDE.md` non citano la versione).
- Il salto v1→v2 **non è una riscrittura**: v2.0.0 cambia solo i nomi dei campi del
  `config.yaml` (es. `portbegin` → `port-begin`), irrilevante per noi.

## A. Sicurezza / anti-DoS

| Commit | Cosa | Rischio conflitto |
|---|---|---|
| `8e7be84` | `metadata_size` negativo bypassava la validazione (DoS remoto) | basso |
| `0809892` | rifiuta messaggi peer oversize prima di allocare | basso |
| `38dad5b` | bounds-check richieste pezzo contro overflow uint32 | basso |
| `2dc8ea7` | conversione `int` errata (CodeQL) | basso |
| `edf37ac`/`d18403f` | tar-slip su `/move-torrent` (RPC rain non esposto) | basso |

## B. Race, leak di memoria e goroutine

| Commit | Cosa | Rischio |
|---|---|---|
| `80e3399` | scritture sbloccate sul bitfield (2.4.2) | medio (stop/verify nostri) |
| `58fcb3c` / `addd69a` | data race `StartAll/StopAll`, `torrentsByInfoHash` | medio |
| `9b0d485` | snapshot prima di Start/Stop (lock su chiamate bloccanti) | medio (coda demone) |
| `a1ee648` / `40df65f` | leak `connectedPeerIPs`; goroutine/context in `resolveAndAddPeer` | basso |
| `97d2b9d` | piece picker: leak peer choked su cancel/disconnect | medio (picker toccato) |
| `e3805ad` | timer `piececache` mai fermati | basso |
| `d5c4881` / `008dfde` | leak/contatore webseed | basso |
| `1bbc02e` | conteggio peer per-sorgente in `addrlist` | basso |
| `10137a2` | stato snubbed non pulito con dati in arrivo | basso |

## C. Correttezza protocollo

| Commit | Cosa | Rischio |
|---|---|---|
| `e3f3555` | backoff UDP tracker con XOR invece di esponenziale | basso |
| `c1d2fb5` | wire ID errato `AllowedFastMessage` | basso |
| `2216a9d` | filtro external-IP tracker HTTP scarta peer sbagliati | basso |
| `5754603`/`ec49299`/`0a9f8cf` | padding BEP47: write, webseed, `%2F` URL | basso |
| `5ecb782`/`1fca14f`/`a7a6a2c` | webseed: pezzi duplicati, pezzo già completo, pad path | basso |
| `85d17cf` | magnet ibridi con entrambi gli `xt` | medio (gestione v1/v2) |
| `4f12617`/`4c1af43` | attesa annuncio "stopped" alla chiusura | medio (ratio/seed) |
| `ff78e0f` | errori `stop()` con `%w` → `Stats.Error` ispezionabile | medio (adapter) |
| `209358b`/`9f71b77` | no panic/Fatal su peer source ignota / RPC | basso |

## D. Feature che coprono i "limiti noti" (docs/gx-torrent.md)

| Commit | Cosa | Limite coperto |
|---|---|---|
| `e1e8878` | sequential download | download sequenziale |
| `aea3e55`/`b1ae35d`/`26c799b`/`8318eb0` | prima/ultima parte e bordi file per primi | prima/ultima parte |
| `8bcf2d6` | `CustomStorage` in `Config` | layout storage (parts/symlink) |
| `3bce37f` | padding size nelle stats | (nuovo dato) |
| `fa1cac4` | `keep-data` su RemoveTorrent | (rimozione) |
| `66fb8ce` | path `$HOME` | (config) |

## E. Refactor necessari per la rebase

`0969642` (module path `/v2`), backoff v6/v7 (`28ad179` blocklist con `Retry`, tocca
il nostro file), `math/rand/v2`, `slices.SortFunc`, `atomic.Int32`, `1b24e47`
(`newTorrent2`→`newTorrent`), `812d787`/`29aee4d` (comandi come closure, tocca
`torrent_commands.go`), `e8a4831` (storage constructor), `267743b` (log handler,
può superare la nostra correzione).

## Mappa dei conflitti con il patchset gextto

File upstream toccati dal fork **e** riscritti a monte (merge manuale):

- `torrent/config.go` — yaml tags, `CustomStorage`, `$HOME` + i nostri 27 campi
- `torrent/session.go` — + i nostri 78
- `torrent/torrent.go` — rename + i nostri 3
- `torrent/torrent_commands.go` — command closures + i nostri 5
- `torrent/torrent_start.go`/`_stop.go`/`_run.go` — + nostre correzioni
- `torrent/session_add.go`/`_load.go` — + plumbing selezione file
- `internal/piecepicker/*` — edge+sequential upstream vs nostro flag `Skip`
- `internal/blocklist/blocklist.go` — `backoff.Retry` vs nostri formati
- `internal/storage/filestorage/*` — constructor/CustomStorage vs preallocazione
- `internal/trackermanager/*`, `internal/tracker/udptracker/transport.go` — backoff vs proxy
- `internal/logger/logger.go` — marshaler vs nostra race fix

File nuovi del fork, nessun conflitto (solo adattamento alle firme interne cambiate):
`internal/netx/netx.go`, `torrent/session_listen.go`, `torrent/torrent_selection.go`.

Import: `github.com/cenkalti/rain/...` → `github.com/cenkalti/rain/v2/...`
(7 righe in `cmd/gx-torrent`, più `sed` interno al fork).

## Piano proposto (rebase su v2.4.2)

1. Worktree dedicata; rimpiazzare `third_party/rain` con il sorgente v2.4.2
   (senza CLI/console/testdata/logo, con `GEXTTO.md`).
2. Cambiare module path a `/v2` in fork, `go.mod`, e i 7 import di `cmd/gx-torrent`.
3. Ricopiare i file nuovi (`netx`, `session_listen`, `torrent_selection`).
4. Merge manuale dei ~12 file in conflitto, combinando upstream + patch gextto
   (attenzione: `piecepicker` deve rispettare `Skip` anche nei nuovi percorsi
   edge/sequential).
5. Adattare i call-site interni ai cambi di firma (`newTorrent`, command closures,
   stats, storage).
6. `go build ./...`, `go test ./cmd/gx-torrent/`, `go test -run GxEngine .`, test di
   trasferimento locale (porta unica, selezione file, proxy, uTP+LSD).
7. Esporre le feature lato gextto (sequential, prima/ultima parte) e aggiornare
   `docs/gx-torrent.md` rimuovendo i limiti coperti.

Alternativa (più conservativa, meno consigliata): restare su v1.13.0 e portare a
mano solo i fix di A/B/C (~30 port, si resta su base vecchia e senza le feature D).
