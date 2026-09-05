# Apple

Account: `apple:<sub>`. The `sub` is the one thing Apple promises stays the
same for a person under one developer team. The email may be a relay address
the person can switch off, so it is the handle and never the identity.

## In Apple's portal

- An App ID with Sign in with Apple enabled.
- A Services ID configured against it. This is `client_id`. Its web
  configuration names the domain the node answers on and the return URL
  `<public_origin>/auth/binding/callback`.
- A Sign in with Apple key. Download the `.p8` once; its Key ID is `key_id`.
  The account's Team ID is `team_id`.

## In am.toml

```toml
[auth.provider.apple]
client_id   = "<Services ID>"
team_id     = "<Team ID>"
key_id      = "<Key ID>"
private_key = "ssm:///path/to/the/p8"
```

All four or none: the secret is minted from the team, the key id and the key,
so a block missing one is a provider that gets drawn and cannot sign. The
parameter holds the `.p8` verbatim. A door may carry its own block under
`[auth.door.<namespace>.provider.apple]`.

## What Apple does differently

- No client secret. The node signs a short-lived JWT with the `.p8` per
  exchange.
- Returns by POST when name or email is asked for. The node answers it with a
  redirect to the same callback as a GET; see ADR-030.
- The name arrives once, in that POST, on the first authorization, and never
  in the token. It rides beside the binding unsigned.
- The identity token is the whole answer. It is verified against the keys
  Apple publishes, by kid, with issuer, audience, expiry and a per-ceremony
  nonce, before the `sub` is believed.
- Apple refuses a return URL that is not HTTPS on a domain name. A node with
  no `auth.public_origin` cannot finish an Apple ceremony.
