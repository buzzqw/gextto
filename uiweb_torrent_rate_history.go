package gextto

// uiweb_torrent_rate_history.go keeps the per-torrent rate history drawn in the
// Speed tab of a torrent's details, and the session totals shown in the head of
// the Downloads panel. Both are filled by bandwidthHistoryWorker, on the same
// tick as the Dashboard's history, so opening the tab shows the last half hour
// at once instead of starting an empty graph.

import (
	"math"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// torrentRateHistoryMax caps the torrents with a history. Only torrents that
// moved data in the window keep one, so the cap is reached only by a session
// with hundreds of busy torrents; a torrent past it simply has no graph.
const torrentRateHistoryMax = 400

// torrentRate is one point of a torrent's history.
type torrentRate struct {
	At       time.Time
	Down, Up uint64
}

// torrentRateHistory is the per-torrent ring of rates, keyed by lowercase hash.
// A torrent idle for the whole window is dropped: a seed nobody downloads from
// costs nothing.
type torrentRateHistory struct {
	mu    sync.RWMutex
	rings map[string][]torrentRate
}

func newTorrentRateHistory() *torrentRateHistory {
	return &torrentRateHistory{rings: map[string][]torrentRate{}}
}

// record adds one sample per torrent of the session and forgets the torrents
// that left it or stayed idle for the whole window.
func (h *torrentRateHistory) record(at time.Time, views []models.TorrentView) {
	if h == nil {
		return
	}
	cutoff := at.Add(-rateHistoryWindow)
	present := make(map[string]struct{}, len(views))
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, view := range views {
		key := strings.ToLower(view.Hash)
		if key == "" {
			continue
		}
		present[key] = struct{}{}
		down, up := v2RateOrZero(view.DownloadRate), v2RateOrZero(view.UploadRate)
		ring, known := h.rings[key]
		if !known && down == 0 && up == 0 {
			continue
		}
		if !known && len(h.rings) >= torrentRateHistoryMax {
			continue
		}
		ring = append(ring, torrentRate{At: at, Down: down, Up: up})
		drop := 0
		for drop < len(ring) && ring[drop].At.Before(cutoff) {
			drop++
		}
		if extra := len(ring) - drop - rateHistoryLen; extra > 0 {
			drop += extra
		}
		if drop > 0 {
			ring = append(ring[:0], ring[drop:]...)
		}
		busy := false
		for _, sample := range ring {
			if sample.Down != 0 || sample.Up != 0 {
				busy = true
				break
			}
		}
		if !busy {
			delete(h.rings, key)
			continue
		}
		h.rings[key] = ring
	}
	for key := range h.rings {
		if _, ok := present[key]; !ok {
			delete(h.rings, key)
		}
	}
}

// samples returns a copy of one torrent's history, oldest first.
func (h *torrentRateHistory) samples(hash string) []torrentRate {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	ring := h.rings[strings.ToLower(hash)]
	out := make([]torrentRate, len(ring))
	copy(out, ring)
	return out
}

// torrentSamples is one torrent's history, nil-safe on a hand-built state.
func (h *rateHistory) torrentSamples(hash string) []torrentRate {
	if h == nil {
		return nil
	}
	return h.torrents.samples(hash)
}

// sessionTotaler is implemented by engines that count the bytes moved since
// they started.
type sessionTotaler interface {
	SessionTotals() (down, up int64, err error)
}

// sessionTotals is the last reading of the engine's session totals.
type sessionTotals struct {
	mu       sync.RWMutex
	down, up int64
	known    bool
}

func (t *sessionTotals) set(down, up int64, known bool) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.down, t.up, t.known = down, up, known
	t.mu.Unlock()
}

func (t *sessionTotals) get() (int64, int64, bool) {
	if t == nil {
		return 0, 0, false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.down, t.up, t.known
}

// sampleSessionTotals reads the totals from the engine, when it counts them.
func sampleSessionTotals(s *AppState) {
	if s == nil || s.rate_history == nil {
		return
	}
	totaler, ok := s.activeEngine().(sessionTotaler)
	if !ok {
		s.rate_history.totals.set(0, 0, false)
		return
	}
	down, up, err := totaler.SessionTotals()
	s.rate_history.totals.set(down, up, err == nil)
}

// v2SessionTotalsView is the "since the engine started" line of the Downloads
// panel head.
type v2SessionTotalsView struct {
	Known    bool
	Down, Up string
}

func v2SessionTotalsFrom(s *AppState) v2SessionTotalsView {
	if s == nil || s.rate_history == nil {
		return v2SessionTotalsView{}
	}
	down, up, known := s.rate_history.totals.get()
	if !known {
		return v2SessionTotalsView{}
	}
	return v2SessionTotalsView{Known: true, Down: logging.HumanBytesI64(down), Up: logging.HumanBytesI64(up)}
}

// v2TorrentSpeedView is the Speed tab: the torrent's download and upload over
// the last half hour, drawn like the Dashboard chart.
type v2TorrentSpeedView struct {
	HasData    bool
	Idle       bool
	Width      int
	Height     int
	DownPoints string
	DownArea   string
	UpPoints   string
	PeakDown   string
	PeakUp     string
	AvgDown    string
	AvgUp      string
	ScaleLabel string
}

func v2TorrentSpeedViewFrom(samples []torrentRate, now time.Time) v2TorrentSpeedView {
	view := v2TorrentSpeedView{Width: trafficChartWidth, Height: trafficChartHeight}
	start := now.Add(-rateHistoryWindow)
	for len(samples) > 0 && samples[0].At.Before(start) {
		samples = samples[1:]
	}
	if len(samples) < 2 {
		view.Idle = len(samples) == 0
		return view
	}
	view.HasData = true
	var peakDown, peakUp, sumDown, sumUp uint64
	for _, sample := range samples {
		if sample.Down > peakDown {
			peakDown = sample.Down
		}
		if sample.Up > peakUp {
			peakUp = sample.Up
		}
		sumDown += sample.Down
		sumUp += sample.Up
	}
	top := uint64(1024)
	if peakDown > top {
		top = peakDown
	}
	if peakUp > top {
		top = peakUp
	}
	scale := float64(top) * 1.15
	height := float64(trafficChartHeight)
	xOf := func(at time.Time) float64 {
		frac := math.Min(math.Max(at.Sub(start).Seconds()/rateHistoryWindow.Seconds(), 0), 1)
		return frac * float64(trafficChartWidth)
	}
	yOf := func(value uint64) float64 { return height - float64(value)/scale*height }
	var down, up strings.Builder
	for index, sample := range samples {
		if index > 0 {
			down.WriteByte(' ')
			up.WriteByte(' ')
		}
		x := xOf(sample.At)
		down.WriteString(svgPoint(x, yOf(sample.Down)))
		up.WriteString(svgPoint(x, yOf(sample.Up)))
	}
	view.DownPoints, view.UpPoints = down.String(), up.String()
	view.DownArea = svgNumber(xOf(samples[0].At)) + "," + svgNumber(height) + " " + view.DownPoints + " " + svgNumber(xOf(samples[len(samples)-1].At)) + "," + svgNumber(height)
	count := uint64(len(samples))
	view.PeakDown = logging.HumanRate(saturatingInt64(peakDown))
	view.PeakUp = logging.HumanRate(saturatingInt64(peakUp))
	view.AvgDown = logging.HumanRate(saturatingInt64(sumDown / count))
	view.AvgUp = logging.HumanRate(saturatingInt64(sumUp / count))
	view.ScaleLabel = logging.HumanRate(saturatingInt64(top))
	return view
}
