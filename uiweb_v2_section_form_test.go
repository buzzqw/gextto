package gextto

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// postSectionForm submits a /section/form like the browser (HTMX) does and
// returns the redirect target.
func postSectionForm(t *testing.T, server *httptest.Server, form url.Values) string {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, server.URL+"/section/form", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("section form -> %d", response.StatusCode)
	}
	return response.Header.Get("HX-Redirect")
}

// TestV2SectionFormSavesBackupAndSimklSettings: the Maintenance backup form and
// the Simkl form really save their fields (they used to send a wrapped body the
// API read as a single unknown key: nothing saved, no error), and a rejected
// value comes back as an error toast instead of a silent reload.
func TestV2SectionFormSavesBackupAndSimklSettings(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	page := func(view string) string {
		_, body := v2Request(t, server, http.MethodGet, "/?view="+view, nil)
		return body
	}
	if body := page("maintenance"); !strings.Contains(body, `name="backup_extra_paths"`) || strings.Contains(body, `name="wrap" value="values"`) {
		t.Fatal("the backup form still wraps its values")
	}

	postSectionForm(t, server, url.Values{
		"view": {"maintenance"}, "path": {"/api/backup/settings"}, "wrap": {""},
		"backup_retention": {"7"}, "_v2_kind_backup_retention": {"number"},
		"backup_ftp_password":  {""},
		"backup_extra_paths":   {"/srv/one.sh\r\n/srv/two"},
		"backup_send_telegram": {"false"}, "_v2_kind_backup_send_telegram": {"select"},
	})
	cfg := latestConfig(state)
	if got := cfg.Settings["backup_extra_paths"]; got != "/srv/one.sh\r\n/srv/two" {
		t.Fatalf("backup_extra_paths = %q", got)
	}
	if got := cfg.Settings["backup_retention"]; got != "7" {
		t.Fatalf("backup_retention = %q", got)
	}

	target := postSectionForm(t, server, url.Values{
		"view": {"maintenance"}, "path": {"/api/backup/settings"},
		"backup_extra_paths": {"relative/path"},
	})
	if !strings.Contains(target, "toast_err=1") || !strings.Contains(target, "toast=") {
		t.Fatalf("a rejected save redirected without an error: %q", target)
	}

	postSectionForm(t, server, url.Values{
		"view": {"integrations"}, "path": {"/api/simkl/settings"},
		"simkl_client_id": {"abc123"},
	})
	if got := latestConfig(state).Settings["simkl_client_id"]; got != "abc123" {
		t.Fatalf("simkl_client_id = %q", got)
	}

	// An old client that still wraps the body is understood.
	if _, status := v2InternalJSON(state, http.MethodPost, "/api/backup/settings", nil, []byte(`{"values":{"backup_cloud_dir":"/mnt/cloud"}}`)); status != http.StatusOK {
		t.Fatalf("wrapped body -> %d", status)
	}
	if got := latestConfig(state).Settings["backup_cloud_dir"]; got != "/mnt/cloud" {
		t.Fatalf("wrapped body not saved: %q", got)
	}
}
