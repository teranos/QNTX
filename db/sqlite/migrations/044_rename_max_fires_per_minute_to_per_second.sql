-- Rename max_fires_per_minute to max_fires_per_second.
-- Per-second is the natural unit for rate limiters and makes testing practical.
-- Old default 150/min ≈ 3/sec, new column default is 3.

ALTER TABLE watchers RENAME COLUMN max_fires_per_minute TO max_fires_per_second;
