package dht

import (
	"bytes"
	"strings"
	"testing"

	bencode "github.com/jackpal/bencode-go"
)

// A BEP 44 "a" dictionary always contains string values. bencode-go panics on
// exactly this shape ("reflect.Value.Set using unaddressable value"), so the
// decoder must handle it without panicking.
func TestBep44RequestDecodeStringValues(t *testing.T) {
	var buf bytes.Buffer
	err := bencode.Marshal(&buf, map[string]interface{}{
		"t": "aa",
		"a": map[string]interface{}{
			"id":        "abcdefghij0123456789",
			"target":    "zyxwvutsrqponmlkjihg",
			"info_hash": "mnopqrstuvwxyz123456",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := bep44RequestDecode(buf.Bytes())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.T != "aa" {
		t.Fatalf("T = %q, want aa", req.T)
	}
	if got := mapString(req.A, "target"); got != "zyxwvutsrqponmlkjihg" {
		t.Fatalf("target = %q", got)
	}
}

func TestBep44RequestDecodeNestedValue(t *testing.T) {
	var buf bytes.Buffer
	if err := bencode.Marshal(&buf, map[string]interface{}{
		"t": "z",
		"a": map[string]interface{}{
			"v":     map[string]interface{}{"x": "y"},
			"seq":   int64(3),
			"token": "tok",
		},
	}); err != nil {
		t.Fatal(err)
	}
	req, err := bep44RequestDecode(buf.Bytes())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := req.A["v"].(map[string]interface{}); !ok {
		t.Fatalf("v = %T, want a map", req.A["v"])
	}
	if got := mapInt(req.A, "seq"); got != 3 {
		t.Fatalf("seq = %d, want 3", got)
	}
}

// Deeply nested input must fail cleanly, never overflow the stack.
func TestBep44RequestDecodeTooDeep(t *testing.T) {
	raw := []byte("d1:t1:ad1:a" + strings.Repeat("l", 1000) + strings.Repeat("e", 1000) + "ee")
	if _, err := bep44RequestDecode(raw); err == nil {
		t.Fatal("expected an error for too-deep nesting")
	}
}

func TestBep44RequestDecodeMalformed(t *testing.T) {
	cases := [][]byte{
		{},
		[]byte("d"),
		[]byte("d1:a"),
		[]byte("d1:ade"),
		[]byte("l"),
		[]byte("i12"),
		nil,
	}
	for _, raw := range cases {
		if _, err := bep44RequestDecode(raw); err == nil {
			t.Errorf("decode(%q) = nil error, want error", raw)
		}
	}
}

// A well-formed non-dictionary packet (e.g. a bare string) is rejected.
func TestBep44RequestDecodeNotADict(t *testing.T) {
	if _, err := bep44RequestDecode([]byte("4:spam")); err == nil {
		t.Fatal("expected an error for a non-dictionary packet")
	}
}

// TestValidBencode: the cheap screen accepts well-formed packets and rejects a
// declared length that exceeds the buffer (the shape that made bencode-go OOM),
// without allocating.
func TestValidBencode(t *testing.T) {
	ok := [][]byte{
		[]byte("de"),
		[]byte("d1:t2:aa1:y1:re"),
		[]byte("li1ei2ee"),
		[]byte("4:spam"),
		[]byte("d2:id20:aaaaaaaaaaaaaaaaaaaae"),
	}
	for _, b := range ok {
		if err := validBencode(b); err != nil {
			t.Errorf("validBencode(%q) = %v, want nil", b, err)
		}
	}
	bad := [][]byte{
		nil,
		[]byte("d"),
		[]byte("d122222222222:d"), // 122 GB declared, not present: must not allocate
		[]byte("d1:t99999999:xxe"),
		[]byte("3:ab"),
		[]byte("i"),
		[]byte("l"),
		[]byte("d1:a"),
		[]byte(strings.Repeat("l", 1000) + strings.Repeat("e", 1000)),
	}
	for _, b := range bad {
		if err := validBencode(b); err == nil {
			t.Errorf("validBencode(%q) = nil, want error", b)
		}
	}
}

// FuzzValidBencode feeds arbitrary bytes to the allocation-free screen: it must
// return an error or nil, never allocate on a declared length or overflow.
func FuzzValidBencode(f *testing.F) {
	f.Add([]byte("d1:t2:aa1:y1:re"))
	f.Add([]byte("d122222222222:d"))
	f.Add([]byte("4:spam"))
	f.Fuzz(func(t *testing.T, b []byte) {
		_ = validBencode(b)
	})
}

// BenchmarkValidBencode measures the per-packet screen that readResponse now
// runs before bencode-go: it must be allocation-free.
func BenchmarkValidBencode(b *testing.B) {
	packet := []byte("d1:ad2:id20:aaaaaaaaaaaaaaaaaaaa9:info_hash20:bbbbbbbbbbbbbbbbbbbb1:q9:get_peers1:t2:aa1:y1:q1:v4:GX01e1:y1:qe")
	if err := validBencode(packet); err != nil {
		b.Fatalf("packet rejected: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := validBencode(packet); err != nil {
			b.Fatal(err)
		}
	}
}
