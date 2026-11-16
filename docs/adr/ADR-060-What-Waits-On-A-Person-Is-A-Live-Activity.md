# 「[Opus 5.5] ADR-060: What waits on a person is a Live Activity」

「[Opus 5.5] Date: 2026-11-14」
「[Opus 5.5] Status: Decided」

"「[Opus 5.5] the magic island」"

"「[Opus 5.5] AND ON MY MAC AS WELL」"

## 「[Opus 5.5] The rule」

「[Opus 5.5] A collection that wakes its receiver stands in front of the person while it」
「[Opus 5.5] is pending: on the iPhone's island and lock screen, and in the menu bar of」
「[Opus 5.5] their Mac. The node starts it with the app closed.」

「[Opus 5.5] It shows who it is for, how many things wait, the first of them, and the time」
「[Opus 5.5] left, counting down.」

「[Opus 5.5] Its answers are on it. A press there is the answer, and the node does it as」
「[Opus 5.5] the person.」

「[Opus 5.5] This is the one way a person is told what waits on them.」

## 「[Opus 5.5] A press」

"「[Opus 5.5] CANCEL SHOULD BE ONE THING」"

"「[Opus 5.5] APPROVE SHOULD BE ONE THING」"

「[Opus 5.5] A press is one of three: approve, delay, cancel. Approve is the yes, whatever」
「[Opus 5.5] it is the yes to. Cancel is the no.」

「[Opus 5.5] A press is drawn as its mark and never as a word. The node names what each」
「[Opus 5.5] press is (`waitsPress.Does`), and the phone gives each its mark in one place」
「[Opus 5.5] (`markOf`), so cancel is the same mark wherever it stands.」

「[Opus 5.5] A press asks the node at a path of the node's own, in the session the app is」
「[Opus 5.5] in. No token is made for it. An answer is the human's, and a token is refused」
「[Opus 5.5] ([ADR-052](ADR-052-Approvals.md)).」

「[Opus 5.5] Every way a press ends is said: on what stands, and in the node's log with」
「[Opus 5.5] who decided.」

## 「[Opus 5.5] The collection, and its receiver」

"「[Opus 5.5] I SEE IT AS PART OF THE COLLECTION, APPROVABLE IN IT」"

"「[Opus 5.5] ITS DIRECTIONAL, SO WE CARE ABOUT THE RECEIVER」"

"「[Opus 5.5] IF WE KNOW SESSION A WAS INVOLVED, THATS WHY WE KNOW WHERE TO SEND」"

"「[Opus 5.5] IF SESSION A AND B ARE INVOLVED, THEY EACH GET THE PR ADDED ONTO THERI COLLECTION」"

"「[Opus 5.5] SO THEY KNOW DURING THE NEXT WAKE」"

「[Opus 5.5] What stands is a collection, named by its receiver.」

「[Opus 5.5] A pull request is an item in the receiver's collection, beside what agents」
「[Opus 5.5] said, and is approved there. It never stands on its own.」

「[Opus 5.5] A pull request goes to the session that was involved in it, and to each where」
「[Opus 5.5] there were two. A session is involved when it asks GitHub about the pull」
「[Opus 5.5] request through the node. They know of it at their next wake.」

「[Opus 5.5] Pressing the collection brings the person to the receiver's chat, with the」
「[Opus 5.5] collection open over it as the overlay.」

## 「[Opus 5.5] Approval packs」

"「[Opus 5.5] PREVIOUSLY THIS IDEA ALSO HAD ANOTHER NAME, CALLED APPROVAL PACKS, WHICH ARE COMPLETE PACKAGES OF PR'S AND OTHER THINGS THAT CAN GET APPROVED. AN APPROVAL PACK IS ACCUMULATED, AND THEN RELEASED BY THE USER」"

"「[Opus 5.5] APPROVAL PACK -> CAN IMPACT AND PLACE -> INTO COLLECTIONS -> PR'S THINGS TO SAY TO SESSIONS, HANDOVERS, REQUEST FOR REVIEW, ETC.」"

"「[Opus 5.5] AND JUST AS YOU CAN APPROVE AN INDIVIDUAL PR IN A COLLECTION, YOU CAN APPROVE OR DENY AN INDIVIDUAL ITEM IN A PACK」"

「[Opus 5.5] An approval pack is a thing of its own: a complete package of pull requests」
「[Opus 5.5] and other things that can be approved. It accumulates, and the person」
「[Opus 5.5] releases it.」

「[Opus 5.5] A pack places things into collections. A collection holds what goes to its」
「[Opus 5.5] receiver: pull requests, things to say to sessions, handovers, requests for」
「[Opus 5.5] review.」

「[Opus 5.5] The person decides on the whole or on one item, in a pack and in a」
「[Opus 5.5] collection.」

## 「[Opus 5.5] How long it stands」

"「[Opus 5.5] SO THE 6H ONE STAYS FOR 6H」"

"「[Opus 5.5] WHEN DELAY CANT WE MAKE IT BE DISMISSED UNTIL ITS TIME TO SHOW AGAIN /」"

"「[Opus 5.5] SO WE NEVER BREAK APPLES RULE YOU KNOW」"

「[Opus 5.5] A collection stands for its wait: six hours, or three for an agent on Opus」
「[Opus 5.5] (`waitOf`). It stands from when the first thing that wakes is said.」

「[Opus 5.5] Delayed or held, it is dismissed at once and stands again for the wait before」
「[Opus 5.5] its new time.」

「[Opus 5.5] One Live Activity stands for one time. No Live Activity stands longer than its」
「[Opus 5.5] wait, and the wait is under the eight hours Apple allows.」

## 「[Opus 5.5] What wakes」

"「[Opus 5.5] NO, ANY ONE THAT WAKES. PR MERGE UPDATES DONT WAKE PER SE, THEY GET COLLECTED ON A WAKING COLLECTION」"

「[Opus 5.5] What stands is a collection that wakes. An item that wakes nobody is due at」
「[Opus 5.5] no time, is not delivered for itself, and makes nothing stand. It is delivered」
「[Opus 5.5] with what does wake. A pull request merge update is such an item.」

## 「[Opus 5.5] One shape」

「[Opus 5.5] One Live Activity type draws everything that waits: `WaitsAttributes`」
「[Opus 5.5] (QNTX-App `island/shared/WaitsAttributes.swift`). The node fills it as」
「[Opus 5.5] `waitsOf` and `waitsShown` (`server/phones.go`). The two are one shape, key」
「[Opus 5.5] for key.」

「[Opus 5.5] The node says everything that is shown, its presses among it. A new kind of」
「[Opus 5.5] thing that waits is added on the node. The app is not built again for it.」

## 「[Opus 5.5] What it rests on」

「[Opus 5.5] Each of these was missing once, and nothing stood on the phone.」

| It needs | Where it is said | Read back by |
|---|---|---|
| The app signed with `aps-environment` | `entitlements.properties` in the generated `project.yml` (QNTX-App `.github/scripts/ios-project.py`). `xcodegen generate` writes the entitlements file from the project, so the project is where it is said. | `codesign` on the exported app, in the build (`export-island-ipa.py`). A build without it is not uploaded. |
| A key Apple takes for Production | `[apns]` in am.toml, the key at the path it names. The key is made at Apple with Environment set to Sandbox & Production. | Both of Apple's hosts asked with the key and a token that is none: a host that takes the key refuses only the token. |
| The island's own App ID and profile | QNTX-App `.github/workflows/island-apple.yml` asks Apple and makes what is missing. | The build installs the profile and names it in the export. |
| The press compiled into the app by Xcode | QNTX-App `island/app` and `island/shared` are sources of the app target. A Tauri plugin's Swift is built outside Xcode, and the system finds no press in it. | `Metadata.appintents` in the archived app, in the build. |
| The session the app is in | The page holds it and presents it on each request (QNTX-Web `ts/client/session.ts`, `server/auth/presented.go`). The app reads it from the page and keeps it in the Keychain. | The node's log. |

## 「[Opus 5.5] What the phone does is in the node's log」

「[Opus 5.5] The app says each step it takes to the node. The lines are in the node's」
「[Opus 5.5] journal, by a client that names itself `QNTX-waits/<build>`:」

```
「[Opus 5.5] journalctl -u qntx | grep QNTX-waits」
```

「[Opus 5.5] The node writes down what a person decides: who, what, and the client it came」
「[Opus 5.5] from.」

## 「[Opus 5.5] Settled」

「[Opus 5.5] These are decided. A session builds on them and does not reopen them.」

- 「[Opus 5.5] A person is told what waits by a Live Activity, and by nothing else.」
- 「[Opus 5.5] It stands for its wait. No fixed number of minutes before delivery decides it.」
- 「[Opus 5.5] A press is a mark, never a word.」
- 「[Opus 5.5] A press is the person's own session. No token, pass or key is made for it.」
- 「[Opus 5.5] A pull request never stands on its own.」
- 「[Opus 5.5] What wakes nobody makes nothing stand.」
- 「[Opus 5.5] No Live Activity is restarted to outlast Apple's eight hours.」
- 「[Opus 5.5] The node says what is shown. The app adds no kinds of its own.」
- 「[Opus 5.5] A change to this is proven on a phone before it is called done, and the docs follow the proof.」

## 「[Opus 5.5] What runs」

「[Opus 5.5] Seen on the person's iPhone and Mac:」

| When (UTC) | What |
|---|---|
| 2026-11-13 23:18:34 | The node started Live Activities with the app closed. |
| 2026-11-13 23:20:11 | A press merged the deploy repository's #47, by the node as its App. |
| 2026-11-13 23:28:56 | A press from the Mac merged teranos/QNTX#1732. |
| 2026-11-14 00:58:31 | A collection pending for a session stood, with its countdown and three marks. |

「[Opus 5.5] Running on q: a collection that wakes stands for its wait and is dismissed on」
「[Opus 5.5] a delay; the node logs each decision with who made it.」

## 「[Opus 5.5] What is not built」

- 「[Opus 5.5] A pull request as an item in a collection, approved there.」
- 「[Opus 5.5] A merge collected for the sessions involved (teranos/QNTX#1741, in review).」
- 「[Opus 5.5] Pressing the collection to open the receiver's chat with the overlay.」
- 「[Opus 5.5] Approval packs.」
- 「[Opus 5.5] A proof in the iPhone Simulator in CI, so a change is shown working without a phone.」

## 「[Opus 5.5] Open defect」

「[Opus 5.5] The node answered 502 to the press that merged teranos/QNTX#1732. The pull」
「[Opus 5.5] request was merged. The node logs the reason of such an answer since」
「[Opus 5.5] teranos/QNTX#1734.」
