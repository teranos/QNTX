-- Drop ats_code from scheduled_pulse_jobs.
--
-- It was the original unit of scheduled work: an AX statement, held as text,
-- that the ticker would evaluate on a schedule. Handlers replaced that, and a
-- handler has a name separate from what it does where a query has only itself
-- to be identified by. Every lookup on the column was already a non-unique
-- SELECT ... LIMIT 1, which is the identity the design never had.
--
-- The column has been NOT NULL with every row written as '' since handlers
-- landed, so nothing is lost here that was not already empty.
--
-- Numbered 057, not 056: the deployment already recorded a 056 on 2026-08-22
-- from a branch that is neither this one nor main. The migrator keys on the
-- version alone, so a number burned on one branch is skipped silently on
-- every other. Take the next number free on the box, not the next free here.

ALTER TABLE scheduled_pulse_jobs DROP COLUMN ats_code;
