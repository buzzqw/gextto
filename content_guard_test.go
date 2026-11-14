package gextto

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func TestUnsafePayloadReason(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		kind  string
		want  string
	}{
		{
			name:  "a film with a program in its folder",
			files: []string{"Movie.2024.1080p.mkv", "Movie.2024.1080p/Setup.exe"},
			kind:  "movie",
			want:  "program or a script (Setup.exe)",
		},
		{
			name:  "a script beside an episode",
			files: []string{"Show.S01E01.mkv", "crack.bat"},
			kind:  "series",
			want:  "program or a script (crack.bat)",
		},
		{
			name:  "a Windows installer side by side",
			files: []string{"Show.S01E01.mkv", "codec-pack.msi"},
			kind:  "series",
			want:  "program or a script (codec-pack.msi)",
		},
		{
			name:  "a payload with no video at all",
			files: []string{"README.txt", "cover.jpg", "sample.mkv"},
			kind:  "movie",
			want:  "no video file",
		},
		{
			name:  "a video living only in a Sample folder",
			files: []string{"Movie.2024/Sample/sample.mkv"},
			kind:  "movie",
			want:  "no video file",
		},
		{
			name:  "a film whose title carries the word sample",
			files: []string{"The.Sample.Case.2024.1080p.mkv"},
			kind:  "movie",
			want:  "",
		},
		{
			name:  "an episode with its sidecars",
			files: []string{"Show.S01E01.mkv", "Show.S01E01.nfo", "Show.S01E01.url", "Show.S01E01.srt"},
			kind:  "series",
			want:  "",
		},
		{
			name:  "a RAR set holding the video",
			files: []string{"Movie.2024.part01.rar", "Movie.2024.part02.rar"},
			kind:  "movie",
			want:  "",
		},
		{
			name:  "the old RAR naming r00",
			files: []string{"Movie.2024.rar", "Movie.2024.r00", "Movie.2024.r01"},
			kind:  "movie",
			want:  "",
		},
		{
			name:  "a split 7z archive",
			files: []string{"Movie.2024.7z.001", "Movie.2024.7z.002"},
			kind:  "movie",
			want:  "",
		},
		{
			name:  "a payload that is not a film or an episode",
			files: []string{"Comic.cbz", "Comic.cbr"},
			kind:  "comic",
			want:  "",
		},
		{
			name:  "a program is refused whatever the kind",
			files: []string{"Comic.cbz", "keygen.exe"},
			kind:  "comic",
			want:  "program or a script (keygen.exe)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := unsafePayloadReason(tc.files, tc.kind)
			if tc.want == "" {
				if got != "" {
					t.Fatalf("unsafePayloadReason(%v, %q) = %q, want no verdict", tc.files, tc.kind, got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("unsafePayloadReason(%v, %q) = %q, want it to contain %q", tc.files, tc.kind, got, tc.want)
			}
		})
	}
}

func TestIsSampleFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"sample.mkv", true},
		{"Movie.2024.sample.mkv", true},
		{"Movie.2024/Sample/sample.mkv", true},
		{"Movie.2024/SAMPLES/clip.mkv", true},
		{"The.Sample.Case.2024.1080p.mkv", false},
		{"Movie.2024.1080p.mkv", false},
	}
	for _, tc := range cases {
		if got := isSampleFile(tc.path); got != tc.want {
			t.Fatalf("isSampleFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// contentInspectFixture builds a series torrent the cycle started, with the
// file list the engine reports once the metadata has arrived.
func contentInspectFixture(t *testing.T, files []models.FileView) (*Config, *Database, *packInspectEngine, string) {
	t.Helper()
	db := newTestDB(t)
	seriesName := "Show"
	season, episode := int64(1), int64(1)
	release := models.Release{
		Title:    "Show.S01E01.1080p",
		Magnet:   packInspectMagnet,
		Kind:     "series",
		Series:   &seriesName,
		Season:   &season,
		Episode:  &episode,
		Quality:  models.Quality{Resolution: "1080p", Codec: "h264"},
		InfoHash: strings.TrimPrefix(packInspectMagnet, "magnet:?xt=urn:btih:"),
	}
	if err := db.RegisterTorrentScored(&release, release.Quality.Score()); err != nil {
		t.Fatalf("register torrent: %v", err)
	}
	hash := strings.TrimPrefix(packInspectMagnet, "magnet:?xt=urn:btih:")
	engine := &packInspectEngine{
		torrents: []models.TorrentView{{Hash: hash, Name: "Show.S01E01.1080p", Progress: 1, TotalDone: 1024}},
		files:    map[string][]models.FileView{hash: files},
	}
	cfg := DefaultConfig()
	cfg.DryRun = false
	cfg.Series = []SeriesConfig{{Name: seriesName, Enabled: true}}
	return &cfg, db, engine, hash
}

func TestInspectActiveContentRemovesAnUnsafePayload(t *testing.T) {
	cfg, db, engine, hash := contentInspectFixture(t, []models.FileView{
		{Path: "Show.S01E01.1080p.mkv"},
		{Path: "Show.S01E01.1080p/Setup.exe"},
	})
	InspectActiveContent(cfg, engine, db, nil, map[string]contentInspectionCheck{})
	if removed, ok := engine.removed[hash]; !ok || !removed {
		t.Fatalf("unsafe torrent not removed with its files: %v", engine.removed)
	}
	blocked, err := db.IsBlocklisted(hash)
	if err != nil {
		t.Fatalf("is blocklisted: %v", err)
	}
	if !blocked {
		t.Fatal("an unsafe payload was not blocklisted")
	}
}

func TestInspectActiveContentKeepsAPayloadWithAVideo(t *testing.T) {
	cfg, db, engine, _ := contentInspectFixture(t, []models.FileView{
		{Path: "Show.S01E01.1080p.mkv"},
		{Path: "Show.S01E01.1080p.nfo"},
	})
	InspectActiveContent(cfg, engine, db, nil, map[string]contentInspectionCheck{})
	if len(engine.removed) != 0 {
		t.Fatalf("a legitimate payload was removed: %v", engine.removed)
	}
}

// A torrent the operator started is their choice: the guard never discards it.
func TestInspectActiveContentLeavesAManualTorrentAlone(t *testing.T) {
	cfg, db, engine, hash := contentInspectFixture(t, []models.FileView{
		{Path: "desktop.exe"},
	})
	if err := db.SetTorrentReason(hash, manualReason); err != nil {
		t.Fatalf("set reason: %v", err)
	}
	InspectActiveContent(cfg, engine, db, nil, map[string]contentInspectionCheck{})
	if len(engine.removed) != 0 {
		t.Fatalf("a manual torrent was removed: %v", engine.removed)
	}
}

func TestInspectActiveContentWaitsForTheFileList(t *testing.T) {
	cfg, db, engine, hash := contentInspectFixture(t, nil)
	checked := map[string]contentInspectionCheck{}
	InspectActiveContent(cfg, engine, db, nil, checked)
	if len(engine.removed) != 0 {
		t.Fatalf("a torrent without its file list was removed: %v", engine.removed)
	}
	if _, ok := checked[hash]; !ok {
		t.Fatal("a torrent without its file list was not asked for again later")
	}
}

func TestInspectActiveContentDoesNothingInTestMode(t *testing.T) {
	cfg, db, engine, _ := contentInspectFixture(t, []models.FileView{
		{Path: "Setup.exe"},
	})
	cfg.DryRun = true
	InspectActiveContent(cfg, engine, db, nil, map[string]contentInspectionCheck{})
	if len(engine.removed) != 0 {
		t.Fatalf("test mode removed a torrent: %v", engine.removed)
	}
}

// refuseUnsafePayload keeps the release out of the next searches for a month
// and quarantines the link, so the cycle does not read the same file list again.
func TestRefuseUnsafePayloadRecordsTheRefusal(t *testing.T) {
	db := newTestDB(t)
	link := "https://indexer.example/dl/unsafe.torrent"
	release := &models.Release{
		Title:      "Show.S01E01.1080p",
		Kind:       "series",
		Magnet:     packInspectMagnet,
		TorrentURL: &link,
	}
	refuseUnsafePayload(db, release, time.Now())

	hash := strings.TrimPrefix(packInspectMagnet, "magnet:?xt=urn:btih:")
	blocked, err := db.IsBlocklisted(hash)
	if err != nil {
		t.Fatalf("is blocklisted: %v", err)
	}
	if !blocked {
		t.Fatal("the refused release is not on the blocklist")
	}
	var reason, expiresAt string
	if err := db.db.QueryRow("SELECT reason, COALESCE(expires_at,'') FROM blocklist WHERE magnet_hash=?1", hash).Scan(&reason, &expiresAt); err != nil {
		t.Fatalf("read blocklist row: %v", err)
	}
	if reason != unsafePayloadBlockReason {
		t.Fatalf("blocklist reason = %q, want %q", reason, unsafePayloadBlockReason)
	}
	if expiresAt == "" {
		t.Fatal("the refusal is permanent, it must expire")
	}
	quarantined, err := db.QuarantinedTorrentLinks(time.Now())
	if err != nil {
		t.Fatalf("quarantined links: %v", err)
	}
	if _, ok := quarantined[torrentLinkKey(link)]; !ok {
		t.Fatalf("the refused link was not quarantined: %v", quarantined)
	}
}

// The pre-download verdict reads the .torrent the cycle already downloaded and
// stops the release before it takes a slot.
func TestCandidateUnsafePayloadIsRefusedBeforeTheDownload(t *testing.T) {
	db := newTestDB(t)
	cfg := DefaultConfig()
	cfg.DryRun = false
	link := "https://indexer.example/dl/unsafe.torrent"
	release := models.Release{
		Title:      "Show.S01E01.1080p",
		Kind:       "series",
		Magnet:     packInspectMagnet,
		TorrentURL: &link,
	}
	torrentPath := writeTestTorrent(t, "Show.S01E01.mkv", "Setup.exe")
	run := &candidateRun{
		p:           &candidatePass{cfg: &cfg, db: db},
		release:     release,
		torrentFile: &torrentPath,
	}
	if !run.unsafePayload() {
		t.Fatal("a payload carrying a program was not refused")
	}
	hash := strings.TrimPrefix(packInspectMagnet, "magnet:?xt=urn:btih:")
	if blocked, err := db.IsBlocklisted(hash); err != nil || !blocked {
		t.Fatalf("the refused release is not blocklisted (blocked=%v, err=%v)", blocked, err)
	}
}

func TestCandidateUnsafePayloadAcceptsAVideo(t *testing.T) {
	db := newTestDB(t)
	cfg := DefaultConfig()
	cfg.DryRun = false
	torrentPath := writeTestTorrent(t, "Show.S01E01.mkv", "Show.S01E01.nfo")
	run := &candidateRun{
		p: &candidatePass{cfg: &cfg, db: db},
		release: models.Release{
			Title:  "Show.S01E01.1080p",
			Kind:   "series",
			Magnet: packInspectMagnet,
		},
		torrentFile: &torrentPath,
	}
	if run.unsafePayload() {
		t.Fatal("a payload with a video file was refused")
	}
}

// BenchmarkUnsafePayloadReason measures the verdict on the worst case: a
// payload that must be read to the end before it can be refused. The guard
// reads the list once per started candidate, so the cost has to stay in the
// microseconds even for a season pack's long list.
func BenchmarkUnsafePayloadReason(b *testing.B) {
	files := make([]string, 0, 5000)
	for i := 0; i < 5000; i++ {
		files = append(files, fmt.Sprintf("Show.S01E%02d.1080p.txt", i%99+1))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if unsafePayloadReason(files, "series") == "" {
			b.Fatal("a payload without a video file was accepted")
		}
	}
}
