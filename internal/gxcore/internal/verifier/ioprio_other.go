//go:build !linux

package verifier

// lowerIOPriority has no per-thread I/O priority to set on this platform; the
// check still paces itself on slow reads.
func lowerIOPriority() {}
