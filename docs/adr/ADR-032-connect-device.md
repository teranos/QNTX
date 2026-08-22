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

## How it runs

`POST /auth/connect` is session-gated and mints a ticket carrying the route and the level
the granting Caller had. The browser builds the URL, not the node — only the browser knows
what path this deployment is served under.

`POST /auth/connect/redeem` spends the ticket into a half-admission for that route and
writes nothing else. No session comes out of it: the finger is what finishes the arrival,
and the enrolment is what records the grant, because the device key does not exist until
the authenticator derives it.

After that the passkey alone is the whole of a login. `handleLoginBegin` and
`handleLoginFinish` accept a laye half-admission *or* a live grant covering this exact
credential — the rule ADR-030 states, relaxed by exactly the thing that justifies relaxing
it. When the thirty days run out the device asks for a new code; the session expiring in
the meantime is a different and shorter clock.

Forgetting a device deletes its grant along with its credential, or a row nobody can see
would still be saying this device may log in by itself.

The QR encoder is written out in `web/ts/qr.ts` rather than pulled in. A code that admits
a device for thirty days should not be produced by something nobody in this repo can read.

## Not done

Possession of the ticket is delegation. ADR-027 says a SUPER User is created by the ROOT
User and by nobody else; a QR granting SUPER creates one by scan. The phone is shown what
it is about to become and presses before it commits, which is a consent rather than a
constraint.

`mayRegister` still does not ask whether an identity already holds a device.

A grant cannot be listed or revoked from the UI. The row is written and nothing reads it
back — the same gap ADR-030 filed, one layer down.

No scanner has read a code this encoder produced. The tests check what is positional and
checkable without a decoder: version for length, the finders, timing, the dark module, and
the format field's BCH coming back with level M and the mask that was applied.
