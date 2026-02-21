package web

import (
	"context"
	"sync"
	"time"
)

type JobState string

const (
	JobRunning   JobState = "running"
	JobCompleted JobState = "completed"
	JobFailed    JobState = "failed"
)

type JobStatus struct {
	State     JobState
	Message   string
	StartedAt time.Time
}

type JobRunner struct {
	mu   sync.Mutex
	jobs map[string]*JobStatus
}

func NewJobRunner() *JobRunner {
	return &JobRunner{
		jobs: make(map[string]*JobStatus),
	}
}

// Start launches fn in a background goroutine with a detached context.
// Returns false if a job with the same key is already running.
func (jr *JobRunner) Start(key string, fn func(ctx context.Context) (string, error)) bool {
	jr.mu.Lock()
	defer jr.mu.Unlock()

	if existing, ok := jr.jobs[key]; ok && existing.State == JobRunning {
		return false
	}

	jr.jobs[key] = &JobStatus{
		State:     JobRunning,
		StartedAt: time.Now(),
	}

	go func() {
		msg, err := fn(context.Background())

		jr.mu.Lock()
		defer jr.mu.Unlock()

		if err != nil {
			jr.jobs[key] = &JobStatus{
				State:     JobFailed,
				Message:   err.Error(),
				StartedAt: jr.jobs[key].StartedAt,
			}
		} else {
			jr.jobs[key] = &JobStatus{
				State:     JobCompleted,
				Message:   msg,
				StartedAt: jr.jobs[key].StartedAt,
			}
		}
	}()

	return true
}

// Status returns the current status of a job, or nil if no job exists for the key.
func (jr *JobRunner) Status(key string) *JobStatus {
	jr.mu.Lock()
	defer jr.mu.Unlock()
	return jr.jobs[key]
}
