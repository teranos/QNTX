-- 052 and 053 added owner_did and admitted_as as NOT NULL DEFAULT '', so a
-- credential with no owner was not merely possible, it was what you got by
-- omission. A credential that cannot say who enrolled it authenticates
-- whoever holds the authenticator.

-- SQLite cannot add a CHECK to an existing column, so the table is rebuilt.
-- Rows with no owner do not come across: an ownerless credential is a
-- provenance failure and re-enrolment is what fixes it.
CREATE TABLE webauthn_credentials_new (
    id TEXT PRIMARY KEY,
    credential_id BLOB NOT NULL UNIQUE,
    public_key BLOB NOT NULL,
    attestation_type TEXT NOT NULL,
    aaguid BLOB,
    sign_count INTEGER NOT NULL DEFAULT 0,
    backup_eligible INTEGER NOT NULL DEFAULT 0,
    backup_state INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    owner_did TEXT NOT NULL CHECK (owner_did <> ''),
    admitted_as TEXT NOT NULL CHECK (admitted_as <> '')
);

INSERT INTO webauthn_credentials_new
SELECT id, credential_id, public_key, attestation_type, aaguid, sign_count,
       backup_eligible, backup_state, created_at, owner_did, admitted_as
FROM webauthn_credentials
WHERE owner_did <> '' AND admitted_as <> '';

DROP TABLE webauthn_credentials;

ALTER TABLE webauthn_credentials_new RENAME TO webauthn_credentials;

CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_owner
    ON webauthn_credentials(owner_did);

CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_admitted_as
    ON webauthn_credentials(admitted_as);
