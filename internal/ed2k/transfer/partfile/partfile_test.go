package partfile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/md4"
)

func md4Of(value string) [16]byte {
	hash := md4.New()
	_, _ = hash.Write([]byte(value))
	var out [16]byte
	copy(out[:], hash.Sum(nil))
	return out
}

func TestHashReaderParts(t *testing.T) {
	hashes, err := hashReader(bytes.NewReader([]byte("abcdefgh")), 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	if hashes.File != md4Of("abcdefgh") {
		t.Fatalf("file hash = %x", hashes.File)
	}
	if len(hashes.Parts) != 2 || hashes.Parts[0] != md4Of("abcd") || hashes.Parts[1] != md4Of("efgh") {
		t.Fatalf("parts = %x", hashes.Parts)
	}
}

func TestHashReaderLastPartIsShort(t *testing.T) {
	hashes, err := hashReader(bytes.NewReader([]byte("abc")), 3, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hashes.Parts) != 2 || hashes.Parts[0] != md4Of("ab") || hashes.Parts[1] != md4Of("c") {
		t.Fatalf("parts = %x", hashes.Parts)
	}
}

func TestHashFileMatchesReader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.bin")
	body := []byte("the quick brown fox")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := HashReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if got.File != want.File || len(got.Parts) != len(want.Parts) || got.Parts[0] != want.Parts[0] {
		t.Fatalf("HashFile = %x / %x, want %x / %x", got.File, got.Parts, want.File, want.Parts)
	}
}

func TestPartGeometry(t *testing.T) {
	if PartCount(0) != 0 || PartCount(PartSize) != 1 || PartCount(PartSize+1) != 2 {
		t.Fatalf("PartCount = %d/%d/%d", PartCount(0), PartCount(PartSize), PartCount(PartSize+1))
	}
	if PartLength(PartSize+10, 0) != PartSize || PartLength(PartSize+10, 1) != 10 {
		t.Fatalf("PartLength = %d/%d", PartLength(PartSize+10, 0), PartLength(PartSize+10, 1))
	}
}
