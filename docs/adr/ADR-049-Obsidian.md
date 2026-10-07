# ADR-049: Obsidian

Date: 2026-10-06
Status: Developed in pr #1094 

"i want to integrate better with Obsidian's sync service if possible, and have
Obsidian be a QNTX thing, I would rather have that than to reinvent everything
Obsidian does, i tried that before actually, and i got somewhere, but truth be
told is that Obsidian is really good at what it is and I like using it as is."

"..., their sync service is great, it never fails me"

## How it flows

Both ways: what is written in the vault reaches QNTX, and what QNTX holds
reaches the vault.

"1. a persistent branch exists for all repo's that the Vault we assign
directory space to."

"2. meaning, for a given doc folder in one of our repositories, an indication
of some sorts is made,"

"3. QNTX receives webhooks on most events github dispatches"

"4. And the persistent branch is obsidian-[nameofvault] and it always contains
the abcdsync diff, and people may merge it at times"

"5. And QNTX ensures changes flow back into the Obsidian Vault as well"

"6. The Obsidian Vault is available to the ROOT agent on the machine"

- A repository's docs are edited from any device the vault is on, the phone
  included, and the repository takes what is merged from `obsidian-<vault>`.
- An Obsidian plugin runs inside the app on every device and can call the
  node; Sync keeps doing the syncing.

## On the box

Obsidian Headless (`obsidian-headless`, open beta, Node.js 22 or later) is
Obsidian's own client for Sync without the app: `ob login`, `ob sync-setup`,
`ob sync --continuous`. On the box it keeps a copy of the vault, which is how
the ROOT agent reaches it as files, and what is written into that copy reaches
every device through Sync.

"Fine, i will deal with this"

- Sync is end-to-end encrypted, and the box is one of the ends: the vault is
  readable there, by the node and by the ROOT agent (ADR-048).

"but you make a good point about ROOT agent"

"just make it do everything essentially"

- The ROOT agent sets the vault up on the box and keeps it syncing: `ob login`,
  `ob sync-setup`, `ob sync --continuous`. Each takes its answers as flags
  (`--email`, `--password`, `--mfa`, `--vault`, `--path`), so none of it waits
  on a prompt.

## The Obsidian element

"you would think there would be a Obsidian Element to make it a bit easier"

"and i guess i want to do the docs tracking in that Element as well"

- The vault is set up, and the folders it holds are named, from an element in
  the tray.

## When both sides changed

The same note changed in the vault and on the repository's main before either
side has seen the other:

"main wins"

## Which folders are the vault's

"So, we could do the Same for Obsidian"

As datapunt's inputs are named on its plugin record, `owner/repo@branch:path`
(ADR-002), the vault's folders are named on a record on the node, and the
folder itself carries nothing.

"name both ends"

- A folder is `owner/repo@branch:path=place`, the place being where in the
  vault it is. A place is inside the vault, and no place is another's or
  holds another, so a note is one folder's.

"and Clean Business will mostly be steered from the Obsidian"

## What reaches the vault

- Only notes: `.md` files. What else the repository's folder holds stays there.
- Asked whether a note deleted on main is deleted from the vault too:

"yes"

- A note main had and has no more is removed from the vault. A note main
  never had is the vault's own and stays.

## What reaches the repository

"i dont want that to be automatically opted in,"

"and i want to set what the name of the branch would be in the obsidian element in the binding."

- A bound folder sends nothing until a branch is named for it in the element.

"concept of default branch, not main or master"

- A folder is bound to its repository's default branch, as GitHub names it.

"it should show red, and have you redo the binding"

- When the default branch is another, the binding is red until it is bound again.

"oh, require it to be a dir in the repo , not in its root"

- A folder is bound to a folder inside the repository, never to its top.
