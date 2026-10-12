-- A passkey's record keeps the transports its authenticator reported and
-- whether it has verified the user (WebAuthn §7.1 step 27), and a sign-in is
-- offered the transports kept (§5.8.3).
--
-- Every passkey before this was made on the device enrolling, and offered as
-- internal and hybrid; the default says so. None had its user verification
-- recorded, so each starts false and takes the UV bit of its next sign-in
-- (§7.2 step 24).
ALTER TABLE webauthn_credentials ADD COLUMN transports TEXT NOT NULL DEFAULT '["internal","hybrid"]';
ALTER TABLE webauthn_credentials ADD COLUMN uv_initialized INTEGER NOT NULL DEFAULT 0;
