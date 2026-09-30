package gextto

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// JobState is the lifecycle state of a background job tracked by JobManager.
type JobState string

const (
	JobQueued    JobState = "queued"
	JobRunning   JobState = "running"
	JobSucceeded JobState = "succeeded"
	JobFailed    JobState = "failed"
	JobCanceled  JobState = "canceled"
)

// Job is the observable snapshot of one background operation. It is designed to
// be serialised to the UI: see `docs/revisione-1.md`, section 7.
type Job struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	State      JobState   `json:"state"`
	Progress   float64    `json:"progress"`
	Message    string     `json:"message"`
	Error      string     `json:"error,omitempty"`
	Result     any        `json:"result,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`

	cancel context.CancelFunc
}

func (j Job) finished() bool {
	switch j.State {
	case JobSucceeded, JobFailed, JobCanceled:
		return true
	default:
		return false
	}
}

// JobFunc is the body of a job. It must return promptly when ctx is canceled and
// must not keep references to the JobManager after it returns. Progress and
// messages are reported through SetProgress so the manager stays the single
// owner of the job state.
type JobFunc func(ctx context.Context, id string)

// JobManager owns the lifecycle of background jobs: bounded concurrency,
// de-duplication of equivalent operations, retention of the most recent
// completed jobs and ordered shutdown. Producers register work with Create +
// Start; consumers read List/Get and stop work with Cancel.
//
// This is the phase-3 foundation of the technical review. Rename-all,
// scan-archives and media-info-backfill are already ported; each ported
// operation keeps its visible HTTP contract (or documents the change) and
// observes cancellation at safe points.
type JobManager struct {
	mu        sync.Mutex
	jobs      map[string]*Job
	order     []string
	active    map[string]string // deduplication key -> active job id
	slots     chan struct{}
	retention int
	nextID    uint64
	ctx       context.Context
	cancelAll context.CancelFunc
	wg        sync.WaitGroup
	closed    bool
}

// NewJobManager builds a manager with the given concurrency limit and retention
// of completed jobs. Non-positive values fall back to safe defaults.
func NewJobManager(maxConcurrent, retention int) *JobManager {
	if maxConcurrent <= 0 {
		maxConcurrent = 2
	}
	if retention <= 0 {
		retention = 50
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &JobManager{
		jobs:      make(map[string]*Job),
		active:    make(map[string]string),
		slots:     make(chan struct{}, maxConcurrent),
		retention: retention,
		ctx:       ctx,
		cancelAll: cancel,
	}
}

// Create registers a queued job. When dedupKey is non-empty and an equivalent
// job is still queued or running, the existing job is returned with created
// false, so callers cannot start two identical operations by mistake.
func (m *JobManager) Create(kind, dedupKey string) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, false
	}
	if dedupKey != "" {
		if id, ok := m.active[dedupKey]; ok {
			if job, ok := m.jobs[id]; ok && !job.finished() {
				return copyJob(job), false
			}
		}
	}
	m.nextID++
	id := fmt.Sprintf("job-%d-%d", time.Now().UnixNano(), m.nextID)
	job := &Job{
		ID:        id,
		Kind:      kind,
		State:     JobQueued,
		Message:   "queued",
		CreatedAt: time.Now(),
	}
	m.jobs[id] = job
	m.order = append(m.order, id)
	if dedupKey != "" {
		m.active[dedupKey] = id
	}
	return copyJob(job), true
}

// Start schedules fn for execution. It returns an error when the id is unknown,
// already started or the manager is closing. The goroutine is tracked so Close
// can wait for it.
func (m *JobManager) Start(id string, fn JobFunc) error {
	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return errors.New("unknown job")
	}
	if m.closed {
		m.mu.Unlock()
		return errors.New("job manager is closing")
	}
	if job.State != JobQueued {
		m.mu.Unlock()
		return errors.New("job already started")
	}
	ctx, cancel := context.WithCancel(m.ctx)
	job.cancel = cancel
	m.wg.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.wg.Done()
		m.run(ctx, id, fn)
	}()
	return nil
}

func (m *JobManager) run(ctx context.Context, id string, fn JobFunc) {
	// Bounded concurrency: block here while every slot is busy. A job canceled
	// while queued never starts.
	select {
	case m.slots <- struct{}{}:
	case <-ctx.Done():
		m.finish(id, JobCanceled, "canceled", ctx.Err())
		return
	}
	defer func() { <-m.slots }()

	m.mu.Lock()
	job, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	if ctx.Err() != nil {
		m.mu.Unlock()
		m.finish(id, JobCanceled, "canceled", ctx.Err())
		return
	}
	now := time.Now()
	job.State = JobRunning
	job.StartedAt = &now
	job.Message = "running"
	m.mu.Unlock()

	defer func() {
		if recovered := recover(); recovered != nil {
			m.finish(id, JobFailed, "", fmt.Errorf("panic: %v", recovered))
		}
	}()

	fn(ctx, id)

	if ctx.Err() != nil {
		m.finish(id, JobCanceled, "canceled", ctx.Err())
		return
	}
	m.finish(id, JobSucceeded, "done", nil)
}

// finish records the terminal state of a job, releases its deduplication key,
// cancels its context and trims the retained history.
func (m *JobManager) finish(id string, state JobState, message string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok || job.finished() {
		return
	}
	now := time.Now()
	job.State = state
	job.FinishedAt = &now
	if message != "" {
		job.Message = message
	}
	if err != nil {
		job.Error = err.Error()
	}
	if state == JobSucceeded {
		job.Progress = 1
	}
	if job.cancel != nil {
		job.cancel()
		job.cancel = nil
	}
	for key, activeID := range m.active {
		if activeID == id {
			delete(m.active, key)
		}
	}
	m.trimLocked()
}

// trimLocked drops the oldest completed jobs beyond the retention limit. The
// caller must hold m.mu.
func (m *JobManager) trimLocked() {
	finished := 0
	for _, id := range m.order {
		if job, ok := m.jobs[id]; ok && job.finished() {
			finished++
		}
	}
	if finished <= m.retention {
		return
	}
	drop := finished - m.retention
	kept := m.order[:0]
	for _, id := range m.order {
		if drop > 0 {
			if job, ok := m.jobs[id]; ok && job.finished() {
				delete(m.jobs, id)
				drop--
				continue
			}
		}
		kept = append(kept, id)
	}
	m.order = kept
}

// SetProgress updates the observable progress of a job still in flight.
func (m *JobManager) SetProgress(id string, progress float64, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok || job.finished() {
		return
	}
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	job.Progress = progress
	if message != "" {
		job.Message = message
	}
}

// Get returns a snapshot of a job.
func (m *JobManager) Get(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *copyJob(job), true
}

// List returns job snapshots filtered by kind and state. Empty filters match
// everything. Jobs are ordered newest first.
func (m *JobManager) List(kind string, state JobState) []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]Job, 0, len(m.jobs))
	for _, id := range m.order {
		job, ok := m.jobs[id]
		if !ok {
			continue
		}
		if kind != "" && job.Kind != kind {
			continue
		}
		if state != "" && job.State != state {
			continue
		}
		result = append(result, *copyJob(job))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

// Cancel requests cancellation of a queued or running job. It reports whether
// the job existed and was still cancellable.
func (m *JobManager) Cancel(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok || job.finished() || job.cancel == nil {
		return false
	}
	job.Message = "canceling"
	job.cancel()
	return true
}

// Fail marks a job as failed from inside its function, so a late error is
// observable in the job state and not only in the logs. It is a no-op when the
// job already reached a terminal state.
func (m *JobManager) Fail(id string, err error) {
	m.finish(id, JobFailed, "", err)
}

// SetResult stores a structured outcome on a job so callers that cannot wait for
// the HTTP response (the operation now runs in the background) can still read
// the detailed report from /api/jobs/{id}.
func (m *JobManager) SetResult(id string, result any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return
	}
	job.Result = result
}

// Close stops accepting jobs, cancels every active job and waits for the
// running goroutines. Producers must return when their context is canceled, or
// Close blocks.
func (m *JobManager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.cancelAll()
	m.mu.Unlock()
	m.wg.Wait()
	logging.Debug("job manager stopped")
}

// copyJob returns a copy safe to hand out: the internal cancel function never
// leaves the manager.
func copyJob(job *Job) *Job {
	clone := *job
	clone.cancel = nil
	return &clone
}
