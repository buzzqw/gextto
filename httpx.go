package gextto

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// defaultHTTPClient is the shared client for provider/source requests. It
// follows the reqwest defaults used by gextto (redirects, gzip automatic).
var defaultHTTPClient = &http.Client{
	Timeout: 90 * time.Second,
}

// Response size limits. They are intentionally generous: the goal is to avoid
// unbounded memory use from a misconfigured feed or a hostile server, not to
// cap legitimate content.
const (
	maxAPIResponseBytes  = 32 << 20  // JSON/API replies
	maxFeedResponseBytes = 64 << 20  // RSS/HTML feeds and .torrent files
	maxDecompressedBytes = 512 << 20 // decompressed archives (e.g. blocklists)
	maxUpdateBytes       = 512 << 20 // self-update payloads
)

// readLimitedBody reads at most limit bytes from r; a larger body is an error
// so a misconfigured or hostile response cannot exhaust memory.
func readLimitedBody(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response body exceeds the %d byte limit", limit)
	}
	return data, nil
}

// copyLimited streams src into dst, failing when the source is larger than
// limit. It never buffers the whole body.
func copyLimited(dst io.Writer, src io.Reader, limit int64) error {
	written, err := io.Copy(dst, io.LimitReader(src, limit+1))
	if err != nil {
		return err
	}
	if written > limit {
		return fmt.Errorf("response body exceeds the %d byte limit", limit)
	}
	return nil
}

// HTTPRequest performs an HTTP request with the given headers and body.
func HTTPRequest(ctx context.Context, method, rawURL string, headers map[string]string, body []byte, contentType string) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	return defaultHTTPClient.Do(request)
}

// HTTPGet performs a GET request.
func HTTPGet(ctx context.Context, rawURL string, headers map[string]string) (*http.Response, error) {
	return HTTPRequest(ctx, http.MethodGet, rawURL, headers, nil, "")
}

// HTTPGetBytes performs a GET and returns the body plus status code.
func HTTPGetBytes(ctx context.Context, rawURL string, headers map[string]string) ([]byte, int, error) {
	response, err := HTTPGet(ctx, rawURL, headers)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	payload, err := readLimitedBody(response.Body, maxFeedResponseBytes)
	if err != nil {
		return nil, response.StatusCode, err
	}
	return payload, response.StatusCode, nil
}

// HTTPGetString performs a GET and returns the body as a string.
func HTTPGetString(ctx context.Context, rawURL string, headers map[string]string) (string, int, error) {
	payload, status, err := HTTPGetBytes(ctx, rawURL, headers)
	return string(payload), status, err
}

// HTTPPostJSON posts a JSON payload and returns the response body and status.
func HTTPPostJSON(ctx context.Context, rawURL string, headers map[string]string, payload []byte) ([]byte, int, error) {
	response, err := HTTPRequest(ctx, http.MethodPost, rawURL, headers, payload, "application/json")
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	body, err := readLimitedBody(response.Body, maxAPIResponseBytes)
	if err != nil {
		return nil, response.StatusCode, err
	}
	return body, response.StatusCode, nil
}

// HTTPDownload streams a URL into a file writer, returning the byte count.
// The stream is capped so a misconfigured or hostile source cannot fill the
// target volume through an unbounded copy.
func HTTPDownload(ctx context.Context, rawURL string, headers map[string]string, writer io.Writer) (int64, error) {
	response, err := HTTPGet(ctx, rawURL, headers)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return 0, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	written, err := io.Copy(writer, io.LimitReader(response.Body, maxDecompressedBytes+1))
	if err != nil {
		return 0, err
	}
	if written > maxDecompressedBytes {
		return 0, fmt.Errorf("response body exceeds the %d byte limit", maxDecompressedBytes)
	}
	return written, nil
}
