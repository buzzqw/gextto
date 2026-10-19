package gextto

// uiweb_bandwidth_history.go keeps a short in-memory history of the aggregate
// transfer rates and turns it into the small bandwidth chart on the Dashboard.
//
// A dedicated background worker samples the summed engine rates on a fixed
// interval into a bounded ring buffer, so the history is the same whether one
// tab is open or none: the graph shows what actually happened in the last half
// hour, not just since the page was opened. The chart itself is rendered as an
// SVG on the server (no charting library, theme-aware through CSS variables)
// and refreshed by the existing chrome SSE stream.

import (
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

const (
	// rateSampleInterval is how often the worker records a point. It matches
	// the live chrome cadence (the SSE ticker is 5 s), rounded to 10 s.
	rateSampleInterval = 10 * time.Second
	// rateHistoryWindow is the span the chart covers.
	rateHistoryWindow = 30 * time.Minute
	// rateHistoryLen is how many samples fit in the window.
	rateHistoryLen = int(rateHistoryWindow / rateSampleInterval) // 180
	// Chart canvas, in SVG user units. The element scales to its container;
	// strokes use non-scaling-stroke so they stay crisp at any width.
	trafficChartWidth  = 600
	trafficChartHeight = 160
	// rateNoiseFloor is the rate below which the dashboard treats the session
	// as idle: trackers, DHT and peer keepalives keep a few hundred bytes per
	// second flowing even when nothing is transferring, and drawing that as a
	// curve is just noise. Anything under 1 KiB/s is shown as 0.
	rateNoiseFloor = 1024
)

// rateSample is one point of the history: the summed engine rates at a moment.
type rateSample struct {
	At   time.Time
	Down uint64
	Up   uint64
}

// rateHistory is a fixed-size ring of rate samples, oldest first.
type rateHistory struct {
	mu       sync.RWMutex
	samples  []rateSample
	capacity int
}

func newRateHistory(capacity int) *rateHistory {
	if capacity <= 0 {
		capacity = 1
	}
	return &rateHistory{samples: make([]rateSample, 0, capacity), capacity: capacity}
}

// add appends a sample, dropping the oldest once full. The backing array is
// reused in place, so the memory footprint stays constant.
func (h *rateHistory) add(sample rateSample) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.samples = append(h.samples, sample)
	if overflow := len(h.samples) - h.capacity; overflow > 0 {
		copy(h.samples, h.samples[overflow:])
		h.samples = h.samples[:h.capacity]
	}
}

// snapshot returns a copy of the samples, oldest first.
func (h *rateHistory) snapshot() []rateSample {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]rateSample, len(h.samples))
	copy(out, h.samples)
	return out
}

// len reports how many samples are stored.
func (h *rateHistory) len() int {
	if h == nil {
		return 0
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.samples)
}

// v2CurrentRates sums the download and upload rates of every torrent in the
// active session, like the top bar does, with sub-kilobyte chatter floored to
// zero for the session panel.
func v2CurrentRates(s *AppState) (uint64, uint64) {
	var down, up uint64
	for _, view := range s.activeEngine().List() {
		down += view.DownloadRate
		up += view.UploadRate
	}
	return v2RateOrZero(down), v2RateOrZero(up)
}

// v2RateOrZero treats anything under the noise floor as no traffic, so the
// session panel and its chart stay still instead of twitching at a few
// hundred bytes per second.
func v2RateOrZero(rate uint64) uint64 {
	if rate < rateNoiseFloor {
		return 0
	}
	return rate
}

// v2TrafficView is the data the bandwidth chart template needs. Points and
// areas are precomputed as SVG coordinate strings; the numbers are already
// formatted so the template stays free of arithmetic.
type v2TrafficView struct {
	HasData     bool
	Width       int
	Height      int
	DownPoints  string
	DownArea    string
	UpPoints    string
	HasLimit    bool
	LimitY      string
	LimitLabel  string
	CurrentDown string
	CurrentUp   string
	PeakDown    string
	PeakUp      string
	ScaleLabel  string
}

// v2TrafficViewFrom builds the chart from the rate history and the effective
// bandwidth policy. It degrades gracefully: with fewer than two samples it
// shows the current rates and a short "collecting" note instead of a line.
func v2TrafficViewFrom(s *AppState) v2TrafficView {
	view := v2TrafficView{
		Width:  trafficChartWidth,
		Height: trafficChartHeight,
	}
	down, up := v2CurrentRates(s)
	view.CurrentDown = logging.HumanRate(saturatingInt64(down))
	view.CurrentUp = logging.HumanRate(saturatingInt64(up))

	// The limit line mirrors the same precedence the daemon applies to the
	// engine: Auto/schedule-Auto means no cap, so no line.
	limitKib, _ := gh6_currentSpeedLimits(latestConfig(s))
	limitBytes := limitKib * 1024

	var samples []rateSample
	if s != nil && s.rate_history != nil {
		samples = s.rate_history.snapshot()
	}
	// Only the window is drawn: an older sample would pile up on the left edge.
	start := time.Now().Add(-rateHistoryWindow)
	for len(samples) > 0 && samples[0].At.Before(start) {
		samples = samples[1:]
	}
	if len(samples) < 2 {
		return view
	}
	view.HasData = true

	var peakDown, peakUp uint64
	for _, sample := range samples {
		if down := v2RateOrZero(sample.Down); down > peakDown {
			peakDown = down
		}
		if up := v2RateOrZero(sample.Up); up > peakUp {
			peakUp = up
		}
	}

	// The vertical scale covers both curves and the limit line, with a margin
	// so a peak at the top of the window does not sit on the frame.
	top := peakDown
	if peakUp > top {
		top = peakUp
	}
	if limitBytes > 0 && uint64(limitBytes) > top {
		top = uint64(limitBytes)
	}
	if top == 0 {
		top = 1024 // 1 KiB/s: a flat baseline instead of a division by zero.
	}
	scale := float64(top) * 1.15

	height := float64(trafficChartHeight)
	xOf := func(at time.Time) float64 {
		frac := at.Sub(start).Seconds() / rateHistoryWindow.Seconds()
		if frac < 0 {
			frac = 0
		} else if frac > 1 {
			frac = 1
		}
		return frac * float64(trafficChartWidth)
	}
	yOf := func(value uint64) float64 {
		if scale <= 0 {
			return height
		}
		return height - (float64(value)/scale)*height
	}

	var downPoints, upPoints strings.Builder
	firstX, lastX := xOf(samples[0].At), 0.0
	for index, sample := range samples {
		if index > 0 {
			downPoints.WriteByte(' ')
			upPoints.WriteByte(' ')
		}
		x := xOf(sample.At)
		lastX = x
		downPoints.WriteString(svgPoint(x, yOf(v2RateOrZero(sample.Down))))
		upPoints.WriteString(svgPoint(x, yOf(v2RateOrZero(sample.Up))))
	}
	view.DownPoints = downPoints.String()
	view.UpPoints = upPoints.String()
	// The area starts under the first sample: the time before it has no data,
	// and closing it at the left edge drew a ramp from zero.
	view.DownArea = svgNumber(firstX) + "," + svgNumber(height) + " " + view.DownPoints + " " + svgNumber(lastX) + "," + svgNumber(height)

	view.PeakDown = logging.HumanRate(saturatingInt64(peakDown))
	view.PeakUp = logging.HumanRate(saturatingInt64(peakUp))
	view.ScaleLabel = logging.HumanRate(saturatingInt64(top))

	if limitBytes > 0 {
		view.HasLimit = true
		view.LimitY = svgNumber(yOf(uint64(limitBytes)))
		view.LimitLabel = logging.HumanRate(limitBytes)
	}
	return view
}

// svgPoint formats a coordinate pair for a polyline.
func svgPoint(x, y float64) string {
	return svgNumber(x) + "," + svgNumber(y)
}

// svgNumber formats a coordinate with one decimal, trimming the trailing ".0".
func svgNumber(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "0"
	}
	return strconv.FormatFloat(math.Round(value*10)/10, 'f', -1, 64)
}

// bandwidthHistoryWorker records one rate sample per interval until shutdown.
func bandwidthHistoryWorker(s *AppState) {
	if s == nil || s.rate_history == nil {
		return
	}
	ticker := time.NewTicker(rateSampleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.bgStop:
			return
		case <-ticker.C:
			down, up := v2CurrentRates(s)
			s.rate_history.add(rateSample{At: time.Now(), Down: down, Up: up})
		}
	}
}
