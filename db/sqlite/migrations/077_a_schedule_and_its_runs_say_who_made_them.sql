-- "Name every meaning": a schedule, and each run it starts, says who made it:
-- a caller, in a sigil call, or no caller. Until now an empty namespace said
-- no caller; each row kept so far is named here as what it said. A row naming
-- neither is refused when it is read.
--
-- "nil is nil"

ALTER TABLE scheduled_pulse_jobs ADD COLUMN made_by TEXT NOT NULL DEFAULT 'unnamed';
UPDATE scheduled_pulse_jobs SET made_by = CASE namespace WHEN '' THEN 'no caller' ELSE 'caller' END;

ALTER TABLE async_ix_jobs ADD COLUMN made_by TEXT NOT NULL DEFAULT 'unnamed';
UPDATE async_ix_jobs SET made_by = CASE namespace WHEN '' THEN 'no caller' ELSE 'caller' END;
