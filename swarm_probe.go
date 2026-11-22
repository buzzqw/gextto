package gextto

import (
	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

// probeSwarmCounts replaces a release's unknown seeder count with what the
// engine's DHT answers, before the release is approved and takes a slot. It runs
// only for an unknown count: a known one is the indexer's word and needs no
// second opinion. A DHT that answers nobody leaves the release as it is — a
// private tracker does not answer the DHT at all, so an empty answer is no
// evidence that the swarm is dead, and the probe that watches the first minutes
// of the download stays the one that decides that.
func probeSwarmCounts(torrents TorrentEngine, release *models.Release) {
	if torrents == nil || release == nil || release.Seeders >= 0 || release.Ed2kLink != "" {
		return
	}
	prober, ok := torrents.(TorrentEngineSwarmProbe)
	if !ok {
		return
	}
	hash, ok := utils.MagnetHash(release.Magnet)
	if !ok {
		return
	}
	seeders, peers, err := prober.ScrapeSwarm(hash)
	if err != nil {
		logging.Debug("the DHT could not count the peers of a release", "target", logTarget(release), "error", err)
		return
	}
	if seeders <= 0 && peers <= 0 {
		return
	}
	release.Seeders = int64(seeders)
	release.Peers = int64(peers)
	logging.Debug("the DHT counted the peers of a release the indexer left unknown",
		"target", logTarget(release), "seeders", seeders, "peers", peers)
}
