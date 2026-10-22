package wstracker

// wstracker.go is the WebSocket tracker client: it announces and scrapes over
// the WebTorrent/bittorrent-tracker protocol (JSON messages on a ws:// or
// wss:// socket). It implements tracker.Tracker, so the announcer, the tier
// logic and BEP 31 retry are unchanged. The binary fields (info_hash, peer_id)
// travel as JSON strings of 20 code points, one per byte, exactly as the
// reference client sends them.

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

// Tracker is one WebSocket tracker endpoint.
type Tracker struct {
	rawURL     string
	parsed     *url.URL
	timeout    time.Duration
	dial       dialFunc
	skipVerify bool
	userAgent  string

	mu   sync.Mutex
	conn *wsConn
}

// New builds a WebSocket tracker for rawURL. dial routes through the session
// egress (the same closure the HTTP transport uses); skipVerify follows the
// session TLS policy.
func New(rawURL string, u *url.URL, timeout time.Duration, dial func(ctx context.Context, network, addr string) (net.Conn, error), skipVerify bool, userAgent ...string) *Tracker {
	agent := ""
	if len(userAgent) > 0 {
		agent = userAgent[0]
	}
	return &Tracker{
		rawURL:     rawURL,
		parsed:     u,
		timeout:    timeout,
		dial:       dial,
		skipVerify: skipVerify,
		userAgent:  agent,
	}
}

// URL returns the tracker URL.
func (t *Tracker) URL() string { return t.rawURL }

// Announce reports progress and collects peers over the socket.
func (t *Tracker) Announce(ctx context.Context, req tracker.AnnounceRequest) (*tracker.AnnounceResponse, error) {
	if req.Torrent.BytesLeft < 0 {
		return nil, errors.New("wstracker: the torrent has a negative remaining size")
	}
	params := map[string]any{
		"action":     "announce",
		"info_hash":  binString(req.Torrent.InfoHash[:]),
		"peer_id":    binString(req.Torrent.PeerID[:]),
		"uploaded":   req.Torrent.BytesUploaded,
		"downloaded": req.Torrent.BytesDownloaded,
		"left":       req.Torrent.BytesLeft,
		"numwant":    req.NumWant,
	}
	if req.Torrent.Port > 0 {
		params["port"] = req.Torrent.Port
	}
	params["event"] = wsEvent(req.Event)

	resp, err := t.exchange(ctx, params)
	if err != nil {
		return nil, err
	}
	if fr, _ := resp["failure reason"].(string); fr != "" {
		retry, _ := resp["retry in"].(string)
		return nil, tracker.NewError(fr, retry)
	}
	out := &tracker.AnnounceResponse{
		Interval:       seconds(resp["interval"]),
		MinInterval:    seconds(resp["min interval"]),
		Leechers:       int32(number(resp["incomplete"])),
		Seeders:        int32(number(resp["complete"])),
		WarningMessage: stringOf(resp["warning message"]),
	}
	out.Peers = decodeWSPeers(resp["peers"])
	return out, nil
}

// Scrape asks for the swarm totals (the reference tracker answers with a
// "files" map; a tracker that does not implement it returns the not-supported
// error so the caller keeps the last known totals).
func (t *Tracker) Scrape(ctx context.Context, req tracker.ScrapeRequest) (*tracker.ScrapeResponse, error) {
	params := map[string]any{
		"action":    "scrape",
		"info_hash": []any{binString(req.Torrent.InfoHash[:])},
	}
	resp, err := t.exchange(ctx, params)
	if err != nil {
		return nil, err
	}
	if fr, _ := resp["failure reason"].(string); fr != "" {
		return nil, tracker.NewError(fr, "")
	}
	files, ok := resp["files"].(map[string]any)
	if !ok {
		return nil, tracker.ErrScrapeNotSupported
	}
	hex := hexString(req.Torrent.InfoHash[:])
	entry, ok := files[hex].(map[string]any)
	if !ok {
		return nil, tracker.ErrScrapeNotSupported
	}
	return &tracker.ScrapeResponse{
		Seeders:    int32(number(entry["complete"])),
		Leechers:   int32(number(entry["incomplete"])),
		Downloaded: int32(number(entry["downloaded"])),
	}, nil
}

// exchange sends one JSON message and reads one JSON answer. The connection is
// kept for the next call and dropped on any error, so a stale socket is not
// reused.
func (t *Tracker) exchange(ctx context.Context, params map[string]any) (map[string]any, error) {
	payload, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn == nil {
		conn, err := dialWebSocket(ctx, t.dial, t.parsed, t.skipVerify, t.timeout, t.userAgent)
		if err != nil {
			return nil, err
		}
		t.conn = conn
	}
	if t.timeout > 0 {
		t.conn.SetDeadline(time.Now().Add(t.timeout))
	}
	if err := t.conn.writeText(payload); err != nil {
		t.dropLocked()
		return nil, err
	}
	raw, err := t.conn.readMessage()
	if err != nil {
		t.dropLocked()
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, tracker.ErrDecode
	}
	return out, nil
}

// dropLocked closes and forgets the connection. The caller holds t.mu.
func (t *Tracker) dropLocked() {
	if t.conn != nil {
		_ = t.conn.Close()
		t.conn = nil
	}
}

// Close releases the socket.
func (t *Tracker) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.dropLocked()
}

// wsEvent maps an announce event to the WebTorrent spelling. A periodic
// announce carries "update" (the reference tracker rejects an empty event).
func wsEvent(e tracker.Event) string {
	switch e {
	case tracker.EventStarted:
		return "started"
	case tracker.EventCompleted:
		return "completed"
	case tracker.EventStopped:
		return "stopped"
	case tracker.EventPaused:
		return "paused"
	default:
		return "update"
	}
}

// binString renders a byte string as a JSON string of one code point per byte,
// matching the reference client's binary strings (not hex, not base64). The
// caller json.Marshal's the result, which escapes the control characters.
func binString(b []byte) string {
	runes := make([]rune, len(b))
	for i, c := range b {
		runes[i] = rune(c)
	}
	return string(runes)
}

// hexString renders an info hash as the lowercase hex key the scrape "files"
// map uses.
func hexString(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = hexdigits[c>>4]
		out[i*2+1] = hexdigits[c&0x0f]
	}
	return string(out)
}

// decodeWSPeers reads the "peers" field: the WebSocket tracker returns full
// peer objects ({"ip","port"}), but a compact binary string is tolerated too.
func decodeWSPeers(v any) []*net.TCPAddr {
	switch peers := v.(type) {
	case []any:
		out := make([]*net.TCPAddr, 0, len(peers))
		for _, raw := range peers {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			ip := net.ParseIP(stringOf(m["ip"]))
			port := int(number(m["port"]))
			if ip == nil || port <= 0 || port > 65535 {
				continue
			}
			out = append(out, &net.TCPAddr{IP: ip, Port: port})
		}
		return out
	case string:
		return decodeCompact(peers)
	default:
		return nil
	}
}

// decodeCompact decodes a BEP 23 compact peer string (6 bytes per IPv4 peer).
func decodeCompact(s string) []*net.TCPAddr {
	b := []byte(s)
	out := make([]*net.TCPAddr, 0, len(b)/6)
	for i := 0; i+6 <= len(b); i += 6 {
		out = append(out, &net.TCPAddr{
			IP:   net.IPv4(b[i], b[i+1], b[i+2], b[i+3]),
			Port: int(binary.BigEndian.Uint16(b[i+4 : i+6])),
		})
	}
	return out
}

// stringOf coerces a JSON value to a string ("" when absent).
func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

// number coerces a JSON number (float64) or numeric string to an int64.
func number(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

// seconds converts a JSON seconds value to a duration.
func seconds(v any) time.Duration {
	n := number(v)
	if n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// ensure the interface is satisfied.
var _ tracker.Tracker = (*Tracker)(nil)
