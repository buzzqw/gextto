package rarstore

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

// unpack5.go decodes the RAR 5 compression: an LZ77 stream over a sliding
// window, coded with four canonical Huffman tables that each block may
// replace, with three kinds of filters (delta, x86 call addresses, ARM branch
// addresses) applied to marked ranges of the output. Written from the
// published description of the RAR 5 format; solid archives are not
// decoded (each file must start a new stream).

// Errors of the decoder.
var (
	ErrCorrupt     = errors.New("the compressed data is damaged")
	ErrUnsupported = errors.New("this RAR compression is not supported")
)

// maxDictionary bounds the window the decoder allocates: video releases
// use far less, and a larger one would not fit a small NAS anyway.
const maxDictionary = 256 << 20

const (
	nc5    = 306 // literals, end/filter/repeat codes and length slots
	dc5    = 64  // distance slots
	ldc5   = 16  // low distance bits
	rc5    = 44  // repeat length slots
	bc5    = 20  // bit lengths of the tables
	table5 = nc5 + dc5 + ldc5 + rc5
)

// bitReader reads the stream most significant bit first, with four bytes of
// look-ahead (zeros past the end, which only a corrupt stream would read).
type bitReader struct {
	r    *bufio.Reader
	buf  []byte // bytes not consumed yet
	bit  uint   // bits consumed of buf[0]
	pos  int64  // bytes consumed before buf[0]
	eof  bool
	tail int // zero bytes added past the end
}

func newBitReader(r io.Reader) *bitReader {
	return &bitReader{r: bufio.NewReaderSize(r, 1<<16)}
}

// fill keeps at least n bytes in buf.
func (b *bitReader) fill(n int) {
	for len(b.buf) < n {
		if b.eof {
			b.buf = append(b.buf, 0)
			b.tail++
			continue
		}
		c, err := b.r.ReadByte()
		if err != nil {
			b.eof = true
			continue
		}
		b.buf = append(b.buf, c)
	}
}

// peek32 returns the next 32 bits.
func (b *bitReader) peek32() uint32 {
	b.fill(5)
	v := uint64(b.buf[0])<<32 | uint64(b.buf[1])<<24 | uint64(b.buf[2])<<16 | uint64(b.buf[3])<<8 | uint64(b.buf[4])
	return uint32(v >> (8 - b.bit))
}

// peek16 returns the next 16 bits.
func (b *bitReader) peek16() uint32 { return b.peek32() >> 16 }

// skip consumes n bits (n <= 32).
func (b *bitReader) skip(n uint) {
	b.bit += n
	drop := int(b.bit / 8)
	b.bit %= 8
	if drop > 0 {
		b.fill(drop)
		b.buf = b.buf[drop:]
		b.pos += int64(drop)
		if len(b.buf) == 0 {
			b.buf = b.buf[:0:0]
		}
	}
}

// read returns the next n bits (n <= 16) and consumes them.
func (b *bitReader) read(n uint) uint32 {
	v := b.peek32() >> (32 - n)
	b.skip(n)
	return v
}

// align skips to the next byte boundary.
func (b *bitReader) align() {
	if b.bit != 0 {
		b.skip(8 - b.bit)
	}
}

// offset is the position of the next unread bit, in bits from the start.
func (b *bitReader) offset() int64 { return b.pos*8 + int64(b.bit) }

// overrun reports reading past the real end of the data.
func (b *bitReader) overrun() bool { return b.tail > 4 }

// huffman is a canonical decoding table (the RAR layout: per bit length the
// left-aligned upper limit and the first position).
type huffman struct {
	decodeLen [16]uint32
	decodePos [16]uint32
	decodeNum []uint16
}

func (h *huffman) build(lengths []byte) {
	var count [16]uint32
	for _, l := range lengths {
		count[l&0xf]++
	}
	count[0] = 0
	h.decodeNum = make([]uint16, len(lengths))
	var upper uint32
	h.decodeLen[0], h.decodePos[0] = 0, 0
	for i := 1; i < 16; i++ {
		upper += count[i]
		h.decodeLen[i] = upper << (16 - i)
		upper *= 2
		h.decodePos[i] = h.decodePos[i-1] + count[i-1]
	}
	next := h.decodePos
	for i, l := range lengths {
		if l &= 0xf; l != 0 {
			h.decodeNum[next[l]] = uint16(i)
			next[l]++
		}
	}
}

func (h *huffman) decode(b *bitReader) int {
	field := b.peek16() & 0xfffe
	bits := uint(15)
	for i := uint(1); i < 15; i++ {
		if field < h.decodeLen[i] {
			bits = i
			break
		}
	}
	b.skip(bits)
	dist := (field - h.decodeLen[bits-1]) >> (16 - bits)
	pos := h.decodePos[bits] + dist
	if int(pos) >= len(h.decodeNum) {
		pos = 0
	}
	if len(h.decodeNum) == 0 {
		return 0
	}
	return int(h.decodeNum[pos])
}

// filter5 is one pending filter on the output.
type filter5 struct {
	kind     uint32
	start    int64 // absolute output position
	length   int64
	channels int
}

const (
	filterDelta = 0
	filterE8    = 1
	filterE8E9  = 2
	filterARM   = 3
	// maxFilterLength bounds a filtered block (the format's own limit).
	maxFilterLength = 0x400000
)

// unpacker5 is the state of one stream.
type unpacker5 struct {
	in      *bitReader
	out     io.Writer
	window  []byte
	mask    int64
	pos     int64 // absolute output position decoded so far
	written int64 // absolute output position written to out
	size    int64 // expected output size
	filters []filter5

	ld, dd, ldd, rd huffman
	tables          bool
	oldDist         [4]int64
	lastLength      int

	blockEnd  int64 // bit offset where the block ends
	lastBlock bool
}

// unpack5 decodes size bytes of a RAR 5 stream from r into w.
func unpack5(r io.Reader, w io.Writer, size int64, dictionary int64) error {
	if dictionary <= 0 || dictionary > maxDictionary {
		return fmt.Errorf("%w: dictionary of %d MiB", ErrUnsupported, dictionary>>20)
	}
	window := int64(1)
	for window < dictionary || window < 1<<18 {
		window <<= 1
	}
	u := &unpacker5{in: newBitReader(r), out: w, window: make([]byte, window), mask: window - 1, size: size}
	return u.run()
}

func (u *unpacker5) run() error {
	if err := u.readBlockHeader(); err != nil {
		return err
	}
	for u.pos < u.size {
		if u.in.overrun() {
			return ErrCorrupt
		}
		if u.in.offset() >= u.blockEnd {
			if u.lastBlock {
				break
			}
			if err := u.readBlockHeader(); err != nil {
				return err
			}
			continue
		}
		if u.pos-u.written > int64(len(u.window))-0x10000 {
			if err := u.flush(); err != nil {
				return err
			}
		}
		slot := u.ld.decode(u.in)
		switch {
		case slot < 256:
			u.put(byte(slot))
		case slot >= 262:
			length := u.slotLength(slot - 262)
			distSlot := u.dd.decode(u.in)
			dist := int64(1)
			var dbits uint
			if distSlot < 4 {
				dist += int64(distSlot)
			} else {
				dbits = uint(distSlot/2 - 1)
				dist += int64(2|(distSlot&1)) << dbits
			}
			if dbits > 0 {
				if dbits >= 4 {
					if dbits > 4 {
						dist += int64(u.in.peek32()>>(36-dbits)) << 4
						u.in.skip(dbits - 4)
					}
					dist += int64(u.ldd.decode(u.in))
				} else {
					dist += int64(u.in.peek32() >> (32 - dbits))
					u.in.skip(dbits)
				}
			}
			if dist > 0x100 {
				length++
				if dist > 0x2000 {
					length++
					if dist > 0x40000 {
						length++
					}
				}
			}
			u.insertOld(dist)
			u.lastLength = length
			if err := u.copy(length, dist); err != nil {
				return err
			}
		case slot == 256:
			if err := u.readFilter(); err != nil {
				return err
			}
		case slot == 257:
			if u.lastLength != 0 {
				if err := u.copy(u.lastLength, u.oldDist[0]); err != nil {
					return err
				}
			}
		default: // 258..261: a recent distance
			index := slot - 258
			dist := u.oldDist[index]
			for i := index; i > 0; i-- {
				u.oldDist[i] = u.oldDist[i-1]
			}
			u.oldDist[0] = dist
			length := u.slotLength(u.rd.decode(u.in))
			u.lastLength = length
			if err := u.copy(length, dist); err != nil {
				return err
			}
		}
	}
	if u.pos < u.size {
		return ErrCorrupt
	}
	return u.flushAll()
}

func (u *unpacker5) put(c byte) {
	if u.pos < u.size {
		u.window[u.pos&u.mask] = c
	}
	u.pos++
}

func (u *unpacker5) copy(length int, dist int64) error {
	if dist <= 0 || dist > u.pos || dist > int64(len(u.window)) {
		return ErrCorrupt
	}
	src := u.pos - dist
	for i := 0; i < length && u.pos < u.size; i++ {
		u.window[u.pos&u.mask] = u.window[(src+int64(i))&u.mask]
		u.pos++
	}
	return nil
}

func (u *unpacker5) insertOld(dist int64) {
	u.oldDist[3], u.oldDist[2], u.oldDist[1], u.oldDist[0] = u.oldDist[2], u.oldDist[1], u.oldDist[0], dist
}

func (u *unpacker5) slotLength(slot int) int {
	var lbits uint
	var length int
	if slot < 8 {
		length = 2 + slot
	} else {
		lbits = uint(slot/4 - 1)
		length = 2 + (4|(slot&3))<<lbits
	}
	if lbits > 0 {
		length += int(u.in.read(lbits))
	}
	return length
}

func (u *unpacker5) readBlockHeader() error {
	u.in.align()
	flags := u.in.read(8)
	byteCount := int((flags>>3)&3) + 1
	if byteCount == 4 {
		return ErrCorrupt
	}
	bitSize := int(flags&7) + 1
	check := u.in.read(8)
	var blockSize int64
	for i := 0; i < byteCount; i++ {
		blockSize |= int64(u.in.read(8)) << (8 * i)
	}
	sum := byte(0x5a) ^ byte(flags) ^ byte(blockSize) ^ byte(blockSize>>8) ^ byte(blockSize>>16)
	if uint32(sum) != check {
		return ErrCorrupt
	}
	start := u.in.offset()
	u.blockEnd = start + (blockSize-1)*8 + int64(bitSize)
	if blockSize == 0 {
		u.blockEnd = start
	}
	u.lastBlock = flags&0x40 != 0
	if flags&0x80 != 0 {
		return u.readTables()
	}
	if !u.tables {
		return ErrCorrupt
	}
	return nil
}

func (u *unpacker5) readTables() error {
	var bitLength [bc5]byte
	for i := 0; i < bc5; i++ {
		l := byte(u.in.read(4))
		if l == 15 {
			zeros := int(u.in.read(4))
			if zeros == 0 {
				bitLength[i] = 15
			} else {
				for zeros += 2; zeros > 0 && i < bc5; zeros-- {
					bitLength[i] = 0
					i++
				}
				i--
			}
			continue
		}
		bitLength[i] = l
	}
	var bd huffman
	bd.build(bitLength[:])
	table := make([]byte, table5)
	for i := 0; i < table5; {
		n := bd.decode(u.in)
		switch {
		case n < 16:
			table[i] = byte(n)
			i++
		case n < 18:
			var count int
			if n == 16 {
				count = int(u.in.read(3)) + 3
			} else {
				count = int(u.in.read(7)) + 11
			}
			if i == 0 {
				return ErrCorrupt
			}
			for ; count > 0 && i < table5; count-- {
				table[i] = table[i-1]
				i++
			}
		default:
			var count int
			if n == 18 {
				count = int(u.in.read(3)) + 3
			} else {
				count = int(u.in.read(7)) + 11
			}
			for ; count > 0 && i < table5; count-- {
				table[i] = 0
				i++
			}
		}
	}
	u.ld.build(table[:nc5])
	u.dd.build(table[nc5 : nc5+dc5])
	u.ldd.build(table[nc5+dc5 : nc5+dc5+ldc5])
	u.rd.build(table[nc5+dc5+ldc5:])
	u.tables = true
	return nil
}

func (u *unpacker5) readFilterData() int64 {
	count := int(u.in.read(2)) + 1
	var v int64
	for i := 0; i < count; i++ {
		v |= int64(u.in.read(8)) << (8 * i)
	}
	return v
}

func (u *unpacker5) readFilter() error {
	start := u.readFilterData()
	length := u.readFilterData()
	kind := u.in.read(3)
	f := filter5{kind: kind, start: u.pos + start, length: length}
	if kind == filterDelta {
		f.channels = int(u.in.read(5)) + 1
	}
	if kind > filterARM || length > maxFilterLength || length <= 0 {
		return ErrCorrupt
	}
	if len(u.filters) >= 8192 {
		return ErrCorrupt
	}
	u.filters = append(u.filters, f)
	return nil
}

// flush writes the decoded bytes that no pending filter is waiting on.
func (u *unpacker5) flush() error {
	for u.written < u.pos {
		end := u.pos
		if len(u.filters) > 0 {
			f := u.filters[0]
			if f.start < u.written {
				return ErrCorrupt
			}
			if f.start == u.written {
				if f.start+f.length > u.pos {
					// The filtered block is not complete yet.
					return nil
				}
				if err := u.writeFiltered(f); err != nil {
					return err
				}
				u.filters = u.filters[1:]
				continue
			}
			if f.start < end {
				end = f.start
			}
		}
		if err := u.writeRaw(end); err != nil {
			return err
		}
	}
	return nil
}

func (u *unpacker5) flushAll() error {
	if err := u.flush(); err != nil {
		return err
	}
	if u.written < u.size {
		// A filter reached past the end of the data: write what remains.
		u.filters = nil
		return u.writeRaw(u.pos)
	}
	return nil
}

// writeRaw writes the window from u.written to end.
func (u *unpacker5) writeRaw(end int64) error {
	end = min(end, u.size)
	for u.written < end {
		from := u.written & u.mask
		n := min(end-u.written, int64(len(u.window))-from)
		if _, err := u.out.Write(u.window[from : from+n]); err != nil {
			return err
		}
		u.written += n
	}
	return nil
}

func (u *unpacker5) writeFiltered(f filter5) error {
	length := min(f.length, u.size-f.start)
	if length <= 0 {
		u.written = f.start + f.length
		return nil
	}
	data := make([]byte, length)
	for i := int64(0); i < length; i++ {
		data[i] = u.window[(f.start+i)&u.mask]
	}
	switch f.kind {
	case filterDelta:
		out := make([]byte, length)
		src := 0
		for ch := 0; ch < f.channels; ch++ {
			var prev byte
			for dst := int64(ch); dst < length; dst += int64(f.channels) {
				if src >= len(data) {
					break
				}
				prev -= data[src]
				out[dst] = prev
				src++
			}
		}
		data = out
	case filterE8, filterE8E9:
		const fileSize = 0x1000000
		for cur := int64(0); cur+4 < length; {
			c := data[cur]
			cur++
			if c == 0xe8 || (f.kind == filterE8E9 && c == 0xe9) {
				offset := uint32((cur + f.start) % fileSize)
				addr := uint32(data[cur]) | uint32(data[cur+1])<<8 | uint32(data[cur+2])<<16 | uint32(data[cur+3])<<24
				if addr&0x80000000 != 0 {
					if (addr+offset)&0x80000000 == 0 {
						addr += fileSize
						putLE32(data[cur:], addr)
					}
				} else if (addr-fileSize)&0x80000000 != 0 {
					addr -= offset
					putLE32(data[cur:], addr)
				}
				cur += 4
			}
		}
	case filterARM:
		for cur := int64(0); cur+3 < length; cur += 4 {
			d := data[cur : cur+4]
			if d[3] == 0xeb {
				offset := uint32(d[0]) | uint32(d[1])<<8 | uint32(d[2])<<16
				offset -= uint32((f.start + cur) / 4)
				d[0], d[1], d[2] = byte(offset), byte(offset>>8), byte(offset>>16)
			}
		}
	}
	if _, err := u.out.Write(data); err != nil {
		return err
	}
	u.written = f.start + length
	return nil
}

func putLE32(b []byte, v uint32) {
	b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
}
