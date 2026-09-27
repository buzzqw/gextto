package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestGuardHandlerRecoversPanic(t *testing.T) {
	panicking := func(w http.ResponseWriter, r *http.Request, s *AppState) {
		panic("boom")
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/boom", nil)
	guardHandler("GET /api/boom", panicking)(recorder, request, &AppState{})
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded["ok"] != false {
		t.Fatalf("body = %v", decoded)
	}
}

func TestSafeGoRestartsAfterPanic(t *testing.T) {
	previous := workerRestartDelay
	workerRestartDelay = 10 * time.Millisecond
	defer func() { workerRestartDelay = previous }()

	var calls atomic.Int32
	done := make(chan struct{})
	safeGo("test_worker", func() {
		if calls.Add(1) == 1 {
			panic("worker exploded")
		}
		close(done)
	})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("worker was not restarted after a panic (calls=%d)", calls.Load())
	}
	if calls.Load() < 2 {
		t.Fatalf("calls = %d, want >= 2", calls.Load())
	}
}
