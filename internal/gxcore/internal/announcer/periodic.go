package announcer

import (
	"context"
	"math"
	"net"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/backoff"
	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resolver"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker/httptracker"
)

// Status is the announcer's contact state.
type Status int

const (
	// NotContactedYet sent nothing yet.
	NotContactedYet Status = iota
	// Contacting waits for a reply.
	Contacting
	// Working heard back fine.
	Working
	// NotWorking failed the last announce.
	NotWorking
)

// PeriodicalAnnouncer announces one torrent to one tracker on the tracker's
// own schedule: started/completed events immediately, steady state at the
// advertised interval (or the minimum while peers are wanted), failures on
// exponential backoff (or the tracker's own retry-in).
type PeriodicalAnnouncer struct {
	Tracker       tracker.Tracker
	status        Status
	statsCommandC chan statsRequest
	numWant       int
	interval      time.Duration
	minInterval   time.Duration
	seeders       int
	leechers      int
	warningMsg    string
	lastError     *AnnounceError
	log           logger.Logger
	completedC    chan struct{}
	newPeers      chan []*net.TCPAddr
	backoff       backoff.BackOff
	getTorrent    func() tracker.Torrent
	lastAnnounce  time.Time
	nextAnnounce  time.Time
	HasAnnounced  bool
	responseC     chan *tracker.AnnounceResponse
	errC          chan error
	// onResult, when set, observes every announce outcome. It runs on the
	// announcer goroutine and must not block.
	onResult func(*tracker.AnnounceResponse, error)
	closeC   chan struct{}
	doneC    chan struct{}

	needMorePeers  bool
	mNeedMorePeers sync.RWMutex
	needMorePeersC chan struct{}
}

// NewPeriodicalAnnouncer prepares an announcer; run it with Run.
func NewPeriodicalAnnouncer(trk tracker.Tracker, numWant int, minInterval time.Duration, getTorrent func() tracker.Torrent, completedC chan struct{}, newPeers chan []*net.TCPAddr, l logger.Logger) *PeriodicalAnnouncer {
	return &PeriodicalAnnouncer{
		Tracker:        trk,
		status:         NotContactedYet,
		statsCommandC:  make(chan statsRequest),
		numWant:        numWant,
		minInterval:    minInterval,
		log:            l,
		completedC:     completedC,
		newPeers:       newPeers,
		getTorrent:     getTorrent,
		needMorePeersC: make(chan struct{}, 1),
		responseC:      make(chan *tracker.AnnounceResponse),
		errC:           make(chan error),
		closeC:         make(chan struct{}),
		doneC:          make(chan struct{}),
		backoff: &backoff.ExponentialBackOff{
			InitialInterval:     5 * time.Second,
			RandomizationFactor: 0.5,
			Multiplier:          2,
			MaxInterval:         30 * time.Minute,
		},
	}
}

// SetResultHook observes every announce outcome. Call before Run; the hook
// must not block.
func (a *PeriodicalAnnouncer) SetResultHook(f func(*tracker.AnnounceResponse, error)) {
	a.onResult = f
}

// Close stops the announcer and waits for the loop to exit.
func (a *PeriodicalAnnouncer) Close() {
	close(a.closeC)
	<-a.doneC
}

type statsRequest struct {
	Response chan Stats
}

// Stats queries the announcer state from its goroutine (zero after Close).
func (a *PeriodicalAnnouncer) Stats() Stats {
	var stats Stats
	req := statsRequest{Response: make(chan Stats, 1)}
	select {
	case a.statsCommandC <- req:
	case <-a.closeC:
	}
	select {
	case stats = <-req.Response:
	case <-a.closeC:
	}
	return stats
}

// NeedMorePeers switches the steady pace between minInterval (hungry) and
// the advertised interval (fed). The ping is lossy by design: only the
// latest pace matters.
func (a *PeriodicalAnnouncer) NeedMorePeers(val bool) {
	a.mNeedMorePeers.Lock()
	a.needMorePeers = val
	a.mNeedMorePeers.Unlock()
	select {
	case a.needMorePeersC <- struct{}{}:
	case <-a.doneC:
	default:
	}
}

// Run serves the announce loop. The first announce goes out as started
// (unless already complete — BEP 3 sends no completed for an initially
// complete torrent); then replies, errors, pace changes, the completed
// event and stats queries drive it until Close.
func (a *PeriodicalAnnouncer) Run() {
	defer close(a.doneC)
	a.backoff.Reset()

	timer := time.NewTimer(math.MaxInt64)
	defer timer.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	select {
	case <-a.completedC:
		a.completedC = nil
	default:
	}

	a.doAnnounce(ctx, tracker.EventStarted, a.numWant)
	for {
		select {
		case <-timer.C:
			if a.status == Contacting {
				break
			}
			a.doAnnounce(ctx, tracker.EventNone, a.numWant)
		case resp := <-a.responseC:
			a.handleResponse(timer, resp)
		case err := <-a.errC:
			a.handleError(timer, err)
		case <-a.needMorePeersC:
			if a.status == Contacting || a.status == NotWorking {
				break
			}
			a.resetTimer(timer, time.Until(a.lastAnnounce.Add(a.getNextInterval())))
		case <-a.completedC:
			if a.status == Contacting {
				cancel()
				ctx, cancel = context.WithCancel(context.Background())
			}
			a.doAnnounce(ctx, tracker.EventCompleted, 0)
			a.completedC = nil // a single completed event per run
		case req := <-a.statsCommandC:
			req.Response <- a.stats()
		case <-a.closeC:
			cancel()
			return
		}
	}
}

// resetTimer arms timer for interval from now, recording the fire time. A
// negative interval means overdue: the timer fires immediately and the
// recorded time is now. It runs only on the Run goroutine, where the timer
// is either drained (timer.C branch) or safely re-armed.
func (a *PeriodicalAnnouncer) resetTimer(timer *time.Timer, interval time.Duration) {
	a.nextAnnounce = time.Now()
	if interval >= 0 {
		a.nextAnnounce = a.nextAnnounce.Add(interval)
	}
	timer.Reset(interval)
}

// handleResponse absorbs one announce reply: status, swarm counts, result
// hook, warning, refreshed schedule (honoring the tracker's min-interval),
// and the peer handoff to the torrent.
func (a *PeriodicalAnnouncer) handleResponse(timer *time.Timer, resp *tracker.AnnounceResponse) {
	a.status = Working
	a.seeders = int(resp.Seeders)
	a.leechers = int(resp.Leechers)
	if a.onResult != nil {
		a.onResult(resp, nil)
	}
	a.warningMsg = resp.WarningMessage
	if a.warningMsg != "" {
		a.log.Debugln("announce warning:", a.warningMsg)
	}
	a.interval = resp.Interval
	if resp.MinInterval > 0 {
		a.minInterval = resp.MinInterval
	}
	a.HasAnnounced = true
	a.lastError = nil
	a.backoff.Reset()
	a.resetTimer(timer, a.getNextInterval())
	go func() {
		select {
		case a.newPeers <- resp.Peers:
		case <-a.closeC:
		}
	}()
}

// handleError absorbs one announce failure: humanized error, logging by
// severity, and reschedule on backoff (or the tracker's retry-in).
func (a *PeriodicalAnnouncer) handleError(timer *time.Timer, err error) {
	a.status = NotWorking
	if a.onResult != nil {
		a.onResult(nil, err)
	}
	a.lastError = a.newAnnounceError(err)
	if a.lastError.Unknown {
		a.log.Errorln("announce error:", a.lastError.ErrorWithType())
	} else {
		a.log.Debugln("announce error:", a.lastError.Err.Error())
	}
	a.resetTimer(timer, a.getNextIntervalFromError(a.lastError))
}

func (a *PeriodicalAnnouncer) getNextInterval() time.Duration {
	a.mNeedMorePeers.RLock()
	need := a.needMorePeers
	a.mNeedMorePeers.RUnlock()
	if need {
		return a.minInterval
	}
	return a.interval
}

func (a *PeriodicalAnnouncer) getNextIntervalFromError(err *AnnounceError) time.Duration {
	if terr, ok := err.Err.(*tracker.Error); ok && terr.RetryIn > 0 {
		return terr.RetryIn
	}
	return a.backoff.NextBackOff()
}

func (a *PeriodicalAnnouncer) doAnnounce(ctx context.Context, event tracker.Event, numWant int) {
	go a.announce(ctx, event, numWant)
	a.status = Contacting
	a.lastAnnounce = time.Now()
}

func (a *PeriodicalAnnouncer) announce(ctx context.Context, event tracker.Event, numWant int) {
	announce(ctx, a.Tracker, event, numWant, a.getTorrent(), a.responseC, a.errC)
}

// Stats about the announcer.
type Stats struct {
	Status       Status
	Error        *AnnounceError
	Warning      string
	Seeders      int
	Leechers     int
	LastAnnounce time.Time
	NextAnnounce time.Time
}

func (a *PeriodicalAnnouncer) stats() Stats {
	return Stats{
		Status:       a.status,
		Error:        a.lastError,
		Warning:      a.warningMsg,
		Seeders:      a.seeders,
		Leechers:     a.leechers,
		LastAnnounce: a.lastAnnounce,
		NextAnnounce: a.nextAnnounce,
	}
}

// AnnounceError pairs a tracker failure with its human-readable message.
// Unknown marks failures nobody classified (logged as errors, not debug).
type AnnounceError struct {
	Err     error
	Message string
	Unknown bool
}

// newAnnounceError classifies err into a user-facing message. The strings
// below are operator-visible (UI/API); keep them stable.
func (a *PeriodicalAnnouncer) newAnnounceError(err error) (e *AnnounceError) {
	e = &AnnounceError{Err: err}
	switch err {
	case resolver.ErrNotIPv4Address:
		parsed, _ := url.Parse(a.Tracker.URL())
		e.Message = "tracker has no IPv4 address: " + parsed.Hostname()
		return
	case resolver.ErrBlocked:
		e.Message = "tracker IP is blocked"
		return
	case resolver.ErrInvalidPort:
		parsed, _ := url.Parse(a.Tracker.URL())
		e.Message = "invalid port number in tracker address: " + parsed.Host
		return
	case tracker.ErrDecode:
		e.Message = "invalid response from tracker"
		return
	case tracker.ErrTierExhausted:
		e.Message = "all trackers in the tier asked to not be retried"
		return
	}
	if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
		e.Message = "timeout contacting tracker"
		return
	}
	switch err := err.(type) {
	case *net.DNSError:
		e.Message = dnsErrorMessage(err)
		if e.Message != "" {
			return
		}
	case *net.AddrError:
		if strings.HasSuffix(err.Error(), "missing port in address") {
			e.Message = "missing port in tracker address"
			return
		}
	case *url.Error:
		if msg, ok := urlErrorMessage(a.Tracker.URL(), err); ok {
			e.Message = msg
			return
		}
	case *httptracker.StatusError:
		if err.Code >= 400 {
			e.Message = "tracker returned HTTP status: " + strconv.Itoa(err.Code)
			if err.Header.Get("content-type") == "text/plain" {
				msg := err.Body
				if len(msg) > 100 {
					msg = msg[:97] + "..."
				}
				e.Message += " message: " + msg
			}
			return
		}
	case *tracker.Error:
		e.Message = "announce error: " + err.FailureReason
		return
	}
	e.Message = "unknown error in announce"
	e.Unknown = true
	return
}

// dnsErrorMessage maps resolver failures to messages, or "" when unmatched.
func dnsErrorMessage(err *net.DNSError) string {
	s := err.Error()
	switch {
	case strings.HasSuffix(s, "no such host"),
		strings.HasSuffix(s, "server misbehaving"):
		return "host not found: " + err.Name
	case strings.HasSuffix(s, "Temporary failure in name resolution"):
		return "temporary failure in name resolution: " + err.Name
	case strings.HasSuffix(s, "No address associated with hostname"):
		return "no address associated with hostname: " + err.Name
	}
	return ""
}

// urlErrorMessage maps transport failures to messages; false when unmatched.
func urlErrorMessage(trackerURL string, err *url.Error) (string, bool) {
	parsed, _ := url.Parse(trackerURL)
	s := err.Error()
	switch {
	case strings.HasSuffix(s, "connection refused"):
		return "tracker refused the connection", true
	case strings.HasSuffix(s, "no such host"):
		return "no such host: " + parsed.Hostname(), true
	case strings.HasSuffix(s, "server misbehaving"):
		return "server misbehaving: " + parsed.Hostname(), true
	case strings.HasSuffix(s, "tls: handshake failure"):
		return "TLS handshake has failed", true
	case strings.HasSuffix(s, "no route to host"):
		return "no route to host: " + parsed.Hostname(), true
	case strings.HasSuffix(s, "No address associated with hostname"):
		return "no address associated with hostname: " + parsed.Hostname(), true
	case strings.HasSuffix(s, resolver.ErrNotIPv4Address.Error()):
		return "tracker has no IPv4 address: " + parsed.Hostname(), true
	case strings.HasSuffix(s, "connection reset by peer"),
		strings.HasSuffix(s, "EOF"):
		return "tracker closed the connection", true
	case strings.HasSuffix(s, "server gave HTTP response to HTTPS client"),
		strings.Contains(s, "malformed HTTP status code"):
		return "invalid server response", true
	case strings.Contains(s, "network is unreachable"):
		return "network is unreachable: " + parsed.Hostname(), true
	}
	return "", false
}

// ErrorWithType prefixes the message with the concrete error type.
func (e *AnnounceError) ErrorWithType() string {
	return reflect.TypeOf(e.Err).String() + ": " + e.Err.Error()
}
