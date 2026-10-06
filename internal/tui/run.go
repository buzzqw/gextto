package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Options configures the terminal interface.
type Options struct {
	// URL is the daemon base URL. Defaults to GEXTTO_URL or 127.0.0.1:5000.
	URL string
	// Lang forces the interface language ("it" or "en"). Empty auto-detects.
	Lang string
	// Refresh is the polling interval (default 2s).
	Refresh time.Duration
	// In/Out default to os.Stdin/os.Stdout (overridable in tests).
	In  *os.File
	Out io.Writer
}

// DefaultURL returns the daemon URL from the environment or the default.
func DefaultURL() string {
	if value := strings.TrimSpace(os.Getenv("GEXTTO_URL")); value != "" {
		return value
	}
	return "http://127.0.0.1:5000"
}

type streamEvent struct {
	snapshot     []string
	line         string
	notification *Event
	connected    bool
	closed       bool
}

// Run starts the terminal interface and blocks until the user quits.
func Run(ctx context.Context, opts Options) error {
	in := opts.In
	if in == nil {
		in = os.Stdin
	}
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	refresh := opts.Refresh
	if refresh <= 0 {
		refresh = 2 * time.Second
	}
	base := strings.TrimSpace(opts.URL)
	if base == "" {
		base = DefaultURL()
	}
	client := NewClient(base)

	daemonLang := ""
	probeCtx, cancelProbe := context.WithTimeout(ctx, 3*time.Second)
	if value, err := client.Language(probeCtx); err == nil {
		daemonLang = value
	}
	cancelProbe()
	model := NewModel(NewTranslator(ResolveLang(opts.Lang, os.Getenv("GEXTTO_LANG"), daemonLang)))
	model.ColorsEnabled = os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	model.HighContrast = strings.EqualFold(strings.TrimSpace(os.Getenv("GEXTTO_TUI_THEME")), "high-contrast")
	ascii := asciiMode(os.Getenv)
	escDelay := escapeDelay(os.Getenv("GEXTTO_TUI_ESCDELAY"))

	if _, err := unix.IoctlGetTermios(int(in.Fd()), unix.TCGETS); err != nil {
		return fmt.Errorf("%s", model.Tr.T("msg.notty"))
	}
	term := newTerminal(in, out)
	if err := term.enter(); err != nil {
		return fmt.Errorf("terminale: %w", err)
	}
	defer term.leave()
	screen := newFrameRenderer(out, ascii)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// SIGHUP arrives when the SSH connection drops: leave cleanly instead of
	// lingering as an orphan that keeps polling the daemon.
	signalCtx, stopSignals := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer stopSignals()
	resizeCh := make(chan os.Signal, 1)
	signal.Notify(resizeCh, syscall.SIGWINCH)
	defer signal.Stop(resizeCh)

	// A panic in a background goroutine would kill the process with the
	// terminal still in raw mode on the alternate screen, which over SSH
	// leaves a session that needs a blind `reset`. Restore it first.
	guard := func() {
		if recovered := recover(); recovered != nil {
			term.leave()
			fmt.Fprintf(os.Stderr, "gextto tui: panic: %v\n%s", recovered, debug.Stack())
			os.Exit(2)
		}
	}

	inputCh := make(chan []byte, 32)
	go func() { defer guard(); readInput(ctx, in, inputCh) }()

	streamCh := make(chan streamEvent, 256)
	go func() { defer guard(); readStream(ctx, client, streamCh) }()
	go func() { defer guard(); readNotifications(ctx, client, streamCh) }()

	// Refreshes run in a goroutine so a slow or unreachable daemon never blocks
	// the keyboard: the interface always reacts to q / Ctrl-C.
	refreshCh := make(chan refreshResult, 4)
	actionCh := make(chan actionResult, 4)
	var fetching int32
	var acting int32
	var lastSlow time.Time
	requestRefresh := func() {
		if !atomic.CompareAndSwapInt32(&fetching, 0, 1) {
			return
		}
		// Read the model here, on the main goroutine: the fetch below runs
		// concurrently with key handling that changes these fields.
		_, terminalHeight := term.size()
		// Health and dashboard statistics change slowly: poll them every
		// slowRefresh instead of every cycle (r and tab switches force it).
		slow := time.Since(lastSlow) >= slowRefresh
		if slow {
			lastSlow = time.Now()
		}
		request := fetchRequest{
			includeSlow:  slow,
			includeLogs:  (model.Tab == TabLogs || model.Tab == TabStatus) && !model.LogStreamConnected,
			tab:          model.Tab,
			archiveQuery: model.ArchiveFilter,
			archivePage:  model.ArchivePage,
			logLimit:     logLimitForHeight(terminalHeight),
		}
		go func() {
			defer guard()
			result := fetchModel(ctx, client, request)
			atomic.StoreInt32(&fetching, 0)
			select {
			case refreshCh <- result:
			case <-ctx.Done():
			}
		}()
	}

	parser := &KeyParser{}
	requestRefresh()
	var termOut, termIn int64
	startAction := func(action Action) bool {
		if action.Kind == ActionRefresh {
			lastSlow = time.Time{}
		}
		if action.Kind == ActionCopy {
			counted := &countingWriter{Writer: out, count: &termOut}
			if err := writeOSC52(counted, action.Text, os.Getenv); err != nil {
				model.SetMessage(model.Tr.Format("msg.copyfailed", err))
			} else {
				model.SetMessage(model.Tr.T("msg.copied"))
			}
			return false
		}
		return startActionAsync(ctx, client, model, action, requestRefresh, actionCh, &acting)
	}
	draw := func() {
		width, height := term.size()
		termOut += int64(screen.draw(model.Render(width, height), width, height))
	}
	meter := &bandwidthMeter{}
	updateMeter := func() bool {
		apiOut, apiIn, requests := client.Traffic()
		rates := meter.sample(time.Now(), [trafficKinds]int64{apiIn + termIn, apiOut + termOut, requests})
		text := formatBandwidth(rates)
		if text == model.Bandwidth {
			return false
		}
		model.Bandwidth = text
		return true
	}
	updateMeter()
	handleKeys := func(keys []Key) bool {
		for _, key := range keys {
			if key.Kind == KeyCtrlL {
				screen.invalidate()
				continue
			}
			if startAction(model.Update(key)) {
				return true
			}
		}
		return false
	}

	ticker := time.NewTicker(refresh)
	defer ticker.Stop()
	// An incomplete escape sequence is flushed only after escDelay of silence
	// on the input, measured from the last byte received: a fixed ticker could
	// fire right after an ESC whose "[A" is still crossing the network, and
	// turn an arrow key into Esc followed by the commands '[' and 'A'.
	keyFlush := time.NewTicker(25 * time.Millisecond)
	defer keyFlush.Stop()
	lastInput := time.Now()
	// Coalesce bursts of SSE log events into at most ten frames per second.
	frame := time.NewTicker(100 * time.Millisecond)
	defer frame.Stop()
	// Countdowns and notification expiry are the only time-based content.
	clock := time.NewTicker(time.Second)
	defer clock.Stop()

	quit := false
	dirty := false
	ticks := 0
	draw()
	for !quit {
		select {
		case <-signalCtx.Done():
			quit = true
		case <-resizeCh:
			screen.invalidate()
			draw()
			dirty = false
		case data, ok := <-inputCh:
			if !ok {
				// The terminal went away (SSH dropped, pty closed).
				quit = true
				break
			}
			lastInput = time.Now()
			termIn += int64(len(data))
			quit = handleKeys(parser.Feed(data, false))
			// Draw keystrokes at once: waiting for the frame ticker would add
			// up to 100 ms on top of the network round trip.
			draw()
			dirty = false
		case <-keyFlush.C:
			if parser.Pending() && time.Since(lastInput) >= escDelay {
				quit = handleKeys(parser.Feed(nil, true))
				draw()
				dirty = false
			}
		case result := <-refreshCh:
			model.applyRefresh(result)
			dirty = true
		case result := <-actionCh:
			atomic.StoreInt32(&acting, 0)
			applyActionResult(model, result, requestRefresh)
			if result.refreshInterval > 0 {
				ticker.Reset(result.refreshInterval)
			}
			dirty = true
		case event := <-streamCh:
			applyStream(model, event)
			dirty = true
		case <-ticker.C:
			requestRefresh()
		case <-clock.C:
			dirty = true
			ticks++
			if ticks%2 == 0 {
				updateMeter()
			}
		case <-frame.C:
			if dirty {
				draw()
				dirty = false
			}
		}
		// Drain pending stream events without blocking the next render.
		for draining := true; draining; {
			select {
			case event := <-streamCh:
				applyStream(model, event)
				dirty = true
			default:
				draining = false
			}
		}
	}
	return nil
}

// asciiMode reports whether to draw with ASCII only: GEXTTO_TUI_ASCII forces
// it either way, otherwise a non-UTF-8 locale (common over SSH when LANG/LC_*
// are not forwarded) or the Linux console switch it on.
func asciiMode(getenv func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv("GEXTTO_TUI_ASCII"))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return !LocaleIsUTF8(getenv)
}

// escapeDelay parses GEXTTO_TUI_ESCDELAY (milliseconds). The default suits
// SSH; raise it on very slow links where arrows turn into stray letters.
func escapeDelay(value string) time.Duration {
	const fallback = 100 * time.Millisecond
	millis, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || millis < 10 || millis > 2000 {
		return fallback
	}
	return time.Duration(millis) * time.Millisecond
}

// readInput forwards terminal input and closes ch when the terminal is gone,
// so the main loop can quit instead of spinning on a dead descriptor.
func readInput(ctx context.Context, in *os.File, ch chan<- []byte) {
	defer close(ch)
	fd := int(in.Fd())
	buffer := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		// Poll with a short timeout instead of trusting VMIN/VTIME: a blocking
		// terminal read can hang forever (and ignore keys) on some setups.
		pollFDs := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		ready, err := unix.Poll(pollFDs, 100)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return
		}
		if ready == 0 {
			continue
		}
		if pollFDs[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 && pollFDs[0].Revents&unix.POLLIN == 0 {
			return
		}
		count, readErr := unix.Read(fd, buffer)
		if count > 0 {
			data := make([]byte, count)
			copy(data, buffer[:count])
			select {
			case ch <- data:
			case <-ctx.Done():
				return
			}
			continue
		}
		if readErr == unix.EAGAIN || readErr == unix.EINTR {
			continue
		}
		// count == 0 without error is end of file; anything else is fatal.
		return
	}
}

func readStream(ctx context.Context, client *Client, ch chan<- streamEvent) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := client.StreamLogs(ctx,
			func() { sendStream(ctx, ch, streamEvent{connected: true}) },
			func(lines []string) { sendStream(ctx, ch, streamEvent{snapshot: lines}) },
			func(line string) { sendStream(ctx, ch, streamEvent{line: line}) },
		)
		sendStream(ctx, ch, streamEvent{connected: false, closed: err != nil})
		if err == nil {
			backoff = time.Second
		} else if backoff < 10*time.Second {
			backoff *= 2
			if backoff > 10*time.Second {
				backoff = 10 * time.Second
			}
		}
		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}
	}
}

func readNotifications(ctx context.Context, client *Client, ch chan<- streamEvent) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := client.StreamNotifications(ctx, func(event Event) {
			copy := event
			sendStream(ctx, ch, streamEvent{notification: &copy})
		})
		if err == nil {
			backoff = time.Second
		} else if backoff < 10*time.Second {
			backoff *= 2
			if backoff > 10*time.Second {
				backoff = 10 * time.Second
			}
		}
		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}
	}
}

func sendStream(ctx context.Context, ch chan<- streamEvent, event streamEvent) {
	select {
	case ch <- event:
	case <-ctx.Done():
	}
}

func applyStream(model *Model, event streamEvent) {
	switch {
	case event.notification != nil:
		model.ObserveEvent(*event.notification)
	case event.snapshot != nil:
		model.SetLogs(event.snapshot)
	case event.line != "":
		model.AppendLog(event.line)
	case event.connected:
		model.SetStreamConnected(true)
		model.SetStreamReconnect(0)
	case event.closed:
		model.SetStreamConnected(false)
		model.SetStreamReconnect(model.StreamReconnect + 1)
	}
}

// refreshResult carries one background poll of the daemon.
type refreshResult struct {
	status        *Status
	statusErr     string
	metrics       *DashboardStats
	hasMetrics    bool
	torrents      []Torrent
	hasTorrents   bool
	logs          []string
	hasLogs       bool
	archive       []ArchiveEntry
	hasArchive    bool
	archiveTotal  int
	archivePages  int
	archivePage   int
	missing       []Gap
	hasMissing    bool
	blocklist     []BlocklistEntry
	hasBlocklist  bool
	health        *Health
	healthErr     string
	httpDownloads []ComicDownload
	hasHTTP       bool
	series        []SeriesLibraryItem
	hasSeries     bool
	movies        []MovieLibraryItem
	hasMovies     bool
	comics        []ComicLibraryItem
	hasComics     bool
}

// fetchModel polls the daemon with a short timeout. It is safe to call from a
// goroutine because it never touches the model.
// slowRefresh is how often slowly changing data (health, statistics) is
// polled on the Status tab.
const slowRefresh = 10 * time.Second

// fetchRequest is a snapshot of the model fields a background poll needs.
type fetchRequest struct {
	includeSlow  bool
	includeLogs  bool
	tab          Tab
	archiveQuery string
	archivePage  int
	logLimit     int
}

func fetchModel(ctx context.Context, client *Client, request fetchRequest) refreshResult {
	includeLogs, tab, archiveQuery, archivePage, logLimit := request.includeLogs, request.tab, request.archiveQuery, request.archivePage, request.logLimit
	callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	var result refreshResult
	if status, err := client.Status(callCtx); err == nil {
		result.status = &status
	} else if ctx.Err() == nil {
		result.statusErr = err.Error()
	}
	if tab == TabStatus && request.includeSlow {
		if stats, err := client.Stats(callCtx); err == nil {
			result.metrics = &stats
			result.hasMetrics = true
		}
		if health, err := client.Health(callCtx); err == nil {
			result.health = &health
		}
	}
	if tab == TabTorrents || tab == TabStatus {
		if torrents, err := client.Torrents(callCtx); err == nil {
			result.torrents = torrents
			result.hasTorrents = true
		}
		if downloads, err := client.ComicDownloads(callCtx); err == nil {
			result.httpDownloads = downloads
			result.hasHTTP = true
		}
	}
	if includeLogs {
		if logs, err := client.Logs(callCtx, logLimit); err == nil {
			result.logs = logs
			result.hasLogs = true
		}
	}
	switch tab {
	case TabArchive:
		if items, total, pages, err := client.Archive(callCtx, archiveQuery, archivePage); err == nil {
			result.archive = items
			result.archiveTotal = total
			result.archivePages = pages
			result.archivePage = archivePage
			result.hasArchive = true
		}
	case TabMissing:
		if items, err := client.Gaps(callCtx); err == nil {
			result.missing = items
			result.hasMissing = true
		}
	case TabBlocklist:
		if items, err := client.Blocklist(callCtx); err == nil {
			result.blocklist = items
			result.hasBlocklist = true
		}
	case TabLibrary:
		if series, err := client.Series(callCtx); err == nil {
			result.series, result.hasSeries = series, true
		}
		if movies, err := client.Movies(callCtx); err == nil {
			result.movies, result.hasMovies = movies, true
		}
		if comics, err := client.Comics(callCtx); err == nil {
			result.comics, result.hasComics = comics, true
		}
	}
	if tab == TabHealth {
		if health, err := client.Health(callCtx); err == nil {
			result.health = &health
		} else {
			result.healthErr = err.Error()
		}
	}
	return result
}

func logLimitForHeight(height int) int {
	limit := (height - 4) * 4
	if limit < 40 {
		limit = 40
	}
	if limit > 500 {
		limit = 500
	}
	return limit
}

// applyRefresh merges a poll result into the model on the main goroutine.
func (m *Model) applyRefresh(result refreshResult) {
	if result.status != nil {
		m.SetStatus(*result.status)
		m.SetDaemonState(true, "")
	} else if result.statusErr != "" {
		m.SetDaemonState(false, result.statusErr)
	}
	if result.hasTorrents {
		m.SetTorrents(result.torrents)
	}
	if result.hasHTTP {
		m.SetHTTPDownloads(result.httpDownloads)
	}
	if result.hasMetrics && result.metrics != nil {
		m.SetMetrics(*result.metrics)
	} else if result.hasTorrents {
		m.sampleTransfer()
	}
	if result.hasSeries {
		m.SetLibrary(result.series)
	}
	if result.hasMovies {
		m.SetLibrary(result.movies)
	}
	if result.hasComics {
		m.SetLibrary(result.comics)
	}
	if result.hasLogs && !m.LogStreamConnected {
		m.SetLogs(result.logs)
	}
	if result.hasArchive {
		m.Archive = result.archive
		m.ArchiveTotal = result.archiveTotal
		m.ArchivePages = result.archivePages
		m.ArchivePage = max(1, result.archivePage)
		m.ArchiveSelected = min(m.ArchiveSelected, max(0, len(m.Archive)-1))
	}
	if result.hasMissing {
		m.Missing = result.missing
		m.MissingSelected = min(m.MissingSelected, max(0, len(m.Missing)-1))
	}
	if result.hasBlocklist {
		m.Blocklist = result.blocklist
		m.BlocklistSelected = min(m.BlocklistSelected, max(0, len(m.Blocklist)-1))
	}
	if result.health != nil {
		m.SetHealth(*result.health)
	} else if result.healthErr != "" {
		m.SetHealthError(result.healthErr)
	}
	m.Loading = false
}

type actionResult struct {
	message         string
	apply           func(*Model)
	refresh         bool
	clearMessage    bool
	refreshInterval time.Duration
}

func startActionAsync(ctx context.Context, client *Client, model *Model, action Action, refresh func(), results chan<- actionResult, acting *int32) bool {
	if action.Kind == ActionNone {
		return false
	}
	tr := model.Tr
	switch action.Kind {
	case ActionQuit:
		return true
	case ActionRefresh:
		model.Loading = true
		refresh()
		model.SetMessage(tr.T("msg.refreshed"))
		return false
	}
	if !atomic.CompareAndSwapInt32(acting, 0, 1) {
		model.SetMessage(tr.T("msg.busy"))
		return false
	}
	model.Loading = true
	go func() {
		result := performAction(ctx, client, tr, action)
		select {
		case results <- result:
		case <-ctx.Done():
		}
	}()
	return false
}

func performAction(ctx context.Context, client *Client, tr *Translator, action Action) actionResult {
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result := actionResult{}
	fail := func(message string, args ...any) actionResult {
		result.message = tr.Format(message, args...)
		return result
	}

	switch action.Kind {
	case ActionRunCycle:
		response, err := client.RunCycle(callCtx, action.Domain)
		if err != nil {
			return fail("msg.cyclefailed", err)
		}
		queued, _ := response["queued"].(bool)
		switch {
		case queued:
			result.message = tr.Format("msg.cyclequeued", action.Domain)
		case action.Domain == "" || action.Domain == "full":
			result.message = tr.T("msg.cyclefull")
		default:
			result.message = tr.Format("msg.cyclestarted", action.Domain)
		}
		result.refresh = true
	case ActionSearch:
		items, err := client.Search(callCtx, action.Text)
		if err != nil {
			return fail("msg.searchfailed", err)
		}
		result.apply = func(m *Model) { m.SetSearchResults(action.Text, items) }
		result.message = tr.Format("msg.search", len(items))
	case ActionAddMagnet:
		if err := client.AddMagnet(callCtx, action.Text); err != nil {
			return fail("msg.addfailed", err)
		}
		result.message = tr.T("msg.magneton")
		result.refresh = true
	case ActionAddFile:
		if err := client.AddTorrentFile(callCtx, action.Text); err != nil {
			return fail("msg.addfailed", err)
		}
		result.message = tr.T("msg.torrentadded")
		result.refresh = true
	case ActionPauseToggle:
		detail, err := client.TorrentDetail(callCtx, action.Hash)
		if err != nil {
			return fail("msg.actionfailed", tr.T("tab.torrents"), err)
		}
		paused := detail.Torrent.State == "paused"
		if paused {
			err = client.Resume(callCtx, action.Hash)
		} else {
			err = client.Pause(callCtx, action.Hash)
		}
		if err != nil {
			return fail("msg.actionfailed", tr.T("tab.torrents"), err)
		}
		if paused {
			result.message = tr.T("msg.resumed")
		} else {
			result.message = tr.T("msg.paused")
		}
		result.refresh = true
	case ActionHTTPPauseToggle:
		downloads, err := client.ComicDownloads(callCtx)
		if err != nil {
			return fail("msg.actionfailed", tr.T("label.downloads"), err)
		}
		paused := false
		for _, download := range downloads {
			if download.ID == action.HTTPID {
				paused = strings.EqualFold(download.Status, "paused")
				break
			}
		}
		if paused {
			err = client.ResumeHTTPDownload(callCtx, action.HTTPID)
		} else {
			err = client.PauseHTTPDownload(callCtx, action.HTTPID)
		}
		if err != nil {
			return fail("msg.actionfailed", tr.T("label.downloads"), err)
		}
		if paused {
			result.message = tr.T("msg.resumed")
		} else {
			result.message = tr.T("msg.paused")
		}
		result.refresh = true
	case ActionHTTPRemove:
		if err := client.RemoveHTTPDownload(callCtx, action.HTTPID); err != nil {
			return fail("msg.actionfailed", tr.T("label.downloads"), err)
		}
		result.message = tr.T("msg.removed")
		result.refresh = true
	case ActionRestart:
		if err := client.Restart(callCtx, action.Hash); err != nil {
			return fail("msg.actionfailed", "restart", err)
		}
		result.message = tr.T("msg.restarted")
		result.refresh = true
	case ActionRemove:
		if err := client.Remove(callCtx, action.Hash, action.DeleteFiles, false); err != nil {
			return fail("msg.actionfailed", "remove", err)
		}
		if action.DeleteFiles {
			result.message = tr.T("msg.removedfiles")
		} else {
			result.message = tr.T("msg.removed")
		}
		result.refresh = true
	case ActionRecheck:
		if err := client.Recheck(callCtx, action.Hash); err != nil {
			return fail("msg.actionfailed", "recheck", err)
		}
		result.message = tr.T("msg.rechecked")
	case ActionReannounce:
		if err := client.Reannounce(callCtx, action.Hash); err != nil {
			return fail("msg.actionfailed", "reannounce", err)
		}
		result.message = tr.T("msg.reannounced")
	case ActionNoRenameToggle:
		detail, err := client.TorrentDetail(callCtx, action.Hash)
		if err != nil {
			return fail("msg.actionfailed", "no-rename", err)
		}
		if err := client.SetNoRename(callCtx, action.Hash, !detail.NoRename); err != nil {
			return fail("msg.actionfailed", "no-rename", err)
		}
		if detail.NoRename {
			result.message = tr.T("msg.norenameoff")
		} else {
			result.message = tr.T("msg.norenameon")
		}
	case ActionPin:
		if err := client.Pin(callCtx, action.Hash); err != nil {
			return fail("msg.actionfailed", "pin", err)
		}
		result.message = tr.T("msg.pinned")
	case ActionUnpin:
		if err := client.Unpin(callCtx); err != nil {
			return fail("msg.actionfailed", "unpin", err)
		}
		result.message = tr.T("msg.unpinned")
	case ActionCleanCompleted:
		response, err := client.RemoveCompleted(callCtx, false)
		if err != nil {
			return fail("msg.actionfailed", "clean", err)
		}
		result.message = tr.Format("msg.removedone", int(numberValue(response["removed"])), int(numberValue(response["skipped"])))
		result.refresh = true
	case ActionSetLimits:
		if err := client.SetSpeedLimits(callCtx, action.DL, action.UL); err != nil {
			return fail("msg.actionfailed", "limits", err)
		}
		result.apply = func(m *Model) {
			if m.Config != nil {
				m.Config.DownloadLimitKib = action.DL
				m.Config.UploadLimitKib = action.UL
			}
		}
		result.message = tr.Format("msg.limits", action.DL, action.UL)
	case ActionLoadConfig:
		config, err := client.Config(callCtx)
		if err != nil {
			return fail("msg.settingsfailed", err)
		}
		result.apply = func(m *Model) {
			m.SetConfig(TUIConfig{
				Active:           config.Active,
				DryRun:           config.DryRun,
				RefreshSecs:      config.RefreshSecs,
				DefaultLanguage:  config.DefaultLanguage,
				DownloadLimitKib: config.Libtorrent.DownloadLimitKib,
				UploadLimitKib:   config.Libtorrent.UploadLimitKib,
			})
		}
		result.clearMessage = true
	case ActionSaveSetting:
		if err := client.SaveSetting(callCtx, action.Domain, action.Text); err != nil {
			return fail("msg.settingsfailed", err)
		}
		result.apply = func(m *Model) {
			if m.Config == nil {
				return
			}
			switch action.Domain {
			case "refresh_interval":
				if seconds, err := strconv.ParseUint(action.Text, 10, 64); err == nil {
					m.Config.RefreshSecs = seconds
				}
			case "dry_run":
				m.Config.DryRun = action.Text == "true"
			}
		}
		if action.Domain == "refresh_interval" {
			if seconds, err := strconv.ParseInt(action.Text, 10, 64); err == nil {
				if seconds < 1 {
					seconds = 1
				} else if seconds > 3600 {
					seconds = 3600
				}
				result.refreshInterval = time.Duration(seconds) * time.Second
			}
		}
		result.message = tr.T("msg.settingssaved")
		if action.Domain == "dry_run" {
			result.message = tr.T("msg.settingsrestart")
		}
		result.refresh = true
	case ActionSetLanguage:
		if err := client.SetLanguage(callCtx, action.Text); err != nil {
			return fail("msg.settingsfailed", err)
		}
		result.apply = func(m *Model) {
			m.Tr = NewTranslator(action.Text)
			if m.Config != nil {
				m.Config.DefaultLanguage = action.Text
			}
		}
		result.message = tr.T("msg.languagesaved")
		result.refresh = true
	case ActionCleanTrash:
		response, err := client.CleanTrash(callCtx)
		if err != nil {
			return fail("msg.trashfailed", err)
		}
		if files, ok := response["files"]; ok {
			result.message = tr.Format("msg.trashcleaned", int(numberValue(files)))
		} else {
			result.message = tr.T("msg.trashcleanedna")
		}
		result.refresh = true
	case ActionQueueRelease:
		if err := client.AddRelease(callCtx, action.Release); err != nil {
			return fail("msg.queuefailed", err)
		}
		result.apply = func(m *Model) { m.Overlay = OverlayNone }
		result.message = tr.T("msg.queued")
		result.refresh = true
	case ActionOpenDetails:
		detail, err := client.TorrentDetail(callCtx, action.Hash)
		if err != nil {
			return fail("msg.detailsfailed", err)
		}
		result.apply = func(m *Model) { m.SetDetail(detail) }
		result.clearMessage = true
	case ActionLoadDetail:
		items, err := client.TorrentCollection(callCtx, action.Hash, action.DetailKind)
		if err != nil {
			return fail("msg.detailfailed", err)
		}
		result.apply = func(m *Model) { m.SetDetailItems(items) }
	case ActionLoadEvents:
		events, err := client.Events(callCtx)
		if err != nil {
			return fail("msg.eventsfailed", err)
		}
		result.apply = func(m *Model) {
			m.SetEvents(events)
			m.Overlay = OverlayEvents
		}
		result.message = tr.Format("msg.events", len(events))
	case ActionLoadArchive:
		page := action.Page
		if page < 1 {
			page = 1
		}
		items, total, pages, err := client.Archive(callCtx, action.Text, page)
		if err != nil {
			return fail("msg.archivefailed", err)
		}
		result.apply = func(m *Model) {
			m.Archive = items
			m.ArchiveTotal = total
			m.ArchivePages = pages
			m.ArchivePage = page
			m.ArchiveSelected = min(m.ArchiveSelected, max(0, len(items)-1))
		}
	case ActionLoadMissing:
		items, err := client.Gaps(callCtx)
		if err != nil {
			return fail("msg.missingfailed", err)
		}
		result.apply = func(m *Model) {
			m.Missing = items
			m.MissingSelected = min(m.MissingSelected, max(0, len(items)-1))
		}
	case ActionLoadBlocklist:
		items, err := client.Blocklist(callCtx)
		if err != nil {
			return fail("msg.blocklistfailed", err)
		}
		result.apply = func(m *Model) {
			m.Blocklist = items
			m.BlocklistSelected = min(m.BlocklistSelected, max(0, len(items)-1))
		}
	case ActionRemoveBlocklist:
		if err := client.RemoveBlocklist(callCtx, action.Hash); err != nil {
			return fail("msg.blocklistremovefailed", err)
		}
		result.message = tr.T("msg.blocklistremoved")
		result.refresh = true
	}
	return result
}

func applyActionResult(model *Model, result actionResult, refresh func()) {
	if result.apply != nil {
		result.apply(model)
	}
	if result.clearMessage {
		model.SetMessage("")
	} else if result.message != "" {
		model.SetMessage(result.message)
	}
	model.Loading = false
	if result.refresh {
		refresh()
	}
}

// countingWriter counts bytes written to the terminal outside the renderer.
type countingWriter struct {
	io.Writer
	count *int64
}

func (w *countingWriter) Write(data []byte) (int, error) {
	written, err := w.Writer.Write(data)
	*w.count += int64(written)
	return written, err
}
