package gextto

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// clearEnv unsets key for the duration of the test and restores it afterwards.
func clearEnv(t *testing.T, key string) {
	t.Helper()
	value, ok := os.LookupEnv(key)
	if ok {
		t.Cleanup(func() { _ = os.Setenv(key, value) })
	} else {
		t.Cleanup(func() { _ = os.Unsetenv(key) })
	}
	_ = os.Unsetenv(key)
}

// newDryRunState builds the smallest AppState the dry-run helpers read: a
// start-up snapshot, a configuration file pointing at a temporary data
// directory, and an empty configuration cache.
func newDryRunState(t *testing.T, startupDryRun bool) (*AppState, string) {
	t.Helper()
	clearEnv(t, "GEXTTO_DRY_RUN")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "gextto.json")
	payload := `{"data_dir": ` + strconv.Quote(dir) + `}`
	if err := os.WriteFile(configPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("write %s: %v", configPath, err)
	}
	state := &AppState{
		cfg:          &Config{DataDir: dir, DryRun: startupDryRun},
		config_path:  configPath,
		config_cache: &ConfigCache{},
	}
	return state, dir
}

// TestDryRunPicksUpASettingChange covers the reason the start-up snapshot is
// not authoritative: the operator turns dry-run off in the interface and the
// handlers must obey without a restart.
func TestDryRunPicksUpASettingChange(t *testing.T) {
	state, dir := newDryRunState(t, true)
	if err := SaveSetting(dir, "dry_run", "yes"); err != nil {
		t.Fatalf("save the initial dry-run setting: %v", err)
	}
	if !state.dryRun() {
		t.Fatal("dry-run setting on: dryRun() is false")
	}
	if err := SaveSetting(dir, "dry_run", "no"); err != nil {
		t.Fatalf("turn dry-run off: %v", err)
	}
	if state.dryRun() {
		t.Fatal("dry-run setting off: dryRun() still reports the start-up snapshot")
	}
}

// TestForcedDryRunSurvivesASettingChange covers --dry-run: the command-line
// switch is not a setting and must keep the daemon read-only even when the
// interface says otherwise.
func TestForcedDryRunSurvivesASettingChange(t *testing.T) {
	state, dir := newDryRunState(t, true)
	state.SetDryRunForced(true)
	if err := SaveSetting(dir, "dry_run", "no"); err != nil {
		t.Fatalf("turn dry-run off: %v", err)
	}
	if !state.dryRun() {
		t.Fatal("dry-run forced on the command line: dryRun() followed the setting instead")
	}
}

// TestDryRunFallsBackToTheSnapshot guards the degraded path: when the
// configuration cannot be reloaded, the start-up value decides instead of
// silently switching to a writable daemon.
func TestDryRunFallsBackToTheSnapshot(t *testing.T) {
	state, dir := newDryRunState(t, true)
	if err := os.Remove(filepath.Join(dir, "gextto.json")); err != nil {
		t.Fatalf("remove the configuration file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "gextto.json"), 0o755); err != nil {
		t.Fatalf("make the configuration path a directory: %v", err)
	}
	if !state.dryRun() {
		t.Fatal("unreadable configuration: dryRun() must fall back to the start-up snapshot")
	}
}
