package ed2k

import (
	"encoding/binary"
	"net"
	"testing"
)

func testSourceTags(ip net.IP, tcpPort, udpPort uint16, kind uint8) []Tag {
	ip4 := ip.To4()
	value := uint64(ip4[0])<<24 | uint64(ip4[1])<<16 | uint64(ip4[2])<<8 | uint64(ip4[3])
	tags := []Tag{
		{Name: TagSourceIP, Type: TagUint32, Int: value},
		{Name: TagSourcePort, Type: TagUint16, Int: uint64(tcpPort)},
		{Name: TagSourceType, Type: TagUint8, Int: uint64(kind)},
	}
	if udpPort != 0 {
		tags = append(tags, Tag{Name: TagSourceUDPPort, Type: TagUint16, Int: uint64(udpPort)})
	}
	return tags
}

func TestGetSourcesEncoding(t *testing.T) {
	hash, err := ParseID("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	small := encodeGetSources(hash, 1234)
	if len(small) != 20 {
		t.Fatalf("small request length = %d", len(small))
	}
	if string(small[:16]) != string(hash[:]) {
		t.Fatal("hash not at the start of the request")
	}
	if got := binary.LittleEndian.Uint32(small[16:]); got != 1234 {
		t.Fatalf("size = %d", got)
	}

	large := encodeGetSources(hash, 5<<32)
	if len(large) != 28 {
		t.Fatalf("large request length = %d", len(large))
	}
	if got := binary.LittleEndian.Uint32(large[16:20]); got != 0 {
		t.Fatalf("large marker = %d", got)
	}
	if got := binary.LittleEndian.Uint64(large[20:]); got != 5<<32 {
		t.Fatalf("large size = %d", got)
	}
}

func TestDecodeFoundSources(t *testing.T) {
	hash, err := ParseID("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	payload := append([]byte(nil), hash[:]...)
	payload = append(payload, 2)
	payload = append(payload, 1, 2, 3, 4, 0x36, 0x12)      // 1.2.3.4:4662
	payload = append(payload, 192, 168, 1, 10, 0x36, 0x12) // private, kept by the codec
	gotHash, sources, err := decodeFoundSources(payload)
	if err != nil {
		t.Fatal(err)
	}
	if gotHash != hash {
		t.Fatal("hash mismatch")
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %+v", sources)
	}
	if !sources[0].IP.Equal(net.IPv4(1, 2, 3, 4)) || sources[0].TCPPort != 4662 || sources[0].Origin != "server" {
		t.Fatalf("first source = %+v", sources[0])
	}
	if !sources[1].IP.Equal(net.IPv4(192, 168, 1, 10)) {
		t.Fatalf("second source = %+v", sources[1])
	}

	if _, _, err := decodeFoundSources(payload[:16]); err == nil {
		t.Fatal("short payload accepted")
	}
	if _, _, err := decodeFoundSources(payload[:20]); err == nil {
		t.Fatal("truncated source list accepted")
	}
}

func TestDecodeFoundSourcesLowID(t *testing.T) {
	hash, _ := ParseID("0123456789abcdef0123456789abcdef")
	payload := append([]byte(nil), hash[:]...)
	payload = append(payload, 1)
	payload = append(payload, 1, 0, 0, 0, 0x36, 0x12) // LowID 1, port 4662
	_, sources, err := decodeFoundSourcesFromServer(payload, net.IPv4(9, 8, 7, 6), 5000)
	if err != nil || len(sources) != 1 {
		t.Fatalf("sources = %+v, %v", sources, err)
	}
	source := sources[0]
	if !source.LowID || source.ClientID != 1 || source.TCPPort != 0 ||
		!source.ServerIP.Equal(net.IPv4(9, 8, 7, 6)) || source.ServerPort != 5000 {
		t.Fatalf("LowID source = %+v", source)
	}
}

func TestDecodeSourceResponse(t *testing.T) {
	sender, _ := ParseID("11111111111111111111111111111111")
	target, _ := ParseID("22222222222222222222222222222222")
	user, _ := ParseID("33333333333333333333333333333333")
	tags := testSourceTags(net.IPv4(1, 2, 3, 4), 4662, 4672, 1)

	w := newWriter(binary.LittleEndian)
	w.kadID(sender)
	w.kadID(target)
	w.u16(1)
	w.kadID(user)
	w.u8(uint8(len(tags)))
	w.raw(WriteTagList(tags, binary.LittleEndian))

	gotTarget, sources, err := decodeSourceResponse(w.buf)
	if err != nil {
		t.Fatal(err)
	}
	if gotTarget != target {
		t.Fatal("target mismatch")
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %+v", sources)
	}
	source := sources[0]
	if !source.IP.Equal(net.IPv4(1, 2, 3, 4)) || source.TCPPort != 4662 || source.UDPPort != 4672 {
		t.Fatalf("source = %+v", source)
	}
	if source.UserHash != user || source.Type != 1 || source.Origin != "kad" {
		t.Fatalf("source = %+v", source)
	}

	if _, _, err := decodeSourceResponse(w.buf[:20]); err == nil {
		t.Fatal("short response accepted")
	}
}

func TestSourceResponseWithoutPortIsSkipped(t *testing.T) {
	sender, _ := ParseID("11111111111111111111111111111111")
	target, _ := ParseID("22222222222222222222222222222222")
	user, _ := ParseID("33333333333333333333333333333333")
	tags := []Tag{{Name: TagSourceIP, Type: TagUint32, Int: 0x01020304}}

	w := newWriter(binary.LittleEndian)
	w.kadID(sender)
	w.kadID(target)
	w.u16(1)
	w.kadID(user)
	w.u8(uint8(len(tags)))
	w.raw(WriteTagList(tags, binary.LittleEndian))

	_, sources, err := decodeSourceResponse(w.buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 0 {
		t.Fatalf("source without port kept: %+v", sources)
	}
}

func TestSourceSearchEncoding(t *testing.T) {
	target, _ := ParseID("fedcba9876543210fedcba9876543210")
	payload := encodeSourceSearch(target, 9_728_000)
	if len(payload) != 26 {
		t.Fatalf("length = %d", len(payload))
	}
	r := newReader(payload, binary.LittleEndian)
	got, err := r.kadID()
	if err != nil || got != target {
		t.Fatalf("target = %v, %v", got, err)
	}
	start, _ := r.u16()
	if start != 0 {
		t.Fatalf("start = %d", start)
	}
	size, _ := r.u64()
	if size != 9_728_000 {
		t.Fatalf("size = %d", size)
	}
}

func TestSourceResponseKeepsLowID(t *testing.T) {
	sender, _ := ParseID("11111111111111111111111111111111")
	target, _ := ParseID("22222222222222222222222222222222")
	user, _ := ParseID("33333333333333333333333333333333")
	tags := []Tag{
		{Name: TagSourceIP, Type: TagUint32, Int: 0x01020304},
		{Name: TagSourcePort, Type: TagUint16, Int: 0},
		{Name: TagSourceUDPPort, Type: TagUint16, Int: 4672},
		{Name: TagSourceType, Type: TagUint8, Int: 3},
	}
	w := newWriter(binary.LittleEndian)
	w.kadID(sender)
	w.kadID(target)
	w.u16(1)
	w.kadID(user)
	w.u8(uint8(len(tags)))
	w.raw(WriteTagList(tags, binary.LittleEndian))
	_, sources, err := decodeSourceResponse(w.buf)
	if err != nil || len(sources) != 1 {
		t.Fatalf("sources = %+v, %v", sources, err)
	}
	if sources[0].TCPPort != 0 || sources[0].UDPPort != 4672 || sources[0].Type != 3 {
		t.Fatalf("lowID source = %+v", sources[0])
	}
}

func TestSourceResponseKeepsBuddy(t *testing.T) {
	sender, _ := ParseID("11111111111111111111111111111111")
	target, _ := ParseID("22222222222222222222222222222222")
	user, _ := ParseID("33333333333333333333333333333333")
	buddy, _ := ParseID("44444444444444444444444444444444")
	tags := []Tag{
		{Name: TagSourceIP, Type: TagUint32, Int: 0x01020304},
		{Name: TagSourcePort, Type: TagUint16, Int: 0},
		{Name: TagSourceUDPPort, Type: TagUint16, Int: 4672},
		{Name: TagSourceType, Type: TagUint8, Int: 3},
		{Name: TagServerIP, Type: TagUint32, Int: 0x05060708},
		{Name: TagServerPort, Type: TagUint16, Int: 5000},
		{Name: TagBuddyHash, Type: TagString, Str: buddy.Hex()},
	}
	w := newWriter(binary.LittleEndian)
	w.kadID(sender)
	w.kadID(target)
	w.u16(1)
	w.kadID(user)
	w.u8(uint8(len(tags)))
	w.raw(WriteTagList(tags, binary.LittleEndian))
	_, sources, err := decodeSourceResponse(w.buf)
	if err != nil || len(sources) != 1 {
		t.Fatalf("sources = %+v, %v", sources, err)
	}
	source := sources[0]
	if source.BuddyID != buddy || !source.BuddyIP.Equal(net.IPv4(5, 6, 7, 8)) || source.BuddyPort != 5000 {
		t.Fatalf("buddy = %+v", source)
	}
}

func TestMergeSourcesKeepsLowID(t *testing.T) {
	low := PeerSource{
		LowID: true, ClientID: 7, ServerIP: net.IPv4(9, 8, 7, 6), ServerPort: 5000,
	}
	merged := mergeSources([]PeerSource{low})
	if len(merged) != 1 || !merged[0].LowID || merged[0].ClientID != 7 {
		t.Fatalf("merged = %+v", merged)
	}
}
