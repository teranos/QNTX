-- Counter tables for O(1) enforcement checks.
-- Replaces expensive COUNT + JOIN queries across 876K+ junction table rows
-- with simple primary-key lookups on small counter tables.

-- Count of attestations per (actor, context) pair.
CREATE TABLE IF NOT EXISTS enforcement_actor_context (
    actor TEXT NOT NULL,
    context TEXT NOT NULL COLLATE NOCASE,
    count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (actor, context)
);

-- Count of distinct contexts per actor.
CREATE TABLE IF NOT EXISTS enforcement_actor_contexts (
    actor TEXT NOT NULL PRIMARY KEY,
    count INTEGER NOT NULL DEFAULT 0
);

-- Count of distinct actors per subject.
CREATE TABLE IF NOT EXISTS enforcement_entity_actors (
    subject TEXT NOT NULL PRIMARY KEY,
    count INTEGER NOT NULL DEFAULT 0
);

-- Detail table for tracking distinct (subject, actor) pairs.
-- Needed to know when a new actor appears for a subject.
CREATE TABLE IF NOT EXISTS enforcement_entity_actors_detail (
    subject TEXT NOT NULL,
    actor TEXT NOT NULL,
    PRIMARY KEY (subject, actor)
);

-- Populate from existing junction table data.
INSERT OR IGNORE INTO enforcement_actor_context (actor, context, count)
SELECT a.actor, c.context, COUNT(*)
FROM attestation_actors a
JOIN attestation_contexts c ON a.attestation_id = c.attestation_id
GROUP BY a.actor, c.context;

INSERT OR IGNORE INTO enforcement_actor_contexts (actor, count)
SELECT actor, COUNT(DISTINCT c.context)
FROM attestation_actors a
JOIN attestation_contexts c ON a.attestation_id = c.attestation_id
GROUP BY a.actor;

INSERT OR IGNORE INTO enforcement_entity_actors (subject, count)
SELECT s.subject, COUNT(DISTINCT a.actor)
FROM attestation_subjects s
JOIN attestation_actors a ON s.attestation_id = a.attestation_id
GROUP BY s.subject;

INSERT OR IGNORE INTO enforcement_entity_actors_detail (subject, actor)
SELECT DISTINCT s.subject, a.actor
FROM attestation_subjects s
JOIN attestation_actors a ON s.attestation_id = a.attestation_id;
