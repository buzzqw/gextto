package btconn

import (
	"bytes"
	"io"
	"net"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/mse"
)

// Accept completes the BitTorrent handshake on an incoming connection,
// transparently handling plain and MSE-encrypted peers. It returns a
// connection ready for protocol messages.
func Accept(
	conn net.Conn,
	handshakeTimeout time.Duration,
	getSKey func(sKeyHash [20]byte) (sKey []byte),
	forceEncryption bool,
	hasInfoHash func([20]byte) bool,
	ourExtensions [8]byte, ourID [20]byte) (
	encConn net.Conn, cipher mse.CryptoMethod, peerExtensions [8]byte, peerID [20]byte, infoHash [20]byte, err error) {
	lookup := func(ih [20]byte) ([20]byte, [8]byte, bool) { return ourID, ourExtensions, hasInfoHash(ih) }
	return AcceptRouted(conn, handshakeTimeout, getSKey, forceEncryption, lookup)
}

// AcceptRouted is Accept for a listener shared by many torrents: the info
// hash offered by the peer selects the torrent, and lookup supplies the peer
// id and reserved bits to answer with for that torrent (ok=false drops the
// peer). Extensions stay per-torrent because the v2 reserved bit may only be
// advertised by a torrent that actually holds a v2 identity.
func AcceptRouted(
	conn net.Conn,
	handshakeTimeout time.Duration,
	getSKey func(sKeyHash [20]byte) (sKey []byte),
	forceEncryption bool,
	lookup func(infoHash [20]byte) (ourID [20]byte, ourExtensions [8]byte, ok bool)) (
	encConn net.Conn, cipher mse.CryptoMethod, peerExtensions [8]byte, peerID [20]byte, infoHash [20]byte, err error) {
	log := logger.New("conn <- " + conn.RemoteAddr().String())

	if forceEncryption && getSKey == nil {
		panic("forceEncryption && getSKey == nil")
	}
	if err = conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return
	}

	encrypted := false

	// Plain handshake first; on a protocol mismatch with a key source
	// available, rewind the bytes already read and try MSE instead.
	var seen bytes.Buffer
	tee := io.TeeReader(conn, &seen)

	peerExtensions, infoHash, err = readHandshake1(tee)
	if err == errInvalidProtocol && getSKey != nil {
		conn, cipher, encrypted, err = upgradeToMSE(conn, &seen, getSKey, forceEncryption, log)
		if err != nil {
			return
		}
		peerExtensions, infoHash, err = readHandshake1(conn)
	}
	if err != nil {
		return
	}
	if forceEncryption && !encrypted {
		err = errNotEncrypted
		return
	}

	var ourID [20]byte
	var ourExtensions [8]byte
	var ok bool
	if ourID, ourExtensions, ok = lookup(infoHash); !ok {
		err = errInvalidInfoHash
		return
	}
	if err = writeHandshake(conn, infoHash, ourID, ourExtensions); err != nil {
		return
	}
	if peerID, err = readHandshake2(conn); err != nil {
		return
	}
	if peerID == ourID {
		err = errOwnConnection
		return
	}
	encConn = conn
	return
}

// upgradeToMSE replays the buffered bytes into an MSE handshake and returns
// the encrypted connection with the negotiated cipher.
func upgradeToMSE(conn net.Conn, seen *bytes.Buffer, getSKey func([20]byte) []byte, forceEncryption bool, log logger.Logger) (net.Conn, mse.CryptoMethod, bool, error) {
	replayed := &rwConn{readWriter{io.MultiReader(seen, conn), conn}, conn}
	mseConn := mse.WrapConn(replayed)
	var cipher mse.CryptoMethod
	encrypted := false
	err := mseConn.HandshakeIncoming(
		getSKey,
		func(provided mse.CryptoMethod) (selected mse.CryptoMethod) {
			// RC4 when offered; plaintext only when not forced.
			if provided&mse.RC4 != 0 {
				selected = mse.RC4
				encrypted = true
			} else if (provided&mse.PlainText != 0) && !forceEncryption {
				selected = mse.PlainText
			}
			cipher = selected
			return
		})
	if err != nil {
		return nil, cipher, false, err
	}
	log.Debugf("Encryption handshake is successful. Selected cipher: %s", cipher)
	return mseConn, cipher, encrypted, nil
}
