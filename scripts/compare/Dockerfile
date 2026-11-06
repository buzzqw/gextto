# Images of the client comparison (scripts/compare/run.py, docs/measurements.md).
# Every client runs on the same Debian base, from its distribution package at a
# pinned version; gx-torrent is built from this checkout. Build one with:
#   docker build -f scripts/compare/Dockerfile --target qbittorrent -t gxc-qbittorrent .
FROM debian:13-slim@sha256:f6b08b09a05725f9431502d3b5714f5c4576e82c1a84fa1debe0c24a7e0c3ec4 AS base
RUN apt-get update \
    && apt-get install -y --no-install-recommends iproute2 ca-certificates \
    && rm -rf /var/lib/apt/lists/*

FROM golang:1.27 AS gx-build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-s -w" -o /out/gx-torrent ./cmd/gx-torrent

FROM base AS gx-torrent
COPY --from=gx-build /out/gx-torrent /usr/local/bin/gx-torrent

FROM base AS qbittorrent
RUN apt-get update \
    && apt-get install -y --no-install-recommends qbittorrent-nox=5.1.0-2 \
    && rm -rf /var/lib/apt/lists/*

FROM base AS transmission
RUN apt-get update \
    && apt-get install -y --no-install-recommends transmission-daemon=4.1.0~beta2+dfsg-3+deb13u2 \
    && rm -rf /var/lib/apt/lists/*

FROM base AS deluge
RUN apt-get update \
    && apt-get install -y --no-install-recommends deluged=2.2.0-1 deluge-web=2.2.0-1 \
    && rm -rf /var/lib/apt/lists/*

# rain and anacrolix/torrent are Go code with no distribution package that pins
# a version, so they are built from the tagged source (the same rule as
# gx-torrent). anacrolix/torrent is a library: the image ships the small front
# end in scripts/compare/anacrolix that adds, rechecks and reports the torrents.
FROM golang:1.27 AS rain-build
RUN CGO_ENABLED=0 go install github.com/cenkalti/rain/v2@v2.4.5

FROM base AS rain
COPY --from=rain-build /go/bin/rain /usr/local/bin/rain

FROM golang:1.27 AS anacrolix-build
WORKDIR /src
COPY scripts/compare/anacrolix/go.mod scripts/compare/anacrolix/go.sum ./
RUN go mod download
COPY scripts/compare/anacrolix/main.go ./
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-s -w" -o /out/anacrolix .

FROM base AS anacrolix
COPY --from=anacrolix-build /out/anacrolix /usr/local/bin/anacrolix

# The tracker of the isolated network.
FROM python:3.13-slim AS tracker
COPY scripts/compare/tracker.py /tracker.py
