package gextto

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebJSONFetchesEnforceExactBodyLimit(t *testing.T) {
	previous := webJSONResponseLimit
	webJSONResponseLimit = 32
	defer func() { webJSONResponseLimit = previous }()

	exact := append([]byte(`{"n":1}`), bytes.Repeat([]byte(" "), 32-len(`{"n":1}`))...)
	tooLarge := append(append([]byte(nil), exact...), ' ')
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/large" {
			_, _ = w.Write(tooLarge)
			return
		}
		_, _ = w.Write(exact)
	}))
	defer server.Close()

	for name, fetch := range map[string]func() (any, error){
		"GET":  func() (any, error) { return fetchJSON(context.Background(), server.URL) },
		"POST": func() (any, error) { return fetchJSONPost(context.Background(), server.URL, []byte(`{}`)) },
	} {
		t.Run(name, func(t *testing.T) {
			value, err := fetch()
			if err != nil {
				t.Fatalf("exact-limit response: %v", err)
			}
			object := objectValue(value)
			if _, ok := object["n"].(json.Number); !ok {
				t.Fatalf("JSON number was not preserved: %#v", object["n"])
			}
			if name == "POST" {
				_, err = fetchJSONPost(context.Background(), server.URL+"/large", []byte(`{}`))
			} else {
				_, err = fetchJSON(context.Background(), server.URL+"/large")
			}
			if err == nil || !strings.Contains(err.Error(), "byte limit") {
				t.Fatalf("limit+1 response error = %v", err)
			}
		})
	}
}

func TestWebHTMLAndServiceProbesEnforceExactBodyLimit(t *testing.T) {
	previousHTML, previousService := webHTMLResponseLimit, servicesProbeResponseLimit
	webHTMLResponseLimit, servicesProbeResponseLimit = 3, 3
	defer func() {
		webHTMLResponseLimit, servicesProbeResponseLimit = previousHTML, previousService
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/large" {
			_, _ = w.Write([]byte("four"))
			return
		}
		_, _ = w.Write([]byte("abc"))
	}))
	defer server.Close()

	body, err := fetchHtml(context.Background(), server.URL, nil)
	if err != nil || body != "abc" {
		t.Fatalf("exact HTML response = %q, %v", body, err)
	}
	if _, err := fetchHtml(context.Background(), server.URL+"/large", nil); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("oversized HTML error = %v", err)
	}

	status, reachable, body := servicesProbe(server.URL, time.Second)
	if status != http.StatusOK || !reachable || body != "abc" {
		t.Fatalf("exact service probe = %d, %v, %q", status, reachable, body)
	}
	status, reachable, body = servicesProbe(server.URL+"/large", time.Second)
	if status != http.StatusOK || reachable || !strings.Contains(body, "byte limit") {
		t.Fatalf("oversized service probe = %d, %v, %q", status, reachable, body)
	}
}

func TestWebHTMLRejectsOversizedFlareSolverrResponse(t *testing.T) {
	resetCloudflareMemoryForTest(t)
	previous := webHTMLResponseLimit
	webHTMLResponseLimit = 1
	defer func() { webHTMLResponseLimit = previous }()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html><title>Just a moment...</title></html>"))
	}))
	defer target.Close()
	flare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var command struct {
			Cmd string `json:"cmd"`
		}
		_ = json.NewDecoder(r.Body).Decode(&command)
		if command.Cmd == "sessions.create" {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "session": "sess-limit"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"solution": map[string]any{
				"response": "ok",
			},
		})
	}))
	defer flare.Close()

	if _, err := fetchHtml(context.Background(), target.URL, &flare.URL); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("oversized FlareSolverr HTML error = %v", err)
	}
}
