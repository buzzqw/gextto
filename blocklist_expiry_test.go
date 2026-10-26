package gextto

// blocklist_expiry_test.go covers the temporary block of a release abandoned
// for a dead swarm: the swarm can revive, so the block must expire and the
// release must return among the candidates. The operator's own blocklist
// entries stay forever.

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func expiryTestRelease() models.Release {
	series := "Example"
	season := int64(1)
	return models.Release{
		Title:  "Example.S01E01.2160p",
		Magnet: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
		Kind:   "series",
		Series: &series,
		Season: &season,
	}
}

func TestBlocklistUntilExpiresAndComesBack(t *testing.T) {
	db := newTestDB(t)
	release := expiryTestRelease()
	hash := "0123456789abcdef0123456789abcdef01234567"

	if err := db.BlocklistUntil(&release, "dead_swarm", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("BlocklistUntil: %v", err)
	}
	if blocked, err := db.IsBlocklisted(hash); err != nil || !blocked {
		t.Fatalf("a block that is still valid must count: blocked=%v err=%v", blocked, err)
	}
	hashes, err := db.BlocklistedHashes()
	if err != nil {
		t.Fatalf("BlocklistedHashes: %v", err)
	}
	if _, ok := hashes[hash]; !ok {
		t.Fatal("the cycle snapshot must contain the still valid block")
	}
	entries, err := db.BlocklistEntries(10)
	if err != nil {
		t.Fatalf("BlocklistEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	entry, _ := entries[0].(map[string]any)
	if entry["expires_at"] == "" {
		t.Fatal("the entry must carry its expiry so the page can show it")
	}

	// An entry whose expiry has passed no longer blocks anything, and the cycle
	// snapshot removes it so the release is free to be chosen again.
	if err := db.BlocklistUntil(&release, "dead_swarm", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("BlocklistUntil (past): %v", err)
	}
	if blocked, err := db.IsBlocklisted(hash); err != nil || blocked {
		t.Fatalf("an expired block must not count: blocked=%v err=%v", blocked, err)
	}
	hashes, err = db.BlocklistedHashes()
	if err != nil {
		t.Fatalf("BlocklistedHashes: %v", err)
	}
	if _, ok := hashes[hash]; ok {
		t.Fatal("the cycle snapshot still contains an expired block")
	}
	entries, err = db.BlocklistEntries(10)
	if err != nil {
		t.Fatalf("BlocklistEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %d after expiry, want 0", len(entries))
	}
	var rows int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM blocklist").Scan(&rows); err != nil {
		t.Fatalf("count blocklist: %v", err)
	}
	if rows != 0 {
		t.Fatalf("expired rows left in the table: %d", rows)
	}
}

func TestBlocklistWithoutExpiryStaysForever(t *testing.T) {
	db := newTestDB(t)
	release := expiryTestRelease()
	hash := "0123456789abcdef0123456789abcdef01234567"
	if err := db.Blocklist(&release, "manual"); err != nil {
		t.Fatalf("Blocklist: %v", err)
	}
	db.purgeExpiredBlocklist(time.Now().Add(365 * 24 * time.Hour))
	if blocked, err := db.IsBlocklisted(hash); err != nil || !blocked {
		t.Fatalf("a permanent block must survive the purge: blocked=%v err=%v", blocked, err)
	}
}

func TestDeadSwarmBlocklistTTLFollowsTheSetting(t *testing.T) {
	cfg := &Config{Settings: map[string]string{}}
	if got := tev_deadSwarmBlocklistTTL(cfg); got != 7*24*time.Hour {
		t.Fatalf("default TTL = %s, want 7 days", got)
	}
	cfg.Settings["torrent_dead_swarm_blocklist_days"] = "0"
	if got := tev_deadSwarmBlocklistTTL(cfg); got != 0 {
		t.Fatalf("zero must keep the release blocked forever, got %s", got)
	}
	cfg.Settings["torrent_dead_swarm_blocklist_days"] = "30"
	if got := tev_deadSwarmBlocklistTTL(cfg); got != 30*24*time.Hour {
		t.Fatalf("configured TTL = %s, want 30 days", got)
	}
}

// A database that already holds dead-swarm blocks from an older version gets
// the default expiry on the next start: an upgrade must not leave those
// releases blocked forever.
func TestReopeningAddsTheExpiryToOldDeadSwarmBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gextto.db")
	db, err := OpenDatabase(path)
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	release := expiryTestRelease()
	if err := db.Blocklist(&release, "dead_swarm"); err != nil {
		t.Fatalf("Blocklist: %v", err)
	}
	if err := db.db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	reopened, err := OpenDatabase(path)
	if err != nil {
		t.Fatalf("OpenDatabase (reopen): %v", err)
	}
	t.Cleanup(func() { _ = reopened.db.Close() })
	entries, err := reopened.BlocklistEntries(10)
	if err != nil {
		t.Fatalf("BlocklistEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	entry, _ := entries[0].(map[string]any)
	if entry["expires_at"] == "" {
		t.Fatal("the old dead-swarm block was not given an expiry")
	}
}

// The blocklist page must say when a temporary block ends, and that a manual
// one has no end, so the operator can tell the two apart.
func TestBlocklistPageShowsTheExpiry(t *testing.T) {
	state := newTestAppState(t)
	expiry := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	temporary := expiryTestRelease()
	if err := state.db.BlocklistUntil(&temporary, "dead_swarm", expiry); err != nil {
		t.Fatalf("BlocklistUntil: %v", err)
	}
	permanent := expiryTestRelease()
	permanent.Title = "Example.S01E02.1080p"
	permanent.Magnet = "magnet:?xt=urn:btih:fedcba9876543210fedcba9876543210fedcba98"
	if err := state.db.Blocklist(&permanent, "manual"); err != nil {
		t.Fatalf("Blocklist: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, body := v2Request(t, server, http.MethodGet, "/?view=blocklist", nil)
	if code != http.StatusOK {
		t.Fatalf("blocklist page -> %d", code)
	}
	if !strings.Contains(body, "Scade") {
		t.Fatal("the blocklist page does not show the expiry column")
	}
	if !strings.Contains(body, "per sempre") {
		t.Fatal("a manual block must read as permanent")
	}
	if !strings.Contains(body, v2CompactDate("2030-01-02 03:04:05")) {
		t.Fatal("the temporary block does not show its expiry date")
	}
}
