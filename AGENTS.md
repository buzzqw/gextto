# AGENTS.md — guide for agents working on gextto

Operating instructions for working on this repository. OpenCode loads this
file automatically, and it is the **only** source of the operating rules.
The rules apply to every change, even a small one: if one point seems to
conflict with another, the more specific one wins.

## Contents

1. [Before every commit](#before-every-commit) — the checklist you must not skip
2. [Commit and PR rules](#commit-and-pr-rules)
3. [What gextto is](#what-gextto-is)
4. [gx-core engine](#gx-core-engine-internalgxcore)
5. [Build and test](#build-and-test)
6. [Release, installer and updates](#release-installer-and-updates)
7. [Code scanning](#code-scanning-codeql)
8. [Service running on this machine](#service-running-on-this-machine)
9. [Change conventions](#change-conventions)
10. [`docs/evoluzione.md`](#docsevoluzionemd)
11. [Interface accessibility and i18n](#interface-accessibility-and-i18n)
12. [Where things live](#where-things-live) — index of the documents

## Before every commit

1. **Changelog** in Italian and English (`changelog.md`, `changelog.en.md`):
   the same entry, explained for the people who use Gextto, with no internal
   details.
2. **Manuals and README** if anything visible or operational changes
   (`docs/MANUAL.it.md`, `docs/MANUAL.en.md`, `README.md`, `README.it.md`).
3. **Technical documents** touched by the change (see *Where things live* at the
   bottom).
4. **Tests green locally**: `make test`, `make vet` and, if you touched the UI,
   `make test-ui`; plus the TUI, performance and invariants described in *Build
   and test*.
5. **Commit** with author `buzzqw <azanzani@gmail.com>`, then push to
   `origin/main`.
6. **Report** commit, push, build and restart in your message, with the short
   hash and the branch.

## Commit and PR rules
- **Author**: `buzzqw <azanzani@gmail.com>`.
- **Changelog in both languages.** Every commit adds or updates the entry in
  `changelog.md` and `changelog.en.md` (same sections, same substance). The
  entry speaks to the people who use Gextto: what changes, for whom, what they
  have to do. No hashes, function names or file lists. **No film or TV series
  names either**, not even as an example in quotes: the changelog is public and
  the episode that showed the problem is told without naming its title. A commit
  that only touches build, tests or internal documentation has no entry of its
  own: its visible part goes into the entry of the functional commit it
  accompanies.
- **Small, verified commits, pushed right away** to `origin/main`.
- **Do not commit** `bin/` or the data directories (`data/`, `gextto-data/`).
  `build_number` and `gx-torrent.build_number` **are** committed instead: they
  are the single sources of the build numbers of Gextto and gx-core, shared
  between checkouts and CI (see *Build and test*).
- **Tests before the commit.** With `make test` (which includes `check-ui`,
  `installer-test`, `test-dht` and `go test ./...`) and `make vet`; if the
  change touches the UI, also `make test-ui` (Playwright in `uiweb/end2end`,
  outside `make test` because it needs Node and a browser, but it runs in CI).
  If you touch build or packaging, also try `scripts/build-release.sh` or the
  `package-*` scripts involved. Proceed only when **everything is green**; a
  red test is fixed first. For a documentation-only change (`.md`) the full
  suite is not needed.
- **CI status is checked on request.** `gh run list` is slow: do not query it
  on every cycle. That does not exempt you from running the same checks
  **locally** as described above.
- **Always report actions on the environment.** When you commit, push,
  rebuild (`make build`, `make gx-torrent`, …) or restart the service
  (`systemctl --user restart gextto`), write it in your message with the
  **short hash** of the commit and the branch/remote (`origin/main`). The user
  must know whether the work is committed, pushed, rebuilt and restarted.
- **Manuals, README and technical documents** are updated in the same commit
  when the change affects something visible or operational: the full rule is in
  [Change conventions](#change-conventions).

## What gextto is
- Go daemon for media acquisition and archiving. The main package is in the
  root; the `main` is in `cmd/gexttod`.
- Server-side UI in `uiweb/v2` (SSR + HTMX), i18n it/en/de/fr/es/pl. The UI
  end2end tests are Playwright (Node) in `uiweb/end2end`, not Go.
- **Torrent engines**: `gx-torrent` (default, pure Go) and `qBittorrent-nox`
  (external or managed). Contract in `torrent_engine.go`; adapters
  `gxtorrent_engine.go`/`gxtorrent_runtime.go` and `qbittorrent_engine.go`.
  Capability matrix in `capabilityLevels` (`torrent_engine.go`): it is the
  **single source of truth**. The API (`GET /api/torrent-backend`) and the
  detail-tab flags derive from it (`v2DetailCapsFor` in `uiweb_v2.go`, test
  `TestV2DetailCapsFollowCapabilityMatrix`). Operations an engine does not have
  are rejected with `ErrCapabilityUnavailable`. **Settings visibility** is a
  different axis (`uiSettingAllowedBackends`): which keys an engine reads, not
  which operations it supports.
- `gx-torrent` is **always started and supervised** by Gextto when it is the
  active engine (no option to turn it off). An external daemon already
  listening on the URL is used as it is.
- The managed daemon **survives restarts of Gextto** (systemd scope
  `gextto-gx-torrent-*.scope`): on restart Gextto reattaches to it if the
  binary and options are the same (`fingerprint` in `/api/v1/health`),
  otherwise it restarts it. It stops by itself after 15 minutes without Gextto.
  Details in `docs/gx-torrent.md`, *Startup*.

## gx-core engine (`internal/gxcore`)
- `internal/gxcore` is the **engine** of gx-core (the `gx-torrent` binary), our
  own original code, written in this repository (the initial permissively
  licensed bases were rewritten file by file), part of the main module as
  `github.com/buzzqw/gextto/internal/gxcore`. It is used by `cmd/gx-torrent`
  and by the tests.
- **Project preference: our own code.** What we need is written in this
  repository, not copied from others.
- Overview and feature inventory of the engine: `internal/gxcore/GEXTTO.md`.
- The DHT lives in `internal/dht`, a separate module below the main module.
- **Standalone features belong to gx-core only.** Pages and engines that do not
  belong to the managed daemon — RSS/feeds/rules/indexers, Wanted/Searches,
  feed Archive, Settings, wizard, indexer search, watch folders — exist **only**
  in `-mode=standalone`; in `-mode=managed` they are off and their routes
  answer 404, because there Gextto is in charge (queue, categories, RSS,
  search, limits) and two acquisition engines on the same session do not mix.
  They are not a gap to close: they must not be enabled in managed mode. Anyone
  who wants feeds/rules/searches runs a standalone gx-torrent (or the test
  instance of `scripts/gx-core-test.sh`). Details: `docs/gx-core.md`.
- Gap analysis against libtorrent and qBittorrent and improvement plan:
  **`docs/evoluzione.md`** (§6–§7); evolution towards a standalone client
  (gx-core): **`docs/gx-core.md`**.

## Build and test

Build commands, the verification to complete before a commit, and the invariants
not to break.

- `make build` — increments `build_number` (committed) and compiles
  `bin/gexttod` **and** `bin/gx-torrent`. The build is **pure Go**
  (`CGO_ENABLED=0`: `gx-torrent` is the default engine).
  `gx-torrent` is replaced only if its code changed
  (`bin/gx-torrent.code-sha256`, `GEXTTO_FORCE_GXTORRENT=1` to force it). The
  comparison compiles with `-buildvcs=false`: without it, Go stamps the git
  revision into the binary and every commit would look like a change to
  gx-torrent.
- `build_number` is the **single source** of the build number: checkout and CI
  read the same file (`build_number`, the current value). CI does not use
  `github.run_number`: the build and `release.json` take the value from the
  file, so the installed version has the **same** version as the checkout. The
  version shown is `<series>.<build>`, where the series is `1.2` and the third
  number is **always the build** (`1.2.2066`). The second number is raised
  **by hand** in `version_series`, only when a new series is deliberately
  started (`1.3.2100`); there is no automatic rollover. The version, the full
  build and the commit it was compiled from are shown at the bottom of the
  **Maintenance** page. To move the build forward, run `make build` and commit
  the updated file. After a `git pull` that changes `build_number`, rebuild to
  align the badge.
- gx-core (the daemon) has its **own build number**
  (`gx-torrent.build_number`, **committed**), separate from Gextto's: it grows
  by one on every real gx-torrent build done with `make build` or
  `make gx-torrent` (`scripts/next-gx-build-number.sh` with
  `GEXTTO_BUMP_BUILD=1`) and stays unchanged when gx-torrent is not rebuilt.
  CI and `build-release.sh` **read** the committed value without incrementing
  it, so the release has the same version as the checkout; `release.json`
  publishes it as `gx_version`/`gx_build`. After a build that recompiles
  gx-torrent, commit `gx-torrent.build_number` too. It is what
  `gx-torrent --version`, `/api/v1/health` and its web page report:
  `<series>.<build>`, with the series in `gx_torrent.version_series` (also
  raised by hand) and the build as the third number. The version formatting is
  `AppVersion`/`GxTorrentVersion` in `internal/constants/constants.go`,
  replicated by `scripts/version.sh` for `release.json`; both read the same
  series files.
- `make gx-torrent` — the daemon only (`CGO_ENABLED=0`), with the same build
  number of its own.
- Do not use `go build ./cmd/gx-torrent/` from the root: it writes a
  `gx-torrent` binary (or `gx-torrent.exe` on Windows) in the root (now
  ignored). For the daemon use `make gx-torrent` (it produces
  `bin/gx-torrent`).
- **Windows**: the engine (`internal/gxcore`) and the daemon (`cmd/gx-torrent`)
  are pure Go and **cross-compile** for Windows (amd64/arm64):
  `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/gx-torrent/`.
  - The platform layer is in `cmd/gx-torrent/*_linux.go`, `*_windows.go`,
    `*_other.go` (`fsinfo_*`, `mem_*`, `storageclass_*`, `gateway_*`,
    `defaultdir_*`, `link_*`, `storage_xdev_*`): memory, free space, network
    filesystems, HDD/SSD class and default gateway are read with the Win32
    APIs; the library link is a **junction** (no symlink privilege) and the
    cross-volume move recognises `ERROR_NOT_SAME_DEVICE`.
  - Since Go 1.23 `os.Lstat` reports a junction as `ModeIrregular`, not
    `ModeSymlink`: to recognise a link always use `isDirLink`. Only `fadvise`
    and `fallocate` have no native equivalent (`Truncate` fallback).
  - The daemon runs as a **native Windows service**
    (`svc.IsWindowsService` → `svc.Run`, `service_windows.go`/
    `service_other.go`), started by `startDaemon` (extracted and shared with the
    interactive path).
  - `scripts/package-gx-torrent-windows.sh` produces
    `gx-torrent-windows-<arch>.zip` (exe + `install-service.ps1` +
    `uninstall-service.ps1` + README), generated by `build-release.sh` and
    published in the release (`dist/gx-torrent-windows-*`).
  - CI: the `build-test` job cross-compiles **engine and daemon** for Windows;
    the `windows-platform` job runs the platform tests on `windows-latest`
    (`go test -run "Windows|MountPoint|Setup|CrossDevice|DaemonStart"
    ./cmd/gx-torrent/`); the `windows-service` job (non-blocking) actually
    installs the service with `scripts/gx-torrent-service-selftest.ps1`
    (install/start/health/stop). The junction buffer is built by
    `mountPointReparseData` (`junction.go`), tested on Linux too.
  - The whole of Gextto (`cmd/gexttod`) is **not** Windows: TUI, process/systemd
    management, fs metadata and installer/updater are Unix (see
    `docs/gx-torrent.md`, *Standalone use → Platforms*).
- `make test` — `check-ui` + `check-scripts` + `installer-test` + `test-dht` +
  the Go tests, pure Go, through `scripts/go-test-sharded.sh`: the root package
  (most tests, with shared package state, so no `t.Parallel`) is split across
  processes of one test binary, the other packages run with `go test` at the
  same time. `GEXTTO_TEST_SHARDS` sets the processes; `make test-serial` is the
  plain `CGO_ENABLED=0 go test ./...`.
- **Static UI guards** (they run with `go test ./...`):
  `TestUIActionPathsAreRegisteredRoutes` (every `Path: "/api/…"` of a
  button/action in the UI must be a registered route: a removed endpoint cannot
  leave a dead button answering 404), `TestInternalJSONPathsAreRegisteredRoutes`
  (the method and path of every `v2InternalJSON` call and of the
  `Endpoint`/`GetPath`/`PostPath` fields of the v2 pages must resolve on the
  real router: the in-process HTTP round trip does not fail at build time),
  `TestSettingsIndexIsWritable` (every option shown on the settings page can be
  saved) and `TestEnd2EndSelectorsReferenceRealSettings` (the
  `#input-<key>`/`#v2-setting-<key>` selectors and the `tab=` values of the
  Playwright tests must exist in the index/tab: a rename cannot leave the suite
  red only in CI).
- `make test-ui` — Playwright end2end suite of the UI (`uiweb/end2end`): it
  builds the daemon, starts it in dry-run and drives the real interface in a
  browser (pages, mobile, i18n, axe accessibility). **It is not part of
  `make test`** (it needs Node and a browser) but CI runs it: launch it when the
  change touches the UI (`uiweb/v2`, templates, JS/CSS, i18n, v2/`/settings`
  routes).
- **Control round at the end of every cycle** (engine or daemon): `make test`,
  `make test-engine`, `make vet` and — when the UI is touched — `make test-ui`,
  plus the invariants below. The rest of the mandatory verification (TUI,
  performance) is in the following points. CI status is not queried on every
  cycle: `gh run list` is slow and must be run **on request**.
- `make fuzz-smoke` — short fuzzing of the network-facing parsers (DHT, bencode
  `.torrent`, magnet, BEP 10 extension, BEP 52 file tree, trackers, Torznab,
  RSS, MSE, IP filter, aMule EC packets), `FUZZ_TIME` seconds per target (default 30s, e.g. `FUZZ_TIME=2m`
  for a serious pass). It is not part of `make test`; crashes found must be
  fixed and their inputs committed as seeds in `testdata/fuzz`. See
  `docs/evoluzione.md` §6.
- Targeted tests: `go test ./cmd/gx-torrent/` and `go test -run GxEngine .`.
- Engine tests (`internal/gxcore`): `make test-engine` (`go test` from the root
  on the engine packages). It is part of the module, so `go test ./...` already
  includes it; `make test-engine` remains to run only those packages.
- Invariants not to break: `scripts/check-ui-settings-index.sh` (UI settings
  index), `scripts/check-scripts.sh` (script lint: `bash -n`, plus `shellcheck`
  when installed) and `scripts/installer-selftest.sh`.
- **Test hygiene**: no test may leave files outside its own temporary folders.
  Use `t.TempDir()` (or `os.MkdirTemp` with `t.Cleanup`/`defer os.RemoveAll`),
  never paths relative to the CWD. A `Daemon` built by hand without `StatePath`
  must not write state (`saveLocked` returns immediately): without the guard it
  would create a `.tmp` in the working directory and leave it there because the
  rename to the empty path fails.
- **Gextto's TUI must always keep working.** After every change cycle run
  `go test ./internal/tui/` and the integration test
  `go test . -run TestTUILibraryClientAgainstTheRouter` (it drives the TUI
  client against the real API), plus the `gexttod tui` smoke test against the
  daemon. Always include the TUI verification in the final control round.
- **Performance (CPU and RAM).** After every cycle of engine or daemon changes,
  do a **thorough performance check**: CPU, RAM (RSS and allocations) and
  transfer latency/throughput. Every replacement or optimisation must be **at
  least equivalent** to the previous one, if not better: measure before and
  after (`go test -bench … -benchmem`, `pprof` CPU/heap profiles, footprint of
  the daemon and of `gx-torrent`, `make measure-seeding`) and record the numbers
  in `docs/measurements.md`.
- **Every time you run benchmarks, update `docs/measurements.md`.** This applies
  to any measurement (Go benchmarks, `make measure-*`, profiles, RSS): the
  document is where the numbers stay, with machine and date — if a measurement
  is not there, it was not done.

## Release, installer and updates

How a package is built, how it is installed, and how an installation updates
itself (or rolls back).

- The release package is built **on Ubuntu 22.04** (glibc 2.35) with
  `scripts/build-release.sh`: locally it runs in a docker/podman container, in
  CI (`continuous.yml`, `release.yml`) the `package` jobs use
  `container: ubuntu:22.04` for x86_64 and aarch64 (`ubuntu-24.04-arm`). A
  binary built on a newer system does not start on Debian 12/Ubuntu 22.04.
- Each architecture ships as a single pure-Go package:
  `gextto-linux-<arch>.tar.gz`; `gexttod --update` updates the installation to
  the same package.
- `scripts/package-gx-torrent.sh` produces the **standalone** archive
  `gx-torrent-linux-<arch>.tar.gz` (binary + systemd unit + README), which
  `build-release.sh` generates and CI publishes next to the Gextto ones: it is
  the client usable on its own, like qbittorrent-nox, without Gextto.
  `scripts/package-gx-torrent-windows.sh` does the same for Windows
  (`gx-torrent-windows-<arch>.zip`, amd64/arm64, native service).
- `scripts/build-daemon.sh` stamps `constants.Commit` and `constants.BuiltAt`
  into the binary (into `gx-torrent` too); `scripts/release-manifest.sh` writes
  `release.json` (version, commit, latest commits, with `"gx": true` on the
  commits that touch gx-torrent's code, that is the `GX_PATHS` kept aligned by
  `TestReleaseManifestCoversDaemonPackages`) published next to the packages: it
  is what the in-app update check of Gextto (`update_check.go`) and of
  standalone gx-torrent (`cmd/gx-torrent/update.go`) reads. Only
  `build-release.sh` stamps `constants.GxReleaseChannel`: a local gx-torrent
  build does not update itself.
- `install.sh` also installs `gextto-update.path`/`.service`: the UI writes
  `<data>/update-request`, the root unit runs `gexttod --update` (log in
  `/var/log/gextto-update.log`), which keeps `gexttod.prev` (and
  `gx-torrent.prev`, like `install.sh` when re-run) and restores it if the new
  version does not start.
- Service user: by default it is **the local user running the installer**
  (`SUDO_USER`), so it can create folders in that user's media; `--user
  gextto` (or `GEXTTO_USER`) uses the isolated system account with no login,
  with `--media-group` for access to other people's media. The group of the
  unit is the user's primary group (`id -gn`), not necessarily the user name.
- Real installer test: a container with systemd (`--privileged
  --cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup:rw`) and
  `install.sh --local-archive dist/gextto-linux-x86_64.tar.gz`.
- The first-start wizard is `uiweb_v2_setup.go` (`/?view=setup`).

## Code scanning (CodeQL)
- Workflow `.github/workflows/codeql.yml` (Go language; it runs on push/PR to
  `main` and once a week). No `sudo` or CGO is needed for the analysis.
- **Local model pack**: `.github/codeql/extensions/gextto-models` declares the
  path and redirect validators CodeQL does not recognise on its own
  (`pathStartsWith`, `folderRenameWithin`, `comicDownloadFilename`,
  `v2SearchRedirectTarget`). The `input` of a `barrierModel` is the node that
  stops the flow (`ReturnValue`) and the `kind` is the query's kind
  (`path-injection`, `url-redirection`, …), not a free label. The action does
  not load local packs by itself: the `CODEQL_ACTION_EXTRA_OPTIONS` already set
  in the *Analyze* step are needed. If you add a new validator, add it to the
  model pack (`models/gextto.model.yml`) instead of suppressing alerts
  wholesale.
- **Triage**: fix the real alerts (data flow from untrusted input to a sink);
  for by-design cases or false positives, dismiss with an explicit reason:
  `gh api -X PATCH repos/buzzqw/gextto/code-scanning/alerts/<n> -f state=dismissed
  -f dismissed_reason='false positive|won't fix|used in tests' -f dismissed_comment='…'`.
  Conventions: `go/path-injection` and `go/request-forgery` on paths/URLs
  configured by the operator (absolute save paths, fetching `.torrent`/IP
  filters) are intentional features, on an API protected by a token; protocol
  cryptography (RC4 in MSE, SHA-1 in the DHT) cannot be changed.
- Open alerts: `gh api 'repos/buzzqw/gextto/code-scanning/alerts?state=open&per_page=100'`.

## Service running on this machine
- It is a **systemd user service** (not a system one):
  `~/.config/systemd/user/gextto.service`, running
  `/home/andres/gextto/bin/gexttod` from the checkout,
  `GEXTTO_DATA_DIR=/home/andres/gextto-data`, UI on `0.0.0.0:5000`.
  `gx-torrent` is started by `gexttod`, in a separate systemd scope that stays
  alive across restarts of the service.
- **When needed, build and restart**: after engine/daemon changes (or to
  verify) run `make build` and `systemctl --user restart gextto`; do not leave
  the service on an old binary. The restart can take ~1 minute (IP filter
  loading and handover of the `gx-torrent` scope), so verify after a wait, not
  immediately.
- After a build: `systemctl --user restart gextto` (no `sudo`). If
  `bin/gx-torrent` did not change, the log says "gx-torrent … was already
  running: kept as it is"; otherwise the daemon is restarted.
- **No document is in the build.** Manuals and changelogs are kept online and
  the UI opens the GitHub links in the interface language
  (`docs/MANUAL.it.md`/`docs/MANUAL.en.md`, `changelog.md`/`changelog.en.md`):
  a change to a document — `README`, `docs/*.md`, `AGENTS.md`,
  `changelog*.md` — is committed and pushed **without `make build` or a
  restart**. The `/?view=manual` page redirects to the online manual; do not
  reintroduce `go:embed` for the manuals, or every change would require a
  rebuild again.
- Logs: `gextto-data/gextto.log` and `gextto-data/gx-torrent/gx-torrent.log`
  (both rotated at 5 MB × 4); `gx-torrent.crash.log` only for crashes.
- Check: `systemctl --user status gextto`;
  `curl -s http://127.0.0.1:5000/api/status` (field `version` = Gextto's
  version, `<series>.<build>`: `1.2.2066`, with the series raised by hand);
  `curl -s http://127.0.0.1:8890/api/v1/health` (gx-torrent daemon:
  `version` = its own `<series>.<build>`, `pid`, `fingerprint`).

## Change conventions

The rules below say *where* to put a change. When in doubt, look at how a
similar feature is already solved.

### Engine capabilities and options
- A new option: daemon (`cmd/gx-torrent`), adapter (`gxtorrent_engine.go`),
  `capabilityLevels` matrix (`torrent_engine.go`), then documentation. Options
  tied to the creation of the picker apply only to torrents added afterwards:
  declare them `"partial"`, never a false success.
- `capabilityLevels` is the **only source of truth**: the detail-tab flags are
  **derived** from the matrix (`v2DetailCapsFor` in `uiweb_v2.go`), not with
  hand-written maps. `TestCapabilitiesBooleanMatchesLevels` fixes that the
  boolean of `Capabilities()` is true for `full`/`partial` and false for
  `none`; `TestV2DetailCapsFollowCapabilityMatrix` locks the expected UI flags.

### New UI setting
Add, in this order: default (`uiweb_settings_defaults.go`), index
(`uiweb_settings.go`, with section and panel `Group`: the script rejects
entries without a group), unit/special values/dependency
(`uiweb_settings_meta.go`, not in the label), tooltip (`uiweb_tooltips.go`),
label and description translations in **all** the `internal_translations*.yml`
catalogs, and the row in Appendix A of the manuals.
- `TestSettingsEveryPreviousOptionIsStillAvailable` counts the options: if you
  add one, update the counts.
- An index key is **writable by construction** (`gh7_setting_key_allowed`
  accepts `uiSettingIndexed`): it does not go into any allowlist.
  `settingWritableAPIOnly` is only for keys without a row on the page
  (dedicated editors or API-only).
- Every write — API, settings page, wizard — goes through
  `validateSettingValue`: a new validation rule goes there, not into the
  individual handler.

### A visible feature
It must be exposed **both** in Gextto's UI (`uiweb/v2`) **and**, where it makes
sense, in the daemon's web page (`cmd/gx-torrent/ui.go`): never reachable only
through the API.

### Log lines are for the user
The log is read by the person running Gextto, not by a developer. Every INFO,
WARN and ERROR line is a sentence in natural language saying what happened and,
when relevant, why. **The log is always in English**: titles, release names,
episodes and paths keep their original language, the sentence around them does
not. Error strings reachable from a log line are English too.
- A restart is one divider line (`━…`) followed by one sentence; the first
  start has neither.
- No `key: value` trails (`releases_examined: 15064`), no internal state names
  (`downloading_metadata`). Field labels are written in words by the logger
  (`downloaded bytes`, not `downloaded_bytes`); use the structured arguments
  only for values a person would also say, and send technical detail to DEBUG.
- Call titles, episodes, files and sources by the name the user sees in the UI.
- Do not repeat in a field a value already written in the sentence.
- Aggregate periodic or identical events (`9 failed downloads removed`) instead
  of one line each; a periodic message says only what changed.
- Keep the `acq` id when it helps follow one download; keep hashes out of the
  message.
- `log_readability_test.go` enforces the mechanical part: a new call with a
  `snake_case` message, a ` · key: ` field inside the message or an Italian
  sentence fails the tests.

### Documentation and manuals
- **Manuals** (`docs/MANUAL.it.md` and `docs/MANUAL.en.md`): they are updated
  in the same commit when anything visible or operational changes — templates,
  JS/CSS, routes, labels, settings, but also acquisition, queue, archive,
  seeding, search, daemon, CLI/TUI. If the change concerns gx-torrent, check
  `docs/gx-torrent.md` and its web page too. The manuals are **online**:
  updating them requires neither `make build` nor a restart.
- **README** (`README.md` and `README.it.md`, same substance): it is
  documentation in every respect and must be updated on every change that
  affects users, especially when it highlights Gextto or gx-core.
- **Technical documents**: every evolution or improvement also updates the
  reference documents, not just the code. See *Where things live* at the bottom.
  An item described as «missing»/«to do» is closed or requalified when the code
  implements it; status, capability and count tables are realigned. This also
  applies to invisible work (engine, queue, DHT, storage).
- **API**: every new route, removed route, method or parameter change is
  aligned in `docs/API.md` in the same piece of work — for Gextto (`/api/*`,
  `web_router.go`) and for the daemon (`/api/v1/*` native and `/api/v2/*`
  qBittorrent-compatible). Guards: `TestAPIDocumentationMatchesRouter` (Gextto)
  and `TestAPIRoutesAreDocumented` (`cmd/gx-torrent`). Procedure in
  `docs/DEVELOPERS.md`, *Web UI and API changes*.
- When you add or move documents, update the `docs/README.md` index.

## `docs/evoluzione.md`

Only two contents: the **current state** (what exists and why) and the **future
improvements** with their gate. It is not a changelog or a manual: no bugs,
fixes, implementation chronicles, dates, build numbers, commit hashes, usage
instructions or measurements. Write in the present tense and describe _what
exists_, not what was done: a sentence that would be false in six months
because it tells the story of a transition must be cut. When a feature lands,
the state is updated and the open point removed; decisions stay with their
rationale, the numbers in `measurements.md`.

## Interface accessibility and i18n
- The pages of the gx-torrent daemon (`internal/webui/assets`) use the **shared
  design system**: `theme.css` (colour tokens for light and dark themes,
  components), `common.js` (theme picker, notifications) and the `icons.svg`
  sprite, served from `/assets/`. No hard-coded colours in the pages: use the
  variables (`--text`, `--surface`, `--accent`, …) and the existing components
  (`.topbar`, `ul.items`, `dialog`, `.chip`, `.badge`, `.fsec`).
- **Accessibility procedure, tests and criteria: `docs/ACCESSIBILITY.md`.**
  Every UI change must check and respect accessibility. It applies to
  `uiweb/v2` (SSR + HTMX), to the daemon page (`cmd/gx-torrent/ui.go`) and to
  every generated HTML page.
- Minimum requirements: semantic HTML and correct ARIA attributes; every field
  with an associated `label`; sufficient contrast and visible focus; full
  keyboard navigation (logical tab order, consistent `Esc`/`Enter`, no control
  usable only with the mouse); alternative text for icons and images;
  status/error messages that can be announced (`role="alert"`/`aria-live`).
- Always verify before closing the work: automated tests or a manual check of
  keyboard, focus, label reading and contrast. A new or changed control without
  this verification is not considered finished.
- It also applies to translated strings (i18n): labels and descriptions must
  stay understandable and have adequate `label`/`aria-label` values in every
  language.
- **Interface fields must always be translated into every available language**
  (`it`, `en`, `de`, `fr`, `es`, `pl`). Labels, descriptions, tooltips,
  placeholders, messages and field names cannot stay in one language only:
  every new string goes into the catalogs (`internal_translations*.yml` for
  Gextto, `uiCatalog` for the daemon) with a translation in **every** language.
  Guards: `TestBundledCatalogsAreWellFormed`/`TestBundledCatalogsShareKeys` and
  `TestUIIsTranslatable` (Gextto), `TestUIPageIsFullyTranslatable` and
  `TestUICatalogTranslatedInEveryLanguage` (daemon).
- **Every option must have its own tooltip, translated into every language.**
  It applies to both UIs (Gextto `uiweb/v2` and the daemon page). The tooltip
  must be defined explicitly: in Gextto `uiweb_tooltips.go` for every key of
  the `uiweb_settings.go` index; in the daemon a `title` (or `aria-label`) on
  the control, or on the `<label>` that wraps it, with the entry in the i18n
  catalogs. Automatic guards: `TestSettingTooltipsAreTranslated` (Gextto),
  `TestEveryOptionHasATooltip` and `TestUIPageIsFullyTranslatable` (daemon). A
  missing or untranslated tooltip makes the tests fail.

## Where things live

Index of the documents: `docs/README.md`.

| What | Document |
| --- | --- |
| Interface usage, visible behaviour | `docs/MANUAL.it.md`, `docs/MANUAL.en.md` |
| Project presentation, quick build | `README.md`, `README.it.md` |
| Architecture and file map | `docs/ARCHITECTURE.md` |
| Current state and future engine improvements | `docs/evoluzione.md` |
| CPU/RAM/latency measurements (numbers only, with machine and date) | `docs/measurements.md` |
| gx-torrent daemon and gx-core engine | `docs/gx-torrent.md`, `docs/gx-core.md`, `internal/gxcore/GEXTTO.md` |
| Engine BEP compliance | `docs/bep.md` |
| HTTP routes of Gextto and the daemon | `docs/API.md` |
| Accessibility (procedure and tests) | `docs/ACCESSIBILITY.md` |
| Development: UI, API, tests | `docs/DEVELOPERS.md` |
| TUI | `docs/tui.md` |
| Public changelog | `changelog.md`, `changelog.en.md` |

Online documents: the UI opens them from GitHub in the interface language. They
are not in the build, so updating them requires neither `make build` nor a
restart.
