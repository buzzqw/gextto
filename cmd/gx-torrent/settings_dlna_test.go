package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The DLNA library-folders button proposes the configured Download folder. The
// two occurrences of the path need distinct placeholders: the renderer replaces
// each placeholder once.
func TestSettingsDLNAFolderUsesTheDownloadFolder(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set("download-dir", "/srv/downloads")
	d.opts.Settings.Set("dlna-enabled", "true")
	rec := httptest.NewRecorder()
	d.renderSettings(rec, httptest.NewRequest(http.MethodGet, uiSettingsPath, nil), "", false)
	body := rec.Body.String()
	if strings.Contains(body, "{{DLNA_DOWNLOAD_DIR}}") || strings.Contains(body, "{{DOWNLOAD_DIR}}") {
		t.Fatal("the settings page still contains an unresolved download-folder placeholder")
	}
	if !strings.Contains(body, `data-path="/srv/downloads"`) {
		t.Fatalf("the DLNA button does not propose the download folder:\n%s", body)
	}
}
