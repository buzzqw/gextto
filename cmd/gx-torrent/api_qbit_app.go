package main

// api_qbit_app.go completes the read-only qBittorrent application endpoints
// used by the alternative interfaces and by mobile clients to pick a folder
// and to show the free space: getFreeSpace and getDirectoryContent. They are
// the same read-only view the standalone folder picker already exposes, under
// the qBittorrent API and behind the same authentication.

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// qbitDirEntry is one row of getDirectoryContent. qBittorrent answers with the
// name, whether it is a directory and the size in bytes; an interface builds
// the path by appending the name to the requested folder.
type qbitDirEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

// handleQbitGetFreeSpace answers the free space of a folder (the download
// folder when the parameter is empty). qBittorrent returns a plain number of
// bytes.
func (d *Daemon) handleQbitGetFreeSpace(w http.ResponseWriter, r *http.Request) {
	dir := strings.TrimSpace(r.URL.Query().Get("path"))
	if dir == "" {
		dir = d.opts.DownloadDir
	}
	dir = d.qbitDirectory(dir)
	free, _ := diskFree(dir)
	writeJSON(w, http.StatusOK, free)
}

// handleQbitGetDirectoryContent lists one folder for the qBittorrent path
// pickers. A path that is a file resolves to its folder; a folder that cannot
// be read is a 404 instead of an empty listing, so the client can tell the two
// apart.
func (d *Daemon) handleQbitGetDirectoryContent(w http.ResponseWriter, r *http.Request) {
	dir := strings.TrimSpace(r.URL.Query().Get("dirPath"))
	if dir == "" {
		dir = d.opts.DownloadDir
	}
	dir = d.qbitDirectory(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	out := make([]qbitDirEntry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		item := qbitDirEntry{Name: entry.Name(), IsDir: entry.IsDir(), Size: info.Size()}
		// A link to a folder is shown as a folder, like the folder picker does.
		if !item.IsDir && info.Mode()&os.ModeSymlink != 0 {
			if target, statErr := os.Stat(filepath.Join(dir, entry.Name())); statErr == nil && target.IsDir() {
				item.IsDir = true
			}
		}
		if item.IsDir {
			item.Size = 0
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	writeJSON(w, http.StatusOK, out)
}

// qbitDirectory turns a client path into an absolute existing folder: a file
// resolves to its parent, a missing path to the closest existing ancestor, and
// an empty value to the download folder. The listing is read-only and behind
// the same login as the page.
func (d *Daemon) qbitDirectory(path string) string {
	if strings.TrimSpace(path) == "" {
		path = d.opts.DownloadDir
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return d.opts.DownloadDir
	}
	info, err := os.Stat(absolute)
	if err == nil {
		if info.IsDir() {
			return absolute
		}
		return filepath.Dir(absolute)
	}
	if nearest := uiBrowseNearestDir(absolute); nearest != "" {
		return nearest
	}
	return d.opts.DownloadDir
}
