# ADR-043: GitHubService

Date: 2026-09-28
Status: Proposed

brandon: "So, this concept, it will be called Forge in the future, 1356, 1352, 1349 are where work already started."

"GitHubService needs to be a thing, and QNTX should own it."

"because of the GitHubService the ratelimit is kept in one place"

"the GitHubService is per namespace, QNTX should be able to deal with multiple tokens set by Authenticated users"


"to see which namespaces have it enabled and if their auth is correct, and wheter is comes from them setting an access token or via OAuth (Oauth path doesnt exist yet, but will)"

## ROOT

- [ ] I log in to QNTX, open the plugin element, press +, enter a repository URL and confirm. The plugin starts disabled; I edit its config and enable it, and I don't think about it anymore.
- [ ] In the GitHub element, I can disable GitHub on the Node entirely.
- [ ] In the GitHub element, I see which namespaces have it enabled, whether their auth is correct, and whether it comes from an access token, OAuth or system namespace
- [ ] In the GitHub element's Actions section, at the bottom, I set the runner path (prefilled `/opt/actions-runner`) and a toggle to enable it; it errors when there is no runner. When enabled, runner stats show there.
- [ ] A plugin's new build lands under the runner, and QNTX has it running as fast as it can. Nothing polls.

## SUPER

- [ ] Administering a namespace, I set its GitHub access token on the panel with the button to create a new canvas, through the GitHub button.
- [ ] My namespace's GitHubService is then set up; a plugin in my namespace calls it for GitHub things and holds no token of its own.
- [ ] My namespace's GitHub rate limit is kept in one place.

## Decided

"that means no am.toml"


The App's own credentials are the operator's, and they are in am.toml under
`[auth.provider.github]`, each secret an `ssm://` or `env:` reference, as
Google's and Apple's are. `client_id` and `client_secret` are what the OAuth
ceremony spends. `private_key` is the App's private key, PEM, which signs the
JWT GitHub requires of a call made as the App itself, such as listing and
redelivering the webhook's deliveries.

"it would not be an attestation"

A `GITHUB` token is the third kind, after `OAUTH` and `REFRESH`.

"the node should be the root identity github for github as a base"

A plugin's workflow that runs on the box runner has its package step write
`qntx-<name>-plugin-<ver>-<os>-<arch>.tar.gz` and its `.sha256` under the
runner. QNTX sees the file land, checks it against the `.sha256`, takes it into
its own plugin directory and restarts the plugin.

A build is looked for at the root of a job's workspace, `<runner>/_work/<repo>/<repo>/`, and
no deeper.

"do you want plugin builds downloaded from GitHub?"

"yes"

A plugin built by a runner on another machine reaches the node as a GitHub Actions artifact of a successful workflow run in the plugin record's repository. The artifact is named as the archive is, and holds the archive and its `.sha256`. The App's `workflow_run` delivery is what has the node download
it, as the App's installation where the repository is; QNTX checks it against the `.sha256` as it checks a build under the runner, and a run older than the
one whose build is installed is not installed over it. A repository no enabled plugin record names has nothing downloaded from it.

A plugin record's URL names the repository and the branch the plugin is built
from: a tree URL's branch, or the repository's default branch when it names
none. A run counts for the plugin only on that repository and that branch.

"yes, i want this."

"When a new namespace get's created, it doesnt have access to the service by default"

"depending on who does it, the namespace will take it from that user"


