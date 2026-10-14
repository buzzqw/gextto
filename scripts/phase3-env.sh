#!/usr/bin/env bash
# phase3-env.sh prepares the real-data environment for the swarm merge Phase 3
# (block-granularity reuse, docs/evoluzione.md §12) and its measurement
# (cmd/gx-torrent TestMeasureMergePhase3).
#
# 1. Downloads public ISOs over BitTorrent (Debian netinst, Arch Linux) with
#    aria2c, unless they are already there.
# 2. For each ISO, builds two BitTorrent v2-only torrents of the same file with
#    python3-libtorrent: a source with small pieces (256 KiB) and a destination
#    with large pieces (1 MiB). Same file => same BEP 52 pieces root.
# 3. Copies the ISO as the source's data and corrupts one source piece out of
#    every CORRUPT_EVERY (default 4), so the source verifies only part of it:
#    every 1 MiB destination piece then straddles verified and unverified
#    source pieces, the exact case Phase 3 reuses at 16 KiB block level.
#
# Usage: scripts/phase3-env.sh [dir]   (default ~/gx-phase3)
# Then:  GX_MEASURE_MERGE3=<dir>/debian go test ./cmd/gx-torrent/ -run MeasureMergePhase3 -v
set -euo pipefail

DIR=${1:-$HOME/gx-phase3}
SRC_PIECE=${SRC_PIECE:-262144}
DST_PIECE=${DST_PIECE:-1048576}
CORRUPT_EVERY=${CORRUPT_EVERY:-4}
mkdir -p "$DIR/iso"

command -v aria2c >/dev/null || { echo "aria2c is required" >&2; exit 1; }
python3 -c 'import libtorrent' 2>/dev/null || { echo "python3-libtorrent is required" >&2; exit 1; }

fetch() { # name torrent-url
	local name=$1 url=$2
	[[ -f "$DIR/$name.torrent" ]] || curl -sfLo "$DIR/$name.torrent" "$url"
}
debian=$(curl -sf https://cdimage.debian.org/debian-cd/current/amd64/bt-cd/ | grep -oE 'debian-[0-9.]+-amd64-netinst\.iso\.torrent' | head -n1 || true)
arch=$(curl -sf https://archlinux.org/releng/releases/ | grep -oE '/releng/releases/[0-9.]+/torrent/' | head -n1 || true)
fetch debian "https://cdimage.debian.org/debian-cd/current/amd64/bt-cd/$debian"
fetch arch "https://archlinux.org$arch"

# Download over BitTorrent. aria2c resumes a partial download (its .aria2
# control file is there) but refuses to reopen a complete one: skip those.
todo=()
for t in "$DIR/debian.torrent" "$DIR/arch.torrent"; do
	name=$(python3 -c 'import sys, libtorrent as lt; print(lt.torrent_info(sys.argv[1]).name())' "$t")
	if [[ ! -f "$DIR/iso/$name" || -f "$DIR/iso/$name.aria2" ]]; then
		todo+=("$t")
	fi
done
if ((${#todo[@]})); then
	aria2c --seed-time=0 --bt-stop-timeout=900 --summary-interval=0 --console-log-level=warn \
		-d "$DIR/iso" "${todo[@]}"
fi

for iso in "$DIR"/iso/*.iso; do
	case "$(basename "$iso")" in
	debian*) name=debian ;;
	arch*) name=arch ;;
	*) continue ;;
	esac
	out="$DIR/$name"
	mkdir -p "$out/seed" "$out/src"
	ln -f "$iso" "$out/seed/" 2>/dev/null || cp -f "$iso" "$out/seed/"
	cp -f --reflink=auto "$iso" "$out/src/"
	python3 - "$out/seed/$(basename "$iso")" "$out" "$SRC_PIECE" "$DST_PIECE" "$CORRUPT_EVERY" <<'PY'
import os, sys, libtorrent as lt
path, out, src_piece, dst_piece, every = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4]), int(sys.argv[5])
def make(piece, dest):
    fs = lt.file_storage()
    lt.add_files(fs, path)
    ct = lt.create_torrent(fs, piece, flags=lt.create_torrent.v2_only)
    lt.set_piece_hashes(ct, os.path.dirname(path))
    with open(dest, "wb") as f:
        f.write(lt.bencode(ct.generate()))
make(src_piece, os.path.join(out, "src.torrent"))
make(dst_piece, os.path.join(out, "dst.torrent"))
# Corrupt one source piece in `every` (the first byte of each), so the source
# verifies the others only.
src = os.path.join(out, "src", os.path.basename(path))
size = os.path.getsize(src)
n = 0
with open(src, "r+b") as f:
    for k in range(1, (size + src_piece - 1) // src_piece, every):
        f.seek(k * src_piece)
        b = f.read(1)
        f.seek(k * src_piece)
        f.write(bytes([b[0] ^ 0xFF]))
        n += 1
print(f"{out}: {size} bytes, src {src_piece} B pieces ({n} corrupted), dst {dst_piece} B pieces")
PY
done
echo "ready: GX_MEASURE_MERGE3=$DIR/debian (or $DIR/arch)"
