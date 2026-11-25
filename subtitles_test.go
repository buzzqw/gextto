package gextto

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
