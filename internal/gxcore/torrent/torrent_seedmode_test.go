package torrent

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
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

// TestSeedModeParkedRequestsAreBounded: a peer parks at most MaxRequestsIn
// requests for unchecked pieces; the rest is dropped (refused with the fast
// extension), so the parked set cannot grow without limit.
func TestSeedModeParkedRequestsAreBounded(t *testing.T) {
	tr := &torrent{
		session:      &Session{config: Config{MaxRequestsIn: 2}},
		seedMode:     true,
		seedVerified: bitfield.New(8),
		seedPending:  map[uint32][]seedModeRequest{},
		seedParked:   map[*peer.Peer]int{},
		seedHashing:  map[uint32]struct{}{},
	}
	// Every hash slot busy, so no hash starts in the test.
	for i := uint32(100); i < 100+seedModeMaxHashing; i++ {
		tr.seedHashing[i] = struct{}{}
	}
	pe := &peer.Peer{}
	for i := uint32(0); i < 5; i++ {
		tr.queueSeedModeRequest(pe, peerprotocol.RequestMessage{Index: i, Length: 16384})
	}
	parked := 0
	for _, reqs := range tr.seedPending {
		parked += len(reqs)
	}
	if parked != 2 || tr.seedParked[pe] != 2 {
		t.Fatalf("parked %d requests (count %d), want 2", parked, tr.seedParked[pe])
	}
}

// TestSeedModeStreamWaitsForTheHash: a player reading a piece claimed in seed
// mode does not get it before its hash, and asking for it is enough to start
// that hash (no peer has to request it).
func TestSeedModeStreamWaitsForTheHash(t *testing.T) {
	const pieceLength = 32 << 10
	content := make([]byte, 3*pieceLength)
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	pieces := seedModePieces(t, content, pieceLength)
	for i := range pieces {
		pieces[i].Done = true
	}
	tr := &torrent{
		pieces:       pieces,
		seedMode:     true,
		seedVerified: bitfield.New(3),
		seedPending:  map[uint32][]seedModeRequest{},
		seedParked:   map[*peer.Peer]int{},
		seedHashing:  map[uint32]struct{}{},
		seedHashC:    make(chan seedHashResult, 1),
		closeC:       make(chan struct{}),
	}
	if tr.piecesDone(0, 1) {
		t.Fatal("an unchecked piece must not be ready for a stream")
	}
	if _, ok := tr.seedHashing[0]; !ok {
		t.Fatal("asking for the piece must start its hash")
	}
	tr.handleSeedHashDone(<-tr.seedHashC)
	if !tr.piecesDone(0, 1) {
		t.Fatal("a piece whose hash matched must be ready")
	}
	if !tr.seedMode || tr.seedVerified.Has(1) {
		t.Fatal("only the asked piece is checked; seed mode goes on")
	}
}
