package main

// api_qbit_torrentcreator.go implements the qBittorrent torrent-creator API
// (/api/v2/torrentcreator/*) on top of torrent.CreateTorrent. qBittorrent runs
// the creation as a background task; here too: addTask starts it and keeps the
// result until the client deletes the task. The engine builds a v1 .torrent, so
// a requested v2/hybrid format is produced as v1 (widely compatible).
// Registered only in standalone mode, like the rest of /api/v2.

import (
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// qbitCreateTask is one torrent-creation task.
type qbitCreateTask struct {
	id              string
	sourcePath      string
	torrentFilePath string
	pieceSize       int
	private         bool
	comment         string
	trackers        []string
	urlSeeds        []string
	status          string // Queued, Running, Finished, Failed
	timeAdded       int64
	timeStarted     int64
	timeFinished    int64
	errorMsg        string
	data            []byte
}

func (d *Daemon) handleQbitTorrentCreatorAdd(w http.ResponseWriter, r *http.Request) {
	sourcePath := strings.TrimSpace(r.FormValue("sourcePath"))
	if sourcePath == "" {
		http.Error(w, "sourcePath is required", http.StatusBadRequest)
		return
	}
	// pieceSize is a power of two between 16 KiB and 64 MiB (qBittorrent's
	// limits); anything else, or an empty value, lets the creator choose. The
	// range is checked before the int -> uint32 conversion.
	pieceSize := 0
	if raw := strings.TrimSpace(r.FormValue("pieceSize")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 16<<10 && v <= 64<<20 {
			pieceSize = v
		}
	}
	now := time.Now().Unix()
	task := &qbitCreateTask{
		sourcePath:      sourcePath,
		torrentFilePath: strings.TrimSpace(r.FormValue("torrentFilePath")),
		pieceSize:       pieceSize,
		private:         formBool(r, "private"),
		comment:         r.FormValue("comment"),
		trackers:        qbitCreatorURLs(r.FormValue("trackers")),
		urlSeeds:        qbitCreatorURLs(r.FormValue("urlSeeds")),
		status:          "Running",
		timeAdded:       now,
		timeStarted:     now,
	}
	d.qbitCreateMu.Lock()
	if d.qbitCreate == nil {
		d.qbitCreate = map[string]*qbitCreateTask{}
	}
	d.qbitCreateSeq++
	id := strconv.Itoa(d.qbitCreateSeq)
	task.id = id
	d.qbitCreate[id] = task
	d.qbitCreateMu.Unlock()

	go d.qbitCreateRun(id)
	writeJSON(w, http.StatusOK, map[string]any{"taskID": id})
}

func (d *Daemon) qbitCreateRun(id string) {
	d.qbitCreateMu.Lock()
	task := d.qbitCreate[id]
	d.qbitCreateMu.Unlock()
	if task == nil {
		return
	}
	data, err := torrent.CreateTorrent(task.sourcePath, torrent.CreateOptions{
		Trackers:    task.trackers,
		Webseeds:    task.urlSeeds,
		Private:     task.private,
		PieceLength: uint32(task.pieceSize),
		Comment:     task.comment,
	})
	if err == nil && task.torrentFilePath != "" {
		if writeErr := os.WriteFile(task.torrentFilePath, data, 0o644); writeErr != nil {
			err = writeErr
		}
	}
	d.qbitCreateMu.Lock()
	if current := d.qbitCreate[id]; current != nil {
		current.timeFinished = time.Now().Unix()
		if err != nil {
			current.status = "Failed"
			current.errorMsg = err.Error()
		} else {
			current.status = "Finished"
			current.data = data
		}
	}
	d.qbitCreateMu.Unlock()
}

func (d *Daemon) handleQbitTorrentCreatorStatus(w http.ResponseWriter, r *http.Request) {
	id := qbitCreateID(r)
	d.qbitCreateMu.Lock()
	ids := make([]string, 0, len(d.qbitCreate))
	for key := range d.qbitCreate {
		if id == "" || key == id {
			ids = append(ids, key)
		}
	}
	tasks := make([]*qbitCreateTask, 0, len(ids))
	for _, key := range ids {
		task := d.qbitCreate[key]
		copy := *task
		tasks = append(tasks, &copy)
	}
	d.qbitCreateMu.Unlock()
	sort.Slice(tasks, func(i, j int) bool {
		a, _ := strconv.Atoi(tasks[i].id)
		b, _ := strconv.Atoi(tasks[j].id)
		return a < b
	})
	out := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, task.json())
	}
	writeJSON(w, http.StatusOK, out)
}

func (t *qbitCreateTask) json() map[string]any {
	node := map[string]any{
		"taskID":         t.id,
		"sourcePath":     t.sourcePath,
		"pieceSize":      t.pieceSize,
		"ignoreDotfiles": false,
		"private":        t.private,
		"format":         "v1",
		"status":         t.status,
		"timeAdded":      t.timeAdded,
	}
	if t.comment != "" {
		node["comment"] = t.comment
	}
	if t.torrentFilePath != "" {
		node["torrentFilePath"] = t.torrentFilePath
	}
	if len(t.trackers) > 0 {
		node["trackers"] = t.trackers
	}
	if len(t.urlSeeds) > 0 {
		node["urlSeeds"] = t.urlSeeds
	}
	if t.timeStarted > 0 {
		node["timeStarted"] = t.timeStarted
	}
	if t.timeFinished > 0 {
		node["timeFinished"] = t.timeFinished
	}
	if t.status == "Failed" {
		node["errorMessage"] = t.errorMsg
	}
	return node
}

func (d *Daemon) handleQbitTorrentCreatorFile(w http.ResponseWriter, r *http.Request) {
	id := qbitCreateID(r)
	d.qbitCreateMu.Lock()
	task := d.qbitCreate[id]
	var data []byte
	status := ""
	if task != nil {
		data = task.data
		status = task.status
	}
	d.qbitCreateMu.Unlock()
	if task == nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if status != "Finished" {
		http.Error(w, "torrent creation is not finished", http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", `attachment; filename="`+id+`.torrent"`)
	_, _ = w.Write(data)
}

func (d *Daemon) handleQbitTorrentCreatorDelete(w http.ResponseWriter, r *http.Request) {
	id := qbitCreateID(r)
	d.qbitCreateMu.Lock()
	if _, ok := d.qbitCreate[id]; ok {
		delete(d.qbitCreate, id)
	}
	d.qbitCreateMu.Unlock()
	w.WriteHeader(http.StatusOK)
}

// qbitCreateID reads the "taskID" parameter (query or form).
func qbitCreateID(r *http.Request) string {
	if id := strings.TrimSpace(r.FormValue("taskID")); id != "" {
		return id
	}
	return strings.TrimSpace(r.URL.Query().Get("taskID"))
}

// qbitCreatorURLs splits the pipe-separated, percent-encoded tracker/webseed
// list qBittorrent sends.
func qbitCreatorURLs(value string) []string {
	fields := strings.Split(value, "|")
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if decoded, err := url.PathUnescape(field); err == nil {
			field = decoded
		}
		out = append(out, field)
	}
	return out
}
