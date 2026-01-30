package async

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/teranos/QNTX/errors"
)

const (
	// MaxJobsLimit is the maximum number of jobs that can be queued
	MaxJobsLimit = 10000
	// SubscriberChannelBufferSize is the buffer size for subscriber channels
	SubscriberChannelBufferSize = 100
)

type Queue struct {
	store       *Store
	mu          sync.RWMutex
	subscribers []chan *Job // Channels to notify of job updates
}

// NewQueue creates a new job queue
func NewQueue(db *sql.DB) *Queue {
	return &Queue{
		store:       NewStore(db),
		subscribers: make([]chan *Job, 0),
	}
}

// Enqueue adds a new job to the queue
func (q *Queue) Enqueue(job *Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if err := q.store.CreateJob(job); err != nil {
		err = errors.Wrap(err, "failed to enqueue job")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Handler: %s", job.HandlerName))
		err = errors.WithDetail(err, fmt.Sprintf("Source: %s", job.Source))
		return err
	}

	// Notify subscribers of new job
	q.notifySubscribers(job)

	return nil
}

// Dequeue gets the next queued job and marks it as running
func (q *Queue) Dequeue() (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Get the oldest queued job
	queuedStatus := JobStatusQueued
	jobs, err := q.store.ListJobs(&queuedStatus, 1)
	if err != nil {
		err = errors.Wrap(err, "failed to get queued jobs")
		err = errors.WithDetail(err, fmt.Sprintf("Status filter: %s", queuedStatus))
		return nil, err
	}

	if len(jobs) == 0 {
		return nil, nil // No jobs available
	}

	job := jobs[0]
	job.Start()

	if err := q.store.UpdateJob(job); err != nil {
		err = errors.Wrap(err, "failed to mark job as running")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Handler: %s", job.HandlerName))
		err = errors.WithDetail(err, fmt.Sprintf("Source: %s", job.Source))
		return nil, err
	}

	// Notify subscribers of job update
	q.notifySubscribers(job)

	return job, nil
}

// GetJob retrieves a job by ID
func (q *Queue) GetJob(id string) (*Job, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.store.GetJob(id)
}

// UpdateJob updates a job's state
func (q *Queue) UpdateJob(job *Job) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if err := q.store.UpdateJob(job); err != nil {
		err = errors.Wrap(err, "failed to update job")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Handler: %s", job.HandlerName))
		err = errors.WithDetail(err, fmt.Sprintf("Status: %s", job.Status))
		return err
	}

	// Notify subscribers of job update
	q.notifySubscribers(job)

	return nil
}

// PauseJob pauses a running job
func (q *Queue) PauseJob(id string, reason string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	job, err := q.store.GetJob(id)
	if err != nil {
		err = errors.Wrapf(err, "failed to pause job %s", id)
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", id))
		err = errors.WithDetail(err, fmt.Sprintf("Pause reason: %s", reason))
		return err
	}

	if job.Status != JobStatusRunning {
		err := errors.Newf("job %s is not running (status: %s)", id, job.Status)
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", id))
		err = errors.WithDetail(err, fmt.Sprintf("Current status: %s", job.Status))
		err = errors.WithDetail(err, fmt.Sprintf("Handler: %s", job.HandlerName))
		return err
	}

	job.Pause(reason)

	if err := q.store.UpdateJob(job); err != nil {
		err = errors.Wrap(err, "failed to pause job")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Handler: %s", job.HandlerName))
		err = errors.WithDetail(err, fmt.Sprintf("Pause reason: %s", reason))
		return err
	}

	// Notify subscribers of job update
	q.notifySubscribers(job)

	return nil
}

// ResumeJob resumes a paused job
func (q *Queue) ResumeJob(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	job, err := q.store.GetJob(id)
	if err != nil {
		err = errors.Wrapf(err, "failed to resume job %s", id)
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", id))
		return err
	}

	if job.Status != JobStatusPaused {
		err := errors.Newf("job %s is not paused (status: %s)", id, job.Status)
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", id))
		err = errors.WithDetail(err, fmt.Sprintf("Current status: %s", job.Status))
		err = errors.WithDetail(err, fmt.Sprintf("Handler: %s", job.HandlerName))
		return err
	}

	job.Resume()

	if err := q.store.UpdateJob(job); err != nil {
		err = errors.Wrap(err, "failed to resume job")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Handler: %s", job.HandlerName))
		return err
	}

	// Notify subscribers of job update
	q.notifySubscribers(job)

	return nil
}

// CompleteJob marks a job as completed and cancels any orphaned children
func (q *Queue) CompleteJob(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	job, err := q.store.GetJob(id)
	if err != nil {
		err = errors.Wrapf(err, "failed to complete job %s", id)
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", id))
		return err
	}

	job.Complete()

	if err := q.store.UpdateJob(job); err != nil {
		err = errors.Wrap(err, "failed to complete job")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Handler: %s", job.HandlerName))
		err = errors.WithDetail(err, fmt.Sprintf("Source: %s", job.Source))
		return err
	}

	// Notify subscribers of job update
	q.notifySubscribers(job)

	// DO NOT cancel children on successful completion - child jobs are the output of parent jobs
	// (e.g., vacancies scraper creates JD ingestion jobs that should continue processing)
	// Children are only cancelled when parent is explicitly deleted by user (see DeleteJobWithChildren)

	return nil
}

// FailJob marks a job as failed with an error and cancels any orphaned children
func (q *Queue) FailJob(id string, jobErr error) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	job, err := q.store.GetJob(id)
	if err != nil {
		err = errors.Wrapf(err, "failed to mark job %s as failed", id)
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", id))
		return err
	}

	job.Fail(jobErr)

	if err := q.store.UpdateJob(job); err != nil {
		err = errors.Wrap(err, "failed to mark job as failed")
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", job.ID))
		err = errors.WithDetail(err, fmt.Sprintf("Handler: %s", job.HandlerName))
		err = errors.WithDetail(err, fmt.Sprintf("Source: %s", job.Source))
		err = errors.WithDetail(err, fmt.Sprintf("Job error: %s", jobErr.Error()))
		return err
	}

	// Notify subscribers of job update
	q.notifySubscribers(job)

	// DO NOT cancel children on failure - child jobs are independent work that should continue
	// (e.g., if vacancies scraper fails after creating some JD jobs, those jobs should still process)
	// Children are only cancelled when parent is explicitly deleted by user (see DeleteJobWithChildren)

	return nil
}

// DeleteJobWithChildren deletes a job and cancels/fails all its child tasks
// This ensures that when a parent job is deleted, all associated scoring tasks are stopped
func (q *Queue) DeleteJobWithChildren(jobID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Get all child tasks for this parent job
	children, err := q.store.ListTasksByParent(jobID)
	if err != nil {
		err = errors.Wrapf(err, "failed to list child tasks for job %s", jobID)
		err = errors.WithDetail(err, fmt.Sprintf("Parent job ID: %s", jobID))
		return err
	}

	// Cancel each child task based on its current status
	for _, child := range children {
		switch child.Status {
		case JobStatusQueued:
			// Cancel queued tasks immediately (they haven't started yet)
			child.Cancel("parent job deleted")
			if err := q.store.UpdateJob(child); err != nil {
				err = errors.Wrapf(err, "failed to cancel child task %s", child.ID)
				err = errors.WithDetail(err, fmt.Sprintf("Child job ID: %s", child.ID))
				err = errors.WithDetail(err, fmt.Sprintf("Parent job ID: %s", jobID))
				err = errors.WithDetail(err, fmt.Sprintf("Handler: %s", child.HandlerName))
				return err
			}
			q.notifySubscribers(child)

		case JobStatusRunning:
			// Cancel running tasks (workers will detect parent deletion)
			child.Cancel("parent job deleted")
			if err := q.store.UpdateJob(child); err != nil {
				err = errors.Wrapf(err, "failed to cancel child task %s", child.ID)
				err = errors.WithDetail(err, fmt.Sprintf("Child job ID: %s", child.ID))
				err = errors.WithDetail(err, fmt.Sprintf("Parent job ID: %s", jobID))
				err = errors.WithDetail(err, fmt.Sprintf("Handler: %s", child.HandlerName))
				return err
			}
			q.notifySubscribers(child)

		case JobStatusPaused:
			// Cancel paused tasks
			child.Cancel("parent job deleted")
			if err := q.store.UpdateJob(child); err != nil {
				err = errors.Wrapf(err, "failed to cancel child task %s", child.ID)
				err = errors.WithDetail(err, fmt.Sprintf("Child job ID: %s", child.ID))
				err = errors.WithDetail(err, fmt.Sprintf("Parent job ID: %s", jobID))
				err = errors.WithDetail(err, fmt.Sprintf("Handler: %s", child.HandlerName))
				return err
			}
			q.notifySubscribers(child)

			// JobStatusCompleted, JobStatusFailed, JobStatusCancelled: leave them as-is for history
		}
	}

	// Now delete the parent job from database
	if err := q.store.DeleteJob(jobID); err != nil {
		err = errors.Wrapf(err, "failed to delete parent job %s", jobID)
		err = errors.WithDetail(err, fmt.Sprintf("Job ID: %s", jobID))
		err = errors.WithDetail(err, fmt.Sprintf("Child tasks cancelled: %d", len(children)))
		return err
	}

	return nil
}

// ListJobs returns jobs, optionally filtered by status
func (q *Queue) ListJobs(status *JobStatus, limit int) ([]*Job, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.store.ListJobs(status, limit)
}

// ListActiveJobs returns all active (queued, running, paused) jobs
func (q *Queue) ListActiveJobs(limit int) ([]*Job, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.store.ListActiveJobs(limit)
}

// Subscribe returns a channel that receives job updates.
// The caller is responsible for calling Unsubscribe when done.
// The returned channel is buffered to prevent blocking the notifier.
func (q *Queue) Subscribe() chan *Job {
	q.mu.Lock()
	defer q.mu.Unlock()

	ch := make(chan *Job, SubscriberChannelBufferSize) // Buffered to avoid blocking
	q.subscribers = append(q.subscribers, ch)
	return ch
}

// Unsubscribe removes a subscriber channel from the queue.
// The channel is NOT closed by this method - callers should close it themselves
// after unsubscribing if needed. This prevents double-close panics.
func (q *Queue) Unsubscribe(ch chan *Job) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i, sub := range q.subscribers {
		if sub == ch {
			// Remove from slice without closing - caller manages channel lifecycle
			q.subscribers = append(q.subscribers[:i], q.subscribers[i+1:]...)
			return
		}
	}
}

// notifySubscribers sends job updates to all subscribers.
// REQUIRES: q.mu must be held by caller (either Lock or RLock).
// Uses non-blocking send to avoid stalling if a subscriber is slow.
func (q *Queue) notifySubscribers(job *Job) {
	for _, ch := range q.subscribers {
		select {
		case ch <- job:
			// Sent successfully
		default:
			// Channel full, skip (non-blocking)
		}
	}
}

// ListTasksByParent returns all tasks for a given parent job
func (q *Queue) ListTasksByParent(parentJobID string) ([]*Job, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.store.ListTasksByParent(parentJobID)
}

// Cleanup removes old completed/failed jobs
func (q *Queue) Cleanup(ctx context.Context, olderThan time.Duration) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.store.CleanupOldJobs(olderThan)
}

// QueueStats returns statistics about the queue
type QueueStats struct {
	Queued    int `json:"queued"`
	Running   int `json:"running"`
	Paused    int `json:"paused"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Total     int `json:"total"`
}

// GetStats returns queue statistics
func (q *Queue) GetStats() (*QueueStats, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	stats := &QueueStats{}

	// Count jobs by status
	for _, status := range []JobStatus{JobStatusQueued, JobStatusRunning, JobStatusPaused, JobStatusCompleted, JobStatusFailed} {
		jobs, err := q.store.ListJobs(&status, MaxJobsLimit) // High limit to count all
		if err != nil {
			err = errors.Wrapf(err, "failed to count %s jobs", status)
			err = errors.WithDetail(err, fmt.Sprintf("Status: %s", status))
			return nil, err
		}

		count := len(jobs)
		switch status {
		case JobStatusQueued:
			stats.Queued = count
		case JobStatusRunning:
			stats.Running = count
		case JobStatusPaused:
			stats.Paused = count
		case JobStatusCompleted:
			stats.Completed = count
		case JobStatusFailed:
			stats.Failed = count
		}
		stats.Total += count
	}

	return stats, nil
}

// GetJobCounts returns quick counts of queued and running jobs (for system metrics)
func (q *Queue) GetJobCounts() (queued int, running int, err error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	// Quick count without detailed stats - optimized for frequent polling
	queuedStatus := JobStatusQueued
	runningStatus := JobStatusRunning

	queuedJobs, err := q.store.ListJobs(&queuedStatus, MaxJobsLimit)
	if err != nil {
		err = errors.Wrap(err, "failed to count queued jobs")
		err = errors.WithDetail(err, fmt.Sprintf("Status: %s", queuedStatus))
		return 0, 0, err
	}

	runningJobs, err := q.store.ListJobs(&runningStatus, MaxJobsLimit)
	if err != nil {
		err = errors.Wrap(err, "failed to count running jobs")
		err = errors.WithDetail(err, fmt.Sprintf("Status: %s", runningStatus))
		return 0, 0, err
	}

	return len(queuedJobs), len(runningJobs), nil
}

// FindActiveJobBySourceAndHandler finds an active (queued, running, or paused) job by source URL and handler name.
// Returns nil if no active job found for this source.
func (q *Queue) FindActiveJobBySourceAndHandler(source string, handlerName string) (*Job, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.store.FindActiveJobBySourceAndHandler(source, handlerName)
}

// FindRecentJobBySourceAndHandler finds a recently completed/failed job by source URL and handler name.
// Returns nil if no job completed/failed within the specified duration.
// This enables time-based deduplication to prevent re-processing recently handled URLs.
func (q *Queue) FindRecentJobBySourceAndHandler(source string, handlerName string, within time.Duration) (*Job, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.store.FindRecentJobBySourceAndHandler(source, handlerName, within)
}
