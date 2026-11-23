package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrefsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gextto", "tui.json")
	getenv := func(key string) string {
		if key == "GEXTTO_TUI_CONFIG" {
			return path
		}
		return ""
	}
	want := prefs{ColorsEnabled: boolPtr(false), HighContrast: boolPtr(true)}
	if err := want.save(getenv); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := loadPrefs(getenv); !got.same(want) {
		t.Fatalf("loaded %+v, want %+v", got, want)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("save left temporary files behind: %v %v", entries, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("preferences file mode = %v, want 0600", info)
	}
}

func TestPrefsIgnoreMissingAndCorruptFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tui.json")
	getenv := func(key string) string {
		if key == "GEXTTO_TUI_CONFIG" {
			return path
		}
		return ""
	}
	if got := loadPrefs(getenv); got.ColorsEnabled != nil || got.HighContrast != nil {
		t.Fatalf("missing file produced preferences: %+v", got)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadPrefs(getenv); got.ColorsEnabled != nil || got.HighContrast != nil {
		t.Fatalf("corrupt file produced preferences: %+v", got)
	}
}
