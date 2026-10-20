package ed2k

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestServerSearchAgainstFakeServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		opcode, _, err := readServerPacket(conn)
		if err != nil || opcode != OpLoginRequest {
			return
		}
		_ = writeServerPacket(conn, OpIDChange, []byte{0, 0, 0, 1})
		opcode, _, err = readServerPacket(conn)
		if err != nil || opcode != OpSearchRequest {
			return
		}
		hash, _ := ParseID("44444444444444444444444444444444")
		w := newWriter(binary.LittleEndian)
		w.u32(1)
		w.id(hash)
		w.u32(0)
		w.u16(0)
		w.u32(3)
		w.tag(Tag{Name: TagFileName, Type: TagString, Str: "Rare Italian Movie 1974.avi"})
		w.tag(Tag{Name: TagFileSize, Type: TagUint32, Int: 700_000_000})
		w.tag(Tag{Name: TagSources, Type: TagUint32, Int: 9})
		_ = writeServerPacket(conn, OpSearchResult, w.buf)
	}()

	client, err := New(Config{
		DataDir:       t.TempDir(),
		UDPListen:     "127.0.0.1:0",
		Servers:       []string{listener.Addr().String()},
		SearchTimeout: 4 * time.Second,
		QueryTimeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	files, err := client.Search(ctx, "Rare Italian Movie")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files: %+v", len(files), files)
	}
	if files[0].Name != "Rare Italian Movie 1974.avi" || files[0].Sources != 9 || files[0].Origin != "server" {
		t.Fatalf("unexpected file: %+v", files[0])
	}
	stats := client.Stats()
	if !stats.ServerIDSet || stats.ServerID != 0x01000000 || stats.LowID {
		t.Fatalf("server id = %#x (set=%v, low=%v)", stats.ServerID, stats.ServerIDSet, stats.LowID)
	}
}

func TestServerMetParsingRejectsGarbage(t *testing.T) {
	if _, err := ParseServerMet([]byte("not a server.met")); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestReadPackedServerPacket(t *testing.T) {
	body := []byte{0x01, 0x02, 0x03, 0x04}
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	payload := append([]byte{OpSearchResult}, compressed.Bytes()...)
	packet := []byte{0xD4}
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(len(payload)))
	packet = append(packet, size...)
	packet = append(packet, payload...)

	opcode, decoded, err := readServerPacket(bytes.NewReader(packet))
	if err != nil {
		t.Fatal(err)
	}
	if opcode != OpSearchResult || !bytes.Equal(decoded, body) {
		t.Fatalf("opcode=%#x body=%x", opcode, decoded)
	}
}

func TestServerSourcesAgainstFakeServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	hash, _ := ParseID("55555555555555555555555555555555")
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		opcode, _, err := readServerPacket(conn)
		if err != nil || opcode != OpLoginRequest {
			return
		}
		_ = writeServerPacket(conn, OpIDChange, []byte{0, 0, 0, 1})
		opcode, payload, err := readServerPacket(conn)
		if err != nil || opcode != OpGetSources || len(payload) != 20 {
			return
		}
		reply := append([]byte(nil), hash[:]...)
		reply = append(reply, 2)
		reply = append(reply, 1, 2, 3, 4, 0x36, 0x12)
		reply = append(reply, 5, 6, 7, 8, 0x88, 0x13)
		_ = writeServerPacket(conn, OpFoundSources, reply)
	}()

	client, err := New(Config{
		DataDir:       t.TempDir(),
		UDPListen:     "127.0.0.1:0",
		Servers:       []string{listener.Addr().String()},
		SearchTimeout: 4 * time.Second,
		QueryTimeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sources, err := client.Sources(ctx, hash.Hex(), 1234)
	if err != nil {
		t.Fatalf("Sources: %v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %+v", sources)
	}
	if !sources[0].IP.Equal(net.IPv4(1, 2, 3, 4)) || sources[0].TCPPort != 4662 || sources[0].Origin != "server" {
		t.Fatalf("source = %+v", sources[0])
	}
	if !sources[1].IP.Equal(net.IPv4(5, 6, 7, 8)) || sources[1].TCPPort != 5000 {
		t.Fatalf("source = %+v", sources[1])
	}
}

func TestSendServerCallback(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan uint32, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		opcode, _, err := readServerPacket(conn)
		if err != nil || opcode != OpLoginRequest {
			return
		}
		_ = writeServerPacket(conn, OpIDChange, []byte{0, 0, 1, 0})
		opcode, payload, err := readServerPacket(conn)
		if err != nil || opcode != OpCallbackReq || len(payload) != 4 {
			return
		}
		done <- binary.LittleEndian.Uint32(payload)
		_ = writeServerPacket(conn, OpCallbackOK, nil)
	}()

	client, err := New(Config{
		DataDir:       t.TempDir(),
		UDPListen:     "127.0.0.1:0",
		Servers:       []string{listener.Addr().String()},
		SearchTimeout: 4 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	source := PeerSource{
		LowID: true, ClientID: 0x00ABCDEF,
		ServerIP: net.ParseIP(host), ServerPort: uint16(port),
	}
	if err := client.SendServerCallback(ctx, source); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-done:
		if id != source.ClientID {
			t.Fatalf("callback id = %#x", id)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not receive callback request")
	}
}
