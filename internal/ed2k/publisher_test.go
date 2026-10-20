package ed2k

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func decodeOffers(t *testing.T, payload []byte) []SharedFile {
	t.Helper()
	pos := 0
	need := func(n int) []byte {
		if len(payload)-pos < n {
			t.Fatalf("offer payload truncated at %d", pos)
		}
		out := payload[pos : pos+n]
		pos += n
		return out
	}
	count := binary.LittleEndian.Uint32(need(4))
	files := make([]SharedFile, 0, count)
	for i := uint32(0); i < count; i++ {
		var file SharedFile
		copy(file.Hash[:], need(16))
		_ = binary.LittleEndian.Uint32(need(4)) // client ID
		_ = binary.LittleEndian.Uint16(need(2)) // port
		tagCount := binary.LittleEndian.Uint32(need(4))
		tags, consumed, err := ReadTagList(payload[pos:], int(tagCount), binary.LittleEndian)
		if err != nil {
			t.Fatalf("tags: %v", err)
		}
		pos += consumed
		for _, tag := range tags {
			switch tag.Name {
			case TagFileName:
				file.Name = tag.Str
			case TagFileSize:
				file.Size = int64(tag.Int)
			}
		}
		files = append(files, file)
	}
	return files
}

func TestPublisherOffersFiles(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	files := []SharedFile{
		{Hash: ID{1}, Name: "film.mkv", Size: 400_000},
		{Hash: ID{2}, Name: "episode.mkv", Size: 12_000_000},
	}
	client := &Client{cfg: Config{TCPPort: 4662}, servers: []string{listener.Addr().String()}}
	client.userHashValue = ID{9}

	publisher := NewPublisher(client, func() []SharedFile { return files })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go publisher.Run(ctx)

	accepted := make(chan []SharedFile, 1)
	errors := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		opcode, _, err := readServerPacket(conn)
		if err != nil || opcode != OpLoginRequest {
			errors <- err
			return
		}
		if err := writeServerPacket(conn, OpIDChange, []byte{1, 0, 0, 0x7F}); err != nil {
			errors <- err
			return
		}
		opcode, payload, err := readServerPacket(conn)
		if err != nil || opcode != OpOfferFiles {
			errors <- err
			return
		}
		accepted <- decodeOffers(t, payload)
	}()

	select {
	case got := <-accepted:
		if len(got) != 2 {
			t.Fatalf("offered %d files", len(got))
		}
		if got[0].Name != "film.mkv" || got[0].Size != 400_000 || got[0].Hash != files[0].Hash {
			t.Fatalf("first offer = %+v", got[0])
		}
		if got[1].Name != "episode.mkv" || got[1].Size != 12_000_000 {
			t.Fatalf("second offer = %+v", got[1])
		}
	case err := <-errors:
		t.Fatalf("fake server: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("publisher did not offer the files")
	}

	server, offered, _, lastErr := publisher.Status()
	if server == "" || offered != 2 || lastErr != "" {
		t.Fatalf("status = %q, %d, %q", server, offered, lastErr)
	}
}
