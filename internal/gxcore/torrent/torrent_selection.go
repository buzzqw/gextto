package torrent

// torrent_selection.go: download only some files of a torrent.

import (
	"path/filepath"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/storage"
	"github.com/buzzqw/gextto/internal/gxcore/internal/storage/filestorage"
)

// selectionPriorities resolves the priority of every info.Files entry
// (0 = skip, 1..4 = normal, 5..7 = high), or nil when no explicit selection is
// configured. An app-provided FilePriorities list takes precedence over the
// legacy FileSelection skip list; when neither is set the BEP 53 magnet "so="
// seed is used.
func (t *torrent) selectionPriorities() []int {
	if t.info() == nil {
		return nil
	}
	if fp := t.session.config.FilePriorities; fp != nil {
		if prio := fp(t.id); len(prio) > 0 {
			return prioritiesToInfoFiles(t.info(), prio)
		}
	}
	if selection := t.session.config.FileSelection; selection != nil {
		if flags := selection(t.id); len(flags) > 0 {
			prio := make([]int, len(flags))
			for i, skip := range flags {
				if !skip {
					prio[i] = piece.PriorityNormal
				}
			}
			return prioritiesToInfoFiles(t.info(), prio)
		}
	}
	if skip := maskFromSelectOnly(t.info(), t.getSelectOnly()); skip != nil {
		prio := make([]int, len(t.info().Files))
		for i := range prio {
			prio[i] = piece.PriorityNormal
			if skip[i] {
				prio[i] = 0
			}
		}
		return prio
	}
	return nil
}

// skippedInfoFiles maps the selection onto info.Files: true means "do not
// download". nil means every file is wanted.
func (t *torrent) skippedInfoFiles() []bool {
	prio := t.selectionPriorities()
	if prio == nil {
		return nil
	}
	out := make([]bool, len(prio))
	any := false
	for i, p := range prio {
		if p <= 0 {
			out[i] = true
			any = true
		}
	}
	if !any {
		return nil
	}
	return out
}

// prioritiesToInfoFiles maps a per-non-padding-file priority list onto
// info.Files (padding files are normal). nil means no explicit selection, so
// every file is normal.
func prioritiesToInfoFiles(info *metainfo.Info, prio []int) []int {
	out := make([]int, len(info.Files))
	any := false
	j := 0
	for i, f := range info.Files {
		out[i] = piece.PriorityNormal
		if f.Padding {
			continue
		}
		if j < len(prio) {
			out[i] = prio[j]
		}
		if out[i] != piece.PriorityNormal {
			any = true
		}
		j++
	}
	if !any {
		return nil
	}
	return out
}

// piecePriorities returns the priority of every piece: the highest priority of
// the non-padding files it holds data from (0 when it holds no wanted file).
// nil means "every piece is normal", so the picker keeps no per-piece value.
func (t *torrent) piecePriorities() []uint8 {
	prio := t.selectionPriorities()
	if prio == nil || t.info() == nil {
		return nil
	}
	pieceLength := int64(t.info().PieceLength)
	out := make([]uint8, t.info().NumPieces)
	var offset int64
	for i, f := range t.info().Files {
		start, end := offset, offset+f.Length
		offset = end
		if f.Padding || f.Length == 0 || prio[i] <= 0 {
			continue
		}
		for p := start / pieceLength; p <= (end-1)/pieceLength && p < int64(len(out)); p++ {
			if uint8(prio[i]) > out[p] {
				out[p] = uint8(prio[i])
			}
		}
	}
	return out
}

// getSelectOnly returns the BEP 53 "so" seed. See selectOnlyMu.
func (t *torrent) getSelectOnly() []int {
	t.selectOnlyMu.Lock()
	defer t.selectOnlyMu.Unlock()
	return t.selectOnly
}

// setSelectOnly replaces the BEP 53 "so" seed. See selectOnlyMu.
func (t *torrent) setSelectOnly(wanted []int) {
	t.selectOnlyMu.Lock()
	t.selectOnly = wanted
	t.selectOnlyMu.Unlock()
}

// maskFromFlags turns a per-non-padding-file skip list into a mask indexed by
// info.Files (padding included). nil means nothing is skipped.
func maskFromFlags(info *metainfo.Info, flags []bool) []bool {
	out := make([]bool, len(info.Files))
	any := false
	j := 0
	for i, f := range info.Files {
		if f.Padding {
			continue
		}
		if j < len(flags) && flags[j] {
			out[i] = true
			any = true
		}
		j++
	}
	if !any {
		return nil
	}
	return out
}

// maskFromSelectOnly returns the skip mask for a BEP 53 "so" selection: every
// non-padding file whose index is not in wanted is skipped. nil means every
// file is wanted: the parameter is ignored when it names no valid file (this
// avoids a magnet that would download nothing).
func maskFromSelectOnly(info *metainfo.Info, wanted []int) []bool {
	if len(wanted) == 0 {
		return nil
	}
	want := make(map[int]struct{}, len(wanted))
	for _, i := range wanted {
		if i >= 0 {
			want[i] = struct{}{}
		}
	}
	out := make([]bool, len(info.Files))
	anyWanted, anySkip := false, false
	j := 0
	for i, f := range info.Files {
		if f.Padding {
			continue
		}
		if _, ok := want[j]; ok {
			anyWanted = true
		} else {
			out[i] = true
			anySkip = true
		}
		j++
	}
	if !anyWanted || !anySkip {
		return nil
	}
	return out
}

// skippedPieces flags the pieces that touch no wanted file. nil means none.
func (t *torrent) skippedPieces(skipFiles []bool) []bool {
	if skipFiles == nil || t.info() == nil {
		return nil
	}
	pieceLength := int64(t.info().PieceLength)
	wanted := make([]bool, t.info().NumPieces)
	var offset int64
	for i, f := range t.info().Files {
		start, end := offset, offset+f.Length
		offset = end
		if f.Padding || skipFiles[i] || f.Length == 0 {
			continue
		}
		for p := start / pieceLength; p <= (end-1)/pieceLength && p < int64(len(wanted)); p++ {
			wanted[p] = true
		}
	}
	skip := make([]bool, len(wanted))
	for i := range wanted {
		skip[i] = !wanted[i]
	}
	return skip
}

func (t *torrent) partsStorage(skip []bool) storage.Storage {
	dir := t.session.config.PartsDir
	if skip == nil || dir == "" {
		return nil
	}
	sto, err := filestorage.New(filepath.Join(dir, t.id), t.session.config.FilePermissions)
	if err != nil {
		t.log.Errorf("cannot open parts storage: %s", err)
		return nil
	}
	return sto
}

func (t *torrent) applySkippedPieces() {
	skip := t.skippedPieces(t.skippedInfoFiles())
	prio := t.piecePriorities()
	t.skipBits = nil
	for i := range t.pieces {
		t.pieces[i].Skip = skip != nil && skip[i]
		if t.pieces[i].Skip {
			if t.skipBits == nil {
				t.skipBits = bitfield.New(uint32(len(t.pieces)))
			}
			t.skipBits.Set(uint32(i))
		}
		t.pieces[i].Priority = piece.PriorityNormal
		if prio != nil {
			t.pieces[i].Priority = prio[i]
		}
	}
}

// peerHasWanted reports whether the peer advertises a piece we miss and want
// (not in a deselected file). It runs a word at a time over the bitfields, so
// it is cheap enough for every peer at every completed piece.
func (t *torrent) peerHasWanted(pe *peer.Peer) bool {
	if pe.Bitfield == nil || t.bitfield == nil {
		return false
	}
	return pe.Bitfield.HasAnyOutside(t.bitfield, t.skipBits)
}

// wantedComplete reports whether every wanted piece is downloaded.
func (t *torrent) wantedComplete() bool {
	if t.bitfield == nil {
		return false
	}
	skip := t.skippedPieces(t.skippedInfoFiles())
	for i := uint32(0); i < t.bitfield.Len(); i++ {
		if skip != nil && skip[i] {
			continue
		}
		if !t.bitfield.Test(i) {
			return false
		}
	}
	return true
}

// uploadOnly reports whether we are a full or partial seed (BEP 21): every
// wanted piece is present, so we are not interested in downloading anything.
func (t *torrent) uploadOnly() bool {
	return t.completed
}

// resetCompletionIfWantedMissing reopens a torrent that was complete when
// the selection grew.
func (t *torrent) resetCompletionIfWantedMissing() {
	if t.completed && !t.wantedComplete() {
		t.completed = false
		t.completeC = make(chan struct{})
	}
}

// selectedBytes returns the size of the wanted pieces and how much of it is
// downloaded, from the bitfield (also while stopped).
func (t *torrent) selectedBytes() (total, done int64) {
	if t.info() == nil {
		return 0, 0
	}
	skip := t.skippedPieces(t.skippedInfoFiles())
	pieceLength := int64(t.info().PieceLength)
	for i := uint32(0); i < t.info().NumPieces; i++ {
		if skip != nil && skip[i] {
			continue
		}
		length := pieceLength
		if i == t.info().NumPieces-1 {
			length = t.info().Length - pieceLength*int64(t.info().NumPieces-1)
		}
		total += length
		if t.bitfield != nil && t.bitfield.Test(i) {
			done += length
		}
	}
	return total, done
}
