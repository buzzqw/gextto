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

func TestEd2kLogWarnings(t *testing.T) {
	lines := []string{
		"Connected to Kad",
		"Error: failed to connect to server",
		"Now sharing file",
		"Kad: no valid servers found",
		"warning: connection lost",
		"nothing to see",
	}
	got := ed2kLogWarnings(lines, 2)
	want := []string{"Kad: no valid servers found", "warning: connection lost"}
	if len(got) != len(want) {
		t.Fatalf("warnings = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("warnings = %v, want %v", got, want)
		}
	}
	if ed2kLogWarnings(lines, 0) != nil {
		t.Fatal("a zero limit must return no warnings")
	}
}

func TestEd2kLogWarningsDropsRepeatsAndSelfHealingLines(t *testing.T) {
	incoming := " 2026-11-01 06:00:05: 06:00:05: Error: Directory '/incoming' couldn't be created (error 13: Permission denied)"
	firstStat := " 2026-11-01 06:00:08: Error: Failed to get file system statistics: No such file or directory"
	lastStat := " 2026-11-01 06:01:00: Error: Failed to get file system statistics: No such file or directory"
	lines := []string{
		incoming,
		incoming,
		firstStat,
		" 2026-11-01 06:00:12: HTTP download failed for https://download.db-ip.com: Error:  (404)",
		" 2026-11-01 06:00:12: DB-IP download failed for the current month - retrying with the previous month's URL.",
		lastStat,
		" 2026-11-01 06:02:00: Connected to Kad",
	}

	got := ed2kLogWarnings(lines, 5)
	want := []string{strings.TrimSpace(incoming), strings.TrimSpace(lastStat)}
	if len(got) != len(want) {
		t.Fatalf("warnings = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("warnings = %v, want %v", got, want)
		}
	}

	// The limit applies after the repeats are collapsed.
	if got := ed2kLogWarnings(lines, 1); len(got) != 1 || got[0] != strings.TrimSpace(lastStat) {
		t.Fatalf("warnings with limit 1 = %v, want the newest line only", got)
	}
}

func TestEd2kSettingsTabIsSeparateFromSources(t *testing.T) {
	core, handler := ed2kTestRouter(t)
	core.SetAllocateFull(true)

	request := httptest.NewRequest(http.MethodGet, "/?view=settings&tab=ed2k", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, "eD2k e Kad tramite aMule") || !strings.Contains(body, "aMule — indirizzo") {
		t.Fatalf("eD2k settings tab: status %d", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/?view=settings&tab=sources", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if strings.Contains(recorder.Body.String(), "eD2k e Kad tramite aMule") {
		t.Fatal("the eD2k settings are still on the Sources tab")
	}
}

func TestEd2kStatusPanelShowsSessionAndPreallocation(t *testing.T) {
	core, handler := ed2kTestRouter(t)
	core.SetSessionTotals(1234, 5678)
	core.SetAllocateFull(true)

	request := httptest.NewRequest(http.MethodGet, "/?view=ed2k", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	for _, want := range []string{"Sessione", "Totale", "Preallocazione file", "attiva"} {
		if !strings.Contains(body, want) {
			t.Fatalf("eD2k page is missing %q", want)
		}
	}
}

func TestHealthPageShowsTheAmuleCard(t *testing.T) {
	previous := amuleProcessPIDs
	amuleProcessPIDs = func() []int { return nil }
	t.Cleanup(func() { amuleProcessPIDs = previous })
	core, handler := ed2kTestRouter(t)
	core.SetLog([]string{"Connected to Kad", "Error: connection lost to server"})

	request := httptest.NewRequest(http.MethodGet, "/?view=health", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	for _, want := range []string{"eD2k e Kad (aMule)", "in uso", "Client eD2k", "Dove gira", "su questa macchina", "Avvisi recenti dal log di aMule", "connection lost to server", "Sessione"} {
		if !strings.Contains(body, want) {
			t.Fatalf("health card is missing %q", want)
		}
	}
}

func TestAmuleRuntimeLabel(t *testing.T) {
	if got := amuleRuntimeLabel(nil); got != "" {
		t.Fatalf("runtime with no config = %q", got)
	}
	if got := amuleRuntimeLabel(&Config{Settings: map[string]string{"amule_host": "192.0.2.10"}}); got != "su un'altra macchina" {
		t.Fatalf("remote runtime = %q", got)
	}
	previous := amuleProcessPIDs
	amuleProcessPIDs = func() []int { return nil }
	t.Cleanup(func() { amuleProcessPIDs = previous })
	if got := amuleRuntimeLabel(&Config{Settings: map[string]string{"amule_host": "127.0.0.1"}}); got != "su questa macchina" {
		t.Fatalf("local runtime = %q", got)
	}
}

func TestEd2kMissingDirectoriesAreReportedAndCreated(t *testing.T) {
	core, handler := ed2kTestRouter(t)
	previous := amuleProcessPIDs
	amuleProcessPIDs = func() []int { return nil }
	t.Cleanup(func() { amuleProcessPIDs = previous })
	missing := filepath.Join(t.TempDir(), "amule-temp")
	core.SetTempDir(missing)

	request := httptest.NewRequest(http.MethodGet, "/?view=ed2k", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	if !strings.Contains(body, "Cartelle di aMule non utilizzabili:") || !strings.Contains(body, missing) || !strings.Contains(body, "Crea le cartelle") {
		t.Fatalf("missing folder not reported: %s", body)
	}

	request = httptest.NewRequest(http.MethodPost, "/ed2k/directories/create", nil)
	request.Header.Set("HX-Request", "true")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Cartelle di aMule create:") {
		t.Fatalf("create: status %d, body %q", recorder.Code, recorder.Body.String())
	}
	if info, err := os.Stat(missing); err != nil || !info.IsDir() {
		t.Fatalf("the folder was not created: %v", err)
	}

	request = httptest.NewRequest(http.MethodGet, "/?view=health", nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if strings.Contains(recorder.Body.String(), "Cartelle di aMule non utilizzabili:") {
		t.Fatal("health still reports the folder after creating it")
	}
}

func TestEd2kSettingsTabAppliesPreallocation(t *testing.T) {
	core, handler := ed2kTestRouter(t)
	core.SetAllocateFull(false)

	request := httptest.NewRequest(http.MethodGet, "/?view=settings&tab=ed2k", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	body := recorder.Body.String()
	for _, want := range []string{"Preallocazione file", "preallocazione completa riserva subito", "Salva la preallocazione"} {
		if !strings.Contains(body, want) {
			t.Fatalf("settings panel is missing %q", want)
		}
	}

	form := url.Values{"allocate": {"1"}}
	request = httptest.NewRequest(http.MethodPost, "/settings/ed2k/preallocation", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusSeeOther || !strings.Contains(recorder.Header().Get("Location"), "tab=ed2k") {
		t.Fatalf("preallocation redirect = %d %q", recorder.Code, recorder.Header().Get("Location"))
	}
	if !core.AllocateFull() {
		t.Fatal("preallocation was not turned on")
	}
}
