//go:build anacrolix

package gextto

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func TestAnacrolixPriorityConversion(t *testing.T) {
	cases := []int{0, 1, 4, 6, 7}
	for _, value := range cases {
		round := anacrolixPriorityToInt(intToAnacrolixPriority(value))
		if value == 0 && round != 0 {
			t.Fatalf("priority %d -> %d", value, round)
		}
		if value >= 7 && round != 7 {
			t.Fatalf("priority %d -> %d", value, round)
		}
	}
}

func TestAnacrolixEngineCapabilityErrors(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.StateDir = filepath.Join(cfg.DataDir, "state")
	cfg.LibtorrentDir = filepath.Join(cfg.DataDir, "downloads")
	cfg.Libtorrent.PortMin = 0
	cfg.Libtorrent.PortMax = 0
	if err := os.MkdirAll(cfg.LibtorrentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	engineIface, err := newAnacrolixEngineImpl(&cfg)
	if err != nil {
		t.Fatalf("newAnacrolixEngineImpl: %v", err)
	}
	engine := engineIface.(*anacrolixEngine)
	defer engine.Close()

	// A move of an unknown torrent is a no-op, not an error.
	if moved, err := engine.MoveStorage("deadbeef", "/tmp/x"); err != nil || moved {
		t.Fatalf("MoveStorage(unknown) = %v, %v", moved, err)
	}
	if _, err := engine.SetLimits("abc", 1, 1, 1, 1); !isCapabilityUnavailable(err) {
		t.Fatalf("SetLimits err = %v", err)
	}
	if _, err := engine.SetGlobalSpeedLimits(100, 100); !isCapabilityUnavailable(err) {
		t.Fatalf("SetGlobalSpeedLimits err = %v", err)
	}
	if engine.Name() != BackendAnacrolix {
		t.Fatalf("name = %q", engine.Name())
	}
}

func TestAnacrolixEngineVerifiesLocalDataAndResumes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping anacrolix data verification in short mode")
	}
	dir := t.TempDir()
	payload := make([]byte, 256*1024)
	for index := range payload {
		payload[index] = byte(index % 251)
	}
	torrentBytes, infoHash := buildTorrentBytes(t, "payload.bin", payload, 16384, "http://127.0.0.1:1/announce")
	torrentPath := filepath.Join(dir, "payload.torrent")
	if err := os.WriteFile(torrentPath, torrentBytes, 0o644); err != nil {
		t.Fatalf("write torrent: %v", err)
	}
	dataDir := filepath.Join(dir, "downloads")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "payload.bin"), payload, 0o644); err != nil {
		t.Fatalf("write data: %v", err)
	}

	cfg := transferTestConfig(t, dir, 0)
	cfg.Libtorrent.PortMin = 0
	cfg.Libtorrent.PortMax = 0
	engineIface, err := newAnacrolixEngineImpl(&cfg)
	if err != nil {
		t.Fatalf("newAnacrolixEngineImpl: %v", err)
	}
	engine := engineIface.(*anacrolixEngine)

	hash, err := engine.AddTorrentFileEx(torrentPath, dataDir, AddOptions{})
	if err != nil || hash == nil {
		t.Fatalf("AddTorrentFileEx = %v, %v", hash, err)
	}
	if *hash != infoHash {
		t.Fatalf("hash = %s, want %s", *hash, infoHash)
	}
	if !waitForProgress(engine, infoHash, 99.99, 30*time.Second) {
		t.Fatalf("torrent did not verify local data: %+v", engine.List())
	}

	// Pause and resume must be idempotent and reflected in the state.
	if ok, err := engine.Pause(infoHash); err != nil || !ok {
		t.Fatalf("Pause = %v, %v", ok, err)
	}
	if view := findView(engine, infoHash); view == nil || view.State != "paused" {
		t.Fatalf("pause state = %+v", view)
	}
	if ok, err := engine.Resume(infoHash); err != nil || !ok {
		t.Fatalf("Resume = %v, %v", ok, err)
	}

	// A restart must restore the torrent from the manifest + .torrent, with the
	// piece completion store avoiding a re-download.
	if err := engine.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	restoredIface, err := newAnacrolixEngineImpl(&cfg)
	if err != nil {
		t.Fatalf("restore engine: %v", err)
	}
	restored := restoredIface.(*anacrolixEngine)
	defer restored.Close()
	if !waitForProgress(restored, infoHash, 99.99, 30*time.Second) {
		t.Fatalf("restored torrent not complete: %+v", restored.List())
	}

	if ok, err := restored.Remove(infoHash, false); err != nil || !ok {
		t.Fatalf("Remove = %v, %v", ok, err)
	}
	if view := findView(restored, infoHash); view != nil {
		t.Fatalf("torrent still present after removal: %+v", view)
	}
	// The data must be kept when deleteFiles is false.
	if _, err := os.Stat(filepath.Join(dataDir, "payload.bin")); err != nil {
		t.Fatalf("data removed despite deleteFiles=false: %v", err)
	}
}

func waitForProgress(engine *anacrolixEngine, hash string, progress float64, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if view := findView(engine, hash); view != nil && view.Progress >= progress {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func findView(engine *anacrolixEngine, hash string) *models.TorrentView {
	for _, view := range engine.List() {
		if view.Hash == hash {
			value := view
			return &value
		}
	}
	return nil
}

func hasEventKind(events []models.TorrentEvent, kind string) bool {
	for _, event := range events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

func TestAnacrolixEventDiffing(t *testing.T) {
	engine := &anacrolixEngine{events: nil}
	engine.diffLocked(
		models.TorrentView{Hash: "abc", Progress: 10, HasMetadata: false},
		models.TorrentView{Hash: "abc", Progress: 55, HasMetadata: true},
	)
	engine.diffLocked(
		models.TorrentView{Hash: "abc", Progress: 55, HasMetadata: true},
		models.TorrentView{Hash: "abc", Progress: 100, HasMetadata: true},
	)
	if !hasEventKind(engine.events, "metadata_received") || !hasEventKind(engine.events, "torrent_finished") {
		t.Fatalf("events = %+v", engine.events)
	}
}

func TestAnacrolixEngineMovesStorage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping anacrolix storage move in short mode")
	}
	dir := t.TempDir()
	payload := make([]byte, 128*1024)
	for index := range payload {
		payload[index] = byte((index * 7) % 256)
	}
	torrentBytes, infoHash := buildTorrentBytes(t, "move.bin", payload, 16384, "http://127.0.0.1:1/announce")
	torrentPath := filepath.Join(dir, "move.torrent")
	if err := os.WriteFile(torrentPath, torrentBytes, 0o644); err != nil {
		t.Fatalf("write torrent: %v", err)
	}
	dataDir := filepath.Join(dir, "downloads")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "move.bin"), payload, 0o644); err != nil {
		t.Fatalf("write data: %v", err)
	}

	cfg := transferTestConfig(t, dir, 0)
	cfg.Libtorrent.PortMin = 0
	cfg.Libtorrent.PortMax = 0
	engineIface, err := newAnacrolixEngineImpl(&cfg)
	if err != nil {
		t.Fatalf("newAnacrolixEngineImpl: %v", err)
	}
	engine := engineIface.(*anacrolixEngine)
	defer engine.Close()

	if _, err := engine.AddTorrentFileEx(torrentPath, dataDir, AddOptions{}); err != nil {
		t.Fatalf("AddTorrentFileEx: %v", err)
	}
	if !waitForProgress(engine, infoHash, 99.99, 30*time.Second) {
		t.Fatalf("torrent did not complete: %+v", engine.List())
	}

	destination := filepath.Join(dir, "moved")
	if moved, err := engine.MoveStorage(infoHash, destination); err != nil || !moved {
		t.Fatalf("MoveStorage = %v, %v", moved, err)
	}
	if !waitForProgress(engine, infoHash, 99.99, 30*time.Second) {
		t.Fatalf("torrent incomplete after move: %+v", engine.List())
	}
	if _, err := os.Stat(filepath.Join(destination, "move.bin")); err != nil {
		t.Fatalf("data not present at destination: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "move.bin")); err == nil {
		t.Fatal("data still present at the old location")
	}
	view := findView(engine, infoHash)
	if view == nil || view.SavePath != destination {
		t.Fatalf("save path after move = %+v", view)
	}
}
