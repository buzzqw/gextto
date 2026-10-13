package webseedsource

import (
	"github.com/buzzqw/gextto/internal/gxcore/internal/urldownloader"
	"github.com/rcrowley/go-metrics"
)

// WebseedSource is one HTTP source a torrent may fetch pieces from
// (BEP 19 / GetRight-style URL seeds), with its live download state.
type WebseedSource struct {
	URL           string
	Disabled      bool
	Downloader    *urldownloader.URLDownloader
	LastError     error
	DownloadSpeed metrics.Meter
}

// NewList builds a source per URL. Meters start nil (zero-valued) until a
// download assigns the real one.
func NewList(sources []string) []*WebseedSource {
	l := make([]*WebseedSource, len(sources))
	for i, url := range sources {
		l[i] = &WebseedSource{URL: url, DownloadSpeed: metrics.NilMeter{}}
	}
	return l
}

// Downloading reports whether a fetch from this source is in flight.
func (s *WebseedSource) Downloading() bool {
	return s.Downloader != nil
}

// Remaining counts the pieces still queued behind the one currently
// downloading (which is excluded).
func (s *WebseedSource) Remaining() uint32 {
	if s.Downloader == nil {
		return 0
	}
	return s.Downloader.End - s.Downloader.ReadCurrent() - 1
}
