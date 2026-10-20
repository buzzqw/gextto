package download

import (
	"bytes"
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/ed2k/transfer/partfile"
	"github.com/buzzqw/gextto/internal/ed2k/transfer/peer"
)

type callbackDiscoverer struct {
	source ed2k.PeerSource
}

func (c callbackDiscoverer) Sources(context.Context, string, int64) ([]ed2k.PeerSource, error) {
	return []ed2k.PeerSource{c.source}, nil
}
func (c callbackDiscoverer) SendDirectCallback(context.Context, ed2k.PeerSource, uint16) error {
	return nil
}
func (c callbackDiscoverer) CallbackIdentity() (ed2k.ID, uint16) { return ed2k.ID{9}, 4662 }

func serveCallbackUploader(t *testing.T, conn net.Conn, data []byte, name string, hashes partfile.Hashes) {
	t.Helper()
	client := peer.NewConn(conn)
	defer client.Close()
	hello := peer.Hello{UserHash: ed2k.ID{7}, ClientID: 0x01020304, Port: 5000, Name: "callback-peer"}
	if err := client.Send(peer.OpHello, peer.EncodeHelloRequest(hello)); err != nil {
		t.Errorf("callback hello: %v", err)
		return
	}
	if _, _, err := client.Receive(); err != nil {
		t.Errorf("callback hello answer: %v", err)
		return
	}
	for {
		opcode, payload, err := client.Receive()
		if err != nil {
			return
		}
		switch opcode {
		case peer.OpEmuleInfo:
			continue
		case peer.OpRequestFileName:
			raw := peer.EncodeFileNameAnswer(peer.FileNameAnswer{Hash: hashes.File, Name: name})
			if err := client.Send(peer.OpFileNameAnswer, raw); err != nil {
				return
			}
		case peer.OpHashsetRequest:
			raw, err := peer.EncodeHashsetAnswer(peer.HashsetAnswer{Hash: hashes.File, Hashes: hashes.Parts})
			if err != nil {
				return
			}
			if err := client.Send(peer.OpHashsetAnswer, raw); err != nil {
				return
			}
		case peer.OpStartUploadReq:
			if err := client.Send(peer.OpAcceptUploadReq, nil); err != nil {
				return
			}
		case peer.OpRequestParts:
			request, err := peer.DecodePartRequest(payload)
			if err != nil {
				return
			}
			for i := 0; i < 3; i++ {
				start, end := int64(request.Starts[i]), int64(request.Ends[i])
				if end <= start {
					continue
				}
				raw, err := peer.EncodeSendingPart(peer.SendingPart{
					Hash: hashes.File, Start: uint32(start), End: uint32(end), Data: data[start:end],
				})
				if err != nil {
					return
				}
				if err := client.Send(peer.OpSendingPart, raw); err != nil {
					return
				}
			}
		}
	}
}

func TestManagerDownloadsViaDirectCallback(t *testing.T) {
	data := testData(300_000)
	hashes := testHashes(t, data)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	discoverer := callbackDiscoverer{source: ed2k.PeerSource{
		IP: net.IPv4(127, 0, 0, 1), UDPPort: 4672, Type: 3, UserHash: ed2k.ID{7}, Origin: "kad",
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := NewManager(ctx, ManagerConfig{
		Fetch: Config{TempDir: t.TempDir(), HandshakeTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second},
	}, discoverer)
	defer manager.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				if manager.WantsIncoming(conn) {
					manager.HandleIncoming(conn)
					return
				}
				conn.Close()
			}()
		}
	}()

	// Replace the callback sender with one that actually connects back.
	discoverer2 := &dialingCallback{t: t, addr: listener.Addr().String(), data: data, name: "callback.mkv", hashes: hashes}
	manager.discover = discoverer2

	transfer, err := manager.Add(Link{Hash: hashes.File, Name: "callback.mkv", Size: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	done := waitState(t, manager, transfer.ID, StateCompleted)
	if done.Path == "" {
		t.Fatalf("completed without a path: %+v", done)
	}
	got, err := os.ReadFile(done.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("callback download differs")
	}
}

// dialingCallback acts as the LowID source: it dials our listener and then
// serves the file as the uploader.
type dialingCallback struct {
	t      *testing.T
	addr   string
	data   []byte
	name   string
	hashes partfile.Hashes
}

func (d *dialingCallback) Sources(context.Context, string, int64) ([]ed2k.PeerSource, error) {
	return []ed2k.PeerSource{{IP: net.IPv4(127, 0, 0, 1), UDPPort: 4672, Type: 3, UserHash: ed2k.ID{7}}}, nil
}

func (d *dialingCallback) SendBuddyCallback(context.Context, ed2k.PeerSource, ed2k.ID, uint16) error {
	d.dial()
	return nil
}

func (d *dialingCallback) SendDirectCallback(context.Context, ed2k.PeerSource, uint16) error {
	d.dial()
	return nil
}

func (d *dialingCallback) dial() {
	go func() {
		conn, err := net.Dial("tcp", d.addr)
		if err != nil {
			return
		}
		serveCallbackUploader(d.t, conn, d.data, d.name, d.hashes)
	}()
}

func (d *dialingCallback) CallbackIdentity() (ed2k.ID, uint16) { return ed2k.ID{9}, 4662 }
func (d *dialingCallback) CanCallback() bool                   { return true }

func TestManagerDownloadsViaBuddyCallback(t *testing.T) {
	data := testData(300_000)
	hashes := testHashes(t, data)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	discoverer := &dialingCallback{
		t: t, addr: listener.Addr().String(), data: data, name: "buddy.mkv", hashes: hashes,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := NewManager(ctx, ManagerConfig{
		Fetch: Config{TempDir: t.TempDir(), HandshakeTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second},
	}, discoverer)
	defer manager.Close()
	// The manager must see a LowID source with a buddy and no UDP port.
	manager.discover = &buddyOnlyDiscoverer{dialingCallback: discoverer}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				if manager.WantsIncoming(conn) {
					manager.HandleIncoming(conn)
					return
				}
				conn.Close()
			}()
		}
	}()

	transfer, err := manager.Add(Link{Hash: hashes.File, Name: "buddy.mkv", Size: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	done := waitState(t, manager, transfer.ID, StateCompleted)
	got, err := os.ReadFile(done.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("buddy download differs")
	}
}

type buddyOnlyDiscoverer struct {
	*dialingCallback
}

func (b *buddyOnlyDiscoverer) Sources(context.Context, string, int64) ([]ed2k.PeerSource, error) {
	return []ed2k.PeerSource{{
		IP: net.IPv4(127, 0, 0, 1), Type: 3, UserHash: ed2k.ID{7},
		BuddyID: ed2k.ID{8}, BuddyIP: net.IPv4(127, 0, 0, 1), BuddyPort: 4672,
	}}, nil
}
