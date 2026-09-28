package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
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
	// Token is the optional API token. Defaults to GEXTTO_API_TOKEN.
	Token string
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
	snapshot  []string
	line      string
	connected bool
	closed    bool
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
	token := strings.TrimSpace(opts.Token)
	if token == "" {
		token = os.Getenv("GEXTTO_API_TOKEN")
	}
	client := NewClient(base, token)

	daemonLang := ""
	probeCtx, cancelProbe := context.WithTimeout(ctx, 3*time.Second)
	if value, err := client.Language(probeCtx); err == nil {
		daemonLang = value
	}
	cancelProbe()
	model := NewModel(NewTranslator(ResolveLang(opts.Lang, os.Getenv("GEXTTO_LANG"), daemonLang)))

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

	// Refreshes run in a goroutine so a slow or unreachable daemon never blocks
	// the keyboard: the interface always reacts to q / Ctrl-C.
	refreshCh := make(chan refreshResult, 4)
	var fetching int32
	requestRefresh := func() {
		if !atomic.CompareAndSwapInt32(&fetching, 0, 1) {
			return
		}
		go func() {
			result := fetchModel(ctx, client)
			atomic.StoreInt32(&fetching, 0)
			select {
			case refreshCh <- result:
			case <-ctx.Done():
			}
		}()
	}

	parser := &KeyParser{}
	requestRefresh()

	ticker := time.NewTicker(refresh)
	defer ticker.Stop()
	flush := time.NewTicker(50 * time.Millisecond)
	defer flush.Stop()

	quit := false
	for !quit {
		width, height := term.size()
		renderANSIConvert(out, model.Render(width, height))

		select {
		case <-signalCtx.Done():
			quit = true
		case data := <-inputCh:
			for _, key := range parser.Feed(data, false) {
				if execute(ctx, client, model, model.Update(key), requestRefresh) {
					quit = true
					break
				}
			}
		case <-flush.C:
			for _, key := range parser.Feed(nil, true) {
				if execute(ctx, client, model, model.Update(key), requestRefresh) {
					quit = true
					break
				}
			}
		case result := <-refreshCh:
			model.applyRefresh(result)
		case event := <-streamCh:
			applyStream(model, event)
		case <-ticker.C:
			requestRefresh()
		}
		// Drain pending stream events without blocking the next render.
		for draining := true; draining; {
			select {
			case event := <-streamCh:
				applyStream(model, event)
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
	for ctx.Err() == nil {
		ch <- streamEvent{connected: true}
		err := client.StreamLogs(ctx,
			func(lines []string) { sendStream(ctx, ch, streamEvent{snapshot: lines}) },
			func(line string) { sendStream(ctx, ch, streamEvent{line: line}) },
		)
		sendStream(ctx, ch, streamEvent{connected: false, closed: err != nil})
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
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
	case event.snapshot != nil:
		model.SetLogs(event.snapshot)
	case event.line != "":
		model.AppendLog(event.line)
	case event.connected:
		model.SetStreamConnected(true)
	case event.closed:
		model.SetStreamConnected(false)
	}
}

// refreshResult carries one background poll of the daemon.
type refreshResult struct {
	status      *Status
	statusErr   string
	torrents    []Torrent
	hasTorrents bool
	logs        []string
	hasLogs     bool
	health      *Health
	healthErr   string
}

// fetchModel polls the daemon with a short timeout. It is safe to call from a
// goroutine because it never touches the model.
func fetchModel(ctx context.Context, client *Client) refreshResult {
	callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	var result refreshResult
	if status, err := client.Status(callCtx); err == nil {
		result.status = &status
	} else if ctx.Err() == nil {
		result.statusErr = err.Error()
	}
	if torrents, err := client.Torrents(callCtx); err == nil {
		result.torrents = torrents
		result.hasTorrents = true
	}
	if logs, err := client.Logs(callCtx, 200); err == nil {
		result.logs = logs
		result.hasLogs = true
	}
	if health, err := client.Health(callCtx); err == nil {
		result.health = &health
	} else {
		result.healthErr = err.Error()
	}
	return result
}

// applyRefresh merges a poll result into the model on the main goroutine.
func (m *Model) applyRefresh(result refreshResult) {
	if result.status != nil {
		m.SetStatus(*result.status)
		m.SetError("")
	} else if result.statusErr != "" {
		m.SetError(result.statusErr)
	}
	if result.hasTorrents {
		m.SetTorrents(result.torrents)
	}
	if result.hasLogs && !m.LogStreamConnected {
		m.SetLogs(result.logs)
	}
	if result.health != nil {
		m.SetHealth(*result.health)
	} else if result.healthErr != "" {
		m.SetHealthError(result.healthErr)
	}
	m.Loading = false
}

func execute(ctx context.Context, client *Client, model *Model, action Action, refresh func()) (quit bool) {
	if action.Kind == ActionNone {
		return false
	}
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	tr := model.Tr

	switch action.Kind {
	case ActionQuit:
		return true
	case ActionRefresh:
		refresh()
		model.SetMessage(tr.T("msg.refreshed"))
	case ActionRunCycle:
		result, err := client.RunCycle(callCtx, action.Domain)
		if err != nil {
			model.SetMessage(tr.Format("msg.cyclefailed", err))
			break
		}
		queued, _ := result["queued"].(bool)
		switch {
		case queued:
			model.SetMessage(tr.Format("msg.cyclequeued", action.Domain))
		case action.Domain == "" || action.Domain == "full":
			model.SetMessage(tr.T("msg.cyclefull"))
		default:
			model.SetMessage(tr.Format("msg.cyclestarted", action.Domain))
		}
	case ActionSearch:
		results, err := client.Search(callCtx, action.Text)
		if err != nil {
			model.SetMessage(tr.Format("msg.searchfailed", err))
			break
		}
		model.SetSearchResults(action.Text, results)
		model.SetMessage(tr.Format("msg.search", len(results)))
	case ActionAddMagnet:
		if err := client.AddMagnet(callCtx, action.Text); err != nil {
			model.SetMessage(tr.Format("msg.addfailed", err))
			break
		}
		model.SetMessage(tr.T("msg.magneton"))
	case ActionAddFile:
		if err := client.AddTorrentFile(callCtx, action.Text); err != nil {
			model.SetMessage(tr.Format("msg.addfailed", err))
			break
		}
		model.SetMessage(tr.T("msg.torrentadded"))
	case ActionPauseToggle:
		paused := false
		if detail, err := client.TorrentDetail(callCtx, action.Hash); err == nil {
			paused = detail.Torrent.State == "paused"
		}
		var err error
		if paused {
			err = client.Resume(callCtx, action.Hash)
		} else {
			err = client.Pause(callCtx, action.Hash)
		}
		if err != nil {
			model.SetMessage(tr.Format("msg.actionfailed", tr.T("tab.torrents"), err))
			break
		}
		if paused {
			model.SetMessage(tr.T("msg.resumed"))
		} else {
			model.SetMessage(tr.T("msg.paused"))
		}
		refresh()
	case ActionRestart:
		model.SetMessage(tr.T("msg.restarted"))
		if err := client.Restart(callCtx, action.Hash); err != nil {
			model.SetMessage(tr.Format("msg.actionfailed", "restart", err))
		}
		refresh()
	case ActionRemove:
		if err := client.Remove(callCtx, action.Hash, action.DeleteFiles, false); err != nil {
			model.SetMessage(tr.Format("msg.actionfailed", "remove", err))
			break
		}
		if action.DeleteFiles {
			model.SetMessage(tr.T("msg.removedfiles"))
		} else {
			model.SetMessage(tr.T("msg.removed"))
		}
		refresh()
	case ActionRecheck:
		if err := client.Recheck(callCtx, action.Hash); err != nil {
			model.SetMessage(tr.Format("msg.actionfailed", "recheck", err))
			break
		}
		model.SetMessage(tr.T("msg.rechecked"))
	case ActionReannounce:
		if err := client.Reannounce(callCtx, action.Hash); err != nil {
			model.SetMessage(tr.Format("msg.actionfailed", "reannounce", err))
			break
		}
		model.SetMessage(tr.T("msg.reannounced"))
	case ActionNoRenameToggle:
		current := false
		if detail, err := client.TorrentDetail(callCtx, action.Hash); err == nil {
			current = detail.NoRename
		}
		if err := client.SetNoRename(callCtx, action.Hash, !current); err != nil {
			model.SetMessage(tr.Format("msg.actionfailed", "no-rename", err))
			break
		}
		if !current {
			model.SetMessage(tr.T("msg.norenameon"))
		} else {
			model.SetMessage(tr.T("msg.norenameoff"))
		}
	case ActionPin:
		if err := client.Pin(callCtx, action.Hash); err != nil {
			model.SetMessage(tr.Format("msg.actionfailed", "pin", err))
			break
		}
		model.SetMessage(tr.T("msg.pinned"))
	case ActionUnpin:
		if err := client.Unpin(callCtx); err != nil {
			model.SetMessage(tr.Format("msg.actionfailed", "unpin", err))
			break
		}
		model.SetMessage(tr.T("msg.unpinned"))
	case ActionCleanCompleted:
		result, err := client.RemoveCompleted(callCtx, false)
		if err != nil {
			model.SetMessage(tr.Format("msg.actionfailed", "clean", err))
			break
		}
		removed := int(numberValue(result["removed"]))
		skipped := int(numberValue(result["skipped"]))
		model.SetMessage(tr.Format("msg.removedone", removed, skipped))
		refresh()
	case ActionSetLimits:
		if err := client.SetSpeedLimits(callCtx, action.DL, action.UL); err != nil {
			model.SetMessage(tr.Format("msg.actionfailed", "limits", err))
			break
		}
		model.SetMessage(tr.Format("msg.limits", action.DL, action.UL))
	case ActionCleanTrash:
		result, err := client.CleanTrash(callCtx)
		if err != nil {
			model.SetMessage(tr.Format("msg.trashfailed", err))
			break
		}
		if files, ok := result["files"]; ok {
			model.SetMessage(tr.Format("msg.trashcleaned", int(numberValue(files))))
		} else {
			model.SetMessage(tr.T("msg.trashcleanedna"))
		}
		refresh()
	case ActionQueueRelease:
		if err := client.AddRelease(callCtx, action.Release); err != nil {
			model.SetMessage(tr.Format("msg.queuefailed", err))
			break
		}
		model.Overlay = OverlayNone
		model.SetMessage(tr.T("msg.queued"))
		refresh()
	case ActionOpenDetails:
		detail, err := client.TorrentDetail(callCtx, action.Hash)
		if err != nil {
			model.SetMessage(tr.Format("msg.detailsfailed", err))
			break
		}
		model.SetDetail(detail)
		model.SetMessage("")
	case ActionLoadDetail:
		items, err := client.TorrentCollection(callCtx, action.Hash, action.DetailKind)
		if err != nil {
			model.SetMessage(tr.Format("msg.detailfailed", action.DetailKind, err))
			break
		}
		model.SetDetailItems(items)
	case ActionLoadEvents:
		events, err := client.Events(callCtx)
		if err != nil {
			model.SetMessage(tr.Format("msg.eventsfailed", err))
			break
		}
		model.SetEvents(events)
		model.Overlay = OverlayEvents
		model.SetMessage(tr.Format("msg.events", len(events)))
	}
	return false
}
