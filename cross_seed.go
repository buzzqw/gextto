package gextto

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

const crossSeedSearchTimeout = 45 * time.Second

var errCrossSeedTargetNotFound = errors.New("completed torrent not found")

type crossSeedTarget struct {
	Hash        string `json:"hash"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	Series      string `json:"series,omitempty"`
	Season      int64  `json:"season,omitempty"`
	Episode     int64  `json:"episode,omitempty"`
	Year        int64  `json:"year,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	// ProcessedPath is where the verified copy lives. It never leaves the
	// server: the discovery API must not expose paths or links.
	ProcessedPath string `json:"-"`
}

type crossSeedMatch struct {
	Source         string   `json:"source"`
	Title          string   `json:"title"`
	SizeBytes      int64    `json:"size_bytes"`
	Seeders        int64    `json:"seeders"`
	Peers          int64    `json:"peers"`
	Swarm          string   `json:"swarm"`
	Freeleech      bool     `json:"freeleech"`
	FreeleechKnown bool     `json:"freeleech_known"`
	LinkType       string   `json:"link_type"`
	TrackerHosts   []string `json:"tracker_hosts,omitempty"`
	Allowed        bool     `json:"allowed"`
	HnRRisk        bool     `json:"hnr_risk,omitempty"`
	HnRNote        string   `json:"hnr_note,omitempty"`
}

type crossSeedSourceStatus struct {
	Source    string `json:"source"`
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type crossSeedResult struct {
	Target  crossSeedTarget         `json:"target"`
	Checked []crossSeedSourceStatus `json:"checked"`
	Matches []crossSeedMatch        `json:"matches"`
	Errors  []crossSeedSourceStatus `json:"errors,omitempty"`
	Partial bool                    `json:"partial"`
}

// crossSeedCandidate is the server-side half of a match. It keeps the usable
// link and the full release so the action can add the torrent; the discovery
// API deliberately serializes only crossSeedMatch, never this type.
type crossSeedCandidate struct {
	crossSeedMatch
	Release models.Release
}

// crossSeedTargetFor resolves a completed, processed torrent without depending
// on the live session: torrent_meta keeps the release after the torrent has been
// removed. Episodes and movies are a fallback for older databases.
func crossSeedTargetFor(db *Database, rawHash string) (crossSeedTarget, error) {
	hash := canonicalInfoHash(rawHash)
	if hash == "" {
		return crossSeedTarget{}, fmt.Errorf("invalid infohash")
	}
	var completedAt, processedPath, network, metadata, title, kind, series string
	var season, episode, year int64
	err := db.db.QueryRow(`SELECT COALESCE(completed_at,''), COALESCE(processed_path,''), COALESCE(network,''), COALESCE(metadata_json,''), COALESCE(title,''), COALESCE(kind,''), COALESCE(series_name,''), COALESCE(season,0), COALESCE(episode,0), COALESCE(year,0) FROM torrent_meta WHERE hash=?1`, hash).Scan(
		&completedAt, &processedPath, &network, &metadata, &title, &kind, &series, &season, &episode, &year,
	)
	if err == nil && completedAt != "" && processedPath != "" && network != "ed2k" {
		target := crossSeedTarget{Hash: hash, Title: title, Kind: kind, Series: series, Season: season, Episode: episode, Year: year, CompletedAt: completedAt, ProcessedPath: processedPath}
		if metadata != "" {
			var meta models.TorrentMeta
			if json.Unmarshal([]byte(metadata), &meta) == nil {
				release := meta.Release
				if release.Title != "" {
					target.Title = release.Title
				}
				if release.Kind != "" {
					target.Kind = release.Kind
				}
				if release.Series != nil {
					target.Series = *release.Series
				}
				if release.Season != nil {
					target.Season = *release.Season
				}
				if release.Episode != nil {
					target.Episode = *release.Episode
				}
				if release.Year != nil {
					target.Year = *release.Year
				}
				if releaseHash := releaseInfoHash(&release); releaseHash != "" && releaseHash != hash {
					return crossSeedTarget{}, errCrossSeedTargetNotFound
				}
			}
		}
		if target.Title == "" {
			target.Title = series
		}
		if target.Title == "" {
			return crossSeedTarget{}, errCrossSeedTargetNotFound
		}
		return target, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return crossSeedTarget{}, err
	}

	var episodeTitle, episodeDownloaded, episodePath, episodeMagnet string
	err = db.db.QueryRow(`SELECT COALESCE(e.title,''), COALESCE(e.downloaded_at,''), COALESCE(e.archive_path,''), COALESCE(e.magnet_link,'') FROM episodes e JOIN series s ON s.id=e.series_id WHERE e.magnet_hash=?1 AND COALESCE(e.archive_path,'')<>'' LIMIT 1`, hash).Scan(
		&series, &season, &episode, &episodeTitle, &episodeDownloaded, &episodePath, &episodeMagnet,
	)
	if err == nil && episodeDownloaded != "" && episodePath != "" {
		if episodeMagnet != "" {
			if magnetHash := releaseInfoHash(&models.Release{Magnet: episodeMagnet}); magnetHash != "" && magnetHash != hash {
				return crossSeedTarget{}, errCrossSeedTargetNotFound
			}
		}
		return crossSeedTarget{Hash: hash, Title: episodeTitle, Kind: "series", Series: series, Season: season, Episode: episode, CompletedAt: episodeDownloaded, ProcessedPath: episodePath}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return crossSeedTarget{}, err
	}

	var movieName, movieTitle, movieDownloaded string
	err = db.db.QueryRow(`SELECT COALESCE(name,''), COALESCE(title,''), COALESCE(year,0), COALESCE(downloaded_at,'') FROM movies WHERE magnet_hash=?1 AND downloaded_at IS NOT NULL LIMIT 1`, hash).Scan(&movieName, &movieTitle, &year, &movieDownloaded)
	if err == nil && movieDownloaded != "" {
		title = movieTitle
		if title == "" {
			title = movieName
		}
		return crossSeedTarget{Hash: hash, Title: title, Kind: "movie", Year: year, CompletedAt: movieDownloaded}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return crossSeedTarget{}, err
	}
	return crossSeedTarget{}, errCrossSeedTargetNotFound
}

type crossSeedSearch struct {
	Query       string
	ExternalIDs [][2]string
	SearchType  string
}

func crossSeedSearches(cfg *Config, target crossSeedTarget) []crossSeedSearch {
	if target.Kind == "series" || target.Series != "" || target.Season > 0 {
		series, ok := gh7FindSeries(cfg, target.Series)
		if !ok && target.Series == "" {
			series, ok = gh7FindSeries(cfg, target.Title)
		}
		title := target.Series
		if title == "" {
			title = target.Title
		}
		var ids [][2]string
		if ok {
			if titles := seriesIndexerTitles(&series); len(titles) > 0 {
				title = titles[0]
			}
			ids = seriesExternalIDs(&series)
		}
		suffix := ""
		if target.Episode > 0 {
			suffix = fmt.Sprintf("S%02dE%02d", target.Season, target.Episode)
		} else if target.Season > 0 {
			suffix = fmt.Sprintf("S%02d", target.Season)
		}
		query := strings.TrimSpace(title + " " + suffix)
		if query == "" {
			return nil
		}
		return []crossSeedSearch{{Query: query, ExternalIDs: ids, SearchType: searchTypeTV}}
	}
	title := target.Title
	var ids [][2]string
	var year *int64
	if target.Year > 0 {
		value := target.Year
		year = &value
	}
	if movie := cfg.FindMovieMatchManual(title, year); movie != nil {
		if titles := movieIndexerTitles(movie); len(titles) > 0 {
			title = titles[0]
		}
		if id := strings.TrimSpace(movie.TmdbID); id != "" {
			ids = append(ids, [2]string{"tmdbid", id})
		}
	}
	if year != nil {
		title += " " + fmt.Sprint(*year)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	return []crossSeedSearch{{Query: title, ExternalIDs: ids, SearchType: searchTypeMovie}}
}

func crossSeedSwarm(release *models.Release) string {
	switch swarmTier(release) {
	case 2:
		if release != nil && release.Seeders > 0 {
			return "healthy"
		}
		return "unknown"
	case 1:
		return "leechers-only"
	default:
		return "empty"
	}
}

func crossSeedLinkType(release *models.Release) string {
	switch {
	case release == nil:
		return "unknown"
	case release.Magnet != "":
		return "magnet"
	case release.TorrentURL != nil:
		return "torrent-url"
	default:
		return "unknown"
	}
}

// crossSeedTrackerHosts returns only hostnames. Magnets and manager download
// URLs often carry passkeys or API keys; none of that is sent to the browser.
func crossSeedTrackerHosts(release *models.Release) []string {
	if release == nil {
		return nil
	}
	seen := map[string]struct{}{}
	add := func(raw string) {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			return
		}
		host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
		if host != "" {
			seen[host] = struct{}{}
		}
	}
	if release.Magnet != "" {
		if parsed, err := url.Parse(release.Magnet); err == nil {
			for key, values := range parsed.Query() {
				if key != "tr" && !strings.HasPrefix(key, "tr.") {
					continue
				}
				for _, value := range values {
					add(value)
				}
			}
		}
	}
	if release.TorrentURL != nil {
		add(*release.TorrentURL)
	}
	hosts := make([]string, 0, len(seen))
	for host := range seen {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	if len(hosts) > 8 {
		hosts = hosts[:8]
	}
	return hosts
}

// discoverCrossSeed is deliberately read-only: it searches configured indexers
// and keeps exact infohash matches, but never adds a torrent, writes the archive
// or updates gap/seen state.
func discoverCrossSeed(ctx context.Context, db *Database, cfg *Config, rawHash string) (crossSeedResult, error) {
	result, _, err := discoverCrossSeedDetailed(ctx, db, cfg, rawHash)
	return result, err
}

// discoverCrossSeedDetailed is the same search, but it also returns the
// server-side candidates with their links. Only the caller of the explicit
// seed action may use them; the discovery response contains no links.
func discoverCrossSeedDetailed(ctx context.Context, db *Database, cfg *Config, rawHash string) (crossSeedResult, []crossSeedCandidate, error) {
	target, err := crossSeedTargetFor(db, rawHash)
	if err != nil {
		return crossSeedResult{}, nil, err
	}
	result := crossSeedResult{Target: target, Checked: []crossSeedSourceStatus{}, Matches: []crossSeedMatch{}}
	if cfg == nil {
		return result, nil, fmt.Errorf("configuration unavailable")
	}
	searches := crossSeedSearches(cfg, target)
	if len(searches) == 0 {
		return result, nil, fmt.Errorf("no title identity available for this torrent")
	}
	indexers := make([]IndexerConfig, 0, len(cfg.Indexers))
	for _, indexer := range cfg.Indexers {
		if indexer.Enabled && strings.TrimSpace(indexer.URL) != "" {
			indexers = append(indexers, indexer)
		}
	}
	if len(indexers) == 0 {
		return result, nil, fmt.Errorf("no enabled indexer is configured")
	}

	searchCtx, cancel := context.WithTimeout(ctx, crossSeedSearchTimeout)
	defer cancel()
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var mu sync.Mutex
	matches := map[string]crossSeedCandidate{}
	for _, indexer := range indexers {
		indexer := indexer
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-searchCtx.Done():
				mu.Lock()
				status := crossSeedSourceStatus{Source: indexer.Name, Status: "skipped", Error: "search time expired"}
				result.Checked = append(result.Checked, status)
				result.Errors = append(result.Errors, status)
				result.Partial = true
				mu.Unlock()
				return
			}
			started := time.Now()
			status := crossSeedSourceStatus{Source: indexer.Name, Status: "ok"}
			for _, search := range searches {
				releases, searchErr := fetchTorznabFlareSolverr(searchCtx, indexer, search.Query, search.ExternalIDs, cfg.FlaresolverrURL, search.SearchType)
				if searchErr != nil {
					status.Status = "error"
					status.Error = utils.RedactURLSecrets(searchErr.Error())
					break
				}
				for i := range releases {
					release := &releases[i]
					if releaseInfoHash(release) != target.Hash {
						continue
					}
					candidate := crossSeedCandidate{
						crossSeedMatch: crossSeedMatch{
							Source: release.Source, Title: release.Title, SizeBytes: release.SizeBytes,
							Seeders: release.Seeders, Peers: release.Peers, Swarm: crossSeedSwarm(release),
							Freeleech: release.Freeleech, FreeleechKnown: release.FreeleechKnown,
							LinkType: crossSeedLinkType(release), TrackerHosts: crossSeedTrackerHosts(release),
						},
						Release: *release,
					}
					if candidate.Source == "" {
						candidate.Source = indexer.Name
					}
					key := strings.ToLower(candidate.Source + "\x00" + candidate.Title + "\x00" + candidate.LinkType)
					mu.Lock()
					if previous, ok := matches[key]; !ok || candidate.Seeders > previous.Seeders {
						matches[key] = candidate
					}
					mu.Unlock()
				}
			}
			status.LatencyMS = time.Since(started).Milliseconds()
			mu.Lock()
			result.Checked = append(result.Checked, status)
			if status.Status == "error" {
				result.Errors = append(result.Errors, status)
				result.Partial = true
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	candidates := make([]crossSeedCandidate, 0, len(matches))
	result.Matches = make([]crossSeedMatch, 0, len(matches))
	for _, candidate := range matches {
		candidates = append(candidates, candidate)
		result.Matches = append(result.Matches, candidate.crossSeedMatch)
	}
	sort.Slice(result.Checked, func(i, j int) bool { return result.Checked[i].Source < result.Checked[j].Source })
	sort.Slice(result.Matches, func(i, j int) bool {
		if result.Matches[i].Source != result.Matches[j].Source {
			return result.Matches[i].Source < result.Matches[j].Source
		}
		if result.Matches[i].Seeders != result.Matches[j].Seeders {
			return result.Matches[i].Seeders > result.Matches[j].Seeders
		}
		return result.Matches[i].Title < result.Matches[j].Title
	})
	for i := range result.Matches {
		result.Matches[i].Allowed = crossSeedAllowed(cfg, &result.Matches[i], &target)
		result.Matches[i].HnRRisk, result.Matches[i].HnRNote = crossSeedHnRNote(cfg, db, target, result.Matches[i])
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Source != candidates[j].Source {
			return candidates[i].Source < candidates[j].Source
		}
		if candidates[i].Seeders != candidates[j].Seeders {
			return candidates[i].Seeders > candidates[j].Seeders
		}
		return candidates[i].Title < candidates[j].Title
	})
	return result, candidates, nil
}

// CrossSeedDiscover implements POST /api/library/cross-seed/discover.
func CrossSeedDiscover(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input struct {
		Hash string `json:"hash"`
	}
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if canonicalInfoHash(input.Hash) == "" {
		jsonError(w, http.StatusBadRequest, "invalid infohash")
		return
	}
	result, err := discoverCrossSeed(r.Context(), s.db, latestConfig(s), input.Hash)
	if errors.Is(err, errCrossSeedTargetNotFound) {
		jsonError(w, http.StatusNotFound, "no completed torrent with this infohash")
		return
	}
	if err != nil {
		jsonError(w, http.StatusConflict, utils.RedactURLSecrets(err.Error()))
		return
	}
	jsonResponse(w, result)
}

var errCrossSeedHnRConfirmation = errors.New("this source has finite seed targets; confirm the hit-and-run risk before seeding")

// crossSeedAllowed is the opt-in policy. An identical copy may be seeded only
// when a seed rule explicitly enables cross-seed for that source or tracker,
// so a tracker that forbids it is never contacted by the action.
func crossSeedAllowed(cfg *Config, match *crossSeedMatch, target *crossSeedTarget) bool {
	if cfg == nil || match == nil || target == nil {
		return false
	}
	category := strings.ToLower(strings.TrimSpace(target.Kind))
	for _, rule := range seedRulesFrom(cfg) {
		if !rule.CrossSeed {
			continue
		}
		if rule.Category != "" && rule.Category != category {
			continue
		}
		needle := strings.ToLower(strings.TrimSpace(rule.Source))
		if needle == "" || strings.Contains(strings.ToLower(match.Source), needle) {
			return true
		}
		for _, host := range match.TrackerHosts {
			if strings.Contains(strings.ToLower(host), needle) {
				return true
			}
		}
	}
	return false
}

// crossSeedHnRNote says, before anything is added, what happens if the seed
// ends early. A finite target on a tracker that enforces hit-and-run rules is
// a risk the operator must confirm; an unlimited seed has no deadline to miss.
func crossSeedHnRNote(cfg *Config, db *Database, target crossSeedTarget, match crossSeedMatch) (bool, string) {
	if cfg == nil {
		return false, ""
	}
	// -1 asks the policy to inherit the rule/global limits: a zero here
	// would read as an explicit "seed forever" and hide the H&R risk.
	view := models.TorrentView{Hash: target.Hash, Progress: 100, TotalSize: match.SizeBytes,
		AllTimeDownload: match.SizeBytes, SeedRatio: -1, SeedDays: -1}
	limits := newSeedPolicy(cfg, db).limitsFor(seedViewForPolicy(view))
	if limits.none() {
		return false, "No finite seed target is configured, so there is no deadline to miss"
	}
	targets := []string{}
	if limits.Ratio != nil {
		targets = append(targets, fmt.Sprintf("ratio %.2f", *limits.Ratio))
	}
	if limits.Seconds != nil {
		targets = append(targets, "time "+logDuration(time.Duration(*limits.Seconds)*time.Second))
	}
	return true, "Seed targets are finite (" + strings.Join(targets, ", ") + "): if this tracker enforces hit-and-run rules, stopping early can be reported"
}

// crossSeedSavePath is the folder holding the verified copy. The engine points
// the new torrent there: it never downloads the payload to a second folder.
func crossSeedSavePath(target crossSeedTarget) (string, error) {
	path := strings.TrimSpace(target.ProcessedPath)
	if path == "" {
		return "", errors.New("the archived copy has no recorded path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("the archived copy cannot be read: %w", err)
	}
	if info.IsDir() {
		return path, nil
	}
	return filepath.Dir(path), nil
}

// crossSeedAddCandidate adds the exact-hash copy paused at metadata. SeedMode
// is never used: skipping the hash check would make an unverified file look
// complete, which is the opposite of this feature's contract.
func crossSeedAddCandidate(ctx context.Context, engine TorrentEngine, cfg *Config, candidate crossSeedCandidate, savePath string) (string, error) {
	options := AddOptions{Paused: true, StopAtMetadata: true}
	release := candidate.Release
	if strings.HasPrefix(strings.TrimSpace(release.Magnet), "magnet:?") {
		added, err := engine.AddWithOptions(release.Magnet, cfg, &savePath, options)
		if err != nil {
			return "", err
		}
		if !added {
			return "", errors.New("the torrent was already in the session")
		}
		return releaseInfoHash(&release), nil
	}
	if release.TorrentURL == nil || strings.TrimSpace(*release.TorrentURL) == "" {
		return "", errors.New("this source exposes neither a magnet nor a torrent link")
	}
	payload, _, err := HTTPGetBytes(ctx, *release.TorrentURL, nil)
	if err != nil {
		return "", err
	}
	hash, ok := utils.TorrentInfoHash(payload)
	if !ok {
		return "", errors.New("invalid torrent payload")
	}
	dir := filepath.Join(cfg.StateDir, "feed_torrents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, "cross-seed-*.torrent")
	if err != nil {
		return "", err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(payload); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if _, err := engine.AddTorrentFileWithOptions(name, cfg, &savePath, options); err != nil {
		return "", err
	}
	return hash, nil
}

func crossSeedView(engine TorrentEngine, hash string) (models.TorrentView, bool) {
	for _, view := range engine.List() {
		if view.Hash == hash {
			return view, true
		}
	}
	return models.TorrentView{}, false
}

// crossSeedStart performs one explicit cross-seed: add at metadata, point the
// torrent at the copy already in the library, verify every piece against the
// torrent and resume only when the local data is complete. No payload is
// downloaded and no other copy of the data is created.
func crossSeedStart(ctx context.Context, s *AppState, rawHash, source, title string, confirmed bool) (map[string]any, error) {
	cfg := latestConfig(s)
	engine := s.activeEngine()
	if cfg == nil || engine == nil {
		return nil, errors.New("no transfer engine is available")
	}
	_, candidates, err := discoverCrossSeedDetailed(ctx, s.db, cfg, rawHash)
	if err != nil {
		return nil, err
	}
	target, err := crossSeedTargetFor(s.db, rawHash)
	if err != nil {
		return nil, err
	}
	var chosen *crossSeedCandidate
	for i := range candidates {
		if candidates[i].Source == source && candidates[i].Title == title {
			chosen = &candidates[i]
			break
		}
	}
	if chosen == nil {
		return nil, errors.New("the copy is no longer available; search again")
	}
	if !crossSeedAllowed(cfg, &chosen.crossSeedMatch, &target) {
		return nil, errors.New("this source is not enabled for cross-seed; add it to a seed rule first")
	}
	if risk, note := crossSeedHnRNote(cfg, s.db, target, chosen.crossSeedMatch); risk && !confirmed {
		return nil, fmt.Errorf("%w: %s", errCrossSeedHnRConfirmation, note)
	}
	savePath, err := crossSeedSavePath(target)
	if err != nil {
		return nil, err
	}
	hash := canonicalInfoHash(rawHash)
	if view, ok := crossSeedView(engine, hash); ok && view.IsSeeding {
		return map[string]any{"status": "already_seeding", "hash": hash}, nil
	}
	if _, ok := crossSeedView(engine, hash); ok {
		return nil, errors.New("a torrent with this infohash is already in the session")
	}
	hash, err = crossSeedAddCandidate(ctx, engine, cfg, *chosen, savePath)
	if err != nil {
		return nil, err
	}
	found := false
	for i := 0; i < 60; i++ {
		if view, ok := crossSeedView(engine, hash); ok && view.HasMetadata {
			found = true
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if !found {
		return nil, errors.New("the torrent metadata did not arrive; the copy was left paused")
	}
	associated, err := engine.AssociateStorage(hash, savePath)
	if err != nil {
		return nil, err
	}
	if !associated {
		return nil, errors.New("the backend could not associate the library copy")
	}
	view, ok := crossSeedView(engine, hash)
	if !ok || view.PiecesTotal <= 0 || view.PiecesHave < view.PiecesTotal {
		logging.Info(fmt.Sprintf("⏸️ «%s» was left paused: its local copy does not match this torrent; no payload was downloaded", chosen.Title))
		return map[string]any{"status": "kept_paused", "hash": hash}, nil
	}
	if _, err := engine.Resume(hash); err != nil {
		return nil, err
	}
	logging.Info(fmt.Sprintf("🌱 «%s» is seeding from the copy already in the library; nothing was downloaded again", chosen.Title), "hash", hash)
	return map[string]any{"status": "seeding", "hash": hash}, nil
}

// CrossSeedSeed implements POST /api/library/cross-seed/seed.
func CrossSeedSeed(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input struct {
		Hash      string `json:"hash"`
		Source    string `json:"source"`
		Title     string `json:"title"`
		Confirmed bool   `json:"confirmed"`
	}
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := crossSeedStart(r.Context(), s, input.Hash, input.Source, input.Title, input.Confirmed)
	if errors.Is(err, errCrossSeedTargetNotFound) {
		jsonError(w, http.StatusNotFound, "no completed torrent with this infohash")
		return
	}
	if err != nil {
		jsonError(w, http.StatusConflict, utils.RedactURLSecrets(err.Error()))
		return
	}
	jsonResponse(w, result)
}
