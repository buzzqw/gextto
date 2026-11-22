package gextto

import (
	"errors"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// fakeProber embeds the contract so only ScrapeSwarm is ever reached.
type fakeProber struct {
	TorrentEngine
	seeders int
	peers   int
	err     error
	calls   int
}

func (f *fakeProber) ScrapeSwarm(string) (int, int, error) {
	f.calls++
	return f.seeders, f.peers, f.err
}

// TestProbeSwarmCountsFillsAnUnknownCount: a release the indexer left at -1
// takes the DHT's numbers before the gate decides.
func TestProbeSwarmCountsFillsAnUnknownCount(t *testing.T) {
	engine := &fakeProber{seeders: 7, peers: 3}
	release := &models.Release{Title: "Series A S01E01 1080p", Seeders: -1, Peers: -1,
		Magnet: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)}
	probeSwarmCounts(engine, release)
	if release.Seeders != 7 || release.Peers != 3 {
		t.Fatalf("seeders=%d peers=%d, want 7 and 3", release.Seeders, release.Peers)
	}
	if engine.calls != 1 {
		t.Fatalf("scrape calls = %d, want 1", engine.calls)
	}
}

// TestProbeSwarmCountsLeavesAKnownCountAlone: an indexer that reported a number
// is not second-guessed, so the probe does not run at all.
func TestProbeSwarmCountsLeavesAKnownCountAlone(t *testing.T) {
	engine := &fakeProber{seeders: 7, peers: 3}
	release := &models.Release{Seeders: 0, Peers: 0,
		Magnet: "magnet:?xt=urn:btih:" + strings.Repeat("b", 40)}
	probeSwarmCounts(engine, release)
	if release.Seeders != 0 || release.Peers != 0 || engine.calls != 0 {
		t.Fatalf("a known count was probed: seeders=%d calls=%d", release.Seeders, engine.calls)
	}
}

// TestProbeSwarmCountsKeepsTheReleaseWhenNobodyAnswers: a private tracker does
// not answer the DHT, so an empty answer is no evidence that the swarm is dead.
func TestProbeSwarmCountsKeepsTheReleaseWhenNobodyAnswers(t *testing.T) {
	engine := &fakeProber{seeders: 0, peers: 0}
	release := &models.Release{Seeders: -1, Peers: -1,
		Magnet: "magnet:?xt=urn:btih:" + strings.Repeat("c", 40)}
	probeSwarmCounts(engine, release)
	if release.Seeders != -1 || release.Peers != -1 {
		t.Fatalf("an empty answer changed the release: seeders=%d peers=%d", release.Seeders, release.Peers)
	}
}

// TestProbeSwarmCountsSurvivesAFailure: a daemon that cannot scrape leaves the
// release untouched and the cycle goes on.
func TestProbeSwarmCountsSurvivesAFailure(t *testing.T) {
	engine := &fakeProber{err: errors.New("dht off")}
	release := &models.Release{Seeders: -1, Peers: -1,
		Magnet: "magnet:?xt=urn:btih:" + strings.Repeat("d", 40)}
	probeSwarmCounts(engine, release)
	if release.Seeders != -1 || release.Peers != -1 {
		t.Fatalf("a failed scrape changed the release: %+v", release)
	}
}

// plainEngine has the contract and nothing more: no DHT to ask.
type plainEngine struct{ TorrentEngine }

// TestProbeSwarmCountsIgnoresAnEngineWithoutADHT: an engine that cannot scrape
// is not asked, and nothing changes.
func TestProbeSwarmCountsIgnoresAnEngineWithoutADHT(t *testing.T) {
	release := &models.Release{Seeders: -1, Peers: -1,
		Magnet: "magnet:?xt=urn:btih:" + strings.Repeat("e", 40)}
	probeSwarmCounts(&plainEngine{}, release)
	if release.Seeders != -1 || release.Peers != -1 {
		t.Fatalf("an engine without a DHT changed the release: %+v", release)
	}
	// A nil engine is answered without a panic.
	probeSwarmCounts(nil, release)
}
