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

Install Go 1.26 or newer, a C++17 toolchain and `libtorrent-rasterbar`
development headers. The normal build embeds the web UI; it does not need a
separate frontend build.

```bash
make build
go test ./...
```

The executable is written to `bin/gexttod`. Supported torrent backends are
embedded libtorrent and the qBittorrent-nox Web API adapter.

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

The official UI is served at `/`; `/v2` is a technical alias and the removed
legacy `/ui` route must not be reintroduced. Live shell metrics are rendered by
the `/v2/partial/chrome` HTMX fragment, while the Scarico table uses its own
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

## Database changes

Database schema changes must be migration-safe for existing installations.
Use the database migration helpers, parameterised SQL and tests covering both a
fresh database and an older schema. Never put user data, local paths or secrets
in fixtures committed to the repository.

## Testing

Run these checks before a commit:

```bash
gofmt -w changed.go
go test ./...
go test -race ./...
node --check uiweb/v2/static/v2-core.js
git diff --check
```

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

The daemon mixes HTTP handlers, long-lived workers and a CGo libtorrent
session, so a few invariants are load-bearing:

- **The native session handle is shared state.** Reads of `LibtorrentClient.session`
  must take `sessionMu.RLock()`; `Shutdown` destroys the handle under
  `sessionMu.Lock()`. Never read the field directly in a new method — a cgo
  call on a destroyed session aborts the process.
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

The payload contains `gexttod`, the bundled `libtorrent`, `run.sh`, `VERSION`
and a quick-start README. It is the payload consumed by the official installer
and by the advanced self-update command.

Keep generated binaries, databases, logs and local configuration out of Git.
Release checks and CI configuration are maintained outside the user manuals.
