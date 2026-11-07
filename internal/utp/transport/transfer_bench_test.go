package transport

// BenchmarkTransferLoopback drives a real uTP transfer between two sockets on
// loopback and reports throughput and allocations, so the read/write paths can
// be profiled (CPU and heap) with realistic traffic. Use it with
// -cpuprofile/-memprofile to attribute costs to the reader copy, the Event
// toggles, the RTO arithmetic and the selective-ACK scan. GO_UTP_PACKET_DROP
// makes the path lossy/reordering, for the selective-ACK scan.

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func BenchmarkTransferLoopback(b *testing.B) {
	serverPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	server, err := NewSocketFromPacketConn(serverPC)
	if err != nil {
		b.Fatal(err)
	}
	defer server.CloseNow()
	clientPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	client, err := NewSocketFromPacketConn(clientPC)
	if err != nil {
		b.Fatal(err)
	}
	defer client.CloseNow()

	accepted := make(chan net.Conn, 1)
	go func() {
		if c, err := server.Accept(); err == nil {
			accepted <- c
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := client.DialContext(ctx, "udp", server.Addr().String())
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()
	var srv net.Conn
	select {
	case srv = <-accepted:
	case <-ctx.Done():
		b.Fatal("accept timed out")
	}
	defer srv.Close()

	const block = 16 << 10
	send := make([]byte, block)
	recv := make([]byte, block)
	b.SetBytes(block)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := conn.Write(send); err != nil {
			b.Fatal(err)
		}
		if _, err := io.ReadFull(srv, recv); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTransferLoopbackNoGSO is the same transfer with the segmented send
// turned off, one write per packet: the before-and-after of the burst sender
// on the same code, same machine.
func BenchmarkTransferLoopbackNoGSO(b *testing.B) {
	gsoDisabled.Store(true)
	defer gsoDisabled.Store(false)
	BenchmarkTransferLoopback(b)
}

// BenchmarkTransferLoopbackStream is a download as the engine does it: the
// sender writes without waiting, the reader asks for each 16 KiB block with
// HintRead and reads the message header and the block apart, so the receive
// window fills and drains in bursts. The allocations it reports are the
// receive path's, which the lockstep benchmark above never exercises.
func BenchmarkTransferLoopbackStream(b *testing.B) {
	serverPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	server, err := NewSocketFromPacketConn(serverPC)
	if err != nil {
		b.Fatal(err)
	}
	defer server.CloseNow()
	clientPC, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	client, err := NewSocketFromPacketConn(clientPC)
	if err != nil {
		b.Fatal(err)
	}
	defer client.CloseNow()

	accepted := make(chan net.Conn, 1)
	go func() {
		if c, err := server.Accept(); err == nil {
			accepted <- c
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := client.DialContext(ctx, "udp", server.Addr().String())
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()
	var srv net.Conn
	select {
	case srv = <-accepted:
	case <-ctx.Done():
		b.Fatal("accept timed out")
	}
	defer srv.Close()

	const header, block = 13, 16 << 10
	go func() {
		send := make([]byte, 64*(header+block))
		for {
			if _, err := conn.Write(send); err != nil {
				return
			}
		}
	}()
	hint := srv.(interface{ HintRead(int) })
	head := make([]byte, header)
	recv := make([]byte, block)
	b.SetBytes(header + block)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := io.ReadFull(srv, head); err != nil {
			b.Fatal(err)
		}
		hint.HintRead(block)
		if _, err := io.ReadFull(srv, recv); err != nil {
			b.Fatal(err)
		}
	}
}
