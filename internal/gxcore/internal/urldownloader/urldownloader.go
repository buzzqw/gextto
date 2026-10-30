package urldownloader

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bandwidth"
	"github.com/buzzqw/gextto/internal/gxcore/internal/bufferpool"
	"github.com/buzzqw/gextto/internal/gxcore/internal/ctxutil"
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// URLDownloader fetches a piece range over HTTP (BEP 19/24 webseeds),
// emitting one PieceResult per completed piece. The range end is atomic so
// the picker can truncate a running download.
type URLDownloader struct {
	URL                 string
	Begin, End, current uint32             // piece index
	bucket              *bandwidth.Limiter // rate changes at runtime
	closeC, doneC       chan struct{}
}

// PieceResult carries one downloaded piece (or its failure) to the torrent.
// Done marks the range end.
type PieceResult struct {
	Downloader *URLDownloader
	Buffer     bufferpool.Buffer
	Index      uint32
	Error      error
	Done       bool
}

// New plans a fetch of [begin, end) from source, throttled by b.
func New(source string, begin, end uint32, b *bandwidth.Limiter) *URLDownloader {
	return &URLDownloader{
		URL:     source,
		Begin:   begin,
		current: begin,
		End:     end,
		bucket:  b,
		closeC:  make(chan struct{}),
		doneC:   make(chan struct{}),
	}
}

// Close aborts the download and waits for the run to unwind.
func (d *URLDownloader) Close() {
	close(d.closeC)
	<-d.doneC
}

// String names the source URL.
func (d *URLDownloader) String() string {
	return d.URL
}

// UpdateEnd truncates (or extends) the range while running.
func (d *URLDownloader) UpdateEnd(value uint32) {
	atomic.StoreUint32(&d.End, value)
}

func (d *URLDownloader) readEnd() uint32 {
	return atomic.LoadUint32(&d.End)
}

func (d *URLDownloader) incrCurrent() uint32 {
	return atomic.AddUint32(&d.current, 1)
}

// ReadCurrent is the piece currently being fetched.
func (d *URLDownloader) ReadCurrent() uint32 {
	return atomic.LoadUint32(&d.current)
}

// Run downloads the planned jobs in order: HTTP range fetches for data jobs,
// buffer skips for padding jobs. A failing job reports its error and ends
// the run, releasing the current buffer.
func (d *URLDownloader) Run(client *http.Client, pieces []piece.Piece, multifile bool, resultC chan *PieceResult, pool *bufferpool.Pool, readTimeout time.Duration) {
	defer close(d.doneC)
	ctx, cancel := ctxutil.FromChan(d.closeC)
	defer cancel()

	jobs := createJobs(pieces, d.Begin, d.readEnd())

	var n int // filled bytes in buf
	buf := pool.Get(int(pieces[d.current].Length))

	// completePiece ships the filled buffer and opens the next piece.
	// True means the range end was reached.
	completePiece := func() (done bool) {
		index := d.current
		done = d.current >= d.readEnd()-1
		d.sendResult(resultC, &PieceResult{Downloader: d, Buffer: buf, Index: index, Done: done})
		if done {
			return true
		}
		d.incrCurrent()
		n = 0
		buf = pool.Get(int(pieces[d.current].Length))
		return false
	}

	// processPadding skips padding spans: no request (padding is absent
	// server-side), the zeroed pool buffer already holds the right bytes.
	processPadding := func(job downloadJob) bool {
		var m int64 // consumed padding bytes
		for m < job.Length {
			skip := calcReadSize(buf, n, job, m)
			n += int(skip)
			m += skip
			if n == len(buf.Data) && completePiece() {
				return true
			}
		}
		return true
	}

	processJob := func(job downloadJob) bool {
		u := d.getURL(job.Filename, multifile)
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			d.sendResult(resultC, &PieceResult{Downloader: d, Error: err})
			return false
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", job.RangeBegin, job.RangeBegin+job.Length-1))
		req = req.WithContext(ctx)
		resp, err := client.Do(req)
		if err != nil {
			d.sendResult(resultC, &PieceResult{Downloader: d, Error: err})
			return false
		}
		defer resp.Body.Close()
		if err := checkStatus(resp, job.RangeBegin); err != nil {
			d.sendResult(resultC, &PieceResult{Downloader: d, Error: err})
			return false
		}
		timer := time.AfterFunc(readTimeout, cancel)
		defer timer.Stop()
		var m int64 // consumed response bytes
		for m < job.Length {
			readSize := calcReadSize(buf, n, job, m)
			if wait := d.bucket.Take(readSize); wait > 0 {
				select {
				case <-time.After(wait):
				case <-d.closeC:
					return false
				}
			}
			o, err := readFull(resp.Body, buf.Data[n:int64(n)+readSize], timer, readTimeout)
			if err != nil {
				d.sendResult(resultC, &PieceResult{Downloader: d, Error: err})
				return false
			}
			n += o
			m += int64(o)
			if n == len(buf.Data) && completePiece() {
				return true
			}
		}
		return true
	}

	for _, job := range jobs {
		var ok bool
		if job.Padding {
			ok = processPadding(job)
		} else {
			ok = processJob(job)
		}
		if !ok {
			buf.Release()
			break
		}
	}
}

// calcReadSize bounds one read by the piece tail and the job tail alike.
func calcReadSize(buf bufferpool.Buffer, bufPos int, job downloadJob, jobPos int64) int64 {
	toPieceEnd := int64(len(buf.Data) - bufPos)
	toJobEnd := job.Length - jobPos
	if toPieceEnd < toJobEnd {
		return toPieceEnd
	}
	return toJobEnd
}

// readFull fills b like io.ReadFull, restarting the stall timer after every
// read that moved bytes, so a slow-but-moving server does not trip the timeout
// and a server that answers nothing cannot reset it forever.
func readFull(r io.Reader, b []byte, t *time.Timer, d time.Duration) (o int, err error) {
	for o < len(b) && err == nil {
		var nn int
		nn, err = r.Read(b[o:])
		o += nn
		if nn > 0 {
			t.Reset(d)
		}
	}
	if o >= len(b) {
		err = nil
	}
	return
}

// getURL addresses one file on the source: bare base URL for single-file
// torrents (plus the file when the base is a directory), base plus path for
// multi-file ones.
func (d *URLDownloader) getURL(filename string, multifile bool) string {
	src := d.URL
	if !multifile {
		if src[len(src)-1] == '/' {
			src += escapeFilePath(filename)
		}
		return src
	}
	if src[len(src)-1] != '/' {
		src += "/"
	}
	return src + escapeFilePath(filename)
}

// escapeFilePath escapes each path segment separately, keeping slashes
// literal as BEP 19 requires: a whole-path escape would turn them into %2F,
// which some servers reject (RFC 3986 treats the two differently). Paths
// arrive OS-joined, hence the OS separator split.
func escapeFilePath(filename string) string {
	parts := strings.Split(filename, string(filepath.Separator))
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

// sendResult delivers res unless the downloader closed meanwhile.
func (d *URLDownloader) sendResult(resultC chan *PieceResult, res *PieceResult) {
	select {
	case <-d.closeC:
		return
	default:
	}
	select {
	case resultC <- res:
	case <-d.closeC:
	}
}

// checkStatus accepts full and partial content. A server that ignores the
// Range header answers 200 with the file from byte zero: that is only valid
// for a job starting at the beginning of the file, otherwise the bytes belong
// to another offset and the piece would be corrupt (and blamed on the swarm).
func checkStatus(resp *http.Response, rangeBegin int64) error {
	switch resp.StatusCode {
	case http.StatusPartialContent:
		return nil
	case http.StatusOK:
		if rangeBegin == 0 {
			return nil
		}
		return fmt.Errorf("server ignored the range request (offset %d)", rangeBegin)
	default:
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}
