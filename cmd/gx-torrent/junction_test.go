package main

import (
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

func TestMountPointReparseDataLayout(t *testing.T) {
	target := `\??\C:\Downloads\Serie`
	buffer := mountPointReparseData(target)
	nameBytes := len(target) * 2
	field := func(off int) int { return int(binary.LittleEndian.Uint16(buffer[off:])) }
	if tag := binary.LittleEndian.Uint32(buffer[0:4]); tag != ioReparseTagMountPoint {
		t.Fatalf("tag = %#x", tag)
	}
	if got, want := field(4), len(buffer)-8; got != want {
		t.Fatalf("ReparseDataLength = %d, want %d", got, want)
	}
	if field(8) != 0 || field(10) != nameBytes {
		t.Fatalf("substitute name offset/length = %d/%d, want 0/%d", field(8), field(10), nameBytes)
	}
	// The kernel requires PrintNameOffset == SubstituteNameLength + sizeof(WCHAR).
	if field(12) != nameBytes+2 || field(14) != 0 {
		t.Fatalf("print name offset/length = %d/%d, want %d/0", field(12), field(14), nameBytes+2)
	}
	if got, want := len(buffer), 16+nameBytes+4; got != want {
		t.Fatalf("buffer = %d bytes, want %d", got, want)
	}
	units := make([]uint16, len(target))
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(buffer[16+i*2:])
	}
	if got := string(utf16.Decode(units)); got != target {
		t.Fatalf("substitute name = %q, want %q", got, target)
	}
	for _, off := range []int{16 + nameBytes, 16 + nameBytes + 2} {
		if field(off) != 0 {
			t.Fatalf("missing NUL terminator at %d", off)
		}
	}
}
