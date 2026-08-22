# ADR-033: First Time Setup

**Status:** Accepted
**Date:** 2026-08-22
**Related:** ADR-027 (Access Levels), ADR-030 (Identity Providers), ADR-031 (The User)

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

## It is the loader

A glyph persists. It lives in the tray, is reopened, melds, and has a life after the
moment it was made for. First-time setup happens once per deployment and can then never
happen again, so there is nothing to return to.

It is the loading screen instead. The loader already means the app is not usable yet and
here is what is still outstanding; on an unclaimed node the outstanding thing is that
nobody owns it. One scrim with one meaning, rather than a second scrim that happens to
look like the first.

So it is black, the dimmed logo, `#888` text, and the ways in are plain lines in that
same column. `init()` asks before it hides, and does not hide while the node is
unclaimed — the app does not start behind it, it starts after it.

## Not done

The display name and email page. Passkey enrolment after the claim.

An entry that is not a Mastodon profile URL is a valid way in but not a one-press one: a
credential provider needs an app password, which is typing (ADR-030).

