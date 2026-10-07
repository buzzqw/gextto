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
// `gxtorrent_listen` chooses the interface (e.g. "0.0.0.0:8890" to expose the
// web page and the API on the LAN); its port is always aligned with the port of
// `gxtorrent_url`, because Gextto reaches the daemon there, so a mismatched
// port would make the managed daemon unreachable. When `gxtorrent_listen` is
// empty the daemon listens on the loopback address of `gxtorrent_url`.
func gxManagedListen(settings gxTorrentSettings) (string, error) {
	parsed, err := url.Parse(settings.BaseURL)
	if err != nil || parsed.Hostname() == "" {
		return "", errors.New("gxtorrent_url is not valid for a managed daemon")
	}
	if parsed.Scheme != "http" {
		return "", errors.New("the managed gx-torrent speaks plain HTTP on loopback: use an http:// URL")
	}
	urlPort := parsed.Port()
	if urlPort == "" {
		urlPort = "80"
	}
	if listen := strings.TrimSpace(settings.Listen); listen != "" {
		host, port, splitErr := net.SplitHostPort(listen)
		if splitErr != nil || strings.TrimSpace(port) == "" {
			return "", fmt.Errorf("gxtorrent_listen must be host:port, got %q", listen)
		}
		if port != urlPort {
			logging.Warn("gxtorrent_listen porta diversa da gxtorrent_url: uso la porta dell'URL per restare raggiungibile",
				"listen_port", port, "url_port", urlPort)
			port = urlPort
		}
		return net.JoinHostPort(strings.TrimSpace(host), port), nil
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return "", errors.New("the managed gx-torrent must listen on localhost/loopback (set gxtorrent_listen to expose it on the LAN)")
	}
	return net.JoinHostPort(host, urlPort), nil
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
	// arguments, so gxNetworkArgs passes it with -ipfilter. A supervisor restart
	// reuses the cached file; the service-boot refresh is done by the caller.
	gxEnsureIPFilter(cfg, false)
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

// managedShutdownGrace is how long the managed daemon gets to exit after
// SIGTERM. rain flushes its resume data and closes the storage cleanly on a
// graceful stop, so a move or a verify in progress is not truncated; only after
// this window is the process killed.
const managedShutdownGrace = 45 * time.Second

// Close asks the daemon to stop (SIGTERM) and waits for it so rain can save
// its resume data; it is killed after managedShutdownGrace.
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
	case <-time.After(managedShutdownGrace):
		_ = p.cmd.Process.Kill()
	}
	return nil
}

// gxCrashBudget records the failed life cycles of the managed daemon and
// decides when Gextto must stop restarting it. Only events inside the window
// count, so a daemon that stays up long enough earns back its retries. It is a
// pure helper so the policy is unit-testable without spawning processes.
type gxCrashBudget struct {
	window time.Duration
	max    int
	events []time.Time
}

// register adds one failure and reports whether the restart budget is spent.
func (b *gxCrashBudget) register(now time.Time) bool {
	b.events = append(b.events, now)
	kept := b.events[:0]
	for _, at := range b.events {
		if now.Sub(at) <= b.window {
			kept = append(kept, at)
		}
	}
	b.events = kept
	return len(b.events) >= b.max
}

// len reports how many failures are currently inside the window.
func (b *gxCrashBudget) len() int { return len(b.events) }

// superviseManagedProcess restarts the managed daemon after an unexpected exit
// or a failed start. It gives up (and hands the transfers back to embedded
// libtorrent, saving the setting and restarting the service) once the crash
// budget is exhausted: the daemon is not recovering on its own.
func (e *gxTorrentEngine) superviseManagedProcess() {
	defer recoverGoroutine("gx-torrent supervisor")
	const restartPause = 5 * time.Second
	budget := gxCrashBudget{window: 10 * time.Minute, max: 6}
	fallbackToLibtorrent := func() {
		logging.Error("gx-torrent non riesce a restare attivo: passo al motore libtorrent e riavvio il servizio",
			"crashes", budget.len(), "window_minutes", int(budget.window.Minutes()))
		if saveErr := SaveSetting(e.settings.dataDir, "torrent_backend", BackendEmbedded); saveErr != nil {
			logging.Error("impossibile impostare torrent_backend=embedded", "error", saveErr)
		}
		requestServiceActionLater("restart")
	}
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
		logging.Error("gx-torrent gestito terminato in modo inatteso", "error", exit, "restarts_in_window", budget.len()+1)
		if budget.register(time.Now()) {
			fallbackToLibtorrent()
			return
		}
		select {
		case <-e.supervisorStop:
			return
		case <-time.After(restartPause):
		}
		// Keep retrying the start without re-waiting on the old, already
		// reaped process: a daemon that cannot even be started is a failed
		// recovery too, and counts toward the fallback.
		var restarted *gxManagedProcess
		for {
			var startErr error
			restarted, startErr = startManagedGxTorrent(e.cfg, e.settings)
			if startErr == nil {
				break
			}
			logging.Error("riavvio di gx-torrent gestito non riuscito", "error", startErr, "restarts_in_window", budget.len()+1)
			if budget.register(time.Now()) {
				fallbackToLibtorrent()
				return
			}
			select {
			case <-e.supervisorStop:
				return
			case <-time.After(restartPause):
			}
		}
		e.processMu.Lock()
		e.process = restarted
		closed = e.closed
		e.processMu.Unlock()
		if closed {
			_ = restarted.Close()
			return
		}
		logging.Info("gx-torrent gestito riavviato dopo un'uscita inattesa", "restart", budget.len())
	}
}

// gxListenInterface splits libtorrent's listen_interfaces ("0.0.0.0:6881-6891",
// "wg0:6881", "[::]:6881") into host and port range. rain binds a single host,
// so the first specific interface wins; wildcard entries and the SSL suffix are
// dropped. Multiple entries no longer discard a usable host behind the first.
func gxListenInterface(value string) (host, ports string) {
	for _, raw := range strings.Split(value, ",") {
		entry := strings.TrimSpace(raw)
		if entry == "" || entry == "::" || entry == "*" || entry == "0.0.0.0" {
			continue
		}
		entryHost, entryPorts := splitListenEntry(entry)
		if host == "" && entryHost != "" && entryHost != "0.0.0.0" && entryHost != "::" {
			host = entryHost
		}
		if ports == "" && entryPorts != "" {
			ports = entryPorts
		}
	}
	return host, ports
}

// splitListenEntry parses one `host:port` (IPv6 in brackets) listen entry.
func splitListenEntry(entry string) (host, ports string) {
	if strings.HasPrefix(entry, "[") {
		if end := strings.Index(entry, "]"); end >= 0 {
			host = entry[1:end]
			ports = strings.TrimPrefix(entry[end+1:], ":")
			return host, strings.TrimSuffix(ports, "s")
		}
	}
	colon := strings.LastIndex(entry, ":")
	if colon < 0 {
		return entry, ""
	}
	host = strings.Trim(entry[:colon], "[]")
	ports = strings.TrimSpace(entry[colon+1:])
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
	if bootstrap := strings.TrimSpace(lt.DhtBootstrapNodes); bootstrap != "" && lt.Dht {
		args = append(args, "-dht-bootstrap", bootstrap)
	}
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
// (gzip/zip decoded); a local path is left to gxIPFilterPath. `force` downloads
// even when the cached file is recent: it is used at service boot, so the daemon
// always starts with a fresh list; a later supervisor restart reuses the file.
func gxEnsureIPFilter(cfg *Config, force bool) {
	if cfg == nil {
		return
	}
	target := strings.TrimSpace(cfg.Libtorrent.IpFilterPath)
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return
	}
	path := filepath.Join(cfg.DataDir, "ipfilter.dat")
	if !force {
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) < 24*time.Hour {
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
