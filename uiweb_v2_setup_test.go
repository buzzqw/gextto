package gextto

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV2FreshInstallOpensSetupWizard(t *testing.T) {
	state := newTestAppState(t)
	recorder := httptest.NewRecorder()
	V2Page(recorder, httptest.NewRequest(http.MethodGet, "/", nil), state)
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/?view=setup" {
		t.Fatalf("fresh install: %d %q, want a redirect to the wizard", recorder.Code, recorder.Header().Get("Location"))
	}

	// An explicit view is never redirected, and a completed setup is not either.
	recorder = httptest.NewRecorder()
	V2Page(recorder, httptest.NewRequest(http.MethodGet, "/?view=dashboard", nil), state)
	if recorder.Code != http.StatusOK {
		t.Fatalf("explicit dashboard -> %d", recorder.Code)
	}
	if err := CompleteSetup(state.cfg); err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	V2Page(recorder, httptest.NewRequest(http.MethodGet, "/", nil), state)
	if recorder.Code != http.StatusOK {
		t.Fatalf("completed setup -> %d, want the dashboard", recorder.Code)
	}
}

func TestV2SetupStepsRender(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	for step, want := range map[string]string{
		"1": `action="/setup/auth"`,
		"2": `action="/setup/paths"`,
		"3": `action="/setup/sources"`,
		"4": `href="/?view=setup&amp;step=5"`,
		"5": `name="real_downloads"`,
	} {
		code, body := v2Request(t, server, http.MethodGet, "/?view=setup&step="+step, nil)
		if code != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("step %s -> %d, missing %q", step, code, want)
		}
	}
}

func TestV2SetupPathsCheckAndSave(t *testing.T) {
	state := newTestAppState(t)
	library := t.TempDir()
	downloads := filepath.Join(t.TempDir(), "downloads")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	// A missing library is refused (it is usually a NAS mount) and nothing is saved.
	response, err := client.PostForm(server.URL+"/setup/paths", url.Values{"action": {"save"}, "archive_root": {filepath.Join(library, "missing")}, "libtorrent_dir": {downloads}})
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if location := response.Header.Get("Location"); !strings.Contains(location, "msg_err=1") || !strings.Contains(location, "step=2") {
		t.Fatalf("missing library accepted: %q", location)
	}

	response, err = client.PostForm(server.URL+"/setup/paths", url.Values{"action": {"save"}, "archive_root": {library}, "libtorrent_dir": {downloads}})
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if location := response.Header.Get("Location"); !strings.Contains(location, "step=3") {
		t.Fatalf("valid folders not saved: %q", location)
	}
	if info, err := os.Stat(downloads); err != nil || !info.IsDir() {
		t.Fatalf("download folder not created: %v", err)
	}
	cfg := latestConfig(state)
	if cfg.ArchiveRoot == nil || *cfg.ArchiveRoot != library || cfg.LibtorrentDir != downloads {
		t.Fatalf("folders not stored: archive=%v downloads=%q", cfg.ArchiveRoot, cfg.LibtorrentDir)
	}
}

func TestV2SetupFinishMarksCompleted(t *testing.T) {
	state := newTestAppState(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	response, err := client.PostForm(server.URL+"/setup/finish", url.Values{"active": {"on"}, "real_downloads": {"on"}})
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	cfg := latestConfig(state)
	if !SetupComplete(cfg) || !cfg.Active || cfg.DryRun {
		t.Fatalf("finish: completed=%v active=%v dry_run=%v", SetupComplete(cfg), cfg.Active, cfg.DryRun)
	}
}

func TestV2SetupPathsKeepsUnchangedDownloadFolder(t *testing.T) {
	state := newTestAppState(t)
	library := t.TempDir()
	current := latestConfig(state).LibtorrentDir
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	response, err := client.PostForm(server.URL+"/setup/paths", url.Values{"action": {"save"}, "archive_root": {library}, "libtorrent_dir": {current}})
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if location := response.Header.Get("Location"); !strings.Contains(location, "step=3") {
		t.Fatalf("unchanged download folder refused: %q", location)
	}
}
