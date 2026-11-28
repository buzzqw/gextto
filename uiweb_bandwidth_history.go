package gextto

// uiweb_bandwidth_history.go keeps a short in-memory history of the aggregate
// transfer rates and turns it into the small bandwidth chart on the Dashboard.
//
// A dedicated background worker samples the summed rates on a fixed interval
// into a bounded ring buffer, so the history is the same whether one tab is
// open or none: the graph shows what actually happened in the last half hour,
// not just since the page was opened. The total is the whole traffic — the
// torrent engine, aMule and the direct HTTP downloads (the comics module).
// The chart itself is rendered as an SVG on the server (no charting library,
// theme-aware through CSS variables) and refreshed by the existing chrome SSE
// stream.

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
	// trafficMinScale is the lowest top the charts scale to. Below it, the
	// few KB/s of eD2k and DHT chatter would fill the chart as tall spikes:
	// with the floor they stay a thin line at the bottom, while the figures
	// in the legend still give the exact rates.
	trafficMinScale = 256 << 10
)

// rateSample is one point of the history: the summed engine rates at a
// moment, and aMule's and the HTTP downloads' rates apart (zero while they are
// off). HTTP downloads only receive data, so they have no upload part.
type rateSample struct {
	At       time.Time
	Down     uint64
	Up       uint64
	Ed2kDown uint64
	Ed2kUp   uint64
	HTTPDown uint64
}

// rateHistory is a fixed-size ring of rate samples, oldest first.
type rateHistory struct {
	mu       sync.RWMutex
	samples  []rateSample
	capacity int
	// torrents is the per-torrent history of the Speed tab, totals the
	// engine's session totals; both are sampled on the same tick.
	torrents *torrentRateHistory
	totals   *sessionTotals
}

func newRateHistory(capacity int) *rateHistory {
	if capacity <= 0 {
		capacity = 1
	}
	return &rateHistory{samples: make([]rateSample, 0, capacity), capacity: capacity,
		torrents: newTorrentRateHistory(), totals: &sessionTotals{}}
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
// active session, with sub-kilobyte chatter floored to zero for the session
// panel. aMule's rates are separate (v2Ed2kRates): the chart adds them to
// reach the total.
func v2CurrentRates(s *AppState) (uint64, uint64) {
	var down, up uint64
	for _, view := range SessionSnapshot(s.activeEngine()) {
		down += view.DownloadRate
		up += view.UploadRate
	}
	return v2RateOrZero(down), v2RateOrZero(up)
}

// v2Ed2kRates returns aMule's download and upload rates as the top bar shows
// them, from the cached status (it never waits on aMule), floored like the
// torrent rates. The figures come from the session counters averaged over the
// last half minute: aMule's own live speed is a one-second window, and a
// client that transfers in bursts reads hundreds of KB/s for an instant and
// zero in between, which made the badge blink between that and no traffic at
// all.
func v2Ed2kRates(s *AppState) (uint64, uint64) {
	if s == nil || !ed2kEnabled(latestConfig(s)) {
		return 0, 0
	}
	if _, err := ed2kStatus(latestConfig(s)); err != nil {
		return 0, 0
	}
	ed2kState.mu.Lock()
	down, up := ed2kState.rates.down, ed2kState.rates.up
	ed2kState.mu.Unlock()
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

// v2SampleDown and v2SampleUp are the total rates of one history point: the
// torrent rate Gextto sampled plus aMule's and the HTTP downloads'. Each part
// is floored on its own, so the protocol chatter of one side never lifts the
// total curve.
func v2SampleDown(sample rateSample) uint64 {
	return v2RateOrZero(sample.Down) + v2RateOrZero(sample.Ed2kDown) + v2RateOrZero(sample.HTTPDown)
}

func v2SampleUp(sample rateSample) uint64 {
	return v2RateOrZero(sample.Up) + v2RateOrZero(sample.Ed2kUp)
}

// v2LastHTTPDown is the HTTP download rate of the most recent sample. The
// downloads module reports the bytes moved, not a speed, so the worker
// measures the growth between two samples; between two ticks the last
// measurement is the freshest figure available.
func v2LastHTTPDown(samples []rateSample) uint64 {
	if len(samples) == 0 {
		return 0
	}
	return v2RateOrZero(samples[len(samples)-1].HTTPDown)
}

// v2TrafficView is the data the bandwidth chart template needs. Points and
// areas are precomputed as SVG coordinate strings; the numbers are already
// formatted so the template stays free of arithmetic.
type v2TrafficView struct {
	HasData bool
	// Idle: every sample of the window is zero. The page shows one line
	// instead of a tall empty chart and zero peaks.
	Idle   bool
	Width  int
	Height int
	// DownLine/UpLine are smooth SVG paths; the areas close them on the
	// baseline for the shaded fill under each curve.
	DownLine    string
	DownArea    string
	UpLine      string
	UpArea      string
	HasLimit    bool
	Auto        bool
	LimitY      string
	LimitLabel  string
	CurrentDown string
	CurrentUp   string
	PeakDown    string
	PeakUp      string
	ScaleLabel  string
	// The two solid curves are the whole traffic: torrents and aMule. The
	// per-source figures below feed the breakdown.
	TorrentDown string
	TorrentUp   string
	HasEd2k     bool
	// eD2k (aMule) keeps its own dashed lines and figures under the total.
	Ed2kDownLine    string
	Ed2kUpLine      string
	CurrentEd2kDown string
	CurrentEd2kUp   string
	// The direct HTTP downloads (the comics module) have no lines of their
	// own: they become visible in the total and in the breakdown while one is
	// transferring.
	HasHTTP         bool
	CurrentHTTPDown string
}

// v2TrafficViewFrom builds the chart from the rate history and the effective
// bandwidth policy. It degrades gracefully: with fewer than two samples it
// shows the current rates and a short "collecting" note instead of a line.
func v2TrafficViewFrom(s *AppState) v2TrafficView {
	view := v2TrafficView{
		Width:  trafficChartWidth,
		Height: trafficChartHeight,
	}
	// The headline figures and the two solid curves are the whole traffic:
	// torrents, aMule and the direct HTTP downloads. The per-source numbers
	// stay beside them, so each share is still readable.
	down, up := v2CurrentRates(s)
	ed2kDown, ed2kUp := v2Ed2kRates(s)
	view.HasEd2k = s != nil && ed2kEnabled(latestConfig(s))

	var samples []rateSample
	if s != nil && s.rate_history != nil {
		samples = s.rate_history.snapshot()
	}
	// HTTP downloads report the bytes moved, not a speed: the worker measures
	// the growth between two samples, so the last one is the freshest figure
	// available for the legend.
	httpDown := v2LastHTTPDown(samples)
	view.HasHTTP = httpDown > 0
	view.TorrentDown = logging.HumanRate(saturatingInt64(down))
	view.TorrentUp = logging.HumanRate(saturatingInt64(up))
	view.CurrentDown = logging.HumanRate(saturatingInt64(down + ed2kDown + httpDown))
	view.CurrentUp = logging.HumanRate(saturatingInt64(up + ed2kUp))
	if view.HasEd2k {
		view.CurrentEd2kDown = logging.HumanRate(saturatingInt64(ed2kDown))
		view.CurrentEd2kUp = logging.HumanRate(saturatingInt64(ed2kUp))
	}
	if view.HasHTTP {
		view.CurrentHTTPDown = logging.HumanRate(saturatingInt64(httpDown))
	}

	// The limit line mirrors the same precedence the daemon applies to the
	// engine: Auto/schedule-Auto means no cap, so no line. The badge still has
	// to say that Auto is what is deciding the speed, not that nothing is set.
	cfg := latestConfig(s)
	limitKib, _ := currentSpeedLimits(cfg)
	limitBytes := limitKib * 1024
	if _, _, source := speedPolicy(cfg); speedSourceIsAuto(source) {
		view.Auto = true
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
		if down := v2SampleDown(sample); down > peakDown {
			peakDown = down
		}
		if up := v2SampleUp(sample); up > peakUp {
			peakUp = up
		}
	}

	view.Idle = peakDown == 0 && peakUp == 0

	// The vertical scale covers both curves and the limit line, with a margin
	// so a peak at the top of the window does not sit on the frame.
	top := peakDown
	if peakUp > top {
		top = peakUp
	}
	if limitBytes > 0 && uint64(limitBytes) > top {
		top = uint64(limitBytes)
	}
	if top < trafficMinScale {
		top = trafficMinScale
	}
	scale := float64(top) * 1.15

	height := float64(trafficChartHeight)
	xOf := trafficXOf(start)
	yOf := func(value uint64) float64 { return height - (float64(value)/scale)*height }

	ats := make([]time.Time, len(samples))
	downs := make([]uint64, len(samples))
	ups := make([]uint64, len(samples))
	ed2kDowns := make([]uint64, len(samples))
	ed2kUps := make([]uint64, len(samples))
	for index, sample := range samples {
		ats[index] = sample.At
		downs[index] = v2SampleDown(sample)
		ups[index] = v2SampleUp(sample)
		ed2kDowns[index] = v2RateOrZero(sample.Ed2kDown)
		ed2kUps[index] = v2RateOrZero(sample.Ed2kUp)
	}
	downCurve := trafficCurve(ats, downs, xOf, yOf)
	upCurve := trafficCurve(ats, ups, xOf, yOf)
	view.DownLine, view.DownArea = svgCurve(downCurve), svgCurveArea(downCurve, height)
	view.UpLine, view.UpArea = svgCurve(upCurve), svgCurveArea(upCurve, height)
	if view.HasEd2k {
		view.Ed2kDownLine = svgCurve(trafficCurve(ats, ed2kDowns, xOf, yOf))
		view.Ed2kUpLine = svgCurve(trafficCurve(ats, ed2kUps, xOf, yOf))
	}

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

// svgPoint formats a coordinate pair.
func svgPoint(x, y float64) string {
	return svgNumber(x) + "," + svgNumber(y)
}

// svgXY is one point of a chart curve, in SVG coordinates.
type svgXY struct{ X, Y float64 }

// trafficXOf maps a time inside the window to the chart's X axis.
func trafficXOf(start time.Time) func(time.Time) float64 {
	return func(at time.Time) float64 {
		frac := math.Min(math.Max(at.Sub(start).Seconds()/rateHistoryWindow.Seconds(), 0), 1)
		return frac * float64(trafficChartWidth)
	}
}

// trafficCurve turns a rate series into the points of a calm curve: two
// samples are averaged into one point (20 s apart, half the path to draw),
// then each point is averaged with its neighbours, so a single burst of a few
// seconds becomes a soft bump instead of a spike.
func trafficCurve(ats []time.Time, values []uint64, xOf func(time.Time) float64, yOf func(uint64) float64) []svgXY {
	type bucket struct {
		at    float64
		value float64
	}
	buckets := make([]bucket, 0, (len(values)+1)/2)
	for index := 0; index < len(values); index += 2 {
		end := index + 2
		if end > len(values) {
			end = len(values)
		}
		var x, sum float64
		for j := index; j < end; j++ {
			x += xOf(ats[j])
			sum += float64(values[j])
		}
		count := float64(end - index)
		buckets = append(buckets, bucket{at: x / count, value: sum / count})
	}
	points := make([]svgXY, len(buckets))
	for index := range buckets {
		var sum, count float64
		for j := index - 1; j <= index+1; j++ {
			if j >= 0 && j < len(buckets) {
				sum += buckets[j].value
				count++
			}
		}
		points[index] = svgXY{X: buckets[index].at, Y: yOf(uint64(sum / count))}
	}
	return points
}

// svgCurve draws a smooth path through the points (Catmull-Rom turned into
// cubic Béziers). Each control point is kept within the height of its own
// segment, so the curve never dips under the baseline or rises above a peak
// that was not measured.
func svgCurve(points []svgXY) string {
	if len(points) == 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(points) * 32)
	b.WriteString("M")
	b.WriteString(svgPoint(points[0].X, points[0].Y))
	clamp := func(value, a, c float64) float64 {
		low, high := math.Min(a, c), math.Max(a, c)
		return math.Min(math.Max(value, low), high)
	}
	for index := 0; index+1 < len(points); index++ {
		p0 := points[max(index-1, 0)]
		p1, p2 := points[index], points[index+1]
		p3 := points[min(index+2, len(points)-1)]
		c1x := clamp(p1.X+(p2.X-p0.X)/6, p1.X, p2.X)
		c1y := clamp(p1.Y+(p2.Y-p0.Y)/6, p1.Y, p2.Y)
		c2x := clamp(p2.X-(p3.X-p1.X)/6, p1.X, p2.X)
		c2y := clamp(p2.Y-(p3.Y-p1.Y)/6, p1.Y, p2.Y)
		b.WriteString(" C")
		b.WriteString(svgPoint(c1x, c1y))
		b.WriteByte(' ')
		b.WriteString(svgPoint(c2x, c2y))
		b.WriteByte(' ')
		b.WriteString(svgPoint(p2.X, p2.Y))
	}
	return b.String()
}

// svgCurveArea closes the curve on the baseline for the shaded fill. It starts
// under the first point: the time before it has no data, and closing it at the
// left edge drew a ramp from zero.
func svgCurveArea(points []svgXY, baseline float64) string {
	if len(points) == 0 {
		return ""
	}
	last := points[len(points)-1]
	return svgCurve(points) + " L" + svgPoint(last.X, baseline) + " L" + svgPoint(points[0].X, baseline) + " Z"
}

// svgNumber formats a coordinate with one decimal, trimming the trailing ".0".
func svgNumber(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "0"
	}
	return strconv.FormatFloat(math.Round(value*10)/10, 'f', -1, 64)
}

// httpDownloadsRate measures how fast the direct HTTP downloads (the comics
// module) are moving between two samples. The module reports the bytes
// received, not the speed, and the average since the start of the transfer
// would flatten every change of speed and keep the first burst in the chart:
// the rate comes from the growth of their byte counters between two ticks.
// The map holds the previous counters and is refreshed in place, so a
// download that starts or ends is never read as a jump.
func httpDownloadsRate(previous map[string]uint64, since, now time.Time) uint64 {
	if previous == nil {
		return 0
	}
	elapsed := now.Sub(since).Seconds()
	if elapsed <= 0 {
		elapsed = rateSampleInterval.Seconds()
	}
	current := make(map[string]uint64, len(previous))
	var down uint64
	for _, download := range HTTPDownloads() {
		if download.Status != "downloading" {
			continue
		}
		current[download.ID] = download.DownloadedBytes
		last, seen := previous[download.ID]
		if !seen || download.DownloadedBytes <= last {
			continue // no baseline yet, or nothing moved
		}
		down += uint64(float64(download.DownloadedBytes-last) / elapsed)
	}
	for id := range previous {
		delete(previous, id)
	}
	for id, bytes := range current {
		previous[id] = bytes
	}
	return v2RateOrZero(down)
}

// bandwidthHistoryWorker records one rate sample per interval until shutdown.
func bandwidthHistoryWorker(s *AppState) {
	if s == nil || s.rate_history == nil {
		return
	}
	ticker := time.NewTicker(rateSampleInterval)
	defer ticker.Stop()
	// The HTTP downloads' counters at the previous tick, kept here because
	// this worker is the only sampler.
	httpPrevious := map[string]uint64{}
	httpPreviousAt := time.Now()
	for {
		select {
		case <-s.bgStop:
			return
		case <-ticker.C:
			now := time.Now()
			views := SessionSnapshot(s.activeEngine())
			var down, up uint64
			for _, view := range views {
				down += view.DownloadRate
				up += view.UploadRate
			}
			ed2kDown, ed2kUp := v2Ed2kRates(s)
			httpDown := httpDownloadsRate(httpPrevious, httpPreviousAt, now)
			httpPreviousAt = now
			s.rate_history.add(rateSample{At: now, Down: v2RateOrZero(down), Up: v2RateOrZero(up), Ed2kDown: ed2kDown, Ed2kUp: ed2kUp, HTTPDown: httpDown})
			s.rate_history.torrents.record(now, views)
			sampleSessionTotals(s)
		}
	}
}
