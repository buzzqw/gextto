package gextto

// Watched folders: drop a `.torrent` or `.magnet` file into a directory and
// gextto adds it automatically.
//
// This is qBittorrent's "watched folders" and BiglyBT's `TorrentFolderWatcher`
// distilled to what gextto needs: a list of directories, an optional recursive
// scan, and a "delete after import" flag. The scanner and the magnet reader
// are pure functions so they can be unit-tested without a running daemon.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// WatchedFolder describes one directory monitored for torrent files.
type WatchedFolder struct {
	// Path is the directory to scan.
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
	// Recursive also scans subdirectories.
	Recursive bool `json:"recursive"`
	// DeleteAfter removes the file after a successful add (default true). When
	// false the worker keeps a processed-file memory so it is not added twice.
	DeleteAfter bool `json:"delete_after"`
}

// UnmarshalJSON applies the JSON defaults: `enabled` and `delete_after`
// default to true when the keys are absent. The plain Go zero value keeps them
// false, matching `WatchedFolder::default()`.
func (w *WatchedFolder) UnmarshalJSON(data []byte) error {
	var raw struct {
		Path        string `json:"path"`
		Enabled     *bool  `json:"enabled"`
		Recursive   bool   `json:"recursive"`
		DeleteAfter *bool  `json:"delete_after"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	w.Path = raw.Path
	w.Enabled = true
	if raw.Enabled != nil {
		w.Enabled = *raw.Enabled
	}
	w.Recursive = raw.Recursive
	w.DeleteAfter = true
	if raw.DeleteAfter != nil {
		w.DeleteAfter = *raw.DeleteAfter
	}
	return nil
}

// LoadWatchedFolders loads watched folders from the `watched_folders` settings
// key. A missing or unparseable value yields an empty list.
func LoadWatchedFolders(settings map[string]string) []WatchedFolder {
	value, ok := settings["watched_folders"]
	if !ok {
		return nil
	}
	var folders []WatchedFolder
	if err := json.Unmarshal([]byte(value), &folders); err != nil {
		return nil
	}
	return folders
}

// SaveWatchedFolders stores watched folders under the `watched_folders`
// settings key as JSON.
func SaveWatchedFolders(dataDir string, folders []WatchedFolder) error {
	if folders == nil {
		folders = []WatchedFolder{}
	}
	encoded, err := json.Marshal(folders)
	if err != nil {
		return fmt.Errorf("serialize watched folders: %w", err)
	}
	return SaveSetting(dataDir, "watched_folders", string(encoded))
}

// ValidateWatchedFolders returns an error message, or an empty string when the
// list is valid.
func ValidateWatchedFolders(folders []WatchedFolder) string {
	if len(folders) > 50 {
		return "too many watched folders (max 50)"
	}
	for _, folder := range folders {
		if strings.TrimSpace(folder.Path) == "" || len(folder.Path) > 4096 {
			return "a watched folder has an empty or oversized path"
		}
	}
	return ""
}

// is_candidate reports whether a path names a `.torrent`/`.magnet` file worth
// importing, skipping hidden and partial files.
func is_candidate(path string) bool {
	name := filepath.Base(path)
	if strings.HasPrefix(name, ".") {
		return false
	}
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".part") || strings.HasSuffix(lower, ".tmp") {
		return false
	}
	return strings.HasSuffix(lower, ".torrent") || strings.HasSuffix(lower, ".magnet")
}

// visit walks a directory collecting candidate files.
func visit(dir string, recursive bool, out *[]string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			if recursive {
				visit(path, recursive, out)
			}
			continue
		}
		if is_candidate(path) {
			*out = append(*out, path)
		}
	}
}

// ScanFolder lists the `.torrent`/`.magnet` files in a watched folder, sorted
// for a stable processing order.
func ScanFolder(folder WatchedFolder) []string {
	root := strings.TrimSpace(folder.Path)
	if root == "" {
		return []string{}
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return []string{}
	}
	out := []string{}
	visit(root, folder.Recursive, &out)
	sort.Strings(out)
	return out
}

// MagnetFromFile reads the magnet URI from a `.magnet` file (the file may
// contain extra text; the first `magnet:?` token wins).
func MagnetFromFile(path string) *string {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := string(content)
	start := strings.Index(text, "magnet:?")
	if start < 0 {
		return nil
	}
	candidate := text[start:]
	end := strings.IndexFunc(candidate, func(character rune) bool {
		return unicode.IsSpace(character) || strings.ContainsRune("\"'<>", character)
	})
	if end < 0 {
		end = len(candidate)
	}
	magnet := strings.TrimSpace(candidate[:end])
	if magnet == "" {
		return nil
	}
	return &magnet
}

// Consume removes a successfully imported file, or renames it to `*.imported`
// when `delete` is false so the scanner does not pick it up again.
func Consume(path string, delete bool) error {
	if delete {
		return os.Remove(path)
	}
	extension := filepath.Ext(path)
	newExtension := "imported"
	if extension != "" {
		newExtension = extension[1:] + ".imported"
	}
	target := strings.TrimSuffix(path, extension) + "." + newExtension
	return os.Rename(path, target)
}
