-- A device admitted by another device (ADR-032). The QR is scanned once and the
-- passkey is the whole of every login after that, so what bounds the delegation
-- is this row rather than the ceremony.

-- A grant is a record, not a line in am.toml: the server never writes the config
-- the deploy would overwrite.
CREATE TABLE IF NOT EXISTS device_grants (
    -- The key the authenticator derived, which is what the credential is about.
    owner_did TEXT PRIMARY KEY CHECK (owner_did <> ''),
    -- The auth.root_identities entry this device speaks for. It is the granting
    -- session's, never one the phone chose.
    admitted_as TEXT NOT NULL CHECK (admitted_as <> ''),
    -- What the granting Caller was. Delegation never escalates.
    level TEXT NOT NULL CHECK (level <> ''),
    -- The route that generated the ticket. Provenance for a device that was
    -- admitted by a device rather than by a provider.
    granted_by TEXT NOT NULL CHECK (granted_by <> ''),
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_device_grants_admitted_as
    ON device_grants(admitted_as);

CREATE INDEX IF NOT EXISTS idx_device_grants_expires_at
    ON device_grants(expires_at);
