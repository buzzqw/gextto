//go:build windows

package main

import (
	"os"
	"path/filepath"
)

// defaultDataDir is the per-user data dir of the daemon:
// %LOCALAPPDATA%\gx-torrent, falling back to the user profile when
// LOCALAPPDATA is not set.
func defaultDataDir() string {
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		return filepath.Join(dir, "gx-torrent")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "gx-torrent")
	}
	return "gx-torrent-data"
}
