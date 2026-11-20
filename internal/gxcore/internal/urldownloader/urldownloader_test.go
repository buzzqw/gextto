package urldownloader

import (
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func response(status int, header map[string]string) *http.Response {
	h := http.Header{}
	for k, v := range header {
		h.Set(k, v)
	}
	return &http.Response{StatusCode: status, Header: h}
}

// TestCheckStatus: a 206 must start where we asked, a 200 is valid only at
// offset zero, and 429/503 carry their Retry-After instead of being a generic
// failure.
func TestCheckStatus(t *testing.T) {
	cases := []struct {
		name   string
		resp   *http.Response
		begin  int64
		length int64
		wantOK bool
	}{
		{"partial at the requested offset", response(206, map[string]string{
			"Content-Range": "bytes 100-199/1000",
		}), 100, 100, true},
		{"partial without a content range", response(206, nil), 100, 100, true},
		{"partial from another offset", response(206, map[string]string{
			"Content-Range": "bytes 0-99/1000",
		}), 100, 100, false},
		{"partial shorter than asked", response(206, map[string]string{
			"Content-Range": "bytes 100-149/1000",
		}), 100, 100, false},
		{"full content at zero", response(200, nil), 0, 100, true},
		{"full content at an offset", response(200, nil), 100, 100, false},
		{"too many requests", response(429, map[string]string{"Retry-After": "120"}), 0, 100, false},
		{"not found", response(404, nil), 0, 100, false},
	}
	for _, c := range cases {
		err := checkStatus(c.resp, c.begin, c.length)
		if c.wantOK && err != nil {
			t.Errorf("%s: unexpected error: %v", c.name, err)
		}
		if !c.wantOK && err == nil {
			t.Errorf("%s: expected an error", c.name)
		}
	}
}

// TestCheckStatusRetryAfter: 429 and 503 keep the delay the server asked for
// (up to an hour), so the caller can park the source for that long.
func TestCheckStatusRetryAfter(t *testing.T) {
	cases := []struct {
		status int
		header string
		want   time.Duration
	}{
		{429, "120", 2 * time.Minute},
		{503, "3600", time.Hour},
		{429, "99999", time.Hour},
		{429, "", 0},
		{429, "not a number", 0},
	}
	for _, c := range cases {
		err := checkStatus(response(c.status, map[string]string{"Retry-After": c.header}), 0, 100)
		var statusErr *HTTPStatusError
		if !errors.As(err, &statusErr) {
			t.Errorf("status %d, retry-after %q: got %v, want an HTTPStatusError", c.status, c.header, err)
			continue
		}
		if statusErr.Code != c.status {
			t.Errorf("status %d: code = %d", c.status, statusErr.Code)
		}
		if statusErr.RetryAfter != c.want {
			t.Errorf("status %d, retry-after %q: RetryAfter = %v, want %v",
				c.status, c.header, statusErr.RetryAfter, c.want)
		}
	}
}

// TestParseRetryAfterDate: the header may be an HTTP date instead of seconds.
func TestParseRetryAfterDate(t *testing.T) {
	when := time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(when)
	if got < 90*time.Second || got > 2*time.Minute {
		t.Errorf("Retry-After date parsed as %v, want about two minutes", got)
	}
	past := time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 0 {
		t.Errorf("past Retry-After date = %v, want 0", got)
	}
}

// TestCheckContentRange: malformed ranges are rejected instead of trusted.
func TestCheckContentRange(t *testing.T) {
	cases := []struct {
		value string
		ok    bool
	}{
		{"bytes 0-99/1000", true},
		{"bytes 0-99/*", true},
		{"", true},
		{"items 0-99/1000", false},
		{"bytes 10-99/1000", false},
		{"bytes 0-9/1000", false},
		{"bytes x-y/1000", false},
	}
	for _, c := range cases {
		err := checkContentRange(c.value, 0, 100)
		if c.ok && err != nil {
			t.Errorf("%q: unexpected error: %v", c.value, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%q: expected an error", c.value)
		}
	}
}

// TestGetURLSingleAndMultiFile pins how a web seed URL is addressed: a single
// file is the URL itself (or the file under a base ending in slash), a folder
// always receives the relative path, even when it holds only one file.
func TestGetURLSingleAndMultiFile(t *testing.T) {
	d := &URLDownloader{URL: "https://host/base"}
	if got := d.getURL("movie.mkv", false); got != "https://host/base" {
		t.Errorf("single-file bare URL = %q", got)
	}
	if got := d.getURL(filepath.Join("pack", "movie.mkv"), true); got != "https://host/base/pack/movie.mkv" {
		t.Errorf("multi-file bare URL = %q", got)
	}
	d.URL = "https://host/base/"
	if got := d.getURL(filepath.Join("pack", "movie.mkv"), false); got != "https://host/base/pack/movie.mkv" {
		t.Errorf("single-file base with slash = %q", got)
	}
	if got := d.getURL(filepath.Join("pack", "movie.mkv"), true); got != "https://host/base/pack/movie.mkv" {
		t.Errorf("multi-file base with slash = %q", got)
	}
}
