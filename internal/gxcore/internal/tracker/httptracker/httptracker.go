package httptracker

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
	"github.com/zeebo/bencode"
)

// HTTPTracker announces and scrapes over HTTP (BEP 3/23/48).
type HTTPTracker struct {
	rawURL            string
	log               logger.Logger
	http              *http.Client
	transport         *http.Transport
	trackerID         string
	userAgent         string
	maxResponseLength int64
}

var _ tracker.Tracker = (*HTTPTracker)(nil)

// New binds a tracker URL to the shared transport.
func New(rawURL string, u *url.URL, timeout time.Duration, t *http.Transport, userAgent string, maxResponseLength int64) *HTTPTracker {
	return &HTTPTracker{
		rawURL:            rawURL,
		log:               logger.New("tracker " + u.Host),
		transport:         t,
		userAgent:         userAgent,
		maxResponseLength: maxResponseLength,
		http: &http.Client{
			Timeout:   timeout,
			Transport: t,
		},
	}
}

// URL returns the configured tracker URL.
func (t *HTTPTracker) URL() string {
	return t.rawURL
}

// Announce reports progress and collects peers. The query spells info_hash
// and peer_id first: some private trackers reject any other order, which is
// why url.Values is not used here.
func (t *HTTPTracker) Announce(ctx context.Context, req tracker.AnnounceRequest) (*tracker.AnnounceResponse, error) {
	query := t.announceQuery(req)
	t.log.Debugf("making request to: %q", query)

	code, header, body, err := t.fetch(ctx, query)
	if uerr, ok := err.(*url.Error); ok && uerr.Err == context.Canceled {
		return nil, context.Canceled
	}
	if err != nil {
		return nil, err
	}
	t.log.Debugf("read %d bytes from body", len(body))

	var response announceResponse
	if err := bencode.DecodeBytes(body, &response); err != nil {
		if code != 200 {
			return nil, &StatusError{Code: code, Header: header, Body: string(body)}
		}
		return nil, tracker.ErrDecode
	}
	if response.FailureReason != "" {
		return nil, tracker.NewError(response.FailureReason, response.RetryIn)
	}
	if response.TrackerID != "" {
		t.trackerID = response.TrackerID
	}
	peers, err := decodeAnnouncePeers(response)
	if err != nil {
		return nil, err
	}
	t.log.Debugf("got %d peers", len(peers))
	peers = filterExternalIP(peers, response.ExternalIP)

	return &tracker.AnnounceResponse{
		Interval:       time.Duration(response.Interval) * time.Second,
		MinInterval:    time.Duration(response.MinInterval) * time.Second,
		Leechers:       response.Incomplete,
		Seeders:        response.Complete,
		Peers:          peers,
		WarningMessage: response.WarningMessage,
	}, nil
}

// announceQuery renders the announce URL with the tracker's required
// parameter order.
func (t *HTTPTracker) announceQuery(req tracker.AnnounceRequest) string {
	var sb strings.Builder
	sb.WriteString(t.rawURL)
	if strings.ContainsRune(t.rawURL, '?') {
		sb.WriteString("&info_hash=")
	} else {
		sb.WriteString("?info_hash=")
	}
	sb.WriteString(percentEscape(req.Torrent.InfoHash))
	sb.WriteString("&peer_id=")
	sb.WriteString(percentEscape(req.Torrent.PeerID))
	sb.WriteString("&port=")
	sb.WriteString(strconv.Itoa(req.Torrent.Port))
	sb.WriteString("&uploaded=")
	sb.WriteString(strconv.FormatInt(req.Torrent.BytesUploaded, 10))
	sb.WriteString("&downloaded=")
	sb.WriteString(strconv.FormatInt(req.Torrent.BytesDownloaded, 10))
	sb.WriteString("&left=")
	sb.WriteString(strconv.FormatInt(req.Torrent.BytesLeft, 10))
	sb.WriteString("&compact=1")
	sb.WriteString("&no_peer_id=1")
	sb.WriteString("&numwant=")
	sb.WriteString(strconv.Itoa(req.NumWant))
	if req.Event != tracker.EventNone {
		sb.WriteString("&event=")
		sb.WriteString(req.Event.String())
	}
	if t.trackerID != "" {
		sb.WriteString("&trackerid=")
		sb.WriteString(t.trackerID)
	}
	sb.WriteString("&key=")
	sb.WriteString(hex.EncodeToString(req.Torrent.PeerID[16:20]))
	return sb.String()
}

// decodeAnnouncePeers parses the peers leg in all its shapes: compact
// binary, dictionary list, plus the BEP 7 IPv6 tail.
func decodeAnnouncePeers(response announceResponse) ([]*net.TCPAddr, error) {
	var peers []*net.TCPAddr
	if len(response.Peers) > 0 {
		var err error
		if response.Peers[0] == 'l' {
			peers, err = parsePeersDictionary(response.Peers)
		} else {
			var b []byte
			if err = bencode.DecodeBytes(response.Peers, &b); err != nil {
				return nil, tracker.ErrDecode
			}
			peers, err = tracker.DecodePeersCompact(b)
		}
		if err != nil {
			return nil, err
		}
	}
	if len(response.Peers6) > 0 {
		var b6 []byte
		if err := bencode.DecodeBytes(response.Peers6, &b6); err != nil {
			return nil, tracker.ErrDecode
		}
		p6, err := tracker.DecodePeersCompact6(b6)
		if err != nil {
			return nil, err
		}
		peers = append(peers, p6...)
	}
	return peers, nil
}

// fetch GETs rawURL and returns status, headers and the body capped at
// maxResponseLength.
func (t *HTTPTracker) fetch(ctx context.Context, rawURL string) (int, http.Header, []byte, error) {
	httpReq, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, nil, nil, err
	}
	httpReq = httpReq.WithContext(ctx)
	httpReq.Header.Set("User-Agent", t.userAgent)

	resp, err := t.http.Do(httpReq)
	if err != nil {
		return 0, nil, nil, err
	}
	t.log.Debugf("tracker responded %d with %d bytes body", resp.StatusCode, resp.ContentLength)
	defer resp.Body.Close()
	if resp.ContentLength > t.maxResponseLength {
		return 0, resp.Header, nil, fmt.Errorf("tracker response too large: %d", resp.ContentLength)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, t.maxResponseLength))
	return resp.StatusCode, resp.Header, data, err
}

// Scrape fetches swarm totals (BEP 48): the announce URL with its last path
// segment switched to "scrape", passkeys in the query preserved.
func (t *HTTPTracker) Scrape(ctx context.Context, req tracker.ScrapeRequest) (*tracker.ScrapeResponse, error) {
	su := scrapeURL(t.rawURL)
	var sb strings.Builder
	sb.WriteString(su)
	if strings.ContainsRune(su, '?') {
		sb.WriteString("&info_hash=")
	} else {
		sb.WriteString("?info_hash=")
	}
	sb.WriteString(percentEscape(req.Torrent.InfoHash))
	t.log.Debugf("making scrape request to: %q", sb.String())

	code, header, body, err := t.fetch(ctx, sb.String())
	if uerr, ok := err.(*url.Error); ok && uerr.Err == context.Canceled {
		return nil, context.Canceled
	}
	if err != nil {
		return nil, err
	}

	var response scrapeResponse
	if err := bencode.DecodeBytes(body, &response); err != nil {
		if code != 200 {
			return nil, &StatusError{Code: code, Header: header, Body: string(body)}
		}
		return nil, tracker.ErrDecode
	}
	if response.FailureReason != "" {
		return nil, tracker.NewError(response.FailureReason, response.RetryIn)
	}
	file, ok := response.Files[string(req.Torrent.InfoHash[:])]
	if !ok {
		// Some trackers answer a single entry under an unreusable binary
		// key; accept it when it is the only one.
		if len(response.Files) == 1 {
			for _, f := range response.Files {
				file, ok = f, true
			}
		}
	}
	if !ok {
		return nil, tracker.ErrDecode
	}
	return &tracker.ScrapeResponse{
		Seeders:    file.Complete,
		Leechers:   file.Incomplete,
		Downloaded: file.Downloaded,
	}, nil
}

// scrapeURL rewrites an announce URL into its scrape sibling.
func scrapeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	switch {
	case strings.HasSuffix(u.Path, "/announce"):
		u.Path = strings.TrimSuffix(u.Path, "/announce") + "/scrape"
	case u.Path == "" || u.Path == "/":
		u.Path = "/scrape"
	default:
		u.Path = strings.TrimSuffix(u.Path, "/") + "/scrape"
	}
	return u.String()
}

// filterExternalIP drops peers matching our own tracker-reported external
// IP, in place and order-preserving, so we never dial ourselves.
func filterExternalIP(peers []*net.TCPAddr, externalIP []byte) []*net.TCPAddr {
	if len(externalIP) == 0 {
		return peers
	}
	kept := peers[:0]
	for _, p := range peers {
		if !bytes.Equal(p.IP[:], externalIP) {
			kept = append(kept, p)
		}
	}
	return kept
}

// percentEscape escapes every byte as %XX: some trackers reject
// url.QueryEscape output, which leaves "safe" characters bare.
func percentEscape(b [20]byte) string {
	var sb strings.Builder
	sb.Grow(60)
	s := hex.EncodeToString(b[:])
	for i := range 20 {
		sb.WriteRune('%')
		sb.WriteByte(s[i*2])
		sb.WriteByte(s[i*2+1])
	}
	return sb.String()
}

// parsePeersDictionary decodes the non-compact peer list form.
func parsePeersDictionary(b bencode.RawMessage) ([]*net.TCPAddr, error) {
	var peers []struct {
		IP   string `bencode:"ip"`
		Port uint16 `bencode:"port"`
	}
	if err := bencode.DecodeBytes(b, &peers); err != nil {
		return nil, tracker.ErrDecode
	}
	addrs := make([]*net.TCPAddr, len(peers))
	for i, p := range peers {
		addrs[i] = &net.TCPAddr{IP: net.ParseIP(p.IP), Port: int(p.Port)}
	}
	return addrs, nil
}
