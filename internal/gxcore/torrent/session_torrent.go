package torrent

import (
	"archive/tar"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer/boltdbresumer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
	"go.etcd.io/bbolt"
)

// Torrent is the public handle of one session torrent. It forwards to the
// run-loop-owned state; the query/command helpers in torrent_commands.go
// carry the calls across.
type Torrent struct {
	torrent *torrent
}

// InfoHash is the 20-byte torrent identity, hex-rendered as 40 chars.
type InfoHash [20]byte

// String hex-encodes the info hash.
func (h InfoHash) String() string {
	return hex.EncodeToString(h[:])
}

// ---------------------------------------------------------------------------
// Identity and views
// ---------------------------------------------------------------------------

// ID is the session-unique torrent id.
func (t *Torrent) ID() string {
	return t.torrent.id
}

// Name is the torrent name (the initial one for magnets; Stats carries the
// info-dict name once known).
func (t *Torrent) Name() string {
	return t.torrent.Name()
}

// Collections returns the BEP 38 collection hints for local-data reuse;
// empty without hints or metadata.
func (t *Torrent) Collections() []string {
	return t.torrent.Collections()
}

// SimilarTorrents returns the BEP 38 similar info hashes for local-data reuse.
func (t *Torrent) SimilarTorrents() [][20]byte {
	return t.torrent.Similar()
}

// Dir holds the torrent's files.
func (t *Torrent) Dir() string {
	return t.torrent.Dir()
}

// Files lists the files; it fails before metadata arrives.
func (t *Torrent) Files() ([]File, error) {
	return t.torrent.Files()
}

// FileStats reports per-file completion; it fails while stopped.
func (t *Torrent) FileStats() ([]FileStats, error) {
	return t.torrent.FileStats()
}

// InfoHash identifies the torrent's files (shared across duplicate adds).
func (t *Torrent) InfoHash() InfoHash {
	var ih InfoHash
	copy(ih[:], t.torrent.InfoHash())
	return ih
}

// AddedAt is the add time.
func (t *Torrent) AddedAt() time.Time {
	return t.torrent.addedAt
}

// Stats snapshots the torrent.
func (t *Torrent) Stats() Stats {
	return t.torrent.Stats()
}

// Magnet exports the link; private torrents refuse.
func (t *Torrent) Magnet() (string, error) {
	return t.torrent.Magnet()
}

// Torrent exports the .torrent bytes; it fails before metadata arrives.
func (t *Torrent) Torrent() ([]byte, error) {
	return t.torrent.Torrent()
}

// Trackers lists the torrent's trackers.
func (t *Torrent) Trackers() []Tracker {
	return t.torrent.Trackers()
}

// Peers lists the handshaked peers.
func (t *Torrent) Peers() []Peer {
	return t.torrent.Peers()
}

// Webseeds lists the HTTP sources.
func (t *Torrent) Webseeds() []Webseed {
	return t.torrent.Webseeds()
}

// Port is the listening peer port.
func (t *Torrent) Port() int {
	return t.torrent.port
}

// NotifyClose fires on RemoveTorrent.
func (t *Torrent) NotifyClose() <-chan struct{} {
	return t.torrent.NotifyClose()
}

// NotifyStop carries the stop error; call after Start.
func (t *Torrent) NotifyStop() <-chan error {
	return t.torrent.NotifyError()
}

// NotifyComplete fires on download completion; call after Start.
func (t *Torrent) NotifyComplete() <-chan struct{} {
	return t.torrent.NotifyComplete()
}

// NotifyMetadata fires on magnet metadata arrival; call after Start.
func (t *Torrent) NotifyMetadata() <-chan struct{} {
	return t.torrent.NotifyMetadata()
}

// ---------------------------------------------------------------------------
// Swarm membership
// ---------------------------------------------------------------------------

// AddPeer queues a peer; silent while stopped.
func (t *Torrent) AddPeer(addr string) error {
	return t.torrent.addPeerString(addr)
}

// AddTracker appends a tracker and persists it in resume.
func (t *Torrent) AddTracker(uri string) error {
	var private bool
	if t.torrent.info() != nil {
		private = t.torrent.info().Private
	}
	tr, err := t.torrent.session.trackerManager.Get(uri, t.torrent.session.config.TrackerHTTPTimeout, t.torrent.session.getTrackerUserAgent(private), int64(t.torrent.session.config.TrackerHTTPMaxResponseSize))
	if err != nil {
		return err
	}
	err = t.torrent.session.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(t.torrent.id))
		value := b.Get(boltdbresumer.Keys.Trackers)
		var trackers [][]string
		if err := json.Unmarshal(value, &trackers); err != nil {
			return err
		}
		trackers = append(trackers, []string{uri})
		value, err = json.Marshal(trackers)
		if err != nil {
			return err
		}
		return b.Put(boltdbresumer.Keys.Trackers, value)
	})
	if err != nil {
		return err
	}
	t.torrent.AddTrackers([]tracker.Tracker{tr})
	return nil
}

// SetTrackers hot-swaps the tracker list (empty removes all, libtorrent
// style): connected peers stay, departed trackers get a best-effort stopped
// announce, and resume follows the new list.
func (t *Torrent) SetTrackers(uris []string) error {
	private := t.torrent.info() != nil && t.torrent.info().Private
	var (
		resolved []tracker.Tracker
		raw      [][]string
		seen     = make(map[string]struct{}, len(uris))
	)
	for _, uri := range uris {
		uri = strings.TrimSpace(uri)
		if uri == "" {
			continue
		}
		if _, dup := seen[uri]; dup {
			continue
		}
		seen[uri] = struct{}{}
		tr, err := t.torrent.session.trackerManager.Get(uri, t.torrent.session.config.TrackerHTTPTimeout, t.torrent.session.getTrackerUserAgent(private), int64(t.torrent.session.config.TrackerHTTPMaxResponseSize))
		if err != nil {
			return err
		}
		resolved = append(resolved, tr)
		raw = append(raw, []string{uri})
	}
	err := t.torrent.session.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(t.torrent.id))
		if b == nil {
			return nil
		}
		if len(raw) == 0 {
			return b.Delete(boltdbresumer.Keys.Trackers)
		}
		value, err := json.Marshal(raw)
		if err != nil {
			return err
		}
		return b.Put(boltdbresumer.Keys.Trackers, value)
	})
	if err != nil {
		return err
	}
	t.torrent.sendCommand(func() { t.torrent.handleSetTrackers(resolved, raw) })
	return nil
}

// AddWebseeds appends webseed URLs live; duplicates and the ceiling are
// ignored.
func (t *Torrent) AddWebseeds(urls []string) error {
	t.torrent.sendCommand(func() { t.torrent.handleAddWebseeds(urls) })
	return nil
}

// RemoveWebseeds drops webseed URLs by exact match, live.
func (t *Torrent) RemoveWebseeds(urls []string) error {
	t.torrent.sendCommand(func() { t.torrent.handleRemoveWebseeds(urls) })
	return nil
}

// ---------------------------------------------------------------------------
// Runtime tuning
// ---------------------------------------------------------------------------

// SetSequential toggles sequential download on the running torrent.
func (t *Torrent) SetSequential(sequential bool) error {
	t.torrent.sendCommand(func() { t.torrent.setOrder(sequential, t.torrent.firstLast) })
	return nil
}

// SetFirstLast toggles the file-ends-first priority on the running torrent.
func (t *Torrent) SetFirstLast(firstLast bool) error {
	t.torrent.sendCommand(func() { t.torrent.setOrder(t.torrent.sequential, firstLast) })
	return nil
}

// Sequential reports the sequential flag.
func (t *Torrent) Sequential() bool {
	return query(t.torrent, func() bool { return t.torrent.sequential })
}

// SetSuperSeeding toggles BEP 16 super-seeding live; it bites once complete.
func (t *Torrent) SetSuperSeeding(superSeeding bool) error {
	t.torrent.sendCommand(func() { t.torrent.setSuperSeeding(superSeeding) })
	return nil
}

// SuperSeeding reports the super-seeding flag.
func (t *Torrent) SuperSeeding() bool {
	return query(t.torrent, func() bool { return t.torrent.superSeeding })
}

// SelectOnly reads the BEP 53 "so" seed (nil wants everything). It copies:
// the caller must not mutate engine state.
func (t *Torrent) SelectOnly() []int {
	wanted := t.torrent.getSelectOnly()
	if len(wanted) == 0 {
		return nil
	}
	return append([]int(nil), wanted...)
}

// ClearSelectOnly drops the "so" seed, restoring Config.FileSelection (or
// want-everything) as authoritative. The daemon calls it after recording
// the seed explicitly.
func (t *Torrent) ClearSelectOnly() {
	t.torrent.setSelectOnly(nil)
}

// RefreshSelection reapplies Config.FilePriorities/FileSelection live,
// without restarting. Only priority levels change here; a different skipped
// set still needs stop/relocate/start.
func (t *Torrent) RefreshSelection() {
	t.torrent.sendCommand(func() { t.torrent.applySkippedPieces() })
}

// PieceStates maps every piece to "have", "downloading", "skipped" or ""
// (missing), in index order. False means no metadata/pieces yet. Past
// completion the picker is gone, so only "downloading" consults it.
func (t *Torrent) PieceStates() ([]string, bool) {
	states := query(t.torrent, func() []string {
		pieces := t.torrent.pieces
		if len(pieces) == 0 {
			return nil
		}
		out := make([]string, len(pieces))
		for i := range pieces {
			switch {
			case pieces[i].Done:
				out[i] = "have"
			case pieces[i].Skip:
				out[i] = "skipped"
			case t.torrent.piecePicker != nil && t.torrent.piecePicker.PieceDownloading(uint32(i)):
				out[i] = "downloading"
			default:
				out[i] = ""
			}
		}
		return out
	})
	return states, states != nil
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// Start downloads (seeding past completion), persisting the started flag.
func (t *Torrent) Start() error {
	if err := t.torrent.session.resumer.WriteStarted(t.torrent.id, true); err != nil {
		return err
	}
	t.torrent.Start()
	return nil
}

// Stop halts without blocking; Stopping announces stopped, then Stopped
// follows (within ~5 s), persisting the cleared flag.
func (t *Torrent) Stop() error {
	if err := t.torrent.session.resumer.WriteStarted(t.torrent.id, false); err != nil {
		return err
	}
	t.torrent.Stop()
	return nil
}

// Announce asks trackers and DHT for peers now (minimum intervals still
// apply).
func (t *Torrent) Announce() {
	t.torrent.Announce()
}

// Verify re-hashes every file from disk: the torrent stops, verifies, and
// stays stopped.
func (t *Torrent) Verify() error {
	err := t.torrent.session.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(t.torrent.id))
		return b.Delete([]byte("bitfield"))
	})
	if err != nil {
		return err
	}
	t.torrent.Verify()
	return nil
}

// ---------------------------------------------------------------------------
// Session transfer
// ---------------------------------------------------------------------------

// Move pushes the torrent to another session's /move-torrent endpoint
// (host:port) and drops the local copy keeping its data.
func (t *Torrent) Move(target string) error {
	t.torrent.Stop()
	spec, err := t.torrent.session.resumer.Read(t.torrent.id)
	if err != nil {
		return err
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go t.prepareBody(pw, mw, spec)

	req, err := http.NewRequest(http.MethodPost, target+"/move-torrent?id="+t.torrent.id, pr) // nolint: noctx
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http error: %d", resp.StatusCode)
	}
	return t.torrent.session.RemoveTorrent(t.torrent.id, true)
}

// prepareBody streams the id, JSON metadata and tarred data parts, failing
// the pipe on the first error.
func (t *Torrent) prepareBody(pw *io.PipeWriter, mw *multipart.Writer, spec *boltdbresumer.Spec) {
	var err error
	defer func() { _ = pw.CloseWithError(err) }()

	iw, err := mw.CreateFormField("id")
	if err != nil {
		t.torrent.log.Errorln("cannot create id form filed:", err)
		return
	}
	if _, err = iw.Write([]byte(t.torrent.id)); err != nil {
		t.torrent.log.Errorln("cannot write id:", err)
		return
	}
	fw, err := mw.CreateFormField("metadata")
	if err != nil {
		t.torrent.log.Errorln("cannot create metadata form filed:", err)
		return
	}
	if err = json.NewEncoder(fw).Encode(spec); err != nil {
		t.torrent.log.Errorln("cannot encode resumer spec:", err)
		return
	}
	dw, err := mw.CreateFormField("data")
	if err != nil {
		t.torrent.log.Errorln("cannot create data form filed:", err)
		return
	}
	tpr, tpw := io.Pipe()
	go t.generateTar(tpw)
	if _, err = io.Copy(dw, tpr); err != nil {
		t.torrent.log.Errorln("error copying pipe:", err)
		return
	}
	if err = mw.Close(); err != nil {
		t.torrent.log.Errorln("cannot close multipart writer:", err)
		return
	}
}

// generateTar streams the torrent's data dir as a tar of regular files
// (0600, paths relative to the root). A vanished root transfers empty.
func (t *Torrent) generateTar(pw *io.PipeWriter) {
	var err error
	defer func() { _ = pw.CloseWithError(err) }()

	provider, ok := t.torrent.session.storage.(*fileStorageProvider)
	if !ok {
		err = errors.New("session is not using file storage")
		return
	}
	root := provider.getDataDir(t.torrent.id)

	tw := tar.NewWriter(pw)
	walkFunc := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		hdr := &tar.Header{
			Name: path[len(root)+1:],
			Mode: 0600,
			Size: info.Size(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.torrent.log.Errorln("cannot write tar header:", err)
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			t.torrent.log.Errorln("cannot open file:", err)
			return err
		}
		_, err = io.Copy(tw, f)
		f.Close()
		if err != nil {
			t.torrent.log.Errorln("cannot copy storage file to tar writer:", err)
			return err
		}
		return nil
	}
	err = filepath.Walk(root, walkFunc)
	if os.IsNotExist(err) {
		err = nil
		return
	}
	if err != nil {
		t.torrent.log.Errorln("error walking files:", err)
		return
	}
	if err = tw.Close(); err != nil {
		t.torrent.log.Errorln("cannot close tar writer:", err)
		return
	}
}
