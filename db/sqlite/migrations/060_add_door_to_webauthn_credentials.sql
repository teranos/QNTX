-- A passkey belongs to the domain it was made at, and with one relying party
-- there was nowhere else a credential could have come from. A node that serves
-- several has to answer a login for one door alone: offering the rest hands a
-- browser keys it will refuse, and says out loud that an account exists
-- somewhere else.
--
-- The door is named by its namespace rather than by its rp id. A namespace is
-- the identity (ADR-026); the rp id is how a browser is told about the door and
-- is edited in am.toml.
--
-- Every credential enrolled before this ran was made at the node's own relying
-- party, which is the door onto default (ADR-034). The default says so.
ALTER TABLE webauthn_credentials ADD COLUMN door TEXT NOT NULL DEFAULT 'default';

CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_door
    ON webauthn_credentials(door);
