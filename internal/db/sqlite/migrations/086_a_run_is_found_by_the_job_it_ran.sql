-- "Always RCA, things happen for real reasons"
--
-- Each job that ended was looked for among every run, on the one connection
-- every read waits for: on q, 237526 runs, 16342 page reads, for one or none.
-- On 2026-11-04 at 23:27:27Z, 74s after a start, that read ran 10.07s and the
-- operational store did not answer for 13s.
-- Here a run is found by the job it ran, without visiting another.
CREATE INDEX IF NOT EXISTS idx_pulse_executions_async_job ON pulse_executions(async_job_id);
