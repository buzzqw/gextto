package gextto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cleanerWrite writes a small file for the cleaner tests.
func cleanerWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// cleanerFileExists reports whether path is an existing regular file.
func cleanerFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// cleanerScore is the pure quality score of a file name ( `score()`).
func cleanerScore(title string) int64 {
	quality := ParseQuality(title)
	return quality.Score()
}

func TestIndexArchiveKeepsBestFilePerEpisode(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "Serie")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cleanerWrite(t, filepath.Join(archive, "Example - S01E01 - Titolo - [1080p][h264][AAC][IT].mkv"), "hd")
	cleanerWrite(t, filepath.Join(archive, "Example - S01E01 - Titolo - [2160p][h265][DDP][HDR][IT].mkv"), "4k")
	cleanerWrite(t, filepath.Join(archive, "Example - S01E02 - Altro - [1080p][h264][AAC][IT].mkv"), "hd")
	// Un'altra serie nella stessa cartella non deve entrare nell'indice.
	cleanerWrite(t, filepath.Join(archive, "Altro - S01E01 - X - [2160p][h265][IT].mkv"), "x")

	index := IndexArchive("Example", archive, map[string]string{})
	best, ok := index.BestFor(1, 1)
	assertTrue(t, ok, "E01 presente")
	assertEqual(t, best.Quality.Resolution, "2160p")
	second, ok := index.BestFor(1, 2)
	assertTrue(t, ok, "E02 presente")
	assertEqual(t, second.Quality.Resolution, "1080p")
	if _, ok := index.BestFor(1, 3); ok {
		t.Fatalf("E03 non deve essere presente")
	}
}

func TestMovesOnlyLowerQualityMatchingEpisodeToTrash(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "archive")
	trash := filepath.Join(root, "trash")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cleanerWrite(t, filepath.Join(archive, "Example.S01E01.720p.WEB-DL.mkv"), "old")

	cfg := DefaultConfig()
	cfg.CleanupUpgrades = true
	cfg.TrashPath = &trash

	kept := filepath.Join(archive, "Example - S01E01 - Title.mkv")
	cleanerWrite(t, kept, "new")

	removed, err := CleanupOldEpisode(&cfg, "Example", 1, 1, cleanerScore("Example.S01E01.1080p.WEB-DL.mkv"), kept, archive)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	assertEqual(t, removed, 1)
	assertTrue(t, cleanerFileExists(filepath.Join(trash, "Example.S01E01.720p.WEB-DL.mkv")), "il vecchio va nel trash")
	assertTrue(t, cleanerFileExists(kept), "il nuovo resta")
}

func TestCleanupUsesApprovedRepackMetadataWhenFinalNameOmitsRepack(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "archive")
	trash := filepath.Join(root, "trash")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// The old normalized name has no source token. This is common for files
	// archived before the current rename format was introduced.
	old := filepath.Join(archive, "Example - S01E01 - Title - [2160p][h265][DV HDR10][EAC3][IT+EN].mkv")
	cleanerWrite(t, old, "old")
	newFile := filepath.Join(archive, "Example - S01E01 - Title - [WEB-DL][2160p][h265][DV HDR10][EAC3][IT+EN].mkv")
	cleanerWrite(t, newFile, "new")

	cfg := DefaultConfig()
	cfg.CleanupUpgrades = true
	cfg.TrashPath = &trash
	releaseQuality := ParseQuality("Example.S01E01.Repack.ITA.ENG.2160p.AMZN.WEB-DL.DDP5.1.DV.HDR.H.265.mkv")

	removed, err := CleanupOldEpisodeWithQuality(
		&cfg, "Example", 1, 1, releaseQuality.Score(), newFile, archive, releaseQuality,
	)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	assertEqual(t, removed, 1)
	assertTrue(t, cleanerFileExists(filepath.Join(trash, filepath.Base(old))), "il vecchio repack va nel trash")
	assertTrue(t, cleanerFileExists(newFile), "il nuovo resta")
}

func TestMovesCompletedPackDirectoryToTrash(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "download")
	trash := filepath.Join(root, "trash")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cleanerWrite(t, filepath.Join(source, "nested", "episode.mkv"), "episode")

	target, err := MoveToTrash(source, trash)
	if err != nil {
		t.Fatalf("move to trash: %v", err)
	}

	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source deve essere rimossa")
	}
	data, err := os.ReadFile(filepath.Join(target, "nested", "episode.mkv"))
	if err != nil {
		t.Fatalf("read trashed file: %v", err)
	}
	assertEqual(t, string(data), "episode")
}

func TestCleanupRecognizesLegacyNxNnEpisodeNames(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "archive")
	trash := filepath.Join(root, "trash")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cleanerWrite(t, filepath.Join(archive, "Example.1x01.720p.WEB-DL.mkv"), "old")
	kept := filepath.Join(archive, "Example - S01E01 - Title - [1080p][h265].mkv")
	cleanerWrite(t, kept, "new")

	cfg := DefaultConfig()
	cfg.CleanupUpgrades = true
	cfg.TrashPath = &trash

	removed, err := CleanupOldEpisode(&cfg, "Example", 1, 1, cleanerScore("Example.S01E01.1080p.WEB-DL.mkv"), kept, archive)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	assertEqual(t, removed, 1)
	assertTrue(t, cleanerFileExists(filepath.Join(trash, "Example.1x01.720p.WEB-DL.mkv")), "il vecchio va nel trash")
}

func TestDiscardsNewEpisodeWhenExistingFileIsBetter(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "archive")
	trash := filepath.Join(root, "trash")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := filepath.Join(archive, "Example.S01E01.1080p.WEB-DL.mkv")
	cleanerWrite(t, existing, "old")
	newFile := filepath.Join(archive, "Example - S01E01 - Title.mkv")
	cleanerWrite(t, newFile, "new")

	cfg := DefaultConfig()
	cfg.CleanupUpgrades = true
	cfg.TrashPath = &trash

	discarded, err := DiscardIfInferior(&cfg, "Example", 1, 1, cleanerScore("Example.S01E01.720p.WEB-DL.mkv"), newFile, archive)
	if err != nil {
		t.Fatalf("discard: %v", err)
	}
	assertTrue(t, discarded, "il nuovo inferiore va scartato")
	if _, err := os.Stat(newFile); !os.IsNotExist(err) {
		t.Fatalf("il nuovo file deve essere rimosso")
	}
	assertTrue(t, cleanerFileExists(filepath.Join(trash, "Example - S01E01 - Title.mkv")), "il nuovo va nel trash")
	assertTrue(t, cleanerFileExists(existing), "l'esistente resta")
}

func TestFindsAndTrashesOnlyLowerResolutionDuplicates(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "archive")
	trash := filepath.Join(root, "trash")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Episodio 1: 1080p + vecchio 480p -> il 480p è un duplicato inferiore.
	hd := filepath.Join(archive, "Example - S01E01 - Pilot - [1080p][h265][AAC 5.1].mkv")
	old := filepath.Join(archive, "Example - S01E01 - Pilot - [480p][XviD][MP3].avi")
	cleanerWrite(t, hd, "new")
	cleanerWrite(t, old, "old")
	// Episodio 2: due 1080p senza tag lingua -> nessun candidato.
	s2a := filepath.Join(archive, "Example - S02E02 - Two - [1080p][h264][EAC3].mkv")
	s2b := filepath.Join(archive, "Example - S02E02 - Two - [1080p][h265][EAC3 5.1].mkv")
	cleanerWrite(t, s2a, "one")
	cleanerWrite(t, s2b, "two")

	cfg := DefaultConfig()
	cfg.CleanupUpgrades = true
	cfg.CleanupAction = "move"
	cfg.TrashPath = &trash

	noProtected := map[string]struct{}{}
	candidates, err := FindInferiorDuplicatesInDir("Example", archive, noProtected, "")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	assertEqual(t, len(candidates), 1)
	assertEqual(t, candidates[0].Episode, int64(1))

	// Un file protetto (torrent in sessione) non viene mai segnalato.
	protected := map[string]struct{}{old: {}}
	guarded, err := FindInferiorDuplicatesInDir("Example", archive, protected, "")
	if err != nil {
		t.Fatalf("find protected: %v", err)
	}
	assertEqual(t, len(guarded), 0)

	removed, err := CleanupInferiorDuplicatesInDir(&cfg, "Example", archive, noProtected)
	if err != nil {
		t.Fatalf("cleanup duplicates: %v", err)
	}
	assertEqual(t, removed, 1)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("il 480p deve essere rimosso")
	}
	assertTrue(t, cleanerFileExists(hd), "il 1080p resta")
	// Le due versioni 1080p senza lingua restano entrambe.
	assertTrue(t, cleanerFileExists(s2a), "il primo 1080p resta")
	assertTrue(t, cleanerFileExists(s2b), "il secondo 1080p resta")
}

func TestHardSourceUpgradeTrashesOldBelowScoreThreshold(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "archive")
	trash := filepath.Join(root, "trash")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Vecchio 1080p HDTV: score ~1100. Nuovo 1080p WEB-DL: ~1250. Con min_diff
	// alto lo score non basta, ma hdtv->webdl è un upgrade "forte".
	cleanerWrite(t, filepath.Join(archive, "Example.S01E01.1080p.HDTV.x264.mkv"), "old")
	newFile := filepath.Join(archive, "Example - S01E01 - Title - [1080p][webdl][h264].mkv")
	cleanerWrite(t, newFile, "new")

	cfg := DefaultConfig()
	cfg.CleanupUpgrades = true
	cfg.CleanupMinScoreDiff = 500
	cfg.TrashPath = &trash

	newScore := cleanerScore("Example - S01E01 - Title - [1080p][webdl][h264].mkv")
	removed, err := CleanupOldEpisode(&cfg, "Example", 1, 1, newScore, newFile, archive)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	assertEqual(t, removed, 1)
	if _, err := os.Stat(filepath.Join(archive, "Example.S01E01.1080p.HDTV.x264.mkv")); !os.IsNotExist(err) {
		t.Fatalf("il vecchio HDTV deve essere rimosso")
	}
}

func TestPrefersLanguageAtSameResolution(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "archive")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cleanerWrite(t, filepath.Join(archive, "Example - S01E01 - Pilot - [1080p][h264][EAC3][IT+EN].mkv"), "ita")
	cleanerWrite(t, filepath.Join(archive, "Example - S01E01 - Pilot - [1080p][h265][EAC3 5.1][EN].mkv"), "eng")

	none := map[string]struct{}{}
	// Preferenza ITA: il file EN a pari risoluzione è candidato.
	candidates, err := FindInferiorDuplicatesInDir("Example", archive, none, "ita")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	assertEqual(t, len(candidates), 1)
	assertTrue(t, strings.Contains(candidates[0].Path, "[EN]"), "il candidato deve essere la versione EN")
	// Senza preferenza lingua, nessun candidato a pari risoluzione.
	without, err := FindInferiorDuplicatesInDir("Example", archive, none, "")
	if err != nil {
		t.Fatalf("find senza lingua: %v", err)
	}
	assertEqual(t, len(without), 0)
}

func TestDiscardsNewReleaseWithoutPreferredLanguage(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "archive")
	trash := filepath.Join(root, "trash")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Esistente ITA 1080p; arriva un EN 1080p: va scartato il nuovo.
	cleanerWrite(t, filepath.Join(archive, "Example - S01E01 - Pilot - [1080p][h264][EAC3][IT+EN].mkv"), "ita")
	newFile := filepath.Join(archive, "Example - S01E01 - Pilot - [1080p][h265][EAC3 5.1][EN].mkv")
	cleanerWrite(t, newFile, "eng")

	cfg := DefaultConfig()
	cfg.CleanupUpgrades = true
	cfg.TrashPath = &trash

	newScore := cleanerScore("Example - S01E01 - Pilot - [1080p][h265][EAC3 5.1][EN].mkv")
	discarded, err := DiscardIfInferior(&cfg, "Example", 1, 1, newScore, newFile, archive)
	if err != nil {
		t.Fatalf("discard: %v", err)
	}
	assertTrue(t, discarded, "il nuovo EN va scartato")
	if _, err := os.Stat(newFile); !os.IsNotExist(err) {
		t.Fatalf("il nuovo EN va nel trash")
	}
	assertTrue(t, cleanerFileExists(filepath.Join(archive, "Example - S01E01 - Pilot - [1080p][h264][EAC3][IT+EN].mkv")), "l'esistente resta")
}

func TestLoweredResolutionSettingDoesNotMakeEqualNewReleaseInferior(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "archive")
	trash := filepath.Join(root, "trash")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Esistente e nuovo sono entrambi 1080p IT+EN: il nuovo non è inferiore.
	cleanerWrite(t, filepath.Join(archive, "Example - S01E01 - Pilot - [1080p][h265][AAC][IT+EN].mkv"), "old")
	newFile := filepath.Join(archive, "Example - S01E01 - Pilot - [WEB-DL][1080p][h264][AAC][IT+EN].mkv")
	cleanerWrite(t, newFile, "new")

	cfg := DefaultConfig()
	cfg.CleanupUpgrades = true
	cfg.CleanupMinScoreDiff = 50
	cfg.TrashPath = &trash
	// Impostazione utente che abbassa il peso del 1080p: se il confronto
	// mescola `score()` e `score_with_settings()` il nuovo 1080p sembra
	// "inferiore" e viene scartato per errore.
	cfg.Settings["score_res_1080p"] = "500"

	newQuality := ParseQuality("Example - S01E01 - Pilot - [WEB-DL][1080p][h264][AAC][IT+EN].mkv")
	newScore := newQuality.ScoreWithSettings(cfg.Settings)
	discarded, err := DiscardIfInferior(&cfg, "Example", 1, 1, newScore, newFile, archive)
	if err != nil {
		t.Fatalf("discard: %v", err)
	}
	assertTrue(t, !discarded, "il nuovo file non va scartato")
	assertTrue(t, cleanerFileExists(newFile), "il nuovo file resta")
}

func TestRemovesEmptiedSubdirectoriesAfterCleanup(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "archive")
	trash := filepath.Join(root, "trash")
	season := filepath.Join(archive, "Season 1")
	if err := os.MkdirAll(season, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cleanerWrite(t, filepath.Join(season, "Example - S01E01 - Pilot - [480p][XviD][MP3].avi"), "old")
	newFile := filepath.Join(archive, "Example - S01E01 - Pilot - [1080p][h265][AAC].mkv")
	cleanerWrite(t, newFile, "new")

	cfg := DefaultConfig()
	cfg.CleanupUpgrades = true
	cfg.TrashPath = &trash

	newScore := cleanerScore("Example - S01E01 - Pilot - [1080p][h265][AAC].mkv")
	removed, err := CleanupOldEpisode(&cfg, "Example", 1, 1, newScore, newFile, archive)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	assertEqual(t, removed, 1)
	if _, err := os.Stat(season); !os.IsNotExist(err) {
		t.Fatalf("la sottocartella svuotata va rimossa")
	}
	info, err := os.Stat(archive)
	assertTrue(t, err == nil && info.IsDir(), "la radice della serie resta")
}

func TestSweepStaleTempFiles(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "archive")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatal(err)
	}

	staleCopy := filepath.Join(archive, ".Example.S01E01.mkv.gextto-copy-12345")
	cleanerWrite(t, staleCopy, "stale copy data")
	freshCopy := filepath.Join(archive, ".Example.S01E02.mkv.gextto-copy-67890")
	cleanerWrite(t, freshCopy, "fresh copy data")

	stalePart := filepath.Join(archive, "Example.S01E03.mkv.gextto-part")
	cleanerWrite(t, stalePart, "stale part data")
	freshPart := filepath.Join(archive, "Example.S01E04.mkv.gextto-part")
	cleanerWrite(t, freshPart, "fresh part data")

	regularFile := filepath.Join(archive, "Example.S01E05.mkv")
	cleanerWrite(t, regularFile, "regular video")

	// Set modification time on stale files to 3 hours ago
	threeHoursAgo := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(staleCopy, threeHoursAgo, threeHoursAgo); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stalePart, threeHoursAgo, threeHoursAgo); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.ArchiveRoot = &archive

	cleaned := SweepStaleTempFiles(&cfg)
	if cleaned != 2 {
		t.Fatalf("expected 2 stale files cleaned, got %d", cleaned)
	}

	// Verify stale files were deleted
	if cleanerFileExists(staleCopy) {
		t.Errorf("stale copy %s should have been removed", staleCopy)
	}
	if cleanerFileExists(stalePart) {
		t.Errorf("stale part %s should have been removed", stalePart)
	}

	// Verify fresh files and regular files were preserved
	if !cleanerFileExists(freshCopy) {
		t.Errorf("fresh copy %s should have been preserved", freshCopy)
	}
	if !cleanerFileExists(freshPart) {
		t.Errorf("fresh part %s should have been preserved", freshPart)
	}
	if !cleanerFileExists(regularFile) {
		t.Errorf("regular file %s should have been preserved", regularFile)
	}
}
