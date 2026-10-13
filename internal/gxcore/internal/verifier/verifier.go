package verifier

import (
	"crypto/sha1"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// Verifier verifies the pieces on disk.
type Verifier struct {
	Bitfield *bitfield.Bitfield
	Error    error

	// Skip, when set, marks pieces whose bytes are already known-verified (for
	// instance the pieces a swarm merge just re-verified against this torrent's
	// own hashes): Run sets their bit without re-reading and re-hashing them.
	Skip func(uint32) bool
	// Only, when set, restricts the check to the pieces it accepts: the others
	// are left unset without being read. It is for storage created empty in
	// this run, where nothing but the pieces written before the check (a swarm
	// merge) can be valid, so hashing the rest would only read holes.
	Only func(uint32) bool
	// Prepare, when set, runs first in the verifier goroutine, so slow work
	// that must precede the check (the swarm merge reads, hashes and writes
	// whole pieces) stays off the torrent's run loop. It may set Skip and Only;
	// stop is closed when the verifier is closed and Prepare should return.
	Prepare func(stop <-chan struct{})

	closeC chan struct{}
	doneC  chan struct{}
}

// Progress information about the verification.
type Progress struct {
	Checked uint32
}

// New returns a new Verifier.
func New() *Verifier {
	return &Verifier{
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

// Close the verifier.
func (v *Verifier) Close() {
	close(v.closeC)
	<-v.doneC
}

// Run and verify all pieces of the torrent.
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
	// The buffer is sized to the largest piece: in v2 the pieces have different
	// lengths (per-file tails), so the first piece is not necessarily the widest.
	maxLen := 0
	for _, p := range pieces {
		if int(p.Length) > maxLen {
			maxLen = int(p.Length)
		}
	}
	buf := make([]byte, maxLen)
	hash := sha1.New()
	var numOK uint32
	for _, p := range pieces {
		if v.Only != nil && !v.Only(p.Index) {
			continue
		}
		if v.Skip != nil && v.Skip(p.Index) {
			v.Bitfield.Set(p.Index)
			numOK++
			select {
			case progressC <- Progress{Checked: p.Index + 1}:
			case <-v.closeC:
				return
			}
			continue
		}
		buf = buf[:p.Length]
		_, v.Error = p.Data.ReadAt(buf, 0)
		if v.Error != nil {
			return
		}
		ok := p.VerifyHash(buf, hash)
		if p.V2 {
			ok = p.VerifyV2(buf)
		}
		if ok {
			v.Bitfield.Set(p.Index)
			numOK++
		}
		select {
		case progressC <- Progress{Checked: p.Index + 1}:
		case <-v.closeC:
			return
		}
		hash.Reset()
	}
}
