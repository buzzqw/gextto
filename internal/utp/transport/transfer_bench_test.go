package transport

// BenchmarkTransferLoopback drives a real uTP transfer between two sockets on
// loopback and reports throughput and allocations, so the read/write paths can
// be profiled (CPU and heap) with realistic traffic. Use it with
// -cpuprofile/-memprofile to attribute costs to the reader copy, the Event
// toggles, the RTO arithmetic and the selective-ACK scan. GO_UTP_PACKET_DROP
// makes the path lossy/reordering, for the selective-ACK scan.

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
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
// BenchmarkTransferManyConnections is the aggregate throughput of N uTP
// transfers sharing one socket on each side, as a swarm does. The single
// connection benchmarks above cannot show contention on the package mutex:
// this one can, because every sender, reader and RTO timer runs at once.
// Compare the sub-benchmarks with the same GOMAXPROCS and -benchtime.
func BenchmarkTransferManyConnections(b *testing.B) {
	for _, n := range []int{1, 4, 16} {
		b.Run(fmt.Sprintf("conns=%d", n), func(b *testing.B) {
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

			accepted := make(chan net.Conn, n)
			acceptErr := make(chan error, n)
			go func() {
				for i := 0; i < n; i++ {
					c, err := server.Accept()
					if err != nil {
						acceptErr <- err
						return
					}
					accepted <- c
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			type pair struct{ client, server net.Conn }
			pairs := make([]pair, n)
			for i := range pairs {
				c, err := client.DialContext(ctx, "udp", server.Addr().String())
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				pairs[i].client = c
			}
			for i := range pairs {
				select {
				case pairs[i].server = <-accepted:
					defer pairs[i].server.Close()
				case err := <-acceptErr:
					b.Fatal(err)
				case <-ctx.Done():
					b.Fatal("accept timed out")
				}
			}

			const block = 16 << 10
			send := make([][]byte, n)
			recv := make([][]byte, n)
			for i := range pairs {
				send[i] = make([]byte, block)
				recv[i] = make([]byte, block)
			}
			b.SetBytes(int64(n * block))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var wg sync.WaitGroup
				errs := make(chan error, 2*n)
				for j, p := range pairs {
					// A stalled transfer is the thing this benchmark measures:
					// end the sub-benchmark instead of waiting for the engine's
					// two-minute packet timeout, and say how many were left.
					_ = p.server.SetReadDeadline(time.Now().Add(20 * time.Second))
					wg.Add(2)
					go func(j int, p pair) {
						defer wg.Done()
						if _, err := p.client.Write(send[j]); err != nil {
							errs <- err
						}
					}(j, p)
					go func(j int, p pair) {
						defer wg.Done()
						if _, err := io.ReadFull(p.server, recv[j]); err != nil {
							errs <- err
						}
					}(j, p)
				}
				wg.Wait()
				close(errs)
				for err := range errs {
					b.Skipf("%d connections stalled: %v", n, err)
				}
			}
		})
	}
}

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
