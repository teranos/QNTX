-- Junction tables for attestation multi-value fields.
-- Replaces json_each() scans with indexed lookups.

CREATE TABLE IF NOT EXISTS attestation_actors (
    attestation_id TEXT NOT NULL REFERENCES attestations(id) ON DELETE CASCADE,
    actor TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS attestation_contexts (
    attestation_id TEXT NOT NULL REFERENCES attestations(id) ON DELETE CASCADE,
    context TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS attestation_subjects (
    attestation_id TEXT NOT NULL REFERENCES attestations(id) ON DELETE CASCADE,
    subject TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS attestation_predicates (
    attestation_id TEXT NOT NULL REFERENCES attestations(id) ON DELETE CASCADE,
    predicate TEXT NOT NULL
);

-- Indexes for fast lookups
CREATE INDEX IF NOT EXISTS idx_junc_actor ON attestation_actors(actor);
CREATE INDEX IF NOT EXISTS idx_junc_actor_id ON attestation_actors(attestation_id);
CREATE INDEX IF NOT EXISTS idx_junc_context ON attestation_contexts(context);
CREATE INDEX IF NOT EXISTS idx_junc_context_id ON attestation_contexts(attestation_id);
CREATE INDEX IF NOT EXISTS idx_junc_subject ON attestation_subjects(subject);
CREATE INDEX IF NOT EXISTS idx_junc_subject_id ON attestation_subjects(attestation_id);
CREATE INDEX IF NOT EXISTS idx_junc_predicate ON attestation_predicates(predicate);
CREATE INDEX IF NOT EXISTS idx_junc_predicate_id ON attestation_predicates(attestation_id);

-- Composite indexes for enforcement queries
CREATE INDEX IF NOT EXISTS idx_junc_actor_context ON attestation_actors(actor, attestation_id);

-- Populate from existing JSON data
INSERT OR IGNORE INTO attestation_actors (attestation_id, actor)
SELECT a.id, j.value FROM attestations a, json_each(a.actors) j;

INSERT OR IGNORE INTO attestation_contexts (attestation_id, context)
SELECT a.id, j.value FROM attestations a, json_each(a.contexts) j;

INSERT OR IGNORE INTO attestation_subjects (attestation_id, subject)
SELECT a.id, j.value FROM attestations a, json_each(a.subjects) j;

INSERT OR IGNORE INTO attestation_predicates (attestation_id, predicate)
SELECT a.id, j.value FROM attestations a, json_each(a.predicates) j;
