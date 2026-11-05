package tracker

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// scriptedTracker returns a fixed error for every call and counts the calls, so
// a test can prove a tracker is (or is not) contacted again.
type scriptedTracker struct {
	url       string
	err       error
	announces int
	scrapes   int
}

func (s *scriptedTracker) Announce(context.Context, AnnounceRequest) (*AnnounceResponse, error) {
	s.announces++
	if s.err != nil {
		return nil, s.err
	}
	return &AnnounceResponse{Interval: time.Minute}, nil
}

func (s *scriptedTracker) Scrape(context.Context, ScrapeRequest) (*ScrapeResponse, error) {
	s.scrapes++
	if s.err != nil {
		return nil, s.err
	}
	return &ScrapeResponse{}, nil
}

func (s *scriptedTracker) URL() string { return s.url }

// TestTierDropsNeverRetryTracker proves that a tracker answering BEP 31
// "retry in: never" is dropped for good while the rest of the tier keeps
// working, instead of pausing the whole tier.
func TestTierDropsNeverRetryTracker(t *testing.T) {
	dead := &scriptedTracker{url: "http://dead.test/announce", err: NewError("unregistered", "never")}
	alive := &scriptedTracker{url: "http://alive.test/announce"}
	// Build the tier directly to keep the order deterministic (NewTier shuffles).
	tier := &Tier{Trackers: []Tracker{dead, alive}}

	if _, err := tier.Announce(context.Background(), AnnounceRequest{}); err == nil {
		t.Fatal("first announce should return the never error")
	}
	if dead.announces != 1 {
		t.Fatalf("dead tracker announces = %d, want 1", dead.announces)
	}

	// The dead tracker must never be contacted again; the next announces go to
	// the alive one.
	for i := 0; i < 3; i++ {
		if _, err := tier.Announce(context.Background(), AnnounceRequest{}); err != nil {
			t.Fatalf("announce %d: %v", i, err)
		}
	}
	if dead.announces != 1 {
		t.Fatalf("dead tracker contacted again: announces = %d, want 1", dead.announces)
	}
	if alive.announces != 3 {
		t.Fatalf("alive tracker announces = %d, want 3", alive.announces)
	}
	if got := tier.URL(); got != alive.url {
		t.Fatalf("URL = %q, want %q", got, alive.url)
	}
}

// TestTierExhaustedWhenAllNever proves that once every tracker is dropped the
// tier reports ErrTierExhausted and stops contacting any of them.
func TestTierExhaustedWhenAllNever(t *testing.T) {
	only := &scriptedTracker{url: "http://only.test/announce", err: NewError("gone", "never")}
	tier := &Tier{Trackers: []Tracker{only}}

	if _, err := tier.Announce(context.Background(), AnnounceRequest{}); err == nil {
		t.Fatal("first announce should return the never error")
	}
	_, err := tier.Announce(context.Background(), AnnounceRequest{})
	if !errors.Is(err, ErrTierExhausted) {
		t.Fatalf("second announce err = %v, want ErrTierExhausted", err)
	}
	if only.announces != 1 {
		t.Fatalf("exhausted tracker announces = %d, want 1", only.announces)
	}
}

// TestTierScrapeDropsNeverRetryTracker mirrors the announce behavior for scrape.
func TestTierScrapeDropsNeverRetryTracker(t *testing.T) {
	dead := &scriptedTracker{url: "http://dead.test/announce", err: NewError("scrape disabled", "never")}
	alive := &scriptedTracker{url: "http://alive.test/announce"}
	tier := &Tier{Trackers: []Tracker{dead, alive}}

	if _, err := tier.Scrape(context.Background(), ScrapeRequest{}); err == nil {
		t.Fatal("first scrape should return the never error")
	}
	if _, err := tier.Scrape(context.Background(), ScrapeRequest{}); err != nil {
		t.Fatalf("second scrape: %v", err)
	}
	if dead.scrapes != 1 {
		t.Fatalf("dead tracker scrapes = %d, want 1", dead.scrapes)
	}
	if alive.scrapes != 1 {
		t.Fatalf("alive tracker scrapes = %d, want 1", alive.scrapes)
	}
}

// peerTracker returns a fixed response and counts the announces, so a test can
// prove every member of a tier was contacted.
type peerTracker struct {
	url       string
	resp      *AnnounceResponse
	err       error
	announces int
}

func (p *peerTracker) Announce(context.Context, AnnounceRequest) (*AnnounceResponse, error) {
	p.announces++
	if p.err != nil {
		return nil, p.err
	}
	return p.resp, nil
}

func (p *peerTracker) Scrape(context.Context, ScrapeRequest) (*ScrapeResponse, error) {
	return &ScrapeResponse{}, nil
}

func (p *peerTracker) URL() string { return p.url }

// TestTierAnnounceAllMergesEveryMember proves the qBittorrent default: every
// tracker of a tier is contacted, duplicate peers collapse, the shortest
// interval wins and the largest swarm count is kept, even when one member
// fails.
func TestTierAnnounceAllMergesEveryMember(t *testing.T) {
	one := &peerTracker{
		url: "http://one.test/announce",
		resp: &AnnounceResponse{
			Interval: 30 * time.Minute, Seeders: 4, Leechers: 9,
			Peers: []*net.TCPAddr{{IP: net.ParseIP("192.0.2.1"), Port: 6881}},
		},
	}
	two := &peerTracker{
		url: "http://two.test/announce",
		resp: &AnnounceResponse{
			Interval: 10 * time.Minute, MinInterval: 5 * time.Minute, Seeders: 12, Leechers: 2,
			Peers: []*net.TCPAddr{
				{IP: net.ParseIP("192.0.2.1"), Port: 6881},
				{IP: net.ParseIP("192.0.2.2"), Port: 6882},
			},
		},
	}
	broken := &peerTracker{url: "http://broken.test/announce", err: errors.New("timeout")}
	tier := &Tier{Trackers: []Tracker{one, two, broken}}
	tier.SetAnnounceAll(true)

	resp, err := tier.Announce(context.Background(), AnnounceRequest{})
	if err != nil {
		t.Fatalf("announce: %v", err)
	}
	if one.announces != 1 || two.announces != 1 || broken.announces != 1 {
		t.Fatalf("announces = %d/%d/%d, want 1/1/1", one.announces, two.announces, broken.announces)
	}
	if resp.Interval != 10*time.Minute {
		t.Fatalf("interval = %s, want the shortest 10m", resp.Interval)
	}
	if resp.MinInterval != 5*time.Minute {
		t.Fatalf("min interval = %s, want 5m", resp.MinInterval)
	}
	if resp.Seeders != 12 || resp.Leechers != 9 {
		t.Fatalf("swarm = %d/%d, want 12/9", resp.Seeders, resp.Leechers)
	}
	if len(resp.Peers) != 2 {
		t.Fatalf("peers = %d, want the duplicate collapsed to 2: %v", len(resp.Peers), resp.Peers)
	}
}

// TestTierAnnounceAllReportsWhenEveryMemberFails keeps the failure visible
// instead of returning an empty success.
func TestTierAnnounceAllReportsWhenEveryMemberFails(t *testing.T) {
	one := &peerTracker{url: "http://one.test/announce", err: errors.New("one")}
	two := &peerTracker{url: "http://two.test/announce", err: errors.New("two")}
	tier := &Tier{Trackers: []Tracker{one, two}}
	tier.SetAnnounceAll(true)

	if _, err := tier.Announce(context.Background(), AnnounceRequest{}); err == nil {
		t.Fatal("announce should report the failure when nobody answers")
	}
	if one.announces != 1 || two.announces != 1 {
		t.Fatalf("announces = %d/%d, want 1/1", one.announces, two.announces)
	}
}
