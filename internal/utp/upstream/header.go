package upstream

// GEXTTO: rewritten in-tree (was vendored from anacrolix/utp): the uTP/BEP 29
// wire codec. The 20-byte header and the selective-ACK extension are fixed by
// the protocol; the parsing and encoding here are our own.

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// extensionTypeSelectiveAck is the BEP 29 extension type for selective ACK.
	extensionTypeSelectiveAck = 1
	// headerLen is the base uTP header size, before extensions.
	headerLen = 20
)

// extensionField is one TLV extension carried after the base header.
type extensionField struct {
	Type  byte
	Bytes []byte
}

// header is the decoded uTP packet header.
type header struct {
	Type          st
	Version       int
	ConnID        uint16
	Timestamp     uint32
	TimestampDiff uint32
	WndSize       uint32
	SeqNr         uint16
	AckNr         uint16
	Extensions    []extensionField
}

var errInvalidHeader = errors.New("invalid header")

// parseExtensions walks the extension chain. curType is the type of the first
// extension; b starts at its length byte (b[0] is the next type, b[1] the
// length). It returns the extension bytes consumed and the decoded fields.
func parseExtensions(curType byte, b []byte) (n int, ef []extensionField, err error) {
	for curType != 0 {
		if len(b) < 2 || len(b) < int(b[1])+2 {
			err = fmt.Errorf("buffer ends prematurely: %x", b)
			return
		}
		payload := b[2 : int(b[1])+2]
		ef = append(ef, extensionField{
			Type:  curType,
			Bytes: append([]byte(nil), payload...),
		})
		n += 2 + len(payload)
		curType = b[0]
		b = b[2+len(payload):]
	}
	return
}

// Unmarshal decodes a uTP header. A hostile peer may send short or malformed
// bytes: every read is length-checked and a bad header is rejected.
func (h *header) Unmarshal(b []byte) (n int, err error) {
	if len(b) < headerLen {
		err = errInvalidHeader
		return
	}
	h.Type = st(b[0] >> 4)
	h.Version = int(b[0] & 0xf)
	if h.Type > stMax || h.Version != 1 {
		err = errInvalidHeader
		return
	}
	var ext int
	ext, h.Extensions, err = parseExtensions(b[1], b[headerLen:])
	if err != nil {
		return
	}
	h.ConnID = binary.BigEndian.Uint16(b[2:4])
	h.Timestamp = binary.BigEndian.Uint32(b[4:8])
	h.TimestampDiff = binary.BigEndian.Uint32(b[8:12])
	h.WndSize = binary.BigEndian.Uint32(b[12:16])
	h.SeqNr = binary.BigEndian.Uint16(b[16:18])
	h.AckNr = binary.BigEndian.Uint16(b[18:20])
	n = headerLen + ext
	return
}

// Marshal encodes h into p, which must have room for the header and its
// extensions, and returns the number of bytes written.
func (h *header) Marshal(p []byte) (n int) {
	extLen := 0
	for _, ext := range h.Extensions {
		extLen += 2 + len(ext.Bytes)
	}
	n = headerLen + extLen
	p = p[:n]
	p[0] = byte(h.Type<<4 | 1) // version 1
	binary.BigEndian.PutUint16(p[2:4], h.ConnID)
	binary.BigEndian.PutUint32(p[4:8], h.Timestamp)
	binary.BigEndian.PutUint32(p[8:12], h.TimestampDiff)
	binary.BigEndian.PutUint32(p[12:16], h.WndSize)
	binary.BigEndian.PutUint16(p[16:18], h.SeqNr)
	binary.BigEndian.PutUint16(p[18:20], h.AckNr)
	// The base header's byte 1 holds the first extension's type; each
	// extension's byte 0 holds the type of the next one (0 ends the chain).
	nextType := p[1:2]
	rest := p[headerLen:]
	for _, ext := range h.Extensions {
		nextType[0] = ext.Type
		nextType = rest[0:1]
		rest[1] = uint8(len(ext.Bytes))
		if copy(rest[2:], ext.Bytes) != len(ext.Bytes) {
			panic("unexpected extension length")
		}
		rest = rest[2+len(ext.Bytes):]
	}
	nextType[0] = 0
	if len(rest) != 0 {
		panic("header length changed")
	}
	return
}

// selectiveAckBitmask is the payload of a selective-ACK extension, one bit per
// packet after the acked sequence number.
type selectiveAckBitmask struct {
	Bytes []byte
}

// expandBytesForBit grows the bitmask so bit index fits, rounding the length up
// to a multiple of four bytes as BEP 29 requires.
func (m *selectiveAckBitmask) expandBytesForBit(index int) {
	minLen := (3 + index/8 + 1) / 4 * 4
	for len(m.Bytes) < minLen {
		m.Bytes = append(m.Bytes, 0)
	}
}

// NumBits reports how many bits the bitmask currently holds.
func (m *selectiveAckBitmask) NumBits() int { return len(m.Bytes) * 8 }

// SetBit marks the bit at index.
func (m *selectiveAckBitmask) SetBit(index int) {
	m.expandBytesForBit(index)
	m.Bytes[index/8] |= 1 << uint(index%8)
}

// BitIsSet reports whether the bit at index is set.
func (m *selectiveAckBitmask) BitIsSet(index int) bool {
	return m.Bytes[index/8]>>uint(index%8)&1 == 1
}
