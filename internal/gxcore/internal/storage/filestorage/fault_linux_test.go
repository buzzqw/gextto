package filestorage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestMappedFaultIsAnError: touching a page the file no longer backs (here the
// file is truncated under the mapping, as a full disk does to a sparse file)
// raises SIGBUS; it must come back as an I/O error, not crash the process.
func TestMappedFaultIsAnError(t *testing.T) {
	const size = 1 << 20
	path := filepath.Join(t.TempDir(), "f")
	of, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := of.Truncate(size); err != nil {
		t.Fatal(err)
	}
	f, ok := (&FileStorage{Mmap: true}).wrap(of, size).(*mmapFile)
	if !ok {
		t.Skip("mmap not available")
	}
	defer f.Close()
	if err := of.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("x"), size/2); !errors.Is(err, errMappedFault) {
		t.Fatalf("write err = %v, want the fault as an error", err)
	}
	buf := make([]byte, 16)
	if _, err := f.ReadAt(buf, size/2); !errors.Is(err, errMappedFault) {
		t.Fatalf("read err = %v, want the fault as an error", err)
	}
}
