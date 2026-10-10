package metainfo

import (
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"path"
	"sort"

	"github.com/buzzqw/gextto/internal/gxcore/internal/merkle"
	"github.com/zeebo/bencode"
)

// V2File is one file entry of a BEP 52 "file tree".
type V2File struct {
	// Path is the file path relative to the torrent root, using '/' as the
	// separator (the file tree nesting flattened).
	Path string
	// Length is the file size in bytes.
	Length int64
	// PiecesRoot is the merkle root of the file (SHA-256 of its 16 KiB blocks).
	// HasRoot reports whether the entry carried one (non-empty files do).
	PiecesRoot [32]byte
	HasRoot    bool
	// Attr is the BEP 52 attribute string (for example "p" for a padding file).
	Attr string
}

// ParseFileTree flattens a BEP 52 "file tree" dictionary into its files. The
// order matches the tree: keys (path components) are sorted and walked depth
// first, which is the order used to map files into the piece address space.
func ParseFileTree(raw []byte) ([]V2File, error) {
	var out []V2File
	var walk func(prefix string, raw bencode.RawMessage) error
	walk = func(prefix string, raw bencode.RawMessage) error {
		var entries map[string]bencode.RawMessage
		if err := bencode.DecodeBytes(raw, &entries); err != nil {
			return err
		}
		names := make([]string, 0, len(entries))
		for name := range entries {
			names = append(names, name)
		}
		sort.Strings(names)
		// The zero-length key holds the properties of this path: a file if it
		// has a "length".
		if leaf, ok := entries[""]; ok {
			f, isFile, err := parseLeaf(prefix, leaf)
			if err != nil {
				return err
			}
			if isFile {
				out = append(out, f)
			}
		}
		for _, name := range names {
			if name == "" {
				continue
			}
			if err := walk(path.Join(prefix, name), entries[name]); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk("", raw); err != nil {
		return nil, err
	}
	return out, nil
}

func parseLeaf(prefix string, raw bencode.RawMessage) (V2File, bool, error) {
	var fields map[string]bencode.RawMessage
	if err := bencode.DecodeBytes(raw, &fields); err != nil {
		return V2File{}, false, err
	}
	lraw, ok := fields["length"]
	if !ok {
		// A directory marker: no length field.
		return V2File{}, false, nil
	}
	var length int64
	if err := bencode.DecodeBytes(lraw, &length); err != nil {
		return V2File{}, false, err
	}
	f := V2File{Path: prefix, Length: length}
	if araw, ok := fields["attr"]; ok {
		var attr string
		if bencode.DecodeBytes(araw, &attr) == nil {
			f.Attr = attr
		}
	}
	if praw, ok := fields["pieces root"]; ok {
		var root []byte
		if bencode.DecodeBytes(praw, &root) == nil && len(root) == 32 {
			copy(f.PiecesRoot[:], root)
			f.HasRoot = true
		}
	}
	return f, true, nil
}

// NewV2Info parses a BitTorrent v2 (or hybrid) info dictionary. Unlike NewInfo
// it does not require the v1 "pieces", so a v2-only info dictionary is
// accepted; the v1 fields are filled in too when present. It is the building
// block for downloading v2 torrents and does not change NewInfo's behaviour.
func NewV2Info(b []byte) (*Info, error) {
	var ib infoType
	if err := bencode.DecodeBytes(b, &ib); err != nil {
		return nil, err
	}
	if ib.MetaVersion > 2 {
		return nil, ErrUnknownMetaVersion
	}
	if ib.MetaVersion != 2 && len(ib.Pieces) == 0 {
		return nil, errZeroPieces
	}
	i := &Info{
		PieceLength: ib.PieceLength,
		Name:        ib.Name,
		MetaVersion: ib.MetaVersion,
		HasV2:       ib.MetaVersion != 0 || len(ib.FileTree) > 0,
		Bytes:       b,
	}
	if i.HasV2 {
		i.V2Hash = sha256.Sum256(b)
	}
	if len(ib.FileTree) > 0 {
		files, err := ParseFileTree(ib.FileTree)
		if err != nil {
			return nil, err
		}
		i.V2Files = files
		for _, f := range files {
			i.Length += f.Length
		}
	}
	if len(ib.Pieces) > 0 && len(ib.Pieces)%sha1.Size == 0 {
		i.NumPieces = uint32(len(ib.Pieces) / sha1.Size)
		i.pieces = ib.Pieces
		i.Hash = sha1.Sum(b)
	}
	// Multi-file v1 torrents describe their files with "files"; keep them for
	// callers that need the v1 layout of a hybrid torrent.
	if len(ib.Files) > 0 {
		for _, f := range ib.Files {
			i.Files = append(i.Files, File{Path: path.Join(append([]string{ib.Name}, f.Path...)...), Length: f.Length})
		}
	}
	return i, nil
}

// VerifyPieceLayers checks a BEP 52 "piece layers" map against the files of a
// v2 info dictionary. For every non-empty file larger than one piece, the
// concatenated hashes must be the layer that covers one piece, and hashing that
// layer up to the tree root (padding the tail with the zero-subtree hash) must
// reproduce the file's "pieces root". Files that fit in one piece need no entry.
func VerifyPieceLayers(files []V2File, pieceLength int, layers map[string][]byte) error {
	if pieceLength < merkle.BlockSize || pieceLength%merkle.BlockSize != 0 {
		return fmt.Errorf("piece length %d is not a multiple of %d", pieceLength, merkle.BlockSize)
	}
	ratio := pieceLength / merkle.BlockSize
	if ratio&(ratio-1) != 0 {
		return fmt.Errorf("piece length %d is not a power of two multiple of %d", pieceLength, merkle.BlockSize)
	}
	level := 0
	for r := ratio; r > 1; r >>= 1 {
		level++
	}
	for _, f := range files {
		if f.Length == 0 || !f.HasRoot || f.Length <= int64(pieceLength) {
			continue
		}
		layer, ok := layers[string(f.PiecesRoot[:])]
		if !ok {
			return fmt.Errorf("missing piece layer for %q", f.Path)
		}
		if len(layer) == 0 || len(layer)%merkle.HashSize != 0 {
			return fmt.Errorf("piece layer for %q has an invalid length", f.Path)
		}
		blocks := int((f.Length + int64(merkle.BlockSize) - 1) / int64(merkle.BlockSize))
		groupSize := 1 << level
		want := (blocks + groupSize - 1) / groupSize
		count := len(layer) / merkle.HashSize
		if count != want {
			return fmt.Errorf("piece layer for %q has %d hashes, want %d", f.Path, count, want)
		}
		full := make([][merkle.HashSize]byte, 0, nextPow2(count))
		for i := 0; i < count; i++ {
			var h [merkle.HashSize]byte
			copy(h[:], layer[i*merkle.HashSize:(i+1)*merkle.HashSize])
			full = append(full, h)
		}
		// Missing entries cover subtrees beyond the end of the file: they are
		// the root of a perfect tree of zero leaves at this level.
		for len(full) < nextPow2(count) {
			full = append(full, merkle.ZeroRoot(level))
		}
		if merkle.Root(full) != f.PiecesRoot {
			return fmt.Errorf("piece layer for %q does not match its pieces root", f.Path)
		}
	}
	return nil
}

func nextPow2(n int) int {
	if n <= 1 {
		return 1
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}
