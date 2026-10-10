package btconn

import (
	"bytes"
	"io"
	"net"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/mse"
)

// Accept BitTorrent handshake from the connection. Handles encryption.
// Returns a new connection that is ready for sending/receiving BitTorrent protocol messages.
func Accept(
	conn net.Conn,
	handshakeTimeout time.Duration,
	getSKey func(sKeyHash [20]byte) (sKey []byte),
	forceEncryption bool,
	hasInfoHash func([20]byte) bool,
	ourExtensions [8]byte, ourID [20]byte) (
	encConn net.Conn, cipher mse.CryptoMethod, peerExtensions [8]byte, peerID [20]byte, infoHash [20]byte, err error) {
	lookup := func(ih [20]byte) ([20]byte, bool) { return ourID, hasInfoHash(ih) }
	return AcceptRouted(conn, handshakeTimeout, getSKey, forceEncryption, lookup, ourExtensions)
}

// AcceptRouted is Accept for a listener shared by many torrents (gextto
// fork): the info hash sent by the peer selects the torrent, and lookup
// returns the peer ID to answer with (ok=false rejects the connection).
func AcceptRouted(
	conn net.Conn,
	handshakeTimeout time.Duration,
	getSKey func(sKeyHash [20]byte) (sKey []byte),
	forceEncryption bool,
	lookup func(infoHash [20]byte) (ourID [20]byte, ok bool),
	ourExtensions [8]byte) (
	encConn net.Conn, cipher mse.CryptoMethod, peerExtensions [8]byte, peerID [20]byte, infoHash [20]byte, err error) {
	log := logger.New("conn <- " + conn.RemoteAddr().String())

	if forceEncryption && getSKey == nil {
		panic("forceEncryption && getSKey == nil")
	}

	if err = conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return
	}

	isEncrypted := false

	// Try to do unencrypted handshake first.
	// If protocol string is not valid, try to do encrypted handshake.
	// rwConn returns the read bytes again that is read by handshake.Read1.
	var (
		buf    bytes.Buffer
		reader = io.TeeReader(conn, &buf)
	)

	peerExtensions, infoHash, err = readHandshake1(reader)
	if err == errInvalidProtocol && getSKey != nil {
		conn = &rwConn{readWriter{io.MultiReader(&buf, conn), conn}, conn}
		mseConn := mse.WrapConn(conn)
		err = mseConn.HandshakeIncoming(
			getSKey,
			func(provided mse.CryptoMethod) (selected mse.CryptoMethod) {
				if provided&mse.RC4 != 0 {
					selected = mse.RC4
					isEncrypted = true
				} else if (provided&mse.PlainText != 0) && !forceEncryption {
					selected = mse.PlainText
				}
				cipher = selected
				return
			})
		if err != nil {
			return
		}
		log.Debugf("Encryption handshake is successful. Selected cipher: %s", cipher)
		conn = mseConn
		peerExtensions, infoHash, err = readHandshake1(conn)
	}
	if err != nil {
		return
	}

	if forceEncryption && !isEncrypted {
		err = errNotEncrypted
		return
	}

	ourID, ok := lookup(infoHash)
	if !ok {
		err = errInvalidInfoHash
		return
	}
	err = writeHandshake(conn, infoHash, ourID, ourExtensions)
	if err != nil {
		return
	}
	peerID, err = readHandshake2(conn)
	if err != nil {
		return
	}
	if peerID == ourID {
		err = errOwnConnection
		return
	}
	encConn = conn
	return
}
