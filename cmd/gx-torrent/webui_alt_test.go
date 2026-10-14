package main

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/settings"
)

func TestAltWebUIExtract(t *testing.T) {
	dir := t.TempDir()

	// A valid archive extracts under the destination.
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("vuetorrent/public/index.html")
	_, _ = f.Write([]byte("<html>alt</html>"))
	f2, _ := w.Create("vuetorrent/version.txt")
	_, _ = f2.Write([]byte("2.36.1"))
	_ = w.Close()
	if err := altWebUIExtract(buf.Bytes(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "vuetorrent", "public", "index.html")); err != nil {
		t.Fatalf("index.html not extracted: %v", err)
	}

	// A zip-slip entry (../) is neutralized: it lands inside the destination,
	// never outside it.
	buf.Reset()
	w = zip.NewWriter(&buf)
	f, _ = w.Create("../evil.txt")
	_, _ = f.Write([]byte("x"))
	_ = w.Close()
	evilDir := t.TempDir()
	if err := altWebUIExtract(buf.Bytes(), evilDir); err != nil {
		t.Fatalf("a ../ entry must be neutralized, not error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(evilDir, "evil.txt")); err != nil {
		t.Fatalf("the entry was not neutralized inside the destination: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(evilDir), "evil.txt")); err == nil {
		t.Fatal("the entry escaped the destination")
	}
}

func TestAltWebUIFallback(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(altWebUISettingKey, "true")
	d.opts.DataDir = t.TempDir()
	pub := d.altWebUIPublic()
	if err := os.MkdirAll(pub, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(pub, "index.html"), []byte("<html>ALT</html>"), 0o644)
	_ = os.WriteFile(filepath.Join(pub, "app.js"), []byte("ALTJS"), 0o644)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v2/app/version", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "API") })
	h := d.altWebUIFallback(mux)
	srv := httptest.NewServer(h)
	defer srv.Close()

	fetch := func(method, path string) (int, string) {
		req, err := http.NewRequest(method, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	// An unknown GET falls back to the alternative index.
	if code, body := fetch("GET", "/"); code != http.StatusOK || body != "<html>ALT</html>" {
		t.Fatalf("root: %d %q", code, body)
	}
	// Its assets are served.
	if _, body := fetch("GET", "/app.js"); body != "ALTJS" {
		t.Fatalf("asset: %q", body)
	}
	// The API always goes to the mux.
	if _, body := fetch("GET", "/api/v2/app/version"); body != "API" {
		t.Fatalf("api: %q", body)
	}
	// A POST is never handled by the alternative interface.
	mux.HandleFunc("POST /api/v2/x", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "P") })
	if code, _ := fetch("POST", "/api/v2/x"); code != http.StatusOK {
		t.Fatalf("post: %d", code)
	}

	// With the feature off the fallback is inert.
	d.opts.Settings.Set(altWebUISettingKey, "false")
	if code, _ := fetch("GET", "/"); code != http.StatusNotFound {
		t.Fatalf("disabled root: %d", code)
	}
}

// TestApplyAltWebUICheckboxEnablesAndPersists: the install button lives in the
// settings form, so "check + install" must enable the interface in one step and
// write the settings file (the store keeps values in memory until a save). An
// unchecked submission must leave the setting alone.
func TestApplyAltWebUICheckboxEnablesAndPersists(t *testing.T) {
	dir := t.TempDir()
	store, err := settings.Load(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{opts: Options{Mode: ModeStandalone, Settings: store}}
	post := func(body string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/ui/webui-install", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}

	if err := d.applyAltWebUICheckbox(post("webui-alternative=1")); err != nil {
		t.Fatal(err)
	}
	if !d.altWebUIEnabled() {
		t.Fatal("a checked install must enable the alternative interface")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"webui-alternative": "true"`) {
		t.Fatalf("the switch was not persisted, it would be lost on restart: %s", raw)
	}

	if err := d.applyAltWebUICheckbox(post("")); err != nil {
		t.Fatal(err)
	}
	if !d.altWebUIEnabled() {
		t.Fatal("an unchecked install must not disable the interface")
	}
}
