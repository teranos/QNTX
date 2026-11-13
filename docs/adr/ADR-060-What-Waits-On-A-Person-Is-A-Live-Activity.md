# 「[Opus 5.5] ADR-060: What waits on a person is a Live Activity」

「[Opus 5.5] Date: 2026-11-14」
「[Opus 5.5] Status: Unpolished」

"「[Opus 5.5] the magic island」"

"「[Opus 5.5] as you may believe, i want to see it on my phone, for the duration of 15 minutes before a collected message is sent, the preview and the buttons to send now, delay and cancel」"

"「[Opus 5.5] TO STAY THERE FOR 15 MINYTES, LIKE LUMA DOES」"

"「[Opus 5.5] AND ON MY MAC AS WELL」"

"「[Opus 5.5] THE BATCH IS A LIVE ACTIVITY」"

"「[Opus 5.5] AND SO IS THE APPROCAL」"

"「[Opus 5.5] ITS KIND OF THE SAME THING」"

"「[Opus 5.5] CANCEL SHOULD BE ONE THING」"

"「[Opus 5.5] APPROVE SHOULD BE ONE THING」"

"「[Opus 5.5] THEY ARE ESSENTIALLY THE SAME GODDAMN THING」"

"「[Opus 5.5] CANT YOU SEE THIS CODEPATH IS CRITICAL」"

"「[Opus 5.5] HERE IS THE PR STORY」"

"「[Opus 5.5] ITS PART OF THE COLLECTON」"

"「[Opus 5.5] SO I DONT SEE IT」"

"「[Opus 5.5] I SEE IT AS PART OF THE COLLECTION, APPROVABLE IN IT」"

"「[Opus 5.5] FOR NOW THAT IS IT, AND CLICKING ON THE COLLECTION, BRING YOU TO IT, THE COLLECTION ITSELF, AND WHERE ITS GOING,」"

"「[Opus 5.5] SO A PENDING COLLECTION FOR A」"

"「[Opus 5.5] BRINGS ME TO A, AND THEN THE OVERLAY」"

"「[Opus 5.5] ITS DIRECTIONAL, SO WE CARE ABOUT THE RECEIVER」"

"IF WE KNOW SESSION A WAS INVOLVED, THATS WHY WE KNOW WHERE TO SEND"

"IF SESSION A AND B ARE INVOLVED, THEY EACH GET THE PR ADDED ONTO THERI COLLECTION"

"「[Opus 5.5] SO THEY KNOW DURING THE NEXT WAKE」"

"PREVIOUSLY THIS IDEA ALSO HAD ANOTHER NAME, CALLED APPROVAL PACKS, WHICH ARE COMPLETE PACKAGES OF PR'S AND OTHER THINGS THAT CAN GET APPROVED. AN APPROVAL PACK IS ACCUMULATED, AND THEN RELEASED BY THE USER"

"I GUESS THE APPROVAL PACK IS STILL A DISTINCT THING"

"BUT THE LAYERING IS"

"APPROVAL PACK -> CAN IMPACT AND PLACE -> INTO COLLECTIONS -> PR'S THINGS TO SAY TO SESSIONS, HANDOVERS, REQUEST FOR REVIEW, ETC."

"AND JUST AS YOU CAN APPROVE AN INDIVIDUAL PR IN A COLLECTION, YOU CAN APPROVE OR DENY AN INDIVIDUAL ITEM IN A PACK"

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

## 「[Opus 5.5] What stands is the collection」

「[Opus 5.5] One thing stands in front of a person: the collection, by who it is for. It」
「[Opus 5.5] is directional, and the receiver is what it is named by.」

「[Opus 5.5] A pull request that waits on the person is an item in the receiver's」
「[Opus 5.5] collection, beside what agents said, and is approved there. It does not stand」
「[Opus 5.5] on its own.」

「[Fable 5.2] Where a pull request goes is who was involved in it: the session that was is」
「[Fable 5.2] the receiver, and where two were, it is added to the collection of each. They」
「[Fable 5.2] know of it when what is collected for them is delivered, at their next wake,」
「[Fable 5.2] and are not woken for it.」

「[Opus 5.5] Pressing the collection brings the person to it and to where it is going:」
「[Opus 5.5] what is pending for an agent brings them to that agent, its chat, and the」
「[Opus 5.5] collection opens over it as the overlay.」

「[Fable 5.2] **Not built yet.** On 2026-11-13 an approval was started as a Live Activity」
「[Fable 5.2] of its own, and four stood at once. The node starts none on its own since. A」
「[Fable 5.2] pull request is not yet an item in a collection, and pressing what stands」
「[Fable 5.2] opens nothing yet.」

## 「[Opus 5.5] The story」

「[Opus 5.5] Things on the node wait on a person. An agent wants a pull request merged」
「[Opus 5.5] ([ADR-052](ADR-052-Approvals.md)). Agents have said things to another agent,」
「[Opus 5.5] and the batch is held (`server/collected.go`).」

「[Opus 5.5] When one of them moves, it stands in front of the person by itself: on the」
「[Opus 5.5] iPhone's island and lock screen, and in the menu bar of their Mac. They opened」
「[Opus 5.5] nothing.」

「[Opus 5.5] It shows what it is, what it says now, and, where it runs out, the time left,」
「[Opus 5.5] counting down.」

「[Opus 5.5] Its answers are on it, as marks. A press there is the answer: the node does」
「[Opus 5.5] it, as the person.」

「[Opus 5.5] Pressed by nobody, it goes as it would have, and leaves the screens.」

「[Opus 5.5] This is how a person is told what waits on them. There is no other way.」

## 「[Opus 5.5] One shape」

「[Opus 5.5] A batch and an approval are one kind of thing, and one Live Activity type」
「[Opus 5.5] draws both: `WaitsAttributes` (QNTX-App `island/shared/WaitsAttributes.swift`),」
「[Opus 5.5] which the node fills as `waitsOf` and `waitsShown` (`server/phones.go`). The」
「[Opus 5.5] two are one shape, key for key: what the node sends is decoded into that type」
「[Opus 5.5] by the phone.」

「[Opus 5.5] The node says everything it shows, its presses among it. A new kind of thing」
「[Opus 5.5] that waits on a person is the node's to add, and the app is not built again」
「[Opus 5.5] for it.」

## 「[Opus 5.5] A press」

「[Opus 5.5] A press is one of three: approve, delay, cancel. Approve is the yes, whatever」
「[Opus 5.5] it is the yes to: sending what waits, merging what was asked. Cancel is the」
「[Opus 5.5] no. The node names what each press is (`waitsPress.Does`), and the phone draws」
「[Opus 5.5] each as one mark wherever it stands (`markOf`). A press is never a word.」

「[Opus 5.5] A press asks the node at a path of the node's own, with what the node said to」
「[Opus 5.5] send there, in the session the app is in. No token is made for it. What」
「[Opus 5.5] [ADR-052](ADR-052-Approvals.md) says of an approval's answer holds for every」
「[Opus 5.5] press: it is the human's, and a token is refused.」

「[Opus 5.5] A press decides what is merged and what is said to an agent. Every way it」
「[Opus 5.5] ends is said, on what stands and in the node's log.」

## 「[Opus 5.5] When it stands」

| What waits | Started | Moved | Ended |
|---|---|---|---|
| A batch | fifteen minutes before it is delivered | when what waits changes | delivered, delayed or canceled |
| An approval | with the collection it is an item of | as its checks run, and when it can be decided | answered, or the pull request is over |

「[Opus 5.5] `showWhatWaits` looks once a minute and makes what stands what waits.」

## 「[Opus 5.5] What it rests on」

「[Opus 5.5] Each of these was missing once, and what stood on the phone was nothing.」

| It needs | Where it is said | Read back by |
|---|---|---|
| The app signed with `aps-environment` | `entitlements.properties` in the generated `project.yml` (QNTX-App `.github/scripts/ios-project.py`). Written into the entitlements file alone, `xcodegen generate` writes the file anew without it. | `codesign` on the exported app, in the build (`export-island-ipa.py`) |
| A key Apple takes for Production | `[apns]` in am.toml, the key at the path it names | Asking both of Apple's hosts with the key and a token that is none: a host that takes the key refuses only the token |
| The island's own App ID and profile | QNTX-App `.github/workflows/island-apple.yml` asks Apple and makes what is missing | The build installs the profile and names it in the export |
| The press compiled into the app by Xcode | QNTX-App `island/app`, `island/shared` as sources of the app target. A Tauri plugin's Swift is built outside Xcode, and the system finds no press in it. | `Metadata.appintents` in the archived app, in the build |
| The session the app is in | The page holds it and presents it on each request (QNTX-Web `ts/client/session.ts`, `server/auth/presented.go`). The app reads it from the page and keeps it for when there is no page. | The node's log |

## 「[Opus 5.5] What the phone does is in the node's log」

「[Opus 5.5] Nothing on a phone is seen from anywhere else. What the app's side does, it」
「[Opus 5.5] says to the node: lines whose client names itself `QNTX-waits/<build>`, in the」
「[Opus 5.5] node's journal. Begun, a token from Apple or none, not handed over and why,」
「[Opus 5.5] pressed, done, not done and why.」

「[Opus 5.5] What a person decides is written down by the node: who answered, what, and」
「[Opus 5.5] from what.」
