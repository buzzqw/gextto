package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/constants"
)

func buildTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestExtractTarGz(t *testing.T) {
	archive := buildTarGz(t, map[string]string{
		"./VERSION": "1.1.20\n",
		"./gx-ed2k": "new-binary",
	})
	got, err := extractTarGz(archive, "gx-ed2k", 1<<20)
	if err != nil || string(got) != "new-binary" {
		t.Fatalf("extract = %q, %v", got, err)
	}
	if _, err := extractTarGz(archive, "missing", 1<<20); err == nil {
		t.Fatal("missing file must fail")
	}
	traversal := buildTarGz(t, map[string]string{"../evil/gx-ed2k": "bad"})
	if _, err := extractTarGz(traversal, "gx-ed2k", 1<<20); err == nil {
		t.Fatal("traversal entry must not be extracted")
	}
}

func TestParseSHA256(t *testing.T) {
	value := strings.Repeat("a", 64)
	if got, err := parseSHA256([]byte(value + "  gx-ed2k-linux-x86_64.tar.gz\n")); err != nil || got != value {
		t.Fatalf("parse = %q, %v", got, err)
	}
	if _, err := parseSHA256([]byte("not-a-hash")); err == nil {
		t.Fatal("malformed checksum must fail")
	}
}

func TestUpdateApplyRejectsLocalBuild(t *testing.T) {
	old := constants.GxEd2kChannel
	constants.GxEd2kChannel = ""
	defer func() { constants.GxEd2kChannel = old }()
	daemon, _ := testDaemon(t)
	recorder := httptest.NewRecorder()
	daemon.handleUpdateApply(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/update/apply", nil))
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "local build") {
		t.Fatalf("local build = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestDownloadUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/large" {
			_, _ = w.Write([]byte("0123456789"))
			return
		}
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("payload"))
	}))
	defer server.Close()
	got, err := downloadUpdate(context.Background(), server.Client(), server.URL+"/ok", 16)
	if err != nil || string(got) != "payload" {
		t.Fatalf("download = %q, %v", got, err)
	}
	if _, err := downloadUpdate(context.Background(), server.Client(), server.URL+"/large", 4); err == nil {
		t.Fatal("large download must fail")
	}
	if _, err := downloadUpdate(context.Background(), server.Client(), server.URL+"/missing", 16); err == nil {
		t.Fatal("HTTP 404 must fail")
	}
}
