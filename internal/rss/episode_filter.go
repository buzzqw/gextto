package rss

// episode_filter.go implements a qBittorrent-style episode filter for series
// rules: one or more season/episode specs, comma- or semicolon-separated, that a
// title's season/episode must fall into. Supported forms (case-insensitive):
//
//	1x01           a single episode
//	1x01-05        a range within one season
//	1x01-1x05      a range across seasons (expanded)
//	1x01-          open-ended: episode 1 and later of that season
//	1x??  1x*      every episode of that season
//	S01E01 …       the SxxExx form is accepted too
//
// The filter matches only titles that carry a recognisable season/episode
// marker, like qBittorrent's episode filter.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// EpisodeFilter is a parsed episode filter; the zero value matches everything.
type EpisodeFilter struct {
	groups []episodeRange
}

// episodeRange is one season with an episode range: a single episode (from==to),
// a range, an open-ended range (from..inf) or a whole season (any).
type episodeRange struct {
	season int
	from   int
	to     int
	any    bool
	open   bool
}

// Empty reports whether the filter has no constraints.
func (f EpisodeFilter) Empty() bool { return len(f.groups) == 0 }

// sxxexx matches the "S01E02" spelling anywhere in a spec.
var sxxexx = regexp.MustCompile(`(?i)s(\d{1,4})e(\d{1,4})`)

// ParseEpisodeFilter reads the friendly episode syntax. A blank value yields an
// empty filter (matches every episode); a malformed spec is an error.
func ParseEpisodeFilter(raw string) (EpisodeFilter, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return EpisodeFilter{}, nil
	}
	// Accept SxxExx so a user can paste a normal episode name.
	normalized := sxxexx.ReplaceAllString(raw, "${1}x${2}")

	var filter EpisodeFilter
	for _, token := range strings.FieldsFunc(normalized, func(r rune) bool { return r == ',' || r == ';' }) {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		groups, err := parseEpisodeToken(token)
		if err != nil {
			return EpisodeFilter{}, err
		}
		filter.groups = append(filter.groups, groups...)
	}
	return filter, nil
}

// isEpisodeWildcard reports whether a spec part means "every episode".
func isEpisodeWildcard(value string) bool {
	if value == "*" {
		return true
	}
	return value != "" && strings.Trim(value, "?") == ""
}

// parseEpisodeToken parses one "SxE[-E]", "SxE-SxE", "SxE-" or wildcard spec.
// A cross-season range expands to a whole-season open range for the first
// season, the full seasons in between and a bounded range for the last.
func parseEpisodeToken(token string) ([]episodeRange, error) {
	lower := strings.ToLower(strings.TrimSpace(token))
	index := strings.IndexByte(lower, 'x')
	if index <= 0 {
		return nil, fmt.Errorf("episode filter %q is not in the SxE form", token)
	}
	season, err := strconv.Atoi(strings.TrimSpace(lower[:index]))
	if err != nil || season < 0 {
		return nil, fmt.Errorf("episode filter %q has an invalid season", token)
	}
	rest := strings.TrimSpace(lower[index+1:])

	left, right, hasRange := strings.Cut(rest, "-")
	left = strings.TrimSpace(left)

	// Whole-season wildcard: "1x*" / "1x??".
	if isEpisodeWildcard(left) {
		return []episodeRange{{season: season, any: true}}, nil
	}
	from, err := strconv.Atoi(left)
	if err != nil || from <= 0 {
		return nil, fmt.Errorf("episode filter %q has an invalid episode", token)
	}
	if !hasRange {
		return []episodeRange{{season: season, from: from, to: from}}, nil
	}

	right = strings.TrimSpace(right)
	// Open-ended: "1x05-" or "1x05-*".
	if right == "" || isEpisodeWildcard(right) {
		return []episodeRange{{season: season, from: from, open: true}}, nil
	}
	// Same-season range: "1x05-08".
	if !strings.Contains(right, "x") {
		to, err := strconv.Atoi(right)
		if err != nil || to <= 0 {
			return nil, fmt.Errorf("episode filter %q has an invalid range", token)
		}
		if from > to {
			return nil, fmt.Errorf("episode filter %q has a reversed range", token)
		}
		return []episodeRange{{season: season, from: from, to: to}}, nil
	}
	// Cross-season range: "1x05-3x02".
	rightIndex := strings.IndexByte(right, 'x')
	season2, err1 := strconv.Atoi(strings.TrimSpace(right[:rightIndex]))
	to, err2 := strconv.Atoi(strings.TrimSpace(right[rightIndex+1:]))
	if err1 != nil || err2 != nil || season2 < season || to <= 0 {
		return nil, fmt.Errorf("episode filter %q has an invalid range", token)
	}
	if season2 == season {
		if from > to {
			return nil, fmt.Errorf("episode filter %q has a reversed range", token)
		}
		return []episodeRange{{season: season, from: from, to: to}}, nil
	}
	groups := []episodeRange{{season: season, from: from, open: true}}
	for mid := season + 1; mid < season2; mid++ {
		groups = append(groups, episodeRange{season: mid, any: true})
	}
	groups = append(groups, episodeRange{season: season2, from: 1, to: to})
	return groups, nil
}

// Match reports whether the title's season/episode falls into the filter. An
// empty filter matches everything; a filter never matches a title without a
// recognisable season/episode marker.
func (f EpisodeFilter) Match(title string) bool {
	if f.Empty() {
		return true
	}
	_, season, episode, ok := FindEpisode(title)
	if !ok {
		return false
	}
	for _, group := range f.groups {
		if group.season != season {
			continue
		}
		switch {
		case group.any:
			return true
		case group.open:
			if episode >= group.from {
				return true
			}
		case episode >= group.from && episode <= group.to:
			return true
		}
	}
	return false
}
