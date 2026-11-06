package gextto

import (
	"regexp"
	"strings"
	"testing"
)

// The scans in ParseQuality replaced a set of regexes. These tests keep the
// old patterns as the reference and compare them on a corpus built from the
// tags that appear in real release names: a scan that drifts from the regex
// changes the language, the resolution or the subtitle flags silently.

var (
	refQualitySeparator = `[._ \-\[\]\(\)+]`
	refSubtitleMarker   = `(?:subs?|subforced|subtitles?|forced|sdh|cc|closedcaptions?)`
	refSubtitleSep      = `[\s._\-/\\\[\]()+|,:;]*`
	refSubtitleLang     = `(?:[a-z]{2,3}|english|italian|italiano|german|deutsch|french|francais|spanish|portuguese|japanese|chinese|korean|russian)`
	refSubtitleTags     = `\b` + refSubtitleMarker + refSubtitleSep +
		`(?:` + refSubtitleLang + refSubtitleSep + `)*(?:it|ita|italian|italiano)\b|\b(?:it|ita|italian|italiano)` +
		refSubtitleSep + refSubtitleMarker + `\b`
	refReSubtitle   = regexp.MustCompile(refSubtitleTags)
	refReNormalize  = regexp.MustCompile(`[._ \-\[\]]`)
	refReItWeb      = regexp.MustCompile(`(?i)\b(it|nf)(\s+web)\b`)
	refReStreaming  = regexp.MustCompile(`(?i)\bitunes\b|\bamzn\b|\bdsnp\b|\bhmax\b|\bparamount\b`)
	refReITPrefix   = regexp.MustCompile(`\bit[\+\|]|\[it\]`)
	refReResolution = regexp.MustCompile(`\b(2160p?|1080p?|720p?|480p?|4k|uhd|bluray|web[\s\-]?dl|webrip|hdtv)\b`)
	refReGroup      = regexp.MustCompile(`[-]([a-z0-9]+)$|\[([a-z0-9]+)\]$`)
)

func refWholeWord(text, word string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`).MatchString(text)
}

func refSeparatedToken(text string, tokens ...string) bool {
	pattern := `(?i)(^|` + refQualitySeparator + `)(` + strings.Join(tokens, "|") + `)(` + refQualitySeparator + `|$)`
	return regexp.MustCompile(pattern).MatchString(text)
}

func refSubtitleMarkerLanguage(text string, languages ...string) bool {
	pattern := `(?i)(sub|subs|subtitle|subtitles)` + refQualitySeparator + `*(` + strings.Join(languages, "|") + `)(` + refQualitySeparator + `|$)`
	return regexp.MustCompile(pattern).MatchString(text)
}

func refStripStreaming(text string) string {
	withoutItWeb := refReItWeb.ReplaceAllString(text, "$2")
	return refReStreaming.ReplaceAllString(withoutItWeb, " ")
}

func qualityCorpus() []string {
	bases := []string{
		"The.Pitt.S01E01.Pilot.1080p.WEB-DL.DDP5.1.H.264-ABC",
		"Movie.2024.2160p.UHD.BluRay.REMUX.DV.HDR10.ITA.ENG.DTS-HD.x265-GROUP",
		"Show.S02E01-05.720p.HDTV.x264",
		"Another.Movie.2021.1080p.WEBRip.AAC2.0.x264",
		"Daily.Show.2026-09-21.WEB-DL.1080p",
		"Film.2019.2160p.BluRay.REMUX",
	}
	tags := []string{
		"ITA", "It", "iT", "It+", "It|", "[IT]", "ITA-ENG", "SubIta", "Sub.ITA",
		"Subs.Ita", "Subs.Eng.Ita", "Ita.Sub", "Forced", "SDH", "CC", "ClosedCaptions",
		"iT.WEB-DL", "NF.WEB", "iTunes", "AMZN", "DSNP", "HMAX", "Paramount",
		"HDR", "HLG", "Real", "Hardcoded", "HC", "Hardsub", "ENG", "English", "EN",
		"With", "Bit", "Split", "Edit", "Unit", "Visit", "Limit", "Exit", "Profit",
		"Submit", "Commit", "Permit", "Admit", "Omit", "Hit", "Kit", "Pit", "Sit",
		"Fit", "Wit", "Knit", "Spit", "Slit", "Itunes", "Sub", "Subs", "Subtitles",
		"itaSub", "italianSubs", "itaSdh", "itaForced", "engSub",
		"2160p", "1080p", "720p", "480p", "4k", "UHD", "BluRay", "WEB-DL", "WEBRip",
		"HDTV", "Sub.Eng", "Sub.Eng.Ita", "German", "French", "Spanish",
	}
	separators := []string{".", "-", "_", " ", "[", "]", "(", ")", "+", "/"}
	corpus := append([]string{}, bases...)
	for _, base := range bases {
		for _, tag := range tags {
			corpus = append(corpus, base+"."+tag)
			corpus = append(corpus, tag+"."+base)
		}
	}
	for _, sep := range separators {
		for _, tag := range []string{"ITA", "It", "It+", "SubIta", "Sub.ITA", "ENG", "Sub.Eng.Ita", "iT.WEB-DL"} {
			corpus = append(corpus, "Show.S01E01"+sep+tag+sep+"1080p"+sep+"WEB-DL")
		}
	}
	return corpus
}

func TestQualityScansMatchTheReferenceRegexes(t *testing.T) {
	for _, title := range qualityCorpus() {
		low := strings.ToLower(title)

		norm := normalizeQualitySeparators(low)
		if want := refReNormalize.ReplaceAllString(low, " "); norm != want {
			t.Fatalf("%q: normalized %q, want %q", title, norm, want)
		}

		if got, want := stripSubtitleTags(norm), refReSubtitle.ReplaceAllString(norm, " "); got != want {
			t.Fatalf("%q: subtitle rewrite %q, want %q", title, got, want)
		}
		if got, want := stripSubtitleTags(low), refReSubtitle.ReplaceAllString(low, " "); got != want {
			t.Fatalf("%q: subtitle rewrite (raw) %q, want %q", title, got, want)
		}

		wantGroup := ""
		if match := refReGroup.FindStringSubmatch(low); match != nil {
			wantGroup = match[1]
			if wantGroup == "" {
				wantGroup = match[2]
			}
		}
		if got := releaseGroupFromTail(low); got != wantGroup {
			t.Fatalf("%q: release group %q, want %q", title, got, wantGroup)
		}

		stripped := stripStreamingTags(norm)
		wantStripped := strings.Join(strings.Fields(refStripStreaming(norm)), " ")
		if got := strings.Join(strings.Fields(stripped), " "); got != wantStripped {
			t.Fatalf("%q: stripped %q, want %q", title, got, wantStripped)
		}

		if got, want := hasAnyWholeWord(stripped, "ita", "italian", "italiano"),
			refWholeWord(stripped, "ita") || refWholeWord(stripped, "italian") || refWholeWord(stripped, "italiano"); got != want {
			t.Fatalf("%q: level1 = %v, want %v (%q)", title, got, want, stripped)
		}

		if got, want := hasITPrefix(low), refReITPrefix.MatchString(low); got != want {
			t.Fatalf("%q: it prefix = %v, want %v", title, got, want)
		}

		if got, want := hasSubtitleMarkerLanguage(low, "ita", "italian", "it"),
			refSubtitleMarkerLanguage(low, "ita", "italian", "it"); got != want {
			t.Fatalf("%q: subtitle ita = %v, want %v", title, got, want)
		}
		if got, want := hasSubtitleMarkerLanguage(low, "eng", "english", "en"),
			refSubtitleMarkerLanguage(low, "eng", "english", "en"); got != want {
			t.Fatalf("%q: subtitle eng = %v, want %v", title, got, want)
		}

		if got, want := hasSeparatedToken(low, "eng", "english", "en"),
			refSeparatedToken(low, "eng", "english", "en"); got != want {
			t.Fatalf("%q: eng = %v, want %v", title, got, want)
		}
		if got, want := hasSeparatedToken(low, "sub", "subs", "subtitle", "subtitles"),
			refSeparatedToken(low, "sub", "subs", "subtitle", "subtitles"); got != want {
			t.Fatalf("%q: subtitle flag = %v, want %v", title, got, want)
		}
		if got, want := hasSeparatedToken(low, "hc", "hardcoded", "hardsub", "hardsubs"),
			refSeparatedToken(low, "hc", "hardcoded", "hardsub", "hardsubs"); got != want {
			t.Fatalf("%q: hardcoded = %v, want %v", title, got, want)
		}

		if got, want := hasWholeWord(low, "real"), refWholeWord(low, "real"); got != want {
			t.Fatalf("%q: real = %v, want %v", title, got, want)
		}
		if got, want := hasAnyWholeWord(norm, "hdr", "hlg"),
			refWholeWord(norm, "hdr") || refWholeWord(norm, "hlg"); got != want {
			t.Fatalf("%q: hdr = %v, want %v", title, got, want)
		}

		wantIndex := -1
		if found := refReResolution.FindStringIndex(norm); found != nil {
			wantIndex = found[0]
		}
		if got := findResolutionIndex(norm); got != wantIndex {
			t.Fatalf("%q: resolution index = %d, want %d", title, got, wantIndex)
		}
	}
}
