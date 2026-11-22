package gextto

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/logging"
)

func TestXFullFlowOnlyLastResort(t *testing.T) {
	state := newCycleState(t)
	t.Cleanup(state.jobs.Close)
	if err := CompleteSetup(state.cfg); err != nil {
		t.Fatalf("complete setup: %v", err)
	}
	cfg := *state.cfg
	cfg.Series = []SeriesConfig{{Name: "Test Show", Quality: "720p-1080p", Language: "ita", Seasons: "1+"}}
	state.config_cache = &ConfigCache{generation: ConfigGeneration(), cfg: &cfg, valid: true}

	if _, err := state.db.db.Exec("INSERT INTO series (name, seasons, quality, language, enabled) VALUES ('Test Show','1+','720p-1080p','ita',1)"); err != nil {
		t.Fatalf("insert series: %v", err)
	}
	if _, err := state.db.db.Exec("INSERT INTO series_metadata (series_name, season, episode_count, updated_at) VALUES ('Test Show',1,1,'2020-01-01')"); err != nil {
		t.Fatalf("insert metadata: %v", err)
	}
	if _, err := state.db.db.Exec(`INSERT INTO series_feed_seen
		(title,name,season,episode,resolution,codec,audio,quality_score,magnet,source,found_at)
		VALUES ('Test Show S01E01 480p iTALiAN','Test Show',1,1,'480p','x264','ac3',10,?,'test','2020-01-01')`,
		"magnet:?xt=urn:btih:"+strings.Repeat("a", 40)); err != nil {
		t.Fatalf("insert feed row: %v", err)
	}

	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	body := url.Values{"series": {"Test Show"}, "slot": {"series"}}
	resp, err := http.PostForm(server.URL+"/series/gaps/search", body)
	if err != nil {
		t.Fatalf("post search: %v", err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	t.Logf("search fragment: %s", page)

	jobs := state.jobs.List("missing-search", "")
	if len(jobs) != 1 {
		t.Fatalf("search jobs = %d", len(jobs))
	}
	waitJobState(t, state.jobs, jobs[0].ID, JobSucceeded)

	var captured bytes.Buffer
	logging.SetOutput(&captured)
	defer logging.SetOutput(os.Stdout)

	resp, err = http.PostForm(server.URL+"/series/gaps/download-best", url.Values{"job": {jobs[0].ID}})
	if err != nil {
		t.Fatalf("post bulk: %v", err)
	}
	answer, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	t.Logf("bulk answer: %s", answer)
	bulk := state.jobs.List("missing-download", "")
	if len(bulk) != 1 {
		t.Fatalf("bulk jobs = %d", len(bulk))
	}
	waitJobState(t, state.jobs, bulk[0].ID, JobSucceeded)
	t.Logf("log: %s", captured.String())
	if !strings.Contains(captured.String(), "S01E01") {
		t.Fatalf("the bulk action did not queue the last resort: %s", captured.String())
	}
	meta, _ := state.db.TorrentMeta(strings.Repeat("a", 40))
	if meta == nil {
		t.Fatal("release not registered")
	}
	_ = context.Background
}
