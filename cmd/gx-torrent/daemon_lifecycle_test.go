package main

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/settings"
)

// TestDaemonStartServeStop exercises the lifecycle shared by the interactive
// process and the Windows service: startDaemon brings up the HTTP server on a
// listener chosen by the OS, answers on it, and shutdown stops it cleanly. It
// runs on Linux and on the Windows CI job (name matches its filter).
func TestDaemonStartServeStop(t *testing.T) {
	dir := t.TempDir()
	store, err := settings.Load(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Mark the setup complete so startDaemon does not try to open a browser.
	store.Set(setupCompleteKey, "true")
	opts := Options{
		Listen:      "127.0.0.1:0",
		DataDir:     dir,
		LinkDir:     filepath.Join(dir, "links"),
		DownloadDir: filepath.Join(dir, "downloads"),
		DBPath:      filepath.Join(dir, "session.db"),
		StatePath:   filepath.Join(dir, "state.json"),
		Mode:        ModeStandalone,
		Settings:    store,
		Network:     NetworkOptions{PortBegin: 46100, PortEnd: 46199, Encryption: 1},
		Tick:        50 * time.Millisecond,
		ProbeWindow: time.Minute,
	}
	run, err := startDaemon(opts)
	if err != nil {
		t.Fatalf("startDaemon: %v", err)
	}
	defer run.shutdown()
	if run.Addr() == nil {
		t.Fatal("no listener address")
	}
	resp, err := http.Get("http://" + run.Addr().String() + "/api/v1/health")
	if err != nil {
		t.Fatalf("health request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", resp.StatusCode)
	}
}
