package torrent

// gextto fork: prioritize the pieces a player is reading, so a media player can
// stream a file while it is still being downloaded.

// filePieceRange maps a byte range of the non-padding file `fileIndex` to the
// piece range that covers it. It must run in the torrent goroutine.
func (t *torrent) filePieceRange(fileIndex int, offset, length int64) (uint32, uint32, bool) {
	if t.info == nil || fileIndex < 0 || length <= 0 || t.info.PieceLength == 0 {
		return 0, 0, false
	}
	// The user-facing file index skips the padding files, which still count for
	// the absolute byte offset.
	var absolute int64
	var index int
	found := false
	for _, f := range t.info.Files {
		if f.Padding {
			absolute += f.Length
			continue
		}
		if index == fileIndex {
			found = true
			break
		}
		absolute += f.Length
		index++
	}
	if !found {
		return 0, 0, false
	}
	start := absolute + offset
	if start < 0 {
		start = 0
	}
	pl := int64(t.info.PieceLength)
	begin := uint32(start / pl)
	end := uint32((start+length-1)/pl) + 1
	if end > t.info.NumPieces {
		end = t.info.NumPieces
	}
	if begin >= end {
		return 0, 0, false
	}
	return begin, end, true
}

// FilePieceRange returns the piece range covering `length` bytes at `offset` of
// the non-padding file `fileIndex` (gextto fork).
func (t *Torrent) FilePieceRange(fileIndex int, offset, length int64) (begin, end uint32, ok bool) {
	type result struct {
		begin, end uint32
		ok         bool
	}
	got := query(t.torrent, func() result {
		b, e, found := t.torrent.filePieceRange(fileIndex, offset, length)
		return result{b, e, found}
	})
	return got.begin, got.end, got.ok
}

// SetFileStreamWindow makes the piece picker request the pieces of a file range
// before any other (gextto fork); `length <= 0` clears the window. It returns
// the piece range set.
func (t *Torrent) SetFileStreamWindow(fileIndex int, offset, length int64) (begin, end uint32, ok bool) {
	type result struct {
		begin, end uint32
		ok         bool
	}
	got := query(t.torrent, func() result {
		if t.torrent.piecePicker == nil {
			return result{}
		}
		if length <= 0 {
			t.torrent.piecePicker.SetStreamWindow(0, 0)
			return result{}
		}
		b, e, found := t.torrent.filePieceRange(fileIndex, offset, length)
		if !found {
			return result{}
		}
		t.torrent.piecePicker.SetStreamWindow(b, e)
		return result{b, e, true}
	})
	return got.begin, got.end, got.ok
}
