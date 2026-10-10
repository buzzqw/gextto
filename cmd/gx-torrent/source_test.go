package main

import (
	"testing"

	"github.com/buzzqw/gextto/internal/engine/torrent"
)

// The daemon web UI must label every peer source, including the BEP 55 one.
func TestSourceNameCoversHolepunch(t *testing.T) {
	if got := sourceName(torrent.SourceHolepunch); got != "holepunch" {
		t.Fatalf("sourceName(SourceHolepunch) = %q, want holepunch", got)
	}
	for _, source := range []torrent.PeerSource{
		torrent.SourceTracker, torrent.SourceDHT, torrent.SourcePEX, torrent.SourceIncoming, torrent.SourceManual,
	} {
		if got := sourceName(source); got == "" || got == "—" {
			t.Fatalf("sourceName(%d) = %q, want a label", source, got)
		}
	}
}
