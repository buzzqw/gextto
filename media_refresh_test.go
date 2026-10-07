package gextto

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type recordedRequest struct {
	Method, Path, Query, Body string
}

func recordingServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, func() []recordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var requests []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, recordedRequest{r.Method, r.URL.Path, r.URL.RawQuery, string(body)})
		mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return server, func() []recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedRequest(nil), requests...)
	}
}

func TestRefreshJellyfinTargetsChangedFolderWithMapping(t *testing.T) {
	server, requests := recordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	cfg := DefaultConfig()
	cfg.Settings["jellyfin_url"] = server.URL
	cfg.Settings["jellyfin_api_key"] = "key"
	cfg.Settings["jellyfin_path_mappings"] = "/mnt/nas/serie=/media/tv"
	RefreshMediaLibraries(&cfg, "/mnt/nas/serie/Show/Season 01", "/mnt/nas/serie/Show/Season 01")
	got := requests()
	if len(got) != 1 || got[0].Path != "/Library/Media/Updated" {
		t.Fatalf("expected one targeted request, got %+v", got)
	}
	var payload struct {
		Updates []struct{ Path, UpdateType string }
	}
	if err := json.Unmarshal([]byte(got[0].Body), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Updates) != 1 || payload.Updates[0].Path != "/media/tv/Show/Season 01" {
		t.Fatalf("unexpected updates: %+v", payload.Updates)
	}
}

func TestRefreshJellyfinFallsBackToFullRefresh(t *testing.T) {
	server, requests := recordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Library/Media/Updated" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	cfg := DefaultConfig()
	cfg.Settings["jellyfin_url"] = server.URL
	cfg.Settings["jellyfin_api_key"] = "key"
	RefreshMediaLibraries(&cfg, "/srv/tv/Show")
	got := requests()
	if len(got) != 2 || got[1].Path != "/Library/Refresh" {
		t.Fatalf("expected targeted then full refresh, got %+v", got)
	}
}

func plexTestServer(t *testing.T) (*httptest.Server, func() []recordedRequest) {
	return recordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/library/sections" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[
				{"key":"1","Location":[{"path":"/data/movies"}]},
				{"key":"2","Location":[{"path":"/data/tv"},{"path":"/data/anime"}]},
				{"key":"3","Location":[{"path":"/data/tv/kids"}]}]}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func TestRefreshPlexTargetsSectionAndFolder(t *testing.T) {
	server, requests := plexTestServer(t)
	cfg := DefaultConfig()
	cfg.Settings["plex_url"] = server.URL
	cfg.Settings["plex_token"] = "token"
	cfg.Settings["plex_path_mappings"] = "/mnt/nas/tv=/data/tv"
	RefreshMediaLibraries(&cfg, "/mnt/nas/tv/kids/Bluey/Season 01")
	got := requests()
	if len(got) != 2 {
		t.Fatalf("expected sections + one refresh, got %+v", got)
	}
	if got[1].Path != "/library/sections/3/refresh" || got[1].Query != "path=%2Fdata%2Ftv%2Fkids%2FBluey%2FSeason+01" {
		t.Fatalf("unexpected targeted refresh: %+v", got[1])
	}
}

func TestRefreshPlexWithoutMatchingSectionRefreshesAll(t *testing.T) {
	server, requests := plexTestServer(t)
	cfg := DefaultConfig()
	cfg.Settings["plex_url"] = server.URL
	cfg.Settings["plex_token"] = "token"
	RefreshMediaLibraries(&cfg, "/elsewhere/Show")
	got := requests()
	if len(got) != 2 || got[1].Path != "/library/sections/all/refresh" {
		t.Fatalf("expected a full refresh fallback, got %+v", got)
	}
}

func TestRefreshWithoutFoldersIsFull(t *testing.T) {
	server, requests := plexTestServer(t)
	cfg := DefaultConfig()
	cfg.Settings["plex_url"] = server.URL
	cfg.Settings["plex_token"] = "token"
	RefreshMediaLibraries(&cfg)
	got := requests()
	if len(got) != 1 || got[0].Path != "/library/sections/all/refresh" {
		t.Fatalf("expected a single full refresh, got %+v", got)
	}
}

func TestMediaRefreshRequestCoalescesFolders(t *testing.T) {
	cfg := DefaultConfig()
	request := &mediaRefreshRequest{}
	request.add(&cfg, "/a")
	request.add(&cfg, "/b")
	if request.full || len(request.folders) != 2 {
		t.Fatalf("unexpected request: %+v", request)
	}
	request.add(&cfg, "")
	if !request.full {
		t.Fatal("an unknown folder must turn the request into a full refresh")
	}
}
