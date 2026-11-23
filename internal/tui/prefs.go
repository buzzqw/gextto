package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// prefs is the small set of TUI settings kept on the client machine. Daemon
// settings (language, refresh, limits, dry-run) are saved by the daemon itself;
// these are only how the terminal client draws itself.
type prefs struct {
	ColorsEnabled *bool `json:"colors_enabled,omitempty"`
	HighContrast  *bool `json:"high_contrast,omitempty"`
}

// prefsPath points at the local preferences file. GEXTTO_TUI_CONFIG allows a
// different location (and keeps tests away from the real user config).
func prefsPath(getenv func(string) string) (string, error) {
	if value := strings.TrimSpace(getenv("GEXTTO_TUI_CONFIG")); value != "" {
		return value, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gextto", "tui.json"), nil
}

func loadPrefs(getenv func(string) string) prefs {
	path, err := prefsPath(getenv)
	if err != nil {
		return prefs{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return prefs{}
	}
	var stored prefs
	if json.Unmarshal(data, &stored) != nil {
		return prefs{}
	}
	return stored
}

// save writes the preferences atomically, so an interrupted write cannot
// leave a half file behind. It is best effort: the TUI keeps working when the
// config directory is not writable.
func (p prefs) save(getenv func(string) string) error {
	path, err := prefsPath(getenv)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".tui-prefs-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

func (p prefs) same(other prefs) bool {
	return boolPtrEqual(p.ColorsEnabled, other.ColorsEnabled) && boolPtrEqual(p.HighContrast, other.HighContrast)
}

func boolPtr(value bool) *bool { return &value }

func boolPtrEqual(left, right *bool) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}
