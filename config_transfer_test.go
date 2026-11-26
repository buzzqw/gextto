package gextto

// A portable configuration is versioned, readable and safe to move between
// machines: secrets stay out by default and an import never erases settings
// that are not in the document.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func storedConfigSetting(t *testing.T, state *AppState, key string) string {
	t.Helper()
	value, err := loadConfigSetting(state.cfg.DataDir, key)
	if err != nil {
		t.Fatalf("load setting %s: %v", key, err)
	}
	return value
}

func TestConfigExportIsVersionedAndExcludesSecrets(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings = map[string]string{
		"torrent_seed_ratio": "1.5",
		"auth_api_key":       "s3cr3t-value",
		"notify_language":    "en",
	}
	cfg.Indexers = []IndexerConfig{{Name: "first", URL: "https://indexer.example", APIKey: "key-s3cr3t"}}
	cfg.Series = []SeriesConfig{{Name: "Example Show", Enabled: true}}

	document := buildConfigExport(&cfg, false)
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if document.Format != configExportFormat || document.SchemaVersion != configExportVersion {
		t.Fatalf("format=%q version=%d", document.Format, document.SchemaVersion)
	}
	if document.Secrets != "excluded" {
		t.Fatalf("secrets = %q, want excluded", document.Secrets)
	}
	if strings.Contains(string(raw), "s3cr3t-value") || strings.Contains(string(raw), "key-s3cr3t") {
		t.Fatalf("the export leaked a secret: %s", raw)
	}
	if !strings.Contains(string(raw), "auth_api_key") {
		t.Fatalf("the excluded key is not listed: %s", raw)
	}
	var indexers []IndexerConfig
	if err := json.Unmarshal(document.Sections["indexers"], &indexers); err != nil || len(indexers) != 1 || indexers[0].APIKey != "" {
		t.Fatalf("indexer API key was exported: %+v err=%v", indexers, err)
	}
	var series []SeriesConfig
	if err := json.Unmarshal(document.Sections["series"], &series); err != nil || len(series) != 1 || series[0].Name != "Example Show" {
		t.Fatalf("series section = %+v err=%v", series, err)
	}
}

func TestConfigImportAppliesOnlyTheDocumentedSections(t *testing.T) {
	source := newTestAppState(t)
	if err := saveConfigSetting(source.cfg.DataDir, "torrent_seed_ratio", "1.5"); err != nil {
		t.Fatal(err)
	}
	if err := saveConfigSetting(source.cfg.DataDir, "auth_api_key", "s3cr3t-value"); err != nil {
		t.Fatal(err)
	}
	document := buildConfigExport(latestConfig(source), false)
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	target := newTestAppState(t)
	if err := saveConfigSetting(target.cfg.DataDir, "notify_language", "en"); err != nil {
		t.Fatal(err)
	}
	targetCfg := latestConfig(target)
	plan, err := parseConfigImport(raw, targetCfg, targetCfg.Indexers)
	if err != nil {
		t.Fatal(err)
	}
	if got := storedConfigSetting(t, target, "torrent_seed_ratio"); got != "" {
		t.Fatalf("the preview wrote a setting before the import: %q", got)
	}
	if err := applyConfigImportPlan(target, plan); err != nil {
		t.Fatal(err)
	}
	if got := storedConfigSetting(t, target, "torrent_seed_ratio"); got != "1.5" {
		t.Fatalf("imported ratio = %q, want 1.5", got)
	}
	if got := storedConfigSetting(t, target, "notify_language"); got != "en" {
		t.Fatalf("an untouched setting was erased: %q", got)
	}
	if got := storedConfigSetting(t, target, "auth_api_key"); got != "" {
		t.Fatalf("a secret was imported from a document that excluded it: %q", got)
	}
}

func TestConfigImportRejectsAnUnknownVersion(t *testing.T) {
	state := newTestAppState(t)
	document := buildConfigExport(latestConfig(state), false)
	document.SchemaVersion = 99
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	cfg := latestConfig(state)
	if _, err := parseConfigImport(raw, cfg, cfg.Indexers); err == nil {
		t.Fatal("an unknown schema version was accepted")
	}
}

func TestConfigImportPreservesAnExistingIndexerKey(t *testing.T) {
	state := newTestAppState(t)
	document := buildConfigExport(latestConfig(state), false)
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	existing := []IndexerConfig{{Name: "first", URL: "https://indexer.example", APIKey: "already-saved", Enabled: true}}
	var decoded configExport
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	decoded.Sections["indexers"], _ = json.Marshal([]IndexerConfig{{Name: "first", URL: "https://indexer.example", Enabled: true}})
	raw, _ = json.Marshal(decoded)
	cfg := latestConfig(state)
	plan, err := parseConfigImport(raw, cfg, existing)
	if err != nil {
		t.Fatal(err)
	}
	var got []IndexerConfig
	if err := json.Unmarshal([]byte(plan.settings["indexers"]), &got); err != nil || len(got) != 1 || got[0].APIKey != "already-saved" {
		t.Fatalf("indexer key was lost: %+v err=%v", got, err)
	}
}

func TestConfigExportEndpointDoesNotLeakSecrets(t *testing.T) {
	state := newTestAppState(t)
	if err := saveConfigSetting(state.cfg.DataDir, "auth_api_key", "s3cr3t-value"); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/config/export", nil)
	ConfigExport(recorder, request, state)
	if recorder.Code != http.StatusOK {
		t.Fatalf("export -> %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "s3cr3t-value") {
		t.Fatal("the export endpoint leaked an API key")
	}
	if !strings.Contains(recorder.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("the export is not offered as a download: %v", recorder.Header())
	}
}
