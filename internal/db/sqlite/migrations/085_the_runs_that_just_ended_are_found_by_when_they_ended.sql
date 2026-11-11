-- "Always RCA, things happen for real reasons"
--
-- The runs that ended since the last look were sorted out of every completed
-- run, every 3 seconds, on the one connection every read waits for: on q,
-- 235636 for none, 17501 page reads. On 2026-11-04 at 14:44:57Z that read ran
-- 35.18s while the node built a comet on the same machine, and the
-- operational store did not answer.
-- Here they are found by when they ended, in order, without visiting another.
CREATE INDEX IF NOT EXISTS idx_pulse_executions_status_completed ON pulse_executions(status, completed_at);
