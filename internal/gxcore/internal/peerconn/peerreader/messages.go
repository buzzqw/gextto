package peerreader

import (
	"github.com/buzzqw/gextto/internal/gxcore/internal/bufferpool"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
)

// Piece is a block payload read from a peer. The data travels in a pooled
// buffer that the consumer must Release after writing it to storage.
type Piece struct {
	peerprotocol.PieceMessage
	Buffer bufferpool.Buffer
}
