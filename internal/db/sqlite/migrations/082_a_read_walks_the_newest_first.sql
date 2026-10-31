-- "I WANT TO BE ABLE TO STORE A FUCKTON OF DATA"
--
-- The newest 300 were sorted out of every row: on k, 533847 for 300.
-- Each index holds rowid after created_at, so walked backwards it is
-- the read's own order, created_at DESC, rowid DESC.
CREATE INDEX IF NOT EXISTS idx_attestations_created ON attestations(created_at);

-- A name's rows, newest first, without visiting the attestation to learn its age.
ALTER TABLE attestation_subjects ADD COLUMN created_at DATETIME;
ALTER TABLE attestation_predicates ADD COLUMN created_at DATETIME;
ALTER TABLE attestation_contexts ADD COLUMN created_at DATETIME;
ALTER TABLE attestation_actors ADD COLUMN created_at DATETIME;

UPDATE attestation_subjects SET created_at = (SELECT created_at FROM attestations WHERE id = attestation_id);
UPDATE attestation_predicates SET created_at = (SELECT created_at FROM attestations WHERE id = attestation_id);
UPDATE attestation_contexts SET created_at = (SELECT created_at FROM attestations WHERE id = attestation_id);
UPDATE attestation_actors SET created_at = (SELECT created_at FROM attestations WHERE id = attestation_id);

CREATE INDEX IF NOT EXISTS idx_junc_subject_created ON attestation_subjects(subject, created_at);
CREATE INDEX IF NOT EXISTS idx_junc_predicate_created ON attestation_predicates(predicate, created_at);
CREATE INDEX IF NOT EXISTS idx_junc_context_created ON attestation_contexts(context, created_at);
CREATE INDEX IF NOT EXISTS idx_junc_actor_created ON attestation_actors(actor, created_at);

-- A writer that leaves created_at out still files its row by age.
CREATE TRIGGER IF NOT EXISTS junc_subject_created AFTER INSERT ON attestation_subjects WHEN NEW.created_at IS NULL
BEGIN UPDATE attestation_subjects SET created_at = (SELECT created_at FROM attestations WHERE id = NEW.attestation_id) WHERE rowid = NEW.rowid; END;
CREATE TRIGGER IF NOT EXISTS junc_predicate_created AFTER INSERT ON attestation_predicates WHEN NEW.created_at IS NULL
BEGIN UPDATE attestation_predicates SET created_at = (SELECT created_at FROM attestations WHERE id = NEW.attestation_id) WHERE rowid = NEW.rowid; END;
CREATE TRIGGER IF NOT EXISTS junc_context_created AFTER INSERT ON attestation_contexts WHEN NEW.created_at IS NULL
BEGIN UPDATE attestation_contexts SET created_at = (SELECT created_at FROM attestations WHERE id = NEW.attestation_id) WHERE rowid = NEW.rowid; END;
CREATE TRIGGER IF NOT EXISTS junc_actor_created AFTER INSERT ON attestation_actors WHEN NEW.created_at IS NULL
BEGIN UPDATE attestation_actors SET created_at = (SELECT created_at FROM attestations WHERE id = NEW.attestation_id) WHERE rowid = NEW.rowid; END;
