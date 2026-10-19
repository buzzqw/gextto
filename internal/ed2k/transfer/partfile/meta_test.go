package partfile

import (
	"bytes"
	"testing"

	"github.com/buzzqw/gextto/internal/ed2k"
)

func TestMetaRoundTrip(t *testing.T) {
	file, _ := ed2k.ParseID("0123456789abcdef0123456789abcdef")
	part, _ := ed2k.ParseID("fedcba9876543210fedcba9876543210")
	meta := NewMeta(file, []ed2k.ID{part}, "The Matrix 1999.mkv", 7_000_000_000, 1234)
	raw, err := meta.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseMeta(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != MetaVersionLarge || got.Modified != 1234 || got.File != file || len(got.Parts) != 1 || got.Parts[0] != part {
		t.Fatalf("meta = %+v", got)
	}
	if got.Name != "The Matrix 1999.mkv" || got.Size != 7_000_000_000 {
		t.Fatalf("name/size = %q/%d", got.Name, got.Size)
	}
}

func TestParseMetaRejectsSplitAndShort(t *testing.T) {
	if _, err := ParseMeta([]byte{MetaVersionSplit}); err == nil {
		t.Fatal("split format must be rejected")
	}
	if _, err := ParseMeta([]byte{0xE0, 0, 0}); err == nil {
		t.Fatal("short buffer must be rejected")
	}
}

func TestMetaSizeHigh(t *testing.T) {
	file, _ := ed2k.ParseID("0123456789abcdef0123456789abcdef")
	meta := &Meta{
		Version: MetaVersion, File: file,
		Tags: []ed2k.Tag{
			{Name: ed2k.TagFileName, Type: ed2k.TagString, Str: "large.bin"},
			{Name: ed2k.TagFileSize, Type: ed2k.TagUint32, Int: 0x00000010},
			{Name: ed2k.TagFileSizeHigh, Type: ed2k.TagUint32, Int: 0x00000001},
		},
	}
	raw, err := meta.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseMeta(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Size != (1<<32)+0x10 {
		t.Fatalf("size = %d", got.Size)
	}
	if !bytes.Contains(raw, []byte("large.bin")) {
		t.Fatal("name missing from the encoded tags")
	}
}
