package gextto

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitJobState(t *testing.T, manager *JobManager, id string, want JobState) Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := manager.Get(id)
		if ok && job.State == want {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	job, _ := manager.Get(id)
	t.Fatalf("job %s state = %q, want %q", id, job.State, want)
	return Job{}
}

func TestJobManagerLifecycle(t *testing.T) {
	manager := NewJobManager(1, 10)
	defer manager.Close()

	job, created := manager.Create("scan", "")
	if !created {
		t.Fatal("expected a new job")
	}
	if job.State != JobQueued {
		t.Fatalf("initial state = %q, want queued", job.State)
	}

	done := make(chan struct{})
	if err := manager.Start(job.ID, func(ctx context.Context, id string) {
		manager.SetProgress(id, 0.5, "half")
		close(done)
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	<-done

	final := waitJobState(t, manager, job.ID, JobSucceeded)
	if final.Progress != 1 {
		t.Fatalf("progress = %v, want 1", final.Progress)
	}
	if final.StartedAt == nil || final.FinishedAt == nil {
		t.Fatal("expected started_at and finished_at to be set")
	}
	if _, ok := manager.Get("missing"); ok {
		t.Fatal("unknown job should not be found")
	}
}

func TestJobManagerDeduplicatesActiveJobs(t *testing.T) {
	manager := NewJobManager(1, 10)
	defer manager.Close()

	release := make(chan struct{})
	first, created := manager.Create("rename", "rename:all")
	if !created {
		t.Fatal("expected first create to succeed")
	}
	if err := manager.Start(first.ID, func(ctx context.Context, id string) {
		select {
		case <-release:
		case <-ctx.Done():
		}
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitJobState(t, manager, first.ID, JobRunning)

	duplicate, created := manager.Create("rename", "rename:all")
	if created {
		t.Fatal("duplicate create should reuse the active job")
	}
	if duplicate.ID != first.ID {
		t.Fatalf("duplicate id = %q, want %q", duplicate.ID, first.ID)
	}

	close(release)
	waitJobState(t, manager, first.ID, JobSucceeded)

	next, created := manager.Create("rename", "rename:all")
	if !created || next.ID == first.ID {
		t.Fatal("a new job should be created after the previous one finished")
	}
}

func TestJobManagerCancelRunningJob(t *testing.T) {
	manager := NewJobManager(1, 10)
	defer manager.Close()

	job, _ := manager.Create("scan", "")
	started := make(chan struct{})
	if err := manager.Start(job.ID, func(ctx context.Context, id string) {
		close(started)
		<-ctx.Done()
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	<-started
	if !manager.Cancel(job.ID) {
		t.Fatal("cancel reported false for a running job")
	}
	final := waitJobState(t, manager, job.ID, JobCanceled)
	if final.Error == "" {
		t.Fatal("canceled job should carry the context error")
	}
	if manager.Cancel(job.ID) {
		t.Fatal("a finished job must not be cancellable")
	}
}

func TestJobManagerCancelQueuedJob(t *testing.T) {
	manager := NewJobManager(1, 10)
	defer manager.Close()

	release := make(chan struct{})
	first, _ := manager.Create("scan", "")
	if err := manager.Start(first.ID, func(ctx context.Context, id string) {
		select {
		case <-release:
		case <-ctx.Done():
		}
	}); err != nil {
		t.Fatalf("start first: %v", err)
	}
	waitJobState(t, manager, first.ID, JobRunning)

	second, _ := manager.Create("scan", "")
	ran := atomic.Bool{}
	if err := manager.Start(second.ID, func(ctx context.Context, id string) { ran.Store(true) }); err != nil {
		t.Fatalf("start second: %v", err)
	}
	if !manager.Cancel(second.ID) {
		t.Fatal("cancel reported false for a queued job")
	}
	waitJobState(t, manager, second.ID, JobCanceled)
	if ran.Load() {
		t.Fatal("canceled queued job must not run")
	}
	close(release)
}

func TestJobManagerBoundedConcurrency(t *testing.T) {
	manager := NewJobManager(1, 10)
	defer manager.Close()

	var current, peak int32
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)
	for i := 0; i < 3; i++ {
		job, _ := manager.Create("scan", "")
		if err := manager.Start(job.ID, func(ctx context.Context, id string) {
			active := atomic.AddInt32(&current, 1)
			for {
				observed := atomic.LoadInt32(&peak)
				if active <= observed || atomic.CompareAndSwapInt32(&peak, observed, active) {
					break
				}
			}
			select {
			case started <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-ctx.Done():
			}
			atomic.AddInt32(&current, -1)
			wg.Done()
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
	}
	// Wait until at least one job is actually running, then verify the single
	// concurrency slot was respected. No sleep-based guessing.
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("no job started")
	}
	peakOK := atomic.LoadInt32(&peak) == 1
	close(release)
	wg.Wait()
	if !peakOK {
		t.Fatalf("peak concurrency = %d, want 1", atomic.LoadInt32(&peak))
	}
}

func TestJobManagerRetentionKeepsRecentJobs(t *testing.T) {
	manager := NewJobManager(2, 2)
	defer manager.Close()

	var ids []string
	for i := 0; i < 5; i++ {
		job, _ := manager.Create("scan", "")
		ids = append(ids, job.ID)
		if err := manager.Start(job.ID, func(ctx context.Context, id string) {}); err != nil {
			t.Fatalf("start: %v", err)
		}
		waitJobState(t, manager, job.ID, JobSucceeded)
	}

	jobs := manager.List("", "")
	if len(jobs) != 2 {
		t.Fatalf("retained jobs = %d, want 2", len(jobs))
	}
	if _, ok := manager.Get(ids[0]); ok {
		t.Fatal("oldest job should have been evicted")
	}
	if _, ok := manager.Get(ids[4]); !ok {
		t.Fatal("newest job should still be retained")
	}
}

func TestJobManagerPanicBecomesFailed(t *testing.T) {
	manager := NewJobManager(1, 10)
	defer manager.Close()

	job, _ := manager.Create("scan", "")
	if err := manager.Start(job.ID, func(ctx context.Context, id string) {
		panic("boom")
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	final := waitJobState(t, manager, job.ID, JobFailed)
	if final.Error == "" {
		t.Fatal("failed job should record the panic")
	}
}

func TestJobManagerFailRecordsLateError(t *testing.T) {
	manager := NewJobManager(1, 10)
	defer manager.Close()

	job, _ := manager.Create("scan", "")
	if err := manager.Start(job.ID, func(ctx context.Context, id string) {
		manager.Fail(id, errors.New("late failure"))
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	final := waitJobState(t, manager, job.ID, JobFailed)
	if final.Error != "late failure" {
		t.Fatalf("job error = %q, want %q", final.Error, "late failure")
	}
}

func TestJobsEndpointsExposeAndCancel(t *testing.T) {
	state := newTestAppState(t)
	t.Cleanup(state.jobs.Close)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	job, created := state.jobs.Create("demo", "")
	if !created {
		t.Fatal("expected a new job")
	}
	if err := state.jobs.Start(job.ID, func(ctx context.Context, id string) { <-ctx.Done() }); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitJobState(t, state.jobs, job.ID, JobRunning)

	status, _, body := webGet(t, server, "/api/jobs")
	if status != http.StatusOK {
		t.Fatalf("GET /api/jobs = %d: %s", status, body)
	}
	var listed struct {
		Jobs []Job `json:"jobs"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Jobs) != 1 || listed.Jobs[0].ID != job.ID {
		t.Fatalf("unexpected job list: %s", body)
	}

	status, _, body = webGet(t, server, "/api/jobs/"+job.ID)
	if status != http.StatusOK {
		t.Fatalf("GET /api/jobs/{id} = %d: %s", status, body)
	}
	var single struct {
		Job Job `json:"job"`
	}
	if err := json.Unmarshal(body, &single); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if single.Job.ID != job.ID {
		t.Fatalf("job id = %q, want %q", single.Job.ID, job.ID)
	}

	status, _, _ = webGet(t, server, "/api/jobs/does-not-exist")
	if status != http.StatusNotFound {
		t.Fatalf("unknown job status = %d, want 404", status)
	}

	response, err := http.Post(server.URL+"/api/jobs/"+job.ID+"/cancel", "application/json", nil)
	if err != nil {
		t.Fatalf("POST cancel: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("cancel status = %d, want 202", response.StatusCode)
	}
	waitJobState(t, state.jobs, job.ID, JobCanceled)
}

func TestJobManagerCloseCancelsAndRejectsNewJobs(t *testing.T) {
	manager := NewJobManager(1, 10)

	job, _ := manager.Create("scan", "")
	started := make(chan struct{})
	if err := manager.Start(job.ID, func(ctx context.Context, id string) {
		close(started)
		<-ctx.Done()
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	<-started
	manager.Close()

	if _, created := manager.Create("scan", ""); created {
		t.Fatal("closed manager must not accept new jobs")
	}
	if err := manager.Start(job.ID, func(ctx context.Context, id string) {}); err == nil {
		t.Fatal("closed manager must reject Start")
	}
}

func TestRenameAllUsesJobManager(t *testing.T) {
	state := newTestAppState(t)
	t.Cleanup(state.jobs.Close)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	if status, body := webPostJSON(t, server, "/api/config/settings", `{"key":"rename_episodes","value":"1"}`); status != http.StatusOK {
		t.Fatalf("enable rename = %d: %s", status, body)
	}

	status, body := webPostJSON(t, server, "/api/rename-all", "{}")
	if status != http.StatusAccepted {
		t.Fatalf("rename-all = %d: %s", status, body)
	}
	var accepted struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(body, &accepted); err != nil {
		t.Fatalf("decode rename-all response: %v (%s)", err, body)
	}
	if accepted.JobID == "" {
		t.Fatalf("rename-all did not return a job id: %s", body)
	}
	waitJobState(t, state.jobs, accepted.JobID, JobSucceeded)

	// The legacy progress endpoint must still reflect the final state.
	status, _, progress := webGet(t, server, "/api/rename-progress")
	if status != http.StatusOK {
		t.Fatalf("rename-progress = %d", status)
	}
	var view struct {
		Progress struct {
			Running bool   `json:"running"`
			Message string `json:"message"`
		} `json:"progress"`
	}
	if err := json.Unmarshal(progress, &view); err != nil {
		t.Fatalf("decode rename progress: %v", err)
	}
	if view.Progress.Running || view.Progress.Message != "completed" {
		t.Fatalf("rename progress after completion = %+v", view.Progress)
	}
}

func TestRenameAllDeduplicatesActiveJob(t *testing.T) {
	state := newTestAppState(t)
	t.Cleanup(state.jobs.Close)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	if status, body := webPostJSON(t, server, "/api/config/settings", `{"key":"rename_episodes","value":"1"}`); status != http.StatusOK {
		t.Fatalf("enable rename = %d: %s", status, body)
	}

	// Occupy the de-duplication key with a job that never finishes on its own.
	blocking, created := state.jobs.Create("rename-all", "rename-all")
	if !created {
		t.Fatal("expected to create the blocking job")
	}
	if err := state.jobs.Start(blocking.ID, func(ctx context.Context, id string) { <-ctx.Done() }); err != nil {
		t.Fatalf("start blocking job: %v", err)
	}
	waitJobState(t, state.jobs, blocking.ID, JobRunning)

	status, body := webPostJSON(t, server, "/api/rename-all", "{}")
	if status != http.StatusConflict {
		t.Fatalf("second rename-all = %d, want 409: %s", status, body)
	}
}

func TestScanAllArchivesRunsAsJob(t *testing.T) {
	state := newTestAppState(t)
	t.Cleanup(state.jobs.Close)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, body := webPostJSON(t, server, "/api/scan-all-archives", "{}")
	if status != http.StatusAccepted {
		t.Fatalf("scan-all-archives = %d: %s", status, body)
	}
	var accepted struct {
		JobID string `json:"job_id"`
		Total int    `json:"total"`
	}
	if err := json.Unmarshal(body, &accepted); err != nil {
		t.Fatalf("decode scan response: %v (%s)", err, body)
	}
	if accepted.JobID == "" {
		t.Fatalf("scan did not return a job id: %s", body)
	}
	waitJobState(t, state.jobs, accepted.JobID, JobSucceeded)
}

func TestScanAllArchivesDeduplicatesActiveJob(t *testing.T) {
	state := newTestAppState(t)
	t.Cleanup(state.jobs.Close)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	blocking, created := state.jobs.Create("scan-archives", "scan-archives")
	if !created {
		t.Fatal("expected to create the blocking job")
	}
	if err := state.jobs.Start(blocking.ID, func(ctx context.Context, id string) { <-ctx.Done() }); err != nil {
		t.Fatalf("start blocking job: %v", err)
	}
	waitJobState(t, state.jobs, blocking.ID, JobRunning)

	status, body := webPostJSON(t, server, "/api/scan-all-archives", "{}")
	if status != http.StatusConflict {
		t.Fatalf("second scan-all-archives = %d, want 409: %s", status, body)
	}
}

func TestJobManagerSetResultIsObservable(t *testing.T) {
	manager := NewJobManager(1, 10)
	defer manager.Close()

	job, _ := manager.Create("scan", "")
	if err := manager.Start(job.ID, func(ctx context.Context, id string) {
		manager.SetResult(id, map[string]any{"probed": 3, "failed": 1})
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	final := waitJobState(t, manager, job.ID, JobSucceeded)
	result, ok := final.Result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", final.Result)
	}
	if result["probed"] != 3 || result["failed"] != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestBackfillMediaInfoRunsAsJob(t *testing.T) {
	state := newTestAppState(t)
	t.Cleanup(state.jobs.Close)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	status, body := webPostJSON(t, server, "/api/maintenance/backfill-media-info", "{}")
	if status != http.StatusAccepted {
		t.Fatalf("backfill-media-info = %d: %s", status, body)
	}
	var accepted struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(body, &accepted); err != nil {
		t.Fatalf("decode backfill response: %v (%s)", err, body)
	}
	if accepted.JobID == "" {
		t.Fatalf("backfill did not return a job id: %s", body)
	}
	final := waitJobState(t, state.jobs, accepted.JobID, JobSucceeded)
	if final.Result == nil {
		t.Fatal("backfill job should expose its report as result")
	}
}

func TestBackfillMediaInfoDeduplicatesActiveJob(t *testing.T) {
	state := newTestAppState(t)
	t.Cleanup(state.jobs.Close)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	blocking, created := state.jobs.Create("media-info-backfill", "media-info-backfill")
	if !created {
		t.Fatal("expected to create the blocking job")
	}
	if err := state.jobs.Start(blocking.ID, func(ctx context.Context, id string) { <-ctx.Done() }); err != nil {
		t.Fatalf("start blocking job: %v", err)
	}
	waitJobState(t, state.jobs, blocking.ID, JobRunning)

	status, body := webPostJSON(t, server, "/api/maintenance/backfill-media-info", "{}")
	if status != http.StatusConflict {
		t.Fatalf("second backfill = %d, want 409: %s", status, body)
	}
}
