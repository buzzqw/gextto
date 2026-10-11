package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

func insertPendingEpisode(t *testing.T, db *Database, name string, season, episode int64, firstSeen, dueAt time.Time) {
	t.Helper()
	res, err := db.db.Exec("INSERT INTO series(name) VALUES(?1)", name)
	if err != nil {
		t.Fatal(err)
	}
	seriesID, _ := res.LastInsertId()
	if _, err := db.db.Exec(`INSERT INTO pending_downloads(series_id,season,episode,best_title,first_seen_at,due_at,status)
		VALUES(?1,?2,?3,'Release.Title',?4,?5,'pending')`,
		seriesID, season, episode, firstSeen.UTC().Format(pendingTimeLayout), dueAt.UTC().Format(pendingTimeLayout)); err != nil {
		t.Fatal(err)
	}
}

func TestPendingQueue(t *testing.T) {
	db := newTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	firstSeen := now.Add(-3 * time.Hour)
	dueAt := now.Add(time.Hour)

	insertPendingEpisode(t, db, "Test Show", 1, 2, firstSeen, dueAt)
	if _, err := db.db.Exec(`INSERT INTO pending_movies(name,year,best_title,first_seen_at,due_at,status)
		VALUES('A Movie',2020,'A.Movie.2020',?1,?2,'pending')`,
		firstSeen.Format(pendingTimeLayout), dueAt.Format(pendingTimeLayout)); err != nil {
		t.Fatal(err)
	}

	items, err := db.PendingQueue(now)
	if err != nil {
		t.Fatalf("PendingQueue: %s", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(items), items)
	}
	byTitle := map[string]PendingQueueItem{}
	for _, it := range items {
		byTitle[it.Title] = it
	}
	ep, ok := byTitle["Test Show"]
	if !ok {
		t.Fatalf("episode missing: %+v", items)
	}
	if ep.Label != "S01E02" {
		t.Errorf("label = %q, want S01E02", ep.Label)
	}
	if ep.Waiting < 2*time.Hour+55*time.Minute || ep.Waiting > 3*time.Hour+5*time.Minute {
		t.Errorf("waiting = %s, want ~3h", ep.Waiting)
	}
	if ep.Ready {
		t.Error("episode should not be ready: due is in the future")
	}
	mov, ok := byTitle["A Movie"]
	if !ok || mov.Label != "2020" {
		t.Fatalf("movie item = %+v", mov)
	}
}

func TestFailedImportsDaily(t *testing.T) {
	db := newTestDB(t)
	// The daily summary groups by UTC day (see FailedImportsDaily), so the
	// events must be recorded in UTC too: with local time the test would flake
	// in the hours where the local and UTC dates differ.
	now := time.Now().UTC()
	if err := db.RecordAcquisitionEvent(logging.Event{At: now, Level: logging.LevelError, Hash: "abc", Message: "torrent completion handling failed"}); err != nil {
		t.Fatal(err)
	}
	// A non-import error must not be counted.
	if err := db.RecordAcquisitionEvent(logging.Event{At: now, Level: logging.LevelError, Hash: "def", Message: "some unrelated error"}); err != nil {
		t.Fatal(err)
	}

	daily, err := db.FailedImportsDaily(7)
	if err != nil {
		t.Fatalf("FailedImportsDaily: %s", err)
	}
	if len(daily) != 1 || daily[0].Count != 1 {
		t.Fatalf("daily = %+v, want one day with count 1", daily)
	}
	if want := time.Now().UTC().Format("2006-01-02"); daily[0].Day != want {
		t.Errorf("day = %q, want %q", daily[0].Day, want)
	}
}

// TestV2HealthQueueAndImportsPanels checks the Salute page renders the delay
// queue (with wait time) and the daily failed-import count.
func TestV2HealthQueueAndImportsPanels(t *testing.T) {
	state := newTestAppState(t)
	now := time.Now().UTC()
	insertPendingEpisode(t, state.db, "Queued Show", 2, 5, now.Add(-2*time.Hour), now.Add(time.Hour))
	if err := state.db.RecordAcquisitionEvent(logging.Event{At: now, Level: logging.LevelError, Hash: "h1", Message: "torrent completion handling failed"}); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/?view=health", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /?view=health -> %d", code)
	}
	for _, want := range []string{"Coda titoli", "Import falliti", "Queued Show", "S02E05", now.Format("2006-01-02")} {
		if !strings.Contains(body, want) {
			t.Fatalf("health page missing %q", want)
		}
	}

	// The panels refresh on their own while the page is open.
	if !strings.Contains(body, `id="v2-health-queue" hx-get="/partial/health/queue" hx-trigger="every 60s"`) {
		t.Fatal("the queue panels are not wired to auto-refresh")
	}
	code, partial := v2Request(t, server, http.MethodGet, "/partial/health/queue", nil)
	if code != http.StatusOK || !strings.Contains(partial, "Coda titoli") || !strings.Contains(partial, "Queued Show") {
		t.Fatalf("queue partial -> %d: %s", code, partial)
	}
}
