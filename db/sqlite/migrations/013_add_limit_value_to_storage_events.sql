-- Add limit_value column to storage_events table
-- Stores the actual configured limit that was enforced/warned about
-- Enables accurate message formatting even when limits are customized

ALTER TABLE storage_events ADD COLUMN limit_value INTEGER;
