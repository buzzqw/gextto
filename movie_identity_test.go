package gextto

// movie_identity_test.go covers the two moments the payload says which film it
// is: the name the torrent carries (before the download) and the duration of the
// file (before the import).

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func movieIdentityTestRelease() *models.Release {
	year := int64(2026)
	return &models.Release{
		Title:   "The Beast",
		Kind:    "movie",
		Year:    &year,
		Magnet:  "magnet:?xt=urn:btih:fbb74f3647c4ea25914a8ccbc90e7a86123357a1",
		Quality: models.Quality{Resolution: "1080p", Source: "webrip"},
	}
}

func movieIdentityTestConfig() *Config {
	cfg := DefaultConfig()
	cfg.Movies = []MovieConfig{{
		ID:      1,
		Name:    "The Beast",
		Year:    "2026",
		TmdbID:  "1185344",
		Enabled: true,
	}}
	return &cfg
}

// The torrent's own name is the first place the payload says which film it is:
// a name that is not the film's is refused before a piece is downloaded.
func TestMovieIdentityMismatchReadsTheTorrentName(t *testing.T) {
	cfg := movieIdentityTestConfig()
	release := movieIdentityTestRelease()
	for _, name := range []string{
		"Beast.Race.2026.1080p.ITA-POR-ENG.MULTI.WEBRip.x265.AAC-V3SP4EV3R.mkv",
		"Beast Race 2026 1080p AMZN WEB DL DUAL DDP5 1 Atmos H 264 FLUX",
		"The Beast Of Whisper Ridge (2026) [1080p] [WEBRip]",
		"Heart.of.the.Beast.2026.1080p.WEB-DL",
	} {
		if reason := movieIdentityMismatch(cfg, release, name); reason == "" {
			t.Errorf("torrent %q was not refused", name)
		}
	}
	for _, name := range []string{
		"The.Beast.2026.1080p.WEBRip.10Bit.DDP5.1.x265-NeoNoir",
		"The Beast (2026) WEB-DL [H.264/1080p]",
		"[ITA] The.Beast.2026.1080p.WEB-DL.mkv",
		"ITA.The.Beast.2026.1080p.WEB-DL-GROUP",
		"Beast.2026.iTALiAN.MD.1080p.HDTS.x264",
	} {
		if reason := movieIdentityMismatch(cfg, release, name); reason != "" {
			t.Errorf("torrent %q was refused: %s", name, reason)
		}
	}
	// Nothing to judge: no name, an episode, or a film that is not in the list.
	if reason := movieIdentityMismatch(cfg, release, ""); reason != "" {
		t.Errorf("an empty name was refused: %s", reason)
	}
	if reason := movieIdentityMismatch(cfg, release, "Show.S01E01.1080p.WEB-DL"); reason != "" {
		t.Errorf("an episodic name was refused: %s", reason)
	}
	if reason := movieIdentityMismatch(cfg, &models.Release{Title: "Unlisted Film", Kind: "movie"}, "Other.Film.2026.1080p"); reason != "" {
		t.Errorf("a film not in the list was judged: %s", reason)
	}
}

// The same torrent can be listed under the name of another monitored film: the
// refusal says which one it really is.
func TestMovieIdentityMismatchNamesTheFilmThePayloadIs(t *testing.T) {
	cfg := movieIdentityTestConfig()
	cfg.Movies = append(cfg.Movies, MovieConfig{ID: 2, Name: "Beast Race", Year: "2026", Enabled: true})
	release := movieIdentityTestRelease()
	reason := movieIdentityMismatch(cfg, release, "Beast.Race.2026.1080p.ITA-POR-ENG.MULTI.WEBRip.x265.AAC-V3SP4EV3R.mkv")
	if reason == "" || !containsFold(reason, "Beast Race") {
		t.Fatalf("reason = %q, want the other film named", reason)
	}
	// Its own release is still accepted, through the film's own name.
	if reason := movieIdentityMismatch(cfg, release, "The.Beast.2026.1080p.WEB-DL"); reason != "" {
		t.Fatalf("the film's own release was refused: %s", reason)
	}
}

// The duration of the completed file is the second place: a video that does not
// run like the film is another film, whatever the name promised.
func TestMovieRuntimeMismatchComparesTheRealDuration(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if filepath.Base(r.URL.Path) != "1185344" {
			t.Errorf("unexpected TMDB path %q", r.URL.Path)
		}
		tmdbWriteJSON(t, w, `{"id":1185344,"title":"The Beast","runtime":99}`)
	})
	previousProbe, previousAvailable := probeFile, probeAvailable
	t.Cleanup(func() { probeFile, probeAvailable = previousProbe, previousAvailable })
	probeAvailable = func() bool { return true }
	runtime := int64(99 * 60)
	probeFile = func(path string) (MediaInfo, error) {
		return MediaInfo{RuntimeSeconds: runtime}, nil
	}
	cfg := movieIdentityTestConfig()
	release := movieIdentityTestRelease()
	key := "test-key"
	tmdb := NewTmdbClientWithLanguage(&key, "it-IT")

	// The wrong film that started this: 2 hours 5 minutes against 1 hour 39.
	runtime = 7521
	reason := movieRuntimeMismatch(context.Background(), cfg, release, "/downloads/film.mkv", tmdb)
	if reason == "" || !containsFold(reason, "2 hours 5 minutes") || !containsFold(reason, "1 hour 39 minutes") {
		t.Fatalf("reason = %q, want both durations named", reason)
	}
	// The same film, with the seconds of its credits and an extended edition.
	for _, seconds := range []int64{99 * 60, 100*60 + 30, 110 * 60} {
		runtime = seconds
		if reason := movieRuntimeMismatch(context.Background(), cfg, release, "/downloads/film.mkv", tmdb); reason != "" {
			t.Errorf("a %d-second file was refused: %s", seconds, reason)
		}
	}
}

// The check never refuses when it cannot tell: no probe, no TMDB runtime, a
// film without an identifier, an episode.
func TestMovieRuntimeMismatchIsSilentWithoutEvidence(t *testing.T) {
	tmdbTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		tmdbWriteJSON(t, w, `{"id":1185344,"title":"The Beast","runtime":0}`)
	})
	previousProbe, previousAvailable := probeFile, probeAvailable
	t.Cleanup(func() { probeFile, probeAvailable = previousProbe, previousAvailable })
	cfg := movieIdentityTestConfig()
	key := "test-key"
	tmdb := NewTmdbClientWithLanguage(&key, "it-IT")
	probeAvailable = func() bool { return false }
	if reason := movieRuntimeMismatch(context.Background(), cfg, movieIdentityTestRelease(), "/downloads/film.mkv", tmdb); reason != "" {
		t.Fatalf("refused without a probe: %s", reason)
	}
	probeAvailable = func() bool { return true }
	probeFile = func(path string) (MediaInfo, error) {
		return MediaInfo{RuntimeSeconds: 7521}, nil
	}
	// TMDB does not know the runtime.
	if reason := movieRuntimeMismatch(context.Background(), cfg, movieIdentityTestRelease(), "/downloads/film.mkv", tmdb); reason != "" {
		t.Fatalf("refused without a runtime: %s", reason)
	}
	// A film without an identifier, and an episode.
	unidentified := movieIdentityTestConfig()
	unidentified.Movies[0].TmdbID = ""
	if reason := movieRuntimeMismatch(context.Background(), unidentified, movieIdentityTestRelease(), "/downloads/film.mkv", tmdb); reason != "" {
		t.Fatalf("refused without an identifier: %s", reason)
	}
	episode := movieIdentityTestRelease()
	episode.Kind = "series"
	if reason := movieRuntimeMismatch(context.Background(), cfg, episode, "/downloads/film.mkv", tmdb); reason != "" {
		t.Fatalf("an episode was judged as a film: %s", reason)
	}
}

// The check runs when the torrent metadata arrives, before any data: the
// torrent is taken out of the session and blocklisted, and the film stays in the
// queue.
func TestMetadataReceivedRejectsATorrentOfAnotherFilm(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDatabase(filepath.Join(dir, "gextto.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := movieIdentityTestConfig()
	release := *movieIdentityTestRelease()
	hash := "fbb74f3647c4ea25914a8ccbc90e7a86123357a1"
	torrents := &stubTorrentSession{}
	event := models.TorrentEvent{Hash: hash, Name: "Beast.Race.2026.1080p.ITA-POR-ENG.MULTI.WEBRip.x265.AAC-V3SP4EV3R.mkv"}
	metadata := &models.TorrentMeta{Release: release}
	handled, err := tevMetadataReceived(cfg, torrents, db, metadata, event)
	if err != nil {
		t.Fatalf("tevMetadataReceived: %v", err)
	}
	if handled {
		t.Fatal("a refused payload must not be reported as handled")
	}
	if len(torrents.removed) != 1 || torrents.removed[0] != hash || !torrents.deleteFlags[0] {
		t.Fatalf("torrent removal = %v %v, want the payload deleted", torrents.removed, torrents.deleteFlags)
	}
	if blocked, err := db.IsBlocklisted(hash); err != nil || !blocked {
		t.Fatalf("IsBlocklisted = %v, %v", blocked, err)
	}
	if record, ok := db.BlocklistRecordFor(hash); !ok || record.Reason != movieIdentityBlockReason {
		t.Fatalf("blocklist reason = %+v (%v), want %q", record, ok, movieIdentityBlockReason)
	}

	// The film's own torrent keeps going.
	good := &stubTorrentSession{}
	goodEvent := models.TorrentEvent{Hash: hash, Name: "The.Beast.2026.1080p.WEBRip.10Bit.DDP5.1.x265-NeoNoir"}
	if _, err := tevMetadataReceived(cfg, good, db, metadata, goodEvent); err != nil {
		t.Fatalf("tevMetadataReceived: %v", err)
	}
	if len(good.removed) != 0 {
		t.Fatalf("the film's own torrent was removed: %v", good.removed)
	}
}

// containsFold is a case-insensitive substring test for the assertions above.
func containsFold(value, needle string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(needle))
}
