package gextto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// simklTestServer points both Simkl hosts (api and data) at a hermetic server.
func simklTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	previousAPI, previousData := simklAPIBaseURL, simklDataBaseURL
	simklAPIBaseURL, simklDataBaseURL = server.URL, server.URL
	t.Cleanup(func() {
		simklAPIBaseURL = previousAPI
		simklDataBaseURL = previousData
		server.Close()
	})
	return server
}

func integrationWriteJSON(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(body)); err != nil {
		t.Errorf("write integration response: %v", err)
	}
}

func integrationDecodeBody(t *testing.T, r *http.Request) any {
	t.Helper()
	var value any
	if err := json.NewDecoder(r.Body).Decode(&value); err != nil {
		t.Errorf("decode request body: %v", err)
	}
	return value
}

func TestSimklFromSettingsConfiguredAndAuthenticated(t *testing.T) {
	empty := (&SimklClient{}).FromSettings(map[string]string{})
	if empty.Configured() || empty.Authenticated() {
		t.Fatal("empty settings must be neither configured nor authenticated")
	}
	full := (&SimklClient{}).FromSettings(map[string]string{
		"simkl_client_id":    "cid",
		"simkl_access_token": "tok",
	})
	if !full.Configured() || !full.Authenticated() {
		t.Fatal("full settings must be configured and authenticated")
	}
	partial := (&SimklClient{}).FromSettings(map[string]string{"simkl_client_id": "cid"})
	if !partial.Configured() || partial.Authenticated() {
		t.Fatal("client id only must be configured but not authenticated")
	}
}

func TestSimklPinStartPayloadAndNoAuth(t *testing.T) {
	simklTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/pin" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.RawQuery; got != "client_id=cid&app-name=gextto&app-version=1" {
			t.Errorf("raw query = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("PIN start must not send Authorization, got %q", got)
		}
		integrationWriteJSON(t, w, `{"user_code":"UC","verification_uri":"https://simkl.com/pin","expires_in":900,"interval":5}`)
	})

	client := (&SimklClient{}).FromSettings(map[string]string{
		"simkl_client_id":    "cid",
		"simkl_access_token": "tok",
	})
	result, err := client.PinStart(context.Background())
	if err != nil {
		t.Fatalf("PinStart: %v", err)
	}
	code, err := TokenString(result, "user_code")
	if err != nil || code != "UC" {
		t.Fatalf("user_code = %q, %v", code, err)
	}
}

func TestSimklPinPollPayload(t *testing.T) {
	simklTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/pin/UC" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.RawQuery; got != "client_id=cid&app-name=gextto&app-version=1" {
			t.Errorf("raw query = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("PIN poll must not send Authorization, got %q", got)
		}
		integrationWriteJSON(t, w, `{"result":"OK","access_token":"tok"}`)
	})
	client := (&SimklClient{}).FromSettings(map[string]string{"simkl_client_id": "cid"})
	result, err := client.PinPoll(context.Background(), "UC")
	if err != nil {
		t.Fatalf("PinPoll: %v", err)
	}
	if token, err := TokenString(result, "access_token"); err != nil || token != "tok" {
		t.Fatalf("access_token = %q, %v", token, err)
	}
}

func TestSimklWatchlistCalendarAndMarkWatched(t *testing.T) {
	t.Run("watchlist", func(t *testing.T) {
		simklTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/sync/all-items" {
				t.Errorf("path = %q", r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer tok" {
				t.Errorf("Authorization = %q", got)
			}
			if got := r.URL.RawQuery; got != "client_id=cid&app-name=gextto&app-version=1" {
				t.Errorf("raw query = %q", got)
			}
			integrationWriteJSON(t, w, `{"shows":[{"show":{"title":"X"}}]}`)
		})
		client := (&SimklClient{}).FromSettings(map[string]string{
			"simkl_client_id":    "cid",
			"simkl_access_token": "tok",
		})
		result, err := client.Watchlist(context.Background())
		if err != nil {
			t.Fatalf("Watchlist: %v", err)
		}
		assertJSONEqual(t, result, `{"shows":[{"show":{"title":"X"}}]}`)
	})

	t.Run("calendar", func(t *testing.T) {
		simklTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/calendars" {
				t.Errorf("path = %q", r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer tok" {
				t.Errorf("Authorization = %q", got)
			}
			if got := r.URL.RawQuery; got != "client_id=cid&app-name=gextto&app-version=1" {
				t.Errorf("raw query = %q", got)
			}
			integrationWriteJSON(t, w, `[{"title":"X","date":"2026-05-01"}]`)
		})
		client := (&SimklClient{}).FromSettings(map[string]string{
			"simkl_client_id":    "cid",
			"simkl_access_token": "tok",
		})
		result, err := client.Calendar(context.Background())
		if err != nil {
			t.Fatalf("Calendar: %v", err)
		}
		assertJSONEqual(t, result, `[{"title":"X","date":"2026-05-01"}]`)
	})

	t.Run("mark watched", func(t *testing.T) {
		simklTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("method = %q, want POST", r.Method)
			}
			if r.URL.Path != "/sync/history" {
				t.Errorf("path = %q", r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer tok" {
				t.Errorf("Authorization = %q", got)
			}
			if got := r.URL.RawQuery; got != "client_id=cid&app-name=gextto&app-version=1" {
				t.Errorf("raw query = %q", got)
			}
			assertJSONEqual(t, integrationDecodeBody(t, r), `{"shows":[{"ids":{"simkl":1},"watched_at":"2026-05-01T00:00:00Z"}]}`)
			integrationWriteJSON(t, w, `{"added":{"shows":1}}`)
		})
		client := (&SimklClient{}).FromSettings(map[string]string{
			"simkl_client_id":    "cid",
			"simkl_access_token": "tok",
		})
		result, err := client.MarkWatched(context.Background(), map[string]any{
			"shows": []any{map[string]any{
				"ids":        map[string]any{"simkl": 1},
				"watched_at": "2026-05-01T00:00:00Z",
			}},
		})
		if err != nil {
			t.Fatalf("MarkWatched: %v", err)
		}
		assertJSONEqual(t, result, `{"added":{"shows":1}}`)
	})
}

func TestSimklHTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		run    func(*SimklClient) error
	}{
		{"watchlist", http.StatusInternalServerError, func(c *SimklClient) error {
			_, err := c.Watchlist(context.Background())
			return err
		}},
		{"calendar", http.StatusInternalServerError, func(c *SimklClient) error {
			_, err := c.Calendar(context.Background())
			return err
		}},
		{"pin start", http.StatusForbidden, func(c *SimklClient) error {
			_, err := c.PinStart(context.Background())
			return err
		}},
		{"mark watched", http.StatusBadGateway, func(c *SimklClient) error {
			_, err := c.MarkWatched(context.Background(), map[string]any{})
			return err
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			simklTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(testCase.status)
			})
			client := (&SimklClient{}).FromSettings(map[string]string{
				"simkl_client_id":    "cid",
				"simkl_access_token": "tok",
			})
			err := testCase.run(client)
			want := "HTTP " + strconv.Itoa(testCase.status)
			if err == nil || err.Error() != want {
				t.Fatalf("error = %v, want %q", err, want)
			}
		})
	}
}

func TestIntegrationJSONSyntaxError(t *testing.T) {
	simklTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		integrationWriteJSON(t, w, `not-json`)
	})
	client := (&SimklClient{}).FromSettings(map[string]string{
		"simkl_client_id":    "cid",
		"simkl_access_token": "tok",
	})
	_, err := client.Watchlist(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("error = %v", err)
	}
}

func TestTokenString(t *testing.T) {
	value := map[string]any{"access_token": "tok", "empty": "", "number": 1.0}
	if token, err := TokenString(value, "access_token"); err != nil || token != "tok" {
		t.Fatalf("TokenString = %q, %v", token, err)
	}
	for _, key := range []string{"missing", "empty", "number"} {
		if _, err := TokenString(value, key); err == nil || err.Error() != "integration response missing "+key {
			t.Fatalf("TokenString(%q) error = %v", key, err)
		}
	}
	if _, err := TokenString([]any{"tok"}, "access_token"); err == nil || err.Error() != "integration response missing access_token" {
		t.Fatalf("non-object error = %v", err)
	}
}

func TestEncodeParamsPreservesOrderAndEscapes(t *testing.T) {
	got := encodeParams([][2]string{{"b", "2"}, {"a", "1"}, {"q", "a b&c"}})
	if got != "b=2&a=1&q=a+b%26c" {
		t.Fatalf("encodeParams = %q", got)
	}
}

func TestClampInt64(t *testing.T) {
	cases := []struct {
		value, lo, hi, want int64
	}{
		{0, 1, 31, 1},
		{7, 1, 31, 7},
		{100, 1, 31, 31},
		{-3, 0, 10, 0},
	}
	for _, testCase := range cases {
		if got := clampInt64(testCase.value, testCase.lo, testCase.hi); got != testCase.want {
			t.Errorf("clampInt64(%d, %d, %d) = %d, want %d",
				testCase.value, testCase.lo, testCase.hi, got, testCase.want)
		}
	}
}
