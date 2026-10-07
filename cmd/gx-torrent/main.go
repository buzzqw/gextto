package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cenkalti/rain/torrent"
	"crypto/rand"
	"encoding/hex"
)

type Daemon struct {
	mu      sync.Mutex
	session *torrent.Session
	cfg     torrent.Config
}

func randomID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (d *Daemon) restartSession() error {
	if d.session != nil {
		d.session.Close()
	}
	s, err := torrent.NewSession(d.cfg)
	if err != nil {
		return err
	}
	d.session = s
	// s.StartAll() is not strictly needed because loadExistingTorrents resumes them automatically if they were running
	return nil
}

func main() {
	fmt.Println("Starting gx-torrent daemon...")

	cfg := torrent.DefaultConfig
	cfg.DataDir = "./downloads"
	cfg.DataDirIncludesTorrentID = true
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		log.Fatalf("Failed to create data dir: %v", err)
	}

	d := &Daemon{cfg: cfg}
	if err := d.restartSession(); err != nil {
		log.Fatalf("Failed to start rain session: %v", err)
	}
	defer d.session.Close()

	http.HandleFunc("/api/v1/torrents", d.listTorrents)
	http.HandleFunc("/api/v1/add", d.addTorrent)
	http.HandleFunc("/api/v1/add-file", d.addTorrentFile)
	http.HandleFunc("/api/v1/torrents/", d.handleTorrentAction)
	http.HandleFunc("/api/v1/config", d.handleConfig)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8890" // default port
	}

	log.Printf("Listening on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func (d *Daemon) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "invalid method", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SpeedLimitDownload *int64 `json:"speed_limit_download"`
		SpeedLimitUpload   *int64 `json:"speed_limit_upload"`
		MaxPeerDial        *int   `json:"max_peer_dial"`
		MaxPeerAccept      *int   `json:"max_peer_accept"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	restartNeeded := false
	if req.SpeedLimitDownload != nil && d.cfg.SpeedLimitDownload != *req.SpeedLimitDownload {
		d.cfg.SpeedLimitDownload = *req.SpeedLimitDownload
		restartNeeded = true
	}
	if req.SpeedLimitUpload != nil && d.cfg.SpeedLimitUpload != *req.SpeedLimitUpload {
		d.cfg.SpeedLimitUpload = *req.SpeedLimitUpload
		restartNeeded = true
	}
	if req.MaxPeerDial != nil && d.cfg.MaxPeerDial != *req.MaxPeerDial {
		d.cfg.MaxPeerDial = *req.MaxPeerDial
		restartNeeded = true
	}
	if req.MaxPeerAccept != nil && d.cfg.MaxPeerAccept != *req.MaxPeerAccept {
		d.cfg.MaxPeerAccept = *req.MaxPeerAccept
		restartNeeded = true
	}

	if restartNeeded {
		if err := d.restartSession(); err != nil {
			http.Error(w, "failed to restart session", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) prepareDestination(dest string) (string, error) {
	id := randomID()
	if dest != "" {
		if err := os.MkdirAll(dest, 0755); err != nil {
			return "", err
		}
		symlinkPath := filepath.Join(d.cfg.DataDir, id)
		if err := os.Symlink(dest, symlinkPath); err != nil {
			return "", err
		}
	}
	return id, nil
}

func (d *Daemon) listTorrents(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	session := d.session
	d.mu.Unlock()
	
	torrents := session.ListTorrents()
	var result []map[string]interface{}

	for _, t := range torrents {
		stats := t.Stats()
		state := "downloading"
		if stats.Status == torrent.Stopped {
			state = "paused"
		} else if stats.Status == torrent.Seeding {
			state = "uploading"
		}

		result = append(result, map[string]interface{}{
			"hash":          t.InfoHash().String(),
			"name":          t.Name(),
			"state":         state,
			"progress":      stats.Bytes.Completed,
			"size":          stats.Bytes.Total,
			"download_rate": stats.Speed.Download,
			"upload_rate":   stats.Speed.Upload,
			"uploaded":      stats.Bytes.Uploaded,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (d *Daemon) addTorrent(w http.ResponseWriter, r *http.Request) {
	magnet := r.FormValue("magnet")
	dest := r.FormValue("destination")
	if magnet == "" {
		http.Error(w, "missing magnet", http.StatusBadRequest)
		return
	}

	id, err := d.prepareDestination(dest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	d.mu.Lock()
	session := d.session
	d.mu.Unlock()

	opt := &torrent.AddTorrentOptions{ID: id}
	t, err := session.AddURI(magnet, opt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	t.Start()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"hash": t.InfoHash().String(),
	})
}

func (d *Daemon) addTorrentFile(w http.ResponseWriter, r *http.Request) {
	file, _, err := r.FormFile("torrent")
	dest := r.FormValue("destination")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()
	
	id, err := d.prepareDestination(dest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	d.mu.Lock()
	session := d.session
	d.mu.Unlock()

	opt := &torrent.AddTorrentOptions{ID: id}
	t, err := session.AddTorrent(file, opt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	t.Start()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"hash": t.InfoHash().String(),
	})
}

func (d *Daemon) handleTorrentAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/torrents/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "missing hash", http.StatusBadRequest)
		return
	}
	hashStr := parts[0]
	
	d.mu.Lock()
	session := d.session
	d.mu.Unlock()
	
	var t *torrent.Torrent
	for _, tor := range session.ListTorrents() {
		if tor.InfoHash().String() == hashStr {
			t = tor
			break
		}
	}
	
	if t == nil {
		http.Error(w, "torrent not found", http.StatusNotFound)
		return
	}

	if r.Method == "DELETE" {
		err := session.RemoveTorrent(t.ID())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	if len(parts) < 2 {
		http.Error(w, "missing action", http.StatusBadRequest)
		return
	}
	action := parts[1]

	var err error
	var result interface{}

	switch action {
	case "pause":
		err = t.Stop()
	case "resume":
		err = t.Start()
	case "verify":
		err = t.Verify()
	case "move":
		// 'move' can be faked if we move the physical files, but for now we just return OK since rain doesn't support changing path per torrent
		// Wait, if it's already symlinked, we could move the symlink target.
		dest := r.FormValue("destination")
		if dest != "" {
			err = fmt.Errorf("move not supported dynamically in rain")
		}
	case "files":
		files, _ := t.Files()
		fileStats, _ := t.FileStats()
		var resFiles []map[string]interface{}
		for i, f := range files {
			prog := int64(0)
			if i < len(fileStats) {
				prog = fileStats[i].BytesCompleted
			}
			resFiles = append(resFiles, map[string]interface{}{
				"path":     f.Path,
				"size":     f.Length,
				"progress": prog,
			})
		}
		result = resFiles
	case "peers":
		peers := t.Peers()
		var resPeers []map[string]interface{}
		for _, p := range peers {
			resPeers = append(resPeers, map[string]interface{}{
				"ip": p.Addr.String(),
			})
		}
		result = resPeers
	case "trackers":
		trackers := t.Trackers()
		var resTrackers []map[string]interface{}
		for _, tr := range trackers {
			resTrackers = append(resTrackers, map[string]interface{}{
				"url": tr.URL,
			})
		}
		result = resTrackers
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if result != nil {
		json.NewEncoder(w).Encode(result)
	} else {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}
