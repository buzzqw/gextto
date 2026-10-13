package btconn

import (
	"errors"
	"io"
	"net"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
)

// Sentinel handshake failures: the peer is wrong, not us.
var (
	errInvalidInfoHash = &HandshakeError{"invalid info hash"}
	errOwnConnection   = &HandshakeError{"dropped own connection"}
	errNotEncrypted    = &HandshakeError{"connection is not encrypted"}
	errInvalidProtocol = &HandshakeError{"invalid protocol"}
)

// HandshakeError describes a failure of the BitTorrent handshake phase.
type HandshakeError struct {
	message string
}

func (e *HandshakeError) Error() string {
	return e.message
}

// LogHandshakeError reports the routine, expected handshake failures
// (peer hangups, network errors, protocol violations) at debug level and
// returns true when it handled err. A false return means the failure is
// surprising: the caller must log it itself, at a level of its choice.
func LogHandshakeError(log logger.Logger, err error) bool {
	var opErr *net.OpError
	var hsErr *HandshakeError
	switch {
	case errors.Is(err, io.EOF):
		log.Debug("peer has closed the connection: EOF")
	case errors.Is(err, io.ErrUnexpectedEOF):
		log.Debug("peer has closed the connection: Unexpected EOF")
	case errors.As(err, &opErr):
		log.Debugln("net operation error:", err)
	case errors.As(err, &hsErr):
		log.Debugln("protocol error:", err)
	default:
		return false
	}
	return true
}
