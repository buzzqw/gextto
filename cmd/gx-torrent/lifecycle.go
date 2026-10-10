package main

// lifecycle.go holds what lets the daemon outlive a Gextto restart: its own
// rotating log (it no longer depends on a file Gextto opened), the orphan
// watchdog (a daemon left alone by Gextto stops by itself) and the tail of
// the Gextto log shown in the web page.

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	clog "github.com/cenkalti/log"

	"github.com/buzzqw/gextto/internal/logging"
)

// Log rotation: 5 MB per file, 4 files kept (gx-torrent.log, .1, .2, .3).
const (
	logMaxBytes = 5 * 1024 * 1024
	logMaxFiles = 4
)

// logOutput is where the daemon and the engine write: stderr, or the rotating file
// chosen with -log-file.
var logOutput io.Writer = os.Stderr

// setupLogFile sends the daemon log and the engine's log to a rotating file.
func setupLogFile(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	writer := logging.NewRotatingWriter(filepath.Dir(abs), filepath.Base(abs), logMaxBytes, logMaxFiles)
	logOutput = writer
	log.SetOutput(writer)
}

// engineLogHandler keeps the engine's own log to warnings, or everything with -debug.
// Errors of single peers (handshake timeouts, resets) and failed announces to
// one tracker are the normal life of a swarm, not daemon errors: they are
// demoted to debug, so the log shows what matters.
func engineLogHandler(debug bool) clog.Handler {
	handler := clog.NewWriterHandler(logOutput)
	if debug {
		handler.SetLevel(clog.DEBUG)
	} else {
		handler.SetLevel(clog.WARNING)
	}
	return &swarmNoiseFilter{Handler: handler}
}

type swarmNoiseFilter struct {
	clog.Handler
}

func (f *swarmNoiseFilter) Handle(rec *clog.Record) {
	if rec.Level > clog.CRITICAL && rec.Level <= clog.WARNING && isSwarmNoise(rec) {
		peerNoiseCounters.add(swarmNoiseCategory(rec))
		demoted := *rec
		demoted.Level = clog.DEBUG
		rec = &demoted
	}
	f.Handler.Handle(rec)
}

// isSwarmNoise recognises the per-peer and per-tracker errors the engine logs at
// error level.
func isSwarmNoise(rec *clog.Record) bool {
	if strings.HasPrefix(rec.LoggerName, "peer ") || strings.HasPrefix(rec.LoggerName, "conn ") {
		return true
	}
	return strings.HasPrefix(rec.LoggerName, "torrent ") &&
		(strings.Contains(rec.Message, "announce error") || strings.Contains(rec.Message, "webseed"))
}

// swarmNoiseCategory buckets a demoted record so the periodic summary says
// whether the swarm is merely chatty or the network is genuinely unhealthy.
func swarmNoiseCategory(rec *clog.Record) string {
	message := rec.Message
	switch {
	case strings.Contains(message, "announce error"):
		return "tracker"
	case strings.Contains(message, "outgoing handshake"):
		return "handshake"
	case strings.Contains(message, "peer reset"):
		return "reset"
	case strings.Contains(message, "timed out waiting for ack"):
		return "ack_timeout"
	case strings.Contains(message, "i/o timeout"):
		return "io_timeout"
	case strings.Contains(message, "cannot write message"):
		return "write"
	default:
		return "other"
	}
}

// peerNoiseStats accumulates the peer/tracker errors demoted by the filter.
// They are no longer printed one by one; a periodic summary reports how many
// arrived since the last check, and the totals are exposed by /api/v1/health.
type peerNoiseStats struct {
	mu       sync.Mutex
	total    map[string]int64
	reported map[string]int64
}

func newPeerNoiseStats() *peerNoiseStats {
	return &peerNoiseStats{total: map[string]int64{}, reported: map[string]int64{}}
}

var peerNoiseCounters = newPeerNoiseStats()

func (s *peerNoiseStats) add(category string) {
	s.mu.Lock()
	s.total[category]++
	s.mu.Unlock()
}

// totals returns a copy of the cumulative counts since start-up.
func (s *peerNoiseStats) totals() map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int64, len(s.total))
	for key, value := range s.total {
		out[key] = value
	}
	return out
}

// drain returns the counts that arrived since the previous drain.
func (s *peerNoiseStats) drain() map[string]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int64{}
	for key, value := range s.total {
		if delta := value - s.reported[key]; delta > 0 {
			out[key] = delta
			s.reported[key] = value
		}
	}
	return out
}

// formatPeerNoise renders the deltas as a stable, compact line.
func formatPeerNoise(deltas map[string]int64) string {
	if len(deltas) == 0 {
		return ""
	}
	keys := make([]string, 0, len(deltas))
	for key := range deltas {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, deltas[key]))
	}
	return strings.Join(parts, " ")
}

// apiSeen records the last API request (Unix seconds) for the orphan watchdog.
var apiSeen atomic.Int64

func markAPISeen() { apiSeen.Store(time.Now().Unix()) }

// watchOrphan stops the daemon (by sending on stop) when no API request has
// arrived for timeout. Gextto polls every few seconds, so a long silence means
// it is gone: a daemon kept alive across Gextto restarts must not run forever
// once Gextto has been stopped for good. Zero disables the watchdog.
func watchOrphan(timeout time.Duration, stop chan<- os.Signal) {
	if timeout <= 0 {
		return
	}
	markAPISeen()
	check := min(timeout/4, time.Minute)
	go func() {
		for range time.Tick(check) {
			last := time.Unix(apiSeen.Load(), 0)
			if time.Since(last) >= timeout {
				logf("no request from Gextto for %s: shutting down", timeout.Round(time.Second))
				stop <- os.Interrupt
				return
			}
		}
	}()
}

// tailLines returns the last n lines of a text file, reading only its end.
func tailLines(path string, n int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	// About 400 bytes a line is plenty for Gextto's log.
	window := int64(n) * 400
	start := max(info.Size()-window, 0)
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	var lines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	first := start > 0
	for scanner.Scan() {
		if first {
			// The window may start in the middle of a line.
			first = false
			continue
		}
		lines = append(lines, scanner.Text())
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, scanner.Err()
}
