package amule_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/amule"
	"github.com/buzzqw/gextto/internal/amule/amuletest"
)

func TestClientAgainstFakeCore(t *testing.T) {
	core, err := amuletest.Start("secret")
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	ctx := context.Background()
	client := amule.New(amule.Config{Address: core.Address(), Password: "secret"})
	defer client.Close()

	status, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Version != "2.3.3" || !status.Ed2kConnected || !status.HighID || status.ServerName != "Fake Server" ||
		status.ServerAddress != "10.0.0.1:4717" || !status.KadConnected || status.DownloadRate != 2048 {
		t.Fatalf("status = %+v", status)
	}
	servers, err := client.Servers(ctx)
	if err != nil || len(servers) != 1 || !servers[0].Connected || servers[0].Users != 5000 {
		t.Fatalf("servers = %+v, %v", servers, err)
	}
	if err := client.ConnectServer(ctx, "10.0.0.1:4717"); err != nil {
		t.Fatal(err)
	}
	if err := client.ConnectServer(ctx, "not-an-address"); err == nil {
		t.Fatal("a bad server address was accepted")
	}
	if err := client.StopKad(ctx); err != nil {
		t.Fatal(err)
	}
	if got := core.Commands(); len(got) != 2 || got[0] != "server_connect" || got[1] != "kad_stop" {
		t.Fatalf("commands = %v", got)
	}

	core.SetResults([]amuletest.Result{
		{Hash: "11111111111111111111111111111111", Name: "few.mkv", Size: 10, Sources: 1},
		{Hash: "22222222222222222222222222222222", Name: "many.mkv", Size: 20, Sources: 9},
	}, "")
	results, err := client.Search(ctx, "query", amule.SearchOptions{Type: amule.SearchKad, Wait: 5 * time.Second, Poll: 10 * time.Millisecond})
	if err != nil || len(results) != 2 || results[0].Name != "many.mkv" {
		t.Fatalf("results = %+v, %v", results, err)
	}
	core.SetResults(nil, "Kad search can't be done if Kad is not running")
	if _, err := client.Search(ctx, "query", amule.SearchOptions{Type: amule.SearchKad}); !errors.Is(err, amule.ErrSearchRefused) {
		t.Fatalf("err = %v", err)
	}

	wrong := amule.New(amule.Config{Address: core.Address(), Password: "wrong"})
	defer wrong.Close()
	if err := wrong.Ping(ctx); !errors.Is(err, amule.ErrAuth) {
		t.Fatalf("wrong password: %v", err)
	}
	// A core that went away is reported, not hidden.
	core.Close()
	if err := client.Ping(ctx); err == nil {
		t.Fatal("a closed core still answered")
	}
}
