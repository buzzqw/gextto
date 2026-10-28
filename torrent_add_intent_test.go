package gextto

import (
	"errors"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

const intentTestHash = "0123456789012345678901234567890123456789"

func intentCount(t *testing.T, db *Database) int {
	t.Helper()
	var n int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM torrent_add_intents").Scan(&n); err != nil {
		t.Fatalf("count intents: %v", err)
	}
	return n
}

func hasMetaRow(t *testing.T, db *Database) bool {
	t.Helper()
	has, err := db.hasTorrentMetaRow(intentTestHash)
	if err != nil {
		t.Fatalf("torrent_meta: %v", err)
	}
	return has
}

func hasPlaceholderRow(t *testing.T, db *Database) bool {
	t.Helper()
	has, err := db.hasReleasePlaceholder(intentTestHash)
	if err != nil {
		t.Fatalf("placeholder: %v", err)
	}
	return has
}

func commitTestRelease(db *Database, release *models.Release) func() (bool, string, error) {
	return func() (bool, string, error) {
		return commitReleaseApproval(db, release, release.Quality.Score(), 200, &models.ApprovalContext{}, false)
	}
}

// Whatever happens to the add, its intent is cleared once the outcome is
// recorded, and a failure after the Add leaves neither a torrent nor a row.
func TestStartApprovedReleaseOutcomes(t *testing.T) {
	addOK := func() (bool, error) { return true, nil }
	for name, tc := range map[string]struct {
		add       func() (bool, error)
		commit    func(db *Database, release *models.Release) func() (bool, string, error)
		want      string
		withdrawn bool
		meta      bool
	}{
		"started":  {add: addOK, commit: commitTestRelease, want: "", meta: true},
		"failed":   {add: func() (bool, error) { return false, errors.New("engine down") }, commit: commitTestRelease, want: addFailed},
		"rejected": {add: func() (bool, error) { return false, nil }, commit: commitTestRelease, want: addRejected},
		"commit refused": {add: addOK, want: addCommitFailed, withdrawn: true,
			commit: func(*Database, *models.Release) func() (bool, string, error) {
				return func() (bool, string, error) { return false, "duplicate", nil }
			}},
	} {
		t.Run(name, func(t *testing.T) {
			db := newTestDB(t)
			engine := &mockTorrentEngine{}
			release := testRelease()
			outcome, _, _ := startApprovedRelease(db, engine, &release, release.Quality.Score(), false, tc.add, tc.commit(db, &release))
			if outcome != tc.want {
				t.Fatalf("outcome %q, want %q", outcome, tc.want)
			}
			if n := intentCount(t, db); n != 0 {
				t.Fatalf("%d intents left after the outcome was recorded", n)
			}
			if _, removed := engine.removed[intentTestHash]; removed != tc.withdrawn {
				t.Fatalf("withdrawn=%v, want %v", removed, tc.withdrawn)
			}
			if got := hasMetaRow(t, db); got != tc.meta {
				t.Fatalf("torrent_meta row=%v, want %v", got, tc.meta)
			}
		})
	}
}

// A release without a magnet hash could be neither registered nor withdrawn:
// it never reaches the engine.
func TestStartApprovedReleaseRefusesReleaseWithoutHash(t *testing.T) {
	db := newTestDB(t)
	release := testRelease()
	release.Magnet = "https://example.invalid/file.torrent"
	called := false
	add := func() (bool, error) { called = true; return true, nil }
	outcome, _, err := startApprovedRelease(db, &mockTorrentEngine{}, &release, 0, false, add, commitTestRelease(db, &release))
	if outcome != addIntentFailed || err == nil || called {
		t.Fatalf("outcome=%q err=%v add called=%v", outcome, err, called)
	}
}

// Dry-run writes nothing, not even the intent.
func TestStartApprovedReleaseDryRunWritesNothing(t *testing.T) {
	db := newTestDB(t)
	release := testRelease()
	before := approvalRowCounts(t, db)
	outcome, _, _ := startApprovedRelease(db, &mockTorrentEngine{}, &release, 0, true,
		func() (bool, error) { return true, nil }, commitTestRelease(db, &release))
	if outcome != "" || intentCount(t, db) != 0 {
		t.Fatalf("dry-run outcome=%q intents=%d", outcome, intentCount(t, db))
	}
	assertSameCounts(t, "dry-run", before, approvalRowCounts(t, db))
}

// A process that died in the middle of an add leaves its intent; the next
// cycle finishes or undoes it according to how far the add went.
func TestRecoverInterruptedAdds(t *testing.T) {
	type state struct {
		committed, registered, inEngine, absenceKnown bool
	}
	for name, tc := range map[string]struct {
		state      state
		wantMeta   bool
		wantPlace  bool
		wantRemove bool
		wantIntent bool
	}{
		// Died after the registration: only the clean-up was lost.
		"registered": {state: state{committed: true, registered: true, inEngine: true, absenceKnown: true}, wantMeta: true, wantPlace: true},
		// Died between commit and registration: the registration is completed.
		"committed, in engine": {state: state{committed: true, inEngine: true, absenceKnown: true}, wantMeta: true, wantPlace: true},
		// Died between Add and commit: taken out of the session, data kept.
		"added only": {state: state{inEngine: true, absenceKnown: true}, wantRemove: true},
		// The torrent is gone: the placeholder is rolled back so the episode is
		// searched again.
		"committed, not in engine": {state: state{committed: true, absenceKnown: true}},
		// An empty snapshot from a backend that cannot report its health is no
		// evidence: the intent waits for a trustworthy one.
		"absence unknown": {state: state{committed: true}, wantPlace: true, wantIntent: true},
	} {
		t.Run(name, func(t *testing.T) {
			db := newTestDB(t)
			engine := &mockTorrentEngine{}
			release := testRelease()
			score := release.Quality.Score()
			if err := db.BeginTorrentAdd(&release, score); err != nil {
				t.Fatal(err)
			}
			if tc.state.committed {
				if ok, reason, err := commitTestRelease(db, &release)(); err != nil || !ok {
					t.Fatalf("commit: %v %q %v", ok, reason, err)
				}
			}
			if tc.state.registered {
				if err := db.RegisterTorrentScored(&release, score); err != nil {
					t.Fatal(err)
				}
			}
			live := map[string]struct{}{}
			if tc.state.inEngine {
				live[intentTestHash] = struct{}{}
			}
			if _, err := recoverInterruptedAdds(db, engine, live, tc.state.absenceKnown, time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if got := hasMetaRow(t, db); got != tc.wantMeta {
				t.Errorf("torrent_meta row=%v, want %v", got, tc.wantMeta)
			}
			if got := hasPlaceholderRow(t, db); got != tc.wantPlace {
				t.Errorf("placeholder=%v, want %v", got, tc.wantPlace)
			}
			deleteFiles, removed := engine.removed[intentTestHash]
			if removed != tc.wantRemove || deleteFiles {
				t.Errorf("removed=%v deleteFiles=%v, want removed=%v without deleting data", removed, deleteFiles, tc.wantRemove)
			}
			if got := intentCount(t, db) == 1; got != tc.wantIntent {
				t.Errorf("intent kept=%v, want %v", got, tc.wantIntent)
			}
		})
	}
}

// An intent of this process belongs to an add still in flight: recovery must
// not touch it.
func TestRecoverInterruptedAddsSkipsAddsInFlight(t *testing.T) {
	db := newTestDB(t)
	engine := &mockTorrentEngine{}
	release := testRelease()
	startedAt := time.Now()
	time.Sleep(2 * time.Millisecond)
	if err := db.BeginTorrentAdd(&release, 0); err != nil {
		t.Fatal(err)
	}
	if n, err := recoverInterruptedAdds(db, engine, map[string]struct{}{intentTestHash: {}}, true, startedAt); err != nil || n != 0 {
		t.Fatalf("resolved %d in-flight adds (err %v)", n, err)
	}
	if intentCount(t, db) != 1 || len(engine.removed) != 0 {
		t.Fatalf("in-flight add was touched: intents=%d removed=%v", intentCount(t, db), engine.removed)
	}
}

// A file the engine or aMule already has is a duplicate, not a cycle problem.
func TestDuplicateAddIsNotACycleProblem(t *testing.T) {
	stats := &models.CycleStats{}
	release := models.Release{Title: "FBI - 2x17 - Promesse infrante.mkv", Kind: "series"}
	logStartFailure(stats, &release, addRejected, "", nil)
	if stats.Errors != 0 {
		t.Fatalf("errors = %d (%v), want 0 for a duplicate", stats.Errors, stats.ErrorDetails)
	}
	logStartFailure(stats, &release, addFailed, "", errors.New("engine down"))
	if stats.Errors != 1 {
		t.Fatalf("errors = %d, want 1 for a failed add", stats.Errors)
	}
}
