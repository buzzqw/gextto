package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
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
	model.ColorsEnabled = os.Getenv("NO_COLOR") == ""
	model.HighContrast = strings.EqualFold(strings.TrimSpace(os.Getenv("GEXTTO_TUI_THEME")), "high-contrast")

	term := newTerminal(in, out)
	if err := term.enter(); err != nil {
		return fmt.Errorf("terminale: %w", err)
	}
	defer term.leave()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	signalCtx, stopSignals := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stopSignals()

	inputCh := make(chan []byte, 32)
	go readInput(ctx, in, inputCh)

	streamCh := make(chan streamEvent, 256)
	go readStream(ctx, client, streamCh)
	go readNotifications(ctx, client, streamCh)

	// Refreshes run in a goroutine so a slow or unreachable daemon never blocks
	// the keyboard: the interface always reacts to q / Ctrl-C.
	refreshCh := make(chan refreshResult, 4)
	actionCh := make(chan actionResult, 4)
	var fetching int32
	var acting int32
	requestRefresh := func() {
		if !atomic.CompareAndSwapInt32(&fetching, 0, 1) {
			return
		}
		go func() {
			_, terminalHeight := term.size()
			result := fetchModel(ctx, client, model.Tab == TabLogs && !model.LogStreamConnected, model.Tab, model.ArchiveFilter, model.ArchivePage, logLimitForHeight(terminalHeight))
			atomic.StoreInt32(&fetching, 0)
			select {
			case refreshCh <- result:
			case <-ctx.Done():
			}
		}()
	}

	parser := &KeyParser{}
	requestRefresh()
	startAction := func(action Action) bool {
		if action.Kind == ActionCopy {
			if err := writeOSC52(out, action.Text); err != nil {
				model.SetMessage(model.Tr.Format("msg.copyfailed", err))
			} else {
				model.SetMessage(model.Tr.T("msg.copied"))
			}
			return false
		}
		return startActionAsync(ctx, client, model, action, requestRefresh, actionCh, &acting)
	}

	ticker := time.NewTicker(refresh)
	defer ticker.Stop()
	// KeyParser needs a short timeout to distinguish an incomplete escape
	// sequence from a standalone key. It must not also drive screen redraws:
	// doing so made the log flash continuously even while it was idle.
	keyFlush := time.NewTicker(50 * time.Millisecond)
	defer keyFlush.Stop()
	// Coalesce bursts of SSE log events into at most ten frames per second.
	frame := time.NewTicker(100 * time.Millisecond)
	defer frame.Stop()
	// The next-cycle countdown is the only time-based content on screen.
	clock := time.NewTicker(time.Second)
	defer clock.Stop()

	quit := false
	dirty := true
	width, height := term.size()
	renderANSIConvert(out, model.Render(width, height))
	dirty = false
	for !quit {
		select {
		case <-signalCtx.Done():
			quit = true
		case data := <-inputCh:
			for _, key := range parser.Feed(data, false) {
				dirty = true
				if startAction(model.Update(key)) {
					quit = true
					break
				}
			}
		case <-keyFlush.C:
			for _, key := range parser.Feed(nil, true) {
				dirty = true
				if startAction(model.Update(key)) {
					quit = true
					break
				}
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
		case <-frame.C:
			if dirty {
				width, height := term.size()
				renderANSIConvert(out, model.Render(width, height))
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

func readInput(ctx context.Context, in *os.File, ch chan<- []byte) {
	fd := int(in.Fd())
	buffer := make([]byte, 256)
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
		count, readErr := unix.Read(fd, buffer)
		if count > 0 {
			data := make([]byte, count)
			copy(data, buffer[:count])
			select {
			case ch <- data:
			case <-ctx.Done():
				return
			}
		}
		if readErr != nil && readErr != unix.EAGAIN && readErr != unix.EINTR {
			return
		}
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
	status       *Status
	statusErr    string
	metrics      *DashboardStats
	hasMetrics   bool
	torrents     []Torrent
	hasTorrents  bool
	logs         []string
	hasLogs      bool
	archive      []ArchiveEntry
	hasArchive   bool
	archiveTotal int
	archivePages int
	archivePage  int
	missing      []Gap
	hasMissing   bool
	blocklist    []BlocklistEntry
	hasBlocklist bool
	health       *Health
	healthErr    string
}

// fetchModel polls the daemon with a short timeout. It is safe to call from a
// goroutine because it never touches the model.
func fetchModel(ctx context.Context, client *Client, includeLogs bool, tab Tab, archiveQuery string, archivePage, logLimit int) refreshResult {
	callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	var result refreshResult
	if status, err := client.Status(callCtx); err == nil {
		result.status = &status
	} else if ctx.Err() == nil {
		result.statusErr = err.Error()
	}
	if tab == TabStatus {
		if stats, err := client.Stats(callCtx); err == nil {
			result.metrics = &stats
			result.hasMetrics = true
		}
	}
	if tab == TabTorrents {
		if torrents, err := client.Torrents(callCtx); err == nil {
			result.torrents = torrents
			result.hasTorrents = true
		}
	}
	if tab == TabLogs && includeLogs {
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
	if result.hasMetrics && result.metrics != nil {
		m.SetMetrics(*result.metrics)
	}
	if result.hasTorrents {
		m.SetTorrents(result.torrents)
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
			if seconds, err := strconv.ParseUint(action.Text, 10, 64); err == nil {
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
