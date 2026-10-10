package peerprotocol

// BEP 52 "hash request", "hashes" and "hash reject" messages. Unlike the
// BEP 10 extensions these are plain peer messages (ids 21, 22, 23) with a
// fixed binary payload (not bencoded):
//
//	pieces root  (32 bytes)
//	base layer   (4 bytes, big-endian)
//	index        (4 bytes, big-endian)
//	length       (4 bytes, big-endian)
//	proof layers (4 bytes, big-endian)
//
// A "hashes" message carries the same header followed by the requested hashes
// and the uncle hashes that anchor them to the file's pieces root.

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	// HashSize is the size in bytes of a SHA-256 hash on the wire.
	HashSize = 32
	// HashRequestHeaderSize is the fixed part of every BEP 52 hash message.
	HashRequestHeaderSize = HashSize + 4*4
)

// HashRequest is the header shared by "hash request", "hashes" and
// "hash reject" messages. It is also used to match a reply to its request.
type HashRequestHeader struct {
	// Root is the merkle root ("pieces root") of the file.
	Root [HashSize]byte
	// Base is the lowest requested layer of the hash tree: 0 is the leaf
	// (block) layer, higher values climb toward the root.
	Base uint32
	// Index is the offset of the first requested hash in the base layer.
	Index uint32
	// Length is the number of hashes to include from the base layer.
	Length uint32
	// ProofLayers is the number of ancestor layers to include.
	ProofLayers uint32
}

// MarshalTo writes the 48-byte header into b, which must be large enough.
func (h HashRequestHeader) MarshalTo(b []byte) int {
	copy(b[0:HashSize], h.Root[:])
	binary.BigEndian.PutUint32(b[HashSize:], h.Base)
	binary.BigEndian.PutUint32(b[HashSize+4:], h.Index)
	binary.BigEndian.PutUint32(b[HashSize+8:], h.Length)
	binary.BigEndian.PutUint32(b[HashSize+12:], h.ProofLayers)
	return HashRequestHeaderSize
}

// WriteTo writes the fixed-size header.
func (h HashRequestHeader) WriteTo(w io.Writer) (int64, error) {
	var buf [HashRequestHeaderSize]byte
	h.MarshalTo(buf[:])
	n, err := w.Write(buf[:])
	return int64(n), err
}

// UnmarshalBinary parses the 48-byte header.
func (h *HashRequestHeader) UnmarshalBinary(b []byte) error {
	if len(b) != HashRequestHeaderSize {
		return fmt.Errorf("hash request header has %d bytes, want %d", len(b), HashRequestHeaderSize)
	}
	copy(h.Root[:], b[0:HashSize])
	h.Base = binary.BigEndian.Uint32(b[HashSize:])
	h.Index = binary.BigEndian.Uint32(b[HashSize+4:])
	h.Length = binary.BigEndian.Uint32(b[HashSize+8:])
	h.ProofLayers = binary.BigEndian.Uint32(b[HashSize+12:])
	return nil
}

// HashRequestMessage is a BEP 52 "hash request" message (id 21).
type HashRequestMessage struct {
	HashRequestHeader
}

// ID returns the peer protocol message type.
func (m HashRequestMessage) ID() MessageID { return HashRequest }

// Read copies the fixed header into b.
func (m HashRequestMessage) Read(b []byte) (int, error) {
	if len(b) < HashRequestHeaderSize {
		return 0, io.ErrShortBuffer
	}
	m.HashRequestHeader.MarshalTo(b)
	return HashRequestHeaderSize, io.EOF
}

// HashRejectMessage is a BEP 52 "hash reject" message (id 23): it has the same
// payload as the request it rejects.
type HashRejectMessage struct {
	HashRequestHeader
}

// ID returns the peer protocol message type.
func (m HashRejectMessage) ID() MessageID { return HashReject }

// Read copies the fixed header into b.
func (m HashRejectMessage) Read(b []byte) (int, error) {
	if len(b) < HashRequestHeaderSize {
		return 0, io.ErrShortBuffer
	}
	m.HashRequestHeader.MarshalTo(b)
	return HashRequestHeaderSize, io.EOF
}

// HashesMessage is a BEP 52 "hashes" message (id 22): the request header
// followed by the requested hashes and the proof (uncle) hashes.
type HashesMessage struct {
	HashRequestHeader
	Hashes [][HashSize]byte
}

// ID returns the peer protocol message type.
func (m HashesMessage) ID() MessageID { return Hashes }

// Read copies the whole message into b. WriteTo is preferred for this
// variable-length message; Read is only there to satisfy peerprotocol.Message.
func (m HashesMessage) Read(b []byte) (int, error) {
	if len(b) < HashRequestHeaderSize {
		return 0, io.ErrShortBuffer
	}
	n := m.HashRequestHeader.MarshalTo(b)
	for _, h := range m.Hashes {
		if n+HashSize > len(b) {
			return n, io.ErrShortBuffer
		}
		copy(b[n:], h[:])
		n += HashSize
	}
	return n, io.EOF
}

// WriteTo writes the header, the requested hashes and then the proof hashes.
func (m HashesMessage) WriteTo(w io.Writer) (int64, error) {
	var buf [HashRequestHeaderSize]byte
	m.HashRequestHeader.MarshalTo(buf[:])
	n, err := w.Write(buf[:])
	total := int64(n)
	if err != nil {
		return total, err
	}
	for _, h := range m.Hashes {
		nn, err := w.Write(h[:])
		total += int64(nn)
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
