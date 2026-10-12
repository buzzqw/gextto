// Package flaresolverr is a minimal client for a FlareSolverr service
// (https://github.com/FlareSolverr/FlareSolverr): it solves pages that sit
// behind a Cloudflare challenge. It posts the `request.get` command to the
// service's /v1 endpoint and returns the solved page body.
//
// It is intentionally stateless (one fresh browser per request): enough for a
// daemon that fetches a feed or runs an indexer query a few times per cycle.
// Gextto keeps its own session-reusing client in rss.go; this package is the
// standalone daemon's copy of the protocol, small and dependency-free.
package flaresolverr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultTimeout bounds one solve: FlareSolverr needs time to solve the
// challenge (its own default maxTimeout is 60 s).
const DefaultTimeout = 60 * time.Second

// maxResponseBytes caps the reply read from the service, so a misbehaving
// endpoint cannot make the daemon allocate unbounded memory.
const maxResponseBytes = 32 << 20 // 32 MiB

// Endpoint normalizes a configured base URL to the /v1 command endpoint, or ""
// when the value is empty.
func Endpoint(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return ""
	}
	if strings.HasSuffix(raw, "/v1") {
		return raw
	}
	return raw + "/v1"
}

// response is the subset of a FlareSolverr /v1 reply this client uses.
type response struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Solution struct {
		Status   int    `json:"status"`
		Response string `json:"response"`
	} `json:"solution"`
}

// Get fetches rawURL through the FlareSolverr service at endpoint and returns
// the solved page body. endpoint may be a base URL or a full /v1 URL.
func Get(ctx context.Context, endpoint, rawURL string) ([]byte, error) {
	return GetWithClient(ctx, &http.Client{Timeout: DefaultTimeout + 15*time.Second}, endpoint, rawURL)
}

// GetWithClient is Get with a caller-provided HTTP client (used by the tests).
func GetWithClient(ctx context.Context, client *http.Client, endpoint, rawURL string) ([]byte, error) {
	cmd := Endpoint(endpoint)
	if cmd == "" {
		return nil, fmt.Errorf("flaresolverr: empty endpoint")
	}
	if strings.TrimSpace(rawURL) == "" {
		return nil, fmt.Errorf("flaresolverr: empty URL")
	}
	if client == nil {
		client = http.DefaultClient
	}
	payload := map[string]any{
		"cmd":        "request.get",
		"url":        rawURL,
		"maxTimeout": int(DefaultTimeout / time.Millisecond),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, DefaultTimeout+15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, cmd, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("flaresolverr: HTTP %d", resp.StatusCode)
	}
	var out response
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("flaresolverr: invalid reply: %w", err)
	}
	if !strings.EqualFold(out.Status, "ok") {
		message := strings.TrimSpace(out.Message)
		if message == "" {
			message = out.Status
		}
		return nil, fmt.Errorf("flaresolverr: %s", message)
	}
	if out.Solution.Response == "" {
		return nil, fmt.Errorf("flaresolverr: empty solution")
	}
	return []byte(out.Solution.Response), nil
}

// Test checks that the service answers by solving a trivial page; it returns a
// short description of the outcome.
func Test(ctx context.Context, endpoint string) error {
	if Endpoint(endpoint) == "" {
		return fmt.Errorf("flaresolverr: empty endpoint")
	}
	body, err := Get(ctx, endpoint, "https://example.com/")
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return fmt.Errorf("flaresolverr: empty answer")
	}
	return nil
}
