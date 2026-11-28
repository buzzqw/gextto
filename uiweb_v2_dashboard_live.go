package gextto

import (
	"context"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// uiweb_v2_dashboard_live.go builds the live parts of the Dashboard: the strip
// of figures at the top (load, network, torrents, space, archive), the frame of
// what is downloading now, the health summary and the week of releases. It also
// feeds the small speed panel at the foot of the sidebar.

// sideBarCount is how many bars the sidebar speed panel draws for the last 30
// minutes: one every minute and a half.
const sideBarCount = 20

// v2SideBars turns the rate history of the last 30 minutes into bar heights in
// percent of the highest one. Nothing is drawn until two samples exist.
func v2SideBars(s *AppState) []int {
	if s == nil || s.rate_history == nil {
		return nil
	}
	samples := s.rate_history.snapshot()
	now := time.Now()
	start := now.Add(-rateHistoryWindow)
	for len(samples) > 0 && samples[0].At.Before(start) {
		samples = samples[1:]
	}
	if len(samples) < 2 {
		return nil
	}
	sums := make([]uint64, sideBarCount)
	counts := make([]int, sideBarCount)
	span := rateHistoryWindow.Seconds()
	for _, sample := range samples {
		index := int(sample.At.Sub(start).Seconds() / span * sideBarCount)
		if index < 0 {
			index = 0
		}
		if index >= sideBarCount {
			index = sideBarCount - 1
		}
		sums[index] += v2SampleDown(sample)
		counts[index]++
	}
	var top uint64
	averages := make([]uint64, sideBarCount)
	for index := range sums {
		if counts[index] > 0 {
			averages[index] = sums[index] / uint64(counts[index])
		}
		if averages[index] > top {
			top = averages[index]
		}
	}
	bars := make([]int, sideBarCount)
	for index, value := range averages {
		height := 4
		if top > 0 {
			height = int(float64(value) / float64(top) * 100)
		}
		if height < 4 {
			height = 4
		}
		bars[index] = height
	}
	return bars
}

// percentOf caps a value to 0..100 for a bar width.
func percentOf(value float64) int {
	switch {
	case value < 0:
		return 0
	case value > 100:
		return 100
	}
	return int(value + 0.5)
}

// v2DashLoadRow is one process (or the host) in the "Carico" tile.
type v2DashLoadRow struct {
	Label  string
	Value  string
	Detail string
	Pct    int
	Kind   string
	Title  string
}

// v2DashStats is the strip at the top of the Dashboard. It is rendered with the
// page and refreshed by the SSE stream every few seconds.
type v2DashStats struct {
	Load []v2DashLoadRow

	Download  string
	Upload    string
	LatencyMs int64
	// Peers is the number of connected peers; DHTNodes and Port come from the
	// gx-torrent engine and are empty with another engine.
	Peers    int
	Seeds    int
	DHTNodes int64
	HasDHT   bool
	Port     string
	PortOK   bool

	Active   int
	Waiting  int
	Seeding  int
	Torrents int

	FreeSpace string
	DiskTotal string
	UsedPct   int
	HasDisk   bool

	ArchiveTotal int64
	SeenGroups   int64
}

// v2DashNetwork is what the engine says about the network, kept for a few
// seconds: on gx-torrent it is one request to the daemon.
type v2DashNetwork struct {
	HasDHT   bool
	DHTNodes int64
	Port     string
	PortOK   bool
}

var (
	v2DashNetworkMu    sync.Mutex
	v2DashNetworkAt    time.Time
	v2DashNetworkValue v2DashNetwork

	v2DashArchiveMu    sync.Mutex
	v2DashArchiveAt    time.Time
	v2DashArchiveTotal int64
	v2DashArchiveSeen  int64
)

const (
	v2DashNetworkTTL = 15 * time.Second
	v2DashArchiveTTL = 60 * time.Second
)

func v2DashNetworkFrom(s *AppState) v2DashNetwork {
	v2DashNetworkMu.Lock()
	defer v2DashNetworkMu.Unlock()
	if time.Since(v2DashNetworkAt) < v2DashNetworkTTL {
		return v2DashNetworkValue
	}
	value := v2DashNetwork{}
	if s != nil && s.hasTorrentEngine() && s.activeEngine().Name() != BackendQbittorrent {
		stats := v2SafeEngineStats(s.activeEngine())
		if loaded, _ := stats["session_loaded"].(bool); loaded {
			session := engineMap(stats, "session")
			mapping := engineMap(stats, "port_mapping")
			if engineBool(stats, "dht") {
				value.HasDHT = true
				value.DHTNodes = engineNum(session, "dht_nodes")
			}
			incoming := engineNum(session, "peers_incoming_tcp") + engineNum(session, "peers_incoming_utp")
			switch {
			case engineString(mapping, "method") != "":
				value.Port, value.PortOK = "aperta sul router", true
			case incoming > 0 || engineNum(session, "dht_incoming_recent") > 0:
				value.Port, value.PortOK = "raggiungibile", true
			case engineNum(stats, "peer_port") > 0:
				value.Port = "forse chiusa sul router"
			}
		}
	}
	v2DashNetworkValue, v2DashNetworkAt = value, time.Now()
	return value
}

// v2SafeEngineStats reads the engine statistics without letting an engine that
// cannot answer take the Dashboard down: the strip then shows no DHT and port.
func v2SafeEngineStats(engine TorrentEngine) (stats map[string]any) {
	defer func() {
		if recover() != nil {
			stats = nil
		}
	}()
	return engine.Stats()
}

// v2DashArchiveFrom counts the archive and the remembered releases at most once
// a minute: the archive holds hundreds of thousands of rows.
func v2DashArchiveFrom(s *AppState) (int64, int64) {
	v2DashArchiveMu.Lock()
	defer v2DashArchiveMu.Unlock()
	if !v2DashArchiveAt.IsZero() && time.Since(v2DashArchiveAt) < v2DashArchiveTTL {
		return v2DashArchiveTotal, v2DashArchiveSeen
	}
	var total, seen int64
	if s != nil && s.archive != nil {
		if page, err := s.archive.BrowsePage("", 1, 1); err == nil && page != nil {
			total = page.Total
		}
	}
	if s != nil && s.db != nil {
		if movies, series, err := s.db.SeenCounts(); err == nil {
			seen = movies + series
		}
	}
	v2DashArchiveTotal, v2DashArchiveSeen, v2DashArchiveAt = total, seen, time.Now()
	return total, seen
}

// v2DashStatsFrom builds the strip from the chrome already computed for the same
// tick, so the process samples are taken once.
func v2DashStatsFrom(s *AppState, chrome uiShellChrome) v2DashStats {
	stats := v2DashStats{
		Download:  chrome.Download,
		Upload:    chrome.Upload,
		LatencyMs: chrome.LatencyMs,
		Peers:     chrome.Peers,
		Seeds:     chrome.Seeds,
		Torrents:  chrome.Torrents,
		Active:    chrome.Transferring,
		Waiting:   chrome.Stuck,
	}
	stats.Load = append(stats.Load, v2DashLoadRow{Label: "Gextto", Value: chrome.CPU, Detail: chrome.RAM, Pct: percentOf(chrome.CPUPercent), Kind: "app", Title: "CPU e RAM del processo Gextto"})
	if chrome.GxLoad != "" {
		stats.Load = append(stats.Load, v2DashLoadRow{Label: "gx-torrent", Value: chrome.GxCPU, Detail: chrome.GxRAM, Pct: percentOf(chrome.GxCPUPercent), Kind: "gx", Title: "CPU e RAM del motore gx-torrent"})
	}
	if chrome.AmuleLoad != "" {
		stats.Load = append(stats.Load, v2DashLoadRow{Label: "aMule", Value: chrome.AmuleCPU, Detail: chrome.AmuleRAM, Pct: percentOf(chrome.AmuleCPUPercent), Kind: "ed2k", Title: "CPU e RAM del processo amuled su questa macchina"})
	}
	if load := healthLoadAverage(); load != nil {
		cores := runtime.NumCPU()
		if cores < 1 {
			cores = 1
		}
		row := v2DashLoadRow{Label: "Host", Value: strconv.FormatFloat(*load, 'f', 2, 64), Pct: percentOf(*load / float64(cores) * 100), Kind: "host", Title: "Carico medio della macchina nell'ultimo minuto, con la memoria in uso"}
		if total, available := memoryInfo(); total > 0 && available <= total {
			row.Detail = logging.HumanBytesI64(saturatingInt64(total-available)) + " / " + logging.HumanBytesI64(saturatingInt64(total))
		}
		stats.Load = append(stats.Load, row)
	}
	if s != nil && s.hasTorrentEngine() {
		for _, view := range SessionSnapshot(s.activeEngine()) {
			if view.Progress >= 100 && (view.IsSeeding || view.State == "seeding" || view.State == "finished") {
				stats.Seeding++
			}
		}
	}
	network := v2DashNetworkFrom(s)
	stats.HasDHT, stats.DHTNodes = network.HasDHT, network.DHTNodes
	stats.Port, stats.PortOK = network.Port, network.PortOK

	if s != nil {
		if cfg := latestConfig(s); cfg != nil && cfg.DownloadDir != "" {
			if total, free := diskSpace(cfg.DownloadDir); total > 0 {
				stats.HasDisk = true
				stats.FreeSpace = logging.HumanBytesI64(saturatingInt64(free))
				stats.DiskTotal = logging.HumanBytesI64(saturatingInt64(total))
				stats.UsedPct = percentOf(float64(total-free) / float64(total) * 100)
			}
		}
	}
	stats.ArchiveTotal, stats.SeenGroups = v2DashArchiveFrom(s)
	return stats
}

// v2DashNowItem is one card of "In scarico ora".
type v2DashNowItem struct {
	Title    string
	Sub      string
	Poster   string
	Initial  string
	Pct      int
	Progress string
	Rate     string
	ETA      string
	Network  string
	State    string
	Stuck    bool
}

type v2DashNowView struct {
	Items  []v2DashNowItem
	Active int
	Total  int
}

// v2DashNowLimit is how many downloads the Dashboard frame shows.
const v2DashNowLimit = 4

// v2DashTitle names a download the way the library does: the series with its
// episode when the release is an episode, the cleaned release name otherwise.
func v2DashTitle(cfg *Config, name string) (title, sub, series string) {
	title = strings.TrimSpace(name)
	release := ParseRelease(name, "", "")
	if release == nil || release.Series == nil || strings.TrimSpace(*release.Series) == "" {
		return title, "", ""
	}
	parsed := strings.TrimSpace(*release.Series)
	if cfg != nil {
		for index := range cfg.Series {
			if seriesNameMatchesAny(cfg.Series[index].Name, cfg.Series[index].Aliases, parsed) {
				series = cfg.Series[index].Name
				break
			}
		}
	}
	if series != "" {
		title = series
	}
	parts := []string{}
	switch {
	case release.Season != nil && release.Episode != nil:
		parts = append(parts, "S"+v2TwoDigits(*release.Season)+"E"+v2TwoDigits(*release.Episode))
	case release.Season != nil:
		parts = append(parts, "S"+v2TwoDigits(*release.Season))
	}
	if label := strings.TrimSpace(string(release.Quality.Resolution)); label != "" {
		parts = append(parts, label)
	}
	return title, strings.Join(parts, " · "), series
}

func v2TwoDigits(value int64) string {
	if value < 10 && value >= 0 {
		return "0" + strconv.FormatInt(value, 10)
	}
	return strconv.FormatInt(value, 10)
}

func v2Initial(title string) string {
	for _, r := range title {
		return strings.ToUpper(string(r))
	}
	return "?"
}

// v2DashNowFrom picks the downloads worth a glance: the ones moving data first,
// fastest first, then the unfinished ones closest to the end.
func v2DashNowFrom(s *AppState) v2DashNowView {
	view := v2DashNowView{}
	if s == nil {
		return view
	}
	cfg := latestConfig(s)
	type candidate struct {
		item  v2DashNowItem
		rate  uint64
		pct   float64
		named string
	}
	var candidates []candidate
	if s.hasTorrentEngine() {
		for _, torrent := range SessionSnapshot(s.activeEngine()) {
			if torrent.Progress >= 100 {
				continue
			}
			view.Total++
			title, sub, series := v2DashTitle(cfg, torrent.Name)
			item := v2DashNowItem{Title: title, Sub: sub, Network: "torrent", Pct: percentOf(torrent.Progress)}
			item.Progress = strconv.Itoa(item.Pct) + "%"
			switch {
			case TorrentTransferring(torrent):
				view.Active++
				item.State = "in scarico"
				item.Rate = logging.HumanRate(saturatingInt64(torrent.DownloadRate))
				if torrent.EtaSeconds > 0 {
					item.ETA = logging.HumanDuration(torrent.EtaSeconds)
				}
			case torrent.State == "paused":
				item.State = "in pausa"
			case torrent.State == "queued":
				item.State = "in coda"
			case !torrent.HasMetadata:
				item.State = "metadati"
			default:
				item.State, item.Stuck = "fermo", true
			}
			candidates = append(candidates, candidate{item: item, rate: torrent.DownloadRate, pct: torrent.Progress, named: series})
		}
	}
	for _, download := range Ed2kDownloads(cfg) {
		if download.Complete() {
			continue
		}
		view.Total++
		title, sub, series := v2DashTitle(cfg, download.Name)
		item := v2DashNowItem{Title: title, Sub: strings.TrimPrefix(sub+" · eD2k", " · "), Network: "ed2k", Pct: percentOf(download.Progress())}
		item.Progress = strconv.Itoa(item.Pct) + "%"
		if download.Speed > 0 {
			view.Active++
			item.State = "in scarico"
			item.Rate = logging.HumanRate(download.Speed)
		} else {
			item.State, item.Stuck = "fermo", true
		}
		candidates = append(candidates, candidate{item: item, rate: uint64(max(download.Speed, 0)), pct: download.Progress(), named: series})
	}
	for _, download := range HTTPDownloads() {
		if download.Status != "downloading" {
			continue
		}
		view.Total++
		view.Active++
		item := v2DashNowItem{Title: strings.TrimSpace(download.Title), Sub: "HTTP", Network: "http", Pct: percentOf(download.Progress), State: "in scarico"}
		item.Progress = strconv.Itoa(item.Pct) + "%"
		item.Rate = logging.HumanRate(saturatingInt64(download.SpeedBytes))
		if download.ETASeconds != nil {
			item.ETA = logging.HumanDuration(saturatingInt64(*download.ETASeconds))
		}
		candidates = append(candidates, candidate{item: item, rate: download.SpeedBytes, pct: download.Progress})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if (candidates[i].rate > 0) != (candidates[j].rate > 0) {
			return candidates[i].rate > 0
		}
		if candidates[i].rate != candidates[j].rate {
			return candidates[i].rate > candidates[j].rate
		}
		return candidates[i].pct > candidates[j].pct
	})
	if len(candidates) > v2DashNowLimit {
		candidates = candidates[:v2DashNowLimit]
	}
	names := []string{}
	for _, c := range candidates {
		if c.named != "" {
			names = append(names, c.named)
		}
	}
	posters := map[string]string{}
	if len(names) > 0 && cfg != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		posters = v2SeriesPosters(ctx, cfg, names)
		cancel()
	}
	for _, c := range candidates {
		item := c.item
		item.Initial = v2Initial(item.Title)
		if c.named != "" {
			item.Poster = v2SafeHref(posters[strings.ToLower(c.named)])
		}
		view.Items = append(view.Items, item)
	}
	return view
}

// V2DashboardNowPartial refreshes "In scarico ora".
func V2DashboardNowPartial(w http.ResponseWriter, r *http.Request, s *AppState) {
	v2Render(w, http.StatusOK, "v2_dash_now", v2DashNowFrom(s), s)
}

// v2DashHealthRow is one line of the health summary.
type v2DashHealthRow struct {
	Label  string
	Detail string
	Value  string
	Note   string
	Warn   bool
	Title  string
	Bars   []int
	Dots   []bool
}

type v2DashHealthView struct {
	Rows  []v2DashHealthRow
	Warns int
}

// v2DashHealthFrom summarises the Salute page: feeds, indexers, the other
// integrations, the title queue, eD2k and the folders. Every figure is read
// from what Gextto already keeps; nothing here probes a remote service.
func v2DashHealthFrom(s *AppState) v2DashHealthView {
	view := v2DashHealthView{}
	if s == nil {
		return view
	}
	cfg := latestConfig(s)
	add := func(row v2DashHealthRow) {
		if row.Warn {
			view.Warns++
		}
		view.Rows = append(view.Rows, row)
	}

	_, _, feeds := v2FeedStatsFrom(s)
	if feeds.Total > 0 {
		row := v2DashHealthRow{Label: "Feed RSS", Title: "Feed letti da Gextto: quanti sono attivi e quante release hanno dato oggi"}
		row.Detail = strconv.Itoa(feeds.Active) + " / " + strconv.Itoa(feeds.Total)
		row.Value = strconv.FormatInt(feeds.Accepted1, 10)
		row.Note = "prese oggi"
		row.Warn = feeds.Disabled > 0
		add(row)
	}

	indexers, failing, other := 0, 0, 0
	if cfg != nil {
		for _, indexer := range cfg.Indexers {
			if indexer.Enabled && strings.TrimSpace(indexer.Name) != "" {
				indexers++
			}
		}
	}
	dots := []bool{}
	if s.db != nil {
		if items, err := s.db.ProviderStatuses(); err == nil {
			for _, entry := range items {
				if cfg != nil && !activeProvider(cfg, entry.Kind, entry.Provider) {
					continue
				}
				if entry.Level <= 0 {
					continue
				}
				if entry.Kind == "indexer" {
					failing++
				} else {
					other++
				}
			}
		}
	}
	if indexers > 0 {
		for index := 0; index < indexers && index < 12; index++ {
			dots = append(dots, index >= indexers-failing)
		}
		row := v2DashHealthRow{Label: "Indexer", Dots: dots, Title: "Un punto per ogni indexer attivo: arancione quando Gextto lo ha messo in pausa dopo degli errori"}
		row.Detail = strconv.Itoa(indexers-failing) + " / " + strconv.Itoa(indexers)
		row.Warn = failing > 0
		add(row)
	}
	integrations := v2DashHealthRow{Label: "Integrazioni", Title: "Metadati, notifiche e servizi collegati: quanti sono in pausa dopo degli errori"}
	if other > 0 {
		integrations.Value, integrations.Note, integrations.Warn = strconv.Itoa(other), "in errore", true
	} else {
		integrations.Value, integrations.Note = "ok", "nessun errore"
	}
	add(integrations)

	if s.db != nil {
		if queue, err := s.db.PendingQueue(time.Now().UTC()); err == nil {
			row := v2DashHealthRow{Label: "Coda titoli", Title: "Titoli trattenuti da un profilo di attesa"}
			row.Detail = strconv.Itoa(len(queue))
			if len(queue) > 0 {
				next := queue[0].Due
				for _, item := range queue[1:] {
					if item.Due < next {
						next = item.Due
					}
				}
				row.Value = logging.HumanDuration(int64(max(next, 0) / time.Second))
				row.Note = "prossima valutazione"
			} else {
				row.Value, row.Note = "—", "nessuno in attesa"
			}
			add(row)
		}
	}

	if cfg != nil && ed2kEnabled(cfg) {
		row := v2DashHealthRow{Label: "eD2k", Title: "Stato di aMule: connessione ai server e a Kad"}
		if health := v2Ed2kHealthFrom(s); health != nil {
			switch {
			case !health.Reachable:
				row.Detail, row.Warn = "non raggiungibile", true
			case !health.Connected:
				row.Detail, row.Warn = "non connesso", true
			case health.HighID:
				row.Detail = "ID alto"
			default:
				row.Detail, row.Warn = "ID basso", true
			}
			if health.Kad != "" {
				row.Value, row.Note = health.Kad, "Kad"
			}
		}
		add(row)
	}

	paths := uiHealthPathsFrom(s)
	folders, writable := 0, 0
	for _, path := range []string{paths.DataDir, paths.DownloadPath, paths.ArchiveRoot} {
		if strings.TrimSpace(path) == "" {
			continue
		}
		folders++
		if dataDirWritable(path) {
			writable++
		}
	}
	if folders > 0 {
		add(v2DashHealthRow{Label: "Cartelle", Title: "Cartella dei dati, dei download e dell'archivio: Gextto deve poterci scrivere",
			Value: strconv.Itoa(writable) + " / " + strconv.Itoa(folders), Note: "scrivibili", Warn: writable < folders})
	}
	return view
}

// V2DashboardHealthPartial refreshes the health summary.
func V2DashboardHealthPartial(w http.ResponseWriter, r *http.Request, s *AppState) {
	v2Render(w, http.StatusOK, "v2_dash_health", v2DashHealthFrom(s), s)
}

// v2DashDay is one column of the week of releases.
type v2DashDay struct {
	Day     int
	Weekday string
	Today   bool
	Items   []v2DashboardCalendarItem
}

type v2DashWeek struct {
	Days  []v2DashDay
	Later []v2DashboardCalendarItem
}

var v2Weekdays = []string{"dom", "lun", "mar", "mer", "gio", "ven", "sab"}

// v2DashWeekFrom lays the upcoming episodes on the next seven days; what airs
// later is listed apart, so nothing the calendar knows is lost.
func v2DashWeekFrom(items []v2DashboardCalendarItem, now time.Time) v2DashWeek {
	week := v2DashWeek{}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	index := map[string]int{}
	for offset := 0; offset < 7; offset++ {
		day := today.AddDate(0, 0, offset)
		index[day.Format("2006-01-02")] = offset
		week.Days = append(week.Days, v2DashDay{Day: day.Day(), Weekday: v2Weekdays[int(day.Weekday())], Today: offset == 0})
	}
	for _, item := range items {
		date := item.AirDate
		if len(date) > 10 {
			date = date[:10]
		}
		if offset, ok := index[date]; ok {
			week.Days[offset].Items = append(week.Days[offset].Items, item)
			continue
		}
		if parsed, err := time.ParseInLocation("2006-01-02", date, now.Location()); err == nil && parsed.After(today) && len(week.Later) < 4 {
			week.Later = append(week.Later, item)
		}
	}
	return week
}
