//go:build linux

package filestorage

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

func openMmapForCloseTest(t *testing.T) *mmapFile {
	t.Helper()
	s, err := New(t.TempDir(), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	s.Mmap = true
	f, _, err := s.Open("mapped.bin", 4096)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := f.(*mmapFile)
	if !ok {
		t.Fatalf("storage is %T, want *mmapFile", f)
	}
	return m
}

// TestMmapCloseWaitsForActiveIO holds the mapping's read side while Close runs.
// Every real ReadAt, WriteAt and Sync holds the same read side for its whole
// access, so an exclusive Close cannot unmap under any of them.
func TestMmapCloseWaitsForActiveIO(t *testing.T) {
	m := openMmapForCloseTest(t)
	m.ioMu.RLock()
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()

	select {
	case err := <-closed:
		t.Fatalf("Close returned during active mapping IO: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	m.ioMu.RUnlock()

	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not finish after mapping IO ended")
	}
	if err := m.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := m.ReadAt(make([]byte, 1), 0); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("ReadAt after Close = %v, want os.ErrClosed", err)
	}
	if _, err := m.WriteAt([]byte{1}, 0); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("WriteAt after Close = %v, want os.ErrClosed", err)
	}
	if err := m.Sync(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Sync after Close = %v, want os.ErrClosed", err)
	}
}

// TestMmapConcurrentIOAndClose is the race-detector regression for unmapping a
// file while readers, writers or a ranged Sync still use its data slice. All
// operations may finish before Close or lose the race to it; the only acceptable
// post-close outcome is os.ErrClosed, never a fault or a torn mapping access.
func TestMmapConcurrentIOAndClose(t *testing.T) {
	m := openMmapForCloseTest(t)
	size := len(m.data)
	const (
		workers = 4
		rounds  = 256
	)
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(workers)
	errC := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		go func(worker int) {
			ready.Done()
			<-start
			buf := make([]byte, 32)
			for i := 0; i < rounds; i++ {
				off := int64((worker*37 + i*64) % (size - len(buf)))
				if _, err := m.ReadAt(buf, off); err != nil && !errors.Is(err, os.ErrClosed) {
					errC <- err
					return
				}
				if _, err := m.WriteAt(buf, off); err != nil && !errors.Is(err, os.ErrClosed) {
					errC <- err
					return
				}
				if err := m.Sync(); err != nil && !errors.Is(err, os.ErrClosed) {
					errC <- err
					return
				}
			}
			errC <- nil
		}(worker)
	}
	ready.Wait()
	close(start)
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for worker := 0; worker < workers; worker++ {
		select {
		case err := <-errC:
			if err != nil {
				t.Fatalf("concurrent mapping IO: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent mapping IO did not finish")
		}
	}
}

// TestMmapSyncAndCloseOrdering keeps a ranged flush active through the same
// read lock as data access. The dirty snapshot is private to Sync, while Close
// waits for msync before discarding the mapping and its pending ranges.
func TestMmapSyncAndCloseOrdering(t *testing.T) {
	m := openMmapForCloseTest(t)
	if _, err := m.WriteAt([]byte("durable"), 0); err != nil {
		t.Fatal(err)
	}
	// Hold the mapping read side as Sync does, then let the real Sync take its
	// own read side and snapshot the dirty ranges.
	m.ioMu.RLock()
	synced := make(chan error, 1)
	go func() { synced <- m.Sync() }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		m.mu.Lock()
		pending := len(m.dirty)
		m.mu.Unlock()
		if pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Sync did not take the dirty-range snapshot")
		}
		time.Sleep(time.Millisecond)
	}
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned during Sync: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	m.ioMu.RUnlock()
	select {
	case err := <-synced:
		if err != nil {
			t.Fatalf("Sync: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Sync did not finish")
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not finish after Sync")
	}
	if len(m.dirty) != 0 {
		t.Fatalf("Close left %d dirty ranges", len(m.dirty))
	}
}

func TestMmapClosedReadAtReportsClosedBeforeBounds(t *testing.T) {
	m := openMmapForCloseTest(t)
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReadAt(nil, -1); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("ReadAt after Close = %v, want os.ErrClosed", err)
	}
	if _, err := m.ReadAt(nil, 0); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("empty ReadAt after Close = %v, want os.ErrClosed", err)
	}
	if _, err := m.WriteAt(nil, -1); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("WriteAt after Close = %v, want os.ErrClosed", err)
	}
}
