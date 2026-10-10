package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	mk := func(origin, referer, host string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "http://"+host+"/ui/action", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if referer != "" {
			req.Header.Set("Referer", referer)
		}
		return req
	}
	cases := []struct {
		name string
		req  *http.Request
		want bool
	}{
		{"no headers", mk("", "", "127.0.0.1:8890"), true},
		{"matching origin", mk("http://127.0.0.1:8890", "", "127.0.0.1:8890"), true},
		{"foreign origin", mk("http://evil.example", "", "127.0.0.1:8890"), false},
		{"origin null", mk("null", "", "127.0.0.1:8890"), false},
		{"matching referer", mk("", "http://127.0.0.1:8890/", "127.0.0.1:8890"), true},
		{"foreign referer", mk("", "http://evil.example/x", "127.0.0.1:8890"), false},
	}
	for _, c := range cases {
		if got := sameOrigin(c.req); got != c.want {
			t.Errorf("%s: sameOrigin = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestStandaloneGuard(t *testing.T) {
	post := func(origin string) (*httptest.ResponseRecorder, *bool) {
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusOK) })
		d := &Daemon{opts: Options{Mode: ModeStandalone}}
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/ui/action", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		d.standaloneGuard(next).ServeHTTP(rec, req)
		return rec, &called
	}

	if rec, called := post("http://evil.example"); rec.Code != http.StatusForbidden || *called {
		t.Fatalf("standalone must reject a cross-origin POST: status=%d called=%v", rec.Code, *called)
	}
	if rec, called := post("http://127.0.0.1:8890"); rec.Code != http.StatusOK || !*called {
		t.Fatalf("standalone must allow a same-origin POST: status=%d called=%v", rec.Code, *called)
	}
}

func TestManagedGuardIsNoop(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusOK) })
	d := &Daemon{opts: Options{Mode: ModeManaged}}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/ui/action", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	d.standaloneGuard(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !called {
		t.Fatalf("managed mode must not change behaviour: status=%d called=%v", rec.Code, called)
	}
}
