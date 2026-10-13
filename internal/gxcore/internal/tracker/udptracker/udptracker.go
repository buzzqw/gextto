package udptracker

// http://bittorrent.org/beps/bep_0015.html
// http://xbtt.sourceforge.net/udp_tracker_protocol.html

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/url"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

// UDPTracker announces and scrapes over the UDP tracker protocol. The
// connection handshake and retransmissions live in Transport; this type only
// shapes requests and parses replies.
type UDPTracker struct {
	rawURL    string
	dest      string
	urlData   string
	log       logger.Logger
	transport *Transport
}

var _ tracker.Tracker = (*UDPTracker)(nil)

// New binds a tracker URL to a shared transport.
func New(rawURL string, u *url.URL, t *Transport) *UDPTracker {
	return &UDPTracker{
		rawURL:    rawURL,
		dest:      u.Host,
		urlData:   u.RequestURI(),
		log:       logger.New("tracker " + u.Host),
		transport: t,
	}
}

// URL returns the configured tracker URL.
func (t *UDPTracker) URL() string {
	return t.rawURL
}

// Announce reports our progress and collects fresh peers.
func (t *UDPTracker) Announce(ctx context.Context, req tracker.AnnounceRequest) (*tracker.AnnounceResponse, error) {
	announce := newTransportRequest(ctx, req, t.dest, t.urlData)

	reply, err := t.transport.Do(announce)
	if err != nil {
		return nil, err
	}
	response, peers, err := t.parseAnnounceResponse(reply)
	if err != nil {
		return nil, tracker.ErrDecode
	}
	t.log.Debugf("Announce response: %#v", response)

	return &tracker.AnnounceResponse{
		Interval: time.Duration(response.Interval) * time.Second,
		Leechers: response.Leechers,
		Seeders:  response.Seeders,
		Peers:    peers,
	}, nil
}

// parseAnnounceResponse decodes the fixed head and the trailing compact
// peer list of an announce reply.
func (t *UDPTracker) parseAnnounceResponse(data []byte) (*udpAnnounceResponse, []*net.TCPAddr, error) {
	var response udpAnnounceResponse
	if err := binary.Read(bytes.NewReader(data), binary.BigEndian, &response); err != nil {
		return nil, nil, err
	}
	t.log.Debugf("announceResponse: %#v", response)
	if response.Action != actionAnnounce {
		return nil, nil, errors.New("invalid action")
	}
	peers, err := tracker.DecodePeersCompact(data[binary.Size(response):])
	if err != nil {
		return nil, nil, err
	}
	return &response, peers, nil
}

// Scrape fetches swarm totals for one info hash (BEP 15, action 2).
func (t *UDPTracker) Scrape(ctx context.Context, req tracker.ScrapeRequest) (*tracker.ScrapeResponse, error) {
	scrape := newTransportScrapeRequest(ctx, req.Torrent.InfoHash, t.dest)

	reply, err := t.transport.Do(scrape)
	if err != nil {
		return nil, err
	}
	response, err := t.parseScrapeResponse(reply)
	if err != nil {
		return nil, tracker.ErrDecode
	}
	t.log.Debugf("Scrape response: %#v", response)
	return response, nil
}

// parseScrapeResponse decodes the head plus the single per-hash body of a
// scrape reply.
func (t *UDPTracker) parseScrapeResponse(data []byte) (*tracker.ScrapeResponse, error) {
	var header udpMessageHeader
	if err := binary.Read(bytes.NewReader(data), binary.BigEndian, &header); err != nil {
		return nil, err
	}
	if header.Action != actionScrape {
		return nil, errors.New("invalid action")
	}
	var response udpScrapeResponse
	if err := binary.Read(bytes.NewReader(data[binary.Size(header):]), binary.BigEndian, &response); err != nil {
		return nil, err
	}
	return &tracker.ScrapeResponse{
		Seeders:    response.Seeders,
		Leechers:   response.Leechers,
		Downloaded: response.Completed,
	}, nil
}
