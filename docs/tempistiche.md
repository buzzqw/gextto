# Tempi di build e test

Tempi misurati su un ambiente di riferimento reale, per dare un ordine di
grandezza a chi contribuisce o valuta il progetto. Sono **indicativi**: dipendono
dall'hardware, e i runner di GitHub hanno meno core di questo ambiente.

Misurazione del **2026-10-12**, codice al commit `07be7a2`.

## Ambiente di riferimento

| Voce | Valore |
|------|--------|
| Sistema | Debian GNU/Linux 13 (trixie), kernel 7.1, x86_64 |
| CPU | Intel **N97**, 4 core / 4 thread (mini-PC a basso consumo) |
| RAM | 15 GiB (nessuno swap) |
| Disco | NVMe Lexar NM620 256 GB (ext4) |
| Go | 1.27.1 |
| C/C++ | gcc/g++ 14.2 (servono per la variante libtorrent) |
| Docker | 29.9 |
| libtorrent | rasterbar 2.0.11 (`libtorrent-rasterbar-dev` presente) |

**Runner di CI**: GitHub Actions `ubuntu-24.04` (x86_64) e `windows-latest`, con
**2 vCPU** per runner. Le release Linux si compilano nel container `ubuntu:22.04`
(glibc 2.35) e, per aarch64, su `ubuntu-24.04-arm`.

## Build (locale)

| Operazione | Comando | Tempo |
|------------|---------|-------|
| Tutto da zero (cache Go **fredda**; `gexttod` + `gx-torrent`) | `go clean -cache && make build` | **~35 s** |
| Variante con **libtorrent** integrato (cgo) | `make build-libtorrent` | **~30 s** |
| Incrementale: una modifica a un pacchetto + relink | `make fast` | **~6 s** |
| Solo il demone `gx-torrent`, una modifica | `make gx-torrent` | **~1,5 s** |
| Nulla da fare (cache calda) | `make fast` | **< 1 s** |

Note:

- `make build` è **Go puro** (`CGO_ENABLED=0`); è anche l'unica che incrementa
  `build_number` e ricostruisce `gx-torrent` **solo se il suo codice è cambiato**
  (confronto di hash, `bin/gx-torrent.code-sha256`).
- `make build-libtorrent` (`CGO_ENABLED=1`) aggiunge la compilazione del bridge
  C++ verso libtorrent; i warning di deprecazione di libtorrent sono normali.
- Il grosso del tempo è l'engine `internal/gxcore`: il binario `gx-torrent` da
  solo ricompila in pochi secondi quando le sue dipendenze sono già in cache.

## Test

Conteggio (2026-10-12):

| | |
|--|--|
| Funzioni di test Go | **1656** |
| Benchmark Go | 16 |
| File `_test.go` | 289 |
| Pacchetti Go | 79, di cui **44 con test** |
| Modulo separato `internal/dht` | testato da `make test-dht` (fuori da `go test ./...`) |
| Test end2end Playwright (`uiweb/end2end`) | 48 |

Tempi locali (`-count=1`, quindi **non cachati**):

| Giro | Comando | Tempo |
|------|---------|-------|
| Test Go, motore predefinito (Go puro) | `CGO_ENABLED=0 go test -count=1 ./...` | **~96 s** |
| Test Go con libtorrent | `CGO_ENABLED=1 go test -count=1 ./...` | **~104 s** |
| Giro di guardia completo (`make test`) | `make test` | **~47 s** (cache calda) |

Il giro `make test` è `check-ui-settings-index` + `installer-selftest` +
`test-dht` + `go test ./...`; gli script costano insieme **< 1 s**
(`check-ui` ~0,03 s, `installer-test` ~0,6 s, `test-dht` ~0,1 s): **tutto il
tempo è la suite Go**.

Gli end2end Playwright **non** sono agganciati a `make` né alla CI (e il loro
harness parte dalla procedura guidata di primo avvio, quindi da una dir vuota
falliscono): è annotato in `docs/archive/da-fare.md`.

## CI (GitHub Actions)

Tempi wall dell'intera run (i job di solito girano in parallelo), da run recenti
su `main`; un runner standard ha 2 vCPU.

| Workflow | Quando | Wall | Job principali (secondi) |
|----------|--------|------|--------------------------|
| `ci.yml` | push e PR | **~4,5–8 min** (263–477 s) | *vet and test (x86_64)* 166–226 · *embedded libtorrent (x86_64)* 175–474 · *race detector* 375 (non bloccante) · *gx-torrent platform tests (Windows)* 45 · *service install (Windows)* 69 (non bloccante) · *container image* 44 |
| `continuous.yml` | push su `main` | **~4 min** (243–259 s) | *test* 122 · *package (x86_64/aarch64 × go/libtorrent)* 74–101, in parallelo · *publish* 11 |
| `codeql.yml` | push, PR e settimanale | **~3,5 min** | *analyze (go)* 203 |
| `release.yml` | tag | — (nessuna run recente da misurare) | *test* + matrix *package (`{x86_64,aarch64} × {go,libtorrent}`)* nel container `ubuntu:22.04`, con i pacchetti Windows accanto |

`continuous.yml` e `release.yml` pubblicano **8 pacchetti** (+ `release.json`):

- `gextto-linux-x86_64.tar.gz`, `gextto-linux-x86_64-libtorrent.tar.gz`;
- `gextto-linux-aarch64.tar.gz`, `gextto-linux-aarch64-libtorrent.tar.gz`;
- `gx-torrent-linux-x86_64.tar.gz`, `gx-torrent-linux-aarch64.tar.gz`;
- `gx-torrent-windows-amd64.zip`, `gx-torrent-windows-arm64.zip`.

## Come sono stati misurati

- Build: `go clean -cache` prima della misura a freddo; `time make …` per le
  altre; la misura incrementale aggiunge una riga a un file e lo ripristina.
- Test: `go test -count=1 ./...` (nessuna cache), una volta con
  `CGO_ENABLED=0` e una con `CGO_ENABLED=1`.
- CI: `gh run view <id> --json jobs` sulle run più recenti di `main`, con la
  durata di ogni job (`completedAt - startedAt`) e il wall della run
  (`updatedAt - createdAt`).

Per riprodurre:

```bash
go clean -cache && time make build        # build pura da zero
time make build-libtorrent                # variante libtorrent (cgo)
time (CGO_ENABLED=0 go test -count=1 ./...)   # suite Go non cachata
time make test                            # giro di guardia (cache calda)
```
