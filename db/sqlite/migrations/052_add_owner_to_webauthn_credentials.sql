-- A passkey belongs to someone. Without this a credential is interchangeable
-- and a session can say only that somebody authenticated, never who.
ALTER TABLE webauthn_credentials ADD COLUMN owner_did TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_owner
    ON webauthn_credentials(owner_did);
