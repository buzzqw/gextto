package main

// feed_archive.go is the standalone feed archive: every item any feed returned
// is kept (deduplicated by info-hash, or by link) in a small bbolt database, so
// the Archive page can search what the feeds found long after it scrolled out
// of them, like Gextto's archive. bbolt is already linked for the resume data,
// so the archive adds no dependency; the file lives in the data directory.
//
// Layout, all keyed by firstSeen(8 bytes BE) + id(20 bytes) so a cursor walks
// newest first and pruning walks oldest first:
//   - "items": the JSON entry (decoded only for the rows of a result page);
//   - "idx":   a compact search record (size, seeders, flags, feed and the
//     normalized title), which a search scans without decoding any JSON;
//   - "ids":   id → key, for deduplication.

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
	bolt "go.etcd.io/bbolt"
)

const (
	feedArchiveFile = "feed-archive.db"
	// archiveDaysKey is the retention in days (settings.json); 0 keeps all.
	archiveDaysKey         = "archive-days"
	archiveDefaultDays     = 180
	archiveMaxEntries      = 200000
	archiveLastSeenRefresh = 6 * time.Hour
	archivePageSize        = 50
	archiveKeyLen          = 8 + sha1.Size
)

var (
	archiveItemsBucket = []byte("items")
	archiveIdxBucket   = []byte("idx")
	archiveIDsBucket   = []byte("ids")
)

// archiveEntry is one release a feed returned.
type archiveEntry struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Download  string `json:"download"`
	Size      int64  `json:"size,omitempty"`
	Seeders   int    `json:"seeders"`
	Leechers  int    `json:"leechers"`
	Feed      string `json:"feed"`
	Source    string `json:"source,omitempty"`
	Published int64  `json:"published,omitempty"`
	FirstSeen int64  `json:"first_seen"`
	LastSeen  int64  `json:"last_seen"`
	Added     bool   `json:"added,omitempty"`
}

type feedArchive struct {
	db *bolt.DB
}

// openFeedArchive opens (creating) the archive database and rebuilds the
// search index when it is missing or out of step.
func openFeedArchive(path string) (*feedArchive, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, err
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		items, err := tx.CreateBucketIfNotExists(archiveItemsBucket)
		if err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists(archiveIDsBucket); err != nil {
			return err
		}
		idx, err := tx.CreateBucketIfNotExists(archiveIdxBucket)
		if err != nil {
			return err
		}
		if idx.Stats().KeyN == items.Stats().KeyN {
			return nil
		}
		return items.ForEach(func(k, v []byte) error {
			var entry archiveEntry
			if json.Unmarshal(v, &entry) != nil {
				return nil
			}
			return idx.Put(append([]byte(nil), k...), archiveIdxRecord(entry))
		})
	}); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &feedArchive{db: db}, nil
}

func (a *feedArchive) close() error {
	if a == nil || a.db == nil {
		return nil
	}
	return a.db.Close()
}

// archiveID is the identity of a release: its info-hash when it has a magnet,
// otherwise its download link, otherwise the feed GUID.
func archiveID(feed string, item rss.Item) []byte {
	key := ""
	if hash, ok := magnetInfoHash(item.Magnet); ok {
		key = "btih:" + hash
	} else if item.Link != "" {
		key = "link:" + item.Link
	} else if item.GUID != "" {
		key = "guid:" + feed + "\x00" + item.GUID
	} else {
		return nil
	}
	sum := sha1.Sum([]byte(key))
	return sum[:]
}

func archiveItemsKey(firstSeen int64, id []byte) []byte {
	key := make([]byte, 8+len(id))
	binary.BigEndian.PutUint64(key, uint64(firstSeen))
	copy(key[8:], id)
	return key
}

// The index record: size(8) seeders(4) flags(1) feedLen(2) feed title, where
// title is normalizeArchiveText(entry.Title).
const archiveFlagAdded = 1

func archiveIdxRecord(entry archiveEntry) []byte {
	title := normalizeArchiveText(entry.Title)
	feed := entry.Feed
	if len(feed) > 0xffff {
		feed = feed[:0xffff]
	}
	out := make([]byte, 15, 15+len(feed)+len(title))
	binary.BigEndian.PutUint64(out[0:8], uint64(max(entry.Size, 0)))
	// Seeders come from a feed and can be arbitrarily large: clamp both ends so
	// the uint32 conversion cannot truncate (feed_archive's index is 4 bytes).
	seeders := min(max(entry.Seeders, 0), math.MaxUint32)
	binary.BigEndian.PutUint32(out[8:12], uint32(seeders))
	if entry.Added {
		out[12] = archiveFlagAdded
	}
	binary.BigEndian.PutUint16(out[13:15], uint16(len(feed)))
	out = append(out, feed...)
	return append(out, title...)
}

// archiveIdxView reads an index record without copying it.
type archiveIdxView struct {
	size    int64
	seeders int
	added   bool
	feed    []byte
	title   []byte
}

func parseArchiveIdx(v []byte) (archiveIdxView, bool) {
	if len(v) < 15 {
		return archiveIdxView{}, false
	}
	feedLen := int(binary.BigEndian.Uint16(v[13:15]))
	if len(v) < 15+feedLen {
		return archiveIdxView{}, false
	}
	return archiveIdxView{
		size:    int64(binary.BigEndian.Uint64(v[0:8])),
		seeders: int(binary.BigEndian.Uint32(v[8:12])),
		added:   v[12]&archiveFlagAdded != 0,
		feed:    v[15 : 15+feedLen],
		title:   v[15+feedLen:],
	}, true
}

// putEntry writes an entry and its index record under key.
func putEntry(tx *bolt.Tx, key []byte, entry archiveEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	key = append([]byte(nil), key...)
	if err := tx.Bucket(archiveItemsBucket).Put(key, data); err != nil {
		return err
	}
	return tx.Bucket(archiveIdxBucket).Put(key, archiveIdxRecord(entry))
}

// deleteEntry removes an entry, its index record and its id.
func deleteEntry(tx *bolt.Tx, key []byte) error {
	key = append([]byte(nil), key...)
	if err := tx.Bucket(archiveItemsBucket).Delete(key); err != nil {
		return err
	}
	if err := tx.Bucket(archiveIdxBucket).Delete(key); err != nil {
		return err
	}
	if len(key) == archiveKeyLen {
		return tx.Bucket(archiveIDsBucket).Delete(key[8:])
	}
	return nil
}

// record stores the items of one feed poll: new releases are inserted, known
// ones refresh their swarm numbers (and last-seen, at most every few hours, so
// a poll that brings nothing new does not rewrite the database).
func (a *feedArchive) record(feed string, items []rss.Item, now time.Time) (int, error) {
	if a == nil || len(items) == 0 {
		return 0, nil
	}
	added := 0
	err := a.db.Update(func(tx *bolt.Tx) error {
		entries := tx.Bucket(archiveItemsBucket)
		ids := tx.Bucket(archiveIDsBucket)
		for _, item := range items {
			if strings.TrimSpace(item.Title) == "" || item.Download() == "" {
				continue
			}
			id := archiveID(feed, item)
			if id == nil {
				continue
			}
			if key := ids.Get(id); key != nil {
				raw := entries.Get(key)
				var entry archiveEntry
				if raw == nil || json.Unmarshal(raw, &entry) != nil {
					continue
				}
				changed := false
				if item.Seeders > 0 && item.Seeders != entry.Seeders {
					entry.Seeders, entry.Leechers, changed = item.Seeders, item.Leechers, true
				}
				if now.Unix()-entry.LastSeen > int64(archiveLastSeenRefresh/time.Second) {
					entry.LastSeen, changed = now.Unix(), true
				}
				if changed {
					if err := putEntry(tx, key, entry); err != nil {
						return err
					}
				}
				continue
			}
			entry := archiveEntry{
				ID:        hex.EncodeToString(id),
				Title:     item.Title,
				Download:  item.Download(),
				Size:      item.Size,
				Seeders:   item.Seeders,
				Leechers:  item.Leechers,
				Feed:      feed,
				Source:    item.Source,
				FirstSeen: now.Unix(),
				LastSeen:  now.Unix(),
			}
			if !item.Published.IsZero() {
				entry.Published = item.Published.Unix()
			}
			key := archiveItemsKey(entry.FirstSeen, id)
			if err := putEntry(tx, key, entry); err != nil {
				return err
			}
			if err := ids.Put(id, key); err != nil {
				return err
			}
			added++
		}
		return nil
	})
	return added, err
}

// markAdded flags the release behind a download target as downloaded.
func (a *feedArchive) markAdded(feed string, item rss.Item) {
	if a == nil {
		return
	}
	id := archiveID(feed, item)
	if id == nil {
		return
	}
	_ = a.db.Update(func(tx *bolt.Tx) error {
		key := tx.Bucket(archiveIDsBucket).Get(id)
		if key == nil {
			return nil
		}
		var entry archiveEntry
		if raw := tx.Bucket(archiveItemsBucket).Get(key); raw == nil || json.Unmarshal(raw, &entry) != nil || entry.Added {
			return nil
		}
		entry.Added = true
		return putEntry(tx, key, entry)
	})
}

// get returns one entry by its hex id.
func (a *feedArchive) get(hexID string) (archiveEntry, bool) {
	var entry archiveEntry
	if a == nil {
		return entry, false
	}
	id, err := hex.DecodeString(strings.TrimSpace(hexID))
	if err != nil || len(id) != sha1.Size {
		return entry, false
	}
	found := false
	_ = a.db.View(func(tx *bolt.Tx) error {
		key := tx.Bucket(archiveIDsBucket).Get(id)
		if key == nil {
			return nil
		}
		raw := tx.Bucket(archiveItemsBucket).Get(key)
		found = raw != nil && json.Unmarshal(raw, &entry) == nil
		return nil
	})
	return entry, found
}

// archiveQuery is a parsed search: every include term (or quoted phrase) must
// appear in the title, no exclude term (-word) may. Matching ignores case and
// treats . _ - as spaces, so "the office" finds The.Office.S01E01.
type archiveQuery struct {
	include, exclude [][]byte
	feed             string
	onlyAdded        bool
}

var archiveSeparators = strings.NewReplacer(".", " ", "_", " ", "-", " ", "[", " ", "]", " ", "(", " ", ")", " ")

func normalizeArchiveText(value string) string {
	value = archiveSeparators.Replace(strings.ToLower(value))
	return " " + strings.Join(strings.Fields(value), " ") + " "
}

func parseArchiveQuery(raw, feed string, onlyAdded bool) archiveQuery {
	q := archiveQuery{feed: strings.TrimSpace(feed), onlyAdded: onlyAdded}
	raw = strings.TrimSpace(raw)
	for raw != "" {
		negate := false
		if strings.HasPrefix(raw, "-") && len(raw) > 1 {
			negate = true
			raw = raw[1:]
		}
		var term string
		if strings.HasPrefix(raw, `"`) {
			end := strings.Index(raw[1:], `"`)
			if end < 0 {
				term, raw = raw[1:], ""
			} else {
				term, raw = raw[1:end+1], raw[end+2:]
			}
		} else if space := strings.IndexByte(raw, ' '); space >= 0 {
			term, raw = raw[:space], raw[space+1:]
		} else {
			term, raw = raw, ""
		}
		raw = strings.TrimSpace(raw)
		norm := strings.TrimSpace(normalizeArchiveText(term))
		if norm == "" {
			continue
		}
		if negate {
			q.exclude = append(q.exclude, []byte(norm))
		} else {
			q.include = append(q.include, []byte(norm))
		}
	}
	return q
}

// empty reports a query that matches everything.
func (q archiveQuery) empty() bool {
	return len(q.include) == 0 && len(q.exclude) == 0 && q.feed == "" && !q.onlyAdded
}

func (q archiveQuery) matchIdx(rec archiveIdxView) bool {
	if q.feed != "" && string(rec.feed) != q.feed {
		return false
	}
	if q.onlyAdded && !rec.added {
		return false
	}
	for _, term := range q.include {
		if !bytes.Contains(rec.title, term) {
			return false
		}
	}
	for _, term := range q.exclude {
		if bytes.Contains(rec.title, term) {
			return false
		}
	}
	return true
}

// archivePage is one page of search results.
type archivePage struct {
	Items []archiveEntry `json:"items"`
	Total int            `json:"total"`
	Page  int            `json:"page"`
	Pages int            `json:"pages"`
}

// search returns one page of matches. sortBy is "new" (default), "old",
// "size", "seeds" or "title". It scans the compact index only and decodes the
// JSON of the rows on the page: by date it keeps just the keys of that page.
func (a *feedArchive) search(q archiveQuery, sortBy string, page, size int) (archivePage, error) {
	out := archivePage{Items: []archiveEntry{}, Page: 1, Pages: 1}
	if a == nil {
		return out, nil
	}
	if size <= 0 {
		size = archivePageSize
	}
	if page < 1 {
		page = 1
	}
	err := a.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(archiveIdxBucket).Cursor()
		type hit struct {
			key     []byte
			size    int64
			seeders int
			title   string
		}
		var keys [][]byte
		total := 0
		switch sortBy {
		case "size", "seeds", "title":
			var hits []hit
			for k, v := c.Last(); k != nil; k, v = c.Prev() {
				rec, ok := parseArchiveIdx(v)
				if !ok || !q.matchIdx(rec) {
					continue
				}
				h := hit{key: k, size: rec.size, seeders: rec.seeders}
				if sortBy == "title" {
					h.title = string(rec.title)
				}
				hits = append(hits, h)
			}
			sort.SliceStable(hits, func(i, j int) bool {
				switch sortBy {
				case "size":
					return hits[i].size > hits[j].size
				case "seeds":
					return hits[i].seeders > hits[j].seeders
				}
				return hits[i].title < hits[j].title
			})
			total = len(hits)
			start := min((page-1)*size, total)
			for _, h := range hits[start:min(start+size, total)] {
				keys = append(keys, h.key)
			}
		default:
			first, step := c.Last, c.Prev
			if sortBy == "old" {
				first, step = c.First, c.Next
			}
			start := (page - 1) * size
			for k, v := first(); k != nil; k, v = step() {
				rec, ok := parseArchiveIdx(v)
				if !ok || !q.matchIdx(rec) {
					continue
				}
				if total >= start && total < start+size {
					keys = append(keys, k)
				}
				total++
			}
		}
		out.Total = total
		out.Pages = max((total+size-1)/size, 1)
		out.Page = min(page, out.Pages)
		if out.Page != page {
			// Past the last page: show the last one instead of nothing.
			return errArchiveReread
		}
		items := tx.Bucket(archiveItemsBucket)
		for _, key := range keys {
			var entry archiveEntry
			if raw := items.Get(key); raw != nil && json.Unmarshal(raw, &entry) == nil {
				out.Items = append(out.Items, entry)
			}
		}
		return nil
	})
	if errors.Is(err, errArchiveReread) {
		return a.search(q, sortBy, out.Page, size)
	}
	return out, err
}

var errArchiveReread = errors.New("archive: page out of range")

// deleteIDs removes entries by hex id.
func (a *feedArchive) deleteIDs(hexIDs []string) (int, error) {
	if a == nil {
		return 0, nil
	}
	removed := 0
	err := a.db.Update(func(tx *bolt.Tx) error {
		ids := tx.Bucket(archiveIDsBucket)
		for _, hexID := range hexIDs {
			id, err := hex.DecodeString(strings.TrimSpace(hexID))
			if err != nil || len(id) != sha1.Size {
				continue
			}
			key := ids.Get(id)
			if key == nil {
				continue
			}
			if err := deleteEntry(tx, key); err != nil {
				return err
			}
			removed++
		}
		return nil
	})
	return removed, err
}

// deleteMatching removes every entry the query matches. An empty query (no
// terms, no feed, not only-added) is refused: it would wipe the archive.
func (a *feedArchive) deleteMatching(q archiveQuery) (int, error) {
	if a == nil {
		return 0, nil
	}
	if q.empty() {
		return 0, errors.New("refusing to delete the whole archive: search for something first")
	}
	removed := 0
	err := a.db.Update(func(tx *bolt.Tx) error {
		var doomed [][]byte
		c := tx.Bucket(archiveIdxBucket).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if rec, ok := parseArchiveIdx(v); ok && q.matchIdx(rec) {
				doomed = append(doomed, append([]byte(nil), k...))
			}
		}
		for _, key := range doomed {
			if err := deleteEntry(tx, key); err != nil {
				return err
			}
			removed++
		}
		return nil
	})
	return removed, err
}

// prune removes the entries first seen before cutoff and, past the size cap,
// the oldest ones. It walks from the oldest entry and stops at the first one
// to keep, so a routine prune costs almost nothing.
func (a *feedArchive) prune(cutoff time.Time, maxEntries int) (int, error) {
	if a == nil {
		return 0, nil
	}
	removed := 0
	err := a.db.Update(func(tx *bolt.Tx) error {
		entries := tx.Bucket(archiveItemsBucket)
		excess := entries.Stats().KeyN - maxEntries
		var doomed [][]byte
		c := entries.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			old := !cutoff.IsZero() && len(k) >= 8 && int64(binary.BigEndian.Uint64(k[:8])) < cutoff.Unix()
			if !old && excess <= 0 {
				break
			}
			doomed = append(doomed, append([]byte(nil), k...))
			excess--
		}
		for _, key := range doomed {
			if err := deleteEntry(tx, key); err != nil {
				return err
			}
			removed++
		}
		return nil
	})
	return removed, err
}

// archiveStats is the archive size for the page.
type archiveStats struct {
	Entries   int      `json:"entries"`
	SizeBytes int64    `json:"size_bytes"`
	Feeds     []string `json:"feeds"`
}

// stats counts the entries and, with feeds set, lists the feed names (a scan
// of the compact index).
func (a *feedArchive) stats(feeds bool) archiveStats {
	out := archiveStats{Feeds: []string{}}
	if a == nil {
		return out
	}
	_ = a.db.View(func(tx *bolt.Tx) error {
		idx := tx.Bucket(archiveIdxBucket)
		out.Entries = idx.Stats().KeyN
		if !feeds {
			return nil
		}
		seen := map[string]bool{}
		return idx.ForEach(func(_, v []byte) error {
			if rec, ok := parseArchiveIdx(v); ok && !seen[string(rec.feed)] {
				seen[string(rec.feed)] = true
				out.Feeds = append(out.Feeds, string(rec.feed))
			}
			return nil
		})
	})
	sort.Strings(out.Feeds)
	if info, err := os.Stat(a.db.Path()); err == nil {
		out.SizeBytes = info.Size()
	}
	return out
}

// archive returns the feed archive, opening it on first use. It exists only in
// standalone mode with a data directory: managed mode has no feeds, and a
// Daemon built by hand in a test must not create files.
func (d *Daemon) archive() *feedArchive {
	if d.opts.Mode != ModeStandalone || d.opts.DataDir == "" {
		return nil
	}
	d.archiveOnce.Do(func() {
		archive, err := openFeedArchive(filepath.Join(d.opts.DataDir, feedArchiveFile))
		if err != nil {
			logf("feed archive unavailable: %v", err)
			return
		}
		d.feedArchive = archive
	})
	return d.feedArchive
}

// archiveRetentionDays is the configured retention (0 = keep everything).
func (d *Daemon) archiveRetentionDays() int {
	if d.opts.Settings == nil {
		return archiveDefaultDays
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(archiveDaysKey, ""))
	if raw == "" {
		return archiveDefaultDays
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days < 0 {
		return archiveDefaultDays
	}
	return days
}

// pruneArchive applies the retention and the size cap.
func (d *Daemon) pruneArchive(now time.Time) {
	archive := d.archive()
	if archive == nil {
		return
	}
	var cutoff time.Time
	if days := d.archiveRetentionDays(); days > 0 {
		cutoff = now.Add(-time.Duration(days) * 24 * time.Hour)
	}
	if removed, err := archive.prune(cutoff, archiveMaxEntries); err != nil {
		logf("feed archive prune: %v", err)
	} else if removed > 0 {
		logf("feed archive: removed %d old item(s)", removed)
	}
}
