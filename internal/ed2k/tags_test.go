package ed2k

import (
	"encoding/binary"
	"testing"
)

func TestTagsRoundTripBigEndian(t *testing.T) {
	w := newWriter(binary.BigEndian)
	w.u8(4)
	w.tag(Tag{Name: TagFileName, Type: TagString, Str: "un nome.mkv"})
	w.tag(Tag{Name: TagFileSize, Type: TagUint64, Int: 5 << 30})
	w.tag(Tag{Name: TagSources, Type: TagUint32, Int: 12})
	w.tag(Tag{Name: TagFileType, Type: TagString, Str: "Video"})

	r := newReader(w.buf, binary.BigEndian)
	count, err := r.u8()
	if err != nil || count != 4 {
		t.Fatalf("count = %d, %v", count, err)
	}
	tags := make([]Tag, 0, count)
	for i := 0; i < int(count); i++ {
		tag, err := r.tag()
		if err != nil {
			t.Fatalf("tag %d: %v", i, err)
		}
		tags = append(tags, tag)
	}
	byID := tagsByID(tags)
	if got, _ := tagString(byID, TagFileName); got != "un nome.mkv" {
		t.Fatalf("name = %q", got)
	}
	if got, ok := fileSizeFromTags(byID); !ok || got != 5<<30 {
		t.Fatalf("size = %d, %v", got, ok)
	}
	if got, _ := tagUint(byID, TagSources); got != 12 {
		t.Fatalf("sources = %d", got)
	}
}

func TestTagsLittleEndianServer(t *testing.T) {
	w := newWriter(binary.LittleEndian)
	w.tag(Tag{Name: TagFileName, Type: TagString, Str: "film.avi"})
	w.tag(Tag{Name: TagFileSize, Type: TagUint32, Int: 1234})
	r := newReader(w.buf, binary.LittleEndian)
	var tags []Tag
	for r.remaining() > 0 {
		tag, err := r.tag()
		if err != nil {
			t.Fatalf("tag: %v", err)
		}
		tags = append(tags, tag)
	}
	byID := tagsByID(tags)
	if got, _ := tagString(byID, TagFileName); got != "film.avi" {
		t.Fatalf("name = %q", got)
	}
	if got, ok := fileSizeFromTags(byID); !ok || got != 1234 {
		t.Fatalf("size = %d, %v", got, ok)
	}
}

func TestTagDecodingRejectsTruncated(t *testing.T) {
	w := newWriter(binary.BigEndian)
	w.tag(Tag{Name: TagFileName, Type: TagString, Str: "abc"})
	for length := 0; length < len(w.buf); length++ {
		r := newReader(w.buf[:length], binary.BigEndian)
		if _, err := r.tag(); err == nil {
			t.Fatalf("truncated tag of length %d accepted", length)
		}
	}
}
