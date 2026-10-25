package gextto

// provider_monitor.go watches the forum login of the mircrew-indexer service.
// That service keeps answering Torznab requests from its local index while the
// forum login is broken: searches return results, the cycle stays clean, and the
// `indexer_failing` notice (which needs two failed `.torrent` links in one
// cycle) never fires, so a login can stay broken for hours without anyone
// noticing. The monitor probes the service's /status (no API key, no side
// effects) and notifies only after the login has been broken continuously for
// providerLoginGrace, so a service restart or a one-off retry is not news.
//
// Like health_monitor.go and cycle_monitor.go it logs the transition and keeps
// the notification for a persistent problem: one notice when the grace expires
// (then at most every providerLoginNotifyInterval) and one when the login works
// again.

import (
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

const (
	providerMonitorFirstPass = 2 * time.Minute
	providerMonitorPeriod    = 5 * time.Minute
	// providerLoginGrace is how long the login may stay broken before the first
	// notification: the service retries with a backoff, and a login that fixes
	// itself in a few minutes must not wake anyone.
	providerLoginGrace = 15 * time.Minute
	// providerLoginNotifyInterval is the minimum gap between two notices for a
	// login that stays broken, so a forgotten service cannot flood the chat.
	providerLoginNotifyInterval = 12 * time.Hour
)

// providerLoginState folds consecutive probes into the action to take. failing
// since is kept across a service that stops answering: an unreachable /status
// is neither a failure nor a recovery, so the caller leaves the state alone.
type providerLoginState struct {
	failingSince time.Time
	notifiedAt   time.Time
	reason       string
}

// reset forgets the current failure (login healthy, or no service to watch).
func (s *providerLoginState) reset() {
	s.failingSince = time.Time{}
	s.notifiedAt = time.Time{}
	s.reason = ""
}

// observe takes the probe outcome and returns "failed" when a notification is
// due, "recovered" when a notified failure is over, or "" when nothing changed.
func (s *providerLoginState) observe(failed bool, reason string, now time.Time) string {
	if !failed {
		notified := !s.notifiedAt.IsZero()
		s.reset()
		if notified {
			return "recovered"
		}
		return ""
	}
	if s.failingSince.IsZero() {
		s.failingSince = now
	}
	s.reason = reason
	if now.Sub(s.failingSince) < providerLoginGrace {
		return ""
	}
	if s.notifiedAt.IsZero() || now.Sub(s.notifiedAt) >= providerLoginNotifyInterval {
		s.notifiedAt = now
		return "failed"
	}
	return ""
}

// providerLoginReason renders the login error answered by the service in
// English: the log is read by the operator, and the service happens to answer
// in Italian. A message we do not know is not guessed: the caller logs the
// generic sentence and keeps the original at debug level.
func providerLoginReason(raw string) string {
	lowered := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case lowered == "":
		return ""
	case strings.Contains(lowered, "credenziali rifiutate") ||
		strings.Contains(lowered, "verifica richiesta") ||
		strings.Contains(lowered, "credenziali non valide"):
		return "credentials rejected or verification required"
	case strings.Contains(lowered, "sessione non autenticata") ||
		strings.Contains(lowered, "sessione scaduta") ||
		strings.Contains(lowered, "non autenticat"):
		return "session not authenticated"
	}
	return ""
}

// providerLoginWorker probes the mircrew-indexer login periodically and
// notifies when it has been broken for longer than the grace period.
func providerLoginWorker(state *AppState) {
	if !state.SleepBackground(providerMonitorFirstPass) {
		return
	}
	var loginState providerLoginState
	for {
		cfg := latestConfig(state)
		if indexer := gh5_mirCrewIndexer(cfg); indexer == nil {
			// No service to watch: drop any pending failure, silently.
			loginState.reset()
		} else {
			reason, failed, reachable := gh5_mirCrewLoginProbe(gh5_providerURL(cfg, indexer.Name))
			if reachable {
				if reason == "" && failed {
					reason = "sessione non autenticata"
				}
				if failed {
					if english := providerLoginReason(reason); english != "" {
						reason = english
					} else {
						logging.Debug("provider login detail", "source", indexer.Name, "reason", reason)
						reason = "login failed (details at debug level)"
					}
				}
				switch loginState.observe(failed, reason, time.Now()) {
				case "failed":
					logging.Warn("provider login failing: check the credentials in the service",
						"source", indexer.Name, "reason", reason)
					if state.notifier != nil {
						_ = state.notifier.NotifyEvent("provider_login_failed", map[string]any{
							"source": indexer.Name,
							"error":  reason,
						})
					}
				case "recovered":
					logging.Info("provider login recovered", "source", indexer.Name)
					if state.notifier != nil {
						_ = state.notifier.NotifyEvent("provider_login_recovered", map[string]any{
							"source": indexer.Name,
						})
					}
				}
			}
		}
		if !state.SleepBackground(providerMonitorPeriod) {
			return
		}
	}
}
