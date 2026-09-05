# Apple

Account: `apple:<sub>`. The `sub` is the one thing Apple promises stays the
same for a person under one developer team. The email may be a relay address
the person can switch off, so it is the handle and never the identity.

## In Apple's portal
every
1. Team ID: https://developer.apple.com/account, under Membership details.
   This is `team_id`.
2. App ID: https://developer.apple.com/account/resources/identifiers/list.
   Open the app's App ID, tick Sign in with Apple, save.
3. Services ID: https://developer.apple.com/account/resources/identifiers/list/serviceId,
   the plus button, Services IDs. Give it an identifier, that is `client_id`.
   After creating it, open it, tick Sign in with Apple, Configure: primary App
   ID is the one above, domain is the host of `auth.public_origin`, return URL
   is `auth.public_origin` plus `/auth/binding/callback`.
4. Key: https://developer.apple.com/account/resources/authkeys/list, the plus
   button, tick Sign in with Apple, Configure to the same App ID, register.
   Download the `.p8` now; Apple offers it once. The Key ID shown is `key_id`.

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
