-- 「[Opus 5.5] ForgetPayloads takes 100 bodies a pass from the jobs that still hold one. With 42,069」
-- 「[Opus 5.5] forwards on q and no index that leaves out the rows already emptied, every pass read」
-- 「[Opus 5.5] past them again, under the store's write lock (Qodo on #1649, 2026-11-13).」
-- 「[Opus 5.5] Only a row that still holds a body is in it, so it shrinks as they are forgotten.」
CREATE INDEX IF NOT EXISTS idx_async_ix_jobs_handler_holding_payload
    ON async_ix_jobs(handler_name)
    WHERE payload IS NOT NULL AND status IN ('completed', 'cancelled');
