package gextto

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestSourceTrustReportAggregatesFactsWithoutSecrets(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "gextto.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.RecordSourceTrustCycle([]models.CycleSourceStat{
		{Kind: "indexer", Name: "jackett:Tracker One", OK: 2, Fail: 1, Results: 5, LastError: "https://user:pass@example.invalid/download?secret=hidden"},
		{Kind: "feed", Name: "https://feed.example.invalid/rss", OK: 1, Results: 3},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordSourceTrustAccepted(&Config{}, &models.Release{Source: "jackett:Tracker One", Network: "torrent"}); err != nil {
		t.Fatal(err)
	}
	report, err := db.SourceTrustReport(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Sources) != 2 {
		t.Fatalf("sources = %+v", report.Sources)
	}
	var indexer SourceTrustStat
	for _, source := range report.Sources {
		if source.Kind == "indexer" {
			indexer = source
		}
	}
	if indexer.Attempts != 3 || indexer.Successes != 2 || indexer.Failures != 1 || indexer.Results != 5 || indexer.Accepted != 1 {
		t.Fatalf("indexer totals = %+v", indexer)
	}
	if strings.Contains(indexer.LastError, "hidden") || strings.Contains(indexer.LastError, "user:pass") {
		t.Fatalf("source error leaked credentials: %q", indexer.LastError)
	}
}

func TestTransferFinalHNRUsesTheRealSeedPolicy(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "gextto.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := DefaultConfig()
	cfg.Transfer.SeedRatio = 1.0
	cfg.Transfer.SeedTimeDays = 1
	hash := strings.Repeat("e", 40)
	release := models.Release{Title: "Example S01E01", Magnet: "magnet:?xt=urn:btih:" + hash, Source: "jackett:Tracker One", Kind: "series", Network: "torrent"}
	if err := db.RegisterTorrentScored(&release, 1); err != nil {
		t.Fatal(err)
	}
	view := &models.TorrentView{Hash: hash, Name: release.Title, Private: true, TotalSize: 1000, AllTimeDownload: 1000, AllTimeUpload: 100, SeedingSeconds: 10, SeedRatio: -1, SeedDays: -1}
	captureTorrentTransferFinal(&cfg, db, view, "removed by the operator", "operator")
	report, err := db.SourceTrustReport(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Finals) != 1 || report.Finals[0].HNRState != "possible_shortfall" {
		t.Fatalf("finals = %+v, want one possible HnR shortfall", report.Finals)
	}
	if report.Finals[0].ShareRatio != 0.1 || report.Finals[0].TargetRatio != 1 {
		t.Fatalf("ratio snapshot = %+v", report.Finals[0])
	}
	for _, source := range report.Sources {
		if source.Kind == "indexer" && (source.Completed != 1 || source.HNRWarnings != 1) {
			t.Fatalf("source totals = %+v", source)
		}
	}

	captureTorrentTransferFinal(&cfg, db, view, "seed limit reached", "policy")
	report, err = db.SourceTrustReport(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Finals) != 2 || report.Finals[0].HNRState != "met" {
		t.Fatalf("policy final = %+v", report.Finals)
	}
}

func TestTransferFinalHNRIgnoresPublicAndInfiniteSeeds(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "gextto.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := DefaultConfig()
	cfg.Transfer.SeedRatio = 1
	cfg.Transfer.SeedTimeDays = 1
	public := &models.TorrentView{Hash: strings.Repeat("a", 40), Name: "Public", TotalSize: 1000, AllTimeDownload: 1000, AllTimeUpload: 0, SeedingSeconds: 1, SeedRatio: -1, SeedDays: -1}
	captureTorrentTransferFinal(&cfg, db, public, "removed", "operator")
	infiniteCfg := DefaultConfig()
	infiniteCfg.Transfer.SeedRatio = 0
	infiniteCfg.Transfer.SeedTimeDays = 0
	private := &models.TorrentView{Hash: strings.Repeat("b", 40), Name: "Private", Private: true, TotalSize: 1000, AllTimeDownload: 1000, AllTimeUpload: 0, SeedingSeconds: 1, SeedRatio: -1, SeedDays: -1}
	captureTorrentTransferFinal(&infiniteCfg, db, private, "removed", "operator")
	report, err := db.SourceTrustReport(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Finals) != 2 {
		t.Fatalf("finals = %+v", report.Finals)
	}
	for _, final := range report.Finals {
		if final.HNRState != "not_applicable" {
			t.Fatalf("final = %+v, want not_applicable", final)
		}
	}
}
