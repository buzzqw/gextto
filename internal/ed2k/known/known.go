// Package known persists the metadata of shared files so a restart does not
// re-hash the whole library. The file is written in the eMule "known.met"
// layout (header 0x0E, or 0x0F when a file is larger than 4 GiB), so the same
// data can be read by eMule and aMule.
package known

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"golang.org/x/crypto/md4"

	"github.com/buzzqw/gextto/internal/ed2k"
)

// partSize is the eD2k part size (EMBLOCKSIZE).
const partSize = 9_728_000

const (
	headerMD4   = 0x0E
	headerMD4Hi = 0x0F

	// Bounds on untrusted input.
	maxEntries = 1 << 17
	maxParts   = 1 << 17
	maxTags    = 4096
	maxSize    = 1 << 34
)

// PartHashCount is how many part hashes eMule stores for a file: none below
// one part, otherwise size/partSize+1 (an exact multiple gets an extra empty
// part hash).
func PartHashCount(size int64) int {
	if size < partSize {
		return 0
	}
	return int(size/partSize) + 1
}

// PartsFor normalises a hasher's part list to the eMule hashset: no hashes for
// a file smaller than one part, and the MD4 of the empty part appended when
// the size is an exact multiple of the part size.
func PartsFor(size int64, parts []ed2k.ID) []ed2k.ID {
	out := append([]ed2k.ID(nil), parts...)
	if size >= partSize && size%partSize == 0 && len(out) == int(size/partSize) {
		var empty ed2k.ID
		copy(empty[:], md4.New().Sum(nil))
		out = append(out, empty)
	}
	want := PartHashCount(size)
	if len(out) > want {
		out = out[:want]
	}
	return out
}

// Entry is one shared file: its hashes, size, name and modification time.
type Entry struct {
	Hash  ed2k.ID
	Parts []ed2k.ID
	Size  int64
	Name  string
	MTime time.Time
}

// Load reads a known.met. A missing file returns an empty list, so a first
// start is not an error.
func Load(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("known: %w", err)
	}
	entries, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("known: %s: %w", path, err)
	}
	return entries, nil
}

// Save writes entries atomically in the eMule known.met layout.
func Save(path string, entries []Entry) error {
	data := encode(entries)
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("known: %w", err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("known: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("known: %w", err)
	}
	return nil
}

func encode(entries []Entry) []byte {
	large := false
	for _, entry := range entries {
		if entry.Size > math.MaxUint32 {
			large = true
		}
	}
	valid := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if entry.Hash == (ed2k.ID{}) || entry.Size <= 0 || entry.Size > maxSize || entry.Name == "" {
			continue
		}
		if PartHashCount(entry.Size) > 0 && len(entry.Parts) == 0 {
			continue
		}
		valid = append(valid, entry)
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].Name < valid[j].Name })

	out := make([]byte, 0, 64+len(valid)*32)
	if large {
		out = append(out, headerMD4Hi)
	} else {
		out = append(out, headerMD4)
	}
	out = appendUint32(out, uint32(len(valid)))
	now := uint32(time.Now().Unix())
	for _, entry := range valid {
		mtime := entry.MTime.Unix()
		if mtime < 0 || mtime > math.MaxUint32 {
			mtime = 0
		}
		parts := PartsFor(entry.Size, entry.Parts)
		out = appendUint32(out, uint32(mtime))
		out = append(out, entry.Hash[:]...)
		out = appendUint16(out, uint16(len(parts)))
		for _, part := range parts {
			out = append(out, part[:]...)
		}
		tags := []ed2k.Tag{
			{Name: ed2k.TagFileName, Type: ed2k.TagString, Str: entry.Name},
			{Name: ed2k.TagLastShared, Type: ed2k.TagUint32, Int: uint64(now)},
		}
		if entry.Size > math.MaxUint32 {
			tags = append(tags, ed2k.Tag{Name: ed2k.TagFileSize, Type: ed2k.TagUint64, Int: uint64(entry.Size)})
		} else {
			tags = append(tags, ed2k.Tag{Name: ed2k.TagFileSize, Type: ed2k.TagUint32, Int: uint64(entry.Size)})
		}
		out = appendUint32(out, uint32(len(tags)))
		out = append(out, ed2k.WriteTagList(tags, binary.LittleEndian)...)
	}
	return out
}

func parse(data []byte) ([]Entry, error) {
	if len(data) < 5 {
		return nil, errors.New("truncated header")
	}
	if data[0] != headerMD4 && data[0] != headerMD4Hi {
		return nil, fmt.Errorf("unsupported header 0x%02x", data[0])
	}
	pos := 1
	count, ok := readUint32(data, &pos)
	if !ok || count > maxEntries {
		return nil, errors.New("invalid entry count")
	}
	entries := make([]Entry, 0, count)
	for i := uint32(0); i < count; i++ {
		mtime, ok := readUint32(data, &pos)
		if !ok {
			return nil, errors.New("truncated modification time")
		}
		hash, ok := readID(data, &pos)
		if !ok {
			return nil, errors.New("truncated file hash")
		}
		partsCount, ok := readUint16(data, &pos)
		if !ok || int(partsCount) > maxParts {
			return nil, errors.New("invalid part count")
		}
		if len(data)-pos < int(partsCount)*16 {
			return nil, errors.New("truncated part hashes")
		}
		parts := make([]ed2k.ID, partsCount)
		for j := range parts {
			copy(parts[j][:], data[pos:pos+16])
			pos += 16
		}
		tagCount, ok := readUint32(data, &pos)
		if !ok || tagCount > maxTags {
			return nil, errors.New("invalid tag count")
		}
		tags, consumed, err := ed2k.ReadTagList(data[pos:], int(tagCount), binary.LittleEndian)
		if err != nil {
			return nil, fmt.Errorf("tags: %w", err)
		}
		pos += consumed
		entry := Entry{Hash: hash, Parts: parts, Name: tagString(tags, ed2k.TagFileName), Size: tagSize(tags)}
		entry.MTime = time.Unix(int64(mtime), 0).UTC()
		if entry.Name == "" || entry.Size <= 0 || entry.Size > maxSize {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func tagString(tags []ed2k.Tag, name uint8) string {
	for _, tag := range tags {
		if tag.Name == name && tag.Type == ed2k.TagString {
			return tag.Str
		}
	}
	return ""
}

func tagSize(tags []ed2k.Tag) int64 {
	var low, high uint64
	var found bool
	for _, tag := range tags {
		switch tag.Name {
		case ed2k.TagFileSize:
			switch tag.Type {
			case ed2k.TagUint64:
				// A 64-bit size already holds the whole value.
				return clampSize(tag.Int)
			case ed2k.TagUint32:
				low, found = tag.Int, true
			}
		case ed2k.TagFileSizeHigh:
			if tag.Type == ed2k.TagUint32 {
				high = tag.Int
			}
		}
	}
	if !found {
		return 0
	}
	return clampSize((high << 32) | low)
}

func clampSize(size uint64) int64 {
	if size > maxSize {
		return 0
	}
	return int64(size)
}

func appendUint16(out []byte, value uint16) []byte {
	var raw [2]byte
	binary.LittleEndian.PutUint16(raw[:], value)
	return append(out, raw[:]...)
}

func appendUint32(out []byte, value uint32) []byte {
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], value)
	return append(out, raw[:]...)
}

func readUint16(data []byte, pos *int) (uint16, bool) {
	if len(data)-*pos < 2 {
		return 0, false
	}
	value := binary.LittleEndian.Uint16(data[*pos:])
	*pos += 2
	return value, true
}

func readUint32(data []byte, pos *int) (uint32, bool) {
	if len(data)-*pos < 4 {
		return 0, false
	}
	value := binary.LittleEndian.Uint32(data[*pos:])
	*pos += 4
	return value, true
}

func readID(data []byte, pos *int) (ed2k.ID, bool) {
	var id ed2k.ID
	if len(data)-*pos < 16 {
		return id, false
	}
	copy(id[:], data[*pos:*pos+16])
	*pos += 16
	return id, true
}
