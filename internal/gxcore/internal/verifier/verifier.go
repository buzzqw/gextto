package verifier

import (
	"crypto/sha1"
	"hash"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// Verifier hashes every piece on disk into a bitfield. It runs on its own
// goroutine: Run blocks until done, Close aborts mid-run.
type Verifier struct {
	Bitfield *bitfield.Bitfield
	Error    error

	// Skip marks pieces already known-verified (e.g. just re-verified by a
	// swarm merge against this torrent's own hashes): their bit is set
	// without re-reading or re-hashing.
	Skip func(uint32) bool
	// Only restricts the check to accepted pieces; the rest stay unset and
	// unread. For storage created empty this run nothing but pre-written
	// pieces (a merge) can be valid, so hashing the holes is skipped.
	Only func(uint32) bool
	// Prepare runs first on the verifier goroutine for slow preamble work
	// (the merge's reads, hashes and writes) that must stay off the run
	// loop. It may set Skip and Only; stop closes when Close is called.
	Prepare func(stop <-chan struct{})

	closeC chan struct{}
	doneC  chan struct{}
}

// Progress reports the latest checked piece index (1-based count).
type Progress struct {
	Checked uint32
}

// New prepares a verifier; run it with Run.
func New() *Verifier {
	return &Verifier{
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

// Close aborts the run and waits for it to unwind.
func (v *Verifier) Close() {
	close(v.closeC)
	<-v.doneC
}

// Run checks every piece, reporting progress per piece and delivering
// itself on resultC — unless closed first, in which case nobody is sent.
func (v *Verifier) Run(pieces []piece.Piece, progressC chan Progress, resultC chan *Verifier) {
	defer close(v.doneC)
	defer func() {
		select {
		case resultC <- v:
		case <-v.closeC:
		}
	}()

	v.Bitfield = bitfield.New(uint32(len(pieces)))
	if v.Prepare != nil {
		v.Prepare(v.closeC)
		select {
		case <-v.closeC:
			return
		default:
		}
	}
	// One buffer sized to the widest piece: v2 pieces vary (per-file
	// tails), so the first piece is not necessarily the largest.
	buf := make([]byte, widestPiece(pieces))
	hash := sha1.New()
	var numOK uint32
	for _, p := range pieces {
		if v.Only != nil && !v.Only(p.Index) {
			continue
		}
		if v.Skip != nil && v.Skip(p.Index) {
			v.Bitfield.Set(p.Index)
			numOK++
			if !v.report(progressC, p.Index+1) {
				return
			}
			continue
		}
		buf = buf[:p.Length]
		if _, v.Error = p.Data.ReadAt(buf, 0); v.Error != nil {
			return
		}
		if ok := v.checkPiece(&p, buf, hash); ok {
			v.Bitfield.Set(p.Index)
			numOK++
		}
		if !v.report(progressC, p.Index+1) {
			return
		}
		hash.Reset()
	}
}

// widestPiece returns the largest piece length for the shared read buffer.
func widestPiece(pieces []piece.Piece) int {
	maxLen := 0
	for _, p := range pieces {
		if int(p.Length) > maxLen {
			maxLen = int(p.Length)
		}
	}
	return maxLen
}

// checkPiece hashes buf: SHA-1 for v1, Merkle verification for v2 pieces. The
// v1 hasher is reused across pieces, so the branch keeps that allocation out
// of the loop instead of calling Piece.Verify.
func (v *Verifier) checkPiece(p *piece.Piece, buf []byte, hash hash.Hash) bool {
	if p.V2 {
		return p.VerifyV2(buf)
	}
	return p.VerifyHash(buf, hash)
}

// report publishes one progress step; false means closed meanwhile.
func (v *Verifier) report(progressC chan Progress, checked uint32) bool {
	select {
	case progressC <- Progress{Checked: checked}:
		return true
	case <-v.closeC:
		return false
	}
}
