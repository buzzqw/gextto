package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestV2ListImportPreviewAndSelectedApply(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveSetting(state.cfg.DataDir, "dry_run", "no"); err != nil {
		t.Fatal(err)
	}
	if err := SaveSetting(state.cfg.DataDir, "simkl_client_id", "client"); err != nil {
		t.Fatal(err)
	}
	if err := SaveSetting(state.cfg.DataDir, "simkl_access_token", "token"); err != nil {
		t.Fatal(err)
	}
	simklTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		integrationWriteJSON(t, w, `{"shows":[{"show":{"title":"Imported Show","year":"2024","ids":{"tmdb":"101"}}}],"movies":[{"movie":{"title":"Imported Movie","year":"2025","ids":{"tvdb":"202"}}}]}`)
	})
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	if code, page := v2Request(t, server, http.MethodGet, "/?view=integrations", nil); code != http.StatusOK || !strings.Contains(page, `href="/?view=list-import"`) {
		t.Fatalf("integrations page -> %d: %s", code, page)
	}
	if code, panel := v2Request(t, server, http.MethodPost, "/list-import/preview", nil); code != http.StatusOK || !strings.Contains(panel, "Imported Show") || !strings.Contains(panel, "Imported Movie") {
		t.Fatalf("preview panel -> %d: %s", code, panel)
	}
	raw, status := v2InternalJSON(state, http.MethodGet, "/api/list-import/preview?source=simkl", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("preview api -> %d", status)
	}
	var preview listImportPreviewResponse
	if err := json.Unmarshal(raw, &preview); err != nil {
		t.Fatal(err)
	}
	seriesIndex := -1
	for i, item := range preview.Items {
		if item.Kind == "series" {
			seriesIndex = i
		}
	}
	if seriesIndex < 0 {
		t.Fatal("series missing from preview")
	}
	items, _ := json.Marshal(preview.Items)
	code, _ := v2Request(t, server, http.MethodPost, "/list-import/apply", url.Values{
		"items": {string(items)}, "accept__" + strconv.Itoa(seriesIndex): {"1"}, "dry_run": {"false"},
	})
	if code != http.StatusOK {
		t.Fatalf("apply panel -> %d", code)
	}
	cfg := latestConfig(state)
	if len(cfg.Movies) != 0 {
		t.Fatalf("unselected movie was imported: %+v", cfg.Movies)
	}
	if len(cfg.Series) != 1 || cfg.Series[0].Name != "Imported Show" {
		t.Fatalf("selected series = %+v", cfg.Series)
	}
}
