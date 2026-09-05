# Google

Account: `google:<sub>`. Google's `sub` is a bare number, so it is qualified
and stays self-describing wherever it travels, `admitted_as` included.

## In Google's console

Google has no call that creates an OAuth client; a person makes one in the
console. It asks for two strings, both of which the node already states:

- Authorized redirect URI: `<public_origin>/auth/binding/callback`
- Authorized JavaScript origins: the door's origins from am.toml

## In am.toml

```toml
[auth.provider.google]
client_id     = "<what the console issues>"
client_secret = "ssm:///path/to/the/secret"
```

The secret is a reference, never a literal: am.toml ships as a world-readable
parameter. A door consents under the node's client until it has one of its
own under `[auth.door.<namespace>.provider.google]`, and then the consent
screen says the door's name.

## What Google does differently

Nothing. It is the shape every redirect provider is measured against: the
person is sent to `accounts.google.com`, returns with a code, the node spends
the code for the userinfo answer and reads the `sub`.
