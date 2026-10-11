// Package tracker provides support for announcing torrents to HTTP and UDP trackers.
package tracker

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"
)

// Tracker tracks the IP address of peers of a Torrent swarm.
type Tracker interface {
	// Announce transfer to the tracker.
	// Announce should be called periodically with the interval returned in AnnounceResponse.
	// Announce should also be called on specific events.
	Announce(ctx context.Context, req AnnounceRequest) (*AnnounceResponse, error)

	// Scrape asks the tracker for swarm statistics (BEP 48) without
	// announcing. Not every tracker supports it.
	Scrape(ctx context.Context, req ScrapeRequest) (*ScrapeResponse, error)

	// URL of the tracker.
	URL() string
}

// AnnounceRequest contains the parameters that are sent in an announce request to trackers.
type AnnounceRequest struct {
	Torrent Torrent
	Event   Event
	NumWant int
}

// AnnounceResponse contains fields from a response to announce request.
type AnnounceResponse struct {
	Interval       time.Duration
	MinInterval    time.Duration
	Leechers       int32
	Seeders        int32
	WarningMessage string
	Peers          []*net.TCPAddr
}

// ScrapeRequest contains the parameters that are sent in a scrape request (BEP 48).
type ScrapeRequest struct {
	// Torrent carries the InfoHash (and the other fields trackers may need).
	Torrent Torrent
}

// ScrapeResponse contains the swarm statistics of a scrape request.
type ScrapeResponse struct {
	// Seeders is the number of peers with the entire file (complete).
	Seeders int32
	// Leechers is the number of non-seeding peers (incomplete).
	Leechers int32
	// Downloaded is the number of times the torrent has been completed.
	Downloaded int32
}

// ErrScrapeNotSupported is returned by a Tracker that cannot scrape (BEP 48 is
// optional and some trackers, mostly private ones, do not expose it).
var ErrScrapeNotSupported = errors.New("tracker does not support scrape")

// ErrDecode is returned from Tracker.Announce method when there is problem with the encoding of response.
var ErrDecode = errors.New("cannot decode response")

// Error is the string that is sent by the tracker from announce or scrape.
type Error struct {
	FailureReason string
	// RetryIn is the BEP 31 "retry in" interval, in minutes, before the next
	// announce. Zero when the tracker did not send it.
	RetryIn time.Duration
	// NeverRetry is BEP 31's "retry in: never": the tracker must not be
	// contacted again, so the tier drops it for good.
	NeverRetry bool
}

func (e *Error) Error() string { return e.FailureReason }

// SetRetryIn parses the BEP 31 "retry in" value: a number of minutes, or the
// string "never" meaning the tracker must not be retried. Anything else (an
// empty or malformed value) leaves RetryIn unset, so the announcer falls back
// to its own backoff.
func (e *Error) SetRetryIn(s string) {
	if strings.EqualFold(strings.TrimSpace(s), "never") {
		e.NeverRetry = true
		return
	}
	if minutes, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && minutes > 0 {
		e.RetryIn = time.Duration(minutes) * time.Minute
	}
}

// NewError builds the tracker.Error for a failure, parsing the BEP 31
// "retry in" value. It is the single place the field is decoded, shared by the
// HTTP and UDP trackers for both announce and scrape.
func NewError(failureReason, retryIn string) *Error {
	e := &Error{FailureReason: failureReason}
	e.SetRetryIn(retryIn)
	return e
}
