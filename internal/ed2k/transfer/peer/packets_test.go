package peer

import (
	"bytes"
	"testing"

	"github.com/buzzqw/gextto/internal/ed2k"
)

func testHash(t *testing.T, value string) ed2k.ID {
	t.Helper()
	hash, err := ed2k.ParseID(value)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestFileNamePackets(t *testing.T) {
	hash := testHash(t, "0123456789abcdef0123456789abcdef")
	request, err := DecodeFileRequest(EncodeFileRequest(FileRequest{Hash: hash}))
	if err != nil || request.Hash != hash {
		t.Fatalf("file request = %+v, %v", request, err)
	}
	answer, err := DecodeFileNameAnswer(EncodeFileNameAnswer(FileNameAnswer{Hash: hash, Name: "movie.mkv"}))
	if err != nil || answer.Hash != hash || answer.Name != "movie.mkv" {
		t.Fatalf("file answer = %+v, %v", answer, err)
	}
	if _, err := DecodeFileNameAnswer([]byte{1, 2}); err == nil {
		t.Fatal("short answer must fail")
	}
}

func TestHashsetPackets(t *testing.T) {
	hash := testHash(t, "0123456789abcdef0123456789abcdef")
	request, err := DecodeHashsetRequest(EncodeHashsetRequest(HashsetRequest{Hash: hash}))
	if err != nil || request.Hash != hash {
		t.Fatalf("hashset request = %+v, %v", request, err)
	}
	answer := HashsetAnswer{Hashes: []ed2k.ID{hash, hash}}
	raw, err := EncodeHashsetAnswer(answer)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeHashsetAnswer(raw)
	if err != nil || len(got.Hashes) != 2 || got.Hashes[0] != hash {
		t.Fatalf("hashset answer = %+v, %v", got, err)
	}
}

func TestPartPackets(t *testing.T) {
	hash := testHash(t, "0123456789abcdef0123456789abcdef")
	request := PartRequest{Hash: hash, Starts: [3]uint32{0, 100, 200}, Ends: [3]uint32{100, 200, 300}}
	got, err := DecodePartRequest(EncodePartRequest(request))
	if err != nil || got.Hash != hash || got.Starts != request.Starts || got.Ends != request.Ends {
		t.Fatalf("part request = %+v, %v", got, err)
	}
	part := SendingPart{Hash: hash, Start: 10, End: 14, Data: []byte("data")}
	raw, err := EncodeSendingPart(part)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeSendingPart(raw)
	if err != nil || !bytes.Equal(decoded.Data, part.Data) || decoded.Start != 10 || decoded.End != 14 {
		t.Fatalf("sending part = %+v, %v", decoded, err)
	}
	if _, err := EncodeSendingPart(SendingPart{Hash: hash, Start: 0, End: 4, Data: []byte("x")}); err == nil {
		t.Fatal("length mismatch must fail")
	}
	compressed := CompressedPart{Hash: hash, Start: 0, Size: 3, Data: []byte("zip")}
	raw, err = EncodeCompressedPart(compressed)
	if err != nil {
		t.Fatal(err)
	}
	decodedCompressed, err := DecodeCompressedPart(raw)
	if err != nil || !bytes.Equal(decodedCompressed.Data, compressed.Data) {
		t.Fatalf("compressed part = %+v, %v", decodedCompressed, err)
	}
}

func TestQueueAndCallbackPackets(t *testing.T) {
	hash := testHash(t, "0123456789abcdef0123456789abcdef")
	ping, err := DecodeReaskFilePing(EncodeReaskFilePing(ReaskFilePing{Hash: hash}))
	if err != nil || ping.Hash != hash {
		t.Fatalf("reask ping = %+v, %v", ping, err)
	}
	ack, err := DecodeReaskAck(EncodeReaskAck(ReaskAck{Rank: 42}))
	if err != nil || ack.Rank != 42 {
		t.Fatalf("reask ack = %+v, %v", ack, err)
	}
	callback := DirectCallbackRequest{Port: 4662, UserHash: hash, Options: 3}
	got, err := DecodeDirectCallbackRequest(EncodeDirectCallbackRequest(callback))
	if err != nil || got.Port != 4662 || got.UserHash != hash || got.Options != 3 {
		t.Fatalf("callback = %+v, %v", got, err)
	}
	if _, err := DecodeDirectCallbackRequest([]byte{1}); err == nil {
		t.Fatal("short callback must fail")
	}
}
