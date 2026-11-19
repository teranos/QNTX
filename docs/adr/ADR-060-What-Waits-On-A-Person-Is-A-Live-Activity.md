# ADR-060: What waits on a person is a Live Activity

Date: 2026-11-14
Status: Decided

"the magic island"

"AND ON MY MAC AS WELL"

## The rule

A collection that wakes its receiver stands in front of the person while it
is pending: on the iPhone's island and lock screen, and in the menu bar of
their Mac. The node starts it with the app closed.

It shows who it is for, how many things wait, the first of them, and the time
left, counting down.

Its answers are on it. A press there is the answer, and the node does it as
the person.

This is the one way a person is told what waits on them.

## A press

"CANCEL SHOULD BE ONE THING"

"APPROVE SHOULD BE ONE THING"

A press is one of three: approve, delay, cancel. Approve is the yes, whatever
it is the yes to. Cancel is the no.

A press is drawn as its mark and never as a word. The node names what each
press is (`waitsPress.Does`), and the phone gives each its mark in one place
(`markOf`), so cancel is the same mark wherever it stands.

A press asks the node at a path of the node's own, in the session the app is
in. No token is made for it. An answer is the human's, and a token is refused
([ADR-052](ADR-052-Approvals.md)).

Every way a press ends is said: on what stands, and in the node's log with
who decided.

## The collection, and its receiver

"I SEE IT AS PART OF THE COLLECTION, APPROVABLE IN IT"

"ITS DIRECTIONAL, SO WE CARE ABOUT THE RECEIVER"

"IF WE KNOW SESSION A WAS INVOLVED, THATS WHY WE KNOW WHERE TO SEND"

"IF SESSION A AND B ARE INVOLVED, THEY EACH GET THE PR ADDED ONTO THERI COLLECTION"

"SO THEY KNOW DURING THE NEXT WAKE"

What stands is a collection, named by its receiver.

A pull request is an item in the receiver's collection, beside what agents
said, and is approved there. It never stands on its own.

A pull request goes to the session that was involved in it, and to each where
there were two. A session is involved when it asks GitHub about the pull
request through the node. They know of it at their next wake.

Pressing the collection brings the person to the receiver's chat, with the
collection open over it as the overlay.

## Approval packs

"PREVIOUSLY THIS IDEA ALSO HAD ANOTHER NAME, CALLED APPROVAL PACKS, WHICH ARE COMPLETE PACKAGES OF PR'S AND OTHER THINGS THAT CAN GET APPROVED. AN APPROVAL PACK IS ACCUMULATED, AND THEN RELEASED BY THE USER"

"APPROVAL PACK -> CAN IMPACT AND PLACE -> INTO COLLECTIONS -> PR'S THINGS TO SAY TO SESSIONS, HANDOVERS, REQUEST FOR REVIEW, ETC."

"AND JUST AS YOU CAN APPROVE AN INDIVIDUAL PR IN A COLLECTION, YOU CAN APPROVE OR DENY AN INDIVIDUAL ITEM IN A PACK"

An approval pack is a thing of its own: a complete package of pull requests
and other things that can be approved. It accumulates, and the person
releases it.

A pack places things into collections. A collection holds what goes to its
receiver: pull requests, things to say to sessions, handovers, requests for
review.

The person decides on the whole or on one item, in a pack and in a
collection.

## How long it stands

"SO THE 6H ONE STAYS FOR 6H"

"WHEN DELAY CANT WE MAKE IT BE DISMISSED UNTIL ITS TIME TO SHOW AGAIN /"

"SO WE NEVER BREAK APPLES RULE YOU KNOW"

A collection stands for its wait: six hours, or three for an agent on Opus
(`waitOf`). It stands from when the first thing that wakes is said.

Delayed or held, it is dismissed at once and stands again for the wait before
its new time.

One Live Activity stands for one time. No Live Activity stands longer than its
wait, and the wait is under the eight hours Apple allows.

## What wakes

"NO, ANY ONE THAT WAKES. PR MERGE UPDATES DONT WAKE PER SE, THEY GET COLLECTED ON A WAKING COLLECTION"

What stands is a collection that wakes. An item that wakes nobody is due at
no time, is not delivered for itself, and makes nothing stand. It is delivered
with what does wake. A pull request merge update is such an item.

## What fails stands too

"The failure is also quiet by construction."

A collection the node could not deliver waits on the person: it stands as
`not delivered, N tries`, with why, send again and cancel, for one wait from
when it began to fail. Every try is said in the node's log, and kept with the
collection (`failing_since`, `tries`, `failure`).

## What a room keeps

"things like coming from rooms, they may degrade over time, as in, it doesnt make sense to keep old room collection items in the waking collection at all"

"also, multiple comments are like one item per pr, and each pr is its own thing, but it can be said in digest form"

A room holds all that was said in it. What a collection holds of a room is its
newest wake alone, and none older than the receiver's wait. A wake tells each
pull request once, in short form: `owner/repo #N`, how many messages, by whom,
when the last was. A wake that is collected is the receiver told.

## One shape

One Live Activity type draws everything that waits: `WaitsAttributes`
(QNTX-App `island/shared/WaitsAttributes.swift`). The node fills it as
`waitsOf` and `waitsShown` (`server/phones.go`). The two are one shape, key
for key.

The node says everything that is shown, its presses among it. A new kind of
thing that waits is added on the node. The app is not built again for it.

## What it rests on

Each of these was missing once, and nothing stood on the phone.

| It needs | Where it is said | Read back by |
|---|---|---|
| The app signed with `aps-environment` | `entitlements.properties` in the generated `project.yml` (QNTX-App `.github/scripts/ios-project.py`). `xcodegen generate` writes the entitlements file from the project, so the project is where it is said. | `codesign` on the exported app, in the build (`export-island-ipa.py`). A build without it is not uploaded. |
| A key Apple takes for Production | `[apns]` in am.toml, the key at the path it names. The key is made at Apple with Environment set to Sandbox & Production. | Both of Apple's hosts asked with the key and a token that is none: a host that takes the key refuses only the token. |
| The island's own App ID and profile | QNTX-App `.github/workflows/island-apple.yml` asks Apple and makes what is missing. | The build installs the profile and names it in the export. |
| The press compiled into the app by Xcode | QNTX-App `island/app` and `island/shared` are sources of the app target. A Tauri plugin's Swift is built outside Xcode, and the system finds no press in it. | `Metadata.appintents` in the archived app, in the build. |
| The session the app is in | The page holds it and presents it on each request (QNTX-Web `ts/client/session.ts`, `server/auth/presented.go`). The app reads it from the page and keeps it in the Keychain, and puts it back into a page that lost it while the node still admits it. laye, in front of the node, hands it on as it came (Ground-Institute/laye#6). | Sentry's qntx-island project. |

## What the phone does is seen

Every failure of the island is a SacredError (teranos/sacred-error), sent to
Sentry's qntx-island project from `island/shared/Island.swift`, with the
steps before it as breadcrumbs. A token hand-over that has not ended a
minute after it began is sent naming the step it is at.

The app also says its steps to the node, by a client that names itself
`QNTX-waits/<build>`:

```
journalctl -u qntx | grep QNTX-waits
```

The node's access log writes only what it refused (`server/accesslog.go`).
A request it answered is no line there, and no line is no proof of anything.

The node writes down what a person decides: who, what, and the client it came
from.

## Settled

These are decided. A session builds on them and does not reopen them.

- A person is told what waits by a Live Activity, and by nothing else.
- It stands for its wait. No fixed number of minutes before delivery decides it.
- A press is a mark, never a word.
- A press is the person's own session. No token, pass or key is made for it.
- A pull request never stands on its own.
- What wakes nobody makes nothing stand.
- No Live Activity is restarted to outlast Apple's eight hours.
- Nothing that fails is quiet: what could not be delivered stands in front of the person.
- A room holds what was said in it; a collection holds its newest wake.
- The node says what is shown. The app adds no kinds of its own.
- A change to this is proven on a phone before it is called done, and the docs follow the proof.

## What runs

Seen on the person's iPhone and Mac:

| When (UTC) | What |
|---|---|
| 2026-11-13 23:18:34 | The node started Live Activities with the app closed. |
| 2026-11-13 23:20:11 | A press merged the deploy repository's #47, by the node as its App. |
| 2026-11-13 23:28:56 | A press from the Mac merged teranos/QNTX#1732. |
| 2026-11-14 00:58:31 | A collection pending for a session stood, with its countdown and three marks. |

Running on q: a collection that wakes stands for its wait and is dismissed on
a delay; the node logs each decision with who made it.

## What is not built

- A pull request as an item in a collection, approved there.
- Pressing the collection to open the receiver's chat with the overlay.
- Approval packs.
- A proof in the iPhone Simulator in CI, so a change is shown working without a phone. A push does not start a Live Activity in CI's Simulator (QNTX-App#31): the test starts it in the app, with what the node would send, and everything after runs for real.

## Open defect

The node answered 502 to the press that merged teranos/QNTX#1732. The pull
request was merged. The node logs the reason of such an answer since
teranos/QNTX#1734.
