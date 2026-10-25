-- Drop daemon_config. Undoes 005_daemon_config.sql.
--
-- 005 created a single-row table holding the daemon's desired state, so that a
-- daemon someone stopped stayed stopped across a restart. The thing that set it
-- was a Stop button in the UI, and that is gone: Pulse is not separate enough
-- from the node to be worth turning on and off, so Pulse starts because the node
-- starts. There is no desired state left to persist.
--
-- Leaving the table would have been worse than dropping it. Its only writers
-- were the start and stop paths, so whatever value a node happened to hold would
-- have frozen there forever — and a node sitting at enabled=0 would have come up
-- with its scheduled work silently dead and no way back.
--
-- This file and 005 are a pair and mean nothing apart. When the migrations are
-- normalised — a 1.0.0 blocker — both go, and the table never existed.

DROP INDEX IF EXISTS idx_daemon_config_enabled;
DROP TABLE IF EXISTS daemon_config;
