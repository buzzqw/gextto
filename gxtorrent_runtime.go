package gextto

// gxtorrent_runtime.go starts and supervises the gx-torrent daemon when
// `gxtorrent_managed` is on: the binary installed next to gexttod (make and
// install.sh keep it there; the PATH is the fallback) runs with its state in
// DATA_DIR/gx-torrent and the Gextto download directory as default save path.

import (
	"context"
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

// gxTorrentBinary finds the daemon executable: the one installed next to
// gexttod (make and install.sh keep it there), then the PATH.
func gxTorrentBinary() (string, error) {
	if self, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(self), "gx-torrent")
		if fileExists(candidate) {
			return candidate, nil
		}
	}
	if path, err := exec.LookPath("gx-torrent"); err == nil {
		return path, nil
	}
	return "", errors.New("gx-torrent binary not found: build it with `make gx-torrent` (it is installed next to gexttod)")
}

// gxManagedListen returns the address the managed daemon must listen on.
// `gxtorrent_listen` wins (e.g. "0.0.0.0:8890" to expose the read-only page on
// the LAN); when it is empty the daemon listens on the loopback address of
// `gxtorrent_url`, so only Gextto reaches it.
func gxManagedListen(settings gxTorrentSettings) (string, error) {
	if listen := strings.TrimSpace(settings.Listen); listen != "" {
		host, port, err := net.SplitHostPort(listen)
		if err != nil || strings.TrimSpace(port) == "" {
			return "", fmt.Errorf("gxtorrent_listen must be host:port, got %q", listen)
		}
		return net.JoinHostPort(host, port), nil
	}
	parsed, err := url.Parse(settings.BaseURL)
	if err != nil || parsed.Hostname() == "" {
		return "", errors.New("gxtorrent_url is not valid for a managed daemon")
	}
	if parsed.Scheme != "http" {
		return "", errors.New("the managed gx-torrent speaks plain HTTP on loopback: use an http:// URL")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return "", errors.New("the managed gx-torrent must listen on localhost/loopback (set gxtorrent_listen to expose it on the LAN)")
	}
	port := parsed.Port()
	if port == "" {
		port = "80"
	}
	return net.JoinHostPort(host, port), nil
}

// listenIsLoopback reports whether an address is reachable only from this host.
// An empty host (all interfaces) is not loopback.
func listenIsLoopback(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	host = strings.TrimSpace(host)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func startManagedGxTorrent(cfg *Config, settings gxTorrentSettings) (*gxManagedProcess, error) {
	binary, err := gxTorrentBinary()
	if err != nil {
		return nil, err
	}
	listen, err := gxManagedListen(settings)
	if err != nil {
		return nil, err
	}
	dataDir := filepath.Join(cfg.DataDir, "gx-torrent")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	args := []string{"-listen", listen, "-data", dataDir}
	if !listenIsLoopback(listen) && settings.Token == "" {
		// The daemon refuses a non-loopback listen without a token. Instead of
		// failing to start we opt into its documented `-insecure` mode, matching
		// Gextto's trusted-LAN default, and say so loudly: the REST API is then
		// reachable on the whole network.
		args = append(args, "-insecure")
		logging.Warn("gx-torrent in ascolto sulla rete senza token: l'API è raggiungibile da chiunque sulla LAN — imposta gxtorrent_token per proteggerla", "listen", listen)
	}
	if dir := strings.TrimSpace(cfg.LibtorrentDir); dir != "" {
		if absolute, absErr := filepath.Abs(dir); absErr == nil {
			args = append(args, "-download-dir", absolute)
		}
	}
	// Make sure the configured IP filter is on disk before building the network
	// arguments, so gxNetworkArgs passes it with -ipfilter.
	gxEnsureIPFilter(cfg)
	args = append(args, gxNetworkArgs(cfg)...)
	command := exec.Command(binary, args...)
	command.Dir = dataDir
	command.Env = os.Environ()
	if settings.Token != "" {
		// Passed through the environment, not argv, so it never shows in ps.
		command.Env = append(command.Env, "GX_TORRENT_TOKEN="+settings.Token)
	}
	if proxy := strings.TrimSpace(cfg.Settings["gxtorrent_proxy"]); proxy != "" {
		// May carry credentials: environment, not argv.
		command.Env = append(command.Env, "GX_TORRENT_PROXY="+proxy)
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

// gxListenInterface splits libtorrent's listen_interfaces ("0.0.0.0:6881-6891",
// "wg0:6881", first entry only) into host and port range.
func gxListenInterface(value string) (host, ports string) {
	entry := strings.TrimSpace(strings.Split(value, ",")[0])
	if entry == "" {
		return "", ""
	}
	colon := strings.LastIndex(entry, ":")
	if colon < 0 {
		return entry, ""
	}
	host = strings.Trim(entry[:colon], "[]")
	ports = strings.TrimSpace(entry[colon+1:])
	if host == "0.0.0.0" || host == "::" || host == "*" {
		host = ""
	}
	return host, strings.TrimSuffix(ports, "s")
}

// gxNetworkArgs translates Gextto's libtorrent network settings into
// gx-torrent flags.
func gxNetworkArgs(cfg *Config) []string {
	lt := cfg.Libtorrent
	ports := ""
	if lt.PortMin > 0 {
		ports = fmt.Sprintf("%d-%d", lt.PortMin, max(lt.PortMin, lt.PortMax))
	}
	host, listenPorts := gxListenInterface(lt.ListenInterfaces)
	if listenPorts != "" {
		ports = listenPorts
	}
	var args []string
	if ports != "" {
		args = append(args, "-peer-ports", ports)
	}
	if host != "" {
		args = append(args, "-listen-interface", host)
	}
	if iface := strings.TrimSpace(lt.OutgoingInterface); iface != "" {
		args = append(args, "-outgoing-interface", iface)
	}
	encryption := lt.Encryption
	if encryption < 0 || encryption > 2 {
		encryption = 1
	}
	args = append(args, "-encryption", fmt.Sprint(encryption))
	if !lt.Dht {
		args = append(args, "-no-dht")
	}
	if !lt.Pex {
		args = append(args, "-no-pex")
	}
	if !lt.Utp {
		args = append(args, "-no-utp")
	}
	if !lt.Lsd {
		args = append(args, "-no-lsd")
	}
	if !lt.Upnp {
		args = append(args, "-no-upnp")
	}
	if !lt.Natpmp {
		args = append(args, "-no-natpmp")
	}
	if path := gxIPFilterPath(cfg); path != "" {
		args = append(args, "-ipfilter", path)
	}
	args = append(args, fmt.Sprintf("-ipfilter-trackers=%t", lt.ApplyIpFilter))
	return args
}

// gxIPFilterPath is the local IP filter file: the configured path, or the
// copy Gextto downloads from a configured URL.
func gxIPFilterPath(cfg *Config) string {
	target := strings.TrimSpace(cfg.Libtorrent.IpFilterPath)
	if target == "" {
		return ""
	}
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		target = filepath.Join(cfg.DataDir, "ipfilter.dat")
	}
	if !fileExists(target) {
		return ""
	}
	return target
}

// gxEnsureIPFilter makes sure the configured IP filter is stored locally before
// the managed daemon starts, so it is passed with -ipfilter. A URL is downloaded
// (gzip/zip decoded); a local path is left to gxIPFilterPath. An already fresh
// file is reused so a restart does not re-download the list every time.
func gxEnsureIPFilter(cfg *Config) {
	if cfg == nil {
		return
	}
	target := strings.TrimSpace(cfg.Libtorrent.IpFilterPath)
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return
	}
	path := filepath.Join(cfg.DataDir, "ipfilter.dat")
	autoupdate := settingsBool(cfg, "libtorrent_ipfilter_autoupdate", true)
	if info, err := os.Stat(path); err == nil {
		if !autoupdate || time.Since(info.ModTime()) < 24*time.Hour {
			return
		}
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		logging.Warn("cannot create the data directory for the IP filter", "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	data, errMessage := gh7_fetch_ipfilter(ctx, target)
	if errMessage != "" {
		logging.Warn("IP filter download failed; keeping the previous list", "url", target, "error", errMessage)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		logging.Warn("cannot store the IP filter", "path", path, "error", err)
		return
	}
	logging.Info("IP filter updated", "path", path, "bytes", len(data))
}
