-- 085's idx_pulse_executions_status_completed begins with status, so every
-- read of runs by status is served by it, and the status index of 007 was one
-- more for every run's insert and every status it passes to keep.
--
-- A migration of its own: a version that is applied is skipped, so a drop
-- written into 085 never runs on a node that applied 085 before it.
DROP INDEX IF EXISTS idx_pulse_executions_status;
