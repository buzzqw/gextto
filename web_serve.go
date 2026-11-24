package gextto

// Web server startup: the Go implementation of `serve` .

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// nodelayListener disables Nagle on every accepted connection: without it the
// first response of every new connection waits for delayed-ACK (~40 ms) on
// loopback.
type nodelayListener struct{ net.Listener }

func (l nodelayListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		if err := tcp.SetNoDelay(true); err != nil {
			logging.Debug("failed to set TCP_NODELAY", "error", err)
		}
	}
	return conn, nil
}

// startBackgroundWorkers launches the long-lived workers that `serve` starts in
// . They are implemented by the handler-group files. Every worker is tracked in
// state.bgWG so stopBackgroundWorkers can wait for them before the torrent
// session is torn down.
func startBackgroundWorkers(state *AppState) {
	register := func(name string, run func()) {
		state.bgWG.Add(1)
		go func() {
			defer state.bgWG.Done()
			safeGoLoop(name, state.bgStop, run)
		}()
	}
	register("torrent_event_worker", func() {
		torrentEventWorker(state.config_path, state.cfg, state, state.db, state.comics, state.torrent_events)
	})
	register("cycle_worker", func() { cycleWorker(state) })
	register("backup_worker", func() { backupWorker(state) })
	register("optimize_worker", func() { optimizeWorker(state) })
	register("ipfilter_refresh_worker", func() { ipFilterRefreshWorker(state) })
	register("temp_cleanup_worker", func() { tempCleanupWorker(state) })
	register("watched_folders_worker", func() { watchedFoldersWorker(state) })
	register("housekeeping_worker", func() { housekeepingWorker(state) })
	register("media_info_backfill_worker", func() { mediaInfoBackfillWorker(state) })
	register("media_integrity_worker", func() { mediaIntegrityWorker(state) })
	register("night_maintenance_worker", func() { nightMaintenanceWorker(state) })
	register("calendar_warmup_worker", func() { calendarWarmupWorker(state) })
	register("flare_solverr_sweeper_worker", func() { flareSolverrSweeperWorker(state) })
	register("db_checkpoint_worker", func() { dbCheckpointWorker(state) })
	register("health_monitor_worker", func() { healthMonitorWorker(state) })
	register("cycle_monitor_worker", func() { cycleHealthWorker(state) })
	register("provider_login_worker", func() { providerLoginWorker(state) })
	register("update_check_worker", func() { updateCheckWorker(state) })
	register("bandwidth_history_worker", func() { bandwidthHistoryWorker(state) })
	register("latency_guard_worker", func() { latencyGuardWorker(state) })
}

// BackgroundStop is closed as soon as the daemon begins shutting down. Workers
// select on it so they return before the torrent session is destroyed.
func (s *AppState) BackgroundStop() <-chan struct{} { return s.bgStop }

// BackgroundContext returns a context that is cancelled when the daemon starts
// shutting down. Long operations that touch the torrent engine (the acquisition
// cycle, manual cycles) take it so they abort before the session is destroyed.
func (s *AppState) BackgroundContext() (context.Context, context.CancelFunc) {
	if s == nil || s.bgContext == nil {
		return context.WithCancel(context.Background())
	}
	return context.WithCancel(s.bgContext)
}

// operationStartMu orders trackOperation against the start of bgWG.Wait. A
// WaitGroup must not gain counters while Wait is running: handlers are drained
// first, but a job or a worker can begin one last operation as shutdown starts.
var operationStartMu sync.Mutex

// trackOperation registers a long-lived operation that may touch the torrent
// engine or the databases, so stopBackgroundWorkers waits for it before they are
// torn down. Callers must invoke the returned function exactly once when the
// operation completes. Once shutdown has begun no new operation is registered;
// callers must observe their cancelled context and return without starting work.
func (s *AppState) trackOperation() func() {
	if s == nil {
		return func() {}
	}
	operationStartMu.Lock()
	defer operationStartMu.Unlock()
	if s.stopping() {
		return func() {}
	}
	s.bgWG.Add(1)
	return s.bgWG.Done
}

// backgroundRootContext is the long-lived root shared by operations which
// outlive the worker tick that starts them. Unlike BackgroundContext it has no
// per-caller cancel function: the root is cancelled once by shutdown.
func (s *AppState) backgroundRootContext() context.Context {
	if s == nil || s.bgContext == nil {
		return context.Background()
	}
	return s.bgContext
}

// stopping reports whether a shutdown has been requested.
func (s *AppState) stopping() bool {
	select {
	case <-s.bgStop:
		return true
	default:
		return false
	}
}

// SleepBackground sleeps for d and returns false when a shutdown was requested
// meanwhile. Workers use it instead of time.Sleep so they can exit promptly.
func (s *AppState) SleepBackground(d time.Duration) bool {
	if d <= 0 {
		d = time.Millisecond
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-s.bgStop:
		return false
	case <-timer.C:
		return true
	}
}

// workerStopTimeout bounds how long shutdown waits for background work that
// is not a file operation (a stuck network call, a slow scan). It never cuts a
// copy or move of media files: those are always waited for, however long they
// take (see mediaFileOps), and the service unit has no stop timeout.
var workerStopTimeout = 45 * time.Second

// shutdownNoticeInterval is how often a long shutdown wait says what it is
// waiting for, so a stop that takes minutes is visibly progressing.
var shutdownNoticeInterval = time.Minute

// stopBackgroundWorkers signals every worker to stop and waits for them.
// Copies and moves of media files in progress are always awaited to the end;
// other work gets workerStopTimeout. Before giving up on that other work the
// file registry is closed, so a worker cannot start a new copy that the exit
// would cut halfway.
func stopBackgroundWorkers(state *AppState) {
	if state == nil {
		return
	}
	state.bgStopOnce.Do(func() {
		if state.bgCancel != nil {
			state.bgCancel()
		}
		if state.bgStop != nil {
			close(state.bgStop)
		}
	})
	// No operation may Add to bgWG after this point. trackOperation checks
	// bgStop under the same mutex and refuses new work once shutdown began.
	operationStartMu.Lock()
	operationStartMu.Unlock()

	// Jobs are not members of bgWG. Cancel them immediately and in parallel
	// with the workers: waiting for workers first let a queued/running job keep
	// using the engine and databases throughout the whole worker drain.
	jobsDone := make(chan struct{})
	go func() {
		if state.jobs != nil {
			state.jobs.Close()
		}
		close(jobsDone)
	}()
	workersDone := make(chan struct{})
	go func() {
		state.bgWG.Wait()
		close(workersDone)
	}()

	started := time.Now()
	var lastNotice time.Time
	jobsStopped, workersStopped := false, false
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-jobsDone:
			jobsStopped = true
			jobsDone = nil
		case <-workersDone:
			workersStopped = true
			workersDone = nil
		case <-ticker.C:
		}
		if jobsStopped && workersStopped {
			mediaFileOps.closeIfIdle()
			return
		}
		if operations := mediaFileOps.running(); len(operations) > 0 {
			if time.Since(lastNotice) >= shutdownNoticeInterval {
				names := make([]string, len(operations))
				for i, operation := range operations {
					names[i] = operation.what
				}
				// The elapsed time is what tells a stop that is progressing
				// from one that is stuck: a 10 GB copy over the network is
				// expected to take a while, and without it the same sentence
				// repeated every 15 seconds says nothing new.
				logging.Info(fmt.Sprintf("⏳ Stopping: waiting for %d file %s to finish copying so nothing is left half-copied: %s (started %s ago)",
					len(names), pluralWord(len(names), "copy", "copies"), quotedList(names, 3),
					logDuration(time.Since(operations[0].started))))
				lastNotice = time.Now()
			}
			continue
		}
		if time.Since(started) >= workerStopTimeout && mediaFileOps.closeIfIdle() {
			// "No file is being copied" alone contradicted the line just
			// above it; name the work that is still running, when the jobs
			// manager knows it.
			remaining := ""
			if state.jobs != nil {
				if running := state.jobs.List("", JobRunning); len(running) > 0 {
					kinds := make([]string, 0, len(running))
					for _, job := range running {
						if kind := jobKindText(job.Kind); kind != "" {
							kinds = append(kinds, kind)
						}
					}
					remaining = quotedList(kinds, 3)
				}
			}
			if remaining == "" {
				logging.Warn(fmt.Sprintf("⚠️ The file copies are finished, but some background work was still running after %s: Gextto stops anyway",
					logDuration(workerStopTimeout)))
			} else {
				logging.Warn(fmt.Sprintf("⚠️ The file copies are finished, but this work was still running after %s: %s. Gextto stops anyway",
					logDuration(workerStopTimeout), remaining))
			}
			// Jobs obey the JobManager cancellation contract. Their Close was
			// started immediately, but never close the databases while a job is
			// still returning from one.
			if !jobsStopped {
				<-jobsDone
			}
			return
		}
	}
}

// jobKindText names a background job the way the interface does, not by its
// internal kind ("media-integrity").
func jobKindText(kind string) string {
	switch kind = strings.TrimSpace(kind); kind {
	case mediaIntegrityJobKind:
		return "media integrity check"
	case "orphan-cleanup":
		return "cleanup of leftover download files"
	case "rename-all":
		return "renaming of the library"
	case "scan-archives":
		return "library scan"
	case "missing-search":
		return "search for missing episodes"
	case "missing-download":
		return "download of the best missing episodes"
	case "media-info-backfill":
		return "reading of the real quality of library files"
	default:
		return strings.ReplaceAll(kind, "-", " ")
	}
}

// quotedList renders up to limit names as «a», «b» and N more.
func quotedList(names []string, limit int) string {
	if limit < 0 {
		limit = 0
	}
	shown := names
	if len(shown) > limit {
		shown = shown[:limit]
	}
	capSize := len(shown)
	if capSize < len(names) && capSize < 1000 {
		capSize++
	}
	parts := make([]string, 0, capSize)
	for _, name := range shown {
		parts = append(parts, "«"+name+"»")
	}
	if extra := len(names) - len(shown); extra > 0 {
		parts = append(parts, fmt.Sprintf("+%d", extra))
	}
	return strings.Join(parts, ", ")
}

func pluralWord(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

// serverShutdownTimeout bounds the graceful HTTP drain. After it the listeners
// and active connections are closed forcibly; handler completion is still
// awaited before Serve returns and the caller may close its databases.
var serverShutdownTimeout = 10 * time.Second

// httpHandlerTracker is the barrier between the HTTP servers and resource
// teardown. Server.Shutdown waits for connections, not for handlers which ignore
// their request context, and Server.Close does not wait for handlers at all.
// Closing the gate rejects new handlers before Shutdown starts, so Wait cannot
// race with a late Add.
type httpHandlerTracker struct {
	mu     sync.Mutex
	closed bool
	active int
	done   chan struct{}
}

func newHTTPHandlerTracker() *httpHandlerTracker {
	return &httpHandlerTracker{done: make(chan struct{})}
}

func (t *httpHandlerTracker) wrap(next http.Handler) http.Handler {
	if t == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			http.Error(w, "server is shutting down", http.StatusServiceUnavailable)
			return
		}
		t.active++
		t.mu.Unlock()
		defer func() {
			t.mu.Lock()
			t.active--
			if t.closed && t.active == 0 {
				close(t.done)
			}
			t.mu.Unlock()
		}()
		next.ServeHTTP(w, r)
	})
}

func (t *httpHandlerTracker) stopAccepting() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if !t.closed {
		t.closed = true
		if t.active == 0 {
			close(t.done)
		}
	}
	t.mu.Unlock()
}

func (t *httpHandlerTracker) wait() {
	if t != nil {
		<-t.done
	}
}

// shutdownServers drains every server under one shared deadline and force-closes
// it afterwards. Errors from both Shutdown and Close are returned to the caller.
func shutdownServers(servers ...*http.Server) error {
	return shutdownServersWithHandlers(nil, servers...)
}

// shutdownServersWithHandlers is shutdownServers with the database-safety
// barrier used by Serve. The final handler wait is deliberately unbounded:
// returning while a handler still uses SQLite would let the caller close the
// database underneath it. Cancelling request contexts and closing connections
// first lets ordinary handlers finish promptly.
func shutdownServersWithHandlers(handlers *httpHandlerTracker, servers ...*http.Server) error {
	if handlers != nil {
		handlers.stopAccepting()
	}
	ctx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
	defer cancel()
	var wg sync.WaitGroup
	errs := make([]error, len(servers))
	for index, server := range servers {
		if server == nil {
			continue
		}
		wg.Add(1)
		go func(index int, server *http.Server) {
			defer wg.Done()
			errs[index] = server.Shutdown(ctx)
			// Shutdown returning (successfully or with the deadline error) does
			// not close connections which are still executing a handler.
			if closeErr := server.Close(); closeErr != nil {
				errs[index] = errors.Join(errs[index], closeErr)
			}
		}(index, server)
	}
	wg.Wait()
	handlers.wait()
	return errors.Join(errs...)
}

// ListenConflicts reports the configured listen addresses that are already in
// use by another process, so the daemon can warn only when there is a real
// conflict (a legacy extto/rextto, or a previous Gextto) instead of doing it
// unconditionally at every start. It binds and immediately releases each
// address; a non-EADDRINUSE error is ignored here because Serve will surface it
// with a proper message.
func ListenConflicts(cfg *Config) []string {
	if cfg == nil {
		return nil
	}
	conflicts := []string{}
	seen := map[string]struct{}{}
	for _, address := range []string{cfg.Listen, cfg.EngineListen} {
		address = strings.TrimSpace(address)
		if address == "" {
			continue
		}
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		listener, err := net.Listen("tcp", address)
		if err != nil {
			if errors.Is(err, syscall.EADDRINUSE) {
				conflicts = append(conflicts, address)
			}
			continue
		}
		_ = listener.Close()
	}
	return conflicts
}

// Serve binds the web and engine listeners, starts the background workers and
// blocks until the process receives SIGINT/SIGTERM, then shuts down gracefully.
func Serve(state *AppState) error {
	webAddr := state.cfg.Listen
	engineAddr := state.cfg.EngineListen

	webListener, err := net.Listen("tcp", webAddr)
	if err != nil {
		return fmt.Errorf("bind web listener %s: %w", webAddr, err)
	}
	engineListener, err := net.Listen("tcp", engineAddr)
	if err != nil {
		_ = webListener.Close()
		return fmt.Errorf("bind engine listener %s: %w", engineAddr, err)
	}

	mode := "downloads are paused (stand-by)"
	if state.cfg.DryRun {
		mode = "test mode: nothing is really downloaded (dry-run)"
	} else if state.cfg.Active {
		mode = "downloads are enabled"
	}
	engineName := "-"
	if state.hasTorrentEngine() {
		active := state.activeEngine()
		engineName = active.Name()
		// What the engine did at start-up (an adopted daemon keeps its
		// transfers) belongs to the start line, not to a line of its own.
		if noter, ok := active.(interface{ StartupNote() string }); ok {
			if note := strings.TrimSpace(noter.StartupNote()); note != "" {
				engineName += " " + note
			}
		}
	}
	// The first search after a start resumes the previous schedule: say it
	// in the start line, so the cycle worker does not add a line of its own.
	nextSearch := ""
	if cfg := latestConfig(state); cfg.Active {
		lastAt, haveLast := state.db.LastCycleAt()
		if due, ok := resumedCycleDue(true, cfg.RefreshSecs, lastAt, haveLast, time.Now()); ok {
			nextSearch = nextSearchNote(time.Now(), due)
			state.announcedNextSearch.Store(due.Unix())
		}
	}
	prevRun := logStartupBanner(state.cfg.DataDir, startupDetails{WebAddr: webAddr, Mode: mode, Engine: engineName, NextSearch: nextSearch})
	notifyRestart(state, prevRun)
	logging.Debug("build details", "identity", buildIdentity(), "engine api", "http://"+engineAddr)

	// Before the workers: the first cycle marks downloads missing from the
	// session as lost, so add back those an unclean stop dropped.
	restoreMissingTorrents(latestConfig(state), state.db, state.activeEngine())
	startBackgroundWorkers(state)

	// Cancel request contexts before closing the native torrent session. This is
	// important for long-lived SSE connections: http.Server.Shutdown waits for
	// active handlers, but an SSE handler may otherwise remain open until the
	// shutdown timeout and race with libtorrent teardown.
	requestCtx, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()

	// The cross-origin guard runs outermost so a cross-site write is refused
	// before it can reach the (often login-free from the LAN) API. Compression
	// sits above everything: the interface is often used over a weak link.
	handlers := newHTTPHandlerTracker()
	app := handlers.wrap(compressResponses(crossOriginGuard(AuthMiddleware(state, Router(state)))))
	// Timeouts defend against slowloris and idle-connection exhaustion. Write is
	// intentionally unlimited: the log/notification SSE streams and long
	// downloads must be able to stay open.
	newServer := func() *http.Server {
		return &http.Server{
			Handler:           app,
			BaseContext:       func(net.Listener) context.Context { return requestCtx },
			ReadHeaderTimeout: 15 * time.Second,
			ReadTimeout:       5 * time.Minute,
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    1 << 20,
		}
	}
	webServer := newServer()
	engineServer := newServer()

	// SIGHUP (the terminal that started Gextto closed) stops it the same
	// clean way as SIGTERM: without it the process died at once, skipping
	// the database checkpoint and the shutdown record.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	errCh := make(chan error, 2)
	go func() {
		if err := webServer.Serve(nodelayListener{webListener}); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	go func() {
		if err := engineServer.Serve(nodelayListener{engineListener}); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		cancelRequests()
		shutdownErr := shutdownServersWithHandlers(handlers, webServer, engineServer)
		stopBackgroundWorkers(state)
		if shutdownErr != nil {
			logging.Warn(fmt.Sprintf("HTTP servers did not shut down cleanly: %v", shutdownErr))
			err = errors.Join(err, shutdownErr)
		}
		recordShutdown(state.cfg.DataDir, "fatal error: "+err.Error())
		notifyDaemonError(state, err)
		return err
	case <-ctx.Done():
		cancelRequests()
		shutdownErr := shutdownServersWithHandlers(handlers, webServer, engineServer)
		stopBackgroundWorkers(state)
		if shutdownErr != nil {
			logging.Warn(fmt.Sprintf("HTTP servers did not shut down cleanly: %v", shutdownErr))
			recordShutdown(state.cfg.DataDir, "shutdown error: "+shutdownErr.Error())
			return shutdownErr
		}
		recordShutdown(state.cfg.DataDir, "")
		return nil
	}
}
