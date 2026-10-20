package ed2k

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"time"
)

// offerChunk bounds the files in one OP_OFFERFILES packet.
const offerChunk = 40

// Publisher keeps an eD2k server session open and offers the shared files, so
// other clients receive us as a source. Servers keep an offer only for the
// lifetime of the connection: a reconnect offers everything again.
type Publisher struct {
	client *Client
	files  func() []SharedFile
	logf   func(string, ...any)

	mu        sync.Mutex
	server    string
	offered   int
	last      time.Time
	lastError string
}

// NewPublisher returns a publisher for the files snapshot function.
func NewPublisher(client *Client, files func() []SharedFile) *Publisher {
	return &Publisher{client: client, files: files}
}

// SetLogf sets the optional logger.
func (p *Publisher) SetLogf(logf func(string, ...any)) { p.logf = logf }

func (p *Publisher) log(format string, args ...any) {
	if p.logf != nil {
		p.logf(format, args...)
	}
}

// Status reports the current server, offered files and last error.
func (p *Publisher) Status() (string, int, time.Time, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.server, p.offered, p.last, p.lastError
}

// Run publishes until the context is cancelled, reconnecting with backoff.
func (p *Publisher) Run(ctx context.Context) {
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		files := p.files()
		if len(files) == 0 {
			if !sleepContext(ctx, 30*time.Second) {
				return
			}
			continue
		}
		err := p.session(ctx, files)
		if ctx.Err() != nil {
			return
		}
		p.setError(err)
		if err != nil {
			p.log("ed2k: publish: %v", err)
		}
		if !sleepContext(ctx, backoff) {
			return
		}
		if backoff < time.Minute {
			backoff *= 2
		}
	}
}

func (p *Publisher) session(ctx context.Context, files []SharedFile) error {
	servers := p.client.loadServers(ctx)
	if len(servers) == 0 {
		return errors.New("ed2k: publish: no eD2k servers configured")
	}
	if len(servers) > 5 {
		servers = servers[:5]
	}
	var lastErr error
	for _, address := range servers {
		if p.client.serverBlocked(ctx, address) {
			continue
		}
		conn, err := p.dial(ctx, address)
		if err != nil {
			lastErr = err
			continue
		}
		err = p.serve(ctx, conn, files)
		_ = conn.Close()
		if err != nil {
			lastErr = err
			continue
		}
	}
	if lastErr == nil {
		lastErr = errors.New("ed2k: publish: no server accepted the connection")
	}
	return lastErr
}

func (p *Publisher) dial(ctx context.Context, address string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", address, err)
	}
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if err := p.client.serverLogin(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%s: login: %w", address, err)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func (p *Publisher) serve(ctx context.Context, conn net.Conn, files []SharedFile) error {
	address := conn.RemoteAddr().String()
	serverID := uint32(0)
	if id, ok := p.client.ServerID(); ok && id >= 0x01000000 {
		serverID = id
	}
	if err := p.offer(ctx, conn, files, serverID); err != nil {
		return err
	}
	p.mu.Lock()
	p.server = address
	p.offered = len(files)
	p.last = time.Now()
	p.lastError = ""
	p.mu.Unlock()
	p.log("ed2k: publish: offered %d files to %s", len(files), address)

	// Servers do not need client input to keep the session, but they drop
	// idle connections. Reconnect (and re-offer) when the read times out.
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Minute))
		_, _, err := readServerPacket(conn)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return errors.New("ed2k: publish: server session idle, reconnecting")
			}
			return err
		}
	}
}

func (p *Publisher) offer(ctx context.Context, conn net.Conn, files []SharedFile, serverID uint32) error {
	port := uint16(p.client.cfg.TCPPort)
	for start := 0; start < len(files); start += offerChunk {
		end := start + offerChunk
		if end > len(files) {
			end = len(files)
		}
		payload := encodeOfferFiles(files[start:end], serverID, port)
		if err := writeServerPacket(conn, OpOfferFiles, payload); err != nil {
			return err
		}
		if end < len(files) {
			if !sleepContext(ctx, 100*time.Millisecond) {
				return errors.New("ed2k: publish interrupted")
			}
		}
	}
	return nil
}

func encodeOfferFiles(files []SharedFile, clientID uint32, port uint16) []byte {
	w := newWriter(binary.LittleEndian)
	w.u32(uint32(len(files)))
	for _, file := range files {
		w.raw(file.Hash[:])
		w.u32(clientID)
		w.u16(port)
		tags := []Tag{
			{Name: TagFileName, Type: TagString, Str: file.Name},
		}
		if file.Size > math.MaxUint32 {
			tags = append(tags,
				Tag{Name: TagFileSize, Type: TagUint32, Int: uint64(uint32(file.Size))},
				Tag{Name: TagFileSizeHigh, Type: TagUint32, Int: uint64(file.Size) >> 32},
			)
		} else {
			tags = append(tags, Tag{Name: TagFileSize, Type: TagUint32, Int: uint64(file.Size)})
		}
		w.u32(uint32(len(tags)))
		w.raw(WriteTagList(tags, binary.LittleEndian))
	}
	return w.buf
}

func (p *Publisher) setError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err == nil {
		p.lastError = ""
		return
	}
	p.lastError = strings.TrimSpace(err.Error())
}

func sleepContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
