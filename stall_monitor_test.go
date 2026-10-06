package gextto

import (
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// stallSession is an engine fake that parks and resumes torrents the way the
// real engines do: MarkStalled pauses and marks, Restart resumes and unmarks.
type stallSession struct {
	stubTorrentSession
	marks    int
	restarts int
}

func (s *stallSession) setState(state string) {
	for i := range s.list {
		s.list[i].State = state
	}
}

func (s *stallSession) MarkStalled(string) (bool, error) {
	s.marks++
	s.setState("stalled")
	return true, nil
}

func (s *stallSession) Restart(string) (bool, error) {
	s.restarts++
	s.setState("downloading")
	return true, nil
}

func TestMonitorStalledParksProbesAndKeepsItsClock(t *testing.T) {
	db := newTestDB(t)
	cfg := &Config{Settings: map[string]string{
		"libtorrent_stall_after_min": "60",
		"libtorrent_stall_retry_min": "60",
	}}
	session := &stallSession{stubTorrentSession: stubTorrentSession{list: []models.TorrentView{
		{Hash: "fbi", Name: "FBI Stagione 2", State: "downloading", Progress: 74, TotalDone: 1000, NumPeers: 2},
	}}}
	watch := map[string]StallWatch{}
	rewind := func(mutate func(*StallWatch)) {
		entry := watch["fbi"]
		mutate(&entry)
		watch["fbi"] = entry
	}

	MonitorStalled(cfg, session, db, nil, watch)
	if session.marks != 0 {
		t.Fatal("a fresh download must not be parked")
	}

	// No byte for the stall window: parked.
	rewind(func(e *StallWatch) { e.lastProgressAt = time.Now().Add(-61 * time.Minute) })
	MonitorStalled(cfg, session, db, nil, watch)
	if session.marks != 1 || session.list[0].State != "stalled" {
		t.Fatalf("stalled download not parked: marks=%d state=%s", session.marks, session.list[0].State)
	}
	stalledSince := *watch["fbi"].stalledSince

	// Parked and not due: left alone.
	MonitorStalled(cfg, session, db, nil, watch)
	if session.marks != 1 || session.restarts != 0 {
		t.Fatalf("parked torrent touched before its retry: marks=%d restarts=%d", session.marks, session.restarts)
	}

	// Retry due: resumed for a probe, still parked as far as the monitor knows.
	rewind(func(e *StallWatch) { e.nextRetryAt = time.Now().Add(-time.Second) })
	MonitorStalled(cfg, session, db, nil, watch)
	if session.restarts != 1 || watch["fbi"].probeUntil.IsZero() {
		t.Fatalf("retry did not start a probe: restarts=%d probe=%v", session.restarts, watch["fbi"].probeUntil)
	}
	// During the probe it is neither re-parked nor forgotten.
	MonitorStalled(cfg, session, db, nil, watch)
	if session.marks != 1 || session.list[0].State != "downloading" {
		t.Fatalf("probe interrupted: marks=%d state=%s", session.marks, session.list[0].State)
	}
	if rows, _ := db.LoadStallWatches(); len(rows) != 1 {
		t.Fatalf("stall row dropped during the probe: %d rows", len(rows))
	}

	// Probe over without bytes: parked again, same stall clock.
	rewind(func(e *StallWatch) { e.probeUntil = time.Now().Add(-time.Second) })
	MonitorStalled(cfg, session, db, nil, watch)
	if session.marks != 2 || session.list[0].State != "stalled" || !watch["fbi"].probeUntil.IsZero() {
		t.Fatalf("empty probe not parked again: marks=%d state=%s", session.marks, session.list[0].State)
	}
	if got := *watch["fbi"].stalledSince; !got.Equal(stalledSince) {
		t.Fatalf("stall clock reset: %v != %v", got, stalledSince)
	}

	// Daemon restart: the watch comes back from the database while the engine
	// is checking data and then reports the torrent merely paused.
	restored, err := db.LoadStallWatches()
	if err != nil {
		t.Fatal(err)
	}
	session.setState("checking_resume_data")
	MonitorStalled(cfg, session, db, nil, restored)
	if _, ok := restored["fbi"]; !ok {
		t.Fatal("a data check after a restart dropped the stalled torrent")
	}
	session.setState("paused")
	MonitorStalled(cfg, session, db, nil, restored)
	if session.list[0].State != "stalled" {
		t.Fatalf("restored torrent not parked again: state=%s", session.list[0].State)
	}
	if got := *restored["fbi"].stalledSince; !got.Equal(stalledSince) {
		t.Fatalf("stall clock reset by the restart: %v != %v", got, stalledSince)
	}

	// Bytes during a probe: recovered.
	rewindRestored := func(mutate func(*StallWatch)) {
		entry := restored["fbi"]
		mutate(&entry)
		restored["fbi"] = entry
	}
	rewindRestored(func(e *StallWatch) { e.nextRetryAt = time.Now().Add(-time.Second) })
	MonitorStalled(cfg, session, db, nil, restored)
	session.list[0].TotalDone = 5000
	MonitorStalled(cfg, session, db, nil, restored)
	if restored["fbi"].stalledSince != nil {
		t.Fatal("progress during a probe did not clear the stall")
	}
	if rows, _ := db.LoadStallWatches(); len(rows) != 0 {
		t.Fatalf("recovered torrent still persisted: %d rows", len(rows))
	}
}

func TestMonitorStalledGivesUpOnTheOriginalClock(t *testing.T) {
	db := newTestDB(t)
	cfg := &Config{Settings: map[string]string{"libtorrent_dead_swarm_giveup_min": "4320"}}
	session := &stallSession{stubTorrentSession: stubTorrentSession{list: []models.TorrentView{
		{Hash: "fbi", Name: "FBI Stagione 3", State: "stalled", Stalled: true, HasMetadata: true, Progress: 9, TotalDone: 1000, NumPeers: 1, NumComplete: 0},
	}}}
	since := time.Now().Add(-73 * time.Hour)
	watch := map[string]StallWatch{"fbi": {
		lastProgressAt: since, lastDone: 1000, stalledSince: &since, nextRetryAt: time.Now().Add(time.Hour),
	}}
	MonitorStalled(cfg, session, db, NewNotifier(), watch)
	if len(session.removed) != 1 || !strings.EqualFold(session.removed[0], "fbi") {
		t.Fatalf("dead swarm past its give-up window was not removed: %v", session.removed)
	}
}

func TestMonitorStalledIdleClockSurvivesRestart(t *testing.T) {
	db := newTestDB(t)
	cfg := &Config{Settings: map[string]string{"libtorrent_stall_after_min": "60"}}
	session := &stallSession{stubTorrentSession: stubTorrentSession{list: []models.TorrentView{
		{Hash: "fbi", Name: "FBI.S03", State: "downloading", Progress: 53, TotalDone: 1000, NumPeers: 4},
	}}}
	watch := map[string]StallWatch{}
	MonitorStalled(cfg, session, db, nil, watch)

	// Idle for 40 minutes: the clock is saved, the torrent not parked yet.
	idleSince := time.Now().Add(-40 * time.Minute)
	entry := watch["fbi"]
	entry.lastProgressAt = idleSince
	watch["fbi"] = entry
	MonitorStalled(cfg, session, db, nil, watch)
	if session.marks != 0 {
		t.Fatal("parked before the stall window")
	}

	// Restart: the clock comes back from the database, survives the data
	// check and keeps counting from the original moment.
	restored, err := db.LoadStallWatches()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := restored["fbi"]; !ok || got.stalledSince != nil || !got.lastProgressAt.Equal(idleSince) {
		t.Fatalf("idle clock not restored: %#v", restored)
	}
	session.setState("checking_resume_data")
	MonitorStalled(cfg, session, db, nil, restored)
	session.setState("downloading")
	MonitorStalled(cfg, session, db, nil, restored)
	if session.marks != 0 {
		t.Fatal("parked before the stall window after the restart")
	}
	rewound := restored["fbi"]
	rewound.lastProgressAt = time.Now().Add(-61 * time.Minute)
	restored["fbi"] = rewound
	MonitorStalled(cfg, session, db, nil, restored)
	if session.marks != 1 {
		t.Fatal("restored idle download not parked once its window elapsed")
	}

	// Progress clears the saved clock.
	fresh := &stallSession{stubTorrentSession: stubTorrentSession{list: []models.TorrentView{
		{Hash: "ok", Name: "Fine", State: "downloading", Progress: 10, TotalDone: 1},
	}}}
	okWatch := map[string]StallWatch{"ok": {lastProgressAt: time.Now().Add(-10 * time.Minute), lastDone: 1}}
	MonitorStalled(cfg, fresh, db, nil, okWatch)
	fresh.list[0].TotalDone = 500
	MonitorStalled(cfg, fresh, db, nil, okWatch)
	rows, _ := db.LoadStallWatches()
	if _, ok := rows["ok"]; ok {
		t.Fatal("a download that moved again kept its saved idle clock")
	}
}
