package gextto

// gxtorrent_runtime.go starts and supervises the gx-torrent daemon when
// `gxtorrent_managed` is on: the binary installed next to gexttod (or the one
// in `gxtorrent_binary`, or on PATH) runs with its state in
// DATA_DIR/gx-torrent and the Gextto download directory as default save path.

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

type gxManagedProcess struct {
	cmd    *exec.Cmd
	mu     sync.Mutex
	closed bool
	done   chan error
}

// gxTorrentBinary finds the daemon executable.
func gxTorrentBinary(settings gxTorrentSettings) (string, error) {
	if settings.Binary != "" {
		if fileExists(settings.Binary) {
			return settings.Binary, nil
		}
		return "", fmt.Errorf("gxtorrent_binary %q not found", settings.Binary)
	}
	if self, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(self), "gx-torrent")
		if fileExists(candidate) {
			return candidate, nil
		}
	}
	if path, err := exec.LookPath("gx-torrent"); err == nil {
		return path, nil
	}
	return "", errors.New("gx-torrent binary not found: build it with `make gx-torrent` (it is installed next to gexttod) or set gxtorrent_binary")
}

// gxManagedListen derives the daemon's listen address from gxtorrent_url,
// which must point at this machine.
func gxManagedListen(baseURL string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Hostname() == "" {
		return "", errors.New("gxtorrent_url is not valid for a managed daemon")
	}
	if parsed.Scheme != "http" {
		return "", errors.New("the managed gx-torrent speaks plain HTTP on loopback: use an http:// URL")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return "", errors.New("the managed gx-torrent must listen on localhost/loopback")
	}
	port := parsed.Port()
	if port == "" {
		port = "80"
	}
	return net.JoinHostPort(host, port), nil
}

func startManagedGxTorrent(cfg *Config, settings gxTorrentSettings) (*gxManagedProcess, error) {
	binary, err := gxTorrentBinary(settings)
	if err != nil {
		return nil, err
	}
	listen, err := gxManagedListen(settings.BaseURL)
	if err != nil {
		return nil, err
	}
	dataDir := filepath.Join(cfg.DataDir, "gx-torrent")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	args := []string{"-listen", listen, "-data", dataDir}
	if dir := strings.TrimSpace(cfg.LibtorrentDir); dir != "" {
		if absolute, absErr := filepath.Abs(dir); absErr == nil {
			args = append(args, "-download-dir", absolute)
		}
	}
	command := exec.Command(binary, args...)
	command.Dir = dataDir
	command.Env = os.Environ()
	if settings.Token != "" {
		// Passed through the environment, not argv, so it never shows in ps.
		command.Env = append(command.Env, "GX_TORRENT_TOKEN="+settings.Token)
	}
	logFile, err := os.OpenFile(filepath.Join(dataDir, "gx-torrent.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("avvio gx-torrent gestito: %w", err)
	}
	logging.Info("gx-torrent avviato da Gextto", "binary", binary, "listen", listen, "data", dataDir)
	process := &gxManagedProcess{cmd: command, done: make(chan error, 1)}
	go func() {
		defer recoverGoroutine("gx-torrent process wait")
		err := command.Wait()
		_ = logFile.Close()
		process.done <- err
		close(process.done)
	}()
	return process, nil
}

func (p *gxManagedProcess) wait() error {
	if p == nil || p.done == nil {
		return nil
	}
	err, ok := <-p.done
	if !ok {
		return nil
	}
	return err
}

// Close asks the daemon to stop (SIGTERM) and waits for it so rain can save
// its resume data; it is killed after 20 seconds.
func (p *gxManagedProcess) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.closed || p.cmd.Process == nil {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = p.cmd.Process.Kill()
		return err
	}
	select {
	case <-p.done:
	case <-time.After(20 * time.Second):
		_ = p.cmd.Process.Kill()
	}
	return nil
}

// superviseManagedProcess restarts the managed daemon after an unexpected
// exit. After three crashes in ten minutes Gextto switches back to the
// embedded libtorrent engine and restarts the service.
func (e *gxTorrentEngine) superviseManagedProcess() {
	defer recoverGoroutine("gx-torrent supervisor")
	const (
		crashWindow  = 10 * time.Minute
		maxCrashes   = 3
		restartPause = 5 * time.Second
	)
	var crashes []time.Time
	for {
		e.processMu.Lock()
		process := e.process
		closed := e.closed
		e.processMu.Unlock()
		if closed || process == nil {
			return
		}
		err := process.wait()
		e.processMu.Lock()
		closed = e.closed
		e.processMu.Unlock()
		if closed {
			return
		}
		exit := "uscita inattesa"
		if err != nil {
			exit = err.Error()
		}
		now := time.Now()
		logging.Error("gx-torrent gestito terminato in modo inatteso", "error", exit, "restarts_in_window", len(crashes)+1)
		crashes = append(crashes, now)
		kept := crashes[:0]
		for _, at := range crashes {
			if now.Sub(at) <= crashWindow {
				kept = append(kept, at)
			}
		}
		crashes = kept
		if len(crashes) >= maxCrashes {
			logging.Error("gx-torrent non riesce a restare attivo: passo al motore libtorrent e riavvio il servizio",
				"crashes", len(crashes), "window_minutes", int(crashWindow.Minutes()))
			if saveErr := SaveSetting(e.settings.dataDir, "torrent_backend", BackendEmbedded); saveErr != nil {
				logging.Error("impossibile impostare torrent_backend=embedded", "error", saveErr)
			}
			requestServiceActionLater("restart")
			return
		}
		select {
		case <-e.supervisorStop:
			return
		case <-time.After(restartPause):
		}
		restarted, startErr := startManagedGxTorrent(e.cfg, e.settings)
		if startErr != nil {
			logging.Error("riavvio di gx-torrent gestito non riuscito", "error", startErr)
			continue
		}
		e.processMu.Lock()
		e.process = restarted
		closed = e.closed
		e.processMu.Unlock()
		if closed {
			_ = restarted.Close()
			return
		}
		logging.Info("gx-torrent gestito riavviato dopo un'uscita inattesa", "restart", len(crashes))
	}
}
