package upload

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
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
	if err != nil || hashset.Hash != file.Hash || len(hashset.Hashes) != 1 {
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
