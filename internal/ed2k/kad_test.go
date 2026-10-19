package ed2k

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeKadNode answers bootstrap, node lookup, hello and keyword search
// requests. It reports one file for the keyword "matrix".
type fakeKadNode struct {
	conn     *net.UDPConn
	id       ID
	fileHash ID
	files    []SearchFile
	queries  chan string
}

func newFakeKadNode(t *testing.T) *fakeKadNode {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := ParseID("11111111111111111111111111111111")
	fileHash, _ := ParseID("22222222222222222222222222222222")
	node := &fakeKadNode{conn: conn, id: id, fileHash: fileHash, queries: make(chan string, 8)}
	t.Cleanup(func() { conn.Close() })
	go node.loop()
	return node
}

func (n *fakeKadNode) address() *net.UDPAddr {
	return n.conn.LocalAddr().(*net.UDPAddr)
}

func (n *fakeKadNode) loop() {
	buffer := make([]byte, 65536)
	for {
		size, addr, err := n.conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		opcode, payload, err := splitKadPacket(buffer[:size])
		if err != nil {
			continue
		}
		switch opcode {
		case KadBootstrapReq:
			n.reply(addr, KadBootstrapRes, encodeBootstrapResponse(n.id, 4662, nil, 20))
		case KadHelloReq:
			n.reply(addr, KadHelloRes, encodeHello(n.id, 4662))
		case KadReq:
			_, target, _, err := decodeNodeRequest(payload)
			if err != nil {
				continue
			}
			n.reply(addr, KadRes, encodeNodeResponse(target, nil))
		case KadSearchKeyReq:
			target, _, _, err := decodeSearchRequest(payload)
			if err != nil {
				continue
			}
			keyword := ""
			for _, candidate := range []string{"matrix", "1999", "1080p", "the"} {
				if KeywordID(candidate) == target {
					keyword = candidate
				}
			}
			select {
			case n.queries <- keyword:
			default:
			}
			w := newWriter(binary.BigEndian)
			w.id(n.id)
			w.id(target)
			w.u16(1)
			w.id(n.fileHash)
			w.u8(2)
			w.tag(Tag{Name: TagFileName, Type: TagString, Str: "The Matrix 1999 1080p.mkv"})
			w.tag(Tag{Name: TagFileSize, Type: TagUint64, Int: 7_000_000_000})
			n.reply(addr, KadSearchRes, w.buf)
		}
	}
}

func (n *fakeKadNode) reply(addr *net.UDPAddr, opcode byte, payload []byte) {
	_, _ = n.conn.WriteToUDP(kadPacket(opcode, payload), addr)
}

func TestKadSearchAgainstFakeNode(t *testing.T) {
	node := newFakeKadNode(t)
	dataDir := t.TempDir()
	seed := Contact{ID: node.id, IP: net.IPv4(127, 0, 0, 1), UDPPort: uint16(node.address().Port), TCPPort: 4662, Version: KadVersion}
	if err := WriteNodesFile(filepath.Join(dataDir, "nodes.dat"), []Contact{seed}); err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{
		DataDir:       dataDir,
		UDPListen:     "127.0.0.1:0",
		SearchTimeout: 4 * time.Second,
		QueryTimeout:  1500 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	files, err := client.Search(ctx, "The Matrix 1999 1080p")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files: %+v", len(files), files)
	}
	file := files[0]
	if file.Name != "The Matrix 1999 1080p.mkv" || file.Size != 7_000_000_000 || file.Hash != node.fileHash.Hex() {
		t.Fatalf("unexpected file: %+v", file)
	}
	if client.Stats().Nodes == 0 {
		t.Fatal("bootstrap did not add a contact")
	}
}

func TestParseNodesFileVersion2Layout(t *testing.T) {
	// Header: count 0, version 2, one contact, then 25 bytes of contact plus
	// the 8-byte Kad UDP key and the verified flag (34 bytes per entry).
	id, _ := ParseID("0123456789abcdef0123456789abcdef")
	w := newWriter(binary.LittleEndian)
	w.u32(0)
	w.u32(2)
	w.u32(1)
	w.id(id)
	w.raw([]byte{192, 0, 2, 1})
	w.u16(4672)
	w.u16(4662)
	w.u8(3)
	w.u32(0x11223344)
	w.u32(0x01020304)
	w.u8(1)
	contacts, err := ParseNodesFile(w.buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 1 {
		t.Fatalf("contacts = %+v", contacts)
	}
	contact := contacts[0]
	if contact.ID != id || contact.IP.String() != "192.0.2.1" || contact.UDPPort != 4672 || contact.TCPPort != 4662 || contact.Version != 3 {
		t.Fatalf("contact = %+v", contact)
	}
}

func TestNodesFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nodes.dat")
	id, _ := ParseID("33333333333333333333333333333333")
	want := Contact{ID: id, IP: net.IPv4(192, 0, 2, 1), UDPPort: 4672, TCPPort: 4662, Version: KadVersion}
	if err := WriteNodesFile(path, []Contact{want}); err != nil {
		t.Fatal(err)
	}
	got := LoadNodesFile(path)
	if len(got) != 1 || got[0].ID != want.ID || got[0].UDPPort != want.UDPPort || got[0].TCPPort != want.TCPPort || got[0].Version != want.Version || !got[0].IP.Equal(want.IP) {
		t.Fatalf("LoadNodesFile = %+v, want %+v", got, want)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("stat: %v", err)
	}
}

func TestParseServerMet(t *testing.T) {
	data := []byte{1, 0, 0, 0, 192, 0, 2, 1, 0x35, 0x12}
	servers, err := ParseServerMet(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0] != "192.0.2.1:4661" {
		t.Fatalf("ParseServerMet = %#v", servers)
	}
}

func TestParseServerMetVersionedWithTags(t *testing.T) {
	w := newWriter(binary.LittleEndian)
	w.u8(0xE0)
	w.u32(1)
	w.raw([]byte{192, 0, 2, 1})
	w.u16(4661)
	w.u32(2)
	w.tag(Tag{Name: 0x01, Type: TagString, Str: "Test server"})
	w.tag(Tag{Name: 0x0E, Type: TagUint32, Int: 1})
	servers, err := ParseServerMet(w.buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0] != "192.0.2.1:4661" {
		t.Fatalf("ParseServerMet = %#v", servers)
	}
}
