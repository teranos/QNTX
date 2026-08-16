# ADR-030: Identity Providers

**Status:** Accepted
**Date:** 2026-08-12
**Related:** ADR-010 (Identity System), ADR-012 (Browser WASM Parity), ADR-014 (plugin-provided service), ADR-025 (Access Tokens), ADR-026 (Namespaces), ADR-027 (Access Levels)

## Decision

"laye would just be another identity provider"

"satisfying a qntx contract"

laye is the wasm that ships with QNTX web.

relaye ceases to exist. QNTX signs bindings with its own node DID and is the
peer laye bootstraps from.

The node DID is the anchor. A QNTX deployment signs bindings with
`server/nodedid/`'s key and is its own root.

An identity provider gives QNTX web three operations:

    did()          -> string             did:key for this browser
    sign(bytes)    -> signature          proof of possession
    bindings()     -> SignedBinding[]    external identities bound to the key

`sign` is the only use the private key is put to. That is what makes "never
leaves the tab" enforceable.

The contract speaks `did:key`. The consumer names the shape and the provider
converts.

## Who gets in

`auth.root_identities` is the list. An entry is either a `did:key`, where the
login signature is the whole proof, or an account, which stands on a binding.
`auth.binding_signers` says whose signature on a binding counts. A binding
carries the key that signed it, so verifying one proves it is self-consistent
and nothing else — the list is the whole of what makes it mean something. Both
sides read it: the node from am.toml, laye from `/auth/status`, because a
browser that skips the check believes any peer that signs its own claim.

Each provider names accounts its own way. Mastodon by profile URL, atproto by
DID. The string in am.toml is whatever the provider calls the account, so
adding a provider adds a vocabulary rather than a field.

Listing several is how one deployment admits several people, and how one person
is admitted from more than one place.

## The ceremony

The node runs it. It registers with the provider, spends the token once to ask
which account it belongs to, and signs the binding — the browser proposes no
part of the answer it is going to be judged on.

The glyph draws it. `/auth/binding/providers` describes what each provider
asks for, so a provider appears in the UI by existing on the node. The one
window that still opens is the provider's own consent screen.

Linking happens before anyone can log in, so the ceremony cannot be gated on a
session. It is gated on a ticket instead: starting one sets a cookie, and the
callback and the result are refused without it. Otherwise a stranger starts a
ceremony naming their own key, sends the authorize URL to someone else, and the
node signs a binding saying the stranger holds that person's account.

The ticket is also what the result is filed under. A binding is collected once,
by the browser that earned it.

## Passkeys

A passkey records the identity that was being admitted when it was enrolled.
That is the moment, and the only moment, when a biometric and an account can
be tied together.

A root identity always stands on a device. laye proves the key in the tab and
finds the account, and that gets as far as the passkey rather than past it —
no session is issued there. An account with no device enrols one, which is
what a first login is; an account with one asserts it, every time.

Login asks am.toml again rather than trusting the enrolment, so striking an
account out of `root_identities` takes its devices with it.

A credential that cannot name both — the key the browser derived and the
identity that admitted it — is not stored. An ownerless credential is a
provenance failure: it authenticates whoever holds the authenticator, and no
later check can recover who it was for. So enrolment requires a session to
speak for, and a browser whose authenticator offers no PRF cannot enrol here.

## The door is attested

`identity:admitted`, `identity:refused` and `identity:released`, in the
`system` namespace, signed by the node. Both outcomes: a refusal is a fact
about the deployment in the same way an admission is.

Recording never fails the thing it records. A login that worked is not undone
by failing to write it down.

## Consequences

- The passkey stops being one provider among several. It is the second half
  of every root admission, and laye is the first.
- The private key never leaves the tab. Everything else — the DID, the
  public key, signatures, bindings — exists in order to leave.
- A deployment with an empty `root_identities` can no longer enrol a
  passkey, because there is no identity for the credential to speak for.
  A passkey answering only to itself was the state every install predating
  this was in, and it is the state that made the credential ownerless.
