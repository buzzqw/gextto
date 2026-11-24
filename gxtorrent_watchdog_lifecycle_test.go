package gextto

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sync"
	"testing"
	"time"
)

func startGxWatchdogTestProcess(t *testing.T) (*exec.Cmd, *gxManagedProcess, <-chan error) {
	t.Helper()
	command := exec.Command("sleep", "30")
	if err := command.Start(); err != nil {
		t.Skip("sleep unavailable")
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	process := adoptGxProcess(command.Process.Pid)
	t.Cleanup(func() {
		_ = command.Process.Kill()
		select {
		case <-waited:
		case <-time.After(2 * time.Second):
		}
	})
	return command, process, waited
}

func TestGxWatchdogPingIsCancelledByStop(t *testing.T) {
	previousInterval, previousTimeout := gxWatchdogInterval, gxWatchdogPingTimeout
	gxWatchdogInterval, gxWatchdogPingTimeout = 5*time.Millisecond, time.Second
	defer func() { gxWatchdogInterval, gxWatchdogPingTimeout = previousInterval, previousTimeout }()

	requestStarted := make(chan struct{})
	var startedOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedOnce.Do(func() { close(requestStarted) })
		<-r.Context().Done()
	}))
	defer server.Close()
	engine := &gxTorrentEngine{
		settings: gxTorrentSettings{BaseURL: server.URL, Timeout: time.Second},
		client:   server.Client(),
	}
	command, process, _ := startGxWatchdogTestProcess(t)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		engine.watchManagedHealth(process, stop)
	}()
	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog ping did not start")
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("watchdog did not return promptly after cancelling its ping")
	}
	if !gxPidAlive(command.Process.Pid) {
		t.Fatal("cancelling the watchdog killed the daemon")
	}
	process.Detach()
}

func TestGxWatchdogRechecksStopBeforeStoppingProcess(t *testing.T) {
	previousInterval := gxWatchdogInterval
	gxWatchdogInterval = 5 * time.Millisecond
	defer func() { gxWatchdogInterval = previousInterval }()

	requestStarted := make(chan struct{})
	var startedOnce sync.Once
	releaseResponse := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedOnce.Do(func() { close(requestStarted) })
		<-releaseResponse
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	engine := &gxTorrentEngine{
		settings: gxTorrentSettings{BaseURL: server.URL, Timeout: time.Second},
		client:   server.Client(),
	}
	command, process, _ := startGxWatchdogTestProcess(t)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Old enough that the failed probe would otherwise request a stop.
		engine.watchManagedHealthSince(process, stop, time.Now().Add(-gxUnresponsiveLimit-time.Second))
	}()
	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog ping did not start")
	}
	close(stop)
	close(releaseResponse)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not return after the in-flight probe")
	}
	if !gxPidAlive(command.Process.Pid) {
		t.Fatal("watchdog stopped the process after shutdown had been requested")
	}
	process.Detach()
}

func TestGxManagedProcessRefusesToSignalAReusedPID(t *testing.T) {
	command, process, _ := startGxWatchdogTestProcess(t)
	startTime, ok := gxProcessStartTime(command.Process.Pid)
	if !ok || startTime == 0 {
		t.Fatal("could not read the test process identity")
	}
	stale := &gxManagedProcess{pid: command.Process.Pid, startTime: startTime + 1, done: make(chan error, 1)}
	if err := stale.Stop(); err != nil {
		t.Fatalf("Stop on a reused PID = %v", err)
	}
	if !gxPidAlive(command.Process.Pid) {
		t.Fatal("Stop signalled a PID whose start time did not match")
	}
	if err := process.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-process.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the identified process was not stopped")
	}
}

func TestGxWatchdogAcceptsOnlyTheManagedDaemonIdentity(t *testing.T) {
	command, _, _ := startGxWatchdogTestProcess(t)
	startTime, ok := gxProcessStartTime(command.Process.Pid)
	if !ok {
		t.Fatal("could not read the test process identity")
	}
	process := &gxManagedProcess{pid: command.Process.Pid, signalPID: command.Process.Pid + 100000, startTime: 1, fingerprint: "another-fingerprint", done: make(chan error, 1)}
	cfg := &Config{DataDir: t.TempDir()}
	health := gxHealth{
		OK:          true,
		Version:     "1.2.1",
		Pid:         command.Process.Pid,
		Fingerprint: "expected-fingerprint",
		DataDir:     cfg.DataDir + "/gx-torrent",
	}
	if process.acceptHealth(cfg, health) {
		t.Fatal("watchdog accepted a daemon with a different fingerprint")
	}
	process.mu.Lock()
	process.fingerprint = health.Fingerprint
	process.mu.Unlock()
	if !process.acceptHealth(cfg, health) {
		t.Fatal("watchdog refused its own managed daemon")
	}
	pid, identityStart, open := process.identity()
	if !open || pid != command.Process.Pid || identityStart != startTime {
		t.Fatalf("watchdog did not bind the daemon identity: pid=%d start=%d open=%v", pid, identityStart, open)
	}
}
