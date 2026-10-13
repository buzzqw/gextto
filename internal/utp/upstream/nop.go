package upstream

// GEXTTO: rewritten in-tree. packetConnNopCloser wraps a net.PacketConn so a
// Socket can use it without owning its Close (see
// NewSocketFromPacketConnNoClose).

import "net"

type packetConnNopCloser struct {
	net.PacketConn
}

// Close does nothing: the wrapped connection outlives the Socket.
func (packetConnNopCloser) Close() error { return nil }
