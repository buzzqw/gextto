package gextto

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

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
	normalized = strings.NewReplacer(
		".", " ", "_", " ", "-", " ", "/", " ", "\\", " ", ":", " ",
	).Replace(normalized)
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
	left := matchingName(stripYearTokens(NormalizeSeriesName(a)))
	right := matchingName(stripYearTokens(NormalizeSeriesName(b)))
	if tokensMatch(left, right) ||
		tokensMatchWithReleaseSuffix(left, right) ||
		tokensMatchWithReleaseSuffix(right, left) {
		return true
	}
	// Titoli "stilizzati" (es. `PLUR1BUS` per *Pluribus*): confronta una
	// variante con le cifre sciolte in lettere, ma solo quando una sola delle
	// due parti contiene cifre. Così non si confondono titoli numerici come
	// `9-1-1` e non si altera il confronto tra nomi già uguali.
	if hasASCIIDigit(left) != hasASCIIDigit(right) {
		foldedLeft := leetFold(left)
		foldedRight := leetFold(right)
		if tokensMatch(foldedLeft, foldedRight) ||
			tokensMatchWithReleaseSuffix(foldedLeft, foldedRight) ||
			tokensMatchWithReleaseSuffix(foldedRight, foldedLeft) {
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
	var words []string
	var current strings.Builder
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
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
	var compacted []string
	index := 0
	for index < len(words) {
		if len([]rune(words[index])) == 1 {
			start := index
			for index < len(words) && len([]rune(words[index])) == 1 {
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

// tokensMatchWithReleaseSuffix allows harmless suffixes commonly left in the
// series prefix by indexers: bare years, season/episode markers and technical
// tags. It intentionally only accepts suffixes, so `New Tricks` does not match
// `Old Dog New Tricks`.
func tokensMatchWithReleaseSuffix(configured, candidate string) bool {
	configuredWords := strings.Fields(configured)
	candidateWords := strings.Fields(candidate)
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

// tokensMatch confronta due nomi già normalizzati: uguaglianza oppure token a
// token con tolleranza per il plurale finale (`Show`/`Shows`).
func tokensMatch(left, right string) bool {
	if left == right {
		return true
	}
	leftWords := strings.Fields(left)
	rightWords := strings.Fields(right)
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
func ParseQuality(title string) models.Quality {
	low := strings.ToLower(title)
	// legacy normalisations: `t_norm` replaces [._-] with spaces; `t_norm_lang`
	// also removes square brackets so renamed files like "[DV HDR10][IT][h265]"
	// are recognised.
	tNormLang := utils.MustCachedRegex(`[._ \-\[\]]`).ReplaceAllString(low, " ")

	// --- Riscrittura dei marker di sottotitoli prima del rilevamento lingua ---
	// Il feed può riportare "ENG ... Sub ita ..." anche quando l'audio è solo
	// inglese: se non rimossi, "sub ita" verrebbe letto come audio italiano.
	const (
		subtitleMarker = `(?:subs?|subforced|subtitles?|forced|sdh|cc|closedcaptions?)`
		subtitleSep    = `[\s._\-/\\\[\]()+|,:;]*`
		subtitleLang   = `(?:[a-z]{2,3}|english|italian|italiano|german|deutsch|french|francais|spanish|portuguese|japanese|chinese|korean|russian)`
	)
	subtitleTags := `\b` + subtitleMarker + subtitleSep +
		`(?:` + subtitleLang + subtitleSep + `)*(?:it|ita|italian|italiano)\b|\b(?:it|ita|italian|italiano)` +
		subtitleSep + subtitleMarker + `\b`
	reSubtitle := utils.MustCachedRegex(subtitleTags)
	tNormLangAudio := reSubtitle.ReplaceAllString(tNormLang, " ")
	tAudio := reSubtitle.ReplaceAllString(low, " ")

	// Tag dei servizi streaming che contengono "it"/"nf" e generano falsi
	// positivi (iT = iTunes, NF = Netflix). La libreria `regex` non supporta i
	// look-ahead, quindi "it web" viene sostituito con il solo " web".
	stripStreaming := func(text string) string {
		withoutItWeb := utils.MustCachedRegex(`(?i)\b(it|nf)(\s+web)\b`).ReplaceAllString(text, "$2")
		return utils.MustCachedRegex(`(?i)\bitunes\b|\bamzn\b|\bdsnp\b|\bhmax\b|\bparamount\b`).ReplaceAllString(withoutItWeb, " ")
	}

	// --- Lingua italiana: tre livelli, dal più sicuro al più specifico ---
	level1 := utils.MustCachedRegex(`\bita\b|\bitalian\b|\bitaliano\b`).MatchString(stripStreaming(tNormLangAudio))
	level2 := utils.MustCachedRegex(`\bit[\+\|]|\[it\]`).MatchString(tAudio)
	ita := false
	if level1 || level2 {
		ita = true
	} else {
		// Livello 3: "\bit\b" solo dalla parte tecnica in poi, per non colpire
		// parole del titolo come "It Chapter" o "Feel It Still".
		resolutionRe := utils.MustCachedRegex(
			`\b(2160p?|1080p?|720p?|480p?|4k|uhd|bluray|web[\s\-]?dl|webrip|hdtv)\b`,
		)
		if found := resolutionRe.FindStringIndex(tNormLangAudio); found != nil {
			tech := stripStreaming(tNormLangAudio[found[0]:])
			ita = utils.MustCachedRegex(`\bit\b`).MatchString(tech) &&
				!utils.MustCachedRegex(
					`\bwith\b|\bbit\b|\bsplit\b|\bedit\b|\bunit\b|\bvisit\b|\blimit\b|\bexit\b|\bprofit\b|\bsubmit\b|\bcommit\b|\bpermit\b|\badmit\b|\bomit\b|\bhit\b|\bkit\b|\bpit\b|\bsit\b|\bfit\b|\bwit\b|\bknit\b|\bspit\b|\bslit\b|\bitunes\b`,
				).MatchString(tech)
		}
	}

	subtitleIta := utils.MustCachedRegex(
		`(?i)(sub|subs|subtitle|subtitles)[._ \-\[\]\(\)+]*(ita|italian|it)([._ \-\[\]\(\)+]|$)`,
	).MatchString(title)
	subtitleEng := utils.MustCachedRegex(
		`(?i)(sub|subs|subtitle|subtitles)[._ \-\[\]\(\)+]*(eng|english|en)([._ \-\[\]\(\)+]|$)`,
	).MatchString(title)
	eng := !subtitleEng &&
		utils.MustCachedRegex(`(?i)(^|[._ \-\[\]\(\)+])(eng|english|en)([._ \-\[\]\(\)+]|$)`).MatchString(title)
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
	case strings.Contains(low, "720p") || hdWordRe.MatchString(low):
		resolution = "720p"
	case strings.Contains(low, "576p") || palWordRe.MatchString(low):
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
	case utils.MustCachedRegex(`\bhdr\b|\bhlg\b`).MatchString(tNormLang):
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

	// --- Audio (ordine di priorità legacy) ---
	audio := "unknown"
	switch {
	case strings.Contains(low, "dts-hd") || strings.Contains(low, "dtshd"):
		audio = "dts-hd"
	case strings.Contains(low, "dts"):
		audio = "dts"
	case strings.Contains(low, "ddp5.1") || strings.Contains(low, "ddp 5.1") || strings.Contains(low, "eac3"):
		audio = "ddp"
	case strings.Contains(low, "ac3") || strings.Contains(low, "dd5.1"):
		audio = "ac3"
	case audio51Re.MatchString(low):
		audio = "5.1"
	case strings.Contains(low, "mp3"):
		audio = "mp3"
	case strings.Contains(low, "aac"):
		audio = "aac"
	}

	// --- Gruppo ---
	group := "unknown"
	if groups := utils.MustCachedRegex(`[-]([a-z0-9]+)$|\[([a-z0-9]+)\]$`).FindStringSubmatch(low); groups != nil {
		// La prima alternativa vince quando il primo gruppo partecipa.
		if groups[1] != "" {
			group = groups[1]
		} else if groups[2] != "" {
			group = groups[2]
		}
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
		Resolution: resolution,
		Source:     source,
		Codec:      codec,
		Audio:      audio,
		HDR:        hdr,
		Group:      group,
		IsIta:      ita,
		IsDV:       isDV,
		IsRepack:   strings.Contains(low, "repack") || strings.Contains(low, "rerip"),
		IsProper:   strings.Contains(low, "proper"),
		IsReal:     utils.MustCachedRegex(`\breal\b`).MatchString(low),
		Language:   language,
		Languages:  languages,
		HasSubtitle: utils.MustCachedRegex(
			`(?i)(^|[._ \-\[\]\(\)+])(sub|subs|subtitle|subtitles)([._ \-\[\]\(\)+]|$)`,
		).MatchString(title),
		SubtitleLanguages: subtitleLanguages,
		HardcodedSubs: utils.MustCachedRegex(
			`(?i)(^|[._ \-\[\]\(\)+])(hc|hardcoded|hardsub|hardsubs)([._ \-\[\]\(\)+]|$)`,
		).MatchString(title),
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
	// Ordine di riconoscimento come legacy: range, multi-episodio concatenato,
	// SxxExx singolo, NxNN, data (YYYY-MM-DD), stagione completa.
	rangeRe := utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+s(\d{1,4})e(\d{1,4})[-–]e?(\d{1,4})(?:[ ._-]|$)`)
	multiRe := utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+s(\d{1,4})((?:e\d{1,4}){2,})(?:[ ._-]|$)`)
	standardRe := utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+s(\d{1,4})e(\d{1,4})(?:[ ._-]|$)`)
	nxRe := utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+(\d{1,2})x(\d{1,4})(?:[ ._-]|$)`)
	itaRe := utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+stagione[ ._-]*(\d{1,2})[ ._-]+(?:episodio|puntata|ep\.?)[ ._-]*(\d{1,4})(?:[ ._-]|$)`)
	dateRe := utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+(\d{4})[-.](\d{1,2})[-.](\d{1,2})(?:[ ._-]|$)`)
	seasonPackRe := utils.MustCachedRegex(`(?i)^(.+?)[ ._-]+(?:stagione[ ._-]*|season[ ._-]?|s)(\d{1,2})(?:[ ._-]+(?:complete|completa))?(?:[ ._-]|$)`)
	seriesName := func(capture []string) *string {
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
	episodeTokenRe := utils.MustCachedRegex(`(?i)e(\d{1,4})`)

	var (
		series       *string
		season       *int64
		episode      *int64
		episodeRange = []int64{}
		seasonPack   bool
	)
	if capture := rangeRe.FindStringSubmatch(title); capture != nil {
		seasonValue, err := strconv.ParseInt(capture[2], 10, 64)
		if err != nil {
			return nil
		}
		episodeValue, err := strconv.ParseInt(capture[3], 10, 64)
		if err != nil {
			return nil
		}
		end := episodeValue
		if capture[4] != "" {
			if parsed, err := strconv.ParseInt(capture[4], 10, 64); err == nil && parsed >= episodeValue {
				end = parsed
			}
		}
		series = seriesName(capture)
		season = &seasonValue
		episode = &episodeValue
		for value := episodeValue; value <= end; value++ {
			episodeRange = append(episodeRange, value)
		}
	} else if capture := multiRe.FindStringSubmatch(title); capture != nil {
		seasonValue, err := strconv.ParseInt(capture[2], 10, 64)
		if err != nil {
			return nil
		}
		joined := capture[3]
		var episodes []int64
		for _, match := range episodeTokenRe.FindAllStringSubmatch(joined, -1) {
			if value, err := strconv.ParseInt(match[1], 10, 64); err == nil && value > 0 {
				episodes = append(episodes, value)
			}
		}
		if len(episodes) >= 2 {
			series = seriesName(capture)
			season = &seasonValue
			first := episodes[0]
			episode = &first
			episodeRange = episodes
		}
	} else if capture := standardRe.FindStringSubmatch(title); capture != nil {
		seasonValue, err := strconv.ParseInt(capture[2], 10, 64)
		if err != nil {
			return nil
		}
		episodeValue, err := strconv.ParseInt(capture[3], 10, 64)
		if err != nil {
			return nil
		}
		series = seriesName(capture)
		season = &seasonValue
		episode = &episodeValue
		episodeRange = []int64{episodeValue}
	} else if capture := nxRe.FindStringSubmatch(title); capture != nil {
		seasonValue, err := strconv.ParseInt(capture[2], 10, 64)
		if err != nil {
			return nil
		}
		if seasonValue >= 1 && seasonValue <= 40 {
			episodeValue, err := strconv.ParseInt(capture[3], 10, 64)
			if err != nil {
				return nil
			}
			if episodeValue <= 99 {
				series = seriesName(capture)
				season = &seasonValue
				episode = &episodeValue
				episodeRange = []int64{episodeValue}
			}
		}
	} else if capture := itaRe.FindStringSubmatch(title); capture != nil {
		seasonValue, err := strconv.ParseInt(capture[2], 10, 64)
		if err != nil {
			return nil
		}
		episodeValue, err := strconv.ParseInt(capture[3], 10, 64)
		if err != nil {
			return nil
		}
		series = seriesName(capture)
		season = &seasonValue
		episode = &episodeValue
		episodeRange = []int64{episodeValue}
	} else if capture := dateRe.FindStringSubmatch(title); capture != nil {
		year, err := strconv.ParseInt(capture[2], 10, 32)
		if err != nil {
			return nil
		}
		month, err := strconv.ParseInt(capture[3], 10, 32)
		if err != nil {
			return nil
		}
		day, err := strconv.ParseInt(capture[4], 10, 32)
		if err != nil {
			return nil
		}
		date := time.Date(int(year), time.Month(month), int(day), 0, 0, 0, 0, time.UTC)
		if date.Year() == int(year) && int(date.Month()) == int(month) && date.Day() == int(day) {
			ordinal := int64(date.YearDay())
			series = seriesName(capture)
			season = &year
			episode = &ordinal
			episodeRange = []int64{ordinal}
		}
	} else if capture := seasonPackRe.FindStringSubmatch(title); capture != nil {
		seasonValue, err := strconv.ParseInt(capture[2], 10, 64)
		if err != nil {
			return nil
		}
		zero := int64(0)
		series = seriesName(capture)
		season = &seasonValue
		episode = &zero
		episodeRange = []int64{0}
		seasonPack = true
	}
	kind := "movie"
	if season != nil {
		kind = "series"
	}
	var absoluteSeries *string
	var absoluteEpisode *int64
	if season == nil {
		absoluteSeries, absoluteEpisode = parseAbsoluteEpisode(title)
	}
	year := releaseYear(title)
	// E00 is a real special/recap episode, not a complete-season pack. Only
	// titles matched by seasonPackRe use the {0} range as a pack sentinel.
	isPack := len(episodeRange) > 1 || seasonPack
	return &models.Release{
		Title:           title,
		Magnet:          sanitizedMagnet,
		TorrentURL:      normalizedURL,
		Source:          source,
		Quality:         ParseQuality(title),
		Kind:            kind,
		Series:          series,
		Season:          season,
		Episode:         episode,
		IsPack:          isPack,
		EpisodeRange:    episodeRange,
		AbsoluteEpisode: absoluteEpisode,
		AbsoluteSeries:  absoluteSeries,
		Year:            year,
		DiscoveredAt:    discoveredAt,
		SizeBytes:       0,
		Seeders:         -1,
		Peers:           -1,
	}
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

var (
	// "5.1" must stand alone: it also appears across digits in "x265.10bit" or
	// "2024.05.12".
	audio51Re = regexp.MustCompile(`(?:^|[^0-9])5\.1(?:[^0-9]|$)`)
	hdWordRe  = regexp.MustCompile(`(?:^|[^a-z0-9-])hd(?:[^a-z0-9-]|$)|hdtv`)
	palWordRe = regexp.MustCompile(`\bpal\b`)
)

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
		name = strings.NewReplacer(".", " ", "_", " ").Replace(name)
		name = trailingResolutionRe.ReplaceAllString(strings.TrimSpace(name), "")
		name = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(name), "-"))
		if name == "" {
			continue
		}
		return &name, &number
	}
	return nil, nil
}
