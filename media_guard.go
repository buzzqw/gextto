package gextto

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// hasVideoExtension reports whether path names a video container, reusing the
// same extension set as the archive scanner (postprocess.go).
func hasVideoExtension(path string) bool {
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	return videoExtensions[extension]
}

// looksLikeVideoFile recognises the magic bytes of the common containers. It is
// deliberately permissive: it only needs to reject preallocated zero-filled or
// truncated files, not to identify the codec.
func looksLikeVideoFile(header []byte) bool {
	if len(header) < 12 {
		return false
	}
	switch {
	case bytes.HasPrefix(header, []byte{0x1A, 0x45, 0xDF, 0xA3}): // Matroska / WebM
		return true
	case bytes.Equal(header[4:8], []byte("ftyp")): // MP4 / MOV
		return true
	case bytes.HasPrefix(header, []byte("RIFF")) && bytes.Equal(header[8:12], []byte("AVI ")): // AVI
		return true
	case bytes.HasPrefix(header, []byte("OggS")): // Ogg
		return true
	case bytes.HasPrefix(header, []byte("FLV")): // FLV
		return true
	case bytes.HasPrefix(header, []byte{0x30, 0x26, 0xB2, 0x75}): // ASF / WMV
		return true
	case bytes.HasPrefix(header, []byte{0x00, 0x00, 0x01, 0xBA}): // MPEG-PS
		return true
	case bytes.HasPrefix(header, []byte{0x00, 0x00, 0x01, 0xB3}): // MPEG video ES
		return true
	case header[0] == 0x47: // MPEG-TS packet sync byte
		return true
	default:
		return false
	}
}

// validateCompletedFile refuses obviously broken downloads before they are
// renamed and recorded as archived: an empty file, an all-zero (preallocated
// but never written) file, or a video file whose container magic is missing.
// Non-video files (subtitles, archives) are accepted as-is.
func validateCompletedFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	if info.Size() <= 0 {
		return fmt.Errorf("%s is empty", path)
	}
	if !hasVideoExtension(path) {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	header := make([]byte, 65536)
	read, err := file.Read(header)
	if err != nil && read == 0 {
		return fmt.Errorf("read %s: %w", path, err)
	}
	header = header[:read]
	if len(header) == 0 {
		return fmt.Errorf("%s has no content", path)
	}
	if allZero(header) {
		return fmt.Errorf("%s is zero-filled (preallocated but never written)", path)
	}
	if !looksLikeVideoFile(header) {
		return fmt.Errorf("%s is not a recognised video container", path)
	}
	return nil
}

func allZero(data []byte) bool {
	for _, value := range data {
		if value != 0 {
			return false
		}
	}
	return true
}

// quarantineCorruptFile moves a file that failed validation out of the way so it
// cannot be picked up again. It returns the destination, or "" when the file
// could not be moved.
func quarantineCorruptFile(path string, cfg *Config) string {
	if cfg != nil && cfg.TrashPath != nil && strings.TrimSpace(*cfg.TrashPath) != "" {
		if destination, err := MoveToTrash(path, *cfg.TrashPath); err == nil {
			return destination
		}
	}
	fallback := path + ".corrupt"
	if err := os.Rename(path, fallback); err == nil {
		return fallback
	}
	return ""
}

// fileSuspiciouslyEmpty reports whether a completed file is missing, empty or
// zero-filled in its first 64 KiB. Used at startup to detect a torrent whose
// fastresume claims completion while the data on disk is not really there.
func fileSuspiciouslyEmpty(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return true
	}
	file, err := os.Open(path)
	if err != nil {
		return true
	}
	defer file.Close()
	header := make([]byte, 65536)
	read, _ := file.Read(header)
	if read == 0 {
		return true
	}
	return allZero(header[:read])
}
