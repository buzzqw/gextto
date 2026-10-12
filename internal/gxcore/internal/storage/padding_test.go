package storage

import "testing"

// A padding file is never written, so its Sync is a no-op that must not panic.
func TestPaddingFileSync(t *testing.T) {
	f := NewPaddingFile(10)
	if err := f.Sync(); err != nil {
		t.Fatalf("PaddingFile.Sync = %v, want nil", err)
	}
}
