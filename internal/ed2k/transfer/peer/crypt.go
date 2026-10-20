package peer

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rc4"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
)

// eMule's client-to-client obfuscation (the "crypt layer") is an RC4 stream
// whose keys are derived from the receiver's user hash, a magic byte and a
// random value sent by the connecting side. The handshake itself is partly
// clear and partly encrypted; after it every byte is RC4.
//
// It is deliberately separate from secure identification: this protects the
// stream from passive protocol inspection, it does not authenticate files.
const (
	cryptMagicRequester = 34
	cryptMagicServer    = 203
	cryptMagicSync      = 0x835E6FC4
	cryptMethodObfus    = 0
)

const cryptDiscard = 1024

// NewOutgoingObfuscated performs the connecting side of the eMule crypt
// handshake on an already established connection. peerHash is the remote
// client's user hash, which the connecting side must know in advance.
func NewOutgoingObfuscated(ctx context.Context, conn net.Conn, peerHash ed2k.ID) (net.Conn, error) {
	if conn == nil {
		return nil, errors.New("peer: nil connection")
	}
	if peerHash == (ed2k.ID{}) {
		return nil, errors.New("peer: obfuscation needs the remote user hash")
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	}
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	sendKey := cryptKey(peerHash, cryptMagicRequester, random[:])
	recvKey := cryptKey(peerHash, cryptMagicServer, random[:])
	send, err := newCryptCipher(sendKey)
	if err != nil {
		return nil, err
	}
	recv, err := newCryptCipher(recvKey)
	if err != nil {
		return nil, err
	}

	header := make([]byte, 0, 9+16)
	header = append(header, cryptMarker())
	header = append(header, random[:]...)
	clear := len(header)
	header = append(header, cryptMagicBytes()...)
	header = append(header, cryptMethodObfus, cryptMethodObfus)
	pad := randomPad(15)
	header = append(header, pad)
	padding := make([]byte, int(pad))
	if _, err := rand.Read(padding); err != nil {
		return nil, err
	}
	header = append(header, padding...)
	if len(header) > clear {
		send.XORKeyStream(header[clear:], header[clear:])
	}
	if _, err := conn.Write(header); err != nil {
		return nil, fmt.Errorf("peer: obfuscation request: %w", err)
	}

	var response [4]byte
	if _, err := io.ReadFull(conn, response[:]); err != nil {
		return nil, err
	}
	recv.XORKeyStream(response[:], response[:])
	if binaryMagic(response[:]) != cryptMagicSync {
		return nil, errors.New("peer: bad obfuscation magic")
	}
	var method [2]byte
	if _, err := io.ReadFull(conn, method[:]); err != nil {
		return nil, err
	}
	recv.XORKeyStream(method[:], method[:])
	if method[0] != cryptMethodObfus {
		return nil, errors.New("peer: unsupported obfuscation method")
	}
	padLen := int(method[1])
	if padLen > 255 {
		return nil, errors.New("peer: bad obfuscation padding")
	}
	if padLen > 0 {
		discard := make([]byte, padLen)
		if _, err := io.ReadFull(conn, discard); err != nil {
			return nil, err
		}
		recv.XORKeyStream(discard, discard)
	}
	_ = conn.SetDeadline(time.Time{})
	return &cryptConn{Conn: conn, send: send, recv: recv}, nil
}

// AcceptObfuscated inspects the first byte of an incoming client connection.
// A normal eD2k header is returned untouched (with the byte replayed); any
// other first byte starts the receiving side of the crypt handshake.
func AcceptObfuscated(conn net.Conn, ourHash ed2k.ID) (net.Conn, error) {
	if conn == nil {
		return nil, errors.New("peer: nil connection")
	}
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	reader := bufio.NewReaderSize(conn, 32<<10)
	first, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if first == EdonkeyProtocol || first == EmuleProtocol || first == 0xD4 {
		_ = conn.SetDeadline(time.Time{})
		return &bufferedConn{Conn: conn, reader: reader, prefix: []byte{first}}, nil
	}
	if ourHash == (ed2k.ID{}) {
		return nil, errors.New("peer: incoming obfuscation needs our user hash")
	}
	var random [4]byte
	if _, err := io.ReadFull(reader, random[:]); err != nil {
		return nil, err
	}
	recvKey := cryptKey(ourHash, cryptMagicRequester, random[:])
	sendKey := cryptKey(ourHash, cryptMagicServer, random[:])
	recv, err := newCryptCipher(recvKey)
	if err != nil {
		return nil, err
	}
	send, err := newCryptCipher(sendKey)
	if err != nil {
		return nil, err
	}

	var request [4]byte
	if _, err := io.ReadFull(reader, request[:]); err != nil {
		return nil, err
	}
	recv.XORKeyStream(request[:], request[:])
	if binaryMagic(request[:]) != cryptMagicSync {
		return nil, errors.New("peer: bad incoming obfuscation magic")
	}
	var method [3]byte
	if _, err := io.ReadFull(reader, method[:]); err != nil {
		return nil, err
	}
	recv.XORKeyStream(method[:], method[:])
	if method[1] != cryptMethodObfus {
		return nil, errors.New("peer: unsupported incoming obfuscation method")
	}
	padLen := int(method[2])
	if padLen > 0 {
		padding := make([]byte, padLen)
		if _, err := io.ReadFull(reader, padding); err != nil {
			return nil, err
		}
		recv.XORKeyStream(padding, padding)
	}

	responsePad := randomPad(15)
	response := append(cryptMagicBytes(), cryptMethodObfus, responsePad)
	padding := make([]byte, int(responsePad))
	if _, err := rand.Read(padding); err != nil {
		return nil, err
	}
	response = append(response, padding...)
	send.XORKeyStream(response, response)
	if _, err := conn.Write(response); err != nil {
		return nil, fmt.Errorf("peer: obfuscation answer: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	base := &bufferedConn{Conn: conn, reader: reader}
	return &cryptConn{Conn: base, send: send, recv: recv}, nil
}

func cryptKey(hash ed2k.ID, magic byte, random []byte) []byte {
	buffer := make([]byte, 0, 21)
	buffer = append(buffer, hash[:]...)
	buffer = append(buffer, magic)
	buffer = append(buffer, random...)
	sum := md5.Sum(buffer)
	return sum[:]
}

func newCryptCipher(key []byte) (*rc4.Cipher, error) {
	cipher, err := rc4.NewCipher(key)
	if err != nil {
		return nil, err
	}
	discard := make([]byte, cryptDiscard)
	cipher.XORKeyStream(discard, discard)
	return cipher, nil
}

func cryptMagicBytes() []byte {
	return []byte{
		byte(cryptMagicSync & 0xFF), byte((cryptMagicSync >> 8) & 0xFF),
		byte((cryptMagicSync >> 16) & 0xFF), byte((cryptMagicSync >> 24) & 0xFF),
	}
}

func binaryMagic(raw []byte) uint32 {
	return uint32(raw[0]) | uint32(raw[1])<<8 | uint32(raw[2])<<16 | uint32(raw[3])<<24
}

func cryptMarker() byte {
	var value [1]byte
	_, _ = rand.Read(value[:])
	switch value[0] {
	case EdonkeyProtocol, EmuleProtocol, 0xD4:
		return 0x02
	}
	return value[0]
}

func randomPad(max int) byte {
	var value [1]byte
	_, _ = rand.Read(value[:])
	return value[0] % byte(max+1)
}

type cryptConn struct {
	net.Conn
	send *rc4.Cipher
	recv *rc4.Cipher
}

func (c *cryptConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.recv.XORKeyStream(p[:n], p[:n])
	}
	return n, err
}

func (c *cryptConn) Write(p []byte) (int, error) {
	buffer := make([]byte, len(p))
	c.send.XORKeyStream(buffer, p)
	written := 0
	for written < len(buffer) {
		n, err := c.Conn.Write(buffer[written:])
		written += n
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
	prefix []byte
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	if len(c.prefix) > 0 {
		n := copy(p, c.prefix)
		c.prefix = c.prefix[n:]
		return n, nil
	}
	return c.reader.Read(p)
}
