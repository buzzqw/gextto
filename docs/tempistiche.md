# Build and test times

What a build, the test suite and a CI run cost on a real reference machine, measured
on **2026-10-17** (build 1.1.1809). The figures are indicative: the hardware changes
them, and a GitHub runner has fewer cores than this machine.

## Reference machine

| Item | Value |
|---|---|
| System | Debian GNU/Linux 13 (trixie), kernel 7.1.13, x86_64 |
| CPU | Intel N97, 4 cores / 4 threads (low-power mini PC) |
| RAM | 15 GiB (no swap) |
| Disk | NVMe Lexar NM620 256 GB (ext4) |
| Go | 1.27.1 |
| Docker | 29.9.0 |

**CI runners**: GitHub Actions `ubuntu-24.04` (x86_64) and `windows-latest`, with
**2 vCPUs** per runner. Linux releases are built in the `ubuntu:22.04` container
(glibc 2.35) and, for aarch64, on `ubuntu-24.04-arm`.

## Build (local)

| Operation | Command | Time |
|---|---|---|
| Everything from scratch (cold Go cache; `gexttod` + `gx-torrent` probe) | `go clean -cache && make build` | **42.5 s** |
| One package changed, plus the link | `make fast` | **7.1 s** |
| Nothing to do (warm cache) | `make fast` | **1.1 s** |
| Only the gx-torrent daemon | `make gx-torrent` | **0.5 s** |

- `make build` is pure Go (`CGO_ENABLED=0`). It increments Gextto's `build_number` and,
  when the engine's code changed, also `gx-torrent.build_number`; it replaces
  `bin/gx-torrent` only when its code changed (hash comparison,
  `bin/gx-torrent.code-sha256`). `make gx-torrent` builds only the daemon and bumps only
  its own number.
- The cold figure is dominated by compiling the engine and the daemon; the incremental
  one is the changed package plus the link.

## Tests

Count (2026-10-17):

| | |
|---|---|
| Go test functions | **2,108** |
| Go benchmarks | 42 |
| `_test.go` files | **413** |
| Go packages | 88, of which **59 with tests** |
| Separate module `internal/dht` | tested by `make test-dht`, outside `go test ./...` |
| Playwright end2end tests (`uiweb/end2end`) | 57 |

Local times. "Cold build cache" means `go clean -cache` first, so the whole suite is
recompiled; `-count=1` disables the test result cache, so every test runs:

| Round | Command | Time |
|---|---|---|
| Go tests, cold build cache, every test run | `go clean -cache && CGO_ENABLED=0 go test -count=1 ./...` | **117.0 s** |
| Go tests, warm build cache, every test run | `CGO_ENABLED=0 go test -count=1 ./...` | **53.0 s** |
| Full guard round, nothing changed (test results cached) | `make test` | **57.3 s** |
| Full guard round after a code or build-number change | `make test` | **123.2 s** |
| Browser suite | `make test-ui` | **115.8 s** |

`make test` is `check-ui` (`scripts/check-ui-settings-index.sh`) + `check-scripts`
(`bash -n`, plus `shellcheck` when installed) + `installer-test`
(`scripts/installer-selftest.sh`) + `test-dht` + `go test ./...`. The scripts cost a few
seconds together; the rest is the Go suite.

`make test-ui` builds the daemon, starts it in dry-run and drives the real UI in
Chromium; the first-start wizard is completed by the harness
(`uiweb/end2end/global-setup.ts`), so an empty data dir works.

## CI (GitHub Actions)

Whole-run wall times and per-job durations from recent runs on `main` (2026-10-17); the
jobs run in parallel, so the wall time is shorter than the sum.

| Workflow | When | Wall | Jobs |
|---|---|---|---|
| `ci.yml` | push and PR | **411 s** | *vet and test (x86_64)* 264 s · *UI end2end (Playwright)* 114 s · *gx-torrent platform tests (Windows)* 74 s · *gx-torrent service install (Windows)* 86 s (non-blocking) · *container image (x86_64)* 45 s · *race detector (x86_64)* 391 s (non-blocking; it failed in this run and did not block) |
| `continuous.yml` | push to `main` | **278 s** | *test* 160 s · *package (x86_64)* 100 s · *package (aarch64)* 81 s · *publish continuous* 11 s |
| `codeql.yml` | push, PR and weekly | **210 s** | *analyze (go)* 207 s |
| `release.yml` | tag | — | no run in the period; *test* + matrix *package* in the `ubuntu:22.04` container, with the Windows packages alongside |

`continuous.yml` and `release.yml` publish **6 packages** plus `release.json`:

- `gextto-linux-x86_64.tar.gz`;
- `gextto-linux-aarch64.tar.gz`;
- `gx-torrent-linux-x86_64.tar.gz`, `gx-torrent-linux-aarch64.tar.gz`;
- `gx-torrent-windows-amd64.zip`, `gx-torrent-windows-arm64.zip`.

## How these were measured

- Build: `go clean -cache` before the cold measurement, `time make …` for the others;
  the incremental figure changes one statement in a non-test package and restores it.
- Tests: `time` around `go test -count=1 ./...` (no test result cache), with
  `CGO_ENABLED=0`; the cold figure also runs `go clean -cache` first. `make test` is
  measured with the test cache valid and again after a change that invalidates it.
- CI: `gh run view <id> --json createdAt,updatedAt,jobs`, with the wall time as
  `updatedAt - createdAt` and each job as `completedAt - startedAt`; runs of
  2026-10-17 (CI 39048101228, continuous 39049025870, CodeQL 39049025808).

To reproduce:

```bash
go clean -cache && time make build            # pure build from scratch
time make fast                                # one change + link (warm cache)
time (CGO_ENABLED=0 go test -count=1 ./...)   # every test, warm build cache
time make test                                # guard round
time make test-ui                             # browser suite
```
