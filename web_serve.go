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
	register("temp_cleanup_worker", func() { tempCleanupWorker(state) })
	register("watched_folders_worker", func() { watchedFoldersWorker(state) })
	register("housekeeping_worker", func() { housekeepingWorker(state) })
	register("media_info_backfill_worker", func() { mediaInfoBackfillWorker(state) })
	register("calendar_warmup_worker", func() { calendarWarmupWorker(state) })
	register("flare_solverr_sweeper_worker", func() { flareSolverrSweeperWorker(state) })
	register("db_checkpoint_worker", func() { dbCheckpointWorker(state) })
	register("health_monitor_worker", func() { healthMonitorWorker(state) })
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

// trackOperation registers a long-lived operation that may touch the torrent
// engine, so stopBackgroundWorkers waits for it before the native session is
// destroyed. Callers must invoke the returned function exactly once when the
// operation completes. The Add happens synchronously in the caller (a handler
// or an already-tracked worker), which is always before bgWG.Wait starts, so it
// cannot race the shutdown wait.
func (s *AppState) trackOperation() func() {
	s.bgWG.Add(1)
	return s.bgWG.Done
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

// workerStopTimeout bounds how long shutdown waits for the background workers.
// Some work cannot be interrupted midway (a season pack being copied to the
// library); without a bound, systemd killed the daemon after TimeoutStopSec and
// the torrent resume data was never saved. Together with sessionDrainTimeout
// and the resume save it stays below the unit's 90 s stop timeout.
var workerStopTimeout = 45 * time.Second

// stopBackgroundWorkers signals every worker to stop and waits for them, up to
// workerStopTimeout. Continuing after the timeout is safe: the libtorrent
// client refuses new native calls once its shutdown starts (enterSession), and
// library copies are atomic, so an interrupted import leaves only a hidden
// temporary file and is completed by the recovery scan after the next start.
func stopBackgroundWorkers(state *AppState) {
	state.bgStopOnce.Do(func() {
		if state.bgCancel != nil {
			state.bgCancel()
		}
		close(state.bgStop)
	})
	done := make(chan struct{})
	go func() {
		state.bgWG.Wait()
		if state.jobs != nil {
			// Cancel and wait for any operation still tracked by the job manager.
			state.jobs.Close()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(workerStopTimeout):
		logging.Warn(fmt.Sprintf("⚠️ Some work was still running after %s (for example a copy to the library); Gextto stops anyway and finishes it after the next start",
			logDuration(workerStopTimeout)))
	}
}

func shutdownServers(servers ...*http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, server := range servers {
		_ = server.Shutdown(ctx)
	}
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
	logging.Info(fmt.Sprintf("🚀 Gextto started — web interface at http://%s · %s", webAddr, mode))
	logging.Debug("startup details", "engine_api", "http://"+engineAddr, "libtorrent", LibtorrentVersion())

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

	app := Router(state)
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
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
		shutdownServers(webServer, engineServer)
		stopBackgroundWorkers(state)
		return err
	case <-ctx.Done():
		cancelRequests()
		shutdownServers(webServer, engineServer)
		stopBackgroundWorkers(state)
		return nil
	}
}
