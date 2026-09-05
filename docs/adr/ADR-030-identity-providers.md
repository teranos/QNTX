# ADR-030: Identity Providers

**Status:** Accepted
**Date:** 2026-08-12
**Related:** ADR-010 (Identity System), ADR-012 (Browser WASM Parity), ADR-014 (plugin-provided service), ADR-025 (Access Tokens), ADR-026 (Namespaces), ADR-027 (Permissions)

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

`auth.root_identities` is a list of ways to reach an identity rather than a
list of identities. An entry is either a `did:key`, where the login signature is
the whole proof, or an account, which stands on a binding.
`auth.binding_signers` says whose signature on a binding counts. A binding
carries the key that signed it, so verifying one proves it is self-consistent
and nothing else — the list is the whole of what makes it mean something. Both
sides read it: the node from am.toml, laye from `/auth/status`, because a
browser that skips the check believes any peer that signs its own claim.

Each provider names accounts its own way. Mastodon by profile URL, atproto by
DID, Google and Apple by the `sub` each puts on an ID token. The string in am.toml is
whatever the provider calls the account, qualified where that name does not say
what it is:

"google:110169484474386276334, i want to know what it is ??"

A profile URL and a `did:` carry their own provenance. A bare number does not,
so Google's sub is written and matched as `google:` and the string stays
self-describing wherever it travels — including `admitted_as`, which travels
alone. Adding a provider adds a vocabulary rather than a field.

There is one ROOT User. Listing several is how that one User is reached from
more than one place — a key it holds, an account it holds — not how a
deployment admits several people.

A deployment reachable off loopback names root identities or does not start.
An empty list on a bind the network can reach is a door with nobody behind it.

The host comes from the route. A Mastodon entry in `auth.root_identities`
carries its own instance, so the ceremony reads it there.

Every entry reaches the same person, whichever one is used:

"and in the future, when you try to login with a root_identity atproto, you get access to the user it belongs to, root in our case"

## The ceremony

The node runs it. It registers with the provider, spends the token once to ask
which account it belongs to, and signs the binding — the browser proposes no
part of the answer it is going to be judged on.

Google does not let a node register itself, so the OAuth client is the
operator's and lives where the rest of their deployment does:

"that Google's client id and secret live in am.toml under [auth.provider.google] — yes, mine will live there"

The secret is a reference, never a literal: am.toml ships as a world-readable
SSM parameter, so a secret written into it is disclosed rather than configured.

The glyph draws it. `/auth/binding/providers` describes what each provider
asks for, so a provider appears in the UI by existing on the node — and Google
exists on a node only once it has been given a client, so a button that could
only fail is never drawn. The one window that still opens is the provider's own
consent screen.

Linking happens before anyone can log in, so the ceremony cannot be gated on a
session. It is gated on a ticket instead: starting one sets a cookie, and the
callback and the result are refused without it. Otherwise a stranger starts a
ceremony naming their own key, sends the authorize URL to someone else, and the
node signs a binding saying the stranger holds that person's account.

The ticket is also what the result is filed under. A binding is collected once,
by the browser that earned it.

A ceremony started as a navigation ends by sending the person back to the door
they left, carrying the ticket. The node sends people only to an origin am.toml
named as a door. An app is a door too: its own scheme stands in the door's
origins, the ceremony runs in Safari where the person's accounts already are,
and Safari hands the ticket back through the scheme. A page at a scheme sends
no Referer, so the navigation names its door, and the node accepts the name
only when am.toml already did. A scheme is never a passkey origin.

"security is a server concern"

## Apple

"we could probably do Apple's login as well" / "as identity provider"

The QNTX App carries this page into the App Store, and the App Store has a
rule about doors. Guideline 4.8, Login Services:

> Apps that use a third-party or social login service (such as Facebook
> Login, Google Sign-In, Log in with X, Sign In with LinkedIn, Login with
> Amazon, or WeChat Login) to set up or authenticate the user's primary
> account with the app must also offer as an equivalent option another login
> service with the following features: the login service limits data
> collection to the user's name and email address; the login service allows
> users to keep their email address private as part of setting up their
> account; and the login service does not collect interactions with your app
> for advertising purposes without consent.

A door that offers Google offers Apple, or the App is not in the store. Apple
is a redirect provider like Google, so the App gets it the way it gets Google.

Apple names an account by the `sub` on its identity token, "unique and static
for your developer team" — a bare opaque string, which is the same reason
Google's is qualified. It is written and matched as `apple:<sub>`.

Apple hands the operator no client secret. It hands them a signing key, and
the secret the token endpoint wants is a JWT the node mints and signs with that
key, naming the team, the key and the client. So `[auth.provider.apple]`
carries the Services ID, the Team ID, the Key ID, and a reference to the key —
never the key itself, for the reason Google's secret is a reference. A secret
is minted per exchange and lives minutes, so there is no long-lived secret to
hold.

Asking for a name or an email means Apple returns by POST rather than by
redirect. The ceremony stands on a cookie, and the cookie is `SameSite=Lax`,
which a browser keeps back from a cross-site POST — RFC 6265bis sends a Lax
cookie cross-site "if and only if they are top-level navigations which use a
'safe' HTTP method", and adds that "a request's method may be changed from
POST to GET for some redirects; in these cases, a request's 'safe'ness is
determined based on the method of the current redirect hop." So the node takes
Apple's POST and answers it with a redirect to the same callback as a GET,
which the cookie rides. The ticket and state checks are the ones Google's
return passes, unchanged; the POST decides nothing on its own.

The name arrives once — in that POST, on the first authorization, never in the
token — and it rides beside the binding unsigned, as Google's does. The
identity token is verified before its `sub` is believed: signature against the
keys Apple publishes, issuer, audience, expiry, and the nonce this ceremony
minted.

Apple refuses a return URL that is not HTTPS with a domain name, and says so:
"can't be an IP address or localhost". A node with no `auth.public_origin`
cannot finish an Apple ceremony, and that is Apple's rule rather than this
node's.

## Passkeys

A passkey records the identity that was being admitted when it was enrolled.
That is the moment, and the only moment, when a biometric and an account can
be tied together.

The ROOT User always stands on a device. laye proves the key in the tab and
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

`mayRegister` asks who an enrolment speaks for and whether that identity is
listed. It does not ask how many devices the identity already holds, so a first
device and a fifth are the same request.

`admitted_as` on a credential is a string. It matches an entry of
`root_identities` and joins to nothing else.

`stillAdmitted` is handed that string and no bindings, so re-checking asks
whether the entry is listed and cannot re-verify the binding behind it.

## Consequences

- The passkey stops being one provider among several. It is the second half
  of every root admission, and laye is the first.
- The private key never leaves the tab. Everything else — the DID, the
  public key, signatures, bindings — exists in order to leave.
- A deployment with an empty `root_identities` can no longer enrol a
  passkey, because there is no identity for the credential to speak for.
  A passkey answering only to itself was the state every install predating
  this was in, and it is the state that made the credential ownerless.
