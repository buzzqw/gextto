package gextto

import (
	"os"
	"runtime"
	"strings"
)

// system_probe.go reads what the machine is, so the guided tuning can propose
// values that fit it. It reads only facts (memory, CPU, volumes, filesystem
// type) and never writes anything: the proposal is built from these facts and
// is applied only after the user confirms it.

// defaultRamdiskDir is the RAM disk the tuning looks for: the shared memory
// mount every Linux system exposes and that the RAM disk feature already uses.
const defaultRamdiskDir = "/dev/shm"

// VolumeFacts is one volume the probe inspected: its path, size and free space.
// Exists is false when the path is empty or not a directory.
type VolumeFacts struct {
	Path   string
	Total  uint64
	Free   uint64
	Exists bool
}

// SystemFacts is everything the tuning reads from the machine.
type SystemFacts struct {
	MemTotal     uint64
	MemAvailable uint64
	NumCPU       int

	Download VolumeFacts
	Archive  VolumeFacts

	// DownloadNetwork/ArchiveNetwork mark a network filesystem (NFS/SMB).
	DownloadNetwork bool
	ArchiveNetwork  bool
	// SameDevice is true when the download folder and the library share a
	// filesystem, so hardlinks are possible.
	SameDevice bool

	// RamdiskDir is the RAM disk the tuning would use, empty when none is
	// available.
	RamdiskDir string
}

// ProbeSystem reads the machine facts used to propose a tuning. It never fails:
// an unavailable path is reported as not existing, and a missing reader simply
// leaves the field at zero.
func ProbeSystem(cfg *Config) SystemFacts {
	facts := SystemFacts{NumCPU: runtime.NumCPU()}
	facts.MemTotal, facts.MemAvailable = memory_info()

	archiveRoot := ""
	if cfg != nil {
		facts.Download = probeVolume(cfg.DownloadDir)
		if cfg.ArchiveRoot != nil {
			archiveRoot = strings.TrimSpace(*cfg.ArchiveRoot)
		}
	}
	facts.Archive = probeVolume(archiveRoot)

	if facts.Download.Exists && facts.Archive.Exists {
		facts.SameDevice = sameFilesystemDevice(facts.Download.Path, facts.Archive.Path)
		facts.DownloadNetwork = networkFilesystem(facts.Download.Path)
		facts.ArchiveNetwork = networkFilesystem(facts.Archive.Path)
	}
	facts.RamdiskDir = ramdiskCandidate()
	return facts
}

// probeVolume gathers the size and free space of one directory. A path that is
// not a readable directory is reported with Exists false and no sizes.
func probeVolume(path string) VolumeFacts {
	path = strings.TrimSpace(path)
	if path == "" {
		return VolumeFacts{}
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return VolumeFacts{Path: path}
	}
	total, free, ok := FilesystemSpace(path)
	if !ok {
		return VolumeFacts{Path: path, Exists: true}
	}
	return VolumeFacts{Path: path, Total: total, Free: free, Exists: true}
}

// ramdiskCandidate returns the RAM disk to use when one is mounted and writable,
// or "" when the machine has none.
func ramdiskCandidate() string {
	if DirectoryWritable(defaultRamdiskDir) {
		return defaultRamdiskDir
	}
	return ""
}
