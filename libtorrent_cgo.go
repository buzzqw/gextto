//go:build cgo

// Package gextto: thin cgo wrappers around the C ABI exposed by
// libtorrent_bridge.h. All logic lives in libtorrent.go; this file only
// marshals Go values across the C boundary. The C++ implementation is the
// package-root libtorrent_bridge.cpp, compiled by cgo.
package gextto

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -ltorrent-rasterbar -lssl -lcrypto -lstdc++
#cgo linux LDFLAGS: -Wl,-rpath,$ORIGIN/lib

#include <stdlib.h>
#include "libtorrent_bridge.h"

// Defined in libtorrent_bridge.cpp: releases freed glibc arena memory to the OS.
void gextto_trim_memory(void);
*/
import "C"

import (
	"strings"
	"unsafe"
)

// goStringFromBytes reads a NUL-terminated string out of a fixed-size C buffer.
func goStringFromBytes(buffer []byte) string {
	for i, value := range buffer {
		if value == 0 {
			return string(buffer[:i])
		}
	}
	return string(buffer)
}

// goStringFromChars reads a NUL-terminated string out of a fixed-size C array.
func goStringFromChars(buffer []C.char) string {
	for i, value := range buffer {
		if value == 0 {
			return C.GoStringN(&buffer[0], C.int(i))
		}
	}
	return C.GoStringN(&buffer[0], C.int(len(buffer)))
}

// errorBuffer returns a 512-byte scratch buffer for native error strings.
func errorBuffer() []byte { return make([]byte, 512) }

func cgoLtCreate(portMin, portMax uint16, downloadLimit, uploadLimit, activeDownloads, activeSeeds, activeLimit, connectionsLimit int32, dht, pex, lsd, upnp, natpmp uint8) (unsafe.Pointer, string) {
	error := errorBuffer()
	raw := C.gextto_lt_create(
		C.ushort(portMin), C.ushort(portMax),
		C.int(downloadLimit), C.int(uploadLimit),
		C.int(activeDownloads), C.int(activeSeeds), C.int(activeLimit), C.int(connectionsLimit),
		C.uchar(dht), C.uchar(pex), C.uchar(lsd), C.uchar(upnp), C.uchar(natpmp),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)),
	)
	return unsafe.Pointer(raw), goStringFromBytes(error)
}

func cgoLtDestroy(session unsafe.Pointer) {
	C.gextto_lt_destroy((*C.gextto_lt_session)(session))
}

// LibtorrentCompiled reports whether this binary includes the embedded
// libtorrent engine. True here: this file is compiled only with cgo.
func LibtorrentCompiled() bool { return true }

func cgoLtVersion() string {
	buffer := make([]byte, 128)
	C.gextto_lt_version((*C.char)(unsafe.Pointer(&buffer[0])), C.size_t(len(buffer)))
	return goStringFromBytes(buffer)
}

// cgoTrimMemory releases unused glibc arena memory back to the operating system.
func cgoTrimMemory() {
	C.gextto_trim_memory()
}

func cgoLtLoadIPFilter(session unsafe.Pointer, path string) (int32, int32, string) {
	sess := (*C.gextto_lt_session)(session)
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	error := errorBuffer()
	var rules C.int
	loaded := C.gextto_lt_load_ipfilter(sess, cpath, &rules, (*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(loaded), int32(rules), goStringFromBytes(error)
}

func cgoLtSaveTorrent(session unsafe.Pointer, hash, path string) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	error := errorBuffer()
	saved := C.gextto_lt_save_torrent(sess, chash, cpath, (*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(saved), goStringFromBytes(error)
}

func cgoLtApplySettings(session unsafe.Pointer, settings string) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	csettings := C.CString(settings)
	defer C.free(unsafe.Pointer(csettings))
	error := errorBuffer()
	applied := C.gextto_lt_apply_settings(sess, csettings, (*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(applied), goStringFromBytes(error)
}

func cgoLtAddFile(session unsafe.Pointer, torrentPath, savePath string) (int32, string, string) {
	sess := (*C.gextto_lt_session)(session)
	cTorrentPath := C.CString(torrentPath)
	defer C.free(unsafe.Pointer(cTorrentPath))
	cSavePath := C.CString(savePath)
	defer C.free(unsafe.Pointer(cSavePath))
	hash := make([]byte, 65)
	error := errorBuffer()
	added := C.gextto_lt_add_file(sess, cTorrentPath, cSavePath,
		(*C.char)(unsafe.Pointer(&hash[0])), C.size_t(len(hash)),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(added), goStringFromBytes(hash), goStringFromBytes(error)
}

func cgoLtAddEx(session unsafe.Pointer, magnet, savePath string, flags int32) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	cmagnet := C.CString(magnet)
	defer C.free(unsafe.Pointer(cmagnet))
	cSavePath := C.CString(savePath)
	defer C.free(unsafe.Pointer(cSavePath))
	error := errorBuffer()
	added := C.gextto_lt_add_ex(sess, cmagnet, cSavePath, C.int(flags),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(added), goStringFromBytes(error)
}

func cgoLtAddFileEx(session unsafe.Pointer, torrentPath, savePath string, flags int32) (int32, string, string) {
	sess := (*C.gextto_lt_session)(session)
	cTorrentPath := C.CString(torrentPath)
	defer C.free(unsafe.Pointer(cTorrentPath))
	cSavePath := C.CString(savePath)
	defer C.free(unsafe.Pointer(cSavePath))
	hash := make([]byte, 65)
	error := errorBuffer()
	added := C.gextto_lt_add_file_ex(sess, cTorrentPath, cSavePath, C.int(flags),
		(*C.char)(unsafe.Pointer(&hash[0])), C.size_t(len(hash)),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(added), goStringFromBytes(hash), goStringFromBytes(error)
}

func cgoLtTorrentCount(session unsafe.Pointer) uint32 {
	return uint32(C.gextto_lt_torrent_count((*C.gextto_lt_session)(session)))
}

func cgoLtStatuses(session unsafe.Pointer) []NativeTorrentStatus {
	sess := (*C.gextto_lt_session)(session)
	total := C.gextto_lt_statuses(sess, nil, 0)
	if total == 0 {
		return nil
	}
	buffer := make([]C.gextto_lt_status, int(total))
	received := int(C.gextto_lt_statuses(sess, &buffer[0], C.size_t(len(buffer))))
	result := make([]NativeTorrentStatus, received)
	for i := 0; i < received; i++ {
		status := buffer[i]
		result[i] = NativeTorrentStatus{
			Hash:            goStringFromChars(status.hash[:]),
			Name:            goStringFromChars(status.name[:]),
			SavePath:        goStringFromChars(status.save_path[:]),
			Progress:        float64(status.progress),
			State:           int32(status.state),
			Paused:          int32(status.paused),
			DownloadRate:    int32(status.download_rate),
			UploadRate:      int32(status.upload_rate),
			NumPeers:        int32(status.num_peers),
			NumSeeds:        int32(status.num_seeds),
			DownloadLimit:   int32(status.download_limit),
			UploadLimit:     int32(status.upload_limit),
			AllTimeUpload:   int64(status.all_time_upload),
			AllTimeDownload: int64(status.all_time_download),
			SeedingSeconds:  int64(status.seeding_seconds),
			QueuePosition:   int32(status.queue_position),
			HasMetadata:     int32(status.has_metadata),
			AutoManaged:     int32(status.auto_managed),
			TorrentVersion:  int32(status.torrent_version),
			TotalSize:       int64(status.total_size),
			TotalDone:       int64(status.total_done),

			Error:             goStringFromChars(status.error[:]),
			CurrentTracker:    goStringFromChars(status.current_tracker[:]),
			DownloadPayload:   int32(status.download_payload_rate),
			UploadPayload:     int32(status.upload_payload_rate),
			NumComplete:       int32(status.num_complete),
			NumIncomplete:     int32(status.num_incomplete),
			NumConnections:    int32(status.num_connections),
			ConnectCandidates: int32(status.connect_candidates),
			FinishedSeconds:   int64(status.finished_time),
			ActiveSeconds:     int64(status.active_time),
			IsSeeding:         int32(status.is_seeding),
			Sequential:        int32(status.sequential_download),
			SuperSeeding:      int32(status.super_seeding),
			UploadMode:        int32(status.upload_mode),
			ShareMode:         int32(status.share_mode),
			DistributedCopies: float32(status.distributed_copies),
		}
	}
	return result
}

func cgoLtEvents(session unsafe.Pointer) []NativeTorrentEvent {
	sess := (*C.gextto_lt_session)(session)
	buffer := make([]C.gextto_lt_event, 256)
	received := int(C.gextto_lt_events(sess, &buffer[0], C.size_t(len(buffer))))
	result := make([]NativeTorrentEvent, received)
	for i := 0; i < received; i++ {
		event := buffer[i]
		result[i] = NativeTorrentEvent{
			Kind:     int32(event.kind),
			Hash:     goStringFromChars(event.hash[:]),
			Name:     goStringFromChars(event.name[:]),
			SavePath: goStringFromChars(event.save_path[:]),
			Message:  goStringFromChars(event.message[:]),
		}
	}
	return result
}

func cgoLtPromoteMetadata(session unsafe.Pointer) {
	C.gextto_lt_promote_metadata((*C.gextto_lt_session)(session))
}

func cgoLtEnsureAutoManaged(session unsafe.Pointer) int {
	return int(C.gextto_lt_ensure_auto_managed((*C.gextto_lt_session)(session)))
}

func cgoLtAdjustQueue(session unsafe.Pointer, enabled, staticDownloads, minimum, maximum, staticSeeds, staticLimit, globalDownloadLimit int32) {
	C.gextto_lt_adjust_queue((*C.gextto_lt_session)(session), C.int(enabled), C.int(staticDownloads),
		C.int(minimum), C.int(maximum), C.int(staticSeeds), C.int(staticLimit), C.int(globalDownloadLimit))
}

func cgoLtPeers(session unsafe.Pointer, hash string) (int, []NativePeer, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	buffer := make([]C.gextto_lt_peer, 256)
	count := int(C.gextto_lt_peers(sess, chash, &buffer[0], C.size_t(len(buffer)),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error))))
	result := make([]NativePeer, count)
	for i := 0; i < count; i++ {
		peer := buffer[i]
		result[i] = NativePeer{
			Address:       goStringFromChars(peer.address[:]),
			Client:        goStringFromChars(peer.client[:]),
			DownloadRate:  int32(peer.download_rate),
			UploadRate:    int32(peer.upload_rate),
			NumPieces:     int32(peer.num_pieces),
			Seed:          int32(peer.seed),
			Flags:         int32(peer.flags),
			Progress:      float32(peer.progress),
			TotalUpload:   int64(peer.total_upload),
			TotalDownload: int64(peer.total_download),
		}
	}
	return count, result, goStringFromBytes(error)
}

func cgoLtTrackers(session unsafe.Pointer, hash string) (int, []NativeTracker, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	buffer := make([]C.gextto_lt_tracker, 256)
	count := int(C.gextto_lt_trackers(sess, chash, &buffer[0], C.size_t(len(buffer)),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error))))
	result := make([]NativeTracker, count)
	for i := 0; i < count; i++ {
		tracker := buffer[i]
		result[i] = NativeTracker{
			URL:              goStringFromChars(tracker.url[:]),
			Message:          goStringFromChars(tracker.message[:]),
			Tier:             int32(tracker.tier),
			Status:           int32(tracker.status),
			Fails:            int32(tracker.fails),
			NextAnnounce:     int32(tracker.next_announce),
			ScrapeIncomplete: int32(tracker.scrape_incomplete),
			ScrapeComplete:   int32(tracker.scrape_complete),
			ScrapeDownloaded: int32(tracker.scrape_downloaded),
			Verified:         int32(tracker.verified),
		}
	}
	return count, result, goStringFromBytes(error)
}

func cgoLtFiles(session unsafe.Pointer, hash string) (int, []NativeFile, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	buffer := make([]C.gextto_lt_file, 2048)
	count := int(C.gextto_lt_files(sess, chash, &buffer[0], C.size_t(len(buffer)),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error))))
	result := make([]NativeFile, count)
	for i := 0; i < count; i++ {
		file := buffer[i]
		result[i] = NativeFile{
			Path:       goStringFromChars(file.path[:]),
			Size:       int64(file.size),
			Downloaded: int64(file.downloaded),
			Priority:   int32(file.priority),
		}
	}
	return count, result, goStringFromBytes(error)
}

func cgoLtMoveStorage(session unsafe.Pointer, hash, destination string) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	cdestination := C.CString(destination)
	defer C.free(unsafe.Pointer(cdestination))
	error := errorBuffer()
	moved := C.gextto_lt_move_storage(sess, chash, cdestination,
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(moved), goStringFromBytes(error)
}

// cgoLtMovingStorage returns how many torrents are moving their files, as a
// map hash -> name; count is -1 on error.
func cgoLtMovingStorage(session unsafe.Pointer) (int32, map[string]string, string) {
	sess := (*C.gextto_lt_session)(session)
	names := make([]byte, 16384)
	error := errorBuffer()
	count := C.gextto_lt_moving_storage(sess, (*C.char)(unsafe.Pointer(&names[0])), C.size_t(len(names)),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	moving := map[string]string{}
	for _, line := range strings.Split(goStringFromBytes(names), "\n") {
		hash, name, found := strings.Cut(line, "\t")
		if found && hash != "" {
			moving[strings.ToLower(hash)] = name
		}
	}
	return int32(count), moving, goStringFromBytes(error)
}

func cgoLtAssociateStorage(session unsafe.Pointer, hash, destination string) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	cdestination := C.CString(destination)
	defer C.free(unsafe.Pointer(cdestination))
	error := errorBuffer()
	ok := C.gextto_lt_associate_storage(sess, chash, cdestination,
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtSetPaused(session unsafe.Pointer, hash string, paused int32) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_set_paused(sess, chash, C.int(paused),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtSetPin(session unsafe.Pointer, hash string, pinned int32) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_set_pin(sess, chash, C.int(pinned),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtSetSequential(session unsafe.Pointer, enabled int32) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	error := errorBuffer()
	ok := C.gextto_lt_set_sequential(sess, C.int(enabled),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtSetTorrentSequential(session unsafe.Pointer, hash string, enabled int32) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_set_torrent_sequential(sess, chash, C.int(enabled),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtQueueTop(session unsafe.Pointer, hash string) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_queue_top(sess, chash,
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtSetFirstLast(session unsafe.Pointer, hash string, enabled int32) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_set_first_last(sess, chash, C.int(enabled),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtSetFilePriorities(session unsafe.Pointer, hash string, priorities []int32) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	var pointer *C.int
	if len(priorities) > 0 {
		pointer = (*C.int)(unsafe.Pointer(&priorities[0]))
	}
	ok := C.gextto_lt_set_file_priorities(sess, chash, pointer, C.size_t(len(priorities)),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtAddWebSeeds(session unsafe.Pointer, hash, urls string, remove int32) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	curls := C.CString(urls)
	defer C.free(unsafe.Pointer(curls))
	error := errorBuffer()
	ok := C.gextto_lt_add_web_seeds(sess, chash, curls, C.int(remove),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtSetTrackers(session unsafe.Pointer, hash, tiered string) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	ctiered := C.CString(tiered)
	defer C.free(unsafe.Pointer(ctiered))
	error := errorBuffer()
	ok := C.gextto_lt_set_trackers(sess, chash, ctiered,
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtSetSuperSeeding(session unsafe.Pointer, hash string, enabled int32) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_set_super_seeding(sess, chash, C.int(enabled),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtRemove(session unsafe.Pointer, hash string, deleteFiles int32) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_remove(sess, chash, C.int(deleteFiles),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtForceRecheck(session unsafe.Pointer, hash string) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_force_recheck(sess, chash,
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtReannounce(session unsafe.Pointer, hash string) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_reannounce(sess, chash,
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtSetLimits(session unsafe.Pointer, hash string, downloadLimit, uploadLimit int32) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_set_limits(sess, chash, C.int(downloadLimit), C.int(uploadLimit),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtRestore(session unsafe.Pointer, stateDir string) (int, string) {
	sess := (*C.gextto_lt_session)(session)
	cStateDir := C.CString(stateDir)
	defer C.free(unsafe.Pointer(cStateDir))
	error := errorBuffer()
	restored := C.gextto_lt_restore(sess, cStateDir,
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int(restored), goStringFromBytes(error)
}

func cgoLtSaveResume(session unsafe.Pointer, stateDir string) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	cStateDir := C.CString(stateDir)
	defer C.free(unsafe.Pointer(cStateDir))
	error := errorBuffer()
	saved := C.gextto_lt_save_resume(sess, cStateDir,
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(saved), goStringFromBytes(error)
}

func cgoLtRequestResumeSave(session unsafe.Pointer, stateDir string) int32 {
	cStateDir := C.CString(stateDir)
	defer C.free(unsafe.Pointer(cStateDir))
	return int32(C.gextto_lt_request_resume_save((*C.gextto_lt_session)(session), cStateDir))
}

func cgoLtSetMaxConnections(session unsafe.Pointer, hash string, value int32) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_set_max_connections(sess, chash, C.int(value),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtSetMaxUploads(session unsafe.Pointer, hash string, value int32) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_set_max_uploads(sess, chash, C.int(value),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtSetUploadMode(session unsafe.Pointer, hash string, enabled int32) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_set_upload_mode(sess, chash, C.int(enabled),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtSetShareMode(session unsafe.Pointer, hash string, enabled int32) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_set_share_mode(sess, chash, C.int(enabled),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtSetTorrentFlag(session unsafe.Pointer, hash string, flag, enabled int32) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_set_torrent_flag(sess, chash, C.int(flag), C.int(enabled),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtScrapeTracker(session unsafe.Pointer, hash string) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_scrape_tracker(sess, chash,
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtForceDhtAnnounce(session unsafe.Pointer, hash string) (int32, string) {
	sess := (*C.gextto_lt_session)(session)
	chash := C.CString(hash)
	defer C.free(unsafe.Pointer(chash))
	error := errorBuffer()
	ok := C.gextto_lt_force_dht_announce(sess, chash,
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(error)
}

func cgoLtSessionStats(session unsafe.Pointer) (int32, string, string) {
	sess := (*C.gextto_lt_session)(session)
	output := make([]byte, 32<<10)
	error := errorBuffer()
	ok := C.gextto_lt_session_stats(sess,
		(*C.char)(unsafe.Pointer(&output[0])), C.size_t(len(output)),
		(*C.char)(unsafe.Pointer(&error[0])), C.size_t(len(error)))
	return int32(ok), goStringFromBytes(output), goStringFromBytes(error)
}
