package amule

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"testing"
)

// FuzzReadPacket feeds arbitrary bytes to the EC packet reader. The bytes come
// from another process over TCP, so a malformed frame has to be an error and
// never a panic: ReadPacket bounds the declared length, the tag decoder checks
// every read against the payload and caps the nesting depth. The seeds cover
// the three shapes the core sends: a plain packet with nested tags, a real
// 64-bit size tag, and a zlib-compressed body.
func FuzzReadPacket(f *testing.F) {
	packet := Packet{Op: OpSearchStart, Tags: []Tag{
		IntTag(TagSearchType, SearchKad,
			StringTag(TagSearchName, "Tracker S04E01"),
			IntTag(TagSearchMinSize, 5_000_000_000),
		),
		HashTag(TagPartfile, [16]byte{1, 2, 3}),
	}}
	if raw, err := packet.Encode(); err == nil {
		f.Add(raw)
	}

	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	_, _ = zw.Write([]byte{OpAuthOK, 0, 0})
	_ = zw.Close()
	body := compressed.Bytes()
	frame := make([]byte, 8+len(body))
	binary.BigEndian.PutUint32(frame[:4], flagBase|flagZlib)
	binary.BigEndian.PutUint32(frame[4:], uint32(len(body)))
	copy(frame[8:], body)
	f.Add(frame)

	f.Add([]byte{0x20, 0, 0, 0, 0, 0, 0, 0}) // a header that declares no body
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		decoded, err := ReadPacket(bytes.NewReader(data))
		if err != nil {
			return
		}
		// A packet that decoded must survive re-encoding too.
		_, _ = decoded.Encode()
	})
}
