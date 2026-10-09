package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// TestBrowseDirKeepsNavigable covers the setup-wizard folder picker: a typed
// path that does not exist yet (a folder to be created) or that points at a
// file must still open a browsable directory, so the picker never strands the
// user with an error and no way to go back up.
func TestBrowseDirKeepsNavigable(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	base := t.TempDir()
	sub := filepath.Join(base, "serie")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Path   string   `json:"path"`
		Parent string   `json:"parent"`
		Dirs   []string `json:"dirs"`
	}
	browse := func(path string) {
		t.Helper()
		status, _, body := webGet(t, server, "/api/browse_dir?path="+url.QueryEscape(path))
		if status != http.StatusOK {
			t.Fatalf("browse %q -> %d: %s", path, status, body)
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("browse %q: %v", path, err)
		}
	}

	// A path that does not exist yet resolves to the nearest existing ancestor.
	browse(filepath.Join(base, "does", "not", "exist"))
	if got.Path != base {
		t.Fatalf("missing path -> %q, want the existing ancestor %q", got.Path, base)
	}
	found := false
	for _, dir := range got.Dirs {
		if dir == sub {
			found = true
		}
	}
	if !found {
		t.Fatalf("existing subfolder %q not listed: %v", sub, got.Dirs)
	}

	// A file resolves to its containing directory.
	file := filepath.Join(sub, "episode.mkv")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	browse(file)
	if got.Path != sub {
		t.Fatalf("file path -> %q, want its directory %q", got.Path, sub)
	}
}
