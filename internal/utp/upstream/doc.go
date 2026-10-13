// Package upstream is the uTP (BEP 29) implementation vendored from
// github.com/anacrolix/utp v0.2.0: upstream files are kept as-is apart from
// the package name and the local patches listed below. It stays under its
// original licence, Mozilla Public License 2.0
// (see LICENSE in this directory); only this directory is MPL-covered,
// the rest of Gextto remains EUPL-1.2.
//
// The vendoring exists so the transport can be tuned and instrumented
// (LEDBAT parameters, backlog policy, counters) without an external
// dependency: the engine talks to internal/utp, never here directly.
// Upstream fixes are pulled by re-copying the files and re-applying the
// local patches listed below (each marked with "GEXTTO:").
//
// Local patches (each marked "GEXTTO:" at the site):
//   - gextto_support.go: own Event/waitEvents/jitterDuration/stoppedFuncTimer,
//     replacing github.com/anacrolix/missinggo and the anacrolix/sync mutex,
//     so no third-party helper remains;
//   - socket.go: per-socket timeout/fast resend counters + ResendStats,
//     initial-RTO seed hook (SetInitialLatencyEstimator/initialLatencyFor);
//   - send.go: timeoutResend feeds the socket counter;
//   - conn.go: ackSkipped fast resend feeds the socket counter; latency()
//     seeds from the estimator when unsampled (clamped [5ms, 400ms]) and floors
//     sampled RTT at minLatency (resolves the vendored minimum-latency TODO);
//     resendTimeout applies the loss-driven adaptive backoff and a delivered
//     packet relaxes it (noteRTOTimeout/noteDelivery, cap maxRTOBackoff);
//   - header.go: Unmarshal rejects <20B inputs (hostile peers) instead of
//     slicing out of range (proven by FuzzHeaderUnmarshal seed#0 on the
//     pristine code).
//   - socket.go: pushBacklog evictions counted (synDrops/SynDrops).
package upstream
