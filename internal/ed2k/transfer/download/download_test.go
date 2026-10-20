package download

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/ed2k/transfer/partfile"
	"github.com/buzzqw/gextto/internal/ed2k/transfer/peer"
)

type fakePeer struct {
	listener net.Listener
	errs     chan error
	done     chan struct{}
}

func startFakePeer(t *testing.T, data []byte, name string, hashes partfile.Hashes, queue bool, corrupt bool) *fakePeer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakePeer{listener: listener, errs: make(chan error, 1), done: make(chan struct{})}
	t.Cleanup(func() { listener.Close() })
	go func() {
		defer close(fake.done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		client := peer.NewConn(conn)
		if err := serveFakePeer(client, data, name, hashes, queue, corrupt); err != nil && !errors.Is(err, io.EOF) {
			fake.errs <- err
		}
	}()
	return fake
}

func startObfuscatedFakePeer(t *testing.T, data []byte, name string, hashes partfile.Hashes) *fakePeer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakePeer{listener: listener, errs: make(chan error, 1), done: make(chan struct{})}
	t.Cleanup(func() { listener.Close() })
	peerHash := hashes.File
	peerHash[0] ^= 0xFF
	if peerHash == (ed2k.ID{}) {
		peerHash[0] = 1
	}
	go func() {
		defer close(fake.done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		wrapped, err := peer.AcceptObfuscated(conn, peerHash)
		if err != nil {
			fake.errs <- err
			return
		}
		client := peer.NewConn(wrapped)
		if err := serveFakePeer(client, data, name, hashes, false, false); err != nil && !errors.Is(err, io.EOF) {
			fake.errs <- err
		}
	}()
	return fake
}

func (f *fakePeer) wait(t *testing.T) {
	t.Helper()
	select {
	case <-f.done:
	case err := <-f.errs:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("fake peer did not finish")
	}
}

func serveFakePeer(client *peer.Conn, data []byte, name string, hashes partfile.Hashes, queue bool, corrupt bool) error {
	opcode, payload, err := client.Receive()
	if err != nil {
		return err
	}
	if opcode != peer.OpHello {
		return errors.New("fake peer: expected hello")
	}
	if _, err := peer.DecodeHelloRequest(payload); err != nil {
		return err
	}
	answer := peer.Hello{UserHash: hashes.File, Port: 5000, Name: "fake"}
	answer.UserHash[0] ^= 0xFF
	if answer.UserHash == (ed2k.ID{}) {
		answer.UserHash[0] = 1
	}
	if err := client.Send(peer.OpHelloAnswer, peer.EncodeHello(answer)); err != nil {
		return err
	}

	aichTree, _ := partfile.BuildAICHTreeReader(bytes.NewReader(data), int64(len(data)))
	accepted := false
	for {
		opcode, payload, err := client.Receive()
		if err != nil {
			return err
		}
		switch opcode {
		case peer.OpRequestFileName:
			request, err := peer.DecodeFileRequest(payload)
			if err != nil {
				return err
			}
			if request.Hash != hashes.File {
				return errors.New("fake peer: wrong file request")
			}
			raw := peer.EncodeFileNameAnswer(peer.FileNameAnswer{Hash: hashes.File, Name: name})
			if err := client.Send(peer.OpFileNameAnswer, raw); err != nil {
				return err
			}
		case peer.OpAICHRequest:
			request, err := peer.DecodeAICHRequest(payload)
			if err != nil || request.Hash != hashes.File || aichTree == nil {
				return errors.New("fake peer: invalid AICH request")
			}
			recovery, err := aichTree.RecoveryData(int(request.Part))
			if err != nil {
				return err
			}
			raw := peer.EncodeAICHAnswer(peer.AICHAnswer{
				Hash: hashes.File, Part: request.Part, Root: request.Root, Data: recovery,
			})
			if err := client.Send(peer.OpAICHAnswer, raw); err != nil {
				return err
			}
		case peer.OpAICHFileHashReq:
			request, err := peer.DecodeAICHFileHashRequest(payload)
			if err != nil || request != hashes.File {
				return errors.New("fake peer: invalid AICH hash request")
			}
			root, err := partfile.AICHRootReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				return err
			}
			raw := peer.EncodeAICHFileHashAnswer(peer.AICHMasterHash{Hash: hashes.File, Root: [20]byte(root)})
			if err := client.Send(peer.OpAICHFileHashAns, raw); err != nil {
				return err
			}
		case peer.OpHashsetRequest:
			raw, err := peer.EncodeHashsetAnswer(peer.HashsetAnswer{Hash: hashes.File, Hashes: hashes.Parts})
			if err != nil {
				return err
			}
			if err := client.Send(peer.OpHashsetAnswer, raw); err != nil {
				return err
			}
		case peer.OpSetReqFileID:
			if !accepted {
				return errors.New("fake peer: OP_SETREQFILEID before the upload slot")
			}
			request, err := peer.DecodeFileRequest(payload)
			if err != nil || request.Hash != hashes.File {
				return errors.New("fake peer: invalid file-status request")
			}
			parts := len(hashes.Parts)
			bits := make([]byte, (parts+7)/8)
			for i := range bits {
				bits[i] = 0xFF
			}
			raw, err := peer.EncodeFileStatus(peer.FileStatus{Hash: hashes.File, Parts: parts, Bits: bits})
			if err != nil {
				return err
			}
			if err := client.Send(peer.OpFileStatus, raw); err != nil {
				return err
			}
		case peer.OpStartUploadReq:
			if queue {
				if err := client.Send(peer.OpQueueRanking, peer.EncodeQueueRanking(peer.QueueRanking{Rank: 4})); err != nil {
					return err
				}
				continue
			}
			if err := client.Send(peer.OpQueueRanking, peer.EncodeQueueRanking(peer.QueueRanking{Rank: 1})); err != nil {
				return err
			}
			if err := client.Send(peer.OpAcceptUploadReq, nil); err != nil {
				return err
			}
			accepted = true
		case peer.OpRequestParts:
			request, err := peer.DecodePartRequest(payload)
			if err != nil {
				return err
			}
			for i := 0; i < 3; i++ {
				start, end := int64(request.Starts[i]), int64(request.Ends[i])
				if end <= start {
					continue
				}
				if start < 0 || end > int64(len(data)) {
					return errors.New("fake peer: range out of file")
				}
				chunk := append([]byte(nil), data[start:end]...)
				if corrupt {
					chunk[0] ^= 0xFF
				}
				raw, err := peer.EncodeSendingPart(peer.SendingPart{
					Hash: hashes.File, Start: uint32(start), End: uint32(end), Data: chunk,
				})
				if err != nil {
					return err
				}
				if err := client.Send(peer.OpSendingPart, raw); err != nil {
					return err
				}
			}
		case peer.OpCancelTransfer:
			return nil
		}
	}
}

func testData(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*31 + 7)
	}
	return data
}

func testHashes(t *testing.T, data []byte) partfile.Hashes {
	t.Helper()
	hashes, err := partfile.HashReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return hashes
}

func TestFetchFromFakePeer(t *testing.T) {
	data := testData(500_000)
	hashes := testHashes(t, data)
	peerServer := startFakePeer(t, data, "film.mkv", hashes, false, false)

	dir := t.TempDir()
	result, err := Fetch(context.Background(), peerServer.listener.Addr().String(), Link{
		Hash: hashes.File, Name: "film.mkv", Size: int64(len(data)),
	}, Config{TempDir: dir, HandshakeTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	peerServer.wait(t)

	if !result.Verified || result.Hash != hashes.File {
		t.Fatalf("result = %+v", result)
	}
	got, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("downloaded file differs")
	}
	if _, err := os.Stat(filepath.Join(dir, hashes.File.Hex()+".part.met")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("metadata not removed: %v", err)
	}
}

func TestFetchObfuscated(t *testing.T) {
	data := testData(300_000)
	hashes := testHashes(t, data)
	peerServer := startObfuscatedFakePeer(t, data, "obfuscated.mkv", hashes)
	peerHash := hashes.File
	peerHash[0] ^= 0xFF

	dir := t.TempDir()
	result, err := Fetch(context.Background(), peerServer.listener.Addr().String(), Link{
		Hash: hashes.File, Name: "obfuscated.mkv", Size: int64(len(data)),
	}, Config{
		TempDir: dir, HandshakeTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second,
		Obfuscation: true, PeerHash: peerHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	peerServer.wait(t)
	got, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("obfuscated download differs")
	}
}

func TestFetchResumesCompletePart(t *testing.T) {
	data := testData(int(partfile.PartSize) + 300_000)
	hashes := testHashes(t, data)
	dir := t.TempDir()
	partPath := filepath.Join(dir, hashes.File.Hex()+".part")

	meta := partfile.NewMeta(hashes.File, hashes.Parts, "resume.mkv", int64(len(data)), uint32(time.Now().Unix()))
	pf, err := partfile.Create(partPath, meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := pf.WritePart(0, data[:partfile.PartSize]); err != nil {
		t.Fatal(err)
	}
	pf.Close()

	peerServer := startFakePeer(t, data, "resume.mkv", hashes, false, false)
	result, err := Fetch(context.Background(), peerServer.listener.Addr().String(), Link{
		Hash: hashes.File, Name: "resume.mkv", Size: int64(len(data)),
	}, Config{TempDir: dir, HandshakeTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	peerServer.wait(t)

	if !result.Resumed {
		t.Fatal("resume not reported")
	}
	got, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("resumed file differs")
	}
}

func TestFetchQueueTimeout(t *testing.T) {
	data := testData(200_000)
	hashes := testHashes(t, data)
	peerServer := startFakePeer(t, data, "queued.mkv", hashes, true, false)

	_, err := Fetch(context.Background(), peerServer.listener.Addr().String(), Link{
		Hash: hashes.File, Name: "queued.mkv", Size: int64(len(data)),
	}, Config{
		TempDir: t.TempDir(), HandshakeTimeout: 5 * time.Second, QueueTimeout: 150 * time.Millisecond,
	})
	if !IsQueued(err) {
		t.Fatalf("err = %v, want queued", err)
	}
}

func TestFetchRejectsCorruptPart(t *testing.T) {
	data := testData(200_000)
	hashes := testHashes(t, data)
	peerServer := startFakePeer(t, data, "bad.mkv", hashes, false, true)

	_, err := Fetch(context.Background(), peerServer.listener.Addr().String(), Link{
		Hash: hashes.File, Name: "bad.mkv", Size: int64(len(data)),
	}, Config{
		TempDir: t.TempDir(), HandshakeTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second,
		MaxPartAttempts: 2,
	})
	if err == nil {
		t.Fatal("corrupt part accepted")
	}
}

func TestFetchAICHRejectsCorruptBlocks(t *testing.T) {
	data := testData(500_000)
	hashes := testHashes(t, data)
	peerServer := startFakePeer(t, data, "bad-aich.mkv", hashes, false, true)

	_, err := Fetch(context.Background(), peerServer.listener.Addr().String(), Link{
		Hash: hashes.File, Name: "bad-aich.mkv", Size: int64(len(data)),
	}, Config{
		TempDir: t.TempDir(), HandshakeTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second,
		MaxPartAttempts: 2,
		OnAICH:          func([20]byte) {},
	})
	if err == nil {
		t.Fatal("corrupt AICH blocks accepted")
	}
}

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"film.mkv":         "film.mkv",
		"../film.mkv":      "film.mkv",
		`C:\temp\film.mkv`: "film.mkv",
		"  .hidden.  ":     ".hidden",
		"bad\x00name.mkv":  "badname.mkv",
		"..":               "",
		".":                "",
		"":                 "",
	}
	for input, want := range cases {
		if got := SafeName(input); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", input, got, want)
		}
	}
}
