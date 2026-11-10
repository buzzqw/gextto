package verifier

import "golang.org/x/sys/unix"

// ioprioWhoProcess with a thread id addresses that single thread (ioprio_set(2)).
const ioprioWhoProcess = 1

// bestEffortLowest is class 2 (best effort), level 7: the lowest priority
// that still gets the disk while other programs use it. The idle class would
// leave a check waiting for as long as anything else reads, which on a
// machine that streams all evening is never. Only the BFQ scheduler (and
// mq-deadline in part) honours it; "none", the usual NVMe choice, ignores it,
// which is why the check also paces itself.
const bestEffortLowest = 2<<13 | 7

// lowerIOPriority lowers the I/O priority of the calling thread. A failure
// (an old kernel, a seccomp filter) leaves the default priority.
func lowerIOPriority() {
	_, _, _ = unix.Syscall(unix.SYS_IOPRIO_SET, ioprioWhoProcess, uintptr(unix.Gettid()), bestEffortLowest)
}
