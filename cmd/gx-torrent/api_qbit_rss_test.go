package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/rss"
)

// TestQbitRssAPI covers the qBittorrent RSS API mapped onto the daemon's feeds
// and rules: the tree, the folder/feed management and the rule management.
func TestQbitRssAPI(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	post := func(path string, form url.Values) int {
		t.Helper()
		resp, err := http.PostForm(srv.URL+path, form)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if code := post("/api/v2/rss/addFolder", url.Values{"path": {"Serie"}}); code != http.StatusOK {
		t.Fatalf("addFolder: %d", code)
	}
	// A feed in a folder, path "Serie\Show" (backslash separator).
	if code := post("/api/v2/rss/addFeed", url.Values{"url": {"http://feed.example/rss"}, "path": {"Serie\\Show"}}); code != http.StatusOK {
		t.Fatalf("addFeed: %d", code)
	}

	// items without withData: the feed is an object with uid/url only.
	var root map[string]json.RawMessage
	getJSONInto(t, srv.URL+"/api/v2/rss/items", &root)
	serieRaw, ok := root["Serie"]
	if !ok {
		t.Fatalf("folder Serie missing: %v", keysOf(root))
	}
	var serie map[string]json.RawMessage
	if err := json.Unmarshal(serieRaw, &serie); err != nil {
		t.Fatal(err)
	}
	feedRaw, ok := serie["Show"]
	if !ok {
		t.Fatalf("feed Show missing: %v", keysOf(serie))
	}
	var feedObj map[string]any
	if err := json.Unmarshal(feedRaw, &feedObj); err != nil {
		t.Fatal(err)
	}
	if feedObj["url"] != "http://feed.example/rss" {
		t.Fatalf("feed url = %v", feedObj["url"])
	}
	if _, ok := feedObj["uid"]; !ok {
		t.Fatal("feed uid missing")
	}
	if _, ok := feedObj["title"]; ok {
		t.Fatal("title must be absent without withData")
	}

	// items with withData: title and the empty article list appear.
	var rootData map[string]json.RawMessage
	getJSONInto(t, srv.URL+"/api/v2/rss/items?withData=1", &rootData)
	if err := json.Unmarshal(rootData["Serie"], &serie); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(serie["Show"], &feedObj); err != nil {
		t.Fatal(err)
	}
	if feedObj["title"] != "Show" || feedObj["isLoading"] != false || feedObj["hasError"] != false {
		t.Fatalf("feed withData = %+v", feedObj)
	}
	if _, ok := feedObj["articles"]; !ok {
		t.Fatal("articles missing with withData")
	}

	// setRule maps the qBittorrent rule onto a gx rule and back.
	ruleDef := `{"enabled":true,"mustContain":"1080p ita","mustNotContain":"cam","useRegex":false,` +
		`"episodeFilter":"1x01-","smartFilter":true,"affectedFeeds":["http://feed.example/rss"],` +
		`"ignoreDays":3,"addPaused":true,"assignedCategory":"tv","savePath":"/srv/tv"}`
	if code := post("/api/v2/rss/setRule", url.Values{"ruleName": {"r1"}, "ruleDef": {ruleDef}}); code != http.StatusOK {
		t.Fatalf("setRule: %d", code)
	}
	var rules map[string]map[string]any
	getJSONInto(t, srv.URL+"/api/v2/rss/rules", &rules)
	rule := rules["r1"]
	if rule == nil {
		t.Fatalf("rule r1 missing: %v", rules)
	}
	if rule["mustContain"] != "1080p ita" || rule["mustNotContain"] != "cam" || rule["useRegex"] != false {
		t.Fatalf("rule match = %+v", rule)
	}
	if rule["episodeFilter"] != "1x01-" || rule["smartFilter"] != true || rule["ignoreDays"] != float64(3) {
		t.Fatalf("rule filters = %+v", rule)
	}
	if rule["addPaused"] != true || rule["assignedCategory"] != "tv" || rule["savePath"] != "/srv/tv" {
		t.Fatalf("rule action = %+v", rule)
	}
	feeds, _ := rule["affectedFeeds"].([]any)
	if len(feeds) != 1 || feeds[0] != "http://feed.example/rss" {
		t.Fatalf("rule affectedFeeds = %+v", rule["affectedFeeds"])
	}

	// matchingArticles is empty without archive data but must be an object.
	var matching map[string][]string
	getJSONInto(t, srv.URL+"/api/v2/rss/matchingArticles?ruleName=r1", &matching)
	if matching == nil {
		t.Fatal("matchingArticles must be an object")
	}

	// renameRule / removeRule.
	if code := post("/api/v2/rss/renameRule", url.Values{"ruleName": {"r1"}, "newRuleName": {"r2"}}); code != http.StatusOK {
		t.Fatalf("renameRule: %d", code)
	}
	rules = nil
	getJSONInto(t, srv.URL+"/api/v2/rss/rules", &rules)
	if _, ok := rules["r2"]; !ok {
		t.Fatalf("rule not renamed: %v", rules)
	}
	if code := post("/api/v2/rss/removeRule", url.Values{"ruleName": {"r2"}}); code != http.StatusOK {
		t.Fatalf("removeRule: %d", code)
	}
	rules = nil
	getJSONInto(t, srv.URL+"/api/v2/rss/rules", &rules)
	if len(rules) != 0 {
		t.Fatalf("rule not removed: %v", rules)
	}

	// moveItem moves the feed to another folder and removeItem deletes it.
	if code := post("/api/v2/rss/moveItem", url.Values{"itemPath": {"Serie\\Show"}, "destPath": {"Film"}}); code != http.StatusOK {
		t.Fatalf("moveItem: %d", code)
	}
	feedsList := d.feeds()
	if len(feedsList) != 1 || feedsList[0].Folder != "Film" {
		t.Fatalf("move did not update the folder: %+v", feedsList)
	}
	if code := post("/api/v2/rss/removeItem", url.Values{"path": {"Film\\Show"}}); code != http.StatusOK {
		t.Fatalf("removeItem: %d", code)
	}
	if feedsList = d.feeds(); len(feedsList) != 0 {
		t.Fatalf("feed not removed: %+v", feedsList)
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestQbitMustContainMapsWordsToEveryTerm checks the qBittorrent rule terms
// keep their meaning: words are all required, "|" separates alternatives.
func TestQbitMustContainMapsWordsToEveryTerm(t *testing.T) {
	include, require := qbitMustContain("the office 1080p")
	if len(include) != 0 || strings.Join(require, ",") != "the,office,1080p" {
		t.Fatalf("words: include=%v require=%v", include, require)
	}
	include, require = qbitMustContain("1080p|2160p | ted lasso")
	if len(require) != 0 || strings.Join(include, ",") != "1080p,2160p,ted lasso" {
		t.Fatalf("alternatives: include=%v require=%v", include, require)
	}
	if got := qbitMustNotContain("cam|hd ts"); strings.Join(got, ",") != "cam,hd ts" {
		t.Fatalf("mustNotContain alternatives = %v", got)
	}
	rule := rss.Rule{Match: rss.Criteria{Require: []string{"the", "office"}}}
	if got := gxRuleToQbit(rule, nil)["mustContain"]; got != "the office" {
		t.Fatalf("mustContain rendered as %q", got)
	}
	rule = rss.Rule{Match: rss.Criteria{Include: []string{"1080p", "2160p"}}}
	if got := gxRuleToQbit(rule, nil)["mustContain"]; got != "1080p|2160p" {
		t.Fatalf("mustContain rendered as %q", got)
	}
}
