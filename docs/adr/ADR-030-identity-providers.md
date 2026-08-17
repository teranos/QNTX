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

## The identity is the key

The identity is the `did:key`. Accounts are what it holds, not what it is.

So `auth.root_identities` is a list of ways to reach an identity rather than a
list of identities. A `did:key` entry is the identity, and the login signature
is the whole proof. An account entry is a route, and the binding is what joins
the route to the key: a binding names the key it is about, so a route never has
to say where it leads.

One key holds any number of accounts across any providers. Two Mastodon
accounts and an atproto DID are one identity with three routes to it.

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

A deployment reachable off loopback names root identities or does not start.
An empty list on a bind the network can reach is a door with nobody behind it.

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

## Not done

A device cannot be listed, named, or removed. Under a model where root always
stands on a device, losing the only one loses the account, and nothing shows
you how many you have.

`mayRegister` never asks whether this identity already holds a device. A
governed deployment asks who the enrolment speaks for and stops there, so
nothing tells a first device from a fifth; the ungoverned path still asks the
deployment rather than the identity.

The first admission on a fresh deployment — no account yet, the first listed
identity to prove itself creates one — has never been run. Every account here
was enrolled under the model this replaced.

The login path does not treat the key as the identity. `admits` returns the
entry of `root_identities` that matched, and that string goes on to be the
session identity, the credential's `admitted_as`, `Caller.Identity`, and a
token's `minted_by`. One key reaching the same deployment by two listed routes
is two identity strings, and nothing joins them.

Making the key the identity everywhere needs the node to keep the bindings it
verified. `stillAdmitted` is handed an identity and no bindings, so a key
admitted by an account route has nothing left to re-check once the browser is
gone — and re-checking on every use is what makes striking an entry out of
am.toml a revocation.

## Consequences

- The passkey stops being one provider among several. It is the second half
  of every root admission, and laye is the first.
- The private key never leaves the tab. Everything else — the DID, the
  public key, signatures, bindings — exists in order to leave.
- A deployment with an empty `root_identities` can no longer enrol a
  passkey, because there is no identity for the credential to speak for.
  A passkey answering only to itself was the state every install predating
  this was in, and it is the state that made the credential ownerless.
