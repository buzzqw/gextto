package partfile

import (
	"bytes"
	"crypto/sha1"
	"testing"
)

func TestAICHOneBlock(t *testing.T) {
	data := []byte("aich block")
	root, err := aichRootReader(bytes.NewReader(data), int64(len(data)), 16, 16)
	if err != nil {
		t.Fatal(err)
	}
	want := sha1.Sum(data)
	if root != AICHRoot(want) {
		t.Fatalf("root = %x, want %x", root, want)
	}
}

func TestAICHTwoBlocks(t *testing.T) {
	data := []byte("abcdefgh")
	root, err := aichRootReader(bytes.NewReader(data), 8, 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	left := sha1.Sum([]byte("abcd"))
	right := sha1.Sum([]byte("efgh"))
	joined := append(append([]byte{}, left[:]...), right[:]...)
	want := sha1.Sum(joined)
	if root != AICHRoot(want) {
		t.Fatalf("root = %x, want %x", root, want)
	}
}

func TestAICHThreeBlocks(t *testing.T) {
	data := []byte("abcdefghijkl")
	root, err := aichRootReader(bytes.NewReader(data), 12, 12, 4)
	if err != nil {
		t.Fatal(err)
	}
	b1 := sha1.Sum([]byte("abcd"))
	b2 := sha1.Sum([]byte("efgh"))
	b3 := sha1.Sum([]byte("ijkl"))
	left := sha1.Sum(append(append([]byte{}, b1[:]...), b2[:]...))
	want := sha1.Sum(append(append([]byte{}, left[:]...), b3[:]...))
	if root != AICHRoot(want) {
		t.Fatalf("root = %x, want %x", root, want)
	}
}

func TestAICHMultiPart(t *testing.T) {
	// Three parts of four bytes each, two-byte blocks: the top tree joins the
	// three part roots with the same left-biased rule.
	data := []byte("abcdefghijkl")
	root, err := aichRootReader(bytes.NewReader(data), 12, 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	part1 := aichPartHash([]byte("abcd"), 2)
	part2 := aichPartHash([]byte("efgh"), 2)
	part3 := aichPartHash([]byte("ijkl"), 2)
	left := sha1.Sum(append(append([]byte{}, part1[:]...), part2[:]...))
	want := sha1.Sum(append(append([]byte{}, left[:]...), part3[:]...))
	if root != AICHRoot(want) {
		t.Fatalf("root = %x, want %x", root, want)
	}
}

func TestAICHRejectsEmpty(t *testing.T) {
	if _, err := AICHRootReader(bytes.NewReader(nil), 0); err == nil {
		t.Fatal("empty file must fail")
	}
}
