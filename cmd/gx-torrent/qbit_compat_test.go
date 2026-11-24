package main

// qbit_compat_test.go drives the standalone daemon with Gextto's own
// qBittorrent client (internal/qbittorrent), so the compatibility with Sonarr,
// Radarr and the rest is proven against a real client implementation rather
// than hand-written requests.

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/auth"
	"github.com/buzzqw/gextto/internal/qbittorrent"
	"github.com/buzzqw/gextto/internal/settings"
)

// TestQbitArrDownloadClientSequences pins the sequence the *arr download
// clients use when gx-torrent is configured as a qBittorrent download client.
// All of them are API-v2 clients: Readarr versions that only speak the legacy
// qBittorrent v1 API are not supported because /api/v1 is gx-torrent's native,
// frozen API and cannot be reused without two masters on the same queue.
func TestQbitArrDownloadClientSequences(t *testing.T) {
	cases := []struct {
		name     string
		category string
		tag      string
		hash     string
	}{
		{"sonarr", "sonarr-tv", "sonarr", "ABCDEF0123456789ABCDEF0123456789ABCDEF01"},
		{"radarr", "radarr-movies", "radarr", "ABCDEF0123456789ABCDEF0123456789ABCDEF02"},
		{"lidarr", "lidarr-music", "lidarr", "ABCDEF0123456789ABCDEF0123456789ABCDEF03"},
		{"readarr", "readarr-books", "readarr", "ABCDEF0123456789ABCDEF0123456789ABCDEF04"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, err := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
			if err != nil {
				t.Fatal(err)
			}
			passwordHash, err := auth.HashPassword("s3cret")
			if err != nil {
				t.Fatal(err)
			}
			// The mode belongs in the Options: the background loops read it for
			// their whole life and must never see a standalone daemon in managed.
			d := newTestDaemonFullMode(t, NetworkOptions{}, store, ModeStandalone)
			d.opts.Settings.Set(standalonePasswordKey, passwordHash)
			d.opts.Settings.Set(standaloneBypassKey, "true")
			d.sessions = auth.NewSessions(time.Hour)
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
			version, err := client.AppVersion(ctx)
			if err != nil || version == "" {
				t.Fatalf("app version = %q, err = %v", version, err)
			}
			api, err := client.WebAPIVersion(ctx)
			if err != nil || api == "" {
				t.Fatalf("webapi version = %q, err = %v", api, err)
			}
			savePath := t.TempDir()
			if err := client.CreateCategory(ctx, tc.category, savePath); err != nil {
				t.Fatalf("create category: %v", err)
			}
			magnet := "magnet:?xt=urn:btih:" + tc.hash + "&dn=release"
			hash, err := client.AddMagnet(ctx, magnet, qbittorrent.AddOptions{
				Category: tc.category,
				SavePath: savePath,
				Tags:     tc.tag,
				Paused:   true,
				// Add-time limits keep their minute precision too.
				SeedingTimeLimit:         int64Ptr(45),
				InactiveSeedingTimeLimit: int64Ptr(15),
			})
			if err != nil {
				t.Fatalf("add magnet: %v", err)
			}
			if hash == "" {
				t.Fatal("add magnet returned no hash")
			}
			// The client then sets share limits when its quality profile asks
			// for them; the round trip must not round minutes up to days.
			if err := client.SetShareLimits(ctx, 1.25, 30, 20, hash); err != nil {
				t.Fatalf("set share limits: %v", err)
			}
			list, err := client.Torrents(ctx)
			if err != nil {
				t.Fatalf("torrents: %v", err)
			}
			var found qbittorrent.Torrent
			for _, item := range list {
				if item.Hash == hash {
					found = item
					break
				}
			}
			if found.Hash == "" {
				t.Fatalf("torrent %s not found in torrents/info: %+v", hash, list)
			}
			if found.Category != tc.category || found.SavePath != savePath {
				t.Fatalf("category/save path = %q/%q, want %q/%s", found.Category, found.SavePath, tc.category, savePath)
			}
			if found.SeedingLimit != 30 || found.InactiveLimit != 20 {
				t.Fatalf("seed limits = %d/%d, want 30/20", found.SeedingLimit, found.InactiveLimit)
			}
			if _, err := client.Sync(ctx, 0); err != nil {
				t.Fatalf("sync/maindata: %v", err)
			}
			if err := client.Delete(ctx, false, hash); err != nil {
				t.Fatalf("delete: %v", err)
			}
		})
	}
}

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
	if err := client.SetGlobalDownloadLimit(ctx, 1<<20); err != nil {
		t.Fatalf("set global download limit: %v", err)
	}
	if err := client.SetGlobalUploadLimit(ctx, 1<<19); err != nil {
		t.Fatalf("set global upload limit: %v", err)
	}
	if _, err := client.Sync(ctx, 0); err != nil {
		t.Fatalf("sync: %v", err)
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
	if err := client.AddTrackers(ctx, "abc", []string{"http://a/announce", "http://b/announce"}); err != nil {
		t.Fatalf("addTrackers: %v", err)
	}
	if err := client.RemoveTrackers(ctx, "abc", []string{"http://a/announce"}); err != nil {
		t.Fatalf("removeTrackers: %v", err)
	}
	if err := client.EditTracker(ctx, "abc", "http://b/announce", "http://c/announce"); err != nil {
		t.Fatalf("editTracker: %v", err)
	}
	if err := client.ToggleSequentialDownload(ctx, "abc"); err != nil {
		t.Fatalf("toggleSequentialDownload: %v", err)
	}
	if err := client.SetSuperSeeding(ctx, true, "abc"); err != nil {
		t.Fatalf("setSuperSeeding: %v", err)
	}
	if _, err := client.Peers(ctx, "abc"); err != nil {
		t.Fatalf("peers: %v", err)
	}
	if err := client.SetFilePriorities(ctx, "abc", []int{0}, 0); err != nil {
		t.Fatalf("filePrio: %v", err)
	}
	if err := client.SetForceStart(ctx, true, "abc"); err != nil {
		t.Fatalf("setForceStart: %v", err)
	}
	if err := client.SetAutoManagement(ctx, true, "abc"); err != nil {
		t.Fatalf("setAutoManagement: %v", err)
	}
	if err := client.SetTorrentDownloadLimit(ctx, 1<<20, "abc"); err != nil {
		t.Fatalf("setDownloadLimit: %v", err)
	}
	if err := client.SetTorrentUploadLimit(ctx, 1<<19, "abc"); err != nil {
		t.Fatalf("setUploadLimit: %v", err)
	}
	if err := client.SetPreferences(ctx, map[string]any{"dl_limit": 1 << 20, "queueing_enabled": false}); err != nil {
		t.Fatalf("setPreferences: %v", err)
	}
	if err := client.Delete(ctx, false, "abc"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}
