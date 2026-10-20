package amule

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

func TestPacketRoundTrip(t *testing.T) {
	packet := Packet{Op: OpSearchStart, Tags: []Tag{
		IntTag(TagSearchType, SearchKad,
			StringTag(TagSearchName, "Tracker S04E01"),
			IntTag(TagSearchMinSize, 5_000_000_000),
			EmptyTag(TagPrefsConnections, IntTag(TagConnMaxDL, 300)),
		),
		HashTag(TagPartfile, [16]byte{1, 2, 3}),
	}}
	raw, err := packet.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(raw[:4]) != flagBase || int(binary.BigEndian.Uint32(raw[4:8])) != len(raw)-8 {
		t.Fatalf("bad header % x", raw[:8])
	}
	decoded, err := ReadPacket(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	search, ok := decoded.Tag(TagSearchType)
	if !ok || search.Int() != SearchKad || search.ChildString(TagSearchName) != "Tracker S04E01" {
		t.Fatalf("search tag = %+v", search)
	}
	if size, _ := search.ChildInt(TagSearchMinSize); size != 5_000_000_000 {
		t.Fatalf("64-bit child = %d", size)
	}
	nested, _ := search.Child(TagPrefsConnections)
	if limit, _ := nested.ChildInt(TagConnMaxDL); limit != 300 {
		t.Fatalf("nested child = %+v", nested)
	}
	hash, _ := decoded.Tag(TagPartfile)
	if value, ok := hash.Hash(); !ok || value[2] != 3 {
		t.Fatalf("hash tag = %+v", hash)
	}
}

// TestTagLengthCountsChildHeaders pins the core's length rule: a tag's length
// is its data plus every child with a 7-byte header (+2 when the child has
// children), not the encoded size of the child count.
func TestTagLengthCountsChildHeaders(t *testing.T) {
	leaf := IntTag(TagConnMaxDL, 1)                // 1 byte of data
	parent := EmptyTag(TagPrefsConnections, leaf)  // 0 + (1 + 7)
	root := StringTag(TagSearchName, "ab", parent) // 3 + (8 + 7 + 2)
	if got := parent.wireLen(); got != 8 {
		t.Fatalf("parent length = %d", got)
	}
	if got := root.wireLen(); got != 20 {
		t.Fatalf("root length = %d", got)
	}
}

func TestDecodeUTF8Numbers(t *testing.T) {
	// opcode, tag count 1, tag name (TagStatsDLSpeed<<1), type uint32,
	// length 4, then 4 raw data bytes: the numbers of the framing are varints,
	// the tag data stays big-endian.
	var payload []byte
	payload = append(payload, encodeVarint(OpStats)...)
	payload = append(payload, encodeVarint(1)...)
	payload = append(payload, encodeVarint(TagStatsDLSpeed<<1)...)
	payload = append(payload, encodeVarint(TypeUint32)...)
	payload = append(payload, encodeVarint(4)...)
	payload = binary.BigEndian.AppendUint32(payload, 123456)
	packet, err := DecodePayload(flagBase|flagUTF8Numbers, payload)
	if err != nil {
		t.Fatal(err)
	}
	tag, _ := packet.Tag(TagStatsDLSpeed)
	if packet.Op != OpStats || tag.Int() != 123456 {
		t.Fatalf("packet = %+v", packet)
	}
}

func TestVarintRoundTrip(t *testing.T) {
	for _, value := range []uint32{0, 0x7F, 0x80, 0x7FF, 0x800, 0xFFFF, 0x10000, 0x1FFFFF, 0x3FFFFFF, 0x7FFFFFFF} {
		encoded := encodeVarint(value)
		decoded, n := decodeVarint(encoded)
		if n != len(encoded) || decoded != uint64(value) {
			t.Fatalf("%#x: got %#x (%d bytes of %d)", value, decoded, n, len(encoded))
		}
	}
	if _, n := decodeVarint([]byte{0xC3, 0x41}); n != 0 {
		t.Fatal("a bad continuation byte must be rejected")
	}
}

func TestDecodeZlibPayload(t *testing.T) {
	raw, _ := Packet{Op: OpLog, Tags: []Tag{StringTag(TagString, "hello")}}.Encode()
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	_, _ = writer.Write(raw[8:])
	_ = writer.Close()
	packet, err := DecodePayload(flagBase|flagZlib, compressed.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if packet.Message() != "hello" {
		t.Fatalf("packet = %+v", packet)
	}
}

func TestDecodeRejectsBadInput(t *testing.T) {
	if _, err := ReadPacket(bytes.NewReader([]byte{0, 0, 0, 0x40, 0, 0, 0, 0})); err == nil {
		t.Fatal("unknown flags accepted")
	}
	if _, err := DecodePayload(flagBase, []byte{OpStats, 0, 1, 0, 2, TypeUint8, 0, 0, 0, 9}); err == nil {
		t.Fatal("a tag longer than the packet was accepted")
	}
}

func TestPasswordResponse(t *testing.T) {
	// MD5(hex(MD5("secret")) + hex(MD5("1234ABCD"))), computed by hand.
	passwordHash := md5.Sum([]byte("secret"))
	saltHash := md5.Sum([]byte("1234ABCD"))
	want := md5.Sum([]byte(hex.EncodeToString(passwordHash[:]) + hex.EncodeToString(saltHash[:])))
	if got := PasswordResponse("secret", 0x1234ABCD); got != want {
		t.Fatalf("response = %x, want %x", got, want)
	}
}

func TestEmptyPasswordIsRefusedWithoutDialing(t *testing.T) {
	client := New(Config{Address: "127.0.0.1:1", Timeout: time.Second})
	if err := client.Ping(context.Background()); !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchResultLink(t *testing.T) {
	result := SearchResult{Name: "A|B.mkv", Size: 42, Hash: "0123456789abcdef0123456789abcdef"}
	if got := result.Link(); got != "ed2k://|file|A_B.mkv|42|0123456789abcdef0123456789abcdef|/" {
		t.Fatalf("link = %q", got)
	}
}
