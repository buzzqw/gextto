package gextto

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// automaticSubtitleProvider serves the small OpenSubtitles surface the
// unattended download needs, and records the language of each search.
func automaticSubtitleProvider(t *testing.T, matches, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/subtitles":
			calls.Add(1)
			if got := r.URL.Query().Get("languages"); got != "it" {
				t.Errorf("search languages = %q, want the title's language", got)
			}
			_, _ = io.WriteString(w, `{"data":[{"id":"10","attributes":{"language":"it","release":"Show.S01E01","files":[{"file_id":20,"file_name":"sub.srt"}],"moviehash_match":`+boolJSON(matches.Load() > 0)+`,"moviebytesize_match":`+boolJSON(matches.Load() > 0)+`,"foreign_parts_only":false,"hearing_impaired":false}}]}`)
		case "/download":
			_, _ = io.WriteString(w, `{"link":"http://`+r.Host+`/sub.srt","file_name":"sub.srt"}`)
		case "/sub.srt":
			_, _ = io.WriteString(w, "1\n00:00:00,000 --> 00:00:01,000\nCiao\n")
		default:
			http.NotFound(w, r)
		}
	}))
}

func boolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func automaticSubtitleState(t *testing.T, subtitle string) (*AppState, string) {
	t.Helper()
	state := newTestAppState(t)
	video := subtitleTestVideo(t)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Show", Seasons: "1+", Enabled: true, Subtitle: subtitle}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := state.db.SyncArchiveFileScored("Show", 1, 1, "Show S01E01", video, 256<<10, 1); err != nil {
		t.Fatal(err)
	}
	return state, video
}

func automaticSubtitleConfig(state *AppState, providerURL string) *Config {
	cfg := latestConfig(state)
	cfg.DryRun = false
	cfg.Settings[subtitleSettingEnabled] = "true"
	cfg.Settings[subtitleSettingProviderURL] = providerURL
	cfg.Settings[subtitleSettingAPIKey] = "key"
	return cfg
}

func TestAutomaticSubtitlesFollowTheTitleLanguage(t *testing.T) {
	state, video := automaticSubtitleState(t, "ita")
	var matches, calls atomic.Int32
	matches.Add(1)
	provider := automaticSubtitleProvider(t, &matches, &calls)
	defer provider.Close()

	added, err := runAutomaticSubtitles(context.Background(), automaticSubtitleConfig(state, provider.URL), state.db, newSubtitleAutoState(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Fatalf("added = %d, want 1", added)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(video), "Show.S01E01.ita.srt"))
	if err != nil || !strings.Contains(string(data), "Ciao") {
		t.Fatalf("subtitle file = %q, err = %v", data, err)
	}
}

func TestAutomaticSubtitlesLeaveExistingSidecarsAlone(t *testing.T) {
	state, video := automaticSubtitleState(t, "ita")
	existing := filepath.Join(filepath.Dir(video), "Show.S01E01.ita.srt")
	if err := os.WriteFile(existing, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var matches, calls atomic.Int32
	matches.Add(1)
	provider := automaticSubtitleProvider(t, &matches, &calls)
	defer provider.Close()

	added, err := runAutomaticSubtitles(context.Background(), automaticSubtitleConfig(state, provider.URL), state.db, newSubtitleAutoState(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || calls.Load() != 0 {
		t.Fatalf("added = %d, searches = %d, want no work", added, calls.Load())
	}
	data, _ := os.ReadFile(existing)
	if string(data) != "keep me\n" {
		t.Fatalf("existing subtitle changed: %q", data)
	}
}

func TestAutomaticSubtitlesRefuseLowConfidenceMatches(t *testing.T) {
	state, video := automaticSubtitleState(t, "ita")
	var matches, calls atomic.Int32
	provider := automaticSubtitleProvider(t, &matches, &calls)
	defer provider.Close()

	added, err := runAutomaticSubtitles(context.Background(), automaticSubtitleConfig(state, provider.URL), state.db, newSubtitleAutoState(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || calls.Load() != 1 {
		t.Fatalf("added = %d, searches = %d, want one search and no write", added, calls.Load())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(video), "Show.S01E01.ita.srt")); err == nil {
		t.Fatal("a low-confidence match must not write a subtitle")
	}
}

func TestAutomaticSubtitlesCooldownAfterAMiss(t *testing.T) {
	state, video := automaticSubtitleState(t, "ita")
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer provider.Close()
	auto := newSubtitleAutoState()
	now := time.Now()
	cfg := automaticSubtitleConfig(state, provider.URL)

	if _, err := runAutomaticSubtitles(context.Background(), cfg, state.db, auto, now); err != nil {
		t.Fatal(err)
	}
	if _, err := runAutomaticSubtitles(context.Background(), cfg, state.db, auto, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("searches = %d, want one before the cooldown expires", calls.Load())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(video), "Show.S01E01.ita.srt")); err == nil {
		t.Fatal("a miss must not write a subtitle")
	}
}

func TestAutomaticSubtitlesRespectDryRunAndDisabledSwitch(t *testing.T) {
	state, _ := automaticSubtitleState(t, "ita")
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer provider.Close()

	cfg := automaticSubtitleConfig(state, provider.URL)
	cfg.DryRun = true
	if _, err := runAutomaticSubtitles(context.Background(), cfg, state.db, newSubtitleAutoState(), time.Now()); err != nil {
		t.Fatal(err)
	}
	cfg.DryRun = false
	delete(cfg.Settings, subtitleSettingEnabled)
	if _, err := runAutomaticSubtitles(context.Background(), cfg, state.db, newSubtitleAutoState(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("searches = %d, want none in dry-run or with the switch off", calls.Load())
	}
}
