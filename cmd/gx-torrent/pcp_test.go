package main

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// startFakePCP runs a one-shot PCP MAP responder on loopback. mutate, when set,
// can perturb the response (or read the request) before it is sent.
func startFakePCP(t *testing.T, mutate func(req, resp []byte)) (*net.UDPAddr, func()) {
	t.Helper()
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1100)
		n, raddr, err := pc.ReadFromUDP(buf)
		if err != nil || n < pcpRequestLen {
			return
		}
		resp := make([]byte, pcpResponseMin)
		resp[0] = pcpVersion
		resp[1] = 0x80 | pcpOpcodeMap // response opcode
		binary.BigEndian.PutUint32(resp[4:8], 7200)
		copy(resp[24:36], buf[24:36]) // echo the nonce
		resp[36] = buf[36]
		copy(resp[40:42], buf[40:42]) // internal port
		binary.BigEndian.PutUint16(resp[42:44], 62000)
		copy(resp[44:60], net.ParseIP("203.0.113.9").To16())
		if mutate != nil {
			mutate(buf[:n], resp)
		}
		_, _ = pc.WriteToUDP(resp, raddr)
	}()
	return pc.LocalAddr().(*net.UDPAddr), func() {
		_ = pc.Close()
		<-done
	}
}

func TestPCPMapSuccess(t *testing.T) {
	srv, stop := startFakePCP(t, nil)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	extPort, extIP, err := pcpMap(ctx, srv.IP, srv.Port, pcpProtoTCP, 6881, 3600)
	if err != nil {
		t.Fatalf("pcpMap: %v", err)
	}
	if extPort != 62000 {
		t.Errorf("external port = %d, want 62000", extPort)
	}
	if !extIP.Equal(net.ParseIP("203.0.113.9")) {
		t.Errorf("external IP = %v, want 203.0.113.9", extIP)
	}
}

func TestPCPMapNonceMismatch(t *testing.T) {
	srv, stop := startFakePCP(t, func(_, resp []byte) { resp[24] ^= 0xff })
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := pcpMap(ctx, srv.IP, srv.Port, pcpProtoUDP, 6881, 0); err == nil {
		t.Fatal("a response with a mismatched nonce must be rejected")
	}
}

func TestPCPMapErrorResult(t *testing.T) {
	srv, stop := startFakePCP(t, func(_, resp []byte) { resp[3] = 2 }) // NOT_AUTHORIZED
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := pcpMap(ctx, srv.IP, srv.Port, pcpProtoTCP, 6881, 3600); err == nil {
		t.Fatal("a non-zero result code must be reported as an error")
	}
}

func TestPCPMapRequestShape(t *testing.T) {
	// The responder runs in its own goroutine: hand the captured request back
	// through a channel so the read below is ordered after the write (the
	// server sends before it answers, so the value is already buffered).
	requests := make(chan []byte, 1)
	srv, stop := startFakePCP(t, func(req, _ []byte) { requests <- append([]byte(nil), req...) })
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := pcpMap(ctx, srv.IP, srv.Port, pcpProtoTCP, 6881, 3600); err != nil {
		t.Fatalf("pcpMap: %v", err)
	}
	got := <-requests
	if len(got) != pcpRequestLen {
		t.Fatalf("request length = %d, want %d", len(got), pcpRequestLen)
	}
	if got[0] != pcpVersion || got[1] != pcpOpcodeMap || got[36] != pcpProtoTCP {
		t.Errorf("request header = % x", got[:2])
	}
	if binary.BigEndian.Uint32(got[4:8]) != 3600 {
		t.Errorf("lifetime = %d, want 3600", binary.BigEndian.Uint32(got[4:8]))
	}
	if binary.BigEndian.Uint16(got[40:42]) != 6881 {
		t.Errorf("internal port = %d, want 6881", binary.BigEndian.Uint16(got[40:42]))
	}
}
