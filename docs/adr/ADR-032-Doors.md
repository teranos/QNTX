# ADR-032 — Doors

One Node, several Doors. 
One place to manage it.

Door enter's into a [[ADR-026-Namespaces|Namespace]], set by it's `rp_id` in the `am.toml` 

Registration is attested.

By default any public [[ADR-031-the-user|User]] registering through a door is a `PUBLIC_REGISTRATION` level user.

Registering at one door isn't the same as registering at another door. 
The rung buys logging in and being attested. It reaches no attestation store, and the refusal names no namespace — somebody who reaches nothing learns nothing about what is there.

Each Door is it's own OAuth client.

"ok, so, whats also interesting to me, is, let's say QNTX helps me out with this when im creating a new namespace as i create it, and just forwards me to where i need to be in google, or it can just create an app itself right? am i correct in my thinking?"

Google has no call that creates an OAuth client — the client and its consent screen are made in the console by a person. So creating a door ends with the console link and the two strings that console asks for, both of which the node already knows: the redirect URI and the door's origins. The operator pastes those in and pastes the client id and secret back.

