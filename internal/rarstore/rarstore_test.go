package rarstore

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

// rar4Volume builds one RAR 4 volume holding one slice of a stored file.
func rar4Volume(name string, slice []byte, sliceCRC uint32, before, after bool, method byte, more bool) []byte {
	var b bytes.Buffer
	b.Write(sigRAR4)
	// Archive header: 13 bytes.
	arch := []byte{0, 0, rar4Archive, 0, 0, 13, 0, 0, 0, 0, 0, 0, 0}
	if more || before {
		binary.LittleEndian.PutUint16(arch[3:5], 0x0001)
	}
	binary.LittleEndian.PutUint16(arch[0:2], uint16(crc32.ChecksumIEEE(arch[2:])))
	b.Write(arch)
	head := make([]byte, 32+len(name))
	head[2] = rar4File
	flags := uint16(rar4HasAddSize)
	if before {
		flags |= rar4SplitBefore
	}
	if after {
		flags |= rar4SplitAfter
	}
	binary.LittleEndian.PutUint16(head[3:5], flags)
	binary.LittleEndian.PutUint16(head[5:7], uint16(len(head)))
	binary.LittleEndian.PutUint32(head[7:11], uint32(len(slice)))
	binary.LittleEndian.PutUint32(head[11:15], uint32(len(slice)))
	binary.LittleEndian.PutUint32(head[16:20], sliceCRC)
	head[24] = 29
	head[25] = method
	binary.LittleEndian.PutUint16(head[26:28], uint16(len(name)))
	copy(head[32:], name)
	binary.LittleEndian.PutUint16(head[0:2], uint16(crc32.ChecksumIEEE(head[2:])))
	b.Write(head)
	b.Write(slice)
	end := []byte{0, 0, rar4End, 0, 0, 7, 0}
	if more {
		binary.LittleEndian.PutUint16(end[3:5], rar4EndNextVol)
	}
	binary.LittleEndian.PutUint16(end[0:2], uint16(crc32.ChecksumIEEE(end[2:])))
	b.Write(end)
	return b.Bytes()
}

func putVint(b *bytes.Buffer, v uint64) {
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			c |= 0x80
		}
		b.WriteByte(c)
		if v == 0 {
			return
		}
	}
}

// rar5Header frames a header body with its CRC and size.
func rar5Header(body []byte) []byte {
	var size bytes.Buffer
	putVint(&size, uint64(len(body)))
	crc := crc32.ChecksumIEEE(append(append([]byte(nil), size.Bytes()...), body...))
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, crc)
	return append(append(out, size.Bytes()...), body...)
}

func rar5Volume(name string, slice []byte, sliceCRC uint32, before, after bool, method uint64, more bool) []byte {
	var b bytes.Buffer
	b.Write(sigRAR5)
	var main bytes.Buffer
	putVint(&main, rar5Main)
	putVint(&main, 0)
	putVint(&main, 1) // volume
	b.Write(rar5Header(main.Bytes()))
	var file bytes.Buffer
	putVint(&file, rar5File)
	flags := uint64(rar5HasData)
	if before {
		flags |= rar5SplitBefore
	}
	if after {
		flags |= rar5SplitAfter
	}
	putVint(&file, flags)
	putVint(&file, uint64(len(slice)))
	putVint(&file, rar5FileCRC)
	putVint(&file, uint64(len(slice)))
	putVint(&file, 0)
	crc := make([]byte, 4)
	binary.LittleEndian.PutUint32(crc, sliceCRC)
	file.Write(crc)
	putVint(&file, method<<7)
	putVint(&file, 1)
	putVint(&file, uint64(len(name)))
	file.WriteString(name)
	b.Write(rar5Header(file.Bytes()))
	b.Write(slice)
	var end bytes.Buffer
	putVint(&end, rar5End)
	putVint(&end, 0)
	if more {
		putVint(&end, rar5EndNextVolume)
	} else {
		putVint(&end, 0)
	}
	b.Write(rar5Header(end.Bytes()))
	return b.Bytes()
}

type volumeFunc func(name string, slice []byte, sliceCRC uint32, before, after bool, more bool) []byte

// writeSet splits payload over n volumes with the given naming.
func writeSet(t *testing.T, dir string, names []string, payload []byte, vol volumeFunc) {
	t.Helper()
	n := len(names)
	step := (len(payload) + n - 1) / n
	for i, name := range names {
		lo, hi := i*step, min((i+1)*step, len(payload))
		slice := payload[lo:hi]
		crc := crc32.ChecksumIEEE(slice)
		if i == n-1 {
			crc = crc32.ChecksumIEEE(payload)
		}
		data := vol("Episode.mkv", slice, crc, i > 0, i < n-1, i < n-1)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func payload() []byte {
	p := make([]byte, 300_000)
	for i := range p {
		p[i] = byte(i * 7)
	}
	return p
}

func extractOne(t *testing.T, first string) []byte {
	t.Helper()
	entries, err := List(first)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "Episode.mkv" {
		t.Fatalf("entries = %+v", entries)
	}
	target := filepath.Join(t.TempDir(), "out.mkv")
	if err := Extract(entries[0], target); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestExtractsMultiVolumeRAR4OldNaming(t *testing.T) {
	dir := t.TempDir()
	want := payload()
	writeSet(t, dir, []string{"set.rar", "set.r00", "set.r01"}, want, func(n string, s []byte, c uint32, b, a, m bool) []byte {
		return rar4Volume(n, s, c, b, a, rar4MethodStore, m)
	})
	firsts, err := FirstVolumes(dir)
	if err != nil || len(firsts) != 1 {
		t.Fatalf("first volumes = %v, %v", firsts, err)
	}
	if got := extractOne(t, firsts[0]); !bytes.Equal(got, want) {
		t.Fatal("extracted bytes differ")
	}
}

func TestExtractsMultiVolumeRAR5PartNaming(t *testing.T) {
	dir := t.TempDir()
	want := payload()
	writeSet(t, dir, []string{"set.part01.rar", "set.part02.rar", "set.part03.rar"}, want, func(n string, s []byte, c uint32, b, a, m bool) []byte {
		return rar5Volume(n, s, c, b, a, 0, m)
	})
	firsts, _ := FirstVolumes(dir)
	if len(firsts) != 1 || filepath.Base(firsts[0]) != "set.part01.rar" {
		t.Fatalf("first volumes = %v", firsts)
	}
	if got := extractOne(t, firsts[0]); !bytes.Equal(got, want) {
		t.Fatal("extracted bytes differ")
	}
}

func TestRefusesCompressedAndMissingVolumes(t *testing.T) {
	dir := t.TempDir()
	data := rar4Volume("Episode.mkv", []byte("x"), crc32.ChecksumIEEE([]byte("x")), false, false, 0x33, false)
	_ = os.WriteFile(filepath.Join(dir, "c.rar"), data, 0o644)
	if _, err := List(filepath.Join(dir, "c.rar")); !errors.Is(err, ErrCompressed) {
		t.Fatalf("compressed RAR 4: %v", err)
	}
	data = rar5Volume("Episode.mkv", []byte("x"), crc32.ChecksumIEEE([]byte("x")), false, false, 3, false)
	_ = os.WriteFile(filepath.Join(dir, "c5.rar"), data, 0o644)
	if _, err := List(filepath.Join(dir, "c5.rar")); !errors.Is(err, ErrCompressed) {
		t.Fatalf("compressed RAR 5: %v", err)
	}
	missing := t.TempDir()
	writeSet(t, missing, []string{"m.part1.rar", "m.part2.rar", "m.part3.rar"}, payload(), func(n string, s []byte, c uint32, b, a, m bool) []byte {
		return rar4Volume(n, s, c, b, a, rar4MethodStore, m)
	})
	_ = os.Remove(filepath.Join(missing, "m.part3.rar"))
	if _, err := List(filepath.Join(missing, "m.part1.rar")); !errors.Is(err, ErrMissingPart) {
		t.Fatalf("missing volume: %v", err)
	}
}

func TestBadChecksumLeavesNoFile(t *testing.T) {
	dir := t.TempDir()
	data := rar4Volume("Episode.mkv", []byte("hello"), 12345, false, false, rar4MethodStore, false)
	_ = os.WriteFile(filepath.Join(dir, "b.rar"), data, 0o644)
	entries, err := List(filepath.Join(dir, "b.rar"))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "out.mkv")
	if err := Extract(entries[0], target); !errors.Is(err, ErrCRC) {
		t.Fatalf("bad CRC: %v", err)
	}
	if _, err := os.Stat(target); err == nil {
		t.Fatal("a file was left behind")
	}
	if _, err := os.Stat(target + ".extracting"); err == nil {
		t.Fatal("a temporary file was left behind")
	}
}

func TestContinuationNames(t *testing.T) {
	for name, want := range map[string]bool{"a.part01.rar": false, "a.part02.rar": true, "a.r00": true, "a.rar": false, "a.mkv": false} {
		if got := IsContinuation(name); got != want {
			t.Errorf("IsContinuation(%q) = %v", name, got)
		}
	}
}
