-- The worker takes the queued job that went longest since anything touched it
-- (pulse/async/store.go OldestQueued): WHERE status = ? ORDER BY updated_at ASC,
-- read in order from this index rather than sorted. A job put back in the queue
-- is touched, and so stands behind every other; one taken by created_at alone
-- stood at the front again at once.
CREATE INDEX IF NOT EXISTS idx_async_ix_jobs_status_updated ON async_ix_jobs(status, updated_at);
