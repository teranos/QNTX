# GitHub

Account: `github:<id>`. GitHub's `id` is a bare number, so it is qualified
the way Google's `sub` is. The login is renamed and released, so it is the
handle and never the identity: "Always use the `id` of the user. This value
will never change for the user or be used to point to a different user."

An account's id is public: `https://api.github.com/users/<login>`.

## At GitHub

A GitHub App, registered at `github.com/settings/apps`:

- Callback URL: `<public_origin>/auth/binding/callback`
- A client secret, generated on the app's page

## In am.toml

```toml
[auth.provider.github]
client_id     = "<the app's Client ID>"
client_secret = "ssm:///path/to/the/secret"
```

The secret is a reference, never a literal: am.toml ships as a world-readable
parameter. A door takes its own client under
`[auth.door.<namespace>.provider.github]`, as it does for Google.

## What GitHub does differently

The web application flow, as [ADR-044](../adr/ADR-044-github.md) links it:
PKCE on the authorize and the exchange, `Accept: application/json` on the
exchange because GitHub otherwise answers form-encoded, and no scope, because
`GET /user` answers without one.
