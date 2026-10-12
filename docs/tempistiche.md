# Build and test times

Times measured on a real reference machine, to give an order of magnitude to
whoever contributes to or evaluates the project. They are **indicative**: they
depend on the hardware, and GitHub runners have fewer cores than this machine.

Measured on **2026-10-12**, code at commit `07be7a2`.

## Reference machine

| Item | Value |
|------|-------|
| System | Debian GNU/Linux 13 (trixie), kernel 7.1, x86_64 |
| CPU | Intel **N97**, 4 cores / 4 threads (low-power mini PC) |
| RAM | 15 GiB (no swap) |
| Disk | NVMe Lexar NM620 256 GB (ext4) |
| Go | 1.27.1 |
| C/C++ | gcc/g++ 14.2 (needed for the libtorrent variant) |
| Docker | 29.9 |
| libtorrent | rasterbar 2.0.11 (`libtorrent-rasterbar-dev` installed) |

**CI runners**: GitHub Actions `ubuntu-24.04` (x86_64) and `windows-latest`, with
**2 vCPUs** per runner. Linux releases are built in the `ubuntu:22.04` container
(glibc 2.35) and, for aarch64, on `ubuntu-24.04-arm`.

## Build (local)

| Operation | Command | Time |
|-----------|---------|------|
| Everything from scratch (**cold** Go cache; `gexttod` + `gx-torrent`) | `go clean -cache && make build` | **~35 s** |
| Variant with the embedded **libtorrent** (cgo) | `make build-libtorrent` | **~30 s** |
| Incremental: one change to a package + relink | `make fast` | **~6 s** |
| Only the `gx-torrent` daemon, one change | `make gx-torrent` | **~1.5 s** |
| Nothing to do (warm cache) | `make fast` | **< 1 s** |

Notes:

- `make build` is **pure Go** (`CGO_ENABLED=0`); it is also the only target that
  increments `build_number`, and it rebuilds `gx-torrent` **only when its code
  changed** (hash comparison, `bin/gx-torrent.code-sha256`).
- `make build-libtorrent` (`CGO_ENABLED=1`) adds the compilation of the C++
  bridge to libtorrent; libtorrent's deprecation warnings are normal.
- Most of the time goes to the `internal/gxcore` engine: the `gx-torrent` binary
  alone recompiles in a few seconds when its dependencies are already cached.

## Tests

Count (2026-10-12):

| | |
|--|--|
| Go test functions | **1656** |
| Go benchmarks | 16 |
| `_test.go` files | 289 |
| Go packages | 79, of which **44 with tests** |
| Separate module `internal/dht` | tested by `make test-dht` (outside `go test ./...`) |
| Playwright end2end tests (`uiweb/end2end`) | 51 |

Local times (`-count=1`, so **not cached**):

| Round | Command | Time |
|-------|---------|------|
| Go tests, default engine (pure Go) | `CGO_ENABLED=0 go test -count=1 ./...` | **~96 s** |
| Go tests with libtorrent | `CGO_ENABLED=1 go test -count=1 ./...` | **~104 s** |
| Full guard round (`make test`) | `make test` | **~47 s** (warm cache) |

The `make test` round is `check-ui-settings-index` + `installer-selftest` +
`test-dht` + `go test ./...`; the scripts together cost **< 1 s**
(`check-ui` ~0.03 s, `installer-test` ~0.6 s, `test-dht` ~0.1 s): **all the time
is the Go suite**.

The Playwright end2end tests run with **`make test-ui`** (it builds the daemon,
starts it in dry-run and drives the real UI) and run in CI in the **`ui-e2e`**
job; the harness completes the first-start wizard by itself
(`uiweb/end2end/global-setup.ts`), so an empty data dir is no longer a problem.

## CI (GitHub Actions)

Wall times of the whole run (jobs usually run in parallel), from recent runs on
`main`; a standard runner has 2 vCPUs.

| Workflow | When | Wall | Main jobs (seconds) |
|----------|------|------|---------------------|
| `ci.yml` | push and PR | **~4.5–8 min** (263–477 s) | *vet and test (x86_64)* 166–226 · *embedded libtorrent (x86_64)* 175–474 · *race detector* 375 (non-blocking) · *gx-torrent platform tests (Windows)* 45 · *service install (Windows)* 69 (non-blocking) · *container image* 44 |
| `continuous.yml` | push to `main` | **~4 min** (243–259 s) | *test* 122 · *package (x86_64/aarch64 × go/libtorrent)* 74–101, in parallel · *publish* 11 |
| `codeql.yml` | push, PR and weekly | **~3.5 min** | *analyze (go)* 203 |
| `release.yml` | tag | — (no recent run to measure) | *test* + matrix *package (`{x86_64,aarch64} × {go,libtorrent}`)* in the `ubuntu:22.04` container, with the Windows packages alongside |

`continuous.yml` and `release.yml` publish **8 packages** (+ `release.json`):

- `gextto-linux-x86_64.tar.gz`, `gextto-linux-x86_64-libtorrent.tar.gz`;
- `gextto-linux-aarch64.tar.gz`, `gextto-linux-aarch64-libtorrent.tar.gz`;
- `gx-torrent-linux-x86_64.tar.gz`, `gx-torrent-linux-aarch64.tar.gz`;
- `gx-torrent-windows-amd64.zip`, `gx-torrent-windows-arm64.zip`.

## How they were measured

- Build: `go clean -cache` before the cold measurement; `time make …` for the
  others; the incremental measurement appends a line to a file and restores it.
- Tests: `go test -count=1 ./...` (no cache), once with `CGO_ENABLED=0` and once
  with `CGO_ENABLED=1`.
- CI: `gh run view <id> --json jobs` on the most recent runs of `main`, with the
  duration of each job (`completedAt - startedAt`) and the wall time of the run
  (`updatedAt - createdAt`).

To reproduce:

```bash
go clean -cache && time make build            # pure build from scratch
time make build-libtorrent                    # libtorrent variant (cgo)
time (CGO_ENABLED=0 go test -count=1 ./...)   # uncached Go suite
time make test                                # guard round (warm cache)
```
