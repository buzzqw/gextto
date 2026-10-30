package piece

import (
	"bytes"
	"crypto/sha1"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/merkle"
)

// sha1Piece builds a v1 piece whose hash matches buf.
func sha1Piece(buf []byte) Piece {
	sum := sha1.Sum(buf)
	return Piece{Length: uint32(len(buf)), Hash: sum[:]}
}

// merklePiece builds a v2 piece whose merkle root matches buf, padded to
// leaves 16 KiB leaves as BEP 52 does at a file tail.
func merklePiece(buf []byte, leaves int) Piece {
	hashes := merkle.LeafHashes(buf)
	if leaves < len(hashes) {
		leaves = len(hashes)
	}
	padded := make([][merkle.HashSize]byte, leaves)
	copy(padded, hashes)
	root := merkle.Root(padded)
	return Piece{
		Length:   uint32(len(buf)),
		Hash:     root[:],
		V2:       true,
		V2Leaves: leaves,
	}
}

func testBytes(n int) []byte {
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = byte(i*7 + i/251)
	}
	return buf
}

// TestVerifyAcceptsTheRightLayout: a v1 piece checks as SHA-1, a v2 piece as
// the BEP 52 merkle node over its 16 KiB leaves.
func TestVerifyAcceptsTheRightLayout(t *testing.T) {
	buf := testBytes(3*BlockSize + 1234)

	v1 := sha1Piece(buf)
	if !v1.Verify(buf) {
		t.Fatal("v1 piece did not verify")
	}

	v2 := merklePiece(buf, 4) // three full leaves plus a short tail
	if !v2.Verify(buf) {
		t.Fatal("v2 piece did not verify")
	}
}

// TestVerifyRejectsASingleChangedByte is the point of the whole check: one
// corrupt byte anywhere in the piece must fail it.
func TestVerifyRejectsASingleChangedByte(t *testing.T) {
	buf := testBytes(2 * BlockSize)

	for _, p := range []Piece{sha1Piece(buf), merklePiece(buf, 2)} {
		broken := bytes.Clone(buf)
		broken[len(broken)-1] ^= 0x01
		if p.Verify(broken) {
			t.Fatalf("v2=%v piece verified with a changed byte", p.V2)
		}
	}
}

// TestVerifyRejectsAWrongLength: a buffer shorter or longer than the piece
// fails without hashing, for both layouts.
func TestVerifyRejectsAWrongLength(t *testing.T) {
	buf := testBytes(BlockSize)

	for _, p := range []Piece{sha1Piece(buf), merklePiece(buf, 1)} {
		if p.Verify(buf[:len(buf)-1]) {
			t.Fatalf("v2=%v piece verified with short bytes", p.V2)
		}
		// Hash alone would match: appending the same byte back must not pass.
		longer := append(bytes.Clone(buf), 0)
		if p.Verify(longer) {
			t.Fatalf("v2=%v piece verified with longer bytes", p.V2)
		}
	}
}

// TestVerifyDoesNotUseTheOtherLayoutsHash locks the fix in this change: a v2
// piece is never checked against its SHA-1, and a v1 piece is never checked
// against a merkle root. Before the fix a v2 piece was hashed both ways and the
// first result thrown away, so either digest alone could have looked enough.
func TestVerifyDoesNotUseTheOtherLayoutsHash(t *testing.T) {
	buf := testBytes(2 * BlockSize)

	// A "v2" piece carrying the SHA-1 of its bytes must not verify: the SHA-1
	// is not what a v2 piece is checked with.
	sum := sha1.Sum(buf)
	v2WithSHA1 := Piece{Length: uint32(len(buf)), Hash: sum[:], V2: true, V2Leaves: 2}
	if v2WithSHA1.Verify(buf) {
		t.Fatal("a v2 piece verified against a SHA-1 digest")
	}

	// And a v1 piece carrying the merkle root must not verify either.
	root := merklePiece(buf, 2).Hash
	v1WithRoot := Piece{Length: uint32(len(buf)), Hash: root}
	if v1WithRoot.Verify(buf) {
		t.Fatal("a v1 piece verified against a merkle root")
	}
}

// TestVerifyV2PadsTheTailToItsLeaves: a v2 tail piece hashes fewer blocks than
// its piece layer, so the remaining leaves are zeros (BEP 52). Getting this
// wrong rejects every v2 file whose last piece is short.
func TestVerifyV2PadsTheTailToItsLeaves(t *testing.T) {
	// One leaf of data in a piece whose layer holds four.
	buf := testBytes(BlockSize)
	p := merklePiece(buf, 4)
	if p.V2Leaves != 4 {
		t.Fatalf("V2Leaves = %d, want 4", p.V2Leaves)
	}
	if !p.Verify(buf) {
		t.Fatal("a short v2 tail did not verify against its padded leaves")
	}

	// The padding is part of the hash: a root computed without it must fail.
	unpadded := merklePiece(buf, 1)
	bad := p
	bad.Hash = unpadded.Hash
	if bad.Verify(buf) {
		t.Fatal("a v2 tail verified against an unpadded root")
	}
}

// TestVerifyLeavesThePieceAlone: verification is a read-only question.
func TestVerifyLeavesThePieceAlone(t *testing.T) {
	buf := testBytes(BlockSize)
	p := merklePiece(buf, 1)
	before := bytes.Clone(p.Hash)

	p.Verify(buf)
	p.Verify([]byte("short"))

	if !bytes.Equal(p.Hash, before) {
		t.Fatal("Verify modified the piece hash")
	}
	if !p.V2 || p.Length != uint32(len(buf)) {
		t.Fatal("Verify modified the piece")
	}
}
