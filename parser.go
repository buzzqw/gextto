package gextto

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

// MergeQuality combina due `Quality`: usa i campi di `primary` quando
// valorizzati, altrimenti quelli di `fallback`. Serve a recuperare
// sorgente/codec/ecc. dal titolo originale della release quando il nome file
// attuale li ha persi.
func MergeQuality(primary, fallback models.Quality) models.Quality {
	pick := func(primary, fallback string) string {
		if strings.TrimSpace(primary) == "" || strings.EqualFold(primary, "unknown") {
			return fallback
		}
		return primary
	}
	languages := primary.Languages
	if len(languages) == 0 {
		languages = fallback.Languages
	}
	subtitleLanguages := primary.SubtitleLanguages
	if len(subtitleLanguages) == 0 {
		subtitleLanguages = fallback.SubtitleLanguages
	}
	return models.Quality{
		Resolution:        pick(primary.Resolution, fallback.Resolution),
		Source:            pick(primary.Source, fallback.Source),
		Codec:             pick(primary.Codec, fallback.Codec),
		Audio:             pick(primary.Audio, fallback.Audio),
		AudioChannels:     pick(primary.AudioChannels, fallback.AudioChannels),
		AudioObjects:      pick(primary.AudioObjects, fallback.AudioObjects),
		HDR:               pick(primary.HDR, fallback.HDR),
		Group:             pick(primary.Group, fallback.Group),
		IsIta:             primary.IsIta || fallback.IsIta,
		IsDV:              primary.IsDV || fallback.IsDV,
		IsRepack:          primary.IsRepack || fallback.IsRepack,
		IsProper:          primary.IsProper || fallback.IsProper,
		IsReal:            primary.IsReal || fallback.IsReal,
		Language:          pick(primary.Language, fallback.Language),
		Languages:         languages,
		HasSubtitle:       primary.HasSubtitle || fallback.HasSubtitle,
		SubtitleLanguages: subtitleLanguages,
		HardcodedSubs:     primary.HardcodedSubs || fallback.HardcodedSubs,
	}
}

// parserNormalizeCache memoises NormalizeSeriesName: series matching normalises
// every configured series name for every candidate release (series × releases
// per cycle). The function is pure, so results are safe to reuse. Bounded to
// keep memory in check.
var (
	parserNormalizeCacheMu sync.RWMutex
	parserNormalizeCache   = map[string]string{}
)

var normalizeSeriesNameReplacer = strings.NewReplacer(".", " ", "_", " ", "-", " ", "/", " ", "\\", " ", ":", " ")

// NormalizeSeriesName normalizza il nome di una serie per il confronto.
func NormalizeSeriesName(value string) string {
	parserNormalizeCacheMu.RLock()
	if hit, ok := parserNormalizeCache[value]; ok {
		parserNormalizeCacheMu.RUnlock()
		return hit
	}
	parserNormalizeCacheMu.RUnlock()
	// Release names use dots, underscores, hyphens, slashes and colons as
	// separators while the configured name is usually human-readable. Keep
	// parentheses intact because they carry useful year information.
	normalized := parserASCIILower(value)
	normalized = normalizeSeriesNameReplacer.Replace(normalized)
	normalized = strings.ReplaceAll(normalized, "'s", "")
	normalized = strings.ReplaceAll(normalized, "’s", "")
	words := strings.Fields(normalized)
	kept := make([]string, 0, len(words))
	for _, word := range words {
		switch word {
		case "the", "a", "an", "il", "lo", "la", "i", "gli", "le", "un", "una":
			continue
		}
		kept = append(kept, word)
	}
	normalized = strings.Join(kept, " ")
	parserNormalizeCacheMu.Lock()
	if len(parserNormalizeCache) < 50_000 {
		parserNormalizeCache[value] = normalized
	}
	parserNormalizeCacheMu.Unlock()
	return normalized
}

// SeriesNamesMatch confronta due nomi di serie.
func SeriesNamesMatch(a, b string) bool {
	return seriesKeysMatch(seriesNameKey(a), seriesNameKey(b))
}

// seriesNameKey is the form of a name SeriesNamesMatch compares: normalised,
// with the year tokens stripped and cut into words. It is pure, so a caller that
// compares one release against every configured series can compute the keys of
// the configured side once (see configuredSeriesIndex).
func seriesNameKey(value string) string {
	return matchingName(stripYearTokens(NormalizeSeriesName(value)))
}

// seriesKeysMatch compares two values already reduced by seriesNameKey.
func seriesKeysMatch(left, right string) bool {
	return seriesKeyWordsMatch(left, right, strings.Fields(left), strings.Fields(right))
}

// seriesKeyWordsMatch is the comparison itself. The words are passed in because
// the configured side keeps them precomputed: the tolerant pass compares a
// release name against every series name and alias of the library.
func seriesKeyWordsMatch(left, right string, leftWords, rightWords []string) bool {
	if tokensMatchWords(leftWords, rightWords) ||
		tokensMatchWithReleaseSuffixWords(leftWords, rightWords) ||
		tokensMatchWithReleaseSuffixWords(rightWords, leftWords) {
		return true
	}
	// Titoli "stilizzati" (es. `PLUR1BUS` per *Pluribus*): confronta una
	// variante con le cifre sciolte in lettere, ma solo quando una sola delle
	// due parti contiene cifre. Così non si confondono titoli numerici come
	// `9-1-1` e non si altera il confronto tra nomi già uguali.
	if hasASCIIDigit(left) != hasASCIIDigit(right) {
		foldedLeft := leetFold(left)
		foldedRight := leetFold(right)
		foldedLeftWords := strings.Fields(foldedLeft)
		foldedRightWords := strings.Fields(foldedRight)
		if tokensMatchWords(foldedLeftWords, foldedRightWords) ||
			tokensMatchWithReleaseSuffixWords(foldedLeftWords, foldedRightWords) ||
			tokensMatchWithReleaseSuffixWords(foldedRightWords, foldedLeftWords) {
			return true
		}
	}
	return false
}

// matchingName is the form used only for comparing names.
// `NormalizeSeriesName` deliberately keeps some punctuation because it is also
// used to build torrent episode keys; matching needs the more permissive
// behaviour of the legacy parser.
func matchingName(value string) string {
	// A release name has a handful of words: sizing the two slices once keeps
	// their growth out of this hot path (it runs for every release and every
	// monitored title).
	words := make([]string, 0, 8)
	var current strings.Builder
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			// The name arrives lowered for ASCII only: an accented capital
			// ("È", "É", "À") is lowered here, or the fold below, which knows the
			// small letters, left it as it was and "Élite" never met "Elite".
			if character >= utf8.RuneSelf {
				character = unicode.ToLower(character)
			}
			current.WriteRune(foldLatinCharacter(character))
		} else if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}

	// Torrent names occasionally spell acronyms as `F B I` or `S W A T`.
	compacted := make([]string, 0, len(words))
	index := 0
	for index < len(words) {
		if utf8.RuneCountInString(words[index]) == 1 {
			start := index
			for index < len(words) && utf8.RuneCountInString(words[index]) == 1 {
				index++
			}
			if index-start >= 2 {
				compacted = append(compacted, strings.Join(words[start:index], ""))
			} else {
				compacted = append(compacted, words[start:index]...)
			}
		} else {
			compacted = append(compacted, words[index])
			index++
		}
	}
	return strings.Join(compacted, " ")
}

func foldLatinCharacter(character rune) rune {
	switch character {
	case 'à', 'á', 'â', 'ã', 'ä', 'å', 'ā', 'ă', 'ą':
		return 'a'
	case 'ç', 'ć', 'ĉ', 'ċ', 'č':
		return 'c'
	case 'ď', 'đ':
		return 'd'
	case 'è', 'é', 'ê', 'ë', 'ē', 'ĕ', 'ė', 'ę', 'ě':
		return 'e'
	case 'ì', 'í', 'î', 'ï', 'ĩ', 'ī', 'ĭ', 'į', 'ı':
		return 'i'
	case 'ñ', 'ń', 'ņ', 'ň':
		return 'n'
	case 'ò', 'ó', 'ô', 'õ', 'ö', 'ø', 'ō', 'ŏ', 'ő':
		return 'o'
	case 'ŕ', 'ŗ', 'ř':
		return 'r'
	case 'ś', 'ŝ', 'ş', 'š':
		return 's'
	case 'ť', 'ţ', 'ŧ':
		return 't'
	case 'ù', 'ú', 'û', 'ü', 'ũ', 'ū', 'ŭ', 'ů', 'ű', 'ų':
		return 'u'
	case 'ý', 'ÿ', 'ŷ':
		return 'y'
	case 'ž', 'ź', 'ż':
		return 'z'
	case 'æ':
		return 'a'
	case 'œ':
		return 'o'
	case 'ß':
		return 's'
	default:
		return asciiLowerRune(character)
	}
}

// tokensMatchWithReleaseSuffixWords is tokensMatchWithReleaseSuffix on words
// that are already split.
func tokensMatchWithReleaseSuffixWords(configuredWords, candidateWords []string) bool {
	if len(candidateWords) <= len(configuredWords) || !tokensMatchPrefix(configuredWords, candidateWords) {
		return false
	}
	for _, token := range candidateWords[len(configuredWords):] {
		if !isReleaseSuffixToken(token) {
			return false
		}
	}
	return true
}

func tokensMatchPrefix(configured, candidate []string) bool {
	for i := range configured {
		if !tokenEqualsOrPlural(configured[i], candidate[i]) {
			return false
		}
	}
	return true
}

func isReleaseSuffixToken(token string) bool {
	isDigits := token != "" && allASCIIDigits(token)
	lower := parserASCIILower(token)
	if isDigits && len(token) == 4 && (strings.HasPrefix(token, "19") || strings.HasPrefix(token, "20")) {
		return true
	}
	switch lower {
	case "complete", "completa", "season", "tv", "web", "webdl", "webrip", "hdtv", "bluray":
		return true
	}
	regex, err := utils.CachedRegex(`(?i)^(?:s\d{1,2}|e\d{1,4}|\d{1,2}x\d{1,4}|\d{3,4}p)$`)
	return err == nil && regex.MatchString(token)
}

// tokensMatchWords is tokensMatch on words that are already split.
func tokensMatchWords(leftWords, rightWords []string) bool {
	if len(leftWords) != len(rightWords) {
		return false
	}
	for i := range leftWords {
		if !tokenEqualsOrPlural(leftWords[i], rightWords[i]) {
			return false
		}
	}
	return true
}

// tokenEqualsOrPlural riproduce `a == b || a.strip_suffix('s') == Some(b) ||
// b.strip_suffix('s') == Some(a)`.
func tokenEqualsOrPlural(left, right string) bool {
	return left == right ||
		strings.TrimSuffix(left, "s") == right ||
		strings.TrimSuffix(right, "s") == left
}

// stripYearTokens rimuove i token che sono solo un anno racchiuso in
// parentesi/quadre (`(2025)`, `[2024]`), frequenti nei nomi file legacy
// `SERIE (ANNO) - S01E01`. Se la rimozione svuoterebbe il nome (serie
// intitolate a un anno, es. `1923`) il valore resta invariato: un anno "nudo"
// non viene toccato.
func stripYearTokens(value string) string {
	tokens := strings.Fields(value)
	kept := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if !isYearToken(token) {
			kept = append(kept, token)
		}
	}
	if len(kept) == 0 {
		return value
	}
	return strings.Join(kept, " ")
}

func isYearToken(token string) bool {
	wrapped := strings.HasPrefix(token, "(") ||
		strings.HasPrefix(token, "[") ||
		strings.HasSuffix(token, ")") ||
		strings.HasSuffix(token, "]")
	if !wrapped {
		return false
	}
	trimmed := strings.TrimFunc(token, func(c rune) bool { return !isASCIIAlphanumeric(c) })
	return len(trimmed) == 4 &&
		(strings.HasPrefix(trimmed, "19") || strings.HasPrefix(trimmed, "20")) &&
		allASCIIDigits(trimmed)
}

func hasASCIIDigit(value string) bool {
	for _, c := range value {
		if c >= '0' && c <= '9' {
			return true
		}
	}
	return false
}

// leetFold sostituisce le cifre usate come lettere nei titoli leet:
// `1`→`i`, `0`→`o`, `3`→`e`, `4`→`a`, `5`→`s`, `7`→`t`, `6`/`9`→`g`, `8`→`b`.
func leetFold(value string) string {
	var out strings.Builder
	for _, c := range value {
		switch c {
		case '0':
			out.WriteByte('o')
		case '1':
			out.WriteByte('i')
		case '3':
			out.WriteByte('e')
		case '4':
			out.WriteByte('a')
		case '5':
			out.WriteByte('s')
		case '6':
			out.WriteByte('g')
		case '7':
			out.WriteByte('t')
		case '8':
			out.WriteByte('b')
		case '9':
			out.WriteByte('g')
		default:
			out.WriteRune(c)
		}
	}
	return out.String()
}

// ParseEpisodeKey costruisce la chiave `(nome serie normalizzato, stagione,
// episodio)` da un nome file o titolo di release. Usata per confrontare le
// release coi torrent già attivi nella sessione.
func ParseEpisodeKey(name string) *models.LiveEpisodeKey {
	pattern, err := utils.CachedRegex(
		`(?i)^(?P<name>.+?)[ ._-]+(?:s(?P<s>\d{1,4})e|(?P<ns>\d{1,2})x)(?P<e>\d{1,4})`,
	)
	if err != nil {
		return nil
	}
	captures := pattern.FindStringSubmatch(name)
	if captures == nil {
		return nil
	}
	seriesIndex := pattern.SubexpIndex("name")
	seasonIndex := pattern.SubexpIndex("s")
	nsIndex := pattern.SubexpIndex("ns")
	episodeIndex := pattern.SubexpIndex("e")
	if seriesIndex < 0 || episodeIndex < 0 {
		return nil
	}
	seasonRaw := ""
	if seasonIndex >= 0 && captures[seasonIndex] != "" {
		seasonRaw = captures[seasonIndex]
	} else if nsIndex >= 0 {
		seasonRaw = captures[nsIndex]
	}
	if seasonRaw == "" {
		return nil
	}
	season, err := strconv.ParseInt(seasonRaw, 10, 64)
	if err != nil {
		return nil
	}
	episode, err := strconv.ParseInt(captures[episodeIndex], 10, 64)
	if err != nil {
		return nil
	}
	return &models.LiveEpisodeKey{
		Series:  NormalizeSeriesName(captures[seriesIndex]),
		Season:  season,
		Episode: episode,
	}
}

// ParseQuality classifica la qualità tecnica di un titolo.
// isQualitySeparator is the byte class the subtitle and language tags use:
// [._ \-\[\]\(\)+].
func isQualitySeparator(c byte) bool {
	switch c {
	case '.', '_', ' ', '-', '[', ']', '(', ')', '+':
		return true
	}
	return false
}

// isWordByte follows regexp's \b: ASCII letters, digits and underscore.
func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// hasDigitBounded reports whether token appears in text without a digit on
// either side, the boundary rule of the "5.1"/"7.1" audio channel tags: they
// also appear across digits in "x265.10bit" and "2024.05.12".
func hasDigitBounded(text, token string) bool {
	for pos := 0; pos < len(text); {
		index := strings.Index(text[pos:], token)
		if index < 0 {
			return false
		}
		start := pos + index
		end := start + len(token)
		beforeOK := start == 0 || text[start-1] < '0' || text[start-1] > '9'
		afterOK := end == len(text) || text[end] < '0' || text[end] > '9'
		if beforeOK && afterOK {
			return true
		}
		pos = start + 1
	}
	return false
}

// hasHDToken is `(?:^|[^a-z0-9-])hd(?:[^a-z0-9-]|$)|hdtv` without the regex.
func hasHDToken(low string) bool {
	if strings.Contains(low, "hdtv") {
		return true
	}
	for pos := 0; pos < len(low); {
		index := strings.Index(low[pos:], "hd")
		if index < 0 {
			return false
		}
		start := pos + index
		end := start + 2
		beforeOK := start == 0 || !isHDNeighbor(low[start-1])
		afterOK := end == len(low) || !isHDNeighbor(low[end])
		if beforeOK && afterOK {
			return true
		}
		pos = start + 1
	}
	return false
}

func isHDNeighbor(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-'
}

// isASCIISpace is the \s class of a regexp.
func isASCIISpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// hasWholeWord reports whether word appears as a whole word (\b semantics).
// It replaces the single-word regexes that dominated ParseQuality: a cycle
// parses tens of thousands of titles and each regex starts a new automaton.
func hasWholeWord(text, word string) bool {
	if word == "" {
		return false
	}
	for pos := 0; pos < len(text); {
		index := strings.Index(text[pos:], word)
		if index < 0 {
			return false
		}
		start := pos + index
		end := start + len(word)
		if (start == 0 || !isWordByte(text[start-1])) &&
			(end == len(text) || !isWordByte(text[end])) {
			return true
		}
		pos = start + 1
	}
	return false
}

// hasAnyWholeWord is hasWholeWord over several spellings.
func hasAnyWholeWord(text string, words ...string) bool {
	for _, word := range words {
		if hasWholeWord(text, word) {
			return true
		}
	}
	return false
}

// hasSeparatedToken reports whether one of tokens appears between quality
// separators or at the ends of text, the boundary rule of the language tags.
func hasSeparatedToken(text string, tokens ...string) bool {
	for _, token := range tokens {
		for pos := 0; pos < len(text); {
			index := strings.Index(text[pos:], token)
			if index < 0 {
				break
			}
			start := pos + index
			end := start + len(token)
			if (start == 0 || isQualitySeparator(text[start-1])) &&
				(end == len(text) || isQualitySeparator(text[end])) {
				return true
			}
			pos = start + 1
		}
	}
	return false
}

// hasITPrefix catches the "it+" / "it|" / "[it]" forms of the Italian tag.
func hasITPrefix(text string) bool {
	for i := 0; i+1 < len(text); i++ {
		if text[i] != 'i' || text[i+1] != 't' {
			continue
		}
		if i > 0 && isWordByte(text[i-1]) {
			continue
		}
		if i+2 < len(text) && (text[i+2] == '+' || text[i+2] == '|') {
			return true
		}
	}
	return strings.Contains(text, "[it]")
}

// isLowerAlnum reports whether value is a non-empty run of [a-z0-9].
func isLowerAlnum(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// releaseGroupFromTail implements `[-]([a-z0-9]+)$|\[([a-z0-9]+)\]$` without
// the automaton: a trailing word after the last hyphen, or the content of the
// trailing square brackets.
func releaseGroupFromTail(low string) string {
	n := len(low)
	if n == 0 {
		return ""
	}
	if low[n-1] == ']' {
		if open := strings.LastIndexByte(low[:n-1], '['); open >= 0 {
			if tail := low[open+1 : n-1]; isLowerAlnum(tail) {
				return tail
			}
		}
		return ""
	}
	if dash := strings.LastIndexByte(low, '-'); dash >= 0 {
		if tail := low[dash+1:]; isLowerAlnum(tail) {
			return tail
		}
	}
	return ""
}

// stripSubtitleTags removes the subtitle tags from text. Most release names
// carry none: the marker check keeps the scanner off that path.
func stripSubtitleTags(text string) string {
	if !hasSubtitleMarker(text) {
		return text
	}
	return removeSubtitleTags(text)
}

// removeSubtitleTags is the subtitle-tag rewrite without the regex. The pattern
// was the most expensive one in ParseQuality: a marker, any number of short
// language tokens separated by punctuation and an Italian token (or the
// reverse). The scanner follows the same left-to-right, greedy rules; the
// reference regex is kept in quality_scan_test.go and compared on a corpus.
func removeSubtitleTags(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	last := 0
	for i := 0; i < len(text); {
		end := subtitleMatchEnd(text, i)
		if end <= i {
			i++
			continue
		}
		out.WriteString(text[last:i])
		out.WriteByte(' ')
		last = end
		i = end
	}
	out.WriteString(text[last:])
	return out.String()
}

// subtitleMatchEnd returns the end of a subtitle tag starting at start, or 0.
func subtitleMatchEnd(text string, start int) int {
	if start >= len(text) || !isWordByte(text[start]) {
		return 0
	}
	if start > 0 && isWordByte(text[start-1]) {
		return 0
	}
	// The regex tries \bMARKER ... ITALIAN\b first, then the reverse.
	if end := subtitleMarkerFirstEnd(text, start); end > 0 {
		return end
	}
	return subtitleItalianFirstEnd(text, start)
}

// isSubtitleSeparator is the separator class [\s._\-/\\\[\]()+|,:;].
func isSubtitleSeparator(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\v', '\f', '.', '_', '-', '/', '\\',
		'[', ']', '(', ')', '+', '|', ',', ':', ';':
		return true
	}
	return false
}

func skipSubtitleSeparators(text string, pos int) int {
	for pos < len(text) && isSubtitleSeparator(text[pos]) {
		pos++
	}
	return pos
}

// subtitleMarkerEnds lists the marker alternatives that match at pos, in the
// order the regex tries them (a greedy `s?` comes before the bare form).
func subtitleMarkerEnds(text string, pos int) []int {
	ends := make([]int, 0, 3)
	add := func(marker string) {
		if strings.HasPrefix(text[pos:], marker) {
			ends = append(ends, pos+len(marker))
		}
	}
	add("subs")
	add("sub")
	add("subforced")
	add("subtitles")
	add("subtitle")
	add("forced")
	add("sdh")
	add("cc")
	add("closedcaptions")
	add("closedcaption")
	return ends
}

// subtitleLanguageEnds lists the [a-z]{2,3} chunks first (3 then 2, as the
// greedy quantifier does) and then the full language names.
func subtitleLanguageEnds(text string, pos int) []int {
	ends := make([]int, 0, 8)
	if pos+3 <= len(text) && isLowerASCII(text[pos:pos+3]) {
		ends = append(ends, pos+3)
	}
	if pos+2 <= len(text) && isLowerASCII(text[pos:pos+2]) {
		ends = append(ends, pos+2)
	}
	for _, name := range []string{
		"english", "italian", "italiano", "german", "deutsch", "french",
		"francais", "spanish", "portuguese", "japanese", "chinese", "korean", "russian",
	} {
		if strings.HasPrefix(text[pos:], name) {
			ends = append(ends, pos+len(name))
		}
	}
	return ends
}

func isLowerASCII(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < 'a' || value[i] > 'z' {
			return false
		}
	}
	return true
}

// subtitleItalianPrefixes lists the Italian spellings that match at pos in
// the regex order: it, ita, italian, italiano.
func subtitleItalianPrefixes(text string, pos int) []int {
	ends := make([]int, 0, 4)
	for _, word := range []string{"it", "ita", "italian", "italiano"} {
		if strings.HasPrefix(text[pos:], word) {
			ends = append(ends, pos+len(word))
		}
	}
	return ends
}

// subtitleMarkerFirstEnd matches MARKER SEP* (LANG SEP*)* ITALIAN\b.
func subtitleMarkerFirstEnd(text string, start int) int {
	for _, markerEnd := range subtitleMarkerEnds(text, start) {
		if end := subtitleTailEnd(text, markerEnd); end > 0 {
			return end
		}
	}
	return 0
}

// subtitleTailEnd is the greedy language run followed by the Italian token.
func subtitleTailEnd(text string, pos int) int {
	pos = skipSubtitleSeparators(text, pos)
	// The regex's (LANG SEP*)* is greedy: try to consume a language first and
	// only fall back to ending the run here.
	for _, languageEnd := range subtitleLanguageEnds(text, pos) {
		next := skipSubtitleSeparators(text, languageEnd)
		if end := subtitleTailEnd(text, next); end > 0 {
			return end
		}
	}
	for _, italianEnd := range subtitleItalianPrefixes(text, pos) {
		if italianEnd == len(text) || !isWordByte(text[italianEnd]) {
			return italianEnd
		}
	}
	return 0
}

// subtitleItalianFirstEnd matches ITALIAN SEP* MARKER\b.
func subtitleItalianFirstEnd(text string, start int) int {
	for _, italianEnd := range subtitleItalianPrefixes(text, start) {
		pos := skipSubtitleSeparators(text, italianEnd)
		for _, markerEnd := range subtitleMarkerEnds(text, pos) {
			if markerEnd == len(text) || !isWordByte(text[markerEnd]) {
				return markerEnd
			}
		}
	}
	return 0
}

// hasSubtitleMarker is a conservative pre-check for the subtitle-removal
// regex: any marker substring means the regex may have work to do. It is
// deliberately looser than the regex (a name that only contains "cc" inside
// another word runs it too), because a false negative would leave a subtitle
// tag in the audio-language text.
func hasSubtitleMarker(text string) bool {
	return strings.Contains(text, "sub") ||
		strings.Contains(text, "forced") ||
		strings.Contains(text, "sdh") ||
		strings.Contains(text, "cc") ||
		strings.Contains(text, "closedcaption")
}

// hasSubtitleMarkerLanguage finds a subtitle marker followed (past the
// separators) by one of languages, which may be glued to it ("SubIta").
func hasSubtitleMarkerLanguage(low string, languages ...string) bool {
	markers := [...]string{"subtitles", "subtitle", "subs", "sub"}
	for _, marker := range markers {
		for pos := 0; pos < len(low); {
			index := strings.Index(low[pos:], marker)
			if index < 0 {
				break
			}
			after := pos + index + len(marker)
			for after < len(low) && isQualitySeparator(low[after]) {
				after++
			}
			for _, language := range languages {
				if !strings.HasPrefix(low[after:], language) {
					continue
				}
				end := after + len(language)
				if end == len(low) || isQualitySeparator(low[end]) {
					return true
				}
			}
			pos += index + 1
		}
	}
	return false
}

// normalizeQualitySeparators is t_norm: [._ -\[\]] become spaces.
func normalizeQualitySeparators(low string) string {
	var builder strings.Builder
	builder.Grow(len(low))
	for i := 0; i < len(low); i++ {
		switch low[i] {
		case '.', '_', '-', ' ', '[', ']':
			builder.WriteByte(' ')
		default:
			builder.WriteByte(low[i])
		}
	}
	return builder.String()
}

// stripStreamingTags removes the streaming-service tags that contain "it"
// (iTunes) or "nf" (Netflix): "it web" becomes " web", and the service words
// disappear. The text is already normalized to spaces.
func stripStreamingTags(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	last := 0
	for i := 0; i < len(text); {
		if !isWordByte(text[i]) {
			i++
			continue
		}
		start := i
		for i < len(text) && isWordByte(text[i]) {
			i++
		}
		switch word := text[start:i]; word {
		case "itunes", "amzn", "dsnp", "hmax", "paramount":
			out.WriteString(text[last:start])
			out.WriteByte(' ')
			last = i
		case "it", "nf":
			// The regex only drops the service tag when "web" follows it.
			next := i
			for next < len(text) && isASCIISpace(text[next]) {
				next++
			}
			end := next
			for end < len(text) && isWordByte(text[end]) {
				end++
			}
			if end > next && text[next:end] == "web" {
				out.WriteString(text[last:start])
				last = i
			}
		}
	}
	out.WriteString(text[last:])
	return out.String()
}

// itFalsePositives are the words that contain "it" and must not be read as
// the Italian audio tag after the technical part of a name.
var itFalsePositives = map[string]struct{}{
	"with": {}, "bit": {}, "split": {}, "edit": {}, "unit": {}, "visit": {},
	"limit": {}, "exit": {}, "profit": {}, "submit": {}, "commit": {},
	"permit": {}, "admit": {}, "omit": {}, "hit": {}, "kit": {}, "pit": {},
	"sit": {}, "fit": {}, "wit": {}, "knit": {}, "spit": {}, "slit": {},
	"itunes": {},
}

// hasITFalsePositive reports whether a space-normalized text contains one of
// the words that look like "it" but are not the language tag.
func hasITFalsePositive(text string) bool {
	for i := 0; i < len(text); {
		for i < len(text) && text[i] == ' ' {
			i++
		}
		start := i
		for i < len(text) && text[i] != ' ' {
			i++
		}
		if start < i {
			if _, found := itFalsePositives[text[start:i]]; found {
				return true
			}
		}
	}
	return false
}

// findResolutionIndex is the byte offset of the first technical token, the
// point from which the language tag may be read (a title can contain "it").
func findResolutionIndex(text string) int {
	tokens := [...]string{
		"2160p", "2160", "1080p", "1080", "720p", "720", "480p", "480",
		"4k", "uhd", "bluray", "webrip", "hdtv", "webdl", "web dl",
	}
	best := -1
	for _, token := range tokens {
		for pos := 0; pos < len(text); {
			index := strings.Index(text[pos:], token)
			if index < 0 {
				break
			}
			start := pos + index
			end := start + len(token)
			if (start == 0 || !isWordByte(text[start-1])) &&
				(end == len(text) || !isWordByte(text[end])) {
				if best < 0 || start < best {
					best = start
				}
				break
			}
			pos = start + 1
		}
	}
	return best
}

func ParseQuality(title string) models.Quality {
	low := strings.ToLower(title)
	// legacy normalisations: `t_norm` replaces [._-] with spaces; `t_norm_lang`
	// also removes square brackets so renamed files like "[DV HDR10][IT][h265]"
	// are recognised.
	tNormLang := normalizeQualitySeparators(low)

	// --- Riscrittura dei marker di sottotitoli prima del rilevamento lingua ---
	// Il feed può riportare "ENG ... Sub ita ..." anche quando l'audio è solo
	// inglese: se non rimossi, "sub ita" verrebbe letto come audio italiano.
	tNormLangAudio := stripSubtitleTags(tNormLang)
	tAudio := stripSubtitleTags(low)

	// --- Lingua italiana: tre livelli, dal più sicuro al più specifico ---
	level1 := hasAnyWholeWord(stripStreamingTags(tNormLangAudio), "ita", "italian", "italiano")
	level2 := hasITPrefix(tAudio)
	ita := false
	if level1 || level2 {
		ita = true
	} else {
		// Livello 3: "\bit\b" solo dalla parte tecnica in poi, per non colpire
		// parole del titolo come "It Chapter" o "Feel It Still".
		if found := findResolutionIndex(tNormLangAudio); found >= 0 {
			tech := stripStreamingTags(tNormLangAudio[found:])
			ita = hasWholeWord(tech, "it") && !hasITFalsePositive(tech)
		}
	}

	subtitleIta := hasSubtitleMarkerLanguage(low, "ita", "italian", "it")
	subtitleEng := hasSubtitleMarkerLanguage(low, "eng", "english", "en")
	eng := !subtitleEng && hasSeparatedToken(low, "eng", "english", "en")
	languages := []string{}
	if ita {
		languages = append(languages, "ita")
	}
	if eng {
		languages = append(languages, "eng")
	}
	subtitleLanguages := []string{}
	if subtitleIta {
		subtitleLanguages = append(subtitleLanguages, "ita")
	}
	if subtitleEng {
		subtitleLanguages = append(subtitleLanguages, "eng")
	}

	// --- Risoluzione ---
	resolution := "unknown"
	switch {
	case strings.Contains(low, "2160p") || strings.Contains(low, "4k") || strings.Contains(low, "uhd"):
		resolution = "2160p"
	case strings.Contains(low, "1080p") || strings.Contains(low, "fullhd"):
		resolution = "1080p"
	case strings.Contains(low, "720p") || hasHDToken(low):
		resolution = "720p"
	case strings.Contains(low, "576p") || hasWholeWord(low, "pal"):
		resolution = "576p"
	case strings.Contains(low, "480p") || strings.Contains(low, "ntsc"):
		resolution = "480p"
	case strings.Contains(low, "360p"):
		resolution = "360p"
	}

	// --- Sorgente ---
	source := "unknown"
	switch {
	// REMUX must win over the generic BluRay match below: the two co-occur in
	// the same title ("...BluRay.REMUX..."), and the parser used to never emit
	// this source, which made score_source_remux and the whole remux upgrade
	// path (Quality.IsRemux, UpgradeReason, incumbentWins) unreachable. Italian
	// releases are almost always tagged "BDRemux", which contains "remux".
	//
	// "DVDRemux" (common on Italian trackers such as MirCrew) is a DVD, not a
	// BluRay remux: scoring it as remux would rank an SD DVD above a BluRay.
	case strings.Contains(low, "dvdremux") || strings.Contains(low, "dvd-remux") || strings.Contains(low, "dvd.remux") || strings.Contains(tNormLang, "dvd remux"):
		source = "dvdrip"
	case strings.Contains(low, "remux"):
		source = "remux"
	// Italian mux tags name the video source the Italian audio was muxed
	// onto: BDMux is a BluRay encode (not a remux), DLMux a WEB-DL, DLRip a
	// WEBRip.
	case strings.Contains(low, "bluray") || strings.Contains(low, "bdrip") || strings.Contains(low, "brrip") || strings.Contains(low, "bdmux"):
		source = "bluray"
	case strings.Contains(low, "dlrip"):
		source = "webrip"
	case strings.Contains(low, "dlmux"):
		source = "webdl"
	// WEBRip must be checked before the generic "web" catch-all: otherwise
	// every WEBRip title was classified as WEB-DL (and scored as one), which
	// turned an equal-quality re-release into a false "source" upgrade.
	case strings.Contains(tNormLang, "webrip") || strings.Contains(low, "web-rip"):
		source = "webrip"
	case strings.Contains(tNormLang, "web dl") || strings.Contains(low, "web-dl") ||
		strings.Contains(low, "webdl") || strings.Contains(low, "web.dl"):
		source = "webdl"
	case strings.Contains(tNormLang, "web"):
		source = "webdl"
	case strings.Contains(low, "hdtv") || strings.Contains(low, "hdtvrip"):
		source = "hdtv"
	case strings.Contains(low, "dvdrip") || strings.Contains(low, "dvd"):
		source = "dvdrip"
	}

	// --- HDR / Dolby Vision ---
	paddedLang := " " + tNormLang + " "
	isDV := strings.Contains(paddedLang, " dv ") ||
		strings.Contains(tNormLang, "dovi") ||
		strings.Contains(tNormLang, "dolby vision")
	hdr := ""
	switch {
	case isDV:
		hdr = "DV"
	case strings.Contains(tNormLang, "hdr10+") ||
		strings.Contains(tNormLang, "hdr10plus") ||
		strings.Contains(tNormLang, "hdr10 plus"):
		hdr = "HDR10Plus"
	case strings.Contains(tNormLang, "hdr10"):
		hdr = "HDR10"
	case hasAnyWholeWord(tNormLang, "hdr", "hlg"):
		hdr = "HDR"
	}

	// --- Codec ---
	codec := "unknown"
	switch {
	case strings.Contains(low, "x265") ||
		strings.Contains(low, "hevc") ||
		strings.Contains(low, "h.265") ||
		strings.Contains(paddedLang, " h265 "):
		codec = "h265"
	case strings.Contains(low, "x264") ||
		strings.Contains(low, "avc") ||
		strings.Contains(low, "h.264") ||
		strings.Contains(paddedLang, " h264 "):
		codec = "h264"
	}

	// --- Audio: codec, canali e audio a oggetti sono campi separati. ---
	// Il layout non deve scegliere il codec: "AAC 5.1" è AAC, non "5.1".
	audioChannels := ""
	if hasDigitBounded(low, "7.1") {
		audioChannels = "7.1"
	} else if hasDigitBounded(low, "5.1") {
		audioChannels = "5.1"
	}
	audioObjects := []string{}
	if strings.Contains(low, "atmos") {
		audioObjects = append(audioObjects, "atmos")
	}
	if strings.Contains(low, "dtsx") || strings.Contains(low, "dts:x") || strings.Contains(low, "dts-x") {
		audioObjects = append(audioObjects, "dtsx")
	}

	audio := "unknown"
	switch {
	case strings.Contains(low, "truehd") || strings.Contains(low, "true hd"):
		audio = "truehd"
	case strings.Contains(low, "dts-hd") || strings.Contains(low, "dtshd"):
		if strings.Contains(low, "hra") {
			audio = "dts-hd-hra"
		} else {
			audio = "dts-hd"
		}
	case strings.Contains(low, "flac"):
		audio = "flac"
	case strings.Contains(low, "pcm") || strings.Contains(low, "lpcm"):
		audio = "pcm"
	case strings.Contains(low, "dts"):
		audio = "dts"
	case strings.Contains(low, "ddp") || strings.Contains(low, "eac3") || strings.Contains(low, "dd+"):
		audio = "ddp"
	case strings.Contains(low, "ac3") || strings.Contains(low, "dd5.1") || strings.Contains(low, "dd 5.1"):
		audio = "ac3"
	case strings.Contains(low, "opus"):
		audio = "opus"
	case strings.Contains(low, "aac"):
		audio = "aac"
	case strings.Contains(low, "mp3"):
		audio = "mp3"
	case audioChannels == "7.1":
		audio = "7.1"
	case audioChannels == "5.1":
		audio = "5.1"
	}

	// --- Gruppo ---
	group := "unknown"
	if tail := releaseGroupFromTail(low); tail != "" {
		group = tail
		// The tail of a hyphenated tag ("WEB-DL", "Blu-Ray") is not a release group.
		if (group == "dl" && strings.HasSuffix(low, "web-dl")) || (group == "ray" && strings.HasSuffix(low, "blu-ray")) {
			group = "unknown"
		}
	}

	language := "unknown"
	if ita {
		language = "ita"
	} else if eng {
		language = "eng"
	}
	return models.Quality{
		Resolution:        resolution,
		Source:            source,
		Codec:             codec,
		Audio:             audio,
		AudioChannels:     audioChannels,
		AudioObjects:      strings.Join(audioObjects, ","),
		HDR:               hdr,
		Group:             group,
		IsIta:             ita,
		IsDV:              isDV,
		IsRepack:          strings.Contains(low, "repack") || strings.Contains(low, "rerip"),
		IsProper:          strings.Contains(low, "proper"),
		IsReal:            hasWholeWord(low, "real"),
		Language:          language,
		Languages:         languages,
		HasSubtitle:       hasSeparatedToken(low, "sub", "subs", "subtitle", "subtitles"),
		SubtitleLanguages: subtitleLanguages,
		HardcodedSubs:     hasSeparatedToken(low, "hc", "hardcoded", "hardsub", "hardsubs"),
	}
}

// PassesMovieFilter is the legacy `Parser.parse_movie` gate: returns false when
// the release is clearly not a movie (an episode, season pack, wrestling/sport,
// magazine, videogame, console ROM, or a music release with a genre prefix).
func PassesMovieFilter(title string) bool {
	if title == "" {
		return false
	}
	matched := func(pattern string) bool {
		regex, err := utils.CachedRegex(pattern)
		return err == nil && regex.MatchString(title)
	}
	if matched(`(?i)[Ss]\d{1,2}[Ee]\d{1,2}`) {
		return false
	}
	if matched(`(?i)\bStagion[ei]\b|\bSeason[ ._-]?\d|\bComplete[ ._-]?S\d+|\bCOMPLETA\b`) {
		return false
	}
	if matched(`(?i)\bWWE\b|\bAEW\b|\bTNA\b|\bWWF\b|\bROH\b|\bImpact\s+Wrestling\b`) {
		return false
	}
	if matched(`(?i)\bMotoGP\b|\bMotoE\b|\bFormula\s*E?\b|\bNASCAR\b|\bSuperBike\b`) {
		return false
	}
	if matched(`\b\d{4}x\d{2,3}\b`) {
		return false
	}
	if matched(`(?i)\bMagazine\b|\bRivista\b`) {
		return false
	}
	if matched(
		`(?i)\bDLCs?\b|\bPortable\b|Build[ ._-]\d{5,}|\bv20\d\d[._]\d{2}[._]\d{2}\b|\bGameDrive\b|\bHypervisor\b|\bDenuvO\b`,
	) {
		return false
	}
	if matched(
		`(?i)PlayStation[ ._-]+\d|Nintendo[ ._-]+(DS|3DS|64|Switch|Wii)|\bN64\b|\bGBA\b|\bNDS\b|\bPSX\b|\bPS[123]\b`,
	) {
		return false
	}
	hasVideo := matched(
		`(?i)\b(2160p|1080p|720p|576p|480p|4[Kk]|UHD|BluRay|BDRip|WEB-DL|WEBRip|HDTV|DVDRip|DVDScr)\b`,
	)
	if !hasVideo {
		if matched(`^\s*\([A-Za-zÀ-ÿ][A-Za-zÀ-ÿ0-9\s,/&-]{2,}\)\s+\S`) {
			return false
		}
		if matched(`(?i)\bbootleg\b`) {
			return false
		}
	}
	return true
}

// ParseRelease parses a release discovered now.
func ParseRelease(title, magnet, source string) *models.Release {
	return ParseReleaseAt(title, magnet, source, time.Now().UTC())
}

// ReconcilePackIdentity reconciles a season pack with the name supplied by the
// torrent itself.
//
// Indexers occasionally publish a title whose season differs from the actual
// torrent name (for example an RSS title saying `S06E01-06` while the torrent
// and its files are `S05E01-06`). The legacy importer parsed the completed
// filenames, so the torrent's identity is authoritative at that point.
func ReconcilePackIdentity(release *models.Release, torrentName string) *models.Release {
	if release == nil || release.Kind != "series" || !release.IsPack || strings.TrimSpace(torrentName) == "" {
		return nil
	}
	parsed := ParseRelease(torrentName, release.Magnet, release.Source)
	if parsed == nil {
		return nil
	}
	if !parsed.IsPack {
		return nil
	}
	sameSeries := false
	if release.Series != nil && parsed.Series != nil {
		sameSeries = SeriesNamesMatch(*release.Series, *parsed.Series)
	}
	if !sameSeries ||
		(optionalInt64Equal(parsed.Season, release.Season) &&
			int64SlicesEqual(parsed.EpisodeRange, release.EpisodeRange)) {
		return nil
	}
	corrected := *release
	corrected.Title = parsed.Title
	// Keep the configured/canonical series name; the torrent name may use an
	// alias or a translated title even when its season is the authoritative
	// one.
	corrected.Season = parsed.Season
	corrected.Episode = parsed.Episode
	corrected.IsPack = parsed.IsPack
	corrected.EpisodeRange = parsed.EpisodeRange
	corrected.Quality = MergeQuality(parsed.Quality, release.Quality)
	return &corrected
}

// ParseReleaseAt parses a release discovered at a given time.
func ParseReleaseAt(title, magnet, source string, discoveredAt time.Time) *models.Release {
	return ParseReleaseSource(title, magnet, nil, source, discoveredAt)
}

// IsEd2kURL reports an eD2k file link. Torznab results from an indexarr eD2k
// source carry it in <link> (and in the ed2k attribute); the release is queued
// in aMule, not downloaded as a torrent.
func IsEd2kURL(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "ed2k://|file|")
}

// IsTorrentURL is true per un link HTTP(S) che punta a un file `.torrent`.
// Jackett does not use a `.torrent` suffix: its download links are
// `/dl/<indexer>/?path=...`.
func IsTorrentURL(value string) bool {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	path := strings.ToLower(parsed.Path)
	if strings.HasSuffix(path, ".torrent") {
		return true
	}
	if strings.Contains(path, "/dl/") {
		if _, ok := parsed.Query()["path"]; ok {
			return true
		}
	}
	// Prowlarr wraps direct torrent downloads in a manager-local proxy URL such
	// as `/12/download?apikey=...&link=...`. The response is still a torrent
	// file, even though its path has no `.torrent` suffix.
	if strings.HasSuffix(path, "/download") {
		if _, ok := parsed.Query()["link"]; ok {
			return true
		}
	}
	return false
}

// ParseReleaseSource is like [`ParseReleaseAt`], but also accepts a direct
// `.torrent` link: many RSS feeds (e.g. TorrentLeech) do not expose a magnet,
// only the `.torrent` download. A release is valid if it has at least one magnet
// **or** a link.
func ParseReleaseSource(title, magnet string, torrentURL *string, source string, discoveredAt time.Time) *models.Release {
	// Alcune fonti (es. BTDigg) usano un magnet troncato come testo del link:
	// non è un titolo valido, scartalo prima di creare una release.
	if strings.HasPrefix(parserASCIILower(strings.TrimSpace(title)), "magnet:") {
		return nil
	}
	// Un titolo vuoto non è una release valida (capita con item RSS malformati).
	if strings.TrimSpace(title) == "" {
		return nil
	}
	var normalizedURL *string
	if torrentURL != nil {
		trimmed := strings.TrimSpace(*torrentURL)
		if IsTorrentURL(trimmed) {
			normalizedURL = &trimmed
		}
	}
	sanitizedMagnet := ""
	magnetValue, ok := utils.SanitizeMagnet(magnet, &title)
	if ok {
		sanitizedMagnet = magnetValue
	} else if normalizedURL == nil {
		return nil
	}
	parsed := parseReleaseTitle(title)
	if parsed.dropped {
		return nil
	}
	kind := "movie"
	if parsed.season != nil {
		kind = "series"
	}
	var absoluteSeries *string
	var absoluteEpisode *int64
	if parsed.season == nil {
		absoluteSeries, absoluteEpisode = parseAbsoluteEpisode(title)
	}
	year := releaseYear(title)
	// E00 is a real special/recap episode, not a complete-season pack. Only a
	// title read by the season-pack pattern uses the {0} range as a pack
	// sentinel.
	isPack := len(parsed.episodeRange) > 1 || parsed.seasonPack
	return &models.Release{
		Title:              title,
		Magnet:             sanitizedMagnet,
		TorrentURL:         normalizedURL,
		Source:             source,
		Quality:            ParseQuality(title),
		Kind:               kind,
		Series:             parsed.series,
		Season:             parsed.season,
		Episode:            parsed.episode,
		IsPack:             isPack,
		EpisodeRange:       parsed.episodeRange,
		InProgressEpisodes: parsed.inProgress,
		AbsoluteEpisode:    absoluteEpisode,
		AbsoluteSeries:     absoluteSeries,
		Year:               year,
		DiscoveredAt:       discoveredAt,
		SizeBytes:          0,
		Seeders:            -1,
		Peers:              -1,
	}
}

// A release title spells season and episode in more than one way, and a
// pattern can match a title whose values it then refuses to read: a season
// number above 40, a date that does not exist. The matchers below are tried
// in the order the parser has always used, and the first one that both
// matches and reads its values wins. A pattern that matches and refuses lets
// the next one try the same title, so a title is never read as a movie only
// because the first pattern it happened to match was the wrong one.
type releaseMatch struct {
	series       *string
	season       *int64
	episode      *int64
	episodeRange []int64
	seasonPack   bool
	// inProgress is how many episodes a pack labelled "in progress" declares
	// it already holds (0 for every other release). The .torrent can lag
	// behind that claim; the acquisition loop checks it.
	inProgress int64
	// dropped marks a title that claims a shape the parser cannot read
	// safely: ParseReleaseSource returns nil for it.
	dropped bool
}

type matchVerdict int

const (
	// matchRefused: the pattern matched but its values are unusable, so the
	// next pattern may still read the title.
	matchRefused matchVerdict = iota
	// matchRead: the title is this kind of release.
	matchRead
	// matchDropped: the title is a release of this shape, but this parser
	// cannot read the shape safely and must not guess another one.
	matchDropped
)

type releaseMatcher struct {
	pattern *regexp.Regexp
	read    func(capture []string) (releaseMatch, matchVerdict)
}

var releaseMatchers = []releaseMatcher{
	{utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+s(\d{1,4})e(\d{1,4})[-–]e?(\d{1,4})(?:[ ._-]|$)`), readEpisodeRange},
	{utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+s(\d{1,4})((?:e\d{1,4}){2,})(?:[ ._-]|$)`), readJoinedEpisodes},
	// Season and episode markers may be separated by a dot or a space
	// ("S01.E01", "S01 E01"), not only glued ("S01E01").
	{utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+s(\d{1,4})[ ._-]?e(\d{1,4})(?:[ ._-]|$)`), readSeasonEpisode},
	{utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+(\d{1,2})x(\d{1,4})[-–](?:\d{1,2}x|x)?(\d{1,4})(?:[ ._-]|$)`), readNXRange},
	{utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+(\d{1,2})x(\d{1,4})(?:[ ._-]|$)`), readNX},
	{utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+stagione[ ._-]*(\d{1,2})[ ._-]+(?:episodio|puntata|ep\.?)[ ._-]*(\d{1,4})(?:[ ._-]|$)`), readSeasonEpisode},
	{utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+season[ ._-]*(\d{1,2})[ ._-]+(?:episode|ep)[ ._-]*(\d{1,4})(?:[ ._-]|$)`), readSeasonEpisode},
	{utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+(\d{4})[-.](\d{1,2})[-.](\d{1,2})(?:[ ._-]|$)`), readDate},
	// A pack marked "IN CORSO n/m" ("in progress", as Italian sites label an
	// airing season) only holds the first n episodes, not the whole season:
	// the pack range stops there, so it fills a gap only if one of those n is
	// really missing. Reading it as a complete season would invent gaps up to
	// the TMDB count and download the pack for nothing.
	// The count may be glued to the label ("[IN CORSO 02/18]") or sit in its
	// own brackets ("[IN CORSO][02/08]", "[IN CORSO] [05/06]"): Italian sites
	// use both. The total may be unknown ("02/??").
	{utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+(?:stagione[ ._-]*|season[ ._-]?|s)(\d{1,2})\b.*?\b(?:in[ ._-]*(?:corso|progress)|incomplete)\b\W{0,6}0?(\d{1,3})[ ._-]*(?:/|di|of)[ ._-]*(?:0?\d{1,3}|\?{1,3})`), readInProgressPack},
	{utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+(?:stagione[ ._-]*|season[ ._-]?|s)(\d{1,2})(?:[ ._-]+(?:complete|completa))?(?:[ ._-]|$)`), readSeasonPack},
}

// couldBeSeriesTitle is a cheap gate before the matcher list: a movie title
// has no season marker, and running ten anchored regexes over it was a large
// part of the parsing cost. It is deliberately generous: a title that passes
// still goes through the exact matchers. low is the lowercased title.
func couldBeSeriesTitle(low string) bool {
	if strings.Contains(low, "season") || strings.Contains(low, "stagione") ||
		strings.Contains(low, "episodio") || strings.Contains(low, "puntata") ||
		strings.Contains(low, "episode") || strings.Contains(low, "in corso") ||
		strings.Contains(low, "incomplete") {
		return true
	}
	for i := 0; i < len(low); i++ {
		c := low[i]
		if c >= '0' && c <= '9' {
			// "1x02"
			if i+2 < len(low) && (low[i+1] == 'x' || low[i+1] == 'X') && low[i+2] >= '0' && low[i+2] <= '9' {
				return true
			}
			// "2026-09-21" and "2026.9.21"
			if i+4 < len(low) && isDigitByte(low[i+1]) && isDigitByte(low[i+2]) &&
				isDigitByte(low[i+3]) && (low[i+4] == '-' || low[i+4] == '.') {
				j := i + 5
				digits := 0
				for j < len(low) && isDigitByte(low[j]) && digits < 2 {
					j++
					digits++
				}
				if digits > 0 && j < len(low) && (low[j] == '-' || low[j] == '.') {
					k := j + 1
					day := 0
					for k < len(low) && isDigitByte(low[k]) && day < 2 {
						k++
						day++
					}
					if day > 0 {
						return true
					}
				}
			}
		}
		if c == 's' || c == 'S' {
			if i == 0 || !isWordByte(low[i-1]) {
				if i+1 < len(low) && low[i+1] >= '0' && low[i+1] <= '9' {
					return true
				}
			}
		}
	}
	return false
}

func isDigitByte(c byte) bool { return c >= '0' && c <= '9' }

func isEpisodeSeparator(c byte) bool {
	return c == ' ' || c == '.' || c == '_' || c == '-'
}

// parseDigitsAt reads at most max digits at pos; the string is empty when
// there is none.
func parseDigitsAt(text string, pos, max int) (string, int) {
	start := pos
	for pos < len(text) && pos-start < max && text[pos] >= '0' && text[pos] <= '9' {
		pos++
	}
	return text[start:pos], pos
}

// rangeDashLen is the length of the range dash: '-' or the en dash.
func rangeDashLen(text string, pos int) int {
	if pos < len(text) && text[pos] == '-' {
		return 1
	}
	if pos+2 < len(text) && text[pos] == 0xE2 && text[pos+1] == 0x80 && text[pos+2] == 0x93 {
		return 3
	}
	return 0
}

type sxxexxPattern int

const (
	sxxexxRange sxxexxPattern = iota
	sxxexxJoined
	sxxexxBasic
)

// parseSxxExxFast reads the three "S01E01" matchers without their regexes.
// They are the first three patterns and the ones that match almost every
// series title; the matcher loop stays as the fallback for the other shapes
// and for a shape whose values the reader refuses.
func parseSxxExxFast(title string) (releaseMatch, bool) {
	// The matcher list tries all ranges before joined episodes and plain
	// episodes: a later S02E10-E12 wins over an earlier S01E01.
	for _, pattern := range []sxxexxPattern{sxxexxRange, sxxexxJoined, sxxexxBasic} {
		match, found, read := scanSxxExx(title, pattern)
		if found && read {
			return match, true
		}
		if found {
			// The leftmost match of this pattern was refused: the matcher list
			// would move to the next pattern, and the fast path does the same.
			continue
		}
	}
	return releaseMatch{}, false
}

// scanSxxExx scans for the leftmost match of one pattern. found reports a
// shape match, read whether its values were usable.
func scanSxxExx(title string, pattern sxxexxPattern) (match releaseMatch, found, read bool) {
	for i := 1; i < len(title); i++ {
		if title[i] != 's' && title[i] != 'S' {
			continue
		}
		if !isEpisodeSeparator(title[i-1]) {
			continue
		}
		sepStart := i - 1
		for sepStart > 0 && isEpisodeSeparator(title[sepStart-1]) {
			sepStart--
		}
		if sepStart == 0 {
			continue
		}
		season, pos := parseDigitsAt(title, i+1, 4)
		if season == "" {
			continue
		}
		var capture []string
		var end int
		switch pattern {
		case sxxexxRange:
			end, capture = sxxexxRangeCapture(title, pos, title[:sepStart], season)
		case sxxexxJoined:
			end, capture = sxxexxJoinedCapture(title, pos, title[:sepStart], season)
		case sxxexxBasic:
			end, capture = sxxexxBasicCapture(title, pos, title[:sepStart], season)
		}
		if end == 0 {
			continue
		}
		capture[0] = title[:end]
		var verdict matchVerdict
		switch pattern {
		case sxxexxRange:
			match, verdict = readEpisodeRange(capture)
		case sxxexxJoined:
			match, verdict = readJoinedEpisodes(capture)
		case sxxexxBasic:
			match, verdict = readSeasonEpisode(capture)
		}
		return match, true, verdict == matchRead
	}
	return releaseMatch{}, false, false
}

func episodeMarkerAt(text string, pos int) int {
	if pos < len(text) && (text[pos] == 'e' || text[pos] == 'E') {
		return pos + 1
	}
	return 0
}

func sxxexxRangeCapture(title string, pos int, series, season string) (int, []string) {
	pos = episodeMarkerAt(title, pos)
	if pos == 0 {
		return 0, nil
	}
	first, pos := parseDigitsAt(title, pos, 4)
	if first == "" {
		return 0, nil
	}
	dash := rangeDashLen(title, pos)
	if dash == 0 {
		return 0, nil
	}
	pos += dash
	if next := episodeMarkerAt(title, pos); next != 0 {
		pos = next
	}
	last, pos := parseDigitsAt(title, pos, 4)
	if last == "" || pos < len(title) && !isEpisodeSeparator(title[pos]) {
		return 0, nil
	}
	return pos, []string{"", series, season, first, last}
}

func sxxexxJoinedCapture(title string, pos int, series, season string) (int, []string) {
	joinedStart := pos
	groups := 0
	for {
		next := episodeMarkerAt(title, pos)
		if next == 0 {
			break
		}
		digits, end := parseDigitsAt(title, next, 4)
		if digits == "" {
			break
		}
		pos = end
		groups++
	}
	if groups < 2 || pos < len(title) && !isEpisodeSeparator(title[pos]) {
		return 0, nil
	}
	return pos, []string{"", series, season, title[joinedStart:pos]}
}

func sxxexxBasicCapture(title string, pos int, series, season string) (int, []string) {
	if pos < len(title) && isEpisodeSeparator(title[pos]) {
		pos++
	}
	pos = episodeMarkerAt(title, pos)
	if pos == 0 {
		return 0, nil
	}
	episode, pos := parseDigitsAt(title, pos, 4)
	if episode == "" || pos < len(title) && !isEpisodeSeparator(title[pos]) {
		return 0, nil
	}
	return pos, []string{"", series, season, episode}
}

// hasInProgressMarker reports an "in corso"/"in progress"/"incomplete" label,
// with the separators the earlier matcher accepts between the words. The pack
// fast path must not run when the in-progress matcher (which comes first) can.
func hasInProgressMarker(low string) bool {
	if strings.Contains(low, "incomplete") {
		return true
	}
	for i := 0; i+2 < len(low); i++ {
		if low[i] != 'i' || low[i+1] != 'n' {
			continue
		}
		if i > 0 && isWordByte(low[i-1]) {
			continue
		}
		j := i + 2
		for j < len(low) && isEpisodeSeparator(low[j]) {
			j++
		}
		if strings.HasPrefix(low[j:], "corso") || strings.HasPrefix(low[j:], "progress") {
			return true
		}
	}
	return false
}

// hasNXEpisodePattern is the "1x02" shape of matchers 4 and 5. It only gates
// the date fast path: when an x pattern exists the matcher list must decide.
func hasNXEpisodePattern(low string) bool {
	for i := 1; i+1 < len(low); i++ {
		if (low[i] == 'x' || low[i] == 'X') && isDigitByte(low[i-1]) && isDigitByte(low[i+1]) {
			return true
		}
	}
	return false
}

// parseDateFast reads the daily-release matcher "Show.2026-09-21" without its
// regex. It is the eighth pattern, so the list used to run seven regexes over
// a daily show before reaching it.
func parseDateFast(title string) (releaseMatch, bool) {
	for i := 1; i < len(title); i++ {
		if !isDigitByte(title[i]) || !isEpisodeSeparator(title[i-1]) {
			continue
		}
		year, pos := parseDigitsAt(title, i, 4)
		if len(year) != 4 || pos >= len(title) || (title[pos] != '-' && title[pos] != '.') {
			continue
		}
		month, pos := parseDigitsAt(title, pos+1, 2)
		if month == "" || pos >= len(title) || (title[pos] != '-' && title[pos] != '.') {
			continue
		}
		day, end := parseDigitsAt(title, pos+1, 2)
		if day == "" || (end < len(title) && !isEpisodeSeparator(title[end])) {
			continue
		}
		sepStart := i - 1
		for sepStart > 0 && isEpisodeSeparator(title[sepStart-1]) {
			sepStart--
		}
		if sepStart == 0 {
			continue
		}
		capture := []string{title[:end], title[:sepStart], year, month, day}
		parsed, verdict := readDate(capture)
		if verdict == matchRead {
			return parsed, true
		}
		return releaseMatch{}, false
	}
	return releaseMatch{}, false
}

// parseSeasonPackFast reads the "S01" pack shape of the tenth matcher. The
// pack readers ignore the "Complete" suffix, so the digits and the boundary
// are enough.
func parseSeasonPackFast(title string) (releaseMatch, bool) {
	for i := 1; i < len(title); i++ {
		if title[i] != 's' && title[i] != 'S' {
			continue
		}
		if !isEpisodeSeparator(title[i-1]) {
			continue
		}
		season, pos := parseDigitsAt(title, i+1, 2)
		if season == "" || (pos < len(title) && !isEpisodeSeparator(title[pos])) {
			continue
		}
		sepStart := i - 1
		for sepStart > 0 && isEpisodeSeparator(title[sepStart-1]) {
			sepStart--
		}
		if sepStart == 0 {
			continue
		}
		capture := []string{title[:pos], title[:sepStart], season}
		parsed, verdict := readSeasonPack(capture)
		if verdict == matchRead {
			return parsed, true
		}
		return releaseMatch{}, false
	}
	return releaseMatch{}, false
}

// parseReleaseTitle tries every matcher in order and returns what the title
// says about season and episode. A title no matcher reads is a movie.
func parseReleaseTitle(title string) releaseMatch {
	low := strings.ToLower(title)
	if !couldBeSeriesTitle(low) {
		return releaseMatch{}
	}
	if match, ok := parseSxxExxFast(title); ok {
		if match.episodeRange == nil {
			match.episodeRange = []int64{}
		}
		return match
	}
	// The x and season-word patterns come before the date: only try the date
	// fast path when they cannot match, or the list would have chosen them.
	if !hasNXEpisodePattern(low) && !strings.Contains(low, "season") && !strings.Contains(low, "stagione") {
		if match, ok := parseDateFast(title); ok {
			if match.episodeRange == nil {
				match.episodeRange = []int64{}
			}
			return match
		}
	}
	// The pack shape is the last matcher: try it only when none of the earlier
	// season, date and in-progress shapes can match.
	if !hasNXEpisodePattern(low) &&
		!strings.Contains(low, "season") && !strings.Contains(low, "stagione") &&
		!strings.Contains(low, "episodio") && !strings.Contains(low, "puntata") &&
		!strings.Contains(low, "episode") &&
		!hasInProgressMarker(low) {
		if match, ok := parseSeasonPackFast(title); ok {
			if match.episodeRange == nil {
				match.episodeRange = []int64{}
			}
			return match
		}
	}
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

// seriesName reads the series name out of a matcher's first capture.
func seriesName(capture []string) *string {
	if capture == nil || capture[1] == "" {
		return nil
	}
	value := strings.TrimSpace(strings.ReplaceAll(capture[1], ".", " "))
	// A resolution tag placed before SxxExx ("Show.2160p.S02E10") is not part
	// of the series name.
	value = trailingResolutionRe.ReplaceAllString(value, "")
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

// captureInt reads one numeric capture; ok is false when the group is absent
// or is not a number.
func captureInt(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

var episodeTokenRe = utils.MustCachedRegex(`(?i)e(\d{1,4})`)

// readEpisodeRange reads "S01E01-E03" and "S01E01-03".
func readEpisodeRange(capture []string) (releaseMatch, matchVerdict) {
	season, ok := captureInt(capture[2])
	if !ok {
		return releaseMatch{}, matchRefused
	}
	first, ok := captureInt(capture[3])
	if !ok {
		return releaseMatch{}, matchRefused
	}
	last := first
	if parsed, ok := captureInt(capture[4]); ok && parsed >= first {
		last = parsed
	}
	episodes := make([]int64, 0, last-first+1)
	for value := first; value <= last; value++ {
		episodes = append(episodes, value)
	}
	return releaseMatch{series: seriesName(capture), season: &season, episode: &first, episodeRange: episodes}, matchRead
}

// readJoinedEpisodes reads "S01E01E02E03".
func readJoinedEpisodes(capture []string) (releaseMatch, matchVerdict) {
	season, ok := captureInt(capture[2])
	if !ok {
		return releaseMatch{}, matchRefused
	}
	episodes := make([]int64, 0, 2)
	for _, token := range episodeTokenRe.FindAllStringSubmatch(capture[3], -1) {
		if value, ok := captureInt(token[1]); ok && value > 0 {
			episodes = append(episodes, value)
		}
	}
	if len(episodes) < 2 {
		return releaseMatch{}, matchRefused
	}
	first := episodes[0]
	return releaseMatch{series: seriesName(capture), season: &season, episode: &first, episodeRange: episodes}, matchRead
}

// readSeasonEpisode reads the ordinary "S01E01", "Season 1 Episode 2" and
// "Stagione 1 Puntata 2".
func readSeasonEpisode(capture []string) (releaseMatch, matchVerdict) {
	season, ok := captureInt(capture[2])
	if !ok {
		return releaseMatch{}, matchRefused
	}
	episode, ok := captureInt(capture[3])
	if !ok {
		return releaseMatch{}, matchRefused
	}
	return releaseMatch{series: seriesName(capture), season: &season, episode: &episode, episodeRange: []int64{episode}}, matchRead
}

// readNXRange reads "1x01-1x03" and "1x01-03".
func readNXRange(capture []string) (releaseMatch, matchVerdict) {
	season, ok := captureInt(capture[2])
	if !ok {
		return releaseMatch{}, matchRefused
	}
	first, ok := captureInt(capture[3])
	if !ok {
		return releaseMatch{}, matchRefused
	}
	if season < 1 || season > 40 || first > 99 {
		return releaseMatch{}, matchRefused
	}
	last := first
	if parsed, ok := captureInt(capture[4]); ok && parsed >= first {
		last = parsed
	}
	episodes := make([]int64, 0, last-first+1)
	for value := first; value <= last; value++ {
		episodes = append(episodes, value)
	}
	return releaseMatch{series: seriesName(capture), season: &season, episode: &first, episodeRange: episodes}, matchRead
}

// readNX reads "1x01".
func readNX(capture []string) (releaseMatch, matchVerdict) {
	season, ok := captureInt(capture[2])
	if !ok {
		return releaseMatch{}, matchRefused
	}
	if season < 1 || season > 40 {
		return releaseMatch{}, matchRefused
	}
	episode, ok := captureInt(capture[3])
	if !ok {
		return releaseMatch{}, matchRefused
	}
	if episode > 99 {
		return releaseMatch{}, matchRefused
	}
	return releaseMatch{series: seriesName(capture), season: &season, episode: &episode, episodeRange: []int64{episode}}, matchRead
}

// readDate reads a daily release "2024-05-12" as the season's day of year.
func readDate(capture []string) (releaseMatch, matchVerdict) {
	year, ok := captureInt(capture[2])
	if !ok {
		return releaseMatch{}, matchRefused
	}
	month, ok := captureInt(capture[3])
	if !ok {
		return releaseMatch{}, matchRefused
	}
	day, ok := captureInt(capture[4])
	if !ok {
		return releaseMatch{}, matchRefused
	}
	// The captures are int64 and the date components are int: bound them first,
	// so the conversion cannot wrap and a title such as "2024-13-45" is refused
	// instead of landing on a day invented by the wrap.
	if year < 1 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31 {
		return releaseMatch{}, matchRefused
	}
	yearInt, monthInt, dayInt := int(year), int(month), int(day)
	date := time.Date(yearInt, time.Month(monthInt), dayInt, 0, 0, 0, 0, time.UTC)
	if date.Year() != yearInt || int(date.Month()) != monthInt || date.Day() != dayInt {
		return releaseMatch{}, matchRefused
	}
	ordinal := int64(date.YearDay())
	return releaseMatch{series: seriesName(capture), season: &year, episode: &ordinal, episodeRange: []int64{ordinal}}, matchRead
}

// readInProgressPack reads a pack that only holds the first episodes of an
// airing season. A label with no usable count is dropped rather than read as
// a complete season: a complete season here would invent gaps up to the TMDB
// count and download the pack for nothing.
func readInProgressPack(capture []string) (releaseMatch, matchVerdict) {
	season, ok := captureInt(capture[2])
	if !ok {
		return releaseMatch{}, matchDropped
	}
	available, ok := captureInt(capture[3])
	if !ok || available < 1 || available > 999 {
		return releaseMatch{}, matchDropped
	}
	first := int64(1)
	episodes := make([]int64, 0, available)
	for value := int64(1); value <= available; value++ {
		episodes = append(episodes, value)
	}
	return releaseMatch{
		series:       seriesName(capture),
		season:       &season,
		episode:      &first,
		episodeRange: episodes,
		inProgress:   available,
	}, matchRead
}

// readSeasonPack reads "S01" and "Season 1 Complete": a whole season.
func readSeasonPack(capture []string) (releaseMatch, matchVerdict) {
	season, ok := captureInt(capture[2])
	if !ok {
		return releaseMatch{}, matchRefused
	}
	zero := int64(0)
	return releaseMatch{
		series:       seriesName(capture),
		season:       &season,
		episode:      &zero,
		episodeRange: []int64{0},
		seasonPack:   true,
	}, matchRead
}

// ParseEd2kRelease turns a Kad/eD2k result into a Release. eD2k names are
// free-form, so the title parsing is the same one used for torrents: a
// synthetic infohash built from the MD4 hash lets ParseRelease run its full
// identity inference, then the magnet is replaced by the eD2k link. Network
// and Sources are kept separate from SizeBytes/Seeders so the UI and the score
// cannot mistake an eD2k source count for a BitTorrent swarm.
func ParseEd2kRelease(name, link, source string, size, sources int64, discoveredAt time.Time) *models.Release {
	parsedLink, err := ed2k.ParseFileLink(link)
	if err != nil {
		return nil
	}
	if size <= 0 {
		size = parsedLink.Size
	}
	fakeMagnet := "magnet:?xt=urn:btih:" + parsedLink.Hash
	release := ParseReleaseAt(strings.TrimSpace(name), fakeMagnet, source, discoveredAt)
	if release == nil {
		// A name with no usable words still identifies a real file; keep a
		// minimal release instead of dropping the only result.
		release = &models.Release{
			Title:        strings.TrimSpace(name),
			Source:       source,
			Kind:         "movie",
			DiscoveredAt: discoveredAt,
			Seeders:      -1,
			Peers:        -1,
			Quality:      ParseQuality(name),
		}
	}
	release.Magnet = ""
	release.TorrentURL = nil
	release.Ed2kLink = parsedLink.String()
	release.Network = "ed2k"
	release.SizeBytes = size
	release.Sources = sources
	release.Seeders = -1
	release.Peers = -1
	return release
}

// parserASCIILower lowercases ASCII letters only, matching the
// `str::to_ascii_lowercase`.
func parserASCIILower(value string) string {
	var out strings.Builder
	out.Grow(len(value))
	for i := 0; i < len(value); i++ {
		b := value[i]
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		out.WriteByte(b)
	}
	return out.String()
}

func asciiLowerRune(character rune) rune {
	if character >= 'A' && character <= 'Z' {
		return character + ('a' - 'A')
	}
	return character
}

func isASCIIAlphanumeric(character rune) bool {
	return (character >= '0' && character <= '9') ||
		(character >= 'a' && character <= 'z') ||
		(character >= 'A' && character <= 'Z')
}

func allASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func optionalInt64Equal(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func int64SlicesEqual(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// trailingResolutionRe matches resolution tags left at the end of a series name.
var trailingResolutionRe = regexp.MustCompile(`(?i)(?:[ ]+(?:480p|576p|720p|1080p|1080i|2160p|4320p|4k|uhd))+$`)

var (
	yearTokenRe      = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
	firstTechTokenRe = regexp.MustCompile(`(?i)\b(480p|576p|720p|1080p|1080i|2160p|4320p|4k|uhd|bluray|bdrip|web[ ._-]?dl|webrip|hdtv|dvdrip|remux)\b`)
)

// releaseYear picks the release year of a title. A number that is part of the
// title ("1917", "2001: A Space Odyssey", "Wonder Woman 1984") must not win over
// the real year that follows it, so the last year token before the first
// technical tag (resolution, source) is used.
func releaseYear(title string) *int64 {
	scope := title
	if loc := firstTechTokenRe.FindStringIndex(title); loc != nil {
		scope = title[:loc[0]]
	}
	matches := yearTokenRe.FindAllStringSubmatch(scope, -1)
	if len(matches) == 0 {
		// No year before the technical tags: accept the first one anywhere.
		matches = yearTokenRe.FindAllStringSubmatch(title, 1)
		if len(matches) == 0 {
			return nil
		}
		value, err := strconv.ParseInt(matches[0][1], 10, 64)
		if err != nil {
			return nil
		}
		return &value
	}
	value, err := strconv.ParseInt(matches[len(matches)-1][1], 10, 64)
	if err != nil {
		return nil
	}
	return &value
}

// Anime releases number episodes from the first one onwards, without seasons:
// "[SubsPlease] One Piece - 1071 (1080p)", "One Piece Ep 1071 SUB ITA",
// "One.Piece.1071.SUB.ITA".
var (
	absoluteGroupDashRe = regexp.MustCompile(`^\[[^\]]*\][ ._]*(.+?)[ ._]+-[ ._]+(\d{1,4})(?:v\d)?(?:[ ._]*[\[(]|[ ._]|$)`)
	absoluteDashRe      = regexp.MustCompile(`^(.+?)[ ._]+-[ ._]+(\d{1,4})(?:v\d)?[ ._]*[\[(]`)
	absoluteEpRe        = regexp.MustCompile(`(?i)^(.+?)[ ._-]+(?:ep|episodio|episode)\.?[ ._-]*(\d{1,4})(?:v\d)?(?:[ ._-]|$)`)
	absoluteLangRe      = regexp.MustCompile(`(?i)^(.+?)[ ._-]+(\d{1,4})(?:v\d)?[ ._-]+(?:sub[ ._-]?ita|ita|eng|vostfr|multi)(?:[ ._-]|$)`)
	leadingGroupRe      = regexp.MustCompile(`^\[[^\]]*\][ ._]*`)
)

var parseAbsoluteEpisodeReplacer = strings.NewReplacer(".", " ", "_", " ")

// parseAbsoluteEpisode returns the series name and absolute episode number of
// an anime-style title, or nil when the title does not look like one.
func parseAbsoluteEpisode(title string) (*string, *int64) {
	for index, pattern := range []*regexp.Regexp{absoluteGroupDashRe, absoluteDashRe, absoluteEpRe, absoluteLangRe} {
		capture := pattern.FindStringSubmatch(title)
		if capture == nil {
			continue
		}
		number, err := strconv.ParseInt(capture[2], 10, 64)
		if err != nil || number <= 0 {
			continue
		}
		// "Title 2021 ITA" is a year, not episode 2021.
		if index == 3 && len(capture[2]) == 4 && number >= 1900 && number <= 2099 {
			continue
		}
		name := leadingGroupRe.ReplaceAllString(capture[1], "")
		name = parseAbsoluteEpisodeReplacer.Replace(name)
		name = trailingResolutionRe.ReplaceAllString(strings.TrimSpace(name), "")
		name = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(name), "-"))
		if name == "" {
			continue
		}
		return &name, &number
	}
	return nil, nil
}
