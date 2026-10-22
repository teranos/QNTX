-- "PULSE FUCKED"
--
-- The newest jobs in a status were sorted out of every job in it: on q, 132078 for fifty.
CREATE INDEX IF NOT EXISTS idx_async_ix_jobs_status_created ON async_ix_jobs(status, created_at DESC);
