package gextto

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/amule"
)

// TestV2Ed2kPageRenders renders the eD2k page in its three states: off,
// unreachable and connected with servers and search results.
func TestV2Ed2kPageRenders(t *testing.T) {
	views := map[string]v2Ed2kView{
		"off":         {Enabled: false, ServersURL: defaultServerMetURL, NodesURL: defaultNodesDatURL},
		"unreachable": {Enabled: true, Error: "connection refused", Address: "127.0.0.1:4712"},
		"connected": {
			Enabled: true, Address: "127.0.0.1:4712", Network: "kad", Message: "Kad avviato.",
			Status: amule.Status{Version: "2.3.3", Ed2kConnected: true, HighID: true, ServerName: "Fake",
				KadConnected: true, KadRunning: true, DownloadRate: 2048, DownloadLimit: 0},
			Servers: []amule.Server{{Name: "Fake", Address: "10.0.0.1:4661", Users: 10, Connected: true}, {Address: "10.0.0.2:4661"}},
		},
	}
	for name, view := range views {
		var buffer bytes.Buffer
		if err := v2Templates.ExecuteTemplate(&buffer, "v2_ed2k", view); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(buffer.String(), `id="v2-ed2k-state"`) {
			t.Fatalf("%s: state block missing", name)
		}
	}
	var buffer bytes.Buffer
	results := v2Ed2kSearchView{Query: "x", Done: true, Results: []v2Ed2kResult{{Name: "a.mkv", Size: 10, Sources: 2, Link: "ed2k://|file|a.mkv|10|0123456789abcdef0123456789abcdef|/"}}}
	if err := v2Templates.ExecuteTemplate(&buffer, "v2_ed2k_results", results); err != nil {
		t.Fatal(err)
	}
	body := buffer.String()
	// Every eD2k result keeps its download button, and the list can be
	// narrowed without asking aMule for a new search.
	for _, want := range []string{
		`data-ed2k-filter`, `data-ed2k-filter-name`, `data-ed2k-filter-sources`,
		`data-ed2k-name="a.mkv"`, `data-ed2k-size="10"`, `data-ed2k-sources="2"`,
		`action="/ed2k/add"`, `name="links" value="ed2k://|file|a.mkv|10|0123456789abcdef0123456789abcdef|/"`,
		`aria-label="Scarica a.mkv"`, ">Scarica<",
		"Parole nel nome", "Fonti minime", "Dimensione minima",
		"Nascondi già noti", "Ordina per", "Azzera", `data-ed2k-shown`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("results fragment is missing %q", want)
		}
	}
	buffer.Reset()
	if err := v2Templates.ExecuteTemplate(&buffer, "v2_ed2k_message", map[string]any{"Message": "m", "Detail": "d", "Error": true}); err != nil {
		t.Fatal(err)
	}
}

// TestV2ArchiveShowsScaricaForEd2kResults guards the archive variant of the
// eD2k results: the block sits under the archive table and must keep the same
// Scarica button that queues the ed2k link in aMule. It was lost once already.
func TestV2ArchiveShowsScaricaForEd2kResults(t *testing.T) {
	link := "ed2k://|file|a.mkv|10|0123456789abcdef0123456789abcdef|/"
	data := v2TableData{
		View: "archive-t0", BaseView: "archive", Title: "Archivio", Colspan: 1,
		Ed2k: &v2Ed2kSearchView{Query: "x", Done: true, Results: []v2Ed2kResult{{Name: "a.mkv", Size: 10, Sources: 2, Link: link}}},
	}
	var buffer bytes.Buffer
	if err := v2Templates.ExecuteTemplate(&buffer, "v2_table_body", data); err != nil {
		t.Fatal(err)
	}
	body := buffer.String()
	for _, want := range []string{
		"archive-ed2k-results",
		`action="/ed2k/add"`,
		`name="links" value="` + link + `"`,
		`aria-label="Scarica a.mkv"`,
		">Scarica<",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("archive eD2k results are missing %q", want)
		}
	}
}

func TestV2Ed2kPageThroughTheRouter(t *testing.T) {
	handler := Router(newTestAppState(t))
	request := httptest.NewRequest(http.MethodGet, "/?view=ed2k", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `id="v2-ed2k-state"`) {
		t.Fatalf("status %d: %.300s", recorder.Code, recorder.Body.String())
	}
}
