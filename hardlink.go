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
	"strings"
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
		reason = hardlinkCrossDeviceReason(source, target)
	}
	logging.Info(fmt.Sprintf("🔗 Hardlink not possible (%s): files that keep seeding are copied into %s, using space twice until the seed ends",
		reason, filepath.Dir(target)))
}

// hardlinkCrossDeviceReason explains an EXDEV from os.Link. Two folders can sit
// on the same NAS and still be separate NFS mounts: the kernel refuses a link
// across mount points even when the server exports one filesystem, and the
// generic "different filesystems" is then misleading. When both paths are NFS
// mounts of the same host, say so and name the fix (a single mount of the
// common parent, or the library inside the download export).
func hardlinkCrossDeviceReason(source, target string) string {
	sourceMount, sourceOK := mountInfoFor(source)
	targetMount, targetOK := mountInfoFor(target)
	if sourceOK && targetOK && isNFS(sourceMount.fsType) && isNFS(targetMount.fsType) {
		sourceHost, sourceExport, sourceNFS := splitNFSSource(sourceMount.source)
		targetHost, _, targetNFS := splitNFSSource(targetMount.source)
		if sourceNFS && targetNFS && sourceHost == targetHost && sourceExport != "" {
			return fmt.Sprintf("both paths are on the NFS server %s but in separate mounts (%s and %s); a hardlink cannot cross mount points — mount a common parent (for example %s:/volume1) or keep the library inside the download export",
				sourceHost, sourceMount.point, targetMount.point, sourceHost)
		}
	}
	return "downloads and library are on different filesystems"
}

// mountInfo is the piece of /proc/self/mountinfo a path belongs to.
type mountInfo struct {
	point  string
	fsType string
	source string
}

// mountInfoFor returns the longest mount point containing path.
func mountInfoFor(path string) (mountInfo, bool) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return mountInfo{}, false
	}
	if absolute, absErr := filepath.Abs(path); absErr == nil {
		path = absolute
	}
	path = filepath.Clean(path)
	var best mountInfo
	bestLen := -1
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 7 {
			continue
		}
		separator := -1
		for index, field := range fields {
			if field == "-" {
				separator = index
				break
			}
		}
		if separator < 0 || separator+2 >= len(fields) {
			continue
		}
		point := unescapeMountField(fields[4])
		if point != "/" && point != path && !strings.HasPrefix(path, point+"/") {
			continue
		}
		if len(point) > bestLen {
			best = mountInfo{point: point, fsType: fields[separator+1], source: fields[separator+2]}
			bestLen = len(point)
		}
	}
	return best, bestLen >= 0
}

// unescapeMountField decodes the octal escapes of /proc/self/mountinfo.
func unescapeMountField(field string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return replacer.Replace(field)
}

func isNFS(fsType string) bool {
	return fsType == "nfs" || fsType == "nfs4"
}

// splitNFSSource separates host and export in "host:/export".
func splitNFSSource(source string) (host, export string, ok bool) {
	index := strings.IndexByte(source, ':')
	if index <= 0 || index == len(source)-1 {
		return "", "", false
	}
	return source[:index], source[index+1:], true
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
