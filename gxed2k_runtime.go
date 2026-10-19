package gextto

// gxed2k_runtime.go starts and supervises the gx-ed2k search daemon when the
// eD2k/Kad provider is enabled. It follows the gx-torrent pattern: the binary
// installed next to gexttod is preferred, an instance already answering on the
// configured URL is adopted, and the managed process runs in its own systemd
// scope so a Gextto restart does not kill it.
//
// The daemon is a search service. It is not part of the torrent engine
// fallback chain: if it is down, the eD2k provider is simply unavailable and
// torrent downloads continue untouched.

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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/messages"
)

type ed2kManaged struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	pid         int
	fingerprint string
	dataDir     string
	listen      string
	lastError   string
}

var ed2kProcess ed2kManaged
var ed2kDesired atomic.Bool

// ed2kBinary finds the daemon next to gexttod, then on PATH.
func ed2kBinary() (string, error) {
	if self, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(self), "gx-ed2k")
		if fileExists(candidate) {
			return candidate, nil
		}
	}
	if path, err := exec.LookPath("gx-ed2k"); err == nil {
		return path, nil
	}
	return "", errors.New("gx-ed2k binary not found: build it with `make gx-ed2k` (it is installed next to gexttod)")
}

// ed2kListenFromURL returns the local listen address for the managed daemon:
// the URL's host:port when it is loopback, otherwise the managed default. A
// URL pointing at a remote host is an external daemon and is never started.
func ed2kListenFromURL(rawURL string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return "", false
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "8899"
	}
	ip := net.ParseIP(host)
	if strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback()) {
		return net.JoinHostPort(host, port), true
	}
	return "", false
}

// ensureEd2kDaemon makes the provider usable when it can: disabled is a no-op,
// an external daemon is left alone, and a managed one is started if missing.
func ensureEd2kDaemon(cfg *Config) {
	if cfg == nil || !ed2kEnabled(cfg) {
		ed2kDesired.Store(false)
		stopManagedEd2k()
		return
	}
	ed2kDesired.Store(true)
	settings, err := ed2kSettingsFromConfig(cfg)
	if err != nil {
		logging.Warn("gx-ed2k configuration is invalid", "error", err)
		return
	}
	listen, managed := ed2kListenFromURL(settings.BaseURL)
	if !managed {
		// The operator points at a daemon they run themselves.
		return
	}
	health := ed2kHealth(cfg)
	ed2kProcess.mu.Lock()
	ownedData := filepath.Join(cfg.DataDir, "gx-ed2k")
	if health.healthy && health.err == "" {
		ed2kProcess.mu.Unlock()
		return
	}
	if ed2kProcess.cmd != nil {
		ed2kProcess.mu.Unlock()
		return
	}
	ed2kProcess.mu.Unlock()

	binary, err := ed2kBinary()
	if err != nil {
		logging.Warn("gx-ed2k is enabled but not installed", "error", err)
		return
	}
	args := []string{
		"-listen", listen,
		"-data", ownedData,
		"-mode", "managed",
		"-kad-listen", "0.0.0.0:4672",
		"-tcp-port", "4662",
		fmt.Sprintf("-upnp=%t", ed2kUPnPEnabled(cfg)),
		"-search-timeout", strconvDuration(settings.Timeout),
		"-log-file", filepath.Join(ownedData, "gx-ed2k.log"),
		"-lang", messages.Language(),
	}
	if nodesURL := strings.TrimSpace(cfg.Settings["ed2k_nodes_url"]); nodesURL != "" {
		args = append(args, "-nodes-url", nodesURL)
	}
	if serverURL := strings.TrimSpace(cfg.Settings["ed2k_serverlist_url"]); serverURL != "" {
		args = append(args, "-serverlist-url", serverURL)
	}
	if servers := strings.TrimSpace(cfg.Settings["ed2k_servers"]); servers != "" {
		args = append(args, "-servers", servers)
	}
	fingerprint, err := gxFingerprint(binary, args, settings.Token, "")
	if err != nil {
		logging.Warn("cannot fingerprint gx-ed2k", "error", err)
		return
	}
	args = append(args, "-fingerprint", fingerprint)

	if err := os.MkdirAll(ownedData, 0o755); err != nil {
		logging.Warn("cannot create gx-ed2k data dir", "error", err)
		return
	}
	var command *exec.Cmd
	if gxCanUseScope() {
		scope := fmt.Sprintf("gextto-gx-ed2k-%d", time.Now().Unix())
		command = exec.Command("systemd-run", append([]string{
			"--user", "--scope", "--quiet", "--collect",
			"--unit=" + scope, "--", binary,
		}, args...)...)
	} else {
		command = exec.Command(binary, args...)
	}
	command.Dir = ownedData
	command.Env = append(os.Environ(), "GX_ED2K_TOKEN="+settings.Token)
	crashLog, _ := os.OpenFile(filepath.Join(ownedData, "gx-ed2k.crash.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if crashLog != nil {
		command.Stdout = crashLog
		command.Stderr = crashLog
	}
	if err := command.Start(); err != nil {
		logging.Warn("cannot start gx-ed2k", "error", err)
		return
	}

	ed2kProcess.mu.Lock()
	ed2kProcess.cmd = command
	ed2kProcess.pid = command.Process.Pid
	ed2kProcess.fingerprint = fingerprint
	ed2kProcess.dataDir = ownedData
	ed2kProcess.listen = listen
	ed2kProcess.lastError = ""
	ed2kProcess.mu.Unlock()
	logging.Info("gx-ed2k started", "listen", listen, "data_dir", ownedData)

	go func(started *exec.Cmd) {
		err := started.Wait()
		ed2kProcess.mu.Lock()
		if ed2kProcess.cmd == started {
			ed2kProcess.cmd = nil
			ed2kProcess.pid = 0
			if err != nil {
				ed2kProcess.lastError = err.Error()
			}
		}
		ed2kProcess.mu.Unlock()
		if err == nil {
			return
		}
		// The search daemon must not take torrents down with it: log the exit
		// and retry on its own while the provider is still enabled.
		logging.Warn("gx-ed2k exited; retrying in 30 seconds", "error", err)
		go func() {
			time.Sleep(30 * time.Second)
			if ed2kDesired.Load() {
				ensureEd2kDaemon(cfg)
			}
		}()
	}(command)
}

func strconvDuration(value time.Duration) string {
	if value < time.Second {
		value = time.Second
	}
	return value.String()
}

// stopManagedEd2k stops only the daemon Gextto started; an external instance
// is never touched.
func stopManagedEd2k() {
	ed2kProcess.mu.Lock()
	command := ed2kProcess.cmd
	ed2kProcess.mu.Unlock()
	if command == nil || command.Process == nil {
		return
	}
	_ = command.Process.Signal(syscall.SIGTERM)
	logging.Info("gx-ed2k stopped because the eD2k provider was disabled")
}

// Ed2kRuntimeStatus is the operational state shown by diagnostics and the
// settings page: installed, reachable, managed and the last error.
func Ed2kRuntimeStatus(cfg *Config) map[string]any {
	health := ed2kHealth(cfg)
	ed2kProcess.mu.Lock()
	managed := ed2kProcess.cmd != nil
	pid := ed2kProcess.pid
	lastError := ed2kProcess.lastError
	ed2kProcess.mu.Unlock()
	if lastError == "" {
		lastError = health.err
	}
	return map[string]any{
		"enabled":      ed2kEnabled(cfg),
		"configured":   ed2kEnabled(cfg),
		"reachable":    health.healthy,
		"download":     health.download,
		"managed":      managed,
		"pid":          pid,
		"version":      health.version,
		"reachability": health.reachability,
		"low_id":       health.lowID,
		"upnp":         health.upnp,
		"last_error":   lastError,
	}
}
