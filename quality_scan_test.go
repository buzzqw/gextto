package gextto

import (
	"regexp"
	"slices"
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
		"Sub.German.Ita", "Subs.Spanish.Ita", "Sub.French.Ita", "Sub.Japanese.Ita",
		"Sub.Chinese.Ita", "Sub.Korean.Ita", "Sub.Portuguese.Ita", "Sub.Russian.Ita",
		"Sub.Italiano", "Sub.Italian", "Subs.Italiano", "Sub.Eng.German.Ita",
		"Sub.Eng.German.French.Spanish.Ita", "Ita.Subs.German", "Italian.Sub.Eng",
		"ClosedCaptions.Ita", "ClosedCaption.Ita", "CC.Ita", "SDH.Ita", "Forced.Ita",
		"Forced.Italian", "SUB.ITA", "Ita.Sub.Ita",
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

// TestParseSxxExxFastPathMatchesTheMatcherList keeps the old matcher loop as
// the reference for the SxxExx fast path: the fast path must never read a
// different shape, a different position or different values.
func TestParseSxxExxFastPathMatchesTheMatcherList(t *testing.T) {
	reference := func(title string) releaseMatch {
		for _, matcher := range releaseMatchers {
			capture := matcher.pattern.FindStringSubmatch(title)
			if capture == nil {
				continue
			}
			parsed, verdict := matcher.read(capture)
			switch verdict {
			case matchRead:
				if parsed.episodeRange == nil {
					parsed.episodeRange = []int64{}
				}
				return parsed
			case matchDropped:
				return releaseMatch{episodeRange: []int64{}, dropped: true}
			}
		}
		return releaseMatch{episodeRange: []int64{}}
	}
	titles := []string{
		"The.Pitt.S01E01.Pilot.1080p.WEB-DL.DDP5.1.H.264-ABC",
		"Show.S02E01-05.720p.HDTV.x264.ITA.SUB.ITA",
		"Show.S02E01E02E03.1080p.WEB-DL",
		"Show.S01E01.1080p.WEB-DL.S02E10-E12",
		"Show.S02E10-E12.1080p.WEB-DL.S01E01",
		"Show.S01.E01.1080p.WEB-DL",
		"Show.S01 E01.1080p.WEB-DL",
		"Show.S01..E01.1080p.WEB-DL",
		"Show.S01E01-E03.1080p.WEB-DL",
		"Show.S01E01–E03.1080p.WEB-DL",
		"Show.2160p.S02E10.1080p.WEB-DL",
		"A.S01E01",
		"S01E01",
		"Show.S99E01",
		"Show.S01E00",
		"Show.S01E01E0",
		"Show.S01E01X",
		"Show.S01E01.S02E02.1080p",
		"Show.S01E01-02.S03E03-04",
		"Show.S01E01-1080p",
		"Show.S01E01-E03.S02E10-E12",
		"Movie.2024.2160p.UHD.BluRay.REMUX.DV.HDR10.ITA.ENG",
		"Another.Movie.2021.1080p.WEBRip.AAC2.0.x264",
		"Daily.Show.2026-09-21.WEB-DL.1080p",
		"Daily Show 2026.09.21 1080p",
		"Show.2026-13-45.1080p",
		"Show.1x02.2026-09-21.1080p",
		"Show.S01E01.2026-09-21.1080p",
		"Show.S01",
		"Show.S01.Complete.1080p",
		"Show.S01.COMPLETA.1080p",
		"Show.S01-1080p",
		"Show.S01E01",
		"Show.S01x",
		"Show.Season.1.Complete",
		"Show.Stagione.2",
		"Show.S01.In.Corso.05.06",
		"Show.S01.Incomplete.05.of.06",
		"Show.Name.S01.IN.CORSO.00/18.1080p",
		"Show.Name.S01.In.Progress.00/06.ITA",
	}
	for _, title := range titles {
		want := reference(title)
		got := parseReleaseTitle(title)
		if got.dropped != want.dropped || got.seasonPack != want.seasonPack || got.inProgress != want.inProgress ||
			!slices.Equal(got.episodeRange, want.episodeRange) ||
			(got.season == nil) != (want.season == nil) ||
			(got.episode == nil) != (want.episode == nil) ||
			(got.series == nil) != (want.series == nil) {
			t.Fatalf("%q: fast path = %+v, reference = %+v", title, got, want)
		}
		if got.season != nil && *got.season != *want.season {
			t.Fatalf("%q: season = %d, reference %d", title, *got.season, *want.season)
		}
		if got.episode != nil && *got.episode != *want.episode {
			t.Fatalf("%q: episode = %d, reference %d", title, *got.episode, *want.episode)
		}
		if got.series != nil && *got.series != *want.series {
			t.Fatalf("%q: series = %q, reference %q", title, *got.series, *want.series)
		}
	}
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
