// Package bitfield manipulates BEP 3 piece-availability bitfields: bit 0 is
// the most significant bit of byte 0.
package bitfield

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/bits"
)

// NumBytes returns the bytes needed for length bits.
func NumBytes(length uint32) int {
	return int((uint64(length) + 7) / 8)
}

// Bitfield is a fixed-length bit string with bounds-checked access.
type Bitfield struct {
	bytes  []byte
	length uint32
}

// New allocates a zeroed bitfield of length bits.
func New(length uint32) *Bitfield {
	return &Bitfield{
		bytes:  make([]byte, NumBytes(length)),
		length: length,
	}
}

// NewBytes wraps b without copying. Spare high bits of a trailing partial
// byte are cleared. It panics when b cannot hold length bits.
func NewBytes(b []byte, length uint32) (*Bitfield, error) {
	requiredBytes := NumBytes(length)
	if len(b) != requiredBytes {
		return nil, errors.New("invalid length")
	}
	if _, mod := divMod8(length); mod != 0 {
		b[len(b)-1] &= ^(0xff >> mod)
	}
	return &Bitfield{
		bytes:  b[:requiredBytes],
		length: length,
	}, nil
}

// Copy duplicates the bitfield.
func (b *Bitfield) Copy() *Bitfield {
	dup := &Bitfield{
		bytes:  make([]byte, len(b.bytes)),
		length: b.length,
	}
	copy(dup.bytes, b.bytes)
	return dup
}

// Bytes exposes the backing store: mutating it mutates the bitfield.
func (b *Bitfield) Bytes() []byte { return b.bytes }

// Len returns the bit count given to New.
func (b *Bitfield) Len() uint32 { return b.length }

// Hex renders the backing bytes; spare trailing bits read as clear.
func (b *Bitfield) Hex() string {
	return hex.EncodeToString(b.bytes)
}

// Set turns bit i on. It panics when i >= Len().
func (b *Bitfield) Set(i uint32) {
	b.checkIndex(i)
	div, mod := divMod8(i)
	b.bytes[div] |= 1 << (7 - mod)
}

// Clear turns bit i off. It panics when i >= Len().
func (b *Bitfield) Clear(i uint32) {
	b.checkIndex(i)
	div, mod := divMod8(i)
	b.bytes[div] &= ^(1 << (7 - mod))
}

// Test reports bit i. It panics when i >= Len().
func (b *Bitfield) Test(i uint32) bool {
	b.checkIndex(i)
	div, mod := divMod8(i)
	return (b.bytes[div] & (1 << (7 - mod))) > 0
}

// Count totals the set bits with hardware popcount, a word at a time.
func (b *Bitfield) Count() uint32 {
	var total int
	rest := b.bytes
	for len(rest) >= 8 {
		total += bits.OnesCount64(binary.LittleEndian.Uint64(rest))
		rest = rest[8:]
	}
	for _, v := range rest {
		total += bits.OnesCount8(v)
	}
	return uint32(total)
}

// All reports whether every bit is set.
func (b *Bitfield) All() bool {
	return b.Count() == b.length
}

// HasAnyNotIn reports a piece b holds that other lacks — i.e. whether a
// peer still advertises something wanted. Both sides must share the length
// (same torrent guarantees it); the shorter backing store bounds the scan.
// The loops are inlined: the per-word/byte predicate is trivial and a
// callback would double the cost in this hot path.
func (b *Bitfield) HasAnyNotIn(other *Bitfield) bool {
	n := min(len(b.bytes), len(other.bytes))
	i := 0
	for ; i+8 <= n; i += 8 {
		if binary.LittleEndian.Uint64(b.bytes[i:])&^binary.LittleEndian.Uint64(other.bytes[i:]) != 0 {
			return true
		}
	}
	for ; i < n; i++ {
		if b.bytes[i]&^other.bytes[i] != 0 {
			return true
		}
	}
	return false
}

// HasAnyOutside reports a piece the peer (b) holds that is neither held nor
// skipped: something missing and wanted. A nil skip degrades to HasAnyNotIn.
// Same length contract; a word at a time.
func (b *Bitfield) HasAnyOutside(have, skip *Bitfield) bool {
	if skip == nil {
		return b.HasAnyNotIn(have)
	}
	n := min(len(b.bytes), len(have.bytes), len(skip.bytes))
	i := 0
	for ; i+8 <= n; i += 8 {
		masked := binary.LittleEndian.Uint64(have.bytes[i:]) | binary.LittleEndian.Uint64(skip.bytes[i:])
		if binary.LittleEndian.Uint64(b.bytes[i:])&^masked != 0 {
			return true
		}
	}
	for ; i < n; i++ {
		if b.bytes[i]&^(have.bytes[i]|skip.bytes[i]) != 0 {
			return true
		}
	}
	return false
}

func (b *Bitfield) checkIndex(i uint32) {
	if i >= b.Len() {
		panic("index out of bound")
	}
}

func divMod8(a uint32) (uint32, uint32) { return a / 8, a % 8 }
