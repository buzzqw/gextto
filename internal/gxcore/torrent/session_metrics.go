package torrent

import (
	"time"

	"github.com/rcrowley/go-metrics"
)

// sessionMetrics exposes the session's live counters through a
// go-metrics registry. Gauges are functional: they are evaluated on read,
// so they always reflect the current state without a refresh loop.
type sessionMetrics struct {
	session  *Session
	registry metrics.Registry

	Torrents              metrics.Gauge
	Peers                 metrics.Counter
	PortsAvailable        metrics.Gauge
	Uptime                metrics.Gauge
	BlockListRules        metrics.Gauge
	BlockListRecency      metrics.Gauge
	ReadCacheObjects      metrics.Gauge
	ReadCacheSize         metrics.Gauge
	ReadCacheUtilization  metrics.Gauge
	ReadsPerSecond        metrics.Meter
	ReadsActive           metrics.Gauge
	ReadsPending          metrics.Gauge
	WriteCacheObjects     metrics.Gauge
	WriteCacheSize        metrics.Gauge
	WriteCachePendingKeys metrics.Gauge
	WritesPerSecond       metrics.Meter
	WritesActive          metrics.Gauge
	WritesPending         metrics.Gauge
	SpeedDownload         metrics.Meter
	SpeedUpload           metrics.Meter
	SpeedRead             metrics.Meter
	SpeedWrite            metrics.Meter
}

func (s *Session) initMetrics() {
	r := metrics.NewRegistry()
	s.metrics = &sessionMetrics{session: s, registry: r}
	m := s.metrics

	// gauge registers a functional gauge and assigns it to dst in one step,
	// keeping the metric name next to the code that computes it.
	gauge := func(dst *metrics.Gauge, name string, f func() int64) {
		*dst = metrics.NewRegisteredFunctionalGauge(name, r, f)
	}

	gauge(&m.Uptime, "uptime", func() int64 {
		return int64(time.Since(s.createdAt) / time.Second)
	})
	gauge(&m.Torrents, "torrents", func() int64 {
		s.mTorrents.RLock()
		defer s.mTorrents.RUnlock()
		return int64(len(s.torrents))
	})
	m.Peers = metrics.NewRegisteredCounter("peers", r)
	gauge(&m.PortsAvailable, "ports_available", func() int64 {
		s.mPorts.RLock()
		defer s.mPorts.RUnlock()
		return int64(len(s.availablePorts))
	})

	gauge(&m.BlockListRules, "blocklist_rules", func() int64 {
		return int64(s.blocklist.Len())
	})
	gauge(&m.BlockListRecency, "blocklist_recency", func() int64 {
		s.mBlocklist.RLock()
		defer s.mBlocklist.RUnlock()
		if s.blocklistTimestamp.IsZero() {
			return -1
		}
		return int64(time.Since(s.blocklistTimestamp) / time.Second)
	})

	gauge(&m.ReadCacheObjects, "read_cache_objects", func() int64 {
		return int64(s.pieceCache.Len())
	})
	gauge(&m.ReadCacheSize, "read_cache_size", func() int64 {
		return s.pieceCache.Size()
	})
	gauge(&m.ReadCacheUtilization, "read_cache_utilization", func() int64 {
		return int64(s.pieceCache.Utilization())
	})

	m.ReadsPerSecond = s.pieceCache.NumLoad
	gauge(&m.ReadsActive, "reads_active", func() int64 {
		return int64(s.pieceCache.LoadsActive())
	})
	gauge(&m.ReadsPending, "reads_pending", func() int64 {
		return int64(s.pieceCache.LoadsWaiting())
	})

	gauge(&m.WriteCacheObjects, "write_cache_objects", func() int64 {
		return int64(s.ram.Stats().AllocatedObjects)
	})
	gauge(&m.WriteCacheSize, "write_cache_size", func() int64 {
		return s.ram.Stats().AllocatedSize
	})
	gauge(&m.WriteCachePendingKeys, "write_cache_pending_keys", func() int64 {
		return int64(s.ram.Stats().PendingKeys)
	})

	m.WritesPerSecond = metrics.NewRegisteredMeter("writes_per_second", r)
	gauge(&m.WritesActive, "writes_active", func() int64 {
		return int64(s.semWrite.Len())
	})
	gauge(&m.WritesPending, "writes_pending", func() int64 {
		return int64(s.semWrite.Waiting())
	})

	m.SpeedDownload = metrics.NewRegisteredMeter("speed_download", r)
	m.SpeedUpload = metrics.NewRegisteredMeter("speed_upload", r)
	m.SpeedRead = s.pieceCache.NumLoadedBytes
	m.SpeedWrite = metrics.NewRegisteredMeter("speed_write", r)

	// Historical metric names, kept for backward compatibility with
	// dashboards scraping the registry.
	_ = r.Register("speed_read", m.SpeedRead)
	_ = r.Register("reads_per_seconds", m.ReadsPerSecond)
}

func (m *sessionMetrics) Close() {
	m.WritesPerSecond.Stop()
	m.SpeedDownload.Stop()
	m.SpeedUpload.Stop()
	m.SpeedWrite.Stop()
}
