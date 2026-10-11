package main

// ui_browse.go serves the folder picker used by the standalone pages (first of
// all the setup wizard): a read-only listing of the daemon machine's
// directories, so the operator can choose a download/temp folder instead of
// typing the path. It is a standalone feature; managed mode refuses it, and it
// asks for the login like every other /ui/* page (before a password is set the
// wizard is open by design, so the picker is too).

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// uiBrowsePath is the picker endpoint; standaloneGuard lets it through before
// the setup is complete (the wizard is its first caller).
const uiBrowsePath = "/ui/browse-dir"

// uiIsDir reports whether path is an existing directory.
func uiIsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// uiBrowseDefaultPath is where the picker starts with no path: the daemon
// user's home when it exists, otherwise the filesystem root.
func uiBrowseDefaultPath() string {
	if home, err := os.UserHomeDir(); err == nil && uiIsDir(home) {
		return home
	}
	return string(filepath.Separator)
}

// uiBrowseNearestDir returns the closest existing directory at or above path, or
// "" when there is none. A hit that is not a directory resolves to its parent.
func uiBrowseNearestDir(path string) string {
	for current := path; ; {
		if info, err := os.Stat(current); err == nil {
			if info.IsDir() {
				return current
			}
			current = filepath.Dir(current)
			continue
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

// handleUIBrowseDir lists the sub-directories of a path for the picker. A
// missing or unreadable path is not an error: the nearest existing ancestor (or
// the reason the directory cannot be read) is reported so the picker stays
// navigable instead of stranding the user.
func (d *Daemon) handleUIBrowseDir(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) {
		return
	}
	requested := strings.TrimSpace(r.URL.Query().Get("path"))
	if requested == "" {
		requested = uiBrowseDefaultPath()
	}
	absolute, err := filepath.Abs(requested)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		// The path may not exist yet (the wizard offers folders created on
		// save): open the nearest existing ancestor.
		canonical = uiBrowseNearestDir(absolute)
		if canonical == "" {
			writeError(w, http.StatusNotFound, err)
			return
		}
	} else if info, statErr := os.Stat(canonical); statErr != nil || !info.IsDir() {
		canonical = filepath.Dir(canonical)
	}

	dirs := []string{}
	message := ""
	entries, err := os.ReadDir(canonical)
	if err != nil {
		// An unreadable directory still has to keep the picker usable: report
		// the reason but let it go back up.
		message = err.Error()
	} else {
		for _, entry := range entries {
			child := filepath.Join(canonical, entry.Name())
			if uiIsDir(child) {
				dirs = append(dirs, child)
			}
		}
	}
	sort.Strings(dirs)

	var parent string
	if up := filepath.Dir(canonical); up != canonical {
		parent = up
	}
	response := map[string]any{
		"path":   canonical,
		"parent": parent,
		"dirs":   dirs,
	}
	if message != "" {
		response["error"] = message
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, response)
}
