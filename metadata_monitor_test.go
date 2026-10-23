package gextto

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

const metadataTestHash = "0123456789abcdef0123456789abcdef01234567"

func metadataTestConfig(giveupMinutes string) Config {
	cfg := DefaultConfig()
	cfg.Settings = map[string]string{"torrent_metadata_giveup_min": giveupMinutes}
	return cfg
}

func insertMetadataTestTorrent(t *testing.T, db *Database, hash, name string) {
	t.Helper()
	if _, err := db.db.Exec("INSERT INTO torrent_meta(hash,name,status,created_at,updated_at) VALUES (?1,?2,'queued',datetime('now'),datetime('now'))", hash, name); err != nil {
		t.Fatalf("insert torrent meta: %v", err)
	}
}

// A magnet's give-up clock lives in the database: after a restart the wait
// continues from where it was instead of starting a new full window.
func TestMetadataGiveUpSurvivesARestart(t *testing.T) {
	db := newTestDB(t)
	cfg := metadataTestConfig("60")
	insertMetadataTestTorrent(t, db, metadataTestHash, "Dead.Magnet")

	// The previous run saved the wait two hours ago.
	if err := db.SaveMetadataWait(metadataTestHash, time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatalf("save metadata wait: %v", err)
	}
	restored, err := db.LoadMetadataWaits()
	if err != nil {
		t.Fatalf("load metadata waits: %v", err)
	}
	if _, ok := restored[metadataTestHash]; !ok {
		t.Fatalf("metadata wait not restored: %v", restored)
	}

	session := &stubTorrentSession{list: []models.TorrentView{{
		Hash: metadataTestHash, Name: "Dead.Magnet", State: "downloading_metadata",
	}}}
	MonitorMetadata(&cfg, session, db, FromConfig(&cfg),
		map[string]time.Time{}, restored, map[string]time.Time{})

	if len(session.removed) != 1 || session.removed[0] != metadataTestHash {
		t.Fatalf("a magnet waiting longer than the give-up must be removed, removed %v", session.removed)
	}
	if waits, err := db.LoadMetadataWaits(); err != nil || len(waits) != 0 {
		t.Fatalf("the wait state must be cleared after the give-up: waits=%v err=%v", waits, err)
	}
}

// A magnet that has met nobody after the probe window is abandoned without
// waiting for the full metadata give-up; a connected peer keeps it alive.
func TestMetadataProbeGivesUpWhenNobodyAnswers(t *testing.T) {
	db := newTestDB(t)
	cfg := metadataTestConfig("180")
	insertMetadataTestTorrent(t, db, metadataTestHash, "Dead.Magnet")
	session := &stubTorrentSession{list: []models.TorrentView{{
		Hash: metadataTestHash, Name: "Dead.Magnet", State: "downloading_metadata",
	}}}
	started := time.Now().Add(-tev_metadataProbeWindow - time.Minute)
	MonitorMetadata(&cfg, session, db, FromConfig(&cfg),
		map[string]time.Time{metadataTestHash: started}, map[string]time.Time{metadataTestHash: started}, map[string]time.Time{})
	if len(session.removed) != 1 {
		t.Fatalf("a magnet with no peer after the probe was not removed: %v", session.removed)
	}

	// A connected peer may still deliver the file list: the probe leaves it.
	db = newTestDB(t)
	insertMetadataTestTorrent(t, db, metadataTestHash, "Slow.Magnet")
	session = &stubTorrentSession{list: []models.TorrentView{{
		Hash: metadataTestHash, Name: "Slow.Magnet", State: "downloading_metadata", NumConnections: 1,
	}}}
	MonitorMetadata(&cfg, session, db, FromConfig(&cfg),
		map[string]time.Time{metadataTestHash: started}, map[string]time.Time{metadataTestHash: started}, map[string]time.Time{})
	if len(session.removed) != 0 {
		t.Fatalf("a magnet with a connected peer was removed too early: %v", session.removed)
	}
}

// The wait is saved while it runs and cleared as soon as the file list
// arrives, so a restart resumes it and a completed magnet leaves no row.
func TestMetadataWaitIsSavedAndCleared(t *testing.T) {
	db := newTestDB(t)
	cfg := metadataTestConfig("60")
	insertMetadataTestTorrent(t, db, metadataTestHash, "Magnet")
	notifier := FromConfig(&cfg)
	session := &stubTorrentSession{list: []models.TorrentView{{
		Hash: metadataTestHash, Name: "Magnet", State: "downloading_metadata",
	}}}
	firstSeen := map[string]time.Time{}

	MonitorMetadata(&cfg, session, db, notifier,
		map[string]time.Time{}, firstSeen, map[string]time.Time{})
	if len(session.removed) != 0 {
		t.Fatalf("a fresh magnet must not be removed, removed %v", session.removed)
	}
	waits, err := db.LoadMetadataWaits()
	if err != nil {
		t.Fatalf("load metadata waits: %v", err)
	}
	if len(waits) != 1 {
		t.Fatalf("a waiting magnet must be saved, waits=%v", waits)
	}

	// A paused magnet is waiting for a slot: the wait is dropped instead of
	// aging out, and the row goes with it.
	session.list[0].State = "paused"
	MonitorMetadata(&cfg, session, db, notifier,
		map[string]time.Time{}, firstSeen, map[string]time.Time{})
	if waits, err := db.LoadMetadataWaits(); err != nil || len(waits) != 0 {
		t.Fatalf("a paused magnet must clear its wait: waits=%v err=%v", waits, err)
	}

	// Back in the metadata state the clock starts again, and it is cleared as
	// soon as the file list arrives.
	session.list[0].State = "downloading_metadata"
	MonitorMetadata(&cfg, session, db, notifier,
		map[string]time.Time{}, firstSeen, map[string]time.Time{})
	session.list[0].HasMetadata = true
	MonitorMetadata(&cfg, session, db, notifier,
		map[string]time.Time{}, firstSeen, map[string]time.Time{})
	if waits, err := db.LoadMetadataWaits(); err != nil || len(waits) != 0 {
		t.Fatalf("metadata arrival must clear the wait: waits=%v err=%v", waits, err)
	}
}
