package gextto

import (
	"bytes"
	"os"
	"sync"
	"testing"

	"github.com/buzzqw/gextto/internal/logging"
)

// syncBuffer is a log sink that can be read while a daemon or a job is still
// writing in the background. A plain bytes.Buffer shares its bytes with the
// logger and races with the reader under -race as soon as a test waits for a
// background task to finish.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (s *syncBuffer) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Len()
}

// captureLog installs a thread-safe sink for the duration of the test.
func captureLog(t *testing.T) *syncBuffer {
	t.Helper()
	captured := &syncBuffer{}
	logging.SetOutput(captured)
	t.Cleanup(func() { logging.SetOutput(os.Stdout) })
	return captured
}
