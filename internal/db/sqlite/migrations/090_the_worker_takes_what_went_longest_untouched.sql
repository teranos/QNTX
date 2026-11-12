-- 「[Fable 5.1] The worker takes the queued job that went longest since anything touched it」
-- 「[Fable 5.1] (pulse/async/store.go OldestQueued): WHERE status = ? ORDER BY updated_at ASC,」
-- 「[Fable 5.1] read in order from this index rather than sorted. A job put back in the queue」
-- 「[Fable 5.1] is touched, and so stands behind every other; one taken by created_at alone」
-- 「[Fable 5.1] stood at the front again at once.」
CREATE INDEX IF NOT EXISTS idx_async_ix_jobs_status_updated ON async_ix_jobs(status, updated_at);
