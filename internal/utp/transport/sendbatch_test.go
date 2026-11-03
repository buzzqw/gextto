package transport

// sendbatch_test.go pins the burst sender: a write larger than one packet
// travels as one segmented datagram burst (UDP GSO) on Linux, the bytes arrive
// intact and in order, and the one-write-per-packet path still works when GSO
// is not available.

import (
	"bytes"
	"context"
	"io"
	"net"
	"runtime"
	"testing"
	"time"
)

func dialLoopback(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	serverPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewSocketFromPacketConn(serverPC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.CloseNow() })
	clientPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewSocketFromPacketConn(clientPC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.CloseNow() })

	accepted := make(chan net.Conn, 1)
	go func() {
		if c, err := server.Accept(); err == nil {
			accepted <- c
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := client.DialContext(ctx, "udp", serverPC.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	select {
	case srv := <-accepted:
		t.Cleanup(func() { srv.Close() })
		return conn, srv
	case <-ctx.Done():
		t.Fatal("accept timed out")
		return nil, nil
	}
}

// TestWriteBurstKeepsTheBytesAndOrder sends payloads of every shape a write
// can have — a single packet, a whole burst, an odd tail — and reads them back.
func TestWriteBurstKeepsTheBytesAndOrder(t *testing.T) {
	send, recv := dialLoopback(t)
	for _, size := range []int{1, 100, maxPayloadSize - 1, maxPayloadSize, maxPayloadSize + 1, 12 * maxPayloadSize, 200 << 10} {
		payload := make([]byte, size)
		for i := range payload {
			payload[i] = byte(i * 7)
		}
		got := make([]byte, size)
		done := make(chan error, 1)
		go func() {
			_, err := io.ReadFull(recv, got)
			done <- err
		}()
		if _, err := send.Write(payload); err != nil {
			t.Fatalf("size %d: write: %v", size, err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("size %d: read: %v", size, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("size %d: read timed out", size)
		}
		if !bytes.Equal(payload, got) {
			t.Fatalf("size %d: the bytes came back changed", size)
		}
	}
}

// TestWriteBurstUsesOneSegmentedSend checks that a multi-packet write is sent
// with one segmented call on Linux, and that forcing the fallback still
// delivers the same bytes.
func TestWriteBurstUsesOneSegmentedSend(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("UDP GSO is Linux-only")
	}
	for _, disabled := range []bool{false, true} {
		gsoDisabled.Store(disabled)
		beforeBatches := gsoBatches.Value()
		beforePackets := gsoPackets.Value()
		send, recv := dialLoopback(t)
		payload := make([]byte, 16<<10)
		for i := range payload {
			payload[i] = byte(i)
		}
		got := make([]byte, len(payload))
		done := make(chan error, 1)
		go func() {
			_, err := io.ReadFull(recv, got)
			done <- err
		}()
		if _, err := send.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(payload, got) {
			t.Fatalf("gsoDisabled=%v: the bytes came back changed", disabled)
		}
		batches := gsoBatches.Value() - beforeBatches
		packets := gsoPackets.Value() - beforePackets
		if disabled {
			if batches != 0 {
				t.Errorf("with GSO disabled %d segmented sends happened", batches)
			}
			continue
		}
		if batches != 1 {
			t.Errorf("a 16 KiB write used %d segmented sends, want 1", batches)
		}
		if packets < 8 {
			t.Errorf("the segmented send carried %d datagrams, want the whole burst", packets)
		}
	}
	gsoDisabled.Store(false)
}
