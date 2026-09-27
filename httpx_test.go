package gextto

import (
	"bytes"
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
