package main

// wanted_metadata.go keeps the real episode list of a wanted series. TVmaze's
// public API needs no key and gives the seasons and episodes with their air
// dates; the snapshot lives in the state file and is refreshed at most once a
// day, so the page never makes a network call while it renders.

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/tvmaze"
)

const (
	metadataEpisodesKey = "metadata-episodes"
	metadataTTL         = 24 * time.Hour
	metadataRetry       = 2 * time.Hour
	metadataPerCycle    = 3
)

// wantedMetadata is the cached episode list of one title.
type wantedMetadata struct {
	Source    string           `json:"source,omitempty"`
	ID        int              `json:"id,omitempty"`
	Name      string           `json:"name,omitempty"`
	Episodes  []tvmaze.Episode `json:"episodes,omitempty"`
	FetchedAt int64            `json:"fetched_at,omitempty"`
	LastTry   int64            `json:"last_try,omitempty"`
	NoMatch   bool             `json:"no_match,omitempty"`
	Error     string           `json:"error,omitempty"`
}

// metadataEpisodesEnabled reports whether the real episode list is on. It is
// on by default in standalone; turning it off leaves the page on the
// feed/indexer ledger alone.
func (d *Daemon) metadataEpisodesEnabled() bool {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(d.opts.Settings.Get(metadataEpisodesKey, "false")), "true")
}

// wantedMetadataFor returns the cached snapshot for a title.
func (d *Daemon) wantedMetadataFor(name string) wantedMetadata {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state.WantedMeta[strings.TrimSpace(name)]
}

// refreshWantedMetadata resolves a title on TVmaze and stores its episode list.
// It returns true when it actually attempted a request. The caller can pass
// force to ignore the TTL (the add/edit action), otherwise a title is refreshed
// once a day and a failed match is retried after metadataRetry.
func (d *Daemon) refreshWantedMetadata(ctx context.Context, w wantedEntry, force bool) bool {
	if !d.metadataEpisodesEnabled() || d.metadata == nil || w.kind() == "movie" {
		return false
	}
	key := strings.TrimSpace(w.Name)
	if key == "" {
		return false
	}
	now := time.Now()
	meta := d.wantedMetadataFor(key)
	if !force {
		if meta.ID > 0 && meta.FetchedAt > 0 && now.Sub(time.Unix(meta.FetchedAt, 0)) < metadataTTL {
			return false
		}
		if meta.LastTry > 0 && now.Sub(time.Unix(meta.LastTry, 0)) < metadataRetry {
			return false
		}
	}
	query := titleSearchText(w.Name)
	if query == "" {
		return false
	}
	searchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	meta.LastTry = now.Unix()
	shows, err := d.metadata.Search(searchCtx, query)
	if err != nil {
		d.saveWantedMetadata(key, meta, err)
		return true
	}
	show, ok := bestMetadataShow(query, shows)
	if !ok {
		meta.NoMatch = true
		meta.Error = ""
		d.saveWantedMetadata(key, meta, nil)
		return true
	}
	episodes, err := d.metadata.Episodes(searchCtx, show.ID)
	if err != nil {
		d.saveWantedMetadata(key, meta, err)
		return true
	}
	meta.Source = "tvmaze"
	meta.ID = show.ID
	meta.Name = show.Name
	meta.Episodes = episodes
	meta.FetchedAt = now.Unix()
	meta.NoMatch = false
	meta.Error = ""
	d.saveWantedMetadata(key, meta, nil)
	return true
}

func (d *Daemon) saveWantedMetadata(key string, meta wantedMetadata, err error) {
	if err != nil {
		meta.Error = err.Error()
	}
	d.mu.Lock()
	if d.state.WantedMeta == nil {
		d.state.WantedMeta = map[string]wantedMetadata{}
	}
	d.state.WantedMeta[key] = meta
	d.dirty = true
	d.mu.Unlock()
	if err != nil {
		logfError("wanted %q: cannot fetch the episode list: %v", key, err)
	}
}

// bestMetadataShow picks the closest name, preferring an exact normalized
// match; TVmaze orders its results by score, so the first is the fallback.
func bestMetadataShow(query string, shows []tvmaze.Show) (tvmaze.Show, bool) {
	normalize := func(s string) string {
		return strings.Join(strings.Fields(strings.ToLower(s)), " ")
	}
	want := normalize(query)
	for _, show := range shows {
		if normalize(show.Name) == want {
			return show, true
		}
	}
	if len(shows) > 0 {
		return shows[0], true
	}
	return tvmaze.Show{}, false
}

// wantedMetadataEpisodes lists the real, already-aired episodes inside the
// wanted seasons. A title without a TVmaze match returns nothing and the page
// keeps using the ledger.
func (d *Daemon) wantedMetadataEpisodes(w wantedEntry, now time.Time) []wantedEpisode {
	if !d.metadataEpisodesEnabled() || w.kind() == "movie" {
		return nil
	}
	meta := d.wantedMetadataFor(w.Name)
	if meta.ID == 0 || len(meta.Episodes) == 0 {
		return nil
	}
	out := make([]wantedEpisode, 0, len(meta.Episodes))
	for _, episode := range meta.Episodes {
		if episode.Season <= 0 || episode.Number <= 0 || !seasonAllowed(w.Seasons, episode.Season) {
			continue
		}
		if !episode.Aired(now) {
			continue
		}
		out = append(out, wantedEpisode{season: episode.Season, episode: episode.Number})
	}
	return out
}

// wantedMetadataChip is the row chip: it says whether the real list is in use
// and how many episodes it knows.
func (d *Daemon) wantedMetadataChip(w wantedEntry) string {
	if !d.metadataEpisodesEnabled() || w.kind() == "movie" {
		return ""
	}
	meta := d.wantedMetadataFor(w.Name)
	switch {
	case meta.ID > 0 && len(meta.Episodes) > 0:
		return "TVmaze " + strconv.Itoa(len(meta.Episodes))
	case meta.NoMatch:
		return "TVmaze: not matched"
	case meta.Error != "":
		return "TVmaze: error"
	default:
		return "TVmaze: not matched"
	}
}
