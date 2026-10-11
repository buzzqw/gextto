package torrent

import (
	"errors"
	"os"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
)

// CreateOptions configures CreateTorrent.
type CreateOptions struct {
	// Name is the torrent name. Empty means the base name of the path.
	Name string
	// Trackers are the announce URLs (kept as a single tier).
	Trackers []string
	// Webseeds are the BEP 19 HTTP seeds stored as "url-list".
	Webseeds []string
	// Private marks the torrent private (BEP 27).
	Private bool
	// PieceLength is the piece size in bytes (multiple of 16 KiB); 0 picks one
	// from the total size.
	PieceLength uint32
	// Comment is stored in the metainfo.
	Comment string
}

// CreateTorrent builds a BitTorrent v1 .torrent from a single file or directory
// on disk. A directory becomes a multi-file torrent named after it (or after
// opt.Name); a file becomes a single-file torrent. The returned bytes are a
// ready-to-use .torrent.
func CreateTorrent(path string, opt CreateOptions) ([]byte, error) {
	if path == "" {
		return nil, errors.New("no path specified")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	info, err := metainfo.NewInfoBytes("", []string{path}, opt.Private, opt.PieceLength, opt.Name, logger.New("create torrent"))
	if err != nil {
		return nil, err
	}
	var trackers [][]string
	if len(opt.Trackers) > 0 {
		trackers = [][]string{opt.Trackers}
	}
	return metainfo.NewBytes(info, trackers, opt.Webseeds, opt.Comment)
}
