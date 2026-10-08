package main

// lifecycle.go holds what lets the daemon outlive a Gextto restart: its own
// rotating log (it no longer depends on a file Gextto opened), the orphan
// watchdog (a daemon left alone by Gextto stops by itself) and the tail of
// the Gextto log shown in the web page.

import (
	"bufio"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
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

// logOutput is where the daemon and rain write: stderr, or the rotating file
// chosen with -log-file.
var logOutput io.Writer = os.Stderr

// setupLogFile sends the daemon log and rain's log to a rotating file.
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

// rainLogHandler keeps rain's own log to warnings, or everything with -debug.
// Errors of single peers (handshake timeouts, resets) and failed announces to
// one tracker are the normal life of a swarm, not daemon errors: they are
// demoted to debug, so the log shows what matters.
func rainLogHandler(debug bool) clog.Handler {
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
		demoted := *rec
		demoted.Level = clog.DEBUG
		rec = &demoted
	}
	f.Handler.Handle(rec)
}

// isSwarmNoise recognises the per-peer and per-tracker errors rain logs at
// error level.
func isSwarmNoise(rec *clog.Record) bool {
	if strings.HasPrefix(rec.LoggerName, "peer ") {
		return true
	}
	return strings.HasPrefix(rec.LoggerName, "torrent ") &&
		(strings.Contains(rec.Message, "announce error") || strings.Contains(rec.Message, "webseed"))
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
