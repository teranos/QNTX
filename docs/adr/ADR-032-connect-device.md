# ADR-032: Connect Device

**Status:** Accepted
**Date:** 2026-08-22
**Related:** ADR-025 (Access Tokens), ADR-027 (Access Levels), ADR-030 (Identity Providers), ADR-031 (The User)

## Decision

"i want to be able to login on my phone with a qr code"

"while i am already loggen on web"

"like, connect device"

A second device is admitted by a device that is already admitted. No provider, no
instance, nothing typed.

## The grant

"my phone is authenticated at the same level as the thing the qr generated for 30 days"

The ticket carries the level of the `Caller` that asked for it and cannot carry more —
delegation never escalates. It is single-use, and its own life is measured against being
photographed rather than against the grant.

A grant is a record, not a line in `am.toml`. The server never writes the config the
deploy would overwrite.

## The passkey

"yes it enrols the passkey, scan once fingerprint after or faceid, and in future app opens the passkey is all that is required"

Admission by QR is enrolment. The ROOT User stands on a device (ADR-030), so the
credential is enrolled against the identity the generating session held — the only moment
a biometric and an account can be tied together.

The QR is scanned once. Thirty days is the grant's life, not the session's.

## Why not the provider ceremony

The ceremony asks where the account lives, because Mastodon is federated and there is no
authorize URL until an instance is named. The node cannot name it there either: that
ceremony is open to anyone, the callback signs a binding for whoever completed it, and
admission is checked afterwards.

The Mastodon iOS app cannot supply it. Its `Info.plist` registers one scheme, `mastodon`,
and `SceneDelegate` accepts `post`, `profile`, `status` and `search` — navigation, all of
it. `mastodon://joinmastodon.org/oauth` is its own outbound callback, not an interface
anyone may call. Its entitlements declare applinks for `mastodon.social` and
`mastodon.online` and no others.

So the form is not fixed here. It is taken off the path.

laye mints a `did:key` per browser, and `admits` already accepts a bare `did:key` where
the login signature is the whole proof. The phone arrives holding what the node takes.

## What this closes

ADR-030 files a device that cannot be listed, named or removed. A grant is a device, and
it is all three.

## Not done

None of it. This is the shape, written down before the code, so the code has something to
answer to.

`mayRegister` still does not ask whether an identity already holds a device.

Possession of the ticket is delegation. ADR-027 says a SUPER User is created by the ROOT
User and by nobody else; a QR granting SUPER creates one by scan. The phone shows what it
is about to become before it commits.
