package metainfo

import (
	"bytes"
	"crypto/sha1"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeebo/bencode"
)

// writeCreateFixture lays out a small multi-file payload whose files do not
// end on a piece boundary, so the hybrid path has to insert pad files.
func writeCreateFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string][]byte{
		"a.bin":     bytes.Repeat([]byte{1}, 100<<10),
		"b.bin":     bytes.Repeat([]byte{2}, 200<<10),
		"sub/c.bin": bytes.Repeat([]byte{3}, 40<<10),
	}
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestCreateInfoV2AndHybrid round-trips both new formats through the reader:
// the v2 file tree, the piece layers and, for the hybrid, the v1 pieces and
// the pad files.
func TestCreateInfoV2AndHybrid(t *testing.T) {
	dir := writeCreateFixture(t)
	const pieceLength = 32 << 10
	for _, format := range []string{"v2", "hybrid"} {
		t.Run(format, func(t *testing.T) {
			info, layers, err := CreateInfo(CreationOptions{
				Paths:       []string{dir},
				PieceLength: pieceLength,
				Format:      format,
				Source:      "test",
			})
			if err != nil {
				t.Fatalf("CreateInfo: %v", err)
			}
			if len(info) == 0 {
				t.Fatal("empty info dict")
			}
			data, err := NewBytesWithLayers(info, [][]string{{"http://tracker.test/announce"}}, nil, layers, "")
			if err != nil {
				t.Fatalf("NewBytesWithLayers: %v", err)
			}
			mi, err := New(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if mi.Info.MetaVersion != 2 || !mi.Info.HasV2 {
				t.Fatalf("meta version = %d, has v2 = %v", mi.Info.MetaVersion, mi.Info.HasV2)
			}
			if format == "v2" {
				if len(mi.Info.V2Files) != 3 {
					t.Fatalf("v2 files = %d, want 3", len(mi.Info.V2Files))
				}
				if err := VerifyPieceLayers(mi.Info.V2Files, pieceLength, layers); err != nil {
					t.Fatalf("piece layers: %v", err)
				}
			} else {
				// NewInfo keeps a hybrid on its v1 side; parse the tree it
				// carries to verify the v2 half of the same file.
				var dbg infoType
				if err := bencode.DecodeBytes(info, &dbg); err != nil {
					t.Fatal(err)
				}
				v2Files, err := ParseFileTree(dbg.FileTree)
				if err != nil {
					t.Fatal(err)
				}
				if len(v2Files) != 3 {
					t.Fatalf("hybrid v2 files = %d, want 3", len(v2Files))
				}
				if err := VerifyPieceLayers(v2Files, pieceLength, layers); err != nil {
					t.Fatalf("piece layers: %v", err)
				}
			}
			if mi.Info.V2Hash == [32]byte{} {
				t.Fatal("missing v2 info hash")
			}
			if format == "v2" {
				if !mi.Info.V2 {
					t.Fatal("v2-only torrent not recognised")
				}
				if mi.Info.NumPieces == 0 || len(mi.Info.Files) != 3 {
					t.Fatalf("v2 pieces = %d, files = %d", mi.Info.NumPieces, len(mi.Info.Files))
				}
				return
			}
			if mi.Info.Hash == [20]byte{} {
				t.Fatal("hybrid torrent has no v1 info hash")
			}
			if mi.Info.NumPieces == 0 {
				t.Fatal("hybrid torrent has no v1 pieces")
			}
			pads := 0
			for _, f := range mi.Info.Files {
				if f.Padding || filepath.Base(filepath.Dir(f.Path)) == ".pad" {
					pads++
				}
			}
			if pads == 0 {
				t.Fatal("hybrid multi-file torrent has no pad files")
			}
			verifyHybridV1Pieces(t, dir, mi.Info, pieceLength)
		})
	}
}

// TestCreateInfoSingleFileV2AndHybrid covers the single-file variant, where v1
// stores "length" and the v2 tree has one entry named after the torrent.
func TestCreateInfoSingleFileV2AndHybrid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "single.bin")
	if err := os.WriteFile(path, bytes.Repeat([]byte{7}, 150<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"v2", "hybrid"} {
		info, layers, err := CreateInfo(CreationOptions{
			Paths:       []string{path},
			PieceLength: 32 << 10,
			Format:      format,
		})
		if err != nil {
			t.Fatalf("%s: CreateInfo: %v", format, err)
		}
		data, err := NewBytesWithLayers(info, nil, nil, layers, "")
		if err != nil {
			t.Fatalf("%s: NewBytesWithLayers: %v", format, err)
		}
		mi, err := New(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: New: %v", format, err)
		}
		if format == "v2" {
			if len(mi.Info.V2Files) != 1 || mi.Info.V2Files[0].Path != "single.bin" {
				t.Fatalf("%s: v2 files = %+v", format, mi.Info.V2Files)
			}
			if err := VerifyPieceLayers(mi.Info.V2Files, 32<<10, layers); err != nil {
				t.Fatalf("%s: piece layers: %v", format, err)
			}
		}
		if mi.Info.Length != 150<<10 {
			t.Fatalf("%s: length = %d", format, mi.Info.Length)
		}
	}
}

// TestCreateInfoV2KeepsEmptyPieceLayers pins the BEP 52 field: even a torrent
// whose files all fit in one piece must carry "piece layers", as an empty
// dictionary.
func TestCreateInfoV2KeepsEmptyPieceLayers(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "small.bin"), bytes.Repeat([]byte{9}, 10<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	info, layers, err := CreateInfo(CreationOptions{Paths: []string{dir}, PieceLength: 32 << 10, Format: "v2"})
	if err != nil {
		t.Fatalf("CreateInfo: %v", err)
	}
	if layers == nil || len(layers) != 0 {
		t.Fatalf("layers = %v, want an empty non-nil map", layers)
	}
	data, err := NewBytesWithLayers(info, nil, nil, layers, "")
	if err != nil {
		t.Fatalf("NewBytesWithLayers: %v", err)
	}
	if !bytes.Contains(data, []byte("12:piece layersde")) {
		t.Fatalf("the v2 torrent has no empty piece layers: %s", data)
	}
}

// TestCreateInfoV1StillWorks keeps the default path unchanged.
func TestCreateInfoV1StillWorks(t *testing.T) {
	dir := writeCreateFixture(t)
	info, layers, err := CreateInfo(CreationOptions{Paths: []string{dir}, PieceLength: 32 << 10, Format: "v1", Source: "test"})
	if err != nil {
		t.Fatalf("CreateInfo: %v", err)
	}
	if len(layers) != 0 {
		t.Fatalf("v1 layers = %d, want 0", len(layers))
	}
	data, err := NewBytesWithLayers(info, nil, nil, nil, "")
	if err != nil {
		t.Fatalf("NewBytesWithLayers: %v", err)
	}
	mi, err := New(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if mi.Info.MetaVersion != 0 || mi.Info.HasV2 {
		t.Fatalf("v1 torrent has meta version %d, has v2 %v", mi.Info.MetaVersion, mi.Info.HasV2)
	}
	if mi.Info.Hash == [20]byte{} || mi.Info.NumPieces == 0 {
		t.Fatal("v1 torrent has no info hash or pieces")
	}
}

// verifyHybridV1Pieces rebuilds the v1 piece stream of a hybrid torrent from
// the real files and the pad files it declares, and compares every piece hash:
// a wrong pad size or order would produce a torrent that cannot seed.
func verifyHybridV1Pieces(t *testing.T, dir string, info Info, pieceLength int) {
	t.Helper()
	root := filepath.ToSlash(info.Name)
	var stream []byte
	for _, f := range info.Files {
		if f.Padding {
			stream = append(stream, make([]byte, f.Length)...)
			continue
		}
		name := filepath.ToSlash(f.Path)
		if root != "" && strings.HasPrefix(name, root+"/") {
			name = name[len(root)+1:]
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		stream = append(stream, data...)
	}
	for i := 0; i*pieceLength < len(stream); i++ {
		start := i * pieceLength
		end := start + pieceLength
		if end > len(stream) {
			end = len(stream)
		}
		want := sha1.Sum(stream[start:end])
		got := info.PieceHash(uint32(i))
		if !bytes.Equal(got, want[:]) {
			t.Fatalf("v1 piece %d does not match the pad-aligned stream", i)
		}
	}
}
