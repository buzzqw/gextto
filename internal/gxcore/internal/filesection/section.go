package filesection

import "io"

// FileSection maps one torrent file (or a slice of it) onto storage.
type FileSection struct {
	File   ReadWriterAt
	Offset int64
	Length int64
	Name   string
	// Padding marks filler bytes between files: never requested from peers,
	// always zero, skipped on writes to keep neighbors aligned.
	Padding bool
}

// ReadWriterAt combines the io.ReaderAt and io.WriterAt interfaces.
type ReadWriterAt interface {
	io.ReaderAt
	io.WriterAt
}

// Piece is the file sections one torrent piece spans. Pieces are computed
// over the concatenation of all files, so a piece usually straddles a file
// boundary (or padding).
type Piece []FileSection

// ReadAt implements io.ReaderAt, serving len(b) bytes from piece offset off.
// Used when uploading blocks. It reads section by section straight from
// storage — no SectionReader/MultiReader chain, zero allocations — with the
// same completion contract as io.ReadFull: nil on a full read,
// io.ErrUnexpectedEOF on a short one.
func (p Piece) ReadAt(b []byte, off int64) (int, error) {
	var n int
	var pos int64 // piece-relative offset of the current section
	for i := range p {
		sec := &p[i]
		if pos+sec.Length <= off {
			pos += sec.Length
			continue
		}
		start := sec.Offset
		if pos < off {
			start += off - pos
		}
		want := int(sec.Length - (start - sec.Offset))
		if rest := len(b) - n; want > rest {
			want = rest
		}
		for want > 0 {
			m, rerr := sec.File.ReadAt(b[n:n+want], start)
			n += m
			start += int64(m)
			want -= m
			if rerr != nil {
				return n, readErr(rerr, n, len(b))
			}
			if m == 0 {
				break // (0, nil) reads must not spin; move to next section
			}
		}
		pos += sec.Length
		if n == len(b) {
			return n, nil
		}
	}
	if n == 0 && len(b) > 0 {
		return 0, io.EOF
	}
	return n, io.ErrUnexpectedEOF
}

// readErr maps a storage read failure to ReadFull's contract: a bare EOF
// after a partial read becomes ErrUnexpectedEOF, anything else passes
// through untouched.
func readErr(rerr error, n, total int) error {
	if rerr == io.EOF {
		if n == 0 && total > 0 {
			return io.EOF
		}
		return io.ErrUnexpectedEOF
	}
	return rerr
}

// Write stores a fully verified piece across its sections. Padding sections
// only consume buffer space (their bytes stay zero on disk). len(b) must
// equal the sections' total length for a complete write.
func (p Piece) Write(b []byte) (n int, err error) {
	var m int
	for _, sec := range p {
		if sec.Padding {
			b = b[sec.Length:]
			continue
		}
		m, err = sec.File.WriteAt(b[:sec.Length], sec.Offset)
		n += m
		if err != nil {
			return n, err
		}
		b = b[m:]
	}
	return n, nil
}
