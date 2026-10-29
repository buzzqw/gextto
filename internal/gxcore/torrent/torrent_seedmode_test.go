package torrent

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// seedModePieces writes content to file.bin under a fresh directory and
// returns its pieces, opened from storage like a seed-mode torrent's.
func seedModePieces(tb testing.TB, content []byte, pieceLength int) []piece.Piece {
	tb.Helper()
	root := tb.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.bin"), content, 0o644); err != nil {
		tb.Fatal(err)
	}
	info := testInfoBytes(tb, content, pieceLength)
	return piece.NewPieces(info, testFiles(tb, root, info))
}

// TestHashStoredPieceCatchesDamage: the lazy check accepts an intact piece
// and refuses a piece with one flipped byte.
func TestHashStoredPieceCatchesDamage(t *testing.T) {
	const pieceLength = 32 << 10
	content := make([]byte, 3*pieceLength)
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	pieces := seedModePieces(t, content, pieceLength)
	for i := range pieces {
		if ok, err := hashStoredPiece(&pieces[i]); !ok || err != nil {
			t.Fatalf("intact piece %d refused: ok=%v err=%v", i, ok, err)
		}
	}
	damaged := append([]byte(nil), content...)
	damaged[pieceLength+7] ^= 0x01
	bad := seedModePieces(t, damaged, pieceLength)
	// The hashes come from the intact content, the data from the damaged one.
	info := testInfoBytes(t, content, pieceLength)
	for i := range bad {
		bad[i].Hash = info.PieceHash(uint32(i))
	}
	if ok, _ := hashStoredPiece(&bad[1]); ok {
		t.Fatal("a damaged piece passed the lazy check")
	}
	if ok, _ := hashStoredPiece(&bad[0]); !ok {
		t.Fatal("an intact neighbour of the damaged piece was refused")
	}
}

// BenchmarkSeedModeLazyHash is the extra latency of the first upload of a
// piece in seed mode: read the whole piece back and hash it before the first
// block leaves. The file sits in the page cache (a just-written test file),
// so this is the CPU side; a cold read adds the disk time of one piece.
func BenchmarkSeedModeLazyHash(b *testing.B) {
	for _, size := range []int{256 << 10, 1 << 20, 4 << 20, 16 << 20} {
		b.Run(fmt.Sprintf("%dKiB", size>>10), func(b *testing.B) {
			content := make([]byte, 2*size)
			if _, err := rand.Read(content); err != nil {
				b.Fatal(err)
			}
			pieces := seedModePieces(b, content, size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if ok, err := hashStoredPiece(&pieces[0]); !ok || err != nil {
					b.Fatalf("ok=%v err=%v", ok, err)
				}
			}
		})
	}
}
