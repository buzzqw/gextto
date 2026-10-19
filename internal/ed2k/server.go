package ed2k

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	serverPacketLimit = 4 << 20
	serverTagName     = 0x01
	serverTagVersion  = 0x11
	serverTagFlags    = 0x20
	serverFlagNewTags = 0x08
	serverFlagUnicode = 0x10
	serverFlagLarge   = 0x100
)

// serverSearch runs the eD2k server protocol against the configured servers.
// It logs in, sends one keyword search and collects results until the query
// window closes. Servers are a fallback: a failure is reported but never
// prevents Kad results from being returned.
func (c *Client) searchServers(ctx context.Context, keyword string) ([]SearchFile, error) {
	servers := c.loadServers(ctx)
	if len(servers) == 0 {
		return nil, errors.New("ed2k: no eD2k servers configured")
	}
	if len(servers) > 3 {
		servers = servers[:3]
	}
	type outcome struct {
		files []SearchFile
		err   error
	}
	results := make(chan outcome, len(servers))
	var wg sync.WaitGroup
	for _, address := range servers {
		wg.Add(1)
		go func(address string) {
			defer wg.Done()
			files, err := c.searchOneServer(ctx, address, keyword)
			results <- outcome{files: files, err: err}
		}(address)
	}
	wg.Wait()
	close(results)
	var groups [][]SearchFile
	var failures []string
	for result := range results {
		if result.err != nil {
			failures = append(failures, result.err.Error())
			continue
		}
		groups = append(groups, result.files)
	}
	files := mergeFiles(0, groups...)
	if len(files) == 0 && len(failures) == len(servers) {
		return nil, errors.New(strings.Join(failures, "; "))
	}
	return files, nil
}

func (c *Client) loadServers(ctx context.Context) []string {
	if len(c.servers) > 0 {
		return c.servers
	}
	url := strings.TrimSpace(c.cfg.ServerListURL)
	if url == "" {
		return nil
	}
	cache := filepath.Join(c.cfg.DataDir, "server.met")
	if data, err := os.ReadFile(cache); err == nil && nodesFileFresh(cache, 7*24*time.Hour) && looksLikeServerMet(data) {
		if servers, err := ParseServerMet(data); err == nil && len(servers) > 0 {
			c.servers = servers
			return servers
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.setError(fmt.Errorf("ed2k: server.met download: %w", err))
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.setError(fmt.Errorf("ed2k: server.met download: HTTP %d", resp.StatusCode))
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil || !looksLikeServerMet(data) {
		return nil
	}
	servers, err := ParseServerMet(data)
	if err != nil || len(servers) == 0 {
		return nil
	}
	_ = os.WriteFile(cache, data, 0o644)
	c.servers = servers
	return servers
}

func (c *Client) searchOneServer(ctx context.Context, address, keyword string) ([]SearchFile, error) {
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", address, err)
	}
	defer conn.Close()
	deadline := time.Now().Add(c.cfg.QueryTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)

	if err := c.serverLogin(conn); err != nil {
		return nil, fmt.Errorf("%s: %w", address, err)
	}
	// Login set its own short deadline; restore the search budget for the
	// result phase.
	_ = conn.SetDeadline(deadline)
	if err := writeServerPacket(conn, OpSearchRequest, encodeSearchExpression(binary.LittleEndian, keyword)); err != nil {
		return nil, fmt.Errorf("%s: %w", address, err)
	}

	var files []SearchFile
	var parseErr error
	for {
		opcode, payload, err := readServerPacket(conn)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, os.ErrDeadlineExceeded) || isTimeout(err) {
				break
			}
			if len(files) > 0 {
				break
			}
			return nil, fmt.Errorf("%s: %w", address, err)
		}
		switch opcode {
		case OpSearchResult:
			parsed, err := decodeServerSearchResult(payload)
			if err != nil {
				parseErr = err
				continue
			}
			files = append(files, parsed...)
		case OpReject:
			return files, fmt.Errorf("%s rejected the search", address)
		}
		if len(files) >= c.cfg.MaxResults {
			break
		}
	}
	if len(files) == 0 && parseErr != nil {
		return nil, fmt.Errorf("%s: %w", address, parseErr)
	}
	for index := range files {
		files[index].Origin = "server"
	}
	return files, nil
}

func (c *Client) serverLogin(conn net.Conn) error {
	hash := c.userHash()
	w := newWriter(binary.LittleEndian)
	w.raw(hash)
	w.u32(0) // client ID: the server assigns one
	w.u16(uint16(c.cfg.TCPPort))
	w.u32(3)
	w.tag(Tag{Name: serverTagName, Type: TagString, Str: "gx-ed2k"})
	w.tag(Tag{Name: serverTagVersion, Type: TagUint32, Int: KadProtocolVersion})
	w.tag(Tag{Name: serverTagFlags, Type: TagUint32, Int: serverFlagNewTags | serverFlagUnicode | serverFlagLarge})
	if err := writeServerPacket(conn, OpLoginRequest, w.buf); err != nil {
		return err
	}
	deadline := time.Now().Add(8 * time.Second)
	_ = conn.SetReadDeadline(deadline)
	for {
		opcode, payload, err := readServerPacket(conn)
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}
		switch opcode {
		case OpIDChange:
			if len(payload) >= 4 {
				c.serverMu.Lock()
				c.serverID = binary.LittleEndian.Uint32(payload)
				c.serverIDSet = true
				c.serverMu.Unlock()
			}
			return nil
		case OpServerIdent:
			return nil
		case OpReject:
			return errors.New("login rejected")
		}
	}
}

func (c *Client) userHash() []byte {
	path := filepath.Join(c.cfg.DataDir, "userhash")
	if raw, err := os.ReadFile(path); err == nil && len(raw) == 16 {
		return raw
	}
	hash := make([]byte, 16)
	if _, err := rand.Read(hash); err != nil {
		return make([]byte, 16)
	}
	_ = os.WriteFile(path, hash, 0o600)
	return hash
}

func writeServerPacket(w io.Writer, opcode byte, payload []byte) error {
	header := make([]byte, 6)
	header[0] = Ed2kProtocol
	binary.LittleEndian.PutUint32(header[1:], uint32(len(payload)+1))
	header[5] = opcode
	if _, err := w.Write(header); err != nil {
		return err
	}
	if len(payload) > 0 {
		_, err := w.Write(payload)
		return err
	}
	return nil
}

func readServerPacket(r io.Reader) (byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}
	protocol := header[0]
	if protocol != Ed2kProtocol && protocol != 0xD4 {
		return 0, nil, fmt.Errorf("ed2k: unexpected protocol byte 0x%02x", protocol)
	}
	size := binary.LittleEndian.Uint32(header[1:])
	if size == 0 || size > serverPacketLimit {
		return 0, nil, fmt.Errorf("ed2k: invalid server packet size %d", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	if protocol == 0xD4 {
		// Servers may compress a large search result. The first payload byte is
		// still the opcode; the zlib stream starts after it. Decompress with a
		// hard cap so a malicious peer cannot expand a small packet into
		// gigabytes.
		if len(payload) < 2 {
			return 0, nil, fmt.Errorf("ed2k: empty compressed packet")
		}
		opcode := payload[0]
		zr, err := zlib.NewReader(bytes.NewReader(payload[1:]))
		if err != nil {
			return 0, nil, fmt.Errorf("ed2k: compressed packet: %w", err)
		}
		defer zr.Close()
		expanded, err := io.ReadAll(io.LimitReader(zr, serverPacketLimit+1))
		if err != nil {
			return 0, nil, fmt.Errorf("ed2k: decompress packet: %w", err)
		}
		if len(expanded) > serverPacketLimit {
			return 0, nil, fmt.Errorf("ed2k: decompressed packet too large")
		}
		return opcode, expanded, nil
	}
	if len(payload) == 0 {
		return 0, nil, fmt.Errorf("ed2k: empty server packet")
	}
	return payload[0], payload[1:], nil
}

func decodeServerSearchResult(payload []byte) ([]SearchFile, error) {
	r := newReader(payload, binary.LittleEndian)
	count, err := r.u32()
	if err != nil {
		return nil, err
	}
	if count > 1000 {
		return nil, fmt.Errorf("ed2k: implausible server result count %d", count)
	}
	files := make([]SearchFile, 0, count)
	for i := uint32(0); i < count; i++ {
		hash, err := r.id()
		if err != nil {
			return nil, err
		}
		clientID, err := r.u32()
		if err != nil {
			return nil, err
		}
		clientPort, err := r.u16()
		if err != nil {
			return nil, err
		}
		tagCount, err := r.u32()
		if err != nil {
			return nil, err
		}
		if tagCount > 200 {
			return nil, fmt.Errorf("ed2k: implausible server tag count %d", tagCount)
		}
		tags := make([]Tag, 0, tagCount)
		for j := uint32(0); j < tagCount; j++ {
			tag, err := r.tag()
			if err != nil {
				return nil, err
			}
			tags = append(tags, tag)
		}
		file, ok := searchFileFromTags(hash, tagsByID(tags), "server")
		if !ok {
			continue
		}
		file.ClientID = clientID
		file.Client = clientPort
		files = append(files, file)
	}
	return files, nil
}

// isTimeout reports whether err is a network timeout.
func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
