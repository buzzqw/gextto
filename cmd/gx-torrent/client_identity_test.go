package main

import (
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

func TestClientIdentityPresets(t *testing.T) {
	gx := resolveClientIdentity("gx-core", "")
	if gx.PeerID != "-GX"+constants.GxTorrentVersion()+"-" || gx.Client != "gx-core "+constants.GxTorrentVersion() {
		t.Fatalf("gx identity = %+v", gx)
	}
	qb := resolveClientIdentity("qbittorrent", "5.2.3")
	if qb.PeerID != "-qB5230-" || qb.Client != "qBittorrent 5.2.3" || qb.UserAgent != "qBittorrent/5.2.3" {
		t.Fatalf("qBittorrent identity = %+v", qb)
	}
	tr := resolveClientIdentity("transmission", "4.1.3")
	if tr.PeerID != "-TR4130-" || tr.Client != "Transmission 4.1.3" {
		t.Fatalf("Transmission identity = %+v", tr)
	}
	rain := resolveClientIdentity("rain", "2.4.4")
	if rain.PeerID != "-RN2.4.4-" || rain.Client != "Rain 2.4.4" {
		t.Fatalf("Rain identity = %+v", rain)
	}
	ana := resolveClientIdentity("anacrolix", "1.61.0")
	if ana.PeerID != "-GT0003-" || ana.Client != "anacrolix/torrent v1.61.0" || ana.UserAgent != "anacrolix-torrent/v1.61.0" {
		t.Fatalf("anacrolix identity = %+v", ana)
	}
	for _, id := range []clientIdentity{gx, qb, tr, rain, ana} {
		if len(id.PeerID) > 20 {
			t.Fatalf("peer id prefix %q is longer than 20 bytes", id.PeerID)
		}
	}
}

func TestClientIdentityNormalisation(t *testing.T) {
	if got := normalizeClientIdentity("nope"); got != "gx-core" {
		t.Fatalf("unknown identity = %q", got)
	}
	if got := clientIdentityVersion("5.2.3 beta"); got != "" {
		t.Fatalf("a version with spaces must be refused, got %q", got)
	}
	if got := clientIdentityVersion("1.2.3.4.5.6.7.8.9"); len(got) > 12 {
		t.Fatalf("version not capped: %q", got)
	}
	if !strings.Contains(clientIdentityOptions("qbittorrent"), `value="qbittorrent" selected`) {
		t.Fatal("the selected preset is not marked")
	}
}

func TestApplyClientIdentityFromSettings(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(clientIdentityKey, "transmission")
	d.opts.Settings.Set(clientIdentityVersionKey, "4.1.3")
	cfg := torrent.DefaultConfig
	d.applyClientIdentity(&cfg)
	if cfg.PublicPeerIDPrefix != "-TR4130-" || cfg.PublicExtensionHandshakeClientVersion != "Transmission 4.1.3" || cfg.PublicTrackerHTTPUserAgent != "Transmission/4.1.3" {
		t.Fatalf("public identity = %q %q %q", cfg.PublicPeerIDPrefix, cfg.PublicExtensionHandshakeClientVersion, cfg.PublicTrackerHTTPUserAgent)
	}
	if cfg.PrivatePeerIDPrefix != cfg.PublicPeerIDPrefix || cfg.PrivateExtensionHandshakeClientVersion != cfg.PublicExtensionHandshakeClientVersion {
		t.Fatal("private torrents must use the selected identity too")
	}
}
