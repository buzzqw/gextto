package wstracker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

// wsHandler is the reference-protocol test tracker: a JSON announce/scrape
// request in, a JSON answer out.
func wsHandler(t *testing.T, handler func(req map[string]any) map[string]any) websocket.Handler {
	t.Helper()
	return websocket.Handler(func(ws *websocket.Conn) {
		for {
			var raw string
			if err := websocket.Message.Receive(ws, &raw); err != nil {
				return
			}
			var req map[string]any
			if err := json.Unmarshal([]byte(raw), &req); err != nil {
				return
			}
			resp, _ := json.Marshal(handler(req))
			if err := websocket.Message.Send(ws, string(resp)); err != nil {
				return
			}
		}
	})
}

// wsTestServer runs a WebSocket tracker against the reference protocol.
func wsTestServer(t *testing.T, handler func(req map[string]any) map[string]any) (*httptest.Server, func(context.Context, string, string) (net.Conn, error)) {
	t.Helper()
	srv := httptest.NewServer(wsHandler(t, handler))
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	return srv, dial
}

func TestWSAnnounce(t *testing.T) {
	ih := [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	srv, dial := wsTestServer(t, func(req map[string]any) map[string]any {
		// The reference client sends 20 code points, one per byte.
		if s, _ := req["info_hash"].(string); len([]rune(s)) != 20 {
			t.Errorf("info_hash = %q, want 20 code points", s)
		}
		if req["action"] != "announce" {
			t.Errorf("action = %v", req["action"])
		}
		if req["event"] != "started" {
			t.Errorf("event = %v", req["event"])
		}
		return map[string]any{
			"action":     "announce",
			"info_hash":  req["info_hash"],
			"complete":   3,
			"incomplete": 5,
			"interval":   120,
			"peers": []any{
				map[string]any{"ip": "10.0.0.1", "port": 6881},
				map[string]any{"ip": "10.0.0.2", "port": 51413},
				map[string]any{"ip": "not-an-ip", "port": 1},
			},
		}
	})
	defer srv.Close()

	u, _ := url.Parse(strings.Replace(srv.URL, "http://", "ws://", 1) + "/announce")
	tr := New(u.String(), u, 5*time.Second, dial, false)
	defer tr.Close()

	resp, err := tr.Announce(context.Background(), tracker.AnnounceRequest{
		Torrent: tracker.Torrent{InfoHash: ih, PeerID: [20]byte{9}, BytesLeft: 100},
		Event:   tracker.EventStarted,
		NumWant: 50,
	})
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if resp.Seeders != 3 || resp.Leechers != 5 || resp.Interval != 120*time.Second {
		t.Fatalf("response = %+v", resp)
	}
	if len(resp.Peers) != 2 {
		t.Fatalf("peers = %d, want 2 (the invalid one dropped)", len(resp.Peers))
	}
	if resp.Peers[0].String() != "10.0.0.1:6881" || resp.Peers[1].String() != "10.0.0.2:51413" {
		t.Fatalf("peers = %v", resp.Peers)
	}
}

func TestWSScrapeAndFailure(t *testing.T) {
	ih := [20]byte{0xaa}
	srv, dial := wsTestServer(t, func(req map[string]any) map[string]any {
		if req["action"] == "scrape" {
			return map[string]any{
				"action": "scrape",
				"files": map[string]any{
					"aa00000000000000000000000000000000000000": map[string]any{
						"complete": 7, "incomplete": 2, "downloaded": 42,
					},
				},
			}
		}
		return map[string]any{"action": "announce", "failure reason": "unregistered torrent", "retry in": "30"}
	})
	defer srv.Close()

	u, _ := url.Parse(strings.Replace(srv.URL, "http://", "ws://", 1) + "/announce")
	tr := New(u.String(), u, 5*time.Second, dial, false)
	defer tr.Close()

	sc, err := tr.Scrape(context.Background(), tracker.ScrapeRequest{Torrent: tracker.Torrent{InfoHash: ih}})
	if err != nil {
		t.Fatalf("Scrape: %v", err)
	}
	if sc.Seeders != 7 || sc.Leechers != 2 || sc.Downloaded != 42 {
		t.Fatalf("scrape = %+v", sc)
	}

	_, err = tr.Announce(context.Background(), tracker.AnnounceRequest{Torrent: tracker.Torrent{InfoHash: ih}})
	var terr *tracker.Error
	if err == nil || !errors.As(err, &terr) {
		t.Fatalf("Announce error = %v, want a tracker.Error", err)
	}
	if terr.RetryIn != 30*time.Minute {
		t.Fatalf("retry in = %v, want 30m", terr.RetryIn)
	}
}

func TestWSSecure(t *testing.T) {
	srv := httptest.NewTLSServer(wsHandler(t, func(req map[string]any) map[string]any {
		return map[string]any{"action": "announce", "complete": 1, "incomplete": 0, "interval": 60}
	}))
	defer srv.Close()

	u, _ := url.Parse(strings.Replace(srv.URL, "https://", "wss://", 1) + "/announce")
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	tr := New(u.String(), u, 5*time.Second, dial, true) // skipVerify: the test cert is self-signed
	defer tr.Close()
	if _, err := tr.Announce(context.Background(), tracker.AnnounceRequest{Torrent: tracker.Torrent{InfoHash: [20]byte{1}}}); err != nil {
		t.Fatalf("wss announce: %v", err)
	}
}

func TestHTTPReadResponseGuard(t *testing.T) {
	// A non-101 handshake answer must be rejected, not treated as a socket.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusBadRequest)
	}))
	defer srv.Close()
	u, _ := url.Parse(strings.Replace(srv.URL, "http://", "ws://", 1) + "/announce")
	tr := New(u.String(), u, 2*time.Second, func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}, false)
	defer tr.Close()
	if _, err := tr.Announce(context.Background(), tracker.AnnounceRequest{Torrent: tracker.Torrent{InfoHash: [20]byte{1}}}); err == nil {
		t.Fatal("expected a handshake error")
	}
}

// FuzzWSReadMessage feeds arbitrary bytes to the frame reader: it must never
// panic or allocate unbounded memory.
func FuzzWSReadMessage(f *testing.F) {
	f.Add([]byte{0x81, 0x02, 'h', 'i'})
	f.Add([]byte{0x89, 0x00}) // ping, empty
	f.Add([]byte{0x88, 0x00}) // close
	f.Add([]byte{0x81, 0x7e, 0x00, 0x05, 'h', 'e', 'l', 'l', 'o'})
	f.Fuzz(func(t *testing.T, data []byte) {
		c := &wsConn{br: bufio.NewReader(bytes.NewReader(data))}
		_, _ = c.readMessage()
	})
}
