package gextto

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func subtitleTestVideo(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Show.S01E01.mkv")
	if err := os.WriteFile(path, make([]byte, 256<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveSubtitleTargetEpisodeAndMovie(t *testing.T) {
	state := newTestAppState(t)
	video := subtitleTestVideo(t)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Show", Seasons: "1+", Enabled: true}}, []MovieConfig{{ID: 7, Name: "Movie", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err := state.db.SyncArchiveFileScored("Show", 1, 1, "Show S01E01", video, 256<<10, 1); err != nil {
		t.Fatal(err)
	}
	target, err := resolveSubtitleTarget(context.Background(), latestConfig(state), state.db, "Show", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if target.VideoPath != video || len(target.OSHash) != 16 || target.SizeBytes != 256<<10 || target.Season != 1 || target.Episode != 1 {
		t.Fatalf("episode target = %+v", target)
	}
	if _, err := resolveSubtitleTarget(context.Background(), latestConfig(state), state.db, "", 0, 0, 7); err == nil {
		t.Fatal("a movie without a recorded downloaded path must be refused")
	}
}

func TestSubtitleSearchAndExplicitApply(t *testing.T) {
	state := newTestAppState(t)
	video := subtitleTestVideo(t)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Show", Seasons: "1+", Enabled: true, Subtitle: "ita"}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := state.db.SyncArchiveFileScored("Show", 1, 1, "Show S01E01", video, 256<<10, 1); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/subtitles":
			if r.URL.Query().Get("languages") != "it" || r.URL.Query().Get("season_number") != "1" || r.URL.Query().Get("episode_number") != "1" {
				t.Errorf("subtitle query = %v", r.URL.Query())
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"10","attributes":{"language":"it","release":"Show.S01E01","files":[{"file_id":20,"file_name":"sub.srt"}],"moviehash_match":true,"moviebytesize_match":true,"foreign_parts_only":false,"hearing_impaired":false}}]}`)
		case "/download":
			_, _ = io.WriteString(w, `{"link":"http://`+r.Host+`/sub.srt","file_name":"sub.srt","remaining":4}`)
		case "/sub.srt":
			_, _ = io.WriteString(w, "1\n00:00:00,000 --> 00:00:01,000\nCiao\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cfg := latestConfig(state)
	cfg.Settings[subtitleSettingEnabled] = "true"
	cfg.Settings[subtitleSettingProviderURL] = server.URL
	cfg.Settings[subtitleSettingAPIKey] = "key"
	target, err := resolveSubtitleTarget(context.Background(), cfg, state.db, "Show", 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := searchSubtitleCandidates(context.Background(), cfg, state.db, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].FileID != 20 || !candidates[0].HashMatch || candidates[0].ProposedName != "Show.S01E01.ita.srt" {
		t.Fatalf("candidates = %+v", candidates)
	}
	path, err := applySubtitle(context.Background(), cfg, state.db, target, 20, "it", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "Show.S01E01.ita.srt" {
		t.Fatalf("path = %q", path)
	}
	if _, err := applySubtitle(context.Background(), cfg, state.db, target, 20, "it", false, false, false); err != errSubtitleCollision {
		t.Fatalf("second apply err = %v, want collision", err)
	}
	if _, err := applySubtitle(context.Background(), cfg, state.db, target, 20, "it", false, false, true); err != nil {
		t.Fatalf("replace apply: %v", err)
	}
}

func TestWriteSubtitleAtomicKeepIsExclusive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Example.ita.srt")
	cfg := DefaultConfig()
	cfg.DataDir = dir
	cfg.CleanupAction = "delete"
	payloads := [][]byte{[]byte(strings.Repeat("A", 256<<10)), []byte(strings.Repeat("B", 256<<10))}
	start := make(chan struct{})
	errs := make(chan error, len(payloads))
	var wg sync.WaitGroup
	for _, payload := range payloads {
		wg.Add(1)
		go func(data []byte) {
			defer wg.Done()
			<-start
			errs <- writeSubtitleAtomic(path, data, false, &cfg)
		}(payload)
	}
	close(start)
	wg.Wait()
	close(errs)
	successes, collisions := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, errSubtitleCollision):
			collisions++
		default:
			t.Fatal(err)
		}
	}
	if successes != 1 || collisions != 1 {
		t.Fatalf("successes=%d collisions=%d, want one exclusive write", successes, collisions)
	}
}

func TestValidateSubtitleSRTRejectsInvalidUTF8AfterBOM(t *testing.T) {
	data := append([]byte{0xef, 0xbb, 0xbf}, []byte("1\n00:00:00,000 --> 00:00:01,000\n\xff\n")...)
	if err := validateSubtitleSRT(data); err == nil {
		t.Fatal("invalid UTF-8 after a BOM must be rejected")
	}
}

func TestSubtitleSettingsKeepBlankSecretsAndValidatePair(t *testing.T) {
	state := newTestAppState(t)
	cfg := latestConfig(state)
	if err := saveSubtitleSettings(cfg, subtitleSettingsInput{Enabled: "true", ProviderURL: "http://127.0.0.1:1", APIKey: "key", Username: "user", Password: "pass", Forced: "any", SDH: "any", Collision: "keep"}); err != nil {
		t.Fatal(err)
	}
	cfg = latestConfig(state)
	if err := saveSubtitleSettings(cfg, subtitleSettingsInput{Enabled: "true", ProviderURL: "http://127.0.0.1:1", Forced: "any", SDH: "any", Collision: "keep"}); err != nil {
		t.Fatal(err)
	}
	stored := subtitlePreferencesFrom(latestConfig(state))
	if stored.APIKey != "key" || stored.Password != "pass" || stored.Username != "user" {
		t.Fatalf("blank secrets were not kept: %+v", stored)
	}
	cfg.Settings[subtitleSettingPassword] = ""
	if err := saveSubtitleSettings(cfg, subtitleSettingsInput{Enabled: "true", ProviderURL: "http://127.0.0.1:1", APIKey: "key", Username: "user", Forced: "any", SDH: "any", Collision: "keep"}); err == nil {
		t.Fatal("username without password must be rejected")
	}
}

func TestV2SubtitleSettingsUseSelectAndVisibleHelp(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveSetting(state.cfg.DataDir, "dry_run", "no"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	defer server.Close()
	code, page := v2Request(t, server, http.MethodGet, "/?view=integrations", nil)
	if code != http.StatusOK {
		t.Fatalf("integrations -> %d", code)
	}
	if !strings.Contains(page, `<select class="input" name="enabled">`) {
		t.Fatalf("enabled is not a select")
	}
	if !strings.Contains(page, `Mostra il pulsante di ricerca sulle schede di episodi e film.`) {
		t.Fatalf("field help is not visible")
	}
	code, _ = v2Request(t, server, http.MethodPost, "/section/form", url.Values{
		"view": {"integrations"}, "path": {"/api/subtitles/settings"},
		"enabled": {"true"}, "_v2_kind_enabled": {"select"},
		"provider_url": {"https://api.opensubtitles.com/api/v1"}, "api_key": {"key"},
		"forced": {"yes"}, "sdh": {"no"}, "collision_policy": {"replace"},
	})
	if code != http.StatusNoContent {
		t.Fatalf("settings form -> %d", code)
	}
	if got := latestConfig(state).Settings[subtitleSettingEnabled]; got != "true" {
		t.Fatalf("enabled = %q, want true", got)
	}
	_, page = v2Request(t, server, http.MethodGet, "/?view=integrations", nil)
	if !strings.Contains(page, `<option value="yes" selected>`) || !strings.Contains(page, `<option value="replace" selected>`) {
		t.Fatalf("stored select values are not selected")
	}
}

func TestV2SubtitleProviderTestUsesFormValues(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer provider.Close()
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	defer server.Close()
	code, body := v2Request(t, server, http.MethodPost, "/subtitles/test", url.Values{
		"enabled": {"false"}, "provider_url": {provider.URL}, "api_key": {"key"}, "forced": {"any"}, "sdh": {"any"}, "collision_policy": {"keep"},
	})
	if code != http.StatusOK || !strings.Contains(body, "Connessione riuscita.") {
		t.Fatalf("test -> %d: %s", code, body)
	}
	if stored := latestConfig(state).Settings[subtitleSettingAPIKey]; stored != "" {
		t.Fatalf("testing saved the API key: %q", stored)
	}
}

func TestV2SubtitleProviderTestExplainsServiceUnavailable(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer provider.Close()
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	defer server.Close()
	code, body := v2Request(t, server, http.MethodPost, "/subtitles/test", url.Values{
		"enabled": {"false"}, "provider_url": {provider.URL}, "api_key": {"key"},
	})
	if code != http.StatusOK || !strings.Contains(body, "Provider temporaneamente non disponibile") {
		t.Fatalf("test -> %d: %s", code, body)
	}
	if calls.Load() != 3 {
		t.Fatalf("provider calls = %d, want initial request and two retries", calls.Load())
	}
}

func TestV2SubtitleActionAppearsForArchivedEpisode(t *testing.T) {
	state := newTestAppState(t)
	video := subtitleTestVideo(t)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Show", Seasons: "1+", Enabled: true}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := state.db.SyncArchiveFileScored("Show", 1, 1, "Show S01E01", video, 256<<10, 1); err != nil {
		t.Fatal(err)
	}
	if err := SaveSetting(state.cfg.DataDir, subtitleSettingEnabled, "true"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	defer server.Close()
	code, page := v2Request(t, server, http.MethodGet, "/?view=series&series=Show", nil)
	if code != http.StatusOK || !strings.Contains(page, `hx-post="/subtitles/search"`) {
		t.Fatalf("series page -> %d: %s", code, page)
	}
}
