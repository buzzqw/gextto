package gextto

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

func acquisitionTestRelease(t *testing.T, db *Database, hash string, season, episode int64) {
	t.Helper()
	name := "TestShow"
	release := models.Release{
		Kind:    "series",
		Series:  &name,
		Season:  &season,
		Episode: &episode,
		Title:   "TestShow.S01E01.1080p",
		Quality: ParseQuality("TestShow.S01E01.1080p"),
		Magnet:  "magnet:?xt=urn:btih:" + hash,
	}
	if err := db.RegisterTorrentScored(&release, 1000); err != nil {
		t.Fatal(err)
	}
}

// TestAcquisitionEventsStoryAndRepeats: gli eventi di un torrent formano la sua
// storia, con l'identità della serie copiata da torrent_meta; un evento uguale
// al precedente aumenta solo il contatore.
func TestAcquisitionEventsStoryAndRepeats(t *testing.T) {
	db := newTestDB(t)
	hash := "1111222233334444555566667777888899990000"
	acquisitionTestRelease(t, db, hash, 1, 1)
	base := time.Date(2026, time.October, 10, 9, 0, 0, 0, time.Local)
	record := func(offset time.Duration, level logging.Level, message, fields string) {
		t.Helper()
		event := logging.Event{At: base.Add(offset), Level: level, Hash: hash, Message: message, Fields: fields}
		if err := db.RecordAcquisitionEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	record(0, logging.LevelInfo, "📥 Downloading TestShow S01E01", "score: 1000")
	record(time.Minute, logging.LevelWarn, "still cannot be moved", "")
	record(2*time.Minute, logging.LevelWarn, "still cannot be moved", "")
	record(3*time.Minute, logging.LevelInfo, "📁 added to the library", "")

	events, err := db.AcquisitionEvents(hash)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(events), 3)
	assertEqual(t, events[0].Message, "📥 Downloading TestShow S01E01")
	assertEqual(t, events[0].SeriesName, "TestShow")
	assertEqual(t, events[0].AcqID, logging.AcqID(hash))
	assertTrue(t, events[0].Season != nil && *events[0].Season == 1, "stagione copiata da torrent_meta")
	assertEqual(t, events[1].Repeat, int64(2))
	assertEqual(t, events[1].FirstAt, "2026-10-10 09:01:00")
	assertEqual(t, events[1].At, "2026-10-10 09:02:00")
	assertEqual(t, events[2].Level, "INFO")

	byID, err := db.AcquisitionEventsByAcqID(logging.AcqID(hash))
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(byID), 3)

	season, episode := int64(1), int64(1)
	forEpisode, err := db.AcquisitionEventsForTitle("testshow", &season, &episode, 10)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(forEpisode), 3)
	assertEqual(t, forEpisode[0].Message, "📁 added to the library")
	other := int64(2)
	none, err := db.AcquisitionEventsForTitle("TestShow", &season, &other, 10)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(none), 0)
}

// TestPruneAcquisitionEvents: via gli eventi oltre la conservazione e quelli
// in eccesso per lo stesso torrent, a partire dai più vecchi.
func TestPruneAcquisitionEvents(t *testing.T) {
	db := newTestDB(t)
	old := "aaaa222233334444555566667777888899990000"
	busy := "bbbb222233334444555566667777888899990000"
	now := time.Date(2026, time.October, 10, 9, 0, 0, 0, time.Local)
	if err := db.RecordAcquisitionEvent(logging.Event{At: now.Add(-acquisitionRetention - time.Hour), Level: logging.LevelInfo, Hash: old, Message: "ancient"}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < acquisitionMaxPerHash+5; index++ {
		event := logging.Event{At: now, Level: logging.LevelInfo, Hash: busy, Message: "step " + strconv.Itoa(index)}
		if err := db.RecordAcquisitionEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := db.PruneAcquisitionEvents(now)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, removed, int64(6))
	events, err := db.AcquisitionEvents(busy)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(events), acquisitionMaxPerHash)
	assertEqual(t, events[0].Message, "step 5")
	gone, _ := db.AcquisitionEvents(old)
	assertEqual(t, len(gone), 0)
}

// TestAcquisitionRecorderStoresLoggedLines: una riga di log con l'hash finisce
// nella storia passando dal registratore asincrono.
func TestAcquisitionRecorderStoresLoggedLines(t *testing.T) {
	db := newTestDB(t)
	hash := "cccc222233334444555566667777888899990000"
	acquisitionTestRelease(t, db, hash, 1, 3)
	stop := StartAcquisitionRecorder(db)
	logging.Info("📥 Downloading TestShow S01E03", "hash", hash, "score", 1200)
	logging.Info("unrelated line", "series", "TestShow")
	stop()
	stop()
	events, err := db.AcquisitionEvents(hash)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(events), 1)
	assertEqual(t, events[0].Fields, "score: 1200")
}

// TestAcquisitionHistoryUIAndAPI: la storia di una puntata compare nella
// finestra 📜, nel pannello della serie e nell'API, cercabile anche per acq.
func TestAcquisitionHistoryUIAndAPI(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "TestShow", Seasons: "*", Quality: "1080p", Language: "ita"}}, nil); err != nil {
		t.Fatalf("SaveLibrary: %v", err)
	}
	hash := "dddd222233334444555566667777888899990000"
	acquisitionTestRelease(t, state.db, hash, 1, 1)
	at := time.Date(2026, time.October, 10, 9, 0, 0, 0, time.Local)
	for _, event := range []logging.Event{
		{At: at, Level: logging.LevelInfo, Hash: hash, Message: "📥 Downloading TestShow S01E01", Fields: "score: 1000"},
		{At: at.Add(time.Hour), Level: logging.LevelWarn, Hash: hash, Message: "⚠️ still cannot be moved"},
		{At: at.Add(2 * time.Hour), Level: logging.LevelWarn, Hash: hash, Message: "⚠️ still cannot be moved"},
	} {
		if err := state.db.RecordAcquisitionEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, body := v2Request(t, server, http.MethodGet, "/series/history?series=TestShow&season=1&episode=1&modal=1", nil)
	if code != http.StatusOK || !strings.Contains(body, "v2-history-title") || !strings.Contains(body, "still cannot be moved") ||
		!strings.Contains(body, "×2") || !strings.Contains(body, "S01E01") || !strings.Contains(body, logging.AcqID(hash)) {
		t.Fatalf("episode history modal -> %d: %s", code, body)
	}
	code, body = v2Request(t, server, http.MethodGet, "/series/history?series=TestShow", nil)
	if code != http.StatusOK || strings.Contains(body, "v2-history-title") || !strings.Contains(body, "Downloading TestShow S01E01") {
		t.Fatalf("series history panel -> %d: %s", code, body)
	}
	code, body = v2Request(t, server, http.MethodGet, "/?view=series&series=TestShow", nil)
	if code != http.StatusOK || !strings.Contains(body, `hx-get="/series/history?series=TestShow"`) {
		t.Fatalf("series page lacks the history panel -> %d", code)
	}
	code, body = v2Request(t, server, http.MethodGet, "/series/history?series=TestShow&season=x", nil)
	if code != http.StatusOK || !strings.Contains(body, "Stagione o episodio non validi.") {
		t.Fatalf("invalid season -> %d: %s", code, body)
	}

	for _, path := range []string{"/api/acquisitions?hash=" + hash, "/api/acquisitions?acq=" + logging.AcqID(hash), "/api/acquisitions?series=TestShow&season=1&episode=1"} {
		code, body = v2Request(t, server, http.MethodGet, path, nil)
		if code != http.StatusOK || strings.Count(body, `"message"`) != 2 || !strings.Contains(body, `"repeat":2`) {
			t.Fatalf("%s -> %d: %s", path, code, body)
		}
	}
	if code, _ = v2Request(t, server, http.MethodGet, "/api/acquisitions", nil); code != http.StatusBadRequest {
		t.Fatalf("missing selector -> %d", code)
	}
}
