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
		t.webseedActiveDownloads--
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
		t.rotateWebseedSource(msg.Downloader.URL)
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

// rotateWebseedSource closes a finished range (matched by URL) and starts
// the source's next one.
func (t *torrent) rotateWebseedSource(url string) {
	for _, src := range t.webseedSources {
		if src.URL != url {
			continue
		}
		t.closeWebseedDownloader(src)
		t.webseedActiveDownloads--
		t.startPieceDownloaderForWebseed(src)
		break
	}
}

// disableSource parks a failing source, optionally scheduling its retry in
// a minute.
func (t *torrent) disableSource(srcurl string, err error, retry bool) {
	for _, src := range t.webseedSources {
		if src.URL != srcurl {
			continue
		}
		src.Disabled = true
		src.LastError = err
		t.closeWebseedDownloader(src)
		if retry {
			go t.notifyWebseedRetry(src)
		}
		break
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
		src := &webseedsource.WebseedSource{URL: url, DownloadSpeed: metrics.NilMeter{}}
		t.webseedSources = append(t.webseedSources, src)
		t.rawWebseedSources = append(t.rawWebseedSources, url)
		existing[url] = struct{}{}
		if t.piecePicker != nil {
			t.piecePicker.AddWebseedSource(src)
			t.startPieceDownloaderForWebseed(src)
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
