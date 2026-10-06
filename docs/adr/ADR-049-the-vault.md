# ADR-049: The vault

Date: 2026-10-06
Status: Proposed

"i want to integrate better with their sync service if possible, and have
Obsidian be a QNTX thing, I would rather have that than to reinvent everything
Obsidian does, i tried that before actually, and i got somehwere, but truth be
told is that Obsidian is really good at what it is"

"i dont mind continuing the 3 dollars per month, their sync service is great,
it never fails me"

## How it flows

Both ways: what is written in the vault reaches QNTX, and what QNTX holds
reaches the vault.

"1. a persistent branch exists for all repo's that the Vault we assign
directory space to."

"2. meaning, for a given doc folder in one of our repositories, an indication
of some sorts is made,"

"3. QNTX receives webhooks on most events github dispatches"

"4. Similar to how we rebuild datapunt on CUE changes"

"5. And the persistent branch is obsidian-[nameofvault] and it always contains
the abcdsync diff, and people may merge it at times"

"6. And QNTX ensures changes flow back into the Obsidian Vault as well"

"7. The Obsidian Vault is available to the ROOT agent on the machine"

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

## When both sides changed

The same note changed in the vault and on the repository's main before either
side has seen the other:

"main wins"

## Which folders are the vault's

"So, we could do the Same for Obsidian"

As datapunt's inputs are named on its plugin record, `owner/repo@branch:path`
(ADR-002), the vault's folders are named on a record on the node, and the
folder itself carries nothing.
