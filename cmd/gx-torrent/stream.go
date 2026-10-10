package main

// stream.go serves a torrent file to a media player over HTTP with Range
// support, while asking the engine to fetch the pieces of the requested window first
// (gextto fork). The player can start before the download is complete.

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

const (
	streamChunk       = 2 << 20
	streamReadahead   = 16 << 20
	streamWaitTimeout = 2 * time.Minute
	streamPollEvery   = 200 * time.Millisecond
)

// handleUIStream serves one file of a torrent, honouring a single HTTP Range.
// It is served under the UI authentication so ?token= works for players.
func (d *Daemon) handleUIStream(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) {
		return
	}
	hash := strings.TrimSpace(r.URL.Query().Get("hash"))
	fileIndex, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("file")))
	if err != nil || fileIndex < 0 {
		http.Error(w, "invalid file index", http.StatusBadRequest)
		return
	}
	d.mu.Lock()
	t, meta := d.findLocked(hash)
	d.mu.Unlock()
	if t == nil {
		http.Error(w, "torrent not found", http.StatusNotFound)
		return
	}
	files, err := t.Files()
	if err != nil || fileIndex >= len(files) {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	file := files[fileIndex]
	size := file.Length()
	path := filepath.Join(meta.SavePath, file.Path())

	start, length, partial, err := parseByteRange(r.Header.Get("Range"), size)
	if err != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	// Prioritize the requested window plus a readahead, so the player keeps
	// finding the next pieces ready while it reads.
	t.SetFileStreamWindow(fileIndex, start, length+streamReadahead)
	defer t.SetFileStreamWindow(fileIndex, 0, 0)

	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "file not readable", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-store")
	if partial {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, size))
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.WriteHeader(http.StatusOK)
	}

	d.streamRange(w, t, fileIndex, f, start, length)
}

// streamRange writes `length` bytes of the file from `start`, waiting for the
// pieces the player has not downloaded yet.
func (d *Daemon) streamRange(w http.ResponseWriter, t *torrent.Torrent, fileIndex int, f *os.File, start, length int64) {
	flusher, _ := w.(http.Flusher)
	deadline := time.Now().Add(streamWaitTimeout)
	buf := make([]byte, streamChunk)
	for written := int64(0); written < length; {
		n := int64(streamChunk)
		if n > length-written {
			n = length - written
		}
		if !d.waitForPieces(t, fileIndex, start+written, n, deadline) {
			return
		}
		m, err := f.ReadAt(buf[:n], start+written)
		if m > 0 {
			if _, werr := w.Write(buf[:m]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			written += int64(m)
			// Move the readahead window forward.
			t.SetFileStreamWindow(fileIndex, start+written, streamReadahead)
		}
		if err != nil {
			return
		}
	}
}

// waitForPieces blocks until the engine reports the pieces covering the byte range as
// complete, or the deadline passes. A range that cannot be mapped (no metadata)
// is treated as ready and read straight from disk.
func (d *Daemon) waitForPieces(t *torrent.Torrent, fileIndex int, offset, length int64, deadline time.Time) bool {
	begin, end, ok := t.FilePieceRange(fileIndex, offset, length)
	if !ok {
		return true
	}
	for {
		if piecesPresent(t, begin, end) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(streamPollEvery)
	}
}

func piecesPresent(t *torrent.Torrent, begin, end uint32) bool {
	// A targeted range check: PieceStates would allocate and visit a state
	// string for every piece of the torrent, every poll.
	return t.PiecesDone(begin, end)
}

// parseByteRange parses a single "bytes=" range. An empty header means the whole
// resource; partial reports whether a 206 must be sent.
func parseByteRange(header string, size int64) (start, length int64, partial bool, err error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0, size, false, nil
	}
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false, fmt.Errorf("unsupported range unit")
	}
	spec := strings.TrimSpace(strings.TrimPrefix(header, "bytes="))
	if strings.Contains(spec, ",") {
		return 0, 0, false, fmt.Errorf("multiple ranges are not supported")
	}
	left, right, found := strings.Cut(spec, "-")
	if !found {
		return 0, 0, false, fmt.Errorf("invalid range")
	}
	left, right = strings.TrimSpace(left), strings.TrimSpace(right)
	switch {
	case left == "" && right != "": // suffix: last N bytes
		n, convErr := strconv.ParseInt(right, 10, 64)
		if convErr != nil || n <= 0 {
			return 0, 0, false, fmt.Errorf("invalid suffix range")
		}
		if n > size {
			n = size
		}
		return size - n, n, true, nil
	case left != "":
		start, err = strconv.ParseInt(left, 10, 64)
		if err != nil || start < 0 || start >= size {
			return 0, 0, false, fmt.Errorf("invalid range start")
		}
		end := size - 1
		if right != "" {
			end, err = strconv.ParseInt(right, 10, 64)
			if err != nil || end < start {
				return 0, 0, false, fmt.Errorf("invalid range end")
			}
			if end >= size {
				end = size - 1
			}
		}
		return start, end - start + 1, true, nil
	default:
		return 0, 0, false, fmt.Errorf("invalid range")
	}
}
