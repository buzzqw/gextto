package peer

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
)

func TestObfuscationRoundTrip(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	hash := ed2k.ID{1, 2, 3}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	accepted := make(chan net.Conn, 1)
	errs := make(chan error, 1)
	go func() {
		conn, err := AcceptObfuscated(server, hash)
		if err != nil {
			errs <- err
			return
		}
		accepted <- conn
	}()
	conn, err := NewOutgoingObfuscated(ctx, client, hash)
	if err != nil {
		t.Fatal(err)
	}
	var serverConn net.Conn
	select {
	case serverConn = <-accepted:
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("incoming handshake timed out")
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_ = serverConn.SetDeadline(time.Now().Add(5 * time.Second))
	payload := []byte("hello over obfuscation")
	go func() { _, _ = conn.Write(payload) }()
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(serverConn, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("server read %q", got)
	}
	go func() { _, _ = serverConn.Write(payload) }()
	got = make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("client read %q", got)
	}
}

func TestAcceptObfuscatedKeepsClearConnections(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	go func() {
		_, _ = client.Write([]byte{EdonkeyProtocol, 0, 0, 0, 1, OpHello, 0xAA})
	}()
	conn, err := AcceptObfuscated(server, ed2k.ID{1})
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 7)
	if _, err := io.ReadFull(conn, buffer); err != nil {
		t.Fatal(err)
	}
	if buffer[0] != EdonkeyProtocol || buffer[5] != OpHello || buffer[6] != 0xAA {
		t.Fatalf("clear bytes = %x", buffer)
	}
}
