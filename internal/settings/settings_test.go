package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileIsEmpty(t *testing.T) {
	store, err := Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if got := store.Get("lang", "en"); got != "en" {
		t.Fatalf("fallback = %q, want en", got)
	}
	if len(store.Keys()) != 0 {
		t.Fatalf("keys = %v, want none", store.Keys())
	}
}

func TestSaveAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	store.Set("lang", "de")
	store.Set("download-dir", "/srv/media")
	if err := store.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file not written: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get("lang", ""); got != "de" {
		t.Fatalf("lang = %q, want de", got)
	}
	if got := reloaded.Get("download-dir", ""); got != "/srv/media" {
		t.Fatalf("download-dir = %q", got)
	}
}

func TestLoadRejectsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("invalid JSON must be reported, not silently ignored")
	}
}

func TestSaveLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	store, err := Load(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	store.Set("k", "v")
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != "settings.json" {
			t.Fatalf("unexpected leftover %q after atomic save", entry.Name())
		}
	}
}
