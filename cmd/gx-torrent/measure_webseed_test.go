package main

// measure_webseed_test.go is an opt-in measurement of the parallel ranges one
// webseed can serve: a host that answers each file in seconds (archive.org)
// used to be bound by that latency, because a source fetched one range at a
// time and the range visited the files one after the other.
//
// The harness builds a multi-file torrent and serves it from a local HTTP
// server that waits before answering every range request, then downloads it
// once with one range per source and once with four, and reports the elapsed
// time and the peak number of requests in flight. The two runs differ only in
// `webseed-max-downloads`, which is exactly the number of ranges a source may
// open.
//
//	GX_MEASURE=1 go test ./cmd/gx-torrent/ -run MeasureWebseed -v -count=1
//
// GX_MEASURE_WEBSWSEED_DELAY (a Go duration, 200ms by default) sets how long
// the server waits before answering a request. See docs/measurements.md.

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

const (
	webseedMeasureFiles      = 8
	webseedMeasureFileSize   = 256 << 10
	webseedMeasurePieceLen   = 128 << 10
	webseedMeasureDeadline   = 60 * time.Second
	webseedMeasureMaxRanges  = 4
	webseedMeasureDelay      = 200 * time.Millisecond
	webseedMeasureDelayEnv   = "GX_MEASURE_WEBSWSEED_DELAY"
	webseedMeasureBodyPrefix = "webseed-measure"
)

// webseedServer serves the torrent payload, one HTTP range per request, with
// a fixed wait before answering and counters for what the client asked.
type webseedServer struct {
	server     *httptest.Server
	files      map[string][]byte
	inFlight   atomic.Int32
	peak       atomic.Int32
	requests   atomic.Int32
	delay      time.Duration
	mu         sync.Mutex
	requestLog []time.Time
}

func newWebseedServer(files map[string][]byte, delay time.Duration) *webseedServer {
	ws := &webseedServer{files: files, delay: delay}
	ws.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		body, ok := ws.files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		current := ws.inFlight.Add(1)
		defer ws.inFlight.Add(-1)
		for {
			peak := ws.peak.Load()
			if current <= peak || ws.peak.CompareAndSwap(peak, current) {
				break
			}
		}
		ws.requests.Add(1)
		ws.mu.Lock()
		ws.requestLog = append(ws.requestLog, time.Now())
		ws.mu.Unlock()
		time.Sleep(ws.delay)
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
	}))
	return ws
}

func (ws *webseedServer) close() { ws.server.Close() }

// webseedMeasureTorrent builds a multi-file torrent whose URL list points at
// the local server, the way a real archive.org item carries its sources.
func webseedMeasureTorrent(serverURL string, files map[string][]byte, names []string) []byte {
	var pieces []byte
	var infoFiles []any
	var pending []byte
	for _, name := range names {
		body := files[name]
		infoFiles = append(infoFiles, map[string]any{
			"length": len(body),
			"path":   []any{name},
		})
		pending = append(pending, body...)
		for len(pending) >= webseedMeasurePieceLen {
			sum := sha1.Sum(pending[:webseedMeasurePieceLen])
			pieces = append(pieces, sum[:]...)
			pending = pending[webseedMeasurePieceLen:]
		}
	}
	if len(pending) > 0 {
		sum := sha1.Sum(pending)
		pieces = append(pieces, sum[:]...)
	}
	return bencode(map[string]any{
		"info": map[string]any{
			"name":         "webseed-measure",
			"piece length": webseedMeasurePieceLen,
			"pieces":       pieces,
			"files":        infoFiles,
		},
		"url-list": []any{serverURL + "/"},
	})
}

// runWebseedDownload downloads the torrent with the given number of ranges per
// source and reports how long it took.
func runWebseedDownload(t *testing.T, data []byte, ranges int, ws *webseedServer) (time.Duration, int32, int32) {
	t.Helper()
	dir := t.TempDir()
	cfg := torrent.DefaultConfig
	cfg.Database = filepath.Join(dir, "session.db")
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.ListenPort = 0
	cfg.DHTEnabled = false
	cfg.RPCEnabled = false
	cfg.ResumeOnStartup = false
	cfg.WebseedMaxDownloads = ranges
	// The harness serves the payload from loopback, which the engine refuses
	// for URLs that come from torrent data unless it is told otherwise.
	cfg.WebSourcesAllowPrivate = true
	cfg.Debug = os.Getenv("GX_TEST_DEBUG") == "1"
	s, err := torrent.NewSession(cfg)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer s.Close()
	tt, err := s.AddTorrent(bytes.NewReader(data), nil)
	if err != nil {
		t.Fatalf("AddTorrent: %v", err)
	}
	ws.mu.Lock()
	ws.requestLog = nil
	ws.mu.Unlock()
	before := ws.requests.Load()
	ws.peak.Store(0)
	start := time.Now()
	select {
	case <-tt.NotifyComplete():
	case <-time.After(webseedMeasureDeadline):
		stats := tt.Stats()
		t.Fatalf("torrent did not complete in %s (completed %d of %d bytes)", webseedMeasureDeadline, stats.Bytes.Completed, stats.Bytes.Total)
	}
	elapsed := time.Since(start)
	if os.Getenv("GX_MEASURE_WEBSWSEED_TRACE") == "1" {
		ws.mu.Lock()
		for i, at := range ws.requestLog {
			t.Logf("  request %d at %s", i, at.Sub(start).Round(time.Millisecond))
		}
		ws.mu.Unlock()
	}
	return elapsed, ws.requests.Load() - before, ws.peak.Load()
}

func TestMeasureWebseedParallelRanges(t *testing.T) {
	if os.Getenv("GX_MEASURE") != "1" {
		t.Skip("set GX_MEASURE=1 to run the webseed measurement")
	}
	delay := webseedMeasureDelay
	if raw := os.Getenv(webseedMeasureDelayEnv); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			t.Fatalf("%s=%q: %v", webseedMeasureDelayEnv, raw, err)
		}
		delay = parsed
	}
	files := make(map[string][]byte, webseedMeasureFiles)
	names := make([]string, 0, webseedMeasureFiles)
	for i := 0; i < webseedMeasureFiles; i++ {
		name := fmt.Sprintf("%s-%02d.bin", webseedMeasureBodyPrefix, i)
		body := bytes.Repeat([]byte{byte(i + 1)}, webseedMeasureFileSize)
		files[name] = body
		names = append(names, name)
	}
	ws := newWebseedServer(files, delay)
	defer ws.close()
	data := webseedMeasureTorrent(ws.server.URL, files, names)

	t.Logf("server delay per request: %s; %d files of %d bytes",
		delay, webseedMeasureFiles, webseedMeasureFileSize)
	for _, ranges := range []int{1, webseedMeasureMaxRanges} {
		elapsed, requests, peak := runWebseedDownload(t, data, ranges, ws)
		t.Logf("ranges per source=%d: elapsed=%s requests=%d peak concurrent requests=%d",
			ranges, elapsed.Round(time.Millisecond), requests, peak)
	}

	// The measurement is reported, not asserted: the gate is the ratio the
	// operator reads above. A broken harness (no request at all) still fails.
	if got := ws.requests.Load(); got == 0 {
		t.Fatal("the webseed server was never contacted")
	}
}
