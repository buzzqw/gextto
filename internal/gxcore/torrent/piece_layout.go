package torrent

import "github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"

// piece_layout.go maps files to pieces for both layouts, from the metainfo
// alone (so it also works while the torrent is stopped and t.pieces is empty).
//
// v1 and hybrid torrents lay the files end to end: a piece can span several
// files, and only the very last piece of the torrent is short. A v2-only
// torrent (BEP 52) gives every file its own pieces instead: a file starts on a
// fresh piece and its last piece is short. Treating a v2-only torrent as one
// stream selects the wrong pieces, counts the short tails as full pieces and
// can declare a file complete while its tail is missing.

// forEachFilePiece calls fn for every overlap between a file and a piece: n
// bytes of info.Files[fi] live in piece. Padding and empty files are skipped.
func forEachFilePiece(info *metainfo.Info, fn func(fi int, piece uint32, n int64)) {
	pl := int64(info.PieceLength)
	if pl <= 0 {
		return
	}
	if info.V2 {
		var global uint32
		for fi, f := range info.Files {
			for off := int64(0); off < f.Length; off += pl {
				fn(fi, global, min(pl, f.Length-off))
				global++
			}
		}
		return
	}
	var offset int64
	for fi, f := range info.Files {
		start, end := offset, offset+f.Length
		offset = end
		if f.Padding || f.Length == 0 {
			continue
		}
		for p := start / pl; p*pl < end && p < int64(info.NumPieces); p++ {
			fn(fi, uint32(p), min((p+1)*pl, end)-max(p*pl, start))
		}
	}
}

// forEachPieceLength calls fn with the length of every piece, in order,
// without allocating.
func forEachPieceLength(info *metainfo.Info, fn func(piece uint32, n int64)) {
	pl := int64(info.PieceLength)
	if pl <= 0 || info.NumPieces == 0 {
		return
	}
	if info.V2 {
		var global uint32
		for _, f := range info.Files {
			for off := int64(0); off < f.Length; off += pl {
				fn(global, min(pl, f.Length-off))
				global++
			}
		}
		return
	}
	last := info.NumPieces - 1
	for i := uint32(0); i < last; i++ {
		fn(i, pl)
	}
	fn(last, info.Length-pl*int64(last))
}

// filePieceBounds returns the first piece of info.Files[fi] and how many
// pieces hold its bytes, plus the offset of the file inside the first piece
// (always 0 for v2-only).
func filePieceBounds(info *metainfo.Info, fi int) (first uint32, count uint32, offsetInFirst int64, ok bool) {
	pl := int64(info.PieceLength)
	if pl <= 0 || fi < 0 || fi >= len(info.Files) {
		return 0, 0, 0, false
	}
	f := info.Files[fi]
	if f.Padding || f.Length == 0 {
		return 0, 0, 0, false
	}
	if info.V2 {
		var global int64
		for i := 0; i < fi; i++ {
			global += (info.Files[i].Length + pl - 1) / pl
		}
		return uint32(global), uint32((f.Length + pl - 1) / pl), 0, true
	}
	var start int64
	for i := 0; i < fi; i++ {
		start += info.Files[i].Length
	}
	end := start + f.Length
	firstPiece := start / pl
	lastPiece := (end - 1) / pl
	return uint32(firstPiece), uint32(lastPiece - firstPiece + 1), start - firstPiece*pl, true
}
