-- Add ax_query field to watchers table for AX query string support
ALTER TABLE watchers ADD COLUMN ax_query TEXT;
