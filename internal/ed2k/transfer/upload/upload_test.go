//go:debug rsa1024min=0

package upload

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/ed2k/secident"
	"github.com/buzzqw/gextto/internal/ed2k/transfer/partfile"
	"github.com/buzzqw/gextto/internal/ed2k/transfer/peer"
)

func TestUploadServesFile(t *testing.T) {
	dir := t.TempDir()
	data := make([]byte, 300_000)
	for i := range data {
		data[i] = byte(i*17 + 3)
	}
	path := filepath.Join(dir, "shared.mkv")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	server := New(1)
	if err := server.IndexDir(dir); err != nil {
		t.Fatal(err)
	}
	if server.Len() != 1 {
		t.Fatalf("shared files = %d", server.Len())
	}
	file := server.files[onlyHash(t, server)]

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
		server.Handle(conn)
	}()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := peer.NewConn(conn)
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	user := file.Hash
	user[0] ^= 0xFF
	if err := client.Send(peer.OpHello, peer.EncodeHelloRequest(peer.Hello{UserHash: user, Port: 5000, Name: "client"})); err != nil {
		t.Fatal(err)
	}
	opcode, _, err := client.Receive()
	if err != nil || opcode != peer.OpHelloAnswer {
		t.Fatalf("hello answer: opcode=%#x err=%v", opcode, err)
	}
	if opcode, _, err := client.Receive(); err != nil || opcode != peer.OpEmuleInfo {
		t.Fatalf("emule info: opcode=%#x err=%v", opcode, err)
	}

	if err := client.Send(peer.OpRequestFileName, peer.EncodeFileRequest(peer.FileRequest{Hash: file.Hash})); err != nil {
		t.Fatal(err)
	}
	opcode, payload, err := client.Receive()
	if err != nil || opcode != peer.OpFileNameAnswer {
		t.Fatalf("file name: opcode=%#x err=%v", opcode, err)
	}
	answer, err := peer.DecodeFileNameAnswer(payload)
	if err != nil || answer.Hash != file.Hash || answer.Name != "shared.mkv" {
		t.Fatalf("file answer = %+v, %v", answer, err)
	}

	if err := client.Send(peer.OpHashsetRequest, peer.EncodeHashsetRequest(peer.HashsetRequest{Hash: file.Hash})); err != nil {
		t.Fatal(err)
	}
	opcode, payload, err = client.Receive()
	if err != nil || opcode != peer.OpHashsetAnswer {
		t.Fatalf("hashset: opcode=%#x err=%v", opcode, err)
	}
	hashset, err := peer.DecodeHashsetAnswer(payload)
	// A file smaller than one eD2k part has no part hashes: the file hash is
	// the only hash eMule expects.
	if err != nil || hashset.Hash != file.Hash || len(hashset.Hashes) != 0 {
		t.Fatalf("hashset = %+v, %v", hashset, err)
	}

	tree, err := partfile.BuildAICHTreeReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Send(peer.OpAICHFileHashReq, peer.EncodeAICHFileHashRequest(file.Hash)); err != nil {
		t.Fatal(err)
	}
	opcode, payload, err = client.Receive()
	if err != nil || opcode != peer.OpAICHFileHashAns {
		t.Fatalf("AICH master: opcode=%#x err=%v", opcode, err)
	}
	master, err := peer.DecodeAICHFileHashAnswer(payload)
	if err != nil || master.Hash != file.Hash || master.Root != [20]byte(tree.RootHash()) {
		t.Fatalf("AICH master = %+v, %v", master, err)
	}
	if err := client.Send(peer.OpAICHRequest, peer.EncodeAICHRequest(peer.AICHRequest{
		Hash: file.Hash, Part: 0, Root: [20]byte(tree.RootHash()),
	})); err != nil {
		t.Fatal(err)
	}
	opcode, payload, err = client.Receive()
	if err != nil || opcode != peer.OpAICHAnswer {
		t.Fatalf("AICH answer: opcode=%#x err=%v", opcode, err)
	}
	aichAnswer, err := peer.DecodeAICHAnswer(payload)
	if err != nil || aichAnswer.Hash != file.Hash || aichAnswer.Part != 0 {
		t.Fatalf("AICH answer = %+v, %v", aichAnswer, err)
	}
	recovered, err := partfile.NewAICHTree(int64(len(data)), [20]byte(tree.RootHash()))
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.ReadRecoveryData(0, aichAnswer.Data); err != nil {
		t.Fatal(err)
	}

	if err := client.Send(peer.OpStartUploadReq, peer.EncodeFileRequest(peer.FileRequest{Hash: file.Hash})); err != nil {
		t.Fatal(err)
	}
	for _, want := range []byte{peer.OpQueueRanking, peer.OpAcceptUploadReq} {
		opcode, _, err = client.Receive()
		if err != nil || opcode != want {
			t.Fatalf("upload accept: opcode=%#x want=%#x err=%v", opcode, want, err)
		}
	}

	start, end := int64(1000), int64(200_000)
	request := peer.PartRequest{Hash: file.Hash}
	request.Starts[0], request.Ends[0] = uint32(start), uint32(end)
	if err := client.Send(peer.OpRequestParts, peer.EncodePartRequest(request)); err != nil {
		t.Fatal(err)
	}
	opcode, payload, err = client.Receive()
	if err != nil || opcode != peer.OpSendingPart {
		t.Fatalf("sending part: opcode=%#x err=%v", opcode, err)
	}
	part, err := peer.DecodeSendingPart(payload)
	if err != nil || part.Start != uint32(start) || part.End != uint32(end) {
		t.Fatalf("part = %+v, %v", part, err)
	}
	if !bytes.Equal(part.Data, data[start:end]) {
		t.Fatal("uploaded bytes differ")
	}
}

func TestUploadRejectsUnknownFile(t *testing.T) {
	server := New(1)
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	go server.Handle(right)
	client := peer.NewConn(left)
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	if err := client.Send(peer.OpHello, peer.EncodeHelloRequest(peer.Hello{UserHash: ed2k.ID{9}, Port: 5000})); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Receive(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Receive(); err != nil {
		t.Fatal(err)
	}
	var hash ed2k.ID
	hash[0] = 1
	if err := client.Send(peer.OpRequestFileName, peer.EncodeFileRequest(peer.FileRequest{Hash: hash})); err != nil {
		t.Fatal(err)
	}
	opcode, _, err := client.Receive()
	if err != nil || opcode != peer.OpFileNoSuchFile {
		t.Fatalf("opcode=%#x err=%v", opcode, err)
	}
}

func onlyHash(t *testing.T, server *Server) string {
	t.Helper()
	server.mu.RLock()
	defer server.mu.RUnlock()
	for hash := range server.files {
		return hash
	}
	return ""
}

func TestUploadQueueAndCredits(t *testing.T) {
	server := New(1)
	hash := ed2k.ID{1}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	first := peer.NewConn(left)
	if !server.acquire(first, hash) {
		t.Fatal("first slot refused")
	}
	serverSide := peer.NewConn(right)
	testSide := peer.NewConn(left)
	_ = testSide
	ready := make(chan bool, 1)
	go func() { ready <- server.acquire(serverSide, hash) }()
	opcode, payload, err := peer.NewConn(left).Receive()
	if err != nil || opcode != peer.OpQueueRanking {
		t.Fatalf("queue rank: opcode=%#x err=%v", opcode, err)
	}
	if rank, err := peer.DecodeQueueRanking(payload); err != nil || rank.Rank != 1 {
		t.Fatalf("rank = %+v, %v", rank, err)
	}
	server.release()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("queued client was not granted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued client timed out")
	}
	server.release()

	path := filepath.Join(t.TempDir(), "credits.json")
	server.SetCreditPath(path)
	server.addCredit(hash, 1234)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(hash.Hex())) {
		t.Fatalf("credits = %s", raw)
	}
}

func TestUploadSecureIdent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.mkv")
	if err := os.WriteFile(path, []byte("shared"), 0o644); err != nil {
		t.Fatal(err)
	}
	serverKey, err := secident.LoadOrCreate(filepath.Join(t.TempDir(), "server.pem"))
	if err != nil {
		t.Fatal(err)
	}
	clientKey, err := secident.LoadOrCreate(filepath.Join(t.TempDir(), "client.pem"))
	if err != nil {
		t.Fatal(err)
	}
	server := New(1)
	server.SecIdent = serverKey
	if err := server.IndexDir(dir); err != nil {
		t.Fatal(err)
	}

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
		server.Handle(conn)
	}()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := peer.NewConn(conn)
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	user := ed2k.ID{9}
	if err := client.Send(peer.OpHello, peer.EncodeHelloRequest(peer.Hello{UserHash: user, Port: 5000, Name: "client", SecIdent: true})); err != nil {
		t.Fatal(err)
	}
	opcode, payload, err := client.Receive()
	if err != nil || opcode != peer.OpHelloAnswer {
		t.Fatalf("hello answer: opcode=%#x err=%v", opcode, err)
	}
	hello, err := peer.DecodeHello(payload)
	if err != nil {
		t.Fatal(err)
	}
	if hello.MiscOption&0x000F0000 != 0x00010000 {
		t.Fatalf("server did not advertise secure identification: %#x", hello.MiscOption)
	}
	if opcode, _, err := client.Receive(); err != nil || opcode != peer.OpEmuleInfo {
		t.Fatalf("emule info: opcode=%#x err=%v", opcode, err)
	}

	opcode, payload, err = client.Receive()
	if err != nil || opcode != peer.OpSecIdentState {
		t.Fatalf("secure ident state: opcode=%#x err=%v", opcode, err)
	}
	ident := peer.NewSecIdent(clientKey)
	replies, err := ident.HandleState(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, packet := range replies {
		if err := client.SendExtended(packet.Opcode, packet.Payload); err != nil {
			t.Fatal(err)
		}
	}
	start, err := ident.Start()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SendExtended(start.Opcode, start.Payload); err != nil {
		t.Fatal(err)
	}

	for !ident.Verified() {
		opcode, payload, err = client.Receive()
		if err != nil {
			t.Fatalf("secure ident exchange: %v", err)
		}
		switch opcode {
		case peer.OpPublicKey:
			replies, err = ident.HandlePublicKey(payload)
			if err != nil {
				t.Fatal(err)
			}
			for _, packet := range replies {
				if err := client.SendExtended(packet.Opcode, packet.Payload); err != nil {
					t.Fatal(err)
				}
			}
		case peer.OpSignature:
			if _, err := ident.HandleSignature(payload); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unexpected opcode %#x during secure identification", opcode)
		}
	}
}

func TestKnownFileCacheSkipsRehash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "film.bin")
	data := make([]byte, 300_000)
	for i := range data {
		data[i] = byte(i * 7)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	knownPath := filepath.Join(t.TempDir(), "known.met")

	first := New(1)
	first.SetKnownPath(knownPath)
	if err := first.IndexDir(dir); err != nil {
		t.Fatal(err)
	}
	firstHash := onlyHash(t, first)
	if first.Len() != 1 {
		t.Fatalf("shared files = %d", first.Len())
	}
	if _, err := os.Stat(knownPath); err != nil {
		t.Fatalf("known.met not written: %v", err)
	}

	// Same size and modification time: the cache must be trusted.
	changed := make([]byte, len(data))
	for i := range changed {
		changed[i] = byte(i * 11)
	}
	if err := os.WriteFile(path, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	second := New(1)
	second.SetKnownPath(knownPath)
	if err := second.IndexDir(dir); err != nil {
		t.Fatal(err)
	}
	if cached := onlyHash(t, second); cached != firstHash {
		t.Fatal("unchanged metadata was hashed again instead of using known.met")
	}

	// A new modification time invalidates the entry.
	later := mtime.Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	third := New(1)
	third.SetKnownPath(knownPath)
	if err := third.IndexDir(dir); err != nil {
		t.Fatal(err)
	}
	if refreshed := onlyHash(t, third); refreshed == firstHash {
		t.Fatal("changed modification time did not trigger a new hash")
	}
}
