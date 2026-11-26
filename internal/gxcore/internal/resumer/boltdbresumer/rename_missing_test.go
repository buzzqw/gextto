package boltdbresumer

import (
	"errors"
	"path/filepath"
	"testing"

	"go.etcd.io/bbolt"
)

func TestWriteRenameFailsWhenTorrentRecordIsMissing(t *testing.T) {
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "resume.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	resumer, err := New(db, []byte("torrents"))
	if err != nil {
		t.Fatal(err)
	}
	err = resumer.WriteRename("missing", []string{"new.mkv"}, "new")
	if !errors.Is(err, errTorrentRecordMissing) {
		t.Fatalf("WriteRename error = %v, want a missing-record error", err)
	}
}
