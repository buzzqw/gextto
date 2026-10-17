package torrent

// content_layout.go implements qBittorrent's content layout, chosen when a
// torrent is added: Original keeps the paths of the metainfo, Subfolder puts a
// single-file torrent in a folder named after it, NoSubfolder drops the root
// folder of a multi-file torrent so its files land straight in the save path.
//
// The layout is a storage-path override, the same mechanism as a rename: the
// info dict (and so the info hash) never changes. It is applied before the
// storage is allocated — at once for a .torrent, when the metadata arrives for
// a magnet — so no file is ever moved.

import (
	"path"
	"strings"

	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
)

// Content layouts, spelled as qBittorrent's contentLayout parameter.
const (
	ContentLayoutOriginal    = "Original"
	ContentLayoutSubfolder   = "Subfolder"
	ContentLayoutNoSubfolder = "NoSubfolder"
)

// ParseContentLayout reads a layout name case-insensitively; empty means
// Original. ok is false for an unknown name.
func ParseContentLayout(value string) (layout string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "original":
		return ContentLayoutOriginal, true
	case "subfolder", "create_subfolder":
		return ContentLayoutSubfolder, true
	case "nosubfolder", "no_subfolder", "dont_create_subfolder":
		return ContentLayoutNoSubfolder, true
	}
	return "", false
}

// layoutPaths returns the storage paths of info's files under the layout, or
// nil when the layout changes nothing (Original, a multi-file torrent asked for
// a subfolder, a single file asked for none) or would make two files collide.
func layoutPaths(info *metainfo.Info, layout string) []string {
	if info == nil || len(info.Files) == 0 {
		return nil
	}
	single := len(info.Files) == 1 && !strings.Contains(info.Files[0].Path, "/")
	paths := make([]string, len(info.Files))
	switch layout {
	case ContentLayoutSubfolder:
		if !single {
			return nil
		}
		name := info.Files[0].Path
		folder := strings.TrimSuffix(name, path.Ext(name))
		if folder == "" || folder == "." {
			folder = name
		}
		paths[0] = folder + "/" + name
	case ContentLayoutNoSubfolder:
		if single {
			return nil
		}
		for i, f := range info.Files {
			idx := strings.IndexByte(f.Path, '/')
			if idx < 0 || idx == len(f.Path)-1 {
				return nil
			}
			paths[i] = f.Path[idx+1:]
		}
	default:
		return nil
	}
	if checkPathCollisions(info.Files, paths) != nil {
		return nil
	}
	return paths
}

// applyContentLayout overrides the storage paths of a torrent whose metadata
// just became known, and records them so the resume and a compaction keep
// them. It runs before allocation and reports whether anything changed.
func (t *torrent) applyContentLayout(info *metainfo.Info) bool {
	paths := layoutPaths(info, t.contentLayout)
	if paths == nil || !info.SetFilePaths(paths) {
		return false
	}
	t.filePaths = paths
	return true
}

// ContentRoot is the top-level entry of the torrent's data under its save path:
// the first component of the first stored file path (a rename or the content
// layout may have moved it away from the name). Empty while the metadata is
// unknown. With the NoSubfolder layout a multi-file torrent has several
// top-level entries and this is only the first of them.
func (t *Torrent) ContentRoot() string {
	info := t.torrent.info()
	if info == nil {
		return ""
	}
	for _, f := range info.Files {
		if f.Padding {
			continue
		}
		root := f.Path
		if idx := strings.IndexByte(root, '/'); idx >= 0 {
			root = root[:idx]
		}
		return root
	}
	return ""
}

// FileCount is the number of non-padding files (0 while the metadata is
// unknown).
func (t *Torrent) FileCount() int {
	info := t.torrent.info()
	if info == nil {
		return 0
	}
	n := 0
	for _, f := range info.Files {
		if !f.Padding {
			n++
		}
	}
	return n
}
