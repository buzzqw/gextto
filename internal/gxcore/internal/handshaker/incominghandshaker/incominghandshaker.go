package incominghandshaker

import (
	"net"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/btconn"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/mse"
)

// IncomingHandshaker completes the BitTorrent handshake on an accepted
// connection: it verifies the info hash belongs to a live torrent and routes
// the peer to it. Close aborts a handshake in flight (and its connection).
type IncomingHandshaker struct {
	Conn       net.Conn
	PeerID     [20]byte
	Extensions [8]byte
	Cipher     mse.CryptoMethod
	Error      error

	closeC chan struct{}
	doneC  chan struct{}
}

// New prepares a handshaker for an accepted conn.
func New(conn net.Conn) *IncomingHandshaker {
	return &IncomingHandshaker{
		Conn:   conn,
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

// Close aborts the handshake, closing the connection when one is ongoing,
// and waits for Run to return.
func (h *IncomingHandshaker) Close() {
	close(h.closeC)
	<-h.doneC
}

// Run handshakes and always reports h on resultC — with Error set on
// failure — unless Close wins the race, in which case the connection is
// closed instead of being handed over.
func (h *IncomingHandshaker) Run(peerID [20]byte, getSKeyFunc func([20]byte) []byte, checkInfoHashFunc func([20]byte) bool, resultC chan *IncomingHandshaker, timeout time.Duration, ourExtensions [8]byte, forceIncomingEncryption bool) {
	defer close(h.doneC)
	defer func() {
		select {
		case resultC <- h:
		case <-h.closeC:
			h.Conn.Close()
		}
	}()

	log := logger.New("conn <- " + h.Conn.RemoteAddr().String())

	conn, cipher, peerExtensions, remoteID, _, err := btconn.Accept(
		h.Conn, timeout, getSKeyFunc, forceIncomingEncryption, checkInfoHashFunc, ourExtensions, peerID)
	if err != nil {
		if !btconn.LogHandshakeError(log, err) {
			log.Debugln("cannot complete incoming handshake:", err)
		}
		h.Error = err
		return
	}
	log.Debugf("Connection accepted. (cipher=%s extensions=%x client=%q)", cipher, peerExtensions, remoteID[:8])

	h.Conn = conn
	h.PeerID = remoteID
	h.Extensions = peerExtensions
	h.Cipher = cipher
}
