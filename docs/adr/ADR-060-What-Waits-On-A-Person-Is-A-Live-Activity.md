# ADR-060: What waits on a person is a Live Activity

Date: 2026-11-14
Status: Unpolished

"the magic island"

"as you may believe, i want to see it on my phone, for the duration of 15 minutes before a collected message is sent, the preview and the buttons to send now, delay and cancel"

"TO STAY THERE FOR 15 MINYTES, LIKE LUMA DOES"

"AND ON MY MAC AS WELL"

"THE BATCH IS A LIVE ACTIVITY"

"AND SO IS THE APPROCAL"

"ITS KIND OF THE SAME THING"

"CANCEL SHOULD BE ONE THING"

"APPROVE SHOULD BE ONE THING"

"THEY ARE ESSENTIALLY THE SAME GODDAMN THING"

"CANT YOU SEE THIS CODEPATH IS CRITICAL"

"HERE IS THE PR STORY"

"ITS PART OF THE COLLECTON"

"SO I DONT SEE IT"

"I SEE IT AS PART OF THE COLLECTION, APPROVABLE IN IT"

"FOR NOW THAT IS IT, AND CLICKING ON THE COLLECTION, BRING YOU TO IT, THE COLLECTION ITSELF, AND WHERE ITS GOING,"

"SO A PENDING COLLECTION FOR A"

"BRINGS ME TO A, AND THEN THE OVERLAY"

"ITS DIRECTIONAL, SO WE CARE ABOUT THE RECEIVER"

"IF WE KNOW SESSION A WAS INVOLVED, THATS WHY WE KNOW WHERE TO SEND"

"IF SESSION A AND B ARE INVOLVED, THEY EACH GET THE PR ADDED ONTO THERI COLLECTION"

"SO THEY KNOW DURING THE NEXT WAKE"

"PREVIOUSLY THIS IDEA ALSO HAD ANOTHER NAME, CALLED APPROVAL PACKS, WHICH ARE COMPLETE PACKAGES OF PR'S AND OTHER THINGS THAT CAN GET APPROVED. AN APPROVAL PACK IS ACCUMULATED, AND THEN RELEASED BY THE USER"

"I GUESS THE APPROVAL PACK IS STILL A DISTINCT THING"

"BUT THE LAYERING IS"

"APPROVAL PACK -> CAN IMPACT AND PLACE -> INTO COLLECTIONS -> PR'S THINGS TO SAY TO SESSIONS, HANDOVERS, REQUEST FOR REVIEW, ETC."

"AND JUST AS YOU CAN APPROVE AN INDIVIDUAL PR IN A COLLECTION, YOU CAN APPROVE OR DENY AN INDIVIDUAL ITEM IN A PACK"

"DONT DO THE 15 MINUTE THING ANYMORE"

"ANY PENDING IS LIVE ACTIVITY"

"ANY PENDING COLLECITON"

## Approval packs

An approval pack is a thing of its own: a complete package of pull requests
and other things that can be approved. It accumulates, and the person
releases it.

The layering is one way. A pack can impact collections and place things into
them, and a collection holds what goes to its receiver: pull requests, things
to say to sessions, handovers, requests for review.

The person decides on the whole or on one item, at both levels: one pull
request in a collection is approved there, and one item in a pack is approved
or denied there.

**Not built.** Nothing in the node is a pack yet.

## What stands is the collection

One thing stands in front of a person: the collection, by who it is for. It
is directional, and the receiver is what it is named by.

A pull request that waits on the person is an item in the receiver's
collection, beside what agents said, and is approved there. It does not stand
on its own.

Where a pull request goes is who was involved in it: the session that was is
the receiver, and where two were, it is added to the collection of each. They
know of it when what is collected for them is delivered, at their next wake,
and are not woken for it.

Pressing the collection brings the person to it and to where it is going:
what is pending for an agent brings them to that agent, its chat, and the
collection opens over it as the overlay.

**Not built yet.** On 2026-11-13 an approval was started as a Live Activity
of its own, and four stood at once. The node starts none on its own since. A
pull request is not yet an item in a collection, and pressing what stands
opens nothing yet.

## The story

Things on the node wait on a person. An agent wants a pull request merged
([ADR-052](ADR-052-Approvals.md)). Agents have said things to another agent,
and the batch is held (`server/collected.go`).

While a collection is pending, it stands in front of the person by itself: on
the iPhone's island and lock screen, and in the menu bar of their Mac. They
opened nothing.

It shows what it is, what it says now, and, where it runs out, the time left,
counting down.

Its answers are on it, as marks. A press there is the answer: the node does
it, as the person.

Pressed by nobody, it goes as it would have, and leaves the screens.

This is how a person is told what waits on them. There is no other way.

## One shape

A batch and an approval are one kind of thing, and one Live Activity type
draws both: `WaitsAttributes` (QNTX-App `island/shared/WaitsAttributes.swift`),
which the node fills as `waitsOf` and `waitsShown` (`server/phones.go`). The
two are one shape, key for key: what the node sends is decoded into that type
by the phone.

The node says everything it shows, its presses among it. A new kind of thing
that waits on a person is the node's to add, and the app is not built again
for it.

## A press

A press is one of three: approve, delay, cancel. Approve is the yes, whatever
it is the yes to: sending what waits, merging what was asked. Cancel is the
no. The node names what each press is (`waitsPress.Does`), and the phone draws
each as one mark wherever it stands (`markOf`). A press is never a word.

A press asks the node at a path of the node's own, with what the node said to
send there, in the session the app is in. No token is made for it. What
[ADR-052](ADR-052-Approvals.md) says of an approval's answer holds for every
press: it is the human's, and a token is refused.

A press decides what is merged and what is said to an agent. Every way it
ends is said, on what stands and in the node's log.

## When it stands

| What waits | Started | Moved | Ended |
|---|---|---|---|
| A batch | when it is pending, and again each eight hours, which is as long as Apple lets one stand | when what waits changes, or a person delays it | delivered or canceled |
| An approval | with the collection it is an item of | as its checks run, and when it can be decided | answered, or the pull request is over |

`showWhatWaits` looks once a minute and makes what stands what waits.

## What it rests on

Each of these was missing once, and what stood on the phone was nothing.

| It needs | Where it is said | Read back by |
|---|---|---|
| The app signed with `aps-environment` | `entitlements.properties` in the generated `project.yml` (QNTX-App `.github/scripts/ios-project.py`). Written into the entitlements file alone, `xcodegen generate` writes the file anew without it. | `codesign` on the exported app, in the build (`export-island-ipa.py`) |
| A key Apple takes for Production | `[apns]` in am.toml, the key at the path it names | Asking both of Apple's hosts with the key and a token that is none: a host that takes the key refuses only the token |
| The island's own App ID and profile | QNTX-App `.github/workflows/island-apple.yml` asks Apple and makes what is missing | The build installs the profile and names it in the export |
| The press compiled into the app by Xcode | QNTX-App `island/app`, `island/shared` as sources of the app target. A Tauri plugin's Swift is built outside Xcode, and the system finds no press in it. | `Metadata.appintents` in the archived app, in the build |
| The session the app is in | The page holds it and presents it on each request (QNTX-Web `ts/client/session.ts`, `server/auth/presented.go`). The app reads it from the page and keeps it for when there is no page. | The node's log |

## What the phone does is in the node's log

Nothing on a phone is seen from anywhere else. What the app's side does, it
says to the node: lines whose client names itself `QNTX-waits/<build>`, in the
node's journal. Begun, a token from Apple or none, not handed over and why,
pressed, done, not done and why.

What a person decides is written down by the node: who answered, what, and
from what.
