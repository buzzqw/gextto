// Command anacrolix is the anacrolix/torrent row of the client comparison.
//
// anacrolix/torrent is a library, not a client, so the harness needs a small
// front end: this program adds a .torrent to a client built from the library
// defaults, rechecks it and reports the state and the progress of every
// torrent over HTTP, which are the calls scripts/compare/run.py makes to the
// other clients. Nothing here changes engine behaviour: the limits, the
// transport and the storage are the library's.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

type daemon struct {
	client    *torrent.Client
	mu        sync.Mutex
	verifying map[string]bool
	state     string
}

type row struct {
	State    string  `json:"state"`
	Progress float64 `json:"progress"`
	Peers    int     `json:"peers,omitempty"`
	Active   int     `json:"active,omitempty"`
}

func main() {
	data := flag.String("data", "/data/dl", "folder the torrents are saved in")
	peerPort := flag.Int("port", 6881, "peer port")
	listen := flag.String("http", "0.0.0.0:8080", "address of this front end")
	state := flag.String("state", "/data/cfg/torrents", "where the added .torrent files are kept, so a restart finds them")
	noUTP := flag.Bool("no-utp", false, "accept only TCP")
	debug := flag.Bool("debug", false, "log the library's decisions")
	flag.Parse()

	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = *data
	cfg.ListenPort = *peerPort
	// The harness compares clients on one isolated network with one tracker:
	// no DHT, no peer exchange, no port forwarding, as for every other row.
	cfg.NoDHT = true
	cfg.DisablePEX = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableIPv6 = true
	cfg.DisableUTP = *noUTP
	// Uploading after completion is part of the comparison.
	cfg.Seed = true
	if *debug {
		cfg.Debug = true
		cfg.Slogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	client, err := torrent.NewClient(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	d := &daemon{client: client, verifying: map[string]bool{}, state: *state}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") })
	mux.HandleFunc("/add", d.add)
	mux.HandleFunc("/recheck", d.recheck)
	mux.HandleFunc("/status", d.status)
	d.restore()
	log.Printf("anacrolix front end on %s, peers on %d, data in %s", *listen, *peerPort, *data)
	log.Fatal(http.ListenAndServe(*listen, mux))
}

// restore adds again the torrents of a previous run: a client keeps its
// torrents across a restart, and the harness restarts the container in the
// "many" scenario.
func (d *daemon) restore() {
	entries, err := os.ReadDir(d.state)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".torrent") {
			continue
		}
		path := filepath.Join(d.state, entry.Name())
		mi, err := metainfo.LoadFromFile(path)
		if err != nil {
			log.Printf("restoring %s: %v", entry.Name(), err)
			continue
		}
		if t, err := d.client.AddTorrent(mi); err == nil {
			t.DownloadAll()
		}
	}
}

// add takes the .torrent as a multipart file ("torrent") or as base64 in the
// torrent_b64 field.
func (d *daemon) add(w http.ResponseWriter, r *http.Request) {
	data, err := torrentBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	hash := mi.HashInfoBytes().HexString()
	if _, ok := d.client.Torrent(mi.HashInfoBytes()); !ok {
		t, err := d.client.AddTorrent(mi)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Adding a torrent only gives it metadata: the pieces it should fetch
		// are chosen by the priorities, and a client wants all of them.
		t.DownloadAll()
		d.remember(hash, data)
	}
	writeJSON(w, map[string]string{"hash": hash})
}

// remember keeps the .torrent on disk so a restart finds it.
func (d *daemon) remember(hash string, data []byte) {
	if err := os.MkdirAll(d.state, 0o750); err != nil {
		log.Printf("keeping %s: %v", hash, err)
		return
	}
	if err := os.WriteFile(filepath.Join(d.state, hash+".torrent"), data, 0o640); err != nil {
		log.Printf("keeping %s: %v", hash, err)
	}
}

func torrentBody(r *http.Request) ([]byte, error) {
	file, _, err := r.FormFile("torrent")
	if err == nil {
		defer file.Close()
		return io.ReadAll(file)
	}
	if encoded := r.FormValue("torrent_b64"); encoded != "" {
		return base64.StdEncoding.DecodeString(encoded)
	}
	return nil, errors.New("no torrent in the request")
}

// recheck hashes the files of a torrent again. The state is published as
// "checking" until the library is done, so the harness sees the check start.
func (d *daemon) recheck(w http.ResponseWriter, r *http.Request) {
	hash := r.FormValue("hash")
	t, ok := d.client.Torrent(metainfo.NewHashFromHex(hash))
	if !ok {
		http.Error(w, "torrent not found", http.StatusNotFound)
		return
	}
	d.mu.Lock()
	d.verifying[hash] = true
	d.mu.Unlock()
	go func() {
		err := t.VerifyData()
		d.mu.Lock()
		delete(d.verifying, hash)
		d.mu.Unlock()
		if err != nil {
			log.Printf("recheck %s: %v", hash, err)
		}
	}()
	writeJSON(w, map[string]string{"hash": hash})
}

// status reports every torrent: checking while a recheck runs, seeding when
// every byte is complete, downloading otherwise.
func (d *daemon) status(w http.ResponseWriter, _ *http.Request) {
	out := map[string]row{}
	for _, t := range d.client.Torrents() {
		hash := t.InfoHash().HexString()
		total, done := t.Length(), t.BytesCompleted()
		progress := 0.0
		if total > 0 {
			progress = float64(done) / float64(total)
		}
		d.mu.Lock()
		checking := d.verifying[hash]
		d.mu.Unlock()
		state := "downloading"
		switch {
		case checking:
			state = "checking"
		case total > 0 && done >= total:
			state = "seeding"
		}
		stats := t.Stats()
		out[hash] = row{State: state, Progress: progress, Peers: stats.PendingPeers, Active: stats.ActivePeers}
	}
	writeJSON(w, out)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("writing the answer: %v", err)
	}
}
