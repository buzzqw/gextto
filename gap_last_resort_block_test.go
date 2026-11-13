package gextto

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// lastResortCopy builds the copy of one gap that the blocklist is about to
// refuse: an eD2k file, like the one that stalled and blocked its own episode.
func lastResortCopy(episode int64, digest string) models.Release {
	season := int64(6)
	name := "Series A"
	return models.Release{
		Title:      "Series.A.6x08.ITA.avi",
		Kind:       "series",
		Series:     &name,
		Season:     &season,
		Episode:    &episode,
		Ed2kLink:   "ed2k://|file|Series.A.6x08.ITA.avi|350000000|" + digest + "|/",
		LastResort: true,
		Quality:    models.Quality{Resolution: "480p", Language: "ita", Languages: []string{"ita"}},
	}
}

// TestSplitLastResortsKeepsABlockedCopyOutOfTheCycle: a copy the blocklist
// refuses cannot start, so it must not reach the download pass as if it were
// taken. The reason and the expiry travel with it, for the log.
func TestSplitLastResortsKeepsABlockedCopyOutOfTheCycle(t *testing.T) {
	db := newTestDB(t)
	blocked := lastResortCopy(8, strings.Repeat("A", 32))
	other := lastResortCopy(8, strings.Repeat("B", 32))
	if _, err := db.BlocklistDeadSwarmBecause(&blocked, 7*24*time.Hour, "stalled_ed2k"); err != nil {
		t.Fatalf("blocklist: %v", err)
	}
	hashes, err := db.BlocklistedHashes()
	if err != nil {
		t.Fatalf("blocklisted hashes: %v", err)
	}

	usable, refused := splitLastResorts(db, []models.Release{blocked, other}, hashes)
	if len(usable) != 1 || usable[0].Ed2kLink != other.Ed2kLink {
		t.Fatalf("usable = %+v, want the copy that is not on the blocklist", usable)
	}
	if len(refused) != 1 || refused[0].reason != blocklistReasonText("stalled_ed2k") || refused[0].expiresAt == "" {
		t.Fatalf("refused = %+v, want the stalled eD2k copy with its reason and expiry", refused)
	}
}

// TestBlockedLastResortIsNotAnnouncedAsTaken is the log the operator read: the
// cycle said the copy was taken as a last resort and no download ever started,
// because the same copy was on the blocklist. The line must say what happened
// and what to do.
func TestBlockedLastResortIsNotAnnouncedAsTaken(t *testing.T) {
	var captured bytes.Buffer
	logging.SetOutput(&captured)
	defer logging.SetOutput(os.Stdout)

	until := time.Now().Add(7 * 24 * time.Hour).UTC().Format("2006-01-02 15:04:05")
	logBlockedLastResorts("Series A", 6, 8, []blockedLastResort{
		{reason: blocklistReasonText("stalled_ed2k"), expiresAt: until},
	})
	out := captured.String()
	if strings.Contains(out, "taken as a last resort") {
		t.Fatalf("a blocked copy was announced as taken:\n%s", out)
	}
	for _, want := range []string{"Series A S06E08", "on the blocklist", "the eD2k download received nothing", "will be tried again on"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

// TestBlockedLastResortSaysHowToUnblockIt: a block that stays has no date, so
// the line has to name the way out instead of promising a retry.
func TestBlockedLastResortSaysHowToUnblockIt(t *testing.T) {
	var captured bytes.Buffer
	logging.SetOutput(&captured)
	defer logging.SetOutput(os.Stdout)

	logBlockedLastResorts("Series A", 6, 8, []blockedLastResort{
		{reason: blocklistReasonText("manual")},
		{reason: blocklistReasonText("manual")},
	})
	out := captured.String()
	for _, want := range []string{"the 2 copies in the right language are", "you added it by hand", "remove them from the blocklist to try them again"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "you added it by hand") != 1 {
		t.Fatalf("the same reason was repeated:\n%s", out)
	}
}

// TestStalledEd2kBlockExpires is the other half of the Walker case: the only
// copy of the gap was the eD2k file that had stalled, blocked for good, so
// every search found it and refused it for ever. It now comes back after the
// wait the setting names, and stays away longer if it stalls again.
func TestStalledEd2kBlockExpires(t *testing.T) {
	db := newTestDB(t)
	release := lastResortCopy(8, strings.Repeat("C", 32))
	base := 7 * 24 * time.Hour

	hash, err := releaseIdentityHash(&release)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	period, err := db.BlocklistDeadSwarmBecause(&release, base, "stalled_ed2k")
	if err != nil {
		t.Fatalf("blocklist: %v", err)
	}
	if period != base {
		t.Fatalf("period = %s, want the first refusal to last %s", period, base)
	}
	record, found := db.BlocklistRecordFor(hash)
	if !found {
		t.Fatal("the stalled copy is not on the blocklist")
	}
	if record.Reason != "stalled_ed2k" || record.ExpiresAt == "" {
		t.Fatalf("record = %+v, want the stalled eD2k reason and an expiry", record)
	}
	if again, _ := db.BlocklistDeadSwarmBecause(&release, base, "stalled_ed2k"); again != 4*base {
		t.Fatalf("second refusal = %s, want four times the wait", again)
	}
}

// TestReopeningGivesTheOldStalledEd2kBlockAnExpiry: before this fix the eD2k
// file that stalled was blocked for good, and an installation that already has
// such an entry must not keep its gap unfillable after the upgrade.
func TestReopeningGivesTheOldStalledEd2kBlockAnExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gextto.db")
	db, err := OpenDatabase(path)
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	release := lastResortCopy(8, strings.Repeat("D", 32))
	if err := db.Blocklist(&release, "stalled_ed2k"); err != nil {
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
	hash, err := releaseIdentityHash(&release)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	record, found := reopened.BlocklistRecordFor(hash)
	if !found {
		t.Fatal("the stalled eD2k entry is gone")
	}
	if record.ExpiresAt == "" {
		t.Fatal("the old stalled eD2k block was not given an expiry")
	}
}

// TestBlocklistReasonIsReadableText keeps the log free of the internal names
// saved in the blocklist.
func TestBlocklistReasonIsReadableText(t *testing.T) {
	for reason, want := range map[string]string{
		"stalled_ed2k":             "the eD2k download received nothing",
		"dead_swarm":               "nobody was sharing it",
		"corrupt_file":             "the downloaded file was damaged",
		"manual":                   "you added it by hand",
		"":                         "the reason is not recorded",
		"a reason saved by a rule": "a reason saved by a rule",
	} {
		if got := blocklistReasonText(reason); got != want {
			t.Fatalf("blocklistReasonText(%q) = %q, want %q", reason, got, want)
		}
	}
}
