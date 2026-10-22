-- Make junction table lookups case-insensitive.
-- Predicates, subjects, actors, and contexts are matched with COLLATE NOCASE
-- so that "Weave" and "weave" resolve to the same attestations.
--
-- SQLite cannot ALTER COLUMN, so we recreate each table and its indexes.
-- attestation_predicates
CREATE TABLE attestation_predicates_new (
    attestation_id TEXT NOT NULL REFERENCES attestations(id) ON DELETE CASCADE,
    predicate TEXT NOT NULL COLLATE NOCASE
);
INSERT INTO attestation_predicates_new SELECT * FROM attestation_predicates;
DROP TABLE attestation_predicates;
ALTER TABLE attestation_predicates_new RENAME TO attestation_predicates;
CREATE INDEX idx_junc_predicate ON attestation_predicates(predicate);
CREATE INDEX idx_junc_predicate_id ON attestation_predicates(attestation_id);

-- attestation_subjects
CREATE TABLE attestation_subjects_new (
    attestation_id TEXT NOT NULL REFERENCES attestations(id) ON DELETE CASCADE,
    subject TEXT NOT NULL COLLATE NOCASE
);
INSERT INTO attestation_subjects_new SELECT * FROM attestation_subjects;
DROP TABLE attestation_subjects;
ALTER TABLE attestation_subjects_new RENAME TO attestation_subjects;
CREATE INDEX idx_junc_subject ON attestation_subjects(subject);
CREATE INDEX idx_junc_subject_id ON attestation_subjects(attestation_id);

-- attestation_actors
CREATE TABLE attestation_actors_new (
    attestation_id TEXT NOT NULL REFERENCES attestations(id) ON DELETE CASCADE,
    actor TEXT NOT NULL COLLATE NOCASE
);
INSERT INTO attestation_actors_new SELECT * FROM attestation_actors;
DROP TABLE attestation_actors;
ALTER TABLE attestation_actors_new RENAME TO attestation_actors;
CREATE INDEX idx_junc_actor ON attestation_actors(actor);
CREATE INDEX idx_junc_actor_id ON attestation_actors(attestation_id);
CREATE INDEX idx_junc_actor_context ON attestation_actors(actor, attestation_id);

-- attestation_contexts
CREATE TABLE attestation_contexts_new (
    attestation_id TEXT NOT NULL REFERENCES attestations(id) ON DELETE CASCADE,
    context TEXT NOT NULL COLLATE NOCASE
);
INSERT INTO attestation_contexts_new SELECT * FROM attestation_contexts;
DROP TABLE attestation_contexts;
ALTER TABLE attestation_contexts_new RENAME TO attestation_contexts;
CREATE INDEX idx_junc_context ON attestation_contexts(context);
CREATE INDEX idx_junc_context_id ON attestation_contexts(attestation_id);

