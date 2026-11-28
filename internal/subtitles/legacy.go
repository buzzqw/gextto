package subtitles

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultLegacyBaseURL is the OpenSubtitles.org XML-RPC endpoint. It is a
// different service from the REST API in DefaultBaseURL: the same account and
// API key do not necessarily work on both, so it is an explicit provider
// choice.
const DefaultLegacyBaseURL = "https://api.opensubtitles.org/xml-rpc"

// LegacyClient speaks the OpenSubtitles.org XML-RPC API. Anonymous login works
// without credentials; a username and password are optional.
type LegacyClient struct {
	cfg     Config
	baseURL string
	mu      sync.Mutex
	loginMu sync.Mutex
	token   string
}

type legacyValue struct {
	String string       `xml:"string"`
	Int    string       `xml:"int"`
	Double string       `xml:"double"`
	Array  legacyArray  `xml:"array"`
	Struct legacyStruct `xml:"struct"`
}

type legacyArray struct {
	Values []legacyValue `xml:"data>value"`
}

type legacyStruct struct {
	Members []legacyMember `xml:"member"`
}

type legacyMember struct {
	Name  string      `xml:"name"`
	Value legacyValue `xml:"value"`
}

type legacyResponse struct {
	Params []legacyValue `xml:"params>param>value"`
}

func NewLegacy(cfg Config) (*LegacyClient, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = DefaultLegacyBaseURL
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("invalid subtitle provider URL")
	}
	if strings.TrimSpace(cfg.UserAgent) == "" {
		cfg.UserAgent = "Gextto"
	}
	if cfg.Client == nil {
		origin, _ := url.Parse(base)
		cfg.Client = &http.Client{
			Timeout:   30 * time.Second,
			Transport: newSubtitleTransport(origin),
		}
	}
	return &LegacyClient{cfg: cfg, baseURL: base}, nil
}

func (c *LegacyClient) login(ctx context.Context) error {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	c.mu.Lock()
	hasToken := c.token != ""
	c.mu.Unlock()
	if hasToken {
		return nil
	}
	login := func(username, password string) (string, error) {
		top, err := c.callRaw(ctx, "LogIn", []string{
			legacyString(username), legacyString(password),
			legacyString("en"), legacyString(c.cfg.UserAgent),
		})
		if err != nil {
			return "", err
		}
		if status := legacyStatus(top); !strings.HasPrefix(status, "200") {
			return "", fmt.Errorf("subtitle provider login returned %s", status)
		}
		token := legacyText(legacyFields(top)["token"])
		if token == "" {
			return "", errors.New("subtitle provider login returned no token")
		}
		return token, nil
	}
	token, err := "", error(nil)
	if c.cfg.Username != "" || c.cfg.Password != "" {
		token, err = login(c.cfg.Username, c.cfg.Password)
	}
	if token == "" {
		token, err = login("", "")
	}
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.token = token
	c.mu.Unlock()
	return nil
}

func (c *LegacyClient) callToken(ctx context.Context, method string, extra []string) (legacyValue, error) {
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		if err := c.login(ctx); err != nil {
			return legacyValue{}, err
		}
		c.mu.Lock()
		token := c.token
		c.mu.Unlock()
		params := append([]string{legacyString(token)}, extra...)
		top, err := c.callRaw(ctx, method, params)
		if err != nil {
			return legacyValue{}, err
		}
		status := legacyStatus(top)
		if strings.HasPrefix(status, "200") {
			return top, nil
		}
		last = fmt.Errorf("subtitle provider %s returned %s", method, status)
		if attempt == 0 && (strings.HasPrefix(status, "401") || strings.HasPrefix(status, "406")) {
			c.mu.Lock()
			c.token = ""
			c.mu.Unlock()
			continue
		}
		break
	}
	return legacyValue{}, last
}

func (c *LegacyClient) callRaw(ctx context.Context, method string, params []string) (legacyValue, error) {
	body := legacyMethodCall(method, params)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return legacyValue{}, err
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("Accept", "text/xml")
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	response, err := c.cfg.Client.Do(req)
	if err != nil {
		return legacyValue{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return legacyValue{}, fmt.Errorf("subtitle provider returned HTTP %d", response.StatusCode)
	}
	var payload legacyResponse
	decoder := xml.NewDecoder(io.LimitReader(response.Body, 8<<20))
	if err := decoder.Decode(&payload); err != nil {
		return legacyValue{}, fmt.Errorf("read subtitle provider answer: %w", err)
	}
	if len(payload.Params) == 0 {
		return legacyValue{}, errors.New("subtitle provider returned an empty answer")
	}
	return payload.Params[0], nil
}

func (c *LegacyClient) Search(ctx context.Context, query SearchQuery) ([]Subtitle, error) {
	fields := [][2]string{{"sublanguageid", legacyLanguages(query.Language)}}
	if id := strings.TrimPrefix(strings.TrimSpace(query.IMDbID), "tt"); id != "" {
		fields = append(fields, [2]string{"imdbid", id})
	} else {
		title := strings.TrimSpace(query.Title)
		if title != "" && strings.EqualFold(strings.TrimSpace(query.Kind), "movie") && query.Year > 0 {
			title = fmt.Sprintf("%s %d", title, query.Year)
		}
		if title != "" {
			fields = append(fields, [2]string{"query", title})
		}
	}
	if query.Season > 0 {
		fields = append(fields, [2]string{"season", strconv.Itoa(query.Season)})
	}
	if query.Episode > 0 {
		fields = append(fields, [2]string{"episode", strconv.Itoa(query.Episode)})
	}
	fields = append(fields, [2]string{"limit", "100"})
	top, err := c.callToken(ctx, "SearchSubtitles", []string{legacyStructArray(fields)})
	if err != nil {
		return nil, err
	}
	subs := legacyFields(top)["data"].Array.Values
	out := make([]Subtitle, 0, len(subs))
	for _, item := range subs {
		fields := legacyFields(item)
		format := strings.ToLower(strings.TrimSpace(legacyText(fields["SubFormat"])))
		if format != "" && format != "srt" {
			continue
		}
		fileID := legacyInt(fields["IDSubtitleFile"])
		if fileID <= 0 {
			continue
		}
		language := strings.ToLower(strings.TrimSpace(legacyText(fields["ISO639"])))
		if language == "" {
			language = legacyLanguageCode(legacyText(fields["SubLanguageID"]))
		}
		release := strings.TrimSpace(legacyText(fields["MovieReleaseName"]))
		if release == "" {
			release = legacyText(fields["SubFileName"])
		}
		matchedBy := strings.ToLower(legacyText(fields["MatchedBy"]))
		trusted := legacyBool(fields["SubFromTrusted"]) || strings.EqualFold(legacyText(fields["UserRank"]), "trusted")
		ai := legacyBool(fields["SubAutoTranslation"])
		machine := false
		hearing := legacyBool(fields["SubHearingImpaired"])
		forced := legacyBool(fields["SubForeignPartsOnly"])
		fps, _ := strconv.ParseFloat(strings.TrimSpace(legacyText(fields["MovieFPS"])), 64)
		sub := Subtitle{
			ID:                flexibleID(fileID),
			SubtitleID:        flexibleID(legacyInt(fields["IDSubtitle"])),
			Language:          language,
			ReleaseName:       release,
			Releases:          []string{release},
			Files:             []File{{ID: flexibleID(fileID), Name: legacyText(fields["SubFileName"])}},
			HashMatch:         strings.Contains(matchedBy, "hash"),
			SizeMatch:         strings.Contains(matchedBy, "size"),
			IMDbMatch:         strings.Contains(matchedBy, "imdb"),
			Title:             legacyText(fields["MovieName"]),
			Year:              int(legacyInt(fields["MovieYear"])),
			Season:            int(legacyInt(fields["SeriesSeason"])),
			Episode:           int(legacyInt(fields["SeriesEpisode"])),
			DownloadCount:     int(legacyInt(fields["SubDownloadsCnt"])),
			FPS:               fps,
			HearingImpaired:   &hearing,
			ForeignPartsOnly:  &forced,
			FromTrusted:       &trusted,
			AITranslated:      &ai,
			MachineTranslated: &machine,
		}
		if sub.Title == "" {
			sub.Title = release
		}
		out = append(out, sub)
	}
	return out, nil
}

func (c *LegacyClient) Download(ctx context.Context, fileID int64, format string) (Download, error) {
	if fileID <= 0 {
		return Download{}, errors.New("subtitle file id is required")
	}
	top, err := c.callToken(ctx, "DownloadSubtitles", []string{legacyStringArray([]string{strconv.FormatInt(fileID, 10)})})
	if err != nil {
		return Download{}, err
	}
	items := legacyFields(top)["data"].Array.Values
	if len(items) == 0 {
		return Download{}, errors.New("subtitle provider returned no download link")
	}
	fields := legacyFields(items[0])
	linkText := strings.TrimSpace(legacyText(fields["Link"]))
	if linkText == "" {
		return Download{}, errors.New("subtitle provider returned no download link")
	}
	link, err := url.Parse(linkText)
	if err != nil || !link.IsAbs() || (link.Scheme != "http" && link.Scheme != "https") {
		return Download{}, errors.New("invalid subtitle download link")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link.String(), nil)
	if err != nil {
		return Download{}, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	response, err := c.cfg.Client.Do(req)
	if err != nil {
		return Download{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Download{}, fmt.Errorf("subtitle file download returned HTTP %d", response.StatusCode)
	}
	buffered := bufio.NewReader(response.Body)
	reader := io.Reader(buffered)
	if magic, _ := buffered.Peek(2); len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		compressed, gzipErr := gzip.NewReader(buffered)
		if gzipErr != nil {
			return Download{}, fmt.Errorf("read compressed subtitle: %w", gzipErr)
		}
		defer compressed.Close()
		reader = compressed
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxSubtitleBytes+1))
	if err != nil {
		return Download{}, err
	}
	if len(data) > maxSubtitleBytes {
		return Download{}, errors.New("subtitle file is too large")
	}
	if len(data) == 0 {
		return Download{}, errors.New("subtitle file is empty")
	}
	name := filepathBase(strings.TrimSpace(legacyText(fields["SubFileName"])))
	if name == "" {
		name = "subtitle.srt"
	}
	name = strings.TrimSuffix(name, ".gz")
	if format = strings.TrimSpace(format); format != "" && !strings.HasSuffix(strings.ToLower(name), "."+strings.ToLower(format)) {
		name = strings.TrimSuffix(name, filepathExt(name)) + "." + strings.ToLower(format)
	}
	return Download{Data: data, FileName: name}, nil
}

func filepathExt(name string) string {
	index := strings.LastIndexByte(name, '.')
	if index <= 0 {
		return ""
	}
	return name[index:]
}

func legacyFields(value legacyValue) map[string]legacyValue {
	fields := make(map[string]legacyValue, len(value.Struct.Members))
	for _, member := range value.Struct.Members {
		fields[member.Name] = member.Value
	}
	return fields
}

func legacyStatus(top legacyValue) string {
	return legacyText(legacyFields(top)["status"])
}

func legacyText(value legacyValue) string {
	if value.String != "" {
		return value.String
	}
	if value.Int != "" {
		return value.Int
	}
	if value.Double != "" {
		return value.Double
	}
	return ""
}

func legacyInt(value legacyValue) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(legacyText(value)), 10, 64)
	return parsed
}

func legacyBool(value legacyValue) bool {
	switch strings.ToLower(strings.TrimSpace(legacyText(value))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func legacyLanguages(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "all") {
		return "all"
	}
	out := []string{}
	for _, code := range strings.Split(raw, ",") {
		if mapped := legacyLanguage3[strings.ToLower(strings.TrimSpace(code))]; mapped != "" {
			out = append(out, mapped)
		}
	}
	if len(out) == 0 {
		return "all"
	}
	return strings.Join(out, ",")
}

var legacyLanguage3 = map[string]string{
	"ar": "ara", "da": "dan", "de": "ger", "en": "eng", "es": "spa", "fi": "fin",
	"fr": "fre", "he": "heb", "hi": "hin", "it": "ita", "ja": "jpn", "ko": "kor",
	"nl": "dut", "no": "nor", "pl": "pol", "pt": "por", "ru": "rus", "sv": "swe",
	"tr": "tur", "zh": "chi",
}

var legacyLanguage2 = map[string]string{
	"ara": "ar", "dan": "da", "ger": "de", "deu": "de", "eng": "en", "spa": "es",
	"fin": "fi", "fre": "fr", "fra": "fr", "heb": "he", "hin": "hi", "ita": "it",
	"jpn": "ja", "kor": "ko", "dut": "nl", "nld": "nl", "nor": "no", "pol": "pl",
	"por": "pt", "rus": "ru", "swe": "sv", "tur": "tr", "chi": "zh", "zho": "zh",
}

func legacyLanguageCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if mapped := legacyLanguage2[code]; mapped != "" {
		return mapped
	}
	if len(code) == 2 {
		return code
	}
	return code
}

func legacyMethodCall(method string, params []string) []byte {
	var out bytes.Buffer
	out.WriteString(`<?xml version="1.0"?><methodCall><methodName>`)
	xml.EscapeText(&out, []byte(method))
	out.WriteString(`</methodName><params>`)
	for _, param := range params {
		out.WriteString("<param>")
		out.WriteString(param)
		out.WriteString("</param>")
	}
	out.WriteString(`</params></methodCall>`)
	return out.Bytes()
}

func legacyString(value string) string {
	var out bytes.Buffer
	out.WriteString("<value><string>")
	xml.EscapeText(&out, []byte(value))
	out.WriteString("</string></value>")
	return out.String()
}

func legacyStructArray(fields [][2]string) string {
	var out bytes.Buffer
	out.WriteString("<value><array><data><value><struct>")
	for _, field := range fields {
		out.WriteString("<member><name>")
		xml.EscapeText(&out, []byte(field[0]))
		out.WriteString("</name>")
		out.WriteString(legacyString(field[1]))
		out.WriteString("</member>")
	}
	out.WriteString("</struct></value></data></array></value>")
	return out.String()
}

func legacyStringArray(values []string) string {
	var out bytes.Buffer
	out.WriteString("<value><array><data>")
	for _, value := range values {
		out.WriteString(legacyString(value))
	}
	out.WriteString("</data></array></value>")
	return out.String()
}
