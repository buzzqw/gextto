package gextto

// Managed amuled: Gextto writes a private aMule configuration under DATA_DIR,
// generates the External Connections password, starts amuled and keeps it
// alive, like the managed qBittorrent runtime does for torrents. The operator
// no longer has to install aMule by hand: only the `amuled` binary has to be
// available (PATH or amule_binary).

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

const (
	amuleManagedDirName = "amule"
	amuleManagedCheck   = 10 * time.Second
	amuleManagedRetry   = 30 * time.Second
)

func amuleManagedEnabled(cfg *Config) bool {
	return cfg != nil && settingIsTrue(cfg.Settings["amule_managed"])
}

// amuleManagedDir is the private config/working directory of the managed core.
func amuleManagedDir(cfg *Config) string {
	if cfg == nil || strings.TrimSpace(cfg.DataDir) == "" {
		return ""
	}
	return filepath.Join(cfg.DataDir, amuleManagedDirName)
}

// amuleManagedSettings is what the worker compares to notice a change: the
// process is restarted when one of these moves.
type amuleManagedSettings struct {
	Binary, Dir, Password, Host string
	ECPort, ED2KPort, KadPort   int
}

func amuleManagedConfig(cfg *Config) (amuleManagedSettings, error) {
	if cfg == nil {
		return amuleManagedSettings{}, errors.New("configuration unavailable")
	}
	dir := amuleManagedDir(cfg)
	if dir == "" {
		return amuleManagedSettings{}, errors.New("the data directory is not configured")
	}
	binary := strings.TrimSpace(cfg.Settings["amule_binary"])
	if binary == "" {
		binary = "amuled"
	}
	host := strings.TrimSpace(cfg.Settings["amule_host"])
	if host == "" {
		host = "127.0.0.1"
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return amuleManagedSettings{}, errors.New("a managed amuled must listen on loopback (amule_host = 127.0.0.1)")
	}
	ecPort := settingIntRange(cfg.Settings["amule_port"], defaultAmulePort, 1, 65535)
	ed2kPort := settingIntRange(cfg.Settings["amule_ed2k_port"], 4662, 1, 65535)
	kadPort := settingIntRange(cfg.Settings["amule_kad_port"], 4672, 1, 65535)
	password := cfg.Settings["amule_password"]
	return amuleManagedSettings{Binary: binary, Dir: dir, Password: password, Host: host,
		ECPort: ecPort, ED2KPort: ed2kPort, KadPort: kadPort}, nil
}

func settingIntRange(raw string, fallback, min, max int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	n := 0
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n < min || n > max {
		return fallback
	}
	return n
}

// ensureAmuleManagedConfig creates the private directory tree, generates the EC
// password when it is still empty and writes amule.conf. It returns the
// settings to use for the launch.
func ensureAmuleManagedConfig(cfg *Config) (amuleManagedSettings, error) {
	settings, err := amuleManagedConfig(cfg)
	if err != nil {
		return settings, err
	}
	if settings.Password == "" {
		bytes := make([]byte, 18)
		if _, err := rand.Read(bytes); err != nil {
			return settings, fmt.Errorf("cannot generate the amuled password: %w", err)
		}
		settings.Password = hex.EncodeToString(bytes)
		if err := SaveSetting(cfg.DataDir, "amule_password", settings.Password); err != nil {
			return settings, fmt.Errorf("cannot save the amuled password: %w", err)
		}
		cfg.Settings["amule_password"] = settings.Password
	}
	for _, dir := range []string{
		settings.Dir,
		filepath.Join(settings.Dir, "Incoming"),
		filepath.Join(settings.Dir, "Temp"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return settings, err
		}
	}
	// aMule's amule.conf stores ECPassword as an MD5 digest; the protocol defines
	// it and the password is generated locally, not a user credential.
	// lgtm[go/weak-sensitive-data-hashing]
	passwordHash := md5.Sum([]byte(settings.Password))
	// aMule reads a sectioned amule.conf; keys outside a section are ignored
	// and the core appends its defaults (with external connections off), so
	// the managed file must use the same layout as the one aMule writes.
	conf := strings.Join([]string{
		"[eMule]",
		"AppVersion=2.3.3",
		"Nick=Gextto",
		"Port=" + fmt.Sprint(settings.ED2KPort),
		"UDPPort=" + fmt.Sprint(settings.KadPort),
		"TempDir=" + filepath.Join(settings.Dir, "Temp"),
		"IncomingDir=" + filepath.Join(settings.Dir, "Incoming"),
		"Autoconnect=1",
		"ConnectToKad=1",
		"ConnectToED2K=1",
		"OnlineSignature=0",
		"IPFilterAutoLoad=0",
		"[ExternalConnect]",
		"AcceptExternalConnections=1",
		"ECAddress=" + settings.Host,
		"ECPort=" + fmt.Sprint(settings.ECPort),
		"ECPassword=" + hex.EncodeToString(passwordHash[:]),
		"", // trailing newline
	}, "\n")
	if err := os.WriteFile(filepath.Join(settings.Dir, "amule.conf"), []byte(conf), 0o600); err != nil {
		return settings, err
	}
	return settings, nil
}

// managedAmuleProcess is a running amuled child.
type managedAmuleProcess struct {
	cmd     *exec.Cmd
	logFile *os.File
	mu      sync.Mutex
	closed  bool
	done    chan error
}

func startManagedAmule(cfg *Config) (*managedAmuleProcess, error) {
	settings, err := ensureAmuleManagedConfig(cfg)
	if err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(filepath.Join(settings.Dir, "amuled.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	command := exec.Command(settings.Binary, "-c", settings.Dir, "-o")
	command.Dir = settings.Dir
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("cannot start amuled: %w", err)
	}
	process := &managedAmuleProcess{cmd: command, logFile: logFile, done: make(chan error, 1)}
	go func() {
		err := command.Wait()
		_ = logFile.Close()
		process.done <- err
		close(process.done)
	}()
	return process, nil
}

func (p *managedAmuleProcess) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	if p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Signal(os.Interrupt)
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
}

// amuleManagedWorker keeps the managed amuled in step with the settings: it
// starts it when eD2k is on and managed mode is selected, stops it when either
// goes away, restarts it after a crash and reloads it when the settings that
// matter change.
func amuleManagedWorker(state *AppState) {
	var proc *managedAmuleProcess
	var running amuleManagedSettings
	var lastErr string
	var retryAt time.Time
	stop := func() {
		if proc != nil {
			proc.Close()
			proc = nil
			logging.Info("aMule gestito: amuled fermato")
		}
		running = amuleManagedSettings{}
	}
	defer stop()
	for {
		cfg := latestConfig(state)
		if !amuleManagedEnabled(cfg) || !ed2kEnabled(cfg) {
			stop()
			if !state.SleepBackground(amuleManagedCheck) {
				return
			}
			continue
		}
		settings, err := amuleManagedConfig(cfg)
		if err != nil {
			if err.Error() != lastErr {
				logging.Warn("aMule gestito non avviato", "error", err)
				lastErr = err.Error()
			}
			stop()
			if !state.SleepBackground(amuleManagedCheck) {
				return
			}
			continue
		}
		if proc != nil {
			select {
			case err := <-proc.done:
				logging.Warn("aMule gestito: amuled è terminato", "error", err)
				proc = nil
				running = amuleManagedSettings{}
				retryAt = time.Now().Add(amuleManagedRetry)
			default:
			}
		}
		if proc != nil && settings != running {
			stop()
		}
		if proc == nil && !time.Now().Before(retryAt) {
			proc, err = startManagedAmule(cfg)
			if err != nil {
				if err.Error() != lastErr {
					logging.Warn("aMule gestito non avviato", "error", err)
					lastErr = err.Error()
				}
				retryAt = time.Now().Add(amuleManagedRetry)
			} else {
				running = settings
				lastErr = ""
				logging.Info("aMule gestito: amuled avviato", "config", settings.Dir, "ec_port", settings.ECPort)
			}
		}
		if !state.SleepBackground(amuleManagedCheck) {
			return
		}
	}
}
