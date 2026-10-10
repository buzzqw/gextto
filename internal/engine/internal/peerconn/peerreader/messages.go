package peerreader

import (
	"github.com/buzzqw/gextto/internal/engine/internal/bufferpool"
	"github.com/buzzqw/gextto/internal/engine/internal/peerprotocol"
)

// Piece message that is read from peers.
// Data of the piece is wrapped with a bufferpool.Buffer object.
type Piece struct {
	peerprotocol.PieceMessage
	Buffer bufferpool.Buffer
}
