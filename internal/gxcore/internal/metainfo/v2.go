package metainfo

import (
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

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
	if ib.MetaVersion == 2 && ib.PieceLength == 0 {
		return nil, errZeroPieceLength
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
	}
	if ib.MetaVersion == 2 && len(ib.Pieces) == 0 {
		// v2-only: pieces are built per file (the tail piece of a file is
		// shorter than the piece length, no padding inside a piece).
		i.V2 = true
		i.PieceHashLen = 32
		var pieces uint32
		for _, f := range i.V2Files {
			name, err := sanitizeV2Path(f.Path)
			if err != nil {
				return nil, err
			}
			i.Files = append(i.Files, File{Path: name, Length: f.Length})
			i.Length += f.Length
			pieces += uint32((f.Length + int64(ib.PieceLength) - 1) / int64(ib.PieceLength))
		}
		i.NumPieces = pieces
		copy(i.Hash[:], i.V2Hash[:20])
		return i, nil
	}
	for _, f := range i.V2Files {
		i.Length += f.Length
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

// AttachV2Pieces fills the per-file piece hashes of a v2 torrent from its
// "piece layers", validating them first. A file that fits in one piece
// contributes its pieces root (BEP 52 stores no layer entry for it).
func (i *Info) AttachV2Pieces(layers map[string][]byte) error {
	if !i.V2 {
		return nil
	}
	if i.NeedsV2Layers() {
		if len(layers) == 0 {
			// The layers have not been fetched yet: this is a magnet whose
			// info dict is known but whose BEP 52 "piece layers" still have to
			// come from peers. Leave the piece hashes unattached.
			return nil
		}
		if err := VerifyPieceLayers(i.V2Files, int(i.PieceLength), layers); err != nil {
			return err
		}
	}
	pieces := make([]byte, 0, int(i.NumPieces)*32)
	for _, f := range i.V2Files {
		if f.Length == 0 {
			continue
		}
		if f.Length <= int64(i.PieceLength) {
			if !f.HasRoot {
				return fmt.Errorf("v2 file %q has no pieces root", f.Path)
			}
			pieces = append(pieces, f.PiecesRoot[:]...)
			continue
		}
		pieces = append(pieces, layers[string(f.PiecesRoot[:])]...)
	}
	if uint32(len(pieces)/32) != i.NumPieces {
		return fmt.Errorf("v2 piece hashes: got %d, want %d", len(pieces)/32, i.NumPieces)
	}
	i.pieces = pieces
	return nil
}

// NeedsV2Layers reports whether a v2 torrent still needs its "piece layers"
// fetched from peers: true when a non-empty file is larger than one piece and
// its layer is not attached. A magnet link resolves its info dict first, so its
// layers have to be requested separately (BEP 52).
func (i *Info) NeedsV2Layers() bool {
	if !i.V2 {
		return false
	}
	for _, f := range i.V2Files {
		if f.Length > 0 && f.HasRoot && f.Length > int64(i.PieceLength) {
			return true
		}
	}
	return false
}

// HasPieceHashes reports whether the piece hashes are attached. For a v2
// torrent they come from the "piece layers" (a .torrent) or from a peer (a
// magnet); building pieces before they are attached is invalid.
func (i *Info) HasPieceHashes() bool {
	return i.NumPieces == 0 || int64(len(i.pieces)) == int64(i.NumPieces)*int64(i.hashLen())
}

// V2FilePieces returns the piece hashes of one v2 file (its entry in the
// concatenated piece hash blob), in file order, or nil when out of range. For a
// file that fits in one piece the single hash is the file's pieces root.
func (i *Info) V2FilePieces(fileIndex int) []byte {
	if !i.V2 || fileIndex < 0 || fileIndex >= len(i.V2Files) {
		return nil
	}
	hl := int(i.hashLen())
	off := 0
	for k := 0; k < fileIndex; k++ {
		off += i.v2FilePieceCount(k)
	}
	n := i.v2FilePieceCount(fileIndex)
	if n == 0 {
		return nil
	}
	start := off * hl
	end := start + n*hl
	if end > len(i.pieces) {
		return nil
	}
	return i.pieces[start:end]
}

// v2FilePieceCount returns the number of piece hashes a v2 file contributes.
func (i *Info) v2FilePieceCount(fileIndex int) int {
	if fileIndex < 0 || fileIndex >= len(i.V2Files) {
		return 0
	}
	f := i.V2Files[fileIndex]
	if f.Length == 0 {
		return 0
	}
	if f.Length <= int64(i.PieceLength) {
		return 1
	}
	return int((f.Length + int64(i.PieceLength) - 1) / int64(i.PieceLength))
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

// sanitizeV2Path cleans a BEP 52 "file tree" path like v1 does: it rejects ".."
// components and sanitizes each one (valid UTF-8, length, separators). BEP 52
// requires this to avoid directory traversal.
func sanitizeV2Path(p string) (string, error) {
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == ".." {
			return "", fmt.Errorf("invalid file name: %q", p)
		}
		out = append(out, cleanName(part))
	}
	return filepath.Join(out...), nil
}

// ParseInfo parses an info dictionary, accepting a BitTorrent v2-only one when
// its "piece layers" are provided (the resumer reload path). It is the v2-aware
// counterpart of NewInfo.
func ParseInfo(b []byte, useUTF8Keys, hidePaddings bool, layers map[string][]byte) (*Info, error) {
	i, err := NewInfo(b, useUTF8Keys, hidePaddings)
	if errors.Is(err, ErrV2Only) {
		v2, verr := NewV2Info(b)
		if verr != nil {
			return nil, verr
		}
		if aerr := v2.AttachV2Pieces(layers); aerr != nil {
			return nil, aerr
		}
		return v2, nil
	}
	return i, err
}

// EncodePieceLayers bencodes a v2 "piece layers" map for persistence.
func EncodePieceLayers(layers map[string][]byte) ([]byte, error) {
	if len(layers) == 0 {
		return nil, nil
	}
	return bencode.EncodeBytes(layers)
}

// DecodePieceLayers decodes a bencoded "piece layers" map.
func DecodePieceLayers(b []byte) (map[string][]byte, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var m map[string][]byte
	if err := bencode.DecodeBytes(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}
