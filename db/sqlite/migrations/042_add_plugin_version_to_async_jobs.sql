-- Add plugin_version to async jobs so execution history shows which plugin version produced each result
ALTER TABLE async_ix_jobs ADD COLUMN plugin_version TEXT NOT NULL DEFAULT '';
