package download

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/ed2k/transfer/partfile"
)

type fakeDiscoverer struct {
	sources []ed2k.PeerSource
	err     error
}

func (f fakeDiscoverer) Sources(context.Context, string, int64) ([]ed2k.PeerSource, error) {
	return f.sources, f.err
}

func waitState(t *testing.T, manager *Manager, id, want string) Transfer {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		transfer, ok := manager.Get(id)
		if ok && transfer.State == want {
			return transfer
		}
		time.Sleep(10 * time.Millisecond)
	}
	transfer, _ := manager.Get(id)
	t.Fatalf("state = %q, want %q (transfer %+v)", transfer.State, want, transfer)
	return Transfer{}
}

func TestManagerCompletesTransfer(t *testing.T) {
	data := testData(300_000)
	hashes := testHashes(t, data)
	peerServer := startFakePeer(t, data, "manager.mkv", hashes, false, false)
	host, portText, _ := net.SplitHostPort(peerServer.listener.Addr().String())
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := NewManager(ctx, ManagerConfig{
		Fetch: Config{TempDir: t.TempDir(), HandshakeTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second},
	}, fakeDiscoverer{sources: []ed2k.PeerSource{{IP: net.ParseIP(host), TCPPort: uint16(port), Origin: "test"}}})
	defer manager.Close()

	transfer, err := manager.Add(Link{Hash: hashes.File, Name: "manager.mkv", Size: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	done := waitState(t, manager, transfer.ID, StateCompleted)
	peerServer.wait(t)
	if done.Path == "" || done.Done != done.Size {
		t.Fatalf("completed transfer = %+v", done)
	}
}

func TestManagerNoSources(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := NewManager(ctx, ManagerConfig{}, fakeDiscoverer{})
	defer manager.Close()

	transfer, err := manager.Add(Link{Hash: ed2k.ID{1}, Name: "missing.mkv", Size: 1000})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitState(t, manager, transfer.ID, StateError)
	if failed.Error == "" {
		t.Fatalf("error state without a reason: %+v", failed)
	}
}

func TestManagerSourceFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := NewManager(ctx, ManagerConfig{
		Fetch: Config{TempDir: t.TempDir(), ConnectTimeout: 100 * time.Millisecond},
	}, fakeDiscoverer{err: errors.New("network down")})
	defer manager.Close()

	transfer, err := manager.Add(Link{Hash: ed2k.ID{2}, Name: "down.mkv", Size: 1000})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitState(t, manager, transfer.ID, StateError)
	if failed.Error == "" {
		t.Fatalf("error state without a reason: %+v", failed)
	}
}

func TestManagerPersistsRows(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "transfers.json")
	ctx, cancel := context.WithCancel(context.Background())
	manager := NewManager(ctx, ManagerConfig{StatePath: statePath}, fakeDiscoverer{})
	transfer, err := manager.Add(Link{Hash: ed2k.ID{3}, Name: "persisted.mkv", Size: 1000})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, manager, transfer.ID, StateError)
	manager.Close()
	cancel()

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	reloaded := NewManager(ctx2, ManagerConfig{StatePath: statePath}, fakeDiscoverer{})
	defer reloaded.Close()
	got, ok := reloaded.Get(transfer.ID)
	if !ok {
		t.Fatal("persisted transfer not loaded")
	}
	if got.State != StateError || got.Name != "persisted.mkv" {
		t.Fatalf("reloaded = %+v", got)
	}
	if got.Error == "" {
		t.Fatal("interrupted transfer has no reason")
	}
}

func TestManagerPauseAndResume(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := NewManager(ctx, ManagerConfig{}, fakeDiscoverer{})
	defer manager.Close()
	transfer, err := manager.Add(Link{Hash: ed2k.ID{4}, Name: "pause.mkv", Size: 1000})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, manager, transfer.ID, StateError)
	if !manager.Pause(transfer.ID) {
		t.Fatal("pause refused")
	}
	paused, _ := manager.Get(transfer.ID)
	if paused.State != StatePaused {
		t.Fatalf("paused = %+v", paused)
	}
	if !manager.Resume(transfer.ID) {
		t.Fatal("resume refused")
	}
	waitState(t, manager, transfer.ID, StateError)
}

func TestManagerDownloadsPartsFromTwoPeers(t *testing.T) {
	data := testData(int(partfile.PartSize) + 300_000)
	hashes := testHashes(t, data)
	first := startFakePeer(t, data, "multi.mkv", hashes, false, false)
	second := startFakePeer(t, data, "multi.mkv", hashes, false, false)
	portOf := func(address string) uint16 {
		_, portText, _ := net.SplitHostPort(address)
		port, err := strconv.Atoi(portText)
		if err != nil {
			t.Fatal(err)
		}
		return uint16(port)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := NewManager(ctx, ManagerConfig{
		MaxPeers: 2,
		Fetch:    Config{TempDir: t.TempDir(), HandshakeTimeout: 5 * time.Second, IdleTimeout: 20 * time.Second},
	}, fakeDiscoverer{sources: []ed2k.PeerSource{
		{IP: net.IPv4(127, 0, 0, 1), TCPPort: portOf(first.listener.Addr().String()), Origin: "test"},
		{IP: net.IPv4(127, 0, 0, 1), TCPPort: portOf(second.listener.Addr().String()), Origin: "test"},
	}})
	defer manager.Close()
	transfer, err := manager.Add(Link{Hash: hashes.File, Name: "multi.mkv", Size: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	done := waitState(t, manager, transfer.ID, StateCompleted)
	first.wait(t)
	second.wait(t)
	got, err := os.ReadFile(done.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("parallel download differs")
	}
}
