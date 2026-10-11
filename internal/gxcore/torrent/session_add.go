package torrent

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/buzzqw/gextto/internal/gxcore/internal/netx"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/dht"
	"github.com/buzzqw/gextto/internal/gxcore/internal/magnet"
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer/boltdbresumer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/storage"
	"github.com/buzzqw/gextto/internal/gxcore/internal/webseedsource"
	"github.com/gofrs/uuid"
)

// AddTorrentOptions contains options for adding a new torrent.
type AddTorrentOptions struct {
	// ID uniquely identifies the torrent in Session.
	// If empty, a random ID is generated.
	ID string
	// Do not start torrent automatically after adding.
	Stopped bool
	// Stop torrent after all pieces are downloaded.
	StopAfterDownload bool
	// Stop torrent after metadata is downloaded from magnet links.
	StopAfterMetadata bool
	// Download both ends of every file first, then the remaining pieces in index order,
	// instead of rarest-first.
	// Useful for streaming, at the cost of slower overall download and worse swarm health.
	Sequential bool
	// FirstLast downloads the pieces at both ends of every file first, then
	// continues rarest-first. Useful for streaming without
	// giving up the rarest-first ordering for the rest of the torrent.
	FirstLast bool
	// SuperSeeding enables BEP 16 super-seeding: once the
	// torrent is complete it advertises one piece at a time and serves only
	// that, so the swarm spreads the data. Seeding strategy only; it does not
	// affect the queue, the seed policy or the bandwidth limits.
	SuperSeeding bool
	// SelectOnly restricts the download to these non-padding file indices
	// (BEP 53 "so"). Empty means every file is wanted. For a magnet link the
	// "so" parameter is used when this field is empty. An app-provided
	// Config.FileSelection takes precedence over it.
	SelectOnly []int
}

// AddTorrent adds a new torrent to the session by reading .torrent metainfo from reader.
// Nil value can be passed as opt for default options.
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
	}
	err = s.resumer.Write(id, rspec)
	if err != nil {
		return nil, err
	}
	t2 := s.insertTorrent(t)
	t2.torrent.emit(EventTorrentAdded, nil)
	return t2, nil
}

// AddURI adds a new torrent to the session from a URI.
// URI may be a magnet link or a HTTP URL.
// In case of a HTTP address, a torrent is tried to be downloaded from that URL.
// Nil value can be passed as opt for default options.
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

func filterOutControlChars(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b < ' ' || b == 0x7f {
			continue
		}
		sb.WriteByte(b)
	}
	return sb.String()
}

func (s *Session) addURL(u string, opt *AddTorrentOptions) (*Torrent, error) {
	client := http.Client{
		Timeout:   s.config.TorrentAddHTTPTimeout,
		Transport: netx.HTTPTransport(),
	}
	resp, err := client.Get(u) // nolint: noctx
	if err != nil {
		return nil, newInputError(err)
	}
	defer resp.Body.Close()

	if resp.ContentLength > int64(s.config.MaxTorrentSize) {
		return nil, newInputError(fmt.Errorf("torrent too large: %d", resp.ContentLength))
	}
	r := io.LimitReader(resp.Body, int64(s.config.MaxTorrentSize))
	return s.AddTorrent(r, opt)
}

func (s *Session) addMagnet(link string, opt *AddTorrentOptions) (*Torrent, error) {
	ma, err := magnet.New(link)
	if err != nil {
		return nil, newInputError(err)
	}
	// Magnet "dht=" bootstrap nodes feed the session DHT (libtorrent's
	// add_torrent_params.dht_nodes). Harmless when the DHT is disabled.
	for _, node := range ma.DHTNodes {
		if s.dht != nil {
			s.dht.AddNode(node)
		}
	}
	// Magnet "xs"/"as": fetch the .torrent straight from the exact/acceptable
	// source before waiting for a peer or the DHT. A failure is not fatal: the
	// metadata download over the swarm still runs.
	if t, err := s.addMagnetFromSources(ma, opt); err == nil {
		return t, nil
	}
	// BEP 46: "xs=urn:btpk:" names a mutable DHT item whose value carries the
	// info hash; resolve it before creating the (metadata-less) torrent.
	if ma.HasBTPK() {
		if s.dht == nil {
			return nil, newInputError(errors.New("magnet uses xs=urn:btpk but the DHT is disabled"))
		}
		item, err := s.dht.GetMutable(ed25519.PublicKey(ma.BTPKPublicKey), ma.BTPKSalt, nil, bep46ResolveTimeout)
		if err != nil {
			return nil, newInputError(fmt.Errorf("BEP 46: cannot resolve the DHT item: %w", err))
		}
		ih, err := bep46InfoHash(item.Value)
		if err != nil {
			return nil, newInputError(err)
		}
		ma.InfoHash = ih
		ma.HasV1 = true
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
	// BEP 53: the magnet "so" selection is the default; an explicit option wins.
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
	}
	err = s.resumer.Write(id, rspec)
	if err != nil {
		return nil, err
	}
	t2 := s.insertTorrent(t)
	if !opt.Stopped {
		err = t2.Start()
	}
	return t2, err
}

// addMagnetFromSources tries the magnet "xs"/"as" URLs to obtain the .torrent
// directly, so a link that names its source does not have to wait for the
// swarm. It returns an error when no source yields metainfo matching the
// magnet's info hash, so the caller can fall back to the peer/DHT download.
func (s *Session) addMagnetFromSources(ma *magnet.Magnet, opt *AddTorrentOptions) (*Torrent, error) {
	sources := ma.MetadataSources()
	if len(sources) == 0 {
		return nil, errors.New("magnet has no HTTP source")
	}
	client := http.Client{
		Timeout:   s.config.TorrentAddHTTPTimeout,
		Transport: netx.HTTPTransport(),
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
		// Keep the extra trackers and the fixed peers the magnet carried.
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

// fetchTorrent downloads one .torrent from a magnet source URL.
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

// magnetInfoHashMatches reports whether the metainfo's identity is the one the
// magnet names: the v1 info hash for a "urn:btih:" link, or the (truncated)
// SHA-256 for a "urn:btmh:" link.
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

// resolvePeers turns magnet "x.pe" values (host:port) into TCP addresses,
// dropping the ones that do not resolve.
func resolvePeers(peers []string) []*net.TCPAddr {
	var out []*net.TCPAddr
	for _, p := range peers {
		if addr, err := net.ResolveTCPAddr("tcp", p); err == nil {
			out = append(out, addr)
		}
	}
	return out
}

// bep46ResolveTimeout bounds the DHT lookup for a BEP 46 magnet.
const bep46ResolveTimeout = 8 * time.Second

// bep46InfoHash extracts the info hash from a BEP 46 DHT item value, which must
// be a dictionary with a 20-byte binary "ih" (BEP 46).
func bep46InfoHash(value interface{}) ([20]byte, error) {
	m, ok := value.(map[string]interface{})
	if !ok {
		return [20]byte{}, errors.New("BEP 46: the DHT item value is not a dictionary")
	}
	ih, ok := m["ih"].(string)
	if !ok || len(ih) != 20 {
		return [20]byte{}, errors.New("BEP 46: the DHT item has no 20-byte \"ih\"")
	}
	var h [20]byte
	copy(h[:], ih)
	return h, nil
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
	var givenID string
	if opt.ID != "" {
		givenID = opt.ID
	}
	if givenID != "" {
		s.mTorrents.RLock()
		defer s.mTorrents.RUnlock()
		if _, ok := s.torrents[givenID]; ok {
			err = errors.New("duplicate torrent id")
			return
		}
		id = givenID
	} else {
		u1, err2 := uuid.NewV1()
		if err2 != nil {
			err = err2
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
