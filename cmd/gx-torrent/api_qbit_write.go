package main

// api_qbit_write.go implements the qBittorrent write endpoints that go beyond
// the base surface in api_qbit.go: adding and banning peers, editing a
// category, toggling first/last piece priority, moving a torrent in the queue
// and shutting the daemon down. Registered only in standalone mode.

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// handleQbitAddPeers adds "peers" (ip:port, pipe/comma separated) to the given
// torrents.
func (d *Daemon) handleQbitAddPeers(w http.ResponseWriter, r *http.Request) {
	peers := splitFormList(r.FormValue("peers"))
	if len(peers) > 0 {
		for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
			d.mu.Lock()
			t, _ := d.findLocked(hash)
			d.mu.Unlock()
			if t == nil {
				continue
			}
			for _, peer := range peers {
				_ = t.AddPeer(peer)
			}
		}
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitBanPeers bans "peers" (ip:port or ip) in the whole session.
func (d *Daemon) handleQbitBanPeers(w http.ResponseWriter, r *http.Request) {
	if d.session != nil {
		for _, peer := range splitFormList(r.FormValue("peers")) {
			ip := strings.TrimSpace(peer)
			if host, _, err := net.SplitHostPort(ip); err == nil {
				ip = host
			}
			if ip != "" {
				d.session.BanIP(ip, banTTL)
			}
		}
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitEditCategory sets the save path of an existing category.
func (d *Daemon) handleQbitEditCategory(w http.ResponseWriter, r *http.Request) {
	d.editCategory(r.FormValue("category"), r.FormValue("savePath"))
	w.WriteHeader(http.StatusOK)
}

// handleQbitToggleFirstLast toggles first/last piece priority on the torrents.
func (d *Daemon) handleQbitToggleFirstLast(w http.ResponseWriter, r *http.Request) {
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.toggleFirstLast(hash)
	}
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitIncreasePrio(w http.ResponseWriter, r *http.Request) {
	d.qbitPriority(w, r, "up")
}

func (d *Daemon) handleQbitDecreasePrio(w http.ResponseWriter, r *http.Request) {
	d.qbitPriority(w, r, "down")
}

func (d *Daemon) handleQbitTopPrio(w http.ResponseWriter, r *http.Request) {
	d.qbitPriority(w, r, "top")
}

func (d *Daemon) handleQbitBottomPrio(w http.ResponseWriter, r *http.Request) {
	d.qbitPriority(w, r, "bottom")
}

// qbitPriority moves the given torrents in the queue: "up"/"down" swap with the
// nearest neighbour, "top"/"bottom" go to the ends.
func (d *Daemon) qbitPriority(w http.ResponseWriter, r *http.Request, mode string) {
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		switch mode {
		case "up":
			_ = d.shiftPriority(hash, true)
		case "down":
			_ = d.shiftPriority(hash, false)
		case "top":
			_ = d.moveToTop(hash)
		case "bottom":
			_ = d.moveToBottom(hash)
		}
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitShutdown stops the daemon cleanly, like qBittorrent's
// /app/shutdown. The API is exposed only in standalone mode, so this cannot
// stop a Gextto-managed daemon; the signal is delivered after the response.
func (d *Daemon) handleQbitShutdown(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	go func() {
		time.Sleep(200 * time.Millisecond)
		if p, err := os.FindProcess(os.Getpid()); err == nil {
			// os.Interrupt is SIGINT on Unix, which the daemon handles as a
			// clean shutdown; on Windows the call is a no-op error, ignored.
			_ = p.Signal(os.Interrupt)
		}
	}()
}

// handleQbitToggleSpeedLimitsMode flips the manual alternative-limits mode (the
// qBittorrent speed-limit button) and applies the new effective limits at once.
func (d *Daemon) handleQbitToggleSpeedLimitsMode(w http.ResponseWriter, _ *http.Request) {
	if d.opts.Settings == nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _, manual := d.altSpeedSettings()
	d.opts.Settings.Set(altSpeedEnabledKey, strconv.FormatBool(!manual))
	_ = d.opts.Settings.Save()
	d.applyScheduleNow()
	w.WriteHeader(http.StatusOK)
}

// handleQbitRename renames a torrent (display name and content root).
func (d *Daemon) handleQbitRename(w http.ResponseWriter, r *http.Request) {
	hash := strings.TrimSpace(r.FormValue("hash"))
	name := strings.TrimSpace(r.FormValue("name"))
	if hash == "" || name == "" {
		http.Error(w, "hash and name are required", http.StatusBadRequest)
		return
	}
	if err := d.renameTorrent(hash, name); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitRenameFile renames one file, or a folder (same prefix semantics).
func (d *Daemon) handleQbitRenameFile(w http.ResponseWriter, r *http.Request) {
	d.qbitRenamePath(w, r, d.renameFile)
}

// handleQbitRenameFolder renames a folder.
func (d *Daemon) handleQbitRenameFolder(w http.ResponseWriter, r *http.Request) {
	d.qbitRenamePath(w, r, d.renameFolder)
}

func (d *Daemon) qbitRenamePath(w http.ResponseWriter, r *http.Request, apply func(hash, oldPath, newPath string) error) {
	hash := strings.TrimSpace(r.FormValue("hash"))
	oldPath := strings.TrimSpace(r.FormValue("oldPath"))
	newPath := strings.TrimSpace(r.FormValue("newPath"))
	if hash == "" || oldPath == "" || newPath == "" {
		http.Error(w, "hash, oldPath and newPath are required", http.StatusBadRequest)
		return
	}
	if err := apply(hash, oldPath, newPath); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusOK)
}
