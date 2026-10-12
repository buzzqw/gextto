package flaresolverr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEndpoint(t *testing.T) {
	cases := map[string]string{
		"":                        "",
		"   ":                     "",
		"http://host:8191":        "http://host:8191/v1",
		"http://host:8191/":       "http://host:8191/v1",
		"http://host:8191/v1":     "http://host:8191/v1",
		"http://host:8191/v1/":    "http://host:8191/v1",
		"  http://host:8191/v1  ": "http://host:8191/v1",
	}
	for in, want := range cases {
		if got := Endpoint(in); got != want {
			t.Errorf("Endpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGetSolvesThePage(t *testing.T) {
	var gotCmd, gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotCmd, _ = req["cmd"].(string)
		gotURL, _ = req["url"].(string)
		fmt.Fprint(w, `{"status":"ok","solution":{"status":200,"response":"<html>solved</html>"}}`)
	}))
	defer srv.Close()

	body, err := Get(context.Background(), srv.URL, "https://target.example/feed")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "<html>solved</html>" {
		t.Fatalf("body = %q", body)
	}
	if gotCmd != "request.get" || gotURL != "https://target.example/feed" {
		t.Fatalf("payload cmd=%q url=%q", gotCmd, gotURL)
	}
}

func TestGetReportsServiceError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"error","message":"challenge not solved"}`)
	}))
	defer srv.Close()
	if _, err := Get(context.Background(), srv.URL, "https://x/"); err == nil {
		t.Fatal("a service error must be returned")
	}
}

func TestGetRejectsEmptyEndpoint(t *testing.T) {
	if _, err := Get(context.Background(), "", "https://x/"); err == nil {
		t.Fatal("an empty endpoint must be rejected")
	}
}

func TestTestAcceptsAWorkingService(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"ok","solution":{"status":200,"response":"ok"}}`)
	}))
	defer srv.Close()
	if err := Test(context.Background(), srv.URL); err != nil {
		t.Fatalf("Test = %v", err)
	}
}
