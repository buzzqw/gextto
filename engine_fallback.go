package gextto

// engine_fallback.go implements the engine-agnostic fallback used when the
// active torrent engine cannot stay up: a crash loop, or an activation refused
// at startup.
//
// The chain therefore does not depend on the removed embedded engine:
//
//  1. the qBittorrent engine — the managed runtime is downloaded on demand from
//     the userdocs/qbittorrent-nox-static releases (the last libtorrent v2
//     build), configured with a private profile and started by Gextto; an
//     operator-configured external qBittorrent is used as is instead;
//  2. recovery: no alternative is available, the transfers stay stopped, the UI
//     stays up and the operator is notified.
//
// Every switch is logged, notified and persisted, so it survives the service
// restart that applies it. A cooldown prevents a restart loop from ping-ponging
// between two engines that both crash: within the window the chain goes
// straight to recovery instead of switching again.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

const (
	// BackendRecovery is not a torrent engine: it is the terminal state of the
	// fallback chain, where no alternative engine can be made to work, the
	// transfers stay stopped and the UI is kept up for the operator.
	BackendRecovery = "recovery"

	// engineFallbackCooldown is how long after a switch the chain refuses to
	// switch again. It breaks the crash-loop ping-pong between two engines.
	engineFallbackCooldown = 30 * time.Minute

	// engineFallbackDownloadTimeout bounds the on-demand qBittorrent download so
	// a stalled network cannot block the supervisor forever.
	engineFallbackDownloadTimeout = 10 * time.Minute

	// defaultManagedQbittorrentURL is the loopback URL used by the managed
	// runtime; a different, non-loopback URL means the operator brought their
	// own qBittorrent.
	defaultManagedQbittorrentURL = "http://127.0.0.1:8080"
)

// engineFallbackPlan is the pure decision of the chain.
type engineFallbackPlan struct {
	// Backend is the target engine (BackendGxTorrent or BackendQbittorrent), or
	// BackendRecovery when nothing is available.
	Backend string
	// Download means the managed qBittorrent runtime must be fetched first.
	Download bool
	// Managed means the switch must enable the managed qBittorrent runtime.
	Managed bool
}

// planEngineFallback chooses the next engine after `current` failed. It never
// touches the network or the filesystem: the caller supplies whether the
// managed qBittorrent runtime is already installed and whether the operator
// brought their own qBittorrent. qBittorrent is the only alternative to
// gx-torrent, which is the reason the chain exists.
func planEngineFallback(current string, qbittorrentInstalled, externalQbittorrent bool) engineFallbackPlan {
	switch {
	case externalQbittorrent && current != BackendQbittorrent:
		// An operator-configured external qBittorrent is already running: use
		// it, nothing to download.
		return engineFallbackPlan{Backend: BackendQbittorrent}
	case current == BackendQbittorrent:
		// qBittorrent is the one that failed: go back to gx-torrent.
		return engineFallbackPlan{Backend: BackendGxTorrent}
	default:
		// gx-torrent failed: the managed qBittorrent, downloaded on demand when
		// it is not installed yet.
		return engineFallbackPlan{Backend: BackendQbittorrent, Download: !qbittorrentInstalled, Managed: true}
	}
}

// engineFallbackSecondary is the engine to use when the primary target cannot be
// prepared (the qBittorrent download failed or its configuration could not be
// written): gx-torrent unless it is the engine that just failed, otherwise
// recovery. It never returns the engine that just failed, to avoid an immediate
// retry.
func engineFallbackSecondary(current string) engineFallbackPlan {
	if current != BackendGxTorrent {
		return engineFallbackPlan{Backend: BackendGxTorrent}
	}
	return engineFallbackPlan{Backend: BackendRecovery}
}

// managedQbittorrentInstalled reports whether the managed qBittorrent binary is
// present with its install metadata.
func managedQbittorrentInstalled(cfg *Config) bool {
	_, ok := qbittorrentReadInstallMetadata(cfg)
	if !ok {
		return false
	}
	binary := qbittorrentManagedBinary(cfg)
	return binary != "" && fileExists(binary)
}

// externalQbittorrentConfigured reports whether the operator points Gextto at a
// qBittorrent it runs itself (managed mode off and a non-loopback URL), so the
// fallback must use it instead of downloading one.
func externalQbittorrentConfigured(cfg *Config) bool {
	if cfg == nil || qbittorrentManagedEnabled(cfg) {
		return false
	}
	raw := strings.TrimSpace(cfg.Settings["qbittorrent_url"])
	if raw == "" || raw == defaultManagedQbittorrentURL {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	return host != "" && !strings.EqualFold(host, "localhost") && !strings.EqualFold(host, "::1") && host != "127.0.0.1"
}

// engineFallbackState records the last switch so the cooldown survives restarts.
type engineFallbackState struct {
	LastAt time.Time `json:"last_at"`
	From   string    `json:"from"`
	To     string    `json:"to"`
}

func engineFallbackStatePath(dataDir string) string {
	return filepath.Join(dataDir, "engine-fallback.json")
}

func readEngineFallbackState(dataDir string) engineFallbackState {
	var state engineFallbackState
	if strings.TrimSpace(dataDir) == "" {
		return state
	}
	raw, err := os.ReadFile(engineFallbackStatePath(dataDir))
	if err != nil {
		return state
	}
	_ = json.Unmarshal(raw, &state)
	return state
}

func writeEngineFallbackState(dataDir string, state engineFallbackState) error {
	if strings.TrimSpace(dataDir) == "" {
		return errors.New("directory dati Gextto non configurata")
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := engineFallbackStatePath(dataDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// engineFallbackWithinCooldown reports whether a switch happened too recently,
// so the chain must not switch again and should go to recovery instead.
func engineFallbackWithinCooldown(dataDir string, now time.Time) bool {
	state := readEngineFallbackState(dataDir)
	if state.LastAt.IsZero() {
		return false
	}
	return now.Sub(state.LastAt) < engineFallbackCooldown
}

// engineFallbackDownload fetches the managed qBittorrent runtime. A variable so
// tests avoid the network.
var engineFallbackDownload = func(ctx context.Context, cfg *Config) error {
	_, err := installQbittorrentRuntime(ctx, cfg)
	return err
}

// engineFallbackRestart applies the persisted switch. A variable so tests do not
// restart the service.
var engineFallbackRestart = requestServiceActionLater

// engineFallbackMu serializes switches inside one process, so two supervisors
// cannot race the settings and the state file.
var engineFallbackMu sync.Mutex

// engineFallbackNotify sends a lifecycle notification when a notifier is wired,
// best effort: a notification failure never affects the switch.
func engineFallbackNotify(notifier *Notifier, event string, data map[string]any) {
	if notifier == nil {
		return
	}
	go func() {
		defer recoverGoroutine("engine fallback notifier")
		if err := notifier.NotifyEvent(event, data); err != nil {
			logging.Warn("engine fallback notification failed", "event", event, "error", err)
		}
	}()
}

// applyEngineFallback runs the chain after `current` failed and persists the
// result. It returns true when it switched the engine (a service restart is
// coming, so the caller must stop). It returns false when no alternative is
// available — within the cooldown, a failed download with no secondary engine,
// or recovery — so the caller keeps the engine in its degraded/safe mode.
func applyEngineFallback(ctx context.Context, cfg *Config, current string, notifier *Notifier) bool {
	if cfg == nil {
		return false
	}
	engineFallbackMu.Lock()
	defer engineFallbackMu.Unlock()

	now := time.Now()
	if engineFallbackWithinCooldown(cfg.DataDir, now) {
		state := readEngineFallbackState(cfg.DataDir)
		logging.Error("fallback motore già avvenuto di recente: niente nuovo cambio, passo in recovery",
			"from", current, "last_fallback", state.LastAt.Format(time.RFC3339), "cooldown", engineFallbackCooldown.String())
		engineFallbackNotify(notifier, "engine_recovery", map[string]any{
			"from": current, "reason": "cooldown", "last_fallback": state.LastAt.Format(time.RFC3339),
		})
		return false
	}

	plan := planEngineFallback(current, managedQbittorrentInstalled(cfg), externalQbittorrentConfigured(cfg))
	if plan.Download {
		downloadCtx, cancel := context.WithTimeout(ctx, engineFallbackDownloadTimeout)
		logging.Warn("fallback motore: scarico qBittorrent-nox (userdocs/qbittorrent-nox-static)", "from", current)
		err := engineFallbackDownload(downloadCtx, cfg)
		cancel()
		if err != nil {
			logging.Error("fallback motore: download qBittorrent-nox non riuscito", "error", err.Error())
			plan = engineFallbackSecondary(current)
			if plan.Backend == BackendRecovery {
				engineFallbackNotify(notifier, "engine_recovery", map[string]any{"from": current, "reason": err.Error()})
				return false
			}
		}
	}
	if plan.Backend == BackendRecovery || plan.Backend == "" {
		engineFallbackNotify(notifier, "engine_recovery", map[string]any{"from": current, "reason": "no alternative engine"})
		return false
	}
	if plan.Backend == BackendQbittorrent && plan.Managed {
		if err := persistManagedQbittorrent(cfg); err != nil {
			logging.Error("fallback motore: configurazione qBittorrent gestito non riuscita", "error", err.Error())
			plan = engineFallbackSecondary(current)
			if plan.Backend == BackendRecovery {
				engineFallbackNotify(notifier, "engine_recovery", map[string]any{"from": current, "reason": err.Error()})
				return false
			}
		}
	}
	if err := SaveSetting(cfg.DataDir, "torrent_backend", plan.Backend); err != nil {
		logging.Error("fallback motore: salvataggio torrent_backend non riuscito", "backend", plan.Backend, "error", err.Error())
		engineFallbackNotify(notifier, "engine_recovery", map[string]any{"from": current, "reason": err.Error()})
		return false
	}
	_ = writeEngineFallbackState(cfg.DataDir, engineFallbackState{LastAt: now, From: current, To: plan.Backend})
	logging.Error("fallback motore: passo a "+plan.Backend+" e riavvio il servizio", "from", current)
	engineFallbackNotify(notifier, "engine_fallback", map[string]any{"from": current, "to": plan.Backend})
	engineFallbackRestart("restart")
	return true
}

// persistManagedQbittorrent stores the managed runtime credentials and URL so
// the qBittorrent adapter starts the downloaded binary on the next boot. The
// generated password is written once; an existing one is preserved.
func persistManagedQbittorrent(cfg *Config) error {
	prepared, err := managedQbittorrentConfig(cfg)
	if err != nil {
		return err
	}
	for _, key := range []string{"qbittorrent_url", "qbittorrent_username", "qbittorrent_password", "qbittorrent_managed"} {
		if value, ok := prepared.Settings[key]; ok {
			if saveErr := SaveSetting(cfg.DataDir, key, value); saveErr != nil {
				return fmt.Errorf("salvataggio %s: %w", key, saveErr)
			}
		}
	}
	return nil
}

// EngineOverrideEnv forces the torrent engine for one run, so a service whose
// saved backend is broken can be recovered from a shell without editing the
// database.
const EngineOverrideEnv = "GEXTTO_ENGINE"

// ApplyEngineOverride rewrites the backend setting from EngineOverrideEnv when
// it names a known engine. It reports the applied backend (empty when nothing
// was overridden). The saved setting is left untouched, so the override lasts
// only for this run: a later start without the variable keeps the stored choice.
func ApplyEngineOverride(cfg *Config) string {
	if cfg == nil {
		return ""
	}
	value := strings.ToLower(strings.TrimSpace(os.Getenv(EngineOverrideEnv)))
	if value == "" {
		return ""
	}
	if !isKnownTorrentBackend(value) {
		logging.Warn("GEXTTO_ENGINE ignorato: motore sconosciuto", "value", value)
		return ""
	}
	if cfg.Settings == nil {
		cfg.Settings = map[string]string{}
	}
	cfg.Settings["torrent_backend"] = value
	return value
}
