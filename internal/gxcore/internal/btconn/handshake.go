package btconn

import (
	"encoding/binary"
	"io"
)

// pstr is the fixed 20-byte protocol identifier every handshake starts with:
// length prefix 19 followed by "BitTorrent protocol".
var pstr = [20]byte{19, 'B', 'i', 't', 'T', 'o', 'r', 'r', 'e', 'n', 't', ' ', 'p', 'r', 'o', 't', 'o', 'c', 'o', 'l'}

// handshake is the 68-byte wire layout both sides exchange.
type handshake struct {
	Pstr       [20]byte
	Extensions [8]byte
	InfoHash   [20]byte
	PeerID     [20]byte
}

// writeHandshake sends our handshake in a single binary write.
func writeHandshake(w io.Writer, ih [20]byte, id [20]byte, extensions [8]byte) error {
	return binary.Write(w, binary.BigEndian, handshake{
		Pstr:       pstr,
		Extensions: extensions,
		InfoHash:   ih,
		PeerID:     id,
	})
}

// readHandshake1 consumes the peer's protocol string, extension bits and
// info hash. The first 20 bytes read into ih are the pstr candidate, then
// reused for the actual info hash.
func readHandshake1(r io.Reader) (extensions [8]byte, ih [20]byte, err error) {
	if _, err = io.ReadFull(r, ih[:]); err != nil {
		return
	}
	if ih != pstr {
		err = errInvalidProtocol
		return
	}
	if _, err = io.ReadFull(r, extensions[:]); err != nil {
		return
	}
	_, err = io.ReadFull(r, ih[:])
	return
}

// readHandshake2 consumes the peer's 20-byte peer id.
func readHandshake2(r io.Reader) (id [20]byte, err error) {
	_, err = io.ReadFull(r, id[:])
	return
}
