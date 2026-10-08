SHELL := /bin/bash
BINARY := gexttod
CMD := ./cmd/gexttod
OUT := bin/$(BINARY)

.PHONY: all build fast gx-torrent test test-race test-real test-rain vet fmt check-ui installer-test tidy package clean run

all: build

build:
	GEXTTO_BUMP_BUILD=1 scripts/build-daemon.sh

fast:
	scripts/build-daemon.sh

# Only the gx-torrent daemon (pure Go, no libtorrent needed). Bumps the daemon's
# own build number, independent from Gextto's.
gx-torrent:
	GX_BUILD="$$(scripts/next-gx-build-number.sh)"; \
	CGO_ENABLED=0 go build -trimpath -buildvcs=false \
		-ldflags "-s -w -X github.com/buzzqw/gextto/internal/constants.GxTorrentBuild=$$GX_BUILD" \
		-o bin/gx-torrent ./cmd/gx-torrent; \
	printf 'built bin/gx-torrent (build %s)\n' "$$GX_BUILD"

test: check-ui installer-test
	CGO_ENABLED=1 go test ./...

test-real:
	CGO_ENABLED=1 go test -run 'LibtorrentLocalTransfer|LibtorrentMagnetTransfer' -v -timeout 300s ./...

# Tests of the vendored rain fork. Run from the root module so the fork packages
# resolve their dependencies through the main go.sum: the nested module has no
# complete go.sum of its own (see docs/rain-allineamento.md), so
# `cd third_party/rain && go test ./...` would fail before it even compiles.
test-rain:
	go test \
		github.com/cenkalti/rain/v2/internal/bandwidth \
		github.com/cenkalti/rain/v2/internal/blocklist \
		github.com/cenkalti/rain/v2/internal/peerconn \
		github.com/cenkalti/rain/v2/internal/piecepicker \
		github.com/cenkalti/rain/v2/internal/storage/filestorage \
		github.com/cenkalti/rain/v2/internal/unchoker \
		github.com/cenkalti/rain/v2/torrent

# Race detector run (technical review, phase 0). Kept separate from `test` so it
# can stay non-blocking until the shared state has been audited.
test-race:
	CGO_ENABLED=1 go test -race ./...

vet:
	CGO_ENABLED=1 go vet ./...

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
