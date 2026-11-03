package gextto

// dead_swarm_block_test.go covers the period a release stays away when nobody
// is sharing it: the first refusal lasts the configured period, and a release
// that comes back and is refused again lasts longer. The refusal count lives
// in its own table because the blocklist row is deleted when the block
// expires.

import (
	"testing"
	"time"
)

func TestDeadSwarmBlockPeriodEscalates(t *testing.T) {
	day := 24 * time.Hour
	cases := []struct {
		base    time.Duration
		strikes int
		want    time.Duration
	}{
		{7 * day, 1, 7 * day},
		{7 * day, 2, 28 * day},
		{7 * day, 3, 84 * day},
		{7 * day, 9, 84 * day},
		{30 * day, 1, 30 * day},
		{30 * day, 2, 90 * day}, // the cap, not four times the base
		{120 * day, 2, 120 * day},
		{0, 1, 0},                     // zero means forever
		{0, 5, 0},                     // and stays forever
		{36500 * day, 3, 36500 * day}, // an absurd base does not overflow
	}
	for _, tc := range cases {
		if got := deadSwarmBlockPeriod(tc.base, tc.strikes); got != tc.want {
			t.Errorf("deadSwarmBlockPeriod(%s, %d) = %s, want %s", tc.base, tc.strikes, got, tc.want)
		}
	}
}

func TestDeadSwarmBlockGrowsOnTheSecondRefusal(t *testing.T) {
	db := newTestDB(t)
	release := expiryTestRelease()
	hash := "0123456789abcdef0123456789abcdef01234567"
	day := 24 * time.Hour

	period, err := db.BlocklistDeadSwarm(&release, 7*day)
	if err != nil {
		t.Fatalf("BlocklistDeadSwarm: %v", err)
	}
	if period != 7*day {
		t.Fatalf("the first refusal = %s, want 7 days", period)
	}
	expiry := blocklistExpiry(t, db, "dead_swarm")
	if !expiry.After(time.Now().Add(6*day)) || expiry.After(time.Now().Add(8*day)) {
		t.Fatalf("the first block expires at %s, want about 7 days from now", expiry)
	}

	// Let the block expire: the release must come back among the candidates,
	// while the refusal count stays.
	if _, err := db.db.Exec("UPDATE blocklist SET expires_at = ?1", sqliteTimestamp(time.Now().Add(-time.Minute))); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if _, err := db.BlocklistedHashes(); err != nil {
		t.Fatalf("BlocklistedHashes: %v", err)
	}
	var rows int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM blocklist").Scan(&rows); err != nil {
		t.Fatalf("count blocklist: %v", err)
	}
	if rows != 0 {
		t.Fatalf("the expired block was not removed: %d rows", rows)
	}
	if strikes, err := db.DeadSwarmStrikes(hash); err != nil || strikes != 1 {
		t.Fatalf("the refusal count must survive the expiry: strikes=%d err=%v", strikes, err)
	}

	period, err = db.BlocklistDeadSwarm(&release, 7*day)
	if err != nil {
		t.Fatalf("BlocklistDeadSwarm (second): %v", err)
	}
	if period != 28*day {
		t.Fatalf("the second refusal = %s, want 28 days", period)
	}
	expiry = blocklistExpiry(t, db, "dead_swarm")
	if !expiry.After(time.Now().Add(27*day)) || expiry.After(time.Now().Add(29*day)) {
		t.Fatalf("the second block expires at %s, want about 28 days from now", expiry)
	}
	if strikes, err := db.DeadSwarmStrikes(hash); err != nil || strikes != 2 {
		t.Fatalf("strikes=%d err=%v, want 2", strikes, err)
	}
}

func TestDeadSwarmStrikesAreForgottenAfterTheRetention(t *testing.T) {
	db := newTestDB(t)
	release := expiryTestRelease()
	hash := "0123456789abcdef0123456789abcdef01234567"
	if _, err := db.BlocklistDeadSwarm(&release, 7*24*time.Hour); err != nil {
		t.Fatalf("BlocklistDeadSwarm: %v", err)
	}
	if _, err := db.db.Exec("UPDATE dead_swarm_strikes SET last_at = ?1", sqliteTimestamp(time.Now().Add(-deadSwarmStrikeRetention-time.Hour))); err != nil {
		t.Fatalf("age the strikes: %v", err)
	}
	db.purgeDeadSwarmStrikes(time.Now())
	if strikes, err := db.DeadSwarmStrikes(hash); err != nil || strikes != 0 {
		t.Fatalf("an old refusal count must be forgotten: strikes=%d err=%v", strikes, err)
	}

	// A recent count stays.
	if _, err := db.BlocklistDeadSwarm(&release, 7*24*time.Hour); err != nil {
		t.Fatalf("BlocklistDeadSwarm (again): %v", err)
	}
	db.purgeDeadSwarmStrikes(time.Now())
	if strikes, err := db.DeadSwarmStrikes(hash); err != nil || strikes != 1 {
		t.Fatalf("a recent refusal count must stay: strikes=%d err=%v", strikes, err)
	}
}

// blocklistExpiry returns the expiry of the only blocklist entry with reason.
func blocklistExpiry(t *testing.T, db *Database, reason string) time.Time {
	t.Helper()
	var raw string
	if err := db.db.QueryRow("SELECT COALESCE(expires_at, '') FROM blocklist WHERE reason = ?1", reason).Scan(&raw); err != nil {
		t.Fatalf("read expiry: %v", err)
	}
	parsed, err := time.Parse("2006-01-02 15:04:05", raw)
	if err != nil {
		t.Fatalf("parse expiry %q: %v", raw, err)
	}
	return parsed
}

// A release that reached the library is not a dead swarm any more: the count
// starts from the first period again on the next refusal.
func TestCompletedReleaseForgetsItsRefusals(t *testing.T) {
	db := newTestDB(t)
	release := expiryTestRelease()
	hash := "0123456789abcdef0123456789abcdef01234567"
	day := 24 * time.Hour

	if _, err := db.BlocklistDeadSwarm(&release, 7*day); err != nil {
		t.Fatalf("BlocklistDeadSwarm: %v", err)
	}
	if _, err := db.BlocklistDeadSwarm(&release, 7*day); err != nil {
		t.Fatalf("BlocklistDeadSwarm (second): %v", err)
	}
	if strikes, err := db.DeadSwarmStrikes(hash); err != nil || strikes != 2 {
		t.Fatalf("strikes=%d err=%v, want 2", strikes, err)
	}
	if err := db.MarkReleaseCompleted(&release, "/library/Example.S01E01.mkv", 1000); err != nil {
		t.Fatalf("MarkReleaseCompleted: %v", err)
	}
	if strikes, err := db.DeadSwarmStrikes(hash); err != nil || strikes != 0 {
		t.Fatalf("a completed release kept its refusal count: strikes=%d err=%v", strikes, err)
	}
	period, err := db.BlocklistDeadSwarm(&release, 7*day)
	if err != nil {
		t.Fatalf("BlocklistDeadSwarm (after completion): %v", err)
	}
	if period != 7*day {
		t.Fatalf("the first period after a completion = %s, want 7 days", period)
	}
}
