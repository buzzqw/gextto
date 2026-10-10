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

Install Go 1.26 or newer. The default build is **pure Go** and needs no
libtorrent: `gx-torrent`, the default engine, is a pure-Go daemon built next to
`gexttod`. To include the embedded **libtorrent** engine, also install a C++17
toolchain and the `libtorrent-rasterbar` development headers, then build with
`GEXTTO_LIBTORRENT=1`. The normal build embeds the web UI; it does not need a
separate frontend build.

```bash
make build                 # pure Go (default): gx-torrent
make build-libtorrent      # also link the embedded libtorrent engine
go test ./...              # pure-Go tests (libtorrent tests are behind //go:build cgo)
```

The executable is written to `bin/gexttod`, with the pure-Go `gx-torrent` daemon
next to it. Supported torrent backends are **gx-torrent** (default, pure Go),
**embedded libtorrent** (present only in a build made with `GEXTTO_LIBTORRENT=1`;
`LibtorrentCompiled()` reports it, and without it the backend is not selectable)
and the **qBittorrent-nox** Web API adapter. See
[ARCHITECTURE.md](ARCHITECTURE.md) for the cgo boundary (`libtorrent_cgo.go` vs
the `libtorrent_nocgo.go` stub).

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

## Web UI and API changes

The server-rendered UI is assembled in `uiweb_v2.go`, `uiweb_sections.go`,
`uiweb_pages.go` and `uiweb/v2/templates`. Static browser behavior belongs in
`uiweb/v2/static/v2-core.js`; styles belong in `uiweb/v2/static/gextto-ui.css`
and `v2.css`.

The official UI is served at `/`; the old `/v2` prefix redirects to the root and
the removed legacy `/ui` route must not be reintroduced. Live shell metrics are
rendered by the `/partial/chrome` HTMX fragment, while the Scarico table uses its own
5-second polling fragment. Keep both polling fragments free of a recursive
`load` trigger when they replace themselves with `outerHTML`.

When adding an API endpoint:

1. implement the handler with the shared `AppState` signature;
2. register it in `web_router.go`;
3. add authorization, validation and bounded input handling;
4. document it in `docs/API.md`;
5. add a focused Go test and, when it changes the UI, a Playwright test;
6. keep the route table in `docs/API.md` aligned.

Use the existing JSON helpers and keep destructive operations explicit and
confirmable in the UI.

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
its own Italian/English catalog in `internal/tui/i18n.go`; daemon log lines are
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
make test                 # pure Go: check-ui + installer self-test + CGO_ENABLED=0 go test ./...
make test-engine            # tests of the gx-core engine
CGO_ENABLED=1 go test -race ./...   # race detector needs cgo (and the libtorrent headers)
node --check uiweb/v2/static/v2-core.js
git diff --check
```

`make test` is pure Go and skips the libtorrent-only tests (guarded by
`//go:build cgo`). Run `make test-libtorrent` and `make build-libtorrent`
(requires the libtorrent headers) when you touch the embedded engine, the cgo
bridge or the capability gating.

The end-to-end suite is under `uiweb/end2end` and requires a running daemon:

```bash
cd uiweb/end2end
npx playwright test
npm run test:a11y
```

`test:a11y` runs the axe-core checks and keyboard/reflow accessibility
regressions. It should pass before changing UI templates, styles or client-side
interaction code.

## Concurrency and safety

The daemon mixes HTTP handlers, long-lived workers and (in a build made with
`GEXTTO_LIBTORRENT=1`) a CGo libtorrent session, so a few invariants are
load-bearing:

- **The native session handle is shared state.** Reads of `LibtorrentClient.session`
  must take `sessionMu.RLock()`; `Shutdown` destroys the handle under
  `sessionMu.Lock()`. Never read the field directly in a new method — a cgo
  call on a destroyed session aborts the process. This only applies when
  libtorrent is compiled in; `LibtorrentCompiled()` distinguishes the builds.
- **Goroutines started by HTTP handlers** (manual cycle, rename-all) must be
  tracked in a `WaitGroup` or derive from `BackgroundContext()` so they finish
  before the torrent session is torn down.
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

The default payload is **pure Go**: `gexttod`, the `gx-torrent` daemon, `run.sh`,
`VERSION` and a quick-start README. It bundles the `libtorrent` shared library
only when built with `GEXTTO_LIBTORRENT=1`; `scripts/build-release.sh` builds the
release in an Ubuntu 22.04 container (the libtorrent C++ ABI pins that baseline).
It is the payload consumed by the official installer and by the advanced
self-update command.

The same release build also produces the **standalone gx-torrent** packages,
published next to Gextto's (pure-Go variant only):

| Script | Output | Contents |
|---|---|---|
| `scripts/package-gx-torrent.sh` | `gx-torrent-linux-<arch>.tar.gz` | binary, `run.sh`, systemd unit, README, `VERSION` |
| `scripts/package-gx-torrent-windows.sh` | `gx-torrent-windows-<arch>.zip` (amd64, arm64) | `gx-torrent.exe` (native Windows service), `install-service.ps1`, `uninstall-service.ps1`, `README.txt` |

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
