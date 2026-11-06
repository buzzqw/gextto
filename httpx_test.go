package gextto

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestReadLimitedBodyWithinLimit(t *testing.T) {
	data, err := readLimitedBody(strings.NewReader("hello"), 5)
	if err != nil {
		t.Fatalf("readLimitedBody within limit: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("body = %q, want hello", string(data))
	}
}

func TestReadLimitedBodyOverLimit(t *testing.T) {
	if _, err := readLimitedBody(strings.NewReader("hello!"), 5); err == nil {
		t.Fatal("readLimitedBody must reject a body larger than the limit")
	}
}

// unsizedReader hides the length of the source, like a chunked HTTP response.
type unsizedReader struct{ r io.Reader }

func (u unsizedReader) Read(p []byte) (int, error) { return u.r.Read(p) }

// TestReadLimitedBodyGrowsInFewSteps pins that an unknown-size body is not
// copied once per doubling from a tiny buffer: a cycle reads tens of these.
func TestReadLimitedBodyGrowsInFewSteps(t *testing.T) {
	data := make([]byte, 1<<20)
	var readErr error
	allocs := testing.AllocsPerRun(5, func() {
		_, readErr = readLimitedBody(unsizedReader{bytes.NewReader(data)}, int64(len(data)))
	})
	if readErr != nil {
		t.Fatalf("readLimitedBody: %v", readErr)
	}
	if allocs > 10 {
		t.Fatalf("a 1 MiB body allocated %v times, want at most 10", allocs)
	}
}

func TestCopyLimitedWithinLimit(t *testing.T) {
	var buffer bytes.Buffer
	if err := copyLimited(&buffer, strings.NewReader("hello"), 5); err != nil {
		t.Fatalf("copyLimited within limit: %v", err)
	}
	if buffer.String() != "hello" {
		t.Fatalf("copied = %q, want hello", buffer.String())
	}
}

func TestCopyLimitedOverLimit(t *testing.T) {
	var buffer bytes.Buffer
	if err := copyLimited(&buffer, strings.NewReader("hello!"), 5); err == nil {
		t.Fatal("copyLimited must reject a source larger than the limit")
	}
}
