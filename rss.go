package gextto

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/cache"
	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"

	"golang.org/x/net/html"
)

// local_name returns the lowercased local part of a possibly namespaced XML
// name (`torznab:attr` -> `attr`), mirroring the helper.
func local_name(value string) string {
	if index := strings.LastIndexByte(value, ':'); index >= 0 {
		value = value[index+1:]
	}
	return strings.ToLower(value)
}

// magnet_in extracts the first `magnet:?` candidate from arbitrary text,
// stopping at whitespace or a delimiter. Returns "" when none is found.
func magnet_in(value string) string {
	start := strings.Index(value, "magnet:?")
	if start < 0 {
		return ""
	}
	candidate := value[start:]
	end := len(candidate)
	for index := 0; index < len(candidate); index++ {
		switch candidate[index] {
		case ' ', '\t', '\n', '\r', '\v', '\f', '<', '>', '"', '\'', ')':
			end = index
		}
		if end != len(candidate) {
			break
		}
	}
	magnet := candidate[:end]
	if strings.HasPrefix(magnet, "magnet:?") {
		return magnet
	}
	return ""
}

// attribute returns the value of an XML attribute matched by local name.
func attribute(start xml.StartElement, name string) (string, bool) {
	for _, attr := range start.Attr {
		if local_name(attr.Name.Local) == name {
			return attr.Value, true
		}
	}
	return "", false
}

// apply_torznab_attr reads a Torznab `<...:attr name="seeders" value="5"/>`
// element into the per-item facts. Unknown or non-numeric values reset the
// current value to None.
func apply_torznab_attr(
	start xml.StartElement,
	sizeBytes **float64,
	seeders **int64,
	peers **int64,
) {
	name, _ := attribute(start, "name")
	name = strings.ToLower(name)
	value, _ := attribute(start, "value")
	switch name {
	case "seeders":
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && parsed >= 0 {
			*seeders = &parsed
		} else {
			*seeders = nil
		}
	case "peers", "leechers":
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && parsed >= 0 {
			*peers = &parsed
		} else {
			*peers = nil
		}
	case "size":
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && parsed > 0.0 {
			*sizeBytes = &parsed
		} else {
			*sizeBytes = nil
		}
	}
}

// parseRSSDate parses the publication-date formats seen in RSS/Atom feeds.
func parseRSSDate(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

// parse_feed_body parses an RSS/Atom body with `encoding/xml` ( used
// `quick-xml`). Text and CDATA are both delivered as `xml.CharData`, so the two
// branches collapse into one.
func parse_feed_body(body, source string) ([]models.Release, error) {
	decoder := xml.NewDecoder(strings.NewReader(body))
	current := ""
	inItem := false
	title := ""
	magnet := ""
	torrentURL := ""
	description := ""
	itemSource := source
	var sizeBytes *float64
	var seeders *int64
	var peers *int64
	discoveredAt := time.Now().UTC()
	var out []models.Release
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			if len(out) > 0 {
				// Some RSS endpoints keep the connection open after sending a
				// useful prefix. Preserve the complete items already parsed
				// instead of discarding them because the XML tail is missing.
				logging.Debug("accepting partial RSS body", "error", err.Error(), "items", len(out))
				break
			}
			return nil, err
		}
		switch event := token.(type) {
		case xml.StartElement:
			name := local_name(event.Name.Local)
			if name == "item" || name == "entry" {
				inItem = true
				title = ""
				magnet = ""
				torrentURL = ""
				description = ""
				itemSource = source
				sizeBytes = nil
				seeders = nil
				peers = nil
				discoveredAt = time.Now().UTC()
			}
			if inItem {
				if name == "jackettindexer" {
					value, ok := attribute(event, "name")
					if !ok {
						value, ok = attribute(event, "value")
					}
					if ok && strings.TrimSpace(value) != "" {
						itemSource = fmt.Sprintf("%s:%s", source, strings.TrimSpace(value))
					}
				}
				if name == "enclosure" {
					if value, ok := attribute(event, "length"); ok {
						if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && parsed > 0.0 {
							sizeBytes = &parsed
						} else {
							sizeBytes = nil
						}
					}
				}
				if name == "attr" {
					if attrName, ok := attribute(event, "name"); ok && strings.EqualFold(attrName, "jackettindexer") {
						if value, ok := attribute(event, "value"); ok && strings.TrimSpace(value) != "" {
							itemSource = fmt.Sprintf("%s:%s", source, strings.TrimSpace(value))
						}
					}
					apply_torznab_attr(event, &sizeBytes, &seeders, &peers)
				}
				value, ok := attribute(event, "href")
				if !ok {
					value, ok = attribute(event, "url")
				}
				if ok {
					if found := magnet_in(value); found != "" {
						magnet = found
					} else if IsTorrentURL(value) {
						torrentURL = value
					}
				}
			}
			current = name
		case xml.CharData:
			if inItem {
				value := strings.TrimSpace(string(event))
				if current == "title" {
					title = value
				}
				if current == "jackettindexer" && value != "" {
					itemSource = fmt.Sprintf("%s:%s", source, value)
				}
				if current == "description" {
					description += value
					description += " "
				}
				if current == "size" {
					if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed > 0.0 {
						sizeBytes = &parsed
					} else {
						sizeBytes = nil
					}
				}
				if current == "seeders" {
					if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed >= 0 {
						seeders = &parsed
					} else {
						seeders = nil
					}
				}
				if current == "peers" || current == "leechers" {
					if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed >= 0 {
						peers = &parsed
					} else {
						peers = nil
					}
				}
				switch current {
				case "pubdate", "published", "updated", "date":
					if parsed, ok := parseRSSDate(value); ok {
						discoveredAt = parsed
					}
				}
				if found := magnet_in(value); found != "" {
					magnet = found
				}
				if current == "link" && IsTorrentURL(value) {
					torrentURL = value
				}
			}
		case xml.EndElement:
			name := local_name(event.Name.Local)
			if inItem && (name == "item" || name == "entry") {
				// size sanity (legacy `_sanity_check`): drop samples/NFO.
				var sizeMB *float64
				if sizeBytes != nil {
					mb := *sizeBytes / 1_048_576.0
					sizeMB = &mb
				} else {
					mb := utils.ParseSizeMB(description)
					if mb > 0.0 {
						sizeMB = &mb
					}
				}
				if sizeMB == nil || *sizeMB >= 50.0 {
					var torrentPtr *string
					if torrentURL != "" {
						value := torrentURL
						torrentPtr = &value
					}
					if release := ParseReleaseSource(title, magnet, torrentPtr, itemSource, discoveredAt); release != nil {
						if sizeBytes != nil {
							rounded := int64(math.Round(*sizeBytes))
							if rounded > 0 {
								release.SizeBytes = rounded
							} else {
								release.SizeBytes = 0
							}
						} else {
							release.SizeBytes = 0
						}
						if seeders != nil {
							release.Seeders = *seeders
						} else {
							release.Seeders = -1
						}
						if peers != nil {
							release.Peers = *peers
						} else {
							release.Peers = -1
						}
						out = append(out, *release)
					}
				}
				inItem = false
			}
			current = ""
		}
	}
	return out, nil
}

// TorznabError detects a Torznab application error embedded in an HTTP 200
// body. Torznab uses HTTP 200 even for application errors such as a wrong API
// key; without this check the parser would return zero results and the provider
// would be marked healthy instead of backing off. Returns "" when healthy.
func TorznabError(body string) string {
	decoder := xml.NewDecoder(strings.NewReader(body))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		if start, ok := token.(xml.StartElement); ok && local_name(start.Name.Local) == "error" {
			code, ok := attribute(start, "code")
			if !ok {
				code = "?"
			}
			description, ok := attribute(start, "description")
			if !ok || strings.TrimSpace(description) == "" {
				description = "errore Torznab"
			}
			return fmt.Sprintf("Torznab %s: %s", code, description)
		}
	}
}

func is_cloudflare_challenge(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "just a moment") ||
		strings.Contains(lower, "cf-challenge") ||
		strings.Contains(lower, "enable javascript and cookies") ||
		strings.Contains(lower, "attention required! | cloudflare") ||
		strings.Contains(lower, "checking your browser") ||
		strings.Contains(lower, "cf-browser-verification") ||
		strings.Contains(lower, "cf_chl_opt") ||
		strings.Contains(lower, "cdn-cgi/challenge-platform") ||
		strings.Contains(lower, "ddos protection by cloudflare")
}

// cfSession is a Cloudflare cookie/User-Agent pair learned from FlareSolverr,
// keyed by domain.
type cfSession struct {
	userAgent string
	cookie    string
}

// cloudflareCookie is one FlareSolverr solution cookie.
type cloudflareCookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

var (
	cfSessionsMu sync.Mutex
	cfSessions   = map[string]cfSession{}
)

const cfDomainTTL = 6 * time.Hour

var (
	cfDomainsMu      sync.Mutex
	cfDomainsWriteMu sync.Mutex
	cfDomains        = map[string]time.Time{}
	cfDomainsPath    string
)

// ConfigureCloudflareState selects the data directory used for the small
// persisted memory of domains that required FlareSolverr. Cookies and
// User-Agents are deliberately kept in memory only.
func ConfigureCloudflareState(dataDir string) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return
	}
	path := filepath.Join(dataDir, "gextto_cf_domains.json")
	cfDomainsMu.Lock()
	defer cfDomainsMu.Unlock()
	if path == cfDomainsPath {
		return
	}
	cfDomainsPath = path
	cfDomains = map[string]time.Time{}
	payload, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var saved map[string]int64
	if json.Unmarshal(payload, &saved) != nil {
		return
	}
	now := time.Now()
	for domain, unix := range saved {
		stamp := time.Unix(unix, 0)
		if now.Sub(stamp) < cfDomainTTL {
			cfDomains[domain] = stamp
		}
	}
}

func cfDomainNeedsFlareSolverr(rawURL string) bool {
	domain := domain_of(rawURL)
	if domain == "" {
		return false
	}
	cfDomainsMu.Lock()
	defer cfDomainsMu.Unlock()
	stamp, ok := cfDomains[domain]
	if !ok {
		return false
	}
	if time.Since(stamp) >= cfDomainTTL {
		delete(cfDomains, domain)
		return false
	}
	return true
}

func rememberCFDomain(rawURL string) {
	domain := domain_of(rawURL)
	if domain == "" {
		return
	}
	now := time.Now()
	cfDomainsMu.Lock()
	cfDomains[domain] = now
	path := cfDomainsPath
	saved := make(map[string]int64, len(cfDomains))
	for name, stamp := range cfDomains {
		if now.Sub(stamp) < cfDomainTTL {
			saved[name] = stamp.Unix()
		}
	}
	cfDomainsMu.Unlock()
	if path == "" {
		return
	}
	payload, err := json.Marshal(saved)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	cfDomainsWriteMu.Lock()
	defer cfDomainsWriteMu.Unlock()
	_ = utils.AtomicWrite(path, payload)
}

// Aggregate concurrency per host. A single feed keeps its own small limit, but
// many feeds run at once and detail scraping can fan out to dozens of requests;
// without a shared cap a host starts answering `429 Too Many Requests`. 8
// concurrent requests per host is well within what torrentgalaxy/ext.to
// tolerate in testing.
const HOST_MAX_CONCURRENCY = 8

var (
	hostLimitsMu sync.Mutex
	hostLimits   = map[string]chan struct{}{}
)

func domain_of(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func host_semaphore(rawURL string) chan struct{} {
	domain := domain_of(rawURL)
	if domain == "" {
		return nil
	}
	hostLimitsMu.Lock()
	defer hostLimitsMu.Unlock()
	semaphore, ok := hostLimits[domain]
	if !ok {
		semaphore = make(chan struct{}, HOST_MAX_CONCURRENCY)
		hostLimits[domain] = semaphore
	}
	return semaphore
}

// Minimum spacing between request starts to the same host. Concurrency alone is
// not enough: torrentgalaxy answers `429` when a burst of parallel requests
// arrives. 250 ms caps the host at 4 req/s.
const HOST_MIN_INTERVAL = 250 * time.Millisecond

var (
	hostLastRequestMu sync.Mutex
	hostLastRequest   = map[string]time.Time{}
)

// throttle_host waits until the per-host start interval has elapsed, then
// claims the slot.
func throttle_host(ctx context.Context, rawURL string) {
	domain := domain_of(rawURL)
	if domain == "" {
		return
	}
	for {
		var wait time.Duration
		hostLastRequestMu.Lock()
		now := time.Now()
		if previous, ok := hostLastRequest[domain]; ok && previous.Add(HOST_MIN_INTERVAL).After(now) {
			wait = previous.Add(HOST_MIN_INTERVAL).Sub(now)
		} else {
			hostLastRequest[domain] = now
		}
		hostLastRequestMu.Unlock()
		if wait <= 0 {
			return
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}
	}
}

// penalize_host pauses a host after a `429` so the next requests do not extend
// the penalty.
func penalize_host(rawURL string, penalty time.Duration) {
	domain := domain_of(rawURL)
	if domain == "" {
		return
	}
	hostLastRequestMu.Lock()
	hostLastRequest[domain] = time.Now().Add(penalty)
	hostLastRequestMu.Unlock()
}

func session_for(rawURL string) (cfSession, bool) {
	domain := domain_of(rawURL)
	if domain == "" {
		return cfSession{}, false
	}
	cfSessionsMu.Lock()
	defer cfSessionsMu.Unlock()
	session, ok := cfSessions[domain]
	return session, ok
}

func remember_session(rawURL, userAgent string, cookies []cloudflareCookie) {
	domain := domain_of(rawURL)
	if domain == "" {
		return
	}
	parts := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie.Name == "" {
			continue
		}
		parts = append(parts, cookie.Name+"="+cookie.Value)
	}
	cookie := strings.Join(parts, "; ")
	if userAgent == "" && cookie == "" {
		return
	}
	cfSessionsMu.Lock()
	defer cfSessionsMu.Unlock()
	entry, ok := cfSessions[domain]
	if !ok {
		entry = cfSession{}
	}
	if userAgent != "" {
		entry.userAgent = userAgent
	}
	if cookie != "" {
		entry.cookie = cookie
	}
	cfSessions[domain] = entry
}

// httpDo performs an HTTP request with an explicit client. It is the shared
// low-level primitive used by both rss.go and websearch.go; a nil client falls
// back to the shared default client.
func httpDo(ctx context.Context, client *http.Client, method, rawURL string, headers map[string]string, body []byte, contentType string) (*http.Response, error) {
	if client == nil {
		client = defaultHTTPClient
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	return client.Do(request)
}

func fetch_with_flaresolverr(ctx context.Context, client *http.Client, flaresolverr, rawURL string) (string, error) {
	if err := utils.AcquireFlareSolverrContext(ctx); err != nil {
		return "", err
	}
	defer utils.ReleaseFlareSolverr()
	endpoint := strings.TrimRight(flaresolverr, "/") + "/v1"
	payload, err := json.Marshal(map[string]any{
		"cmd":        "request.get",
		"url":        rawURL,
		"maxTimeout": 20000,
	})
	if err != nil {
		return "", err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	response, err := httpDo(requestCtx, client, http.MethodPost, endpoint, nil, payload, "application/json")
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d", response.StatusCode)
	}
	body, err := readLimitedBody(response.Body, maxAPIResponseBytes)
	if err != nil {
		return "", err
	}
	var value struct {
		Status   string `json:"status"`
		Solution struct {
			UserAgent string             `json:"userAgent"`
			Cookies   []cloudflareCookie `json:"cookies"`
			Response  string             `json:"response"`
		} `json:"solution"`
	}
	if err := json.Unmarshal(body, &value); err != nil {
		return "", err
	}
	if value.Status != "" && value.Status != "ok" {
		return "", fmt.Errorf("FlareSolverr returned status %q", value.Status)
	}
	remember_session(rawURL, value.Solution.UserAgent, value.Solution.Cookies)
	if value.Solution.Response == "" {
		return "", fmt.Errorf("flaresolverr returned an empty response")
	}
	rememberCFDomain(rawURL)
	return value.Solution.Response, nil
}

// cloudflare_blocked reports HTTP statuses that mean "Cloudflare is in front of
// the origin", where a real browser (FlareSolverr) can succeed. Random statuses
// such as 404 or 429 must NOT trigger FlareSolverr.
func cloudflare_blocked(status int) bool {
	return status == 403 || status == 503 || (status >= 520 && status <= 530)
}

func flaresolverr_or(ctx context.Context, client *http.Client, flaresolverr, rawURL, reason string, retryDirectOnFailure bool) (string, error) {
	logging.Info("trying FlareSolverr to retrieve the feed; success will continue with feed parsing",
		"feed_url", rawURL, "flaresolverr", flaresolverr, "reason", reason)
	body, err := fetch_with_flaresolverr(ctx, client, flaresolverr, rawURL)
	if err != nil {
		nextStep := "the feed will be marked as failed"
		if retryDirectOnFailure {
			nextStep = "direct HTTP attempts will continue"
		}
		logging.Warn("FlareSolverr failed; "+nextStep,
			"feed_url", rawURL, "flaresolverr", flaresolverr, "error", err.Error())
		return "", err
	}
	logging.Info("FlareSolverr solved the challenge; parsing the retrieved feed",
		"feed_url", rawURL, "response_bytes", len(body))
	return body, nil
}

// Tentativi per scaricare il corpo di un feed: Cloudflare a volte chiude lo
// stream a metà. I feed sono testi piccoli, quindi in caso di errore transitorio
// si può riscaricare.
const FEED_FETCH_ATTEMPTS = 3

// Timeout del singolo tentativo.
const FEED_FETCH_TIMEOUT = 20 * time.Second

// Knaben keeps streaming a very large RSS response instead of closing it
// promptly. Stop after one normal listing page.
const KNABEN_STREAM_ITEM_LIMIT = 50
const KNABEN_STREAM_BODY_LIMIT = 512 * 1024

// fetchAttemptKind is the outcome classification of a single direct fetch.
type fetchAttemptKind int

const (
	fetchAttemptBody fetchAttemptKind = iota
	fetchAttemptCloudflare
	fetchAttemptTransient
	fetchAttemptFatal
)

type fetchAttempt struct {
	kind  fetchAttemptKind
	value string
}

func fetch_body_direct(ctx context.Context, client *http.Client, rawURL string, timeout time.Duration) fetchAttempt {
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	headers := map[string]string{}
	if session, ok := session_for(rawURL); ok {
		if session.userAgent != "" {
			headers["User-Agent"] = session.userAgent
		}
		if session.cookie != "" {
			headers["Cookie"] = session.cookie
		}
	}
	// Cap aggregate concurrency per host; the permit is released before a retry
	// or a FlareSolverr call (which talks to another host).
	semaphore := host_semaphore(rawURL)
	if semaphore != nil {
		select {
		case semaphore <- struct{}{}:
		case <-requestCtx.Done():
			return fetchAttempt{kind: fetchAttemptFatal, value: "host request limiter closed"}
		}
	}
	throttle_host(requestCtx, rawURL)
	response, err := httpDo(requestCtx, client, http.MethodGet, rawURL, headers, nil, "")
	if semaphore != nil {
		<-semaphore
	}
	if err != nil {
		return fetchAttempt{kind: fetchAttemptTransient, value: err.Error()}
	}
	defer response.Body.Close()
	status := response.StatusCode
	if status >= 200 && status < 300 {
		var body string
		if strings.HasSuffix(domain_of(rawURL), "knaben.org") {
			body, err = read_knaben_stream(response)
		} else {
			var payload []byte
			payload, err = readLimitedBody(response.Body, maxFeedResponseBytes)
			body = string(payload)
		}
		if err != nil {
			// Stream chiuso a metà: errore transitorio, si ritenta.
			return fetchAttempt{kind: fetchAttemptTransient, value: err.Error()}
		}
		if is_cloudflare_challenge(body) {
			return fetchAttempt{kind: fetchAttemptCloudflare, value: "cloudflare challenge body"}
		}
		return fetchAttempt{kind: fetchAttemptBody, value: body}
	}
	if cloudflare_blocked(status) {
		return fetchAttempt{kind: fetchAttemptCloudflare, value: fmt.Sprintf("direct status %d", status)}
	}
	if status == 429 {
		penalize_host(rawURL, 2*time.Second)
		logging.Warn("host rate limited the request (429)", "feed_url", rawURL)
		return fetchAttempt{kind: fetchAttemptTransient, value: fmt.Sprintf("HTTP %d", status)}
	}
	if status >= 500 {
		return fetchAttempt{kind: fetchAttemptTransient, value: fmt.Sprintf("HTTP %d", status)}
	}
	return fetchAttempt{kind: fetchAttemptFatal, value: fmt.Sprintf("HTTP %d", status)}
}

// read_knaben_stream reads the useful prefix of Knaben's streaming RSS
// response: its endpoint can keep producing items without sending the closing
// `</rss>`.
func read_knaben_stream(response *http.Response) (string, error) {
	var body []byte
	buffer := make([]byte, 32*1024)
	for {
		read, err := response.Body.Read(buffer)
		if read > 0 {
			body = append(body, buffer[:read]...)
		}
		itemCount := bytes.Count(body, []byte("</item>"))
		if itemCount >= KNABEN_STREAM_ITEM_LIMIT || len(body) >= KNABEN_STREAM_BODY_LIMIT {
			break
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
	}
	if len(body) == 0 {
		return "", fmt.Errorf("empty Knaben RSS response")
	}
	return string(body), nil
}

// fetch_body downloads a feed body with a few attempts on transient errors.
// Cloudflare is the last resort, after the direct retries, so it is not
// hammered on every micro-error.
func fetch_body(ctx context.Context, client *http.Client, rawURL string, flaresolverr *string) (string, error) {
	flareTried := false
	// Once a domain has required a browser challenge, prefer the browser when
	// there is no usable in-memory session yet. A successful solution populates
	// the session cache, so subsequent listing/detail requests can go direct.
	if flaresolverr != nil && strings.TrimSpace(*flaresolverr) != "" &&
		cfDomainNeedsFlareSolverr(rawURL) {
		_, hasSession := session_for(rawURL)
		if !hasSession {
			flareTried = true
			if body, err := flaresolverr_or(ctx, client, *flaresolverr, rawURL, "domain remembered as Cloudflare-protected", true); err == nil {
				return body, nil
			}
		}
	}
	lastTransient := ""
	for attempt := 1; attempt <= FEED_FETCH_ATTEMPTS; attempt++ {
		result := fetch_body_direct(ctx, client, rawURL, FEED_FETCH_TIMEOUT)
		switch result.kind {
		case fetchAttemptBody:
			return result.value, nil
		case fetchAttemptCloudflare:
			// Non è un problema di rete transitorio: lascia fare a FlareSolverr.
			if flaresolverr == nil || strings.TrimSpace(*flaresolverr) == "" {
				return "", fmt.Errorf("%s", result.value)
			}
			if flareTried {
				return "", fmt.Errorf("%s", result.value)
			}
			flareTried = true
			return flaresolverr_or(ctx, client, *flaresolverr, rawURL, result.value, false)
		case fetchAttemptFatal:
			return "", fmt.Errorf("%s", result.value)
		case fetchAttemptTransient:
			lastTransient = result.value
			if attempt < FEED_FETCH_ATTEMPTS {
				logging.Debug(
					"feed fetch attempt failed; retrying",
					"attempt", attempt,
					"attempts", FEED_FETCH_ATTEMPTS,
					"error", lastTransient,
				)
				select {
				case <-time.After(time.Duration(attempt) * time.Second):
				case <-ctx.Done():
				}
			}
		}
	}
	// Stream ostinato: ultima spiaggia FlareSolverr, se configurato.
	if flaresolverr != nil && strings.TrimSpace(*flaresolverr) != "" && !flareTried {
		return flaresolverr_or(ctx, client, *flaresolverr, rawURL, "direct attempts exhausted", false)
	}
	return "", fmt.Errorf("%s", lastTransient)
}

// FetchFeed downloads a feed/listing and parses it. ext.to / Corsaro /
// TorrentGalaxy are HTML listings, not RSS.
func FetchFeed(ctx context.Context, rawURL string, flaresolverr *string, maxPages int, maxAgeDays int64, oldRatio float64) ([]models.Release, error) {
	lower := strings.ToLower(rawURL)
	kind := ""
	switch {
	case strings.Contains(lower, "ext.to") || strings.Contains(lower, "extto"):
		kind = "ExtTo"
	case strings.Contains(lower, "corsaro"):
		kind = "Corsaro"
	case strings.Contains(lower, "torrentgalaxy"):
		kind = "TorrentGalaxy"
	default:
		body, err := fetch_body(ctx, defaultHTTPClient, rawURL, flaresolverr)
		if err != nil {
			return nil, err
		}
		return parse_feed_body(body, rawURL)
	}
	body, err := fetch_body(ctx, defaultHTTPClient, rawURL, flaresolverr)
	if err != nil {
		return nil, err
	}
	label := source_label(kind, rawURL)
	// TorrentGalaxy uploader listings link to `/post-detail/...` pages instead
	// of carrying magnets, so they need the dedicated parser (legacy `_tgx_user`).
	if kind == "TorrentGalaxy" {
		return fetch_tgx_listing(ctx, defaultHTTPClient, body, rawURL, label, flaresolverr)
	}
	// Age filter + early-stop (legacy `max_age_days` / `stop_on_old_page_threshold`).
	var cutoff *time.Time
	if maxAgeDays > 0 {
		value := time.Now().UTC().Add(-time.Duration(maxAgeDays) * 24 * time.Hour)
		cutoff = &value
	}
	var all []models.Release
	oldTotal := 0
	pageTotal := 0
	// `feed_max_pages`: number of listing pages to walk (legacy MAX_PAGES).
	pages := maxPages
	if pages < 1 {
		pages = 1
	}
	for page := 0; page < pages; page++ {
		pageURL := rawURL
		if page != 0 {
			separator := "?"
			if strings.Contains(rawURL, "?") {
				separator = "&"
			}
			pageURL = fmt.Sprintf("%s%spage=%d", rawURL, separator, page)
		}
		var pageBody string
		if page == 0 {
			pageBody = body
		} else {
			fetched, err := fetch_body(ctx, defaultHTTPClient, pageURL, flaresolverr)
			if err != nil {
				break
			}
			pageBody = fetched
		}
		items, old, total, err := fetch_traditional_listing(ctx, defaultHTTPClient, pageURL, pageBody, kind, label, flaresolverr, cutoff)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 && total == 0 {
			break
		}
		all = append(all, items...)
		oldTotal += old
		pageTotal += total
		if cutoff != nil && pageTotal > 0 {
			ratio := float64(oldTotal) / float64(pageTotal)
			if ratio >= oldRatio {
				logging.Info(
					"listing early-stop: page is mostly older than the age limit",
					"feed_url", rawURL,
					"page", page,
					"old", oldTotal,
					"total", pageTotal,
				)
				break
			}
		}
	}
	return all, nil
}

func fetch_traditional_listing(ctx context.Context, client *http.Client, rawURL, body, kind, label string, flaresolverr *string, cutoff *time.Time) ([]models.Release, int, int, error) {
	selector := "a[href*='/torrent/'], a[href*='/torrents/'], a[href^='magnet:']"
	if kind == "ExtTo" {
		selector = "a.torrent-title-link, a[href^='magnet:']"
	}
	base, err := url.Parse(rawURL)
	if err != nil {
		return nil, 0, 0, err
	}
	document, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, 0, 0, err
	}
	nodes := htmlSelectAll(document, selector)
	// legacy walks 50 items per page; ext.to listings carry exactly 50 direct
	// magnets, so a lower cap silently drops releases.
	if len(nodes) > 50 {
		nodes = nodes[:50]
	}
	pageTotal := len(nodes)
	var output []models.Release
	var pending []rssDetailLink
	for _, node := range nodes {
		href, _ := htmlAttr(node, "href")
		title := strings.TrimSpace(htmlText(node))
		if strings.HasPrefix(href, "magnet:") {
			// ext.to download buttons have no text: recover the title from the
			// magnet `dn=` parameter, exactly like legacy's listing fallback.
			if title == "" {
				title = title_from_magnet(href)
			}
			push_release(&output, title, href, label)
			continue
		}
		if title == "" {
			continue
		}
		if len(pending) >= 12 {
			continue
		}
		if detail, err := base.Parse(href); err == nil {
			pending = append(pending, rssDetailLink{title: title, url: detail})
		}
	}
	old := fetch_detail_magnets(ctx, client, pending, label, flaresolverr, &output, cutoff)
	return output, old, pageTotal, nil
}

// rssDetailLink is a pending `(title, detail URL)` pair.
type rssDetailLink struct {
	title string
	url   *url.URL
}

// tgx_detail_links parses TorrentGalaxy `div.tgxtablerow` rows into
// `(title, detail_url)` pairs.
func tgx_detail_links(body string, base *url.URL) []rssDetailLink {
	document, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil
	}
	rows := htmlSelectAll(document, "div.tgxtablerow")
	if len(rows) > 50 {
		rows = rows[:50]
	}
	var pending []rssDetailLink
	for _, row := range rows {
		links := htmlSelectAll(row, "a.txlight[href*='post-detail'], a[href*='post-detail']")
		if len(links) == 0 {
			continue
		}
		link := links[0]
		title := strings.TrimSpace(htmlText(link))
		href, _ := htmlAttr(link, "href")
		if title == "" || href == "" {
			continue
		}
		if detail, err := base.Parse(href); err == nil {
			pending = append(pending, rssDetailLink{title: title, url: detail})
		}
	}
	return pending
}

// fetch_tgx_listing resolves TorrentGalaxy uploader pages (`/get-posts/user:NAME/`)
// to magnets. Ports legacy's `_tgx_user` parser (no age filter).
func fetch_tgx_listing(ctx context.Context, client *http.Client, body, rawURL, label string, flaresolverr *string) ([]models.Release, error) {
	base, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	pending := tgx_detail_links(body, base)
	if len(pending) == 0 {
		logging.Warn("torrentgalaxy listing returned no post-detail rows", "feed_url", rawURL)
		return []models.Release{}, nil
	}
	var output []models.Release
	fetch_detail_magnets(ctx, client, pending, label, flaresolverr, &output, nil)
	logging.Debug("torrentgalaxy listing parsed", "feed_url", rawURL, "label", label, "items", len(output))
	return output, nil
}

// fetch_detail_magnets resolves detail pages to magnets, reusing the persistent
// cache by title so already-seen releases cost no HTTP request. Returns how many
// fetched details were older than `cutoff` (age filter).
func fetch_detail_magnets(ctx context.Context, client *http.Client, pending []rssDetailLink, label string, flaresolverr *string, output *[]models.Release, cutoff *time.Time) int {
	old := 0
	var misses []rssDetailLink
	for _, item := range pending {
		if magnet, ok := cache.Get(item.title); ok && !is_placeholder_magnet(magnet) {
			// Una civetta in cache non vale: la si risolve di nuovo dal dettaglio.
			push_release(output, item.title, magnet, label)
		} else {
			misses = append(misses, item)
		}
	}
	if len(misses) == 0 {
		return old
	}
	type fetchedDetail struct {
		title string
		body  string
		ok    bool
	}
	results := make(chan fetchedDetail)
	semaphore := make(chan struct{}, 6)
	var wait sync.WaitGroup
	go func() {
		for _, item := range misses {
			item := item
			wait.Add(1)
			semaphore <- struct{}{}
			go func() {
				defer wait.Done()
				defer func() { <-semaphore }()
				body, err := fetch_body(ctx, client, item.url.String(), flaresolverr)
				if err != nil {
					results <- fetchedDetail{title: item.title}
					return
				}
				results <- fetchedDetail{title: item.title, body: body, ok: true}
			}()
		}
		wait.Wait()
		close(results)
	}()
	for result := range results {
		if !result.ok {
			continue
		}
		magnet := extract_magnet(result.body)
		if magnet == "" {
			continue
		}
		discoveredAt, ok := utils.ParseDateAny(result.body)
		if !ok {
			discoveredAt = time.Now().UTC()
		}
		if cutoff != nil && discoveredAt.Before(*cutoff) {
			old++
		} else {
			cache.Set(result.title, magnet)
			push_release_at(output, result.title, magnet, label, discoveredAt)
		}
	}
	return old
}

func push_release(output *[]models.Release, title, magnet, label string) {
	push_release_at(output, title, magnet, label, time.Now().UTC())
}

func push_release_at(output *[]models.Release, title, magnet, label string, discoveredAt time.Time) {
	// La civetta anti-bot di ext.to non è un magnet reale: non archiviarla.
	if is_placeholder_magnet(magnet) {
		return
	}
	tagged := with_source_tag(title, label)
	if release := ParseReleaseAt(tagged, magnet, label, discoveredAt); release != nil {
		*output = append(*output, *release)
	}
}

// with_source_tag is legacy `t_display`: append the source tag to the title so
// the uploader survives into the archive and the generated magnet feed.
func with_source_tag(title, label string) string {
	if label == "" || strings.Contains(title, "["+label+"]") {
		return title
	}
	return title + " [" + label + "]"
}

// title_from_magnet recovers a readable title from the magnet `dn=` parameter.
func title_from_magnet(magnet string) string {
	parsed, err := url.Parse(magnet)
	if err != nil {
		return ""
	}
	values := parsed.Query()["dn"]
	if len(values) == 0 {
		return ""
	}
	value := values[0]
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return value
}

// source_label builds the per-uploader source label, matching legacy
// (`ExtTo - user`, `TGx - user`).
func source_label(kind, rawURL string) string {
	extract := func(pattern string) (string, bool) {
		regex, err := utils.CachedRegex(pattern)
		if err != nil {
			return "", false
		}
		captures := regex.FindStringSubmatch(rawURL)
		if captures == nil {
			return "", false
		}
		return decode_component(captures[1]), true
	}
	switch kind {
	case "ExtTo":
		if user, ok := extract(`filter=u=([^&]+)`); ok {
			return "ExtTo - " + user
		}
		return "ExtTo"
	case "Corsaro":
		if user, ok := extract(`/user/([^/?]+)`); ok {
			return "Corsaro - " + user
		}
		return "Corsaro"
	case "TorrentGalaxy":
		if user, ok := extract(`/get-posts/user:([^/?]+)`); ok {
			return "TGx - " + user
		}
		return "TGx"
	default:
		return kind
	}
}

func decode_component(value string) string {
	var out strings.Builder
	for _, part := range strings.Split(value, "&") {
		key := part
		if index := strings.IndexByte(part, '='); index >= 0 {
			key = part[:index]
		}
		decoded, err := url.QueryUnescape(key)
		if err != nil {
			decoded = key
		}
		out.WriteString(decoded)
	}
	return out.String()
}

// is_placeholder_magnet reports ext.to's anti-bot magnet, which must be ignored
// because `extract_magnet` would otherwise mistake its base32-looking hash for a
// real infohash.
func is_placeholder_magnet(magnet string) bool {
	return strings.Contains(strings.ToLower(magnet), "btih:aremousemovesmostlystraightlined")
}

func extract_magnet(body string) string {
	direct := ""
	if regex, err := utils.CachedRegex(`magnet:\?xt=urn:bt(?:ih|mh):[0-9A-Za-z]{32,68}[^\s"'<>]*`); err == nil {
		direct = regex.FindString(body)
	}
	if direct == "" {
		if regex, err := utils.CachedRegex(`(?i)\b([a-f0-9]{40}|[a-z2-7]{32})\b`); err == nil {
			if captures := regex.FindStringSubmatch(body); captures != nil {
				direct = "magnet:?xt=urn:btih:" + captures[1]
			}
		}
	}
	if is_placeholder_magnet(direct) {
		return ""
	}
	return direct
}

func torznab_endpoint(indexer IndexerConfig) string {
	base := strings.TrimRight(strings.TrimSpace(indexer.URL), "/")
	if strings.Contains(base, "/api") || strings.Contains(base, "torznab") {
		return base
	}
	// legacy detects the kind from the URL/port (Prowlarr default 9696, Jackett
	// 9117), not from the configured name: a renamed indexer must still work.
	lowerURL := strings.ToLower(base)
	lowerName := strings.ToLower(indexer.Name)
	isProwlarr := strings.Contains(lowerURL, "prowlarr") ||
		strings.Contains(lowerURL, ":9696") ||
		strings.Contains(lowerName, "prowlarr")
	if isProwlarr {
		return base + "/api/v1/search"
	}
	return base + "/api/v2.0/indexers/all/results/torznab/api"
}

// jsonNumberInt converts a JSON number (decoded with UseNumber) to *int64.
func jsonNumberInt(value any) *int64 {
	switch typed := value.(type) {
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return &parsed
		}
	case float64:
		if typed == math.Trunc(typed) {
			parsed := int64(typed)
			return &parsed
		}
	}
	return nil
}

// jsonStringValue returns a JSON string value or "".
func jsonStringValue(value any) string {
	text, _ := value.(string)
	return text
}

func parseProwlarrItems(items []map[string]any, source string) []models.Release {
	var out []models.Release
	for _, item := range items {
		// Prowlarr can aggregate usenet indexers too: only torrents are usable.
		if protocol := jsonStringValue(item["protocol"]); protocol != "" && !strings.EqualFold(protocol, "torrent") {
			continue
		}
		title := jsonStringValue(item["title"])
		if title == "" {
			continue
		}
		magnet := jsonStringValue(item["magnetUrl"])
		if !strings.HasPrefix(magnet, "magnet:") {
			if download := jsonStringValue(item["downloadUrl"]); strings.HasPrefix(download, "magnet:") {
				magnet = download
			}
		}
		if !strings.HasPrefix(magnet, "magnet:") {
			// Rebuild the link from the infohash when the JSON omits the magnet
			// (several indexers only expose it that way through Prowlarr).
			if hash := strings.ToLower(strings.TrimSpace(jsonStringValue(item["infoHash"]))); (len(hash) == 40 || len(hash) == 64) && isHexString(hash) {
				magnet = "magnet:?xt=urn:btih:" + hash + "&dn=" + url.QueryEscape(title)
			}
		}
		if !strings.HasPrefix(magnet, "magnet:") {
			continue
		}
		label := source
		if indexer := jsonStringValue(item["indexer"]); indexer != "" {
			label = "prowlarr:" + indexer
		}
		release := ParseRelease(title, magnet, label)
		if release == nil {
			continue
		}
		if size := jsonNumberInt(item["size"]); size != nil && *size > 0 {
			// Drop samples/NFO fragments that are never the episode.
			if *size < 50_000_000 {
				continue
			}
			release.SizeBytes = *size
		} else {
			release.SizeBytes = 0
		}
		if seeders := jsonNumberInt(item["seeders"]); seeders != nil && *seeders >= 0 {
			release.Seeders = *seeders
		} else {
			release.Seeders = -1
		}
		peers := jsonNumberInt(item["leechers"])
		if peers == nil {
			peers = jsonNumberInt(item["peers"])
		}
		if peers != nil && *peers >= 0 {
			release.Peers = *peers
		} else {
			release.Peers = -1
		}
		out = append(out, *release)
	}
	return out
}

// resolveProwlarrMagnets fills in `magnetUrl` for items that only expose a
// Prowlarr `downloadUrl`: following it without redirects returns a `magnet:`
// Location. Several indexers (e.g. LimeTorrents) expose no magnet nor infohash
// otherwise, and would be silently dropped.
func resolveProwlarrMagnets(ctx context.Context, items []map[string]any) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resolved := 0
	for _, item := range items {
		if resolved >= 12 {
			break
		}
		if strings.HasPrefix(jsonStringValue(item["magnetUrl"]), "magnet:") {
			continue
		}
		if hash := strings.TrimSpace(jsonStringValue(item["infoHash"])); hash != "" {
			continue
		}
		download := jsonStringValue(item["downloadUrl"])
		if !strings.HasPrefix(download, "http") {
			continue
		}
		resolved++
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, download, nil)
		if err != nil {
			continue
		}
		response, err := client.Do(request)
		if err != nil {
			continue
		}
		location := response.Header.Get("Location")
		_ = response.Body.Close()
		if strings.HasPrefix(location, "magnet:") {
			item["magnetUrl"] = location
		}
	}
}

func parse_prowlarr_json(body, source string) ([]models.Release, error) {
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	var items []map[string]any
	if err := decoder.Decode(&items); err != nil {
		return nil, err
	}
	return parseProwlarrItems(items, source), nil
}

// parseProwlarrBody decodes a Prowlarr JSON reply, resolves the missing magnets
// through the proxy download URLs, then parses the items.
func parseProwlarrBody(ctx context.Context, body, source string) ([]models.Release, error) {
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	var items []map[string]any
	if err := decoder.Decode(&items); err != nil {
		return nil, err
	}
	resolveProwlarrMagnets(ctx, items)
	return parseProwlarrItems(items, source), nil
}

// isHexString reports whether value contains only hexadecimal characters.
func isHexString(value string) bool {
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return value != ""
}

// episodeFromQuery extracts a season/episode pair from a search query such as
// "Show S01E02" so Jackett can be queried with t=tvsearch.
func episodeFromQuery(query string) (int64, int64, bool) {
	match := utils.MustCachedRegex(`(?i)s(\d{1,2})[ ._-]?e(\d{1,3})`).FindStringSubmatch(query)
	if match == nil {
		return 0, 0, false
	}
	season, errSeason := strconv.ParseInt(match[1], 10, 64)
	episode, errEpisode := strconv.ParseInt(match[2], 10, 64)
	if errSeason != nil || errEpisode != nil || season <= 0 || episode <= 0 {
		return 0, 0, false
	}
	return season, episode, true
}

// torznabRequestURL builds the full Torznab/Prowlarr request URL, including the
// API key. Extracted so it can be tested without networking.
func torznabRequestURL(indexer IndexerConfig, query string, externalIDs [][2]string) string {
	endpoint := torznab_endpoint(indexer)
	isProwlarrJSON := strings.HasSuffix(endpoint, "/api/v1/search")
	var pairs []string
	appendPair := func(key, value string) {
		pairs = append(pairs, url.QueryEscape(key)+"="+url.QueryEscape(value))
	}
	if isProwlarrJSON {
		appendPair("query", query)
		appendPair("type", "search")
		// Ask for a full page; the default is much smaller.
		appendPair("limit", "100")
	} else {
		// Jackett/Caps: a real tvsearch filters by season/episode server-side
		// and returns more relevant releases than a plain free-text search.
		if season, episode, ok := episodeFromQuery(query); ok {
			appendPair("t", "tvsearch")
			appendPair("season", strconv.FormatInt(season, 10))
			appendPair("ep", strconv.FormatInt(episode, 10))
		} else {
			appendPair("t", "search")
		}
		appendPair("q", query)
		appendPair("extended", "1")
		appendPair("limit", "100")
		for _, pair := range externalIDs {
			if strings.TrimSpace(pair[1]) != "" {
				appendPair(pair[0], pair[1])
			}
		}
	}
	appendPair("apikey", indexer.APIKey)
	fullURL := endpoint
	if len(pairs) > 0 {
		separator := "?"
		if strings.Contains(endpoint, "?") {
			separator = "&"
		}
		fullURL = endpoint + separator + strings.Join(pairs, "&")
	}
	return fullURL
}

// FetchTorznab runs a Torznab search without FlareSolverr or external ids.
func FetchTorznab(ctx context.Context, indexer IndexerConfig, query string) ([]models.Release, error) {
	return FetchTorznabFlareSolverr(ctx, indexer, query, nil, nil)
}

// FetchTorznabWith runs a Torznab search with extra external-id query pairs.
func FetchTorznabWith(ctx context.Context, indexer IndexerConfig, query string, externalIDs [][2]string) ([]models.Release, error) {
	return FetchTorznabFlareSolverr(ctx, indexer, query, externalIDs, nil)
}

// FetchTorznabFlareSolverr runs a Torznab search with an optional FlareSolverr
// fallback when the indexer blocks the request (Cloudflare / 403).
func FetchTorznabFlareSolverr(ctx context.Context, indexer IndexerConfig, query string, externalIDs [][2]string, flaresolverr *string) ([]models.Release, error) {
	fullURL := torznabRequestURL(indexer, query, externalIDs)
	headers := map[string]string{}
	if session, ok := session_for(fullURL); ok {
		if session.userAgent != "" {
			headers["User-Agent"] = session.userAgent
		}
		if session.cookie != "" {
			headers["Cookie"] = session.cookie
		}
	}
	response, transportErr := httpDo(ctx, defaultHTTPClient, http.MethodGet, fullURL, headers, nil, "")
	if response != nil {
		defer response.Body.Close()
	}
	var contentType string
	var body string
	switch {
	case transportErr != nil:
		if flaresolverr == nil || strings.TrimSpace(*flaresolverr) == "" {
			return nil, transportErr
		}
		fetched, fetchErr := torznabViaFlareSolverr(ctx, indexer, *flaresolverr, fullURL, utils.RedactURLSecrets(transportErr.Error()))
		if fetchErr != nil {
			return nil, fmt.Errorf("direct Torznab request failed (%s); FlareSolverr fallback failed: %w",
				utils.RedactURLSecrets(transportErr.Error()), fetchErr)
		}
		body = fetched
		contentType = torznabContentType(body)
	case response.StatusCode >= 400:
		if !cloudflare_blocked(response.StatusCode) {
			return nil, fmt.Errorf("HTTP %d", response.StatusCode)
		}
		if flaresolverr == nil || strings.TrimSpace(*flaresolverr) == "" {
			return nil, fmt.Errorf("HTTP %d", response.StatusCode)
		}
		fetched, fetchErr := torznabViaFlareSolverr(ctx, indexer, *flaresolverr, fullURL, utils.RedactURLSecrets(fmt.Sprintf("HTTP %d", response.StatusCode)))
		if fetchErr != nil {
			return nil, fetchErr
		}
		body = fetched
		contentType = torznabContentType(body)
	default:
		contentType = response.Header.Get("Content-Type")
		payload, readErr := readLimitedBody(response.Body, maxFeedResponseBytes)
		if readErr != nil {
			return nil, readErr
		}
		body = string(payload)
		if is_cloudflare_challenge(body) {
			if flaresolverr == nil || strings.TrimSpace(*flaresolverr) == "" {
				return nil, fmt.Errorf("Cloudflare challenge")
			}
			fetched, fetchErr := torznabViaFlareSolverr(ctx, indexer, *flaresolverr, fullURL, "Cloudflare challenge body")
			if fetchErr != nil {
				return nil, fetchErr
			}
			body = fetched
			contentType = torznabContentType(body)
		}
	}
	if torznabError := TorznabError(body); torznabError != "" {
		return nil, fmt.Errorf("%s", torznabError)
	}
	// Torznab/Prowlarr replies can be large; XML/JSON decoding also invokes the
	// release parser for every item.
	if strings.Contains(contentType, "json") || strings.HasPrefix(strings.TrimSpace(body), "[") {
		return parseProwlarrBody(ctx, body, indexer.Name)
	}
	return parse_feed_body(body, indexer.Name)
}

// torznabViaFlareSolverr retries a blocked Torznab request through
// FlareSolverr, logging the (redacted) original error.
func torznabViaFlareSolverr(ctx context.Context, indexer IndexerConfig, flaresolverr, fullURL, errorText string) (string, error) {
	logging.Info("torznab request failed; retrying via FlareSolverr",
		"indexer", indexer.Name, "reason", errorText)
	return fetch_with_flaresolverr(ctx, defaultHTTPClient, flaresolverr, fullURL)
}

func torznabContentType(body string) string {
	if strings.HasPrefix(strings.TrimSpace(body), "[") {
		return "application/json"
	}
	return "application/xml"
}

// HealthProbeURL is the read-only endpoint used by the health check. For
// Jackett `t=caps` validates both the service and the API key. For Prowlarr the
// native status endpoint validates the key and the service (the Torznab `caps`
// endpoint is not reliable on aggregate search).
func HealthProbeURL(indexer IndexerConfig) string {
	endpoint := torznab_endpoint(indexer)
	if strings.HasSuffix(endpoint, "/api/v1/search") {
		base := strings.TrimSuffix(endpoint, "/api/v1/search")
		return base + "/api/v1/system/status?apikey=" + url.QueryEscape(indexer.APIKey)
	}
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}
	return endpoint + separator + "t=caps&apikey=" + url.QueryEscape(indexer.APIKey)
}

// ---------------------------------------------------------------------------
// Minimal CSS selector engine backed by golang.org/x/net/html ( used
// `scraper`). Supports tag/class/id, attribute selectors with `=`, `^=`, `$=`,
// `*=`, and descendant combinators.
// ---------------------------------------------------------------------------

type htmlAttributeSelector struct {
	name  string
	op    string
	value string
}

type htmlSimpleSelector struct {
	tag     string
	id      string
	classes []string
	attrs   []htmlAttributeSelector
}

type htmlSelector struct {
	parts []htmlSimpleSelector
}

func htmlSelectorIdentifierByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '-' || b == '_'
}

func parseHTMLSimpleSelector(text string) (htmlSimpleSelector, bool) {
	var part htmlSimpleSelector
	index := 0
	start := index
	for index < len(text) && (htmlSelectorIdentifierByte(text[index]) || text[index] == '-') {
		index++
	}
	if index > start {
		part.tag = strings.ToLower(text[start:index])
	} else if index < len(text) {
		switch text[index] {
		case '.', '#', '[', '*':
		default:
			return part, false
		}
	}
	for index < len(text) {
		switch text[index] {
		case '.':
			index++
			start = index
			for index < len(text) && htmlSelectorIdentifierByte(text[index]) {
				index++
			}
			if index == start {
				return part, false
			}
			part.classes = append(part.classes, text[start:index])
		case '#':
			index++
			start = index
			for index < len(text) && htmlSelectorIdentifierByte(text[index]) {
				index++
			}
			if index == start {
				return part, false
			}
			part.id = text[start:index]
		case '[':
			index++
			start = index
			for index < len(text) && text[index] != ']' && text[index] != '=' &&
				text[index] != '^' && text[index] != '$' && text[index] != '*' {
				index++
			}
			attr := htmlAttributeSelector{name: strings.ToLower(strings.TrimSpace(text[start:index]))}
			if index < len(text) && (text[index] == '^' || text[index] == '$' || text[index] == '*') {
				if index+1 < len(text) && text[index+1] == '=' {
					attr.op = string(text[index]) + "="
					index += 2
				} else {
					return part, false
				}
			} else if index < len(text) && text[index] == '=' {
				attr.op = "="
				index++
			}
			if index < len(text) && (text[index] == '\'' || text[index] == '"') {
				quote := text[index]
				index++
				start = index
				for index < len(text) && text[index] != quote {
					index++
				}
				attr.value = text[start:index]
				if index < len(text) {
					index++
				}
			} else {
				start = index
				for index < len(text) && text[index] != ']' {
					index++
				}
				attr.value = strings.TrimSpace(text[start:index])
			}
			if index >= len(text) || text[index] != ']' {
				return part, false
			}
			index++
			part.attrs = append(part.attrs, attr)
		case '*':
			index++
		default:
			return part, false
		}
	}
	return part, true
}

func parseHTMLSelector(expression string) []htmlSelector {
	var selectors []htmlSelector
	for _, group := range strings.Split(expression, ",") {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		fields := strings.Fields(group)
		parts := make([]htmlSimpleSelector, 0, len(fields))
		valid := true
		for _, field := range fields {
			part, ok := parseHTMLSimpleSelector(field)
			if !ok {
				valid = false
				break
			}
			parts = append(parts, part)
		}
		if valid && len(parts) > 0 {
			selectors = append(selectors, htmlSelector{parts: parts})
		}
	}
	return selectors
}

func htmlAttr(node *html.Node, name string) (string, bool) {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val, true
		}
	}
	return "", false
}

func htmlHasClass(node *html.Node, class string) bool {
	value, ok := htmlAttr(node, "class")
	if !ok {
		return false
	}
	for _, token := range strings.Fields(value) {
		if token == class {
			return true
		}
	}
	return false
}

func htmlMatchesSimple(node *html.Node, part htmlSimpleSelector) bool {
	if node.Type != html.ElementNode {
		return false
	}
	if part.tag != "" && node.Data != part.tag {
		return false
	}
	if part.id != "" {
		if value, ok := htmlAttr(node, "id"); !ok || value != part.id {
			return false
		}
	}
	for _, class := range part.classes {
		if !htmlHasClass(node, class) {
			return false
		}
	}
	for _, attr := range part.attrs {
		value, ok := htmlAttr(node, attr.name)
		if !ok {
			return false
		}
		switch attr.op {
		case "":
		case "=":
			if value != attr.value {
				return false
			}
		case "^=":
			if !strings.HasPrefix(value, attr.value) {
				return false
			}
		case "$=":
			if !strings.HasSuffix(value, attr.value) {
				return false
			}
		case "*=":
			if !strings.Contains(value, attr.value) {
				return false
			}
		}
	}
	return true
}

func htmlMatchesSelector(node *html.Node, selector htmlSelector) bool {
	return htmlMatchesFrom(node, selector.parts)
}

func htmlMatchesFrom(node *html.Node, parts []htmlSimpleSelector) bool {
	if len(parts) == 0 {
		return true
	}
	if !htmlMatchesSimple(node, parts[len(parts)-1]) {
		return false
	}
	if len(parts) == 1 {
		return true
	}
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if htmlMatchesSimple(ancestor, parts[len(parts)-2]) {
			if htmlMatchesFrom(ancestor, parts[:len(parts)-1]) {
				return true
			}
		}
	}
	return false
}

// htmlSelectAll returns all descendant elements matching the CSS expression.
func htmlSelectAll(root *html.Node, expression string) []*html.Node {
	selectors := parseHTMLSelector(expression)
	if len(selectors) == 0 {
		return nil
	}
	var result []*html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			for _, selector := range selectors {
				if htmlMatchesSelector(child, selector) {
					result = append(result, child)
					break
				}
			}
			walk(child)
		}
	}
	walk(root)
	return result
}

// htmlText returns the concatenated descendant text of a node, matching
// scraper's `ElementRef::text`.
func htmlText(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}
