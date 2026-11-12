package rarstore

import (
	"bytes"
	"errors"
	"hash/crc32"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// bitWriter writes most significant bit first, like the decoder reads.
type bitWriter struct {
	buf  []byte
	bits int
}

func (w *bitWriter) write(v uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		if w.bits%8 == 0 {
			w.buf = append(w.buf, 0)
		}
		if v>>uint(i)&1 != 0 {
			w.buf[len(w.buf)-1] |= 0x80 >> uint(w.bits%8)
		}
		w.bits++
	}
}

// encoder5 writes a RAR 5 stream with fixed-length codes: every table symbol
// gets the same length, so a symbol's code is its index.
type encoder5 struct {
	w bitWriter
}

const (
	ldBits  = 9 // 306 symbols
	ddBits  = 6 // 64
	lddBits = 4 // 16
	rdBits  = 6 // 44
	bdBits  = 5 // 20
)

func (e *encoder5) tables() {
	for i := 0; i < bc5; i++ {
		e.w.write(bdBits, 4)
	}
	for i := 0; i < table5; i++ {
		var l uint32
		switch {
		case i < nc5:
			l = ldBits
		case i < nc5+dc5:
			l = ddBits
		case i < nc5+dc5+ldc5:
			l = lddBits
		default:
			l = rdBits
		}
		e.w.write(l, bdBits)
	}
}

func (e *encoder5) literal(c byte) { e.w.write(uint32(c), ldBits) }

func lengthSlot(length int) (slot int, extra uint32, bits uint) {
	if length-2 < 8 {
		return length - 2, 0, 0
	}
	for s := 8; s < rc5; s++ {
		b := uint(s/4 - 1)
		base := 2 + (4|(s&3))<<b
		if length >= base && length-base < 1<<b {
			return s, uint32(length - base), b
		}
	}
	panic("length out of range")
}

func distSlot(dist int64) (slot int, extra int64, bits uint) {
	d := dist - 1
	if d < 4 {
		return int(d), 0, 0
	}
	for s := 4; s < dc5; s++ {
		b := uint(s/2 - 1)
		base := int64(2|(s&1)) << b
		if d >= base && d-base < 1<<b {
			return s, d - base, b
		}
	}
	panic("distance out of range")
}

// match writes a copy of length bytes from dist back.
func (e *encoder5) match(length int, dist int64) {
	adj := 0
	if dist > 0x100 {
		adj++
		if dist > 0x2000 {
			adj++
			if dist > 0x40000 {
				adj++
			}
		}
	}
	slot, lextra, lbits := lengthSlot(length - adj)
	e.w.write(uint32(262+slot), ldBits)
	if lbits > 0 {
		e.w.write(lextra, int(lbits))
	}
	ds, dextra, dbits := distSlot(dist)
	e.w.write(uint32(ds), ddBits)
	if dbits >= 4 {
		if dbits > 4 {
			e.w.write(uint32(dextra>>4), int(dbits-4))
		}
		e.w.write(uint32(dextra&15), lddBits)
	} else if dbits > 0 {
		e.w.write(uint32(dextra), int(dbits))
	}
}

func (e *encoder5) filterData(v int64) {
	e.w.write(3, 2) // four bytes
	for i := 0; i < 4; i++ {
		e.w.write(uint32(v>>(8*i))&0xff, 8)
	}
}

// deltaFilter marks length bytes starting start bytes ahead as delta coded.
func (e *encoder5) deltaFilter(start, length int64, channels int) {
	e.w.write(256, ldBits)
	e.filterData(start)
	e.filterData(length)
	e.w.write(filterDelta, 3)
	e.w.write(uint32(channels-1), 5)
}

// block frames the bits written so far as one block.
func block(payload bitWriter, tables, last bool) []byte {
	size := len(payload.buf)
	bitSize := payload.bits - (size-1)*8
	flags := byte(bitSize-1) | byte(1)<<3 // two size bytes
	if tables {
		flags |= 0x80
	}
	if last {
		flags |= 0x40
	}
	check := byte(0x5a) ^ flags ^ byte(size) ^ byte(size>>8) ^ byte(size>>16)
	out := []byte{flags, check, byte(size), byte(size >> 8)}
	return append(out, payload.buf...)
}

// TestUnpack5 decodes a stream with literals, every kind of distance, the
// last-length and recent-distance codes, a delta filter and a second block
// that reuses the first one's tables.
func TestUnpack5(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	var want []byte
	var first encoder5
	first.tables()
	for i := 0; i < 600; i++ {
		c := byte(rng.Intn(256))
		first.literal(c)
		want = append(want, c)
	}
	copyMatch := func(e *encoder5, length int, dist int64) {
		e.match(length, dist)
		for i := 0; i < length; i++ {
			want = append(want, want[len(want)-int(dist)])
		}
	}
	copyMatch(&first, 5, 1)
	copyMatch(&first, 40, 3)
	copyMatch(&first, 300, 17)
	copyMatch(&first, 9, 500)
	// 257: repeat the last length at the last distance.
	first.w.write(257, ldBits)
	for i := 0; i < 9; i++ {
		want = append(want, want[len(want)-500])
	}
	firstBlock := block(first.w, true, false)

	var second encoder5
	// 258: the most recent distance (500) with a length from RD.
	second.w.write(258, ldBits)
	second.w.write(uint32(4), rdBits) // slot 4: length 6
	for i := 0; i < 6; i++ {
		want = append(want, want[len(want)-500])
	}
	// A delta-filtered run of 64 bytes, two channels.
	plain := make([]byte, 64)
	for i := range plain {
		plain[i] = byte(i*3 + i/2)
	}
	coded := make([]byte, 0, 64)
	for ch := 0; ch < 2; ch++ {
		var prev byte
		for i := ch; i < 64; i += 2 {
			coded = append(coded, prev-plain[i])
			prev = plain[i]
		}
	}
	second.deltaFilter(0, 64, 2)
	for _, c := range coded {
		second.literal(c)
	}
	want = append(want, plain...)
	for i := 0; i < 50; i++ {
		c := byte(rng.Intn(256))
		second.literal(c)
		want = append(want, c)
	}
	stream := append(firstBlock, block(second.w, false, true)...)

	var got bytes.Buffer
	if err := unpack5(bytes.NewReader(stream), &got, int64(len(want)), 1<<20); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		for i := range want {
			if i >= got.Len() || got.Bytes()[i] != want[i] {
				t.Fatalf("output differs at byte %d of %d (got %d bytes)", i, len(want), got.Len())
			}
		}
		t.Fatalf("output has %d bytes, want %d", got.Len(), len(want))
	}
}

func TestUnpack5RefusesDamage(t *testing.T) {
	var e encoder5
	e.tables()
	e.match(4, 10) // reaches before the start of the output
	stream := block(e.w, true, true)
	if err := unpack5(bytes.NewReader(stream), &bytes.Buffer{}, 4, 1<<20); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("a distance before the start: %v", err)
	}
	stream[1] ^= 0xff // block checksum
	if err := unpack5(bytes.NewReader(stream), &bytes.Buffer{}, 4, 1<<20); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("a damaged header: %v", err)
	}
	if err := unpack5(bytes.NewReader(nil), &bytes.Buffer{}, 4, 1<<40); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("a huge dictionary: %v", err)
	}
}

// TestExtractCompressedRAR5 builds a compressed RAR 5 archive split over two
// volumes and extracts it with its CRC checked.
func TestExtractCompressedRAR5(t *testing.T) {
	var e encoder5
	e.tables()
	var want []byte
	for i := 0; i < 3000; i++ {
		c := byte(i % 251)
		e.literal(c)
		want = append(want, c)
	}
	e.match(2000, 251)
	for i := 0; i < 2000; i++ {
		want = append(want, want[len(want)-251])
	}
	stream := block(e.w, true, true)
	dir := t.TempDir()
	half := len(stream) / 2
	names := []string{"c.part1.rar", "c.part2.rar"}
	for i, slice := range [][]byte{stream[:half], stream[half:]} {
		crc := crc32.ChecksumIEEE(slice)
		if i == 1 {
			crc = crc32.ChecksumIEEE(want)
		}
		vol := rar5CompressedVolume("Episode.mkv", slice, int64(len(want)), crc, i > 0, i == 0, i == 0)
		if err := os.WriteFile(filepath.Join(dir, names[i]), vol, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := List(filepath.Join(dir, names[0]))
	if err != nil || len(entries) != 1 || !entries[0].Compressed() || entries[0].Size != int64(len(want)) {
		t.Fatalf("entries = %+v, %v", entries, err)
	}
	target := filepath.Join(dir, "out.mkv")
	if err := Extract(entries[0], target); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, want) {
		t.Fatal("extracted bytes differ")
	}
}
