# Developer manual

This guide is for contributors who build, test or extend Gextto. Normal
installation and update instructions are in the main README.

## Project layout

```text
cmd/gexttod/       executable entry point
*.go               daemon, database, web handlers and services
uiweb/templates/   server-rendered HTML templates
uiweb/static/      CSS and browser JavaScript
uiweb/end2end/     Playwright tests
internal/           shared packages and backend helpers
scripts/            build, update, packaging and service helpers
docs/               user, advanced-user and developer documentation
```

The root Go package contains the application logic. `cmd/gexttod/main.go` only
assembles and starts the executable.

## Requirements and build

Install Go 1.26 or newer, a C++17 toolchain and the `libtorrent-rasterbar`
development headers. The normal build embeds the web UI and does not need a
separate frontend build.

```bash
make build
go test ./...
```

The executable is written to `bin/gexttod`. The optional native-Go torrent
backend uses the `anacrolix` build tag:

```bash
make build-anacrolix
make test-anacrolix
```

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

The server-rendered UI is assembled in `uiweb.go`, `uiweb_sections.go`,
`uiweb_pages.go` and `uiweb/templates`. Static browser behavior belongs in
`uiweb/static/gextto-ui.js`; styles belong in `gextto-ui.css`.

When adding an API endpoint:

1. implement the handler with the shared `AppState` signature;
2. register it in `web_router.go`;
3. add authorization, validation and bounded input handling;
4. document it in `docs/API.md`;
5. add a focused Go test and, when it changes the UI, a Playwright test.

Use the existing JSON helpers and keep destructive operations explicit and
confirmable in the UI.

## Database changes

Database schema changes must be migration-safe for existing installations.
Use the database migration helpers, parameterised SQL and tests covering both a
fresh database and an older schema. Never put user data, local paths or secrets
in fixtures committed to the repository.

## Testing

Useful checks before a commit:

```bash
gofmt -w changed.go
go test ./...
node --check uiweb/static/gextto-ui.js
git diff --check
```

The end-to-end suite is under `uiweb/end2end` and requires a running daemon:

```bash
cd uiweb/end2end
npx playwright test
```

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
