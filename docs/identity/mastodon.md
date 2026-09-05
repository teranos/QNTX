# Mastodon

Account: the profile URL, `https://<instance>/@<name>`. A URL carries its own
provenance, so it is not qualified.

## Operator setup

None. The node registers itself as an app on the instance at the start of
every ceremony, with the scope `read:accounts` and the node's callback as the
redirect URI, and spends that registration once. Nothing about it is kept.

## In am.toml

Nothing under `[auth.provider]`. A Mastodon entry in `root_identities` names
its own instance, and the ceremony reads the host there.

## What Mastodon does differently

The instance is typed at the door, since there are many. It is the one
redirect provider with a host prompt.
