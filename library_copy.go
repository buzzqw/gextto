package gextto

import (
	"strings"
	"sync"
	"time"
)

// libraryCopies tracks the file copies Gextto makes while importing a
// completed torrent into the library. The copy runs inside the event worker
// and can take a long time on a network destination: the download keeps
// seeding meanwhile, so the UI names the copy on the torrent's row instead of
// leaving the operator to wonder why nothing is moving.

type libraryCopy struct {
	started time.Time
	target  string
	count   int
}

type libraryCopyRegistry struct {
	mu     sync.Mutex
	active map[string]libraryCopy
}

var libraryCopies = &libraryCopyRegistry{active: map[string]libraryCopy{}}

// beginLibraryCopy records a copy for hash and returns the function that ends
// it. Call the returned function exactly once, whichever way the copy ends:
// overlapping copies of the same torrent keep the badge until the last one.
func beginLibraryCopy(hash, target string) func() {
	key := strings.ToLower(strings.TrimSpace(hash))
	libraryCopies.mu.Lock()
	entry := libraryCopies.active[key]
	entry.count++
	if entry.count == 1 {
		entry.started = time.Now()
		entry.target = target
	}
	libraryCopies.active[key] = entry
	libraryCopies.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			libraryCopies.mu.Lock()
			if entry, ok := libraryCopies.active[key]; ok {
				entry.count--
				if entry.count <= 0 {
					delete(libraryCopies.active, key)
				} else {
					libraryCopies.active[key] = entry
				}
			}
			libraryCopies.mu.Unlock()
		})
	}
}

// libraryCopyInProgress reports whether Gextto is copying this torrent's files
// into the library right now.
func libraryCopyInProgress(hash string) bool {
	libraryCopies.mu.Lock()
	defer libraryCopies.mu.Unlock()
	_, ok := libraryCopies.active[strings.ToLower(strings.TrimSpace(hash))]
	return ok
}
