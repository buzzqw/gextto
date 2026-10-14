package metainfo

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bencodecheck"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/zeebo/bencode"
)

var (
	errInvalidPieceData = errors.New("invalid piece data")
	errZeroPieceLength  = errors.New("torrent has zero piece length")
	errZeroPieces       = errors.New("torrent has zero pieces")
	errPieceLength      = errors.New("piece length must be multiple of 16K")
	errPieceLengthBig   = errors.New("piece length is too large")
	// ErrV2Only marks a v2-only torrent (BEP 52): meta version 2 with a file
	// tree but no v1 pieces.
	ErrV2Only = errors.New("torrent is BitTorrent v2 only")
	// ErrUnknownMetaVersion marks a newer, unsupported meta version.
	ErrUnknownMetaVersion = errors.New("unsupported meta version")
)

// maxPieceLength caps a requested piece size: the standard v1 ceiling is
// 16 MiB and the calculator never exceeds it, so a crafted create-torrent
// request cannot force a giant hash buffer.
const maxPieceLength = 16 << 20

// Info is the decoded info dictionary: piece layout, files, hashes and the
// v2/BEP 38 extensions.
type Info struct {
	PieceLength uint32
	Name        string
	Hash        [20]byte
	Length      int64
	Padding     int64
	NumPieces   uint32
	Bytes       []byte
	Private     bool
	Files       []File
	// MetaVersion is the BEP 52 meta version (0 when absent, i.e. v1).
	MetaVersion int64
	// V2Hash is the SHA-256 of the info dict when MetaVersion >= 2; the
	// handshake and tracker use its first 20 bytes.
	V2Hash [32]byte
	// HasV2 reports a meta version or file tree in the dict.
	HasV2 bool
	// V2Files are the BEP 52 file-tree files in tree order (NewV2Info only).
	V2Files []V2File
	// V2 marks v2 torrents: per-file pieces with short tails, no padding,
	// SHA-256 merkle verification.
	V2 bool
	// PieceHashLen is one hash's bytes (0 = 20 SHA-1; 32 = v2 SHA-256).
	PieceHashLen uint32
	// Collections/Similar are the BEP 38 hints, merged from info and
	// top-level dicts.
	Collections []string
	Similar     [][20]byte
	pieces      []byte
}

// File is one torrent file.
type File struct {
	Length int64
	Path   string
	// https://www.bittorrent.org/beps/bep_0047.html
	Padding bool
}

// SetFilePaths overrides the storage paths of the files, in order, when the
// list matches the file count. It returns false otherwise. Used by the resume
// loader to apply a persisted rename (qBittorrent renameFile/renameFolder):
// the info dict is untouched, so the info hash stays the original.
func (i *Info) SetFilePaths(paths []string) bool {
	if len(paths) != len(i.Files) {
		return false
	}
	for j := range i.Files {
		i.Files[j].Path = paths[j]
	}
	return true
}

// file is the bencoded file entry.
type file struct {
	Length   int64    `bencode:"length"`
	Path     []string `bencode:"path"`
	PathUTF8 []string `bencode:"path.utf-8,omitempty"`
	Attr     string   `bencode:"attr"`
}

// isPadding reports BEP 47 pad files and the BitComet out-of-spec
// "_____padding_file" convention.
func (f *file) isPadding() bool {
	if strings.ContainsRune(f.Attr, 'p') {
		return true
	}
	if len(f.Path) > 0 && strings.HasPrefix(f.Path[len(f.Path)-1], "_____padding_file") {
		return true
	}
	return false
}

// infoType is the bencoded info dictionary shape.
type infoType struct {
	PieceLength uint32             `bencode:"piece length"`
	Pieces      []byte             `bencode:"pieces"`
	Name        string             `bencode:"name"`
	NameUTF8    string             `bencode:"name.utf-8,omitempty"`
	Private     bencode.RawMessage `bencode:"private"`
	Length      int64              `bencode:"length"` // Single File Mode
	Files       []file             `bencode:"files"`  // Multiple File mode
	// BEP 52 (BitTorrent v2)
	MetaVersion int64              `bencode:"meta version"`
	FileTree    bencode.RawMessage `bencode:"file tree"`
	// BEP 38 hints inside the info dict.
	Collections []string `bencode:"collections"`
	Similar     []string `bencode:"similar"`
}

// overrideUTF8Keys prefers the utf-8 path spellings when present.
func (ib *infoType) overrideUTF8Keys() {
	if len(ib.NameUTF8) > 0 {
		ib.Name = ib.NameUTF8
	}
	for i := range ib.Files {
		if len(ib.Files[i].PathUTF8) > 0 {
			ib.Files[i].Path = ib.Files[i].PathUTF8
		}
	}
}

// NewInfo decodes b into an Info: v1, hybrid or (via ErrV2Only) v2-only.
// Validation runs meta-version first, then piece data, then file names.
func NewInfo(b []byte, utf8 bool, pad bool) (*Info, error) {
	if err := bencodecheck.Validate(b); err != nil {
		return nil, err
	}
	var ib infoType
	if err := bencode.DecodeBytes(b, &ib); err != nil {
		return nil, err
	}
	if ib.MetaVersion > 2 {
		return nil, ErrUnknownMetaVersion
	}
	if ib.MetaVersion == 2 && len(ib.Pieces) == 0 {
		return nil, ErrV2Only
	}
	if ib.PieceLength == 0 {
		return nil, errZeroPieceLength
	}
	if len(ib.Pieces)%sha1.Size != 0 {
		return nil, errInvalidPieceData
	}
	numPieces := len(ib.Pieces) / sha1.Size
	if numPieces == 0 {
		return nil, errZeroPieces
	}
	if utf8 {
		ib.overrideUTF8Keys()
	}
	for _, file := range ib.Files {
		for _, part := range file.Path {
			if strings.TrimSpace(part) == ".." {
				return nil, fmt.Errorf("invalid file name: %q", filepath.Join(file.Path...))
			}
		}
	}
	i := Info{
		PieceLength: ib.PieceLength,
		NumPieces:   uint32(numPieces),
		pieces:      ib.Pieces,
		Name:        ib.Name,
		Private:     parsePrivateField(ib.Private),
		MetaVersion: ib.MetaVersion,
		HasV2:       ib.MetaVersion != 0 || len(ib.FileTree) > 0,
		Collections: normalizeCollections(ib.Collections),
		Similar:     normalizeSimilar(ib.Similar),
	}
	multiFile := len(ib.Files) > 0
	if multiFile {
		for _, f := range ib.Files {
			i.Length += f.Length
			if f.isPadding() {
				i.Padding += f.Length
			}
		}
	} else {
		i.Length = ib.Length
	}
	if delta := int64(i.PieceLength)*int64(i.NumPieces) - i.Length; delta >= int64(i.PieceLength) || delta < 0 {
		return nil, errInvalidPieceData
	}
	i.Bytes = b

	i.Hash = sha1.Sum(b)
	if i.HasV2 {
		i.V2Hash = sha256.Sum256(b)
	}
	if ib.Name != "" {
		i.Name = ib.Name
	} else {
		i.Name = hex.EncodeToString(i.Hash[:])
	}
	if err := i.buildFiles(ib.Files, multiFile, pad); err != nil {
		return nil, err
	}
	return &i, nil
}

// buildFiles maps the bencoded entries onto cleaned storage paths, rejecting
// duplicate non-padding paths (pad files legitimately share ".pad/<length>"
// per BEP 47).
func (i *Info) buildFiles(entries []file, multiFile, pad bool) error {
	if !multiFile {
		i.Files = []File{{Path: cleanName(i.Name), Length: i.Length}}
		return nil
	}
	i.Files = make([]File, len(entries))
	uniquePaths := make(map[string]any, len(entries))
	for j, f := range entries {
		parts := make([]string, 0, len(f.Path)+1)
		parts = append(parts, cleanName(i.Name))
		for _, p := range f.Path {
			parts = append(parts, cleanName(p))
		}
		joinedPath := filepath.Join(parts...)
		isPadding := pad && f.isPadding()
		if !isPadding {
			if _, ok := uniquePaths[joinedPath]; ok {
				return fmt.Errorf("duplicate file name: %q", joinedPath)
			}
			uniquePaths[joinedPath] = nil
		}
		i.Files[j] = File{
			Path:    joinedPath,
			Length:  f.Length,
			Padding: isPadding,
		}
	}
	return nil
}

// cleanName sanitizes one path segment for storage: valid UTF-8, bounded
// length with the extension kept, invalid sequences stripped, no separators.
func cleanName(s string) string {
	s = strings.ToValidUTF8(s, string(unicode.ReplacementChar))
	s = trimName(s, 255)
	s = strings.ToValidUTF8(s, "")
	return replaceSeparator(s)
}

// trimName caps length keeping the extension when it fits.
func trimName(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	ext := path.Ext(s)
	if len(ext) > maxLen {
		return s[:maxLen]
	}
	return s[:maxLen-len(ext)] + ext
}

// replaceSeparator neutralizes embedded slashes.
func replaceSeparator(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' {
			return '_'
		}
		return r
	}, s)
}

// parsePrivateField reads the loosely-typed private flag: missing is false,
// integers test non-zero, strings test non-empty and non-"0", undecodable
// values default to private (fail closed).
func parsePrivateField(s bencode.RawMessage) bool {
	if len(s) == 0 {
		return false
	}
	var intVal int64
	if err := bencode.DecodeBytes(s, &intVal); err == nil {
		return intVal != 0
	}
	var stringVal string
	if err := bencode.DecodeBytes(s, &stringVal); err != nil {
		return true
	}
	return stringVal != "" && stringVal != "0"
}

// NewInfoBytes hashes the files on disk into an info dict.
func NewInfoBytes(root string, paths []string, private bool, pieceLength uint32, name string, log logger.Logger) ([]byte, error) {
	single, err := resolveCreationLayout(root, paths, &name)
	if err != nil {
		return nil, err
	}
	totalLength, err := findTotalLength(paths)
	if err != nil {
		return nil, err
	}
	if totalLength == 0 {
		return nil, errors.New("no files")
	}
	if pieceLength == 0 {
		pieceLength = calculatePieceLength(totalLength)
		log.Infof("Calculated piece length: %d K", pieceLength>>10)
	} else if pieceLength%(16<<10) != 0 {
		return nil, errPieceLength
	}
	if pieceLength > maxPieceLength {
		return nil, errPieceLengthBig
	}
	files, pieces, err := hashFiles(root, paths, pieceLength, log)
	if err != nil {
		return nil, err
	}
	b := struct {
		Name        string `bencode:"name"`
		Private     bool   `bencode:"private"`
		PieceLength uint32 `bencode:"piece length"`
		Pieces      []byte `bencode:"pieces"`
		Length      int64  `bencode:"length,omitempty"` // Single File Mode
		Files       []file `bencode:"files,omitempty"`  // Multiple File mode
	}{
		Name:        name,
		Private:     private,
		PieceLength: pieceLength,
		Pieces:      pieces,
	}
	if single {
		b.Length = totalLength
	} else {
		b.Files = files
	}
	return bencode.EncodeBytes(b)
}

// resolveCreationLayout validates the creation arguments and defaults the
// name, reporting whether the result is a single-file torrent.
func resolveCreationLayout(root string, paths []string, name *string) (bool, error) {
	switch len(paths) {
	case 0:
		return false, errors.New("no path specified")
	case 1:
		if *name == "" {
			*name = filepath.Base(paths[0])
		}
		fi, err := os.Stat(paths[0])
		if err != nil {
			return false, err
		}
		return !fi.IsDir(), nil
	default:
		if root == "" {
			return false, errors.New("no root specified")
		}
		if *name == "" {
			return false, errors.New("no name specified")
		}
		return false, nil
	}
}

// hashFiles walks every path, hashing the concatenated bytes into piece
// hashes and collecting the file entries.
func hashFiles(root string, paths []string, pieceLength uint32, log logger.Logger) ([]file, []byte, error) {
	buf := make([]byte, pieceLength)
	offset := 0
	remaining := func() []byte { return buf[offset:] }
	hash := sha1.New()
	var files []file
	var pieces []byte
	for _, path := range paths {
		relroot := path
		if root != "" {
			relroot = root
		}
		visit := func(vpath string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if fi.IsDir() {
				return nil
			}
			f, err := os.Open(vpath)
			if err != nil {
				return err
			}
			defer f.Close()
			relpath, err := filepath.Rel(relroot, vpath)
			log.Infof("Adding %q", relpath)
			if err != nil {
				return err
			}
			files = append(files, file{Path: strings.Split(relpath, string(os.PathSeparator)), Length: fi.Size()})
			for {
				n, err := io.ReadFull(f, remaining())
				offset += n
				if err == io.ErrUnexpectedEOF || err == io.EOF {
					return nil // file finished, continue with next file
				}
				if err != nil {
					return err
				}
				_, _ = hash.Write(buf)
				pieces = hash.Sum(pieces)
				hash.Reset()
				offset = 0
			}
		}
		if err := filepath.Walk(path, visit); err != nil {
			return nil, nil, err
		}
	}
	if offset > 0 {
		_, _ = hash.Write(buf[:offset])
		pieces = hash.Sum(pieces)
	}
	return files, pieces, nil
}

// PieceHash returns piece index's hash bytes.
func (i *Info) PieceHash(index uint32) []byte {
	n := i.hashLen()
	begin := index * n
	return i.pieces[begin : begin+n]
}

// hashLen is one hash's bytes: 20 SHA-1 unless set for v2.
func (i *Info) hashLen() uint32 {
	if i.PieceHashLen > 0 {
		return i.PieceHashLen
	}
	return sha1.Size
}

func findTotalLength(paths []string) (n int64, err error) {
	for _, path := range paths {
		err = filepath.Walk(path, func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if fi.IsDir() {
				return nil
			}
			n += fi.Size()
			return nil
		})
		if err != nil {
			return
		}
	}
	return
}

// calculatePieceLength targets ~2000 pieces between 32 KiB and 16 MiB,
// rounded up to a power of two.
func calculatePieceLength(totalLength int64) uint32 {
	const maxPieces = 2000
	pieceLength := totalLength / maxPieces
	switch {
	case pieceLength < 32<<10:
		return 32 << 10
	case pieceLength > 16<<20:
		return 16 << 20
	default:
		v := uint32(pieceLength)
		v--
		v |= v >> 1
		v |= v >> 2
		v |= v >> 4
		v |= v >> 8
		v |= v >> 16
		v++
		return v
	}
}
