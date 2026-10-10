//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

// defaultDataDir is the XDG data dir of the daemon: $XDG_DATA_HOME/gx-torrent,
// else ~/.local/share/gx-torrent.
func defaultDataDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "gx-torrent")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "gx-torrent")
	}
	return "gx-torrent-data"
}
