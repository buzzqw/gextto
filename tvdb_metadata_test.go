package gextto

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// tvdbFakeSeries serves a TVDB series (id 77) with two seasons and a
// special: season 1 has 3 episodes, season 2 has 2, the last one airing in
// the future. Episodes come in two pages; the Italian list translates only
// some titles. logins counts the /login calls, pins records the PINs sent.
type tvdbFake struct {
	logins atomic.Int32
	pins   []string
}

func newTvdbFake(t *testing.T) *tvdbFake {
	t.Helper()
	fake := &tvdbFake{}
	future := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
	page0 := `{"data":{"episodes":[
		{"id":1,"seasonNumber":0,"number":1,"name":"Special","aired":"2019-12-01"},
		{"id":2,"seasonNumber":1,"number":1,"name":"Pilot","aired":"2020-01-01"},
		{"id":3,"seasonNumber":1,"number":2,"name":"Second","aired":"2020-01-08"}]},
		"links":{"next":"https://api4.thetvdb.com/v4/series/77/episodes/default?page=1"}}`
	page1 := `{"data":{"episodes":[
		{"id":4,"seasonNumber":1,"number":3,"name":"Third","aired":"2020-01-15"},
		{"id":5,"seasonNumber":2,"number":1,"name":"Return","aired":"2021-01-01"},
		{"id":6,"seasonNumber":2,"number":2,"name":"Next one","aired":"` + future + `"}]},
		"links":{"next":null}}`
	italian := `{"data":{"episodes":[
		{"id":2,"seasonNumber":1,"number":1,"name":"L'inizio","aired":"2020-01-01"},
		{"id":3,"seasonNumber":1,"number":2,"name":null,"aired":"2020-01-08"}]},
		"links":{"next":null}}`
	extended := `{"data":{"id":77,"name":"Example Show","image":"https://artworks.thetvdb.com/77.jpg",
		"firstAired":"2020-01-01","lastAired":"2021-01-01","status":{"name":"Continuing"},
		"originalCountry":"usa","originalNetwork":{"name":"HBO"},"genres":[{"name":"Drama"}],
		"translations":{"nameTranslations":[{"language":"ita","name":"Esempio"}],
		"overviewTranslations":[{"language":"eng","overview":"An example."},{"language":"ita","overview":"Un esempio."}]}}}`
	tvdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/login":
			fake.logins.Add(1)
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			fake.pins = append(fake.pins, payload["pin"])
			tvdbWriteJSON(t, w, `{"data":{"token":"tok"}}`)
		case r.Header.Get("Authorization") != "Bearer tok":
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/search":
			tvdbWriteJSON(t, w, `{"data":[{"tvdb_id":"77","name":"Example Show","year":"2020"}]}`)
		case r.URL.Path == "/series/77/episodes/default" && r.URL.Query().Get("page") == "1":
			tvdbWriteJSON(t, w, page1)
		case r.URL.Path == "/series/77/episodes/default":
			tvdbWriteJSON(t, w, page0)
		case r.URL.Path == "/series/77/episodes/default/ita":
			tvdbWriteJSON(t, w, italian)
		case r.URL.Path == "/series/77/extended":
			tvdbWriteJSON(t, w, extended)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	return fake
}

func tvdbOnlyConfig() Config {
	cfg := DefaultConfig()
	cfg.TmdbAPIKey = nil
	cfg.Settings["tvdb_api_key"] = "tvdb-key"
	cfg.Settings["tvdb_pin"] = "1234"
	return cfg
}

func TestTvdbMetadataEpisodesCountsAndTitles(t *testing.T) {
	fake := newTvdbFake(t)
	cfg := tvdbOnlyConfig()
	ctx := context.Background()
	meta := seriesMetadataFor(&cfg)
	if meta.Source() != "tvdb" || !meta.Configured() {
		t.Fatalf("without a TMDB key the provider must be TVDB, got %s", meta.Source())
	}
	id, err := meta.ResolveSeriesID(ctx, "Example Show")
	if err != nil || id == nil || *id != "77" {
		t.Fatalf("resolve = %v, %v", id, err)
	}
	counts, err := meta.SeasonCounts(ctx, "77")
	if err != nil || counts[0] != 1 || counts[1] != 3 || counts[2] != 2 {
		t.Fatalf("counts = %v, %v (both pages must be read)", counts, err)
	}
	title, _ := meta.EpisodeTitle(ctx, "77", 1, 1)
	if title == nil || *title != "L'inizio" {
		t.Fatalf("translated title = %v", title)
	}
	title, _ = meta.EpisodeTitle(ctx, "77", 1, 2)
	if title == nil || *title != "Second" {
		t.Fatalf("an untranslated episode keeps the original title, got %v", title)
	}
	next, _ := meta.NextEpisode(ctx, "77")
	if next == nil || *next.SeasonNumber != 2 || *next.EpisodeNumber != 2 {
		t.Fatalf("next episode = %+v", next)
	}
	season, _ := meta.SeasonEpisodes(ctx, "77", 2)
	if len(season) != 2 || season[0].AirDate == nil || *season[0].AirDate != "2021-01-01" {
		t.Fatalf("season 2 = %+v", season)
	}
	info, err := meta.SeriesInfo(ctx, "Example Show", nil)
	if err != nil || info == nil {
		t.Fatalf("info = %v, %v", info, err)
	}
	if info["name"] != "Esempio" || info["overview"] != "Un esempio." || info["status"] != "Returning Series" ||
		info["poster_path"] != "https://artworks.thetvdb.com/77.jpg" || info["number_of_seasons"] != 2 ||
		info["number_of_episodes"] != 5 || info["first_air_date"] != "2020-01-01" {
		t.Fatalf("info = %+v", info)
	}
	if _, hasID := info["id"]; hasID {
		t.Fatal("a TVDB record must not carry an \"id\": it would be read as a TMDB id")
	}
	if countries, _ := info["origin_country"].([]any); len(countries) != 1 || countries[0] != "US" {
		t.Fatalf("country = %v", info["origin_country"])
	}
	// Clients are built per request: the token is shared, one login only, PIN sent.
	if fake.logins.Load() != 1 || len(fake.pins) != 1 || fake.pins[0] != "1234" {
		t.Fatalf("logins = %d, pins = %v", fake.logins.Load(), fake.pins)
	}
}

func TestSeriesMetadataPrefersTmdb(t *testing.T) {
	cfg := tvdbOnlyConfig()
	key := "tmdb-key"
	cfg.TmdbAPIKey = &key
	if source := seriesMetadataFor(&cfg).Source(); source != "tmdb" {
		t.Fatalf("with both keys the provider must be TMDB, got %s", source)
	}
	cfg.TmdbAPIKey = nil
	delete(cfg.Settings, "tvdb_api_key")
	if seriesMetadataFor(&cfg).Configured() {
		t.Fatal("no key: the provider must not be configured")
	}
	if got := metadataImageURL("/abc.jpg", "w154"); got != TmdbImageBaseURL+"/w154/abc.jpg" {
		t.Fatalf("TMDB path = %q", got)
	}
	if got := metadataImageURL("https://artworks.thetvdb.com/x.jpg", "w154"); got != "https://artworks.thetvdb.com/x.jpg" {
		t.Fatalf("TVDB URL = %q", got)
	}
}

func TestTvdbOnlyCycleMetadataAndStatus(t *testing.T) {
	newTvdbFake(t)
	cfg := tvdbOnlyConfig()
	cfg.Series = []SeriesConfig{{Name: "Example Show", Enabled: true}}
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "gextto.db"))
	if err != nil {
		t.Fatal(err)
	}
	refreshSeriesMetadata(context.Background(), &cfg, db)
	counts, err := db.SeriesSeasonCounts("Example Show")
	if err != nil || len(counts) != 2 || counts[0] != [2]int64{1, 3} || counts[1] != [2]int64{2, 2} {
		t.Fatalf("season counts = %v, %v", counts, err)
	}
	statuses, _ := db.SeriesStatuses()
	if statuses["Example Show"] != "Returning Series" {
		t.Fatalf("status = %q", statuses["Example Show"])
	}
	// The missing episodes now include the seasons TVDB knows about.
	gaps, err := db.UnarchivedEpisodesForSeries("Example Show", nil)
	if err != nil || len(gaps) < 5 {
		t.Fatalf("gaps = %v, %v", gaps, err)
	}
}

func TestTvdbOnlyAnimeNumbering(t *testing.T) {
	newTvdbFake(t)
	cfg := tvdbOnlyConfig()
	cfg.Series = []SeriesConfig{{Name: "Example Show", Enabled: true, Anime: true}}
	numbering := animeNumberingFor(context.Background(), &cfg, &cfg.Series[0])
	// Absolute 4 is the first episode of season 2 (season 1 has 3).
	season, episode, ok := numbering.seasonEpisode(4)
	if !ok || season != 2 || episode != 1 {
		t.Fatalf("absolute 4 -> S%dE%d (%v)", season, episode, ok)
	}
}

func TestTvdbOnlyEpisodeRenameUsesTvdbTitle(t *testing.T) {
	newTvdbFake(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.mkv"), []byte("episode"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := tvdbOnlyConfig()
	cfg.RenameEpisodes = true
	cfg.RenameFormat = "standard"
	cfg.Series = []SeriesConfig{{Name: "Example Show", Enabled: true, TvdbID: "77"}}
	release := models.Release{
		Title: "Example Show S01E01", Magnet: "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Source: "rss", Kind: "series", Series: stringPtr("Example Show"), Season: int64Ptr(1), Episode: int64Ptr(1),
		EpisodeRange: []int64{1}, Seeders: -1, Peers: -1, DiscoveredAt: time.Now().UTC(),
	}
	renamed, err := RenameEpisode(context.Background(), root, &release, &cfg, NewTmdbClient(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.Base(renamed), "L'inizio") {
		t.Fatalf("renamed = %q, want the TVDB title", renamed)
	}
}

func TestTvdbOnlyEpisodeRenameSurvivesTvdbFailure(t *testing.T) {
	tvdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.mkv"), []byte("episode"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := tvdbOnlyConfig()
	cfg.RenameEpisodes = true
	cfg.RenameFormat = "standard"
	cfg.Series = []SeriesConfig{{Name: "Example Show", Enabled: true}}
	release := models.Release{
		Title: "Example Show S01E01", Magnet: "magnet:?xt=urn:btih:0123456789012345678901234567890123456789",
		Source: "rss", Kind: "series", Series: stringPtr("Example Show"), Season: int64Ptr(1), Episode: int64Ptr(1),
		EpisodeRange: []int64{1}, Seeders: -1, Peers: -1, DiscoveredAt: time.Now().UTC(),
	}
	renamed, err := RenameEpisode(context.Background(), root, &release, &cfg, NewTmdbClient(nil))
	if err != nil || renamed == "" {
		t.Fatalf("a TVDB login failure must not block the import: %q, %v", renamed, err)
	}
}

func TestTvdbOnlyCalendar(t *testing.T) {
	newTvdbFake(t)
	cfg := tvdbOnlyConfig()
	cfg.Series = []SeriesConfig{{Name: "Example Show", Enabled: true}}
	response := gh0_buildCalendar(context.Background(), &cfg)
	items, _ := response["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("calendar = %+v", response)
	}
	item := items[0].(map[string]any)
	if item["tvdb_id"] != "77" || item["tmdb_id"] != "" || item["poster"] == nil {
		t.Fatalf("calendar item = %+v", item)
	}
	episode := item["episode"].(map[string]any)
	if v2AnyID(episode["season_number"]) != "2" || v2AnyID(episode["episode_number"]) != "2" {
		t.Fatalf("next episode = %+v", episode)
	}
}
