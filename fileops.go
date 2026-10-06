package gextto

import (
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Copies and moves of media files must never be cut halfway: a stop in the
// middle of one leaves a partial file in the library (or a half-moved torrent
// whose retry then fails because the destination already exists). Every
// long-running file operation registers itself here, and shutdown waits until
// the registry is empty before tearing anything down.

type fileOperation struct {
	what    string
	started time.Time
}

type fileOperationRegistry struct {
	mu     sync.Mutex
	nextID uint64
	active map[uint64]fileOperation
	// closed is set by shutdown once nothing is running. A copy that would
	// start afterwards blocks instead, so the exiting process can never leave
	// it half done.
	closed bool
	// blockWhenClosed makes begin wait forever once closed (the daemon); tests
	// that stop and restart workers in one process turn it off.
	blockWhenClosed bool
}

var mediaFileOps = &fileOperationRegistry{active: map[uint64]fileOperation{}, blockWhenClosed: true}

// beginFileOperation records a copy/move of path in progress; call the
// returned function exactly once when it finishes (successfully or not).
func beginFileOperation(path string) func() {
	return mediaFileOps.begin(filepath.Base(path))
}

func (r *fileOperationRegistry) begin(what string) func() {
	r.mu.Lock()
	if r.closed && r.blockWhenClosed {
		r.mu.Unlock()
		select {} // shutting down: never start a copy the exit would cut
	}
	r.nextID++
	id := r.nextID
	r.active[id] = fileOperation{what: what, started: time.Now()}
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			delete(r.active, id)
			r.mu.Unlock()
		})
	}
}

// running returns the operations in progress, oldest first.
func (r *fileOperationRegistry) running() []fileOperation {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := make([]fileOperation, 0, len(r.active))
	for _, operation := range r.active {
		list = append(list, operation)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].started.Before(list[j].started) })
	return list
}

// runningNames returns the file names being copied or moved, oldest first.
func (r *fileOperationRegistry) runningNames() []string {
	operations := r.running()
	names := make([]string, 0, len(operations))
	for _, operation := range operations {
		names = append(names, operation.what)
	}
	return names
}

// closeIfIdle closes the registry to new operations when none is running and
// reports whether it did. It is atomic with begin, so no copy can slip in
// between the check and the close.
func (r *fileOperationRegistry) closeIfIdle() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.active) > 0 {
		return false
	}
	r.closed = true
	return true
}
