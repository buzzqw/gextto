// Package upstream is Gextto's own uTP (BEP 29) implementation. It lives in
// this repository under EUPL-1.2 and is written against the anacrolix/utp
// v0.2.0 reference, whose observable behaviour it matches; the code is our own,
// so the transport can be tuned and instrumented freely. The engine talks to
// internal/utp, never here directly.
//
// A Socket multiplexes one packet connection and hands each uTP packet to the
// Conn it belongs to; a Conn is a uTP stream and implements net.Conn. Every
// Conn and Socket state change is serialised by the package mutex mu.
//
// Improvements over the reference:
//   - a floor on the sampled RTT (minLatency) and a loss-driven per-connection
//     RTO backoff (resendTimeout, noteRTOTimeout, noteDelivery);
//   - per-socket retransmission counters (ResendStats) and backlog SYN drops;
//   - a per-remote RTT seed for fresh connections tied to the dial race;
//   - a total, length-checked header parser that rejects short or malformed
//     packets instead of slicing out of range (FuzzHeaderUnmarshal);
//   - own support primitives (Event, waitEvents, jitterDuration,
//     stoppedFuncTimer), so no third-party helper is linked.
package upstream
