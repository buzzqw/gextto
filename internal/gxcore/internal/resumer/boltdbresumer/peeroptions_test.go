package boltdbresumer

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"go.etcd.io/bbolt"
)

// TestPeerOptionsRoundTrip pins the per-torrent bandwidth weight and the
// DHT/PEX switches through the bolt record and the move-torrent payload,
// where they are omitted at their defaults.
func TestPeerOptionsRoundTrip(t *testing.T) {
	db, err := bbolt.Open(filepath.Join(t.TempDir(), "resume.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r, err := New(db, []byte("torrents"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Write("t1", &Spec{InfoHash: make([]byte, 20), Name: "x", BandwidthWeight: 250, NoDHT: true}); err != nil {
		t.Fatal(err)
	}
	spec, err := r.Read("t1")
	if err != nil || spec.BandwidthWeight != 250 || !spec.NoDHT || spec.NoPEX {
		t.Fatalf("options lost on the full write: %+v, %v", spec, err)
	}
	if err := r.WritePeerOptions("t1", 0, false, true); err != nil {
		t.Fatal(err)
	}
	if spec, err = r.Read("t1"); err != nil || spec.BandwidthWeight != 0 || spec.NoDHT || !spec.NoPEX {
		t.Fatalf("options not replaced: %+v, %v", spec, err)
	}
	plain, _ := json.Marshal(Spec{Name: "x"})
	if bytes.Contains(plain, []byte("BandwidthWeight")) || bytes.Contains(plain, []byte("NoDHT")) {
		t.Fatalf("default options must not appear in the payload: %s", plain)
	}
	set, _ := json.Marshal(Spec{Name: "x", BandwidthWeight: 300, NoPEX: true})
	var back Spec
	if err := json.Unmarshal(set, &back); err != nil || back.BandwidthWeight != 300 || !back.NoPEX {
		t.Fatalf("options lost in the JSON payload: %s, %v", set, err)
	}
}
