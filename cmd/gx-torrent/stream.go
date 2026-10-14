package main

// stream.go serves a torrent file to a media player over HTTP with Range
// support, while asking the engine to fetch the pieces of the requested window
// first, so the player can start before the download is complete. The same
// core (serveTorrentFile) backs the UI player (/ui/stream, authenticated) and
// the DLNA server (dlna.go, LAN, no auth).

import (
	"context"
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

// mediaContentTypes maps media extensions to a Content-Type so a browser or a
// player can play the stream inline instead of downloading it. The DLNA server
// also uses the class derived from it.
var mediaContentTypes = map[string]string{
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".mkv":  "video/x-matroska",
	".webm": "video/webm",
	".avi":  "video/x-msvideo",
	".mov":  "video/quicktime",
	".ts":   "video/mp2t",
	".mpg":  "video/mpeg",
	".mpeg": "video/mpeg",
	".flv":  "video/x-flv",
	".wmv":  "video/x-ms-wmv",
	".mp3":  "audio/mpeg",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".flac": "audio/flac",
	".ogg":  "audio/ogg",
	".opus": "audio/ogg",
	".wav":  "audio/wav",
	".srt":  "application/x-subrip",
	".vtt":  "text/vtt",
}

// mediaContentType returns the Content-Type for a file name: the media type for
// a known extension, application/octet-stream otherwise.
func mediaContentType(name string) string {
	if ct, ok := mediaContentTypes[strings.ToLower(filepath.Ext(name))]; ok {
		return ct
	}
	return "application/octet-stream"
}

// isMediaFile reports whether a file name has a known media extension (used by
// the DLNA server to list only playable files).
func isMediaFile(name string) bool {
	_, ok := mediaContentTypes[strings.ToLower(filepath.Ext(name))]
	return ok
}

// beginStream records one active stream of a torrent.
func (d *Daemon) beginStream(hash string) {
	d.streamMu.Lock()
	if d.activeStreams == nil {
		d.activeStreams = map[string]int{}
	}
	d.activeStreams[hash]++
	d.streamMu.Unlock()
}

// endStream drops one active stream and reports whether it was the last, so the
// caller clears the engine window only when nobody else needs it.
func (d *Daemon) endStream(hash string) (last bool) {
	d.streamMu.Lock()
	if d.activeStreams[hash] <= 1 {
		delete(d.activeStreams, hash)
		last = true
	} else {
		d.activeStreams[hash]--
	}
	d.streamMu.Unlock()
	return last
}

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
	d.serveTorrentFile(w, r, hash, t, meta, fileIndex)
}

// serveTorrentFile is the shared media-serving core: it resolves the file, sets
// the engine's stream windows, and writes the requested Range (waiting for the
// pieces). It handles HEAD too. The caller has already resolved t and meta and,
// for the UI path, authenticated.
func (d *Daemon) serveTorrentFile(w http.ResponseWriter, r *http.Request, hash string, t *torrent.Torrent, meta *torrentMeta, fileIndex int) {
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

	// The bytes the player needs now are prioritised first; the readahead just
	// after, so the next pieces are ready while it reads. Closing one of
	// several concurrent streams leaves the window the others still use.
	d.beginStream(hash)
	defer func() {
		if d.endStream(hash) {
			t.SetFileStreamWindow(fileIndex, 0, 0)
			t.SetFileReadaheadWindow(fileIndex, 0, 0)
		}
	}()
	setStreamWindows(t, fileIndex, start)

	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "file not readable", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", mediaContentType(file.Path()))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", "inline; filename=\""+sanitizeFilename(file.Path())+"\"")
	if partial {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, size))
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.WriteHeader(http.StatusOK)
	}
	if r.Method == http.MethodHead {
		return
	}

	d.streamRange(r.Context(), w, t, fileIndex, f, start, length)
}

// sanitizeFilename strips the path and characters that would break a
// Content-Disposition header.
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, "\"", "")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	return name
}

// setStreamWindows tells the engine which pieces the player needs now (the next
// streamChunk bytes are the stream window) and which it will read next (the
// readahead window). Splitting the two keeps the playhead ahead of the prefetch.
func setStreamWindows(t *torrent.Torrent, fileIndex int, pos int64) {
	t.SetFileStreamWindow(fileIndex, pos, streamChunk)
	t.SetFileReadaheadWindow(fileIndex, pos+streamChunk, streamReadahead)
}

// streamRange writes `length` bytes of the file from `start`, waiting for the
// pieces the player has not downloaded yet. The request context stops the wait
// when the player seeks away or closes the connection.
func (d *Daemon) streamRange(ctx context.Context, w http.ResponseWriter, t *torrent.Torrent, fileIndex int, f *os.File, start, length int64) {
	flusher, _ := w.(http.Flusher)
	deadline := time.Now().Add(streamWaitTimeout)
	buf := make([]byte, streamChunk)
	for written := int64(0); written < length; {
		n := int64(streamChunk)
		if n > length-written {
			n = length - written
		}
		if !d.waitForPieces(ctx, t, fileIndex, start+written, n, deadline) {
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
			// Move the stream and readahead windows forward.
			setStreamWindows(t, fileIndex, start+written)
		}
		if err != nil {
			return
		}
	}
}

// waitForPieces blocks until the engine reports the pieces covering the byte range as
// complete, or the deadline passes. A range that cannot be mapped (no metadata)
// is treated as ready and read straight from disk.
func (d *Daemon) waitForPieces(ctx context.Context, t *torrent.Torrent, fileIndex int, offset, length int64, deadline time.Time) bool {
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
		select {
		case <-ctx.Done():
			return false
		case <-time.After(streamPollEvery):
		}
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
