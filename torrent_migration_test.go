package gextto

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/buzzqw/gextto/internal/qbittorrent"
)

func TestMigrationPlanEmptyEngine(t *testing.T) {
	state := newTestAppState(t)
	plan := BuildMigrationPlan(state, state.cfg, BackendEmbedded)
	if !plan.Ready {
		t.Fatalf("embedded plan not ready: %+v", plan.Warnings)
	}
	if len(plan.Items) != 0 {
		t.Fatalf("items = %d, want 0", len(plan.Items))
	}
	if plan.FromBackend != BackendEmbedded || plan.ToBackend != BackendEmbedded {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestMigrationManifestPersistAndLoad(t *testing.T) {
	state := newTestAppState(t)
	plan := BuildMigrationPlan(state, state.cfg, BackendEmbedded)
	path, err := PersistMigrationManifest(state.cfg, plan)
	if err != nil {
		t.Fatalf("PersistMigrationManifest: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
	loaded, err := LoadMigrationManifest(state.cfg)
	if err != nil || loaded == nil {
		t.Fatalf("LoadMigrationManifest = %+v, %v", loaded, err)
	}
	if loaded.ToBackend != BackendEmbedded {
		t.Fatalf("loaded = %+v", loaded)
	}
	_ = os.Remove(path)
	if removed, err := LoadMigrationManifest(state.cfg); err != nil || removed != nil {
		t.Fatalf("manifest not removed: %+v, %v", removed, err)
	}
}

func TestMigrationPlanRejectsIncompleteTarget(t *testing.T) {
	state := newTestAppState(t)
	// qBittorrent without a URL must not be marked ready.
	plan := BuildMigrationPlan(state, state.cfg, BackendQbittorrent)
	if plan.Ready {
		t.Fatal("qbittorrent plan must not be ready without a URL")
	}
	// anacrolix without the build tag must not be ready.
	plan = BuildMigrationPlan(state, state.cfg, BackendAnacrolix)
	if plan.Ready && newAnacrolixEngine == nil {
		t.Fatal("anacrolix plan must not be ready without the build tag")
	}
}

func TestMigrationPlanListsManagedTorrents(t *testing.T) {
	state := newTestAppState(t)
	fake := newFakeQB()
	server := httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(server.Close)
	fake.setTorrents([]qbittorrent.Torrent{{
		Hash: "abc", Name: "x", State: "downloading", Progress: 0.5, Size: 100,
	}})
	cfg := state.cfg
	cfg.Settings["torrent_backend"] = BackendQbittorrent
	cfg.Settings["qbittorrent_url"] = server.URL
	engine, err := newQbittorrentEngine(cfg)
	if err != nil {
		t.Fatalf("newQbittorrentEngine: %v", err)
	}
	state.setActiveEngine(engine)

	plan := BuildMigrationPlan(state, cfg, BackendEmbedded)
	if len(plan.Items) != 1 || plan.Items[0].Hash != "abc" {
		t.Fatalf("plan items = %+v", plan.Items)
	}
	if plan.FromBackend != BackendQbittorrent {
		t.Fatalf("from backend = %q", plan.FromBackend)
	}
	// The engine persisted the .torrent while listing, so the plan is ready and
	// carries the recoverable metadata.
	if plan.Items[0].TorrentFile == "" {
		t.Fatalf("expected a persisted .torrent in the plan: %+v", plan.Items[0])
	}
	if !plan.Ready {
		t.Fatalf("plan should be ready: %+v", plan.Warnings)
	}
}
