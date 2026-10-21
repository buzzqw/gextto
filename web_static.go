package gextto

// Static asset caching for the web interface.
//
// The UI used to answer every /static request with "Cache-Control: no-cache"
// and no validator (the embedded files have no modification time), so each
// page navigation downloaded ~290 KB of CSS and JavaScript again. The assets
// now carry a content ETag and a long cache lifetime when the page references
// them with its build number (?v=<build>), which the shell does. After a
// rebuild the HTML changes and points at a new URL, so a cached asset can
// never be stale while repeat navigation costs no request at all.

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
)

var (
	staticETagsOnce sync.Once
	staticETags     map[string]string
)

// v2StaticETag returns the strong ETag of one embedded asset, computed once
// from its content. The name is the path below /static.
func v2StaticETag(name string) string {
	staticETagsOnce.Do(func() {
		staticETags = map[string]string{}
		root := v2StaticFSRoot()
		if root == nil {
			return
		}
		_ = fs.WalkDir(root, ".", func(current string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return nil
			}
			content, readErr := fs.ReadFile(root, current)
			if readErr != nil {
				return nil
			}
			sum := sha256.Sum256(content)
			staticETags[current] = `"` + hex.EncodeToString(sum[:8]) + `"`
			return nil
		})
	})
	return staticETags[name]
}

// etagMatches implements the If-None-Match list comparison (weak validators
// included, since a proxy may turn a strong ETag into a weak one).
func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		if strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

// v2StaticHandler serves the embedded assets with a content validator and a
// cache lifetime chosen from the version query the shell adds.
func v2StaticHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/static")
		name = strings.TrimPrefix(name, "/")
		if etag := v2StaticETag(name); etag != "" {
			header := w.Header()
			header.Set("ETag", etag)
			header.Set("Vary", "Accept-Encoding")
			if r.URL.Query().Get("v") != "" {
				header.Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				// Old bookmarks and pages built before this change: cache for a
				// short while and revalidate with the ETag.
				header.Set("Cache-Control", "public, max-age=300")
			}
			if etagMatches(r.Header.Get("If-None-Match"), etag) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
