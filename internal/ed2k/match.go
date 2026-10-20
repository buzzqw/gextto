package ed2k

// match.go filters search results by name. eD2k and Kad index files per
// keyword, not by phrase, so a search for "Tracker S04E01" also returns
// other episodes and unrelated files that share one word: the caller keeps
// only the names that contain every searchable token of the query.

import (
	"regexp"
	"strings"
)

// SearchTerms returns the query tokens without stop words.
func SearchTerms(query string) []string {
	all := QueryTokens(query)
	out := make([]string, 0, len(all))
	for _, token := range all {
		if _, stop := stopWords[token]; stop {
			continue
		}
		out = append(out, token)
	}
	return out
}

var stopWords = map[string]struct{}{
	"the": {}, "and": {}, "of": {}, "for": {}, "with": {},
	"di": {}, "da": {}, "il": {}, "lo": {}, "la": {}, "le": {}, "les": {},
	"el": {}, "los": {}, "las": {}, "der": {}, "die": {}, "das": {},
	"und": {}, "et": {}, "de": {}, "del": {}, "della": {}, "dei": {},
	"degli": {}, "un": {}, "una": {}, "uno": {}, "per": {}, "con": {},
	"von": {}, "van": {},
}

var episodeToken = regexp.MustCompile(`^s\d{1,2}[ ._-]?e\d{1,3}$|^\d{1,2}x\d{1,3}$`)

// QueryTokens returns the distinct usable tokens of a query, used to make
// sure a result actually matches what the caller asked for.
func QueryTokens(query string) []string {
	invalid := " ()[]{}<>,._-!?:;\\/\"'"
	words := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return strings.ContainsRune(invalid, r)
	})
	seen := map[string]struct{}{}
	out := make([]string, 0, len(words))
	for _, word := range words {
		if len([]byte(word)) < 3 {
			continue
		}
		if _, ok := seen[word]; ok {
			continue
		}
		seen[word] = struct{}{}
		out = append(out, word)
	}
	return out
}

func queryHasEpisode(query string) bool {
	for _, token := range SearchTerms(query) {
		if episodeToken.MatchString(token) {
			return true
		}
	}
	return false
}

// MatchesQuery reports whether a file name contains every searchable token of
// the query. Results are indexed per word, not by phrase, so a client-side
// filter is required to avoid showing unrelated files. The token
// list matches SearchTerms, so a name without "The" still matches "The Matrix".
func MatchesQuery(name, query string) bool {
	lower := strings.ToLower(name)
	for _, token := range SearchTerms(query) {
		if episodeToken.MatchString(token) {
			if !episodeInName(lower, token) {
				return false
			}
			continue
		}
		if !strings.Contains(lower, token) {
			return false
		}
	}
	return true
}

var episodeInNamePattern = regexp.MustCompile(`(?i)s(\d{1,2})[ ._-]?e(\d{1,3})`)
var episodeXInNamePattern = regexp.MustCompile(`(?i)(?:^|[^0-9])(\d{1,2})x(\d{1,3})(?:[^0-9]|$)`)

// episodeInName accepts the common episode spellings: S04E01, S04.E01,
// S04 E01 and 4x01/04x01. It is used both for the query token and for the
// file name.
func episodeInName(name, token string) bool {
	season, episode, ok := parseEpisodeToken(token)
	if !ok {
		return false
	}
	for _, match := range episodeInNamePattern.FindAllStringSubmatch(name, -1) {
		if atoiSmall(match[1]) == season && atoiSmall(match[2]) == episode {
			return true
		}
	}
	for _, match := range episodeXInNamePattern.FindAllStringSubmatch(name, -1) {
		if atoiSmall(match[1]) == season && atoiSmall(match[2]) == episode {
			return true
		}
	}
	return false
}

func parseEpisodeToken(token string) (int, int, bool) {
	if match := regexp.MustCompile(`^s(\d{1,2})[ ._-]?e(\d{1,3})$`).FindStringSubmatch(strings.ToLower(token)); match != nil {
		return atoiSmall(match[1]), atoiSmall(match[2]), true
	}
	if match := regexp.MustCompile(`^(\d{1,2})x(\d{1,3})$`).FindStringSubmatch(strings.ToLower(token)); match != nil {
		return atoiSmall(match[1]), atoiSmall(match[2]), true
	}
	return 0, 0, false
}

func atoiSmall(value string) int {
	n := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}
