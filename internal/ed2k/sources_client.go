package ed2k

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sources asks Kad and the eD2k servers for peers that hold a file. The hash
// is the MD4 from the ed2k link and size is the link size. A failing network
// does not discard the other one: sources are merged by IP:TCP port.
func (c *Client) Sources(ctx context.Context, hash string, size int64) ([]PeerSource, error) {
	target, err := FileID(hash)
	if err != nil {
		return nil, err
	}
	if size <= 0 {
		return nil, errors.New("ed2k: invalid file size")
	}
	// Kad needs the UDP socket; the server path does not. A bind failure is
	// reported by sourcesFromKad and does not stop the server query.
	_ = c.Start(ctx)
	searchCtx, cancel := context.WithTimeout(ctx, c.cfg.SearchTimeout)
	defer cancel()

	type result struct {
		sources []PeerSource
		err     error
	}
	kadCh := make(chan result, 1)
	serverCh := make(chan result, 1)
	go func() {
		sources, err := c.sourcesFromKad(searchCtx, target, size)
		kadCh <- result{sources: sources, err: err}
	}()
	go func() {
		sources, err := c.sourcesFromServers(searchCtx, target, size)
		serverCh <- result{sources: sources, err: err}
	}()

	kad := <-kadCh
	servers := <-serverCh
	merged := c.filterSources(mergeSources(kad.sources, servers.sources))
	c.mu.Lock()
	if len(merged) > 0 {
		c.results += int64(len(merged))
	} else if kad.err != nil {
		c.lastErr = kad.err.Error()
	} else if servers.err != nil {
		c.lastErr = servers.err.Error()
	}
	c.mu.Unlock()
	c.saveNodes()

	if len(merged) > 0 {
		return merged, nil
	}
	if kad.err != nil && servers.err != nil {
		return nil, fmt.Errorf("ed2k: sources failed: Kad: %v; servers: %v", kad.err, servers.err)
	}
	if servers.err != nil {
		return nil, servers.err
	}
	if kad.err != nil {
		return nil, kad.err
	}
	return nil, nil
}

func (c *Client) sourcesFromKad(ctx context.Context, target ID, size int64) ([]PeerSource, error) {
	if err := c.Bootstrap(ctx); err != nil {
		return nil, err
	}
	c.lookup(ctx, target, 3*time.Second)

	collector := &collector{sources: make(chan PeerSource, 256)}
	c.addCollector(target, collector)
	defer c.removeCollector(target, collector)

	contacts := c.table.closest(target, 16)
	if len(contacts) == 0 {
		return nil, errors.New("ed2k: no Kad contacts")
	}
	payload := encodeSourceSearch(target, size)
	for index, contact := range contacts {
		if index > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(kadSendGap):
			}
		}
		c.send(contact, KadSearchSource, payload)
	}

	deadline := time.Now().Add(c.cfg.QueryTimeout)
	if max := time.Now().Add(6 * time.Second); deadline.After(max) {
		deadline = max
	}
	var sources []PeerSource
	var last time.Time
	for time.Now().Before(deadline) && len(sources) < c.cfg.MaxResults {
		if len(sources) > 0 && time.Since(last) > 500*time.Millisecond {
			break
		}
		select {
		case <-ctx.Done():
			return sources, nil
		case source := <-collector.sources:
			sources = append(sources, source)
			last = time.Now()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return sources, nil
}

func (c *Client) sourcesFromServers(ctx context.Context, hash ID, size int64) ([]PeerSource, error) {
	servers := c.loadServers(ctx)
	if len(servers) == 0 {
		return nil, errors.New("ed2k: no eD2k servers configured")
	}
	if len(servers) > 5 {
		servers = servers[:5]
	}
	type result struct {
		sources []PeerSource
		err     error
	}
	results := make(chan result, len(servers))
	var wg sync.WaitGroup
	for _, address := range servers {
		wg.Add(1)
		go func(address string) {
			defer wg.Done()
			sources, err := c.sourcesFromOneServer(ctx, address, hash, size)
			results <- result{sources: sources, err: err}
		}(address)
	}
	wg.Wait()
	close(results)
	var groups [][]PeerSource
	var failures []string
	for outcome := range results {
		if outcome.err != nil {
			failures = append(failures, outcome.err.Error())
			continue
		}
		groups = append(groups, outcome.sources)
	}
	sources := mergeSources(groups...)
	if len(sources) == 0 && len(failures) == len(servers) {
		return nil, errors.New(strings.Join(failures, "; "))
	}
	return sources, nil
}

func (c *Client) sourcesFromOneServer(ctx context.Context, address string, hash ID, size int64) ([]PeerSource, error) {
	serverHost, serverPortText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", address, err)
	}
	serverPort, err := strconv.Atoi(serverPortText)
	if err != nil || serverPort <= 0 || serverPort > 65535 {
		return nil, fmt.Errorf("%s: invalid port", address)
	}
	serverIP := net.ParseIP(serverHost)
	if serverIP == nil {
		resolved, resolveErr := net.ResolveIPAddr("ip4", serverHost)
		if resolveErr != nil {
			return nil, fmt.Errorf("%s: %w", address, resolveErr)
		}
		serverIP = resolved.IP
	}
	if c.cfg.IPFilter.Blocked(serverIP) {
		return nil, fmt.Errorf("%s: blocked by the IP filter", address)
	}
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
	if err := writeServerPacket(conn, OpGetSources, encodeGetSources(hash, size)); err != nil {
		return nil, fmt.Errorf("%s: %w", address, err)
	}

	var sources []PeerSource
	for {
		opcode, payload, err := readServerPacket(conn)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || isTimeout(err) {
				break
			}
			if len(sources) > 0 {
				break
			}
			return nil, fmt.Errorf("%s: %w", address, err)
		}
		switch opcode {
		case OpFoundSources:
			foundHash, found, err := decodeFoundSourcesFromServer(payload, serverIP, uint16(serverPort))
			if err != nil || foundHash != hash {
				continue
			}
			sources = append(sources, found...)
			idle := time.Now().Add(2 * time.Second)
			if idle.After(deadline) {
				idle = deadline
			}
			_ = conn.SetDeadline(idle)
		case OpReject:
			return sources, fmt.Errorf("%s rejected the source request", address)
		}
	}
	return sources, nil
}

func mergeSources(groups ...[]PeerSource) []PeerSource {
	seen := make(map[string]int)
	var out []PeerSource
	for _, group := range groups {
		for _, source := range group {
			if source.LowID {
				if source.ClientID == 0 || source.ServerIP == nil || source.ServerPort == 0 {
					continue
				}
			} else if source.IP == nil || (source.TCPPort == 0 && source.UDPPort == 0) {
				continue
			}
			key := ""
			if source.LowID {
				key = fmt.Sprintf("lowid/%d@%s:%d", source.ClientID, source.ServerIP, source.ServerPort)
			} else if source.IP != nil {
				key = fmt.Sprintf("%s:%d", source.IP.String(), source.TCPPort)
				if source.TCPPort == 0 {
					key = fmt.Sprintf("%s:udp/%d", source.IP.String(), source.UDPPort)
				}
			}
			if key == "" {
				continue
			}
			if index, ok := seen[key]; ok {
				existing := &out[index]
				if existing.UserHash == (ID{}) && source.UserHash != (ID{}) {
					existing.UserHash = source.UserHash
				}
				if existing.UDPPort == 0 && source.UDPPort != 0 {
					existing.UDPPort = source.UDPPort
				}
				if existing.BuddyID == (ID{}) && source.BuddyID != (ID{}) {
					existing.BuddyID = source.BuddyID
					existing.BuddyIP = source.BuddyIP
					existing.BuddyPort = source.BuddyPort
				}
				if existing.ClientID == 0 && source.ClientID != 0 {
					existing.ClientID = source.ClientID
					existing.LowID = source.LowID
					existing.ServerIP = source.ServerIP
					existing.ServerPort = source.ServerPort
				}
				if existing.Origin != source.Origin {
					existing.Origin = "mixed"
				}
				continue
			}
			seen[key] = len(out)
			out = append(out, source)
		}
	}
	return out
}
