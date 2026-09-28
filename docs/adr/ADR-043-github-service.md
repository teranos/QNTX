# ADR-043: GitHubService

Date: 2026-09-28
Status: Proposed

"GitHubService needs to be a thing, and QNTX should own it."

A plugin that does GitHub things calls it, and keeps no client or token of its
own.

```toml
[github]
# host = "github.com"
access_token = "ssm:///garden/github-token"
# This box runs the Actions runner.
actions_runner = "/opt/actions-runner"
```

`[github]` replaces `[[plugin.access_token]]`.

## Example

Garden enables `https://github.com/garden/grove`. Garden's runner builds grove
on the same box, under `actions_runner`, and QNTX runs the new grove from
there. Nothing polls.
