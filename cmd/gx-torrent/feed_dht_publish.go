package main

// feed_dht_publish.go is the publisher side of the BEP 46 DHT feeds the daemon
// already resolves (feed_dht.go): it keeps an ed25519 key in settings.json,
// signs a mutable item listing the torrents and stores it on the K closest DHT
// nodes, then returns the dht:// URL a client (or Gextto) subscribes to. The
// sequence grows on every publish, as BEP 44 requires.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

const (
	dhtFeedKeySetting = "dht-feed-key" // ed25519 private key, hex
	dhtFeedSeqSetting = "dht-feed-seq" // last published sequence
	dhtFeedPublishMax = 2000           // entries in one feed
	dhtFeedPublishTTL = 25 * time.Second
)

// dhtFeedKey returns the feed signing key, generating and storing one on the
// first publish. Only standalone mode has a settings store: in managed mode
// the acquisition pipelines belong to Gextto and no feed is published.
func (d *Daemon) dhtFeedKey() (ed25519.PrivateKey, ed25519.PublicKey, error) {
	if d.opts.Settings == nil {
		return nil, nil, errors.New("a DHT feed can only be published in standalone mode")
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(dhtFeedKeySetting, ""))
	if raw != "" {
		key, err := hex.DecodeString(raw)
		if err != nil || len(key) != ed25519.PrivateKeySize {
			return nil, nil, errors.New("the stored DHT feed key is invalid")
		}
		priv := ed25519.PrivateKey(key)
		pub, _ := priv.Public().(ed25519.PublicKey)
		return priv, pub, nil
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot generate the DHT feed key: %w", err)
	}
	d.opts.Settings.Set(dhtFeedKeySetting, hex.EncodeToString(priv))
	if err := d.opts.Settings.Save(); err != nil {
		return nil, nil, fmt.Errorf("cannot save the DHT feed key: %w", err)
	}
	return priv, pub, nil
}

// dhtFeedURL is the dht:// address of the feed signed by pub.
func dhtFeedURL(pub ed25519.PublicKey) string {
	return dhtFeedScheme + "://" + hex.EncodeToString(pub)
}

// dhtFeedItemsFromTorrents builds the feed entries: every torrent of the
// session, or only the hashes given (hex).
func dhtFeedItemsFromTorrents(all []*torrent.Torrent, hashes []string) ([]torrent.DHTFeedItem, error) {
	wanted := map[string]bool{}
	for _, h := range hashes {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			continue
		}
		raw, err := hex.DecodeString(h)
		if err != nil || len(raw) != 20 {
			return nil, fmt.Errorf("invalid info hash %q", h)
		}
		wanted[h] = true
	}
	items := make([]torrent.DHTFeedItem, 0, len(all))
	for _, t := range all {
		ih := t.InfoHash()
		if len(wanted) > 0 && !wanted[ih.String()] {
			continue
		}
		items = append(items, torrent.DHTFeedItem{InfoHash: [20]byte(ih), Name: t.Name()})
		if len(items) >= dhtFeedPublishMax {
			break
		}
	}
	if len(items) == 0 {
		return nil, errors.New("no torrent matches the feed")
	}
	return items, nil
}

// publishDHTFeed signs and stores the feed. The sequence is bumped only after
// a successful put, so a failed publish can be retried with the same number.
func (d *Daemon) publishDHTFeed(name string, hashes []string) (string, int64, error) {
	if d.session == nil {
		return "", 0, errors.New("the torrent session is not ready")
	}
	d.dhtFeedMu.Lock()
	defer d.dhtFeedMu.Unlock()
	priv, pub, err := d.dhtFeedKey()
	if err != nil {
		return "", 0, err
	}
	items, err := dhtFeedItemsFromTorrents(d.session.ListTorrents(), hashes)
	if err != nil {
		return "", 0, err
	}
	seq := int64(1)
	if raw := strings.TrimSpace(d.opts.Settings.Get(dhtFeedSeqSetting, "")); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n >= 0 {
			seq = n + 1
		}
	}
	_, kept, err := d.session.PublishDHTFeed(priv, seq, name, items, nil, dhtFeedPublishTTL)
	if err != nil {
		return "", 0, err
	}
	if kept < len(items) {
		logf("DHT feed: %d of %d entries fit in one BEP 44 item; the tail was left out", kept, len(items))
	}
	d.opts.Settings.Set(dhtFeedSeqSetting, strconv.FormatInt(seq, 10))
	if err := d.opts.Settings.Save(); err != nil {
		// The feed is already on the DHT; a lost sequence only means the next
		// publish may reuse this number, so report it in the log, not as a
		// publish failure.
		logfError("DHT feed: cannot save the sequence: %v", err)
	}
	return dhtFeedURL(pub), seq, nil
}

// dhtFeedPublishParams reads the feed name and the optional hash list from a
// JSON body (the API and the page use it) or from a form/query. Empty hashes
// mean every torrent of the session.
func dhtFeedPublishParams(w http.ResponseWriter, r *http.Request) (string, []string, error) {
	var name string
	var hashes []string
	if r.Method == http.MethodPost && strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		var body struct {
			Name   string   `json:"name"`
			Hashes []string `json:"hashes"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			return "", nil, errors.New("invalid JSON body")
		}
		name, hashes = strings.TrimSpace(body.Name), body.Hashes
	} else {
		_ = r.ParseForm()
		name = strings.TrimSpace(r.FormValue("name"))
		hashes = r.Form["hash"]
		if len(hashes) == 0 {
			hashes = strings.Split(strings.TrimSpace(r.FormValue("hashes")), ",")
		}
	}
	if name == "" {
		name = "gx-torrent feed"
	}
	return name, hashes, nil
}

// handlePublishDHTFeed serves GET /api/v1/dht-feed/publish?name=…[&hash=…] and
// POST /api/v1/dht-feed/publish with {"name":"…","hashes":["…"]}.
func (d *Daemon) handlePublishDHTFeed(w http.ResponseWriter, r *http.Request) {
	name, hashes, err := dhtFeedPublishParams(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	url, seq, err := d.publishDHTFeed(name, hashes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": url, "seq": seq})
}

// handleUIPublishDHTFeed publishes the selected torrents (or all of them) from
// the standalone page: POST /ui/dht-feed/publish.
func (d *Daemon) handleUIPublishDHTFeed(w http.ResponseWriter, r *http.Request) {
	name, hashes, err := dhtFeedPublishParams(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	url, _, err := d.publishDHTFeed(name, hashes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "url": url})
}
