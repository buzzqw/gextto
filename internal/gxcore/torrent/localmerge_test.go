package torrent

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/allocator"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bitfield"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
	"github.com/buzzqw/gextto/internal/gxcore/internal/storage/filestorage"
	"github.com/zeebo/bencode"
)

// testInfoBytes builds a single-file v1 info dictionary for content.
func testInfoBytes(t *testing.T, content []byte, pieceLength int) *metainfo.Info {
	t.Helper()
	var pieces []byte
	for off := 0; off < len(content); off += pieceLength {
		end := min(off+pieceLength, len(content))
		sum := sha1.Sum(content[off:end])
		pieces = append(pieces, sum[:]...)
	}
	info := struct {
		Name        string `bencode:"name"`
		Length      int64  `bencode:"length"`
		PieceLength int    `bencode:"piece length"`
		Pieces      []byte `bencode:"pieces"`
	}{Name: "file.bin", Length: int64(len(content)), PieceLength: pieceLength, Pieces: pieces}
	raw, err := bencode.EncodeBytes(info)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := metainfo.NewInfo(raw, true, true)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// testFiles opens one storage file per info file under root.
func testFiles(t testing.TB, root string, info *metainfo.Info) []allocator.File {
	t.Helper()
	sto, err := filestorage.New(root, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	files := make([]allocator.File, len(info.Files))
	for i, f := range info.Files {
		file, _, err := sto.Open(f.Path, f.Length)
		if err != nil {
			t.Fatal(err)
		}
		files[i] = allocator.File{Storage: file, Name: f.Path, Padding: f.Padding}
	}
	return files
}

func TestPrefillLocalDataReusesIdenticalLayout(t *testing.T) {
	const pieceLength = 16384
	content := make([]byte, 40000) // three pieces
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	info := testInfoBytes(t, content, pieceLength)

	// B holds the full content and reports every piece as verified.
	filesB := testFiles(t, t.TempDir(), info)
	if _, err := filesB[0].Storage.WriteAt(content, 0); err != nil {
		t.Fatal(err)
	}
	bf := bitfield.New(info.NumPieces)
	for i := uint32(0); i < info.NumPieces; i++ {
		bf.Set(i)
	}
	b := &torrent{pieces: piece.NewPieces(info, filesB), bitfield: bf}
	b.setInfo(info)
	b.publishLocalData()

	// A is empty, with the same layout.
	filesA := testFiles(t, t.TempDir(), info)
	a := &torrent{pieces: piece.NewPieces(info, filesA)}
	a.setInfo(info)

	if got := a.prefillFrom([]*Torrent{{torrent: b}}); got != int(info.NumPieces) {
		t.Fatalf("merged %d pieces, want %d", got, info.NumPieces)
	}
	got := make([]byte, len(content))
	if _, err := filesA[0].Storage.ReadAt(got, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("A's file does not hold the reused content")
	}
}

func TestPrefillLocalDataSkipsDifferentContent(t *testing.T) {
	const pieceLength = 16384
	infoA := testInfoBytes(t, make([]byte, 40000), pieceLength)
	other := make([]byte, 40000)
	if _, err := rand.Read(other); err != nil {
		t.Fatal(err)
	}
	infoB := testInfoBytes(t, other, pieceLength)

	filesB := testFiles(t, t.TempDir(), infoB)
	if _, err := filesB[0].Storage.WriteAt(other, 0); err != nil {
		t.Fatal(err)
	}
	bf := bitfield.New(infoB.NumPieces)
	for i := uint32(0); i < infoB.NumPieces; i++ {
		bf.Set(i)
	}
	b := &torrent{pieces: piece.NewPieces(infoB, filesB), bitfield: bf}
	b.setInfo(infoB)
	b.publishLocalData()

	a := &torrent{pieces: piece.NewPieces(infoA, testFiles(t, t.TempDir(), infoA))}
	a.setInfo(infoA)
	if got := a.prefillFrom([]*Torrent{{torrent: b}}); got != 0 {
		t.Fatalf("merged %d pieces with a different content, want 0", got)
	}
}

func TestPrefillLocalDataRespectsPrivacy(t *testing.T) {
	const pieceLength = 16384
	content := make([]byte, 40000)
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	info := testInfoBytes(t, content, pieceLength)

	filesB := testFiles(t, t.TempDir(), info)
	if _, err := filesB[0].Storage.WriteAt(content, 0); err != nil {
		t.Fatal(err)
	}
	bf := bitfield.New(info.NumPieces)
	for i := uint32(0); i < info.NumPieces; i++ {
		bf.Set(i)
	}
	b := &torrent{pieces: piece.NewPieces(info, filesB), bitfield: bf}
	b.setInfo(info)
	snap := &localDataSnapshot{pieces: b.pieces, private: true, length: info.Length, pieceLength: info.PieceLength}
	b.localData.Store(snap)

	a := &torrent{pieces: piece.NewPieces(info, testFiles(t, t.TempDir(), info))}
	a.setInfo(info)
	if got := a.prefillFrom([]*Torrent{{torrent: b}}); got != 0 {
		t.Fatalf("merged %d pieces from a private swarm, want 0", got)
	}
}

// TestPrefillLocalDataSkipsTorrentWithoutBitfield: a candidate that has not
// verified anything yet (no bitfield: allocating or checking) is not read.
func TestPrefillLocalDataSkipsTorrentWithoutBitfield(t *testing.T) {
	const pieceLength = 16384
	content := make([]byte, 40000)
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}
	info := testInfoBytes(t, content, pieceLength)
	filesB := testFiles(t, t.TempDir(), info)
	if _, err := filesB[0].Storage.WriteAt(content, 0); err != nil {
		t.Fatal(err)
	}
	b := &torrent{pieces: piece.NewPieces(info, filesB)} // no bitfield
	b.setInfo(info)
	b.publishLocalData()
	a := &torrent{pieces: piece.NewPieces(info, testFiles(t, t.TempDir(), info))}
	a.setInfo(info)
	if got := a.prefillFrom([]*Torrent{{torrent: b}}); got != 0 {
		t.Fatalf("merged %d pieces from a torrent with no verified piece", got)
	}
}
