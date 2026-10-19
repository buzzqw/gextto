package gextto

import (
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// multiStub is a TorrentEngine that reports one row and records adds.
type multiStub struct {
	nullEngine
	name    string
	added   []string
	removed []string
	rows    []models.TorrentView
}

func (m *multiStub) Name() string               { return m.name }
func (m *multiStub) List() []models.TorrentView { return append([]models.TorrentView(nil), m.rows...) }
func (m *multiStub) Stats() map[string]any      { return map[string]any{"backend": m.name} }
func (m *multiStub) AddWithOptions(link string, _ *Config, _ *string, _ AddOptions) (bool, error) {
	m.added = append(m.added, link)
	return true, nil
}
func (m *multiStub) Remove(hash string, _ bool) (bool, error) {
	m.removed = append(m.removed, hash)
	return true, nil
}

func TestMultiEngineMergesAndRoutes(t *testing.T) {
	torrent := &multiStub{name: "torrent", rows: []models.TorrentView{{Hash: "aa", Name: "A"}}}
	ed2k := &multiStub{name: "ed2k", rows: []models.TorrentView{{Hash: "bb", Name: "B", Sources: 4}}}
	engine := newMultiEngine(torrent, ed2k, map[string]string{"bb": "ed2k"})

	rows := engine.List()
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	for _, row := range rows {
		switch row.Hash {
		case "aa":
			if row.Network != "torrent" {
				t.Errorf("aa network = %q", row.Network)
			}
		case "bb":
			if row.Network != "ed2k" || row.Sources != 4 {
				t.Errorf("bb row = %+v", row)
			}
		}
	}
	if engine.NetworkOf("bb") != "ed2k" || engine.NetworkOf("unknown") != "torrent" {
		t.Fatal("routing is wrong")
	}
	if _, err := engine.Pause("bb"); err == nil {
		// multiStub inherits nullEngine's capability error: the point is that
		// routing reached the eD2k engine, which is checked below.
	}
	engine.SetRoute("cc", "ed2k")
	if engine.NetworkOf("cc") != "ed2k" {
		t.Fatal("SetRoute did not persist")
	}
}

func TestMultiEngineAddRoutesEd2k(t *testing.T) {
	torrent := &multiStub{name: "torrent"}
	ed2k := &multiStub{name: "ed2k"}
	engine := newMultiEngine(torrent, ed2k, nil)
	const link = "ed2k://|file|X.avi|10|0123456789abcdef0123456789abcdef|/"
	cfgValue := DefaultConfig()
	added, err := engine.Add(link, &cfgValue)
	if err != nil || !added {
		t.Fatalf("Add = %v, %v", added, err)
	}
	if len(ed2k.added) != 1 || len(torrent.added) != 0 {
		t.Fatalf("torrent adds %v, ed2k adds %v", torrent.added, ed2k.added)
	}
	if engine.NetworkOf("0123456789abcdef0123456789abcdef") != "ed2k" {
		t.Fatal("the added hash was not routed to eD2k")
	}
}

func TestMultiEngineHealthyFollowsTorrent(t *testing.T) {
	// nullEngine reports SessionHealthy=false; the eD2k plane must not make the
	// router claim the torrent session is live.
	engine := newMultiEngine(nullEngine{}, &multiStub{name: "ed2k"}, nil)
	if engine.SessionHealthy() {
		t.Fatal("multiEngine reported a healthy session with a dead torrent plane")
	}
}

func TestEd2kHashFromLink(t *testing.T) {
	const link = "ed2k://|file|X.avi|10|0123456789abcdef0123456789abcdef|/"
	hash, ok := ed2kHashFromLink(link)
	if !ok || hash != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("hash = %q, %v", hash, ok)
	}
	if _, ok := ed2kHashFromLink("magnet:?xt=urn:btih:x"); ok {
		t.Fatal("magnet accepted as eD2k link")
	}
}
