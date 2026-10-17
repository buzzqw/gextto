package torrent

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/backoff"
	"github.com/buzzqw/gextto/internal/gxcore/internal/blocklist"
	"github.com/buzzqw/gextto/internal/gxcore/internal/ctxutil"
	"github.com/buzzqw/gextto/internal/gxcore/internal/netx"
	"go.etcd.io/bbolt"
)

// startBlocklistReloader primes the IP filter at startup — from the session
// db when fresh, from the remote URL otherwise — and arms the periodic
// reloader. Empty URL disables the whole machinery.
func (s *Session) startBlocklistReloader() error {
	if s.config.BlocklistURL == "" {
		return nil
	}
	storedAt, err := s.getBlocklistTimestamp()
	if err != nil {
		return err
	}
	s.mBlocklist.Lock()
	s.blocklistTimestamp = storedAt
	s.mBlocklist.Unlock()

	deadline := storedAt.Add(s.config.BlocklistUpdateInterval)
	now := time.Now()
	var nextReload time.Duration
	switch {
	case storedAt.IsZero():
		s.log.Infof("Blocklist is empty. Loading blocklist...")
		s.retryReloadBlocklist()
		nextReload = s.config.BlocklistUpdateInterval
	case deadline.Before(now):
		s.log.Infof("Last blocklist reload was %s ago. Reloading blocklist...", now.Sub(s.blocklistTimestamp).Truncate(time.Second).String())
		s.retryReloadBlocklist()
		nextReload = s.config.BlocklistUpdateInterval
	default:
		s.log.Infof("Loading blocklist from session db...")
		if err := s.loadBlocklistFromDB(); err != nil {
			s.log.Errorln("Couldn't load blocklist from session db:", err)
			s.log.Infof("Loading blocklist from remote URL...")
			s.retryReloadBlocklist()
			nextReload = s.config.BlocklistUpdateInterval
		} else {
			nextReload = deadline.Sub(now)
		}
	}
	go s.blocklistReloader(nextReload)
	return nil
}

// getBlocklistTimestamp reads the stored filter time, but only when the
// stored URL hash still matches the configured URL — a changed URL
// invalidates the cache.
func (s *Session) getBlocklistTimestamp() (time.Time, error) {
	sum := sha1.Sum([]byte(s.config.BlocklistURL))
	var t time.Time
	err := s.db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(sessionBucket)
		if val := b.Get(blocklistURLHashKey); val == nil || !bytes.Equal(val, sum[:]) {
			return nil
		}
		val := b.Get(blocklistTimestampKey)
		if val == nil {
			return nil
		}
		var err error
		t, err = time.Parse(time.RFC3339, string(val))
		return err
	})
	return t, err
}

// retryReloadBlocklist refetches until success, logging each failure. The
// zero max elapsed time means unbounded retries while the session lives.
func (s *Session) retryReloadBlocklist() {
	ctx, cancel := ctxutil.FromChan(s.closeC)
	defer cancel()
	_, _ = backoff.Retry(ctx, func() (result struct{}, err error) {
		if err = s.reloadBlocklist(ctx); err != nil {
			s.log.Errorln("cannot load blocklist:", err.Error())
		}
		return struct{}{}, err
	}, backoff.WithMaxElapsedTime(0))
}

// reloadBlocklist downloads, validates, gunzips, installs and persists one
// filter snapshot. Responses must declare their length up front and fit the
// configured ceiling — an unbounded body is never consumed.
func (s *Session) reloadBlocklist(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.config.BlocklistURL, nil)
	if err != nil {
		return err
	}
	client := http.Client{
		Timeout:   s.config.BlocklistUpdateTimeout,
		Transport: netx.HTTPTransport(),
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	s.log.Infoln("Blocklist response status code:", resp.StatusCode)
	s.log.Infoln("Blocklist response content length:", resp.ContentLength)
	s.log.Infoln("Blocklist response content type:", resp.Header.Get("content-type"))

	if resp.StatusCode != 200 {
		return errors.New("invalid blocklist status code")
	}
	if resp.ContentLength == -1 {
		return errors.New("unknown content length")
	}
	if resp.ContentLength > s.config.BlocklistMaxResponseSize {
		return errors.New("response too big")
	}
	buf := make([]byte, resp.ContentLength)
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		return err
	}
	resp.Body.Close()

	if resp.Header.Get("content-type") == "application/x-gzip" {
		gr, err := gzip.NewReader(bytes.NewReader(buf))
		if err != nil {
			return err
		}
		defer gr.Close()
		if buf, err = io.ReadAll(gr); err != nil {
			return err
		}
	}
	if err := s.loadBlocklistReader(bytes.NewReader(buf)); err != nil {
		return err
	}
	now := time.Now()
	s.mBlocklist.Lock()
	s.blocklistTimestamp = now
	s.mBlocklist.Unlock()

	return s.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(sessionBucket)
		if err := b.Put(blocklistKey, buf); err != nil {
			return err
		}
		sum := sha1.Sum([]byte(s.config.BlocklistURL))
		if err := b.Put(blocklistURLHashKey, sum[:]); err != nil {
			return err
		}
		return b.Put(blocklistTimestampKey, []byte(now.Format(time.RFC3339)))
	})
}

// loadBlocklistFromDB installs the persisted snapshot, failing when empty.
func (s *Session) loadBlocklistFromDB() error {
	return s.db.View(func(tx *bbolt.Tx) error {
		val := tx.Bucket(sessionBucket).Get(blocklistKey)
		if len(val) == 0 {
			return errors.New("no blocklist data in db")
		}
		return s.loadBlocklistReader(bytes.NewReader(val))
	})
}

// loadBlocklistReader installs rules from r, reporting the count.
func (s *Session) loadBlocklistReader(r io.Reader) error {
	n, err := s.blocklist.Reload(r)
	if err != nil {
		return err
	}
	s.log.Infof("Loaded %d rules from blocklist.", n)
	return nil
}

// blocklistReloader refetches every interval until the session closes.
func (s *Session) blocklistReloader(d time.Duration) {
	for {
		select {
		case <-time.After(d):
		case <-s.closeC:
			return
		}
		s.log.Info("Reloading blocklist...")
		s.retryReloadBlocklist()
		d = s.config.BlocklistUpdateInterval
	}
}

// LoadBlocklist swaps the IP filter with rules read from r (local ipfilter
// files), returning the rule count.
func (s *Session) LoadBlocklist(r io.Reader) (int, error) {
	return s.blocklist.Reload(r)
}

// BlocklistRules is a parsed IP filter rule set (see BlocklistRules and
// SetBlocklistRules).
type BlocklistRules = blocklist.Rules

// BlocklistRules returns the parsed IP filter rules currently loaded.
func (s *Session) BlocklistRules() BlocklistRules {
	return s.blocklist.Rules()
}

// SetBlocklistRules installs rules parsed by an earlier session, so a
// session reopen does not read and parse the filter file again. It returns
// the rule count.
func (s *Session) SetBlocklistRules(r BlocklistRules) int {
	return s.blocklist.SetRules(r)
}
