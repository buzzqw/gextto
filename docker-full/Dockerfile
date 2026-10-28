# Full Gextto container: gexttod and the managed gx-torrent daemon, for the
# docker-full stack (Gextto + Jackett + Prowlarr + FlareSolverr + aMule).
#
# Build from the repository root; docker-full/docker-compose.yml does it for
# you. The standalone gx-torrent-only image remains the root Dockerfile.
# syntax=docker/dockerfile:1

FROM golang:1.27 AS build
WORKDIR /src
# Copy everything first: go.mod has a replace to ./internal/dht, so the module
# graph cannot be resolved before that path exists.
COPY . .
RUN go mod download

# The same version stamping as the release build. The compose file passes the
# values read from build_number / gx-torrent.build_number; the defaults keep a
# manual `docker build` working.
ARG GEXTTO_SERIES=1.2
ARG GEXTTO_BUILD=0
ARG GX_SERIES=1.1
ARG GX_BUILD=0
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false \
        -ldflags "-s -w -X github.com/buzzqw/gextto/internal/constants.Version=${GEXTTO_SERIES}.${GEXTTO_BUILD} -X github.com/buzzqw/gextto/internal/constants.VersionSeries=${GEXTTO_SERIES} -X github.com/buzzqw/gextto/internal/constants.Build=${GEXTTO_BUILD} -X github.com/buzzqw/gextto/internal/constants.Commit=docker -X github.com/buzzqw/gextto/internal/constants.BuiltAt=docker" \
        -o /out/gexttod ./cmd/gexttod
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false \
        -ldflags "-s -w -X github.com/buzzqw/gextto/internal/constants.GxTorrentBuild=${GX_BUILD} -X github.com/buzzqw/gextto/internal/constants.GxVersionSeries=${GX_SERIES} -X github.com/buzzqw/gextto/internal/gxcore/torrent.Version=${GX_SERIES}.${GX_BUILD} -X github.com/buzzqw/gextto/internal/constants.Commit=docker -X github.com/buzzqw/gextto/internal/constants.BuiltAt=docker" \
        -o /out/gx-torrent ./cmd/gx-torrent

FROM debian:12-slim
# tini reaps the managed gx-torrent child and forwards SIGTERM; curl is used by
# the compose healthcheck and by people troubleshooting in the container.
# ffmpeg brings ffprobe (the media probe) and the remux fallback; MKVToolNix
# brings mkvmerge, the preferred remuxer for Matroska. Together they implement
# the optional audio cleanup: without them Gextto starts normally and simply
# leaves the files alone.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates curl ffmpeg mkvtoolnix tini tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && useradd -r -u 1000 -m -d /data gextto \
    && mkdir -p /data /media /library /amule/incoming /amule/temp \
    && chown -R gextto:gextto /data /media /library /amule
COPY --from=build /out/gexttod /usr/local/bin/gexttod
COPY --from=build /out/gx-torrent /usr/local/bin/gx-torrent
# Licence and third-party notices of the shipped binaries.
COPY LICENSE THIRD-PARTY-NOTICES /usr/share/doc/gextto/

USER gextto
WORKDIR /data
ENV HOME=/data \
    GEXTTO_DATA_DIR=/data \
    GEXTTO_LISTEN=0.0.0.0:5000 \
    GEXTTO_ENGINE_LISTEN=127.0.0.1:8889 \
    GEXTTO_LOG=info
VOLUME ["/data", "/media", "/library", "/amule/incoming"]
# Web UI; the managed gx-torrent uses the single peer port (TCP + UDP).
EXPOSE 5000 6881/tcp 6881/udp
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=5 \
    CMD curl -fsS http://127.0.0.1:5000/api/status >/dev/null || exit 1
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/gexttod"]
