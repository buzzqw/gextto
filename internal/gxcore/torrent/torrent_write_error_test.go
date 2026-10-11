package torrent

import (
	"errors"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/storage"
)

type fakeStorage struct{ dir string }

func (f fakeStorage) Open(string, int64) (storage.File, bool, error) { return nil, false, nil }
func (f fakeStorage) RootDir() string                                { return f.dir }

func TestStorageWriteError(t *testing.T) {
	tr := &torrent{storage: fakeStorage{dir: "/mnt/nas/downloads"}, session: &Session{}}
	cause := errors.New("no space left on device")

	err := tr.storageWriteError(7, cause)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "/mnt/nas/downloads") || !strings.Contains(err.Error(), "piece #7") {
		t.Fatalf("error does not identify the storage/piece: %s", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("wrapped error lost the cause: %v", err)
	}
	if got := tr.session.storageErrors.Load(); got != 1 {
		t.Fatalf("storageErrors = %d, want 1", got)
	}

	// Without a storage the error is returned unchanged (and not counted).
	if got := (&torrent{}).storageWriteError(1, cause); got != cause {
		t.Fatalf("no storage: got %v, want the original error", got)
	}
}
