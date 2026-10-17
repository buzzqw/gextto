package gextto

import (
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// TestPackIdentityVerdictIsReused pins that a consistent season pack is not
// re-read from the database on every tick, while a new torrent name (the
// metadata arriving with a different season) is checked at once and rejected.
func TestPackIdentityVerdictIsReused(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "gextto.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	hash := "0123456789abcdef0123456789abcdef01234567"
	release := ParseRelease("Slow.Horses.S06E01-06.1080p.WEB-DL.ITA", "magnet:?xt=urn:btih:"+hash, "test")
	if release == nil || !release.IsPack {
		t.Fatalf("release = %#v, want a season pack", release)
	}
	if err := db.RegisterTorrentScored(release, 0); err != nil {
		t.Fatal(err)
	}
	session := &stubTorrentSession{list: []models.TorrentView{{Hash: hash, Name: "Slow Horses S06E01-06 1080p"}}}
	checked := map[string]packIdentityCheck{}

	RejectActivePackIdentityMismatches(session, db, checked)
	if len(session.removed) != 0 {
		t.Fatalf("a consistent pack was removed: %v", session.removed)
	}
	if _, ok := checked[hash]; !ok {
		t.Fatal("the consistent verdict was not remembered")
	}
	// The metadata arrives with another season: the new name is checked now.
	session.list[0].Name = "Slow Horses S05e01-06 (1080p Ita Eng Spa h265 10bit SubS)"
	RejectActivePackIdentityMismatches(session, db, checked)
	if len(session.removed) != 1 || session.removed[0] != hash {
		t.Fatalf("a mismatched pack must be removed at once: %v", session.removed)
	}
	if _, ok := checked[hash]; ok {
		t.Fatal("a mismatched pack must not keep a consistent verdict")
	}
	// A torrent that left the session is forgotten.
	session.list = nil
	checked["gone"] = packIdentityCheck{}
	RejectActivePackIdentityMismatches(session, db, checked)
	if len(checked) != 0 {
		t.Fatalf("stale verdicts kept: %v", checked)
	}
}
