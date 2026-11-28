package gextto

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

type fakeV2SubtitleEngine struct {
	TorrentEngine
	view  models.TorrentView
	files []models.FileView
}

func (fakeV2SubtitleEngine) Name() string { return BackendGxTorrent }
func (f fakeV2SubtitleEngine) List() []models.TorrentView {
	return []models.TorrentView{f.view}
}
func (f fakeV2SubtitleEngine) Files(string) ([]models.FileView, bool, error) {
	return append([]models.FileView(nil), f.files...), true, nil
}
func (fakeV2SubtitleEngine) Trackers(string) ([]models.TrackerView, bool, error) {
	return nil, true, nil
}

func TestV2DetailSubtitlesTabListsVideoFiles(t *testing.T) {
	state := newTestAppState(t)
	hash := strings.Repeat("d", 40)
	state.setActiveEngine(fakeV2SubtitleEngine{
		view: models.TorrentView{Hash: hash, Name: "Show.S01", SavePath: t.TempDir()},
		files: []models.FileView{
			{Path: "Show.S01E01.mkv", Size: 256 << 10, Downloaded: 0, Priority: 1},
			{Path: "Show.S01E01.srt", Size: 1000, Downloaded: 1000, Priority: 1},
			{Path: "Show.S01E02.mkv", Size: 256 << 10, Downloaded: 256 << 10, Priority: 1},
		},
	})
	if err := SaveSetting(state.cfg.DataDir, subtitleSettingEnabled, "true"); err != nil {
		t.Fatal(err)
	}
	view := v2DetailViewFrom(state, hash, "subtitles")
	hasTab := false
	for _, tab := range view.Tabs {
		if tab.ID == "subtitles" {
			hasTab = true
		}
	}
	if !hasTab || len(view.Subtitles.Files) != 2 || view.Subtitles.Files[0].Index != 0 || view.Subtitles.Files[1].Index != 2 {
		t.Fatalf("subtitle tab = %+v, files = %+v", view.Tabs, view.Subtitles.Files)
	}
	server := httptest.NewServer(Router(state))
	defer server.Close()
	code, page := v2Request(t, server, http.MethodGet, "/downloads/detail?hash="+hash+"&tab=subtitles", nil)
	if code != http.StatusOK || !strings.Contains(page, `data-v2-detail-tab="subtitles"`) || !strings.Contains(page, `name="file_index"`) || !strings.Contains(page, `name="languages"`) {
		t.Fatalf("subtitle detail -> %d: %s", code, page)
	}
}

func TestV2TorrentSubtitleSearchShowsScoreAndLanguageFilter(t *testing.T) {
	state := newTestAppState(t)
	hash := strings.Repeat("e", 40)
	state.setActiveEngine(fakeV2SubtitleEngine{
		view: models.TorrentView{Hash: hash, Name: "Show.S01", SavePath: t.TempDir()},
		files: []models.FileView{
			{Path: "Show.S01E01.1080p.WEB-DL.x264-GROUP.mkv", Size: 256 << 10, Downloaded: 0, Priority: 1},
		},
	})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subtitles" {
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query()
		if query.Get("languages") != "" || query.Get("type") != "episode" || query.Get("season_number") != "1" || query.Get("episode_number") != "1" || query.Get("moviehash") != "" {
			t.Errorf("provider query = %v", query)
		}
		_, _ = io.WriteString(w, `{"data":[{"attributes":{"language":"it","release":"Show.S01E01.1080p.WEB-DL.x264-GROUP","files":[{"file_id":20}],"from_trusted":true,"ai_translated":false,"machine_translated":false,"download_count":5000}}]}`)
	}))
	defer provider.Close()
	for key, value := range map[string]string{
		subtitleSettingEnabled: "true", subtitleSettingProviderURL: provider.URL, subtitleSettingAPIKey: "key",
	} {
		if err := SaveSetting(state.cfg.DataDir, key, value); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(Router(state))
	defer server.Close()
	code, body := v2Request(t, server, http.MethodPost, "/subtitles/search", url.Values{
		"torrent_hash": {hash}, "file_index": {"0"}, "languages": {"all"},
	})
	if code != http.StatusOK || !strings.Contains(body, "Show.S01E01.1080p.WEB-DL.x264-GROUP") || !strings.Contains(body, "Consigliato") || !strings.Contains(body, "Trusted") || !strings.Contains(body, "/100") {
		t.Fatalf("torrent subtitle search -> %d: %s", code, body)
	}
	if !strings.Contains(body, "Il file non è ancora completo") {
		t.Fatalf("the incomplete-file warning is missing: %s", body)
	}
}

func TestV2TorrentSubtitleSearchRejectsInvalidLanguage(t *testing.T) {
	state := newTestAppState(t)
	hash := strings.Repeat("2", 40)
	state.setActiveEngine(fakeV2SubtitleEngine{
		view: models.TorrentView{Hash: hash, Name: "Show.S01", SavePath: t.TempDir()},
		files: []models.FileView{
			{Path: "Show.S01E01.mkv", Size: 256 << 10, Downloaded: 0, Priority: 1},
		},
	})
	for key, value := range map[string]string{
		subtitleSettingEnabled: "true", subtitleSettingProviderURL: "http://127.0.0.1:1", subtitleSettingAPIKey: "key",
	} {
		if err := SaveSetting(state.cfg.DataDir, key, value); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(Router(state))
	defer server.Close()
	code, body := v2Request(t, server, http.MethodPost, "/subtitles/search", url.Values{
		"torrent_hash": {hash}, "file_index": {"0"}, "languages": {"not-a-language"},
	})
	if code != http.StatusOK || !strings.Contains(body, "unsupported subtitle language") {
		t.Fatalf("invalid language -> %d: %s", code, body)
	}
}
