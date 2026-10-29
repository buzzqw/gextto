package boltdbresumer

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"go.etcd.io/bbolt"
)

// TestSeedModeRoundTrip pins the seed-mode flag through the bolt record
// (full write, single-flag write, read) and the JSON move-torrent payload,
// where it is omitted when off so the payload of every other torrent is
// unchanged.
func TestSeedModeRoundTrip(t *testing.T) {
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "resume.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r, err := New(db, []byte("torrents"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Write("t1", &Spec{InfoHash: make([]byte, 20), Name: "x", SeedMode: true}); err != nil {
		t.Fatal(err)
	}
	spec, err := r.Read("t1")
	if err != nil || !spec.SeedMode {
		t.Fatalf("seed mode lost on the full write: %+v, %v", spec, err)
	}
	if err := r.WriteSeedMode("t1", false); err != nil {
		t.Fatal(err)
	}
	if spec, err = r.Read("t1"); err != nil || spec.SeedMode {
		t.Fatalf("seed mode not cleared: %+v, %v", spec, err)
	}

	off, err := json.Marshal(Spec{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(off, []byte("SeedMode")) {
		t.Fatalf("an off flag must not appear in the payload: %s", off)
	}
	on, err := json.Marshal(Spec{Name: "x", SeedMode: true})
	if err != nil {
		t.Fatal(err)
	}
	var back Spec
	if err := json.Unmarshal(on, &back); err != nil || !back.SeedMode {
		t.Fatalf("seed mode lost in the JSON payload: %s, %v", on, err)
	}
}
