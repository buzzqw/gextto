package main

// ui_rss_bulk_test.go covers the pasted-list feed editor: the line parsing
// (including the listing addresses that contain "|"), the derived names, and the
// handler that adds several feeds, skipping the duplicates and the unusable
// lines and reporting what happened.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestBulkFeedNameURLLeavesAListingAddressAlone: the tag filters of an
// RSS-Bridge listing contain "|", so such a line is a URL, not "Name | URL".
func TestBulkFeedNameURLLeavesAListingAddressAlone(t *testing.T) {
	line := "https://rss.knaben.org/ita///1337x|eztv|showunsafe|hidexxx"
	name, address := bulkFeedNameURL(line)
	if name != "" || address != line {
		t.Fatalf("bulkFeedNameURL(%q) = %q, %q; want the whole line as the address", line, name, address)
	}
	name, address = bulkFeedNameURL("Torrentleech | https://rss24h.torrentleech.org/a4ae63ba69e34c5e344c")
	if name != "Torrentleech" || address != "https://rss24h.torrentleech.org/a4ae63ba69e34c5e344c" {
		t.Fatalf("bulkFeedNameURL with a name = %q, %q", name, address)
	}
}

// TestFeedNameFromURL keeps the derived names readable and distinct: the site,
// then what tells its listings apart.
func TestFeedNameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://extto.org/browse/?filter=u=ppb1":                    "extto.org ppb1",
		"https://extto.org/browse/?filter=c=Books&filter=tg=Italian": "extto.org Books Italian",
		"https://extto.org/browse/?q=MIRCrew":                        "extto.org MIRCrew",
		"https://torrentgalaxy.one/get-posts/user:MIRCrewRS/":        "torrentgalaxy.one MIRCrewRS",
		"https://rss.knaben.org/ita///1337x|eztv|showunsafe|hidexxx": "rss.knaben.org 1337x eztv showunsafe hidexxx",
		"https://example.com/feed.rss":                               "example.com",
		"https://example.com/browse/":                                "example.com",
	}
	for raw, want := range cases {
		if got := feedNameFromURL(raw); got != want {
			t.Errorf("feedNameFromURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestUIRssFeedsBulkAdd drives the whole form: one feed per line, the options
// shared by every feed, the duplicates and the unusable lines skipped and said
// on the page.
func TestUIRssFeedsBulkAdd(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	list := strings.Join([]string{
		"https://extto.org/browse/?filter=u=ppb1",
		"https://torrentgalaxy.one/get-posts/user:MIRCrewRS/",
		"Torrentleech | https://rss24h.torrentleech.org/a4ae63ba69e34c5e344c",
		"https://example.com/a/feed.rss",
		"https://example.com/b/feed.rss",
		"",
		"# a comment line",
		"questo non è un indirizzo",
		"https://extto.org/browse/?filter=u=ppb1",
	}, "\n")

	resp, err := http.PostForm(srv.URL+"/ui/rss-feed", url.Values{
		"op": {"add"}, "urls": {list}, "folder": {"listings"}, "include": {"1080p"}, "paused": {"1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	feeds := d.feeds()
	if len(feeds) != 5 {
		t.Fatalf("5 feeds expected, got %d: %+v", len(feeds), feeds)
	}
	want := []string{
		"extto.org ppb1",
		"torrentgalaxy.one MIRCrewRS",
		"Torrentleech",
		"example.com",
		"example.com 2", // two listings of the same site share the derived name
	}
	for i, name := range want {
		if feeds[i].Name != name {
			t.Errorf("feeds[%d].Name = %q, want %q", i, feeds[i].Name, name)
		}
		if feeds[i].Folder != "listings" || feeds[i].Include != "1080p" || !feeds[i].Paused {
			t.Errorf("feeds[%d] did not inherit the form options: %+v", i, feeds[i])
		}
	}
	if feeds[2].URL != "https://rss24h.torrentleech.org/a4ae63ba69e34c5e344c" {
		t.Errorf("the named line kept the wrong address: %+v", feeds[2])
	}
	page := string(body)
	if !strings.Contains(page, "feeds added") || !strings.Contains(page, "not feed addresses") {
		t.Fatalf("the page does not report the outcome: %s", page)
	}
	if !strings.Contains(page, "questo non è un indirizzo") {
		t.Fatal("the page does not list the line that could not be used")
	}

	// The same list again: everything is already configured.
	resp, err = http.PostForm(srv.URL+"/ui/rss-feed", url.Values{"op": {"add"}, "urls": {list}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(d.feeds()) != 5 {
		t.Fatalf("nothing must be added twice: %+v", d.feeds())
	}
	if !strings.Contains(string(body), "No feed added") {
		t.Fatalf("the page does not say that nothing was added: %s", body)
	}

	// With nothing to report the form goes back to the feeds tab.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err = client.PostForm(srv.URL+"/ui/rss-feed", url.Values{"op": {"add"}, "urls": {"https://extto.org/browse/?filter=u=littlemouse"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect to the feeds list", resp.StatusCode)
	}
	if got := d.feeds(); len(got) != 6 || got[5].Name != "extto.org littlemouse" {
		t.Fatalf("feeds = %+v", got)
	}
}
