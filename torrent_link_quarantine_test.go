package gextto

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func openQuarantineDB(t *testing.T) *Database {
	t.Helper()
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "series.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestTorrentLinkQuarantineNeedsTwoCycles(t *testing.T) {
	db := openQuarantineDB(t)
	const link = "http://192.168.1.161:9118/dl/9748.torrent?k=abc"
	now := time.Now()

	failures, err := db.RecordTorrentLinkFailure(link, "HTTP 502", now)
	if err != nil || failures != 1 {
		t.Fatalf("first failure = %d, %v; want 1, nil", failures, err)
	}
	// The same release proposed twice in one cycle (archive + feed) is still a
	// single strike: a transient 502 must not quarantine a working link.
	failures, err = db.RecordTorrentLinkFailure(link, "HTTP 502", now.Add(time.Minute))
	if err != nil || failures != 1 {
		t.Fatalf("same-cycle failure = %d, %v; want 1, nil", failures, err)
	}
	quarantined, err := db.QuarantinedTorrentLinks(now)
	if err != nil {
		t.Fatalf("quarantined links: %v", err)
	}
	if reason, dead := quarantined[link]; dead {
		t.Fatalf("link quarantined after one strike (reason %q)", reason)
	}

	failures, err = db.RecordTorrentLinkFailure(link, "HTTP 502", now.Add(6*time.Hour))
	if err != nil || failures != torrentLinkFailureLimit {
		t.Fatalf("second-cycle failure = %d, %v; want %d", failures, err, torrentLinkFailureLimit)
	}
	quarantined, err = db.QuarantinedTorrentLinks(now.Add(6 * time.Hour))
	if err != nil {
		t.Fatalf("quarantined links: %v", err)
	}
	if reason, dead := quarantined[link]; !dead || reason != "HTTP 502" {
		t.Fatalf("link not quarantined after two strikes: dead=%v reason=%q", dead, reason)
	}

	// The first quarantine is short: a repaired indexer is retried the same day.
	quarantined, err = db.QuarantinedTorrentLinks(now.Add(6*time.Hour + 7*time.Hour))
	if err != nil {
		t.Fatalf("quarantined links: %v", err)
	}
	if _, dead := quarantined[link]; dead {
		t.Fatal("the 6h quarantine did not expire")
	}
}

func TestTorrentLinkQuarantineLadder(t *testing.T) {
	if got := torrentLinkQuarantine(1); got != 0 {
		t.Fatalf("one strike = %v, want 0", got)
	}
	if got := torrentLinkQuarantine(2); got != 6*time.Hour {
		t.Fatalf("two strikes = %v, want 6h", got)
	}
	if got := torrentLinkQuarantine(3); got != 24*time.Hour {
		t.Fatalf("three strikes = %v, want 24h", got)
	}
	if got := torrentLinkQuarantine(9); got != 7*24*time.Hour {
		t.Fatalf("many strikes = %v, want 7 days", got)
	}
}

func TestTorrentLinkClearedWhenItWorks(t *testing.T) {
	db := openQuarantineDB(t)
	const link = "http://host/dl/1.torrent"
	now := time.Now()
	if _, err := db.RecordTorrentLinkFailure(link, "HTTP 502", now); err != nil {
		t.Fatalf("first failure: %v", err)
	}
	if _, err := db.RecordTorrentLinkFailure(link, "HTTP 502", now.Add(6*time.Hour)); err != nil {
		t.Fatalf("second failure: %v", err)
	}
	quarantined, err := db.QuarantinedTorrentLinks(now.Add(6 * time.Hour))
	if err != nil {
		t.Fatalf("quarantined links: %v", err)
	}
	if _, dead := quarantined[link]; !dead {
		t.Fatal("link should be quarantined before it is cleared")
	}
	if err := db.ClearTorrentLinkFailure(link); err != nil {
		t.Fatalf("clear: %v", err)
	}
	quarantined, err = db.QuarantinedTorrentLinks(now.Add(6 * time.Hour))
	if err != nil {
		t.Fatalf("quarantined links: %v", err)
	}
	if _, dead := quarantined[link]; dead {
		t.Fatal("a link that works was not cleared")
	}
}

func TestTorrentLinkFailureIgnoresAnEmptyURL(t *testing.T) {
	db := openQuarantineDB(t)
	if failures, err := db.RecordTorrentLinkFailure("   ", "HTTP 502", time.Now()); err != nil || failures != 0 {
		t.Fatalf("empty URL = %d, %v; want 0, nil", failures, err)
	}
	if err := db.ClearTorrentLinkFailure(""); err != nil {
		t.Fatalf("clear empty URL: %v", err)
	}
}

// BenchmarkQuarantinedTorrentLinks measures the per-cycle cost of the check,
// with as many quarantined rows as the live archive holds dead links today.
func BenchmarkQuarantinedTorrentLinks(b *testing.B) {
	db, err := OpenDatabase(filepath.Join(b.TempDir(), "series.db"))
	if err != nil {
		b.Fatalf("open database: %v", err)
	}
	defer db.Close()
	now := time.Now()
	for i := 0; i < 300; i++ {
		url := "http://host/dl/" + strconv.Itoa(i) + ".torrent?k=x"
		if _, err := db.RecordTorrentLinkFailure(url, "HTTP 502", now.Add(-time.Duration(i)*time.Minute)); err != nil {
			b.Fatal(err)
		}
		if _, err := db.RecordTorrentLinkFailure(url, "HTTP 502", now.Add(-time.Duration(i)*time.Minute+time.Hour)); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := db.QuarantinedTorrentLinks(now); err != nil {
			b.Fatal(err)
		}
	}
}
