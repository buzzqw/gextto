package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/updatecheck"
)

func TestUpdateViewFor(t *testing.T) {
	current := "1.1.19"
	newer := &updatecheck.Manifest{GxEd2kVersion: "1.1.20", GxEd2kBuild: 20}
	view := updateViewFor(current, 19, "stable", newer)
	if !view.Available || view.Latest != "1.1.20" || !strings.Contains(view.URL, "gx-ed2k-linux-") {
		t.Fatalf("newer = %+v", view)
	}
	same := updateViewFor(current, 19, "stable", &updatecheck.Manifest{GxEd2kVersion: "1.1.19", GxEd2kBuild: 19})
	if same.Available || same.URL != "" {
		t.Fatalf("same = %+v", same)
	}
	missing := updateViewFor(current, 19, "stable", &updatecheck.Manifest{})
	if missing.Available {
		t.Fatalf("missing build = %+v", missing)
	}
}

func TestUpdateEndpointForLocalBuild(t *testing.T) {
	daemon, _ := testDaemon(t)
	recorder := httptest.NewRecorder()
	daemon.handleUpdate(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/update", nil))
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `"local_build":true`) {
		t.Fatalf("local build = %d %s", recorder.Code, recorder.Body.String())
	}
}
