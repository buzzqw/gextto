package gextto

import (
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// missingArchiveFlushAfter is how long the vanished files of one series are
// gathered before they are written as one line: a check of the library meets
// them all within a second or two.
const missingArchiveFlushAfter = 3 * time.Second

// missingArchiveEpisode is one library file found missing on disk.
type missingArchiveEpisode struct {
	season, episode int64
	path            string
}

// missingArchiveLog gathers the vanished files per series and writes one
// warning for each series instead of one per episode.
type missingArchiveLog struct {
	mu      sync.Mutex
	pending map[string][]missingArchiveEpisode
	write   func(series string, episodes []missingArchiveEpisode)
	after   time.Duration
}

var missingArchiveReports = &missingArchiveLog{write: logMissingArchiveEpisodes, after: missingArchiveFlushAfter}

// add records one vanished file; the first one of a series schedules the line.
func (m *missingArchiveLog) add(series string, season, episode int64, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil {
		m.pending = map[string][]missingArchiveEpisode{}
	}
	first := len(m.pending[series]) == 0
	m.pending[series] = append(m.pending[series], missingArchiveEpisode{season: season, episode: episode, path: path})
	if first {
		time.AfterFunc(m.after, func() { m.flush(series) })
	}
}

// flush writes the line of one series and forgets its files.
func (m *missingArchiveLog) flush(series string) {
	m.mu.Lock()
	episodes := m.pending[series]
	delete(m.pending, series)
	m.mu.Unlock()
	if len(episodes) > 0 {
		m.write(series, episodes)
	}
}

// missingArchiveText is the warning for the vanished files of one series, and
// the folder (or file) to name next to it.
func missingArchiveText(series string, episodes []missingArchiveEpisode) (string, string) {
	sorted := append([]missingArchiveEpisode(nil), episodes...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].season != sorted[j].season {
			return sorted[i].season < sorted[j].season
		}
		return sorted[i].episode < sorted[j].episode
	})
	if len(sorted) == 1 {
		e := sorted[0]
		return fmt.Sprintf("The library file of %s S%02dE%02d is no longer on disk, so the episode can be downloaded again",
			series, e.season, e.episode), e.path
	}
	names := make([]string, len(sorted))
	folder := filepath.Dir(sorted[0].path)
	for i, e := range sorted {
		names[i] = fmt.Sprintf("S%02dE%02d", e.season, e.episode)
		if filepath.Dir(e.path) != folder {
			folder = ""
		}
	}
	return fmt.Sprintf("%s of %s are no longer on disk (%s), so they can be downloaded again",
		countLabel(len(sorted), "library file", "library files"), series, listWithMore(names, 8)), folder
}

// listWithMore renders up to limit names, then "and N more".
func listWithMore(names []string, limit int) string {
	if len(names) <= limit {
		return joinWithAnd(names)
	}
	shown := append(append([]string(nil), names[:limit]...), fmt.Sprintf("%d more", len(names)-limit))
	return joinWithAnd(shown)
}

func logMissingArchiveEpisodes(series string, episodes []missingArchiveEpisode) {
	text, path := missingArchiveText(series, episodes)
	if path == "" {
		logging.Warn(text)
		return
	}
	logging.Warn(text, "path", path)
}
