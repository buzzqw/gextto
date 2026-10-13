package torrent

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestNewTorrentIDIsUniqueAndWellFormed(t *testing.T) {
	seen := make(map[string]struct{})
	for range 1000 {
		id, err := newTorrentID()
		if err != nil {
			t.Fatalf("newTorrentID: %v", err)
		}
		if id[6]>>4 != 1 {
			t.Fatalf("version nibble = %x, want 1", id[6]>>4)
		}
		if id[8]>>6 != 2 {
			t.Fatalf("variant bits = %02b, want 10", id[8]>>6)
		}
		s := base64.RawURLEncoding.EncodeToString(id[:])
		if len(s) != 22 {
			t.Fatalf("encoded id %q has length %d, want 22", s, len(s))
		}
		if _, dup := seen[s]; dup {
			t.Fatalf("duplicate id %q", s)
		}
		seen[s] = struct{}{}
	}
}

func TestNewTorrentIDTimestampIsCurrent(t *testing.T) {
	id, err := newTorrentID()
	if err != nil {
		t.Fatalf("newTorrentID: %v", err)
	}
	if dt := time.Since(torrentIDTime(id)); dt < 0 || dt > time.Minute {
		t.Fatalf("embedded timestamp %v is not current", torrentIDTime(id))
	}
}
