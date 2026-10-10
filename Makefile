SHELL := /bin/bash
BINARY := gexttod
CMD := ./cmd/gexttod
OUT := bin/$(BINARY)

.PHONY: all build build-libtorrent fast fast-libtorrent gx-torrent test test-libtorrent test-race test-real test-rain vet fmt check-ui installer-test tidy package clean run measure-seeding

all: build

# Default build: pure Go. gx-torrent (the default engine) is a pure-Go daemon;
# the embedded libtorrent engine is opt-in (see build-libtorrent).
build:
	GEXTTO_BUMP_BUILD=1 scripts/build-daemon.sh

# Same, linking the embedded libtorrent engine (needs libtorrent-rasterbar-dev
# and a C/C++ toolchain).
build-libtorrent:
	GEXTTO_BUMP_BUILD=1 GEXTTO_LIBTORRENT=1 scripts/build-daemon.sh

fast:
	scripts/build-daemon.sh

fast-libtorrent:
	GEXTTO_LIBTORRENT=1 scripts/build-daemon.sh

# Only the gx-torrent daemon (pure Go, no libtorrent needed). Bumps the daemon's
# own build number, independent from Gextto's.
gx-torrent:
	GX_BUILD="$$(scripts/next-gx-build-number.sh)"; \
	CGO_ENABLED=0 go build -trimpath -buildvcs=false \
		-ldflags "-s -w -X github.com/buzzqw/gextto/internal/constants.GxTorrentBuild=$$GX_BUILD" \
		-o bin/gx-torrent ./cmd/gx-torrent; \
	printf 'built bin/gx-torrent (build %s)\n' "$$GX_BUILD"

test: check-ui installer-test
	CGO_ENABLED=0 go test ./...

# Tests with the embedded libtorrent engine (cgo; needs the dev headers). The
# default `make test` is pure Go and skips the libtorrent-only tests.
test-libtorrent: check-ui installer-test
	CGO_ENABLED=1 go test ./...

test-real:
	CGO_ENABLED=1 go test -run 'LibtorrentLocalTransfer|LibtorrentMagnetTransfer' -v -timeout 300s ./...

# Tests of the engine (the moved-in rain code). It now lives in internal/engine
# as part of the main module, so the packages resolve through the main go.sum.
test-rain:
	go test \
		github.com/buzzqw/gextto/internal/engine/internal/bandwidth \
		github.com/buzzqw/gextto/internal/engine/internal/bitfield \
		github.com/buzzqw/gextto/internal/engine/internal/blocklist \
		github.com/buzzqw/gextto/internal/engine/internal/mse \
		github.com/buzzqw/gextto/internal/engine/internal/peerconn \
		github.com/buzzqw/gextto/internal/engine/internal/peerconn/peerwriter \
		github.com/buzzqw/gextto/internal/engine/internal/peerprotocol \
		github.com/buzzqw/gextto/internal/engine/internal/piececache \
		github.com/buzzqw/gextto/internal/engine/internal/piecepicker \
		github.com/buzzqw/gextto/internal/engine/internal/storage/filestorage \
		github.com/buzzqw/gextto/internal/engine/internal/unchoker \
		github.com/buzzqw/gextto/internal/engine/torrent

# Race detector run (technical review, phase 0). Kept separate from `test` so it
# can stay non-blocking until the shared state has been audited.
test-race:
	CGO_ENABLED=1 go test -race ./...

# Seeding/choking measurements (gextto fork). The deterministic unchoker harness
# runs with `make test-rain`; this runs the opt-in local-swarm harness. Add
# GX_MEASURE_SUPERSEED=1 to compare against super-seeding, GX_MEASURE_LEECHERS=N
# for the swarm size. See docs/evoluzione.md (section 6).
measure-seeding:
	GX_MEASURE=1 go test ./cmd/gx-torrent/ -run MeasureSeeding -v -count=1

vet:
	CGO_ENABLED=0 go vet ./...

fmt:
	gofmt -w .

# Validate the server-rendered UI settings index.
check-ui:
	scripts/check-ui-settings-index.sh

# Side-effect-free checks for the bash installers (also run by `make test`).
installer-test:
	scripts/installer-selftest.sh

tidy:
	go mod tidy

package:
	scripts/package-linux.sh --binary $(OUT)

run: fast
	GEXTTO_DATA_DIR="$$PWD/data" GEXTTO_DRY_RUN=1 GEXTTO_ACTIVE=0 $(OUT)

clean:
	rm -rf bin
