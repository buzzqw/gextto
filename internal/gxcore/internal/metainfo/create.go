package metainfo

// create.go builds the info dictionary the torrent creator writes: v1, v2
// (BEP 52) and the hybrid of the two. The hybrid follows the upgrade path of
// BEP 52: the v1 piece stream gets BEP 47 pad files (in ".pad") so every real
// file starts on a piece boundary, exactly where v2 places it; the v2 "file
// tree" lists only the real files, as libtorrent does.
//
// The calls are streaming: a file is read in 16 KiB blocks and its piece
// hashes are kept, never the whole payload. A 100 GB file at 4 MiB pieces
// costs 25k hashes (about 800 KB), not 100 GB.

import (
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/merkle"
	"github.com/zeebo/bencode"
)

// CreationOptions configures CreateInfo.
type CreationOptions struct {
	// Root is the common folder the paths are relative to (required with more
	// than one path).
	Root string
	// Paths are the files or folders to include.
	Paths []string
	// Name is the torrent name; empty means the base name of the path.
	Name string
	// Private sets the BEP 27 private flag.
	Private bool
	// PieceLength is a power-of-two multiple of 16 KiB; 0 picks one.
	PieceLength uint32
	// Format is "v1" (default), "v2" or "hybrid".
	Format string
	// Source is the qBittorrent "source" string stored in the info dict.
	Source string
	// Log receives the per-file progress lines; nil is silent.
	Log logger.Logger
}

type createFile struct {
	// path is relative to the torrent name, with '/' separators in RelPath.
	RelPath []string
	Abs     string
	Size    int64
}

// CreateInfo hashes the files into an info dict and, for v2 and hybrid, the
// top-level "piece layers" map.
func CreateInfo(opt CreationOptions) ([]byte, map[string][]byte, error) {
	format := strings.ToLower(strings.TrimSpace(opt.Format))
	switch format {
	case "", "v1":
		format = "v1"
	case "v2", "hybrid":
	default:
		return nil, nil, fmt.Errorf("unknown torrent format %q", opt.Format)
	}
	if opt.Log == nil {
		opt.Log = logger.New("create torrent")
	}

	if format == "v1" {
		// v1 keeps the historical walk order and hashing path: the file order
		// is part of the v1 info hash, so it must not change under existing
		// users of the creator.
		info, err := createV1Info(opt)
		return info, nil, err
	}

	single, name, files, total, err := collectCreateFiles(opt.Root, opt.Paths, opt.Name)
	if err != nil {
		return nil, nil, err
	}
	if total == 0 {
		return nil, nil, errors.New("no files")
	}
	pieceLength := opt.PieceLength
	if pieceLength == 0 {
		pieceLength = calculatePieceLength(total)
		opt.Log.Infof("Calculated piece length: %d K", pieceLength>>10)
	}
	if pieceLength%(16<<10) != 0 {
		return nil, nil, errPieceLength
	}
	if pieceLength > maxPieceLength {
		return nil, nil, errPieceLengthBig
	}
	if !isPowerOfTwo(pieceLength) {
		return nil, nil, fmt.Errorf("the %s format needs a power-of-two piece length, got %d", format, pieceLength)
	}

	tree, layers, err := buildV2Tree(name, files, single, pieceLength, opt.Log)
	if err != nil {
		return nil, nil, err
	}

	info := v1Info{
		Name:        name,
		Private:     opt.Private,
		PieceLength: pieceLength,
		Source:      opt.Source,
		FileTree:    tree,
		MetaVersion: 2,
	}
	if format == "hybrid" {
		v1Files, pieces, err := hashV1WithPadding(files, single, pieceLength, opt.Log)
		if err != nil {
			return nil, nil, err
		}
		info.Pieces = pieces
		if single {
			info.Length = files[0].Size
		} else {
			info.Files = v1Files
		}
	}
	data, err := bencode.EncodeBytes(info)
	if err != nil {
		return nil, nil, err
	}
	return data, layers, nil
}

// v1Info is the info dictionary of every format. The v2 fields are omitted in
// v1 torrents; the v1 fields are omitted in v2-only torrents.
type v1Info struct {
	Name        string         `bencode:"name"`
	Private     bool           `bencode:"private"`
	PieceLength uint32         `bencode:"piece length"`
	Pieces      []byte         `bencode:"pieces,omitempty"`
	Length      int64          `bencode:"length,omitempty"`
	Files       []file         `bencode:"files,omitempty"`
	MetaVersion int64          `bencode:"meta version,omitempty"`
	FileTree    map[string]any `bencode:"file tree,omitempty"`
	Source      string         `bencode:"source,omitempty"`
}

// createV1Info is the v1 path: NewInfoBytes plus the optional "source".
func createV1Info(opt CreationOptions) ([]byte, error) {
	name := opt.Name
	single, err := resolveCreationLayout(opt.Root, opt.Paths, &name)
	if err != nil {
		return nil, err
	}
	total, err := findTotalLength(opt.Paths)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return nil, errors.New("no files")
	}
	pieceLength := opt.PieceLength
	if pieceLength == 0 {
		pieceLength = calculatePieceLength(total)
		opt.Log.Infof("Calculated piece length: %d K", pieceLength>>10)
	} else if pieceLength%(16<<10) != 0 {
		return nil, errPieceLength
	}
	if pieceLength > maxPieceLength {
		return nil, errPieceLengthBig
	}
	files, pieces, err := hashFiles(opt.Root, opt.Paths, pieceLength, opt.Log)
	if err != nil {
		return nil, err
	}
	info := v1Info{
		Name:        name,
		Private:     opt.Private,
		PieceLength: pieceLength,
		Pieces:      pieces,
		Source:      opt.Source,
	}
	if single {
		info.Length = total
	} else {
		info.Files = files
	}
	return bencode.EncodeBytes(info)
}

// collectCreateFiles enumerates the payload once, in the component-wise order
// the v2 file tree uses, so v1 and v2 describe the same sequence.
func collectCreateFiles(root string, paths []string, name string) (single bool, torrentName string, files []createFile, total int64, err error) {
	if len(paths) == 0 {
		return false, "", nil, 0, errors.New("no path specified")
	}
	if len(paths) > 1 && root == "" {
		return false, "", nil, 0, errors.New("no root specified")
	}
	if len(paths) > 1 && name == "" {
		return false, "", nil, 0, errors.New("no name specified")
	}
	torrentName = name
	for _, path := range paths {
		fi, err := os.Stat(path)
		if err != nil {
			return false, "", nil, 0, err
		}
		base := root
		if len(paths) == 1 {
			base = path
			if fi.IsDir() {
				if torrentName == "" {
					torrentName = filepath.Base(filepath.Clean(path))
				}
			} else if torrentName == "" {
				torrentName = filepath.Base(filepath.Clean(path))
			}
		}
		if !fi.IsDir() {
			if len(paths) == 1 {
				single = true
				files = append(files, createFile{RelPath: []string{torrentName}, Abs: path, Size: fi.Size()})
				total += fi.Size()
				continue
			}
			rel, err := filepath.Rel(base, path)
			if err != nil {
				return false, "", nil, 0, err
			}
			if rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				return false, "", nil, 0, fmt.Errorf("%q is outside the torrent root", path)
			}
			files = append(files, createFile{RelPath: splitRelPath(rel), Abs: path, Size: fi.Size()})
			total += fi.Size()
			continue
		}
		if single {
			// A single directory is a multi-file torrent.
			single = false
		}
		err = filepath.Walk(path, func(vpath string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if fi.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(base, vpath)
			if err != nil {
				return err
			}
			if rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				return fmt.Errorf("%q is outside the torrent root", vpath)
			}
			files = append(files, createFile{RelPath: splitRelPath(rel), Abs: vpath, Size: fi.Size()})
			total += fi.Size()
			return nil
		})
		if err != nil {
			return false, "", nil, 0, err
		}
	}
	if len(files) == 0 {
		return false, "", nil, 0, errors.New("no files")
	}
	if torrentName == "" || torrentName == "." || torrentName == ".." ||
		strings.ContainsAny(torrentName, `/\`) {
		return false, "", nil, 0, fmt.Errorf("invalid torrent name %q", torrentName)
	}
	sort.Slice(files, func(i, j int) bool { return compareComponents(files[i].RelPath, files[j].RelPath) < 0 })
	return single, torrentName, files, total, nil
}

// splitRelPath turns an OS path into the components of the file tree.
func splitRelPath(rel string) []string {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	out := parts[:0]
	for _, part := range parts {
		if part != "" && part != "." {
			out = append(out, part)
		}
	}
	return append([]string(nil), out...)
}

// compareComponents compares two paths component by component. It is not the
// same as comparing the joined strings: "a/b" sorts before "a.b" here (the
// component "a" is shorter), while byte order would put "a.b" first. The v2
// file tree is walked by components, so this is the order that keeps v1 and v2
// aligned.
func compareComponents(a, b []string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

// buildV2Tree hashes each real file into its pieces root and assembles the
// BEP 52 file tree. A file that fits in one piece has no "piece layers" entry.
func buildV2Tree(name string, files []createFile, single bool, pieceLength uint32, log logger.Logger) (map[string]any, map[string][]byte, error) {
	tree := make(map[string]any)
	layers := make(map[string][]byte)
	for _, f := range files {
		root, layer, err := hashV2File(f.Abs, f.Size, pieceLength)
		if err != nil {
			return nil, nil, err
		}
		rel := f.RelPath
		if single {
			rel = []string{name}
		}
		if log != nil {
			log.Infof("Adding %q", filepath.Join(rel...))
		}
		if err := insertFileTree(tree, rel, f.Size, root); err != nil {
			return nil, nil, err
		}
		if len(layer) > 0 {
			layers[string(root[:])] = layer
		}
	}
	return tree, layers, nil
}

// insertFileTree walks the path components and stores the leaf dictionary at
// the empty key, rejecting a file that collides with a directory.
func insertFileTree(tree map[string]any, rel []string, size int64, root [merkle.HashSize]byte) error {
	if len(rel) == 0 {
		return errors.New("a file with an empty path")
	}
	node := tree
	for _, part := range rel[:len(rel)-1] {
		next, ok := node[part]
		if !ok {
			child := make(map[string]any)
			node[part] = child
			node = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("path conflict at %q", part)
		}
		node = child
	}
	leafName := rel[len(rel)-1]
	if existing, ok := node[leafName]; ok {
		if _, isDir := existing.(map[string]any); isDir {
			return fmt.Errorf("path conflict at %q", leafName)
		}
	}
	leaf := make(map[string]any)
	if size > 0 {
		leaf["pieces root"] = string(root[:])
	}
	leaf["length"] = size
	node[leafName] = map[string]any{"": leaf}
	return nil
}

// hashV2File computes the merkle root of one file and, for a file larger than
// one piece, its piece layer (the hashes that cover one piece).
func hashV2File(path string, size int64, pieceLength uint32) ([merkle.HashSize]byte, []byte, error) {
	var zero [merkle.HashSize]byte
	if size == 0 {
		return zero, nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return zero, nil, err
	}
	defer f.Close()

	ratio := int(pieceLength) / merkle.BlockSize
	if size <= int64(pieceLength) {
		leaves, err := readLeaves(f, size)
		if err != nil {
			return zero, nil, err
		}
		return merkle.Root(leaves), nil, nil
	}

	level := merkle.PieceLevel(int(pieceLength))
	if level < 0 {
		return zero, nil, fmt.Errorf("invalid piece length %d", pieceLength)
	}
	var layer []byte
	var pieces [][merkle.HashSize]byte
	buf := make([]byte, pieceLength)
	remaining := size
	for remaining > 0 {
		n := int64(len(buf))
		if remaining < n {
			n = remaining
		}
		if _, err := io.ReadFull(f, buf[:n]); err != nil {
			return zero, nil, err
		}
		group := make([][merkle.HashSize]byte, ratio)
		for i := 0; i < ratio; i++ {
			start := i * merkle.BlockSize
			end := start + merkle.BlockSize
			if end > int(n) {
				end = int(n)
			}
			if start >= end {
				break
			}
			group[i] = sha256Sum(buf[start:end])
		}
		hash := merkle.Root(group)
		pieces = append(pieces, hash)
		layer = append(layer, hash[:]...)
		remaining -= n
	}
	padded := make([][merkle.HashSize]byte, nextPowerOfTwo(len(pieces)))
	copy(padded, pieces)
	pad := merkle.ZeroRoot(level)
	for i := len(pieces); i < len(padded); i++ {
		padded[i] = pad
	}
	return merkle.Root(padded), layer, nil
}

// sha256Sum is a tiny wrapper so this file does not import crypto/sha256 for a
// single call.
func sha256Sum(b []byte) [merkle.HashSize]byte {
	return sha256.Sum256(b)
}

// readLeaves hashes the first size bytes of r into 16 KiB leaves.
func readLeaves(r io.Reader, size int64) ([][merkle.HashSize]byte, error) {
	var leaves [][merkle.HashSize]byte
	buf := make([]byte, merkle.BlockSize)
	for size > 0 {
		n := int64(len(buf))
		if size < n {
			n = size
		}
		if _, err := io.ReadFull(r, buf[:n]); err != nil {
			return nil, err
		}
		leaves = append(leaves, sha256Sum(buf[:n]))
		size -= n
	}
	return leaves, nil
}

// hashV1Pieces hashes the concatenated payload with no padding (a v1 torrent).
// hashV1WithPadding hashes the v1 payload with the BEP 47 pad files a hybrid
// torrent needs, and returns the v1 file entries in that order (pad, file,
// pad, file…).
func hashV1WithPadding(files []createFile, single bool, pieceLength uint32, log logger.Logger) ([]file, []byte, error) {
	return hashV1Stream(files, single, pieceLength, true, log)
}

// hashV1Stream feeds the files through one SHA-1 piece buffer. When pad is
// true, a zero pad file fills the current piece before every file that does not
// start on a piece boundary (single-file torrents never need one).
func hashV1Stream(files []createFile, single bool, pieceLength uint32, pad bool, log logger.Logger) ([]file, []byte, error) {
	buf := make([]byte, pieceLength)
	offset := 0
	hash := sha1.New()
	var pieces []byte
	var v1Files []file
	flush := func(n int) {
		_, _ = hash.Write(buf[:n])
		pieces = hash.Sum(pieces)
		hash.Reset()
		offset = 0
	}
	for _, f := range files {
		if pad && !single && offset != 0 && f.Size > 0 {
			padSize := int(pieceLength) - offset
			for i := offset; i < int(pieceLength); i++ {
				buf[i] = 0
			}
			flush(int(pieceLength))
			v1Files = append(v1Files, file{
				Path:   []string{".pad", strconv.Itoa(padSize)},
				Length: int64(padSize),
				Attr:   "p",
			})
		}
		if log != nil {
			log.Infof("Adding %q", filepath.Join(f.RelPath...))
		}
		if !single {
			v1Files = append(v1Files, file{Path: f.RelPath, Length: f.Size})
		}
		fh, err := os.Open(f.Abs)
		if err != nil {
			return nil, nil, err
		}
		for {
			n, err := io.ReadFull(fh, buf[offset:])
			offset += n
			if err == io.ErrUnexpectedEOF || err == io.EOF {
				break
			}
			if err != nil {
				fh.Close()
				return nil, nil, err
			}
			flush(int(pieceLength))
		}
		fh.Close()
	}
	if offset > 0 {
		flush(offset)
	}
	return v1Files, pieces, nil
}

// isPowerOfTwo reports whether n is a positive power of two.
func isPowerOfTwo(n uint32) bool {
	return n > 0 && n&(n-1) == 0
}

// nextPowerOfTwo rounds n up to a positive power of two.
func nextPowerOfTwo(n int) int {
	if n <= 1 {
		return 1
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}
