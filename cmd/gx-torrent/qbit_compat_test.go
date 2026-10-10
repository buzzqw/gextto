package main

// qbit_compat_test.go drives the standalone daemon with Gextto's own
// qBittorrent client (internal/qbittorrent), so the compatibility with Sonarr,
// Radarr and the rest is proven against a real client implementation rather
// than hand-written requests.

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/buzzqw/gextto/internal/qbittorrent"
)

func TestQbitCompatibleWithGexttoClient(t *testing.T) {
	d := standaloneTestDaemon(t, "s3cret", true)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	client, err := qbittorrent.New(qbittorrent.Config{BaseURL: srv.URL, Username: "admin", Password: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := client.Login(ctx); err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := client.AppVersion(ctx); err != nil {
		t.Fatalf("app version: %v", err)
	}
	if _, err := client.WebAPIVersion(ctx); err != nil {
		t.Fatalf("webapi version: %v", err)
	}
	list, err := client.Torrents(ctx)
	if err != nil {
		t.Fatalf("torrents: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected an empty list, got %d", len(list))
	}
	if _, err := client.TransferInfo(ctx); err != nil {
		t.Fatalf("transfer info: %v", err)
	}
	if err := client.CreateCategory(ctx, "movies", "/srv/movies"); err != nil {
		t.Fatalf("createCategory: %v", err)
	}
	if err := client.SetCategory(ctx, "movies", "abc"); err != nil {
		t.Fatalf("setCategory: %v", err)
	}
	if err := client.AddTags(ctx, "sonarr", "abc"); err != nil {
		t.Fatalf("addTags: %v", err)
	}
	if err := client.Pause(ctx, "abc"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := client.Resume(ctx, "abc"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if err := client.Recheck(ctx, "abc"); err != nil {
		t.Fatalf("recheck: %v", err)
	}
	if err := client.Reannounce(ctx, "abc"); err != nil {
		t.Fatalf("reannounce: %v", err)
	}
	if err := client.SetLocation(ctx, "/srv/movies", "abc"); err != nil {
		t.Fatalf("setLocation: %v", err)
	}
	if err := client.Delete(ctx, false, "abc"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}
