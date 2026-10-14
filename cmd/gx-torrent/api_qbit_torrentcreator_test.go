package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestQbitTorrentCreatorAPI covers the qBittorrent torrent-creator API: addTask,
// status, torrentFile and deleteTask, on top of the engine's CreateTorrent.
func TestQbitTorrentCreatorAPI(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	dir := t.TempDir()
	source := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(source, make([]byte, 40_000), 0o644); err != nil {
		t.Fatal(err)
	}
	outFile := filepath.Join(dir, "created.torrent")

	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	var started struct {
		TaskID string `json:"taskID"`
	}
	if code := postFormJSON(t, srv.URL+"/api/v2/torrentcreator/addTask",
		url.Values{"sourcePath": {source}, "pieceSize": {"16384"}, "torrentFilePath": {outFile}}, &started); code != http.StatusOK {
		t.Fatalf("addTask: %d", code)
	}
	if started.TaskID == "" {
		t.Fatal("addTask returned no taskID")
	}

	deadline := time.Now().Add(5 * time.Second)
	var status []map[string]any
	for {
		status = nil
		getJSONInto(t, srv.URL+"/api/v2/torrentcreator/status?taskID="+started.TaskID, &status)
		if len(status) == 1 && status[0]["status"] == "Finished" {
			break
		}
		if len(status) == 1 && status[0]["status"] == "Failed" {
			t.Fatalf("creation failed: %+v", status[0])
		}
		if time.Now().After(deadline) {
			t.Fatalf("creation did not finish: %+v", status)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status[0]["sourcePath"] != source || status[0]["format"] != "v1" {
		t.Fatalf("task status = %+v", status[0])
	}

	// The .torrent is served and also written to torrentFilePath.
	resp := mustGet(t, srv.URL+"/api/v2/torrentcreator/torrentFile?taskID="+started.TaskID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("torrentFile: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) == 0 || body[0] != 'd' {
		t.Fatalf("torrentFile is not a bencoded torrent: %q", body)
	}
	if written, err := os.ReadFile(outFile); err != nil || len(written) == 0 {
		t.Fatalf("torrentFilePath was not written: %v", err)
	}

	// deleteTask removes it.
	if code := postFormJSON(t, srv.URL+"/api/v2/torrentcreator/deleteTask",
		url.Values{"taskID": {started.TaskID}}, nil); code != http.StatusOK {
		t.Fatalf("deleteTask: %d", code)
	}
	status = nil
	getJSONInto(t, srv.URL+"/api/v2/torrentcreator/status", &status)
	if len(status) != 0 {
		t.Fatalf("task not deleted: %+v", status)
	}
}
