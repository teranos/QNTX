-- A passkey stands on several devices. Apple copies one from a phone to a
-- laptop, and each device derives its own key from it (WebAuthn PRF). One
-- owner_did per credential said a passkey lived on one device, so the copy on
-- the second was refused as somebody else's.
--
-- "Clearly I want the honest model."
--
-- Each device a passkey stands on records the key it derived, under the
-- provider-proven admission it first answered on. The column on
-- webauthn_credentials stays: it is the key of the device that enrolled it,
-- and the first row here.
CREATE TABLE webauthn_credential_owners (
    id TEXT NOT NULL,
    owner_did TEXT NOT NULL CHECK (owner_did <> ''),
    admitted_as TEXT NOT NULL CHECK (admitted_as <> ''),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%f', 'now')),
    PRIMARY KEY (id, owner_did)
);

INSERT INTO webauthn_credential_owners (id, owner_did, admitted_as, created_at)
SELECT id, owner_did, admitted_as, created_at FROM webauthn_credentials;
