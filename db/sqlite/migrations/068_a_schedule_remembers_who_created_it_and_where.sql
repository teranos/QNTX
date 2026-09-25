-- "a schedule remembers who created it and where"
--
-- A schedule created during a sigil call keeps the caller's User and namespace,
-- and each run it starts carries them to the plugin. Empty is a schedule no
-- caller made, which is every row written before this migration.
ALTER TABLE scheduled_pulse_jobs ADD COLUMN user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE scheduled_pulse_jobs ADD COLUMN namespace TEXT NOT NULL DEFAULT '';
ALTER TABLE async_ix_jobs ADD COLUMN user_id TEXT NOT NULL DEFAULT '';
ALTER TABLE async_ix_jobs ADD COLUMN namespace TEXT NOT NULL DEFAULT '';
