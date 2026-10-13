package outgoinghandshaker

import (
	"net"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/btconn"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/mse"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peersource"
)

// OutgoingHandshaker dials a peer address and completes the BitTorrent
// handshake on it. The negotiated connection (or the failure) is delivered
// on resultC; Close aborts a handshake in flight.
type OutgoingHandshaker struct {
	Addr       *net.TCPAddr
	Source     peersource.Source
	Conn       net.Conn
	PeerID     [20]byte
	Extensions [8]byte
	Cipher     mse.CryptoMethod
	Error      error

	closeC chan struct{}
	doneC  chan struct{}
}

// New prepares a handshaker for addr discovered via source.
func New(addr *net.TCPAddr, source peersource.Source) *OutgoingHandshaker {
	return &OutgoingHandshaker{
		Addr:   addr,
		Source: source,
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

// Close aborts the handshake and waits for Run to return.
func (h *OutgoingHandshaker) Close() {
	close(h.closeC)
	<-h.doneC
}

// Run dials and handshakes, then reports h on resultC. On failure h.Error
// describes the cause and Conn stays nil; when Close wins the race the
// freshly dialed conn is closed instead of leaking.
func (h *OutgoingHandshaker) Run(dialTimeout, handshakeTimeout time.Duration, peerID, infoHash [20]byte, resultC chan *OutgoingHandshaker, ourExtensions [8]byte, disableOutgoingEncryption, forceOutgoingEncryption bool) {
	defer close(h.doneC)
	log := logger.New("peer -> " + h.Addr.String())

	conn, cipher, peerExtensions, remoteID, err := btconn.Dial(h.Addr, dialTimeout, handshakeTimeout, !disableOutgoingEncryption, forceOutgoingEncryption, ourExtensions, infoHash, peerID, h.closeC)
	if err != nil {
		if !btconn.LogHandshakeError(log, err) {
			log.Errorln("cannot complete outgoing handshake:", err)
		}
		h.Error = err
		select {
		case resultC <- h:
		case <-h.closeC:
		}
		return
	}
	log.Debugf("Connected to peer. (cipher=%s extensions=%x client=%q)", cipher, peerExtensions, remoteID[:8])

	h.Conn = conn
	h.PeerID = remoteID
	h.Extensions = peerExtensions
	h.Cipher = cipher

	select {
	case resultC <- h:
	case <-h.closeC:
		conn.Close()
	}
}
