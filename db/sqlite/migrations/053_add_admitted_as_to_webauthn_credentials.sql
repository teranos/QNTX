-- owner_did is a key the browser derived; it says which authenticator, never
-- which account. A passkey enrolled from a laye session records the identity
-- that admitted that session, so a fingerprint can stand in for the ceremony
-- and still answer to auth.root_identities.
ALTER TABLE webauthn_credentials ADD COLUMN admitted_as TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_admitted_as
    ON webauthn_credentials(admitted_as);
