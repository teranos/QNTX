-- "GROUND NAMESPACE WILL KEEP GROWING YOU KNOW"
--
-- Every stats refresh counted the namespace's rows one by one, on the
-- read pool the namespace's reads wait for.
CREATE TABLE IF NOT EXISTS attestation_total (
    one INTEGER PRIMARY KEY CHECK (one = 1),
    n INTEGER NOT NULL
);
INSERT OR REPLACE INTO attestation_total (one, n) SELECT 1, COUNT(*) FROM attestations;

CREATE TRIGGER IF NOT EXISTS attestation_total_put AFTER INSERT ON attestations
BEGIN UPDATE attestation_total SET n = n + 1 WHERE one = 1; END;
CREATE TRIGGER IF NOT EXISTS attestation_total_delete AFTER DELETE ON attestations
BEGIN UPDATE attestation_total SET n = n - 1 WHERE one = 1; END;
