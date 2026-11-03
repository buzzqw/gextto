package filestorage

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// injectReadErrors makes the first n reads fail with err and records the
// handles each read used.
func injectReadErrors(t *testing.T, n int, err error) *[]*os.File {
	t.Helper()
	oldRead, oldDelays := readFileAt, readRetryDelays
	t.Cleanup(func() { readFileAt, readRetryDelays = oldRead, oldDelays })
	readRetryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	used := []*os.File{}
	readFileAt = func(file *os.File, p []byte, off int64) (int, error) {
		used = append(used, file)
		if len(used) <= n {
			return 0, &os.PathError{Op: "read", Path: file.Name(), Err: err}
		}
		return file.ReadAt(p, off)
	}
	return &used
}

func openTestFile(t *testing.T, content string) *osFile {
	t.Helper()
	s, err := New(t.TempDir(), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := s.Open("a.bin", int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if _, err := f.WriteAt([]byte(content), 0); err != nil {
		t.Fatal(err)
	}
	of, ok := f.(*osFile)
	if !ok {
		t.Fatalf("unexpected file type %T", f)
	}
	return of
}

// TestTransientReadErrorReopensTheFile: an EIO from a network or FUSE mount is
// retried through a fresh open of the same path, and the read succeeds.
func TestTransientReadErrorReopensTheFile(t *testing.T) {
	f := openTestFile(t, "data")
	first := f.file
	used := injectReadErrors(t, 2, syscall.EIO)
	buf := make([]byte, 4)
	n, err := f.ReadAt(buf, 0)
	if err != nil || n != 4 || string(buf) != "data" {
		t.Fatalf("ReadAt = %d, %v, %q; want the data after a reopen", n, err, buf)
	}
	if len(*used) != 3 {
		t.Fatalf("%d reads, want 3 (two failures, one success)", len(*used))
	}
	if (*used)[2] == first {
		t.Fatal("the retry used the old handle instead of a fresh open")
	}
	// The fresh handle keeps working for writes and Sync.
	if _, err := f.WriteAt([]byte("DATA"), 0); err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(filepath.Dir(f.name), "a.bin")); string(data) != "DATA" {
		t.Fatalf("content after a reopen = %q", data)
	}
}

// TestPersistentReadErrorGivesUp: an error that survives every retry is
// returned, so a damaged file still stops the torrent.
func TestPersistentReadErrorGivesUp(t *testing.T) {
	f := openTestFile(t, "data")
	used := injectReadErrors(t, 100, syscall.EIO)
	if _, err := f.ReadAt(make([]byte, 4), 0); err == nil {
		t.Fatal("a read that always fails returned no error")
	}
	if want := 1 + len(readRetryDelays); len(*used) != want {
		t.Fatalf("%d reads, want %d", len(*used), want)
	}
}

// TestOtherReadErrorsAreNotRetried: only transient I/O errors are retried.
func TestOtherReadErrorsAreNotRetried(t *testing.T) {
	f := openTestFile(t, "data")
	used := injectReadErrors(t, 100, syscall.EACCES)
	if _, err := f.ReadAt(make([]byte, 4), 0); err == nil {
		t.Fatal("expected the injected error")
	}
	if len(*used) != 1 {
		t.Fatalf("%d reads, want 1", len(*used))
	}
}
