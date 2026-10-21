package amule_test

import (
	"context"
	"testing"

	"github.com/buzzqw/gextto/internal/amule"
	"github.com/buzzqw/gextto/internal/amule/amuletest"
)

func TestSharedFilesReportUploadCounters(t *testing.T) {
	core, err := amuletest.Start("secret")
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	client := amule.New(amule.Config{Address: core.Address(), Password: "secret"})
	defer client.Close()
	ctx := context.Background()

	core.SetShared([]amuletest.File{{
		Hash:           "ac9506f90a4e91273d5114f9bc1438a8",
		Name:           "Scrubs - S02E04 - My Big Mouth.mkv",
		Size:           529727691,
		Priority:       2,
		AutoPriority:   true,
		Transferred:    779238,
		TransferredAll: 2287181,
		Requests:       2,
		RequestsAll:    6,
		Accepts:        2,
		AcceptsAll:     6,
		Queued:         3,
		UploadRate:     4096,
		LastUpload:     1792603914,
	}}, "/incoming")

	files, err := client.SharedFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %+v", files)
	}
	file := files[0]
	if file.Name != "Scrubs - S02E04 - My Big Mouth.mkv" || file.Size != 529727691 || file.Dir != "/incoming" {
		t.Fatalf("file = %+v", file)
	}
	if file.Transferred != 779238 || file.TransferredAll != 2287181 {
		t.Fatalf("uploaded = %d/%d", file.Transferred, file.TransferredAll)
	}
	if file.Requests != 2 || file.RequestsAll != 6 || file.Accepts != 2 || file.AcceptsAll != 6 {
		t.Fatalf("requests/accepts = %+v", file)
	}
	if file.Queued != 3 || file.Priority != 2 || !file.AutoPriority {
		t.Fatalf("queue/priority = %+v", file)
	}
	if file.UploadRate != 4096 || file.LastUpload != 1792603914 {
		t.Fatalf("upload activity = %+v", file)
	}

	status, err := client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.SharedFiles != 1 {
		t.Fatalf("shared count = %d", status.SharedFiles)
	}
}

func TestReloadSharedFilesAndSharedDirs(t *testing.T) {
	core, err := amuletest.Start("secret")
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	client := amule.New(amule.Config{Address: core.Address(), Password: "secret"})
	defer client.Close()
	ctx := context.Background()

	if err := client.ReloadSharedFiles(ctx); err != nil {
		t.Fatal(err)
	}
	commands := core.Commands()
	if len(commands) != 1 || commands[0] != "reload-shared" {
		t.Fatalf("commands = %v", commands)
	}

	core.SetSharedDirs([]string{"/media/downloads", "/media/movies"})
	prefs, err := client.Preferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(prefs.SharedDirs) != 2 || prefs.SharedDirs[0] != "/media/downloads" || prefs.SharedDirs[1] != "/media/movies" {
		t.Fatalf("shared dirs = %v", prefs.SharedDirs)
	}
	if prefs.IncomingDir != core.Incoming {
		t.Fatalf("incoming = %q", prefs.IncomingDir)
	}
}

func TestUploadQueueListsActiveClientsFirst(t *testing.T) {
	core, err := amuletest.Start("secret")
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	client := amule.New(amule.Config{Address: core.Address(), Password: "secret"})
	defer client.Close()
	ctx := context.Background()

	core.SetUploads([]amule.UploadClient{
		{Name: "queued-peer", Software: "eMule", IP: "10.0.0.9", FileName: "b.mkv", UpSpeed: 0, WaitingPosition: 4, UploadedAll: 100},
		{Name: "fast-peer", Software: "aMule", IP: "10.0.0.7", FileName: "a.mkv", UpSpeed: 4096, Uploaded: 2048, FileID: 12},
	})
	uploads, err := client.UploadQueue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(uploads) != 2 {
		t.Fatalf("uploads = %+v", uploads)
	}
	if uploads[0].Name != "fast-peer" || uploads[0].UpSpeed != 4096 || uploads[0].FileName != "a.mkv" {
		t.Fatalf("first upload = %+v", uploads[0])
	}
	if uploads[1].Name != "queued-peer" || uploads[1].WaitingPosition != 4 {
		t.Fatalf("second upload = %+v", uploads[1])
	}
	if uploads[0].IP != "10.0.0.7" || uploads[0].Software != "aMule" || uploads[0].Uploaded != 2048 {
		t.Fatalf("upload fields = %+v", uploads[0])
	}
}

func TestSetFilePreallocationKeepsTheOtherFilePreferences(t *testing.T) {
	core, err := amuletest.Start("secret")
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	client := amule.New(amule.Config{Address: core.Address(), Password: "secret"})
	defer client.Close()
	ctx := context.Background()

	core.SetAllocateFull(true)
	prefs, err := client.Preferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !prefs.AllocateFull {
		t.Fatal("the fake core reports preallocation on")
	}

	if err := client.SetFilePreallocation(ctx, false); err != nil {
		t.Fatal(err)
	}
	if core.AllocateFull() {
		t.Fatal("preallocation is still on")
	}
	if !core.KeptIch() {
		t.Fatal("the write dropped the other file preferences")
	}
	prefs, err = client.Preferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prefs.AllocateFull {
		t.Fatal("preallocation still reported on")
	}
}

func TestStatusReportsSessionAndAllTimeTotals(t *testing.T) {
	core, err := amuletest.Start("secret")
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	client := amule.New(amule.Config{Address: core.Address(), Password: "secret"})
	defer client.Close()
	core.SetSessionTotals(1234*1024, 5678*1024)
	core.SetTotalTransferred(34989945335, 110092796292)
	core.SetShared([]amuletest.File{{Hash: "ac9506f90a4e91273d5114f9bc1438a8", Name: "a.mkv", Size: 10}}, "/incoming")
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.SessionSent != 1234*1024 || status.SessionRecv != 5678*1024 {
		t.Fatalf("session totals = %d/%d", status.SessionSent, status.SessionRecv)
	}
	if status.TotalSent != 34989945335 || status.TotalRecv != 110092796292 {
		t.Fatalf("all-time totals = %d/%d", status.TotalSent, status.TotalRecv)
	}
	if status.SharedFiles != 1 {
		t.Fatalf("shared files = %d", status.SharedFiles)
	}
}
