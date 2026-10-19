// Command measure-ed2k runs the Fase 0 gate against a live gx-ed2k daemon:
// it samples real gaps from Gextto, queries "<series> SxxEyy" (using an alias
// when the library has one) and reports how many gaps Kad covers with an exact
// episode, a recognisable quality, a plausible size and enough sources.
//
// The sampling and the spacing are deliberate: the gap list is grouped by
// series, so a plain spread overweights one show, and peers throttle a burst
// of searches. Both mistakes made the first measurements unrepresentative.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

type gap struct {
	Series  string   `json:"series"`
	Season  int64    `json:"season"`
	Episode int64    `json:"episode"`
	AirDate string   `json:"air_date"`
	Aliases []string `json:"aliases"`
}

type daemonResult struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	Hash      string `json:"hash"`
	Link      string `json:"link"`
	Sources   int64  `json:"sources"`
	Origin    string `json:"origin"`
}

type searchResponse struct {
	Results []daemonResult `json:"results"`
}

type measurement struct {
	Series      string         `json:"series"`
	Query       string         `json:"query"`
	UsedAlias   bool           `json:"used_alias"`
	Season      int64          `json:"season"`
	Episode     int64          `json:"episode"`
	AirDate     string         `json:"air_date,omitempty"`
	Total       int            `json:"total"`
	Kad         int            `json:"kad"`
	Exact       int            `json:"exact"`
	Qualified   int            `json:"qualified"`
	WithSources int            `json:"with_sources"`
	Best        []daemonResult `json:"best,omitempty"`
	Error       string         `json:"error,omitempty"`
}

type summary struct {
	Queries     int `json:"queries"`
	KadHits     int `json:"kad_hits"`
	Exact       int `json:"exact"`
	Qualified   int `json:"qualified"`
	WithSources int `json:"with_sources"`
}

type report struct {
	GeneratedAt string        `json:"generated_at"`
	Gextto      string        `json:"gextto"`
	Daemon      string        `json:"daemon"`
	MinSources  int           `json:"min_sources"`
	Items       []measurement `json:"items"`
	Summary     summary       `json:"summary"`
}

type options struct {
	gextto     string
	daemon     string
	token      string
	limit      int
	spacing    time.Duration
	timeout    time.Duration
	minSources int
	today      string
	sample     string
	seed       int64
	out        string
	client     *http.Client
	now        func() time.Time
}

var (
	episodePattern  = regexp.MustCompile(`(?i)s(\d{1,2})[ ._-]?e(\d{1,3})`)
	episodeXPattern = regexp.MustCompile(`(?i)(?:^|[^0-9])(\d{1,2})x(\d{1,3})(?:[^0-9]|$)`)
	qualityPattern  = regexp.MustCompile(`(?i)(2160p|4k|1080p|1080i|720p|576p|480p|bluray|bdrip|brrip|webrip|web[- .]?dl|hdtv|dvdrip|xvid|divx|hevc|x265|x264|h\.?264|h\.?265)`)
)

func main() {
	opt := options{}
	flag.StringVar(&opt.gextto, "gextto", "http://127.0.0.1:8889", "Gextto base URL (GET /api/gaps)")
	flag.StringVar(&opt.daemon, "daemon", "http://127.0.0.1:8899", "gx-ed2k base URL (GET /api/v1/search)")
	flag.StringVar(&opt.token, "token", "", "gx-ed2k API token (X-Gx-Token)")
	flag.IntVar(&opt.limit, "limit", 30, "number of gaps to test (one per series)")
	flag.DurationVar(&opt.spacing, "spacing", 65*time.Second, "pause between searches (peers throttle bursts)")
	flag.DurationVar(&opt.timeout, "timeout", 15*time.Second, "per-search daemon timeout")
	flag.IntVar(&opt.minSources, "min-sources", 3, "source floor used by the automatic fallback")
	flag.StringVar(&opt.today, "today", time.Now().Format("2006-01-02"), "ignore gaps that air after this date")
	flag.StringVar(&opt.sample, "sample", "first", "gap per series: first, last or random")
	flag.Int64Var(&opt.seed, "seed", 1, "random seed when -sample random")
	flag.StringVar(&opt.out, "out", "measure-ed2k.json", "JSON report destination")
	flag.Parse()
	opt.client = &http.Client{Timeout: opt.timeout + 30*time.Second}
	opt.now = time.Now

	ctx := context.Background()
	rep, err := run(ctx, opt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "measure-ed2k:", err)
		os.Exit(1)
	}
	printReport(rep)
	if opt.out != "" {
		raw, _ := json.MarshalIndent(rep, "", "  ")
		if err := os.WriteFile(opt.out, append(raw, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "measure-ed2k: write report:", err)
			os.Exit(1)
		}
		fmt.Printf("\nreport: %s\n", opt.out)
	}
}

func run(ctx context.Context, opt options) (*report, error) {
	gaps, err := fetchGaps(ctx, opt)
	if err != nil {
		return nil, err
	}
	sample := sampleStratified(gaps, opt.limit, opt.today, opt.sample, rand.New(rand.NewSource(opt.seed)))
	if len(sample) == 0 {
		return nil, fmt.Errorf("no eligible gaps (today %s)", opt.today)
	}
	rep := &report{
		GeneratedAt: opt.now().UTC().Format(time.RFC3339),
		Gextto:      opt.gextto,
		Daemon:      opt.daemon,
		MinSources:  opt.minSources,
		Items:       make([]measurement, 0, len(sample)),
	}
	for index, item := range sample {
		if index > 0 && opt.spacing > 0 {
			select {
			case <-ctx.Done():
				return rep, ctx.Err()
			case <-time.After(opt.spacing):
			}
		}
		measurement := measureGap(ctx, opt, item)
		rep.Items = append(rep.Items, measurement)
		if measurement.Error != "" {
			fmt.Printf("%2d %-50s error: %s\n", index+1, measurement.Query, measurement.Error)
			continue
		}
		fmt.Printf("%2d %-50s total %4d kad %3d exact %3d qual %3d src%d+ %3d\n",
			index+1, measurement.Query, measurement.Total, measurement.Kad, measurement.Exact,
			measurement.Qualified, opt.minSources, measurement.WithSources)
	}
	rep.Summary = summarize(rep.Items)
	return rep, nil
}

func fetchGaps(ctx context.Context, opt options) ([]gap, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(opt.gextto, "/")+"/api/gaps", nil)
	if err != nil {
		return nil, err
	}
	response, err := opt.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /api/gaps: HTTP %d", response.StatusCode)
	}
	var payload struct {
		Items []gap `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("GET /api/gaps: %w", err)
	}
	return payload.Items, nil
}

// sampleStratified keeps one already-aired gap per series: the gap list is
// grouped by show, so a plain spread overweights a single series. The mode
// selects the first, the last or a random eligible gap of each show, which
// lets a second run test different episodes without changing the structure.
func sampleStratified(gaps []gap, limit int, today, mode string, rng *rand.Rand) []gap {
	bySeries := map[string][]gap{}
	series := make([]string, 0, len(gaps))
	for _, item := range gaps {
		if item.AirDate != "" && item.AirDate > today {
			continue
		}
		if _, ok := bySeries[item.Series]; !ok {
			series = append(series, item.Series)
		}
		bySeries[item.Series] = append(bySeries[item.Series], item)
	}
	sort.Strings(series)
	out := make([]gap, 0, len(series))
	for _, name := range series {
		list := bySeries[name]
		if len(list) == 0 {
			continue
		}
		index := 0
		switch mode {
		case "last":
			index = len(list) - 1
		case "random":
			index = rng.Intn(len(list))
		}
		out = append(out, list[index])
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func measureGap(ctx context.Context, opt options, item gap) measurement {
	title := item.Series
	usedAlias := false
	if len(item.Aliases) > 0 && strings.TrimSpace(item.Aliases[0]) != "" {
		title = strings.TrimSpace(item.Aliases[0])
		usedAlias = true
	}
	query := fmt.Sprintf("%s S%02dE%02d", title, item.Season, item.Episode)
	result := measurement{Series: item.Series, Query: query, UsedAlias: usedAlias, Season: item.Season, Episode: item.Episode, AirDate: item.AirDate}
	endpoint := strings.TrimRight(opt.daemon, "/") + "/api/v1/search?" + url.Values{
		"q":          {query},
		"timeout_ms": {fmt.Sprint(opt.timeout.Milliseconds())},
	}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if opt.token != "" {
		request.Header.Set("X-Gx-Token", opt.token)
	}
	response, err := opt.client.Do(request)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		result.Error = fmt.Sprintf("HTTP %d", response.StatusCode)
		return result
	}
	var payload searchResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(&payload); err != nil {
		result.Error = err.Error()
		return result
	}
	result.Total = len(payload.Results)
	for _, file := range payload.Results {
		if !strings.Contains(strings.ToLower(file.Origin), "kad") {
			continue
		}
		result.Kad++
		if !nameMatches(file.Name, item.Series, title, item.Season, item.Episode) {
			continue
		}
		result.Exact++
		if !qualityPattern.MatchString(file.Name) || file.SizeBytes < 20_000_000 {
			continue
		}
		result.Qualified++
		if file.Sources >= int64(opt.minSources) {
			result.WithSources++
		}
		if len(result.Best) < 3 {
			result.Best = append(result.Best, file)
		}
	}
	return result
}

func nameMatches(name, series, alias string, season, episode int64) bool {
	normalized := normalizeName(name)
	if !strings.Contains(normalized, normalizeName(alias)) && !strings.Contains(normalized, normalizeName(series)) {
		return false
	}
	for _, match := range episodePattern.FindAllStringSubmatch(name, -1) {
		if atoi(match[1]) == season && atoi(match[2]) == episode {
			return true
		}
	}
	for _, match := range episodeXPattern.FindAllStringSubmatch(name, -1) {
		if atoi(match[1]) == season && atoi(match[2]) == episode {
			return true
		}
	}
	return false
}

func normalizeName(value string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127 {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func atoi(value string) int64 {
	var n int64
	for _, r := range value {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int64(r-'0')
	}
	return n
}

func summarize(items []measurement) summary {
	var sum summary
	for _, item := range items {
		if item.Error != "" {
			continue
		}
		sum.Queries++
		if item.Kad > 0 {
			sum.KadHits++
		}
		if item.Exact > 0 {
			sum.Exact++
		}
		if item.Qualified > 0 {
			sum.Qualified++
		}
		if item.WithSources > 0 {
			sum.WithSources++
		}
	}
	return sum
}

func printReport(rep *report) {
	fmt.Printf("\n%d queries · %d with Kad results · %d exact · %d qualified · %d with >=%d sources\n",
		rep.Summary.Queries, rep.Summary.KadHits, rep.Summary.Exact, rep.Summary.Qualified, rep.Summary.WithSources, rep.MinSources)
}
