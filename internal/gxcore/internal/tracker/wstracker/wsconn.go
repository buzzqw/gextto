package wstracker

// wsconn.go is a minimal RFC 6455 WebSocket client for the tracker transport:
// just the client handshake and the text frames a tracker exchange needs. It is
// deliberately bounded (a hard message cap, control frames handled inline) and
// original code, so the same dial path as the HTTP tracker (proxy, resolver,
// blocklist) can be reused instead of the standard library's fixed dialer.

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// maxWSMessage caps one tracker message; a tracker answer is a few hundred
	// bytes even with many peers.
	maxWSMessage = 4 << 20
	// wsGUID is the RFC 6455 handshake magic value.
	wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

// opcodes.
const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

// dialFunc opens a TCP connection to addr, routing through the session egress
// (proxy, resolver, blocklist) exactly like the HTTP tracker transport.
type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// wsConn is one WebSocket connection. Writes are serialized and reads are
// serialized, so it is safe for one reader and one writer at a time (the
// tracker client additionally serializes whole request/response exchanges).
type wsConn struct {
	conn net.Conn
	br   *bufio.Reader

	writeMu sync.Mutex
	readMu  sync.Mutex
}

// dialWebSocket opens and handshakes a ws:// or wss:// connection to rawURL.
func dialWebSocket(ctx context.Context, dial dialFunc, rawURL *url.URL, skipVerify bool, timeout time.Duration, userAgent string) (*wsConn, error) {
	if rawURL.Scheme != "ws" && rawURL.Scheme != "wss" {
		return nil, fmt.Errorf("wstracker: unsupported scheme %q", rawURL.Scheme)
	}
	host := rawURL.Host
	if host == "" {
		return nil, errors.New("wstracker: missing host")
	}
	if _, _, err := net.SplitHostPort(host); err != nil {
		if rawURL.Scheme == "wss" {
			host = net.JoinHostPort(host, "443")
		} else {
			host = net.JoinHostPort(host, "80")
		}
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	raw, err := dial(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	if rawURL.Scheme == "wss" {
		hostname := rawURL.Hostname()
		tlsConn := tls.Client(raw, &tls.Config{ServerName: hostname, InsecureSkipVerify: skipVerify}) //nolint:gosec // operator policy
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, err
		}
		raw = tlsConn
	}
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		raw.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	path := rawURL.RequestURI()
	if path == "" {
		path = "/"
	}
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + rawURL.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Origin: http://" + rawURL.Host + "\r\n" +
		"User-Agent: " + wsUserAgent(userAgent) + "\r\n\r\n"
	if timeout > 0 {
		_ = raw.SetDeadline(time.Now().Add(timeout))
	}
	if _, err := io.WriteString(raw, req); err != nil {
		raw.Close()
		return nil, err
	}
	br := bufio.NewReaderSize(raw, 32*1024)
	httpReq := &http.Request{Method: "GET"}
	resp, err := http.ReadResponse(br, httpReq)
	if err != nil {
		raw.Close()
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		raw.Close()
		return nil, fmt.Errorf("wstracker: handshake failed: %s", resp.Status)
	}
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != acceptKey(key) {
		raw.Close()
		return nil, errors.New("wstracker: bad Sec-WebSocket-Accept")
	}
	_ = raw.SetDeadline(time.Time{})
	return &wsConn{conn: raw, br: br}, nil
}

// acceptKey computes the RFC 6455 expected Sec-WebSocket-Accept.
func acceptKey(key string) string {
	h := sha1.New()
	io.WriteString(h, key)
	io.WriteString(h, wsGUID)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// writeText sends one masked text frame.
func (c *wsConn) writeText(payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeFrame(opText, payload)
}

// writeFrame writes one client frame (always masked, per RFC 6455).
func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	if c.conn == nil {
		return io.ErrClosedPipe
	}
	var header []byte
	b0 := byte(0x80) | opcode // FIN set
	header = append(header, b0)
	n := len(payload)
	switch {
	case n < 126:
		header = append(header, byte(0x80)|byte(n))
	case n <= 0xffff:
		header = append(header, byte(0x80)|126)
		header = binary.BigEndian.AppendUint16(header, uint16(n))
	default:
		header = append(header, byte(0x80)|127)
		header = binary.BigEndian.AppendUint64(header, uint64(n))
	}
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	header = append(header, mask...)
	masked := make([]byte, n)
	for i := 0; i < n; i++ {
		masked[i] = payload[i] ^ mask[i&3]
	}
	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	_, err := c.conn.Write(masked)
	return err
}

// readMessage reads one complete text/binary message, answering pings and a
// close inline. It enforces maxWSMessage.
func (c *wsConn) readMessage() ([]byte, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	var buf []byte
	for {
		fin, opcode, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		switch opcode {
		case opPing:
			if err := c.writeFrame(opPong, payload); err != nil {
				return nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			_ = c.writeFrame(opClose, nil)
			return nil, io.EOF
		case opText, opBinary, opContinuation:
			if len(buf)+len(payload) > maxWSMessage {
				return nil, errors.New("wstracker: message too big")
			}
			buf = append(buf, payload...)
			if fin {
				return buf, nil
			}
		default:
			return nil, fmt.Errorf("wstracker: unsupported opcode %d", opcode)
		}
	}
}

// readFrame reads one frame. Server frames are not masked (a masked one is
// tolerated and unmasked).
func (c *wsConn) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	var h [2]byte
	if _, err = io.ReadFull(c.br, h[:]); err != nil {
		return
	}
	fin = h[0]&0x80 != 0
	opcode = h[0] & 0x0f
	masked := h[1]&0x80 != 0
	length := int64(h[1] & 0x7f)
	switch length {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return
		}
		length = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(c.br, ext[:]); err != nil {
			return
		}
		length = int64(binary.BigEndian.Uint64(ext[:]))
	}
	if length < 0 || length > maxWSMessage {
		err = errors.New("wstracker: frame too big")
		return
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(c.br, mask[:]); err != nil {
			return
		}
	}
	payload = make([]byte, length)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i&3]
		}
	}
	return fin, opcode, payload, nil
}

// Close closes the underlying connection.
func wsUserAgent(value string) string {
	if strings.TrimSpace(value) == "" {
		return "gx-core"
	}
	return value
}

func (c *wsConn) Close() error { return c.conn.Close() }

// SetDeadline bounds one exchange.
func (c *wsConn) SetDeadline(t time.Time) { _ = c.conn.SetDeadline(t) }
