-- A token says when it ends (ADR-025). One minted before it had to was read as
-- never ending; that is what it meant, so it is written down: 9999-12-31,
-- the node's NeverExpires.
--
-- "nil is nil"

UPDATE access_tokens
SET record = json_set(record, '$.expires_at', 253402300799000)
WHERE json_extract(record, '$.expires_at') IS NULL;
