package torrent

// torrent_selection.go (gextto fork): download only some files of a torrent.

import (
	"path/filepath"

	"github.com/buzzqw/gextto/internal/gxcore/internal/storage"
	"github.com/buzzqw/gextto/internal/gxcore/internal/storage/filestorage"
)

// skippedInfoFiles maps Config.FileSelection (non-padding files) onto
// info.Files. nil means every file is wanted.
func (t *torrent) skippedInfoFiles() []bool {
	selection := t.session.config.FileSelection
	if selection == nil || t.info == nil {
		return nil
	}
	flags := selection(t.id)
	if len(flags) == 0 {
		return nil
	}
	out := make([]bool, len(t.info.Files))
	any := false
	j := 0
	for i, f := range t.info.Files {
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
