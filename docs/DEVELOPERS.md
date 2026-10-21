# Developing Gextto

This guide is for contributors who build, test or extend Gextto. For installing
and operating a daemon, use the repository README and user manual instead.

## Contributor workflow

1. Make a focused change and preserve existing user data and configuration.
2. Format changed Go files, run the focused test, then run the full suite.
3. Update the user-facing documentation and API reference with the behavior.
4. Inspect `git diff --check` before committing.

## Project layout

```text
cmd/gexttod/       executable entry point
*.go               daemon, database, web handlers and services
uiweb/v2/templates/ server-rendered HTML templates
uiweb/v2/static/   CSS and browser JavaScript
uiweb/end2end/     Playwright tests
internal/           shared packages and backend helpers
scripts/            build, update, packaging and service helpers
docs/               user, advanced-user and developer documentation
```

The root Go package contains the application logic. `cmd/gexttod/main.go` only
assembles and starts the executable. For a file-by-file map of that package,
the torrent backends and where to make common changes, see
[ARCHITECTURE.md](ARCHITECTURE.md).

## Requirements and build

Install Go 1.26.8 or newer (the version in `go.mod`). The build is **pure Go**
and has no C/C++ dependency: `gx-torrent`, the default engine, is a pure-Go
daemon built next to `gexttod`. The normal build embeds the web UI; it does not
need a separate frontend build.

```bash
make build                 # pure Go (default): gexttod + gx-torrent
go test ./...              # pure-Go tests
```

The executable is written to `bin/gexttod`, with the `gx-torrent` daemon next to
it. Supported torrent backends are **gx-torrent** (default, pure Go) and the
**qBittorrent-nox** Web API adapter. See
[ARCHITECTURE.md](ARCHITECTURE.md) for the torrent engine layer.

## Local development

Run without real downloads in an isolated data directory:

```bash
GEXTTO_DATA_DIR="$PWD/data-dev" \
GEXTTO_ACTIVE=0 \
GEXTTO_DRY_RUN=1 \
./bin/gexttod --dry-run
```

For a user service:

```bash
GEXTTO_DATA_DIR="$HOME/gextto-data" \
  GEXTTO_LISTEN=127.0.0.1:5000 \
  scripts/install-user-service.sh
```

After a source update, `scripts/update.sh` builds the daemon and restarts the
detected service.

### Testing a local gx-core build

`scripts/gx-core-test.sh` runs a **test gx-core** (gx-torrent in standalone
mode) built from the checkout, next to the Gextto in production, so a change
to the engine or the daemon can be tried as soon as it compiles, without
waiting for a CI release:

```bash
scripts/gx-core-test.sh start           # start it (rebuilds if the code changed)
scripts/gx-core-test.sh restart         # stop + start (rebuilds if changed)
scripts/gx-core-test.sh status          # pid, version, data directory
scripts/gx-core-test.sh log             # follow its log
scripts/gx-core-test.sh stop
scripts/gx-core-test.sh start --build   # force a rebuild even if nothing changed
scripts/gx-core-test.sh start -- -debug # extra daemon options after "--"
```

It keeps everything apart from Gextto: its own directory (`~/gx-core-test`:
binary, state, session database, downloads, log), its own web page/API address
(`0.0.0.0:8899`, open on the whole LAN like the packaged standalone daemon) and
peer ports (`51500-51510`), no UPnP/NAT-PMP mapping. `start` and `status` print
the address another device on the network opens (the machine's LAN address); its
page stays open until a password is set in its own `settings.json` (standalone's
LAN bypass). The build goes to `~/gx-core-test/bin`, not to `bin/`, and does not
bump `gx-torrent.build_number`: Gextto's managed gx-torrent is never replaced.
The binary carries the commit (`-dirty` when the engine or daemon sources have
uncommitted changes) and no release channel, so it never updates itself.
`GX_TEST_DIR`, `GX_TEST_LISTEN` (e.g. `127.0.0.1:8899` to keep it on this
machine only), `GX_TEST_PEER_PORTS`, `GX_TEST_TOKEN` and `GX_TEST_BIN` change the
defaults.
`start` and `restart` rebuild the test binary **by themselves** when the
gx-torrent sources changed since the last build: the script hashes them exactly
as `scripts/build-daemon.sh` does for `bin/gx-torrent`, so a copy left behind by
a merge is refreshed instead of showing the old page (`--build` forces a rebuild,
`GX_TEST_BIN` is never touched). When the instance was set up with a page
password, the script still recognises it: the API answers `401` until a session
exists, and the script reports that instead of calling the daemon dead.
The script can be called through a symlink (for example
`ln -s ~/gextto/scripts/gx-core-test.sh ~/gx-core-test.sh`): it resolves the
link and builds the checkout the link points to.

## Web UI and API changes

The server-rendered UI is assembled in `uiweb_v2.go`, `uiweb_sections.go`,
`uiweb_pages.go` and `uiweb/v2/templates`. Static browser behavior belongs in
`uiweb/v2/static/v2-core.js`; styles belong in `uiweb/v2/static/gextto-ui.css`
and `v2.css`.

The official UI is served at `/`; the old `/v2` prefix redirects to the root and
the removed legacy `/ui` route must not be reintroduced. Live shell metrics are
rendered by the `/partial/chrome` HTMX fragment, while the Downloads table uses its own
5-second polling fragment. Keep both polling fragments free of a recursive
`load` trigger when they replace themselves with `outerHTML`.

When adding an API endpoint:

1. implement the handler with the shared `AppState` signature;
2. register it in `web_router.go`;
3. add authorization, validation and bounded input handling;
4. document it in `docs/API.md`;
5. add a focused Go test and, when it changes the UI, a Playwright test;
6. keep the route table in `docs/API.md` aligned
   (`TestAPIDocumentationMatchesRouter` for Gextto, `TestAPIRoutesAreDocumented`
   for the gx-torrent `/api/v1` and `/api/v2` tables).

Use the existing JSON helpers and keep destructive operations explicit and
confirmable in the UI.

The v2 pages read and write through the JSON API in-process
(`v2InternalJSON`, using the router stored on `AppState`). A renamed route does
not break the build, so `TestInternalJSONPathsAreRegisteredRoutes` resolves
every literal method and path of those calls, and the `Endpoint`/`GetPath`/
`PostPath`/`TestEndpoint`/`AddPath` fields of the page specs, against the real
router.

Settings: a key in the settings index (`uiweb_settings.go`) is writable by
construction; `settingWritableAPIOnly` (`web_handlers_g7.go`) lists only the
keys with no row on the page. Every write path (the JSON API, the settings page,
the setup wizard) validates the value through `validateSettingValue`
(`backend_config_validation.go`), so a rule added there applies everywhere.
The starting value the page shows (`uiSettingDefaults`,
`uiweb_settings_defaults.go`) must match what the backend uses when nothing is
saved: `TestSettingDefaultsMatchTheBackend` pins it against the typed
`DefaultTransferSettings` (derived in `Config.applyTransferSettings`) and against
the scalar getters, and `TestDefaultTransferSettingsMatchLoader` pins those
fallbacks against the struct. A new option with a backend default needs an entry
there. The `/api/config` view (`ConfigView`, `web_handlers_core.go`) repeats a
page default for the flat options it reports; `TestConfigViewDefaultsMatchThePage`
compares those to the page (the list keys and the nested `transfer`/`paths` blocks
are a different representation and are read through the config instead), so a
backend default must reach the view as well as the page.

Every route sits behind `AuthMiddleware` (`auth.go`). Access control is off by
default; when it is on, only `/login`, `/logout`, `/static/`, the favicon, the
manifest and the service worker are public, and local clients pass without a
login unless the operator disabled that. A new public route needs an explicit
entry in `authPublicPath`.

### Interface texts and translations

Templates and Go code write interface texts in Italian; the rendered HTML is
translated with the bundled catalogs `internal_translations.yml` (English),
`_de`, `_fr`, `_es` and `_pl`, whose keys are the Italian strings. Every new
text needs an entry in all five catalogs (a test checks that they share the
same keys). Texts built outside the templates use `uiText(s, "…")`. The TUI has
its own six-language catalog (IT, EN, DE, FR, ES, PL) in `internal/tui/i18n.go`; daemon log lines are
English only.

## Database changes

Database schema changes must be migration-safe for existing installations.
Use the database migration helpers, parameterised SQL and tests covering both a
fresh database and an older schema. Never put user data, local paths or secrets
in fixtures committed to the repository.

## Testing

Run these checks before a commit:

```bash
gofmt -w changed.go
make test                 # pure Go: check-ui + installer self-test + test-dht + CGO_ENABLED=0 go test ./...
make test-engine            # tests of the gx-core engine
make test-ui              # UI end2end (Playwright): pages, mobile, a11y (needs Node + a browser)
CGO_ENABLED=1 go test -race ./...   # race detector needs cgo (a C compiler)
node --check uiweb/v2/static/v2-core.js
git diff --check
```

`make test` is pure Go.

The end-to-end suite is under `uiweb/end2end` (Playwright + Node). `make test-ui`
builds the daemon, starts it in dry-run on its own port, completes the first-run
wizard once in `global-setup.ts` and drives the real interface in a browser:

```bash
make test-ui              # pages, mobile layout, i18n, axe-core accessibility
GEXTTO_TEST_UI_SERIAL=1 make test-ui   # old single process
```

Locally `scripts/test-ui.sh` splits the specs into two independent groups — one
with the settings, i18n and the smaller read-only specs, one with the
accessibility and page specs — each with its own dry-run daemon, data directory
and ports, and runs them concurrently. CI keeps the serial process. The script
kills the test `gx-torrent` at the end; an interrupted run can leave one
listening until its 15-minute orphan timeout.

The suite also gives the test daemon its own gx-torrent: `playwright.config.ts`
sets `GEXTTO_GXTORRENT_LISTEN=127.0.0.1:18893`, which replaces both the managed
daemon's listen address and the URL Gextto uses to reach it. Without it, a
gx-torrent already answering on the default 8890 (a live installation) would be
adopted by the test instance, which then reloaded its IP filter and restarted
its session. Gextto now refuses a daemon that carries a fingerprint but belongs
to another data directory, so that mistake fails loudly instead of disturbing a
running installation.

It runs in CI in the `ui-e2e` job. `tests/accessibility.spec.ts` holds the
axe-core checks and the keyboard/reflow regressions: it should pass before
changing UI templates, styles or client-side interaction code.

### Fuzzing

The network-facing parsers have Go fuzz targets. Run them before touching the
`internal/gxcore` codecs, the DHT packets, the `.torrent`/magnet/RSS/Torznab
parsing or the MSE/peer-wire handshake:

```bash
make fuzz-smoke                # 30s per target (FUZZ_TIME default)
FUZZ_TIME=2m make fuzz-smoke   # a serious pass
```

A crashing input is written under `testdata/fuzz/<Target>/`: fix the code and
commit the input as a regression seed. `.github/workflows/fuzz.yml` runs the same
check nightly (2m per target), on a push that touches an engine/parser path (30s)
and on demand from the Actions tab, uploading the failing inputs as an artifact.
Each target is retried once, so a spurious `context deadline exceeded` from the
Go fuzzer shutting down under load does not fail the run (a real crash repeats).

## Temporary space (/tmp)

`/tmp` here is a **tmpfs** (RAM), so everything left there costs memory and, once
it fills up, breaks the tests: gextto's disk guard pauses downloads when the
target filesystem has less than 1 GiB free, and the tests that download then wait
for a torrent that never starts (a suite that takes 40 s turns into minutes of
timeouts). Keep it clean:

```bash
scripts/tmp-smart-clean.sh --dry-run   # what would go
scripts/tmp-smart-clean.sh --verbose   # what goes
scripts/install-tmp-cleaner.sh         # daily systemd *user* timer (no sudo)
```

The cleaner is deliberately conservative: it only looks at the current user's
entries, only older than 24 h (build/test residue, `go-build*`, `Test*`,
`playwright-*`, ages faster, 6 h, with a 1 h floor), never anything a process
still holds open (a directory holding an open file, or a socket, is left alone),
and never the system/desktop plumbing (`systemd-private-*`, `snap-private-tmp`,
`.X11-unix`, `tmux-*`, `ssh-*`, …). The timer runs it daily with a 30 minute
jitter; the log is in the journal (`journalctl --user -u tmp-clean`).

Claude Code's scratch area (`/tmp/claude-<uid>`) is cleaned too, one session at
a time rather than as a whole: `<project>/<session>` goes once the session is
idle — its transcript `~/.claude/projects/<project>/<session>.jsonl` and its
own content untouched for 12 h (`--claude-idle HOURS`) — and nothing in it is
open or is a process's working directory, so a running session keeps its
scratchpad. The loose files beside the sessions age like build residue (6 h).

## Build cache

The Go build cache (`GOCACHE`, normally `~/.cache/go-build`) grows over time: a
few gigabytes are normal on a busy machine. `scripts/refresh-build-cache.sh`
keeps it in check:
when the cache passes a threshold (default 5 GiB; override with
`GEXTTO_BUILD_CACHE_LIMIT_GB` or `--limit-gb N`) it runs `go clean -cache`, then
`go build ./...` and `make test`, so the cache is left **clean and warm** rather
than cold. A fresh rebuild is usually far smaller than the accumulated one (for
example 4.3 GiB → 0.5 GiB in a real run). It deliberately does **not** run
`make build`, which would bump the committed `build_number` and dirty the
checkout.

```bash
scripts/refresh-build-cache.sh --dry-run   # size and decision only
scripts/refresh-build-cache.sh --status    # size, limit and the user timer
scripts/refresh-build-cache.sh --force     # clean + rebuild + test regardless
scripts/refresh-build-cache.sh             # clean + rebuild + test only if over the limit
scripts/refresh-build-cache.sh --install   # weekly systemd user timer
make cache-refresh                         # same check, via Make
```

`--install` copies `systemd/gextto-build-cache.{service,timer}` into
`~/.config/systemd/user`, points `ExecStart` at the script in the checkout and
enables the timer (`systemctl --user list-timers gextto-build-cache.timer`);
`--uninstall` removes it. The timer runs weekly and does nothing when the cache
is still under the limit.

### GitHub Actions cache

Distinct from the local Go cache, GitHub keeps a **10 GB budget of Actions
caches** per repository: the `setup-go` and npm caches, plus the CodeQL overlay
base databases, which are one per commit and ~75 MB each — on this repository
they alone fill most of the budget. GitHub has no "keep the last N" policy: past
the budget the least recently used caches are evicted, which can throw away the
useful build caches before the overlay databases that caused the pressure.
`.github/workflows/cache-prune.yml` runs daily (and on demand from *Actions →
Cache prune*) and keeps only the newest caches of every kind (default 20).

```bash
scripts/prune-actions-caches.sh --dry-run       # list what would be deleted
scripts/prune-actions-caches.sh --keep 20       # keep the newest 20 per kind
scripts/prune-actions-caches.sh --overlay-only  # CodeQL base databases only
```


## Concurrency and safety

The daemon mixes HTTP handlers and long-lived workers, so a few invariants are
load-bearing:

- **Goroutines started by HTTP handlers** (manual cycle, rename-all) must be
  tracked in a `WaitGroup` or derive from `BackgroundContext()` so they finish
  before their state is torn down.
- **Run `go test -race ./...`.** It catches the races above; do not merge a
  change that introduces a new race.
- New endpoints must validate and bound every user input and restrict filesystem
  paths to the configured roots before use.

## Packaging and releases

Build the daemon and standalone Linux payload with:

```bash
make build
scripts/package-linux.sh
```

The payload is **pure Go**: `gexttod`, the `gx-torrent` daemon, `run.sh`,
`VERSION` and a quick-start README. `scripts/build-release.sh` builds the
release in an Ubuntu 22.04 container (glibc 2.35 is the baseline the released
binaries must run against). It is the payload consumed by the official installer
and by the advanced self-update command.

The same release build also produces the **standalone gx-torrent** packages,
published next to Gextto's:

| Script | Output | Contents |
|---|---|---|
| `scripts/package-gx-torrent.sh` | `gx-torrent-linux-<arch>.tar.gz` | binary, `run.sh`, systemd unit, README, `VERSION` |
| `scripts/package-gx-torrent-windows.sh` | `gx-torrent-windows-<arch>.zip` (amd64, arm64) | `gx-torrent.exe` (native Windows service), `install-service.ps1`, `uninstall-service.ps1`, `README.txt` |

### aMule (EC) against a real core

The default suite uses the fake core in `internal/amule/amuletest`. To check
the EC client against a real `amuled` (for example a throw-away instance with
its own `--config-dir` and EC port):

```bash
GEXTTO_AMULE_EC=127.0.0.1:4712 GEXTTO_AMULE_PASSWORD=secret \
  go test ./internal/amule/ -run Live -v
```

Windows support is guarded in CI (`ci.yml`): `build-test` cross-compiles the
engine and the daemon and runs `go vet` for Windows; `windows-platform` runs the
platform tests on `windows-latest`; `windows-service` (non-blocking) installs
the real service with `scripts/gx-torrent-service-selftest.ps1`, waits for
`/api/v1/health` and checks a clean stop. Platform-specific daemon code lives
in `cmd/gx-torrent/*_linux.go`, `*_windows.go` and `*_other.go`; on Windows a
library link is a junction, so always test for a link with `isDirLink`, never
with `os.ModeSymlink`.

Keep generated binaries, databases, logs and local configuration out of Git.
Release checks and CI configuration are maintained outside the user manuals.
