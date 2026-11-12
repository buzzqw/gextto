// Package rarstore extracts files from RAR archives written in "store" mode
// (no compression), the form video releases split into volumes use: the
// payload is copied out of the volumes as it is, with its CRC checked. RAR 4
// and RAR 5, single and multi-volume ("name.part01.rar" or "name.rar",
// "name.r00", …). Compressed or encrypted entries are reported, never guessed.
package rarstore

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Errors a caller can tell apart.
var (
	ErrNotRAR      = errors.New("not a RAR archive")
	ErrCompressed  = errors.New("the archive is compressed: only uncompressed (stored) RAR archives can be extracted")
	ErrEncrypted   = errors.New("the archive is password-protected")
	ErrMissingPart = errors.New("a volume of the archive is missing")
	ErrCRC         = errors.New("the extracted data does not match the archive checksum")
)

var (
	sigRAR4 = []byte("Rar!\x1a\x07\x00")
	sigRAR5 = []byte("Rar!\x1a\x07\x01\x00")
)

// Entry is one file stored in the archive.
type Entry struct {
	Name string
	Size int64
	// parts are the byte ranges of the entry in each volume, in order.
	parts []part
	// crc is the checksum of the whole file when the archive gives one.
	crc    uint32
	hasCRC bool
}

type part struct {
	volume string
	offset int64
	size   int64
	// crc is the checksum this volume's header carries for the entry.
	crc    uint32
	hasCRC bool
}

// partRE matches the "name.partNN.rar" volume naming.
var partRE = regexp.MustCompile(`(?i)^(.*)\.part(\d+)\.rar$`)

// oldRE matches the "name.rNN"/"name.sNN" continuation volumes.
var oldRE = regexp.MustCompile(`(?i)^(.*)\.([r-z])(\d\d)$`)

// FirstVolumes lists the first volume of every archive in dir.
func FirstVolumes(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if m := partRE.FindStringSubmatch(name); m != nil {
			if n, _ := strconv.Atoi(m[2]); n == 1 {
				out = append(out, filepath.Join(dir, name))
			}
			continue
		}
		if strings.EqualFold(filepath.Ext(name), ".rar") {
			out = append(out, filepath.Join(dir, name))
		}
	}
	sort.Strings(out)
	return out, nil
}

// Volumes returns the volume files of the archive whose first volume is
// first, in order, as far as they exist on disk.
func Volumes(first string) []string {
	dir, base := filepath.Dir(first), filepath.Base(first)
	out := []string{first}
	if m := partRE.FindStringSubmatch(base); m != nil {
		width := len(m[2])
		for n := 2; ; n++ {
			name := fmt.Sprintf("%s.part%0*d.rar", m[1], width, n)
			path := filepath.Join(dir, name)
			if !exists(path) {
				// Same name with another case of "part"/"rar".
				if alt := findFold(dir, name); alt != "" {
					path = alt
				} else {
					return out
				}
			}
			out = append(out, path)
		}
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	for n := 0; n < 26*100; n++ {
		letter := byte('r' + n/100)
		if letter > 'z' {
			break
		}
		name := fmt.Sprintf("%s.%c%02d", stem, letter, n%100)
		path := filepath.Join(dir, name)
		if !exists(path) {
			if alt := findFold(dir, name); alt != "" {
				path = alt
			} else {
				break
			}
		}
		out = append(out, path)
	}
	return out
}

func exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// findFold finds name in dir ignoring case.
func findFold(dir, name string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), name) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// IsContinuation reports whether name is a later volume of a set (part2 and
// up, or .r00…), which callers should not open as a first volume.
func IsContinuation(name string) bool {
	if m := partRE.FindStringSubmatch(filepath.Base(name)); m != nil {
		n, _ := strconv.Atoi(m[2])
		return n > 1
	}
	return oldRE.MatchString(filepath.Base(name))
}

// List reads the headers of every volume of the archive and returns its
// entries. Directories are left out.
func List(first string) ([]*Entry, error) {
	volumes := Volumes(first)
	byName := map[string]*Entry{}
	var order []*Entry
	var open *Entry // the entry continuing into the next volume
	for index, volume := range volumes {
		chunks, more, err := readVolume(volume)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(volume), err)
		}
		for _, c := range chunks {
			if c.continued {
				if open == nil || open.Name != c.name {
					return nil, fmt.Errorf("%s: %w", filepath.Base(volume), ErrMissingPart)
				}
			} else {
				open = &Entry{Name: c.name}
				if _, dup := byName[c.name]; !dup {
					byName[c.name] = open
					order = append(order, open)
				}
			}
			open.parts = append(open.parts, part{volume: volume, offset: c.offset, size: c.size, crc: c.crc, hasCRC: c.hasCRC})
			open.Size += c.size
			if !c.continues {
				open.crc, open.hasCRC = c.crc, c.hasCRC
				open = nil
			}
		}
		if open != nil && index == len(volumes)-1 {
			return nil, fmt.Errorf("%s: %w", filepath.Base(open.Name), ErrMissingPart)
		}
		if !more && open == nil {
			break
		}
	}
	return order, nil
}

// Extract copies one entry to target, checking the checksum; a partial file
// is never left at target.
func Extract(entry *Entry, target string) error {
	tmp := target + ".extracting"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	whole := crc32.NewIEEE()
	buffered := bufio.NewWriterSize(out, 1<<20)
	for i, p := range entry.parts {
		f, err := os.Open(p.volume)
		if err != nil {
			out.Close()
			return err
		}
		partCRC := crc32.NewIEEE()
		copied, err := io.Copy(io.MultiWriter(buffered, whole, partCRC), io.NewSectionReader(f, p.offset, p.size))
		f.Close()
		if err == nil && copied != p.size {
			err = fmt.Errorf("%s ends before the data of the archive: %w", filepath.Base(p.volume), ErrMissingPart)
		}
		if err != nil {
			out.Close()
			return err
		}
		last := i == len(entry.parts)-1
		// A volume that the file goes on from carries the checksum of its own
		// slice; the last carries the whole file's (or, in some writers, its
		// own slice's too).
		if p.hasCRC && !last && partCRC.Sum32() != p.crc {
			out.Close()
			return ErrCRC
		}
		if p.hasCRC && last && p.crc != whole.Sum32() && p.crc != partCRC.Sum32() {
			out.Close()
			return ErrCRC
		}
	}
	if err := buffered.Flush(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		return err
	}
	ok = true
	return nil
}

// chunk is the slice of one entry found in one volume.
type chunk struct {
	name      string
	offset    int64
	size      int64
	crc       uint32
	hasCRC    bool
	continued bool // the slice goes on from the previous volume
	continues bool // the slice goes on in the next volume
}

// readVolume reads one volume's headers; more is false when the archive
// says it has no further volume.
func readVolume(path string) ([]chunk, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	sig := make([]byte, 8)
	if _, err := io.ReadFull(f, sig[:7]); err != nil {
		return nil, false, ErrNotRAR
	}
	if string(sig[:7]) == string(sigRAR4) {
		return readRAR4(f, info.Size())
	}
	if _, err := io.ReadFull(f, sig[7:]); err == nil && string(sig) == string(sigRAR5) {
		return readRAR5(f, info.Size())
	}
	return nil, false, ErrNotRAR
}

// RAR 4 block types and flags.
const (
	rar4Archive = 0x73
	rar4File    = 0x74
	rar4End     = 0x7b

	rar4HasAddSize  = 0x8000
	rar4SplitBefore = 0x01
	rar4SplitAfter  = 0x02
	rar4Password    = 0x04
	rar4DirMask     = 0xe0
	rar4Large       = 0x100
	rar4Unicode     = 0x200
	rar4HeadersEnc  = 0x80 // archive header: headers encrypted
	rar4EndNextVol  = 0x01 // end block: another volume follows
	rar4MethodStore = 0x30
)

func readRAR4(f *os.File, size int64) ([]chunk, bool, error) {
	var out []chunk
	pos := int64(7)
	more := true
	for pos+7 <= size {
		base := make([]byte, 7)
		if _, err := f.ReadAt(base, pos); err != nil {
			return nil, false, err
		}
		kind := base[2]
		flags := binary.LittleEndian.Uint16(base[3:5])
		headSize := int64(binary.LittleEndian.Uint16(base[5:7]))
		if headSize < 7 || pos+headSize > size {
			return nil, false, fmt.Errorf("damaged header at byte %d", pos)
		}
		head := make([]byte, headSize)
		if _, err := f.ReadAt(head, pos); err != nil {
			return nil, false, err
		}
		if crc32.ChecksumIEEE(head[2:])&0xffff != uint32(binary.LittleEndian.Uint16(head[0:2])) {
			return nil, false, fmt.Errorf("damaged header at byte %d", pos)
		}
		var addSize int64
		if flags&rar4HasAddSize != 0 && headSize >= 11 {
			addSize = int64(binary.LittleEndian.Uint32(head[7:11]))
		}
		switch kind {
		case rar4Archive:
			if flags&rar4HeadersEnc != 0 {
				return nil, false, ErrEncrypted
			}
		case rar4File:
			if headSize < 32 {
				return nil, false, fmt.Errorf("damaged file header at byte %d", pos)
			}
			packSize := int64(binary.LittleEndian.Uint32(head[7:11]))
			crc := binary.LittleEndian.Uint32(head[16:20])
			method := head[25]
			nameSize := int(binary.LittleEndian.Uint16(head[26:28]))
			nameAt := 32
			if flags&rar4Large != 0 {
				if headSize < 40 {
					return nil, false, fmt.Errorf("damaged file header at byte %d", pos)
				}
				packSize |= int64(binary.LittleEndian.Uint32(head[32:36])) << 32
				nameAt = 40
			}
			addSize = packSize
			if nameAt+nameSize > len(head) {
				return nil, false, fmt.Errorf("damaged file name at byte %d", pos)
			}
			name := head[nameAt : nameAt+nameSize]
			if flags&rar4Unicode != 0 {
				if i := indexByte(name, 0); i >= 0 {
					name = name[:i]
				}
			}
			if flags&rar4DirMask == rar4DirMask {
				break
			}
			if flags&rar4Password != 0 {
				return nil, false, ErrEncrypted
			}
			if method != rar4MethodStore {
				return nil, false, ErrCompressed
			}
			if packSize < 0 || pos+headSize+packSize > size {
				return nil, false, fmt.Errorf("the data of %s goes past the end of the volume", cleanName(string(name)))
			}
			out = append(out, chunk{
				name: cleanName(string(name)), offset: pos + headSize, size: packSize,
				crc: crc, hasCRC: true,
				continued: flags&rar4SplitBefore != 0, continues: flags&rar4SplitAfter != 0,
			})
		case rar4End:
			more = flags&rar4EndNextVol != 0
			return out, more, nil
		}
		pos += headSize + addSize
	}
	return out, more, nil
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// cleanName turns an archive path into a portable relative one.
func cleanName(name string) string {
	return strings.ReplaceAll(name, "\\", "/")
}

// RAR 5 header types and flags.
const (
	rar5Main       = 1
	rar5File       = 2
	rar5Encryption = 4
	rar5End        = 5

	rar5HasExtra      = 0x01
	rar5HasData       = 0x02
	rar5SplitBefore   = 0x08
	rar5SplitAfter    = 0x10
	rar5FileDir       = 0x01
	rar5FileTime      = 0x02
	rar5FileCRC       = 0x04
	rar5EndNextVolume = 0x01
	rar5ExtraCrypt    = 0x01
)

// vint reads a RAR 5 variable-length integer.
func vint(b []byte, at *int) (uint64, error) {
	var v uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if *at >= len(b) {
			return 0, errors.New("truncated header")
		}
		c := b[*at]
		*at++
		v |= uint64(c&0x7f) << shift
		if c&0x80 == 0 {
			return v, nil
		}
	}
	return 0, errors.New("bad number in header")
}

func readRAR5(f *os.File, size int64) ([]chunk, bool, error) {
	var out []chunk
	pos := int64(8)
	more := true
	for pos+5 < size {
		// CRC32, then the header size as a vint (at most 3 bytes for 2 MiB).
		lead := make([]byte, 7)
		n, _ := f.ReadAt(lead, pos)
		if n < 5 {
			break
		}
		at := 4
		headSize, err := vint(lead[:n], &at)
		if err != nil || headSize == 0 || headSize > 2<<20 {
			return nil, false, fmt.Errorf("damaged header at byte %d", pos)
		}
		start := pos + int64(at)
		if start+int64(headSize) > size {
			return nil, false, fmt.Errorf("damaged header at byte %d", pos)
		}
		head := make([]byte, headSize)
		if _, err := f.ReadAt(head, start); err != nil {
			return nil, false, err
		}
		if crc32.ChecksumIEEE(append(append([]byte(nil), lead[4:at]...), head...)) != binary.LittleEndian.Uint32(lead[0:4]) {
			return nil, false, fmt.Errorf("damaged header at byte %d", pos)
		}
		p := 0
		kind, err := vint(head, &p)
		if err != nil {
			return nil, false, err
		}
		flags, err := vint(head, &p)
		if err != nil {
			return nil, false, err
		}
		var extraSize, dataSize uint64
		if flags&rar5HasExtra != 0 {
			if extraSize, err = vint(head, &p); err != nil {
				return nil, false, err
			}
		}
		if flags&rar5HasData != 0 {
			if dataSize, err = vint(head, &p); err != nil {
				return nil, false, err
			}
		}
		dataAt := start + int64(headSize)
		if dataSize > uint64(size) || dataAt+int64(dataSize) > size {
			return nil, false, fmt.Errorf("damaged header at byte %d: data past the end of the volume", pos)
		}
		switch kind {
		case rar5Encryption:
			return nil, false, ErrEncrypted
		case rar5File:
			c, skip, err := rar5FileChunk(head, p, extraSize, flags)
			if err != nil {
				return nil, false, err
			}
			if !skip {
				c.offset, c.size = dataAt, int64(dataSize)
				out = append(out, c)
			}
		case rar5End:
			endFlags, _ := vint(head, &p)
			more = endFlags&rar5EndNextVolume != 0
			return out, more, nil
		}
		pos = dataAt + int64(dataSize)
	}
	return out, more, nil
}

// rar5FileChunk reads the fields of a file header after the common ones.
func rar5FileChunk(head []byte, p int, extraSize, flags uint64) (chunk, bool, error) {
	var c chunk
	fileFlags, err := vint(head, &p)
	if err != nil {
		return c, false, err
	}
	if _, err = vint(head, &p); err != nil { // unpacked size
		return c, false, err
	}
	if _, err = vint(head, &p); err != nil { // attributes
		return c, false, err
	}
	if fileFlags&rar5FileTime != 0 {
		p += 4
	}
	if fileFlags&rar5FileCRC != 0 {
		if p+4 > len(head) {
			return c, false, errors.New("truncated header")
		}
		c.crc, c.hasCRC = binary.LittleEndian.Uint32(head[p:p+4]), true
		p += 4
	}
	compression, err := vint(head, &p)
	if err != nil {
		return c, false, err
	}
	if _, err = vint(head, &p); err != nil { // host OS
		return c, false, err
	}
	nameLen, err := vint(head, &p)
	if err != nil || p+int(nameLen) > len(head) {
		return c, false, errors.New("truncated header")
	}
	c.name = cleanName(string(head[p : p+int(nameLen)]))
	p += int(nameLen)
	// The extra area sits at the end of the header: look for encryption.
	if extraSize > 0 && int(extraSize) <= len(head) {
		extra := head[len(head)-int(extraSize):]
		for q := 0; q < len(extra); {
			recSize, err := vint(extra, &q)
			if err != nil {
				break
			}
			end := q + int(recSize)
			recType, err := vint(extra, &q)
			if err != nil {
				break
			}
			if recType == rar5ExtraCrypt {
				return c, false, ErrEncrypted
			}
			if end <= q || end > len(extra) {
				break
			}
			q = end
		}
	}
	if fileFlags&rar5FileDir != 0 {
		return c, true, nil
	}
	if method := (compression >> 7) & 0x7; method != 0 {
		return c, false, ErrCompressed
	}
	c.continued = flags&rar5SplitBefore != 0
	c.continues = flags&rar5SplitAfter != 0
	return c, false, nil
}
