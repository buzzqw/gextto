package gextto

// gxtorrent_runtime.go starts and supervises the gx-torrent daemon when that
// engine is active: the binary installed next to gexttod (make and
// install.sh keep it there; the PATH is the fallback) runs with its state in
// DATA_DIR/gx-torrent and the Gextto download directory as default save path.
// An external daemon already answering on the URL is used as-is.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/messages"
)

// gxManagedProcess is the managed daemon: one Gextto started (cmd) or one it
// found running from a previous Gextto run and adopted (pid only).
type gxManagedProcess struct {
	cmd    *exec.Cmd
	pid    int
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

// gxOrphanTimeout is how long a daemon kept running across Gextto restarts
// waits for Gextto to come back before stopping by itself.
const gxOrphanTimeout = 15 * time.Minute

// gxManagedCommand is everything needed to start the managed daemon.
type gxManagedCommand struct {
	binary      string
	args        []string
	env         []string
	dataDir     string
	listen      string
	fingerprint string
}

// buildManagedGxCommand prepares the daemon command line. The fingerprint
// covers the binary content and every option, so a running daemon is reused
// only when it is exactly the one this Gextto would start.
func buildManagedGxCommand(cfg *Config, settings gxTorrentSettings) (gxManagedCommand, error) {
	binary, err := gxTorrentBinary()
	if err != nil {
		return gxManagedCommand{}, err
	}
	listen, err := gxManagedListen(settings)
	if err != nil {
		return gxManagedCommand{}, err
	}
	dataDir := filepath.Join(cfg.DataDir, "gx-torrent")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return gxManagedCommand{}, err
	}
	args := []string{"-listen", listen, "-data", dataDir, "-lang", messages.Language()}
	if !listenIsLoopback(listen) && settings.Token == "" {
		// The daemon refuses a non-loopback listen without a token. Instead of
		// failing to start we opt into its documented `-insecure` mode, matching
		// Gextto's trusted-LAN default: the REST API is then reachable on the
		// whole network (set gxtorrent_token to protect it).
		args = append(args, "-insecure")
		logging.Debug("gx-torrent listens on the network without a token: set gxtorrent_token to protect its API", "listen", listen)
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
	if settings.SafeMode {
		args = append(args, "-no-utp", "-no-holepunch")
	}
	args = append(args,
		"-log-file", filepath.Join(dataDir, "gx-torrent.log"),
		"-gextto-log", filepath.Join(cfg.DataDir, "gextto.log"),
		"-orphan-timeout", gxOrphanTimeout.String(),
	)
	if source := strings.TrimSpace(cfg.Libtorrent.IpFilterPath); source != "" {
		args = append(args, "-ipfilter-source", source)
	}
	env := os.Environ()
	if settings.Token != "" {
		// Passed through the environment, not argv, so it never shows in ps.
		env = append(env, "GX_TORRENT_TOKEN="+settings.Token)
	}
	proxy := strings.TrimSpace(cfg.Settings["gxtorrent_proxy"])
	if proxy != "" {
		// May carry credentials: environment, not argv.
		env = append(env, "GX_TORRENT_PROXY="+proxy)
	}
	fingerprint, err := gxFingerprint(binary, args, settings.Token, proxy)
	if err != nil {
		return gxManagedCommand{}, err
	}
	args = append(args, "-fingerprint", fingerprint)
	return gxManagedCommand{binary: binary, args: args, env: env, dataDir: dataDir, listen: listen, fingerprint: fingerprint}, nil
}

// gxFingerprint hashes the binary content and the options (secrets included
// only as a hash, never in clear).
func gxFingerprint(binary string, args []string, token, proxy string) (string, error) {
	file, err := os.Open(binary)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	for _, part := range append(append([]string{}, args...), "token="+token, "proxy="+proxy) {
		hash.Write([]byte(part))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))[:24], nil
}

var (
	gxScopeOnce      sync.Once
	gxScopeAvailable bool
)

// gxCanUseScope reports whether the daemon can be started in its own systemd
// scope. Under the gextto systemd service this is what lets the daemon survive
// a Gextto restart: systemd stops every process of the service's cgroup, and
// the scope is a cgroup of its own.
func gxCanUseScope() bool {
	gxScopeOnce.Do(func() {
		if os.Getenv("INVOCATION_ID") == "" {
			return // not started by systemd
		}
		path, err := exec.LookPath("systemd-run")
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		probe := exec.CommandContext(ctx, path, "--user", "--scope", "--quiet", "--collect", "true")
		gxScopeAvailable = probe.Run() == nil
		if !gxScopeAvailable {
			logging.Debug("systemd-run --user --scope unavailable: gx-torrent restarts with Gextto")
		}
	})
	return gxScopeAvailable
}

func startManagedGxTorrent(cfg *Config, settings gxTorrentSettings) (*gxManagedProcess, error) {
	spec, err := buildManagedGxCommand(cfg, settings)
	if err != nil {
		return nil, err
	}
	var command *exec.Cmd
	if gxCanUseScope() {
		scope := fmt.Sprintf("gextto-gx-torrent-%d", time.Now().Unix())
		command = exec.Command("systemd-run", append([]string{"--user", "--scope", "--quiet", "--collect", "--unit=" + scope, "--", spec.binary}, spec.args...)...)
	} else {
		command = exec.Command(spec.binary, spec.args...)
	}
	command.Dir = spec.dataDir
	command.Env = spec.env
	// Its own session: a signal to Gextto's process group does not reach it.
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// The daemon writes its own rotating log (-log-file); this file only
	// catches what the Go runtime prints on a crash.
	crashLog, err := os.OpenFile(filepath.Join(spec.dataDir, "gx-torrent.crash.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	command.Stdout = crashLog
	command.Stderr = crashLog
	if err := command.Start(); err != nil {
		crashLog.Close()
		return nil, fmt.Errorf("avvio gx-torrent gestito: %w", err)
	}
	logging.Debug("gx-torrent avviato da Gextto", "binary", spec.binary, "listen", spec.listen, "data", spec.dataDir, "scope", gxCanUseScope())
	process := &gxManagedProcess{cmd: command, pid: command.Process.Pid, done: make(chan error, 1)}
	go func() {
		defer recoverGoroutine("gx-torrent process wait")
		err := command.Wait()
		_ = crashLog.Close()
		process.done <- err
		close(process.done)
	}()
	return process, nil
}

// adoptGxProcess follows a daemon started by a previous Gextto run: it is not
// a child of this process, so its exit is detected by polling the pid.
func adoptGxProcess(pid int) *gxManagedProcess {
	process := &gxManagedProcess{pid: pid, done: make(chan error, 1)}
	go func() {
		defer recoverGoroutine("gx-torrent adopted process watch")
		for gxPidAlive(pid) {
			time.Sleep(2 * time.Second)
		}
		process.done <- errors.New("exited")
		close(process.done)
	}()
	return process
}

func gxPidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
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

// Stop asks the daemon to stop (SIGTERM) and waits for it so rain can save
// its resume data; it is killed after managedShutdownGrace.
func (p *gxManagedProcess) Stop() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.closed || p.pid <= 0 {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()
	if err := syscall.Kill(p.pid, syscall.SIGTERM); err != nil {
		_ = syscall.Kill(p.pid, syscall.SIGKILL)
		return err
	}
	select {
	case <-p.done:
	case <-time.After(managedShutdownGrace):
		_ = syscall.Kill(p.pid, syscall.SIGKILL)
	}
	return nil
}

// Detach leaves the daemon running when Gextto exits: the next Gextto start
// adopts it (same binary and options) instead of restarting every transfer.
func (p *gxManagedProcess) Detach() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
}

// gxStopLeftoverDaemon stops a managed gx-torrent left running by a previous
// Gextto run when another engine is now active: it would hold the peer port
// and keep transferring outside Gextto's control. A daemon without a
// fingerprint was not started by Gextto and is left alone.
func gxStopLeftoverDaemon(cfg *Config) {
	settings, err := gxTorrentSettingsFromConfig(cfg)
	if err != nil {
		return
	}
	probe := &gxTorrentEngine{settings: settings, client: &http.Client{Timeout: 2 * time.Second}}
	probe.settings.Timeout = 2 * time.Second
	health, ok := probe.health()
	if !ok || !health.ownedBy(cfg) || !gxPidAlive(health.Pid) {
		return
	}
	logging.Info(fmt.Sprintf("⏹️ Stopping gx-torrent %s: another torrent engine is now active", health.Version))
	gxStopPid(health.Pid)
}

// gxStopPid stops a daemon known only by its pid and waits for it to exit.
func gxStopPid(pid int) {
	if !gxPidAlive(pid) {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	deadline := time.Now().Add(managedShutdownGrace)
	for time.Now().Before(deadline) {
		if !gxPidAlive(pid) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
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

// notifyEvent sends a lifecycle notification when a notifier is wired. Best
// effort: a failure to notify must never affect supervision.
func (e *gxTorrentEngine) notifyEvent(event string, data map[string]any) {
	if e == nil || e.notifier == nil {
		return
	}
	notifier := e.notifier
	go func() {
		defer recoverGoroutine("gx-torrent notifier")
		if err := notifier.NotifyEvent(event, data); err != nil {
			logging.Warn("gx-torrent notification failed", "event", event, "error", err)
		}
	}()
}

// gxUnstablePauses are the waits before trying gx-torrent again, one per
// exhausted crash budget, on a build without libtorrent.
var gxUnstablePauses = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

// gxUnstablePause returns the wait after the n-th exhausted crash budget
// (n from 0); the last pause repeats.
func gxUnstablePause(n int) time.Duration {
	if n >= len(gxUnstablePauses) {
		n = len(gxUnstablePauses) - 1
	}
	if n < 0 {
		n = 0
	}
	return gxUnstablePauses[n]
}

// gxStableUptime is how long the daemon must stay up before the unstable
// pauses start again from the shortest one.
const gxStableUptime = 30 * time.Minute

// superviseManagedProcess restarts the managed daemon after an unexpected exit
// or a failed start. Once the crash budget is exhausted the daemon is not
// recovering on its own: a build with libtorrent hands the transfers back to
// the embedded engine (saving the setting and restarting the service). A build
// without libtorrent has nothing to fall back to, so it keeps gx-torrent, in
// safe mode and with growing pauses, instead of restarting the service in a
// loop.
func (e *gxTorrentEngine) superviseManagedProcess() {
	defer recoverGoroutine("gx-torrent supervisor")
	const restartPause = 5 * time.Second
	newBudget := func() gxCrashBudget { return gxCrashBudget{window: 10 * time.Minute, max: 6} }
	budget := newBudget()
	startSettings := e.settings
	unstable := 0
	startedAt := time.Now()
	fallbackToLibtorrent := func() {
		logging.Error("gx-torrent non riesce a restare attivo: passo al motore libtorrent e riavvio il servizio",
			"crashes", budget.len(), "window_minutes", int(budget.window.Minutes()))
		e.notifyEvent("engine_fallback", map[string]any{"crashes": budget.len()})
		e.processMu.Lock()
		process := e.process
		e.processMu.Unlock()
		_ = process.Stop()
		if saveErr := SaveSetting(e.settings.dataDir, "torrent_backend", BackendEmbedded); saveErr != nil {
			logging.Error("impossibile impostare torrent_backend=embedded", "error", saveErr)
		}
		requestServiceActionLater("restart")
	}
	// budgetExhausted reacts to an exhausted crash budget and reports whether
	// the supervisor must stop.
	budgetExhausted := func() bool {
		if LibtorrentCompiled() {
			fallbackToLibtorrent()
			return true
		}
		pause := gxUnstablePause(unstable)
		unstable++
		startSettings.SafeMode = true
		logging.Error("gx-torrent non riesce a restare attivo e questa build non include libtorrent: trasferimenti fermi, nuovo tentativo in modalità sicura (senza uTP e holepunch)",
			"crashes", budget.len(), "window_minutes", int(budget.window.Minutes()),
			"retry_in", pause.String(), "crash_log", filepath.Join(e.cfg.DataDir, "gx-torrent", "gx-torrent.crash.log"))
		e.notifyEvent("engine_unstable", map[string]any{
			"crashes":       budget.len(),
			"retry_minutes": int(pause.Minutes()),
		})
		budget = newBudget()
		select {
		case <-e.supervisorStop:
			return true
		case <-time.After(pause):
			return false
		}
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
		replacing := e.replacing
		e.replacing = false
		e.processMu.Unlock()
		if closed {
			return
		}
		if replacing {
			// A deliberate stop to run the updated daemon: start it at once,
			// it is not a crash.
			if restarted, err := startManagedGxTorrent(e.cfg, startSettings); err == nil {
				e.processMu.Lock()
				e.process = restarted
				e.processMu.Unlock()
				startedAt = time.Now()
				continue
			}
		}
		if time.Since(startedAt) >= gxStableUptime {
			unstable = 0
		}
		exit := "uscita inattesa"
		if err != nil {
			exit = err.Error()
		}
		logging.Error("gx-torrent gestito terminato in modo inatteso", "error", exit, "restarts_in_window", budget.len()+1)
		e.notifyEvent("engine_crashed", map[string]any{"error": exit, "restarts_in_window": budget.len() + 1})
		if budget.register(time.Now()) && budgetExhausted() {
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
			restarted, startErr = startManagedGxTorrent(e.cfg, startSettings)
			if startErr == nil {
				break
			}
			logging.Error("riavvio di gx-torrent gestito non riuscito", "error", startErr, "restarts_in_window", budget.len()+1)
			if budget.register(time.Now()) && budgetExhausted() {
				return
			}
			select {
			case <-e.supervisorStop:
				return
			case <-time.After(restartPause):
			}
		}
		startedAt = time.Now()
		e.processMu.Lock()
		e.process = restarted
		closed = e.closed
		e.processMu.Unlock()
		if closed {
			_ = restarted.Stop()
			return
		}
		logging.Info("gx-torrent gestito riavviato dopo un'uscita inattesa", "restart", budget.len())
		e.notifyEvent("engine_restarted", map[string]any{"restarts_in_window": budget.len()})
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
	if !lt.Holepunch {
		args = append(args, "-no-holepunch")
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
func gxEnsureIPFilter(cfg *Config, force bool) bool {
	if cfg == nil {
		return false
	}
	target := strings.TrimSpace(cfg.Libtorrent.IpFilterPath)
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return false
	}
	path := filepath.Join(cfg.DataDir, "ipfilter.dat")
	if !force {
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) < 24*time.Hour {
			return false
		}
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		logging.Warn("cannot create the data directory for the IP filter", "error", err)
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	data, errMessage := gh7_fetch_ipfilter(ctx, target)
	if errMessage != "" {
		logging.Warn("IP filter download failed; keeping the previous list", "url", target, "error", errMessage)
		return false
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		logging.Warn("cannot store the IP filter", "path", path, "error", err)
		return false
	}
	logging.Info("IP filter updated", "path", path, "bytes", len(data))
	return true
}
