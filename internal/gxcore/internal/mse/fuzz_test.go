package mse

import (
	"net"
	"testing"
	"time"
)

// FuzzHandshakeIncoming feeds arbitrary bytes to the MSE (Message Stream
// Encryption) incoming handshake over an in-memory connection. A peer that
// speaks garbage before the handshake completes must produce an error, never a
// panic or a hang: the write side is closed right after the fuzz bytes, so
// every read past them returns EOF.
func FuzzHandshakeIncoming(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, 96))
	f.Add(make([]byte, 96+512))
	f.Fuzz(func(t *testing.T, b []byte) {
		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		_ = client.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_ = server.SetDeadline(time.Now().Add(5 * time.Second))
		go func() {
			_, _ = client.Write(b)
			_ = client.Close()
		}()
		s := NewStream(server)
		_ = s.HandshakeIncoming(
			func([20]byte) []byte { return make([]byte, 20) },
			func(m CryptoMethod) CryptoMethod { return m },
		)
	})
}
