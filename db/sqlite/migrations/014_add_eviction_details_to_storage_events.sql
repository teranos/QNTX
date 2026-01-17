-- Add eviction_details column to storage_events for debugging
-- Stores JSON with sample attestations that were evicted

ALTER TABLE storage_events ADD COLUMN eviction_details TEXT;

-- eviction_details format (JSON):
-- {
--   "evicted_actors": ["actor1", "actor2"],       -- actors that were evicted (for entity_actors_limit)
--   "evicted_contexts": ["ctx1", "ctx2"],         -- contexts that were evicted (for actor_contexts_limit)
--   "sample_predicates": ["pred1", "pred2"],      -- sample predicates from evicted attestations
--   "sample_subjects": ["subj1", "subj2"],        -- sample subjects from evicted attestations
--   "last_seen": "2025-01-15T..."                 -- timestamp of most recent evicted data
-- }
