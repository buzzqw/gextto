package gextto

// Static asset caching for the web interface.
//
// The UI used to answer every /static request with "Cache-Control: no-cache"
// and no validator (the embedded files have no modification time), so each
// page navigation downloaded ~290 KB of CSS and JavaScript again. The assets
// now carry a content ETag and a long cache lifetime when the page references
// them with the asset version (?v=<build>-<fingerprint>), which the shell does.
// The version carries a hash of the embedded files and not only the build
// number: a release that changes just a stylesheet keeps the same build, and
// the browser, which caches ?v= immutably, would otherwise keep the old file
// for a year. After any change the HTML points at a new URL, so a cached asset
// can never be stale while repeat navigation costs no request at all.

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
)

var (
	staticETagsOnce sync.Once
	staticETags     map[string]string

	staticFingerprintOnce sync.Once
	staticFingerprint     string
)

// v2StaticETags returns the strong ETag of every embedded asset, computed once
// from its content. The key is the path below /static.
func v2StaticETags() map[string]string {
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
	return staticETags
}

// v2StaticETag returns the strong ETag of one embedded asset.
func v2StaticETag(name string) string {
	return v2StaticETags()[name]
}

// v2StaticFingerprint is a short hash of every embedded asset, in name order:
// it changes whenever one of them changes, whatever the build number.
func v2StaticFingerprint() string {
	staticFingerprintOnce.Do(func() {
		staticFingerprint = staticFingerprintOf(v2StaticETags())
	})
	return staticFingerprint
}

// staticFingerprintOf hashes a set of content checksums. The map order must not
// matter, so the names are sorted first.
func staticFingerprintOf(etags map[string]string) string {
	names := make([]string, 0, len(etags))
	for name := range etags {
		names = append(names, name)
	}
	sort.Strings(names)
	digest := sha256.New()
	for _, name := range names {
		_, _ = digest.Write([]byte(name))
		_, _ = digest.Write([]byte(etags[name]))
	}
	return hex.EncodeToString(digest.Sum(nil)[:4])
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
