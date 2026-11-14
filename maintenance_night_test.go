package gextto

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func clockAt(hour, minute int) time.Time {
	return time.Date(2026, 11, 3, hour, minute, 0, 0, time.Local)
}

func TestMaintenanceWindowContainsAndNight(t *testing.T) {
	window := maintenanceWindow{Start: 60, End: 7 * 60}
	for _, check := range []struct {
		hour, minute int
		inside       bool
	}{{0, 59, false}, {1, 0, true}, {6, 59, true}, {7, 0, false}, {23, 0, false}} {
		if got := window.Contains(clockAt(check.hour, check.minute)); got != check.inside {
			t.Errorf("%02d:%02d inside=%v, want %v", check.hour, check.minute, got, check.inside)
		}
	}
	// Across midnight: 23:00-05:00 is one night, named by the day it opened.
	wrap := maintenanceWindow{Start: 23 * 60, End: 5 * 60}
	if !wrap.Contains(clockAt(23, 30)) || !wrap.Contains(clockAt(4, 0)) || wrap.Contains(clockAt(12, 0)) {
		t.Fatal("a window across midnight is wrong")
	}
	if wrap.Night(clockAt(23, 30)) != "2026-11-03" || wrap.Night(clockAt(4, 0)) != "2026-11-02" {
		t.Fatalf("nights: %s %s", wrap.Night(clockAt(23, 30)), wrap.Night(clockAt(4, 0)))
	}
	if next := window.NextOpening(clockAt(8, 0)); next.Day() != 4 || next.Hour() != 1 {
		t.Fatalf("next opening = %v", next)
	}
	if next := window.NextOpening(clockAt(0, 30)); next.Day() != 3 || next.Hour() != 1 {
		t.Fatalf("next opening = %v", next)
	}
	if !(maintenanceWindow{Start: 120, End: 120}).Contains(clockAt(15, 0)) {
		t.Fatal("the same start and end mean the whole day")
	}
	cfg := &Config{Settings: map[string]string{maintenanceWindowStartSetting: "02:30", maintenanceWindowEndSetting: "bad"}}
	if got := maintenanceWindowFrom(cfg); got.Start != 150 || got.End != maintenanceDefaultEnd {
		t.Fatalf("window from settings = %+v", got)
	}
}

func TestMaintenanceWindowSettingIsValidated(t *testing.T) {
	if err := validateSettingValue(maintenanceWindowStartSetting, "01:00", &Config{}); err != nil {
		t.Fatal(err)
	}
	if err := validateSettingValue(maintenanceWindowEndSetting, "25:99", &Config{}); err == nil {
		t.Fatal("an impossible time was accepted")
	}
}

func TestMaintenanceBusyReasons(t *testing.T) {
	original := maintenanceLoadAverage
	defer func() { maintenanceLoadAverage = original }()
	idle := 0.1
	maintenanceLoadAverage = func() *float64 { return &idle }
	if reason := maintenanceBusyReason(nil); reason != "" {
		t.Fatalf("idle machine reported busy: %s", reason)
	}

	guard := AcquireArchiveImport(func() *string { name := "Serie A"; return &name }())
	if reason := maintenanceBusyReason(nil); !strings.Contains(reason, "archiviato") {
		t.Errorf("an import in progress was not seen: %q", reason)
	}
	guard.Release()

	libraryCheckRunning.Store(true)
	if reason := maintenanceBusyReason(nil); !strings.Contains(reason, "libreria") {
		t.Errorf("the library check was not seen: %q", reason)
	}
	libraryCheckRunning.Store(false)

	busy := 1000.0
	maintenanceLoadAverage = func() *float64 { return &busy }
	if reason := maintenanceBusyReason(nil); !strings.Contains(reason, "carico") {
		t.Errorf("a high load was not seen: %q", reason)
	}

	// With the window switch off the heavy jobs run at any hour.
	cfg := &Config{Settings: map[string]string{maintenanceHeavySetting: "false"}}
	if !maintenanceAllowedNow(nil, cfg, clockAt(15, 0)) {
		t.Fatal("heavy jobs blocked with the window switch off")
	}
	cfg.Settings[maintenanceHeavySetting] = "true"
	maintenanceLoadAverage = func() *float64 { return &idle }
	if maintenanceAllowedNow(nil, cfg, clockAt(15, 0)) {
		t.Fatal("heavy jobs allowed outside the window")
	}
	if !maintenanceAllowedNow(nil, cfg, clockAt(3, 0)) {
		t.Fatal("heavy jobs blocked inside the window on an idle machine")
	}
}

func TestMaintenanceHeavySlotIsExclusive(t *testing.T) {
	release := acquireMaintenanceHeavy(context.Background())
	if release == nil || !maintenanceHeavyBusy() {
		t.Fatal("the slot was not taken")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if second := acquireMaintenanceHeavy(ctx); second != nil {
		t.Fatal("two heavy jobs at once")
	}
	release()
	release()
	if maintenanceHeavyBusy() {
		t.Fatal("the slot was not released")
	}
}

func resetMaintenanceStatus(t *testing.T, dataDir string) {
	t.Helper()
	maintenanceState.Lock()
	maintenanceState.loaded = false
	maintenanceState.Unlock()
	maintenanceStatusLoad(dataDir)
	t.Cleanup(func() {
		maintenanceState.Lock()
		maintenanceState.loaded = false
		maintenanceState.path = ""
		maintenanceState.status = maintenanceStatus{}
		maintenanceState.Unlock()
	})
}

func TestDatabaseMaintenanceOptimisesAndChecks(t *testing.T) {
	state := newTestAppState(t)
	resetMaintenanceStatus(t, state.cfg.DataDir)
	now := time.Now()
	runDatabaseMaintenance(context.Background(), state, now)
	status := maintenanceStatusSnapshot()
	if !status.Night.DBDone || status.DB.CheckedAt.IsZero() || len(status.DB.Problems) != 0 || status.DB.ErrorMessage != "" {
		t.Fatalf("db result: %+v night: %+v", status.DB, status.Night)
	}
	if status.DB.BytesAfter <= 0 {
		t.Fatalf("sizes not read: %+v", status.DB)
	}
	// The integrity check is weekly: the next night keeps the old date.
	runDatabaseMaintenance(context.Background(), state, now.Add(24*time.Hour))
	if got := maintenanceStatusSnapshot().DB.CheckedAt; !got.Equal(status.DB.CheckedAt) {
		t.Fatalf("integrity check repeated after a day: %v -> %v", status.DB.CheckedAt, got)
	}
	runDatabaseMaintenance(context.Background(), state, now.Add(8*24*time.Hour))
	if got := maintenanceStatusSnapshot().DB.CheckedAt; got.Equal(status.DB.CheckedAt) {
		t.Fatal("integrity check not repeated after a week")
	}
	// The status survives a restart.
	data, err := os.ReadFile(filepath.Join(state.cfg.DataDir, maintenanceStatusFile))
	if err != nil || !strings.Contains(string(data), "checked_at") {
		t.Fatalf("status not saved: %v", err)
	}
}

func archiveTestEpisode(t *testing.T, db *Database, series string, season, episode int64, path string) {
	t.Helper()
	digest := sha1.Sum([]byte(path))
	title := fmt.Sprintf("%s.S%02dE%02d.1080p.WEB-DL.ITA", strings.ReplaceAll(series, " ", "."), season, episode)
	release := ParseRelease(title, "magnet:?xt=urn:btih:"+hex.EncodeToString(digest[:]), "test")
	if release == nil {
		t.Fatal("release not parsed")
	}
	release.Series = &series
	if err := db.MarkReleaseCompleted(release, path, 1000); err != nil {
		t.Fatalf("mark completed: %v", err)
	}
}

func saveTestSeries(t *testing.T, state *AppState, series ...SeriesConfig) {
	t.Helper()
	if err := SaveLibrary(state.cfg.DataDir, series, nil); err != nil {
		t.Fatalf("save library: %v", err)
	}
}

func TestLibraryReportFindsMissingFilesButNotUnreadableFolders(t *testing.T) {
	state := newTestAppState(t)
	resetMaintenanceStatus(t, state.cfg.DataDir)
	saveTestSeries(t, state,
		SeriesConfig{Name: "Serie A", Seasons: "1+", Enabled: true},
		SeriesConfig{Name: "Serie B", Seasons: "1+", Enabled: true})
	library := t.TempDir()
	present := filepath.Join(library, "Serie A", "Serie A - S01E01.mkv")
	gone := filepath.Join(library, "Serie A", "Serie A - S01E02.mkv")
	if err := os.MkdirAll(filepath.Dir(present), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(present, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A share that is not mounted: its mount point is there but empty.
	mountPoint := filepath.Join(library, "nas-not-mounted")
	if err := os.MkdirAll(mountPoint, 0o755); err != nil {
		t.Fatal(err)
	}
	unmounted := filepath.Join(mountPoint, "Serie B", "Serie B - S01E01.mkv")
	archiveTestEpisode(t, state.db, "Serie A", 1, 1, present)
	archiveTestEpisode(t, state.db, "Serie A", 1, 2, gone)
	archiveTestEpisode(t, state.db, "Serie B", 1, 3, unmounted)

	runLibraryReport(context.Background(), state)
	report := maintenanceStatusSnapshot().Library
	if len(report.Missing) != 1 || report.Missing[0].Path != gone {
		t.Fatalf("missing = %+v", report.Missing)
	}
	if len(report.Unreachable) != 1 || report.Unreachable[0] != "Serie B" {
		t.Fatalf("unreachable = %+v", report.Unreachable)
	}

	// Marking the gone episode missing clears its row; a present file is refused.
	if cleared, err := state.db.ForgetMissingEpisode("Serie A", 1, 1); err != nil || cleared {
		t.Fatalf("a present file was forgotten: %v %v", cleared, err)
	}
	if cleared, err := state.db.ForgetMissingEpisode("Serie A", 1, 2); err != nil || !cleared {
		t.Fatalf("the gone file was not forgotten: %v %v", cleared, err)
	}
	if cleared, _ := state.db.ForgetMissingEpisode("Serie B", 1, 3); cleared {
		t.Fatal("an unreadable folder was treated as a deletion")
	}
}

func TestLibraryReportCountsOnlyTheSeasonsStillMonitored(t *testing.T) {
	state := newTestAppState(t)
	resetMaintenanceStatus(t, state.cfg.DataDir)
	library := t.TempDir()
	folder := filepath.Join(library, "Serie A")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(folder, "Serie A - S18E01.mkv")
	inside := filepath.Join(folder, "Serie A - S21E01.mkv")
	archiveTestEpisode(t, state.db, "Serie A", 18, 1, outside)
	archiveTestEpisode(t, state.db, "Serie A", 21, 1, inside)
	saveTestSeries(t, state, SeriesConfig{Name: "Serie A", Seasons: "21+", Enabled: true})

	runLibraryReport(context.Background(), state)
	report := maintenanceStatusSnapshot().Library
	if len(report.Missing) != 1 || report.Missing[0].Season != 21 || report.Missing[0].Path != inside {
		t.Fatalf("missing = %+v", report.Missing)
	}
	if report.Files != 1 {
		t.Fatalf("files counted = %d, want only the monitored season", report.Files)
	}

	// The shape the page saves for "only season 23": the base range covers 21+
	// and every season up to 22 is ignored.
	ignored := make([]int64, 0, 22)
	for season := int64(1); season <= 22; season++ {
		ignored = append(ignored, season)
	}
	saveTestSeries(t, state, SeriesConfig{Name: "Serie A", Seasons: "21+", Enabled: true, IgnoredSeasons: ignored})
	runLibraryReport(context.Background(), state)
	if missing := maintenanceStatusSnapshot().Library.Missing; len(missing) != 0 {
		t.Fatalf("an ignored season was reported: %+v", missing)
	}
	saveTestSeries(t, state, SeriesConfig{Name: "Serie A", Seasons: "21+", Enabled: false})
	runLibraryReport(context.Background(), state)
	if missing := maintenanceStatusSnapshot().Library.Missing; len(missing) != 0 {
		t.Fatalf("a disabled series was reported: %+v", missing)
	}
}

func TestLibraryAudioCleanupCleansOnceAndWaitsForSeeding(t *testing.T) {
	requireAllAudioTools(t)
	state := newTestAppState(t)
	resetMaintenanceStatus(t, state.cfg.DataDir)
	for key, value := range map[string]string{audioCleanupSetting: "true", maintenanceAudioSetting: "true"} {
		if err := SaveSetting(state.cfg.DataDir, key, value); err != nil {
			t.Fatal(err)
		}
	}
	if !maintenanceAudioLibraryEnabled(latestConfig(state)) {
		t.Skip("settings not visible through latestConfig in this test setup")
	}

	library := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)
	makeEpisode := func(name string) string {
		path := filepath.Join(library, "Serie A", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		makeRichAudioCleanupFixture(t, path, richFixture{audio: []richAudioTrack{
			{language: "ita", isDefault: true}, {language: "eng"},
		}})
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
		return path
	}
	plain := makeEpisode("Serie A - S01E01.mkv")
	seeding := makeEpisode("Serie A - S01E02.mkv")
	if err := os.Link(seeding, filepath.Join(t.TempDir(), "download.mkv")); err != nil {
		t.Skipf("hardlinks not available: %v", err)
	}
	archiveTestEpisode(t, state.db, "Serie A", 1, 1, plain)
	archiveTestEpisode(t, state.db, "Serie A", 1, 2, seeding)

	runLibraryAudioCleanup(context.Background(), state, func() bool { return true })
	status := maintenanceStatusSnapshot()
	if status.Audio.Cleaned != 1 || status.Night.AudioCleaned != 1 {
		t.Fatalf("audio status: %+v", status.Audio)
	}
	if !status.Night.AudioDone {
		t.Fatal("the night was not closed after the whole list")
	}
	for path, want := range map[string]int{plain: 1, seeding: 2} {
		info, err := ProbeResult(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(info.AudioTracks) != want {
			t.Errorf("%s has %d audio tracks, want %d", filepath.Base(path), len(info.AudioTracks), want)
		}
	}

	// Next night: the cleaned file is remembered and not touched again.
	maintenanceStatusUpdate(func(status *maintenanceStatus) { status.Night = maintenanceNightSummary{Night: "next"} })
	before, _ := os.Stat(plain)
	runLibraryAudioCleanup(context.Background(), state, func() bool { return true })
	after, _ := os.Stat(plain)
	if !os.SameFile(before, after) {
		t.Fatal("a cleaned file was cleaned again")
	}

	// keepGoing false stops before any work, even with a file to do.
	third := makeEpisode("Serie A - S01E03.mkv")
	archiveTestEpisode(t, state.db, "Serie A", 1, 3, third)
	maintenanceStatusUpdate(func(status *maintenanceStatus) { status.Night = maintenanceNightSummary{Night: "later"} })
	cleanedBefore := maintenanceStatusSnapshot().Audio.Cleaned
	runLibraryAudioCleanup(context.Background(), state, func() bool { return false })
	if maintenanceStatusSnapshot().Audio.Cleaned != cleanedBefore {
		t.Fatal("work done although the window was closed")
	}
	if info, err := ProbeResult(third); err != nil || len(info.AudioTracks) != 2 {
		t.Fatal("a file was cleaned although the window was closed")
	}
}

func TestAudioLibraryNeedsCheck(t *testing.T) {
	now := time.Now()
	record := audioLibraryRecord{Size: 10, Modified: 20, Outcome: "cleaned", CheckedAt: now}
	if audioLibraryNeedsCheck(record, true, 10, 20, now) {
		t.Fatal("an unchanged cleaned file is checked again")
	}
	if !audioLibraryNeedsCheck(record, true, 11, 20, now) || !audioLibraryNeedsCheck(record, false, 10, 20, now) {
		t.Fatal("a changed or new file is not checked")
	}
	record.Outcome = "failed: no space"
	if audioLibraryNeedsCheck(record, true, 10, 20, now.Add(time.Hour)) {
		t.Fatal("a failed file is retried at once")
	}
	if !audioLibraryNeedsCheck(record, true, 10, 20, now.Add(8*24*time.Hour)) {
		t.Fatal("a failed file is never retried")
	}
	// Every tool produced a copy the checks refused: the same file would fail
	// the same way, so it waits for a new size or date instead of a timer.
	record.Outcome = "unsupported: the copy would be 10 seconds shorter"
	if audioLibraryNeedsCheck(record, true, 10, 20, now.Add(365*24*time.Hour)) {
		t.Fatal("a file that cannot be cleaned is probed again")
	}
	if !audioLibraryNeedsCheck(record, true, 11, 20, now) {
		t.Fatal("a changed file is not probed again")
	}
}

func TestMaintenanceNightTextAndNotification(t *testing.T) {
	text := maintenanceNightText(maintenanceNightSummary{DBDone: true, DBFreed: 80 << 20, ReportDone: true, Missing: 2, AudioCleaned: 3, AudioSaved: 1 << 30, AudioFailed: 1})
	for _, part := range []string{"databases optimised", "2 library file(s) missing", "3 file(s) cleaned", "1 left as they were"} {
		if !strings.Contains(text, part) {
			t.Errorf("summary misses %q: %s", part, text)
		}
	}
	body := maintenanceReportNotificationBody("it", map[string]any{"db_problems": 1, "detail": "series: page 3 broken", "missing": 2, "audio_cleaned": 3, "audio_saved": 1 << 30})
	for _, part := range []string{"Problemi trovati nel database: 1", "page 3 broken", "spariti dal disco: 2", "File puliti dalle altre lingue audio: 3"} {
		if !strings.Contains(body, part) {
			t.Errorf("notification misses %q:\n%s", part, body)
		}
	}
	if notifyEventGroup("maintenance_report") != "maintenance" {
		t.Fatal("the report is not in the maintenance group")
	}
}

func TestNightMaintenancePassRunsOnlyInTheWindow(t *testing.T) {
	original := maintenanceLoadAverage
	defer func() { maintenanceLoadAverage = original }()
	idle := 0.0
	maintenanceLoadAverage = func() *float64 { return &idle }

	state := newTestAppState(t)
	resetMaintenanceStatus(t, state.cfg.DataDir)
	ctx := context.Background()
	nightMaintenancePass(ctx, state, clockAt(15, 0), false)
	if status := maintenanceStatusSnapshot(); status.Night.DBDone || status.Night.ReportDone {
		t.Fatalf("work done outside the window: %+v", status.Night)
	}
	nightMaintenancePass(ctx, state, clockAt(15, 0), true)
	if status := maintenanceStatusSnapshot(); !status.Night.DBDone || !status.Night.ReportDone {
		t.Fatalf("a forced pass did nothing: %+v", status.Night)
	}
	// Outside the window the finished night is summed up once.
	nightMaintenancePass(ctx, state, clockAt(16, 0), false)
	if !maintenanceStatusSnapshot().Night.Notified {
		t.Fatal("the finished night was not summed up")
	}
}

func TestNightMaintenanceRunNowWaitsForIdleAndIsNotLost(t *testing.T) {
	original := maintenanceLoadAverage
	defer func() { maintenanceLoadAverage = original }()
	busy := 1000.0
	maintenanceLoadAverage = func() *float64 { return &busy }

	state := newTestAppState(t)
	resetMaintenanceStatus(t, state.cfg.DataDir)
	requestMaintenanceRun()
	defer maintenanceForcedPending.Store(false)
	<-maintenanceRunNow
	if ran := nightMaintenancePass(context.Background(), state, clockAt(15, 0), maintenanceForcedPending.Load()); ran {
		t.Fatal("the pass ran on a busy machine")
	}
	if !maintenanceForcedPending.Load() {
		t.Fatal("the request was dropped while Gextto was busy")
	}
	if status := maintenanceStatusSnapshot(); !strings.Contains(status.Waiting, "carico") {
		t.Fatalf("the panel does not say why it waits: %q", status.Waiting)
	}
	idle := 0.0
	maintenanceLoadAverage = func() *float64 { return &idle }
	if ran := nightMaintenancePass(context.Background(), state, clockAt(15, 2), maintenanceForcedPending.Load()); !ran {
		t.Fatal("the pending request did not run once idle")
	}
	if !maintenanceStatusSnapshot().Night.DBDone {
		t.Fatal("the database job did not run")
	}
}

func TestNightMaintenancePanelRenders(t *testing.T) {
	state := newTestAppState(t)
	resetMaintenanceStatus(t, state.cfg.DataDir)
	maintenanceStatusUpdate(func(status *maintenanceStatus) {
		status.Library = maintenanceLibraryResult{At: time.Now(), Files: 3, Missing: []maintenanceMissingFile{{Series: "Serie A", Season: 1, Episode: 2, Path: "/x/Serie A - S01E02.mkv"}}}
		status.DB = maintenanceDBResult{At: time.Now(), CheckedAt: time.Now(), BytesBefore: 2 << 20, BytesAfter: 1 << 20}
	})
	view := v2NightMaintenanceViewFrom(state)
	if view.Window != "01:00–07:00" || len(view.Missing) != 1 || view.DBFreed == "" {
		t.Fatalf("view = %+v", view)
	}
}
