package metainfo

import (
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// resolveDuplicatePaths makes the storage paths usable on a case-insensitive
// filesystem (Windows, macOS by default), where two entries whose names differ
// only in case are the same file: the second one would overwrite the first.
// It walks the path elements level by level and renames later colliding entries
// with a -N suffix before the extension (readme.txt -> readme-1.txt), the way
// libtorrent does, so the same torrent lays out the same way everywhere and a
// resume written on one system stays valid on another.
//
// A file whose path is also the folder of another file ("a" and "a/b") is
// still refused: no rename would make that layout writable, and failing at
// parse time beats failing half way through the download. Padding files are
// left alone, since BEP 47 gives every one of them the same ".pad/<length>"
// path.
func (i *Info) resolveDuplicatePaths() error {
	if err := checkFileDirCollisions(i.Files); err != nil {
		return err
	}
	if len(i.Files) < 2 {
		return nil
	}
	type elementKey struct {
		parent  string
		element string
	}
	// taken maps a final parent directory to the folded names in it; renamed
	// maps (final parent, original element) to the final element, so every path
	// that shares an original element gets the same renamed one.
	taken := make(map[string]map[string]struct{}, len(i.Files))
	renamed := make(map[elementKey]string, len(i.Files)*2)
	for idx := range i.Files {
		f := &i.Files[idx]
		if f.Padding {
			continue
		}
		parts := strings.Split(f.Path, string(filepath.Separator))
		parent := ""
		for pos, part := range parts {
			var final string
			if pos == len(parts)-1 {
				// A file leaf is always its own entry: two files with the same
				// name are two files, and the second one gets the suffix.
				final = uniqueElement(taken, parent, part)
			} else {
				// A directory is shared by every path that names it, so the
				// first rename must be remembered.
				key := elementKey{parent: parent, element: part}
				final = renamed[key]
				if final == "" {
					final = uniqueElement(taken, parent, part)
					renamed[key] = final
				}
			}
			if parent == "" {
				parent = final
			} else {
				parent += string(filepath.Separator) + final
			}
		}
		f.Path = parent
	}
	return nil
}

// uniqueElement returns name, or name-1/name-2... when the folded name is
// already used under parent, and records the chosen name in taken. The suffix
// goes before the extension, so a file keeps its type.
func uniqueElement(taken map[string]map[string]struct{}, parent, name string) string {
	set := taken[parent]
	if set == nil {
		set = make(map[string]struct{})
		taken[parent] = set
	}
	if _, used := set[strings.ToLower(name)]; !used {
		set[strings.ToLower(name)] = struct{}{}
		return name
	}
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for n := 1; ; n++ {
		candidate := base + "-" + strconv.Itoa(n) + ext
		key := strings.ToLower(candidate)
		if _, used := set[key]; used {
			continue
		}
		set[key] = struct{}{}
		return candidate
	}
}
