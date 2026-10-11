package torrent

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
)

func TestCreateTorrentDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("second file"), 0o600); err != nil {
		t.Fatal(err)
	}

	data, err := CreateTorrent(dir, CreateOptions{
		Trackers: []string{"http://tracker.example/announce"},
		Comment:  "made by test",
	})
	if err != nil {
		t.Fatalf("CreateTorrent: %s", err)
	}

	mi, err := metainfo.New(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("the created torrent does not parse: %s", err)
	}
	if mi.Info.Name != filepath.Base(dir) {
		t.Errorf("name = %q, want %q", mi.Info.Name, filepath.Base(dir))
	}
	if got := mi.Info.Length; got != int64(len("hello world")+len("second file")) {
		t.Errorf("length = %d", got)
	}
	var files int
	for _, f := range mi.Info.Files {
		if !f.Padding {
			files++
		}
	}
	if files != 2 {
		t.Errorf("files = %d, want 2", files)
	}
	if mi.Info.Hash == ([20]byte{}) {
		t.Error("info hash not computed")
	}
	if len(mi.AnnounceList) == 0 || mi.AnnounceList[0][0] != "http://tracker.example/announce" {
		t.Errorf("announce list = %v", mi.AnnounceList)
	}
}

func TestCreateTorrentSingleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.mkv")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := CreateTorrent(path, CreateOptions{Name: "renamed.mkv"})
	if err != nil {
		t.Fatalf("CreateTorrent: %s", err)
	}
	mi, err := metainfo.New(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	if mi.Info.Name != "renamed.mkv" || mi.Info.Length != 10 {
		t.Errorf("info = %q len %d", mi.Info.Name, mi.Info.Length)
	}
	if len(mi.Info.Files) != 1 || mi.Info.Files[0].Path != "renamed.mkv" {
		t.Errorf("files = %+v", mi.Info.Files)
	}
}

func TestCreateTorrentMissingPath(t *testing.T) {
	if _, err := CreateTorrent(filepath.Join(t.TempDir(), "nope"), CreateOptions{}); err == nil {
		t.Fatal("expected an error for a missing path")
	}
}
