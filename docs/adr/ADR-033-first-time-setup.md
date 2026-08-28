# ADR-033: First Time Setup

**Status:** Accepted
**Date:** 2026-08-22
**Related:** ADR-027 (Permissions), ADR-030 (Identity Providers), ADR-031 (The User)

## Decision

A node with `auth.root_identities` set and no User belongs to nobody, and that is not an
auth state.

"Because this is a new root box without a user and only root_identities set, the first glyph you see isnt the auth glyph"

"Its a wizard anyone can see"

"only one of the root_identities are able to proof who they are so those are the only ones allowed o continue from here."

Seeing it is not passing it. The gate is the proof, not the visibility.

"when one of the root_identities is proven, the user get's created immediately"

"setting any of these isnt required for root identity acc creation, it aready happened when we proofed we own the acc set in root_identities."

So the display name and the email are asked for, never required. Walking away from that
page leaves you ROOT, signed in, and called `root` (ADR-031).

The device comes before the name. A `display_name` is settled once and can never be taken
back, and the half-admission laye leaves behind is by definition an admission nobody has
finished — it buys one ceremony and nothing else. An unfinished admission must not be able
to leave a permanent mark, so naming yourself is something the far side of a session does.

## What it may show

"frontend needs to no leak who root identities are , it just needs to present the login methods it has"

How, never who. A stranger loading the page learns that this node takes a Mastodon
account and nothing about which one — not the handle, not the instance. `GET /setup`
answers with providers; `POST /setup/claim` names a provider, and the node picks which of
its listed identities that means. The browser learns the instance by being sent to it.

One entry per provider, not per identity: counting them would say how many people the
node lists.

Once a User exists, the node stops answering any of this. An owned node does not publish
how it is entered.

## It is a door

A glyph persists. It lives in the tray, is reopened, melds, and has a life after the
moment it was made for. First-time setup happens once per deployment, so there is nothing
to return to, and it is not a glyph.

Not being authenticated feels cold and distant, mechanical. So the door is brushed metal,
machined rather than designed, and the one living thing on it is the fingerprint: green
because it is alive, anonymous because the node does not yet know whose it is. A slight
CRT over it is what makes it wait for the true attestor to arrive rather than merely sit
there.

The door is inside the system bar. The scrim does everything it can do without you, lifts,
and the bar is standing open with the door in it, visible and shut. The app does not start
behind the door — it starts after it.

Opening the door is the bar going to its minimised state with the door section collapsing
inside it. Nothing slides aside; the way in stops taking up room.

On mobile the bar is already at the top, under `safe-area-inset-top`, which puts the
fingerprint very close to the Dynamic Island — where Face ID comes out of.

## Walking back out

The door is not thrown away when you go through it. It is how you walked in, and it is how
you walk back out: a latch in the bar's header stands it up again, showing who the node
thinks you are and the two ways out.

Logging out ends a session. Forgetting ends the device: `POST /auth/forget` deletes the
credential and takes the keys it stood on off the User, so the next arrival here is a
stranger. It is destructive, so the credential itself is what names which one — the same
touch as a login, sent somewhere else.

## Not done

A browser this node has never seen still has to go through the provider ceremony, which is
the one place anything is typed. Admitting a second device from one already admitted is
deferred.

An entry that is not a Mastodon profile URL is a valid way in but not a one-press one: a
credential provider needs an app password, which is typing (ADR-030).

