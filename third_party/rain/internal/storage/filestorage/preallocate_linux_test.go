package filestorage

import (
	"os"
	"syscall"
	"testing"
)

// gextto fork: on tmpfs a preallocated file would take its whole size from
// RAM at once; it must stay sparse.
func TestPreallocateKeepsTmpfsFilesSparse(t *testing.T) {
	dir, err := os.MkdirTemp("/dev/shm", "rain-prealloc-")
	if err != nil {
		t.Skip("no /dev/shm tmpfs")
	}
	defer os.RemoveAll(dir)
	s, err := New(dir, 0o750)
	if err != nil {
		t.Fatal(err)
	}
	s.Preallocate = true
	f, exists, err := s.Open("big.bin", 256<<20)
	if err != nil || exists {
		t.Fatalf("open: exists=%v err=%v", exists, err)
	}
	defer f.Close()
	info, err := os.Stat(dir + "/big.bin")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 256<<20 {
		t.Fatalf("size %d", info.Size())
	}
	if blocks := info.Sys().(*syscall.Stat_t).Blocks; blocks != 0 {
		t.Fatalf("tmpfs file was allocated: %d blocks", blocks)
	}
}
