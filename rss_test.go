package gextto

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/cache"
	"github.com/buzzqw/gextto/internal/models"
)

func TestTraditionalListingResolvesEveryVisibleDetailLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/listing" {
			for index := 1; index <= 13; index++ {
				_, _ = fmt.Fprintf(w, `<a href="/torrent/%d">Example.Show.S01E%02d.1080p.WEB-DL</a>`, index, index)
			}
			return
		}
		index, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/torrent/"))
		_, _ = fmt.Fprintf(w, `<a href="magnet:?xt=urn:btih:abcdefabcdefabcdefabcdefabcdefabcdefab%02x&amp;dn=Example.Show.S01E%02d.1080p.WEB-DL">download</a>`, index, index)
	}))
	defer server.Close()

	base, err := url.Parse(server.URL + "/listing")
	if err != nil {
		t.Fatal(err)
	}
	body, err := fetch_html(context.Background(), base.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	items, _, _, err := fetch_traditional_listing(context.Background(), defaultHTTPClient, base.String(), body, "Corsaro", "test", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 13 {
		t.Fatalf("resolved detail releases = %d, want 13", len(items))
	}
}

func TestFlareSolverrErrorMessageIsUserFriendly(t *testing.T) {
	if got := flaresolverr_error_message(context.DeadlineExceeded); got != "FlareSolverr did not respond in time" {
		t.Fatalf("timeout message = %q", got)
	}
	if got := flaresolverr_error_message(errors.New("context deadline exceeded")); got != "feed URL could not be retrieved via FlareSolverr" {
		t.Fatalf("generic message = %q", got)
	}
}

func TestBuildsTorznabEndpointsForJackettAndProwlarr(t *testing.T) {
	jackett := IndexerConfig{Name: "jackett", URL: "http://host:9117", APIKey: "k", Enabled: true}
	if got := torznab_endpoint(jackett); got != "http://host:9117/api/v2.0/indexers/all/results/torznab/api" {
		t.Fatalf("jackett endpoint = %q", got)
	}
	prowlarr := IndexerConfig{Name: "prowlarr", URL: "http://host:9696", APIKey: "k", Enabled: true}
	if got := torznab_endpoint(prowlarr); got != "http://host:9696/api/v1/search" {
		t.Fatalf("prowlarr endpoint = %q", got)
	}
	explicit := IndexerConfig{Name: "custom", URL: "http://host:9696/12/api", APIKey: "k", Enabled: true}
	if got := torznab_endpoint(explicit); got != "http://host:9696/12/api" {
		t.Fatalf("explicit endpoint = %q", got)
	}
	// The kind is detected from the port even when the name is arbitrary.
	renamed := IndexerConfig{Name: "my-indexer", URL: "http://host:9696", APIKey: "k", Enabled: true}
	if got := torznab_endpoint(renamed); got != "http://host:9696/api/v1/search" {
		t.Fatalf("renamed prowlarr endpoint = %q", got)
	}
	jackettRenamed := IndexerConfig{Name: "feed", URL: "http://host:9117", APIKey: "k", Enabled: true}
	if got := torznab_endpoint(jackettRenamed); got != "http://host:9117/api/v2.0/indexers/all/results/torznab/api" {
		t.Fatalf("renamed jackett endpoint = %q", got)
	}
}

func TestParsesProwlarrJSONResults(t *testing.T) {
	body := `[{"title":"Example S01E01 1080p","magnetUrl":"magnet:?xt=urn:btih:0123456789012345678901234567890123456789","indexer":"Knaben"}]`
	releases, err := parse_prowlarr_json(body, "prowlarr")
	if err != nil {
		t.Fatalf("parse_prowlarr_json: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("len = %d, want 1", len(releases))
	}
	if releases[0].Source != "prowlarr:Knaben" {
		t.Fatalf("source = %q", releases[0].Source)
	}
}

func TestDetectsTorznabApplicationErrorsInHTTP200Responses(t *testing.T) {
	body := `<rss><channel><error code="202" description="No indexers configured" /></channel></rss>`
	if got := TorznabError(body); got != "Torznab 202: No indexers configured" {
		t.Fatalf("TorznabError = %q", got)
	}
}

func TestKeepsJackettTrackerIdentityInReleaseSource(t *testing.T) {
	body := `<rss><channel><item>
            <title>Example.Show.S01E01.1080p.WEB-DL</title>
            <attr name="jackettindexer" value="Tracker One" />
            <link>magnet:?xt=urn:btih:0123456789012345678901234567890123456789</link>
        </item></channel></rss>`
	releases, err := parse_feed_body(body, "jackett")
	if err != nil {
		t.Fatalf("parse_feed_body: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("len = %d, want 1", len(releases))
	}
	if releases[0].Source != "jackett:Tracker One" {
		t.Fatalf("source = %q", releases[0].Source)
	}
}

func TestJackettHealthProbeValidatesAPIKey(t *testing.T) {
	jackett := IndexerConfig{Name: "jackett", URL: "http://host:9117", APIKey: "secret", Enabled: true}
	want := "http://host:9117/api/v2.0/indexers/all/results/torznab/api?t=caps&apikey=secret"
	if got := HealthProbeURL(jackett); got != want {
		t.Fatalf("HealthProbeURL = %q, want %q", got, want)
	}
}

func TestExtractsMagnetFromNamespacedRSSAttributesAndDescription(t *testing.T) {
	xml := `<rss><channel><item><title>Example.S01E01.1080p</title><description><![CDATA[<a href="magnet:?xt=urn:btih:0123456789012345678901234567890123456789">download</a>]]></description></item></channel></rss>`
	releases, err := parse_feed_body(xml, "test")
	if err != nil {
		t.Fatalf("parse_feed_body: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("len = %d, want 1", len(releases))
	}
	if !strings.Contains(releases[0].Magnet, "xt=urn:btih:0123456789012345678901234567890123456789") {
		t.Fatalf("magnet = %q", releases[0].Magnet)
	}
	got := magnet_in("prefix magnet:?xt=urn:btih:0123456789012345678901234567890123456789 suffix")
	if got != "magnet:?xt=urn:btih:0123456789012345678901234567890123456789" {
		t.Fatalf("magnet_in = %q", got)
	}
}

func TestParsesCDATATitlesBeforeATruncatedItem(t *testing.T) {
	xml := "<rss><channel>\n            <item><title><![CDATA[Example.S01E01.1080p.ITA]]></title><link><![CDATA[magnet:?xt=urn:btih:0123456789012345678901234567890123456789]]></link></item>\n            <item><title><![CDATA[Truncated.S01E02]]></title><description><![CDATA[unfinished\n        </channel></rss>"
	releases, err := parse_feed_body(xml, "test")
	if err != nil {
		t.Fatalf("parse_feed_body: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("len = %d, want 1", len(releases))
	}
	if releases[0].Title != "Example.S01E01.1080p.ITA" {
		t.Fatalf("title = %q", releases[0].Title)
	}
}

func TestPreservesRSSPublicationDateForReleaseAgeFilters(t *testing.T) {
	xml := `<rss><channel><item><title>Example.S01E01.1080p.ITA</title><pubDate>Tue, 01 Sep 2026 12:00:00 +0000</pubDate><link>magnet:?xt=urn:btih:0123456789012345678901234567890123456789</link></item></channel></rss>`
	releases, err := parse_feed_body(xml, "test")
	if err != nil {
		t.Fatalf("parse_feed_body: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("len = %d, want 1", len(releases))
	}
	want := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	if !releases[0].DiscoveredAt.Equal(want) {
		t.Fatalf("discovered_at = %s, want %s", releases[0].DiscoveredAt, want)
	}
}

func TestMalformedRSSIsReportedAsError(t *testing.T) {
	// Malformed XML must surface an error (the caller then falls back to the
	// listing parser or skips the feed).
	xml := "<rss><channel><item><title>Broken</title></link></head>"
	if _, err := parse_feed_body(xml, "test"); err == nil {
		t.Fatal("expected an error for malformed RSS")
	}
}

func TestFeedSizeSanityDropsTinyItems(t *testing.T) {
	xml := `<rss><channel>
          <item><title>Sample.S01E01.1080p</title><link>magnet:?xt=urn:btih:0123456789012345678901234567890123456789</link><enclosure length="1048576" type="application/x-bittorrent"/></item>
          <item><title>Real.S01E02.1080p.ITA</title><link>magnet:?xt=urn:btih:abcdefabcdefabcdefabcdefabcdefabcdefabcd</link><enclosure length="2147483648" type="application/x-bittorrent"/></item>
          <item><title>Tiny.From.Description.2024</title><link>magnet:?xt=urn:btih:1111111111111111111111111111111111111111</link><description>Size: 20 MB</description></item>
        </channel></rss>`
	releases, err := parse_feed_body(xml, "test")
	if err != nil {
		t.Fatalf("parse_feed_body: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("len = %d, want 1", len(releases))
	}
	if releases[0].Title != "Real.S01E02.1080p.ITA" {
		t.Fatalf("title = %q", releases[0].Title)
	}
}

func TestBuildsPerUploaderSourceLabelsLikeExtTo(t *testing.T) {
	if got := source_label("ExtTo", "https://extto.org/browse/?filter=u=CorsaroNero"); got != "ExtTo - CorsaroNero" {
		t.Fatalf("ExtTo label = %q", got)
	}
	if got := source_label("TorrentGalaxy", "https://torrentgalaxy.one/get-posts/user:MIRCrewRS/"); got != "TGx - MIRCrewRS" {
		t.Fatalf("TorrentGalaxy label = %q", got)
	}
	if got := source_label("ExtTo", "https://extto.org/browse/?filter=c=Movies&filter=tg=Italian"); got != "ExtTo" {
		t.Fatalf("plain ExtTo label = %q", got)
	}
}

func TestRecoversTitleFromMagnetDisplayName(t *testing.T) {
	magnet := "magnet:?xt=urn:btih:0123456789012345678901234567890123456789&dn=The%20Black%20Hole%201979%20ITA&tr=udp%3A%2F%2Ft.example"
	if got := title_from_magnet(magnet); got != "The Black Hole 1979 ITA" {
		t.Fatalf("title_from_magnet = %q", got)
	}
	if got := title_from_magnet("magnet:?xt=urn:btih:0123456789012345678901234567890123456789"); got != "" {
		t.Fatalf("title_from_magnet without dn = %q", got)
	}
}

func TestAppendsSourceTagOnce(t *testing.T) {
	if got := with_source_tag("Movie 2024", "ExtTo - X"); got != "Movie 2024 [ExtTo - X]" {
		t.Fatalf("with_source_tag = %q", got)
	}
	if got := with_source_tag("Movie 2024 [ExtTo - X]", "ExtTo - X"); got != "Movie 2024 [ExtTo - X]" {
		t.Fatalf("with_source_tag (already tagged) = %q", got)
	}
	if got := with_source_tag("Movie 2024", ""); got != "Movie 2024" {
		t.Fatalf("with_source_tag (no label) = %q", got)
	}
}

func TestParsesTorrentgalaxyRowsToDetailLinks(t *testing.T) {
	body := `
            <div class="tgxtablerow txlight">
              <div class="tgxtablecell">
                <a class="txlight" title="Clash of the Thundermans (2026)" href="/post-detail/969210/clash/"><span src="torrent"><b>Clash of the Thundermans (2026) 2160p H265 iTA EnG MIRCrew</b></span></a>
              </div>
            </div>`
	base, err := url.Parse("https://torrentgalaxy.one/get-posts/user:MIRCrewRS/")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	links := tgx_detail_links(body, base)
	if len(links) != 1 {
		t.Fatalf("len = %d, want 1", len(links))
	}
	if links[0].title != "Clash of the Thundermans (2026) 2160p H265 iTA EnG MIRCrew" {
		t.Fatalf("title = %q", links[0].title)
	}
	if links[0].url.String() != "https://torrentgalaxy.one/post-detail/969210/clash/" {
		t.Fatalf("url = %q", links[0].url.String())
	}
}

func TestOnlyCloudflareStatusesTriggerFlareSolverr(t *testing.T) {
	for _, status := range []int{403, 503, 520, 521, 524, 530} {
		if !cloudflare_blocked(status) {
			t.Fatalf("%d should be CF-like", status)
		}
	}
	for _, status := range []int{200, 301, 404, 429, 500, 502} {
		if cloudflare_blocked(status) {
			t.Fatalf("%d must not be CF-like", status)
		}
	}
}

func TestRSSRateLimitDoesNotFallbackToFlareSolverr(t *testing.T) {
	resetCloudflareMemoryForTest(t)
	var flareCalls int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer target.Close()
	flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flareCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer flare.Close()

	_, err := fetch_body(context.Background(), target.Client(), target.URL, &flare.URL)
	if err == nil || !strings.Contains(err.Error(), "HTTP 429") {
		t.Fatalf("fetch error = %v, want HTTP 429", err)
	}
	if flareCalls != 0 {
		t.Fatalf("FlareSolverr calls = %d, want 0 for a rate limit", flareCalls)
	}
}

func TestHostLimiterIsSharedPerDomain(t *testing.T) {
	a := host_semaphore("https://torrentgalaxy.one/get-posts/user:X/")
	b := host_semaphore("https://torrentgalaxy.one/post-detail/1/")
	c := host_semaphore("https://other.example/x")
	if a == nil || b == nil || c == nil {
		t.Fatal("host_semaphore returned nil")
	}
	if a != b {
		t.Fatal("same domain should share one limiter")
	}
	if a == c {
		t.Fatal("different domains must not share a limiter")
	}
}

func TestRemembersCloudflareSessionFromFlareSolverrSolution(t *testing.T) {
	cookies := []cloudflareCookie{
		{Name: "cf_clearance", Value: "abc"},
		{Name: "PHPSESSID", Value: "xyz"},
	}
	remember_session("https://ext-to-test.example/browse/", "BrowserUA/1.0", cookies)
	session, ok := session_for("https://ext-to-test.example/post-detail/1/")
	if !ok {
		t.Fatal("expected a remembered session")
	}
	if session.userAgent != "BrowserUA/1.0" {
		t.Fatalf("user agent = %q", session.userAgent)
	}
	if !strings.Contains(session.cookie, "cf_clearance=abc") || !strings.Contains(session.cookie, "PHPSESSID=xyz") {
		t.Fatalf("cookie = %q", session.cookie)
	}
	if _, ok := session_for("https://never-visited.example/"); ok {
		t.Fatal("unexpected session for never-visited host")
	}
}

func TestDetailCacheAvoidsNetworkFetch(t *testing.T) {
	detail, err := url.Parse("http://127.0.0.1:1/never")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	pending := []rssDetailLink{{title: "Cached Movie 2024", url: detail}}
	// The cache is keyed by the detail page, not by the title.
	cache.Set(detailCacheKey(pending[0]), "magnet:?xt=urn:btih:0123456789012345678901234567890123456789")
	var output []models.Release
	old := fetch_detail_magnets(context.Background(), defaultHTTPClient, pending, "ExtTo - X", nil, &output, nil)
	if old != 0 {
		t.Fatalf("old = %d, want 0", old)
	}
	if len(output) != 1 {
		t.Fatalf("len = %d, want 1", len(output))
	}
	if output[0].Title != "Cached Movie 2024 [ExtTo - X]" {
		t.Fatalf("title = %q", output[0].Title)
	}
	if output[0].Source != "ExtTo - X" {
		t.Fatalf("source = %q", output[0].Source)
	}
}

func TestListingUsesMagnetDnWhenAnchorHasNoText(t *testing.T) {
	// ext.to serves `<a href="magnet:...&dn=...">` with no inner text.
	body := `<html><body><a class="dwn-btn torrent-dwn" rel="nofollow" href="magnet:?xt=urn:btih:0123456789012345678901234567890123456789&amp;dn=The%20Black%20Hole%201979%20ITA%201080p&amp;tr=udp%3A%2F%2Ft.example"> </a></body></html>`
	releases, old, total, err := fetch_traditional_listing(
		context.Background(),
		defaultHTTPClient,
		"https://extto.org/browse/?filter=u=CorsaroNero",
		body,
		"ExtTo",
		"ExtTo - CorsaroNero",
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("fetch_traditional_listing: %v", err)
	}
	if len(releases) != 1 {
		t.Fatalf("len = %d, want 1", len(releases))
	}
	if old != 0 {
		t.Fatalf("old = %d, want 0", old)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if releases[0].Title != "The Black Hole 1979 ITA 1080p [ExtTo - CorsaroNero]" {
		t.Fatalf("title = %q", releases[0].Title)
	}
	if releases[0].Source != "ExtTo - CorsaroNero" {
		t.Fatalf("source = %q", releases[0].Source)
	}
}

func TestFetchFeedStopsWhenAPageAddsNoNewHash(t *testing.T) {
	const page = `<html><body>
<a class="torrent-title-link" href="magnet:?xt=urn:btih:0123456789012345678901234567890123456789">One</a>
<a class="torrent-title-link" href="magnet:?xt=urn:btih:1123456789012345678901234567890123456789">Two</a>
</body></html>`
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = fmt.Fprint(w, page)
	}))
	defer server.Close()

	// The site parser is chosen from host and path, never from the query.
	releases, err := FetchFeed(context.Background(), server.URL+"/ext.to/browse/", nil, 4, 0, 0)
	if err != nil {
		t.Fatalf("FetchFeed: %v", err)
	}
	if len(releases) != 2 {
		t.Fatalf("releases = %d, want 2 (a repeated page must stop the walk)", len(releases))
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2 (first page + one repeated page)", requests)
	}
}

func TestDetailCacheDoesNotShareMagnetsBetweenSameTitles(t *testing.T) {
	first, _ := url.Parse("http://127.0.0.1:1/detail/1")
	second, _ := url.Parse("http://127.0.0.1:1/detail/2")
	cache.Set(detailCacheKey(rssDetailLink{title: "Same Title 2024", url: first}),
		"magnet:?xt=urn:btih:0123456789012345678901234567890123456789")
	if _, ok := cache.Get(detailCacheKey(rssDetailLink{title: "Same Title 2024", url: second})); ok {
		t.Fatal("another release with the same title must not reuse the cached magnet")
	}
}

func TestFeedKindIgnoresQueryString(t *testing.T) {
	if got := feedKindSubject("https://example.org/search?q=corsaro&apikey=extto"); strings.Contains(got, "corsaro") || strings.Contains(got, "extto") {
		t.Fatalf("query string leaked into the parser choice: %q", got)
	}
	if got := feedKindSubject("https://extto.org/browse/?filter=x"); !strings.Contains(got, "extto") {
		t.Fatalf("host lost: %q", got)
	}
}

// ext.to numbers its pages from 1: `page=1` is the same listing as the bare
// URL, so the second page must be requested as `page=2`.
func TestFetchFeedExtToPagesStartAtOne(t *testing.T) {
	listing := func(hashes ...string) string {
		body := "<html><body>"
		for i, hash := range hashes {
			body += fmt.Sprintf(`<a class="torrent-title-link" href="magnet:?xt=urn:btih:%s">T%d</a>`, hash, i)
		}
		return body + "</body></html>"
	}
	first := listing("0123456789012345678901234567890123456789", "1123456789012345678901234567890123456789")
	second := listing("2123456789012345678901234567890123456789", "3123456789012345678901234567890123456789")
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		requested = append(requested, page)
		switch page {
		case "", "1":
			_, _ = fmt.Fprint(w, first)
		case "2":
			_, _ = fmt.Fprint(w, second)
		default:
			_, _ = fmt.Fprint(w, "<html><body></body></html>")
		}
	}))
	defer server.Close()

	releases, err := FetchFeed(context.Background(), server.URL+"/ext.to/browse/?filter=u=x", nil, 3, 0, 0)
	if err != nil {
		t.Fatalf("FetchFeed: %v", err)
	}
	if len(releases) != 4 {
		t.Fatalf("releases = %d, want 4 (pages 1 and 2); requested pages %q", len(releases), requested)
	}
	if len(requested) < 2 || requested[1] != "2" {
		t.Fatalf("requested pages = %q, want the second request to be page=2", requested)
	}
}
