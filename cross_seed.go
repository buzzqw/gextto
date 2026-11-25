package gextto

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

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
		target := crossSeedTarget{Hash: hash, Title: title, Kind: kind, Series: series, Season: season, Episode: episode, Year: year, CompletedAt: completedAt}
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
		return crossSeedTarget{Hash: hash, Title: episodeTitle, Kind: "series", Series: series, Season: season, Episode: episode, CompletedAt: episodeDownloaded}, nil
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
	target, err := crossSeedTargetFor(db, rawHash)
	if err != nil {
		return crossSeedResult{}, err
	}
	result := crossSeedResult{Target: target, Checked: []crossSeedSourceStatus{}, Matches: []crossSeedMatch{}}
	if cfg == nil {
		return result, fmt.Errorf("configuration unavailable")
	}
	searches := crossSeedSearches(cfg, target)
	if len(searches) == 0 {
		return result, fmt.Errorf("no title identity available for this torrent")
	}
	indexers := make([]IndexerConfig, 0, len(cfg.Indexers))
	for _, indexer := range cfg.Indexers {
		if indexer.Enabled && strings.TrimSpace(indexer.URL) != "" {
			indexers = append(indexers, indexer)
		}
	}
	if len(indexers) == 0 {
		return result, fmt.Errorf("no enabled indexer is configured")
	}

	searchCtx, cancel := context.WithTimeout(ctx, crossSeedSearchTimeout)
	defer cancel()
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var mu sync.Mutex
	matches := map[string]crossSeedMatch{}
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
					match := crossSeedMatch{
						Source: release.Source, Title: release.Title, SizeBytes: release.SizeBytes,
						Seeders: release.Seeders, Peers: release.Peers, Swarm: crossSeedSwarm(release),
						Freeleech: release.Freeleech, FreeleechKnown: release.FreeleechKnown,
						LinkType: crossSeedLinkType(release), TrackerHosts: crossSeedTrackerHosts(release),
					}
					if match.Source == "" {
						match.Source = indexer.Name
					}
					key := strings.ToLower(match.Source + "\x00" + match.Title + "\x00" + match.LinkType)
					mu.Lock()
					if previous, ok := matches[key]; !ok || match.Seeders > previous.Seeders {
						matches[key] = match
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
	result.Matches = make([]crossSeedMatch, 0, len(matches))
	for _, match := range matches {
		result.Matches = append(result.Matches, match)
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
	return result, nil
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
