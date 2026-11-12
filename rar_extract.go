package gextto

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/rarstore"
)

// A release can arrive packed: the video split in a set of RAR volumes
// (name.part01.rar… or name.rar, name.r00…). Before the import looks for the
// video, the stored (uncompressed) archives of the download folder are
// extracted next to them; the volumes stay, so the torrent keeps seeding.

// extractedMarker names the file that records an extraction already done,
// so a completion seen twice does not extract again.
const extractedMarker = ".gextto-extracted"

// extractReleaseArchives extracts the video files of the RAR sets in dir
// when dir holds no video of its own. It returns the files written.
func extractReleaseArchives(dir, label string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil, nil
	}
	if hasLooseVideo(dir) {
		return nil, nil
	}
	firsts, err := rarstore.FirstVolumes(dir)
	if err != nil || len(firsts) == 0 {
		return nil, err
	}
	var written []string
	var failures []string
	for _, first := range firsts {
		entries, err := rarstore.List(first)
		if err != nil {
			failures = append(failures, archiveFailure(first, err))
			continue
		}
		for _, entry := range entries {
			base := filepath.Base(entry.Name)
			if !isVideoExtension(base) || strings.Contains(strings.ToLower(entry.Name), "sample") {
				continue
			}
			target := filepath.Join(dir, base)
			if existing, err := os.Stat(target); err == nil && existing.Size() == entry.Size {
				written = append(written, target)
				continue
			}
			if free := FreeSpaceBytes(dir); free != nil && *free < uint64(entry.Size) {
				failures = append(failures, fmt.Sprintf("%s: not enough free space (%s needed)", base, logging.HumanBytesI64(entry.Size)))
				continue
			}
			if err := rarstore.Extract(entry, target); err != nil {
				failures = append(failures, archiveFailure(first, err))
				continue
			}
			written = append(written, target)
		}
	}
	if len(written) > 0 {
		_ = os.WriteFile(filepath.Join(dir, extractedMarker), []byte(strings.Join(written, "\n")+"\n"), 0o644)
		logging.Info(fmt.Sprintf("📦 %s arrived packed in RAR archives: %s extracted for the library; the archives stay for seeding", label, countLabel(int64(len(written)), "video", "videos")))
	}
	if len(failures) > 0 {
		logging.Warn(fmt.Sprintf("📦 %s arrived packed in RAR archives that could not be extracted: %s", label, strings.Join(failures, "; ")))
		if len(written) == 0 {
			return nil, errors.New(failures[0])
		}
	}
	return written, nil
}

// archiveFailure explains why an archive could not be extracted.
func archiveFailure(first string, err error) string {
	switch {
	case errors.Is(err, rarstore.ErrCompressed):
		return filepath.Base(first) + ": the archive is compressed, only uncompressed archives can be extracted"
	case errors.Is(err, rarstore.ErrEncrypted):
		return filepath.Base(first) + ": the archive is password-protected"
	case errors.Is(err, rarstore.ErrMissingPart):
		return filepath.Base(first) + ": a volume of the archive is missing"
	case errors.Is(err, rarstore.ErrCRC):
		return filepath.Base(first) + ": the extracted video does not match the archive checksum"
	default:
		return filepath.Base(first) + ": " + err.Error()
	}
}

// hasLooseVideo reports a video file (not a sample) directly in dir or one
// level below it.
func hasLooseVideo(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipDir
		}
		if d.IsDir() {
			if rel, _ := filepath.Rel(dir, path); strings.Count(rel, string(filepath.Separator)) >= 1 {
				return filepath.SkipDir
			}
			return nil
		}
		if isVideoExtension(d.Name()) && !strings.Contains(strings.ToLower(d.Name()), "sample") {
			found = true
		}
		return nil
	})
	return found
}
