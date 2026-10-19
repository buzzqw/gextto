package peer

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
)

func pipePair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	left, right := net.Pipe()
	t.Cleanup(func() { left.Close(); right.Close() })
	return left, right
}

func filledHash(fill byte) ed2k.ID {
	var hash ed2k.ID
	for i := range hash {
		hash[i] = fill
	}
	return hash
}

func TestConnRoundTrip(t *testing.T) {
	left, right := pipePair(t)
	client, server := NewConn(left), NewConn(right)

	done := make(chan struct{})
	go func() {
		defer close(done)
		opcode, payload, err := server.Receive()
		if err != nil {
			t.Errorf("server receive: %v", err)
			return
		}
		if opcode != OpHello || string(payload) != "hello" {
			t.Errorf("server got 0x%02X %q", opcode, payload)
			return
		}
		if err := server.Send(OpHelloAnswer, []byte("answer")); err != nil {
			t.Errorf("server send: %v", err)
		}
	}()

	if err := client.Send(OpHello, []byte("hello")); err != nil {
		t.Fatalf("client send: %v", err)
	}
	opcode, payload, err := client.Receive()
	if err != nil {
		t.Fatalf("client receive: %v", err)
	}
	if opcode != OpHelloAnswer || string(payload) != "answer" {
		t.Fatalf("client got 0x%02X %q", opcode, payload)
	}
	<-done
}

func TestConnHandshake(t *testing.T) {
	left, right := pipePair(t)
	client, server := NewConn(left), NewConn(right)

	done := make(chan struct{})
	go func() {
		defer close(done)
		opcode, payload, err := server.Receive()
		if err != nil {
			t.Errorf("server receive: %v", err)
			return
		}
		if opcode != OpHello {
			t.Errorf("server opcode 0x%02X", opcode)
			return
		}
		hello, err := DecodeHelloRequest(payload)
		if err != nil {
			t.Errorf("decode hello: %v", err)
			return
		}
		if hello.Name != "gextto" || hello.Port != 4662 {
			t.Errorf("hello = %+v", hello)
		}
		answer := Hello{
			UserHash: filledHash(2),
			ClientID: 0x0100007F,
			Port:     5000,
			Name:     "peer",
			Version:  0x3C,
			KadPort:  4672,
		}
		if err := server.Send(OpHelloAnswer, EncodeHello(answer)); err != nil {
			t.Errorf("server answer: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	answer, err := client.Handshake(ctx, Hello{UserHash: filledHash(1), Port: 4662, Name: "gextto"})
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if answer.Name != "peer" || answer.Port != 5000 || answer.KadPort != 4672 {
		t.Fatalf("answer = %+v", answer)
	}
	<-done
}

func TestHandshakeRejectsSelfAndZero(t *testing.T) {
	left, right := pipePair(t)
	client, server := NewConn(left), NewConn(right)
	hash := filledHash(3)

	go func() {
		for i := 0; i < 2; i++ {
			_, payload, err := server.Receive()
			if err != nil {
				return
			}
			if _, err := DecodeHelloRequest(payload); err != nil {
				return
			}
			answer := Hello{UserHash: hash, Port: 4662}
			if i == 1 {
				answer.UserHash = ed2k.ID{}
			}
			if err := server.Send(OpHelloAnswer, EncodeHello(answer)); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := client.Handshake(ctx, Hello{UserHash: hash, Port: 4662}); err == nil {
		t.Fatal("self answer accepted")
	}
	if _, err := client.Handshake(ctx, Hello{UserHash: hash, Port: 4662}); err == nil {
		t.Fatal("zero user hash accepted")
	}
}

func TestReceiveRejectsBadPackets(t *testing.T) {
	t.Run("wrong protocol", func(t *testing.T) {
		left, right := pipePair(t)
		client := NewConn(left)
		go right.Write([]byte{0xAA, OpHello, 0, 0, 0, 0})
		if _, _, err := client.Receive(); err == nil {
			t.Fatal("server protocol accepted")
		}
	})

	t.Run("oversized", func(t *testing.T) {
		left, right := pipePair(t)
		client := NewConn(left)
		if err := client.SetMaxPacketSize(8); err != nil {
			t.Fatal(err)
		}
		header := []byte{EdonkeyProtocol, 0, 0, 0, 0, OpHello}
		binary.LittleEndian.PutUint32(header[1:], 10)
		go right.Write(header)
		if _, _, err := client.Receive(); err == nil {
			t.Fatal("oversized packet accepted")
		}
	})

	t.Run("truncated", func(t *testing.T) {
		left, right := pipePair(t)
		client := NewConn(left)
		header := []byte{EdonkeyProtocol, 0, 0, 0, 0, OpHello}
		binary.LittleEndian.PutUint32(header[1:], 11)
		go func() {
			right.Write(append(header, []byte("abc")...))
			right.Close()
		}()
		if _, _, err := client.Receive(); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("err = %v, want unexpected EOF", err)
		}
	})
}

func TestSendRejectsOversizedPayload(t *testing.T) {
	left, right := pipePair(t)
	client := NewConn(left)
	defer right.Close()
	if err := client.SetMaxPacketSize(4); err != nil {
		t.Fatal(err)
	}
	if err := client.Send(OpHello, make([]byte, 5)); err == nil {
		t.Fatal("oversized payload sent")
	}
	if err := client.SetMaxPacketSize(0); err == nil {
		t.Fatal("zero packet limit accepted")
	}
}

func TestHandshakeRejectsUnexpectedOpcode(t *testing.T) {
	left, right := pipePair(t)
	client, server := NewConn(left), NewConn(right)
	go func() {
		if _, _, err := server.Receive(); err != nil {
			return
		}
		server.Send(OpRequestParts, nil)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := client.Handshake(ctx, Hello{UserHash: filledHash(4), Port: 4662}); err == nil {
		t.Fatal("unexpected opcode accepted")
	}
}
