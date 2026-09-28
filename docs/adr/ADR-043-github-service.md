# ADR-043: GitHubService

Date: 2026-09-28
Status: Proposed

"GitHubService needs to be a thing, and QNTX should own it."

"because of the GitHubService the ratelimit is kept in one place"

"the GitHubService is per namespace, QNTX should be able to deal with multiple tokens set by Authenticated users"

"to disable it on the Node entirely"

"to see which namespaces have it enabled and if their auth is correct, and wheter is comes from them setting an access token or via OAuth (Oauth path doesnt exist yet, but will)"

## ROOT

- [ ] I log in to QNTX, open the plugin element, press +, enter a repository URL and confirm. The plugin starts disabled; I edit its config and enable it, and I don't think about it anymore.
- [ ] In the GitHub element, I can disable GitHub on the Node entirely.
- [ ] In the GitHub element, I see which namespaces have it enabled, whether their auth is correct, and whether it comes from an access token or OAuth.
- [ ] In the GitHub element's Actions section, at the bottom, I set the runner path (prefilled `/opt/actions-runner`) and a toggle to enable it; it errors when there is no runner. When enabled, runner stats show there.
- [ ] A plugin's new build lands under the runner, and QNTX has it running as fast as it can. Nothing polls.

## SUPER

- [ ] Administering a namespace, I set its GitHub access token on the panel with the button to create a new canvas, through the GitHub button.
- [ ] My namespace's GitHubService is then set up; a plugin in my namespace calls it for GitHub things and holds no token of its own.
- [ ] My namespace's GitHub rate limit is kept in one place.

## Decided

"that means no am.toml"

The plugin element is the only place a plugin is added, configured and enabled.

"qntx has the concept of tokens"

"it would not be an attestation"

A `GITHUB` token is the third kind, after `OAUTH` and `REFRESH`.

"the node should be the root identity github for github as a base"

The plugin's workflow runs on the box runner and its package step writes
`qntx-<name>-plugin-<ver>-<os>-<arch>.tar.gz` and its `.sha256` under the
runner. QNTX sees the file land, checks it against the `.sha256`, takes it into
its own plugin directory and restarts the plugin. Nothing is downloaded from
GitHub.

"yes, i want this."

"When a new namespace get's created, it doesnt have access to the service by default"

"depending on who does it, the namespace will take it from that user"

"that isolates crazy users or agents"
