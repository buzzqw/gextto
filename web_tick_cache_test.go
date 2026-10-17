package gextto

import (
	"path/filepath"
	"testing"
	"time"

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

// TestDetachErrorTorrentsReusesTheNotErrorVerdict pins that a startup torrent
// found not in error is not queried again within notErrorRecheckInterval, and
// that a torrent in error is still detached.
func TestDetachErrorTorrentsReusesTheNotErrorVerdict(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "gextto.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	hash := "0123456789abcdef0123456789abcdef01234567"
	release := ParseRelease("Some.Show.S01E01.1080p.WEB-DL", "magnet:?xt=urn:btih:"+hash, "test")
	if err := db.RegisterTorrentScored(release, 0); err != nil {
		t.Fatal(err)
	}
	session := &stubTorrentSession{list: []models.TorrentView{{Hash: hash, Name: "Some Show"}}}
	startup := map[string]struct{}{hash: {}}
	notError := map[string]time.Time{}

	DetachErrorTorrents(session, db, startup, notError)
	if len(session.removed) != 0 || notError[hash].IsZero() {
		t.Fatalf("a healthy torrent: removed=%v, verdict=%v", session.removed, notError[hash])
	}
	if err := db.MarkTorrentError(hash, "boom"); err != nil {
		t.Fatal(err)
	}
	DetachErrorTorrents(session, db, startup, notError)
	if len(session.removed) != 0 {
		t.Fatal("within the recheck window the cached verdict must be reused")
	}
	notError[hash] = time.Now().Add(-time.Second)
	DetachErrorTorrents(session, db, startup, notError)
	if len(session.removed) != 1 {
		t.Fatalf("an error torrent must be detached once the verdict expires: %v", session.removed)
	}
}

// TestCalendarCacheKeyFollowsTheSeries pins that the 15-minute calendar cache
// is rebuilt at once when the enabled series change.
func TestCalendarCacheKeyFollowsTheSeries(t *testing.T) {
	cfg := &Config{Series: []SeriesConfig{{Name: "A", Enabled: true}, {Name: "B", Enabled: false}}}
	base := calendarCacheKey(cfg)
	if calendarCacheKey(cfg) != base {
		t.Fatal("the key must be stable for the same series")
	}
	cfg.Series[1].Enabled = true
	if calendarCacheKey(cfg) == base {
		t.Fatal("enabling a series must change the key")
	}
	cfg.Series[1].Enabled = false
	cfg.Series[0].TmdbID = "123"
	if calendarCacheKey(cfg) == base {
		t.Fatal("a new provider id must change the key")
	}
}
