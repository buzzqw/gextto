package main

// feed_dht.go adds a BEP 46 "DHT feed" source to the standalone feed engine: a
// feed URL of the form dht://<ed25519-pubkey-hex>[?salt=<hex>] is resolved
// through the DHT as a signed mutable item whose value lists torrents (the
// "items"/"ih" form decoded by the engine). It reuses the whole feed pipeline —
// rules, archive, dedup, add — because each entry becomes an rss.Item carrying
// a magnet link. In managed mode Gextto owns acquisition, so the DHT feed runs
// like the other feeds: only in standalone mode.

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
	"github.com/buzzqw/gextto/internal/rss"
)

const (
	// dhtFeedScheme tags a DHT feed in the feed URL field, so the existing feed
	// editor, storage and list need no new field.
	dhtFeedScheme = "dht"
	// dhtFeedTimeout bounds one DHT lookup of a feed (the engine's own resolver
	// default is 8s; a feed poll is not latency-critical, so it is roomier).
	dhtFeedTimeout = 20 * time.Second
	// dhtFeedMaxSalt mirrors BEP 44's 64-byte salt ceiling.
	dhtFeedMaxSalt = 64
)

// isDHTFeed reports whether a feed URL addresses a BEP 46 feed.
func isDHTFeed(raw string) bool {
	_, _, ok := dhtFeedTarget(raw)
	return ok
}

// dhtFeedTarget parses "dht://<pubkey-hex>[?salt=<hex>]" (also tolerating
// "dht:<pubkey-hex>"). It returns the ed25519 public key and the optional salt.
func dhtFeedTarget(raw string) (ed25519.PublicKey, []byte, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(raw), dhtFeedScheme+":") {
		return nil, nil, false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, nil, false
	}
	keyHex := parsed.Host
	if keyHex == "" {
		// "dht:<key>?...": url.Parse keeps the key in Opaque.
		keyHex = parsed.Opaque
	}
	keyHex = strings.TrimSpace(strings.TrimPrefix(keyHex, "//"))
	pub, err := hex.DecodeString(keyHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, nil, false
	}
	var salt []byte
	if s := strings.TrimSpace(parsed.Query().Get("salt")); s != "" {
		b, err := hex.DecodeString(s)
		if err != nil || len(b) > dhtFeedMaxSalt {
			return nil, nil, false
		}
		salt = b
	}
	return ed25519.PublicKey(pub), salt, true
}

// fetchDHTFeedItems resolves a DHT feed and maps its torrents to RSS items. A
// DHT that is disabled or unreachable surfaces as the feed's last error, like a
// failing RSS URL.
func (d *Daemon) fetchDHTFeedItems(rawURL string) ([]rss.Item, error) {
	pub, salt, ok := dhtFeedTarget(rawURL)
	if !ok {
		return nil, errors.New("invalid DHT feed URL (expected dht://<pubkey-hex>[?salt=<hex>])")
	}
	if d.session == nil {
		return nil, errors.New("the torrent session is not ready")
	}
	items, _, err := d.session.ResolveDHTFeed(pub, salt, nil, dhtFeedTimeout)
	if err != nil {
		return nil, err
	}
	return dhtFeedItemsToRSS(items), nil
}

// dhtFeedItemsToRSS maps engine feed entries to RSS items, each as a magnet so
// the existing add path (addFeedItem) handles them unchanged.
func dhtFeedItemsToRSS(items []torrent.DHTFeedItem) []rss.Item {
	out := make([]rss.Item, 0, len(items))
	for _, it := range items {
		hash := hex.EncodeToString(it.InfoHash[:])
		title := strings.TrimSpace(it.Name)
		if title == "" {
			title = hash
		}
		out = append(out, rss.Item{
			Title:  title,
			Magnet: "magnet:?xt=urn:btih:" + hash,
			GUID:   "dht:" + hash,
			Source: "dht",
		})
	}
	return out
}

// handleDHTFeed resolves a BEP 46 feed for external consumers (Gextto in
// managed mode, scripts): GET /api/v1/dht-feed?key=<pubkey-hex>[&salt=<hex>].
func (d *Daemon) handleDHTFeed(w http.ResponseWriter, r *http.Request) {
	raw := "dht://" + strings.TrimSpace(r.URL.Query().Get("key"))
	if s := strings.TrimSpace(r.URL.Query().Get("salt")); s != "" {
		raw += "?salt=" + s
	}
	items, err := d.fetchDHTFeedItems(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	// Return the info hashes and names directly, not the RSS shape.
	type entry struct {
		InfoHash string `json:"info_hash"`
		Name     string `json:"name,omitempty"`
		Magnet   string `json:"magnet"`
	}
	out := make([]entry, 0, len(items))
	for _, it := range items {
		out = append(out, entry{InfoHash: strings.TrimPrefix(it.GUID, "dht:"), Name: it.Title, Magnet: it.Magnet})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": out})
}
