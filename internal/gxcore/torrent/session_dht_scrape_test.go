package torrent

import (
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
)

// TestDHTScrapeRefusesAPrivateTorrent pins BEP 27 against the operator's own
// button: a scrape would put a private torrent's info hash on the DHT, so the
// session must refuse it even when the hash belongs to a torrent it is running.
func TestDHTScrapeRefusesAPrivateTorrent(t *testing.T) {
	s := &Session{
		torrents: map[string]*Torrent{},
		config:   Config{DHTEnabled: true},
	}
	var ih [20]byte
	ih[0] = 1
	inner := &torrent{session: s, infoHash: ih}
	inner.setInfo(&metainfo.Info{Private: true})
	s.torrents["one"] = &Torrent{torrent: inner}

	if _, _, err := s.DHTScrape(ih, time.Second); err == nil {
		t.Fatal("a private torrent was scraped")
	} else if !strings.Contains(err.Error(), "private") {
		t.Fatalf("the error does not say why: %v", err)
	}
}

// TestDHTScrapeAllowsAPublicTorrent pins that the guard is about private
// torrents only: a public one goes on to the DHT check, which fails here
// because the session runs no node.
func TestDHTScrapeAllowsAPublicTorrent(t *testing.T) {
	s := &Session{
		torrents: map[string]*Torrent{},
		config:   Config{DHTEnabled: true},
	}
	var ih [20]byte
	ih[0] = 2
	inner := &torrent{session: s, infoHash: ih}
	inner.setInfo(&metainfo.Info{})
	s.torrents["two"] = &Torrent{torrent: inner}

	if _, _, err := s.DHTScrape(ih, time.Second); err == nil {
		t.Fatal("a scrape without a DHT node reported success")
	} else if strings.Contains(err.Error(), "private") {
		t.Fatalf("a public torrent was refused as private: %v", err)
	}
}
