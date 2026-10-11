-- A passkey is made for one user account, and the authenticator keeps at most
-- one per user handle per relying party (WebAuthn §4, User Handle). Every
-- passkey was made under the one handle 'qntx-owner', so a second account
-- enrolling on an authenticator replaced the first account's passkey there.
--
-- A passkey is now made under its User's id, and the handle it was made under
-- is kept with it: the authenticator hands it back at every sign-in, and it is
-- checked against this. Every passkey made before this was made under
-- 'qntx-owner', and the default says so.
ALTER TABLE webauthn_credentials ADD COLUMN user_handle TEXT NOT NULL DEFAULT 'qntx-owner';
