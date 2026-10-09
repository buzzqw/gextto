//go:build !cgo

package gextto

// libtorrent_nocgo.go: stubs for a build without cgo, which is the default.
//
// Gextto's default torrent engine is gx-torrent, a pure-Go daemon; the embedded
// libtorrent engine is opt-in. In a build without cgo every bridge call fails
// with a clear error instead of failing to compile. The real implementation is
// in libtorrent_cgo.go, compiled only with `//go:build cgo` (i.e. when the build
// sets CGO_ENABLED=1 and libtorrent-rasterbar is available).

import (
	"errors"
	"unsafe"
)

// ErrLibtorrentNotCompiled is returned by the embedded engine when this binary
// was built without libtorrent support.
var ErrLibtorrentNotCompiled = errors.New("libtorrent non incluso in questa build (compila con GEXTTO_LIBTORRENT=1 / make build-libtorrent)")

func ltUnavailable() string { return ErrLibtorrentNotCompiled.Error() }

// LibtorrentCompiled reports whether this binary includes the embedded
// libtorrent engine. It is false for the default (pure-Go) build.
func LibtorrentCompiled() bool { return false }

func cgoLtCreate(portMin, portMax uint16, downloadLimit, uploadLimit, activeDownloads, activeSeeds, activeLimit, connectionsLimit int32, dht, pex, lsd, upnp, natpmp uint8) (unsafe.Pointer, string) {
	return nil, ltUnavailable()
}

func cgoLtDestroy(session unsafe.Pointer) {}

func cgoLtVersion() string { return "non incluso" }

func cgoTrimMemory() {}

func cgoLtLoadIPFilter(session unsafe.Pointer, path string) (int32, int32, string) {
	return 0, 0, ltUnavailable()
}

func cgoLtSaveTorrent(session unsafe.Pointer, hash, path string) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtApplySettings(session unsafe.Pointer, settings string) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtAddFile(session unsafe.Pointer, torrentPath, savePath string) (int32, string, string) {
	return 0, "", ltUnavailable()
}

func cgoLtAddEx(session unsafe.Pointer, magnet, savePath string, flags int32) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtAddFileEx(session unsafe.Pointer, torrentPath, savePath string, flags int32) (int32, string, string) {
	return 0, "", ltUnavailable()
}

func cgoLtTorrentCount(session unsafe.Pointer) uint32 { return 0 }

func cgoLtStatuses(session unsafe.Pointer) []NativeTorrentStatus { return nil }

func cgoLtEvents(session unsafe.Pointer) []NativeTorrentEvent { return nil }

func cgoLtPromoteMetadata(session unsafe.Pointer) {}

func cgoLtEnsureAutoManaged(session unsafe.Pointer) int { return 0 }

func cgoLtAdjustQueue(session unsafe.Pointer, enabled, staticDownloads, minimum, maximum, staticSeeds, staticLimit, globalDownloadLimit int32) {
}

func cgoLtPeers(session unsafe.Pointer, hash string) (int, []NativePeer, string) {
	return 0, nil, ltUnavailable()
}

func cgoLtTrackers(session unsafe.Pointer, hash string) (int, []NativeTracker, string) {
	return 0, nil, ltUnavailable()
}

func cgoLtFiles(session unsafe.Pointer, hash string) (int, []NativeFile, string) {
	return 0, nil, ltUnavailable()
}

func cgoLtMoveStorage(session unsafe.Pointer, hash, destination string) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtMovingStorage(session unsafe.Pointer) (int32, map[string]string, string) {
	return 0, nil, ltUnavailable()
}

func cgoLtAssociateStorage(session unsafe.Pointer, hash, destination string) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtSetPaused(session unsafe.Pointer, hash string, paused int32) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtSetPin(session unsafe.Pointer, hash string, pinned int32) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtSetSequential(session unsafe.Pointer, enabled int32) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtSetTorrentSequential(session unsafe.Pointer, hash string, enabled int32) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtQueueTop(session unsafe.Pointer, hash string) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtSetFirstLast(session unsafe.Pointer, hash string, enabled int32) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtSetFilePriorities(session unsafe.Pointer, hash string, priorities []int32) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtAddWebSeeds(session unsafe.Pointer, hash, urls string, remove int32) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtSetTrackers(session unsafe.Pointer, hash, tiered string) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtSetSuperSeeding(session unsafe.Pointer, hash string, enabled int32) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtRemove(session unsafe.Pointer, hash string, deleteFiles int32) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtForceRecheck(session unsafe.Pointer, hash string) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtReannounce(session unsafe.Pointer, hash string) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtSetLimits(session unsafe.Pointer, hash string, downloadLimit, uploadLimit int32) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtRestore(session unsafe.Pointer, stateDir string) (int, string) {
	return 0, ltUnavailable()
}

func cgoLtSaveResume(session unsafe.Pointer, stateDir string) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtRequestResumeSave(session unsafe.Pointer, stateDir string) int32 { return 0 }

func cgoLtSetMaxConnections(session unsafe.Pointer, hash string, value int32) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtSetMaxUploads(session unsafe.Pointer, hash string, value int32) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtSetUploadMode(session unsafe.Pointer, hash string, enabled int32) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtSetShareMode(session unsafe.Pointer, hash string, enabled int32) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtSetTorrentFlag(session unsafe.Pointer, hash string, flag, enabled int32) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtScrapeTracker(session unsafe.Pointer, hash string) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtForceDhtAnnounce(session unsafe.Pointer, hash string) (int32, string) {
	return 0, ltUnavailable()
}

func cgoLtSessionStats(session unsafe.Pointer) (int32, string, string) {
	return 0, "", ltUnavailable()
}
