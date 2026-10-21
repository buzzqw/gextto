package torrent

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/dht"
	"github.com/buzzqw/gextto/internal/gxcore/internal/magnet"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	"github.com/buzzqw/gextto/internal/gxcore/internal/netx"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer/boltdbresumer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/storage"
	"github.com/buzzqw/gextto/internal/gxcore/internal/webseedsource"
	bencode "github.com/jackpal/bencode-go"
)

// AddTorrentOptions tunes one added torrent.
type AddTorrentOptions struct {
	// ID fixes the torrent id; empty mints a random one.
	ID string
	// Stopped skips the automatic start after adding.
	Stopped bool
	// StopAfterDownload stops a finished download.
	StopAfterDownload bool
	// StopAfterMetadata stops a magnet after its info dict arrives.
	StopAfterMetadata bool
	// Sequential pulls both file ends first, then index order: streaming at
	// the cost of slower overall download and a weaker swarm.
	Sequential bool
	// FirstLast pulls both file ends first, then rarest-first: streaming
	// without giving up rarest-first for the rest.
	FirstLast bool
	// SuperSeeding arms BEP 16 super-seeding once complete: one advertised
	// piece at a time so the swarm spreads the data. Seeding strategy only.
	SuperSeeding bool
	// SelectOnly restricts the download to these non-padding file indices
	// (BEP 53 "so"); empty wants everything. A magnet's "so" fills it when
	// empty; Config.FileSelection wins over both.
	SelectOnly []int
	// ContentLayout is qBittorrent's content layout (ContentLayoutOriginal,
	// ContentLayoutSubfolder, ContentLayoutNoSubfolder); empty = Original.
	ContentLayout string
}

// ValidateTorrentFile reports whether data is a .torrent file the session would
// accept, without adding it (v1, hybrid or v2-only metainfo).
func ValidateTorrentFile(data []byte) error {
	_, err := metainfo.New(bytes.NewReader(data))
	return err
}

// AddTorrent adds a torrent from .torrent metainfo, starting it unless told
// otherwise. A nil opt means defaults.
func (s *Session) AddTorrent(r io.Reader, opt *AddTorrentOptions) (*Torrent, error) {
	if opt == nil {
		opt = &AddTorrentOptions{}
	}
	t, err := s.addTorrentStopped(r, opt)
	if err != nil {
		return nil, err
	}
	if !opt.Stopped {
		err = t.Start()
	}
	return t, err
}

// parseMetaInfo decodes metainfo capped at the configured piece ceiling.
func (s *Session) parseMetaInfo(r io.Reader) (*metainfo.MetaInfo, error) {
	mi, err := metainfo.New(r)
	if err != nil {
		return nil, err
	}
	if mi.Info.NumPieces > s.config.MaxPieces {
		return nil, fmt.Errorf("too many pieces: %d", mi.Info.NumPieces)
	}
	return mi, nil
}

// addTorrentStopped stages a .torrent torrent without starting it: id, port
// and storage reservation, torrent construction, resume record. A failure at
// any step unwinds the reservations.
func (s *Session) addTorrentStopped(r io.Reader, opt *AddTorrentOptions) (*Torrent, error) {
	r = io.LimitReader(r, int64(s.config.MaxTorrentSize))
	mi, err := s.parseMetaInfo(r)
	if err != nil {
		return nil, newInputError(err)
	}
	id, port, sto, err := s.add(opt)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			s.releasePort(port)
		}
	}()
	// The content layout overrides the storage paths before anything is
	// allocated.
	layoutPaths := layoutPaths(&mi.Info, opt.ContentLayout)
	if layoutPaths != nil {
		mi.Info.SetFilePaths(layoutPaths)
	}
	t, err := newTorrent(
		s,
		id,
		time.Now(),
		mi.Info.Hash[:],
		sto,
		mi.Info.Name,
		port,
		s.parseTrackers(mi.AnnounceList, mi.Info.Private),
		nil, // fixedPeers
		&mi.Info,
		nil, // bitfield
		resumer.Stats{},
		webseedsource.NewList(mi.URLList),
		opt.StopAfterDownload,
		opt.StopAfterMetadata,
		false, // completeCmdRun
		opt.Sequential,
		opt.FirstLast,
		opt.SuperSeeding,
	)
	if err != nil {
		return nil, err
	}
	go s.checkTorrent(t)
	defer func() {
		if err != nil {
			t.Close()
		}
	}()
	t.pieceLayers = mi.PieceLayers
	t.filePaths = layoutPaths
	t.contentLayout = opt.ContentLayout
	t.setSelectOnly(opt.SelectOnly)
	layers, _ := metainfo.EncodePieceLayers(mi.PieceLayers)
	rspec := &boltdbresumer.Spec{
		InfoHash:          mi.Info.Hash[:],
		Port:              port,
		Name:              mi.Info.Name,
		Trackers:          mi.AnnounceList,
		URLList:           mi.URLList,
		Info:              mi.Info.Bytes,
		PieceLayers:       layers,
		AddedAt:           t.addedAt,
		StopAfterDownload: opt.StopAfterDownload,
		StopAfterMetadata: opt.StopAfterMetadata,
		Sequential:        opt.Sequential,
		FirstLast:         opt.FirstLast,
		SuperSeeding:      opt.SuperSeeding,
		FilePaths:         layoutPaths,
		ContentLayout:     opt.ContentLayout,
	}
	if err = s.resumer.Write(id, rspec); err != nil {
		return nil, err
	}
	t2 := s.insertTorrent(t)
	t2.torrent.emit(EventTorrentAdded, nil)
	return t2, nil
}

// AddURI adds a torrent from a magnet link or an HTTP(S) .torrent URL. A nil
// opt means defaults.
func (s *Session) AddURI(uri string, opt *AddTorrentOptions) (*Torrent, error) {
	uri = filterOutControlChars(uri)
	if opt == nil {
		opt = &AddTorrentOptions{}
	}
	u, err := url.Parse(uri)
	if err != nil {
		return nil, newInputError(err)
	}
	switch u.Scheme {
	case "http", "https":
		return s.addURL(uri, opt)
	case "magnet":
		return s.addMagnet(uri, opt)
	default:
		return nil, errors.New("unsupported uri scheme: " + u.Scheme)
	}
}

// filterOutControlChars strips ASCII control characters (and DEL) from a
// user-supplied URI before parsing.
func filterOutControlChars(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if b := s[i]; b >= ' ' && b != 0x7f {
			sb.WriteByte(b)
		}
	}
	return sb.String()
}

// addURL fetches a .torrent over HTTP(S), size-capped, and adds it.
func (s *Session) addURL(u string, opt *AddTorrentOptions) (*Torrent, error) {
	client := http.Client{
		Timeout:   s.config.TorrentAddHTTPTimeout,
		Transport: netx.HTTPTransport(),
		Jar:       s.config.CookieJar,
	}
	resp, err := client.Get(u) // nolint: noctx
	if err != nil {
		return nil, newInputError(err)
	}
	defer resp.Body.Close()

	if resp.ContentLength > int64(s.config.MaxTorrentSize) {
		return nil, newInputError(fmt.Errorf("torrent too large: %d", resp.ContentLength))
	}
	return s.AddTorrent(io.LimitReader(resp.Body, int64(s.config.MaxTorrentSize)), opt)
}

// addMagnet stages a metadata-less torrent: DHT bootstrap nodes first, then
// a direct .torrent fetch from xs/as sources, then BEP 46 key resolution,
// and finally the swarm metadata download.
func (s *Session) addMagnet(link string, opt *AddTorrentOptions) (*Torrent, error) {
	ma, err := magnet.New(link)
	if err != nil {
		return nil, newInputError(err)
	}
	for _, node := range ma.DHTNodes {
		if s.dht != nil {
			s.dht.AddNode(node)
		}
	}
	if t, err := s.addMagnetFromSources(ma, opt); err == nil {
		return t, nil
	}
	if ma.HasBTPK() {
		if err := s.resolveBTPK(ma); err != nil {
			return nil, err
		}
	}
	id, port, sto, err := s.add(opt)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			s.releasePort(port)
		}
	}()
	t, err := newTorrent(
		s,
		id,
		time.Now(),
		ma.InfoHash[:],
		sto,
		ma.Name,
		port,
		s.parseTrackers(ma.Trackers, false),
		ma.Peers,
		nil, // info
		nil, // bitfield
		resumer.Stats{},
		webseedsource.NewList(ma.WebSeeds),
		opt.StopAfterDownload,
		opt.StopAfterMetadata,
		false, // completeCmdRun
		opt.Sequential,
		opt.FirstLast,
		opt.SuperSeeding,
	)
	if err != nil {
		return nil, err
	}
	t.hasV2Hint = ma.HasV2
	t.contentLayout = opt.ContentLayout
	// BEP 53: the magnet "so" is the default selection; an explicit option wins.
	wanted := opt.SelectOnly
	if len(wanted) == 0 {
		wanted = ma.SelectOnly
	}
	t.setSelectOnly(wanted)
	go s.checkTorrent(t)
	defer func() {
		if err != nil {
			t.Close()
		}
	}()
	rspec := &boltdbresumer.Spec{
		InfoHash:          ma.InfoHash[:],
		Port:              port,
		Name:              ma.Name,
		Trackers:          ma.Trackers,
		URLList:           ma.WebSeeds,
		FixedPeers:        ma.Peers,
		AddedAt:           t.addedAt,
		StopAfterDownload: opt.StopAfterDownload,
		StopAfterMetadata: opt.StopAfterMetadata,
		Sequential:        opt.Sequential,
		FirstLast:         opt.FirstLast,
		SuperSeeding:      opt.SuperSeeding,
		ContentLayout:     opt.ContentLayout,
	}
	if err = s.resumer.Write(id, rspec); err != nil {
		return nil, err
	}
	t2 := s.insertTorrent(t)
	if !opt.Stopped {
		err = t2.Start()
	}
	return t2, err
}

// resolveBTPK resolves a BEP 46 keyed magnet's info hash from the DHT
// mutable item, requiring an enabled DHT.
func (s *Session) resolveBTPK(ma *magnet.Magnet) error {
	if s.dht == nil {
		return newInputError(errors.New("magnet uses xs=urn:btpk but the DHT is disabled"))
	}
	item, err := s.dht.GetMutable(ed25519.PublicKey(ma.BTPKPublicKey), ma.BTPKSalt, nil, bep46ResolveTimeout)
	if err != nil {
		return newInputError(fmt.Errorf("BEP 46: cannot resolve the DHT item: %w", err))
	}
	ih, err := bep46InfoHash(item.Value)
	if err != nil {
		return newInputError(err)
	}
	ma.InfoHash = ih
	ma.HasV1 = true
	return nil
}

// addMagnetFromSources fetches the .torrent straight from the magnet's
// xs/as URLs, so a sourced link skips the swarm wait. It fails when no
// source yields metainfo matching the magnet's hash, and the caller falls
// back to the peer/DHT metadata download.
func (s *Session) addMagnetFromSources(ma *magnet.Magnet, opt *AddTorrentOptions) (*Torrent, error) {
	sources := ma.MetadataSources()
	if len(sources) == 0 {
		return nil, errors.New("magnet has no HTTP source")
	}
	// xs=/as= URLs may come from untrusted feeds: public addresses only,
	// unless the operator allows private ones (SSRF guard).
	transport := netx.PublicHTTPTransport()
	if s.config.WebSourcesAllowPrivate {
		transport = netx.HTTPTransport()
	}
	client := http.Client{
		Timeout:   s.config.TorrentAddHTTPTimeout,
		Transport: transport,
		Jar:       s.config.CookieJar,
	}
	var lastErr error
	for _, src := range sources {
		data, err := s.fetchTorrent(src, &client)
		if err != nil {
			lastErr = err
			continue
		}
		mi, err := metainfo.New(bytes.NewReader(data))
		if err != nil {
			lastErr = err
			continue
		}
		if !magnetInfoHashMatches(ma, &mi.Info) {
			lastErr = fmt.Errorf("magnet source %q has a different info hash", src)
			continue
		}
		o := *opt
		if len(o.SelectOnly) == 0 {
			o.SelectOnly = ma.SelectOnly
		}
		t, err := s.AddTorrent(bytes.NewReader(data), &o)
		if err != nil {
			lastErr = err
			continue
		}
		// Keep the magnet's extra trackers and fixed peers on top.
		if extra := s.parseTrackers(ma.Trackers, mi.Info.Private); len(extra) > 0 {
			t.torrent.AddTrackers(extra)
		}
		if peers := resolvePeers(ma.Peers); len(peers) > 0 {
			t.torrent.AddPeers(peers)
		}
		return t, nil
	}
	return nil, lastErr
}

// fetchTorrent downloads one magnet-sourced .torrent, status- and
// size-checked.
func (s *Session) fetchTorrent(src string, client *http.Client) ([]byte, error) {
	resp, err := client.Get(src) // nolint: noctx
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("source returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > int64(s.config.MaxTorrentSize) {
		return nil, fmt.Errorf("torrent too large: %d", resp.ContentLength)
	}
	return io.ReadAll(io.LimitReader(resp.Body, int64(s.config.MaxTorrentSize)))
}

// magnetInfoHashMatches checks metainfo against the magnet: v1 hash for a
// btih link, truncated SHA-256 for a btmh link.
func magnetInfoHashMatches(ma *magnet.Magnet, info *metainfo.Info) bool {
	if ma.HasV1 && info.Hash == ma.InfoHash {
		return true
	}
	if ma.HasV2 {
		var v2 [20]byte
		copy(v2[:], info.V2Hash[:20])
		if v2 == ma.InfoHash {
			return true
		}
	}
	return false
}

// resolvePeers turns "x.pe" host:port values into dialable addresses,
// dropping the unresolvable.
func resolvePeers(peers []string) []*net.TCPAddr {
	var out []*net.TCPAddr
	for _, p := range peers {
		if addr, err := net.ResolveTCPAddr("tcp", p); err == nil {
			out = append(out, addr)
		}
	}
	return out
}

// bep46ResolveTimeout bounds the DHT lookup of a BEP 46 magnet.
const bep46ResolveTimeout = 8 * time.Second

// bep46InfoHash extracts the 20-byte binary "ih" from a BEP 46 item value
// (the single-torrent form, or the first item of a feed).
func bep46InfoHash(value interface{}) ([20]byte, error) {
	items, err := bep46Feed(value)
	if err != nil {
		return [20]byte{}, err
	}
	if len(items) == 0 {
		return [20]byte{}, errors.New("BEP 46: the DHT item lists no torrent")
	}
	return items[0].InfoHash, nil
}

// DHTFeedItem is one entry of a BEP 46 feed: a torrent's info hash and an
// optional display name.
type DHTFeedItem struct {
	InfoHash [20]byte
	Name     string
}

// bep46Feed decodes a BEP 46 mutable item value into a list of torrents. It
// accepts both the original single-torrent form ({"ih": <20 bytes>}) and a
// feed ({"items": [{"ih": <20 bytes>, "name": "..."}, ...]}), so a magnet's
// "xs=urn:btpk:" keeps working and the same key can also carry a subscription
// channel. The value is untrusted: an entry without a 20-byte "ih" is skipped.
func bep46Feed(value interface{}) ([]DHTFeedItem, error) {
	m, ok := value.(map[string]interface{})
	if !ok {
		return nil, errors.New("BEP 46: the DHT item value is not a dictionary")
	}
	if ih, ok := m["ih"]; ok {
		item, err := bep46FeedItem(ih, m["name"])
		if err != nil {
			return nil, err
		}
		return []DHTFeedItem{item}, nil
	}
	raw, ok := m["items"].([]interface{})
	if !ok {
		return nil, errors.New("BEP 46: the DHT item has no \"ih\" and no \"items\"")
	}
	items := make([]DHTFeedItem, 0, len(raw))
	for _, e := range raw {
		em, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		item, err := bep46FeedItem(em["ih"], em["name"])
		if err != nil {
			continue // one malformed entry must not drop the whole feed
		}
		items = append(items, item)
	}
	return items, nil
}

// bep46FeedItem validates one feed entry.
func bep46FeedItem(rawIH, rawName interface{}) (DHTFeedItem, error) {
	ih, ok := rawIH.(string)
	if !ok || len(ih) != 20 {
		return DHTFeedItem{}, errors.New("BEP 46: an item has no 20-byte \"ih\"")
	}
	var item DHTFeedItem
	copy(item.InfoHash[:], ih)
	if name, ok := rawName.(string); ok {
		item.Name = name
	}
	return item, nil
}

// ResolveDHTFeed fetches a BEP 46 feed (the mutable item addressed by the given
// ed25519 key and optional salt) from the DHT and returns its torrents. It
// requires an enabled DHT; seq, when non-nil, is our last seen sequence (the
// responder may then answer without the value). The DHT client verifies the
// signature before the value is accepted.
func (s *Session) ResolveDHTFeed(pub ed25519.PublicKey, salt []byte, seq *int64, timeout time.Duration) ([]DHTFeedItem, int64, error) {
	if s.dht == nil {
		return nil, 0, errors.New("the DHT is disabled")
	}
	if len(pub) != ed25519.PublicKeySize {
		return nil, 0, errors.New("BEP 46: invalid ed25519 public key")
	}
	if timeout <= 0 {
		timeout = bep46ResolveTimeout
	}
	item, err := s.dht.GetMutable(pub, salt, seq, timeout)
	if err != nil {
		return nil, 0, fmt.Errorf("BEP 46: cannot resolve the DHT item: %w", err)
	}
	items, err := bep46Feed(item.Value)
	if err != nil {
		return nil, item.Seq, err
	}
	return items, item.Seq, nil
}

// PublishDHTFeed signs and publishes a BEP 46 feed: the value lists the given
// torrents ({"items": [{"ih": ..., "name": ...}]}), the DHT item is addressed
// by the signer's public key and salt. seq must be greater than the last
// published sequence. One BEP 44 item is capped at 1000 bytes, so the tail of
// the list is dropped when needed; kept says how many entries were published.
// It is the publisher side of ResolveDHTFeed.
func (s *Session) PublishDHTFeed(priv ed25519.PrivateKey, seq int64, name string, items []DHTFeedItem, salt []byte, timeout time.Duration) ([20]byte, int, error) {
	if s.dht == nil {
		return [20]byte{}, 0, errors.New("the DHT is disabled")
	}
	if len(priv) != ed25519.PrivateKeySize {
		return [20]byte{}, 0, errors.New("BEP 46: invalid ed25519 private key")
	}
	if len(salt) > 64 {
		return [20]byte{}, 0, errors.New("BEP 46: the salt is longer than 64 bytes")
	}
	if seq < 0 {
		return [20]byte{}, 0, errors.New("BEP 46: the sequence must not be negative")
	}
	if timeout <= 0 {
		timeout = bep46ResolveTimeout
	}
	items = trimDHTFeed(name, items)
	if len(items) == 0 {
		return [20]byte{}, 0, errors.New("BEP 46: the feed does not fit in one BEP 44 item")
	}
	value := bep46FeedValue(name, items)
	target, err := s.dht.PublishMutable(priv, seq, value, salt, timeout)
	if err != nil {
		return target, len(items), fmt.Errorf("BEP 46: cannot publish the DHT feed: %w", err)
	}
	return target, len(items), nil
}

// bep46FeedMaxValue is BEP 44's ceiling for a mutable item. A feed is a
// single item, so a long list is trimmed to the last entry that fits instead
// of failing the whole publish.
const bep46FeedMaxValue = 1000

func bep46FeedValue(name string, items []DHTFeedItem) map[string]interface{} {
	entries := make([]interface{}, 0, len(items))
	for _, it := range items {
		entry := map[string]interface{}{"ih": it.InfoHash[:]}
		if n := strings.TrimSpace(it.Name); n != "" {
			entry["name"] = n
		}
		entries = append(entries, entry)
	}
	value := map[string]interface{}{"items": entries}
	if name = strings.TrimSpace(name); name != "" {
		value["name"] = name
	}
	return value
}

// trimDHTFeed removes entries from the tail until the encoded value fits BEP
// 44. It is deterministic, so the first entries (the order the caller chose)
// win.
func trimDHTFeed(name string, items []DHTFeedItem) []DHTFeedItem {
	for len(items) > 0 {
		var buf bytes.Buffer
		err := bencode.Marshal(&buf, bep46FeedValue(name, items))
		if err != nil || buf.Len() <= bep46FeedMaxValue {
			return items
		}
		items = items[:len(items)-1]
	}
	return nil
}

func (s *Session) add(opt *AddTorrentOptions) (id string, port int, sto storage.Storage, err error) {
	port, err = s.getPort()
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			s.releasePort(port)
		}
	}()
	if opt.ID != "" {
		s.mTorrents.RLock()
		defer s.mTorrents.RUnlock()
		if _, ok := s.torrents[opt.ID]; ok {
			err = errors.New("duplicate torrent id")
			return
		}
		id = opt.ID
	} else {
		var u1 [16]byte
		if u1, err = newTorrentID(); err != nil {
			return
		}
		id = base64.RawURLEncoding.EncodeToString(u1[:])
	}
	sto, err = s.storage.GetStorage(id)
	if err != nil {
		return
	}
	return
}

// insertTorrent registers the torrent by id and info hash, announcing it.
func (s *Session) insertTorrent(t *torrent) *Torrent {
	t.log.Info("added torrent")
	t2 := &Torrent{
		torrent: t,
	}
	s.mTorrents.Lock()
	defer s.mTorrents.Unlock()
	s.torrents[t.id] = t2
	ih := dht.InfoHash(t.InfoHash())
	s.torrentsByInfoHash[ih] = append(s.torrentsByInfoHash[ih], t2)
	return t2
}
