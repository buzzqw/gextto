// Package upstream is the uTP (BEP 29) implementation vendored from
// github.com/anacrolix/utp v0.2.0, byte-identical apart from the package
// name. It stays under its original licence, Mozilla Public License 2.0
// (see LICENSE in this directory); only this directory is MPL-covered,
// the rest of Gextto remains EUPL-1.2.
//
// The vendoring exists so the transport can be tuned and instrumented
// (LEDBAT parameters, backlog policy, counters) without an external
// dependency: the engine talks to internal/utp, never here directly.
// Upstream fixes are pulled by re-copying the files and re-applying the
// local patches listed below (each marked with "GEXTTO:").
//
// Local patches: none yet.
package upstream
