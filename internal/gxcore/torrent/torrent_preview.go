package torrent

// torrent_preview.go exposes the file list of a .torrent without adding it, so
// the daemon's add dialog can let the operator choose the files first. It is a
// thin public wrapper around the metainfo reader: the engine packages live
// under internal/gxcore and cmd/gx-torrent cannot import them directly.

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
)

// FilePreview is one downloadable file of a .torrent.
type FilePreview struct {
	Name   string
	Length int64
}

// PreviewFiles decodes data and returns the files in download order, padding
// files excluded. The name is relative to the torrent root (the root folder is
// stripped when the metainfo carries it). A single-file torrent yields one
// entry named after the torrent.
func PreviewFiles(data []byte) ([]FilePreview, error) {
	mi, err := metainfo.New(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	files := make([]FilePreview, 0, len(mi.Info.Files))
	root := filepath.ToSlash(mi.Info.Name)
	for _, f := range mi.Info.Files {
		if f.Padding {
			continue
		}
		name := filepath.ToSlash(f.Path)
		if root != "" && strings.HasPrefix(name, root+"/") {
			name = name[len(root)+1:]
		}
		files = append(files, FilePreview{Name: name, Length: f.Length})
	}
	if len(files) == 0 {
		files = append(files, FilePreview{Name: mi.Info.Name, Length: mi.Info.Length})
	}
	return files, nil
}
