package peerwriter

import (
	"encoding/binary"
	"io"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
)

// Piece is one block payload queued for upload. On the wire it is the
// 8-byte (index, begin) header followed by the block bytes, read straight
// from storage at send time so large pieces never sit fully in memory.
type Piece struct {
	Data io.ReaderAt
	peerprotocol.RequestMessage
}

// ID returns the BitTorrent protocol message ID.
func (p Piece) ID() peerprotocol.MessageID { return peerprotocol.Piece }

// Read serializes the header and the block into b (which must hold
// 8+Length bytes) and reports io.EOF on a full block, like every other
// outgoing message.
func (p Piece) Read(b []byte) (int, error) {
	binary.BigEndian.PutUint32(b[0:4], p.Index)
	binary.BigEndian.PutUint32(b[4:8], p.Begin)
	n, err := p.Data.ReadAt(b[8:8+p.Length], int64(p.Begin))
	if err != nil {
		return n + 8, err
	}
	return n + 8, io.EOF
}
