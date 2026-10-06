package gextto

import (
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// approvalRowCounts snapshots the tables an approval may write.
func approvalRowCounts(t *testing.T, db *Database) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, table := range []string{"series", "episodes", "movies", "upgrade_backup"} {
		var n int
		if err := db.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		counts[table] = n
	}
	return counts
}

func assertSameCounts(t *testing.T, label string, before, after map[string]int) {
	t.Helper()
	for table, n := range before {
		if after[table] != n {
			t.Fatalf("%s wrote to %s: %d rows before, %d after", label, table, n, after[table])
		}
	}
}

// The acquisition loop decides first and writes only after the torrent is in
// the engine: the evaluation must not touch the database, the commit must
// reach the same decision and write it.
func TestApprovalEvaluationWritesNothingAndCommitRecords(t *testing.T) {
	pack := testRelease()
	pack.Title = "Example.S01.Pack"
	pack.Magnet = "magnet:?xt=urn:btih:abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	pack.IsPack = true
	pack.EpisodeRange = []int64{1, 2, 3}
	movie := models.Release{
		Title:   "Example Movie",
		Magnet:  "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Source:  "rss",
		Quality: models.Quality{Resolution: "1080p", Source: "webdl"},
		Kind:    "movie",
		Year:    int64Ptr(2024),
		Seeders: -1,
		Peers:   -1,
	}
	for name, release := range map[string]models.Release{"episode": testRelease(), "pack": pack, "movie": movie} {
		t.Run(name, func(t *testing.T) {
			db := newTestDB(t)
			score := release.Quality.Score()
			before := approvalRowCounts(t, db)
			approved, reason, err := evaluateReleaseApproval(db, &release, score, 200, &models.ApprovalContext{}, false, true)
			if err != nil || !approved || reason != "approved" {
				t.Fatalf("evaluate: approved=%v reason=%q err=%v", approved, reason, err)
			}
			assertSameCounts(t, "evaluation", before, approvalRowCounts(t, db))

			committed, commitReason, err := commitReleaseApproval(db, &release, score, 200, &models.ApprovalContext{}, false)
			if err != nil || !committed || commitReason != reason {
				t.Fatalf("commit: approved=%v reason=%q err=%v", committed, commitReason, err)
			}
			table := "episodes"
			if release.Kind == "movie" {
				table = "movies"
			}
			if after := approvalRowCounts(t, db); after[table] <= before[table] {
				t.Fatalf("commit wrote no %s row: %+v", table, after)
			}
		})
	}
}

// An upgrade evaluation must leave both the library row and the upgrade backup
// untouched; only the commit replaces the row and saves the backup.
func TestUpgradeEvaluationKeepsLibraryRowUntilCommit(t *testing.T) {
	db := newTestDB(t)
	old := testRelease()
	old.Quality = models.Quality{Resolution: "720p"}
	old.Title = "Example.S01E01.720p"
	if approved, _, err := db.CheckSeries(&old); err != nil || !approved {
		t.Fatalf("initial approval: approved=%v err=%v", approved, err)
	}
	upgrade := testRelease()
	upgrade.Quality = models.Quality{Resolution: "2160p", Source: "webdl"}
	upgrade.Title = "Example.S01E01.2160p.WEB-DL"
	upgrade.Magnet = "magnet:?xt=urn:btih:fedcba9876543210fedcba9876543210fedcba98"
	score := upgrade.Quality.Score()

	before := approvalRowCounts(t, db)
	approved, reason, err := evaluateReleaseApproval(db, &upgrade, score, 1, &models.ApprovalContext{}, false, true)
	if err != nil || !approved || reason != "upgrade" {
		t.Fatalf("evaluate: approved=%v reason=%q err=%v", approved, reason, err)
	}
	assertSameCounts(t, "upgrade evaluation", before, approvalRowCounts(t, db))
	var title string
	if err := db.db.QueryRow("SELECT title FROM episodes WHERE season=1 AND episode=1").Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != old.Title {
		t.Fatalf("evaluation replaced the library row: title = %q", title)
	}

	if committed, commitReason, err := commitReleaseApproval(db, &upgrade, score, 1, &models.ApprovalContext{}, false); err != nil || !committed || commitReason != "upgrade" {
		t.Fatalf("commit: approved=%v reason=%q err=%v", committed, commitReason, err)
	}
	if after := approvalRowCounts(t, db); after["upgrade_backup"] != before["upgrade_backup"]+1 {
		t.Fatalf("commit saved no upgrade backup: %+v", after)
	}
	if err := db.db.QueryRow("SELECT title FROM episodes WHERE season=1 AND episode=1").Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != upgrade.Title {
		t.Fatalf("commit did not apply the upgrade: title = %q", title)
	}
}
