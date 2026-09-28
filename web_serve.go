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
// . They are implemented by the handler-group files.
func startBackgroundWorkers(state *AppState) {
	safeGo("torrent_event_worker", func() {
		torrentEventWorker(state.config_path, state.cfg, state, state.db, state.comics, state.torrent_events)
	})
	safeGo("cycle_worker", func() { cycleWorker(state) })
	safeGo("backup_worker", func() { backupWorker(state) })
	safeGo("optimize_worker", func() { optimizeWorker(state) })
	safeGo("temp_cleanup_worker", func() { tempCleanupWorker(state) })
	safeGo("watched_folders_worker", func() { watchedFoldersWorker(state) })
	safeGo("housekeeping_worker", func() { housekeepingWorker(state) })
	safeGo("media_info_backfill_worker", func() { mediaInfoBackfillWorker(state) })
	safeGo("calendar_warmup_worker", func() { calendarWarmupWorker(state) })
	safeGo("db_checkpoint_worker", func() { dbCheckpointWorker(state) })
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

	mode := "stand-by (downloads paused)"
	if state.cfg.DryRun {
		mode = "dry-run (no real downloads)"
	} else if state.cfg.Active {
		mode = "active (downloads enabled)"
	}
	logging.Info(fmt.Sprintf(
		"🚀 Gextto started · UI http://%s · engine http://%s · libtorrent %s · %s",
		webAddr, engineAddr, LibtorrentVersion(), mode,
	))

	startBackgroundWorkers(state)

	app := Router(state)
	// Timeouts defend against slowloris and idle-connection exhaustion. Write is
	// intentionally unlimited: the log/notification SSE streams and long
	// downloads must be able to stay open.
	newServer := func() *http.Server {
		return &http.Server{
			Handler:           app,
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
		shutdownServers(webServer, engineServer)
		return err
	case <-ctx.Done():
		shutdownServers(webServer, engineServer)
		return nil
	}
}
