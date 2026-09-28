# Development and local installations

This document contains the technical instructions that are intentionally kept
out of the main README. Normal users only need the installation and update
commands shown there.

## Requirements

- Go 1.26 or newer;
- a C++17 toolchain;
- `libtorrent-rasterbar` development headers and library;
- Linux with the libraries required by the selected build.

The web UI is embedded into the Go binary. No separate frontend build is
needed.

## Build from source

```bash
make build
```

The binary is written to `bin/gexttod`. The equivalent direct command is:

```bash
CGO_ENABLED=1 go build -o bin/gexttod ./cmd/gexttod
```

Run the test suite with:

```bash
go test ./...
```

The optional native-Go torrent backend uses a build tag:

```bash
make build-anacrolix
make test-anacrolix
```

## Local dry-run

To run a local daemon without real downloads:

```bash
GEXTTO_DATA_DIR="$PWD/data" \
GEXTTO_ACTIVE=0 \
GEXTTO_DRY_RUN=1 \
./bin/gexttod --dry-run
```

## Per-user systemd service

```bash
GEXTTO_DATA_DIR="$HOME/gextto-data" \
  GEXTTO_LISTEN=127.0.0.1:5000 \
  scripts/install-user-service.sh
systemctl --user status gextto.service
```

To keep the service running after logout:

```bash
loginctl enable-linger "$USER"
```

After changing the source checkout, update it with:

```bash
git pull --ff-only
./scripts/update.sh
```

## Packaging and releases

The standalone Linux payload is produced by:

```bash
make build
scripts/package-linux.sh
```

The generated archive is the same type consumed by the installer and by the
advanced `gexttod --update` command.

Release and CI details belong to the project wiki and are not part of the user
documentation.
