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
}

type libraryCopyRegistry struct {
	mu     sync.Mutex
	active map[string]libraryCopy
}

var libraryCopies = &libraryCopyRegistry{active: map[string]libraryCopy{}}

// beginLibraryCopy records a copy for hash and returns the function that ends
// it. Call the returned function exactly once, whichever way the copy ends.
func beginLibraryCopy(hash, target string) func() {
	key := strings.ToLower(strings.TrimSpace(hash))
	libraryCopies.mu.Lock()
	libraryCopies.active[key] = libraryCopy{started: time.Now(), target: target}
	libraryCopies.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			libraryCopies.mu.Lock()
			delete(libraryCopies.active, key)
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
