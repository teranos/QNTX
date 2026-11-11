-- "tear out eviction and distillation from QNTX"
--
-- No store evicts or distills, so nothing counts toward a limit and nothing
-- logs what a limit deleted. 010, 013 and 014 made storage_events; 049 made
-- the enforcement counters.
DROP TABLE IF EXISTS storage_events;
DROP TABLE IF EXISTS enforcement_actor_context;
DROP TABLE IF EXISTS enforcement_actor_contexts;
DROP TABLE IF EXISTS enforcement_entity_actors;
DROP TABLE IF EXISTS enforcement_entity_actors_detail;
