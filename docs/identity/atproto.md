# atproto

Account: the DID, `did:plc:<id>`. A `did:` carries its own provenance, so it
is not qualified.

## Operator setup

None.

## In am.toml

Nothing under `[auth.provider]`. The DID goes in `root_identities`.

## What atproto does differently

It is the credential provider: the person types a handle and an app password
at the door, against a PDS, `bsky.social` by default. The node spends the
password once on `createSession` and gets a DID and a handle back.

Anyone can run a PDS and have it answer with any DID. The DID document is the
only thing that says which host speaks for a DID, so the node asks it, and a
PDS that answered for a DID whose document names another host is refused.
