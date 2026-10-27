package gextto

// indexer_lanes.go queries Jackett and Prowlarr one indexer at a time instead
// of through their "all indexers" aggregate. A manager serves each of its
// indexers one search at a time and an aggregate answers when its slowest
// indexer has: measured on 2026-10-17, Prowlarr gave every aggregate the
// throughput of TorrentLeech (3.9 s/search) whatever the concurrency, six
// concurrent title searches each waited 24.6 s, and a request cut at its
// timeout threw away the LimeTorrents and The Pirate Bay results that were
// ready. A lane per underlying indexer gets the concurrency the manager really
// serves, its own timeout and its own health, so a slow tracker costs only its
// own results. A tracker listed in both managers is searched once, through
// the lane that answers faster; the indexers Prowlarr reports as failing are
// skipped. When the list cannot be read the manager is queried as before.
//
// indexarr is the exception: it is always searched through its own /api
// aggregate. It already queries its sources in parallel, answers as soon as
// the fast ones have, keeps the slow ones running in the background, merges
// the duplicates and names the site of each result (jackettindexer). Measured
// on 2026-10-27, one lane per indexarr source turned every title into 46
// requests and a 104-title search into 3 min 40 s, held back by the two slots
// of its slowest source; the aggregate answers the same title in 2.6 s.

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/utils"
)

const (
	// laneListTTL is how long a manager's indexer list is reused;
	// laneListRetry the wait after a failed read.
	laneListTTL   = 10 * time.Minute
	laneListRetry = 2 * time.Minute
	// laneListTimeout bounds the read of a manager's indexer list.
	laneListTimeout = 10 * time.Second
	// laneSlotsProwlarr: Prowlarr runs one search at a time per indexer (more
	// concurrent requests only queue inside it). laneSlotsJackett: Jackett
	// answers its indexers in parallel and caches repeated searches.
	// laneSlotsPlain keeps a direct indexer as it was (six title searches).
	laneSlotsProwlarr = 1
	laneSlotsJackett  = 2
	laneSlotsPlain    = 6
	// laneLatencyWeight is the weight of a new sample in a lane's moving
	// average (which of two duplicate lanes is faster).
	laneLatencyWeight = 0.3
)

// searchLane is one source of a title search: a plain indexer, or one
// indexer behind a manager.
type searchLane struct {
	// indexer is what the request is built from. Its Name stays the manager's,
	// so the release labels ("Prowlarr - LimeTorrents") do not change.
	indexer IndexerConfig
	// health names the lane for the source statistics, the provider backoff
	// and the cycle warnings: "prowlarr · TorrentLeech".
	health string
	// key identifies the lane for its concurrency slots and latency.
	key   string
	slots int
	// timeout bounds one request of the lane once it holds a slot.
	timeout time.Duration
	// dedup is the tracker identity used to search a tracker once.
	dedup string
}

// managerIndexer is one indexer read from a manager's list.
type managerIndexer struct {
	id    string
	title string
	dedup string
	caps  *searchCaps
}

type laneListEntry struct {
	indexers []managerIndexer
	fetched  time.Time
	failed   bool
}

// managerLanes switches the per-indexer lanes on; off, a manager is queried
// as an aggregate (the measurement compares the two).
var managerLanes = true

var (
	laneListMu    sync.Mutex
	laneListCache = map[string]laneListEntry{}
	laneFetchMu   sync.Map // manager key -> *sync.Mutex, one read at a time

	laneSlotMu sync.Mutex
	laneSlots  = map[string]chan struct{}{}
	laneQueue  = map[string]int{} // lane key -> searches waiting for a slot

	laneLatencyMu sync.Mutex
	laneLatency   = map[string]float64{} // lane key -> seconds (moving average)
)

// trackerKey normalizes a tracker name for the cross-manager dedup.
func trackerKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// managerBase is the manager's root URL (scheme://host[:port][/prefix]).
func managerBase(indexer IndexerConfig) string {
	base := strings.TrimRight(strings.TrimSpace(indexer.URL), "/")
	for _, suffix := range []string{"/api/v1/search", "/api/v2.0/indexers/all/results/torznab/api", "/api/v2.0/indexers/all/results/torznab", "/api"} {
		base = strings.TrimSuffix(base, suffix)
	}
	return base
}

// readManagerIndexers lists the enabled torrent indexers of a manager.
func readManagerIndexers(ctx context.Context, indexer IndexerConfig) ([]managerIndexer, error) {
	ctx, cancel := context.WithTimeout(ctx, laneListTimeout)
	defer cancel()
	base := managerBase(indexer)
	get := func(rawURL string) ([]byte, error) {
		response, err := httpDo(ctx, defaultHTTPClient, http.MethodGet, rawURL, nil, nil, "")
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, &laneHTTPError{status: response.StatusCode}
		}
		return io.ReadAll(io.LimitReader(response.Body, 8<<20))
	}
	key := url.QueryEscape(indexer.APIKey)
	if managerKind(indexer) == ManagerProwlarr {
		body, err := get(base + "/api/v1/indexer?apikey=" + key)
		if err != nil {
			return nil, err
		}
		var list []json.RawMessage
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, err
		}
		failing := map[int64]bool{}
		if statusBody, err := get(base + "/api/v1/indexerstatus?apikey=" + key); err == nil {
			var statuses []struct {
				IndexerID    int64     `json:"indexerId"`
				DisabledTill time.Time `json:"disabledTill"`
			}
			if json.Unmarshal(statusBody, &statuses) == nil {
				for _, status := range statuses {
					if status.DisabledTill.After(time.Now()) {
						failing[status.IndexerID] = true
					}
				}
			}
		}
		var out []managerIndexer
		for _, raw := range list {
			var item struct {
				ID             int64  `json:"id"`
				Name           string `json:"name"`
				Enable         bool   `json:"enable"`
				Protocol       string `json:"protocol"`
				DefinitionName string `json:"definitionName"`
			}
			if json.Unmarshal(raw, &item) != nil || !item.Enable || failing[item.ID] {
				continue
			}
			if item.Protocol != "" && !strings.EqualFold(item.Protocol, "torrent") {
				continue
			}
			dedup := trackerKey(item.DefinitionName)
			if dedup == "" {
				dedup = trackerKey(item.Name)
			}
			out = append(out, managerIndexer{
				id:    strconv.FormatInt(item.ID, 10),
				title: item.Name,
				dedup: dedup,
				// The capabilities of this one indexer, not the intersection
				// over all of them the aggregate has to use.
				caps: parseProwlarrCaps("[" + string(raw) + "]"),
			})
		}
		return out, nil
	}
	body, err := get(base + "/api/v2.0/indexers/all/results/torznab/api?t=indexers&configured=true&apikey=" + key)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Indexers []struct {
			ID         string `xml:"id,attr"`
			Configured string `xml:"configured,attr"`
			Title      string `xml:"title"`
		} `xml:"indexer"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	var out []managerIndexer
	for _, item := range doc.Indexers {
		if item.ID == "" || strings.EqualFold(item.Configured, "false") {
			continue
		}
		out = append(out, managerIndexer{id: item.ID, title: item.Title, dedup: trackerKey(item.ID)})
	}
	return out, nil
}

type laneHTTPError struct{ status int }

func (e *laneHTTPError) Error() string { return "HTTP " + strconv.Itoa(e.status) }

// managerIndexersCached returns the manager's indexers, reading the list when
// missing or stale; ok is false when no list is available (query the manager
// as an aggregate).
func managerIndexersCached(ctx context.Context, indexer IndexerConfig) ([]managerIndexer, bool) {
	cacheKey := managerBase(indexer) + "\x00" + indexer.APIKey
	fresh := func() ([]managerIndexer, bool, bool) {
		laneListMu.Lock()
		defer laneListMu.Unlock()
		entry, ok := laneListCache[cacheKey]
		if !ok {
			return nil, false, false
		}
		ttl := laneListTTL
		if entry.failed {
			ttl = laneListRetry
		}
		if time.Since(entry.fetched) >= ttl {
			return nil, false, false
		}
		return entry.indexers, !entry.failed && len(entry.indexers) > 0, true
	}
	if list, ok, hit := fresh(); hit {
		return list, ok
	}
	lockValue, _ := laneFetchMu.LoadOrStore(cacheKey, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	if list, ok, hit := fresh(); hit {
		return list, ok
	}
	list, err := readManagerIndexers(ctx, indexer)
	if ctx.Err() != nil {
		return nil, false // the search was cancelled: remember nothing
	}
	laneListMu.Lock()
	laneListCache[cacheKey] = laneListEntry{indexers: list, fetched: time.Now(), failed: err != nil}
	laneListMu.Unlock()
	if err != nil {
		logging.Debug("indexer manager list unavailable; querying it as an aggregate",
			"indexer", indexer.Name, "error", utils.RedactURLSecrets(err.Error()))
		return nil, false
	}
	return list, len(list) > 0
}

// laneForManagerIndexer builds the lane of one indexer behind a manager.
func laneForManagerIndexer(manager IndexerConfig, item managerIndexer) searchLane {
	lane := searchLane{
		indexer: manager,
		health:  manager.Name + " · " + item.title,
		dedup:   item.dedup,
		// One indexer answers on its own: the aggregate's longer limit is
		// not needed.
		timeout: indexerRequestTimeout,
	}
	base := managerBase(manager)
	if managerKind(manager) == ManagerProwlarr {
		lane.indexer.URL = base + "/api/v1/search?indexerIds=" + url.QueryEscape(item.id)
		lane.slots = laneSlotsProwlarr
		// A search Gextto abandons keeps running inside Prowlarr and queues the
		// next ones behind it: with one search per indexer, waiting longer
		// costs less than abandoning.
		lane.timeout = managerRequestTimeout
		if item.caps != nil {
			capsURL, _ := searchCapsURL(lane.indexer)
			searchCapsCache.Store(capsURL, searchCapsEntry{caps: item.caps, fetched: time.Now()})
		}
	} else {
		lane.indexer.URL = base + "/api/v2.0/indexers/" + url.PathEscape(item.id) + "/results/torznab/api"
		lane.slots = laneSlotsJackett
	}
	lane.key = lane.indexer.URL
	return lane
}

// expandSearchLanes turns the configured indexers into the lanes of a title
// search: a manager becomes one lane per indexer, a tracker present in more
// than one manager is kept once (the faster lane), and a plain indexer is
// its own lane.
func expandSearchLanes(ctx context.Context, indexers []IndexerConfig) []searchLane {
	var lanes []searchLane
	for _, indexer := range indexers {
		switch {
		case !isManagerSource(indexer):
			lanes = append(lanes, searchLane{indexer: indexer, health: indexer.Name, key: "plain\x00" + indexer.Name + "\x00" + indexer.URL, slots: laneSlotsPlain, timeout: indexerRequestTimeout})
		case !managerLanes || managerKind(indexer) == ManagerIndexarr:
			lanes = append(lanes, aggregateLane(indexer))
		default:
			list, ok := managerIndexersCached(ctx, indexer)
			if !ok {
				lanes = append(lanes, aggregateLane(indexer))
				continue
			}
			for _, item := range list {
				lanes = append(lanes, laneForManagerIndexer(indexer, item))
			}
		}
	}
	return dedupeLanes(lanes)
}

// aggregateLane queries a manager through its "all indexers" endpoint.
func aggregateLane(indexer IndexerConfig) searchLane {
	return searchLane{indexer: indexer, health: indexer.Name, key: "aggregate\x00" + indexer.Name + "\x00" + indexer.URL, slots: laneSlotsPlain, timeout: managerRequestTimeout}
}

// dedupeLanes keeps, of the lanes searching the same tracker, the one with
// the lowest latency so far; a lane never measured is preferred, so each is
// tried once. Lanes without a tracker identity are all kept, in order.
func dedupeLanes(lanes []searchLane) []searchLane {
	best := map[string]int{}
	for i, lane := range lanes {
		if lane.dedup == "" {
			continue
		}
		j, seen := best[lane.dedup]
		if !seen || laneLatencyOf(lane.key) < laneLatencyOf(lanes[j].key) {
			best[lane.dedup] = i
		}
	}
	out := make([]searchLane, 0, len(lanes))
	for i, lane := range lanes {
		if lane.dedup != "" && best[lane.dedup] != i {
			continue
		}
		out = append(out, lane)
	}
	return out
}

func laneLatencyOf(key string) float64 {
	laneLatencyMu.Lock()
	defer laneLatencyMu.Unlock()
	return laneLatency[key]
}

// noteLaneLatency folds one request's duration into the lane's average.
func noteLaneLatency(key string, took time.Duration) {
	laneLatencyMu.Lock()
	defer laneLatencyMu.Unlock()
	seconds := took.Seconds()
	if previous, ok := laneLatency[key]; ok {
		seconds = previous*(1-laneLatencyWeight) + seconds*laneLatencyWeight
	}
	laneLatency[key] = seconds
}

// errLaneBusy: the lane's queue would outlast the title's budget, so this
// title skips it. Not a failure of the source.
var errLaneBusy = errors.New("lane busy beyond the search budget")

// acquireLane waits for a free slot of the lane; release gives it back. It
// fails when ctx ends first, or at once with errLaneBusy when the searches
// already waiting, at the lane's average time, would outlast ctx's deadline.
func acquireLane(ctx context.Context, lane searchLane) (release func(), err error) {
	slots := max(lane.slots, 1)
	laneSlotMu.Lock()
	ch, ok := laneSlots[lane.key]
	if !ok {
		ch = make(chan struct{}, slots)
		laneSlots[lane.key] = ch
	}
	ahead := laneQueue[lane.key] + len(ch)
	if deadline, has := ctx.Deadline(); has && ahead >= slots {
		perSearch := laneLatencyOf(lane.key)
		wait := time.Duration(float64(ahead-slots+1) / float64(slots) * perSearch * float64(time.Second))
		if perSearch > 0 && time.Until(deadline) < wait+time.Duration(perSearch*float64(time.Second)) {
			laneSlotMu.Unlock()
			return nil, errLaneBusy
		}
	}
	laneQueue[lane.key]++
	laneSlotMu.Unlock()
	defer func() {
		laneSlotMu.Lock()
		laneQueue[lane.key]--
		laneSlotMu.Unlock()
	}()
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
