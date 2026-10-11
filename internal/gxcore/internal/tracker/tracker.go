// Package tracker provides support for announcing torrents to HTTP and UDP trackers.
package tracker

import (
	"context"
	"errors"
	"net"
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
	RetryIn       time.Duration
}

func (e *Error) Error() string { return e.FailureReason }
