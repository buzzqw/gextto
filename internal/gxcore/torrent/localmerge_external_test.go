package torrent

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// TestPrefillExternalAdoptsOnlyVerifiedPieces reads an external copy that is
// half written (the rest zeros, then garbage): only the pieces whose bytes
// match the torrent's hashes are adopted, and they land in its storage.
func TestPrefillExternalAdoptsOnlyVerifiedPieces(t *testing.T) {
	const pieceLength = 16384
	content := make([]byte, 5*pieceLength+1000) // six pieces
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	info := testInfoBytes(t, content, pieceLength)
	files := testFiles(t, t.TempDir(), info)
	a := &torrent{pieces: piece.NewPieces(info, files), files: files}
	a.setInfo(info)

	// The external file: pieces 0 and 1 right, piece 2 not written yet
	// (zeros), piece 3 damaged, pieces 4 and 5 right.
	external := append([]byte(nil), content...)
	clear(external[2*pieceLength : 3*pieceLength])
	external[3*pieceLength+7] ^= 0xff
	path := filepath.Join(t.TempDir(), "001.part")
	if err := os.WriteFile(path, external, 0o644); err != nil {
		t.Fatal(err)
	}
	merged := map[uint32]bool{}
	before := externalPiecesTotal.Load()
	if got := a.prefillExternal([]ExternalSource{{FileIndex: 0, Path: path}}, merged, nil, nil); got != 4 {
		t.Fatalf("adopted %d pieces, want 4", got)
	}
	for _, i := range []uint32{0, 1, 4, 5} {
		if !merged[i] {
			t.Errorf("piece %d not adopted", i)
		}
	}
	if merged[2] || merged[3] {
		t.Fatal("an unwritten or damaged piece was adopted")
	}
	if got := externalPiecesTotal.Load() - before; got != 4 {
		t.Fatalf("external counter += %d", got)
	}
	got := make([]byte, pieceLength)
	if _, err := files[0].Storage.ReadAt(got, 4*pieceLength); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content[4*pieceLength:5*pieceLength]) {
		t.Fatal("an adopted piece did not land in the torrent's storage")
	}
	// A missing or wrong-index source adopts nothing and does not fail.
	if got := a.prefillExternal([]ExternalSource{{FileIndex: 3, Path: path}, {FileIndex: 0, Path: path + ".missing"}}, map[uint32]bool{}, nil, nil); got != 0 {
		t.Fatalf("a bad source adopted %d pieces", got)
	}
}
