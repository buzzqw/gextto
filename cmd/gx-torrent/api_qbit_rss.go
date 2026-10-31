package main

// api_qbit_rss.go implements the qBittorrent RSS API (/api/v2/rss/*) on top of
// the daemon's own feeds and rules. The tree is the qBittorrent one: a folder
// is a JSON object of its children, a feed is an object with uid/url (and, with
// withData, title/lastBuildDate/isLoading/hasError/articles). Feeds are the ones
// in settings.json; folders are the feeds' "folder" field plus an explicit list
// (rss-folders) so an empty folder created via the API survives.
//
// Rule terms: qBittorrent's "mustContain" requires every word, and "|"
// separates alternatives. setRule maps the words to the rule's Require list
// (every term) and single-word alternatives to Include (one of), so a rule
// set through this API takes what it takes in qBittorrent; the reverse
// mapping renders Require as words and Include as "a|b". The rule action is
// read and written both in the legacy flat fields and in the 4.6+
// "torrentParams" object, which is where the tags travel. The feed uid comes
// from the feed's address, so a rename or a move keeps it; "lastMatch" and
// "previouslyMatchedEpisodes" are filled from the daemon's own record instead
// of being left empty. Registered only in standalone.

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
)

const (
	qbitRssPathSep         = `\`
	rssFoldersSettingKey   = "rss-folders"
	rssFeedUIDsSettingKey  = "rss-feed-uids"
	qbitRssArticlesPerFeed = 20
)

// rssFolders returns the explicit folder names (settings "rss-folders").
func (d *Daemon) rssFolders() []string {
	if d.opts.Settings == nil {
		return nil
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(rssFoldersSettingKey, ""))
	if raw == "" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil
	}
	return list
}

func (d *Daemon) saveRssFolders(list []string) {
	if d.opts.Settings == nil {
		return
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return
	}
	d.opts.Settings.Set(rssFoldersSettingKey, string(raw))
	_ = d.opts.Settings.Save()
}

// qbitRssJoin builds the qBittorrent path of a feed ("folder\name").
func qbitRssJoin(folder, name string) string {
	if folder == "" {
		return name
	}
	return folder + qbitRssPathSep + name
}

// qbitRssSplit splits a qBittorrent path into folder and name.
func qbitRssSplit(path string) (folder, name string) {
	if idx := strings.LastIndex(path, qbitRssPathSep); idx >= 0 {
		return path[:idx], path[idx+1:]
	}
	return "", path
}

// qbitRssUID derives a stable UUID (v5-like) from the feed's identity. The
// identity is the address, not the path in the tree, so renaming or moving a
// feed keeps its uid the way qBittorrent does.
func qbitRssUID(identity string) string {
	sum := sha1.Sum([]byte(identity))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// feedIdentity is what a feed's uid is derived from: its address, not its name
// in the tree, so a rename or a move does not change it. A feed that searches
// the indexers without a URL is identified by its query.
func feedIdentity(feed feedConfig) string {
	if url := strings.TrimSpace(feed.URL); url != "" {
		return "url:" + url
	}
	if query := strings.TrimSpace(feed.Search); query != "" {
		return "search:" + query
	}
	return "name:" + qbitRssJoin(feed.Folder, feed.Name)
}

// rssFeedUIDs returns the uid overrides saved when a feed's address changes.
func (d *Daemon) rssFeedUIDs() map[string]string {
	out := map[string]string{}
	if d.opts.Settings == nil {
		return out
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(rssFeedUIDsSettingKey, ""))
	if raw == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]string{}
	}
	return out
}

func (d *Daemon) saveRssFeedUIDs(uids map[string]string) error {
	if d.opts.Settings == nil {
		return nil
	}
	raw, err := json.Marshal(uids)
	if err != nil {
		return err
	}
	d.opts.Settings.Set(rssFeedUIDsSettingKey, string(raw))
	return d.opts.Settings.Save()
}

// feedUIDFor returns the uid recorded for this identity, or the derived one.
func (d *Daemon) feedUIDFor(identity string) string {
	if uid := d.rssFeedUIDs()[identity]; uid != "" {
		return uid
	}
	return qbitRssUID(identity)
}

// feedUID is the uid of a configured feed.
func (d *Daemon) feedUID(feed feedConfig) string {
	return d.feedUIDFor(feedIdentity(feed))
}

// carryFeedUID keeps the uid when a feed's address changes (setFeedURL and the
// page edit), so a client that knows the feed by uid does not lose it.
func (d *Daemon) carryFeedUID(oldIdentity, newIdentity string) {
	if oldIdentity == "" || newIdentity == "" || oldIdentity == newIdentity {
		return
	}
	uids := d.rssFeedUIDs()
	uids[newIdentity] = d.feedUIDFor(oldIdentity)
	if err := d.saveRssFeedUIDs(uids); err != nil {
		logfError("cannot save the feed uid: %v", err)
	}
}

// forgetFeedUIDs drops the recorded uid of feeds that were removed.
func (d *Daemon) forgetFeedUIDs(identities []string) {
	if len(identities) == 0 {
		return
	}
	uids := d.rssFeedUIDs()
	changed := false
	for _, identity := range identities {
		if _, ok := uids[identity]; ok {
			delete(uids, identity)
			changed = true
		}
	}
	if !changed {
		return
	}
	if err := d.saveRssFeedUIDs(uids); err != nil {
		logfError("cannot save the feed uid: %v", err)
	}
}

func (d *Daemon) handleQbitRssItems(w http.ResponseWriter, r *http.Request) {
	withData := formBool(r, "withData")
	statuses := map[string]feedStatus{}
	for _, s := range d.feedStatusesSnapshot() {
		statuses[s.Name] = s
	}
	root := map[string]any{}
	folderNode := func(name string) map[string]any {
		if existing, ok := root[name].(map[string]any); ok {
			return existing
		}
		node := map[string]any{}
		root[name] = node
		return node
	}
	for _, folder := range d.rssFolders() {
		if folder != "" {
			folderNode(folder)
		}
	}
	for _, feed := range d.feeds() {
		node := d.qbitRssFeedNode(feed, withData, statuses[feed.Name])
		if feed.Folder == "" {
			root[feed.Name] = node
			continue
		}
		folderNode(feed.Folder)[feed.Name] = node
	}
	writeJSON(w, http.StatusOK, root)
}

// qbitRssFeedNode serializes one feed the way qBittorrent does: uid/url always,
// the rest only with withData.
func (d *Daemon) qbitRssFeedNode(feed feedConfig, withData bool, status feedStatus) map[string]any {
	node := map[string]any{
		"uid": d.feedUID(feed),
		"url": feed.URL,
	}
	if withData {
		node["title"] = feed.Name
		node["isLoading"] = false
		node["hasError"] = status.LastError != ""
		if status.LastCheck > 0 {
			node["lastBuildDate"] = time.Unix(status.LastCheck, 0).UTC().Format(time.RFC1123Z)
		} else {
			node["lastBuildDate"] = ""
		}
		if status.LastError != "" {
			node["error"] = status.LastError
		}
		node["articles"] = d.qbitRssArticles(feed.Name)
	}
	return node
}

// qbitRssArticles maps the feed's archived items to qBittorrent articles.
func (d *Daemon) qbitRssArticles(feed string) []map[string]any {
	out := []map[string]any{}
	archive := d.archive()
	if archive == nil || feed == "" {
		return out
	}
	page, err := archive.search(archiveQuery{feed: feed}, "new", 1, qbitRssArticlesPerFeed)
	if err != nil {
		return out
	}
	for _, entry := range page.Items {
		article := map[string]any{
			"id":          entry.ID,
			"title":       entry.Title,
			"torrentURL":  entry.Download,
			"link":        entry.Download,
			"description": "",
			"isRead":      false,
		}
		switch {
		case entry.Published > 0:
			article["date"] = time.Unix(entry.Published, 0).UTC().Format(time.RFC1123Z)
		case entry.FirstSeen > 0:
			article["date"] = time.Unix(entry.FirstSeen, 0).UTC().Format(time.RFC1123Z)
		}
		out = append(out, article)
	}
	return out
}

func (d *Daemon) handleQbitRssAddFolder(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.FormValue("path"))
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	d.addRssFolder(path)
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) addRssFolder(path string) {
	if path == "" {
		return
	}
	for _, folder := range d.rssFolders() {
		if folder == path {
			return
		}
	}
	d.saveRssFolders(append(d.rssFolders(), path))
}

func (d *Daemon) handleQbitRssAddFeed(w http.ResponseWriter, r *http.Request) {
	url := strings.TrimSpace(r.FormValue("url"))
	path := strings.TrimSpace(r.FormValue("path"))
	if url == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}
	if path == "" {
		path = url
	}
	folder, name := qbitRssSplit(path)
	feeds := d.feeds()
	for _, feed := range feeds {
		if feed.URL == url {
			w.WriteHeader(http.StatusOK) // already present
			return
		}
	}
	if err := d.saveFeeds(append(feeds, feedConfig{Name: name, URL: url, Folder: folder})); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if folder != "" {
		d.addRssFolder(folder)
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitRssSetFeedURL changes the URL of one feed (path -> url).
func (d *Daemon) handleQbitRssSetFeedURL(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.FormValue("path"))
	url := strings.TrimSpace(r.FormValue("url"))
	if path == "" || url == "" {
		http.Error(w, "path and url are required", http.StatusBadRequest)
		return
	}
	feeds := d.feeds()
	changed := false
	for i := range feeds {
		if qbitRssJoin(feeds[i].Folder, feeds[i].Name) == path {
			old := feedIdentity(feeds[i])
			feeds[i].URL = url
			d.carryFeedUID(old, feedIdentity(feeds[i]))
			changed = true
		}
	}
	if changed {
		if err := d.saveFeeds(feeds); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitRssSetFeedInterval accepts the per-feed refresh interval: the daemon
// uses one global interval (feed-interval-secs), so the value is accepted and
// ignored.
func (d *Daemon) handleQbitRssSetFeedInterval(w http.ResponseWriter, r *http.Request) {
	logf("qbit rss/setFeedRefreshInterval: the daemon uses the global feed-interval-secs")
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitRssRemoveItem(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.FormValue("path"))
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	kept := make([]feedConfig, 0, len(d.feeds()))
	var removed []string
	for _, feed := range d.feeds() {
		if qbitRssCovered(qbitRssJoin(feed.Folder, feed.Name), feed.Folder, path) {
			removed = append(removed, feedIdentity(feed))
			continue
		}
		kept = append(kept, feed)
	}
	if err := d.saveFeeds(kept); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.forgetFeedUIDs(removed)
	d.saveRssFolders(qbitRssDropFolders(d.rssFolders(), path))
	w.WriteHeader(http.StatusOK)
}

// qbitRssCovered reports whether a feed (by full path and folder) is the item
// at "path" or lives under the folder "path".
func qbitRssCovered(full, folder, path string) bool {
	if full == path {
		return true
	}
	return folder != "" && (folder == path || strings.HasPrefix(folder, path+qbitRssPathSep))
}

func qbitRssDropFolders(folders []string, path string) []string {
	out := make([]string, 0, len(folders))
	for _, folder := range folders {
		if folder == path || strings.HasPrefix(folder, path+qbitRssPathSep) {
			continue
		}
		out = append(out, folder)
	}
	return out
}

func (d *Daemon) handleQbitRssMoveItem(w http.ResponseWriter, r *http.Request) {
	itemPath := strings.TrimSpace(r.FormValue("itemPath"))
	destPath := strings.TrimSpace(r.FormValue("destPath"))
	if itemPath == "" {
		http.Error(w, "itemPath is required", http.StatusBadRequest)
		return
	}
	feeds := d.feeds()
	for i := range feeds {
		full := qbitRssJoin(feeds[i].Folder, feeds[i].Name)
		switch {
		case full == itemPath:
			feeds[i].Folder = destPath
		case feeds[i].Folder != "" && strings.HasPrefix(feeds[i].Folder, itemPath+qbitRssPathSep):
			rest := strings.TrimPrefix(feeds[i].Folder, itemPath+qbitRssPathSep)
			feeds[i].Folder = qbitRssJoin(destPath, rest)
		}
	}
	if err := d.saveFeeds(feeds); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	folders := d.rssFolders()
	moved := make([]string, 0, len(folders))
	for _, folder := range folders {
		switch {
		case folder == itemPath:
			if destPath != "" {
				moved = append(moved, destPath)
			}
		case strings.HasPrefix(folder, itemPath+qbitRssPathSep):
			moved = append(moved, qbitRssJoin(destPath, strings.TrimPrefix(folder, itemPath+qbitRssPathSep)))
		default:
			moved = append(moved, folder)
		}
	}
	d.saveRssFolders(moved)
	w.WriteHeader(http.StatusOK)
}

// handleQbitRssRefreshItem reloads the feed (or the feeds of a folder) now.
func (d *Daemon) handleQbitRssRefreshItem(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.FormValue("itemPath"))
	if path == "" {
		path = strings.TrimSpace(r.URL.Query().Get("itemPath"))
	}
	for _, feed := range d.feeds() {
		if qbitRssCovered(qbitRssJoin(feed.Folder, feed.Name), feed.Folder, path) {
			name := feed.Name
			go d.pollFeedsOnlyDetailed(name)
		}
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitRssMarkAsRead is accepted: the daemon has no per-article read flag
// in the API (the archive records what was added).
func (d *Daemon) handleQbitRssMarkAsRead(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitRssRules(w http.ResponseWriter, _ *http.Request) {
	urlByFeed := map[string]string{}
	for _, feed := range d.feeds() {
		urlByFeed[feed.Name] = feed.URL
	}
	out := map[string]any{}
	for _, rule := range d.rules() {
		out[rule.Name] = gxRuleToQbit(rule, urlByFeed, d.ruleLastMatch(rule.Name), d.ruleEpisodes(rule.Name))
	}
	writeJSON(w, http.StatusOK, out)
}

// gxRuleToQbit renders a gx rule in the qBittorrent rule JSON shape: the match
// terms, the legacy flat action fields, the rule's last match, the episodes a
// smart filter already took and the torrentParams object the 4.6+ clients read.
func gxRuleToQbit(rule rss.Rule, urlByFeed map[string]string, lastMatch time.Time, episodes []string) map[string]any {
	enabled := true
	if rule.Enabled != nil {
		enabled = *rule.Enabled
	}
	mustContain := strings.Join(rule.Match.Require, " ")
	if mustContain == "" {
		mustContain = strings.Join(rule.Match.Include, "|")
	}
	mustNotContain := strings.Join(rule.Match.Exclude, "|")
	useRegex := false
	if rule.Match.Regex != "" {
		mustContain = rule.Match.Regex
		useRegex = true
	}
	if rule.Match.NotRegex != "" {
		mustNotContain = rule.Match.NotRegex
		useRegex = true
	}
	affected := []string{}
	if len(rule.Feeds) == 0 {
		for _, url := range urlByFeed {
			affected = append(affected, url)
		}
	} else {
		for _, name := range rule.Feeds {
			if url, ok := urlByFeed[name]; ok {
				affected = append(affected, url)
			}
		}
	}
	sort.Strings(affected)
	if episodes == nil {
		episodes = []string{}
	}
	matched := ""
	if !lastMatch.IsZero() {
		// qBittorrent serializes the time as an RFC 2822 date.
		matched = lastMatch.Local().Format(time.RFC1123Z)
	}
	return map[string]any{
		"enabled":                   enabled,
		"mustContain":               mustContain,
		"mustNotContain":            mustNotContain,
		"useRegex":                  useRegex,
		"episodeFilter":             rule.Match.EpisodeFilter,
		"smartFilter":               rule.Match.SmartEpisode,
		"previouslyMatchedEpisodes": episodes,
		"affectedFeeds":             affected,
		"ignoreDays":                rule.IgnoreDays,
		"lastMatch":                 matched,
		"addPaused":                 rule.Action.Paused,
		"assignedCategory":          rule.Action.Category,
		"savePath":                  rule.Action.SavePath,
		"torrentParams":             qbitTorrentParamsFor(rule.Action),
	}
}

// qbitTorrentParamsFor renders the rule's action as the qBittorrent
// torrentParams object (the 4.6+ form), tags included: they survive a
// round-trip from a client, where the legacy flat fields had no place for them.
func qbitTorrentParamsFor(action rss.Action) map[string]any {
	tags := action.Tags
	if tags == nil {
		tags = []string{}
	}
	params := map[string]any{
		"save_path": action.SavePath,
		"category":  action.Category,
		"tags":      tags,
		"stopped":   action.Paused,
	}
	if action.Top {
		params["add_to_top_of_queue"] = true
	}
	return params
}

func (d *Daemon) handleQbitRssMatchingArticles(w http.ResponseWriter, r *http.Request) {
	ruleName := strings.TrimSpace(r.URL.Query().Get("ruleName"))
	if ruleName == "" {
		ruleName = strings.TrimSpace(r.FormValue("ruleName"))
	}
	out := map[string][]string{}
	var rule *rss.Rule
	for _, candidate := range d.rules() {
		if candidate.Name == ruleName {
			c := candidate
			rule = &c
			break
		}
	}
	archive := d.archive()
	if rule == nil || archive == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	page, err := archive.search(archiveQuery{}, "new", 1, rulePreviewScan)
	if err != nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	now := time.Now()
	for _, entry := range page.Items {
		item := rss.Item{Title: entry.Title, Size: entry.Size, Seeders: entry.Seeders, Leechers: entry.Leechers, Magnet: entry.Download}
		if entry.Published > 0 {
			item.Published = time.Unix(entry.Published, 0)
		} else {
			item.Published = time.Unix(entry.FirstSeen, 0)
		}
		if _, _, matched := rss.Evaluate([]rss.Rule{*rule}, entry.Feed, item, now); matched {
			out[entry.Feed] = append(out[entry.Feed], entry.Title)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// qbitRuleDef is the qBittorrent auto-downloading rule definition.
type qbitRuleDef struct {
	Enabled          *bool                  `json:"enabled"`
	MustContain      string                 `json:"mustContain"`
	MustNotContain   string                 `json:"mustNotContain"`
	UseRegex         bool                   `json:"useRegex"`
	EpisodeFilter    string                 `json:"episodeFilter"`
	SmartFilter      bool                   `json:"smartFilter"`
	AffectedFeeds    []string               `json:"affectedFeeds"`
	IgnoreDays       int                    `json:"ignoreDays"`
	AddPaused        bool                   `json:"addPaused"`
	AssignedCategory string                 `json:"assignedCategory"`
	SavePath         string                 `json:"savePath"`
	TorrentParams    *qbitRuleTorrentParams `json:"torrentParams"`
}

// qbitRuleTorrentParams is the 4.6+ action object of a rule. It is the form
// that carries the tags, so a rule set by a client keeps them.
type qbitRuleTorrentParams struct {
	SavePath string   `json:"save_path"`
	Category string   `json:"category"`
	Tags     []string `json:"tags"`
	Stopped  *bool    `json:"stopped"`
	QueueTop *bool    `json:"add_to_top_of_queue"`
}

func (d *Daemon) handleQbitRssSetRule(w http.ResponseWriter, r *http.Request) {
	ruleName := strings.TrimSpace(r.FormValue("ruleName"))
	if ruleName == "" {
		http.Error(w, "ruleName is required", http.StatusBadRequest)
		return
	}
	var def qbitRuleDef
	if err := json.Unmarshal([]byte(r.FormValue("ruleDef")), &def); err != nil {
		http.Error(w, "invalid ruleDef", http.StatusBadRequest)
		return
	}
	nameByFeedURL := map[string]string{}
	for _, feed := range d.feeds() {
		nameByFeedURL[feed.URL] = feed.Name
	}
	rule := rss.Rule{Name: ruleName, IgnoreDays: def.IgnoreDays}
	if def.Enabled != nil && !*def.Enabled {
		disabled := false
		rule.Enabled = &disabled
	}
	rule.Match.EpisodeFilter = strings.TrimSpace(def.EpisodeFilter)
	rule.Match.SmartEpisode = def.SmartFilter
	if def.UseRegex {
		rule.Match.Regex = strings.TrimSpace(def.MustContain)
		rule.Match.NotRegex = strings.TrimSpace(def.MustNotContain)
	} else {
		rule.Match.Include, rule.Match.Require = qbitMustContain(def.MustContain)
		rule.Match.Exclude = qbitMustNotContain(def.MustNotContain)
	}
	for _, url := range def.AffectedFeeds {
		if name, ok := nameByFeedURL[url]; ok {
			rule.Feeds = append(rule.Feeds, name)
		}
	}
	rule.Action = rss.Action{SavePath: strings.TrimSpace(def.SavePath), Category: strings.TrimSpace(def.AssignedCategory), Paused: def.AddPaused}
	// The 4.6+ clients send the action inside torrentParams (tags included);
	// when it is there it wins over the deprecated flat fields.
	if params := def.TorrentParams; params != nil {
		rule.Action.SavePath = strings.TrimSpace(params.SavePath)
		rule.Action.Category = strings.TrimSpace(params.Category)
		rule.Action.Tags = qbitTags(params.Tags)
		if params.Stopped != nil {
			rule.Action.Paused = *params.Stopped
		}
		if params.QueueTop != nil {
			rule.Action.Top = *params.QueueTop
		}
	}

	rules := d.rules()
	replaced := false
	for i := range rules {
		if rules[i].Name == ruleName {
			rules[i] = rule
			replaced = true
			break
		}
	}
	if !replaced {
		rules = append(rules, rule)
	}
	if err := d.saveRules(rules); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// qbitMustContain maps qBittorrent's mustContain onto the rule terms: words
// without "|" must all appear (Require); "a|b|c" is one of them (Include),
// each alternative kept as a phrase.
func qbitMustContain(value string) (include, require []string) {
	if !strings.Contains(value, "|") {
		return nil, qbitTerms(value)
	}
	for _, alt := range strings.Split(value, "|") {
		if alt = strings.Join(strings.Fields(alt), " "); alt != "" {
			include = append(include, alt)
		}
	}
	return include, nil
}

// qbitMustNotContain maps mustNotContain: "a|b" excludes either alternative;
// plain words each exclude (the stricter reading of "a b").
func qbitMustNotContain(value string) []string {
	if !strings.Contains(value, "|") {
		return qbitTerms(value)
	}
	var out []string
	for _, alt := range strings.Split(value, "|") {
		if alt = strings.Join(strings.Fields(alt), " "); alt != "" {
			out = append(out, alt)
		}
	}
	return out
}

// qbitTags normalizes the tag list of a rule: trimmed, empty and duplicate
// entries dropped, the order kept.
func qbitTags(tags []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

// qbitTerms splits a qBittorrent mustContain/mustNotContain value into terms.
func qbitTerms(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' || r == ' ' })
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			out = append(out, field)
		}
	}
	return out
}

func (d *Daemon) handleQbitRssRenameRule(w http.ResponseWriter, r *http.Request) {
	oldName := strings.TrimSpace(r.FormValue("ruleName"))
	newName := strings.TrimSpace(r.FormValue("newRuleName"))
	if oldName == "" || newName == "" {
		http.Error(w, "ruleName and newRuleName are required", http.StatusBadRequest)
		return
	}
	rules := d.rules()
	for i := range rules {
		if rules[i].Name == oldName {
			rules[i].Name = newName
		}
	}
	if err := d.saveRules(rules); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.renameRuleState(oldName, newName)
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitRssRemoveRule(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("ruleName"))
	if name == "" {
		http.Error(w, "ruleName is required", http.StatusBadRequest)
		return
	}
	kept := make([]rss.Rule, 0, len(d.rules()))
	for _, rule := range d.rules() {
		if rule.Name == name {
			continue
		}
		kept = append(kept, rule)
	}
	if err := d.saveRules(kept); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	d.clearRuleState(name)
	w.WriteHeader(http.StatusOK)
}
