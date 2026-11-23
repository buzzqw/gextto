//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly || aix || solaris || illumos

package tui

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

// signalState groups the process signals the main loop listens to. Windows has
// no SIGHUP or SIGWINCH, so each platform supplies its own watcher.
type signalState struct {
	ctx     context.Context
	stop    context.CancelFunc
	resize  <-chan os.Signal
	cleanup func()
}

func watchSignals(ctx context.Context) signalState {
	signalCtx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	resizeCh := make(chan os.Signal, 1)
	signal.Notify(resizeCh, syscall.SIGWINCH)
	return signalState{
		ctx:     signalCtx,
		stop:    stop,
		resize:  resizeCh,
		cleanup: func() { signal.Stop(resizeCh) },
	}
}

// readInput forwards terminal input and closes ch when the terminal is gone,
// so the main loop can quit instead of spinning on a dead descriptor.
func readInput(ctx context.Context, in *os.File, ch chan<- []byte) {
	defer close(ch)
	fd := int(in.Fd())
	buffer := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		// Poll with a short timeout instead of trusting VMIN/VTIME: a blocking
		// terminal read can hang forever (and ignore keys) on some setups.
		pollFDs := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		ready, err := unix.Poll(pollFDs, 100)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return
		}
		if ready == 0 {
			continue
		}
		if pollFDs[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 && pollFDs[0].Revents&unix.POLLIN == 0 {
			return
		}
		count, readErr := unix.Read(fd, buffer)
		if count > 0 {
			data := make([]byte, count)
			copy(data, buffer[:count])
			select {
			case ch <- data:
			case <-ctx.Done():
				return
			}
			continue
		}
		if readErr == unix.EAGAIN || readErr == unix.EINTR {
			continue
		}
		// count == 0 without error is end of file; anything else is fatal.
		return
	}
}
