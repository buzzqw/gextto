package torrent

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/piecewriter"
	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer/boltdbresumer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/urldownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/webseedsource"
	"github.com/rcrowley/go-metrics"
	"go.etcd.io/bbolt"
)

// handleWebseedPieceResult settles one downloaded piece: errors disable the
// source and restart the swarm; stale results for already-Done/ Writing
// pieces are discarded (a concurrent peer copy won the race); fresh pieces
// go to the write ring like peer pieces, and a finished range rotates the
// source to a new one.
func (t *torrent) handleWebseedPieceResult(msg *urldownloader.PieceResult) {
	if msg.Error != nil {
		t.log.Debugln("webseed download error:", msg.Error)
		t.disableSource(msg.Downloader.URL, msg.Error, true)
		t.startPieceDownloaders()
		return
	}
	if t.verifier != nil {
		// A verification or swarm merge is exclusive: the piece's truth is
		// being recomputed, so a webseed result is stale by definition.
		t.discardStaleWebseedPiece(msg)
		return
	}
	if piece := &t.pieces[msg.Index]; piece.Done || piece.Writing {
		t.discardStaleWebseedPiece(msg)
		return
	}
	t.log.Debugf("piece #%d downloaded from %s", msg.Index, msg.Downloader.URL)

	size := int64(len(msg.Buffer.Data))
	t.bytesDownloaded.Inc(size)
	t.downloadSpeed.Mark(size)
	for _, src := range t.webseedSources {
		if src.URL != msg.Downloader.URL {
			continue
		}
		src.DownloadSpeed.Mark(size)
		break
	}

	piece := &t.pieces[msg.Index]
	if piece.Writing {
		t.crash("piece is already writing")
	}
	piece.Writing = true
	t.cancelDuplicateDownloads(piece.Index)
	t.beginPieceWrite()

	pw := piecewriter.New(piece, msg.Downloader, msg.Buffer)
	go pw.Run(t.pieceWriterResultC, t.doneC, t.session.metrics.WritesPerSecond, t.session.metrics.SpeedWrite, t.session.semWrite)

	if msg.Done {
		t.rotateWebseedSource(msg.Downloader)
	}
}

// discardStaleWebseedPiece drops a result for a piece completed elsewhere
// (peer copy, or a write still in flight): re-writing would trip the
// already-have check downstream. A range that ended with it still rotates
// its source, matched by downloader identity — the source may already carry
// another downloader, in which case there is nothing to clean up.
func (t *torrent) discardStaleWebseedPiece(msg *urldownloader.PieceResult) {
	t.log.Debugf("discarding already completed piece #%d from webseed %s", msg.Index, msg.Downloader.URL)
	t.bytesWasted.Inc(int64(len(msg.Buffer.Data)))
	msg.Buffer.Release()
	if !msg.Done {
		return
	}
	for _, src := range t.webseedSources {
		if src.Downloader != msg.Downloader {
			continue
		}
		t.closeWebseedDownloader(src)
		t.webseedActiveDownloads--
		t.startPieceDownloaderForWebseed(src)
		break
	}
}

// rotateWebseedSource closes a finished range and starts that source's next
// one. The range is matched by its downloader, not by the URL: a host can hold
// several copies, and rotating the first one would close a stranger and
// release a slot that is still in use.
func (t *torrent) rotateWebseedSource(d *urldownloader.URLDownloader) {
	for _, src := range t.webseedSources {
		if src.Downloader != d {
			continue
		}
		t.closeWebseedDownloader(src)
		t.webseedActiveDownloads--
		t.startPieceDownloaderForWebseed(src)
		return
	}
}

// disableSource parks every copy of a failing source, optionally scheduling
// the retry in a minute. Each copy still fetching is closed here and its
// slot released, the one that failed included: the downloader is dropped
// without going through the piece-result path.
func (t *torrent) disableSource(srcurl string, err error, retry bool) {
	for _, src := range t.webseedSources {
		if src.URL != srcurl {
			continue
		}
		src.Disabled = true
		src.LastError = err
		if src.Downloading() {
			t.closeWebseedDownloader(src)
			if t.webseedActiveDownloads > 0 {
				t.webseedActiveDownloads--
			}
		}
		if retry {
			go t.notifyWebseedRetry(src)
		}
	}
}

// notifyWebseedRetry re-offers a disabled source after a minute, unless the
// torrent closed meanwhile.
func (t *torrent) notifyWebseedRetry(src *webseedsource.WebseedSource) {
	select {
	case <-time.After(time.Minute):
		select {
		case t.webseedRetryC <- src:
		case <-t.closeC:
		}
	case <-t.closeC:
	}
}

// capWebseedSources applies the configured ceiling to a list of webseed
// sources. A ceiling of zero or less means "no ceiling", the same rule
// handleAddWebseeds follows, so both entry points agree. The list is truncated
// in place: the caller owns it and the dropped tail has no other owner.
func capWebseedSources(sources []*webseedsource.WebseedSource, maxSources int) []*webseedsource.WebseedSource {
	if maxSources <= 0 || len(sources) <= maxSources {
		return sources
	}
	return sources[:maxSources]
}

// expandWebseedSources opens every source into up to `parallel` copies, one
// per range the host can serve at once. The copies are interleaved (first
// URL of each source, then their replicas) so a single pass over the list
// spends the shared download budget fairly between the hosts, and every copy
// after the first of its URL is marked Replica so the user still sees one
// entry per URL.
func expandWebseedSources(sources []*webseedsource.WebseedSource, parallel int) []*webseedsource.WebseedSource {
	if parallel <= 1 || len(sources) == 0 {
		return sources
	}
	out := make([]*webseedsource.WebseedSource, 0, len(sources)*parallel)
	for copyIndex := 0; copyIndex < parallel; copyIndex++ {
		for _, src := range sources {
			if copyIndex == 0 {
				out = append(out, src)
				continue
			}
			out = append(out, &webseedsource.WebseedSource{
				URL:           src.URL,
				DownloadSpeed: metrics.NilMeter{},
				Replica:       true,
			})
		}
	}
	return out
}

// newWebseedSources builds the sources of one URL: the primary and the
// replicas that let one host serve several ranges at once.
func newWebseedSources(url string, parallel int) []*webseedsource.WebseedSource {
	if parallel < 1 {
		parallel = 1
	}
	out := make([]*webseedsource.WebseedSource, 0, parallel)
	out = append(out, &webseedsource.WebseedSource{URL: url, DownloadSpeed: metrics.NilMeter{}})
	for i := 1; i < parallel; i++ {
		out = append(out, &webseedsource.WebseedSource{URL: url, DownloadSpeed: metrics.NilMeter{}, Replica: true})
	}
	return out
}

// handleAddWebseeds appends webseed URLs at runtime, skipping blanks,
// duplicates and the configured ceiling, persisting the survivors. Sources
// added while the picker is missing wait for the next start.
func (t *torrent) handleAddWebseeds(urls []string) {
	existing := make(map[string]struct{}, len(t.webseedSources)+len(urls))
	for _, src := range t.webseedSources {
		existing[src.URL] = struct{}{}
	}
	maxSources := t.session.config.WebseedMaxSources
	for _, url := range urls {
		url = strings.TrimSpace(url)
		if url == "" {
			continue
		}
		if _, dup := existing[url]; dup {
			continue
		}
		if maxSources > 0 && len(t.webseedSources) >= maxSources {
			break
		}
		sources := newWebseedSources(url, t.session.config.WebseedMaxDownloads)
		existing[url] = struct{}{}
		t.rawWebseedSources = append(t.rawWebseedSources, url)
		for _, src := range sources {
			t.webseedSources = append(t.webseedSources, src)
			if t.piecePicker != nil {
				t.piecePicker.AddWebseedSource(src)
				t.startPieceDownloaderForWebseed(src)
			}
		}
	}
	t.persistWebseeds()
}

// handleRemoveWebseeds drops webseed URLs by exact match at runtime. A
// downloading source's active slot is released here — its teardown never
// passes through the piece-result path.
func (t *torrent) handleRemoveWebseeds(urls []string) {
	remove := make(map[string]struct{}, len(urls))
	for _, url := range urls {
		if url = strings.TrimSpace(url); url != "" {
			remove[url] = struct{}{}
		}
	}
	if len(remove) == 0 {
		return
	}
	kept := make([]*webseedsource.WebseedSource, 0, len(t.webseedSources))
	raw := make([]string, 0, len(t.rawWebseedSources))
	for _, src := range t.webseedSources {
		if _, drop := remove[src.URL]; drop {
			wasDownloading := src.Downloading()
			if t.piecePicker != nil {
				t.closeWebseedDownloader(src)
				t.piecePicker.RemoveWebseedSource(src)
			}
			if wasDownloading && t.webseedActiveDownloads > 0 {
				t.webseedActiveDownloads--
			}
			continue
		}
		kept = append(kept, src)
		raw = append(raw, src.URL)
	}
	t.webseedSources = kept
	t.rawWebseedSources = raw
	t.persistWebseeds()
	t.startPieceDownloaders()
}

// persistWebseeds stores the URL list in resume, so runtime additions and
// removals survive a reload. An empty list deletes the key.
func (t *torrent) persistWebseeds() {
	raw := t.rawWebseedSources
	_ = t.session.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(torrentsBucket).Bucket([]byte(t.id))
		if b == nil {
			return nil
		}
		if len(raw) == 0 {
			return b.Delete(boltdbresumer.Keys.URLList)
		}
		value, err := json.Marshal(raw)
		if err != nil {
			return err
		}
		return b.Put(boltdbresumer.Keys.URLList, value)
	})
}
