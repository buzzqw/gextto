package known

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
)

func testEntry(name string, size int64, seed byte) Entry {
	entry := Entry{Name: name, Size: size, MTime: time.Unix(1700000000, 0).UTC()}
	entry.Hash[0] = seed
	parts := (size + 9_728_000 - 1) / 9_728_000
	for i := int64(0); i < parts; i++ {
		var part ed2k.ID
		part[0] = seed
		part[15] = byte(i)
		entry.Parts = append(entry.Parts, part)
	}
	return entry
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known.met")
	entries := []Entry{
		testEntry("film.mkv", 12_000_000, 1),
		testEntry("episodio àèìòù.mkv", 400_000, 2),
	}
	if err := Save(path, entries); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("entries = %d, want 2", len(loaded))
	}
	if loaded[0].Name != "episodio àèìòù.mkv" {
		t.Fatalf("entries not sorted by name: %q", loaded[0].Name)
	}
	for _, entry := range loaded {
		if entry.Hash == (ed2k.ID{}) || entry.Size <= 0 {
			t.Fatalf("incomplete entry: %+v", entry)
		}
		if want := PartHashCount(entry.Size); len(entry.Parts) != want {
			t.Fatalf("%s: %d part hashes, want %d", entry.Name, len(entry.Parts), want)
		}
		if !entry.MTime.Equal(time.Unix(1700000000, 0).UTC()) {
			t.Fatalf("mtime = %v", entry.MTime)
		}
	}
}

func TestLoadMissingFile(t *testing.T) {
	entries, err := Load(filepath.Join(t.TempDir(), "known.met"))
	if err != nil || entries != nil {
		t.Fatalf("missing file = %v, %v", entries, err)
	}
}

func TestParseRejectsTruncated(t *testing.T) {
	if _, err := parse([]byte{headerMD4, 1, 0}); err == nil {
		t.Fatal("truncated header accepted")
	}
	path := filepath.Join(t.TempDir(), "known.met")
	if err := Save(path, []Entry{testEntry("a.bin", 20_000_000, 3)}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parse(data[:len(data)-10]); err == nil {
		t.Fatal("truncated file accepted")
	}
	if _, err := parse([]byte("garbage")); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestPartsForExactMultiple(t *testing.T) {
	parts := []ed2k.ID{{1}}
	got := PartsFor(9_728_000, parts)
	if len(got) != 2 {
		t.Fatalf("parts = %d, want 2 (the last is the empty part)", len(got))
	}
	// The appended hash is the MD4 of the empty string.
	if got[1][0] != 0x31 || got[1][15] != 0xC0 {
		t.Fatalf("empty part hash = %x", got[1])
	}
	if got := PartsFor(400_000, []ed2k.ID{{9}}); len(got) != 0 {
		t.Fatalf("small file kept %d part hashes", len(got))
	}
	if got := PartsFor(12_000_000, []ed2k.ID{{1}, {2}}); len(got) != 2 {
		t.Fatalf("multi-part file = %d hashes, want 2", len(got))
	}
}

func TestLargeFileHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known.met")
	if err := Save(path, []Entry{testEntry("big.iso", 5<<30, 4)}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if data[0] != headerMD4Hi {
		t.Fatalf("header = %#x, want 0x%x", data[0], headerMD4Hi)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Size != 5<<30 {
		t.Fatalf("large entry lost: %+v", loaded)
	}
}
