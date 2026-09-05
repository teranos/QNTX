# Identity providers

One page per provider: what the operator registers elsewhere, what am.toml
carries for it, how it names an account, and where it deviates from the shape.
The shape itself, and why, is [ADR-030](../adr/ADR-030-identity-providers.md).

| Provider | Kind | Account | Operator setup |
|---|---|---|---|
| [Google](google.md) | redirect | `google:<sub>` | an OAuth client in Google's console |
| [Apple](apple.md) | redirect | `apple:<sub>` | a Services ID and a signing key in Apple's portal |
| [Mastodon](mastodon.md) | redirect | the profile URL | none |
| [atproto](atproto.md) | credential | the DID | none |

A redirect provider sends the person to the provider's own page and back. A
credential provider is asked at the door with something the person types.
