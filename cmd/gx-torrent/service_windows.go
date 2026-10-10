//go:build windows

package main

import (
	"errors"
	"net/http"

	"golang.org/x/sys/windows/svc"
)

// isWindowsService reports whether this process was started by the Windows
// Service Control Manager.
func isWindowsService() bool {
	is, err := svc.IsWindowsService()
	return err == nil && is
}

// runWindowsService runs the daemon under the Windows service dispatcher: the
// same startDaemon used interactively, driven by the SCM's start/stop requests.
func runWindowsService(opts Options) error {
	return svc.Run("gx-torrent", &gxServiceHandler{opts: opts})
}

type gxServiceHandler struct {
	opts Options
}

var _ svc.Handler = (*gxServiceHandler)(nil)

// Execute implements svc.Handler: it starts the daemon, reports Running and
// stops it on a Stop/Shutdown request from the Service Control Manager.
func (h *gxServiceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	run, err := startDaemon(h.opts)
	if err != nil {
		logf("service cannot start: %v", err)
		return false, 1
	}
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				status <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				run.shutdown()
				return false, 0
			}
		case err := <-run.serverErr:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				logf("service server failed: %v", err)
				run.shutdown()
				return false, 1
			}
			run.shutdown()
			return false, 0
		}
	}
}
