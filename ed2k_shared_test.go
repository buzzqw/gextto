package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/amule"
	"github.com/buzzqw/gextto/internal/amule/amuletest"
)

func TestEd2kSharedAPI(t *testing.T) {
	core, handler := ed2kTestRouter(t)
	core.SetShared([]amuletest.File{{
		Hash:           "ac9506f90a4e91273d5114f9bc1438a8",
		Name:           "Scrubs - S02E04 - My Big Mouth.mkv",
		Size:           529727691,
		Priority:       1,
		Transferred:    779238,
		TransferredAll: 2287181,
		Requests:       2,
		RequestsAll:    6,
		Accepts:        2,
		AcceptsAll:     6,
		Queued:         3,
	}}, core.Incoming)
	core.SetSharedDirs([]string{"/media/downloads"})
	core.SetUploads([]amule.UploadClient{{
		Name: "some-peer", Software: "eMule", IP: "10.0.0.9", FileName: "Scrubs - S02E04 - My Big Mouth.mkv",
		UpSpeed: 4096, Uploaded: 2048, WaitingPosition: 1,
	}})

	payload := ed2kRequest(t, handler, http.MethodGet, "/api/ed2k/shared", "")
	if payload["enabled"] != true {
		t.Fatalf("payload = %v", payload)
	}
	if payload["incoming"] != core.Incoming {
		t.Fatalf("incoming = %v", payload["incoming"])
	}
	dirs, _ := payload["shared_dirs"].([]any)
	if len(dirs) != 1 || dirs[0] != "/media/downloads" {
		t.Fatalf("shared dirs = %v", payload["shared_dirs"])
	}
	files, _ := payload["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files = %v", payload["files"])
	}
	file := files[0].(map[string]any)
	if file["name"] != "Scrubs - S02E04 - My Big Mouth.mkv" || file["transferred"].(float64) != 779238 ||
		file["requests_all"].(float64) != 6 || file["queued"].(float64) != 3 || file["priority"] != "normal" {
		t.Fatalf("file = %v", file)
	}
	if link, _ := file["link"].(string); !strings.HasPrefix(link, "ed2k://|file|Scrubs") {
		t.Fatalf("link = %q", file["link"])
	}
	uploads, _ := payload["uploads"].([]any)
	if len(uploads) != 1 || uploads[0].(map[string]any)["name"] != "some-peer" {
		t.Fatalf("uploads = %v", payload["uploads"])
	}

	ed2kRequest(t, handler, http.MethodPost, "/api/ed2k/shared/reload", "")
	commands := core.Commands()
	if len(commands) != 1 || commands[0] != "reload-shared" {
		t.Fatalf("commands = %v", commands)
	}
}

func TestEd2kSharedAPIIsOffByDefault(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveSetting(state.cfg.DataDir, "ed2k_enabled", "false"); err != nil {
		t.Fatal(err)
	}
	handler := Router(state)
	payload := ed2kRequest(t, handler, http.MethodGet, "/api/ed2k/shared", "")
	if payload["enabled"] != false {
		t.Fatalf("payload = %v", payload)
	}
}

func TestEd2kSharedPanelRendersAndReloads(t *testing.T) {
	core, handler := ed2kTestRouter(t)
	core.SetShared([]amuletest.File{{
		Hash: "ac9506f90a4e91273d5114f9bc1438a8", Name: "The.Pitt.S02E14.mkv", Size: 2941579665,
		Transferred: 563814, TransferredAll: 563814, Requests: 1, RequestsAll: 1, Accepts: 1, AcceptsAll: 1,
	}}, core.Incoming)
	core.SetSharedDirs([]string{"/media/movies"})

	request := httptest.NewRequest(http.MethodGet, "/ed2k/shared", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{"The.Pitt.S02E14.mkv", core.Incoming, "/media/movies", "Ricarica file condivisi", "Copia"} {
		if !strings.Contains(body, want) {
			t.Fatalf("panel is missing %q: %s", want, body)
		}
	}

	request = httptest.NewRequest(http.MethodPost, "/ed2k/shared/reload", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("reload status = %d, body %q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "Scansione dei file condivisi richiesta ad aMule.") {
		t.Fatalf("reload did not confirm: %s", recorder.Body.String())
	}
	commands := core.Commands()
	if len(commands) != 1 || commands[0] != "reload-shared" {
		t.Fatalf("commands = %v", commands)
	}
}
