package btconn

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"syscall"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/ctxutil"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/mse"
	"github.com/buzzqw/gextto/internal/gxcore/internal/netx"
)

// Dial connects to addr and completes the BitTorrent handshake, handling
// encryption per the local policy: encrypted first when enabled (RC4, plus
// plaintext unless forced), with one plaintext redial when the encrypted
// attempt fails and encryption is optional. The returned conn is ready for
// peer messages; on error the socket is already closed.
//
// If the race picked TCP and the peer reset it before the handshake (a
// uTP-only peer accepts the TCP leg and resets it), Dial retries once over
// uTP alone instead of losing the peer until the next announce.
func Dial(
	addr net.Addr,
	dialTimeout, handshakeTimeout time.Duration,
	enableEncryption,
	forceEncryption bool,
	ourExtensions [8]byte,
	ih [20]byte,
	ourID [20]byte,
	stopC chan struct{}) (
	conn net.Conn, cipher mse.CryptoMethod, peerExtensions [8]byte, peerID [20]byte, err error) {
	log := logger.New("conn -> " + addr.String())
	ctx, cancel := ctxutil.FromChan(stopC)
	defer cancel()
	dialer := net.Dialer{Timeout: dialTimeout}

	conn, cipher, peerExtensions, peerID, usedUTP, err := dialAttempt(
		ctx, &dialer, addr, handshakeTimeout, enableEncryption, forceEncryption,
		ourExtensions, ih, ourID, stopC, log, false)
	if err == nil || stopped(stopC) || usedUTP || !retryableDialError(err) || !netx.UTPAvailable() {
		return
	}
	log.Debugln("TCP connection failed before the handshake, retrying over uTP:", err)
	firstErr := err
	conn, cipher, peerExtensions, peerID, _, err = dialAttempt(
		ctx, &dialer, addr, handshakeTimeout, enableEncryption, forceEncryption,
		ourExtensions, ih, ourID, stopC, log, true)
	if err != nil {
		err = firstErr
	}
	return
}

// dialAttempt is one full connection attempt: dial, optional encryption, and
// the BitTorrent handshake. usedUTP reports which transport the dial picked,
// so Dial can decide whether a uTP retry makes sense after a failure.
func dialAttempt(
	ctx context.Context,
	dialer *net.Dialer,
	addr net.Addr,
	handshakeTimeout time.Duration,
	enableEncryption, forceEncryption bool,
	ourExtensions [8]byte,
	ih [20]byte,
	ourID [20]byte,
	stopC chan struct{},
	log logger.Logger,
	forceUTP bool) (
	conn net.Conn, cipher mse.CryptoMethod, peerExtensions [8]byte, peerID [20]byte, usedUTP bool, err error) {
	done := make(chan struct{})
	defer close(done)

	// The handshake bytes are buffered up front: the encrypted and the plain
	// path send the same 68 bytes.
	out := bytes.NewBuffer(make([]byte, 0, 68))
	if err = writeHandshake(out, ih, ourID, ourExtensions); err != nil {
		return
	}

	conn, err = dialGuarded(ctx, dialer, addr, handshakeTimeout, stopC, done, log, forceUTP)
	if err != nil {
		return
	}
	usedUTP = netx.IsUTP(conn)
	// Every error below closes the current conn; a redial closes the old one
	// explicitly before replacing it (a failed redial leaves conn nil, which
	// the guard must not close).
	defer func() {
		if err != nil && conn != nil {
			conn.Close()
		}
	}()

	if enableEncryption {
		var encConn net.Conn
		if encConn, cipher, err = tryEncrypted(conn, ih[:], out.Bytes(), forceEncryption, log); err != nil {
			if stopped(stopC) {
				return
			}
			log.Debugln("Encryption handshake has failed:", err)
			if forceEncryption {
				log.Debug("Will not try again because outgoing encryption is forced.")
				err = errNotEncrypted
				return
			}
			conn.Close()
			log.Debug("Connecting again without encryption...")
			if conn, err = dialGuarded(ctx, dialer, addr, handshakeTimeout, stopC, done, log, forceUTP); err != nil {
				return
			}
			usedUTP = netx.IsUTP(conn)
			if _, err = conn.Write(out.Bytes()); err != nil {
				return
			}
		} else {
			log.Debugf("Encryption handshake is successful. Selected cipher: %s", cipher)
			conn = encConn
		}
	} else if _, err = conn.Write(out.Bytes()); err != nil {
		return
	}

	var ihRead [20]byte
	if peerExtensions, ihRead, err = readHandshake1(conn); err != nil {
		return
	}
	if ihRead != ih {
		err = errInvalidInfoHash
		return
	}
	if peerID, err = readHandshake2(conn); err != nil {
		return
	}
	if peerID == ourID {
		err = errOwnConnection
		return
	}
	return
}

// retryableDialError reports whether a failed attempt may succeed over uTP.
// Only a connection that the peer accepted and then dropped before the
// handshake is retried: a protocol mismatch or a timeout means the race
// already tried uTP, so another dial would only repeat the wait.
func retryableDialError(err error) bool {
	if errors.Is(err, errOwnConnection) ||
		errors.Is(err, errInvalidInfoHash) ||
		errors.Is(err, errInvalidProtocol) ||
		errors.Is(err, errNotEncrypted) {
		return false
	}
	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNABORTED)
}

// dialGuarded dials, arms the handshake deadline, and ties the socket's
// lifetime to stopC (closed on abort) versus done (normal return). With
// forceUTP the dial uses uTP alone instead of racing TCP.
func dialGuarded(ctx context.Context, dialer *net.Dialer, addr net.Addr, handshakeTimeout time.Duration, stopC, done chan struct{}, log logger.Logger, forceUTP bool) (net.Conn, error) {
	log.Debug("Connecting to peer...")
	var (
		conn net.Conn
		err  error
	)
	if forceUTP {
		conn, err = netx.DialPeerUTP(ctx, *dialer, addr.String())
	} else {
		conn, err = netx.DialPeer(ctx, *dialer, addr.Network(), addr.String())
	}
	if err != nil {
		return nil, err
	}
	log.Debug("Connected")
	go func() {
		select {
		case <-stopC:
			conn.Close()
		case <-done:
		}
	}()
	if err := conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// tryEncrypted runs the MSE handshake over conn: RC4 when offered, plaintext
// only when not forced. On failure the raw conn is returned for the caller
// to close or reuse.
func tryEncrypted(conn net.Conn, sKeyHash, handshake []byte, forceEncryption bool, log logger.Logger) (net.Conn, mse.CryptoMethod, error) {
	sKey := make([]byte, 20)
	copy(sKey, sKeyHash)
	provide := mse.RC4
	if !forceEncryption {
		provide |= mse.PlainText
	}
	encConn := mse.WrapConn(conn)
	cipher, err := encConn.HandshakeOutgoing(sKey, provide, handshake)
	if err != nil {
		return conn, cipher, err
	}
	return encConn, cipher, nil
}

// stopped reports a stopC abort, when the caller must return as-is.
func stopped(stopC chan struct{}) bool {
	select {
	case <-stopC:
		return true
	default:
		return false
	}
}
