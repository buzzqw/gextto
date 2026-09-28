package gextto

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFolderRenameScanProposesTMDBEpisodeName(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/tv":
			tmdbWriteJSON(t, w, `{"results":[{"id":1399,"name":"The Office","first_air_date":"2005-03-24"}]}`)
		case "/tv/1399/season/1/episode/1":
			tmdbWriteJSON(t, w, `{"name":"Pilot"}`)
		default:
			t.Fatalf("unexpected TMDB path %q", r.URL.Path)
		}
	})

	root := t.TempDir()
	source := filepath.Join(root, "The.Office.S01E01.1080p.mkv")
	if err := os.WriteFile(source, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := "test-key"
	cfg := DefaultConfig()
	cfg.TmdbAPIKey = &key
	items, err := folderRenameScan(t.Context(), root, &cfg)
	if err != nil {
		t.Fatalf("folderRenameScan: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	item := items[0]
	if item.Kind != "series" || item.Status != "proposta" {
		t.Fatalf("item classification = %#v", item)
	}
	if !strings.Contains(filepath.Base(item.Target), "The Office - S01E01 - Pilot") {
		t.Fatalf("target = %q", item.Target)
	}
}
