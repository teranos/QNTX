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

## The story

Things on the node wait on a person. An agent wants a pull request merged
([ADR-052](ADR-052-Approvals.md)). Agents have said things to another agent,
and the batch is held (`server/collected.go`).

When one of them moves, it stands in front of the person by itself: on the
iPhone's island and lock screen, and in the menu bar of their Mac. They opened
nothing.

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
| A batch | fifteen minutes before it is delivered | when what waits changes | delivered, delayed or canceled |
| An approval | when it is asked, or a check on its head changes | as its checks run, and when it can be decided | answered, or the pull request is over |

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
