package torrent

import (
	"context"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

// ScrapeResult is the swarm statistics reported by one tracker for a torrent
// (BEP 48). Error is set instead of the counts when the tracker does not
// support scrape or the request fails.
type ScrapeResult struct {
	URL        string
	Seeders    int
	Leechers   int
	Downloaded int
	Error      string
}

// Scrape queries every tracker of the torrent for its swarm statistics without
// announcing. The trackers are scraped concurrently; a failure on one tracker
// is reported in its ScrapeResult and does not stop the others.
func (t *Torrent) Scrape(ctx context.Context) []ScrapeResult {
	trackers := query(t.torrent, func() []tracker.Tracker {
		out := make([]tracker.Tracker, len(t.torrent.trackers))
		copy(out, t.torrent.trackers)
		return out
	})
	if len(trackers) == 0 {
		return nil
	}
	fields := t.torrent.announcerFields()

	results := make([]ScrapeResult, len(trackers))
	done := make(chan struct{}, len(trackers))
	for i, tr := range trackers {
		go func(i int, tr tracker.Tracker) {
			res := ScrapeResult{URL: tr.URL()}
			resp, err := tr.Scrape(ctx, tracker.ScrapeRequest{Torrent: fields})
			if err != nil {
				res.Error = err.Error()
			} else {
				res.Seeders = int(resp.Seeders)
				res.Leechers = int(resp.Leechers)
				res.Downloaded = int(resp.Downloaded)
			}
			results[i] = res
			done <- struct{}{}
		}(i, tr)
	}
	for range trackers {
		<-done
	}
	return results
}
