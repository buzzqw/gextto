package gextto

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// failingCloseListener makes Server.Shutdown return a listener error which can
// be checked at the shutdownServers boundary.
type failingCloseListener struct {
	net.Listener
	accepted chan struct{}
	once     sync.Once
	err      error
}

func (l *failingCloseListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.once.Do(func() { close(l.accepted) })
	}
	return conn, err
}

func (l *failingCloseListener) Close() error {
	_ = l.Listener.Close()
	return l.err
}

func TestShutdownServersPropagatesListenerErrors(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closeErr := errors.New("listener close failed")
	observed := &failingCloseListener{Listener: listener, accepted: make(chan struct{}), err: closeErr}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})}
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = server.Serve(observed)
	}()
	response, err := http.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	select {
	case <-observed.accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not accept the probe connection")
	}

	if err := shutdownServers(server); !errors.Is(err, closeErr) {
		t.Fatalf("shutdown error = %v, want %v", err, closeErr)
	}
	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after shutdown")
	}
}

func TestShutdownServersForceCloseAndWaitForActiveHandlers(t *testing.T) {
	previous := serverShutdownTimeout
	serverShutdownTimeout = 30 * time.Millisecond
	defer func() { serverShutdownTimeout = previous }()

	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	tracker := newHTTPHandlerTracker()
	server := &http.Server{Handler: tracker.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(handlerStarted)
		<-releaseHandler
		w.WriteHeader(http.StatusNoContent)
	}))}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = server.Serve(listener)
	}()

	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: test\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- shutdownServersWithHandlers(tracker, server) }()

	// Close must drop the active connection after the graceful deadline even
	// though the handler itself ignores its request context.
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var one [1]byte
	if _, err := conn.Read(one[:]); err == nil {
		t.Fatal("active connection survived forced Close")
	}
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned while a handler was active: %v", err)
	default:
	}

	// The gate is already closed: a late handler is rejected instead of being
	// allowed to start and race the final wait.
	recorder := httptest.NewRecorder()
	tracker.wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("a handler entered after the shutdown gate closed")
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://test/", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("late handler status = %d, want 503", recorder.Code)
	}

	close(releaseHandler)
	select {
	case err := <-shutdownDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("shutdown error = %v, want deadline exceeded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish after the active handler returned")
	}
	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return")
	}
}

func TestStopBackgroundWorkersCancelsJobsBeforeWaitingForWorkers(t *testing.T) {
	previousRegistry := mediaFileOps
	mediaFileOps = &fileOperationRegistry{active: map[uint64]fileOperation{}, blockWhenClosed: false}
	defer func() { mediaFileOps = previousRegistry }()

	ctx, cancel := context.WithCancel(context.Background())
	state := &AppState{
		bgStop:    make(chan struct{}),
		bgContext: ctx,
		bgCancel:  cancel,
		jobs:      NewJobManager(1, 5),
	}
	workerRelease := make(chan struct{})
	state.bgWG.Add(1)
	go func() {
		defer state.bgWG.Done()
		<-workerRelease
	}()

	job, created := state.jobs.Create("test-job", "test-job")
	if !created {
		t.Fatal("job was not created")
	}
	jobStarted := make(chan struct{})
	jobCanceled := make(chan struct{})
	if err := state.jobs.Start(job.ID, func(ctx context.Context, id string) {
		close(jobStarted)
		<-ctx.Done()
		close(jobCanceled)
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-jobStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not start")
	}

	stopDone := make(chan struct{})
	go func() {
		stopBackgroundWorkers(state)
		close(stopDone)
	}()
	select {
	case <-jobCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("job was not cancelled while a background worker was still blocked")
	}
	select {
	case <-stopDone:
		t.Fatal("shutdown returned before the tracked worker completed")
	default:
	}

	close(workerRelease)
	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish after the worker returned")
	}
	// Registration after shutdown is a no-op, so a late caller cannot make a
	// future WaitGroup.Wait race with Add.
	state.trackOperation()()
}

func TestSeriesRenameApplyUsesCancelledLifecycleContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := &AppState{
		bgStop:    make(chan struct{}),
		bgContext: ctx,
		bgCancel:  cancel,
	}
	status, value := seriesRenameApply(state, "Series A", true, false, false)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; value = %#v", status, value)
	}
	message, _ := value.(map[string]any)
	if message["error"] != "shutdown in progress" {
		t.Fatalf("error = %#v", message["error"])
	}
}
