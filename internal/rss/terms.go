package rss

// terms.go is the one way a filter term is found in a release title, shared
// by the rules (Include, Require, Exclude), the per-feed filter and the wanted
// titles' languages.
//
// Release names are written with dots, underscores and dashes as often as
// with spaces ("The.Office.US.S03E01", "SUB-ITA"), and a filter is typed with
// spaces: both sides are normalized, so "the office" finds "The.Office" and
// "sub ita" finds "SUB.ITA". A term must also start where a word starts, so
// "ita" finds "ITA" and "Italian" but not "Capitan", "eng" finds "English"
// and "1080" finds "1080p".
//
// The match scans the title in place, jumping to the occurrences of the
// term's first letter with strings.IndexByte: no string is built, so a rule
// over thousands of archived items allocates nothing (docs/measurements.md).

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// isTermSep reports whether r separates words in a release name or a term.
func isTermSep(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '.', '_', '-', '[', ']', '(', ')', '{', '}', ',', '+', '/':
		return true
	}
	return false
}

// foldRune lowers r, fast for ASCII.
func foldRune(r rune) rune {
	if r < utf8.RuneSelf {
		if 'A' <= r && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}
	return unicode.ToLower(r)
}

// trimTermSeps drops the separators around a term, so " ita " is "ita".
func trimTermSeps(term string) string {
	start, end := 0, len(term)
	for start < end {
		r, size := utf8.DecodeRuneInString(term[start:])
		if !isTermSep(r) {
			break
		}
		start += size
	}
	for end > start {
		r, size := utf8.DecodeLastRuneInString(term[start:end])
		if !isTermSep(r) {
			break
		}
		end -= size
	}
	return term[start:end]
}

// HasTerm reports whether the release title contains the filter term, with
// the separators of both normalized and the term starting at a word start. An
// empty term is not found.
func HasTerm(title, term string) bool {
	term = trimTermSeps(term)
	if term == "" {
		return false
	}
	first := term[0]
	if first >= utf8.RuneSelf {
		return hasTermScan(title, term)
	}
	lower, upper := first, first
	if 'a' <= first && first <= 'z' {
		upper = first - ('a' - 'A')
	} else if 'A' <= first && first <= 'Z' {
		lower = first + ('a' - 'A')
	}
	for i := 0; i < len(title); {
		// The next occurrence of the first letter, in either case.
		j := strings.IndexByte(title[i:], lower)
		if upper != lower {
			if k := strings.IndexByte(title[i:], upper); k >= 0 && (j < 0 || k < j) {
				j = k
			}
		}
		if j < 0 {
			return false
		}
		at := i + j
		if wordStart(title, at) && termAt(title[at:], term) {
			return true
		}
		i = at + 1
	}
	return false
}

// wordStart reports whether position i of s starts a word.
func wordStart(s string, i int) bool {
	if i == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return isTermSep(r)
}

// hasTermScan is HasTerm for a term starting with a non-ASCII letter.
func hasTermScan(title, term string) bool {
	prevSep := true
	for i := 0; i < len(title); {
		r, size := utf8.DecodeRuneInString(title[i:])
		sep := isTermSep(r)
		if prevSep && !sep && termAt(title[i:], term) {
			return true
		}
		prevSep = sep
		i += size
	}
	return false
}

// termAt reports whether title starts with term, a run of separators on
// either side matching any run on the other, case-insensitively.
func termAt(title, term string) bool {
	ti, ki := 0, 0
	for ki < len(term) {
		if ti >= len(title) {
			return false
		}
		kr, ksize := rune(term[ki]), 1
		if kr >= utf8.RuneSelf {
			kr, ksize = utf8.DecodeRuneInString(term[ki:])
		}
		tr, tsize := rune(title[ti]), 1
		if tr >= utf8.RuneSelf {
			tr, tsize = utf8.DecodeRuneInString(title[ti:])
		}
		if isTermSep(kr) {
			if !isTermSep(tr) {
				return false
			}
			ki = skipTermSeps(term, ki)
			ti = skipTermSeps(title, ti)
			continue
		}
		if foldRune(kr) != foldRune(tr) {
			return false
		}
		ki += ksize
		ti += tsize
	}
	return true
}

// skipTermSeps returns the index after the run of separators at i.
func skipTermSeps(s string, i int) int {
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		if !isTermSep(r) {
			break
		}
		i += n
	}
	return i
}

// hasAnyTerm reports whether the title contains at least one of the terms;
// blank terms are ignored and a list of only blanks is not a match.
func hasAnyTerm(title string, terms []string) bool {
	for _, term := range terms {
		if HasTerm(title, term) {
			return true
		}
	}
	return false
}

// countTerms is how many non-blank terms the list holds.
func countTerms(terms []string) int {
	n := 0
	for _, term := range terms {
		if strings.TrimSpace(term) != "" {
			n++
		}
	}
	return n
}
