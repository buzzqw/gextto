package gextto

// hardlink.go imports a download that keeps seeding into the library as a
// hardlink instead of a copy. Both names point at the same data, so the
// library file costs no extra space while the torrent seeds, and removing the
// download name at the end of the seed leaves the library file intact. When a
// hardlink is not possible (download and library on different filesystems, a
// RAM disk, a share that does not support links) the import falls back to the
// atomic copy used before.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/buzzqw/gextto/internal/logging"
)

// hardlinkSetting enables hardlinks for imports that keep the download seeding.
const hardlinkSetting = "hardlink_seeding"

func hardlinksEnabled(cfg *Config) bool {
	return cfg != nil && settingsBool(cfg, hardlinkSetting, true)
}

// linkOrCopyFile publishes target as a hardlink of source when enabled and
// possible, otherwise as an atomic copy. It reports whether a link was made.
func linkOrCopyFile(cfg *Config, source, target string) (bool, error) {
	if hardlinksEnabled(cfg) {
		err := hardlinkAtomically(source, target)
		if err == nil {
			return true, nil
		}
		noteHardlinkFallback(source, target, err)
	}
	return false, copyFileAtomically(source, target)
}

// hardlinkAtomically links source under a hidden sibling of target and
// publishes it with one rename, like copyFileAtomically, so a reader never
// sees a half-made entry and an existing target is replaced in one step.
func hardlinkAtomically(source, target string) error {
	source = filepath.Clean(source)
	target = filepath.Clean(target)
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	temporary := filepath.Join(
		parent,
		fmt.Sprintf(".%s.gextto-link-%s", filepath.Base(target), randomToken()),
	)
	if err := os.Link(source, temporary); err != nil {
		return err
	}
	err := os.Rename(temporary, target)
	// rename(2) is a no-op when both names already point at the same file,
	// which would leave the hidden name behind: always drop it.
	_ = os.Remove(temporary)
	return err
}

// hardlinkFallbackLogged remembers the filesystem pairs already reported, so
// the reason is logged once per daemon run instead of once per episode.
var hardlinkFallbackLogged sync.Map

func noteHardlinkFallback(source, target string, err error) {
	key := filepath.Dir(source) + "\x00" + filepath.Dir(target)
	if sourceDev, ok := deviceOf(source); ok {
		if targetDev, ok := deviceOf(filepath.Dir(target)); ok {
			key = fmt.Sprintf("%d>%d", sourceDev, targetDev)
		}
	}
	logging.Debug("hardlink not possible, copying instead",
		"source", source, "target", target, "error", err.Error())
	if _, seen := hardlinkFallbackLogged.LoadOrStore(key, true); seen {
		return
	}
	reason := "the library folder does not accept hardlinks"
	if errors.Is(err, syscall.EXDEV) {
		reason = "downloads and library are on different filesystems"
	}
	logging.Info(fmt.Sprintf("🔗 Hardlink not possible (%s): files that keep seeding are copied into %s, using space twice until the seed ends",
		reason, filepath.Dir(target)))
}

func deviceOf(path string) (uint64, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(stat.Dev), true //nolint:unconvert // Dev is int32 on some platforms
}
