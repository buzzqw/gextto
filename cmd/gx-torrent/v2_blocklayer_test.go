package main

// v2_blocklayer_test.go proves the interop contract of M-08: a v2 seed answers a
// BEP 52 "hash request" at the block layer (base=0), the message a libtorrent
// leecher uses to verify blocks while it downloads. A raw client connects to the
// seed, does the BitTorrent handshake and sends message 21 by hand, then checks
// the "hashes" reply anchors to the file's pieces root with merkle.VerifyHashes.

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	zbencode "github.com/zeebo/bencode"
)

// v2InfoHash20 is the truncated v2 info hash the seed uses as its identity.
func v2InfoHash20(t *testing.T, torrent []byte) [20]byte {
	t.Helper()
	var top struct {
		Info zbencode.RawMessage `bencode:"info"`
	}
	if err := zbencode.DecodeBytes(torrent, &top); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(top.Info)
	var h [20]byte
	copy(h[:], sum[:20])
	return h
}

// rawV2Handshake performs the plain BitTorrent handshake, setting the v2
// reserved bit, and reads the seed's handshake back.
func rawV2Handshake(t *testing.T, conn net.Conn, infoHash [20]byte) {
	t.Helper()
	var buf [68]byte
	buf[0] = 19
	copy(buf[1:], "BitTorrent protocol")
	buf[27] = 0x10 // v2 reserved bit (byte 7)
	copy(buf[28:48], infoHash[:])
	copy(buf[48:68], "-GXTST0-abcdefghijkl")
	if _, err := conn.Write(buf[:]); err != nil {
		t.Fatalf("handshake write: %v", err)
	}
	var reply [68]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		t.Fatalf("handshake read: %v", err)
	}
	if reply[0] != 19 || string(reply[1:20]) != "BitTorrent protocol" {
		t.Fatalf("bad handshake reply: %q", reply[:20])
	}
}

// readPeerMessage returns the id and payload of the next framed peer message,
// skipping keepalives.
func readPeerMessage(t *testing.T, conn net.Conn) (byte, []byte) {
	t.Helper()
	for {
		var length [4]byte
		if _, err := io.ReadFull(conn, length[:]); err != nil {
			t.Fatalf("message length: %v", err)
		}
		n := binary.BigEndian.Uint32(length[:])
		if n == 0 {
			continue // keep-alive
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(conn, buf); err != nil {
			t.Fatalf("message body: %v", err)
		}
		return buf[0], buf[1:]
	}
}

func writeHashRequest(t *testing.T, conn net.Conn, root [32]byte, base, index, length, proof uint32) {
	t.Helper()
	msg := make([]byte, 4+1+48)
	binary.BigEndian.PutUint32(msg[0:4], 49)
	msg[4] = 21 // hash request
	copy(msg[5:37], root[:])
	binary.BigEndian.PutUint32(msg[37:41], base)
	binary.BigEndian.PutUint32(msg[41:45], index)
	binary.BigEndian.PutUint32(msg[45:49], length)
	binary.BigEndian.PutUint32(msg[49:53], proof)
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("hash request write: %v", err)
	}
}

// TestV2BlockLayerHashRequest asks a live seed for the block layer of one file
// and verifies the answer against the file's pieces root.
func TestV2BlockLayerHashRequest(t *testing.T) {
	seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 43600, PortEnd: 43699, Encryption: 1})
	payload := v2Payload(5*v2BlockSize, 0x21) // 5 blocks, layer built from data
	torrent := makeV2TorrentMulti(t, []v2FileSpec{{"a.bin", payload}}, 2*v2BlockSize)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	hash, _, err := seeder.add(addRequest{TorrentData: torrent, Destination: src})
	if err != nil {
		t.Fatalf("seeder add: %v", err)
	}
	waitFor(t, "seeder seeding", func() bool { return stateOf(seeder, hash) == "seeding" })

	root := v2Root(v2LeafHashes(payload))
	padded, logPadded := 1, 0
	for padded < 5 {
		padded <<= 1
		logPadded++
	}
	count := padded
	// proofLayers = log2(padded) - 1: every ancestor level above the leaves.
	proof := uint32(logPadded - 1)

	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(seeder.peerPort), 5*time.Second)
	if err != nil {
		t.Fatalf("dial seed: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	rawV2Handshake(t, conn, v2InfoHash20(t, torrent))
	writeHashRequest(t, conn, root, 0, 0, uint32(count), proof)

	for {
		id, payloadBytes := readPeerMessage(t, conn)
		switch id {
		case 23: // hash reject
			t.Fatal("the seed rejected a base=0 hash request")
		case 22: // hashes
			if len(payloadBytes) != 48+count*32 {
				t.Fatalf("hashes reply has %d bytes, want %d", len(payloadBytes), 48+count*32)
			}
			if !bytes.Equal(payloadBytes[0:32], root[:]) {
				t.Fatalf("reply root = %x, want %x", payloadBytes[0:32], root[:])
			}
			base := binary.BigEndian.Uint32(payloadBytes[32:36])
			length := binary.BigEndian.Uint32(payloadBytes[40:44])
			if base != 0 || length != uint32(count) {
				t.Fatalf("reply base=%d length=%d, want base=0 length=%d", base, length, count)
			}
			hashes := make([][32]byte, count)
			for i := 0; i < count; i++ {
				copy(hashes[i][:], payloadBytes[48+i*32:])
			}
			// count is the whole padded block layer, so folding it gives the root.
			if v2Root(hashes) != root {
				t.Fatal("the block-layer hashes do not anchor to the pieces root")
			}
			return
		default:
			// bitfield, extension handshake, has/have: skip.
		}
	}
}
