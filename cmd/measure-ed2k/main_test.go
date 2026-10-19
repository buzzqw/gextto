package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSampleStratified(t *testing.T) {
	gaps := []gap{
		{Series: "Show A", Season: 1, Episode: 1},
		{Series: "Show A", Season: 1, Episode: 2},
		{Series: "Show B", Season: 2, Episode: 1},
		{Series: "Future", Season: 1, Episode: 1, AirDate: "2027-01-01"},
	}
	got := sampleStratified(gaps, 0, "2026-10-19")
	if len(got) != 2 || got[0].Series != "Show A" || got[0].Episode != 1 || got[1].Series != "Show B" {
		t.Fatalf("sample = %+v", got)
	}
	if limited := sampleStratified(gaps, 1, "2026-10-19"); len(limited) != 1 {
		t.Fatalf("limit = %+v", limited)
	}
}

func TestNameMatches(t *testing.T) {
	cases := []struct {
		name    string
		series  string
		alias   string
		season  int64
		episode int64
		want    bool
	}{
		{"Murdoch.Mysteries.S13E12.1080p.mkv", "I misteri di Murdoch", "Murdoch Mysteries", 13, 12, true},
		{"I.misteri.di.Murdoch.13x12.mkv", "I misteri di Murdoch", "Murdoch Mysteries", 13, 12, true},
		{"Murdoch.Mysteries.S13E13.mkv", "I misteri di Murdoch", "Murdoch Mysteries", 13, 12, false},
		{"Another.Show.S13E12.mkv", "I misteri di Murdoch", "Murdoch Mysteries", 13, 12, false},
	}
	for _, tc := range cases {
		if got := nameMatches(tc.name, tc.series, tc.alias, tc.season, tc.episode); got != tc.want {
			t.Errorf("nameMatches(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRunAgainstFakeServers(t *testing.T) {
	gextto := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/gaps" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"items": []gap{
				{Series: "Show A", Season: 1, Episode: 2, Aliases: []string{"Alias Show"}},
				{Series: "Show A", Season: 1, Episode: 3},
				{Series: "Show B", Season: 2, Episode: 1},
				{Series: "Future Show", Season: 1, Episode: 1, AirDate: "2027-01-01"},
			},
		})
	}))
	defer gextto.Close()
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query().Get("q")
		results := []daemonResult{}
		if strings.HasPrefix(q, "Alias Show") {
			results = append(results, daemonResult{
				Name: "Alias.Show.S01E02.1080p.mkv", SizeBytes: 2_000_000_000,
				Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Sources: 4, Origin: "kad",
			})
		}
		_ = json.NewEncoder(w).Encode(searchResponse{Results: results})
	}))
	defer daemon.Close()

	opt := options{
		gextto: gextto.URL, daemon: daemon.URL, limit: 2, spacing: 0,
		timeout: time.Second, minSources: 3, today: "2026-10-19",
		client: daemon.Client(), now: func() time.Time { return time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC) },
	}
	rep, err := run(context.Background(), opt)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Summary.Queries != 2 || rep.Summary.KadHits != 1 || rep.Summary.Exact != 1 ||
		rep.Summary.Qualified != 1 || rep.Summary.WithSources != 1 {
		t.Fatalf("summary = %+v", rep.Summary)
	}
	if !rep.Items[0].UsedAlias || rep.Items[0].Query != "Alias Show S01E02" {
		t.Fatalf("first query = %+v", rep.Items[0])
	}
	if rep.Items[1].WithSources != 0 {
		t.Fatalf("second query = %+v", rep.Items[1])
	}
}
