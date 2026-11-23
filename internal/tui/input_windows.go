//go:build windows

package tui

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// signalState groups the process signals the main loop listens to. Windows has
// no SIGHUP or SIGWINCH: Ctrl-C and SIGTERM stop the TUI, and a console resize
// is picked up by comparing the terminal size at every draw instead.
type signalState struct {
	ctx     context.Context
	stop    context.CancelFunc
	resize  <-chan os.Signal
	cleanup func()
}

func watchSignals(ctx context.Context) signalState {
	signalCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	return signalState{
		ctx:     signalCtx,
		stop:    stop,
		resize:  nil,
		cleanup: func() {},
	}
}

// readInput forwards console input. x/term puts the Windows console in a mode
// where a read returns as soon as a key is available, so no poll(2) is needed.
func readInput(ctx context.Context, in *os.File, ch chan<- []byte) {
	defer close(ch)
	buffer := make([]byte, 4096)
	for ctx.Err() == nil {
		count, err := in.Read(buffer)
		if count > 0 {
			data := make([]byte, count)
			copy(data, buffer[:count])
			select {
			case ch <- data:
			case <-ctx.Done():
				return
			}
		}
		if err != nil {
			return
		}
	}
}
