package metainfo

import (
	"crypto/sha1"
	"errors"
	"testing"
)

func TestInfoRejectsNegativeLengths(t *testing.T) {
	const pieceLength = int64(16384)
	pieces := make([]byte, sha1.Size)

	cases := []struct {
		name  string
		raw   []byte
		parse func([]byte) (*Info, error)
	}{
		{
			name: "v1 single file",
			raw: mustInfo(t, map[string]any{
				"name": "one.bin", "piece length": pieceLength, "pieces": pieces, "length": -1,
			}),
			parse: func(b []byte) (*Info, error) { return NewInfo(b, true, true) },
		},
		{
			name: "v1 negative pad file",
			raw: mustInfo(t, map[string]any{
				"name": "pack", "piece length": pieceLength, "pieces": pieces,
				"files": []any{
					map[string]any{"length": pieceLength, "path": []string{"data.bin"}},
					map[string]any{"length": -1, "path": []string{".pad", "1"}, "attr": "p"},
				},
			}),
			parse: func(b []byte) (*Info, error) { return NewInfo(b, true, true) },
		},
		{
			name: "v2 single file",
			raw: mustInfo(t, map[string]any{
				"name": "one.bin", "piece length": pieceLength, "meta version": 2,
				"file tree": map[string]any{"one.bin": map[string]any{"": map[string]any{"length": -1}}},
			}),
			parse: func(b []byte) (*Info, error) { return NewV2Info(b) },
		},
		{
			name: "v2 negative pad file",
			raw: mustInfo(t, map[string]any{
				"name": "pack", "piece length": pieceLength, "meta version": 2,
				"file tree": map[string]any{"pack": map[string]any{
					"data.bin": map[string]any{"": map[string]any{"length": pieceLength}},
					".pad":     map[string]any{"": map[string]any{"length": -1, "attr": "p"}},
				}},
			}),
			parse: func(b []byte) (*Info, error) { return NewV2Info(b) },
		},
		{
			name: "hybrid negative v2 pad file",
			raw: mustInfo(t, map[string]any{
				"name": "pack", "piece length": pieceLength, "pieces": pieces, "meta version": 2,
				"files": []any{map[string]any{"length": pieceLength, "path": []string{"data.bin"}}},
				"file tree": map[string]any{"pack": map[string]any{
					"data.bin": map[string]any{"": map[string]any{"length": pieceLength}},
					".pad":     map[string]any{"": map[string]any{"length": -1, "attr": "p"}},
				}},
			}),
			parse: func(b []byte) (*Info, error) { return NewInfo(b, true, true) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if info, err := tc.parse(tc.raw); !errors.Is(err, errNegativeFileLength) || info != nil {
				t.Fatalf("parse = (%v, %v), want nil/errNegativeFileLength", info, err)
			}
		})
	}

	v2 := cases[2].raw
	if _, err := ParseInfo(v2, true, true, nil); !errors.Is(err, errNegativeFileLength) {
		t.Fatalf("ParseInfo(v2) = %v, want errNegativeFileLength", err)
	}
}

func TestInfoRejectsFileLengthSumOverflow(t *testing.T) {
	const pieceLength = int64(16384)
	pieces := make([]byte, sha1.Size)

	// Individually valid lengths whose sum wraps to exactly one piece. Before
	// validation, the wrapped total passed the v1 piece-count check.
	v1 := mustInfo(t, map[string]any{
		"name": "pack", "piece length": pieceLength, "pieces": pieces,
		"files": []any{
			map[string]any{"length": maxInt64, "path": []string{"a.bin"}},
			map[string]any{"length": pieceLength + 1, "path": []string{"b.bin"}},
		},
	})
	for name, parse := range map[string]func([]byte) (*Info, error){
		"NewInfo":   func(b []byte) (*Info, error) { return NewInfo(b, true, true) },
		"ParseInfo": func(b []byte) (*Info, error) { return ParseInfo(b, true, true, nil) },
	} {
		t.Run("v1 "+name, func(t *testing.T) {
			if info, err := parse(v1); !errors.Is(err, errFileLengthOverflow) || info != nil {
				t.Fatalf("parse = (%v, %v), want nil/errFileLengthOverflow", info, err)
			}
		})
	}

	v2 := mustInfo(t, map[string]any{
		"name": "pack", "piece length": pieceLength, "meta version": 2,
		"file tree": map[string]any{"pack": map[string]any{
			"a.bin": map[string]any{"": map[string]any{"length": maxInt64}},
			"b.bin": map[string]any{"": map[string]any{"length": pieceLength + 1}},
		}},
	})
	for name, parse := range map[string]func([]byte) (*Info, error){
		"NewV2Info": func(b []byte) (*Info, error) { return NewV2Info(b) },
		"ParseInfo": func(b []byte) (*Info, error) { return ParseInfo(b, true, true, nil) },
	} {
		t.Run("v2 "+name, func(t *testing.T) {
			if info, err := parse(v2); !errors.Is(err, errFileLengthOverflow) || info != nil {
				t.Fatalf("parse = (%v, %v), want nil/errFileLengthOverflow", info, err)
			}
		})
	}
}

func TestInfoRejectsPieceFileCountMismatch(t *testing.T) {
	const pieceLength = int64(16384)
	onePiece := make([]byte, sha1.Size)
	twoPieces := make([]byte, 2*sha1.Size)

	cases := []struct {
		name  string
		raw   []byte
		parse func([]byte) (*Info, error)
	}{
		{
			name: "too few v1 pieces",
			raw: mustInfo(t, map[string]any{
				"name": "one.bin", "piece length": pieceLength, "pieces": onePiece, "length": 2*pieceLength + 1,
			}),
			parse: func(b []byte) (*Info, error) { return NewInfo(b, true, true) },
		},
		{
			name: "too many v1 pieces",
			raw: mustInfo(t, map[string]any{
				"name": "one.bin", "piece length": pieceLength, "pieces": twoPieces, "length": 1,
			}),
			parse: func(b []byte) (*Info, error) { return ParseInfo(b, true, true, nil) },
		},
		{
			name: "v1 file list mismatch",
			raw: mustInfo(t, map[string]any{
				"name": "pack", "piece length": pieceLength, "pieces": onePiece,
				"files": []any{map[string]any{"length": pieceLength + 1, "path": []string{"data.bin"}}},
			}),
			parse: func(b []byte) (*Info, error) { return NewInfo(b, true, true) },
		},
		{
			name: "hybrid v1 fields mismatch",
			raw: mustInfo(t, map[string]any{
				"name": "pack", "piece length": pieceLength, "pieces": onePiece, "meta version": 2,
				"files": []any{map[string]any{"length": pieceLength + 1, "path": []string{"data.bin"}}},
				"file tree": map[string]any{"pack": map[string]any{
					"data.bin": map[string]any{"": map[string]any{"length": pieceLength + 1}},
				}},
			}),
			parse: func(b []byte) (*Info, error) { return NewV2Info(b) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if info, err := tc.parse(tc.raw); !errors.Is(err, errInvalidPieceData) || info != nil {
				t.Fatalf("parse = (%v, %v), want nil/errInvalidPieceData", info, err)
			}
		})
	}
}

func TestV2InfoRejectsPieceCountOverflow(t *testing.T) {
	const pieceLength = int64(16384)
	oneTooMany := int64(maxPieceCount+1) * pieceLength
	halfLimit := int64(1<<31) * pieceLength

	cases := []struct {
		name   string
		length []int64
	}{
		{"single file", []int64{oneTooMany}},
		{"sum across files", []int64{halfLimit, halfLimit}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := make(map[string]any, len(tc.length))
			for i, length := range tc.length {
				name := string(rune('a'+i)) + ".bin"
				files[name] = map[string]any{"": map[string]any{"length": length}}
			}
			raw := mustInfo(t, map[string]any{
				"name": "pack", "piece length": pieceLength, "meta version": 2,
				"file tree": map[string]any{"pack": files},
			})
			if info, err := NewV2Info(raw); !errors.Is(err, errPieceCountOverflow) || info != nil {
				t.Fatalf("NewV2Info = (%v, %v), want nil/errPieceCountOverflow", info, err)
			}
			if info, err := ParseInfo(raw, true, true, nil); !errors.Is(err, errPieceCountOverflow) || info != nil {
				t.Fatalf("ParseInfo = (%v, %v), want nil/errPieceCountOverflow", info, err)
			}
		})
	}
}
