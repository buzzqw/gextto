package main

// torrentlog.go keeps a short history of what happened to each torrent, for
// the Log tab of the detail drawer: added, completed, stopped, errors,
// tracker failures and recoveries, pieces that failed their hash. It is fed
// by the engine's session events and kept in memory only.

import (
	"fmt"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// torrentLogSize is how many entries a torrent keeps; torrentLogTorrents caps
// the torrents remembered (the least recently touched goes first).
const (
	torrentLogSize     = 50
	torrentLogTorrents = 2000
)

// torrentLogEntry is one line of a torrent's history. Msg is a catalog key;
// Detail is shown as it is (an address, a URL, an error from the engine).
// Count is how many identical events were folded into the line.
type torrentLogEntry struct {
	At     time.Time
	Msg    string
	Detail string
	Count  int
	Bad    bool
}

type torrentLogBook struct {
	entries []torrentLogEntry
	touched time.Time
	// trackers remembers, per announce URL, whether the last announce
	// failed: a failure is logged when it starts, a success when it is the
	// first or ends a failure, not at every announce.
	trackers map[string]bool
}

type torrentLog struct {
	mu    sync.Mutex
	books map[string]*torrentLogBook
}

// entries returns a torrent's history, newest first.
func (l *torrentLog) entries(hash string) []torrentLogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.books[hash]
	if b == nil {
		return nil
	}
	out := make([]torrentLogEntry, len(b.entries))
	for i, e := range b.entries {
		out[len(out)-1-i] = e
	}
	return out
}

// forget drops a removed torrent's history.
func (l *torrentLog) forget(hash string) {
	l.mu.Lock()
	delete(l.books, hash)
	l.mu.Unlock()
}

// add appends one line to a torrent's history, folding it into the last line
// when it repeats it.
func (l *torrentLog) add(hash string, at time.Time, msg, detail string, bad bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.bookLocked(hash, at)
	if n := len(b.entries); n > 0 && b.entries[n-1].Msg == msg && b.entries[n-1].Detail == detail {
		b.entries[n-1].Count++
		b.entries[n-1].At = at
		return
	}
	b.entries = append(b.entries, torrentLogEntry{At: at, Msg: msg, Detail: detail, Count: 1, Bad: bad})
	if len(b.entries) > torrentLogSize {
		b.entries = append(b.entries[:0], b.entries[len(b.entries)-torrentLogSize:]...)
	}
}

func (l *torrentLog) bookLocked(hash string, at time.Time) *torrentLogBook {
	if l.books == nil {
		l.books = make(map[string]*torrentLogBook)
	}
	b := l.books[hash]
	if b == nil {
		if len(l.books) >= torrentLogTorrents {
			var oldest string
			var oldestAt time.Time
			for h, ob := range l.books {
				if oldest == "" || ob.touched.Before(oldestAt) {
					oldest, oldestAt = h, ob.touched
				}
			}
			delete(l.books, oldest)
		}
		b = &torrentLogBook{}
		l.books[hash] = b
	}
	b.touched = at
	return b
}

// record turns one engine event into a history line, or ignores it.
func (l *torrentLog) record(ev torrent.Event) {
	if ev.Torrent == "" {
		return
	}
	at := ev.At
	if at.IsZero() {
		at = time.Now()
	}
	switch ev.Type {
	case torrent.EventTorrentAdded:
		l.add(ev.Torrent, at, "Added to the session", "", false)
	case torrent.EventTorrentCompleted:
		l.add(ev.Torrent, at, "Download complete", "", false)
	case torrent.EventTorrentStopped:
		l.add(ev.Torrent, at, "Stopped", "", false)
	case torrent.EventTorrentError:
		l.add(ev.Torrent, at, "Stopped on an error", ev.Error, true)
	case torrent.EventPieceHashFailed:
		from := ev.Peer
		if from == "" {
			from = ev.Source
		}
		l.add(ev.Torrent, at, "A piece failed its hash check and is downloaded again", from, true)
	case torrent.EventTrackerAnnounce:
		l.recordTracker(ev, at)
	}
}

func (l *torrentLog) recordTracker(ev torrent.Event, at time.Time) {
	if ev.Tracker == "" {
		return
	}
	l.mu.Lock()
	b := l.bookLocked(ev.Torrent, at)
	if b.trackers == nil {
		b.trackers = make(map[string]bool)
	}
	failedBefore, seen := b.trackers[ev.Tracker]
	failed := ev.Error != ""
	b.trackers[ev.Tracker] = failed
	l.mu.Unlock()
	switch {
	case failed:
		l.add(ev.Torrent, at, "Tracker not answering", ev.Tracker+": "+ev.Error, true)
	case !seen:
		l.add(ev.Torrent, at, "Tracker answered", fmt.Sprintf("%s: %d seeds, %d peers", ev.Tracker, ev.Seeders, ev.Leechers), false)
	case failedBefore:
		l.add(ev.Torrent, at, "Tracker answering again", fmt.Sprintf("%s: %d seeds, %d peers", ev.Tracker, ev.Seeders, ev.Leechers), false)
	}
}
