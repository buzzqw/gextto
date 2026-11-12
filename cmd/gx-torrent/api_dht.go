package main

import (
	"net/http"
	"time"

	"github.com/buzzqw/gextto/internal/dht"
)

// dhtReport is the DHT diagnostics of GET /api/v1/dht: whether it runs, the
// routing-table view and whether the DHT port receives packets from outside.
type dhtReport struct {
	Enabled  bool           `json:"enabled"`
	ReadOnly bool           `json:"read_only"`
	Port     int            `json:"port"`
	View     *dht.TableView `json:"view,omitempty"`
	// Incoming is "yes" when a remote node sent a query in the last hour,
	// "no" after an hour without one, "unknown" right after the start.
	Incoming string `json:"incoming"`
	// ClientFilterRefused counts the peers refused by the client filter.
	ClientFilterRefused int64 `json:"client_filter_refused"`
	// TrackerList is the state of the tracker list (empty URL = off).
	TrackerList trackerListReport `json:"tracker_list"`
}

// trackerListReport is the tracker list as the pages show it.
type trackerListReport struct {
	URL       string     `json:"url,omitempty"`
	Trackers  int        `json:"trackers"`
	FetchedAt *time.Time `json:"fetched_at,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// incomingWindow is how recent an incoming query must be to say yes, and
// how long after the start the answer stays unknown.
const incomingWindow = time.Hour

func (d *Daemon) dhtReport(now time.Time) dhtReport {
	d.mu.Lock()
	session := d.session
	readOnly := d.opts.Network.DHTReadOnly
	enabled := d.opts.Network.DHT && d.opts.Network.Proxy == ""
	startedAt := d.startedAt
	port := d.peerPort
	d.mu.Unlock()
	out := dhtReport{Enabled: enabled, ReadOnly: readOnly, Incoming: "unknown"}
	if session != nil {
		out.ClientFilterRefused = session.ClientFilterRefused()
		if enabled {
			out.View = session.DHTView()
			out.Port = port
		}
	}
	if out.View != nil {
		switch {
		case out.View.LastQuery != nil && now.Sub(*out.View.LastQuery) < incomingWindow:
			out.Incoming = "yes"
		case !startedAt.IsZero() && now.Sub(startedAt) >= incomingWindow:
			out.Incoming = "no"
		}
	}
	url, list, fetchedAt, lastErr := d.trackerList.snapshot()
	if configured := d.trackerListURL(); configured != "" {
		out.TrackerList.URL = configured
		if url == configured {
			out.TrackerList.Trackers = len(list)
			if !fetchedAt.IsZero() {
				at := fetchedAt
				out.TrackerList.FetchedAt = &at
			}
			out.TrackerList.Error = lastErr
		}
	}
	return out
}

func (d *Daemon) handleDHT(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.dhtReport(time.Now()))
}
