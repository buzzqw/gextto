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

	"github.com/buzzqw/gextto/internal/models"
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
	if !strings.Contains(page, `Mostra la ricerca dei sottotitoli su episodi, film e dettagli dei torrent.`) {
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

func TestSubtitleReleaseSimilarityIgnoresSeparatorsAndExtension(t *testing.T) {
	if got := subtitleReleaseSimilarity("Show.S01E02.1080p.WEB-DL.x264-GROUP.mkv", "show s01e02 1080p web dl x264 group"); got != 100 {
		t.Fatalf("same normalized release = %d, want 100", got)
	}
	if got := subtitleReleaseSimilarity("Show.S01E02.1080p.WEB-DL.x264-GROUP.mkv", "Show.S01E02.2160p.WEB-DL.x265-OTHER"); got < 65 {
		t.Fatalf("closely related releases = %d, want a strong similarity", got)
	}
	if got := subtitleReleaseSimilarity("Show.S01E02.mkv", "Different.Movie.2020.mkv"); got > 30 {
		t.Fatalf("unrelated releases = %d, want a low similarity", got)
	}
}

func TestSubtitleCandidateScoreUsesSimilarityAndReputation(t *testing.T) {
	trusted, untrusted := true, false
	ai, human := true, false
	base := subtitleCandidate{ReleaseSimilarity: 90, FromTrusted: &trusted, AITranslated: &human, MachineTranslated: &human, DownloadCount: 5000}
	if base.Score != 0 {
		t.Fatal("test fixture must start without a precomputed score")
	}
	strong := subtitleCandidateScore(base)
	weak := subtitleCandidateScore(subtitleCandidate{ReleaseSimilarity: 20, FromTrusted: &untrusted, AITranslated: &ai, MachineTranslated: &ai})
	if strong <= weak || strong < 55 {
		t.Fatalf("strong=%d weak=%d, want the similar trusted human subtitle first", strong, weak)
	}
	exact := subtitleCandidateScore(subtitleCandidate{HashMatch: true, SizeMatch: true, ExternalIDMatch: true, ReleaseSimilarity: 100})
	if exact != 100 {
		t.Fatalf("exact match score = %d, want 100", exact)
	}
}

func TestSubtitleLanguagesFilter(t *testing.T) {
	languages, err := subtitleLanguagesFilter("all")
	if err != nil || languages != nil {
		t.Fatalf("all languages = %v, %v", languages, err)
	}
	languages, err = subtitleLanguagesFilter("ita,en,eng")
	if err != nil || len(languages) != 2 || languages[0] != "it" || languages[1] != "en" {
		t.Fatalf("mapped languages = %v, %v", languages, err)
	}
	if _, err := subtitleLanguagesFilter("not-a-language"); err == nil {
		t.Fatal("an invalid language filter must be refused")
	}
}

func TestSubtitleSearchBrowsesAllLanguagesAndMarksBestPerLanguage(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subtitles" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("languages"); got != "" {
			t.Errorf("all-language search sent languages=%q", got)
		}
		_, _ = io.WriteString(w, `{"data":[
			{"attributes":{"language":"it","release":"Show.S01E01.1080p.WEB-DL.x264-GROUP","files":[{"file_id":20}],"from_trusted":true,"ai_translated":false,"machine_translated":false,"download_count":5000}},
			{"attributes":{"language":"it","release":"Show.S01E01.2160p.WEB-DL.x265-OTHER","files":[{"file_id":21}],"from_trusted":false,"ai_translated":true,"machine_translated":true,"download_count":90000}},
			{"attributes":{"language":"en","release":"Show.S01E01.1080p.WEB-DL.x264-GROUP","files":[{"file_id":22}],"from_trusted":true,"ai_translated":false,"machine_translated":false,"download_count":4000}}
		]}`)
	}))
	defer server.Close()
	cfg := latestConfig(state)
	cfg.Settings[subtitleSettingEnabled] = "true"
	cfg.Settings[subtitleSettingProviderURL] = server.URL
	cfg.Settings[subtitleSettingAPIKey] = "key"
	client, prefs, err := subtitleProviderClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	target := subtitleTarget{Kind: "episode", VideoPath: filepath.Join(t.TempDir(), "Show.S01E01.1080p.WEB-DL.x264-GROUP.mkv"), Title: "Show", Season: 1, Episode: 1}
	candidates, err := searchSubtitleCandidatesWithLanguages(context.Background(), client, prefs, cfg, state.db, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 {
		t.Fatalf("candidates = %+v", candidates)
	}
	if candidates[0].Language != "it" || !candidates[0].Recommended || candidates[0].ReleaseSimilarity != 100 {
		t.Fatalf("best Italian candidate = %+v", candidates[0])
	}
	if candidates[1].Language != "en" || !candidates[1].Recommended {
		t.Fatalf("best English candidate = %+v", candidates[1])
	}
	if candidates[2].Recommended || !candidates[2].IsAITranslated() {
		t.Fatalf("weaker Italian candidate = %+v", candidates[2])
	}
}

type fakeSubtitleTorrentEngine struct {
	view   models.TorrentView
	files  []models.FileView
	ok     bool
	err    error
	called string
}

func (f *fakeSubtitleTorrentEngine) List() []models.TorrentView { return []models.TorrentView{f.view} }
func (f *fakeSubtitleTorrentEngine) Files(hash string) ([]models.FileView, bool, error) {
	f.called = hash
	return f.files, f.ok, f.err
}

func TestResolveSubtitleTorrentTargetUsesSelectedFileAndAllowsIncomplete(t *testing.T) {
	state := newTestAppState(t)
	root := t.TempDir()
	relative := filepath.Join("pack", "Show.S01E03.1080p.WEB-DL.x264-GROUP.mkv")
	video := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(video), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(video, make([]byte, 256<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 40)
	series, season, sentinel := "Show", int64(1), int64(0)
	release := models.Release{Title: "Show.S01", Magnet: "magnet:?xt=urn:btih:" + hash, InfoHash: hash, Source: manualSource, Kind: "series", Series: &series, Season: &season, Episode: &sentinel, EpisodeRange: []int64{0}, IsPack: true}
	if err := state.db.RegisterTorrentScored(&release, 1); err != nil {
		t.Fatal(err)
	}
	engine := &fakeSubtitleTorrentEngine{
		view:  models.TorrentView{Hash: hash, Name: "Show.S01", SavePath: root, Progress: 0.5},
		files: []models.FileView{{Path: relative, Size: 256 << 10, Downloaded: 256 << 10, Priority: 1}},
		ok:    true,
	}
	target, err := resolveSubtitleTorrentTarget(context.Background(), latestConfig(state), state.db, engine, hash, 0)
	if err != nil {
		t.Fatal(err)
	}
	if target.VideoPath != video || target.Series != "Show" || target.Season != 1 || target.Episode != 3 || !target.FileComplete || len(target.OSHash) != 16 {
		t.Fatalf("complete target = %+v", target)
	}
	if target.TorrentHash != hash || target.FileIndex == nil || *target.FileIndex != 0 || target.ReferenceName != "Show.S01" || engine.called != hash {
		t.Fatalf("torrent identity = %+v, called=%q", target, engine.called)
	}
	engine.files[0].Downloaded = 0
	target, err = resolveSubtitleTorrentTarget(context.Background(), latestConfig(state), state.db, engine, hash, 0)
	if err != nil {
		t.Fatal(err)
	}
	if target.FileComplete || target.OSHash != "" || target.SizeBytes != 0 || target.Episode != 3 {
		t.Fatalf("incomplete target = %+v", target)
	}
}

func TestResolveSubtitleTorrentTargetParsesRawManualName(t *testing.T) {
	state := newTestAppState(t)
	root := t.TempDir()
	relative := filepath.Join("Season 02", "Show.S02E04.mkv")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, relative)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, relative), make([]byte, 256<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("b", 40)
	engine := &fakeSubtitleTorrentEngine{
		view:  models.TorrentView{Hash: hash, Name: "S02", SavePath: root},
		files: []models.FileView{{Path: relative, Size: 256 << 10, Downloaded: 0, Priority: 1}},
		ok:    true,
	}
	target, err := resolveSubtitleTorrentTarget(context.Background(), latestConfig(state), state.db, engine, hash, 0)
	if err != nil {
		t.Fatal(err)
	}
	if target.Kind != "episode" || target.Title != "Show" || target.Season != 2 || target.Episode != 4 || target.FileComplete {
		t.Fatalf("raw target = %+v", target)
	}
}

func TestResolveSubtitleTorrentTargetRejectsUnsafeOrAmbiguousFile(t *testing.T) {
	state := newTestAppState(t)
	root := t.TempDir()
	hash := strings.Repeat("c", 40)
	for _, relative := range []string{"../outside.mkv", "/absolute.mkv", `..\outside.mkv`, "Show.S01E01-E02.mkv"} {
		engine := &fakeSubtitleTorrentEngine{
			view:  models.TorrentView{Hash: hash, Name: "Show.S01", SavePath: root},
			files: []models.FileView{{Path: relative, Size: 256 << 10, Downloaded: 0, Priority: 1}},
			ok:    true,
		}
		if _, err := resolveSubtitleTorrentTarget(context.Background(), latestConfig(state), state.db, engine, hash, 0); err == nil {
			t.Fatalf("path %q was accepted", relative)
		}
	}
	engine := &fakeSubtitleTorrentEngine{
		view:  models.TorrentView{Hash: hash, Name: "Show.S01", SavePath: root},
		files: []models.FileView{{Path: "Show.S01E01.mkv", Size: 256 << 10, Downloaded: 0, Priority: 1}},
		ok:    true,
	}
	if _, err := resolveSubtitleTorrentTarget(context.Background(), latestConfig(state), state.db, engine, hash, 1); err == nil {
		t.Fatal("an out-of-range file index was accepted")
	}
}

func TestResolveSubtitleTorrentTargetRejectsSymlinksAndWrongCompleteSize(t *testing.T) {
	state := newTestAppState(t)
	root, outside := t.TempDir(), t.TempDir()
	hash := strings.Repeat("f", 40)
	outsideVideo := filepath.Join(outside, "Show.S01E01.mkv")
	if err := os.WriteFile(outsideVideo, make([]byte, 256<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideVideo, filepath.Join(root, "Show.S01E01.mkv")); err != nil {
		t.Fatal(err)
	}
	engine := &fakeSubtitleTorrentEngine{
		view:  models.TorrentView{Hash: hash, Name: "Show.S01", SavePath: root},
		files: []models.FileView{{Path: "Show.S01E01.mkv", Size: 256 << 10, Downloaded: 0, Priority: 1}},
		ok:    true,
	}
	if _, err := resolveSubtitleTorrentTarget(context.Background(), latestConfig(state), state.db, engine, hash, 0); err == nil {
		t.Fatal("a final symlink was accepted")
	}
	if err := os.Remove(filepath.Join(root, "Show.S01E01.mkv")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	engine.files[0].Path = "linked/Show.S01E01.mkv"
	if _, err := resolveSubtitleTorrentTarget(context.Background(), latestConfig(state), state.db, engine, hash, 0); err == nil {
		t.Fatal("a directory symlink escaping the save root was accepted")
	}
	if err := os.Remove(filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Show.S01E01.mkv"), make([]byte, 128<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	engine.files[0] = models.FileView{Path: "Show.S01E01.mkv", Size: 256 << 10, Downloaded: 256 << 10, Priority: 1}
	if _, err := resolveSubtitleTorrentTarget(context.Background(), latestConfig(state), state.db, engine, hash, 0); err == nil {
		t.Fatal("a complete file whose disk size differs from the torrent was accepted")
	}
}

func TestResolveSubtitleTorrentTargetParsesRawManualMovie(t *testing.T) {
	state := newTestAppState(t)
	root := t.TempDir()
	hash := strings.Repeat("1", 40)
	engine := &fakeSubtitleTorrentEngine{
		view:  models.TorrentView{Hash: hash, Name: "Example.Movie.2024.1080p", SavePath: root},
		files: []models.FileView{{Path: "Example.Movie.2024.mkv", Size: 256 << 10, Downloaded: 0, Priority: 1}},
		ok:    true,
	}
	target, err := resolveSubtitleTorrentTarget(context.Background(), latestConfig(state), state.db, engine, hash, 0)
	if err != nil {
		t.Fatal(err)
	}
	if target.Kind != "movie" || target.Title != "Example.Movie.2024.1080p" || target.Year != 2024 {
		t.Fatalf("raw movie target = %+v", target)
	}
}
