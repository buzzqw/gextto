package udptracker

import (
	"net"
	"time"
)

// connection tracks one UDP tracker session: the initial connect handshake
// plus the announce/scrape requests queued behind it. Once the transport's
// run loop completes the connect, addr/id/connectedAt are filled in and the
// queued requests go out with the negotiated connection id.
type connection struct {
	*requestBase
	*connectRequest

	// requests waits for the connect to succeed before being sent.
	requests []udpRequest

	// Set by the Transport run loop on a successful connect.
	addr        *net.UDPAddr
	id          int64
	connectedAt time.Time
}

var _ udpRequest = (*connection)(nil)

// newConnection starts a session for req, issuing the connect first.
func newConnection(req udpRequest) *connection {
	return &connection{
		requestBase:    newRequestBase(req.GetContext(), req.GetDest()),
		connectRequest: newConnectRequest(),
		requests:       []udpRequest{req},
	}
}
