// Package bitfield provides support for manipulating bits in a []byte.
package bitfield

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/bits"
)

// NumBytes calculates the number of bytes required to represent a bitfield of `length` bits.
func NumBytes(length uint32) int {
	return int((uint64(length) + 7) / 8)
}

// Bitfield is described in BEP 3.
type Bitfield struct {
	bytes  []byte
	length uint32
}

// New creates a new Bitfield of length bits.
func New(length uint32) *Bitfield {
	return &Bitfield{
		bytes:  make([]byte, NumBytes(length)),
		length: length,
	}
}

// NewBytes returns a new Bitfield from bytes.
// Bytes in b are not copied. Unused bits in last byte are cleared.
// Panics if b is not big enough to hold "length" bits.
func NewBytes(b []byte, length uint32) (*Bitfield, error) {
	requiredBytes := NumBytes(length)
	if len(b) != requiredBytes {
		return nil, errors.New("invalid length")
	}
	if _, mod := divMod32(length); mod != 0 {
		// Clear the unused high bits in the last (incomplete) byte.
		b[len(b)-1] &= ^(0xff >> mod)
	}
	return &Bitfield{
		bytes:  b[:requiredBytes],
		length: length,
	}, nil
}

// Copy returns a new copy of Bitfield.
func (b *Bitfield) Copy() *Bitfield {
	b2 := &Bitfield{
		bytes:  make([]byte, len(b.bytes)),
		length: b.length,
	}
	copy(b2.bytes, b.bytes)
	return b2
}

// Bytes returns bytes in b. If you modify the returned slice the bits in b are modified too.
func (b *Bitfield) Bytes() []byte { return b.bytes }

// Len returns the number of bits as given to New.
func (b *Bitfield) Len() uint32 { return b.length }

// Hex returns bytes as string. If not all the bits in last byte are used, they encode as not set.
func (b *Bitfield) Hex() string {
	return hex.EncodeToString(b.bytes)
}

// Set bit i. 0 is the most significant bit. Panics if i >= b.Len().
func (b *Bitfield) Set(i uint32) {
	b.checkIndex(i)
	div, mod := divMod32(i)
	b.bytes[div] |= 1 << (7 - mod)
}

// Clear bit i. 0 is the most significant bit. Panics if i >= b.Len().
func (b *Bitfield) Clear(i uint32) {
	b.checkIndex(i)
	div, mod := divMod32(i)
	b.bytes[div] &= ^(1 << (7 - mod))
}

// Test bit i. 0 is the most significant bit. Panics if i >= b.Len().
func (b *Bitfield) Test(i uint32) bool {
	b.checkIndex(i)
	div, mod := divMod32(i)
	return (b.bytes[div] & (1 << (7 - mod))) > 0
}

// Count returns the count of set bits. It uses the hardware popcount
// (gextto fork) instead of a 256-byte lookup table.
func (b *Bitfield) Count() uint32 {
	var total int
	bytes := b.bytes
	for len(bytes) >= 8 {
		total += bits.OnesCount64(binary.LittleEndian.Uint64(bytes))
		bytes = bytes[8:]
	}
	for _, v := range bytes {
		total += bits.OnesCount8(v)
	}
	return uint32(total)
}

// All returns true if all bits are set, false otherwise.
func (b *Bitfield) All() bool {
	return b.Count() == b.length
}

func (b *Bitfield) checkIndex(i uint32) {
	if i >= b.Len() {
		panic("index out of bound")
	}
}

func divMod32(a uint32) (uint32, uint32) { return a / 8, a % 8 }
