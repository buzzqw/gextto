package main

// dlna_library.go adds the library folders to the DLNA media server: besides
// the torrents, a TV browses configured directories (the archive on the NAS
// Gextto files the media into) as a real folder tree and plays their video and
// audio files. Every access goes through os.Root, so ".." or a symlink cannot
// leave the folder; hidden entries and the NAS system folders (Synology
// @eaDir, #recycle) are skipped, and the excluded paths (Gextto's trash) are
// neither listed nor served.

import (
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	dlnaFoldersKey = "dlna-folders"
	// dlnaTorrentsID is the container of the torrents when library folders
	// are configured (without folders the torrents sit directly in the root).
	dlnaTorrentsID = "torrents"
	dlnaLibPrefix  = "lib:"
	// dlnaDirTTL bounds how long a directory listing is reused: a TV pages a
	// folder with several Browse calls, and the folder may be on a NAS.
	dlnaDirTTL      = 30 * time.Second
	dlnaDirCacheMax = 128
)

// dlnaSplitList splits a folder list: one per line or separated by ";".
func dlnaSplitList(raw string) []string {
	var out []string
	for _, line := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == ';' }) {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// dlnaFolders is the list of library folders: settings.json in standalone,
// else the flags Gextto passed in managed mode.
func (d *Daemon) dlnaFolders() []string {
	if d.opts.Settings != nil {
		if v := dlnaSplitList(d.opts.Settings.Get(dlnaFoldersKey, "")); len(v) > 0 {
			return v
		}
	}
	return d.opts.DLNAFolders
}

// dlnaLibRoot is one library folder exposed as a top-level container.
type dlnaLibRoot struct {
	name string // shown title: the folder's base name
	path string // absolute, cleaned
}

// dlnaLibRoots turns the configured folders into roots: absolute, cleaned,
// deduplicated, in the configured order. A folder that is missing now (a NAS
// not mounted yet) is kept: its container is empty until it comes back.
func dlnaLibRoots(folders []string) []dlnaLibRoot {
	var roots []dlnaLibRoot
	seen := map[string]bool{}
	for _, f := range folders {
		abs, err := filepath.Abs(f)
		if err != nil || seen[abs] {
			continue
		}
		seen[abs] = true
		name := filepath.Base(abs)
		if name == string(filepath.Separator) || name == "." {
			name = abs
		}
		roots = append(roots, dlnaLibRoot{name: name, path: abs})
	}
	return roots
}

// dlnaSkipName reports whether a directory entry is hidden from the TV: dot
// files and the NAS system folders (Synology @eaDir thumbnails, #recycle,
// #snapshot).
func dlnaSkipName(name string) bool {
	return name == "" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") || strings.HasPrefix(name, "#")
}

// dlnaLibID is the object id of a path inside library root n ("" = the root).
func dlnaLibID(n int, rel string) string {
	id := dlnaLibPrefix + strconv.Itoa(n)
	if rel != "" {
		id += "/" + rel
	}
	return id
}

// parseLibID splits a lib:<n>[/<rel>] id. rel is cleaned and must stay inside
// the root (no "..", not absolute).
func parseLibID(id string, roots int) (n int, rel string, ok bool) {
	rest, found := strings.CutPrefix(id, dlnaLibPrefix)
	if !found {
		return 0, "", false
	}
	num, rel, _ := strings.Cut(rest, "/")
	n, err := strconv.Atoi(num)
	if err != nil || n < 0 || n >= roots || strconv.Itoa(n) != num {
		return 0, "", false
	}
	rel, ok = cleanLibRel(rel)
	return n, rel, ok
}

// cleanLibRel cleans a slash path relative to a library root; "" is the root.
func cleanLibRel(rel string) (string, bool) {
	if rel == "" {
		return "", true
	}
	if strings.ContainsRune(rel, 0) || strings.HasPrefix(rel, "/") {
		return "", false
	}
	clean := path.Clean(rel)
	if clean == "." {
		return "", true
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || !fs.ValidPath(clean) {
		return "", false
	}
	for _, part := range strings.Split(clean, "/") {
		if dlnaSkipName(part) {
			return "", false
		}
	}
	return clean, true
}

// libParentID is the parent object id of lib:<n>/<rel>.
func libParentID(n int, rel string) string {
	if rel == "" {
		return dlnaRootID
	}
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}
	return dlnaLibID(n, dir)
}

// dlnaDirListing is one directory of a library: its subfolders and its
// playable files (with their subtitles), both sorted by name.
type dlnaDirListing struct {
	dirs   []string
	folder dlnaFolder // items' fileIndex points into names
	names  []string
	// images are the artwork images of the directory (lowercased name ->
	// name); folderArt is the folder's image and itemArt the image of each
	// video by file name, both as paths relative to the library root, since a
	// season folder takes its poster from the series folder (dlna_art.go).
	images    map[string]string
	folderArt string
	itemArt   map[string]string
}

// excluded reports whether an absolute path is (inside) an excluded folder.
func (s *dlnaServer) excluded(abs string) bool {
	for _, ex := range s.exclude {
		if abs == ex || strings.HasPrefix(abs, ex+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// readLibDir lists one directory of library root n, cached for dlnaDirTTL.
func (s *dlnaServer) readLibDir(n int, rel string) (*dlnaDirListing, bool) {
	key := strconv.Itoa(n) + "\x00" + rel
	s.dirMu.Lock()
	if c, ok := s.dirCache[key]; ok && time.Since(c.at) < dlnaDirTTL {
		s.dirMu.Unlock()
		return c.listing, true
	}
	s.dirMu.Unlock()

	listing, ok := s.scanLibDir(n, rel)
	if !ok {
		return nil, false
	}
	s.dirMu.Lock()
	if s.dirCache == nil || len(s.dirCache) >= dlnaDirCacheMax {
		s.dirCache = map[string]dlnaDirCacheEntry{}
	}
	s.dirCache[key] = dlnaDirCacheEntry{listing: listing, at: time.Now()}
	s.dirMu.Unlock()
	return listing, true
}

type dlnaDirCacheEntry struct {
	listing *dlnaDirListing
	at      time.Time
}

// scanLibDir reads one directory through os.Root (no escape from the root).
func (s *dlnaServer) scanLibDir(n int, rel string) (*dlnaDirListing, bool) {
	root := s.libRoots[n]
	if s.excluded(filepath.Join(root.path, filepath.FromSlash(rel))) {
		return nil, false
	}
	r, err := os.OpenRoot(root.path)
	if err != nil {
		return &dlnaDirListing{}, rel == "" // a missing root is an empty container
	}
	defer r.Close()
	dirPath := rel
	if dirPath == "" {
		dirPath = "."
	}
	f, err := r.Open(dirPath)
	if err != nil {
		return nil, false
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return nil, false
	}
	listing := &dlnaDirListing{}
	var files []dlnaFile
	images := map[string]string{} // lowercased name -> name
	for _, e := range entries {
		name := e.Name()
		if dlnaSkipName(name) {
			continue
		}
		childRel := path.Join(rel, name)
		if s.excluded(filepath.Join(root.path, filepath.FromSlash(childRel))) {
			continue
		}
		isDir := e.IsDir()
		mime := mediaContentType(name)
		wanted := dlnaPlayable(mime) || dlnaSubtitleKind(mime) != ""
		art := dlnaArtImage(name)
		if !isDir && !wanted && !art && e.Type()&fs.ModeSymlink == 0 {
			continue
		}
		info, err := r.Stat(childRel) // follows a symlink, but only inside the root
		if err != nil {
			continue
		}
		switch {
		case info.IsDir():
			listing.dirs = append(listing.dirs, name)
		case info.Mode().IsRegular() && wanted:
			listing.names = append(listing.names, name)
			files = append(files, dlnaFile{path: name, length: info.Size()})
		case info.Mode().IsRegular() && art:
			images[strings.ToLower(name)] = name
		}
	}
	sort.Slice(listing.dirs, func(a, b int) bool { return strings.ToLower(listing.dirs[a]) < strings.ToLower(listing.dirs[b]) })
	listing.folder, _ = dlnaFolderFor("", "", files)
	listing.images = images
	listing.assignArt(rel, s.seasonArt(n, rel))
	return listing, true
}

// libItem renders one library file as a DIDL item.
func (s *dlnaServer) writeLibItem(b *strings.Builder, n int, dirRel string, listing *dlnaDirListing, e dlnaEntry, base string) {
	urlOf := func(index int) string {
		return dlnaLibURL(base, n, path.Join(dirRel, listing.names[index]))
	}
	name := listing.names[e.fileIndex]
	id := dlnaLibID(n, path.Join(dirRel, name))
	art := ""
	if a := listing.itemArt[name]; a != "" {
		art = dlnaArtURL(base, n, a)
	}
	writeItemAt(b, e, id, dlnaLibID(n, dirRel), art, urlOf)
}

// dlnaLibURL is the media URL of a library file: /lib/<n>/<escaped path>.
func dlnaLibURL(base string, n int, rel string) string {
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return base + "/lib/" + strconv.Itoa(n) + "/" + strings.Join(parts, "/")
}

// writeLibContainer renders a library folder; artURL is its image ("" none).
func writeLibContainer(b *strings.Builder, id, parent, title, artURL string) {
	b.WriteString(`<container id="` + xmlEscape(id) + `" parentID="` + xmlEscape(parent) +
		`" restricted="1" searchable="0"><dc:title>` + xmlEscape(title) +
		`</dc:title><upnp:class>object.container.storageFolder</upnp:class>`)
	writeAlbumArt(b, artURL)
	b.WriteString(`</container>`)
}

// libFolderArtURL is the artwork URL of a library folder: /art/<n>/<dir>/.
// Whether the folder has an image is only known once it is read, so a
// subfolder always gets the URL and answers 404 when it has none: reading
// every subfolder to list a parent would cost a directory scan each.
func libFolderArtURL(base string, n int, rel string) string {
	if rel == "" {
		return base + "/art/" + strconv.Itoa(n) + "/"
	}
	return dlnaArtURL(base, n, rel) + "/"
}

// browseLib answers a Browse on a library object (lib:<n>[/<rel>]).
func (s *dlnaServer) browseLib(objectID, flag string, start, count int, base string) (string, int, int, *dlnaFault) {
	n, rel, ok := parseLibID(objectID, len(s.libRoots))
	if !ok || s.excluded(filepath.Join(s.libRoots[n].path, filepath.FromSlash(rel))) {
		return "", 0, 0, dlnaNoSuchObject
	}
	var b strings.Builder
	b.WriteString(didlOpen)
	finish := func(returned, total int) (string, int, int, *dlnaFault) {
		b.WriteString(`</DIDL-Lite>`)
		return b.String(), returned, total, nil
	}
	// The object is a directory when it lists; otherwise it must be a file
	// offered by its parent directory.
	listing, isDir := s.readLibDir(n, rel)
	if !isDir {
		dirRel := path.Dir(rel)
		if dirRel == "." {
			dirRel = ""
		}
		parent, ok := s.readLibDir(n, dirRel)
		if !ok {
			return "", 0, 0, dlnaNoSuchObject
		}
		e, found := parent.item(path.Base(rel))
		if !found {
			return "", 0, 0, dlnaNoSuchObject
		}
		switch flag {
		case "BrowseMetadata":
			s.writeLibItem(&b, n, dirRel, parent, e, base)
			return finish(1, 1)
		case "BrowseDirectChildren":
			return finish(0, 0)
		}
		return "", 0, 0, dlnaInvalidArgs
	}
	switch flag {
	case "BrowseMetadata":
		title := s.libRoots[n].name
		if rel != "" {
			title = path.Base(rel)
		}
		art := ""
		if listing.folderArt != "" {
			art = libFolderArtURL(base, n, rel)
		}
		writeLibContainer(&b, objectID, libParentID(n, rel), title, art)
		return finish(1, 1)
	case "BrowseDirectChildren":
		items := listing.folder.items
		total := len(listing.dirs) + len(items)
		end := total
		if count > 0 && start+count < total {
			end = start + count
		}
		returned := 0
		for i := start; i < end; i++ {
			if i < len(listing.dirs) {
				name := listing.dirs[i]
				child := path.Join(rel, name)
				writeLibContainer(&b, dlnaLibID(n, child), objectID, name, libFolderArtURL(base, n, child))
			} else {
				s.writeLibItem(&b, n, rel, listing, items[i-len(listing.dirs)], base)
			}
			returned++
		}
		return finish(returned, total)
	}
	return "", 0, 0, dlnaInvalidArgs
}

// item finds the playable file called name in a listing.
func (l *dlnaDirListing) item(name string) (dlnaEntry, bool) {
	for _, e := range l.folder.items {
		if l.names[e.fileIndex] == name {
			return e, true
		}
	}
	return dlnaEntry{}, false
}

// servable reports whether name is offered by the listing: a playable file or
// a subtitle attached to one. It returns the MIME type.
func (l *dlnaDirListing) servable(name string) (string, bool) {
	for _, e := range l.folder.items {
		if l.names[e.fileIndex] == name {
			return e.mime, true
		}
		for _, sub := range e.subs {
			if l.names[sub.fileIndex] == name {
				return sub.mime, true
			}
		}
	}
	return "", false
}

// handleLibMedia serves one library file with Range (http.ServeContent),
// through os.Root and only when the directory listing offers it.
func (s *dlnaServer) handleLibMedia(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 || n >= len(s.libRoots) || strconv.Itoa(n) != r.PathValue("n") {
		http.NotFound(w, r)
		return
	}
	rel, ok := cleanLibRel(r.PathValue("path"))
	if !ok || rel == "" {
		http.NotFound(w, r)
		return
	}
	dirRel := path.Dir(rel)
	if dirRel == "." {
		dirRel = ""
	}
	listing, ok := s.readLibDir(n, dirRel)
	if !ok {
		http.NotFound(w, r)
		return
	}
	mime, ok := listing.servable(path.Base(rel))
	if !ok {
		http.NotFound(w, r)
		return
	}
	root, err := os.OpenRoot(s.libRoots[n].path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	f, err := root.Open(rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet && s.throttle(&s.servedMu, &s.served, r.RemoteAddr+"lib"+strconv.Itoa(n)+rel) {
		logf("DLNA: %s plays %s", r.RemoteAddr, filepath.Join(s.libRoots[n].path, filepath.FromSlash(rel)))
	}
	h := w.Header()
	h.Set("Content-Type", mime)
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Disposition", contentDisposition(rel))
	if dlnaSubtitleKind(mime) != "" {
		h["transferMode.dlna.org"] = []string{"Interactive"}
	} else {
		h["transferMode.dlna.org"] = []string{"Streaming"}
		h["contentFeatures.dlna.org"] = []string{dlnaFeatures}
	}
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// dlnaDirCache fields live on dlnaServer (dirMu/dirCache); declared here so
// the library code stays in one file.
type dlnaDirCacheState struct {
	dirMu    sync.Mutex
	dirCache map[string]dlnaDirCacheEntry
}
