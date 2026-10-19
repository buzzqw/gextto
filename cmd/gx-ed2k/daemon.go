package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/ed2k/transfer/download"
)

// searcher is the part of *ed2k.Client the HTTP layer uses. It is an interface
// so the daemon tests can answer without touching the network.
type searcher interface {
	Search(ctx context.Context, query string) ([]ed2k.SearchFile, error)
	Stats() ed2k.Stats
	Nodes() []ed2k.Contact
	SelfID() ed2k.ID
	UDPPort() int
	Close() error
}

type daemonOptions struct {
	DataDir     string
	Mode        Mode
	Token       string
	Fingerprint string
	Lang        string
	Commit      string
}

type Daemon struct {
	opts      daemonOptions
	client    searcher
	transfers *download.Manager
	start     time.Time

	mu           sync.Mutex
	startupError string
}

func newDaemon(opts daemonOptions, client searcher) *Daemon {
	return &Daemon{opts: opts, client: client, start: time.Now()}
}

func (d *Daemon) setStartupError(err error) {
	if err == nil {
		return
	}
	d.mu.Lock()
	d.startupError = err.Error()
	d.mu.Unlock()
}

func (d *Daemon) startup() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.startupError
}

// apiResult is the JSON shape of one search result.
type apiResult struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	Hash      string `json:"hash"`
	Link      string `json:"link"`
	Sources   int64  `json:"sources"`
	Origin    string `json:"origin"`
}

func toAPIResults(files []ed2k.SearchFile) []apiResult {
	out := make([]apiResult, 0, len(files))
	for _, file := range files {
		link := ed2k.FileLink{Name: file.Name, Size: file.Size, Hash: file.Hash}
		out = append(out, apiResult{
			Name:      file.Name,
			SizeBytes: file.Size,
			Hash:      strings.ToLower(file.Hash),
			Link:      link.String(),
			Sources:   file.Sources,
			Origin:    file.Origin,
		})
	}
	return out
}

func (d *Daemon) healthPayload() map[string]any {
	stats := d.client.Stats()
	payload := map[string]any{
		"ok":           true,
		"version":      runtimeVersion(),
		"pid":          os.Getpid(),
		"mode":         string(d.opts.Mode),
		"fingerprint":  d.opts.Fingerprint,
		"data_dir":     d.opts.DataDir,
		"kad_id":       d.client.SelfID().Hex(),
		"udp_port":     d.client.UDPPort(),
		"download":     d.transfers != nil,
		"search":       true,
		"uptime_secs":  int64(time.Since(d.start).Seconds()),
		"nodes":        stats.Nodes,
		"reachability": stats.Reachability,
		"low_id":       stats.LowID,
		"transfers":    d.transferCount(),
		"peer_packets": stats.PeerPackets,
		"upnp":         stats.UPnP,
	}
	if startup := d.startup(); startup != "" {
		payload["startup_error"] = startup
	}
	return payload
}

func (d *Daemon) statusPayload() map[string]any {
	stats := d.client.Stats()
	return map[string]any{
		"ok":          true,
		"version":     runtimeVersion(),
		"commit":      d.opts.Commit,
		"mode":        string(d.opts.Mode),
		"fingerprint": d.opts.Fingerprint,
		"data_dir":    d.opts.DataDir,
		"kad_id":      d.client.SelfID().Hex(),
		"udp_port":    d.client.UDPPort(),
		"uptime_secs": int64(time.Since(d.start).Seconds()),
		"capabilities": map[string]bool{
			"search":   true,
			"download": d.transfers != nil,
			"upload":   false,
		},
		"stats":         stats,
		"startup_error": d.startup(),
	}
}

func (d *Daemon) transferCount() int {
	if d.transfers == nil {
		return 0
	}
	return len(d.transfers.List())
}

// nodePayload is the JSON shape of a routing-table contact.
type nodePayload struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	UDPPort int    `json:"udp_port"`
	TCPPort int    `json:"tcp_port"`
	Version int    `json:"version"`
}

func (d *Daemon) nodesPayload() []nodePayload {
	contacts := d.client.Nodes()
	out := make([]nodePayload, 0, len(contacts))
	for _, contact := range contacts {
		address := ""
		if contact.IP != nil {
			address = contact.IP.String()
		}
		out = append(out, nodePayload{
			ID: contact.ID.Hex(), Address: address,
			UDPPort: int(contact.UDPPort), TCPPort: int(contact.TCPPort), Version: int(contact.Version),
		})
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
