package main

// export.go is the standalone export-dir feature (qBittorrent's export_dir and
// export_dir_fin): it copies a torrent's .torrent file to a folder when the
// metadata is known and again when the torrent completes. Off when the folders
// are empty; in managed mode Gextto owns the copies.

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

const (
	exportDirKey    = "export-dir"
	exportFinDirKey = "export-dir-fin"
)

// exportDirs reads the two export folders (empty = off).
func (d *Daemon) exportDirs() (dir, finDir string) {
	if d.opts.Settings == nil {
		return "", ""
	}
	return strings.TrimSpace(d.opts.Settings.Get(exportDirKey, "")),
		strings.TrimSpace(d.opts.Settings.Get(exportFinDirKey, ""))
}

// maybeExport copies the torrent's .torrent to the configured folders: once when
// the metadata is available (export_dir) and once on completion (export_dir_fin).
// It runs on the queue tick, under d.mu; the flags on the meta avoid repeats.
func (d *Daemon) maybeExport(t *torrent.Torrent, meta *torrentMeta, stats torrent.Stats, complete bool) {
	dir, finDir := d.exportDirs()
	if dir == "" && finDir == "" {
		return
	}
	if stats.Bytes.Total <= 0 {
		return // metadata not here yet
	}
	if dir != "" && !meta.ExportedTorrent {
		d.exportOnce(t, &meta.ExportedTorrent, dir, stats.Name)
	}
	if complete && finDir != "" && !meta.ExportedFin {
		d.exportOnce(t, &meta.ExportedFin, finDir, stats.Name)
	}
}

func (d *Daemon) exportOnce(t *torrent.Torrent, done *bool, dir, name string) {
	data, err := t.Torrent()
	if err != nil || len(data) == 0 {
		return
	}
	if err := exportTorrent(dir, name, data); err != nil {
		logf("export %s: %v", t.ID(), err)
		return
	}
	*done = true
	d.dirty = true
}

// exportTorrent writes data to dir/<name>.torrent, creating the folder if needed.
func exportTorrent(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, exportFilename(name)+".torrent"), data, 0o644)
}

// exportFilename turns the torrent name into a safe file name.
func exportFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.NewReplacer("/", "_", "\\", "_").Replace(name)
	if name == "" || name == "." || name == ".." {
		return "torrent"
	}
	return name
}
