-- FindActiveJobBySourceAndHandler, asked on every scheduled tick, was served by
-- the status index alone: with 12,804 github.forward jobs queued on q it read
-- and sorted each of them, holding the store's one read connection until the
-- node stopped on the operational store not answering in 1m0s (2026-11-07).
CREATE INDEX IF NOT EXISTS idx_async_ix_jobs_handler_source_status
    ON async_ix_jobs(handler_name, source, status, created_at);
