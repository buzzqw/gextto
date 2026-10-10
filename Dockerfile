# Standalone gx-torrent container: the daemon alone, usable like
# qbittorrent-nox (web UI + wizard, qBittorrent-compatible API, RSS), without
# Gextto.
#
# Build from the repository root:
#   docker build -t gx-torrent .
# Run, exposing the web UI and the peer port:
#   docker run -d --name gx-torrent -p 8080:8080 -p 6881:6881 -p 6881:6881/udp \
#     -v gx-torrent-data:/data gx-torrent
# Then open http://HOST:8080/ and follow the first-run wizard.

FROM golang:1.27 AS build
WORKDIR /src
# Copy everything first: go.mod has a replace to ./third_party/dht, so the
# module graph cannot be resolved before that path exists.
COPY . .
RUN go mod download
# The daemon's own build number, injected like the release build does.
ARG GX_BUILD=0
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false \
        -ldflags "-s -w -X github.com/buzzqw/gextto/internal/constants.GxTorrentBuild=${GX_BUILD}" \
        -o /out/gx-torrent ./cmd/gx-torrent

FROM debian:12-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd -r -u 10001 -m -d /data gx \
    && mkdir -p /data \
    && chown gx /data
COPY --from=build /out/gx-torrent /usr/local/bin/gx-torrent

USER gx
# Standalone mode: the first visit shows the wizard; data lives in /data.
ENV GX_TORRENT_MODE=standalone \
    GX_TORRENT_DATA=/data \
    GX_TORRENT_LISTEN=0.0.0.0:8080
VOLUME ["/data"]
# Web UI, then the single peer port (TCP + UDP).
EXPOSE 8080 6881/tcp 6881/udp
ENTRYPOINT ["/usr/local/bin/gx-torrent"]
