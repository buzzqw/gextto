package udptracker

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/blocklist"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

func TestParseScrapeResponse(t *testing.T) {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, udpMessageHeader{Action: actionScrape, TransactionID: 1})
	_ = binary.Write(&buf, binary.BigEndian, udpScrapeResponse{Seeders: 5, Completed: 11, Leechers: 2})

	tr := &UDPTracker{}
	got, err := tr.parseScrapeResponse(buf.Bytes())
	if err != nil {
		t.Fatalf("parseScrapeResponse: %s", err)
	}
	if got.Seeders != 5 || got.Downloaded != 11 || got.Leechers != 2 {
		t.Fatalf("got seeders=%d downloaded=%d leechers=%d, want 5/11/2", got.Seeders, got.Downloaded, got.Leechers)
	}
}

func TestScrapeRequestEncoding(t *testing.T) {
	hash := [20]byte{}
	for i := range hash {
		hash[i] = byte(i)
	}
	req := newTransportScrapeRequest(context.Background(), hash, "127.0.0.1:1234")
	req.SetConnectionID(0x0102030405060708)

	var buf bytes.Buffer
	if _, err := req.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if len(b) != 36 {
		t.Fatalf("encoded %d bytes, want 36", len(b))
	}
	if got := binary.BigEndian.Uint64(b[0:8]); got != 0x0102030405060708 {
		t.Errorf("connection id = %#x", got)
	}
	if got := int32(binary.BigEndian.Uint32(b[8:12])); got != int32(actionScrape) {
		t.Errorf("action = %d, want %d", got, actionScrape)
	}
	if !bytes.Equal(b[16:36], hash[:]) {
		t.Errorf("info hash mismatch: %x", b[16:36])
	}
}

func TestUDPScrape(t *testing.T) {
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go serveScrapeTracker(pc)

	transport := NewTransport(blocklist.New(), time.Second)
	go transport.Run()
	defer transport.Close()

	raw := "udp://" + pc.LocalAddr().String()
	u, _ := url.Parse(raw)
	tr := New(raw, u, transport)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := tr.Scrape(ctx, tracker.ScrapeRequest{Torrent: tracker.Torrent{InfoHash: [20]byte{9}}})
	if err != nil {
		t.Fatalf("Scrape: %s", err)
	}
	if resp.Seeders != 5 || resp.Leechers != 2 || resp.Downloaded != 11 {
		t.Fatalf("got seeders=%d leechers=%d downloaded=%d, want 5/2/11", resp.Seeders, resp.Leechers, resp.Downloaded)
	}
}

// serveScrapeTracker answers the connect and scrape transactions of the UDP
// protocol, enough for a scrape round-trip.
func serveScrapeTracker(pc *net.UDPConn) {
	buf := make([]byte, 2048)
	for {
		n, addr, err := pc.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if n < 16 {
			continue
		}
		// Requests begin with connection_id (8) then action (4) then
		// transaction_id (4); the response header has no connection id.
		action := action(binary.BigEndian.Uint32(buf[8:12]))
		transactionID := int32(binary.BigEndian.Uint32(buf[12:16]))

		resp := new(bytes.Buffer)
		switch action {
		case actionConnect:
			_ = binary.Write(resp, binary.BigEndian, connectResponse{
				udpMessageHeader: udpMessageHeader{Action: actionConnect, TransactionID: transactionID},
				ConnectionID:     0x0102030405060708,
			})
		case actionScrape:
			_ = binary.Write(resp, binary.BigEndian, udpMessageHeader{Action: actionScrape, TransactionID: transactionID})
			_ = binary.Write(resp, binary.BigEndian, udpScrapeResponse{Seeders: 5, Completed: 11, Leechers: 2})
		default:
			continue
		}
		_, _ = pc.WriteToUDP(resp.Bytes(), addr)
	}
}
