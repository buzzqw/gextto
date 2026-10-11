package torrent

// torrent_selection.go: download only some files of a torrent.

import (
	"path/filepath"

	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	"github.com/buzzqw/gextto/internal/gxcore/internal/storage"
	"github.com/buzzqw/gextto/internal/gxcore/internal/storage/filestorage"
)

// skippedInfoFiles maps Config.FileSelection (non-padding files) onto
// info.Files. nil means every file is wanted. An app-provided selection takes
// precedence; when the app has none, the BEP 53 magnet "so=" seed is used.
func (t *torrent) skippedInfoFiles() []bool {
	if t.info == nil {
		return nil
	}
	if selection := t.session.config.FileSelection; selection != nil {
		if flags := selection(t.id); len(flags) > 0 {
			return maskFromFlags(t.info, flags)
		}
	}
	return maskFromSelectOnly(t.info, t.getSelectOnly())
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
	if skipFiles == nil || t.info == nil {
		return nil
	}
	pieceLength := int64(t.info.PieceLength)
	wanted := make([]bool, t.info.NumPieces)
	var offset int64
	for i, f := range t.info.Files {
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
	for i := range t.pieces {
		t.pieces[i].Skip = skip != nil && skip[i]
	}
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
	if t.info == nil {
		return 0, 0
	}
	skip := t.skippedPieces(t.skippedInfoFiles())
	pieceLength := int64(t.info.PieceLength)
	for i := uint32(0); i < t.info.NumPieces; i++ {
		if skip != nil && skip[i] {
			continue
		}
		length := pieceLength
		if i == t.info.NumPieces-1 {
			length = t.info.Length - pieceLength*int64(t.info.NumPieces-1)
		}
		total += length
		if t.bitfield != nil && t.bitfield.Test(i) {
			done += length
		}
	}
	return total, done
}
