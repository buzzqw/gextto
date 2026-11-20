package metainfo

import (
	"crypto/sha256"
	"testing"
)

// This file is the small torture corpus for the shapes four independent
// clients fixed in 2026: v2 files with a short tail between two files, empty
// files after a piece, and a hybrid with BEP 47 padding. Parsing and the piece
// map must agree with BEP 52; no shape may panic or count a piece twice.

// TestV2EmptyFilesAfterAPiece: a one-piece file followed by two empty files is
// still one piece, and the empty files keep their place in the file list.
func TestV2EmptyFilesAfterAPiece(t *testing.T) {
	root := sha256.Sum256(make([]byte, 16384))
	raw := mustInfo(t, map[string]any{
		"name": "x", "piece length": 16384, "meta version": 2,
		"file tree": map[string]any{"x": map[string]any{
			"a": map[string]any{"": map[string]any{"length": 16384, "pieces root": string(root[:])}},
			"b": map[string]any{"": map[string]any{"length": 0}},
			"c": map[string]any{"": map[string]any{"length": 0}},
		}},
	})
	info, err := NewV2Info(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if info.NumPieces != 1 {
		t.Fatalf("NumPieces = %d, want 1", info.NumPieces)
	}
	if !info.MultiFile {
		t.Fatal("a tree with a folder is multi-file")
	}
	if len(info.Files) != 3 || info.Files[1].Length != 0 || info.Files[2].Length != 0 {
		t.Fatalf("files = %+v, want the two empty files kept", info.Files)
	}
	if err := info.AttachV2Pieces(nil); err != nil {
		t.Fatalf("attach pieces: %v", err)
	}
	if got := len(info.pieces) / 32; got != 1 {
		t.Fatalf("piece hashes = %d, want 1", got)
	}
}

// TestV2ShortTailBetweenFiles: every v2 file starts on a piece boundary, so
// two 100-byte files in a 16 KiB-piece torrent are two short pieces, not one.
func TestV2ShortTailBetweenFiles(t *testing.T) {
	rootA := sha256.Sum256(make([]byte, 100))
	rootB := sha256.Sum256(make([]byte, 100))
	raw := mustInfo(t, map[string]any{
		"name": "pack", "piece length": 16384, "meta version": 2,
		"file tree": map[string]any{"pack": map[string]any{
			"a": map[string]any{"": map[string]any{"length": 100, "pieces root": string(rootA[:])}},
			"b": map[string]any{"": map[string]any{"length": 100, "pieces root": string(rootB[:])}},
		}},
	})
	info, err := NewV2Info(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if info.NumPieces != 2 {
		t.Fatalf("NumPieces = %d, want 2 short pieces", info.NumPieces)
	}
	if err := info.AttachV2Pieces(nil); err != nil {
		t.Fatalf("attach pieces: %v", err)
	}
	if got := len(info.pieces) / 32; got != 2 {
		t.Fatalf("piece hashes = %d, want 2", got)
	}
}

// TestHybridPaddingAndEmptyFile: a hybrid info with v1 files (a padded file
// plus an empty one) keeps the pad marked and the empty file in place, and
// both v1 and v2 identities are computed.
func TestHybridPaddingAndEmptyFile(t *testing.T) {
	root := sha256.Sum256(make([]byte, 10000))
	pieces := make([]byte, 20) // one SHA-1 piece
	raw := mustInfo(t, map[string]any{
		"name": "hybrid", "piece length": 16384, "pieces": pieces,
		"meta version": 2,
		"files": []any{
			map[string]any{"length": 10000, "path": []string{"a.bin"}},
			map[string]any{"length": 6384, "path": []string{".pad", "6384"}, "attr": "p"},
			map[string]any{"length": 0, "path": []string{"empty.bin"}},
		},
		"file tree": map[string]any{"hybrid": map[string]any{
			"a.bin":     map[string]any{"": map[string]any{"length": 10000, "pieces root": string(root[:])}},
			"empty.bin": map[string]any{"": map[string]any{"length": 0}},
		}},
	})
	info, err := NewInfo(raw, true, true)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if info.MetaVersion != 2 || !info.HasV2 {
		t.Fatalf("MetaVersion=%d HasV2=%v, want a hybrid", info.MetaVersion, info.HasV2)
	}
	if !info.MultiFile {
		t.Fatal("a hybrid with a files list is multi-file")
	}
	if info.Padding != 6384 {
		t.Fatalf("Padding = %d, want 6384", info.Padding)
	}
	if len(info.Files) != 3 {
		t.Fatalf("files = %+v, want three entries", info.Files)
	}
	if !info.Files[1].Padding {
		t.Fatalf("the pad file is not marked: %+v", info.Files[1])
	}
	if info.Files[2].Length != 0 {
		t.Fatalf("the empty file is not kept: %+v", info.Files[2])
	}
	if info.NumPieces != 1 || info.Length != 16384 {
		t.Fatalf("pieces=%d length=%d, want 1/16384", info.NumPieces, info.Length)
	}
}
