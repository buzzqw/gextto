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
	payload, err := io.ReadAll(response.Body)
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
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, response.StatusCode, err
	}
	return body, response.StatusCode, nil
}

// HTTPDownload streams a URL into a file writer, returning the byte count.
func HTTPDownload(ctx context.Context, rawURL string, headers map[string]string, writer io.Writer) (int64, error) {
	response, err := HTTPGet(ctx, rawURL, headers)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return 0, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return io.Copy(writer, response.Body)
}
