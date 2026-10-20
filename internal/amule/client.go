package amule

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// ErrAuth is returned when amuled refuses the password or the protocol.
var ErrAuth = errors.New("amule: authentication failed")

// Config is how to reach amuled's External Connections port.
type Config struct {
	// Address is host:port of the EC listener (amuled default port 4712).
	Address string
	// Password is the plain EC password; it is hashed before it is sent.
	Password string
	// ClientName and ClientVersion identify us in the core's log.
	ClientName    string
	ClientVersion string
	// Timeout bounds dialing and every request/response round trip.
	Timeout time.Duration
}

// Client is one authenticated EC session. Requests are serialised: the
// protocol is strictly request/response on a single connection. A broken
// connection is reopened on the next request.
type Client struct {
	cfg Config

	mu            sync.Mutex
	conn          net.Conn
	reader        *bufio.Reader
	serverVersion string
}

// New returns a client; the connection is opened on first use.
func New(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.ClientName == "" {
		cfg.ClientName = "Gextto"
	}
	if cfg.ClientVersion == "" {
		cfg.ClientVersion = "1"
	}
	return &Client{cfg: cfg}
}

// Close drops the connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeLocked()
}

func (c *Client) closeLocked() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	c.reader = nil
	return err
}

// ServerVersion is the aMule version the core reported at login.
func (c *Client) ServerVersion() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.serverVersion
}

// PasswordResponse computes the login answer the core expects:
// MD5(hex(MD5(password)) + hex(MD5(uppercase hex of the salt))).
func PasswordResponse(password string, salt uint64) [16]byte {
	passwordHash := md5.Sum([]byte(password))
	saltHash := md5.Sum([]byte(fmt.Sprintf("%X", salt)))
	return md5.Sum([]byte(hex.EncodeToString(passwordHash[:]) + hex.EncodeToString(saltHash[:])))
}

func (c *Client) connectLocked(ctx context.Context) error {
	if c.conn != nil {
		return nil
	}
	if strings.TrimSpace(c.cfg.Password) == "" {
		return fmt.Errorf("%w: the EC password is empty", ErrAuth)
	}
	dialer := net.Dialer{Timeout: c.cfg.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", c.cfg.Address)
	if err != nil {
		return fmt.Errorf("amule: connect %s: %w", c.cfg.Address, err)
	}
	c.conn = conn
	c.reader = bufio.NewReader(conn)

	login := Packet{Op: OpAuthReq, Tags: []Tag{
		StringTag(TagClientName, c.cfg.ClientName),
		StringTag(TagClientVersion, c.cfg.ClientVersion),
		IntTag(TagProtocolVersion, ProtocolVersion),
	}}
	reply, err := c.roundTripLocked(ctx, login)
	if err != nil {
		_ = c.closeLocked()
		return err
	}
	if reply.Op != OpAuthSalt {
		_ = c.closeLocked()
		return authError(reply)
	}
	saltTag, ok := reply.Tag(TagPasswdSalt)
	if !ok {
		_ = c.closeLocked()
		return fmt.Errorf("%w: no salt in the answer", ErrAuth)
	}
	response := PasswordResponse(c.cfg.Password, saltTag.Int())
	reply, err = c.roundTripLocked(ctx, Packet{Op: OpAuthPasswd, Tags: []Tag{HashTag(TagPasswdHash, response)}})
	if err != nil {
		_ = c.closeLocked()
		return err
	}
	if reply.Op != OpAuthOK {
		_ = c.closeLocked()
		return authError(reply)
	}
	if version, ok := reply.Tag(TagServerVer); ok {
		c.serverVersion = version.String()
	}
	return nil
}

func authError(reply Packet) error {
	if message := reply.Message(); message != "" {
		return fmt.Errorf("%w: %s", ErrAuth, message)
	}
	return fmt.Errorf("%w (opcode %#x)", ErrAuth, reply.Op)
}

func (c *Client) roundTripLocked(ctx context.Context, request Packet) (Packet, error) {
	raw, err := request.Encode()
	if err != nil {
		return Packet{}, err
	}
	deadline := time.Now().Add(c.cfg.Timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = c.conn.SetDeadline(deadline)
	if _, err := c.conn.Write(raw); err != nil {
		return Packet{}, fmt.Errorf("amule: send: %w", err)
	}
	reply, err := ReadPacket(c.reader)
	if err != nil {
		return Packet{}, fmt.Errorf("amule: receive: %w", err)
	}
	return reply, nil
}

// Request sends one packet and returns the answer. A transport failure closes
// the connection and is retried once on a fresh login, so a restarted amuled
// does not surface as an error to the caller.
func (c *Client) Request(ctx context.Context, request Packet) (Packet, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; ; attempt++ {
		if err := c.connectLocked(ctx); err != nil {
			return Packet{}, err
		}
		reply, err := c.roundTripLocked(ctx, request)
		if err == nil {
			return reply, nil
		}
		_ = c.closeLocked()
		if attempt > 0 || ctx.Err() != nil {
			return Packet{}, err
		}
	}
}

// requestOK sends a command and maps EC_OP_FAILED to an error.
func (c *Client) requestOK(ctx context.Context, request Packet) (Packet, error) {
	reply, err := c.Request(ctx, request)
	if err != nil {
		return Packet{}, err
	}
	if reply.Op == OpFailed {
		if message := reply.Message(); message != "" {
			return reply, fmt.Errorf("amule: %s", message)
		}
		return reply, errors.New("amule: the core refused the request")
	}
	return reply, nil
}

// Ping verifies the session with a cheap request.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Request(ctx, Packet{Op: OpGetConnState})
	return err
}
