package metainfo

import (
	"bytes"
	"testing"
)

// TestNewRejectsDeclaredLengthBeyondInput pins the fix for the fuzzer's
// OOM: a tiny torrent claiming a multi-gigabyte string must be refused before
// zeebo/bencode allocates the declared length.
func TestNewRejectsDeclaredLengthBeyondInput(t *testing.T) {
	// The bytes the fuzzer minimised to (testdata/fuzz/FuzzMetaInfoNew and
	// FuzzNewV2Info).
	crafted := []byte("d4:infod4:name1:011:000000000ie6:\x00deces2000000000:name1:11:00:")
	if _, err := New(bytes.NewReader(crafted)); err == nil {
		t.Fatal("expected an error for a declared length beyond the input")
	}
	// A bare huge length as the leading value.
	if _, err := New(bytes.NewReader([]byte("2000000000:"))); err == nil {
		t.Fatal("expected an error for a bare huge declared length")
	}
	// Every raw-bytes entry point must refuse it too (the fuzzer reached
	// NewV2Info directly).
	huge := []byte("d4:00d12000000000:")
	if _, err := NewV2Info(huge); err == nil {
		t.Error("NewV2Info accepted a huge declared length")
	}
	if _, err := NewInfo(huge, true, true); err == nil {
		t.Error("NewInfo accepted a huge declared length")
	}
	if _, err := ParseInfo(huge, true, true, nil); err == nil {
		t.Error("ParseInfo accepted a huge declared length")
	}
	if _, err := ParseFileTree([]byte("d12000000000:")); err == nil {
		t.Error("ParseFileTree accepted a huge declared length")
	}
	if _, err := DecodePieceLayers([]byte("d12000000000:1:xe")); err == nil {
		t.Error("DecodePieceLayers accepted a huge declared length")
	}
}
