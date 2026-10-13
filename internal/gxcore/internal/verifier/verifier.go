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
