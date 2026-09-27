package gextto

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

// embeddedUI bundles the compiled single-page web interface so the daemon is a
// single self-contained binary, like the original release archive. A disk
// directory (GEXTTO_UI_DIR or `<exe>/ui`) still takes precedence, so the UI can
// be replaced without rebuilding.
//
//go:embed webui
var embeddedUI embed.FS

// embeddedUISub returns the embedded UI filesystem rooted at the site directory.
func embeddedUISub() (fs.FS, bool) {
	sub, err := fs.Sub(embeddedUI, "webui")
	if err != nil {
		return nil, false
	}
	return sub, true
}

// readDiskAsset reads an asset from the on-disk UI directory.
func readDiskAsset(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(UiSiteDir(), name))
}

// uiAsset reads an asset by site-relative path from disk first, then from the
// embedded bundle.
func uiAsset(name string) ([]byte, bool) {
	if data, err := readDiskAsset(name); err == nil {
		return data, true
	}
	if sub, ok := embeddedUISub(); ok {
		if data, err := fs.ReadFile(sub, name); err == nil {
			return data, true
		}
	}
	return nil, false
}

// uiBundleAvailable reports whether a compiled UI (ui.js) is reachable.
func uiBundleAvailable() bool {
	if info, err := os.Stat(filepath.Join(UiPkgDir(), "ui.js")); err == nil && !info.IsDir() {
		return true
	}
	if sub, ok := embeddedUISub(); ok {
		if info, err := fs.Stat(sub, "pkg/ui.js"); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// uiPackageHandler serves `/pkg/*` from disk when present, else embedded.
func uiPackageHandler() http.Handler {
	if info, err := os.Stat(UiPkgDir()); err == nil && info.IsDir() {
		return http.StripPrefix("/pkg/", http.FileServer(http.Dir(UiPkgDir())))
	}
	if sub, ok := embeddedUISub(); ok {
		if pkg, err := fs.Sub(sub, "pkg"); err == nil {
			return http.StripPrefix("/pkg/", http.FileServer(http.FS(pkg)))
		}
	}
	return http.NotFoundHandler()
}
