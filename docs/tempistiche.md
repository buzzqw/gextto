# Build and test times

Times measured on a real reference machine, to give an order of magnitude to
whoever contributes to or evaluates the project. They are **indicative**: they
depend on the hardware, and GitHub runners have fewer cores than this machine.

The machine and the counts were re-checked on **2026-10-17** (build 1.1.1801); the
timed figures were measured on **2026-10-13**, code at commit `ef2c1fc`, and have not
been re-measured since: read them as orders of magnitude, not as current absolutes.

## Reference machine

| Item | Value |
|------|-------|
| System | Debian GNU/Linux 13 (trixie), kernel 7.1, x86_64 |
| CPU | Intel **N97**, 4 cores / 4 threads (low-power mini PC) |
| RAM | 15 GiB (no swap) |
| Disk | NVMe Lexar NM620 256 GB (ext4) |
| Go | 1.27.1 |
| Docker | 29.9 |

**CI runners**: GitHub Actions `ubuntu-24.04` (x86_64) and `windows-latest`, with
**2 vCPUs** per runner. Linux releases are built in the `ubuntu:22.04` container
(glibc 2.35) and, for aarch64, on `ubuntu-24.04-arm`.

## Build (local)

| Operation | Command | Time |
|-----------|---------|------|
| Everything from scratch (**cold** Go cache; `gexttod` + `gx-torrent`) | `go clean -cache && make build` | **~35 s** |
| Incremental: one change to a package + relink | `make fast` | **~6 s** |
| Only the `gx-torrent` daemon, one change | `make gx-torrent` | **~1.5 s** |
| Nothing to do (warm cache) | `make fast` | **< 1 s** |

Notes:

- `make build` is **pure Go** (`CGO_ENABLED=0`); it increments Gextto's
  `build_number` and, when the engine's code changed, also
  `gx-torrent.build_number` (the daemon has its own number). It rebuilds
  `gx-torrent` **only when its code changed** (hash comparison,
  `bin/gx-torrent.code-sha256`); `make gx-torrent` builds only the daemon and
  bumps only its own number.
- Most of the time goes to the `internal/gxcore` engine: the `gx-torrent` binary
  alone recompiles in a few seconds when its dependencies are already cached.

## Tests

Count (2026-10-17, build 1.1.1801):

| | |
|--|--|
| Go test functions | **2,108** |
| Go benchmarks | 42 |
| `_test.go` files | **413** |
| Go packages | 88, of which **59 with tests** |
| Separate module `internal/dht` | tested by `make test-dht` (outside `go test ./...`) |
| Playwright end2end tests (`uiweb/end2end`) | 57 |

Local times, measured **2026-10-13** on the reference machine (`-count=1`, so **not
cached**; "cold cache" means `go clean -cache` first, i.e. the whole suite is
recompiled). The suite has since grown by about a fifth, so expect more:

| Round | Command | Time |
|-------|---------|------|
| Go tests, default engine (pure Go), cold build cache | `go clean -cache && CGO_ENABLED=0 go test -count=1 ./...` | **~98 s** |
| Go tests, default engine (pure Go), warm build cache | `CGO_ENABLED=0 go test -count=1 ./...` | **~46 s** |
| Full guard round (`make test`) | `make test` | **~48 s** (warm cache) |

The `make test` round is `check-ui` (`scripts/check-ui-settings-index.sh`) +
`check-scripts` (`bash -n` and `shellcheck` when installed) + `installer-test`
(`scripts/installer-selftest.sh`) + `test-dht` + `go test ./...`; at the time of the
measurement the scripts together cost **< 1 s** (the `check-scripts` lint has been
added since); **all the time is the Go suite**.

The Playwright end2end tests run with **`make test-ui`** (it builds the daemon,
starts it in dry-run and drives the real UI) and run in CI in the **`ui-e2e`**
job; the harness completes the first-start wizard by itself
(`uiweb/end2end/global-setup.ts`), so an empty data dir works.

## CI (GitHub Actions)

Wall times of the whole run (jobs usually run in parallel), measured on
2026-10-13; a standard runner has 2 vCPUs. The job list is current, the times are
not: re-read them with `gh run view` before quoting them.

| Workflow | When | Wall | Main jobs (seconds) |
|----------|------|------|---------------------|
| `ci.yml` | push and PR | **~4.5–8 min** (263–477 s) | *vet and test (x86_64)* 166–226 · *UI end2end (Playwright)* · *gx-torrent platform tests (Windows)* 45 · *gx-torrent service install (Windows)* 69 (non-blocking) · *race detector (x86_64)* 375 (non-blocking) · *container image (x86_64)* 44 |
| `continuous.yml` | push to `main` | **~4 min** (243–259 s) | *test* 122 · *package (x86_64/aarch64)* 74–101, in parallel · *publish* 11 |
| `codeql.yml` | push, PR and weekly | **~3.5 min** | *analyze (go)* 203 |
| `release.yml` | tag | — (no recent run to measure) | *test* + matrix *package (`{x86_64,aarch64}`)* in the `ubuntu:22.04` container, with the Windows packages alongside |

`continuous.yml` and `release.yml` publish **6 packages** (+ `release.json`):

- `gextto-linux-x86_64.tar.gz`;
- `gextto-linux-aarch64.tar.gz`;
- `gx-torrent-linux-x86_64.tar.gz`, `gx-torrent-linux-aarch64.tar.gz`;
- `gx-torrent-windows-amd64.zip`, `gx-torrent-windows-arm64.zip`.

## How they were measured

- Build: `go clean -cache` before the cold measurement; `time make …` for the
  others; the incremental measurement appends a line to a file and restores it.
- Tests: `go test -count=1 ./...` (no test cache) with `CGO_ENABLED=0`; the cold
  figure also does `go clean -cache` first, so the whole suite and its test
  binaries are recompiled.
- CI: `gh run view <id> --json jobs` on the most recent runs of `main`, with the
  duration of each job (`completedAt - startedAt`) and the wall time of the run
  (`updatedAt - createdAt`).

To reproduce:

```bash
go clean -cache && time make build            # pure build from scratch
time (CGO_ENABLED=0 go test -count=1 ./...)   # uncached Go suite
time make test                                # guard round (warm cache)
```
