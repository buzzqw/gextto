package rss

import (
	"regexp"
	"sync"
)

// regex_cache.go keeps the rule patterns compiled. Criteria.Match runs for
// every item of every feed on every poll, and compiling the same rule regexp
// each time cost more than the rest of the match put together. A regexp is safe
// for concurrent use, so one compiled copy serves every caller.

// regexCacheLimit bounds the cache: the patterns come from the rules and the
// wanted entries, a few dozen in practice. Past the limit the cache starts over
// instead of growing with patterns typed in the editor and then discarded.
const regexCacheLimit = 256

type regexCacheEntry struct {
	re  *regexp.Regexp
	err error
}

var regexCache = struct {
	mu      sync.Mutex
	entries map[string]regexCacheEntry
}{entries: map[string]regexCacheEntry{}}

// compiledRegex returns pattern compiled, from the cache when it was seen
// before. An invalid pattern keeps its error, so it is not recompiled either.
func compiledRegex(pattern string) (*regexp.Regexp, error) {
	regexCache.mu.Lock()
	entry, ok := regexCache.entries[pattern]
	regexCache.mu.Unlock()
	if ok {
		return entry.re, entry.err
	}
	re, err := regexp.Compile(pattern)
	regexCache.mu.Lock()
	if len(regexCache.entries) >= regexCacheLimit {
		clear(regexCache.entries)
	}
	regexCache.entries[pattern] = regexCacheEntry{re: re, err: err}
	regexCache.mu.Unlock()
	return re, err
}

// CompiledRegex is compiledRegex for the callers outside the package that
// match the same patterns item after item (the wanted entries of the daemon).
func CompiledRegex(pattern string) (*regexp.Regexp, error) {
	return compiledRegex(pattern)
}
