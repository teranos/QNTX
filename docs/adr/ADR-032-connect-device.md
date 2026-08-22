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
delegation never escalates. It is single-use, and its own life is measured against the
person holding the phone rather than against the grant: fifteen minutes, which is long
enough to find the phone, install the app and point it at the screen.

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

## Why the phone has a camera button

QNTX-App is a WKWebView running this same frontend, and its `Info.plist` already declares
`NSCameraUsageDescription`. What it does not declare is associated domains — so a native
camera scanning `https://…/#connect=…` opens Safari, not the app. Safari is a different
browser: it would enrol a passkey for itself and leave the app exactly as unconnected as
it was.

So the app scans, in the page, and `web/ts/qr-scan.ts` is the decoder that makes that
possible. It only ever has to read codes `qr.ts` writes — byte mode, level M, versions one
through ten — which is what keeps it the size it is.

The button shows only where the bar is already at the top and there is a camera behind it.
A desktop is the device that shows codes, never the one that scans them.

Declaring associated domains on the app would make the native camera open the app
directly, which is fewer presses still. That is a change in two other repos and it does
not remove the button: a phone already open in the app has nothing to go back to Safari
for.

## What the round trip caught

The encoder placed format bit 8 at `(6, 8)` — a timing module — instead of `(7, 8)`. Both
runs of the field step over row six and column six, and this one did not.

Nothing found it for two commits. The encoder's own tests read the field back through the
same wrong table, so it round-tripped. The timing-pattern test passed by luck, because for
that payload the bit happened to equal the timing module it was overwriting. What found it
was a decoder written to the standard rather than to the encoder, on the first payload
where the bit differed.

## Not done

Possession of the ticket is delegation. ADR-027 says a SUPER User is created by the ROOT
User and by nobody else; a QR granting SUPER creates one by scan. The phone is shown what
it is about to become and presses before it commits, which is a consent rather than a
constraint.

`mayRegister` still does not ask whether an identity already holds a device.

A grant cannot be listed or revoked from the UI. The row is written and nothing reads it
back — the same gap ADR-030 filed, one layer down.

No phone camera has read a code this encoder produced, and no code has been read by this
decoder off a real screen. What is checked is the round trip: every version the encoder
writes is painted into a buffer and read back, including one with a hole blotted in it, so
the error correction is exercised rather than assumed.

**VERIFY at the next tagged release.** A grant is only reachable from a node someone has
just claimed, so exercising this end to end means nuking the ROOT User and re-initializing
the deployment — the same reset first-time setup needs. Until that happens, every line of
connect-device is code nobody has run.

`auth.app_url` has no value on any deployment yet, so the setup wizard draws one code
rather than two. It is a TestFlight invite and the deployment that owns the origin is what
supplies it.
