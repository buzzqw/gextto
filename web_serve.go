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
		torrentEventWorker(state.config_path, state.cfg, state.torrents, state.db, state.comics, state.torrent_events)
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

	// Warn once when the API is exposed beyond loopback without a token: a
	// default install that binds 0.0.0.0 must know it is unauthenticated.
	if state.cfg.APIToken == nil || strings.TrimSpace(*state.cfg.APIToken) == "" {
		if host, _, err := net.SplitHostPort(webAddr); err == nil {
			switch host {
			case "127.0.0.1", "::1", "localhost":
			default:
				logging.Warn("web UI/API is reachable without an API token; set GEXTTO_API_TOKEN or bind to 127.0.0.1",
					"listen", webAddr)
			}
		}
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
