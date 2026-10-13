package torrent

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/utp"
	"github.com/rcrowley/go-metrics"
)

// stubSessionMetrics builds a metrics set without a live session, so Stats()
// can be exercised on a bare Session.
func stubSessionMetrics() *sessionMetrics {
	return &sessionMetrics{
		Uptime:                metrics.NewGauge(),
		Torrents:              metrics.NewGauge(),
		Peers:                 metrics.NewCounter(),
		PortsAvailable:        metrics.NewGauge(),
		BlockListRules:        metrics.NewGauge(),
		BlockListRecency:      metrics.NewGauge(),
		ReadCacheObjects:      metrics.NewGauge(),
		ReadCacheSize:         metrics.NewGauge(),
		ReadCacheUtilization:  metrics.NewGauge(),
		ReadsPerSecond:        metrics.NewMeter(),
		ReadsActive:           metrics.NewGauge(),
		ReadsPending:          metrics.NewGauge(),
		WriteCacheObjects:     metrics.NewGauge(),
		WriteCacheSize:        metrics.NewGauge(),
		WriteCachePendingKeys: metrics.NewGauge(),
		WritesPerSecond:       metrics.NewMeter(),
		WritesActive:          metrics.NewGauge(),
		WritesPending:         metrics.NewGauge(),
		SpeedDownload:         metrics.NewMeter(),
		SpeedUpload:           metrics.NewMeter(),
		SpeedRead:             metrics.NewMeter(),
		SpeedWrite:            metrics.NewMeter(),
	}
}

// Stats() must carry the uTP socket counters: zero without a socket, live
// values once the session owns one.
func TestSessionStatsCarriesUTPSocketCounters(t *testing.T) {
	s := &Session{metrics: stubSessionMetrics(), createdAt: time.Now()}
	st := s.Stats()
	if st.UTP {
		t.Fatal("UTP reported active without a socket")
	}
	if st.UTPDials != 0 || st.UTPAccepts != 0 || st.UTPDialErrors != 0 || st.UTPAcceptErrors != 0 {
		t.Fatalf("expected zero socket counters, got %+v", st)
	}

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sock, err := utp.NewSocketFromPacketConn(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer sock.CloseNow()
	s.utpSocket = sock

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	// Closed port: the dial must fail, bumping both counters exactly once.
	_, _ = sock.DialContext(ctx, "udp", "127.0.0.1:1")

	st = s.Stats()
	if !st.UTP {
		t.Fatal("UTP not reported active with a socket")
	}
	if st.UTPDials != 1 || st.UTPDialErrors != 1 {
		t.Fatalf("expected 1 dial/1 dial error, got dials=%d errors=%d", st.UTPDials, st.UTPDialErrors)
	}
	if st.UTPAccepts != 0 || st.UTPAcceptErrors != 0 {
		t.Fatalf("expected zero accepts, got %+v", st)
	}
}
