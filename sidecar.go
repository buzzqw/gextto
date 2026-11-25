package gextto

import (
	"os"
	"path/filepath"
	"strings"
)

// sidecarExtensions is the single list of companion files that follow a video
// through the library. Keeping it here avoids the slightly different lists
// that had grown in the rename, move and discard paths.
var sidecarExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	".srt": true, ".sub": true, ".ass": true, ".ssa": true, ".vtt": true,
	".idx": true, ".sup": true, ".smi": true,
	".nfo": true, ".txt": true, ".xml": true,
}

func isSidecarName(name string) bool {
	return sidecarExtensions[strings.ToLower(filepath.Ext(name))]
}

// associatedSidecarRemainder recognises only a companion whose name starts
// with the complete video stem. The boundary matters: the subtitles of
// "Movie.mkv" must not sweep in "Movie 2.srt" from a shared folder.
func associatedSidecarRemainder(source, name string) (string, bool) {
	stem := fileStem(source)
	if stem == "" {
		return "", false
	}
	remainder, ok := strings.CutPrefix(name, stem)
	if !ok || remainder == "" {
		return "", false
	}
	switch remainder[0] {
	case '.', '-', '_':
		return remainder, true
	case ' ':
		// A space is a boundary only for the conventional " (copia 1)"
		// duplicate suffix, not for another title such as "Movie 2.srt".
		return remainder, strings.HasPrefix(remainder, " (")
	default:
		return "", false
	}
}

// sidecarPathsForVideo returns the regular companion files directly next to
// source that belong to it. Symlinks are never followed.
func sidecarPathsForVideo(source string) []string {
	dir := filepath.Dir(source)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		name := entry.Name()
		if !isSidecarName(name) {
			continue
		}
		if _, ok := associatedSidecarRemainder(source, name); !ok {
			continue
		}
		paths = append(paths, filepath.Join(dir, name))
	}
	return paths
}

// transferSidecarsToTarget moves or copies the companions of source next to
// target, preserving language/flag suffixes such as ".ita.forced.srt". An
// existing target companion belongs to the video being replaced, so the
// incoming one wins; the old file follows the configured trash/delete policy.
func transferSidecarsToTarget(source, target string, cfg *Config, copy bool) ([][2]string, error) {
	targetDir := filepath.Dir(target)
	targetStem := fileStem(target)
	if targetStem == "" {
		return [][2]string{}, nil
	}
	moved := make([][2]string, 0)
	for _, path := range sidecarPathsForVideo(source) {
		if SamePath(path, source) {
			continue
		}
		remainder, _ := associatedSidecarRemainder(source, filepath.Base(path))
		newPath := filepath.Join(targetDir, targetStem+remainder)
		if SamePath(path, newPath) {
			continue
		}
		if _, err := os.Stat(newPath); err == nil {
			if err := trashOrRemove(newPath, cfg); err != nil {
				return moved, err
			}
		} else if !os.IsNotExist(err) {
			return moved, err
		}
		var err error
		if copy {
			err = copyFileAtomically(path, newPath)
		} else {
			err = moveAcrossDevices(path, newPath)
		}
		if err != nil {
			return moved, err
		}
		moved = append(moved, [2]string{path, newPath})
	}
	return moved, nil
}
