package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeclaredDependenciesDoNotLeakSecrets(t *testing.T) {
	state := newTestAppState(t)
	if err := saveConfigSetting(state.cfg.DataDir, subtitleSettingAPIKey, "s3cr3t-subtitle-key"); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/health/dependencies", nil)
	DependenciesAPI(recorder, request, state)
	if recorder.Code != http.StatusOK {
		t.Fatalf("dependencies -> %d", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "s3cr3t-subtitle-key") {
		t.Fatalf("a secret leaked in the dependency report: %s", body)
	}
	for _, name := range []string{"TMDB", "TheTVDB", "TVmaze", "Simkl", "FlareSolverr", "Subtitles provider"} {
		if !strings.Contains(body, name) {
			t.Fatalf("%s is not declared: %s", name, body)
		}
	}
	if !strings.Contains(body, "configured") {
		t.Fatal("the report has no per-provider configured flag")
	}
}

func TestDeclaredDependenciesHandlesMissingConfig(t *testing.T) {
	report := declaredDependencies(nil)
	if report.OK || report.Note == "" {
		t.Fatalf("a missing configuration must be reported plainly: %+v", report)
	}
}
