#!/usr/bin/env bash
# Build the release archive on the oldest supported baseline: Ubuntu 22.04
# (glibc 2.35). The default release is pure Go (static, no libtorrent); with
# GEXTTO_LIBTORRENT=1 it also links libtorrent 2.0.5, whose C++ ABI pins the
# baseline. A binary built on a newer system may need a newer glibc and not
# start on the older ones.
#
#   scripts/build-release.sh [--label TEXT] [--output-dir DIR]
#       runs the build in an ubuntu:22.04 container (docker or podman) and
#       leaves gextto-linux-<arch>.tar.gz(.sha256) in DIR (default: dist/)
#   scripts/build-release.sh --in-container [--label TEXT] [--output-dir DIR]
#       builds in the current system, which must be the baseline (CI jobs run
#       with `container: ubuntu:22.04`)
#
# Environment: GEXTTO_BUILD (build number; when unset the committed
# build_number file is used, so checkout and CI share the same number),
# GEXTTO_COMMIT, GEXTTO_BASE_IMAGE, GEXTTO_LIBTORRENT (set to 1 to include the
# embedded libtorrent engine; by default the release is pure Go).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BASE_IMAGE="${GEXTTO_BASE_IMAGE:-ubuntu:22.04}"
LABEL=""
OUTPUT_DIR="$ROOT/dist"
IN_CONTAINER=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --in-container) IN_CONTAINER=1; shift ;;
    --label|--output-dir)
      [[ $# -ge 2 && -n "$2" ]] || { echo "$1 requires a value" >&2; exit 2; }
      [[ "$1" == "--label" ]] && LABEL="$2" || OUTPUT_DIR="$2"
      shift 2
      ;;
    -h|--help) sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

if [[ "$IN_CONTAINER" != "1" ]]; then
  engine="$(command -v docker || command -v podman || true)"
  [[ -n "$engine" ]] || { echo "docker or podman is required (or run with --in-container on the baseline)" >&2; exit 1; }
  mkdir -p "$OUTPUT_DIR"
  OUTPUT_DIR="$(cd "$OUTPUT_DIR" && pwd)"
  commit="${GEXTTO_COMMIT:-$(git -C "$ROOT" rev-parse HEAD 2>/dev/null || true)}"
  # The checkout is mounted read-only and copied inside the container, so the
  # build never writes build numbers or binaries into the working tree.
  exec "$engine" run --rm \
    -v "$ROOT:/src:ro" -v "$OUTPUT_DIR:/out" \
    -e GEXTTO_BUILD="${GEXTTO_BUILD:-}" -e GEXTTO_COMMIT="$commit" \
    -e GEXTTO_LIBTORRENT="${GEXTTO_LIBTORRENT:-0}" \
    "$BASE_IMAGE" bash -c '
      set -euo pipefail
      mkdir -p /build
      tar -C /src --exclude=./bin --exclude=./dist --exclude=./data --exclude=./gextto-data --exclude=./.git -cf - . | tar -C /build -xf -
      /build/scripts/build-release.sh --in-container --output-dir /out '"${LABEL:+--label "$LABEL"}"'
      chown -R '"$(id -u):$(id -g)"' /out'
fi

export DEBIAN_FRONTEND=noninteractive
if command -v apt-get >/dev/null 2>&1; then
  SUDO=""
  [[ "$(id -u)" == "0" ]] || SUDO="sudo"
  $SUDO apt-get update -qq
  packages="ca-certificates curl git binutils"
  if [[ "${GEXTTO_LIBTORRENT:-0}" == "1" ]]; then
    packages="build-essential pkg-config libtorrent-rasterbar-dev $packages"
  fi
  $SUDO apt-get install -y -qq --no-install-recommends $packages >/dev/null
fi

# Go: the version required by go.mod, unless a recent enough one is installed.
GO_VERSION="$(awk '$1 == "go" { print $2; exit }' "$ROOT/go.mod")"
if ! command -v go >/dev/null 2>&1 || [[ "$(printf '%s\n%s\n' "$GO_VERSION" "$(go env GOVERSION | sed 's/^go//')" | sort -V | head -1)" != "$GO_VERSION" ]]; then
  case "$(uname -m)" in
    x86_64) goarch=amd64 ;;
    aarch64) goarch=arm64 ;;
    *) echo "unsupported build architecture: $(uname -m)" >&2; exit 1 ;;
  esac
  tarball="go${GO_VERSION}.linux-${goarch}.tar.gz"
  workdir="$(mktemp -d)"
  curl -fsSL "https://dl.google.com/go/${tarball}" -o "$workdir/$tarball"
  expected="$(curl -fsSL "https://dl.google.com/go/${tarball}.sha256" | tr -d '[:space:]')"
  echo "$expected  $workdir/$tarball" | sha256sum -c - >/dev/null
  tar -C "$workdir" -xzf "$workdir/$tarball"
  export GOROOT="$workdir/go" PATH="$workdir/go/bin:$PATH"
fi
export HOME="${HOME:-/root}" GOTOOLCHAIN=local

ARCH="$(uname -m)"
mkdir -p "$OUTPUT_DIR"
BINARY="$ROOT/bin/gexttod"
GEXTTO_BINARY="$BINARY" GEXTTO_FORCE_GXTORRENT=1 "$ROOT/scripts/build-daemon.sh"

# Fail early, with the exact reason, if the binaries do not run here.
"$BINARY" --version
"$ROOT/bin/gx-torrent" --version >/dev/null
glibc="$(objdump -T "$BINARY" | grep -o 'GLIBC_[0-9.]*' | sort -Vu | tail -1)"
echo "highest glibc symbol required: ${glibc:-none}"

"$ROOT/scripts/package-linux.sh" --binary "$BINARY" --arch "$ARCH" \
  --output "$OUTPUT_DIR/gextto-linux-${ARCH}.tar.gz" ${LABEL:+--label "$LABEL"}
# List first, then search: `tar | grep -q` fails under pipefail when grep
# stops reading early and tar gets SIGPIPE.
contents="$(tar -tzf "$OUTPUT_DIR/gextto-linux-${ARCH}.tar.gz")"
grep -qx './gx-torrent' <<< "$contents" \
  || { echo "gx-torrent is missing from the release archive" >&2; exit 1; }
